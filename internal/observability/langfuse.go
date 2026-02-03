package observability

import (
	"context"
	"fmt"
	"time"

	"knowledge-agent/internal/config"
	"knowledge-agent/internal/logger"

	langfuse "github.com/git-hulk/langfuse-go"
	"github.com/git-hulk/langfuse-go/pkg/traces"
)

// LangfuseTracer wraps git-hulk/langfuse-go client for LLM observability
type LangfuseTracer struct {
	client  *langfuse.Langfuse
	enabled bool
	config  *config.LangfuseConfig
}

// NewLangfuseTracer creates a new Langfuse tracer using git-hulk/langfuse-go
func NewLangfuseTracer(cfg *config.LangfuseConfig) (*LangfuseTracer, error) {
	if !cfg.Enabled {
		return &LangfuseTracer{enabled: false, config: cfg}, nil
	}

	if cfg.PublicKey == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("langfuse public_key and secret_key are required when enabled")
	}

	// Create client with git-hulk SDK
	client := langfuse.NewClient(cfg.Host, cfg.PublicKey, cfg.SecretKey)

	log := logger.Get()
	log.Infow("Langfuse tracing enabled",
		"host", cfg.Host,
		"public_key_prefix", cfg.PublicKey[:min(10, len(cfg.PublicKey))]+"...",
		"sdk", "git-hulk/langfuse-go",
	)

	return &LangfuseTracer{
		client:  client,
		enabled: true,
		config:  cfg,
	}, nil
}

// IsEnabled returns whether Langfuse tracing is enabled
func (t *LangfuseTracer) IsEnabled() bool {
	return t.enabled
}

// QueryTrace tracks a complete query execution with ADK events
type QueryTrace struct {
	trace     *traces.Trace
	tracer    *LangfuseTracer
	startTime time.Time
	metadata  map[string]any
	TraceID   string // Exported for external access

	// ADK event tracking
	generations      []*traces.Observation
	toolCalls        map[string]*traces.Observation // Map tool name to observation
	eventCount       int
	promptTokens     int
	completionTokens int
	totalTokens      int
}

// StartQueryTrace starts tracing a query
func (t *LangfuseTracer) StartQueryTrace(ctx context.Context, question string, metadata map[string]any) *QueryTrace {
	if !t.enabled {
		return &QueryTrace{
			tracer:    t,
			startTime: time.Now(),
			metadata:  metadata,
		}
	}

	log := logger.Get()

	// Create trace using git-hulk SDK
	trace := t.client.StartTrace(ctx, "knowledge-agent-query")

	// Set trace properties
	trace.Input = map[string]any{"question": question}
	trace.Tags = []string{"query", "knowledge-agent"}
	trace.Metadata = metadata

	// Extract user_name from metadata for UserID
	if userName, ok := metadata["user_name"].(string); ok && userName != "" {
		trace.UserID = userName
	}

	log.Infow("Langfuse trace created",
		"trace_id", trace.ID,
		"question_length", len(question),
		"metadata_keys", getKeys(metadata),
	)

	return &QueryTrace{
		trace:       trace,
		tracer:      t,
		startTime:   time.Now(),
		metadata:    metadata,
		TraceID:     trace.ID,
		generations: make([]*traces.Observation, 0),
		toolCalls:   make(map[string]*traces.Observation),
	}
}

// Helper function to get keys from metadata
func getKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// StartGeneration starts tracking an LLM generation
func (qt *QueryTrace) StartGeneration(modelName string, input any) *traces.Observation {
	if !qt.tracer.enabled || qt.trace == nil {
		return nil
	}

	log := logger.Get()
	qt.eventCount++

	// Create generation observation
	generation := qt.trace.StartGeneration(fmt.Sprintf("generation-%d", qt.eventCount))
	generation.Model = modelName
	generation.Input = input

	qt.generations = append(qt.generations, generation)

	log.Debugw("Started LLM generation",
		"trace_id", qt.TraceID,
		"model", modelName,
		"generation_number", len(qt.generations),
	)

	return generation
}

// EndGeneration ends the LLM generation tracking
func (qt *QueryTrace) EndGeneration(generation *traces.Observation, output any, promptTokens, completionTokens int) {
	if !qt.tracer.enabled || generation == nil {
		return
	}

	log := logger.Get()

	// Set generation output and usage
	generation.Output = output
	generation.Usage = traces.Usage{
		Input:  promptTokens,
		Output: completionTokens,
		Total:  promptTokens + completionTokens,
		Unit:   traces.UnitTokens,
	}

	// Accumulate tokens
	qt.promptTokens += promptTokens
	qt.completionTokens += completionTokens
	qt.totalTokens += (promptTokens + completionTokens)

	generation.End()

	log.Debugw("Completed LLM generation",
		"trace_id", qt.TraceID,
		"prompt_tokens", promptTokens,
		"completion_tokens", completionTokens,
		"total_tokens", promptTokens+completionTokens,
	)
}

// StartToolCall starts tracking a tool call
func (qt *QueryTrace) StartToolCall(toolName string, args map[string]any) {
	if !qt.tracer.enabled || qt.trace == nil {
		return
	}

	log := logger.Get()
	qt.eventCount++

	// Create tool observation
	tool := qt.trace.StartObservation(toolName, traces.ObservationTypeTool)
	tool.Input = args

	qt.toolCalls[toolName] = tool

	log.Debugw("Started tool call",
		"trace_id", qt.TraceID,
		"tool_name", toolName,
	)
}

// EndToolCall ends the tool call tracking
func (qt *QueryTrace) EndToolCall(toolName string, output any, err error) {
	if !qt.tracer.enabled {
		return
	}

	log := logger.Get()

	tool, exists := qt.toolCalls[toolName]
	if !exists {
		log.Warnw("Tool call not found in trace",
			"trace_id", qt.TraceID,
			"tool_name", toolName,
		)
		return
	}

	// Set tool output
	tool.Output = output

	if err != nil {
		tool.Level = traces.ObservationLevelError
		tool.StatusMessage = err.Error()
	}

	tool.End()

	log.Debugw("Completed tool call",
		"trace_id", qt.TraceID,
		"tool_name", toolName,
		"error", err,
	)
}

// CalculateTotalCost calculates the total cost based on token usage
func (qt *QueryTrace) CalculateTotalCost(modelName string, inputCostPer1M float64, outputCostPer1M float64) float64 {
	inputCost := (float64(qt.promptTokens) / 1_000_000) * inputCostPer1M
	outputCost := (float64(qt.completionTokens) / 1_000_000) * outputCostPer1M
	totalCost := inputCost + outputCost

	// Store in metadata
	qt.metadata["model"] = modelName
	qt.metadata["input_cost_per_1m"] = inputCostPer1M
	qt.metadata["output_cost_per_1m"] = outputCostPer1M
	qt.metadata["total_cost_usd"] = totalCost
	qt.metadata["prompt_tokens"] = qt.promptTokens
	qt.metadata["completion_tokens"] = qt.completionTokens
	qt.metadata["total_tokens"] = qt.totalTokens

	return totalCost
}

// GetAccumulatedTokens returns accumulated token counts
func (qt *QueryTrace) GetAccumulatedTokens() (promptTokens, completionTokens, totalTokens int) {
	return qt.promptTokens, qt.completionTokens, qt.totalTokens
}

// GetSummary returns a summary of the trace
func (qt *QueryTrace) GetSummary() map[string]any {
	return map[string]any{
		"generations_count": len(qt.generations),
		"tool_calls_count":  len(qt.toolCalls),
		"total_events":      qt.eventCount,
		"prompt_tokens":     qt.promptTokens,
		"completion_tokens": qt.completionTokens,
		"total_tokens":      qt.totalTokens,
	}
}

// End finishes the query trace
func (qt *QueryTrace) End(success bool, answer string) {
	if !qt.tracer.enabled || qt.trace == nil {
		return
	}

	log := logger.Get()

	// Calculate total cost
	modelName := "unknown"
	if model, ok := qt.metadata["model"].(string); ok {
		modelName = model
	}
	totalCost := qt.CalculateTotalCost(
		modelName,
		qt.tracer.config.InputCostPer1M,
		qt.tracer.config.OutputCostPer1M,
	)

	// Set trace output
	qt.trace.Output = map[string]any{
		"success":           success,
		"answer":            answer,
		"duration_ms":       time.Since(qt.startTime).Milliseconds(),
		"prompt_tokens":     qt.promptTokens,
		"completion_tokens": qt.completionTokens,
		"total_tokens":      qt.totalTokens,
		"generations_count": len(qt.generations),
		"tool_calls_count":  len(qt.toolCalls),
	}

	// Set total cost
	qt.trace.TotalCost = totalCost

	// Update metadata
	qt.trace.Metadata = qt.metadata

	// End trace (automatically calculates latency and submits to batch)
	qt.trace.End()

	log.Infow("Trace completed",
		"trace_id", qt.TraceID,
		"success", success,
		"duration_ms", time.Since(qt.startTime).Milliseconds(),
		"prompt_tokens", qt.promptTokens,
		"completion_tokens", qt.completionTokens,
		"total_tokens", qt.totalTokens,
		"total_cost_usd", fmt.Sprintf("$%.6f", totalCost),
		"generations_count", len(qt.generations),
		"tool_calls_count", len(qt.toolCalls),
	)
}

// IngestTrace tracks a complete ingest operation with memory saves
type IngestTrace struct {
	trace       *traces.Trace
	tracer      *LangfuseTracer
	startTime   time.Time
	metadata    map[string]any
	TraceID     string // Exported for external access
	memorySaves int
}

// StartIngestTrace starts tracing an ingest operation
func (t *LangfuseTracer) StartIngestTrace(ctx context.Context, threadTS, channelID string, msgCount int, metadata map[string]any) *IngestTrace {
	if !t.enabled {
		return &IngestTrace{
			tracer:    t,
			startTime: time.Now(),
			metadata:  metadata,
		}
	}

	log := logger.Get()

	// Create trace using git-hulk SDK
	trace := t.client.StartTrace(ctx, "knowledge-agent-ingest")

	// Set trace properties
	trace.Input = map[string]any{
		"thread_ts":     threadTS,
		"channel_id":    channelID,
		"message_count": msgCount,
	}
	trace.Tags = []string{"ingest", "knowledge-agent"}
	trace.Metadata = metadata

	// Extract user_name from metadata for UserID
	if userName, ok := metadata["user_name"].(string); ok && userName != "" {
		trace.UserID = userName
	}

	log.Infow("Langfuse ingest trace created",
		"trace_id", trace.ID,
		"thread_ts", threadTS,
		"channel_id", channelID,
		"message_count", msgCount,
	)

	return &IngestTrace{
		trace:     trace,
		tracer:    t,
		startTime: time.Now(),
		metadata:  metadata,
		TraceID:   trace.ID,
	}
}

// RecordMemorySave records a memory save operation within the ingest trace
func (it *IngestTrace) RecordMemorySave(content string) {
	it.memorySaves++

	if !it.tracer.enabled || it.trace == nil {
		return
	}

	log := logger.Get()

	// Create a span for the memory save
	span := it.trace.StartSpan(fmt.Sprintf("memory-save-%d", it.memorySaves))
	span.Input = map[string]any{"content_preview": truncateForLog(content, 200)}
	span.End()

	log.Debugw("Memory save recorded in ingest trace",
		"trace_id", it.TraceID,
		"save_number", it.memorySaves,
	)
}

// End finishes the ingest trace
func (it *IngestTrace) End(success bool, summary string) {
	if !it.tracer.enabled || it.trace == nil {
		return
	}

	log := logger.Get()

	// Set trace output
	it.trace.Output = map[string]any{
		"success":       success,
		"summary":       truncateForLog(summary, 500),
		"duration_ms":   time.Since(it.startTime).Milliseconds(),
		"memories_saved": it.memorySaves,
	}

	// Update metadata
	it.trace.Metadata = it.metadata

	// End trace
	it.trace.End()

	log.Infow("Ingest trace completed",
		"trace_id", it.TraceID,
		"success", success,
		"duration_ms", time.Since(it.startTime).Milliseconds(),
		"memories_saved", it.memorySaves,
	)
}

// truncateForLog truncates a string for logging purposes
func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// Flush flushes pending traces to Langfuse
func (t *LangfuseTracer) Flush() error {
	if !t.enabled {
		return nil
	}

	t.client.Flush()
	return nil
}

// Close closes the Langfuse client with graceful shutdown
func (t *LangfuseTracer) Close() error {
	if !t.enabled {
		return nil
	}

	log := logger.Get()
	log.Info("Closing Langfuse client (with automatic flush)...")

	// Close automatically flushes pending traces
	if err := t.client.Close(); err != nil {
		log.Warnw("Error closing Langfuse client", "error", err)
		return err
	}

	log.Info("Langfuse client closed successfully")
	return nil
}

package observability

import (
	"context"
	"fmt"
	"sync/atomic"
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
	eventCount       atomic.Int32                   // Thread-safe event counter
	promptTokens     int
	completionTokens int
	totalTokens      int
}

// StartQueryTrace starts tracing a query
// sessionID groups related traces in Langfuse's Sessions view (e.g., "thread-C123-1234567890.123456")
func (t *LangfuseTracer) StartQueryTrace(ctx context.Context, question string, sessionID string, metadata map[string]any) *QueryTrace {
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

	// Set session ID for grouping in Langfuse Sessions view
	if sessionID != "" {
		trace.SessionID = sessionID
	}

	// Extract user identity for UserID - prefer email over username
	if userEmail, ok := metadata["user_email"].(string); ok && userEmail != "" {
		trace.UserID = userEmail
	} else if userName, ok := metadata["user_name"].(string); ok && userName != "" {
		trace.UserID = userName
	}

	log.Infow("Langfuse trace created",
		"trace_id", trace.ID,
		"session_id", sessionID,
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
	qt.eventCount.Add(1)

	// Create generation observation
	generation := qt.trace.StartGeneration(fmt.Sprintf("generation-%d", qt.eventCount.Load()))
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
// toolID is the unique identifier for this specific tool call (from FunctionCall.ID)
// toolName is the name of the tool being called
func (qt *QueryTrace) StartToolCall(toolID, toolName string, args map[string]any) {
	if !qt.tracer.enabled || qt.trace == nil {
		return
	}

	log := logger.Get()
	qt.eventCount.Add(1)

	// Use toolID as the key to support multiple calls to the same tool
	// The observation name includes both for readability in Langfuse UI
	observationName := toolName
	if toolID != "" {
		observationName = fmt.Sprintf("%s", toolName)
	}

	// Create tool observation
	tool := qt.trace.StartObservation(observationName, traces.ObservationTypeTool)
	tool.Input = map[string]any{
		"tool_id": toolID,
		"args":    args,
	}

	// Use toolID as key if available, otherwise fallback to toolName
	key := toolID
	if key == "" {
		key = toolName
	}
	qt.toolCalls[key] = tool

	log.Debugw("Started tool call",
		"trace_id", qt.TraceID,
		"tool_id", toolID,
		"tool_name", toolName,
	)
}

// EndToolCall ends the tool call tracking
// toolID is the unique identifier for this specific tool call (from FunctionResponse.ID)
// toolName is included for logging purposes
func (qt *QueryTrace) EndToolCall(toolID, toolName string, output any, err error) {
	if !qt.tracer.enabled {
		return
	}

	log := logger.Get()

	// Use toolID as key if available, otherwise fallback to toolName
	key := toolID
	if key == "" {
		key = toolName
	}

	tool, exists := qt.toolCalls[key]
	if !exists {
		log.Warnw("Tool call not found in trace",
			"trace_id", qt.TraceID,
			"tool_id", toolID,
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

	// Remove from map after ending
	delete(qt.toolCalls, key)

	log.Debugw("Completed tool call",
		"trace_id", qt.TraceID,
		"tool_id", toolID,
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
		"total_events":      qt.eventCount.Load(),
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

// truncateForLog truncates a string for logging purposes
func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// Context key for QueryTrace
type queryTraceKey struct{}

// ContextWithQueryTrace adds a QueryTrace to the context
func ContextWithQueryTrace(ctx context.Context, qt *QueryTrace) context.Context {
	return context.WithValue(ctx, queryTraceKey{}, qt)
}

// QueryTraceFromContext retrieves the QueryTrace from context
func QueryTraceFromContext(ctx context.Context) *QueryTrace {
	if qt, ok := ctx.Value(queryTraceKey{}).(*QueryTrace); ok {
		return qt
	}
	return nil
}

// RecordA2ACall records an A2A sub-agent call with the cleaned query
func (qt *QueryTrace) RecordA2ACall(agentName, originalQuery, cleanedQuery string, durationMs int64) {
	if !qt.tracer.enabled || qt.trace == nil {
		return
	}

	log := logger.Get()
	qt.eventCount.Add(1)

	// Create a span for the A2A call
	span := qt.trace.StartSpan(fmt.Sprintf("a2a-call-%s", agentName))
	span.Input = map[string]any{
		"agent":          agentName,
		"original_query": truncateForLog(originalQuery, 500),
		"cleaned_query":  cleanedQuery,
		"original_len":   len(originalQuery),
		"cleaned_len":    len(cleanedQuery),
	}
	// Calculate reduction percentage (avoid division by zero)
	reductionPercent := 0.0
	if len(originalQuery) > 0 {
		reductionPercent = float64(len(originalQuery)-len(cleanedQuery)) / float64(len(originalQuery)) * 100
	}
	span.Output = map[string]any{
		"reduction_percent": fmt.Sprintf("%.1f%%", reductionPercent),
		"duration_ms":       durationMs,
	}
	span.End()

	log.Debugw("Recorded A2A call in trace",
		"trace_id", qt.TraceID,
		"agent", agentName,
		"original_len", len(originalQuery),
		"cleaned_len", len(cleanedQuery),
		"duration_ms", durationMs,
	)
}

// RecordPreSearch records a pre-search memory operation
func (qt *QueryTrace) RecordPreSearch(query string, resultCount int, duration time.Duration) {
	if !qt.tracer.enabled || qt.trace == nil {
		return
	}

	log := logger.Get()
	qt.eventCount.Add(1)

	// Create a span for the pre-search operation
	span := qt.trace.StartSpan("pre-search-memory")
	span.Input = map[string]any{
		"query": truncateForLog(query, 200),
	}
	span.Output = map[string]any{
		"results_count": resultCount,
		"duration_ms":   duration.Milliseconds(),
	}
	span.End()

	log.Debugw("Recorded pre-search in trace",
		"trace_id", qt.TraceID,
		"query_len", len(query),
		"results_count", resultCount,
		"duration_ms", duration.Milliseconds(),
	)
}

// RecordRESTCall records a REST sub-agent call
func (qt *QueryTrace) RecordRESTCall(agentName, query, response string, duration time.Duration, err error) {
	if !qt.tracer.enabled || qt.trace == nil {
		return
	}

	log := logger.Get()
	qt.eventCount.Add(1)

	// Create a span for the REST call
	span := qt.trace.StartSpan(fmt.Sprintf("rest-call-%s", agentName))
	span.Input = map[string]any{
		"agent": agentName,
		"query": truncateForLog(query, 500),
	}

	output := map[string]any{
		"response_length": len(response),
		"duration_ms":     duration.Milliseconds(),
	}
	if err != nil {
		output["error"] = err.Error()
		span.Level = traces.ObservationLevelError
		span.StatusMessage = err.Error()
	}
	span.Output = output
	span.End()

	log.Debugw("Recorded REST call in trace",
		"trace_id", qt.TraceID,
		"agent", agentName,
		"query_len", len(query),
		"response_len", len(response),
		"duration_ms", duration.Milliseconds(),
		"error", err,
	)
}

// RecordSessionRepair records a session repair operation (corrupted session recovery)
func (qt *QueryTrace) RecordSessionRepair(sessionID string, attempt int) {
	if !qt.tracer.enabled || qt.trace == nil {
		return
	}

	log := logger.Get()
	qt.eventCount.Add(1)

	// Create a span for the session repair
	span := qt.trace.StartSpan("session-repair")
	span.Input = map[string]any{
		"session_id": sessionID,
		"attempt":    attempt,
	}
	span.Output = map[string]any{
		"action": "session_deleted_for_retry",
	}
	span.Level = traces.ObservationLevelWarning
	span.StatusMessage = "Corrupted session detected and repaired"
	span.End()

	log.Debugw("Recorded session repair in trace",
		"trace_id", qt.TraceID,
		"session_id", sessionID,
		"attempt", attempt,
	)
}

// RecordAuxiliaryGeneration records a generation from auxiliary LLM calls (summarizer, cleaner)
// Returns usage struct with input/output tokens for caller to track
func (qt *QueryTrace) RecordAuxiliaryGeneration(name, model string, input, output string, inputTokens, outputTokens int64, duration time.Duration) {
	if !qt.tracer.enabled || qt.trace == nil {
		return
	}

	log := logger.Get()
	qt.eventCount.Add(1)

	// Create generation observation
	generation := qt.trace.StartGeneration(name)
	generation.Model = model
	generation.Input = truncateForLog(input, 500)
	generation.Output = truncateForLog(output, 500)
	generation.Usage = traces.Usage{
		Input:  int(inputTokens),
		Output: int(outputTokens),
		Total:  int(inputTokens + outputTokens),
		Unit:   traces.UnitTokens,
	}
	generation.End()

	// Note: We don't accumulate tokens from auxiliary calls to the main trace total
	// because they use different (cheaper) models and shouldn't inflate the main model's costs

	log.Debugw("Recorded auxiliary generation in trace",
		"trace_id", qt.TraceID,
		"name", name,
		"model", model,
		"input_tokens", inputTokens,
		"output_tokens", outputTokens,
		"duration_ms", duration.Milliseconds(),
	)
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

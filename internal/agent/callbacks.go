package agent

import (
	"fmt"
	"time"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/tool"

	"knowledge-agent/internal/logger"
	"knowledge-agent/internal/observability"
)

// beforeModelCallback starts a Langfuse generation span before each LLM call.
func (a *Agent) beforeModelCallback(ctx agent.CallbackContext, req *model.LLMRequest) (*model.LLMResponse, error) {
	trace := observability.QueryTraceFromContext(ctx)
	if trace == nil {
		return nil, nil
	}

	trace.StartGeneration(a.config.Anthropic.Model, req)
	return nil, nil
}

// afterModelCallback ends the Langfuse generation span with token usage.
func (a *Agent) afterModelCallback(ctx agent.CallbackContext, resp *model.LLMResponse, respErr error) (*model.LLMResponse, error) {
	trace := observability.QueryTraceFromContext(ctx)
	if trace == nil {
		return nil, nil
	}

	log := logger.Get()

	// Get the last started generation
	gen := trace.GetLastActiveGeneration()
	if gen == nil {
		log.Debugw("No active generation to end in afterModelCallback")
		return nil, nil
	}

	var output string
	var promptTokens, completionTokens int

	if resp != nil {
		if resp.Content != nil {
			for _, part := range resp.Content.Parts {
				if part.Text != "" {
					output += part.Text
				}
			}
		}
		if resp.UsageMetadata != nil {
			promptTokens = int(resp.UsageMetadata.PromptTokenCount)
			completionTokens = int(resp.UsageMetadata.CandidatesTokenCount)
		}
	}

	trace.EndGeneration(gen, output, promptTokens, completionTokens)
	return nil, nil
}

// beforeToolCallback starts a Langfuse tool span and records the start time.
func (a *Agent) beforeToolCallback(ctx tool.Context, t tool.Tool, args map[string]any) (map[string]any, error) {
	trace := observability.QueryTraceFromContext(ctx)
	if trace == nil {
		return args, nil
	}

	toolName := t.Name()
	toolID := ctx.FunctionCallID()

	log := logger.Get()
	log.Infow("Tool call started", "tool", toolName, "tool_id", toolID)

	trace.StartToolCall(toolID, toolName, args)

	// Store start time in trace metadata for duration calculation
	trace.SetToolStartTime(toolID, toolName, time.Now())

	return args, nil
}

// afterToolCallback ends the Langfuse tool span and records Prometheus metrics.
func (a *Agent) afterToolCallback(ctx tool.Context, t tool.Tool, args, result map[string]any, err error) (map[string]any, error) {
	trace := observability.QueryTraceFromContext(ctx)

	toolName := t.Name()
	toolID := ctx.FunctionCallID()

	log := logger.Get()

	var duration time.Duration
	if trace != nil {
		if startTime, ok := trace.GetToolStartTime(toolID, toolName); ok {
			duration = time.Since(startTime)
		}
	}

	success := err == nil && !containsError(result)

	if err != nil {
		log.Warnw("Tool call returned error", "tool", toolName, "tool_id", toolID, "error", err)
	}

	log.Infow("Tool call completed",
		"tool", toolName,
		"tool_id", toolID,
		"duration_ms", duration.Milliseconds(),
		"success", success,
	)

	// Record Prometheus metrics
	observability.GetMetrics().RecordToolCall(toolName, duration, success)

	// End Langfuse span
	if trace != nil {
		trace.EndToolCall(toolID, toolName, result, err)
	}

	return result, nil
}

// buildCallbacks creates the callback slices for llmagent.Config.
func (a *Agent) buildCallbacks() (
	[]llmagent.BeforeModelCallback,
	[]llmagent.AfterModelCallback,
	[]llmagent.BeforeToolCallback,
	[]llmagent.AfterToolCallback,
) {
	return []llmagent.BeforeModelCallback{a.beforeModelCallback},
		[]llmagent.AfterModelCallback{a.afterModelCallback},
		[]llmagent.BeforeToolCallback{a.beforeToolCallback},
		[]llmagent.AfterToolCallback{a.afterToolCallback}
}

// LogTraceSummary logs the Langfuse trace summary.
func LogTraceSummary(trace *observability.QueryTrace, model string, inputCostPer1M, outputCostPer1M float64) {
	if trace == nil {
		return
	}

	log := logger.Get()

	promptTokens, completionTokens, totalTokens := trace.GetAccumulatedTokens()
	totalCost := trace.CalculateTotalCost(model, inputCostPer1M, outputCostPer1M)
	traceSummary := trace.GetSummary()

	log.Infow("Query trace summary",
		"trace_id", trace.TraceID,
		"prompt_tokens", promptTokens,
		"completion_tokens", completionTokens,
		"total_tokens", totalTokens,
		"total_cost_usd", fmt.Sprintf("$%.6f", totalCost),
		"tool_calls_count", traceSummary["tool_calls_count"],
		"generations_count", traceSummary["generations_count"],
	)
}

package parallel

import (
	"context"
	"iter"
	"sync"
	"time"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"

	"knowledge-agent/internal/config"
	"knowledge-agent/internal/logger"
)

// RunnerWrapper wraps an ADK runner to provide parallel execution metrics
type RunnerWrapper struct {
	baseRunner *runner.Runner
	executor   *Executor
	enabled    bool

	// Metrics
	mu                  sync.Mutex
	totalToolCalls      int64
	parallelizableCalls int64
	timesSaved          time.Duration
}

// NewRunnerWrapper creates a new runner wrapper with parallel execution support
func NewRunnerWrapper(baseRunner *runner.Runner, cfg *config.ParallelConfig) *RunnerWrapper {
	if cfg == nil || !cfg.Enabled {
		return &RunnerWrapper{
			baseRunner: baseRunner,
			enabled:    false,
		}
	}

	executor := NewExecutor(
		cfg.MaxParallelism,
		cfg.ToolTimeout,
		cfg.SequentialTools,
	)

	return &RunnerWrapper{
		baseRunner: baseRunner,
		executor:   executor,
		enabled:    true,
	}
}

// Run executes the runner and tracks parallel execution opportunities
// NOTE: Currently always passes through to base runner directly because wrapping
// the iterator interferes with ADK's internal handling of A2A sub-agent responses.
// The parallel execution monitoring is disabled until we find a way to observe
// events without wrapping the iterator.
func (rw *RunnerWrapper) Run(ctx context.Context, userID, sessionID string, msg *genai.Content, cfg agent.RunConfig) iter.Seq2[*session.Event, error] {
	// Always pass through to base runner directly
	// Wrapping the iterator breaks A2A sub-agent response handling
	return rw.baseRunner.Run(ctx, userID, sessionID, msg, cfg)
}

// pendingToolCall tracks a tool call in progress
type pendingToolCall struct {
	name      string
	args      map[string]any
	startTime time.Time
	endTime   time.Time
	duration  time.Duration
}

// analyzeParallelization logs analysis of potential parallelization savings
func (rw *RunnerWrapper) analyzeParallelization(calls []*pendingToolCall) {
	if len(calls) < 2 {
		return
	}

	log := logger.Get()

	var sequentialTime time.Duration
	var longestCall time.Duration
	toolNames := make([]string, 0, len(calls))

	for _, call := range calls {
		if call.duration > 0 {
			sequentialTime += call.duration
			if call.duration > longestCall {
				longestCall = call.duration
			}
		}
		toolNames = append(toolNames, call.name)
	}

	potentialTime := longestCall
	savedTime := sequentialTime - potentialTime

	if savedTime > 0 {
		rw.mu.Lock()
		rw.timesSaved += savedTime
		rw.mu.Unlock()

		log.Infow("Parallelization opportunity detected",
			"tool_count", len(calls),
			"tools", toolNames,
			"sequential_ms", sequentialTime.Milliseconds(),
			"parallel_would_be_ms", potentialTime.Milliseconds(),
			"potential_savings_ms", savedTime.Milliseconds(),
			"savings_percent", int(float64(savedTime)/float64(sequentialTime)*100),
		)
	}
}

// GetMetrics returns the current parallel execution metrics
func (rw *RunnerWrapper) GetMetrics() ParallelMetrics {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	return ParallelMetrics{
		TotalToolCalls:      rw.totalToolCalls,
		ParallelizableCalls: rw.parallelizableCalls,
		TotalTimeSaved:      rw.timesSaved,
	}
}

// ParallelMetrics contains metrics about parallel execution
type ParallelMetrics struct {
	TotalToolCalls      int64
	ParallelizableCalls int64
	TotalTimeSaved      time.Duration
}

// Helper functions

func extractFunctionCalls(parts []*genai.Part) []*genai.FunctionCall {
	var calls []*genai.FunctionCall
	for _, part := range parts {
		if part != nil && part.FunctionCall != nil {
			calls = append(calls, part.FunctionCall)
		}
	}
	return calls
}

func extractFunctionResponses(parts []*genai.Part) []*genai.FunctionResponse {
	var responses []*genai.FunctionResponse
	for _, part := range parts {
		if part != nil && part.FunctionResponse != nil {
			responses = append(responses, part.FunctionResponse)
		}
	}
	return responses
}

func getToolNames(calls []*genai.FunctionCall) []string {
	names := make([]string, len(calls))
	for i, c := range calls {
		names[i] = c.Name
	}
	return names
}

// GetBaseRunner returns the underlying ADK runner
func (rw *RunnerWrapper) GetBaseRunner() *runner.Runner {
	return rw.baseRunner
}

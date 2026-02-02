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
func (rw *RunnerWrapper) Run(ctx context.Context, userID, sessionID string, msg *genai.Content, cfg agent.RunConfig) iter.Seq2[*session.Event, error] {
	// If not enabled, just pass through to base runner directly
	if !rw.enabled {
		return rw.baseRunner.Run(ctx, userID, sessionID, msg, cfg)
	}

	return func(yield func(*session.Event, error) bool) {
		log := logger.Get()
		var pendingCalls []*pendingToolCall
		var lastCallTime time.Time

		// Track tool calls in the current turn
		for event, err := range rw.baseRunner.Run(ctx, userID, sessionID, msg, cfg) {
			if err != nil {
				if !yield(event, err) {
					return
				}
				continue
			}

			// Analyze event for tool calls (only if we have content)
			if event != nil && event.Content != nil && len(event.Content.Parts) > 0 {
				functionCalls := extractFunctionCalls(event.Content.Parts)
				functionResponses := extractFunctionResponses(event.Content.Parts)

				if len(functionCalls) > 0 {
					rw.mu.Lock()
					rw.totalToolCalls += int64(len(functionCalls))
					rw.mu.Unlock()

					now := time.Now()
					if !lastCallTime.IsZero() && now.Sub(lastCallTime) < 100*time.Millisecond {
						rw.mu.Lock()
						rw.parallelizableCalls += int64(len(functionCalls))
						rw.mu.Unlock()
					}
					lastCallTime = now

					for _, fc := range functionCalls {
						pendingCalls = append(pendingCalls, &pendingToolCall{
							name:      fc.Name,
							args:      fc.Args,
							startTime: now,
						})
					}

					// Only log if executor exists
					if rw.executor != nil {
						log.Debugw("Tool calls detected",
							"count", len(functionCalls),
							"tools", getToolNames(functionCalls),
						)
					}
				}

				if len(functionResponses) > 0 {
					now := time.Now()
					for _, fr := range functionResponses {
						for i, pc := range pendingCalls {
							if pc.name == fr.Name && pc.endTime.IsZero() {
								pendingCalls[i].endTime = now
								pendingCalls[i].duration = now.Sub(pc.startTime)
								break
							}
						}
					}
				}
			}

			if !yield(event, nil) {
				return
			}
		}

		// Log parallelization analysis at end of run
		if len(pendingCalls) > 1 {
			rw.analyzeParallelization(pendingCalls)
		}
	}
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

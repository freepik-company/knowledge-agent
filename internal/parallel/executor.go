package parallel

import (
	"context"
	"sync"
	"time"

	"knowledge-agent/internal/logger"
)

// ToolCall represents a tool call to be executed
type ToolCall struct {
	ID      string         // Unique identifier for this call
	Name    string         // Tool name (e.g., "transfer_to_agent", "search_memory")
	Args    map[string]any // Tool arguments
	Execute func(ctx context.Context) (map[string]any, error)
}

// ToolResult represents the result of a tool execution
type ToolResult struct {
	ID       string         // Matches the ToolCall ID
	Name     string         // Tool name
	Response map[string]any // Tool response
	Error    error          // Error if execution failed
	Duration time.Duration  // Execution duration
}

// Executor handles parallel execution of tool calls
type Executor struct {
	maxParallelism  int
	toolTimeout     time.Duration
	sequentialTools map[string]bool
	semaphore       chan struct{}
}

// NewExecutor creates a new parallel executor
func NewExecutor(maxParallelism int, toolTimeout time.Duration, sequentialTools []string) *Executor {
	if maxParallelism <= 0 {
		maxParallelism = DefaultMaxParallelism
	}
	if toolTimeout <= 0 {
		toolTimeout = DefaultToolTimeout
	}

	seqTools := make(map[string]bool)
	for _, t := range sequentialTools {
		seqTools[t] = true
	}

	return &Executor{
		maxParallelism:  maxParallelism,
		toolTimeout:     toolTimeout,
		sequentialTools: seqTools,
		semaphore:       make(chan struct{}, maxParallelism),
	}
}

// CanParallelize checks if tool calls can be executed in parallel
func (e *Executor) CanParallelize(calls []ToolCall) bool {
	if len(calls) <= 1 {
		return false
	}

	// Check if any tool requires sequential execution
	for _, call := range calls {
		if e.sequentialTools[call.Name] {
			return false
		}
	}

	return true
}

// ExecuteParallel executes multiple tool calls in parallel
// Returns results in the same order as input calls
func (e *Executor) ExecuteParallel(ctx context.Context, calls []ToolCall) []ToolResult {
	log := logger.Get()

	if len(calls) == 0 {
		return nil
	}

	// Single call - execute directly
	if len(calls) == 1 {
		result := e.executeSingle(ctx, calls[0])
		return []ToolResult{result}
	}

	// Check if parallelization is allowed
	if !e.CanParallelize(calls) {
		log.Debugw("Sequential execution required",
			"call_count", len(calls),
			"first_tool", calls[0].Name,
		)
		return e.executeSequential(ctx, calls)
	}

	startTime := time.Now()
	log.Infow("Starting parallel tool execution",
		"call_count", len(calls),
		"max_parallelism", e.maxParallelism,
	)

	results := make([]ToolResult, len(calls))
	var wg sync.WaitGroup

	for i, call := range calls {
		wg.Add(1)
		go func(idx int, tc ToolCall) {
			defer wg.Done()

			// Acquire semaphore to limit concurrency
			select {
			case e.semaphore <- struct{}{}:
				defer func() { <-e.semaphore }()
			case <-ctx.Done():
				results[idx] = ToolResult{
					ID:    tc.ID,
					Name:  tc.Name,
					Error: ctx.Err(),
				}
				return
			}

			results[idx] = e.executeSingle(ctx, tc)
		}(i, call)
	}

	wg.Wait()

	totalDuration := time.Since(startTime)

	// Calculate sequential time for comparison
	var sequentialTime time.Duration
	for _, r := range results {
		sequentialTime += r.Duration
	}

	savedTime := sequentialTime - totalDuration
	if savedTime < 0 {
		savedTime = 0
	}

	log.Infow("Parallel execution completed",
		"call_count", len(calls),
		"total_ms", totalDuration.Milliseconds(),
		"sequential_would_be_ms", sequentialTime.Milliseconds(),
		"saved_ms", savedTime.Milliseconds(),
	)

	return results
}

// executeSequential executes calls one by one
func (e *Executor) executeSequential(ctx context.Context, calls []ToolCall) []ToolResult {
	results := make([]ToolResult, len(calls))
	for i, call := range calls {
		results[i] = e.executeSingle(ctx, call)
	}
	return results
}

// executeSingle executes a single tool call with timeout
func (e *Executor) executeSingle(ctx context.Context, call ToolCall) ToolResult {
	log := logger.Get()
	startTime := time.Now()

	// Create context with timeout
	callCtx, cancel := context.WithTimeout(ctx, e.toolTimeout)
	defer cancel()

	log.Debugw("Executing tool",
		"tool", call.Name,
		"id", call.ID,
		"timeout", e.toolTimeout,
	)

	response, err := call.Execute(callCtx)
	duration := time.Since(startTime)

	if err != nil {
		log.Warnw("Tool execution failed",
			"tool", call.Name,
			"id", call.ID,
			"error", err,
			"duration_ms", duration.Milliseconds(),
		)
	} else {
		log.Debugw("Tool execution completed",
			"tool", call.Name,
			"id", call.ID,
			"duration_ms", duration.Milliseconds(),
		)
	}

	return ToolResult{
		ID:       call.ID,
		Name:     call.Name,
		Response: response,
		Error:    err,
		Duration: duration,
	}
}

// ExecuteParallelSimple is a simplified version that just executes functions in parallel
// Useful when you don't need the full ToolCall structure
func ExecuteParallelSimple[T any](ctx context.Context, maxParallel int, fns []func(context.Context) (T, error)) ([]T, []error) {
	if len(fns) == 0 {
		return nil, nil
	}

	results := make([]T, len(fns))
	errors := make([]error, len(fns))

	if len(fns) == 1 {
		results[0], errors[0] = fns[0](ctx)
		return results, errors
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxParallel)

	for i, fn := range fns {
		wg.Add(1)
		go func(idx int, f func(context.Context) (T, error)) {
			defer wg.Done()

			// Acquire semaphore
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				errors[idx] = ctx.Err()
				return
			}

			results[idx], errors[idx] = f(ctx)
		}(i, fn)
	}

	wg.Wait()
	return results, errors
}

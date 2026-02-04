package a2a

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"knowledge-agent/internal/logger"
	"knowledge-agent/internal/observability"
)

// CallResult represents the result of a single A2A call
type CallResult struct {
	AgentName string        // Name of the sub-agent
	Response  any           // Response from the sub-agent
	Error     error         // Error if the call failed
	Duration  time.Duration // How long the call took
}

// ParallelExecutor executes A2A calls in parallel with configurable concurrency
type ParallelExecutor struct {
	maxConcurrency int
}

// NewParallelExecutor creates a new parallel executor
// maxConcurrency controls how many A2A calls can run simultaneously
// If maxConcurrency <= 0, defaults to 5
func NewParallelExecutor(maxConcurrency int) *ParallelExecutor {
	if maxConcurrency <= 0 {
		maxConcurrency = 5
	}
	return &ParallelExecutor{
		maxConcurrency: maxConcurrency,
	}
}

// CallFunc is a function that executes an A2A call and returns the result
// It receives context for cancellation and timeout support
type CallFunc func(ctx context.Context) CallResult

// Execute runs multiple A2A calls in parallel
// It respects the maxConcurrency limit and continues even if some calls fail
// Returns all results, including partial failures
func (e *ParallelExecutor) Execute(ctx context.Context, calls []CallFunc) []CallResult {
	log := logger.Get()

	if len(calls) == 0 {
		return nil
	}

	// For a single call, just execute it directly
	if len(calls) == 1 {
		result := calls[0](ctx)
		return []CallResult{result}
	}

	log.Infow("Starting parallel A2A execution",
		"total_calls", len(calls),
		"max_concurrency", e.maxConcurrency,
	)

	startTime := time.Now()
	results := make([]CallResult, len(calls))
	var mu sync.Mutex

	// Use errgroup for parallel execution with concurrency limit
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(e.maxConcurrency)

	for i, call := range calls {
		i, call := i, call // Capture loop variables
		g.Go(func() error {
			result := call(ctx)

			mu.Lock()
			results[i] = result
			mu.Unlock()

			// Don't propagate errors - we want to continue with other calls
			// The error is recorded in the CallResult
			return nil
		})
	}

	// Wait for all calls to complete
	_ = g.Wait() // Error is always nil since we don't return errors from goroutines

	totalDuration := time.Since(startTime)

	// Count successes and failures
	successCount := 0
	failureCount := 0
	var maxIndividualDuration time.Duration
	for _, r := range results {
		if r.Error != nil {
			failureCount++
		} else {
			successCount++
		}
		if r.Duration > maxIndividualDuration {
			maxIndividualDuration = r.Duration
		}
	}

	log.Infow("Parallel A2A execution completed",
		"total_calls", len(calls),
		"success_count", successCount,
		"failure_count", failureCount,
		"total_duration_ms", totalDuration.Milliseconds(),
		"max_individual_duration_ms", maxIndividualDuration.Milliseconds(),
		"parallelism_benefit_ms", maxIndividualDuration.Milliseconds()-totalDuration.Milliseconds(),
	)

	// Record metrics for the parallel batch
	observability.GetMetrics().RecordA2AParallelBatch(len(calls), successCount, totalDuration)

	return results
}

// ExecuteWithTimeout wraps Execute with a timeout for the entire batch
func (e *ParallelExecutor) ExecuteWithTimeout(ctx context.Context, calls []CallFunc, timeout time.Duration) []CallResult {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return e.Execute(ctx, calls)
}

// MergeResults combines multiple CallResults into a single summary
// Useful for creating aggregated responses to return to the LLM
func MergeResults(results []CallResult) (successResponses []any, errors []error) {
	for _, r := range results {
		if r.Error != nil {
			errors = append(errors, fmt.Errorf("%s: %w", r.AgentName, r.Error))
		} else {
			successResponses = append(successResponses, r.Response)
		}
	}
	return
}

// HasErrors returns true if any of the results contain an error
func HasErrors(results []CallResult) bool {
	for _, r := range results {
		if r.Error != nil {
			return true
		}
	}
	return false
}

// AllSucceeded returns true if all results succeeded (no errors)
func AllSucceeded(results []CallResult) bool {
	return !HasErrors(results)
}

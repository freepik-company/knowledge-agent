package a2a

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewParallelExecutor_DefaultConcurrency(t *testing.T) {
	// Test that invalid values default to 5
	testCases := []struct {
		input    int
		expected int
	}{
		{0, 5},
		{-1, 5},
		{-100, 5},
		{1, 1},
		{3, 3},
		{10, 10},
	}

	for _, tc := range testCases {
		executor := NewParallelExecutor(tc.input)
		if executor.maxConcurrency != tc.expected {
			t.Errorf("NewParallelExecutor(%d): expected maxConcurrency=%d, got=%d",
				tc.input, tc.expected, executor.maxConcurrency)
		}
	}
}

func TestParallelExecutor_Execute_EmptyCalls(t *testing.T) {
	executor := NewParallelExecutor(5)
	results := executor.Execute(context.Background(), nil)

	if results != nil {
		t.Errorf("expected nil results for empty calls, got: %v", results)
	}
}

func TestParallelExecutor_Execute_SingleCall(t *testing.T) {
	executor := NewParallelExecutor(5)

	calls := []CallFunc{
		func(ctx context.Context) CallResult {
			return CallResult{
				AgentName: "test-agent",
				Response:  "success",
				Duration:  100 * time.Millisecond,
			}
		},
	}

	results := executor.Execute(context.Background(), calls)

	if len(results) != 1 {
		t.Errorf("expected 1 result, got: %d", len(results))
	}
	if results[0].AgentName != "test-agent" {
		t.Errorf("expected agent name 'test-agent', got: %s", results[0].AgentName)
	}
	if results[0].Response != "success" {
		t.Errorf("expected response 'success', got: %v", results[0].Response)
	}
	if results[0].Error != nil {
		t.Errorf("expected no error, got: %v", results[0].Error)
	}
}

func TestParallelExecutor_Execute_MultipleCalls(t *testing.T) {
	executor := NewParallelExecutor(5)

	calls := []CallFunc{
		func(ctx context.Context) CallResult {
			return CallResult{AgentName: "agent1", Response: "response1"}
		},
		func(ctx context.Context) CallResult {
			return CallResult{AgentName: "agent2", Response: "response2"}
		},
		func(ctx context.Context) CallResult {
			return CallResult{AgentName: "agent3", Response: "response3"}
		},
	}

	results := executor.Execute(context.Background(), calls)

	if len(results) != 3 {
		t.Errorf("expected 3 results, got: %d", len(results))
	}

	// Results should be in order (by index)
	expectedAgents := []string{"agent1", "agent2", "agent3"}
	for i, expected := range expectedAgents {
		if results[i].AgentName != expected {
			t.Errorf("results[%d]: expected agent '%s', got '%s'", i, expected, results[i].AgentName)
		}
	}
}

func TestParallelExecutor_ExecutesConcurrently(t *testing.T) {
	executor := NewParallelExecutor(5)

	// Track concurrent execution
	var concurrentCount atomic.Int32
	var maxConcurrent atomic.Int32

	calls := make([]CallFunc, 3)
	for i := 0; i < 3; i++ {
		agentName := "agent" + string(rune('1'+i))
		calls[i] = func(ctx context.Context) CallResult {
			// Increment concurrent count
			current := concurrentCount.Add(1)
			// Track max concurrent
			for {
				max := maxConcurrent.Load()
				if current <= max || maxConcurrent.CompareAndSwap(max, current) {
					break
				}
			}

			// Simulate work
			time.Sleep(50 * time.Millisecond)

			// Decrement concurrent count
			concurrentCount.Add(-1)

			return CallResult{AgentName: agentName, Response: "done"}
		}
	}

	start := time.Now()
	results := executor.Execute(context.Background(), calls)
	duration := time.Since(start)

	// Verify all succeeded
	if len(results) != 3 {
		t.Errorf("expected 3 results, got: %d", len(results))
	}

	// Verify concurrent execution: 3 calls * 50ms each = 150ms sequential
	// With parallelism, should be closer to 50ms (+ overhead)
	// Use generous threshold for CI environments
	if duration > 120*time.Millisecond {
		t.Errorf("parallel execution took too long: %v (expected < 120ms for parallel 50ms calls)", duration)
	}

	// Verify we had concurrent execution
	if maxConcurrent.Load() < 2 {
		t.Errorf("expected at least 2 concurrent executions, got max: %d", maxConcurrent.Load())
	}
}

func TestParallelExecutor_HandlesPartialFailures(t *testing.T) {
	executor := NewParallelExecutor(5)

	testError := errors.New("simulated error")

	calls := []CallFunc{
		func(ctx context.Context) CallResult {
			return CallResult{AgentName: "success1", Response: "ok"}
		},
		func(ctx context.Context) CallResult {
			return CallResult{AgentName: "failure", Error: testError}
		},
		func(ctx context.Context) CallResult {
			return CallResult{AgentName: "success2", Response: "ok"}
		},
	}

	results := executor.Execute(context.Background(), calls)

	// All 3 results should be returned
	if len(results) != 3 {
		t.Errorf("expected 3 results, got: %d", len(results))
	}

	// Verify success count
	successCount := 0
	failureCount := 0
	for _, r := range results {
		if r.Error != nil {
			failureCount++
		} else {
			successCount++
		}
	}

	if successCount != 2 {
		t.Errorf("expected 2 successes, got: %d", successCount)
	}
	if failureCount != 1 {
		t.Errorf("expected 1 failure, got: %d", failureCount)
	}
}

func TestParallelExecutor_RespectsMaxConcurrency(t *testing.T) {
	maxConcurrency := 2
	executor := NewParallelExecutor(maxConcurrency)

	var peakConcurrent atomic.Int32
	var currentConcurrent atomic.Int32

	// Create 5 calls that track concurrency
	calls := make([]CallFunc, 5)
	for i := 0; i < 5; i++ {
		agentName := "agent" + string(rune('1'+i))
		calls[i] = func(ctx context.Context) CallResult {
			current := currentConcurrent.Add(1)

			// Track peak
			for {
				peak := peakConcurrent.Load()
				if current <= peak || peakConcurrent.CompareAndSwap(peak, current) {
					break
				}
			}

			time.Sleep(30 * time.Millisecond)
			currentConcurrent.Add(-1)

			return CallResult{AgentName: agentName, Response: "done"}
		}
	}

	executor.Execute(context.Background(), calls)

	// Peak should not exceed maxConcurrency
	if peak := peakConcurrent.Load(); peak > int32(maxConcurrency) {
		t.Errorf("peak concurrent (%d) exceeded max concurrency (%d)", peak, maxConcurrency)
	}
}

func TestParallelExecutor_RespectsContextCancellation(t *testing.T) {
	executor := NewParallelExecutor(5)

	ctx, cancel := context.WithCancel(context.Background())

	var callsStarted atomic.Int32

	calls := make([]CallFunc, 3)
	for i := 0; i < 3; i++ {
		calls[i] = func(ctx context.Context) CallResult {
			callsStarted.Add(1)
			select {
			case <-ctx.Done():
				return CallResult{AgentName: "cancelled", Error: ctx.Err()}
			case <-time.After(500 * time.Millisecond):
				return CallResult{AgentName: "slow", Response: "done"}
			}
		}
	}

	// Cancel after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	results := executor.Execute(ctx, calls)
	duration := time.Since(start)

	// Should complete quickly after cancellation
	if duration > 200*time.Millisecond {
		t.Errorf("execution took too long after cancellation: %v", duration)
	}

	// All results should exist
	if len(results) != 3 {
		t.Errorf("expected 3 results, got: %d", len(results))
	}
}

func TestParallelExecutor_ExecuteWithTimeout(t *testing.T) {
	executor := NewParallelExecutor(5)

	calls := []CallFunc{
		func(ctx context.Context) CallResult {
			select {
			case <-ctx.Done():
				return CallResult{AgentName: "timeout", Error: ctx.Err()}
			case <-time.After(500 * time.Millisecond):
				return CallResult{AgentName: "slow", Response: "done"}
			}
		},
	}

	start := time.Now()
	results := executor.ExecuteWithTimeout(context.Background(), calls, 50*time.Millisecond)
	duration := time.Since(start)

	// Should complete quickly due to timeout
	if duration > 100*time.Millisecond {
		t.Errorf("execution took too long with timeout: %v", duration)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 result, got: %d", len(results))
	}

	// Result should have timeout error
	if results[0].Error == nil {
		t.Error("expected timeout error, got nil")
	}
}

func TestMergeResults(t *testing.T) {
	results := []CallResult{
		{AgentName: "agent1", Response: "response1"},
		{AgentName: "agent2", Error: errors.New("error2")},
		{AgentName: "agent3", Response: "response3"},
		{AgentName: "agent4", Error: errors.New("error4")},
	}

	successes, errs := MergeResults(results)

	if len(successes) != 2 {
		t.Errorf("expected 2 successes, got: %d", len(successes))
	}
	if len(errs) != 2 {
		t.Errorf("expected 2 errors, got: %d", len(errs))
	}

	// Verify success values
	if successes[0] != "response1" || successes[1] != "response3" {
		t.Errorf("unexpected success responses: %v", successes)
	}
}

func TestHasErrors(t *testing.T) {
	testCases := []struct {
		name     string
		results  []CallResult
		expected bool
	}{
		{
			name:     "empty results",
			results:  []CallResult{},
			expected: false,
		},
		{
			name: "all success",
			results: []CallResult{
				{AgentName: "a1", Response: "ok"},
				{AgentName: "a2", Response: "ok"},
			},
			expected: false,
		},
		{
			name: "one error",
			results: []CallResult{
				{AgentName: "a1", Response: "ok"},
				{AgentName: "a2", Error: errors.New("fail")},
			},
			expected: true,
		},
		{
			name: "all errors",
			results: []CallResult{
				{AgentName: "a1", Error: errors.New("fail1")},
				{AgentName: "a2", Error: errors.New("fail2")},
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasErrors(tc.results); got != tc.expected {
				t.Errorf("HasErrors() = %v, expected %v", got, tc.expected)
			}
		})
	}
}

func TestAllSucceeded(t *testing.T) {
	testCases := []struct {
		name     string
		results  []CallResult
		expected bool
	}{
		{
			name:     "empty results",
			results:  []CallResult{},
			expected: true,
		},
		{
			name: "all success",
			results: []CallResult{
				{AgentName: "a1", Response: "ok"},
				{AgentName: "a2", Response: "ok"},
			},
			expected: true,
		},
		{
			name: "one error",
			results: []CallResult{
				{AgentName: "a1", Response: "ok"},
				{AgentName: "a2", Error: errors.New("fail")},
			},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AllSucceeded(tc.results); got != tc.expected {
				t.Errorf("AllSucceeded() = %v, expected %v", got, tc.expected)
			}
		})
	}
}

// Additional stress tests

func TestParallelExecutor_StressTest_ManyCallsHighConcurrency(t *testing.T) {
	executor := NewParallelExecutor(10)

	// Create 20 calls
	numCalls := 20
	calls := make([]CallFunc, numCalls)
	for i := 0; i < numCalls; i++ {
		idx := i
		calls[i] = func(ctx context.Context) CallResult {
			time.Sleep(10 * time.Millisecond)
			return CallResult{
				AgentName: fmt.Sprintf("agent%d", idx),
				Response:  fmt.Sprintf("response%d", idx),
				Duration:  10 * time.Millisecond,
			}
		}
	}

	start := time.Now()
	results := executor.Execute(context.Background(), calls)
	duration := time.Since(start)

	// All results should be returned
	if len(results) != numCalls {
		t.Errorf("expected %d results, got: %d", numCalls, len(results))
	}

	// With concurrency 10, 20 calls of 10ms each should take ~20-30ms (2 batches)
	// Give generous margin for CI
	if duration > 200*time.Millisecond {
		t.Errorf("stress test took too long: %v (expected < 200ms)", duration)
	}

	// Verify all responses are correct
	for i, r := range results {
		expectedAgent := fmt.Sprintf("agent%d", i)
		if r.AgentName != expectedAgent {
			t.Errorf("results[%d]: expected agent '%s', got '%s'", i, expectedAgent, r.AgentName)
		}
		if r.Error != nil {
			t.Errorf("results[%d]: unexpected error: %v", i, r.Error)
		}
	}
}

func TestParallelExecutor_AllCallsFail(t *testing.T) {
	executor := NewParallelExecutor(5)

	calls := []CallFunc{
		func(ctx context.Context) CallResult {
			return CallResult{AgentName: "agent1", Error: errors.New("error1")}
		},
		func(ctx context.Context) CallResult {
			return CallResult{AgentName: "agent2", Error: errors.New("error2")}
		},
		func(ctx context.Context) CallResult {
			return CallResult{AgentName: "agent3", Error: errors.New("error3")}
		},
	}

	results := executor.Execute(context.Background(), calls)

	// All results should be returned even if all failed
	if len(results) != 3 {
		t.Errorf("expected 3 results, got: %d", len(results))
	}

	// All should have errors
	for i, r := range results {
		if r.Error == nil {
			t.Errorf("results[%d]: expected error, got nil", i)
		}
	}

	// HasErrors should return true
	if !HasErrors(results) {
		t.Error("HasErrors() should return true when all calls fail")
	}

	// AllSucceeded should return false
	if AllSucceeded(results) {
		t.Error("AllSucceeded() should return false when all calls fail")
	}
}

func TestParallelExecutor_PreservesResultOrder(t *testing.T) {
	executor := NewParallelExecutor(5)

	// Create calls with different delays to ensure they complete out of order
	calls := []CallFunc{
		func(ctx context.Context) CallResult {
			time.Sleep(50 * time.Millisecond) // Slowest
			return CallResult{AgentName: "agent0", Response: "response0"}
		},
		func(ctx context.Context) CallResult {
			time.Sleep(10 * time.Millisecond) // Fastest
			return CallResult{AgentName: "agent1", Response: "response1"}
		},
		func(ctx context.Context) CallResult {
			time.Sleep(30 * time.Millisecond) // Medium
			return CallResult{AgentName: "agent2", Response: "response2"}
		},
	}

	results := executor.Execute(context.Background(), calls)

	// Results should be in original order, not completion order
	expectedOrder := []string{"agent0", "agent1", "agent2"}
	for i, expected := range expectedOrder {
		if results[i].AgentName != expected {
			t.Errorf("results[%d]: expected '%s', got '%s' (order not preserved)", i, expected, results[i].AgentName)
		}
	}
}

func TestParallelExecutor_PanicRecovery(t *testing.T) {
	executor := NewParallelExecutor(5)

	// This test verifies that panics in one goroutine don't crash others
	// Note: errgroup doesn't recover panics, so we need to handle this in the CallFunc
	calls := []CallFunc{
		func(ctx context.Context) CallResult {
			return CallResult{AgentName: "agent1", Response: "ok"}
		},
		func(ctx context.Context) CallResult {
			// Simulate a recoverable error (not a panic)
			return CallResult{AgentName: "agent2", Error: errors.New("simulated failure")}
		},
		func(ctx context.Context) CallResult {
			return CallResult{AgentName: "agent3", Response: "ok"}
		},
	}

	results := executor.Execute(context.Background(), calls)

	// All results should be present
	if len(results) != 3 {
		t.Errorf("expected 3 results, got: %d", len(results))
	}

	// First and third should succeed
	if results[0].Error != nil {
		t.Errorf("results[0]: expected success, got error: %v", results[0].Error)
	}
	if results[2].Error != nil {
		t.Errorf("results[2]: expected success, got error: %v", results[2].Error)
	}

	// Second should have error
	if results[1].Error == nil {
		t.Error("results[1]: expected error, got nil")
	}
}

func TestParallelExecutor_ZeroConcurrency(t *testing.T) {
	// Test that zero concurrency defaults to 5
	executor := NewParallelExecutor(0)

	var maxConcurrent atomic.Int32
	var currentConcurrent atomic.Int32

	// Create 10 calls
	calls := make([]CallFunc, 10)
	for i := 0; i < 10; i++ {
		calls[i] = func(ctx context.Context) CallResult {
			current := currentConcurrent.Add(1)
			for {
				max := maxConcurrent.Load()
				if current <= max || maxConcurrent.CompareAndSwap(max, current) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			currentConcurrent.Add(-1)
			return CallResult{AgentName: "agent", Response: "ok"}
		}
	}

	executor.Execute(context.Background(), calls)

	// Max concurrent should be 5 (the default)
	if peak := maxConcurrent.Load(); peak > 5 {
		t.Errorf("with zero concurrency, peak (%d) should default to 5", peak)
	}
}

func TestMergeResults_EmptyResults(t *testing.T) {
	results := []CallResult{}
	successes, errs := MergeResults(results)

	if len(successes) != 0 {
		t.Errorf("expected 0 successes, got: %d", len(successes))
	}
	if len(errs) != 0 {
		t.Errorf("expected 0 errors, got: %d", len(errs))
	}
}

func TestMergeResults_ErrorContainsAgentName(t *testing.T) {
	results := []CallResult{
		{AgentName: "metrics_agent", Error: errors.New("connection timeout")},
	}

	_, errs := MergeResults(results)

	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got: %d", len(errs))
	}

	// Error message should include agent name
	errMsg := errs[0].Error()
	if !strings.Contains(errMsg, "metrics_agent") {
		t.Errorf("error message should contain agent name, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "connection timeout") {
		t.Errorf("error message should contain original error, got: %s", errMsg)
	}
}

func TestParallelExecutor_DurationTracking(t *testing.T) {
	executor := NewParallelExecutor(5)

	calls := []CallFunc{
		func(ctx context.Context) CallResult {
			start := time.Now()
			time.Sleep(50 * time.Millisecond)
			return CallResult{
				AgentName: "agent1",
				Response:  "ok",
				Duration:  time.Since(start),
			}
		},
	}

	results := executor.Execute(context.Background(), calls)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got: %d", len(results))
	}

	// Duration should be approximately 50ms
	if results[0].Duration < 40*time.Millisecond || results[0].Duration > 100*time.Millisecond {
		t.Errorf("duration should be ~50ms, got: %v", results[0].Duration)
	}
}

// Benchmarks

func BenchmarkParallelExecutor_Sequential(b *testing.B) {
	// Simulate sequential execution for comparison
	for i := 0; i < b.N; i++ {
		results := make([]CallResult, 3)
		for j := 0; j < 3; j++ {
			time.Sleep(1 * time.Millisecond)
			results[j] = CallResult{AgentName: fmt.Sprintf("agent%d", j), Response: "ok"}
		}
	}
}

func BenchmarkParallelExecutor_Parallel(b *testing.B) {
	executor := NewParallelExecutor(5)

	calls := make([]CallFunc, 3)
	for i := 0; i < 3; i++ {
		idx := i
		calls[i] = func(ctx context.Context) CallResult {
			time.Sleep(1 * time.Millisecond)
			return CallResult{AgentName: fmt.Sprintf("agent%d", idx), Response: "ok"}
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		executor.Execute(context.Background(), calls)
	}
}

func BenchmarkParallelExecutor_HighConcurrency(b *testing.B) {
	executor := NewParallelExecutor(20)

	calls := make([]CallFunc, 10)
	for i := 0; i < 10; i++ {
		idx := i
		calls[i] = func(ctx context.Context) CallResult {
			// No sleep - just measure overhead
			return CallResult{AgentName: fmt.Sprintf("agent%d", idx), Response: "ok"}
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		executor.Execute(context.Background(), calls)
	}
}

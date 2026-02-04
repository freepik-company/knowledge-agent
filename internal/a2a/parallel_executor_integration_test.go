package a2a

import (
	"context"
	"testing"
	"time"
)

// TestParallelExecutor_IntegrationScenario simulates a real-world scenario
// where multiple A2A agents are called in parallel
func TestParallelExecutor_IntegrationScenario(t *testing.T) {
	executor := NewParallelExecutor(5)

	// Simulate 3 A2A calls with different response times
	calls := []CallFunc{
		func(ctx context.Context) CallResult {
			start := time.Now()
			time.Sleep(100 * time.Millisecond) // metrics_agent
			return CallResult{
				AgentName: "metrics_agent",
				Response:  "CPU: 45%, Memory: 2.3GB",
				Duration:  time.Since(start),
			}
		},
		func(ctx context.Context) CallResult {
			start := time.Now()
			time.Sleep(80 * time.Millisecond) // logs_agent
			return CallResult{
				AgentName: "logs_agent",
				Response:  "Found 15 errors in last hour",
				Duration:  time.Since(start),
			}
		},
		func(ctx context.Context) CallResult {
			start := time.Now()
			time.Sleep(50 * time.Millisecond) // alerts_agent
			return CallResult{
				AgentName: "alerts_agent",
				Response:  "2 active alerts",
				Duration:  time.Since(start),
			}
		},
	}

	totalStart := time.Now()
	results := executor.Execute(context.Background(), calls)
	totalDuration := time.Since(totalStart)

	// Verify all results are present and correct
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got: %d", len(results))
	}

	// Calculate theoretical sequential time
	var sumIndividual time.Duration
	for _, r := range results {
		if r.Error != nil {
			t.Errorf("unexpected error for %s: %v", r.AgentName, r.Error)
		}
		sumIndividual += r.Duration
	}

	// Verify parallel execution is faster than sequential
	// Sequential would be ~230ms (100+80+50), parallel should be ~100ms (max of individual times)
	expectedSequential := 230 * time.Millisecond
	maxParallel := 150 * time.Millisecond // Give some margin

	if totalDuration > maxParallel {
		t.Errorf("parallel execution took %v, expected < %v (sequential would be %v)",
			totalDuration, maxParallel, expectedSequential)
	}

	// Log performance metrics
	speedup := float64(sumIndividual) / float64(totalDuration)
	t.Logf("Integration test results:")
	t.Logf("  Total parallel time: %v", totalDuration)
	t.Logf("  Sum of individual times: %v", sumIndividual)
	t.Logf("  Speedup factor: %.2fx", speedup)

	// Verify results are in correct order
	expectedAgents := []string{"metrics_agent", "logs_agent", "alerts_agent"}
	for i, expected := range expectedAgents {
		if results[i].AgentName != expected {
			t.Errorf("results[%d]: expected %s, got %s", i, expected, results[i].AgentName)
		}
	}
}

// TestParallelExecutor_MixedSuccess simulates a scenario where some agents succeed
// and some fail, verifying that partial results are still returned
func TestParallelExecutor_MixedSuccess(t *testing.T) {
	executor := NewParallelExecutor(5)

	calls := []CallFunc{
		func(ctx context.Context) CallResult {
			time.Sleep(30 * time.Millisecond)
			return CallResult{AgentName: "success_agent_1", Response: "data1"}
		},
		func(ctx context.Context) CallResult {
			time.Sleep(50 * time.Millisecond)
			// Simulate an agent that's down
			return CallResult{AgentName: "failed_agent", Error: context.DeadlineExceeded}
		},
		func(ctx context.Context) CallResult {
			time.Sleep(40 * time.Millisecond)
			return CallResult{AgentName: "success_agent_2", Response: "data2"}
		},
	}

	results := executor.Execute(context.Background(), calls)

	// All 3 results should be returned
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got: %d", len(results))
	}

	// Count successes and failures
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

	// Verify the helper functions work correctly
	if !HasErrors(results) {
		t.Error("HasErrors should return true")
	}
	if AllSucceeded(results) {
		t.Error("AllSucceeded should return false")
	}

	// Verify MergeResults
	successes, errors := MergeResults(results)
	if len(successes) != 2 {
		t.Errorf("MergeResults: expected 2 successes, got: %d", len(successes))
	}
	if len(errors) != 1 {
		t.Errorf("MergeResults: expected 1 error, got: %d", len(errors))
	}
}

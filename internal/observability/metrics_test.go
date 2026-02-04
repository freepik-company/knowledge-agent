package observability

import (
	"testing"
	"time"
)

func TestMetrics_RecordA2AParallelBatch(t *testing.T) {
	m := GetMetrics()
	m.Reset()

	// Record a parallel batch
	m.RecordA2AParallelBatch(3, 2, 500*time.Millisecond)

	stats := m.GetStats()
	a2a, ok := stats["a2a"].(map[string]any)
	if !ok {
		t.Fatal("expected a2a stats to be a map")
	}

	parallelBatches, ok := a2a["parallel_batches"].(map[string]any)
	if !ok {
		t.Fatal("expected parallel_batches stats to be a map")
	}

	// Verify the metrics
	if total := parallelBatches["total"].(int64); total != 1 {
		t.Errorf("expected total=1, got: %d", total)
	}
	if successes := parallelBatches["total_successes"].(int64); successes != 2 {
		t.Errorf("expected total_successes=2, got: %d", successes)
	}
	if avgSize := parallelBatches["avg_batch_size"].(float64); avgSize != 3.0 {
		t.Errorf("expected avg_batch_size=3.0, got: %f", avgSize)
	}
	if avgLatency := parallelBatches["avg_latency_ms"].(float64); avgLatency != 500.0 {
		t.Errorf("expected avg_latency_ms=500.0, got: %f", avgLatency)
	}
}

func TestMetrics_RecordA2AParallelBatch_MultipleCalls(t *testing.T) {
	m := GetMetrics()
	m.Reset()

	// Record multiple batches
	m.RecordA2AParallelBatch(2, 2, 200*time.Millisecond)
	m.RecordA2AParallelBatch(4, 3, 400*time.Millisecond)

	stats := m.GetStats()
	a2a := stats["a2a"].(map[string]any)
	parallelBatches := a2a["parallel_batches"].(map[string]any)

	// Verify aggregated metrics
	if total := parallelBatches["total"].(int64); total != 2 {
		t.Errorf("expected total=2, got: %d", total)
	}
	if successes := parallelBatches["total_successes"].(int64); successes != 5 { // 2 + 3
		t.Errorf("expected total_successes=5, got: %d", successes)
	}
	// avg_batch_size = (2 + 4) / 2 = 3
	if avgSize := parallelBatches["avg_batch_size"].(float64); avgSize != 3.0 {
		t.Errorf("expected avg_batch_size=3.0, got: %f", avgSize)
	}
	// avg_latency = (200 + 400) / 2 = 300
	if avgLatency := parallelBatches["avg_latency_ms"].(float64); avgLatency != 300.0 {
		t.Errorf("expected avg_latency_ms=300.0, got: %f", avgLatency)
	}
}

func TestMetrics_Reset_IncludesParallelBatchMetrics(t *testing.T) {
	m := GetMetrics()

	// Record some data
	m.RecordA2AParallelBatch(5, 4, 1*time.Second)

	// Reset
	m.Reset()

	stats := m.GetStats()
	a2a := stats["a2a"].(map[string]any)
	parallelBatches := a2a["parallel_batches"].(map[string]any)

	// All should be zero after reset
	if total := parallelBatches["total"].(int64); total != 0 {
		t.Errorf("expected total=0 after reset, got: %d", total)
	}
	if successes := parallelBatches["total_successes"].(int64); successes != 0 {
		t.Errorf("expected total_successes=0 after reset, got: %d", successes)
	}
}

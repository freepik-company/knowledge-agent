package observability

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestQueryTrace_RecordMethods_NilSafe verifies that all Record* methods
// handle nil trace gracefully (no panic when tracer is disabled)
func TestQueryTrace_RecordMethods_NilSafe(t *testing.T) {
	// Create a QueryTrace with disabled tracer (simulates disabled Langfuse)
	qt := &QueryTrace{
		tracer: &LangfuseTracer{enabled: false},
		trace:  nil,
	}

	// All Record* methods should not panic with nil trace
	t.Run("RecordPreSearch", func(t *testing.T) {
		qt.RecordPreSearch("test query", 5, 100*time.Millisecond)
		// Should not panic
	})

	t.Run("RecordRESTCall_success", func(t *testing.T) {
		qt.RecordRESTCall("test-agent", "query", "response", 500*time.Millisecond, nil)
		// Should not panic
	})

	t.Run("RecordRESTCall_error", func(t *testing.T) {
		qt.RecordRESTCall("test-agent", "query", "", 100*time.Millisecond, errors.New("test error"))
		// Should not panic
	})

	t.Run("RecordSessionRepair", func(t *testing.T) {
		qt.RecordSessionRepair("session-123", 1)
		// Should not panic
	})

	t.Run("RecordAuxiliaryGeneration", func(t *testing.T) {
		qt.RecordAuxiliaryGeneration("test-gen", "haiku", "input", "output", 100, 50, 200*time.Millisecond)
		// Should not panic
	})

	t.Run("RecordA2ACall", func(t *testing.T) {
		qt.RecordA2ACall("agent-name", "original query", "cleaned query", 150)
		// Should not panic
	})
}

// TestQueryTrace_RecordMethods_EventCount verifies that eventCount is incremented
func TestQueryTrace_RecordMethods_EventCount(t *testing.T) {
	// We can't test with a real trace without Langfuse, but we can verify
	// the eventCount logic works when tracer is disabled
	qt := &QueryTrace{
		tracer: &LangfuseTracer{enabled: false},
		trace:  nil,
	}

	initialCount := qt.eventCount.Load()

	// Call methods - they should return early without incrementing
	// because tracer is disabled
	qt.RecordPreSearch("query", 1, time.Second)
	qt.RecordRESTCall("agent", "q", "r", time.Second, nil)
	qt.RecordSessionRepair("sess", 0)
	qt.RecordAuxiliaryGeneration("name", "model", "in", "out", 10, 10, time.Second)
	qt.RecordA2ACall("agent", "orig", "clean", 100)

	// Count should not change when tracer is disabled
	if qt.eventCount.Load() != initialCount {
		t.Errorf("eventCount changed when tracer was disabled: got %d, want %d",
			qt.eventCount.Load(), initialCount)
	}
}

// TestQueryTrace_GetSummary verifies GetSummary returns correct data
func TestQueryTrace_GetSummary(t *testing.T) {
	qt := &QueryTrace{
		tracer:           &LangfuseTracer{enabled: false},
		promptTokens:     1000,
		completionTokens: 500,
		totalTokens:      1500,
	}
	qt.eventCount.Store(10)

	summary := qt.GetSummary()

	if summary["total_events"] != int32(10) {
		t.Errorf("total_events = %v, want 10", summary["total_events"])
	}
	if summary["prompt_tokens"] != 1000 {
		t.Errorf("prompt_tokens = %v, want 1000", summary["prompt_tokens"])
	}
	if summary["completion_tokens"] != 500 {
		t.Errorf("completion_tokens = %v, want 500", summary["completion_tokens"])
	}
	if summary["total_tokens"] != 1500 {
		t.Errorf("total_tokens = %v, want 1500", summary["total_tokens"])
	}
}

// TestQueryTraceFromContext verifies context functions work correctly
func TestQueryTraceFromContext(t *testing.T) {
	t.Run("nil context returns nil", func(t *testing.T) {
		result := QueryTraceFromContext(context.Background())
		if result != nil {
			t.Error("expected nil QueryTrace from empty context")
		}
	})

	t.Run("context with trace returns trace", func(t *testing.T) {
		qt := &QueryTrace{
			TraceID: "test-trace-id",
		}
		ctx := ContextWithQueryTrace(context.Background(), qt)

		result := QueryTraceFromContext(ctx)
		if result == nil {
			t.Fatal("expected non-nil QueryTrace")
		}
		if result.TraceID != "test-trace-id" {
			t.Errorf("TraceID = %q, want %q", result.TraceID, "test-trace-id")
		}
	})
}

// TestTruncateForLog verifies string truncation
func TestTruncateForLog(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "short string unchanged",
			input:  "hello",
			maxLen: 10,
			want:   "hello",
		},
		{
			name:   "exact length unchanged",
			input:  "hello",
			maxLen: 5,
			want:   "hello",
		},
		{
			name:   "long string truncated",
			input:  "hello world",
			maxLen: 5,
			want:   "hello...",
		},
		{
			name:   "empty string unchanged",
			input:  "",
			maxLen: 10,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateForLog(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateForLog(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

// TestLangfuseTracer_DisabledBehavior verifies disabled tracer behavior
func TestLangfuseTracer_DisabledBehavior(t *testing.T) {
	tracer := &LangfuseTracer{enabled: false}

	t.Run("IsEnabled returns false", func(t *testing.T) {
		if tracer.IsEnabled() {
			t.Error("expected IsEnabled() = false for disabled tracer")
		}
	})

	t.Run("Flush succeeds", func(t *testing.T) {
		err := tracer.Flush()
		if err != nil {
			t.Errorf("Flush() error = %v, want nil", err)
		}
	})

	t.Run("Close succeeds", func(t *testing.T) {
		err := tracer.Close()
		if err != nil {
			t.Errorf("Close() error = %v, want nil", err)
		}
	})

	t.Run("StartQueryTrace returns stub", func(t *testing.T) {
		qt := tracer.StartQueryTrace(context.Background(), "test", "session", nil)
		if qt == nil {
			t.Fatal("expected non-nil QueryTrace")
		}
		if qt.trace != nil {
			t.Error("expected nil trace in stub QueryTrace")
		}
	})
}

// TestEventCount_ThreadSafety tests that eventCount is thread-safe
func TestEventCount_ThreadSafety(t *testing.T) {
	qt := &QueryTrace{
		tracer: &LangfuseTracer{enabled: false},
	}

	// This test verifies the atomic type works correctly
	// In production, Record* methods would increment this
	const iterations = 1000

	done := make(chan struct{})

	// Spawn multiple goroutines incrementing eventCount
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < iterations; j++ {
				qt.eventCount.Add(1)
			}
			done <- struct{}{}
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	expected := int32(10 * iterations)
	if qt.eventCount.Load() != expected {
		t.Errorf("eventCount = %d, want %d", qt.eventCount.Load(), expected)
	}
}

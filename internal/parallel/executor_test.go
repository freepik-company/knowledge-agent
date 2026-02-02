package parallel

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewExecutor(t *testing.T) {
	tests := []struct {
		name            string
		maxParallelism  int
		toolTimeout     time.Duration
		sequentialTools []string
		wantMax         int
		wantTimeout     time.Duration
	}{
		{
			name:           "default values when zero",
			maxParallelism: 0,
			toolTimeout:    0,
			wantMax:        DefaultMaxParallelism,
			wantTimeout:    DefaultToolTimeout,
		},
		{
			name:           "custom values",
			maxParallelism: 10,
			toolTimeout:    30 * time.Second,
			wantMax:        10,
			wantTimeout:    30 * time.Second,
		},
		{
			name:            "with sequential tools",
			maxParallelism:  5,
			toolTimeout:     60 * time.Second,
			sequentialTools: []string{"save_to_memory", "custom_tool"},
			wantMax:         5,
			wantTimeout:     60 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewExecutor(tt.maxParallelism, tt.toolTimeout, tt.sequentialTools)

			if e.maxParallelism != tt.wantMax {
				t.Errorf("maxParallelism = %d, want %d", e.maxParallelism, tt.wantMax)
			}
			if e.toolTimeout != tt.wantTimeout {
				t.Errorf("toolTimeout = %v, want %v", e.toolTimeout, tt.wantTimeout)
			}
			for _, tool := range tt.sequentialTools {
				if !e.sequentialTools[tool] {
					t.Errorf("sequentialTools missing %s", tool)
				}
			}
		})
	}
}

func TestExecutor_CanParallelize(t *testing.T) {
	e := NewExecutor(5, 30*time.Second, []string{"save_to_memory"})

	tests := []struct {
		name  string
		calls []ToolCall
		want  bool
	}{
		{
			name:  "empty calls",
			calls: []ToolCall{},
			want:  false,
		},
		{
			name: "single call",
			calls: []ToolCall{
				{Name: "search_memory"},
			},
			want: false,
		},
		{
			name: "multiple parallel calls",
			calls: []ToolCall{
				{Name: "search_memory"},
				{Name: "transfer_to_agent"},
			},
			want: true,
		},
		{
			name: "contains sequential tool",
			calls: []ToolCall{
				{Name: "search_memory"},
				{Name: "save_to_memory"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := e.CanParallelize(tt.calls); got != tt.want {
				t.Errorf("CanParallelize() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExecutor_ExecuteParallel(t *testing.T) {
	e := NewExecutor(5, 30*time.Second, nil)
	ctx := context.Background()

	t.Run("empty calls", func(t *testing.T) {
		results := e.ExecuteParallel(ctx, nil)
		if len(results) != 0 {
			t.Errorf("expected empty results, got %d", len(results))
		}
	})

	t.Run("single call", func(t *testing.T) {
		calls := []ToolCall{
			{
				ID:   "1",
				Name: "test_tool",
				Execute: func(ctx context.Context) (map[string]any, error) {
					return map[string]any{"result": "ok"}, nil
				},
			},
		}

		results := e.ExecuteParallel(ctx, calls)

		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if results[0].Error != nil {
			t.Errorf("unexpected error: %v", results[0].Error)
		}
		if results[0].Response["result"] != "ok" {
			t.Errorf("unexpected response: %v", results[0].Response)
		}
	})

	t.Run("parallel execution", func(t *testing.T) {
		var counter int32
		startTime := time.Now()

		calls := []ToolCall{
			{
				ID:   "1",
				Name: "slow_tool_1",
				Execute: func(ctx context.Context) (map[string]any, error) {
					time.Sleep(100 * time.Millisecond)
					atomic.AddInt32(&counter, 1)
					return map[string]any{"id": "1"}, nil
				},
			},
			{
				ID:   "2",
				Name: "slow_tool_2",
				Execute: func(ctx context.Context) (map[string]any, error) {
					time.Sleep(100 * time.Millisecond)
					atomic.AddInt32(&counter, 1)
					return map[string]any{"id": "2"}, nil
				},
			},
			{
				ID:   "3",
				Name: "slow_tool_3",
				Execute: func(ctx context.Context) (map[string]any, error) {
					time.Sleep(100 * time.Millisecond)
					atomic.AddInt32(&counter, 1)
					return map[string]any{"id": "3"}, nil
				},
			},
		}

		results := e.ExecuteParallel(ctx, calls)
		elapsed := time.Since(startTime)

		// All should complete
		if atomic.LoadInt32(&counter) != 3 {
			t.Errorf("expected 3 completions, got %d", counter)
		}

		// Should take ~100ms (parallel), not ~300ms (sequential)
		if elapsed > 200*time.Millisecond {
			t.Errorf("execution took too long: %v (expected parallel execution)", elapsed)
		}

		// Results should be in order
		if len(results) != 3 {
			t.Fatalf("expected 3 results, got %d", len(results))
		}
		for i, r := range results {
			if r.Error != nil {
				t.Errorf("result %d: unexpected error: %v", i, r.Error)
			}
		}
	})

	t.Run("error handling", func(t *testing.T) {
		expectedErr := errors.New("tool failed")
		calls := []ToolCall{
			{
				ID:   "1",
				Name: "success_tool",
				Execute: func(ctx context.Context) (map[string]any, error) {
					return map[string]any{"ok": true}, nil
				},
			},
			{
				ID:   "2",
				Name: "failing_tool",
				Execute: func(ctx context.Context) (map[string]any, error) {
					return nil, expectedErr
				},
			},
		}

		results := e.ExecuteParallel(ctx, calls)

		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}
		if results[0].Error != nil {
			t.Errorf("result 0: unexpected error: %v", results[0].Error)
		}
		if results[1].Error != expectedErr {
			t.Errorf("result 1: expected error %v, got %v", expectedErr, results[1].Error)
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		calls := []ToolCall{
			{
				ID:   "1",
				Name: "long_running_tool",
				Execute: func(ctx context.Context) (map[string]any, error) {
					select {
					case <-time.After(5 * time.Second):
						return map[string]any{"done": true}, nil
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				},
			},
		}

		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		results := e.ExecuteParallel(ctx, calls)

		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if results[0].Error == nil {
			t.Error("expected context cancellation error")
		}
	})
}

func TestExecutor_SemaphoreLimit(t *testing.T) {
	// Executor with max parallelism of 2
	e := NewExecutor(2, 30*time.Second, nil)
	ctx := context.Background()

	var concurrent int32
	var maxConcurrent int32

	calls := make([]ToolCall, 5)
	for i := 0; i < 5; i++ {
		calls[i] = ToolCall{
			ID:   string(rune('a' + i)),
			Name: "concurrent_tool",
			Execute: func(ctx context.Context) (map[string]any, error) {
				current := atomic.AddInt32(&concurrent, 1)

				// Track max concurrent
				for {
					max := atomic.LoadInt32(&maxConcurrent)
					if current <= max || atomic.CompareAndSwapInt32(&maxConcurrent, max, current) {
						break
					}
				}

				time.Sleep(50 * time.Millisecond)
				atomic.AddInt32(&concurrent, -1)
				return nil, nil
			},
		}
	}

	e.ExecuteParallel(ctx, calls)

	if atomic.LoadInt32(&maxConcurrent) > 2 {
		t.Errorf("max concurrent = %d, expected <= 2", maxConcurrent)
	}
}

func TestExecuteParallelSimple(t *testing.T) {
	ctx := context.Background()

	t.Run("basic execution", func(t *testing.T) {
		fns := []func(context.Context) (int, error){
			func(ctx context.Context) (int, error) { return 1, nil },
			func(ctx context.Context) (int, error) { return 2, nil },
			func(ctx context.Context) (int, error) { return 3, nil },
		}

		results, errs := ExecuteParallelSimple(ctx, 5, fns)

		if len(results) != 3 {
			t.Fatalf("expected 3 results, got %d", len(results))
		}
		for i, r := range results {
			if r != i+1 {
				t.Errorf("result %d = %d, want %d", i, r, i+1)
			}
			if errs[i] != nil {
				t.Errorf("error %d: %v", i, errs[i])
			}
		}
	})

	t.Run("empty functions", func(t *testing.T) {
		results, errs := ExecuteParallelSimple[int](ctx, 5, nil)
		if results != nil || errs != nil {
			t.Error("expected nil results for empty input")
		}
	})

	t.Run("single function", func(t *testing.T) {
		fns := []func(context.Context) (string, error){
			func(ctx context.Context) (string, error) { return "hello", nil },
		}

		results, errs := ExecuteParallelSimple(ctx, 5, fns)

		if len(results) != 1 || results[0] != "hello" {
			t.Errorf("unexpected result: %v", results)
		}
		if errs[0] != nil {
			t.Errorf("unexpected error: %v", errs[0])
		}
	})
}

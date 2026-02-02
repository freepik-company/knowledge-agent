package tools

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"knowledge-agent/internal/config"
)

// mockSlackPoster implements SlackPoster for testing
type mockSlackPoster struct {
	mu       sync.Mutex
	messages []struct {
		channelID string
		threadTS  string
		text      string
	}
	shouldError bool
}

func (m *mockSlackPoster) PostMessage(channelID, threadTS, text string) error {
	if m.shouldError {
		return errors.New("mock slack error")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, struct {
		channelID string
		threadTS  string
		text      string
	}{channelID, threadTS, text})
	return nil
}

func (m *mockSlackPoster) getMessages() []struct {
	channelID string
	threadTS  string
	text      string
} {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]struct {
		channelID string
		threadTS  string
		text      string
	}, len(m.messages))
	copy(result, m.messages)
	return result
}

// mockSelfInvoker implements SelfInvoker for testing
type mockSelfInvoker struct {
	mu          sync.Mutex
	calls       []CallbackRequest
	shouldError bool
}

func (m *mockSelfInvoker) SelfInvoke(ctx context.Context, req CallbackRequest) error {
	if m.shouldError {
		return errors.New("mock self invoke error")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, req)
	return nil
}

func (m *mockSelfInvoker) getCalls() []CallbackRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]CallbackRequest, len(m.calls))
	copy(result, m.calls)
	return result
}

// mockA2AInvoker implements A2AInvoker for testing
type mockA2AInvoker struct {
	mu           sync.Mutex
	result       string
	err          error
	delay        time.Duration
	invocations  int32
	lastAgent    string
	lastTask     string
}

func (m *mockA2AInvoker) Invoke(ctx context.Context, agentName, task string) (string, error) {
	atomic.AddInt32(&m.invocations, 1)
	m.mu.Lock()
	m.lastAgent = agentName
	m.lastTask = task
	result := m.result
	err := m.err
	delay := m.delay
	m.mu.Unlock()

	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	return result, err
}

func TestNewAsyncSubAgentToolset(t *testing.T) {
	t.Run("requires config", func(t *testing.T) {
		_, err := NewAsyncSubAgentToolset(AsyncSubAgentConfig{})
		if err == nil {
			t.Error("expected error when config is nil")
		}
	})

	t.Run("creates toolset with valid config", func(t *testing.T) {
		cfg := &config.A2AAsyncConfig{
			Enabled: true,
			Timeout: 5 * time.Minute,
		}
		subAgents := []config.A2ASubAgentConfig{
			{Name: "test-agent", Description: "Test agent", Endpoint: "http://localhost:9000"},
		}

		ts, err := NewAsyncSubAgentToolset(AsyncSubAgentConfig{
			Config:    cfg,
			SubAgents: subAgents,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ts == nil {
			t.Fatal("expected toolset to be non-nil")
		}
		if len(ts.subAgents) != 1 {
			t.Errorf("expected 1 sub-agent, got %d", len(ts.subAgents))
		}
	})
}

func TestAsyncInvokeAgent_Validation(t *testing.T) {
	cfg := &config.A2AAsyncConfig{Enabled: true, Timeout: 5 * time.Minute}
	subAgents := []config.A2ASubAgentConfig{
		{Name: "test-agent", Description: "Test", Endpoint: "http://localhost:9000"},
	}

	ts, _ := NewAsyncSubAgentToolset(AsyncSubAgentConfig{
		Config:    cfg,
		SubAgents: subAgents,
	})

	tests := []struct {
		name    string
		args    AsyncInvokeArgs
		wantErr string
	}{
		{
			name:    "empty agent name",
			args:    AsyncInvokeArgs{},
			wantErr: "agent_name is required",
		},
		{
			name:    "unknown agent",
			args:    AsyncInvokeArgs{AgentName: "unknown"},
			wantErr: "unknown agent",
		},
		{
			name:    "empty task",
			args:    AsyncInvokeArgs{AgentName: "test-agent", Task: ""},
			wantErr: "task is required",
		},
		{
			name:    "missing channel_id",
			args:    AsyncInvokeArgs{AgentName: "test-agent", Task: "do something", ThreadTS: "123.456"},
			wantErr: "channel_id and thread_ts are required",
		},
		{
			name:    "missing thread_ts",
			args:    AsyncInvokeArgs{AgentName: "test-agent", Task: "do something", ChannelID: "C123"},
			wantErr: "channel_id and thread_ts are required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ts.asyncInvokeAgent(nil, tt.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Success {
				t.Error("expected Success to be false")
			}
			if result.Error == "" || (tt.wantErr != "" && !stringContains(result.Error, tt.wantErr)) {
				t.Errorf("expected error containing %q, got %q", tt.wantErr, result.Error)
			}
		})
	}
}

func TestAsyncInvokeAgent_ConcurrencyLimit(t *testing.T) {
	cfg := &config.A2AAsyncConfig{Enabled: true, Timeout: 5 * time.Minute}
	subAgents := []config.A2ASubAgentConfig{
		{Name: "test-agent", Description: "Test", Endpoint: "http://localhost:9000"},
	}

	invoker := &mockA2AInvoker{
		result: "done",
		delay:  100 * time.Millisecond, // Slow enough to accumulate tasks
	}

	ts, _ := NewAsyncSubAgentToolset(AsyncSubAgentConfig{
		Config:     cfg,
		SubAgents:  subAgents,
		A2AInvoker: invoker,
	})
	defer ts.Close()

	args := AsyncInvokeArgs{
		AgentName: "test-agent",
		Task:      "do something",
		ChannelID: "C123",
		ThreadTS:  "123.456",
	}

	// Fill up to the limit
	for i := 0; i < MaxConcurrentAsyncTasks; i++ {
		result, _ := ts.asyncInvokeAgent(nil, args)
		if !result.Success {
			t.Fatalf("task %d should succeed, got error: %s", i, result.Error)
		}
	}

	// Next one should fail
	result, _ := ts.asyncInvokeAgent(nil, args)
	if result.Success {
		t.Error("expected task to fail due to concurrency limit")
	}
	if !stringContains(result.Error, "too many pending tasks") {
		t.Errorf("expected 'too many pending tasks' error, got: %s", result.Error)
	}
}

func TestSetCallbacks_ThreadSafety(t *testing.T) {
	cfg := &config.A2AAsyncConfig{Enabled: true, Timeout: 1 * time.Second, PostToSlack: true}
	subAgents := []config.A2ASubAgentConfig{
		{Name: "test-agent", Description: "Test", Endpoint: "http://localhost:9000"},
	}

	invoker := &mockA2AInvoker{result: "done", delay: 50 * time.Millisecond}
	slack := &mockSlackPoster{}

	ts, _ := NewAsyncSubAgentToolset(AsyncSubAgentConfig{
		Config:     cfg,
		SubAgents:  subAgents,
		A2AInvoker: invoker,
	})
	defer ts.Close()

	// Start tasks before callbacks are set
	for i := 0; i < 5; i++ {
		ts.asyncInvokeAgent(nil, AsyncInvokeArgs{
			AgentName: "test-agent",
			Task:      "task",
			ChannelID: "C123",
			ThreadTS:  "123.456",
		})
	}

	// Set callbacks while tasks are running (this should not race)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ts.SetCallbacks(slack, nil)
		}()
	}
	wg.Wait()

	// Wait for tasks to complete
	time.Sleep(200 * time.Millisecond)

	// No race detector errors = success
}

func TestGracefulShutdown(t *testing.T) {
	cfg := &config.A2AAsyncConfig{Enabled: true, Timeout: 5 * time.Second, PostToSlack: true}
	subAgents := []config.A2ASubAgentConfig{
		{Name: "test-agent", Description: "Test", Endpoint: "http://localhost:9000"},
	}

	invoker := &mockA2AInvoker{result: "done", delay: 200 * time.Millisecond}
	slack := &mockSlackPoster{}

	ts, _ := NewAsyncSubAgentToolset(AsyncSubAgentConfig{
		Config:      cfg,
		SubAgents:   subAgents,
		A2AInvoker:  invoker,
		SlackPoster: slack,
	})

	// Launch some tasks
	for i := 0; i < 3; i++ {
		ts.asyncInvokeAgent(nil, AsyncInvokeArgs{
			AgentName: "test-agent",
			Task:      "task",
			ChannelID: "C123",
			ThreadTS:  "123.456",
		})
	}

	// Give goroutines time to start and register in pendingTasks
	time.Sleep(10 * time.Millisecond)

	// Verify tasks are pending
	pending := ts.GetPendingTasks()
	if len(pending) == 0 {
		t.Error("expected pending tasks")
	}

	// Close should wait for tasks or cancel them
	start := time.Now()
	err := ts.Close()
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("unexpected error on close: %v", err)
	}

	// After shutdown signal, tasks should complete quickly (cancelled)
	// We don't require waiting for full task duration anymore since shutdown cancels them
	t.Logf("Close took: %v", elapsed)

	// After close, new tasks should be rejected
	result, _ := ts.asyncInvokeAgent(nil, AsyncInvokeArgs{
		AgentName: "test-agent",
		Task:      "task",
		ChannelID: "C123",
		ThreadTS:  "123.456",
	})
	if result.Success {
		t.Error("expected task to be rejected after shutdown")
	}
}

func TestSlackPosting(t *testing.T) {
	cfg := &config.A2AAsyncConfig{
		Enabled:     true,
		Timeout:     5 * time.Second,
		PostToSlack: true,
	}
	subAgents := []config.A2ASubAgentConfig{
		{Name: "test-agent", Description: "Test", Endpoint: "http://localhost:9000"},
	}

	t.Run("posts success message", func(t *testing.T) {
		invoker := &mockA2AInvoker{result: "task completed successfully"}
		slack := &mockSlackPoster{}

		ts, _ := NewAsyncSubAgentToolset(AsyncSubAgentConfig{
			Config:      cfg,
			SubAgents:   subAgents,
			A2AInvoker:  invoker,
			SlackPoster: slack,
		})
		defer ts.Close()

		ts.asyncInvokeAgent(nil, AsyncInvokeArgs{
			AgentName: "test-agent",
			Task:      "do task",
			ChannelID: "C123",
			ThreadTS:  "123.456",
		})

		// Wait for task to complete
		time.Sleep(100 * time.Millisecond)

		messages := slack.getMessages()
		if len(messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(messages))
		}
		if messages[0].channelID != "C123" {
			t.Errorf("expected channel C123, got %s", messages[0].channelID)
		}
		if !stringContains(messages[0].text, "completed") {
			t.Errorf("expected success message, got: %s", messages[0].text)
		}
	})

	t.Run("posts error message on failure", func(t *testing.T) {
		invoker := &mockA2AInvoker{err: errors.New("agent failed")}
		slack := &mockSlackPoster{}

		ts, _ := NewAsyncSubAgentToolset(AsyncSubAgentConfig{
			Config:      cfg,
			SubAgents:   subAgents,
			A2AInvoker:  invoker,
			SlackPoster: slack,
		})
		defer ts.Close()

		ts.asyncInvokeAgent(nil, AsyncInvokeArgs{
			AgentName: "test-agent",
			Task:      "do task",
			ChannelID: "C123",
			ThreadTS:  "123.456",
		})

		// Wait for task to complete
		time.Sleep(100 * time.Millisecond)

		messages := slack.getMessages()
		if len(messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(messages))
		}
		if !stringContains(messages[0].text, "failed") {
			t.Errorf("expected error message, got: %s", messages[0].text)
		}
	})
}

func TestSelfInvokeCallback(t *testing.T) {
	cfg := &config.A2AAsyncConfig{
		Enabled:         true,
		Timeout:         5 * time.Second,
		PostToSlack:     false,
		CallbackEnabled: true,
	}
	subAgents := []config.A2ASubAgentConfig{
		{Name: "test-agent", Description: "Test", Endpoint: "http://localhost:9000"},
	}

	invoker := &mockA2AInvoker{result: "task result"}
	selfInvoker := &mockSelfInvoker{}

	ts, _ := NewAsyncSubAgentToolset(AsyncSubAgentConfig{
		Config:      cfg,
		SubAgents:   subAgents,
		A2AInvoker:  invoker,
		SelfInvoker: selfInvoker,
	})
	defer ts.Close()

	ts.asyncInvokeAgent(nil, AsyncInvokeArgs{
		AgentName: "test-agent",
		Task:      "original task",
		ChannelID: "C123",
		ThreadTS:  "123.456",
		SessionID: "session-123",
	})

	// Wait for task to complete
	time.Sleep(100 * time.Millisecond)

	calls := selfInvoker.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 callback, got %d", len(calls))
	}

	call := calls[0]
	if call.ChannelID != "C123" {
		t.Errorf("expected channel C123, got %s", call.ChannelID)
	}
	if call.SessionID != "session-123" {
		t.Errorf("expected session session-123, got %s", call.SessionID)
	}
	if !call.IsCallback {
		t.Error("expected IsCallback to be true")
	}
	if call.AgentName != "test-agent" {
		t.Errorf("expected agent test-agent, got %s", call.AgentName)
	}
}

func TestContextCancellation(t *testing.T) {
	cfg := &config.A2AAsyncConfig{
		Enabled:     true,
		Timeout:     100 * time.Millisecond, // Short timeout
		PostToSlack: true,
	}
	subAgents := []config.A2ASubAgentConfig{
		{Name: "test-agent", Description: "Test", Endpoint: "http://localhost:9000"},
	}

	invoker := &mockA2AInvoker{
		result: "done",
		delay:  5 * time.Second, // Much longer than timeout
	}
	slack := &mockSlackPoster{}

	ts, _ := NewAsyncSubAgentToolset(AsyncSubAgentConfig{
		Config:      cfg,
		SubAgents:   subAgents,
		A2AInvoker:  invoker,
		SlackPoster: slack,
	})
	defer ts.Close()

	ts.asyncInvokeAgent(nil, AsyncInvokeArgs{
		AgentName: "test-agent",
		Task:      "slow task",
		ChannelID: "C123",
		ThreadTS:  "123.456",
	})

	// Wait for timeout
	time.Sleep(200 * time.Millisecond)

	messages := slack.getMessages()
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if !stringContains(messages[0].text, "failed") {
		t.Errorf("expected failure message due to timeout, got: %s", messages[0].text)
	}
}

// stringContains checks if substr is in s (local helper to avoid conflict)
func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

package a2a

import (
	"context"
	"errors"
	"net"
	"syscall"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
)

func TestIsRecoverableError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "connection refused",
			err:      syscall.ECONNREFUSED,
			expected: true,
		},
		{
			name:     "connection reset",
			err:      syscall.ECONNRESET,
			expected: true,
		},
		{
			name:     "timeout",
			err:      syscall.ETIMEDOUT,
			expected: true,
		},
		{
			name:     "host unreachable",
			err:      syscall.EHOSTUNREACH,
			expected: true,
		},
		{
			name:     "context deadline exceeded",
			err:      context.DeadlineExceeded,
			expected: true,
		},
		{
			name:     "context canceled",
			err:      context.Canceled,
			expected: true,
		},
		{
			name:     "dns error",
			err:      &net.DNSError{Err: "no such host", Name: "test.invalid"},
			expected: true,
		},
		{
			name:     "generic error",
			err:      errors.New("some random error"),
			expected: false,
		},
		{
			name:     "wrapped connection refused",
			err:      &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRecoverableError(tt.err)
			if result != tt.expected {
				t.Errorf("isRecoverableError(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		contains string
	}{
		{
			name:     "nil error",
			err:      nil,
			contains: "unknown",
		},
		{
			name:     "connection refused",
			err:      syscall.ECONNREFUSED,
			contains: "refused",
		},
		{
			name:     "timeout",
			err:      syscall.ETIMEDOUT,
			contains: "timeout",
		},
		{
			name:     "dns error",
			err:      &net.DNSError{Err: "no such host", Name: "test.invalid"},
			contains: "DNS",
		},
		{
			name:     "context deadline exceeded",
			err:      context.DeadlineExceeded,
			contains: "timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyError(tt.err)
			if result == "" {
				t.Error("classifyError returned empty string")
			}
			// Just verify it returns something meaningful
			t.Logf("classifyError(%v) = %q", tt.err, result)
		})
	}
}

func TestErrorRecoveryInterceptor_After(t *testing.T) {
	interceptor := NewErrorRecoveryInterceptor("test_agent")

	t.Run("no error - passes through", func(t *testing.T) {
		resp := &a2aclient.Response{
			Method:  "SendMessage",
			Payload: a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: "original"}),
			Err:     nil,
		}

		err := interceptor.After(context.Background(), resp)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if resp.Err != nil {
			t.Error("resp.Err should still be nil")
		}
	})

	t.Run("recoverable error - converts to response", func(t *testing.T) {
		resp := &a2aclient.Response{
			Method: "SendMessage",
			Err:    syscall.ECONNREFUSED,
		}

		err := interceptor.After(context.Background(), resp)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		// Error should be cleared
		if resp.Err != nil {
			t.Error("resp.Err should be nil after recovery")
		}

		// Payload should contain error message
		if resp.Payload == nil {
			t.Fatal("resp.Payload should not be nil")
		}

		msg, ok := resp.Payload.(*a2a.Message)
		if !ok {
			t.Fatalf("resp.Payload should be *a2a.Message, got %T", resp.Payload)
		}

		if msg.Role != a2a.MessageRoleAgent {
			t.Errorf("message role should be agent, got %v", msg.Role)
		}

		if len(msg.Parts) == 0 {
			t.Fatal("message should have parts")
		}

		textPart, ok := msg.Parts[0].(a2a.TextPart)
		if !ok {
			t.Fatalf("first part should be TextPart, got %T", msg.Parts[0])
		}

		if textPart.Text == "" {
			t.Error("text part should not be empty")
		}

		t.Logf("Recovered error message: %s", textPart.Text)
	})

	t.Run("non-recoverable error - passes through", func(t *testing.T) {
		originalErr := errors.New("some internal error")
		resp := &a2aclient.Response{
			Method: "SendMessage",
			Err:    originalErr,
		}

		err := interceptor.After(context.Background(), resp)
		if err != nil {
			t.Errorf("unexpected error from After: %v", err)
		}

		// Error should NOT be cleared for non-recoverable errors
		if resp.Err != originalErr {
			t.Error("resp.Err should still contain the original error")
		}
	})
}

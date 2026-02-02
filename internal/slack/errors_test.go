package slack

import (
	"context"
	"errors"
	"net"
	"syscall"
	"testing"
)

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		statusCode int
		want       ErrorType
	}{
		// HTTP status code tests
		{
			name:       "status 500 returns ServerError",
			err:        nil,
			statusCode: 500,
			want:       ErrorTypeServerError,
		},
		{
			name:       "status 502 returns ServerError",
			err:        nil,
			statusCode: 502,
			want:       ErrorTypeServerError,
		},
		{
			name:       "status 503 returns Overloaded",
			err:        nil,
			statusCode: 503,
			want:       ErrorTypeOverloaded,
		},
		{
			name:       "status 504 returns Timeout",
			err:        nil,
			statusCode: 504,
			want:       ErrorTypeTimeout,
		},
		{
			name:       "status 408 returns Timeout",
			err:        nil,
			statusCode: 408,
			want:       ErrorTypeTimeout,
		},
		{
			name:       "status 413 returns PayloadTooLarge",
			err:        nil,
			statusCode: 413,
			want:       ErrorTypePayloadTooLarge,
		},
		{
			name:       "status 200 with nil error returns Unknown",
			err:        nil,
			statusCode: 200,
			want:       ErrorTypeUnknown,
		},
		{
			name:       "status 0 with nil error returns Unknown",
			err:        nil,
			statusCode: 0,
			want:       ErrorTypeUnknown,
		},

		// Context errors
		{
			name:       "context.DeadlineExceeded returns Timeout",
			err:        context.DeadlineExceeded,
			statusCode: 0,
			want:       ErrorTypeTimeout,
		},
		{
			name:       "context.Canceled returns Timeout",
			err:        context.Canceled,
			statusCode: 0,
			want:       ErrorTypeTimeout,
		},
		{
			name:       "wrapped context deadline exceeded returns Timeout",
			err:        errors.New("operation failed: context deadline exceeded"),
			statusCode: 0,
			want:       ErrorTypeTimeout,
		},
		{
			name:       "wrapped context canceled returns Timeout",
			err:        errors.New("request failed: context canceled"),
			statusCode: 0,
			want:       ErrorTypeTimeout,
		},

		// String-based error detection
		{
			name:       "connection refused string returns ConnectionRefused",
			err:        errors.New("dial tcp: connection refused"),
			statusCode: 0,
			want:       ErrorTypeConnectionRefused,
		},
		{
			name:       "EOF error returns Overloaded",
			err:        errors.New("unexpected EOF"),
			statusCode: 0,
			want:       ErrorTypeOverloaded,
		},
		{
			name:       "connection reset returns Overloaded",
			err:        errors.New("read: connection reset by peer"),
			statusCode: 0,
			want:       ErrorTypeOverloaded,
		},
		{
			name:       "timeout in error string returns Timeout",
			err:        errors.New("i/o timeout"),
			statusCode: 0,
			want:       ErrorTypeTimeout,
		},
		{
			name:       "timed out in error string returns Timeout",
			err:        errors.New("operation timed out"),
			statusCode: 0,
			want:       ErrorTypeTimeout,
		},

		// Unknown errors
		{
			name:       "generic error returns Unknown",
			err:        errors.New("something went wrong"),
			statusCode: 0,
			want:       ErrorTypeUnknown,
		},
		{
			name:       "status code takes precedence over error",
			err:        errors.New("some random error"),
			statusCode: 503,
			want:       ErrorTypeOverloaded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyError(tt.err, tt.statusCode)
			if got != tt.want {
				t.Errorf("ClassifyError() = %v, want %v", got, tt.want)
			}
		})
	}
}

// mockNetError implements net.Error interface for testing
type mockNetError struct {
	timeout   bool
	temporary bool
	msg       string
}

func (e *mockNetError) Error() string   { return e.msg }
func (e *mockNetError) Timeout() bool   { return e.timeout }
func (e *mockNetError) Temporary() bool { return e.temporary }

func TestClassifyError_NetError(t *testing.T) {
	tests := []struct {
		name string
		err  net.Error
		want ErrorType
	}{
		{
			name: "net.Error with Timeout() true returns Timeout",
			err:  &mockNetError{timeout: true, msg: "network timeout"},
			want: ErrorTypeTimeout,
		},
		{
			name: "net.Error with Timeout() false returns Unknown",
			err:  &mockNetError{timeout: false, msg: "network error"},
			want: ErrorTypeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyError(tt.err, 0)
			if got != tt.want {
				t.Errorf("ClassifyError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClassifyError_OpError(t *testing.T) {
	// Test with a real net.OpError wrapping ECONNREFUSED
	connRefusedErr := &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: &net.OpError{
			Op:  "connect",
			Net: "tcp",
			Err: syscall.ECONNREFUSED,
		},
	}

	// Note: The current implementation checks for *syscall.Errno which won't match
	// syscall.ECONNREFUSED directly. The string fallback should catch it.
	got := ClassifyError(connRefusedErr, 0)

	// Should be caught by string matching "connection refused"
	if got != ErrorTypeConnectionRefused {
		t.Errorf("ClassifyError(connRefusedErr) = %v, want ConnectionRefused", got)
	}
}

func TestFormatUserError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		statusCode int
		wantMsg    string
	}{
		{
			name:       "timeout error",
			err:        context.DeadlineExceeded,
			statusCode: 0,
			wantMsg:    FriendlyErrorMessages[ErrorTypeTimeout],
		},
		{
			name:       "connection refused",
			err:        errors.New("connection refused"),
			statusCode: 0,
			wantMsg:    FriendlyErrorMessages[ErrorTypeConnectionRefused],
		},
		{
			name:       "server error status",
			err:        nil,
			statusCode: 500,
			wantMsg:    FriendlyErrorMessages[ErrorTypeServerError],
		},
		{
			name:       "overloaded status",
			err:        nil,
			statusCode: 503,
			wantMsg:    FriendlyErrorMessages[ErrorTypeOverloaded],
		},
		{
			name:       "payload too large",
			err:        nil,
			statusCode: 413,
			wantMsg:    FriendlyErrorMessages[ErrorTypePayloadTooLarge],
		},
		{
			name:       "unknown error",
			err:        errors.New("random error"),
			statusCode: 0,
			wantMsg:    FriendlyErrorMessages[ErrorTypeUnknown],
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatUserError(tt.err, tt.statusCode)
			if got != tt.wantMsg {
				t.Errorf("FormatUserError() = %q, want %q", got, tt.wantMsg)
			}
		})
	}
}

func TestFriendlyErrorMessages_AllTypesHaveMessages(t *testing.T) {
	// Ensure all ErrorTypes have a corresponding friendly message
	allTypes := []ErrorType{
		ErrorTypeUnknown,
		ErrorTypeTimeout,
		ErrorTypeConnectionRefused,
		ErrorTypeServerError,
		ErrorTypeOverloaded,
		ErrorTypePayloadTooLarge,
	}

	for _, errType := range allTypes {
		if msg, ok := FriendlyErrorMessages[errType]; !ok || msg == "" {
			t.Errorf("ErrorType %v missing friendly message", errType)
		}
	}
}

func TestFriendlyErrorMessages_NoTechnicalDetails(t *testing.T) {
	// Ensure friendly messages don't contain technical jargon
	technicalTerms := []string{
		"stack trace",
		"nil pointer",
		"panic",
		"goroutine",
		"syscall",
		"ECONNREFUSED",
		"EOF",
		"HTTP",
		"status code",
	}

	for errType, msg := range FriendlyErrorMessages {
		for _, term := range technicalTerms {
			if contains(msg, term) {
				t.Errorf("ErrorType %v message contains technical term %q: %s", errType, term, msg)
			}
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsLower(s, substr))
}

func containsLower(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

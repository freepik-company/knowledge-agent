package a2a

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"syscall"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"

	"knowledge-agent/internal/logger"
)

// errorRecoveryInterceptor converts connection/timeout errors into valid A2A responses
// This allows the LLM to handle sub-agent failures gracefully instead of crashing
type errorRecoveryInterceptor struct {
	a2aclient.PassthroughInterceptor
	agentName string
}

// NewErrorRecoveryInterceptor creates a new error recovery interceptor
func NewErrorRecoveryInterceptor(agentName string) a2aclient.CallInterceptor {
	return &errorRecoveryInterceptor{agentName: agentName}
}

// After intercepts errors and converts recoverable ones into valid responses
func (eri *errorRecoveryInterceptor) After(ctx context.Context, resp *a2aclient.Response) error {
	if resp.Err == nil {
		return nil
	}

	// Check if this is a recoverable error
	if !isRecoverableError(resp.Err) {
		return nil // Let the error propagate normally
	}

	log := logger.Get()
	log.Warnw("Sub-agent error recovered - converting to response",
		"agent", eri.agentName,
		"error", resp.Err,
		"method", resp.Method,
	)

	// Create a message that explains the error to the LLM
	errorMsg := fmt.Sprintf(
		"Error: The sub-agent '%s' is currently unavailable. Reason: %s. "+
			"Please inform the user that this information source is temporarily unavailable "+
			"and try to answer with the information you have from other sources.",
		eri.agentName,
		classifyError(resp.Err),
	)

	// Create a valid A2A response with the error message
	resp.Payload = a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: errorMsg})
	resp.Err = nil // Clear the error so ADK doesn't fail

	return nil
}

// isRecoverableError checks if an error is recoverable (connection issues, timeouts, etc.)
func isRecoverableError(err error) bool {
	if err == nil {
		return false
	}

	// Network errors
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true // All network errors are recoverable
	}

	// DNS errors
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}

	// URL errors (often wrap network errors)
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}

	// Syscall errors (connection refused, etc.)
	var syscallErr syscall.Errno
	if errors.As(err, &syscallErr) {
		switch syscallErr {
		case syscall.ECONNREFUSED, syscall.ECONNRESET, syscall.ECONNABORTED,
			syscall.ETIMEDOUT, syscall.EHOSTUNREACH, syscall.ENETUNREACH:
			return true
		}
	}

	// Context errors
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}

	return false
}

// classifyError returns a user-friendly description of the error
func classifyError(err error) string {
	if err == nil {
		return "unknown error"
	}

	// Check for specific error types
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return "connection timeout"
		}
		return "network error"
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "DNS resolution failed (host not found)"
	}

	var syscallErr syscall.Errno
	if errors.As(err, &syscallErr) {
		switch syscallErr {
		case syscall.ECONNREFUSED:
			return "connection refused (service may be down)"
		case syscall.ECONNRESET:
			return "connection reset"
		case syscall.ETIMEDOUT:
			return "connection timeout"
		case syscall.EHOSTUNREACH:
			return "host unreachable"
		case syscall.ENETUNREACH:
			return "network unreachable"
		}
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return "request timeout"
	}

	if errors.Is(err, context.Canceled) {
		return "request canceled"
	}

	return "connection error"
}

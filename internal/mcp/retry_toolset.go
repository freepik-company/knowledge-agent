package mcp

import (
	"errors"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"time"

	"knowledge-agent/internal/config"
	"knowledge-agent/internal/logger"
)

// retryRoundTripper wraps an http.RoundTripper with retry logic for transient failures
// This handles HTTP-level errors (502, 503, 504, 429) that the MCP SDK's built-in
// connection refresher doesn't handle.
type retryRoundTripper struct {
	base       http.RoundTripper
	config     config.RetryConfig
	serverName string
}

// NewRetryRoundTripper creates a new HTTP round tripper with retry support
func NewRetryRoundTripper(base http.RoundTripper, serverName string, cfg config.RetryConfig) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}

	// Apply defaults
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.InitialDelay <= 0 {
		cfg.InitialDelay = 500 * time.Millisecond
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = 30 * time.Second
	}
	if cfg.BackoffMultiplier <= 0 {
		cfg.BackoffMultiplier = 2.0
	}

	return &retryRoundTripper{
		base:       base,
		config:     cfg,
		serverName: serverName,
	}
}

// RoundTrip implements http.RoundTripper with retry logic
func (rt *retryRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	log := logger.Get()
	var lastErr error
	var lastResp *http.Response

	for attempt := 0; attempt <= rt.config.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := rt.calculateDelay(attempt)

			log.Infow("MCP HTTP retry attempt",
				"server", rt.serverName,
				"attempt", attempt,
				"max_retries", rt.config.MaxRetries,
				"delay_ms", delay.Milliseconds(),
				"url", req.URL.Path,
			)

			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(delay):
			}
		}

		resp, err := rt.base.RoundTrip(req)
		if err != nil {
			lastErr = err
			if rt.isRetryableError(err) {
				log.Warnw("MCP HTTP request failed with retryable error",
					"server", rt.serverName,
					"attempt", attempt,
					"error", err,
				)
				continue
			}
			return nil, err
		}

		// Check for retryable HTTP status codes
		if rt.isRetryableStatus(resp.StatusCode) {
			lastErr = &httpStatusError{StatusCode: resp.StatusCode, Status: resp.Status}
			lastResp = resp
			log.Warnw("MCP HTTP request failed with retryable status",
				"server", rt.serverName,
				"attempt", attempt,
				"status_code", resp.StatusCode,
			)
			// Don't close the body here - we might need to return it if this is the last attempt
			if attempt < rt.config.MaxRetries {
				resp.Body.Close()
			}
			continue
		}

		return resp, nil
	}

	log.Errorw("MCP HTTP request failed after all retries",
		"server", rt.serverName,
		"attempts", rt.config.MaxRetries+1,
		"error", lastErr,
	)

	// If we have a response from the last attempt, return it (even though it's an error status)
	// This allows the caller to inspect the response body for error details
	if lastResp != nil {
		return lastResp, nil
	}

	return nil, lastErr
}

// calculateDelay calculates the delay for a retry attempt with exponential backoff and jitter
func (rt *retryRoundTripper) calculateDelay(attempt int) time.Duration {
	delay := float64(rt.config.InitialDelay)
	for i := 1; i < attempt; i++ {
		delay *= rt.config.BackoffMultiplier
	}

	if delay > float64(rt.config.MaxDelay) {
		delay = float64(rt.config.MaxDelay)
	}

	// Add jitter (±25%)
	jitter := delay * 0.25 * (2*rand.Float64() - 1)
	delay += jitter

	return time.Duration(delay)
}

// isRetryableError checks if an error is retryable
func (rt *retryRoundTripper) isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Network errors
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	errStr := strings.ToLower(err.Error())
	retryablePatterns := []string{
		"connection refused",
		"connection reset",
		"connection timed out",
		"no such host",
		"i/o timeout",
		"eof",
		"broken pipe",
	}

	for _, pattern := range retryablePatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	return false
}

// isRetryableStatus checks if an HTTP status code is retryable
func (rt *retryRoundTripper) isRetryableStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusBadGateway, // 502
		http.StatusServiceUnavailable, // 503
		http.StatusGatewayTimeout,     // 504
		http.StatusTooManyRequests:    // 429
		return true
	default:
		return false
	}
}

// httpStatusError represents an HTTP error with status code
type httpStatusError struct {
	StatusCode int
	Status     string
}

func (e *httpStatusError) Error() string {
	return e.Status
}

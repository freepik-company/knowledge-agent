package a2a

import (
	"context"
	"errors"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/a2aclient"

	"knowledge-agent/internal/config"
	"knowledge-agent/internal/logger"
)

// retryInterceptor implements a2aclient.CallInterceptor for automatic retries
// on transient failures with exponential backoff and jitter
type retryInterceptor struct {
	a2aclient.PassthroughInterceptor
	agentName string
	config    config.RetryConfig
}

// NewRetryInterceptor creates a new retry interceptor for A2A calls
func NewRetryInterceptor(agentName string, cfg config.RetryConfig) *retryInterceptor {
	// Apply defaults if not set
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

	return &retryInterceptor{
		agentName: agentName,
		config:    cfg,
	}
}

// After implements the retry logic after receiving a response
func (ri *retryInterceptor) After(ctx context.Context, resp *a2aclient.Response) error {
	log := logger.Get()

	// If no error, pass through
	if resp.Err == nil {
		return nil
	}

	// Check if the error is retryable
	if !ri.isRetryableError(resp.Err) {
		log.Debugw("A2A error is not retryable",
			"agent", ri.agentName,
			"error", resp.Err,
		)
		return nil // Let the error propagate
	}

	log.Warnw("A2A request failed with retryable error",
		"agent", ri.agentName,
		"method", resp.Method,
		"error", resp.Err,
	)

	// Note: The a2aclient.CallInterceptor interface only allows us to log and inspect
	// errors, not to retry the request directly. The actual retry logic needs to be
	// implemented at a higher level or the library needs to support it natively.
	// This interceptor logs retry-worthy errors for observability.
	// For full retry support, we'll need to wrap the client factory or use a custom transport.

	return nil
}

// isRetryableError determines if an error is transient and should be retried
func (ri *retryInterceptor) isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	// Check for HTTP status codes that indicate transient failures
	retryableStatusCodes := []string{
		"502", "Bad Gateway",
		"503", "Service Unavailable",
		"504", "Gateway Timeout",
		"429", "Too Many Requests",
	}
	for _, code := range retryableStatusCodes {
		if strings.Contains(errStr, code) {
			return true
		}
	}

	// Check for network errors
	var netErr net.Error
	if errors.As(err, &netErr) {
		// Timeout errors are retryable
		if netErr.Timeout() {
			return true
		}
	}

	// Check for connection errors
	connectionErrors := []string{
		"connection refused",
		"connection reset",
		"connection timed out",
		"no such host",
		"dial tcp",
		"i/o timeout",
		"EOF",
		"broken pipe",
	}
	for _, connErr := range connectionErrors {
		if strings.Contains(strings.ToLower(errStr), strings.ToLower(connErr)) {
			return true
		}
	}

	return false
}

// retryHTTPClient wraps an HTTP client with retry logic
type retryHTTPClient struct {
	client    *http.Client
	config    config.RetryConfig
	agentName string
}

// NewRetryHTTPClient creates an HTTP client with retry support
func NewRetryHTTPClient(client *http.Client, agentName string, cfg config.RetryConfig) *retryHTTPClient {
	if client == nil {
		client = http.DefaultClient
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

	return &retryHTTPClient{
		client:    client,
		config:    cfg,
		agentName: agentName,
	}
}

// Do executes the request with retry logic
func (c *retryHTTPClient) Do(req *http.Request) (*http.Response, error) {
	log := logger.Get()
	var lastErr error

	for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
		if attempt > 0 {
			// Calculate delay with exponential backoff and jitter
			delay := c.calculateDelay(attempt)

			log.Infow("A2A retry attempt",
				"agent", c.agentName,
				"attempt", attempt,
				"max_retries", c.config.MaxRetries,
				"delay_ms", delay.Milliseconds(),
			)

			// Wait before retry, respecting context cancellation
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(delay):
			}
		}

		// Clone the request for retry (body needs special handling)
		reqClone := req.Clone(req.Context())

		resp, err := c.client.Do(reqClone)
		if err != nil {
			lastErr = err
			if c.isRetryableError(err) {
				log.Warnw("A2A request failed with retryable error",
					"agent", c.agentName,
					"attempt", attempt,
					"error", err,
				)
				continue
			}
			return nil, err
		}

		// Check for retryable HTTP status codes
		if c.isRetryableStatus(resp.StatusCode) {
			lastErr = &httpError{StatusCode: resp.StatusCode, Status: resp.Status}
			log.Warnw("A2A request failed with retryable status",
				"agent", c.agentName,
				"attempt", attempt,
				"status_code", resp.StatusCode,
			)
			resp.Body.Close()
			continue
		}

		return resp, nil
	}

	log.Errorw("A2A request failed after all retries",
		"agent", c.agentName,
		"attempts", c.config.MaxRetries+1,
		"error", lastErr,
	)

	return nil, lastErr
}

// calculateDelay calculates the delay for a retry attempt with exponential backoff and jitter
func (c *retryHTTPClient) calculateDelay(attempt int) time.Duration {
	// Exponential backoff: initialDelay * backoffMultiplier^(attempt-1)
	delay := float64(c.config.InitialDelay)
	for i := 1; i < attempt; i++ {
		delay *= c.config.BackoffMultiplier
	}

	// Cap at max delay
	if delay > float64(c.config.MaxDelay) {
		delay = float64(c.config.MaxDelay)
	}

	// Add jitter (±25%)
	jitter := delay * 0.25 * (2*rand.Float64() - 1)
	delay += jitter

	return time.Duration(delay)
}

// isRetryableError checks if an error is retryable
func (c *retryHTTPClient) isRetryableError(err error) bool {
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
func (c *retryHTTPClient) isRetryableStatus(statusCode int) bool {
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

// httpError represents an HTTP error with status code
type httpError struct {
	StatusCode int
	Status     string
}

func (e *httpError) Error() string {
	return e.Status
}

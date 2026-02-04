package a2a

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/a2aclient"

	"knowledge-agent/internal/config"
	"knowledge-agent/internal/logger"
	"knowledge-agent/internal/observability"
)

// resolveAuthHeader resolves the auth configuration to header name and value
func resolveAuthHeader(auth config.A2AAuthConfig) (headerName, headerValue string, err error) {
	authType := strings.ToLower(auth.Type)

	switch authType {
	case "api_key":
		// API Key auth: custom header with key from environment
		if auth.Header == "" {
			return "", "", fmt.Errorf("api_key auth requires 'header' field")
		}
		if auth.KeyEnv == "" {
			return "", "", fmt.Errorf("api_key auth requires 'key_env' field")
		}
		key := os.Getenv(auth.KeyEnv)
		if key == "" {
			return "", "", fmt.Errorf("environment variable %s not set", auth.KeyEnv)
		}
		return auth.Header, key, nil

	case "bearer":
		// Bearer token auth: Authorization header
		if auth.TokenEnv == "" {
			return "", "", fmt.Errorf("bearer auth requires 'token_env' field")
		}
		token := os.Getenv(auth.TokenEnv)
		if token == "" {
			return "", "", fmt.Errorf("environment variable %s not set", auth.TokenEnv)
		}
		return "Authorization", "Bearer " + token, nil

	case "oauth2":
		// OAuth2 client credentials flow is not supported for sub_agents
		// Use api_key or bearer auth instead
		return "", "", fmt.Errorf("oauth2 auth not supported for sub_agents, use api_key or bearer instead")

	default:
		return "", "", fmt.Errorf("unsupported auth type: %s", authType)
	}
}

// a2aStartTimeKey is the context key for storing A2A request start time
type a2aStartTimeKey struct{}

// loggingInterceptor implements a2aclient.CallInterceptor for debugging A2A calls
type loggingInterceptor struct {
	a2aclient.PassthroughInterceptor
	agentName string
}

// Before logs the outgoing A2A request and records start time for metrics
func (li *loggingInterceptor) Before(ctx context.Context, req *a2aclient.Request) (context.Context, error) {
	log := logger.Get()
	log.Infow("A2A outgoing request",
		"agent", li.agentName,
		"method", req.Method,
		"base_url", req.BaseURL,
		"has_payload", req.Payload != nil,
	)
	// Store start time in context for After() to calculate duration
	ctx = context.WithValue(ctx, a2aStartTimeKey{}, time.Now())
	return ctx, nil
}

// After logs the A2A response and records metrics
func (li *loggingInterceptor) After(ctx context.Context, resp *a2aclient.Response) error {
	log := logger.Get()

	// Calculate duration from context
	var duration time.Duration
	if startTime, ok := ctx.Value(a2aStartTimeKey{}).(time.Time); ok {
		duration = time.Since(startTime)
	}

	success := resp.Err == nil

	if resp.Err != nil {
		log.Errorw("A2A request failed",
			"agent", li.agentName,
			"method", resp.Method,
			"error", resp.Err,
			"duration_ms", duration.Milliseconds(),
		)
	} else {
		log.Infow("A2A response received",
			"agent", li.agentName,
			"method", resp.Method,
			"base_url", resp.BaseURL,
			"has_payload", resp.Payload != nil,
			"duration_ms", duration.Milliseconds(),
		)
	}

	// Record A2A metrics
	observability.GetMetrics().RecordA2ACall(li.agentName, duration, success)

	return nil
}

// authInterceptor implements a2aclient.CallInterceptor to add auth headers to requests
type authInterceptor struct {
	a2aclient.PassthroughInterceptor
	headerName  string
	headerValue string
}

// Before adds the auth header to the request metadata
func (ai *authInterceptor) Before(ctx context.Context, req *a2aclient.Request) (context.Context, error) {
	log := logger.Get()
	log.Debugw("A2A interceptor: Adding auth header to request",
		"header", ai.headerName,
		"method", req.Method,
	)

	if req.Meta == nil {
		req.Meta = make(a2aclient.CallMeta)
	}
	req.Meta[ai.headerName] = []string{ai.headerValue}
	return ctx, nil
}

package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"knowledge-agent/internal/auth/keycloak"
	"knowledge-agent/internal/ctxutil"
	"knowledge-agent/internal/logger"
	"knowledge-agent/internal/observability"
)

// RESTClient is a simple HTTP client for calling sub-agents via /api/query
// It's simpler and faster than the A2A protocol for internal agents
type RESTClient struct {
	name           string           // Agent name for logging
	endpoint       string           // Full URL to /api/query endpoint
	httpClient     *http.Client     // HTTP client with configured timeout
	authHeaderName string           // Auth header name (e.g., "X-API-Key")
	authHeaderVal  string           // Auth header value
	keycloakClient *keycloak.Client // Optional Keycloak client for JWT propagation
}

// RESTClientConfig holds configuration for creating a REST client
type RESTClientConfig struct {
	Name           string           // Agent name (for logging)
	BaseURL        string           // Base URL (e.g., http://agent:8081)
	APIPath        string           // API endpoint path (e.g., "/query" or "/api/query"). Default: "/query"
	Timeout        time.Duration    // HTTP timeout
	AuthHeaderName string           // Auth header name (empty if no auth)
	AuthHeaderVal  string           // Auth header value
	KeycloakClient *keycloak.Client // Optional Keycloak client
}

// QueryRequest is the request body for REST sub-agent endpoints
type QueryRequest struct {
	Query     string `json:"query"` // The query to send (fc_logs_agent expects "query")
	ChannelID string `json:"channel_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

// QueryResponse wraps a sub-agent response and provides format-agnostic access.
// It handles multiple response formats transparently:
// - Known formats: {"answer": "..."}, {"response": "..."}, {"text": "..."}, etc.
// - Unknown formats: The entire JSON is converted to text for LLM interpretation
// - Plain text: Used directly if response is not valid JSON
type QueryResponse struct {
	// Extracted answer text (from known field or entire response)
	extractedAnswer string
	// Whether the response indicates success
	success bool
	// Error message if present
	errorMessage string
	// Raw response for debugging
	rawResponse string
}

// knownAnswerFields are field names commonly used for response text, in priority order
var knownAnswerFields = []string{
	"answer",   // Our format
	"response", // ADK format
	"text",     // Common
	"result",   // Common
	"output",   // Common
	"content",  // Common
	"data",     // Common wrapper
	"message",  // Sometimes used for response (check context)
}

// knownErrorFields are field names that indicate an error message
var knownErrorFields = []string{
	"error",
	"error_message",
	"err",
}

// knownSuccessFields are field names that indicate success status
var knownSuccessFields = []string{
	"success",
	"ok",
	"status",
}

// ParseQueryResponse parses a raw response body into a QueryResponse.
// It attempts to extract the answer from known fields, falling back to
// converting the entire response to text if no known fields are found.
func ParseQueryResponse(body []byte) *QueryResponse {
	resp := &QueryResponse{
		rawResponse: string(body),
		success:     true, // Assume success unless we find evidence otherwise
	}

	// Try to parse as JSON
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		// Not valid JSON - use raw text as the answer
		resp.extractedAnswer = string(body)
		return resp
	}

	// Check for error indicators first
	for _, field := range knownErrorFields {
		if errVal, ok := data[field]; ok {
			if errStr, ok := errVal.(string); ok && errStr != "" {
				resp.success = false
				resp.errorMessage = errStr
				return resp
			}
		}
	}

	// Check for explicit success field
	for _, field := range knownSuccessFields {
		if val, ok := data[field]; ok {
			switch v := val.(type) {
			case bool:
				resp.success = v
			case string:
				resp.success = strings.EqualFold(v, "true") || strings.EqualFold(v, "ok") || strings.EqualFold(v, "success")
			}
			break
		}
	}

	// Try to extract answer from known fields
	for _, field := range knownAnswerFields {
		if val, ok := data[field]; ok {
			switch v := val.(type) {
			case string:
				if v != "" {
					resp.extractedAnswer = v
					return resp
				}
			case map[string]any:
				// Nested object (e.g., {"data": {"text": "..."}}) - recurse one level
				for _, nestedField := range knownAnswerFields {
					if nestedVal, ok := v[nestedField]; ok {
						if nestedStr, ok := nestedVal.(string); ok && nestedStr != "" {
							resp.extractedAnswer = nestedStr
							return resp
						}
					}
				}
			}
		}
	}

	// No known fields found - convert entire JSON to formatted text
	// This allows the LLM to interpret any response format
	formatted, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		// Fallback to raw response
		resp.extractedAnswer = string(body)
	} else {
		resp.extractedAnswer = string(formatted)
	}

	return resp
}

// GetAnswer returns the extracted answer text
func (r *QueryResponse) GetAnswer() string {
	return r.extractedAnswer
}

// IsSuccess returns true if the response indicates success
func (r *QueryResponse) IsSuccess() bool {
	return r.success
}

// GetError returns the error message if the response indicates failure
func (r *QueryResponse) GetError() string {
	return r.errorMessage
}

// GetRaw returns the raw response body for debugging
func (r *QueryResponse) GetRaw() string {
	return r.rawResponse
}

// NewRESTClient creates a new REST client for calling sub-agents
func NewRESTClient(cfg RESTClientConfig) *RESTClient {
	// Build endpoint from base URL and API path
	endpoint := strings.TrimSuffix(cfg.BaseURL, "/")

	// Use configured API path or default to /query
	apiPath := cfg.APIPath
	if apiPath == "" {
		apiPath = "/query"
	}
	// Ensure path starts with /
	if !strings.HasPrefix(apiPath, "/") {
		apiPath = "/" + apiPath
	}
	endpoint = endpoint + apiPath

	return &RESTClient{
		name:           cfg.Name,
		endpoint:       endpoint,
		httpClient:     &http.Client{Timeout: cfg.Timeout},
		authHeaderName: cfg.AuthHeaderName,
		authHeaderVal:  cfg.AuthHeaderVal,
		keycloakClient: cfg.KeycloakClient,
	}
}

// Query sends a question to the sub-agent and returns the response
func (c *RESTClient) Query(ctx context.Context, question string) (*QueryResponse, error) {
	log := logger.Get()

	// Build request body
	// Note: session_id is propagated via X-Session-Id header, not in body
	// (fc_logs_agent validates session_id format strictly)
	reqBody := QueryRequest{
		Query:     question,
		ChannelID: "a2a-rest", // Marker for A2A REST calls in logs
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Add auth header if configured
	if c.authHeaderName != "" && c.authHeaderVal != "" {
		req.Header.Set(c.authHeaderName, c.authHeaderVal)
	}

	// Propagate identity headers from context
	c.propagateIdentity(ctx, req)

	log.Debugw("REST client sending request",
		"agent", c.name,
		"endpoint", c.endpoint,
		"question_length", len(question),
		"has_auth", c.authHeaderName != "",
	)

	startTime := time.Now()

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	duration := time.Since(startTime)

	// Read response body with size limit to prevent memory exhaustion (10MB max)
	const maxResponseBodySize = 10 * 1024 * 1024 // 10MB
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySize))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		log.Errorw("REST client received error response",
			"agent", c.name,
			"status", resp.StatusCode,
			"body", truncateForLog(string(respBody), 500),
			"duration_ms", duration.Milliseconds(),
		)
		httpErr := fmt.Errorf("agent %s returned HTTP %d: %s", c.name, resp.StatusCode, truncateForLog(string(respBody), 200))
		// Record error in Langfuse trace
		if trace := observability.QueryTraceFromContext(ctx); trace != nil {
			trace.RecordRESTCall(c.name, question, "", duration, httpErr)
		}
		return nil, httpErr
	}

	// Parse response using format-agnostic parser
	queryResp := ParseQueryResponse(respBody)

	log.Infow("REST client received response",
		"agent", c.name,
		"success", queryResp.IsSuccess(),
		"answer_length", len(queryResp.GetAnswer()),
		"duration_ms", duration.Milliseconds(),
	)

	// Record in Langfuse trace if available
	if trace := observability.QueryTraceFromContext(ctx); trace != nil {
		trace.RecordRESTCall(c.name, question, queryResp.GetAnswer(), duration, nil)
	}

	return queryResp, nil
}

// propagateIdentity adds identity headers from context to the request
func (c *RESTClient) propagateIdentity(ctx context.Context, req *http.Request) {
	log := logger.Get()

	// Propagate user email
	if userEmail := ctxutil.UserEmail(ctx); userEmail != "" {
		req.Header.Set(HeaderUserEmail, userEmail)
		// Also set X-User-ID for Langfuse compatibility
		req.Header.Set(HeaderUserID, userEmail)
	}

	// Propagate Slack user ID
	if slackUserID := ctxutil.SlackUserID(ctx); slackUserID != "" {
		req.Header.Set(HeaderSlackUserID, slackUserID)
	}

	// Propagate caller ID
	if callerID := ctxutil.CallerID(ctx); callerID != "" && callerID != "unknown" {
		req.Header.Set(HeaderCallerID, callerID)
	}

	// NOTE: We do NOT propagate X-Session-Id to REST sub-agents because some
	// agents interpret it as an existing session to validate/resume.
	// Each sub-agent creates its own session. X-User-ID is still propagated.

	// Propagate user groups as JSON array
	if userGroups := ctxutil.UserGroups(ctx); len(userGroups) > 0 {
		if groupsJSON, err := json.Marshal(userGroups); err != nil {
			log.Warnw("Failed to marshal user groups for REST request",
				"agent", c.name,
				"error", err,
				"groups_count", len(userGroups),
			)
		} else {
			req.Header.Set(HeaderUserGroups, string(groupsJSON))
		}
	}

	// Add Keycloak JWT for identity propagation (separate from X-API-Key auth)
	// Uses X-Identity-Token to avoid conflicts with authentication headers
	if c.keycloakClient != nil && c.keycloakClient.IsEnabled() {
		userEmail := ctxutil.UserEmail(ctx)
		token, extraHeaders, err := c.keycloakClient.GetTokenWithUserClaim(ctx, userEmail)
		if err != nil {
			log.Warnw("Failed to get Keycloak token for REST request",
				"agent", c.name,
				"error", err,
			)
		} else if token != "" {
			req.Header.Set(HeaderIdentityToken, token)

			// Add extra headers from Keycloak (e.g., X-User-Email)
			for k, v := range extraHeaders {
				req.Header.Set(k, v)
			}

			log.Debugw("Keycloak identity token added to REST request",
				"agent", c.name,
				"has_user_email", userEmail != "",
				"extra_headers", len(extraHeaders),
			)
		}
	}
}

// Close releases any resources held by the client
func (c *RESTClient) Close() error {
	// HTTP client doesn't need explicit cleanup
	return nil
}

// ExtractBaseURL extracts the base URL (scheme://host:port) from a full URL
// Used to convert agent-card URLs to REST endpoints
func ExtractBaseURL(fullURL string) (string, error) {
	u, err := url.Parse(fullURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}

	return fmt.Sprintf("%s://%s", u.Scheme, u.Host), nil
}

// truncateForLog truncates a string for logging purposes
func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

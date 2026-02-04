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
)

// RESTClient is a simple HTTP client for calling sub-agents via /api/query
// It's simpler and faster than the A2A protocol for internal agents
type RESTClient struct {
	name           string          // Agent name for logging
	endpoint       string          // Full URL to /api/query endpoint
	httpClient     *http.Client    // HTTP client with configured timeout
	authHeaderName string          // Auth header name (e.g., "X-API-Key")
	authHeaderVal  string          // Auth header value
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

// QueryRequest is the request body for /api/query
type QueryRequest struct {
	Question  string `json:"question"`
	ChannelID string `json:"channel_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

// QueryResponse is the response body from /api/query
type QueryResponse struct {
	Success bool   `json:"success"`
	Answer  string `json:"answer"`
	Message string `json:"message,omitempty"`
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
	reqBody := QueryRequest{
		Question:  question,
		ChannelID: "a2a-rest", // Marker for A2A REST calls in logs
	}

	// Propagate session ID if available in context
	if sessionID := ctxutil.SessionID(ctx); sessionID != "" {
		reqBody.SessionID = sessionID
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
		return nil, fmt.Errorf("agent %s returned HTTP %d: %s", c.name, resp.StatusCode, truncateForLog(string(respBody), 200))
	}

	// Parse response
	var queryResp QueryResponse
	if err := json.Unmarshal(respBody, &queryResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	log.Infow("REST client received response",
		"agent", c.name,
		"success", queryResp.Success,
		"answer_length", len(queryResp.Answer),
		"duration_ms", duration.Milliseconds(),
	)

	return &queryResp, nil
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

	// Propagate session ID (for Langfuse trace correlation)
	if sessionID := ctxutil.SessionID(ctx); sessionID != "" {
		req.Header.Set(HeaderSessionID, sessionID)
	}

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

	// Add Keycloak JWT for identity propagation via Authorization header
	// With token exchange, the JWT contains the user's email claim
	if c.keycloakClient != nil && c.keycloakClient.IsEnabled() {
		userEmail := ctxutil.UserEmail(ctx)
		token, extraHeaders, err := c.keycloakClient.GetTokenWithUserClaim(ctx, userEmail)
		if err != nil {
			log.Warnw("Failed to get Keycloak token for REST request",
				"agent", c.name,
				"error", err,
			)
		} else if token != "" {
			req.Header.Set(HeaderAuthorization, "Bearer "+token)

			// Add extra headers from Keycloak (fallback user claim if token exchange unavailable)
			for k, v := range extraHeaders {
				req.Header.Set(k, v)
			}

			log.Debugw("Keycloak token added to REST request",
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

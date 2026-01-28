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

	"knowledge-agent/internal/config"
	"knowledge-agent/internal/logger"
)

// MaxResponseSize is the maximum allowed response body size (1MB)
const MaxResponseSize = 1 * 1024 * 1024

// QueryRequest is the request payload for calling an external agent
type QueryRequest struct {
	Question string         `json:"question"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// QueryResponse is the response from an external agent
type QueryResponse struct {
	Success bool   `json:"success"`
	Answer  string `json:"answer,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Client is an HTTP client for calling external A2A agents
type Client struct {
	agentName     string
	endpoint      string
	timeout       time.Duration
	authenticator Authenticator
	selfName      string
	httpClient    *http.Client
}

// validateA2AEndpoint validates that an A2A endpoint is safe to call (SSRF protection)
// Note: A2A is designed for internal agent communication, so internal IPs are allowed.
// We focus on blocking cloud metadata services which are the primary SSRF risk.
func validateA2AEndpoint(endpoint string) error {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Only allow http/https schemes
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("unsupported scheme '%s': only http and https are allowed", parsed.Scheme)
	}

	// Block empty hostname
	hostname := strings.ToLower(parsed.Hostname())
	if hostname == "" {
		return fmt.Errorf("endpoint must have a valid hostname")
	}

	// Block cloud metadata services (primary SSRF risk)
	// Note: We intentionally allow localhost/internal IPs since A2A is for internal communication
	metadataHosts := []string{
		"metadata.google.internal",    // GCP metadata service
		"169.254.169.254",             // AWS/Azure/GCP metadata service IP
	}
	for _, blocked := range metadataHosts {
		if hostname == blocked {
			return fmt.Errorf("access to cloud metadata service '%s' is not allowed", hostname)
		}
	}

	// Block link-local IP range (metadata service range)
	if strings.HasPrefix(hostname, "169.254.") {
		return fmt.Errorf("access to link-local addresses (169.254.x.x) is not allowed")
	}

	return nil
}

// NewClient creates a new A2A client for the given agent configuration
func NewClient(agentCfg config.A2AAgentConfig, selfName string) (*Client, error) {
	log := logger.Get()

	// SSRF Protection: Validate endpoint before creating client
	if err := validateA2AEndpoint(agentCfg.Endpoint); err != nil {
		return nil, fmt.Errorf("invalid endpoint for agent %s: %w", agentCfg.Name, err)
	}

	// Create authenticator
	auth, err := NewAuthenticator(agentCfg.Auth)
	if err != nil {
		return nil, fmt.Errorf("failed to create authenticator: %w", err)
	}

	timeout := time.Duration(agentCfg.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	log.Infow("Creating A2A client",
		"agent", agentCfg.Name,
		"endpoint", agentCfg.Endpoint,
		"auth_type", agentCfg.Auth.Type,
		"timeout", timeout,
	)

	return &Client{
		agentName:     agentCfg.Name,
		endpoint:      agentCfg.Endpoint,
		timeout:       timeout,
		authenticator: auth,
		selfName:      selfName,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

// Query sends a query to the external agent
func (c *Client) Query(ctx context.Context, question string, metadata map[string]any) (*QueryResponse, error) {
	log := logger.Get()

	// Get call context from incoming request context
	cc := GetCallContext(ctx)

	// Add self to call chain for outgoing request
	outgoingCC := cc.AddAgent(c.selfName)

	log.Debugw("Sending A2A query",
		"agent", c.agentName,
		"endpoint", c.endpoint,
		"request_id", outgoingCC.RequestID,
		"call_chain", outgoingCC.CallChain,
		"call_depth", outgoingCC.CallDepth,
	)

	// Prepare request body
	reqBody := QueryRequest{
		Question: question,
		Metadata: metadata,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint+"/api/query", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "KnowledgeAgent/1.0 (A2A Client)")

	// Set A2A loop prevention headers
	outgoingCC.SetHeaders(req)

	// Apply authentication
	if err := c.authenticator.Authenticate(req); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	// Send request
	startTime := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	duration := time.Since(startTime)

	log.Debugw("A2A response received",
		"agent", c.agentName,
		"status", resp.StatusCode,
		"duration_ms", duration.Milliseconds(),
		"request_id", outgoingCC.RequestID,
	)

	// Check for loop detection response
	if resp.StatusCode == StatusLoopDetected {
		return nil, fmt.Errorf("loop detected by remote agent %s", c.agentName)
	}

	// Check for other errors
	if resp.StatusCode != http.StatusOK {
		// Read error body for debugging but don't expose in error message (security)
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		log.Debugw("Remote agent error response",
			"agent", c.agentName,
			"status", resp.StatusCode,
			"body", string(errBody),
			"request_id", outgoingCC.RequestID,
		)
		// Return sanitized error without potentially sensitive response body
		return nil, fmt.Errorf("agent %s returned HTTP %d", c.agentName, resp.StatusCode)
	}

	// Read response body with size limit
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseSize))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse response
	var queryResp QueryResponse
	if err := json.Unmarshal(body, &queryResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if !queryResp.Success {
		return nil, fmt.Errorf("agent returned error: %s", queryResp.Error)
	}

	log.Infow("A2A query completed",
		"agent", c.agentName,
		"request_id", outgoingCC.RequestID,
		"duration_ms", duration.Milliseconds(),
		"answer_length", len(queryResp.Answer),
	)

	return &queryResp, nil
}

package keycloak

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"knowledge-agent/internal/logger"
)

// Config holds Keycloak client configuration
type Config struct {
	Enabled         bool   `yaml:"enabled" mapstructure:"enabled" default:"false"`
	ServerURL       string `yaml:"server_url" mapstructure:"server_url" envconfig:"KEYCLOAK_SERVER_URL"`          // e.g., https://keycloak.example.com
	Realm           string `yaml:"realm" mapstructure:"realm" envconfig:"KEYCLOAK_REALM"`                         // e.g., my-realm
	ClientID        string `yaml:"client_id" mapstructure:"client_id" envconfig:"KEYCLOAK_CLIENT_ID"`             // Service account client ID
	ClientSecret    string `yaml:"client_secret" mapstructure:"client_secret" envconfig:"KEYCLOAK_CLIENT_SECRET"` // Service account client secret
	UserClaimName   string `yaml:"user_claim_name" mapstructure:"user_claim_name" default:"X-User-Email"`         // Header name for user email propagation
	GroupsClaimPath string `yaml:"groups_claim_path" mapstructure:"groups_claim_path" default:"groups"`           // JWT claim path for groups extraction
}

// cachedToken stores a token with its expiration time
type cachedToken struct {
	accessToken string
	expiresAt   time.Time
}

// Client handles Keycloak authentication using Client Credentials flow
type Client struct {
	config     Config
	httpClient *http.Client
	tokenCache *cachedToken
	cacheMu    sync.RWMutex
}

// tokenResponse represents the OAuth2 token response from Keycloak
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"` // Seconds until expiration
	TokenType   string `json:"token_type"`
}

// NewClient creates a new Keycloak client
func NewClient(cfg Config) (*Client, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	if cfg.ServerURL == "" {
		return nil, fmt.Errorf("keycloak server_url is required when enabled")
	}
	// Validate ServerURL is a valid URL
	if _, err := url.Parse(cfg.ServerURL); err != nil {
		return nil, fmt.Errorf("keycloak server_url is not a valid URL: %w", err)
	}
	if cfg.Realm == "" {
		return nil, fmt.Errorf("keycloak realm is required when enabled")
	}
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("keycloak client_id is required when enabled")
	}
	if cfg.ClientSecret == "" {
		return nil, fmt.Errorf("keycloak client_secret is required when enabled")
	}

	// Set default user claim name if not specified
	if cfg.UserClaimName == "" {
		cfg.UserClaimName = "X-User-Email"
	}

	log := logger.Get()
	log.Infow("Keycloak client initialized",
		"server_url", cfg.ServerURL,
		"realm", cfg.Realm,
		"client_id", cfg.ClientID,
		"user_claim_name", cfg.UserClaimName,
	)

	return &Client{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// IsEnabled returns whether Keycloak integration is enabled
func (c *Client) IsEnabled() bool {
	return c != nil && c.config.Enabled
}

// GetUserClaimName returns the header name for user email propagation
func (c *Client) GetUserClaimName() string {
	if c == nil {
		return "X-User-Email"
	}
	return c.config.UserClaimName
}

// GetGroupsClaimPath returns the JWT claim path for groups extraction
func (c *Client) GetGroupsClaimPath() string {
	if c == nil {
		return "groups"
	}
	if c.config.GroupsClaimPath == "" {
		return "groups"
	}
	return c.config.GroupsClaimPath
}

// GetServiceToken retrieves the service token using Client Credentials flow
// Uses caching to avoid unnecessary token requests
func (c *Client) GetServiceToken(ctx context.Context) (string, error) {
	if c == nil || !c.config.Enabled {
		return "", nil
	}

	// Check cache first
	c.cacheMu.RLock()
	if c.tokenCache != nil && time.Now().Before(c.tokenCache.expiresAt) {
		token := c.tokenCache.accessToken
		c.cacheMu.RUnlock()
		return token, nil
	}
	c.cacheMu.RUnlock()

	// Cache miss or expired - fetch new token
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	// Double-check after acquiring write lock (another goroutine might have refreshed)
	if c.tokenCache != nil && time.Now().Before(c.tokenCache.expiresAt) {
		return c.tokenCache.accessToken, nil
	}

	log := logger.Get()
	log.Debug("Fetching new Keycloak service token")

	// Build token endpoint URL
	tokenURL := fmt.Sprintf("%s/realms/%s/protocol/openid-connect/token",
		strings.TrimSuffix(c.config.ServerURL, "/"),
		c.config.Realm,
	)

	// Build request body for Client Credentials flow
	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", c.config.ClientID)
	data.Set("client_secret", c.config.ClientSecret)

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to request token: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read token response: %w", err)
	}

	// Check for error response
	if resp.StatusCode != http.StatusOK {
		log.Errorw("Keycloak token request failed",
			"status_code", resp.StatusCode,
			"response", string(body),
		)
		return "", fmt.Errorf("token request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var tokenResp tokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("failed to parse token response: %w", err)
	}

	// Cache the token (with 30s safety margin before actual expiration)
	// Ensure minimum cache duration of 10 seconds to avoid constant refreshes
	expirySeconds := tokenResp.ExpiresIn - 30
	if expirySeconds < 10 {
		expirySeconds = 10
	}
	expiresAt := time.Now().Add(time.Duration(expirySeconds) * time.Second)
	c.tokenCache = &cachedToken{
		accessToken: tokenResp.AccessToken,
		expiresAt:   expiresAt,
	}

	log.Debugw("Keycloak service token cached",
		"expires_in", tokenResp.ExpiresIn,
		"expires_at", expiresAt.Format(time.RFC3339),
	)

	return tokenResp.AccessToken, nil
}

// GetTokenWithUserClaim retrieves the service token and returns headers for user identity propagation
// The user identity is passed as a separate header (configured via user_claim_name)
// This approach uses the service's JWT for authentication while propagating user identity separately
func (c *Client) GetTokenWithUserClaim(ctx context.Context, userEmail string) (token string, extraHeaders map[string]string, err error) {
	if c == nil || !c.config.Enabled {
		return "", nil, nil
	}

	// Get the service token
	token, err = c.GetServiceToken(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("failed to get service token: %w", err)
	}

	// Prepare extra headers for user identity propagation
	extraHeaders = make(map[string]string)
	if userEmail != "" {
		extraHeaders[c.config.UserClaimName] = userEmail
	}

	return token, extraHeaders, nil
}

// Close performs cleanup (currently no-op, but included for interface consistency)
func (c *Client) Close() error {
	return nil
}

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

// userTokenCache stores tokens per user email
type userTokenCache struct {
	tokens map[string]*cachedToken
	mu     sync.RWMutex
}

// Client handles Keycloak authentication using Client Credentials flow
type Client struct {
	config         Config
	httpClient     *http.Client
	tokenCache     *cachedToken
	cacheMu        sync.RWMutex
	userTokenCache *userTokenCache          // Cache for user impersonation tokens
	tokenExchange  bool                     // Whether token exchange is available (detected at runtime)
	tokenExchangeMu sync.RWMutex            // Protects tokenExchange flag
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
		userTokenCache: &userTokenCache{
			tokens: make(map[string]*cachedToken),
		},
		tokenExchange: true, // Assume available until proven otherwise
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

// GetUserToken retrieves a token that represents the specified user using Token Exchange (impersonation)
// This requires the token-exchange feature to be enabled in Keycloak and proper permissions configured
// Returns empty string if token exchange is not available (falls back gracefully)
func (c *Client) GetUserToken(ctx context.Context, userEmail string) (string, error) {
	if c == nil || !c.config.Enabled {
		return "", nil
	}

	if userEmail == "" {
		return "", nil
	}

	// Check if token exchange is known to be unavailable
	c.tokenExchangeMu.RLock()
	exchangeAvailable := c.tokenExchange
	c.tokenExchangeMu.RUnlock()

	if !exchangeAvailable {
		return "", nil // Token exchange not available, caller should use service token
	}

	// Check user token cache
	c.userTokenCache.mu.RLock()
	if cached, ok := c.userTokenCache.tokens[userEmail]; ok && time.Now().Before(cached.expiresAt) {
		token := cached.accessToken
		c.userTokenCache.mu.RUnlock()
		return token, nil
	}
	c.userTokenCache.mu.RUnlock()

	log := logger.Get()
	log.Debugw("Fetching user token via token exchange", "user_email", userEmail)

	// Build token endpoint URL
	tokenURL := fmt.Sprintf("%s/realms/%s/protocol/openid-connect/token",
		strings.TrimSuffix(c.config.ServerURL, "/"),
		c.config.Realm,
	)

	// Build request body for Token Exchange (Direct Naked Impersonation)
	data := url.Values{}
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:token-exchange")
	data.Set("client_id", c.config.ClientID)
	data.Set("client_secret", c.config.ClientSecret)
	data.Set("requested_subject", userEmail) // User to impersonate

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create token exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to execute token exchange request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read token exchange response: %w", err)
	}

	// Check for error response
	if resp.StatusCode != http.StatusOK {
		// Check if token exchange is not supported (feature not enabled)
		bodyStr := string(body)
		if resp.StatusCode == http.StatusBadRequest &&
			(strings.Contains(bodyStr, "invalid_grant") ||
				strings.Contains(bodyStr, "not enabled") ||
				strings.Contains(bodyStr, "unsupported_grant_type")) {
			log.Warnw("Token exchange not available in Keycloak, disabling for future requests",
				"status_code", resp.StatusCode,
				"response", bodyStr,
			)
			// Mark token exchange as unavailable
			c.tokenExchangeMu.Lock()
			c.tokenExchange = false
			c.tokenExchangeMu.Unlock()
			return "", nil // Graceful fallback
		}

		// Check for permission/user not found errors
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
			log.Warnw("Token exchange permission denied or user not found",
				"user_email", userEmail,
				"status_code", resp.StatusCode,
				"response", bodyStr,
			)
			return "", nil // Graceful fallback for this user
		}

		log.Errorw("Token exchange request failed",
			"user_email", userEmail,
			"status_code", resp.StatusCode,
			"response", bodyStr,
		)
		return "", fmt.Errorf("token exchange failed with status %d", resp.StatusCode)
	}

	// Parse response
	var tokenResp tokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("failed to parse token exchange response: %w", err)
	}

	// Cache the user token (with 30s safety margin)
	expirySeconds := tokenResp.ExpiresIn - 30
	if expirySeconds < 10 {
		expirySeconds = 10
	}
	expiresAt := time.Now().Add(time.Duration(expirySeconds) * time.Second)

	c.userTokenCache.mu.Lock()
	c.userTokenCache.tokens[userEmail] = &cachedToken{
		accessToken: tokenResp.AccessToken,
		expiresAt:   expiresAt,
	}
	c.userTokenCache.mu.Unlock()

	log.Infow("User token obtained via token exchange",
		"user_email", userEmail,
		"expires_in", tokenResp.ExpiresIn,
	)

	return tokenResp.AccessToken, nil
}

// GetTokenWithUserClaim retrieves a token for the specified user
// It first attempts Token Exchange (impersonation) to get a token that represents the user
// If Token Exchange is not available, it falls back to service token + extra headers
func (c *Client) GetTokenWithUserClaim(ctx context.Context, userEmail string) (token string, extraHeaders map[string]string, err error) {
	if c == nil || !c.config.Enabled {
		return "", nil, nil
	}

	log := logger.Get()
	extraHeaders = make(map[string]string)

	// Try Token Exchange first (if userEmail is provided)
	if userEmail != "" {
		userToken, err := c.GetUserToken(ctx, userEmail)
		if err != nil {
			log.Warnw("Token exchange failed, falling back to service token",
				"user_email", userEmail,
				"error", err,
			)
		} else if userToken != "" {
			// Success! User token has email claim embedded
			log.Debugw("Using user token from token exchange",
				"user_email", userEmail,
			)
			return userToken, extraHeaders, nil
		}
		// Token exchange not available, fall through to service token
	}

	// Fallback: Get service token + propagate user identity via header
	token, err = c.GetServiceToken(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("failed to get service token: %w", err)
	}

	// Add user email as extra header (for sub-agents that don't support token exchange)
	if userEmail != "" {
		extraHeaders[c.config.UserClaimName] = userEmail
		log.Debugw("Using service token with user header fallback",
			"user_email", userEmail,
			"header", c.config.UserClaimName,
		)
	}

	return token, extraHeaders, nil
}

// keycloakUser represents a user from the Keycloak Admin API
type keycloakUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

// keycloakGroup represents a group from the Keycloak Admin API
type keycloakGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

// GetUserGroups retrieves the groups for a user by their email address
// Uses the Keycloak Admin API to lookup the user and their groups
// Requires the service account to have 'view-users' role in realm-management
func (c *Client) GetUserGroups(ctx context.Context, email string) ([]string, error) {
	if c == nil || !c.config.Enabled {
		return nil, nil
	}

	if email == "" {
		return nil, nil
	}

	log := logger.Get()

	// Get service token for Admin API access
	token, err := c.GetServiceToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get service token: %w", err)
	}

	// Step 1: Find user by email
	userURL := fmt.Sprintf("%s/admin/realms/%s/users?email=%s&exact=true",
		strings.TrimSuffix(c.config.ServerURL, "/"),
		c.config.Realm,
		url.QueryEscape(email),
	)

	req, err := http.NewRequestWithContext(ctx, "GET", userURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create user lookup request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup user: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Warnw("Keycloak user lookup failed",
			"email", email,
			"status_code", resp.StatusCode,
			"response", string(body),
		)
		return nil, fmt.Errorf("user lookup failed with status %d", resp.StatusCode)
	}

	var users []keycloakUser
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		return nil, fmt.Errorf("failed to parse user lookup response: %w", err)
	}

	if len(users) == 0 {
		log.Debugw("User not found in Keycloak", "email", email)
		return nil, nil
	}

	userID := users[0].ID

	// Step 2: Get user's groups
	groupsURL := fmt.Sprintf("%s/admin/realms/%s/users/%s/groups",
		strings.TrimSuffix(c.config.ServerURL, "/"),
		c.config.Realm,
		userID,
	)

	req, err = http.NewRequestWithContext(ctx, "GET", groupsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create groups request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err = c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get user groups: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Warnw("Keycloak groups lookup failed",
			"email", email,
			"user_id", userID,
			"status_code", resp.StatusCode,
			"response", string(body),
		)
		return nil, fmt.Errorf("groups lookup failed with status %d", resp.StatusCode)
	}

	var groups []keycloakGroup
	if err := json.NewDecoder(resp.Body).Decode(&groups); err != nil {
		return nil, fmt.Errorf("failed to parse groups response: %w", err)
	}

	// Extract group paths (e.g., "/google-workspace/dev@freepik.com")
	groupPaths := make([]string, len(groups))
	for i, g := range groups {
		groupPaths[i] = g.Path
	}

	log.Debugw("Retrieved user groups from Keycloak",
		"email", email,
		"groups_count", len(groupPaths),
	)

	return groupPaths, nil
}

// Close performs cleanup (currently no-op, but included for interface consistency)
func (c *Client) Close() error {
	return nil
}

package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"knowledge-agent/internal/config"
	"knowledge-agent/internal/logger"
)

// Authenticator is the interface for A2A authentication methods
type Authenticator interface {
	// Authenticate adds authentication to the request
	Authenticate(req *http.Request) error
}

// NewAuthenticator creates an authenticator based on the auth configuration
func NewAuthenticator(cfg config.A2AAuthConfig) (Authenticator, error) {
	switch cfg.Type {
	case "api_key":
		return NewAPIKeyAuth(cfg)
	case "bearer":
		return NewBearerAuth(cfg)
	case "oauth2":
		return NewOAuth2Auth(cfg)
	case "none", "":
		return &NoAuth{}, nil
	default:
		return nil, fmt.Errorf("unsupported auth type: %s", cfg.Type)
	}
}

// NoAuth is an authenticator that does nothing
type NoAuth struct{}

// Authenticate implements Authenticator for NoAuth
func (a *NoAuth) Authenticate(req *http.Request) error {
	return nil
}

// APIKeyAuth implements API key authentication with a configurable header
type APIKeyAuth struct {
	header string
	key    string
}

// NewAPIKeyAuth creates a new API key authenticator
func NewAPIKeyAuth(cfg config.A2AAuthConfig) (*APIKeyAuth, error) {
	key := os.Getenv(cfg.KeyEnv)
	if key == "" {
		return nil, fmt.Errorf("API key environment variable %s is not set", cfg.KeyEnv)
	}

	header := cfg.Header
	if header == "" {
		header = "X-API-Key" // Default header
	}

	return &APIKeyAuth{
		header: header,
		key:    key,
	}, nil
}

// Authenticate implements Authenticator for APIKeyAuth
func (a *APIKeyAuth) Authenticate(req *http.Request) error {
	req.Header.Set(a.header, a.key)
	return nil
}

// BearerAuth implements Bearer token authentication
type BearerAuth struct {
	token string
}

// NewBearerAuth creates a new Bearer token authenticator
func NewBearerAuth(cfg config.A2AAuthConfig) (*BearerAuth, error) {
	token := os.Getenv(cfg.TokenEnv)
	if token == "" {
		return nil, fmt.Errorf("bearer token environment variable %s is not set", cfg.TokenEnv)
	}

	return &BearerAuth{
		token: token,
	}, nil
}

// Authenticate implements Authenticator for BearerAuth
func (a *BearerAuth) Authenticate(req *http.Request) error {
	req.Header.Set("Authorization", "Bearer "+a.token)
	return nil
}

// OAuth2Auth implements OAuth2 client credentials flow with token caching
type OAuth2Auth struct {
	tokenURL     string
	clientID     string
	clientSecret string
	scopes       []string

	// Token cache
	mu           sync.RWMutex
	accessToken  string
	tokenExpiry  time.Time
	httpClient   *http.Client
}

// NewOAuth2Auth creates a new OAuth2 authenticator with client credentials flow
func NewOAuth2Auth(cfg config.A2AAuthConfig) (*OAuth2Auth, error) {
	// Security: Require HTTPS for token URL to protect credentials
	if !strings.HasPrefix(strings.ToLower(cfg.TokenURL), "https://") {
		return nil, fmt.Errorf("OAuth2 token_url must use HTTPS to protect credentials")
	}

	clientID := os.Getenv(cfg.ClientIDEnv)
	if clientID == "" {
		return nil, fmt.Errorf("OAuth2 client ID environment variable %s is not set", cfg.ClientIDEnv)
	}

	clientSecret := os.Getenv(cfg.ClientSecretEnv)
	if clientSecret == "" {
		return nil, fmt.Errorf("OAuth2 client secret environment variable %s is not set", cfg.ClientSecretEnv)
	}

	return &OAuth2Auth{
		tokenURL:     cfg.TokenURL,
		clientID:     clientID,
		clientSecret: clientSecret,
		scopes:       cfg.Scopes,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// Authenticate implements Authenticator for OAuth2Auth
func (a *OAuth2Auth) Authenticate(req *http.Request) error {
	token, err := a.getAccessToken(req.Context())
	if err != nil {
		return fmt.Errorf("failed to get OAuth2 access token: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	return nil
}

// getAccessToken returns a valid access token, refreshing if necessary
func (a *OAuth2Auth) getAccessToken(ctx context.Context) (string, error) {
	log := logger.Get()

	// Check if we have a valid cached token
	a.mu.RLock()
	if a.accessToken != "" && time.Now().Before(a.tokenExpiry.Add(-30*time.Second)) {
		token := a.accessToken
		a.mu.RUnlock()
		return token, nil
	}
	a.mu.RUnlock()

	// Need to refresh the token
	a.mu.Lock()
	defer a.mu.Unlock()

	// Double-check after acquiring write lock
	if a.accessToken != "" && time.Now().Before(a.tokenExpiry.Add(-30*time.Second)) {
		return a.accessToken, nil
	}

	log.Debugw("Refreshing OAuth2 access token", "token_url", a.tokenURL)

	// Prepare token request
	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", a.clientID)
	data.Set("client_secret", a.clientSecret)
	if len(a.scopes) > 0 {
		data.Set("scope", strings.Join(a.scopes, " "))
	}

	req, err := http.NewRequestWithContext(ctx, "POST", a.tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token request returned status %d", resp.StatusCode)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to decode token response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("no access token in response")
	}

	// Cache the token
	a.accessToken = tokenResp.AccessToken
	if tokenResp.ExpiresIn > 0 {
		a.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	} else {
		// Default to 1 hour if not specified
		a.tokenExpiry = time.Now().Add(1 * time.Hour)
	}

	log.Debugw("OAuth2 access token refreshed",
		"token_url", a.tokenURL,
		"expires_in", tokenResp.ExpiresIn,
	)

	return a.accessToken, nil
}

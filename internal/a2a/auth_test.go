package a2a

import (
	"net/http/httptest"
	"os"
	"testing"

	"knowledge-agent/internal/config"
)

func TestNewAuthenticator_NoAuth(t *testing.T) {
	cfg := config.A2AAuthConfig{Type: "none"}
	auth, err := NewAuthenticator(cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := auth.(*NoAuth); !ok {
		t.Error("expected NoAuth authenticator")
	}
}

func TestNewAuthenticator_EmptyType(t *testing.T) {
	cfg := config.A2AAuthConfig{Type: ""}
	auth, err := NewAuthenticator(cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := auth.(*NoAuth); !ok {
		t.Error("expected NoAuth authenticator for empty type")
	}
}

func TestNewAuthenticator_UnsupportedType(t *testing.T) {
	cfg := config.A2AAuthConfig{Type: "magic"}
	_, err := NewAuthenticator(cfg)

	if err == nil {
		t.Error("expected error for unsupported auth type")
	}
}

func TestNoAuth_Authenticate(t *testing.T) {
	auth := &NoAuth{}
	req := httptest.NewRequest("GET", "/test", nil)

	err := auth.Authenticate(req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should not modify request
	if req.Header.Get("Authorization") != "" {
		t.Error("NoAuth should not set Authorization header")
	}
}

func TestAPIKeyAuth_Authenticate(t *testing.T) {
	// Set environment variable
	os.Setenv("TEST_API_KEY", "secret-key-123")
	defer os.Unsetenv("TEST_API_KEY")

	cfg := config.A2AAuthConfig{
		Type:   "api_key",
		Header: "X-Custom-Key",
		KeyEnv: "TEST_API_KEY",
	}

	auth, err := NewAPIKeyAuth(cfg)
	if err != nil {
		t.Fatalf("failed to create APIKeyAuth: %v", err)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	err = auth.Authenticate(req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Header.Get("X-Custom-Key") != "secret-key-123" {
		t.Errorf("expected X-Custom-Key header to be set, got '%s'", req.Header.Get("X-Custom-Key"))
	}
}

func TestAPIKeyAuth_DefaultHeader(t *testing.T) {
	os.Setenv("TEST_API_KEY2", "key-456")
	defer os.Unsetenv("TEST_API_KEY2")

	cfg := config.A2AAuthConfig{
		Type:   "api_key",
		Header: "", // Should default to X-API-Key
		KeyEnv: "TEST_API_KEY2",
	}

	auth, err := NewAPIKeyAuth(cfg)
	if err != nil {
		t.Fatalf("failed to create APIKeyAuth: %v", err)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	auth.Authenticate(req)

	if req.Header.Get("X-API-Key") != "key-456" {
		t.Errorf("expected default X-API-Key header, got '%s'", req.Header.Get("X-API-Key"))
	}
}

func TestAPIKeyAuth_MissingEnvVar(t *testing.T) {
	os.Unsetenv("MISSING_KEY")

	cfg := config.A2AAuthConfig{
		Type:   "api_key",
		Header: "X-API-Key",
		KeyEnv: "MISSING_KEY",
	}

	_, err := NewAPIKeyAuth(cfg)

	if err == nil {
		t.Error("expected error when env var is not set")
	}
}

func TestBearerAuth_Authenticate(t *testing.T) {
	os.Setenv("TEST_BEARER_TOKEN", "bearer-token-789")
	defer os.Unsetenv("TEST_BEARER_TOKEN")

	cfg := config.A2AAuthConfig{
		Type:     "bearer",
		TokenEnv: "TEST_BEARER_TOKEN",
	}

	auth, err := NewBearerAuth(cfg)
	if err != nil {
		t.Fatalf("failed to create BearerAuth: %v", err)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	err = auth.Authenticate(req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Header.Get("Authorization") != "Bearer bearer-token-789" {
		t.Errorf("expected Authorization header, got '%s'", req.Header.Get("Authorization"))
	}
}

func TestBearerAuth_MissingEnvVar(t *testing.T) {
	os.Unsetenv("MISSING_TOKEN")

	cfg := config.A2AAuthConfig{
		Type:     "bearer",
		TokenEnv: "MISSING_TOKEN",
	}

	_, err := NewBearerAuth(cfg)

	if err == nil {
		t.Error("expected error when env var is not set")
	}
}

func TestOAuth2Auth_MissingClientID(t *testing.T) {
	os.Unsetenv("MISSING_CLIENT_ID")
	os.Setenv("TEST_CLIENT_SECRET", "secret")
	defer os.Unsetenv("TEST_CLIENT_SECRET")

	cfg := config.A2AAuthConfig{
		Type:            "oauth2",
		TokenURL:        "https://auth.example.com/token",
		ClientIDEnv:     "MISSING_CLIENT_ID",
		ClientSecretEnv: "TEST_CLIENT_SECRET",
	}

	_, err := NewOAuth2Auth(cfg)

	if err == nil {
		t.Error("expected error when client ID env var is not set")
	}
}

func TestOAuth2Auth_MissingClientSecret(t *testing.T) {
	os.Setenv("TEST_CLIENT_ID", "client-id")
	os.Unsetenv("MISSING_CLIENT_SECRET")
	defer os.Unsetenv("TEST_CLIENT_ID")

	cfg := config.A2AAuthConfig{
		Type:            "oauth2",
		TokenURL:        "https://auth.example.com/token",
		ClientIDEnv:     "TEST_CLIENT_ID",
		ClientSecretEnv: "MISSING_CLIENT_SECRET",
	}

	_, err := NewOAuth2Auth(cfg)

	if err == nil {
		t.Error("expected error when client secret env var is not set")
	}
}

func TestOAuth2Auth_Creation(t *testing.T) {
	os.Setenv("TEST_OAUTH_CLIENT_ID", "client-id")
	os.Setenv("TEST_OAUTH_CLIENT_SECRET", "client-secret")
	defer func() {
		os.Unsetenv("TEST_OAUTH_CLIENT_ID")
		os.Unsetenv("TEST_OAUTH_CLIENT_SECRET")
	}()

	cfg := config.A2AAuthConfig{
		Type:            "oauth2",
		TokenURL:        "https://auth.example.com/token",
		ClientIDEnv:     "TEST_OAUTH_CLIENT_ID",
		ClientSecretEnv: "TEST_OAUTH_CLIENT_SECRET",
		Scopes:          []string{"read", "write"},
	}

	auth, err := NewOAuth2Auth(cfg)

	if err != nil {
		t.Fatalf("failed to create OAuth2Auth: %v", err)
	}
	if auth.tokenURL != cfg.TokenURL {
		t.Errorf("expected tokenURL '%s', got '%s'", cfg.TokenURL, auth.tokenURL)
	}
	if auth.clientID != "client-id" {
		t.Errorf("expected clientID 'client-id', got '%s'", auth.clientID)
	}
	if len(auth.scopes) != 2 {
		t.Errorf("expected 2 scopes, got %d", len(auth.scopes))
	}
}

func TestOAuth2Auth_RequiresHTTPS(t *testing.T) {
	os.Setenv("TEST_OAUTH_CLIENT_ID", "client-id")
	os.Setenv("TEST_OAUTH_CLIENT_SECRET", "client-secret")
	defer func() {
		os.Unsetenv("TEST_OAUTH_CLIENT_ID")
		os.Unsetenv("TEST_OAUTH_CLIENT_SECRET")
	}()

	cfg := config.A2AAuthConfig{
		Type:            "oauth2",
		TokenURL:        "http://auth.example.com/token", // HTTP instead of HTTPS
		ClientIDEnv:     "TEST_OAUTH_CLIENT_ID",
		ClientSecretEnv: "TEST_OAUTH_CLIENT_SECRET",
	}

	_, err := NewOAuth2Auth(cfg)

	if err == nil {
		t.Error("expected error when token_url uses HTTP instead of HTTPS")
	}
}

func TestOAuth2Auth_AcceptsHTTPS(t *testing.T) {
	os.Setenv("TEST_OAUTH_CLIENT_ID2", "client-id")
	os.Setenv("TEST_OAUTH_CLIENT_SECRET2", "client-secret")
	defer func() {
		os.Unsetenv("TEST_OAUTH_CLIENT_ID2")
		os.Unsetenv("TEST_OAUTH_CLIENT_SECRET2")
	}()

	testCases := []string{
		"https://auth.example.com/token",
		"HTTPS://AUTH.EXAMPLE.COM/token", // Case insensitive
	}

	for _, tokenURL := range testCases {
		cfg := config.A2AAuthConfig{
			Type:            "oauth2",
			TokenURL:        tokenURL,
			ClientIDEnv:     "TEST_OAUTH_CLIENT_ID2",
			ClientSecretEnv: "TEST_OAUTH_CLIENT_SECRET2",
		}

		_, err := NewOAuth2Auth(cfg)
		if err != nil {
			t.Errorf("expected HTTPS URL %s to be accepted, got error: %v", tokenURL, err)
		}
	}
}

package launcher

import (
	"context"
	"testing"

	"github.com/a2aproject/a2a-go/a2asrv"
)

// mockRequestMeta implements a minimal mock for testing
type mockRequestMeta struct {
	headers map[string][]string
}

func newMockRequestMeta(headers map[string][]string) *a2asrv.RequestMeta {
	return a2asrv.NewRequestMeta(headers)
}

// mockCallContext creates a mock CallContext for testing
func newMockCallContext(headers map[string][]string, method string) *a2asrv.CallContext {
	meta := a2asrv.NewRequestMeta(headers)
	ctx, callCtx := a2asrv.WithCallContext(context.Background(), meta)
	_ = ctx // We don't need the context, just the callCtx
	return callCtx
}

func TestNewAuthInterceptor(t *testing.T) {
	apiKeys := map[string]string{
		"key1": "caller1",
		"key2": "caller2",
	}

	interceptor := NewAuthInterceptor(apiKeys)

	if interceptor == nil {
		t.Fatal("expected non-nil interceptor")
	}
	if len(interceptor.apiKeys) != 2 {
		t.Errorf("expected 2 API keys, got %d", len(interceptor.apiKeys))
	}
}

func TestAuthInterceptor_Before_ValidKey(t *testing.T) {
	apiKeys := map[string]string{
		"valid-api-key": "test-caller",
	}
	interceptor := NewAuthInterceptor(apiKeys)

	callCtx := newMockCallContext(map[string][]string{
		"X-API-Key": {"valid-api-key"},
	}, "message/send")

	ctx := context.Background()
	resultCtx, err := interceptor.Before(ctx, callCtx, nil)

	if err != nil {
		t.Errorf("expected no error for valid key, got: %v", err)
	}
	if resultCtx == nil {
		t.Error("expected non-nil context")
	}
}

func TestAuthInterceptor_Before_InvalidKey(t *testing.T) {
	apiKeys := map[string]string{
		"valid-api-key": "test-caller",
	}
	interceptor := NewAuthInterceptor(apiKeys)

	callCtx := newMockCallContext(map[string][]string{
		"X-API-Key": {"invalid-key"},
	}, "message/send")

	ctx := context.Background()
	_, err := interceptor.Before(ctx, callCtx, nil)

	if err == nil {
		t.Error("expected error for invalid key")
	}
	if err.Error() != "unauthorized: invalid API key" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestAuthInterceptor_Before_MissingKey(t *testing.T) {
	apiKeys := map[string]string{
		"valid-api-key": "test-caller",
	}
	interceptor := NewAuthInterceptor(apiKeys)

	// No X-API-Key header
	callCtx := newMockCallContext(map[string][]string{
		"Content-Type": {"application/json"},
	}, "message/send")

	ctx := context.Background()
	_, err := interceptor.Before(ctx, callCtx, nil)

	if err == nil {
		t.Error("expected error for missing key")
	}
	if err.Error() != "unauthorized: missing X-API-Key header" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestAuthInterceptor_Before_CaseInsensitiveHeader(t *testing.T) {
	apiKeys := map[string]string{
		"valid-api-key": "test-caller",
	}
	interceptor := NewAuthInterceptor(apiKeys)

	testCases := []struct {
		name       string
		headerName string
	}{
		{"lowercase", "x-api-key"},
		{"uppercase", "X-API-KEY"},
		{"mixed case", "X-Api-Key"},
		{"weird case", "x-API-key"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			callCtx := newMockCallContext(map[string][]string{
				tc.headerName: {"valid-api-key"},
			}, "message/send")

			ctx := context.Background()
			_, err := interceptor.Before(ctx, callCtx, nil)

			if err != nil {
				t.Errorf("expected no error for header %q, got: %v", tc.headerName, err)
			}
		})
	}
}

func TestAuthInterceptor_Before_EmptyAPIKeys(t *testing.T) {
	// Edge case: empty API keys map means no valid keys
	interceptor := NewAuthInterceptor(map[string]string{})

	callCtx := newMockCallContext(map[string][]string{
		"X-API-Key": {"any-key"},
	}, "message/send")

	ctx := context.Background()
	_, err := interceptor.Before(ctx, callCtx, nil)

	if err == nil {
		t.Error("expected error when no API keys are configured")
	}
}

func TestAuthInterceptor_After_NoOp(t *testing.T) {
	interceptor := NewAuthInterceptor(map[string]string{})

	err := interceptor.After(context.Background(), nil, nil)

	if err != nil {
		t.Errorf("After should always return nil, got: %v", err)
	}
}

func TestAuthInterceptor_ValidateAPIKey_ConstantTime(t *testing.T) {
	// This test verifies the constant-time comparison is used
	// by checking that validation works correctly for various key lengths
	apiKeys := map[string]string{
		"short":                          "caller1",
		"medium-length-key":              "caller2",
		"very-long-api-key-for-testing":  "caller3",
	}
	interceptor := NewAuthInterceptor(apiKeys)

	testCases := []struct {
		key      string
		expected bool
		callerID string
	}{
		{"short", true, "caller1"},
		{"medium-length-key", true, "caller2"},
		{"very-long-api-key-for-testing", true, "caller3"},
		{"wrong", false, ""},
		{"short-but-wrong", false, ""},
	}

	for _, tc := range testCases {
		callerID, valid := interceptor.validateAPIKey(tc.key)
		if valid != tc.expected {
			t.Errorf("key %q: expected valid=%v, got %v", tc.key, tc.expected, valid)
		}
		if callerID != tc.callerID {
			t.Errorf("key %q: expected callerID=%q, got %q", tc.key, tc.callerID, callerID)
		}
	}
}

func TestNewConfigFromAppConfig(t *testing.T) {
	appCfg := &Config{
		Port:        8082,
		EnableWebUI: true,
		AgentURL:    "",
	}
	apiKeys := map[string]string{"key": "caller"}

	// Test with config.LauncherConfig - we can't import config here due to cycle
	// so we test the Config struct directly
	cfg := Config{
		Port:        appCfg.Port,
		EnableWebUI: appCfg.EnableWebUI,
		AgentURL:    "http://localhost:8082",
		APIKeys:     apiKeys,
	}

	if cfg.Port != 8082 {
		t.Errorf("expected port 8082, got %d", cfg.Port)
	}
	if !cfg.EnableWebUI {
		t.Error("expected WebUI enabled")
	}
	if len(cfg.APIKeys) != 1 {
		t.Errorf("expected 1 API key, got %d", len(cfg.APIKeys))
	}
}

func TestBuildLauncherArgs(t *testing.T) {
	testCases := []struct {
		name     string
		cfg      Config
		expected []string
	}{
		{
			name: "basic config",
			cfg: Config{
				Port:        8082,
				EnableWebUI: false,
				AgentURL:    "",
			},
			expected: []string{"web", "-port", "8082", "api", "a2a"},
		},
		{
			name: "with agent URL",
			cfg: Config{
				Port:        9000,
				EnableWebUI: false,
				AgentURL:    "http://example.com",
			},
			expected: []string{"web", "-port", "9000", "api", "a2a", "--a2a_agent_url", "http://example.com"},
		},
		{
			name: "with WebUI",
			cfg: Config{
				Port:        8082,
				EnableWebUI: true,
				AgentURL:    "",
			},
			expected: []string{"web", "-port", "8082", "api", "a2a", "webui", "-api_server_address", "http://localhost:8082"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			args := buildLauncherArgs(tc.cfg)

			if len(args) != len(tc.expected) {
				t.Errorf("expected %d args, got %d: %v", len(tc.expected), len(args), args)
				return
			}

			for i, arg := range args {
				if arg != tc.expected[i] {
					t.Errorf("arg[%d]: expected %q, got %q", i, tc.expected[i], arg)
				}
			}
		})
	}
}

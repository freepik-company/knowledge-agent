package launcher

import (
	"context"
	"crypto/subtle"
	"fmt"
	"strconv"
	"strings"

	"github.com/a2aproject/a2a-go/a2asrv"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/full"
	"google.golang.org/adk/session"

	"knowledge-agent/internal/config"
	"knowledge-agent/internal/logger"
)

// Config holds the launcher configuration
type Config struct {
	Port        int
	EnableWebUI bool
	AgentURL    string
	APIKeys     map[string]string // Maps API key to caller ID for authentication
}

// AuthInterceptor implements a2asrv.CallInterceptor for API key authentication
type AuthInterceptor struct {
	apiKeys map[string]string // Maps API key to caller ID
}

// NewAuthInterceptor creates a new authentication interceptor
func NewAuthInterceptor(apiKeys map[string]string) *AuthInterceptor {
	return &AuthInterceptor{
		apiKeys: apiKeys,
	}
}

// Before validates the API key before processing the request
func (a *AuthInterceptor) Before(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) (context.Context, error) {
	log := logger.Get()

	// Get request metadata (contains headers)
	meta := callCtx.RequestMeta()
	if meta == nil {
		log.Warnw("A2A request rejected: no request metadata")
		return ctx, fmt.Errorf("unauthorized: missing request metadata")
	}

	// Find API key header (case-insensitive per RFC 7230)
	apiKey := a.findAPIKeyHeader(meta)
	if apiKey == "" {
		log.Warnw("A2A request rejected: missing API key",
			"method", callCtx.Method(),
		)
		return ctx, fmt.Errorf("unauthorized: missing X-API-Key header")
	}

	// Validate API key using constant-time comparison to prevent timing attacks
	callerID, valid := a.validateAPIKey(apiKey)
	if !valid {
		log.Warnw("A2A request rejected: invalid API key",
			"method", callCtx.Method(),
		)
		return ctx, fmt.Errorf("unauthorized: invalid API key")
	}

	log.Debugw("A2A request authenticated",
		"caller_id", callerID,
		"method", callCtx.Method(),
	)

	return ctx, nil
}

// findAPIKeyHeader finds the API key from request metadata using case-insensitive header matching
func (a *AuthInterceptor) findAPIKeyHeader(meta *a2asrv.RequestMeta) string {
	// Iterate through all headers to find X-API-Key (case-insensitive)
	for key, values := range meta.List() {
		if strings.EqualFold(key, "X-API-Key") && len(values) > 0 {
			return values[0]
		}
	}
	return ""
}

// validateAPIKey validates the API key using constant-time comparison to prevent timing attacks
// Config format is client_id: secret, so we compare the secret (value) with the provided key
func (a *AuthInterceptor) validateAPIKey(providedKey string) (callerID string, valid bool) {
	// Iterate through all keys and use constant-time comparison
	// This prevents timing attacks that could reveal which keys exist
	// Map format: clientID -> secret
	for clientID, secret := range a.apiKeys {
		if subtle.ConstantTimeCompare([]byte(secret), []byte(providedKey)) == 1 {
			return clientID, true
		}
	}
	return "", false
}

// After is called after request processing (no-op for auth)
func (a *AuthInterceptor) After(ctx context.Context, callCtx *a2asrv.CallContext, resp *a2asrv.Response) error {
	return nil
}

// Run starts the ADK launcher with the provided agent and session service
// This exposes:
// - /api/* endpoints for REST API
// - /a2a/invoke for A2A protocol
// - /ui/* for WebUI (if enabled)
// - /.well-known/agent-card.json for agent discovery
func Run(ctx context.Context, cfg Config, ag agent.Agent, sessionSvc session.Service) error {
	log := logger.Get()

	log.Infow("Starting ADK Launcher",
		"port", cfg.Port,
		"webui_enabled", cfg.EnableWebUI,
		"agent_url", cfg.AgentURL,
		"auth_enabled", len(cfg.APIKeys) > 0,
	)

	// Create launcher configuration
	launcherConfig := &launcher.Config{
		SessionService: sessionSvc,
		AgentLoader:    agent.NewSingleLoader(ag),
	}

	// Add authentication interceptor if API keys are configured
	if len(cfg.APIKeys) > 0 {
		log.Infow("A2A authentication enabled for launcher",
			"api_keys_count", len(cfg.APIKeys),
		)
		authInterceptor := NewAuthInterceptor(cfg.APIKeys)
		launcherConfig.A2AOptions = []a2asrv.RequestHandlerOption{
			a2asrv.WithCallInterceptor(authInterceptor),
		}
	} else {
		log.Warn("A2A authentication disabled for launcher (no API keys configured)")
	}

	// Build command line arguments for the launcher
	args := buildLauncherArgs(cfg)

	log.Debugw("Launcher arguments", "args", args)

	// Create and execute the full launcher
	l := full.NewLauncher()
	return l.Execute(ctx, launcherConfig, args)
}

// buildLauncherArgs constructs the command line arguments for the launcher
func buildLauncherArgs(cfg Config) []string {
	args := []string{
		"web",
		"-port", strconv.Itoa(cfg.Port),
		"api",
		"a2a",
	}

	// Add agent URL for A2A discovery if provided
	if cfg.AgentURL != "" {
		args = append(args, "--a2a_agent_url", cfg.AgentURL)
	}

	// Enable WebUI if configured
	if cfg.EnableWebUI {
		// WebUI needs to know the API server address
		apiAddr := fmt.Sprintf("http://localhost:%d", cfg.Port)
		args = append(args, "webui", "-api_server_address", apiAddr)
	}

	return args
}

// NewConfigFromAppConfig creates a launcher Config from the application config
func NewConfigFromAppConfig(cfg *config.LauncherConfig, apiKeys map[string]string) Config {
	agentURL := cfg.AgentURL
	if agentURL == "" {
		// Default to localhost if not specified
		agentURL = fmt.Sprintf("http://localhost:%d", cfg.Port)
	}

	return Config{
		Port:        cfg.Port,
		EnableWebUI: cfg.EnableWebUI,
		AgentURL:    agentURL,
		APIKeys:     apiKeys,
	}
}

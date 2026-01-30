package mcp

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/mcptoolset"

	"knowledge-agent/internal/config"
	"knowledge-agent/internal/logger"
)

// CreateMCPToolset creates an MCP toolset from configuration
// Returns (toolset, error) where error is non-nil on failure
func CreateMCPToolset(ctx context.Context, cfg config.MCPServerConfig, retryCfg config.RetryConfig) (tool.Toolset, error) {
	log := logger.Get()

	if !cfg.Enabled {
		return nil, fmt.Errorf("server %s is disabled", cfg.Name)
	}

	log.Infow("Creating MCP toolset",
		"server", cfg.Name,
		"transport", cfg.TransportType,
		"description", cfg.Description)

	// Create transport based on type
	var transport mcp.Transport
	var err error

	switch cfg.TransportType {
	case "command":
		transport, err = createCommandTransport(cfg)
	case "sse":
		transport, err = createSSETransport(cfg, retryCfg)
	case "streamable":
		transport, err = createStreamableTransport(cfg, retryCfg)
	default:
		return nil, fmt.Errorf("unsupported transport type: %s", cfg.TransportType)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create transport: %w", err)
	}

	// Build mcptoolset config
	mcpConfig := mcptoolset.Config{
		Transport: transport,
	}

	// Apply tool filter if specified
	if len(cfg.ToolFilter) > 0 {
		log.Infow("Applying tool filter", "server", cfg.Name, "tools", cfg.ToolFilter)
		mcpConfig.ToolFilter = tool.StringPredicate(cfg.ToolFilter)
	}

	// Create toolset
	toolset, err := mcptoolset.New(mcpConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create MCP toolset: %w", err)
	}

	log.Infow("MCP toolset created successfully", "server", cfg.Name)
	return toolset, nil
}

// commandTransportFactory creates a new command for each connection
// This is necessary because exec.Cmd cannot be reused
type commandTransportFactory struct {
	path              string
	args              []string
	env               []string
	terminateDuration time.Duration
}

// Connect implements mcp.Transport by creating a fresh command each time
func (f *commandTransportFactory) Connect(ctx context.Context) (mcp.Connection, error) {
	// Create a fresh command for this connection
	cmd := exec.CommandContext(ctx, f.path, f.args...)

	// Set environment variables
	if len(f.env) > 0 {
		cmd.Env = f.env
	}

	// Create transport with the fresh command
	transport := &mcp.CommandTransport{
		Command:           cmd,
		TerminateDuration: f.terminateDuration,
	}

	return transport.Connect(ctx)
}

// getSafeEnvironmentVariables returns a filtered list of environment variables
// that are safe to pass to MCP server processes. This prevents leaking secrets
// like ANTHROPIC_API_KEY, POSTGRES_URL, etc. to potentially untrusted MCP servers.
func getSafeEnvironmentVariables() []string {
	// Allowlist of safe environment variables
	safeVars := []string{
		"PATH",
		"HOME",
		"USER",
		"LANG",
		"LC_ALL",
		"LC_CTYPE",
		"TMPDIR",
		"TMP",
		"TEMP",
		"SHELL",
		"TERM",
		// Node.js related (for npx-based MCP servers)
		"NODE_OPTIONS",
		"NPM_CONFIG_PREFIX",
		// Python related (for pip-based MCP servers)
		"PYTHONPATH",
		"VIRTUAL_ENV",
	}

	env := []string{}
	for _, key := range safeVars {
		if val, ok := os.LookupEnv(key); ok {
			env = append(env, fmt.Sprintf("%s=%s", key, val))
		}
	}
	return env
}

// createCommandTransport creates a command-based transport (stdio)
func createCommandTransport(cfg config.MCPServerConfig) (mcp.Transport, error) {
	if cfg.Command == nil {
		return nil, fmt.Errorf("command configuration is required")
	}

	log := logger.Get()
	log.Infow("Creating command transport factory",
		"server", cfg.Name,
		"path", cfg.Command.Path,
		"args", cfg.Command.Args)

	// Start with safe environment variables (no secrets)
	env := getSafeEnvironmentVariables()

	// Add inherited environment variables from pod (by name)
	// These are explicitly listed in config, read from current environment
	if len(cfg.Command.InheritEnv) > 0 {
		log.Infow("Inheriting environment variables from pod",
			"server", cfg.Name,
			"vars", cfg.Command.InheritEnv)
		for _, varName := range cfg.Command.InheritEnv {
			if value, ok := os.LookupEnv(varName); ok {
				env = append(env, fmt.Sprintf("%s=%s", varName, value))
			} else {
				log.Warnw("Inherited env var not found in environment",
					"server", cfg.Name,
					"var", varName)
			}
		}
	}

	// Add server-specific environment variables from config (static values)
	// These are explicitly configured per-server with literal values
	if len(cfg.Command.Env) > 0 {
		log.Infow("Adding static environment variables",
			"server", cfg.Name,
			"count", len(cfg.Command.Env))
		for key, value := range cfg.Command.Env {
			env = append(env, fmt.Sprintf("%s=%s", key, value))
		}
	}

	log.Debugw("MCP environment prepared",
		"server", cfg.Name,
		"total_vars", len(env))

	// Create factory that generates fresh commands
	factory := &commandTransportFactory{
		path:              cfg.Command.Path,
		args:              cfg.Command.Args,
		env:               env,
		terminateDuration: 5 * time.Second,
	}

	return factory, nil
}

// createSSETransport creates an SSE-based transport with automatic reconnection
func createSSETransport(cfg config.MCPServerConfig, retryCfg config.RetryConfig) (mcp.Transport, error) {
	log := logger.Get()
	log.Infow("Creating SSE transport", "server", cfg.Name, "endpoint", cfg.Endpoint, "retry_enabled", retryCfg.Enabled)

	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("endpoint is required for SSE transport")
	}

	// For Server-Sent Events (SSE), we need special timeout handling:
	// - NO Client.Timeout: This would kill long-running SSE connections
	// - ResponseHeaderTimeout: Only for initial handshake/headers (30s default)
	// - IdleConnTimeout: For detecting broken connections

	responseHeaderTimeout := time.Duration(cfg.Timeout) * time.Second
	if responseHeaderTimeout == 0 {
		responseHeaderTimeout = 30 * time.Second
	}

	// Create custom transport with SSE-friendly timeouts
	var httpTransport http.RoundTripper = &http.Transport{
		ResponseHeaderTimeout: responseHeaderTimeout, // Timeout only for headers, not body
		IdleConnTimeout:       90 * time.Second,      // Detect broken idle connections
		// NO DialTimeout or TLSHandshakeTimeout needed - use defaults
	}

	// Wrap with retry logic if enabled
	if retryCfg.Enabled {
		log.Infow("Adding HTTP retry wrapper for MCP server",
			"server", cfg.Name,
			"max_retries", retryCfg.MaxRetries,
			"initial_delay", retryCfg.InitialDelay,
		)
		httpTransport = NewRetryRoundTripper(httpTransport, cfg.Name, retryCfg)
	}

	httpClient := &http.Client{
		Transport: httpTransport,
		Timeout:   0, // CRITICAL: No global timeout for SSE streaming connections
	}

	// Apply authentication if configured
	if cfg.Auth != nil {
		httpClient = applyAuth(httpClient, cfg.Auth)
	}

	// Create base SSE client transport
	sseTransport := &mcp.SSEClientTransport{
		Endpoint:   cfg.Endpoint,
		HTTPClient: httpClient,
	}

	// Wrap with retry logic for automatic reconnection
	return &retryTransport{
		name:          cfg.Name,
		baseTransport: sseTransport,
		maxRetries:    5,
		initialDelay:  500 * time.Millisecond,
	}, nil
}

// createStreamableTransport creates a streamable HTTP transport with automatic reconnection
func createStreamableTransport(cfg config.MCPServerConfig, retryCfg config.RetryConfig) (mcp.Transport, error) {
	log := logger.Get()
	log.Infow("Creating streamable transport", "server", cfg.Name, "endpoint", cfg.Endpoint, "retry_enabled", retryCfg.Enabled)

	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("endpoint is required for streamable transport")
	}

	// For streaming connections (SSE/Streamable), we need special timeout handling:
	// - NO Client.Timeout: This would kill long-running streaming connections
	// - ResponseHeaderTimeout: Only for initial handshake/headers (30s default)
	// - IdleConnTimeout: For detecting broken connections

	responseHeaderTimeout := time.Duration(cfg.Timeout) * time.Second
	if responseHeaderTimeout == 0 {
		responseHeaderTimeout = 30 * time.Second
	}

	// Create custom transport with streaming-friendly timeouts
	var httpTransport http.RoundTripper = &http.Transport{
		ResponseHeaderTimeout: responseHeaderTimeout, // Timeout only for headers, not body
		IdleConnTimeout:       90 * time.Second,      // Detect broken idle connections
		// NO DialTimeout or TLSHandshakeTimeout needed - use defaults
	}

	// Wrap with retry logic if enabled
	if retryCfg.Enabled {
		log.Infow("Adding HTTP retry wrapper for MCP server",
			"server", cfg.Name,
			"max_retries", retryCfg.MaxRetries,
			"initial_delay", retryCfg.InitialDelay,
		)
		httpTransport = NewRetryRoundTripper(httpTransport, cfg.Name, retryCfg)
	}

	httpClient := &http.Client{
		Transport: httpTransport,
		Timeout:   0, // CRITICAL: No global timeout for streaming connections
	}

	// Apply authentication if configured
	if cfg.Auth != nil {
		httpClient = applyAuth(httpClient, cfg.Auth)
	}

	// Create base streamable client transport
	streamTransport := &mcp.StreamableClientTransport{
		Endpoint:   cfg.Endpoint,
		HTTPClient: httpClient,
	}

	// Wrap with retry logic for automatic reconnection
	return &retryTransport{
		name:          cfg.Name,
		baseTransport: streamTransport,
		maxRetries:    5,
		initialDelay:  500 * time.Millisecond,
	}, nil
}

// applyAuth applies authentication to HTTP client by wrapping its existing Transport
// This preserves any custom Transport settings (timeouts, etc.)
func applyAuth(client *http.Client, auth *config.MCPAuthConfig) *http.Client {
	log := logger.Get()

	// Get the base transport (use DefaultTransport if none set)
	baseTransport := client.Transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}

	switch auth.Type {
	case "bearer":
		token := auth.Token
		if auth.TokenEnv != "" {
			token = os.Getenv(auth.TokenEnv)
			if token == "" {
				log.Warnw("Token environment variable is empty", "env_var", auth.TokenEnv)
			}
		}
		if token != "" {
			// Wrap the existing transport (preserves custom timeouts)
			client.Transport = &bearerTransport{
				base:  baseTransport,
				token: token,
			}
			log.Debug("Applied bearer token authentication (preserving base transport)")
		}

	case "basic":
		username := auth.Username
		password := auth.Password
		if username != "" && password != "" {
			// Wrap the existing transport (preserves custom timeouts)
			client.Transport = &basicAuthTransport{
				base:     baseTransport,
				username: username,
				password: password,
			}
			log.Debug("Applied basic authentication (preserving base transport)")
		}

	case "oauth2":
		log.Warn("OAuth2 authentication not yet implemented")

	default:
		log.Warnw("Unknown authentication type", "type", auth.Type)
	}

	return client
}

// bearerTransport implements HTTP Bearer token authentication
type bearerTransport struct {
	base  http.RoundTripper
	token string
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+t.token)
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

// basicAuthTransport implements HTTP Basic authentication
type basicAuthTransport struct {
	base     http.RoundTripper
	username string
	password string
}

func (t *basicAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.SetBasicAuth(t.username, t.password)
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

// retryTransport wraps an MCP transport with automatic reconnection logic
// This is crucial for HTTP-based transports (SSE, Streamable) that can disconnect between queries
// IMPORTANT: Uses a long-lived background context for connections to prevent premature closes
type retryTransport struct {
	name          string
	baseTransport mcp.Transport
	maxRetries    int
	initialDelay  time.Duration
}

// Connect implements mcp.Transport with exponential backoff retry logic
// CRITICAL: We use context.Background() for the actual connection to ensure streaming
// connections stay alive beyond individual query contexts. The passed ctx is only used
// to respect cancellation during the connection attempt itself.
func (r *retryTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	log := logger.Get()
	var lastErr error

	// Create a long-lived context for the actual connection
	// This prevents the streaming connection from being closed when individual
	// query contexts are canceled
	connCtx := context.Background()

	for attempt := 1; attempt <= r.maxRetries; attempt++ {
		// Check if the caller's context is still valid before attempting
		if ctx.Err() != nil {
			log.Warnw("Caller context canceled before connection attempt",
				"server", r.name,
				"attempt", attempt)
			return nil, fmt.Errorf("caller context canceled: %w", ctx.Err())
		}

		log.Debugw("Attempting MCP connection",
			"server", r.name,
			"attempt", attempt,
			"max_retries", r.maxRetries)

		// Use the long-lived connCtx for the actual connection
		conn, err := r.baseTransport.Connect(connCtx)
		if err == nil {
			log.Infow("MCP connection established with long-lived context",
				"server", r.name,
				"attempt", attempt)
			return conn, nil
		}

		lastErr = err
		log.Warnw("MCP connection attempt failed",
			"server", r.name,
			"attempt", attempt,
			"error", err)

		// Don't sleep after the last attempt
		if attempt < r.maxRetries {
			// Exponential backoff: initialDelay * 2^(attempt-1)
			delay := r.initialDelay * time.Duration(1<<uint(attempt-1))
			log.Debugw("Waiting before retry",
				"server", r.name,
				"delay_ms", delay.Milliseconds())

			// Use caller's context for backoff cancellation
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("caller context canceled during backoff: %w", ctx.Err())
			case <-time.After(delay):
				// Continue to next retry
			}
		}
	}

	log.Errorw("MCP connection failed after all retries",
		"server", r.name,
		"attempts", r.maxRetries,
		"error", lastErr)

	return nil, fmt.Errorf("failed to connect after %d attempts: %w", r.maxRetries, lastErr)
}

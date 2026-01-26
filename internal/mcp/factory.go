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
func CreateMCPToolset(ctx context.Context, cfg config.MCPServerConfig) (tool.Toolset, error) {
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
		transport, err = createSSETransport(cfg)
	case "streamable":
		transport, err = createStreamableTransport(cfg)
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

	// Prepare environment variables
	env := os.Environ()
	if len(cfg.Command.Env) > 0 {
		for key, value := range cfg.Command.Env {
			env = append(env, fmt.Sprintf("%s=%s", key, value))
		}
	}

	// Create factory that generates fresh commands
	factory := &commandTransportFactory{
		path:              cfg.Command.Path,
		args:              cfg.Command.Args,
		env:               env,
		terminateDuration: 5 * time.Second,
	}

	return factory, nil
}

// createSSETransport creates an SSE-based transport
func createSSETransport(cfg config.MCPServerConfig) (mcp.Transport, error) {
	log := logger.Get()
	log.Infow("Creating SSE transport", "server", cfg.Name, "endpoint", cfg.Endpoint)

	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("endpoint is required for SSE transport")
	}

	// Create HTTP client with timeout
	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	httpClient := &http.Client{
		Timeout: timeout,
	}

	// Apply authentication if configured
	if cfg.Auth != nil {
		httpClient = applyAuth(httpClient, cfg.Auth)
	}

	// Create SSE client transport
	transport := &mcp.SSEClientTransport{
		Endpoint:   cfg.Endpoint,
		HTTPClient: httpClient,
	}

	return transport, nil
}

// createStreamableTransport creates a streamable HTTP transport
func createStreamableTransport(cfg config.MCPServerConfig) (mcp.Transport, error) {
	log := logger.Get()
	log.Infow("Creating streamable transport", "server", cfg.Name, "endpoint", cfg.Endpoint)

	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("endpoint is required for streamable transport")
	}

	// Create HTTP client with timeout
	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	httpClient := &http.Client{
		Timeout: timeout,
	}

	// Apply authentication if configured
	if cfg.Auth != nil {
		httpClient = applyAuth(httpClient, cfg.Auth)
	}

	// Create streamable client transport
	transport := &mcp.StreamableClientTransport{
		Endpoint:   cfg.Endpoint,
		HTTPClient: httpClient,
	}

	return transport, nil
}

// applyAuth applies authentication to HTTP client
func applyAuth(client *http.Client, auth *config.MCPAuthConfig) *http.Client {
	log := logger.Get()

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
			client.Transport = &bearerTransport{
				base:  client.Transport,
				token: token,
			}
			log.Debug("Applied bearer token authentication")
		}

	case "basic":
		username := auth.Username
		password := auth.Password
		if username != "" && password != "" {
			client.Transport = &basicAuthTransport{
				base:     client.Transport,
				username: username,
				password: password,
			}
			log.Debug("Applied basic authentication")
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

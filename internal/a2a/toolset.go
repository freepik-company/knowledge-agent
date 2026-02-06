package a2a

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2aclient/agentcard"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"

	"knowledge-agent/internal/auth/keycloak"
	"knowledge-agent/internal/config"
	"knowledge-agent/internal/logger"
)

// Timeout constants for A2A operations
const (
	// agentCardResolveTimeout is the maximum time to wait for agent card resolution
	agentCardResolveTimeout = 30 * time.Second

	// defaultHTTPClientTimeout is the default timeout for HTTP requests to sub-agents
	// This is intentionally high as sub-agents may perform complex operations
	defaultHTTPClientTimeout = 180 * time.Second

	// maxResponseTextLength is the maximum length of response text from sub-agents
	// Responses longer than this will be truncated to prevent memory issues
	maxResponseTextLength = 100_000 // 100KB
)

// SubAgentClient is the interface for clients that can query sub-agents
// Both A2A clients and REST clients implement this interface
type SubAgentClient interface {
	// Query sends a query to the sub-agent and returns the response text
	// For A2A clients, this wraps SendMessage
	// For REST clients, this calls /api/query directly
	Query(ctx context.Context, query string) (string, error)
	// Close releases any resources held by the client
	Close() error
}

// a2aClientWrapper wraps an a2aclient.Client to implement SubAgentClient
type a2aClientWrapper struct {
	client *a2aclient.Client
	name   string
}

func (w *a2aClientWrapper) Query(ctx context.Context, query string) (string, error) {
	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: query})
	result, err := w.client.SendMessage(ctx, &a2a.MessageSendParams{Message: msg})
	if err != nil {
		return "", err
	}
	return extractTextFromResult(result), nil
}

func (w *a2aClientWrapper) Close() error {
	if w.client != nil {
		return w.client.Destroy()
	}
	return nil
}

// restClientWrapper wraps a RESTClient to implement SubAgentClient
type restClientWrapper struct {
	client *RESTClient
	name   string
}

func (w *restClientWrapper) Query(ctx context.Context, query string) (string, error) {
	resp, err := w.client.Query(ctx, query)
	if err != nil {
		return "", err
	}
	if !resp.IsSuccess() {
		if errMsg := resp.GetError(); errMsg != "" {
			return "", fmt.Errorf("agent returned error: %s", errMsg)
		}
		return "", fmt.Errorf("agent returned success=false")
	}
	return resp.GetAnswer(), nil
}

func (w *restClientWrapper) Close() error {
	if w.client != nil {
		return w.client.Close()
	}
	return nil
}

// A2AToolset provides tools for calling A2A sub-agents without handoff.
// Each sub-agent becomes a tool named "query_<agent_name>" that the LLM can call.
// Unlike remoteagent.NewA2A which performs a handoff (terminating the parent agent flow),
// these tools return results to the LLM allowing sequential/multiple sub-agent calls.
// Additionally, a "query_multiple_agents" tool enables parallel execution of multiple
// sub-agent calls for improved performance.
//
// Supports two protocols:
// - "a2a" (default): Uses A2A protocol with JSON-RPC, interceptors, and full protocol support
// - "rest": Uses direct HTTP calls to /api/query (simpler, faster, better error messages)
type A2AToolset struct {
	tools      []tool.Tool
	clients    []SubAgentClient          // Unified client interface (A2A or REST)
	clientsMap map[string]SubAgentClient // For parallel query tool
	a2aClients []*a2aclient.Client       // Keep reference for legacy compatibility
}

// QuerySubAgentArgs are the arguments for sub-agent query tools
type QuerySubAgentArgs struct {
	Query string `json:"query" description:"The query or task to send to the sub-agent"`
}

// QuerySubAgentResult is the result from a sub-agent query
type QuerySubAgentResult struct {
	Success  bool   `json:"success"`
	Response string `json:"response,omitempty"`
	Error    string `json:"error,omitempty"`
}

// NewA2AToolset creates tools for each configured sub-agent.
// Each sub-agent becomes a tool named "query_<agent_name>" that can be called by the LLM.
// keycloakClient can be nil if Keycloak integration is disabled.
func NewA2AToolset(ctx context.Context, cfg *config.A2AConfig, keycloakClient *keycloak.Client) (*A2AToolset, error) {
	log := logger.Get()

	if !cfg.Enabled {
		return nil, nil
	}

	if len(cfg.SubAgents) == 0 {
		log.Debug("A2A enabled but no sub_agents configured")
		return nil, nil
	}

	log.Infow("Creating A2A toolset",
		"self_name", cfg.SelfName,
		"sub_agents_count", len(cfg.SubAgents),
		"polling", cfg.Polling,
		"query_extractor_enabled", cfg.QueryExtractor.Enabled,
		"keycloak_enabled", keycloakClient != nil && keycloakClient.IsEnabled(),
	)

	toolset := &A2AToolset{
		tools:      make([]tool.Tool, 0, len(cfg.SubAgents)+1), // +1 for parallel query tool
		clients:    make([]SubAgentClient, 0, len(cfg.SubAgents)),
		clientsMap: make(map[string]SubAgentClient),
		a2aClients: make([]*a2aclient.Client, 0, len(cfg.SubAgents)),
	}

	for _, subAgentCfg := range cfg.SubAgents {
		// Determine protocol (default: a2a for backwards compatibility)
		protocol := strings.ToLower(subAgentCfg.Protocol)
		if protocol == "" {
			protocol = "a2a"
		}

		var t tool.Tool
		var client SubAgentClient
		var err error

		switch protocol {
		case "rest":
			t, client, err = createRESTSubAgentTool(ctx, subAgentCfg, keycloakClient)
		case "a2a":
			var a2aClient *a2aclient.Client
			t, a2aClient, err = createA2ASubAgentTool(ctx, subAgentCfg, cfg, keycloakClient)
			if err == nil && a2aClient != nil {
				client = &a2aClientWrapper{client: a2aClient, name: subAgentCfg.Name}
				toolset.a2aClients = append(toolset.a2aClients, a2aClient)
			}
		default:
			log.Warnw("Unknown protocol for sub-agent, skipping",
				"agent", subAgentCfg.Name,
				"protocol", protocol,
			)
			continue
		}

		if err != nil {
			// Graceful degradation: log warning but continue with other agents
			log.Warnw("Failed to create sub-agent tool, skipping",
				"agent", subAgentCfg.Name,
				"protocol", protocol,
				"error", err,
			)
			continue
		}

		toolset.tools = append(toolset.tools, t)
		toolset.clients = append(toolset.clients, client)
		toolset.clientsMap[subAgentCfg.Name] = client // Store by name for parallel queries
		log.Infow("Sub-agent tool created",
			"name", t.Name(),
			"endpoint", subAgentCfg.Endpoint,
			"protocol", protocol,
			"auth_type", subAgentCfg.Auth.Type,
		)
	}

	// Create parallel query tool if we have at least 2 sub-agents
	if len(toolset.clientsMap) >= 2 {
		parallelTool, err := createParallelQueryTool(toolset.clientsMap)
		if err != nil {
			log.Warnw("Failed to create parallel query tool",
				"error", err,
			)
		} else {
			toolset.tools = append(toolset.tools, parallelTool)
			log.Infow("Parallel query tool created",
				"available_agents", getAvailableAgents(toolset.clientsMap),
			)
		}
	}

	if len(toolset.tools) > 0 {
		log.Infow("A2A toolset created successfully",
			"tools_count", len(toolset.tools),
			"sub_agents", len(toolset.clientsMap),
			"parallel_enabled", len(toolset.clientsMap) >= 2,
		)
	} else if len(cfg.SubAgents) > 0 {
		log.Warn("A2A enabled but no sub-agent tools were created successfully")
	}

	return toolset, nil
}

// createRESTSubAgentTool creates a tool for a sub-agent using REST protocol
// This is simpler and faster than A2A, with better error messages
func createRESTSubAgentTool(ctx context.Context, cfg config.A2ASubAgentConfig, keycloakClient *keycloak.Client) (tool.Tool, SubAgentClient, error) {
	log := logger.Get()

	log.Debugw("Creating REST sub-agent tool",
		"name", cfg.Name,
		"endpoint", cfg.Endpoint,
		"auth_type", cfg.Auth.Type,
		"keycloak_enabled", keycloakClient != nil && keycloakClient.IsEnabled(),
	)

	// Prepare auth headers if needed
	var authHeaderName, authHeaderValue string
	authType := strings.ToLower(cfg.Auth.Type)
	if authType != "" && authType != "none" {
		var err error
		authHeaderName, authHeaderValue, err = resolveAuthHeader(cfg.Auth)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to resolve auth for agent %s: %w", cfg.Name, err)
		}

		log.Debugw("Configuring auth for REST sub-agent tool",
			"agent", cfg.Name,
			"auth_type", authType,
			"header", authHeaderName,
		)
	}

	// Try to resolve agent card to get description (optional for REST)
	var description string
	cardResolveOpts := []agentcard.ResolveOption{}
	if authHeaderName != "" {
		cardResolveOpts = append(cardResolveOpts, agentcard.WithRequestHeader(authHeaderName, authHeaderValue))
	}

	cardCtx, cancel := context.WithTimeout(ctx, agentCardResolveTimeout)
	defer cancel()

	card, err := agentcard.DefaultResolver.Resolve(cardCtx, cfg.Endpoint, cardResolveOpts...)
	if err != nil {
		log.Warnw("Could not resolve agent card for REST tool (using default description)",
			"agent", cfg.Name,
			"error", err,
		)
		description = fmt.Sprintf("Query the %s agent for specialized assistance.", cfg.Name)
	} else {
		log.Infow("Agent card resolved for REST tool",
			"agent", cfg.Name,
			"has_description", card.Description != "",
			"description_len", len(card.Description),
		)
		if card.Description != "" {
			description = fmt.Sprintf("Query the %s agent: %s", cfg.Name, card.Description)
		} else {
			description = fmt.Sprintf("Query the %s agent for specialized assistance.", cfg.Name)
		}
	}

	// Extract base URL from endpoint (remove any path like /a2a/invoke)
	baseURL, err := ExtractBaseURL(cfg.Endpoint)
	if err != nil {
		// If URL parsing fails, try using the endpoint directly
		baseURL = cfg.Endpoint
	}

	// Build HTTP client with configured timeout
	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout <= 0 {
		timeout = defaultHTTPClientTimeout
	}

	// Create REST client
	restClient := NewRESTClient(RESTClientConfig{
		Name:           cfg.Name,
		BaseURL:        baseURL,
		APIPath:        cfg.APIPath, // Configurable path (default: /query)
		Timeout:        timeout,
		AuthHeaderName: authHeaderName,
		AuthHeaderVal:  authHeaderValue,
		KeycloakClient: keycloakClient,
	})

	// Create the handler function that calls the sub-agent via REST
	handler := createRESTSubAgentHandler(cfg.Name, restClient)

	// Create the function tool
	toolName := fmt.Sprintf("query_%s", strings.ReplaceAll(cfg.Name, "-", "_"))
	t, err := functiontool.New(functiontool.Config{
		Name:        toolName,
		Description: description,
	}, handler)
	if err != nil {
		restClient.Close()
		return nil, nil, fmt.Errorf("failed to create function tool for %s: %w", cfg.Name, err)
	}

	wrapper := &restClientWrapper{client: restClient, name: cfg.Name}
	return t, wrapper, nil
}

// createRESTSubAgentHandler creates the handler function for a REST-based sub-agent tool
func createRESTSubAgentHandler(agentName string, client *RESTClient) functiontool.Func[QuerySubAgentArgs, QuerySubAgentResult] {
	return func(ctx tool.Context, args QuerySubAgentArgs) (QuerySubAgentResult, error) {
		log := logger.Get()

		log.Infow("REST sub-agent tool invoked",
			"agent", agentName,
			"query_length", len(args.Query),
			"query_preview", truncateString(args.Query, 100),
		)

		if args.Query == "" {
			return QuerySubAgentResult{
				Success: false,
				Error:   "query cannot be empty",
			}, nil
		}

		// Call sub-agent via REST
		resp, err := client.Query(ctx, args.Query)
		if err != nil {
			log.Errorw("REST sub-agent call failed",
				"agent", agentName,
				"error", err,
			)
			return QuerySubAgentResult{
				Success: false,
				Error:   fmt.Sprintf("failed to call %s: %v", agentName, err),
			}, nil
		}

		responseText := resp.GetAnswer()
		if len(responseText) > maxResponseTextLength {
			responseText = responseText[:maxResponseTextLength] + "\n[TRUNCATED - response exceeded 100KB limit]"
		}

		log.Infow("REST sub-agent call completed",
			"agent", agentName,
			"success", resp.IsSuccess(),
			"response_length", len(responseText),
			"response_preview", truncateString(responseText, 100),
		)

		return QuerySubAgentResult{
			Success:  resp.IsSuccess(),
			Response: responseText,
		}, nil
	}
}

// createA2ASubAgentTool creates a tool for a single sub-agent using A2A protocol
func createA2ASubAgentTool(ctx context.Context, cfg config.A2ASubAgentConfig, a2aCfg *config.A2AConfig, keycloakClient *keycloak.Client) (tool.Tool, *a2aclient.Client, error) {
	log := logger.Get()

	log.Debugw("Creating sub-agent tool",
		"name", cfg.Name,
		"endpoint", cfg.Endpoint,
		"auth_type", cfg.Auth.Type,
		"polling", a2aCfg.Polling,
		"query_extractor_enabled", a2aCfg.QueryExtractor.Enabled,
		"keycloak_enabled", keycloakClient != nil && keycloakClient.IsEnabled(),
	)

	// Prepare auth headers if needed
	var authHeaderName, authHeaderValue string
	authType := strings.ToLower(cfg.Auth.Type)
	if authType != "" && authType != "none" {
		var err error
		authHeaderName, authHeaderValue, err = resolveAuthHeader(cfg.Auth)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to resolve auth for agent %s: %w", cfg.Name, err)
		}

		log.Debugw("Configuring auth for sub-agent tool",
			"agent", cfg.Name,
			"auth_type", authType,
			"header", authHeaderName,
		)
	}

	// Build card resolve options (for pre-resolving agent card)
	var cardResolveOpts []agentcard.ResolveOption
	if authHeaderName != "" {
		cardResolveOpts = append(cardResolveOpts, agentcard.WithRequestHeader(authHeaderName, authHeaderValue))
	}

	// Pre-resolve the agent card to extract description
	cardCtx, cancel := context.WithTimeout(ctx, agentCardResolveTimeout)
	defer cancel()

	card, err := agentcard.DefaultResolver.Resolve(cardCtx, cfg.Endpoint, cardResolveOpts...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve agent card for %s: %w", cfg.Name, err)
	}

	// Log agent card info for debugging
	log.Infow("Agent card resolved for tool",
		"agent", cfg.Name,
		"has_description", card.Description != "",
		"description_len", len(card.Description),
	)

	// Disable streaming when polling is enabled (required for A2A communication)
	if a2aCfg.Polling {
		log.Debugw("Disabling streaming for sub-agent tool (polling mode)",
			"agent", cfg.Name,
			"original_streaming", card.Capabilities.Streaming,
		)
		card.Capabilities.Streaming = false
	}

	// Build HTTP client with configured timeout
	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout <= 0 {
		timeout = defaultHTTPClientTimeout
	}
	httpClient := &http.Client{Timeout: timeout}

	log.Debugw("Configuring A2A HTTP client for tool",
		"agent", cfg.Name,
		"timeout", timeout,
	)

	// Build client factory options with custom HTTP client
	factoryOpts := []a2aclient.FactoryOption{
		a2aclient.WithJSONRPCTransport(httpClient),
		a2aclient.WithConfig(a2aclient.Config{
			Polling: a2aCfg.Polling,
		}),
	}

	// Add identity interceptor FIRST - propagates user identity (Slack ID, email, session ID) and Keycloak JWT
	factoryOpts = append(factoryOpts, a2aclient.WithInterceptors(
		NewIdentityInterceptor(cfg.Name, keycloakClient),
	))

	// Add query extractor interceptor if enabled
	if a2aCfg.QueryExtractor.Enabled {
		log.Debugw("Adding query extractor interceptor for sub-agent tool",
			"agent", cfg.Name,
			"model", a2aCfg.QueryExtractor.Model,
			"has_card_description", card.Description != "",
		)
		factoryOpts = append(factoryOpts, a2aclient.WithInterceptors(
			NewQueryExtractorInterceptor(cfg.Name, card.Description, a2aCfg.QueryExtractor),
		))
	}

	// Always add logging interceptor for debugging A2A calls
	factoryOpts = append(factoryOpts, a2aclient.WithInterceptors(&loggingInterceptor{
		agentName: cfg.Name,
	}))

	// Add auth interceptor if configured
	if authHeaderName != "" {
		factoryOpts = append(factoryOpts, a2aclient.WithInterceptors(&authInterceptor{
			headerName:  authHeaderName,
			headerValue: authHeaderValue,
		}))
	}

	// Add error recovery interceptor LAST - converts connection errors to valid responses
	factoryOpts = append(factoryOpts, a2aclient.WithInterceptors(
		NewErrorRecoveryInterceptor(cfg.Name),
	))

	// Create A2A client from the resolved card
	client, err := a2aclient.NewFromCard(ctx, card, factoryOpts...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create A2A client for %s: %w", cfg.Name, err)
	}

	// Build tool description from agent card
	toolDescription := fmt.Sprintf("Query the %s agent for specialized assistance.", cfg.Name)
	if card.Description != "" {
		toolDescription = fmt.Sprintf("Query the %s agent: %s", cfg.Name, card.Description)
	}

	// Create the handler function that calls the sub-agent
	handler := createSubAgentHandler(cfg.Name, client)

	// Create the function tool
	toolName := fmt.Sprintf("query_%s", strings.ReplaceAll(cfg.Name, "-", "_"))
	t, err := functiontool.New(functiontool.Config{
		Name:        toolName,
		Description: toolDescription,
	}, handler)
	if err != nil {
		client.Destroy()
		return nil, nil, fmt.Errorf("failed to create function tool for %s: %w", cfg.Name, err)
	}

	return t, client, nil
}

// createSubAgentHandler creates the handler function for a sub-agent tool
func createSubAgentHandler(agentName string, client *a2aclient.Client) functiontool.Func[QuerySubAgentArgs, QuerySubAgentResult] {
	return func(ctx tool.Context, args QuerySubAgentArgs) (QuerySubAgentResult, error) {
		log := logger.Get()

		log.Infow("Sub-agent tool invoked",
			"agent", agentName,
			"query_length", len(args.Query),
			"query_preview", truncateString(args.Query, 100),
		)

		if args.Query == "" {
			return QuerySubAgentResult{
				Success: false,
				Error:   "query cannot be empty",
			}, nil
		}

		// Create A2A message
		msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: args.Query})

		// Send message to sub-agent
		result, err := client.SendMessage(ctx, &a2a.MessageSendParams{Message: msg})
		if err != nil {
			log.Errorw("Sub-agent call failed",
				"agent", agentName,
				"error", err,
			)
			return QuerySubAgentResult{
				Success: false,
				Error:   fmt.Sprintf("failed to call %s: %v", agentName, err),
			}, nil
		}

		// Extract text from result
		responseText := extractTextFromResult(result)

		log.Infow("Sub-agent call completed",
			"agent", agentName,
			"response_length", len(responseText),
			"response_preview", truncateString(responseText, 100),
		)

		return QuerySubAgentResult{
			Success:  true,
			Response: responseText,
		}, nil
	}
}

// extractTextFromResult extracts text content from an A2A SendMessageResult
// The result is truncated to maxResponseTextLength to prevent memory issues
func extractTextFromResult(result a2a.SendMessageResult) string {
	if result == nil {
		return ""
	}

	var texts []string

	// SendMessageResult can be a Message or a Task
	switch r := result.(type) {
	case *a2a.Message:
		for _, part := range r.Parts {
			if textPart, ok := part.(a2a.TextPart); ok && textPart.Text != "" {
				texts = append(texts, textPart.Text)
			}
		}
	case *a2a.Task:
		// If it's a task, get the status message
		if r.Status.Message != nil {
			for _, part := range r.Status.Message.Parts {
				if textPart, ok := part.(a2a.TextPart); ok && textPart.Text != "" {
					texts = append(texts, textPart.Text)
				}
			}
		}
		// Also check artifacts
		for _, artifact := range r.Artifacts {
			for _, part := range artifact.Parts {
				if textPart, ok := part.(a2a.TextPart); ok && textPart.Text != "" {
					texts = append(texts, textPart.Text)
				}
			}
		}
	}

	joined := strings.Join(texts, "\n")

	// Truncate to prevent memory issues with very large responses
	if len(joined) > maxResponseTextLength {
		return joined[:maxResponseTextLength] + "\n[TRUNCATED - response exceeded 100KB limit]"
	}

	return joined
}

// Name returns the name of the toolset
func (ts *A2AToolset) Name() string {
	return "a2a_toolset"
}

// ToolCount returns the number of tools in the toolset
func (ts *A2AToolset) ToolCount() int {
	if ts == nil {
		return 0
	}
	return len(ts.tools)
}

// Tools returns the list of tools in the toolset
func (ts *A2AToolset) Tools(ctx agent.ReadonlyContext) ([]tool.Tool, error) {
	if ts == nil {
		return nil, nil
	}
	return ts.tools, nil
}

// Close cleans up all clients (A2A and REST)
func (ts *A2AToolset) Close() error {
	if ts == nil {
		return nil
	}

	log := logger.Get()
	var errs []error

	for _, client := range ts.clients {
		if client != nil {
			if err := client.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}

	if len(errs) > 0 {
		log.Warnw("Errors closing A2A toolset", "error_count", len(errs))
		return fmt.Errorf("errors closing clients: %v", errs)
	}

	log.Debug("A2A toolset closed successfully")
	return nil
}

package agent

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/memory"
	"google.golang.org/adk/server/adkrest"
	"google.golang.org/adk/tool"

	genaianthropic "github.com/achetronic/adk-utils-go/genai/anthropic"
	memorypostgres "github.com/achetronic/adk-utils-go/memory/postgres"
	sessionredis "github.com/achetronic/adk-utils-go/session/redis"
	memorytools "github.com/achetronic/adk-utils-go/tools/memory"

	"knowledge-agent/internal/a2a"
	"knowledge-agent/internal/auth/keycloak"
	"knowledge-agent/internal/config"
	"knowledge-agent/internal/logger"
	"knowledge-agent/internal/mcp"
	"knowledge-agent/internal/observability"
	"knowledge-agent/internal/tools"
)

const (
	// AppName is the ADK application name used for session management and agent registration.
	AppName = "knowledge-agent"
)

// init registers the default knowledge agent prompt
func init() {
	SetDefaultPrompt(SystemPrompt)
}

// truncateString truncates a string to maxLen and adds ellipsis if needed
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// containsError checks if a tool response contains an error indicator
func containsError(response map[string]any) bool {
	if response == nil {
		return false
	}

	// Check if response has "error" key
	if _, hasError := response["error"]; hasError {
		return true
	}
	// Also check for "Error" key
	if _, hasError := response["Error"]; hasError {
		return true
	}

	// Check if output field contains error patterns
	if output, ok := response["output"].(string); ok {
		lowerStr := strings.ToLower(output)
		if strings.Contains(lowerStr, "error") || strings.Contains(lowerStr, "failed") {
			return true
		}
	}

	return false
}

// resolveSessionID determines the session_id based on available context
// Priority:
// 1. Client-provided conversation_id -> use it directly
// 2. channel_id + thread_ts -> "thread-{channel}-{thread_ts}" (maintains context per thread)
// 3. channel_id only -> "channel-{channel}-{timestamp}"
// 4. No Slack context -> "api-{timestamp}"
func ResolveSessionID(conversationID, channelID, threadTS string) string {
	// 1. Client-provided conversation_id takes precedence
	if conversationID != "" {
		return conversationID
	}

	// 2. Thread context (channel + thread_ts)
	if channelID != "" && threadTS != "" {
		return fmt.Sprintf("thread-%s-%s", channelID, threadTS)
	}

	// 3. Channel only (rare case)
	if channelID != "" {
		return fmt.Sprintf("channel-%s-%d", channelID, time.Now().Unix())
	}

	// 4. API-only context (no Slack)
	return fmt.Sprintf("api-%d", time.Now().Unix())
}

// resolveUserID determines the user_id based on knowledge_scope configuration
// Note: A2A requests (without channel/user context) use "shared-knowledge" namespace
// which may be isolated from channel/user-specific data. For full A2A interoperability,
// use knowledge_scope: "shared"
func ResolveUserID(scope, channelID, slackUserID string) string {
	switch scope {
	case "shared":
		return "shared-knowledge"
	case "channel":
		if channelID != "" {
			return channelID
		}
		// A2A fallback: use shared namespace (isolated from channel-specific data)
		return "shared-knowledge"
	case "user":
		if slackUserID != "" {
			return slackUserID
		}
		// A2A fallback: use shared namespace (isolated from user-specific data)
		return "shared-knowledge"
	default:
		return "shared-knowledge"
	}
}

// Agent represents the knowledge agent
type Agent struct {
	config            *config.Config
	llmAgent          agent.Agent
	restHandler       http.Handler
	sessionService    *sessionredis.RedisSessionService
	memoryService     *memorypostgres.PostgresMemoryService

	permissionChecker *MemoryPermissionChecker
	promptManager     *PromptManager
	langfuseTracer    *observability.LangfuseTracer
	sessionManager    *SessionManager
	sessionSyncer     *SessionSyncer
	sessionCompactor  *SessionCompactor
	keycloakClient    *keycloak.Client // nil if Keycloak is disabled
	a2aToolset        *a2a.A2AToolset  // nil if A2A is disabled
}

// New creates a new agent instance with full ADK integration
func New(ctx context.Context, cfg *config.Config) (*Agent, error) {
	log := logger.Get()

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	log.Info("Initializing Knowledge Agent with ADK")

	// 1. Initialize Redis session service
	log.Infow("Connecting to Redis for session management", "addr", cfg.Redis.Addr)
	sessionService, err := sessionredis.NewRedisSessionService(sessionredis.RedisSessionServiceConfig{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       0,
		TTL:      cfg.Redis.TTL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Redis session service: %w", err)
	}

	// 2. Initialize PostgreSQL memory service with Ollama embeddings
	log.Info("Connecting to PostgreSQL for long-term memory")
	memoryService, err := memorypostgres.NewPostgresMemoryService(ctx, memorypostgres.PostgresMemoryServiceConfig{
		ConnString: cfg.Postgres.URL,
		EmbeddingModel: memorypostgres.NewOpenAICompatibleEmbedding(memorypostgres.OpenAICompatibleEmbeddingConfig{
			BaseURL: cfg.Ollama.BaseURL,
			Model:   cfg.Ollama.EmbeddingModel,
		}),
	})
	if err != nil {
		sessionService.Close()
		return nil, fmt.Errorf("failed to create Postgres memory service: %w", err)
	}

	// 3. Initialize Anthropic LLM client
	log.Infow("Initializing Anthropic client", "model", cfg.Anthropic.Model)
	llmModel := genaianthropic.New(genaianthropic.Config{
		APIKey:    cfg.Anthropic.APIKey,
		ModelName: cfg.Anthropic.Model,
	})

	// 4. Initialize permission checker
	permChecker := NewMemoryPermissionChecker(&cfg.Permissions)

	// 5. Wrap memory service with permission checking
	// This intercepts AddSession calls to enforce save_to_memory permissions.
	// Permission context is propagated by ADK from the HTTP request context.
	log.Info("Wrapping memory service with permission enforcement")
	permissionMemoryService := NewPermissionMemoryService(memoryService, permChecker)

	// 6. Create memory toolset using adk-utils-go with wrapped service
	log.Info("Creating memory toolset (adk-utils-go)")
	memoryToolset, err := memorytools.NewToolset(memorytools.ToolsetConfig{
		MemoryService: permissionMemoryService, // Use wrapped service
		AppName:       AppName,
	})
	if err != nil {
		memoryService.Close()
		sessionService.Close()
		return nil, fmt.Errorf("failed to create memory toolset: %w", err)
	}

	// 7. Create web fetch toolset
	log.Info("Creating web fetch toolset")
	webToolset, err := tools.NewWebFetchToolset(tools.WebFetchConfig{
		Timeout:          cfg.Tools.WebFetch.Timeout,
		DefaultMaxLength: cfg.Tools.WebFetch.DefaultMaxLength,
	})
	if err != nil {
		memoryService.Close()
		sessionService.Close()
		return nil, fmt.Errorf("failed to create web fetch toolset: %w", err)
	}

	// 8. Create MCP toolsets (if enabled)
	var mcpToolsets []tool.Toolset
	if cfg.MCP.Enabled {
		log.Infow("MCP integration enabled", "servers", len(cfg.MCP.Servers))
		for _, serverCfg := range cfg.MCP.Servers {
			if !serverCfg.Enabled {
				log.Debugw("Skipping disabled MCP server", "server", serverCfg.Name)
				continue
			}

			mcpToolset, err := mcp.CreateMCPToolset(ctx, serverCfg, cfg.MCP.Retry)
			if err != nil {
				// Graceful degradation: log error but don't fail agent startup
				log.Warnw("Failed to create MCP toolset, skipping",
					"server", serverCfg.Name,
					"error", err)
				continue
			}

			mcpToolsets = append(mcpToolsets, mcpToolset)
			log.Infow("MCP toolset added", "server", serverCfg.Name)
		}

		if len(mcpToolsets) > 0 {
			log.Infow("MCP toolsets created successfully", "count", len(mcpToolsets))
		} else {
			log.Warn("MCP enabled but no toolsets were created successfully")
		}
	} else {
		log.Debug("MCP integration disabled")
	}

	// 8b. Initialize Keycloak client (for user identity propagation to sub-agents)
	var keycloakClient *keycloak.Client
	if cfg.Keycloak.Enabled {
		log.Infow("Initializing Keycloak client for user identity propagation",
			"server_url", cfg.Keycloak.ServerURL,
			"realm", cfg.Keycloak.Realm,
		)
		keycloakClient, err = keycloak.NewClient(keycloak.Config{
			Enabled:         cfg.Keycloak.Enabled,
			ServerURL:       cfg.Keycloak.ServerURL,
			Realm:           cfg.Keycloak.Realm,
			ClientID:        cfg.Keycloak.ClientID,
			ClientSecret:    cfg.Keycloak.ClientSecret,
			UserClaimName:   cfg.Keycloak.UserClaimName,
			GroupsClaimPath: cfg.Keycloak.GroupsClaimPath,
		})
		if err != nil {
			// Graceful degradation: log warning but don't fail agent startup
			log.Warnw("Failed to create Keycloak client", "error", err)
		}
	} else {
		log.Debug("Keycloak integration disabled")
	}

	// 8c. Create A2A toolset (if enabled) - tools for calling sub-agents without handoff
	var a2aToolset *a2a.A2AToolset
	if cfg.A2A.Enabled {
		log.Infow("A2A integration enabled (toolset mode - no handoff)",
			"self_name", cfg.A2A.SelfName,
			"sub_agents", len(cfg.A2A.SubAgents),
			"keycloak_enabled", keycloakClient != nil,
		)

		if len(cfg.A2A.SubAgents) > 0 {
			a2aToolset, err = a2a.NewA2AToolset(ctx, &cfg.A2A, keycloakClient)
			if err != nil {
				// Graceful degradation: log warning but don't fail agent startup
				log.Warnw("Failed to create A2A toolset", "error", err)
			}
		}
	} else {
		log.Debug("A2A integration disabled")
	}

	// 9. Initialize prompt manager
	log.Info("Initializing prompt manager")
	promptManager, err := NewPromptManager(&cfg.Prompt)
	if err != nil {
		memoryService.Close()
		sessionService.Close()
		return nil, fmt.Errorf("failed to create prompt manager: %w", err)
	}

	// Get base prompt from manager
	basePrompt := promptManager.GetPrompt()

	// Build complete system prompt with agent name and permission rules
	systemPromptWithPermissions := BuildSystemPrompt(basePrompt, cfg.AgentName, &cfg.Permissions)

	// 9. Initialize Langfuse tracer for observability
	log.Info("Initializing Langfuse tracer")
	langfuseTracer, err := observability.NewLangfuseTracer(&cfg.Langfuse)
	if err != nil {
		memoryService.Close()
		sessionService.Close()
		promptManager.Close()
		return nil, fmt.Errorf("failed to create langfuse tracer: %w", err)
	}

	// 10. Initialize session manager, syncer, and compactor
	sessionManager := NewSessionManager(sessionService)
	sessionSyncer := NewSessionSyncer(sessionService)
	sessionCompactor := NewSessionCompactor(cfg, sessionService)
	log.Infow("Session management initialized",
		"compact_threshold", cfg.Session.CompactThreshold,
		"compact_keep_turns", cfg.Session.CompactKeepTurns,
	)

	// 11. Create ADK agent with system prompt and toolsets
	log.Info("Creating LLM agent with permission-enforced tools")

	// Build toolsets array (base + MCP + A2A)
	toolsets := []tool.Toolset{
		memoryToolset, // Uses wrapped permission memory service
		webToolset,
	}
	toolsets = append(toolsets, mcpToolsets...)

	// Add A2A toolset if available (provides query_<agent_name> tools)
	if a2aToolset != nil {
		toolsets = append(toolsets, a2aToolset)
		log.Infow("A2A toolset added to agent", "tools_count", a2aToolset.ToolCount())
	}

	// Build the agent instance early so we can attach callbacks
	ag := &Agent{
		config:            cfg,
		sessionService:    sessionService,
		memoryService:     memoryService,
		permissionChecker: permChecker,
		promptManager:     promptManager,
		langfuseTracer:    langfuseTracer,
		sessionManager:    sessionManager,
		sessionSyncer:     sessionSyncer,
		sessionCompactor:  sessionCompactor,
		keycloakClient:    keycloakClient,
		a2aToolset:        a2aToolset,
	}

	// Build callbacks for observability (Langfuse + Prometheus)
	beforeModel, afterModel, beforeTool, afterTool := ag.buildCallbacks()

	llmAgent, err := llmagent.New(llmagent.Config{
		Name:                 AppName,
		Model:                llmModel,
		Description:          "An intelligent assistant that helps teams build and maintain their institutional knowledge base by ingesting and organizing conversation threads.",
		Instruction:          systemPromptWithPermissions,
		Toolsets:             toolsets,
		BeforeModelCallbacks: beforeModel,
		AfterModelCallbacks:  afterModel,
		BeforeToolCallbacks:  beforeTool,
		AfterToolCallbacks:   afterTool,
	})
	if err != nil {
		memoryService.Close()
		sessionService.Close()
		return nil, fmt.Errorf("failed to create agent: %w", err)
	}
	ag.llmAgent = llmAgent

	// 12. Create ADK REST handler via launcher config
	log.Info("Creating ADK REST handler")

	// SSE write timeout: use server write timeout or default to 10 minutes
	sseWriteTimeout := 10 * time.Minute
	if cfg.Server.WriteTimeout > 0 {
		sseWriteTimeout = time.Duration(cfg.Server.WriteTimeout) * time.Second
	}

	launcherCfg := &launcher.Config{
		SessionService: sessionService,
		MemoryService:  memoryService,
		AgentLoader:    agent.NewSingleLoader(llmAgent),
	}
	ag.restHandler = adkrest.NewHandler(launcherCfg, sseWriteTimeout)

	log.Info("Knowledge Agent fully initialized with ADK REST handler and callbacks")

	return ag, nil
}

// Close closes all agent resources with parallel shutdown and global timeout
func (a *Agent) Close() error {
	log := logger.Get()
	log.Info("Shutting down agent resources (parallel)")

	// Global timeout for all resource closures
	const shutdownTimeout = 5 * time.Second

	type closeResult struct {
		name string
		err  error
	}

	// Collect resources to close
	type resource struct {
		name    string
		closeFn func() error
	}
	var resources []resource

	if a.langfuseTracer != nil {
		resources = append(resources, resource{"langfuse_tracer", a.langfuseTracer.Close})
	}
	if a.promptManager != nil {
		resources = append(resources, resource{"prompt_manager", a.promptManager.Close})
	}
	if a.keycloakClient != nil {
		resources = append(resources, resource{"keycloak_client", a.keycloakClient.Close})
	}
	if a.a2aToolset != nil {
		resources = append(resources, resource{"a2a_toolset", a.a2aToolset.Close})
	}
	if a.sessionService != nil {
		resources = append(resources, resource{"session_service", a.sessionService.Close})
	}
	if a.memoryService != nil {
		resources = append(resources, resource{"memory_service", a.memoryService.Close})
	}

	if len(resources) == 0 {
		log.Info("No resources to close")
		return nil
	}

	// Close all resources in parallel
	resultCh := make(chan closeResult, len(resources))
	for _, r := range resources {
		go func(res resource) {
			err := res.closeFn()
			resultCh <- closeResult{name: res.name, err: err}
		}(r)
	}

	// Wait for all closures with global timeout
	var errors []error
	timeout := time.After(shutdownTimeout)
	completed := 0

	for completed < len(resources) {
		select {
		case result := <-resultCh:
			completed++
			if result.err != nil {
				log.Warnw("Error closing resource", "resource", result.name, "error", result.err)
				errors = append(errors, fmt.Errorf("%s: %w", result.name, result.err))
			} else {
				log.Infow("Resource closed successfully", "resource", result.name)
			}
		case <-timeout:
			remaining := len(resources) - completed
			log.Errorw("Shutdown timeout - some resources did not close",
				"timeout", shutdownTimeout,
				"remaining", remaining,
			)
			errors = append(errors, fmt.Errorf("shutdown timeout: %d resources did not close within %v", remaining, shutdownTimeout))
			// Break out of the loop - don't wait for remaining resources
			completed = len(resources)
		}
	}

	if len(errors) > 0 {
		log.Warnw("Shutdown completed with errors", "error_count", len(errors))
		return fmt.Errorf("shutdown errors: %v", errors)
	}

	log.Info("Agent resources closed successfully")
	return nil
}

// RESTHandler returns the ADK REST handler for /agent/run and /agent/run_sse
func (a *Agent) RESTHandler() http.Handler {
	return a.restHandler
}

// GetLLMAgent returns the underlying LLM agent for use with the ADK launcher
func (a *Agent) GetLLMAgent() agent.Agent {
	return a.llmAgent
}

// GetSessionService returns the session service for use with the ADK launcher
func (a *Agent) GetSessionService() *sessionredis.RedisSessionService {
	return a.sessionService
}

// GetMemoryService returns the memory service for pre-search in middleware
func (a *Agent) GetMemoryService() *memorypostgres.PostgresMemoryService {
	return a.memoryService
}

// GetLangfuseTracer returns the Langfuse tracer for middleware
func (a *Agent) GetLangfuseTracer() *observability.LangfuseTracer {
	return a.langfuseTracer
}

// GetSessionManager returns the session manager
func (a *Agent) GetSessionManager() *SessionManager {
	return a.sessionManager
}

// GetSessionSyncer returns the session syncer
func (a *Agent) GetSessionSyncer() *SessionSyncer {
	return a.sessionSyncer
}

// GetSessionCompactor returns the session compactor
func (a *Agent) GetSessionCompactor() *SessionCompactor {
	return a.sessionCompactor
}

// GetConfig returns the agent configuration
func (a *Agent) GetConfig() *config.Config {
	return a.config
}

// GetPermissionChecker returns the permission checker
func (a *Agent) GetPermissionChecker() *MemoryPermissionChecker {
	return a.permissionChecker
}

// GetKeycloakClient returns the Keycloak client for server middleware
// Returns nil if Keycloak is disabled
func (a *Agent) GetKeycloakClient() *keycloak.Client {
	return a.keycloakClient
}

// PreSearchMemory executes search_memory programmatically before the LLM loop.
// This ensures the agent always has relevant memory context before deciding what to do.
// Exported for use by the ADK pre-processing middleware.
func (a *Agent) PreSearchMemory(ctx context.Context, query, userID string) string {
	log := logger.Get()

	if strings.TrimSpace(query) == "" {
		return ""
	}

	const preSearchTimeout = 3 * time.Second
	searchCtx, cancel := context.WithTimeout(ctx, preSearchTimeout)
	defer cancel()

	startTime := time.Now()

	searchResp, err := a.memoryService.Search(searchCtx, &memory.SearchRequest{
		Query:   query,
		UserID:  userID,
		AppName: AppName,
	})

	duration := time.Since(startTime)
	observability.GetMetrics().RecordPreSearch(duration, err == nil)

	if err != nil {
		log.Warnw("Pre-search memory failed",
			"error", err,
			"query", truncateString(query, 100),
			"duration_ms", duration.Milliseconds(),
		)
		return ""
	}

	if searchResp == nil || len(searchResp.Memories) == 0 {
		log.Debugw("Pre-search memory: no results found",
			"query", truncateString(query, 100),
			"duration_ms", duration.Milliseconds(),
		)
		return "No relevant information found in memory."
	}

	const maxPreSearchResults = 5
	var sb strings.Builder
	resultCount := 0

	for i, entry := range searchResp.Memories {
		if resultCount >= maxPreSearchResults {
			break
		}
		if entry.Content != nil && len(entry.Content.Parts) > 0 {
			if entry.Content.Parts[0].Text != "" {
				sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, entry.Content.Parts[0].Text))
				resultCount++
			}
		}
	}

	resultsText := sb.String()
	if resultsText == "" {
		return "No relevant information found in memory."
	}

	log.Infow("Pre-search memory completed",
		"query", truncateString(query, 100),
		"results_count", resultCount,
		"total_found", len(searchResp.Memories),
		"duration_ms", duration.Milliseconds(),
	)

	if trace := observability.QueryTraceFromContext(ctx); trace != nil {
		trace.RecordPreSearch(query, resultCount, duration)
	}

	return resultsText
}

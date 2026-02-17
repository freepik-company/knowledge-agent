package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/memory"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"

	genaianthropic "github.com/achetronic/adk-utils-go/genai/anthropic"
	memorypostgres "github.com/achetronic/adk-utils-go/memory/postgres"
	sessionredis "github.com/achetronic/adk-utils-go/session/redis"
	memorytools "github.com/achetronic/adk-utils-go/tools/memory"
	"github.com/git-hulk/langfuse-go/pkg/traces"

	"knowledge-agent/internal/a2a"
	"knowledge-agent/internal/auth/keycloak"
	"knowledge-agent/internal/config"
	"knowledge-agent/internal/ctxutil"
	"knowledge-agent/internal/logger"
	"knowledge-agent/internal/mcp"
	"knowledge-agent/internal/observability"
	"knowledge-agent/internal/tools"
)

const (
	appName = "knowledge-agent"
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
func resolveSessionID(conversationID, channelID, threadTS string) string {
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
func resolveUserID(scope, channelID, slackUserID string) string {
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
	runner            *runner.Runner
	sessionService    *sessionredis.RedisSessionService
	memoryService     *memorypostgres.PostgresMemoryService
	contextHolder     *contextHolder
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

	// 4. Initialize context holder and permission checker EARLY
	contextHolder := &contextHolder{}
	permChecker := NewMemoryPermissionChecker(&cfg.Permissions)

	// 5. Wrap memory service with permission checking
	// This intercepts AddSession calls to enforce save_to_memory permissions
	log.Info("Wrapping memory service with permission enforcement")
	permissionMemoryService := NewPermissionMemoryService(memoryService, permChecker, contextHolder)

	// 6. Create memory toolset using adk-utils-go with wrapped service
	log.Info("Creating memory toolset (adk-utils-go)")
	memoryToolset, err := memorytools.NewToolset(memorytools.ToolsetConfig{
		MemoryService: permissionMemoryService, // Use wrapped service
		AppName:       appName,
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

	llmAgent, err := llmagent.New(llmagent.Config{
		Name:        "Knowledge Agent",
		Model:       llmModel,
		Description: "An intelligent assistant that helps teams build and maintain their institutional knowledge base by ingesting and organizing conversation threads.",
		Instruction: systemPromptWithPermissions,
		Toolsets:    toolsets,
		// SubAgents removed - now using A2A toolset instead (no handoff)
	})
	if err != nil {
		memoryService.Close()
		sessionService.Close()
		return nil, fmt.Errorf("failed to create agent: %w", err)
	}

	// 6. Create runner with both services
	log.Info("Creating agent runner")
	baseRunner, err := runner.New(runner.Config{
		AppName:        appName,
		Agent:          llmAgent,
		SessionService: sessionService,
		MemoryService:  memoryService,
	})
	if err != nil {
		memoryService.Close()
		sessionService.Close()
		return nil, fmt.Errorf("failed to create runner: %w", err)
	}

	log.Info("Knowledge Agent fully initialized with ADK and permission enforcement")

	return &Agent{
		config:            cfg,
		llmAgent:          llmAgent,
		runner:            baseRunner,
		sessionService:    sessionService,
		memoryService:     memoryService,
		contextHolder:     contextHolder,
		permissionChecker: permChecker,
		promptManager:     promptManager,
		langfuseTracer:    langfuseTracer,
		sessionManager:    sessionManager,
		sessionSyncer:     sessionSyncer,
		sessionCompactor:  sessionCompactor,
		keycloakClient:    keycloakClient,
		a2aToolset:        a2aToolset,
	}, nil
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

// GetLLMAgent returns the underlying LLM agent for use with the ADK launcher
func (a *Agent) GetLLMAgent() agent.Agent {
	return a.llmAgent
}

// GetSessionService returns the session service for use with the ADK launcher
func (a *Agent) GetSessionService() *sessionredis.RedisSessionService {
	return a.sessionService
}

// GetKeycloakClient returns the Keycloak client for server middleware
// Returns nil if Keycloak is disabled
func (a *Agent) GetKeycloakClient() *keycloak.Client {
	return a.keycloakClient
}

// buildInstruction creates the user instruction for the agent.
// Shared between Query() and QueryStream() to avoid duplication.
func (a *Agent) buildInstruction(req QueryRequest, isIngest, hasImages bool, contextStr, preSearchResults string) string {
	currentDate := time.Now().Format("Monday, January 2, 2006")

	// Build user context line
	userContext := ""
	if req.UserRealName != "" {
		userContext = fmt.Sprintf("\n**User**: %s (@%s)", req.UserRealName, req.UserName)
	} else if req.UserName != "" {
		userContext = fmt.Sprintf("\n**User**: @%s", req.UserName)
	}

	if isIngest {
		return fmt.Sprintf(`Ingest this conversation thread into the knowledge base.

**Date**: %s%s
**Thread**: %s | **Channel**: %s | **Messages**: %d

%s

Analyze, extract valuable information, save with save_to_memory, and summarize what you saved.`,
			currentDate, userContext, req.ThreadTS, req.ChannelID, len(req.Messages), contextStr)
	}

	// Standard query (with or without images)
	var sb strings.Builder
	fmt.Fprintf(&sb, "**Date**: %s%s\n", currentDate, userContext)

	if preSearchResults != "" {
		fmt.Fprintf(&sb, "\n**Memory** (pre-searched):\n%s\n", preSearchResults)
	}

	if hasImages {
		sb.WriteString("\n[Image attached]\n")
	}

	fmt.Fprintf(&sb, "\n%s", req.Query)
	return sb.String()
}

// preSearchMemory executes search_memory programmatically before the LLM loop.
// This ensures the agent always has relevant memory context before deciding what to do.
// NOTE: Uses memoryService directly (not permission-wrapped) because reads
// don't require permission checks. See PermissionMemoryService.Search().
func (a *Agent) preSearchMemory(ctx context.Context, query, userID string) string {
	log := logger.Get()

	// Skip empty or whitespace-only queries
	if strings.TrimSpace(query) == "" {
		return ""
	}

	// Pre-search should not block the main query if memory service is slow
	const preSearchTimeout = 3 * time.Second
	searchCtx, cancel := context.WithTimeout(ctx, preSearchTimeout)
	defer cancel()

	startTime := time.Now()

	// Execute search on memory service directly
	searchResp, err := a.memoryService.Search(searchCtx, &memory.SearchRequest{
		Query:   query,
		UserID:  userID,
		AppName: appName,
	})

	duration := time.Since(startTime)

	// Record pre-search metrics
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

	// Format results for context (limit to avoid token overflow)
	const maxPreSearchResults = 5
	var sb strings.Builder
	resultCount := 0

	for i, entry := range searchResp.Memories {
		if resultCount >= maxPreSearchResults {
			break
		}
		if entry.Content != nil && len(entry.Content.Parts) > 0 {
			// Extract text from the first part
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

	// Record pre-search in Langfuse trace
	if trace := observability.QueryTraceFromContext(ctx); trace != nil {
		trace.RecordPreSearch(query, resultCount, duration)
	}

	return resultsText
}

// querySetup holds the common state prepared for both Query and QueryStream.
type querySetup struct {
	isIngest    bool
	callerID    string
	slackUserID string
	sessionID   string
	userID      string
	instruction string
	userMsg     *genai.Content
	trace       *observability.QueryTrace
	ctx         context.Context
	startTime   time.Time
}

// prepareQuery performs common setup for Query and QueryStream.
// It resolves identifiers, creates the Langfuse trace, manages the session,
// syncs thread messages, compacts if needed, and builds the user instruction.
func (a *Agent) prepareQuery(ctx context.Context, req QueryRequest, streaming bool) (*querySetup, error) {
	log := logger.Get()
	startTime := time.Now()

	isIngest := req.Intent == "ingest"
	callerID := ctxutil.CallerID(ctx)
	slackUserID := ctxutil.SlackUserID(ctx)

	// Resolve session ID
	sessionID := resolveSessionID(req.ConversationID, req.ChannelID, req.ThreadTS)
	if isIngest && req.ConversationID == "" {
		sessionID = "ingest-" + sessionID
	}

	// Start Langfuse trace
	traceMetadata := map[string]any{
		"caller_id":      callerID,
		"slack_user_id":  slackUserID,
		"channel_id":     req.ChannelID,
		"thread_ts":      req.ThreadTS,
		"user_name":      req.UserName,
		"user_real_name": req.UserRealName,
		"user_email":     req.UserEmail,
		"session_id":     sessionID,
		"intent":         req.Intent,
	}
	if streaming {
		traceMetadata["streaming"] = true
	}
	trace := a.langfuseTracer.StartQueryTrace(ctx, req.Query, sessionID, traceMetadata)
	ctx = observability.ContextWithQueryTrace(ctx, trace)

	// Update context holder for permission checks
	a.contextHolder.SetContext(ctx)

	// Log request details
	canSave, permissionReason := a.permissionChecker.CanSaveToMemory(ctx)
	isEmpty := a.permissionChecker.IsEmpty()

	logFields := []any{
		"caller_id", callerID,
		"query", req.Query,
		"channel_id", req.ChannelID,
	}
	if streaming {
		logFields = append(logFields, "streaming", true)
	}
	if isIngest {
		logFields = append(logFields, "intent", "ingest", "message_count", len(req.Messages))
	}
	if req.UserName != "" {
		logFields = append(logFields, "user_name", req.UserName)
	}
	if slackUserID != "" {
		logFields = append(logFields, "slack_user_id", slackUserID)
	}
	if !isEmpty {
		logFields = append(logFields, "can_save_to_memory", canSave, "permission_reason", permissionReason)
	}
	if isIngest {
		log.Infow("Processing ingest request", logFields...)
	} else {
		log.Infow("Processing query", logFields...)
	}

	// Resolve user ID
	userID := resolveUserID(a.config.RAG.KnowledgeScope, req.ChannelID, slackUserID)

	// Get or create session
	sessionResult, err := a.sessionManager.GetOrCreate(ctx, appName, userID, sessionID)
	if err != nil {
		log.Errorw("Failed to get or create session", "error", err, "session_id", sessionID)
		trace.End(false, fmt.Sprintf("Session error: %v", err))
		return nil, fmt.Errorf("session error: %w", err)
	}

	// Sync thread messages as session events (skip for ingest)
	if !isIngest && len(req.Messages) > 0 {
		if err := a.sessionSyncer.SyncThreadMessages(ctx, sessionResult.Session, req.Messages); err != nil {
			log.Warnw("Failed to sync thread messages to session", "error", err, "session_id", sessionID)
		}
	}

	// Compact session proactively (skip for ingest)
	if !isIngest {
		if err := a.sessionCompactor.CompactIfNeeded(ctx, appName, userID, sessionID); err != nil {
			log.Warnw("Session compaction failed", "error", err, "session_id", sessionID)
		}
	}

	// Build thread context and detect images
	var contextStr string
	hasImages := false
	if len(req.Messages) > 0 {
		if isIngest {
			contextStr = a.buildThreadContextFromMessages(req.Messages)
		}
		lastMsg := req.Messages[len(req.Messages)-1]
		if images, ok := lastMsg["images"].([]any); ok && len(images) > 0 {
			hasImages = true
		}
	}

	// Pre-search memory (skip for ingest)
	var preSearchResults string
	if !isIngest {
		preSearchResults = a.preSearchMemory(ctx, req.Query, userID)
	}

	// Build instruction and user message
	instruction := a.buildInstruction(req, isIngest, hasImages, contextStr, preSearchResults)
	userMsg := a.buildContentWithImages(instruction, req.Messages)

	// Add identity to context for sub-agent propagation
	if req.UserEmail != "" {
		ctx = context.WithValue(ctx, ctxutil.UserEmailKey, req.UserEmail)
	}
	ctx = context.WithValue(ctx, ctxutil.SessionIDKey, sessionID)

	return &querySetup{
		isIngest:    isIngest,
		callerID:    callerID,
		slackUserID: slackUserID,
		sessionID:   sessionID,
		userID:      userID,
		instruction: instruction,
		userMsg:     userMsg,
		trace:       trace,
		ctx:         ctx,
		startTime:   startTime,
	}, nil
}

// logTraceSummary logs the Langfuse trace summary (shared between Query and QueryStream).
func (a *Agent) logTraceSummary(setup *querySetup, eventCount int, responseLen int) {
	log := logger.Get()

	promptTokens, completionTokens, totalTokens := setup.trace.GetAccumulatedTokens()
	totalCost := setup.trace.CalculateTotalCost(
		a.config.Anthropic.Model,
		a.config.Langfuse.InputCostPer1M,
		a.config.Langfuse.OutputCostPer1M,
	)
	traceSummary := setup.trace.GetSummary()

	log.Infow("Query trace summary",
		"trace_id", setup.trace.TraceID,
		"prompt_tokens", promptTokens,
		"completion_tokens", completionTokens,
		"total_tokens", totalTokens,
		"total_cost_usd", fmt.Sprintf("$%.6f", totalCost),
		"tool_calls_count", traceSummary["tool_calls_count"],
		"generations_count", traceSummary["generations_count"],
	)

	log.Infow("Query completed successfully",
		"caller_id", setup.callerID,
		"total_events", eventCount,
		"response_length", responseLen,
	)
}

// Query handles question answering and thread ingestion using the knowledge base.
func (a *Agent) Query(ctx context.Context, req QueryRequest) (*QueryResponse, error) {
	log := logger.Get()

	setup, err := a.prepareQuery(ctx, req, false)
	if err != nil {
		return nil, err
	}
	ctx = setup.ctx

	defer func() {
		setup.trace.End(true, "")
	}()
	defer func() {
		if setup.isIngest {
			observability.GetMetrics().RecordIngest(time.Since(setup.startTime), nil)
		} else {
			observability.GetMetrics().RecordQuery(time.Since(setup.startTime), nil)
		}
	}()

	// Run agent
	var responseText string
	var generationOutput string
	var currentGeneration *traces.Observation
	toolStartTimes := make(map[string]time.Time)
	eventCount := 0

	log.Infow("Running agent", "user_id", setup.userID, "session_id", setup.sessionID, "ingest", setup.isIngest)

	// Retry loop for handling corrupted sessions
	maxRetries := 1 // Retry once if session is corrupted
	for attempt := 0; attempt <= maxRetries; attempt++ {
		var runnerErr error
		shouldRetry := false

		for event, err := range a.runner.Run(ctx, setup.userID, setup.sessionID, setup.userMsg, agent.RunConfig{}) {
			if err != nil {
				runnerErr = err
				break
			}

			eventCount++

			// Check for usage metadata in the event
			var usageTokens *genai.GenerateContentResponseUsageMetadata
			if event.UsageMetadata != nil {
				usageTokens = event.UsageMetadata
			}

			log.Debugw("Runner event received",
				"event_number", eventCount,
				"has_content", event.Content != nil,
				"error_code", event.ErrorCode,
				"has_usage", usageTokens != nil,
			)

			if event.ErrorCode != "" {
				log.Errorw("Event error during query",
					"error_code", event.ErrorCode,
					"error_message", event.ErrorMessage,
				)
				setup.trace.End(false, fmt.Sprintf("Agent error: %s - %s", event.ErrorCode, event.ErrorMessage))
				return nil, fmt.Errorf("agent error: %s - %s", event.ErrorCode, event.ErrorMessage)
			}

			// Process event content
			if event.Content != nil {
				// Start generation if we have token usage (indicates LLM call)
				if usageTokens != nil && currentGeneration == nil {
					currentGeneration = setup.trace.StartGeneration(a.config.Anthropic.Model, setup.instruction)
					generationOutput = "" // Reset for new generation
				}

				// Check if this event contains tool calls (FunctionCall).
				// Text in events with tool calls is "thinking out loud" (e.g., "Let me search..."),
				// not the actual answer. Only accumulate text from pure-text events (the final response).
				eventHasToolCall := false
				for _, part := range event.Content.Parts {
					if part.FunctionCall != nil {
						eventHasToolCall = true
						break
					}
				}

				for _, part := range event.Content.Parts {
					// Text content: only accumulate for the response if no tool call in this event
					if part.Text != "" {
						if !eventHasToolCall {
							responseText += part.Text
						}
						// Always track for Langfuse generation output
						if currentGeneration != nil {
							generationOutput += part.Text
						}
					}

					// Tool call
					if part.FunctionCall != nil {
						toolID := part.FunctionCall.ID
						toolName := part.FunctionCall.Name
						trackKey := toolID
						if trackKey == "" {
							trackKey = toolName
						}
						toolStartTimes[trackKey] = time.Now()

						log.Infow("Tool call started",
							"tool", toolName,
							"tool_id", toolID,
							"args_count", len(part.FunctionCall.Args),
						)
						setup.trace.StartToolCall(toolID, toolName, part.FunctionCall.Args)
					}

					// Tool response
					if part.FunctionResponse != nil {
						toolID := part.FunctionResponse.ID
						toolName := part.FunctionResponse.Name
						trackKey := toolID
						if trackKey == "" {
							trackKey = toolName
						}

						var duration time.Duration
						hadStartTime := false
						if st, ok := toolStartTimes[trackKey]; ok {
							duration = time.Since(st)
							delete(toolStartTimes, trackKey)
							hadStartTime = true
						}

						if !hadStartTime {
							setup.trace.StartToolCall(toolID, toolName, nil)
						}

						success := !containsError(part.FunctionResponse.Response)

						log.Infow("Tool call completed",
							"tool", toolName,
							"tool_id", toolID,
							"duration_ms", duration.Milliseconds(),
							"success", success,
						)

						observability.GetMetrics().RecordToolCall(toolName, duration, success)
						setup.trace.EndToolCall(toolID, toolName, part.FunctionResponse.Response, nil)
					}
				}

				// End generation if we have token usage
				if usageTokens != nil && currentGeneration != nil {
					promptTokens := int(usageTokens.PromptTokenCount)
					completionTokens := int(usageTokens.CandidatesTokenCount)
					setup.trace.EndGeneration(currentGeneration, generationOutput, promptTokens, completionTokens)
					currentGeneration = nil
					generationOutput = ""
				}
			}
		}

		// Handle runner errors
		if runnerErr != nil {
			if isOrphanedToolCallError(runnerErr) {
				log.Warnw("Detected orphaned tool call error",
					"error", runnerErr,
					"session_id", setup.sessionID,
					"attempt", attempt,
				)
				setup.trace.RecordSessionRepair(setup.sessionID, attempt)

				if err := deleteCorruptedSession(ctx, a.sessionService, appName, setup.userID, setup.sessionID); err != nil {
					log.Errorw("Failed to delete corrupted session", "error", err, "session_id", setup.sessionID)
				}

				if attempt < maxRetries {
					log.Infow("Retrying after session repair", "session_id", setup.sessionID, "attempt", attempt+1)
					shouldRetry = true
					responseText = ""
					eventCount = 0
				}
			}

			if !shouldRetry && isContextOverflowError(runnerErr) {
				log.Warnw("Detected context overflow, compacting session",
					"error", runnerErr,
					"session_id", setup.sessionID,
					"attempt", attempt,
				)

				if err := a.sessionCompactor.Compact(ctx, appName, setup.userID, setup.sessionID); err != nil {
					log.Errorw("Failed to compact session after overflow", "error", err, "session_id", setup.sessionID)
				} else if attempt < maxRetries {
					log.Infow("Retrying after session compaction", "session_id", setup.sessionID, "attempt", attempt+1)
					shouldRetry = true
					responseText = ""
					eventCount = 0
				}
			}

			if !shouldRetry {
				setup.trace.End(false, fmt.Sprintf("Runner error: %v", runnerErr))
				return nil, fmt.Errorf("agent error: %w", runnerErr)
			}
		}

		// Break out of retry loop if no error or not retrying
		if !shouldRetry {
			break
		}
	}

	a.logTraceSummary(setup, eventCount, len(responseText))
	setup.trace.End(true, responseText)

	return &QueryResponse{
		Success: true,
		Answer:  responseText,
	}, nil
}

// QueryStream handles streaming query responses via SSE.
// It uses a callback-based approach: onEvent is called for each SSE event.
func (a *Agent) QueryStream(ctx context.Context, req QueryRequest, onEvent func(StreamEvent)) error {
	log := logger.Get()

	setup, err := a.prepareQuery(ctx, req, true)
	if err != nil {
		onEvent(StreamEvent{EventType: "error", Data: map[string]any{"message": "Session error"}})
		return err
	}
	ctx = setup.ctx

	defer func() {
		setup.trace.End(true, "")
	}()
	defer func() {
		if setup.isIngest {
			observability.GetMetrics().RecordIngest(time.Since(setup.startTime), nil)
		} else {
			observability.GetMetrics().RecordQuery(time.Since(setup.startTime), nil)
		}
	}()

	// Emit session_id event
	onEvent(StreamEvent{EventType: "session_id", Data: map[string]any{"session_id": setup.sessionID}})

	// Run agent with streaming
	var responseText string
	var generationOutput string
	var currentGeneration *traces.Observation
	toolStartTimes := make(map[string]time.Time)
	eventCount := 0

	log.Infow("Running agent (stream)", "user_id", setup.userID, "session_id", setup.sessionID, "ingest", setup.isIngest)

	// Retry loop for corrupted sessions
	maxRetries := 1
	for attempt := 0; attempt <= maxRetries; attempt++ {
		var runnerErr error
		shouldRetry := false

		for event, err := range a.runner.Run(ctx, setup.userID, setup.sessionID, setup.userMsg, agent.RunConfig{
			StreamingMode: agent.StreamingModeSSE,
		}) {
			if ctx.Err() != nil {
				log.Infow("Client disconnected during stream", "session_id", setup.sessionID)
				return ctx.Err()
			}

			if err != nil {
				runnerErr = err
				break
			}

			eventCount++

			var usageTokens *genai.GenerateContentResponseUsageMetadata
			if event.UsageMetadata != nil {
				usageTokens = event.UsageMetadata
			}

			if event.ErrorCode != "" {
				log.Errorw("Event error during stream",
					"error_code", event.ErrorCode,
					"error_message", event.ErrorMessage,
				)
				onEvent(StreamEvent{EventType: "error", Data: map[string]any{"message": fmt.Sprintf("Agent error: %s - %s", event.ErrorCode, event.ErrorMessage)}})
				setup.trace.End(false, fmt.Sprintf("Agent error: %s - %s", event.ErrorCode, event.ErrorMessage))
				return fmt.Errorf("agent error: %s - %s", event.ErrorCode, event.ErrorMessage)
			}

			if event.Content != nil {
				if usageTokens != nil && currentGeneration == nil {
					currentGeneration = setup.trace.StartGeneration(a.config.Anthropic.Model, setup.instruction)
					generationOutput = ""
				}

				for _, part := range event.Content.Parts {
					// Only stream partial deltas to avoid duplication with aggregated events
					if part.Text != "" && event.Partial {
						responseText += part.Text
						if currentGeneration != nil {
							generationOutput += part.Text
						}
						onEvent(StreamEvent{EventType: "content_delta", Data: map[string]any{"text": part.Text}})
					}

					// Tool call
					if part.FunctionCall != nil {
						toolID := part.FunctionCall.ID
						toolName := part.FunctionCall.Name
						trackKey := toolID
						if trackKey == "" {
							trackKey = toolName
						}
						toolStartTimes[trackKey] = time.Now()

						log.Infow("Tool call started (stream)", "tool", toolName, "tool_id", toolID)
						setup.trace.StartToolCall(toolID, toolName, part.FunctionCall.Args)

						onEvent(StreamEvent{EventType: "tool_start", Data: map[string]any{"tool_id": toolID, "tool_name": toolName}})
						onEvent(StreamEvent{EventType: "tool_input", Data: map[string]any{"tool_id": toolID, "tool_name": toolName, "args": part.FunctionCall.Args}})
					}

					// Tool response
					if part.FunctionResponse != nil {
						toolID := part.FunctionResponse.ID
						toolName := part.FunctionResponse.Name
						trackKey := toolID
						if trackKey == "" {
							trackKey = toolName
						}

						var duration time.Duration
						hadStartTime := false
						if st, ok := toolStartTimes[trackKey]; ok {
							duration = time.Since(st)
							delete(toolStartTimes, trackKey)
							hadStartTime = true
						}

						if !hadStartTime {
							setup.trace.StartToolCall(toolID, toolName, nil)
						}

						isError := containsError(part.FunctionResponse.Response)

						log.Infow("Tool call completed (stream)",
							"tool", toolName,
							"tool_id", toolID,
							"duration_ms", duration.Milliseconds(),
							"success", !isError,
						)

						observability.GetMetrics().RecordToolCall(toolName, duration, !isError)
						setup.trace.EndToolCall(toolID, toolName, part.FunctionResponse.Response, nil)

						onEvent(StreamEvent{EventType: "tool_result", Data: map[string]any{
							"tool_id":   toolID,
							"tool_name": toolName,
							"result":    part.FunctionResponse.Response,
							"is_error":  isError,
							"duration":  duration.Seconds(),
						}})
					}
				}

				if usageTokens != nil && currentGeneration != nil {
					promptTokens := int(usageTokens.PromptTokenCount)
					completionTokens := int(usageTokens.CandidatesTokenCount)
					setup.trace.EndGeneration(currentGeneration, generationOutput, promptTokens, completionTokens)
					currentGeneration = nil
					generationOutput = ""
				}
			}
		}

		// Handle runner errors
		if runnerErr != nil {
			if isOrphanedToolCallError(runnerErr) {
				log.Warnw("Detected orphaned tool call error (stream)",
					"error", runnerErr,
					"session_id", setup.sessionID,
					"attempt", attempt,
				)
				setup.trace.RecordSessionRepair(setup.sessionID, attempt)

				if err := deleteCorruptedSession(ctx, a.sessionService, appName, setup.userID, setup.sessionID); err != nil {
					log.Errorw("Failed to delete corrupted session", "error", err, "session_id", setup.sessionID)
				}

				if attempt < maxRetries {
					log.Infow("Retrying stream after session repair", "session_id", setup.sessionID, "attempt", attempt+1)
					shouldRetry = true
					responseText = ""
					eventCount = 0
					onEvent(StreamEvent{EventType: "session_id", Data: map[string]any{"session_id": setup.sessionID}})
				}
			}

			if !shouldRetry && isContextOverflowError(runnerErr) {
				log.Warnw("Detected context overflow in stream, compacting session",
					"error", runnerErr,
					"session_id", setup.sessionID,
					"attempt", attempt,
				)

				if err := a.sessionCompactor.Compact(ctx, appName, setup.userID, setup.sessionID); err != nil {
					log.Errorw("Failed to compact session after overflow", "error", err, "session_id", setup.sessionID)
				} else if attempt < maxRetries {
					log.Infow("Retrying stream after session compaction", "session_id", setup.sessionID, "attempt", attempt+1)
					shouldRetry = true
					responseText = ""
					eventCount = 0
					onEvent(StreamEvent{EventType: "session_id", Data: map[string]any{"session_id": setup.sessionID}})
				}
			}

			if !shouldRetry {
				onEvent(StreamEvent{EventType: "error", Data: map[string]any{"message": "Internal server error"}})
				setup.trace.End(false, fmt.Sprintf("Runner error: %v", runnerErr))
				return fmt.Errorf("agent error: %w", runnerErr)
			}
		}

		if !shouldRetry {
			break
		}
	}

	a.logTraceSummary(setup, eventCount, len(responseText))
	setup.trace.End(true, responseText)

	onEvent(StreamEvent{EventType: "end", Data: map[string]any{"session_id": setup.sessionID}})

	return nil
}

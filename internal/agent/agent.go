package agent

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"

	genaianthropic "github.com/achetronic/adk-utils-go/genai/anthropic"
	memorypostgres "github.com/achetronic/adk-utils-go/memory/postgres"
	sessionredis "github.com/achetronic/adk-utils-go/session/redis"
	memorytools "github.com/achetronic/adk-utils-go/tools/memory"
	"github.com/git-hulk/langfuse-go/pkg/traces"

	"knowledge-agent/internal/a2a"
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

// resolveSessionID determines the session_id based on available context
// Priority:
// 1. Client-provided session_id -> use it directly
// 2. channel_id + thread_ts -> "thread-{channel}-{thread_ts}" (maintains context per thread)
// 3. channel_id only -> "channel-{channel}-{timestamp}"
// 4. No Slack context -> "api-{timestamp}"
func resolveSessionID(clientSessionID, channelID, threadTS string) string {
	// 1. Client-provided session_id takes precedence
	if clientSessionID != "" {
		return clientSessionID
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
	responseCleaner   *ResponseCleaner
	contextSummarizer *ContextSummarizer
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

	// 8b. Create A2A sub-agents using remoteagent (if enabled)
	var subAgents []agent.Agent
	if cfg.A2A.Enabled {
		log.Infow("A2A integration enabled",
			"self_name", cfg.A2A.SelfName,
			"sub_agents", len(cfg.A2A.SubAgents),
		)

		if len(cfg.A2A.SubAgents) > 0 {
			subAgents, err = a2a.CreateSubAgents(&cfg.A2A)
			if err != nil {
				// Graceful degradation: log warning but don't fail agent startup
				log.Warnw("Failed to create A2A sub-agents", "error", err)
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

	// 10. Initialize response cleaner (uses Haiku to clean agent narration)
	responseCleaner := NewResponseCleaner(cfg)
	if cfg.ResponseCleaner.Enabled {
		log.Infow("Response cleaner enabled", "model", cfg.ResponseCleaner.Model)
	}

	// 10b. Initialize context summarizer (uses Haiku to compress long contexts)
	contextSummarizer := NewContextSummarizer(cfg)
	if cfg.ContextSummarizer.Enabled {
		log.Infow("Context summarizer enabled",
			"model", cfg.ContextSummarizer.Model,
			"token_threshold", cfg.ContextSummarizer.TokenThreshold,
		)
	}

	// 11. Create ADK agent with system prompt and toolsets
	log.Info("Creating LLM agent with permission-enforced tools")

	// Build toolsets array (base + MCP)
	toolsets := []tool.Toolset{
		memoryToolset, // Uses wrapped permission memory service
		webToolset,
	}
	toolsets = append(toolsets, mcpToolsets...)

	llmAgent, err := llmagent.New(llmagent.Config{
		Name:        "Knowledge Agent",
		Model:       llmModel,
		Description: "An intelligent assistant that helps teams build and maintain their institutional knowledge base by ingesting and organizing conversation threads.",
		Instruction: systemPromptWithPermissions,
		Toolsets:    toolsets,
		SubAgents:   subAgents, // A2A remote agents via remoteagent.NewA2A
	})
	if err != nil {
		memoryService.Close()
		sessionService.Close()
		return nil, fmt.Errorf("failed to create agent: %w", err)
	}

	// 6. Create runner with both services
	log.Info("Creating agent runner")
	runnr, err := runner.New(runner.Config{
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
		runner:            runnr,
		sessionService:    sessionService,
		memoryService:     memoryService,
		contextHolder:     contextHolder,
		permissionChecker: permChecker,
		promptManager:     promptManager,
		langfuseTracer:    langfuseTracer,
		responseCleaner:   responseCleaner,
		contextSummarizer: contextSummarizer,
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

// GetConfig returns the agent configuration
func (a *Agent) GetConfig() *config.Config {
	return a.config
}

// IngestThread handles thread ingestion using ADK agent
func (a *Agent) IngestThread(ctx context.Context, req IngestRequest) (*IngestResponse, error) {
	log := logger.Get()

	// Extract caller information from context
	callerID := ctxutil.CallerID(ctx)
	slackUserID := ctxutil.SlackUserID(ctx)

	// Update context holder for permission checks
	a.contextHolder.SetContext(ctx)

	// Check if user has save permissions and log it
	canSave, permissionReason := a.permissionChecker.CanSaveToMemory(ctx)
	isEmpty := a.permissionChecker.IsEmpty()

	logFields := []any{
		"caller_id", callerID,
		"thread_ts", req.ThreadTS,
		"channel_id", req.ChannelID,
		"message_count", len(req.Messages),
	}
	if slackUserID != "" {
		logFields = append(logFields, "slack_user_id", slackUserID)
	}
	if !isEmpty {
		logFields = append(logFields, "can_save_to_memory", canSave, "permission_reason", permissionReason)
	}

	log.Infow("Starting thread ingestion via ADK", logFields...)

	// Resolve session ID (client-provided or auto-generated from thread context)
	sessionID := resolveSessionID(req.SessionID, req.ChannelID, req.ThreadTS)
	// For ingestion, add prefix to distinguish from query sessions
	if req.SessionID == "" {
		sessionID = "ingest-" + sessionID
	}

	// Determine user_id based on knowledge_scope configuration
	userID := resolveUserID(a.config.RAG.KnowledgeScope, req.ChannelID, slackUserID)

	// Create new session
	_, err := a.sessionService.Create(ctx, &session.CreateRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err != nil {
		log.Warnw("Failed to create session (may already exist)", "error", err)
	}

	// Build thread context for the agent
	threadContext := a.buildThreadContext(req)

	// Get current date for temporal context
	currentDate := time.Now().Format("Monday, January 2, 2006")

	// Build permission context for LLM if permissions are configured
	permissionContext := ""
	if !isEmpty {
		permissionContext = fmt.Sprintf("\n**Current Request Context**:\n- Caller ID: %s\n- Slack User ID: %s\n- Can Save to Memory: %t\n", callerID, slackUserID, canSave)
	}

	// Create instruction for the agent
	instruction := fmt.Sprintf(`You are receiving a conversation thread to ingest into the knowledge base.

**Current Date**: %s%s

Thread Information:
- Thread ID: %s
- Channel: %s
- Number of messages: %d

Here is the complete conversation thread:

%s

Your task:
1. Analyze this conversation carefully
2. Identify all important information, decisions, solutions, or insights
3. Use the save_to_memory tool to store each piece of valuable information
4. When saving, include the date if the conversation contains temporal references (e.g., "esta semana", "hoy", "last week")
5. After saving everything, provide a summary of what you saved

Please begin the ingestion now.`, currentDate, permissionContext, req.ThreadTS, req.ChannelID, len(req.Messages), threadContext)

	// Create user message with the thread content
	userMsg := genai.NewContentFromText(instruction, genai.RoleUser)

	// Run agent to process and save the thread
	var responseText string
	var memoriesSaved int

	log.Info("Running agent for thread ingestion")
	eventCount := 0
	for event, err := range a.runner.Run(ctx, userID, sessionID, userMsg, agent.RunConfig{}) {
		if err != nil {
			log.Errorw("Runner error during ingestion", "error", err)
			return nil, fmt.Errorf("ingestion failed: %w", err)
		}

		eventCount++
		log.Debugw("Ingestion event received",
			"event_number", eventCount,
			"has_content", event.Content != nil,
			"error_code", event.ErrorCode,
		)

		if event.ErrorCode != "" {
			log.Errorw("Event error during ingestion",
				"error_code", event.ErrorCode,
				"error_message", event.ErrorMessage,
			)
			return nil, fmt.Errorf("agent error: %s - %s", event.ErrorCode, event.ErrorMessage)
		}

		// Process event content
		if event.Content != nil {
			log.Debugw("Processing ingestion event content",
				"parts_count", len(event.Content.Parts),
				"role", event.Content.Role,
			)

			// Collect response text
			if len(event.Content.Parts) > 0 && event.Content.Parts[0].Text != "" {
				responseText += event.Content.Parts[0].Text
			}

			// Count and log memory saves
			for _, part := range event.Content.Parts {
				if part.FunctionCall != nil {
					log.Infow("Agent calling tool during ingestion",
						"tool", part.FunctionCall.Name,
					)
					if part.FunctionCall.Name == "save_to_memory" {
						memoriesSaved++
						log.Debugw("Memory save detected",
							"total_saves", memoriesSaved,
						)
					}
				}
				if part.FunctionResponse != nil {
					log.Debugw("Tool response during ingestion",
						"tool", part.FunctionResponse.Name,
					)
				}
			}
		}
	}

	completionLogFields := []any{
		"caller_id", callerID,
		"memories_saved", memoriesSaved,
		"total_events", eventCount,
		"response_length", len(responseText),
	}
	if slackUserID != "" {
		completionLogFields = append(completionLogFields, "slack_user_id", slackUserID)
	}
	log.Infow("Thread ingestion completed", completionLogFields...)

	return &IngestResponse{
		Success:       true,
		Message:       responseText,
		MemoriesAdded: memoriesSaved,
	}, nil
}

// Query handles question answering using the knowledge base
func (a *Agent) Query(ctx context.Context, req QueryRequest) (*QueryResponse, error) {
	log := logger.Get()
	startTime := time.Now()

	// Extract caller information from context
	callerID := ctxutil.CallerID(ctx)
	slackUserID := ctxutil.SlackUserID(ctx)

	// Start Langfuse trace
	trace := a.langfuseTracer.StartQueryTrace(ctx, req.Question, map[string]any{
		"caller_id":      callerID,
		"slack_user_id":  slackUserID,
		"channel_id":     req.ChannelID,
		"thread_ts":      req.ThreadTS,
		"user_name":      req.UserName,
		"user_real_name": req.UserRealName,
	})
	defer func() {
		// Finalize trace at the end
		trace.End(true, "")
	}()

	// Update context holder for permission checks
	a.contextHolder.SetContext(ctx)

	// Check if user has save permissions and log it
	canSave, permissionReason := a.permissionChecker.CanSaveToMemory(ctx)
	isEmpty := a.permissionChecker.IsEmpty()

	logFields := []any{
		"caller_id", callerID,
		"question", req.Question,
		"channel_id", req.ChannelID,
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

	log.Infow("Processing query", logFields...)

	// Record query metrics
	defer func() {
		observability.GetMetrics().RecordQuery(time.Since(startTime), nil)
	}()

	// Resolve session ID (client-provided or auto-generated from thread context)
	sessionID := resolveSessionID(req.SessionID, req.ChannelID, req.ThreadTS)

	// Determine user_id based on knowledge_scope configuration
	userID := resolveUserID(a.config.RAG.KnowledgeScope, req.ChannelID, slackUserID)

	// Create new session
	_, err := a.sessionService.Create(ctx, &session.CreateRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err != nil {
		log.Warnw("Failed to create session (may already exist)", "error", err)
	}

	// Build context from current thread if available
	var contextStr string
	hasImages := false
	if len(req.Messages) > 0 {
		contextStr = a.buildThreadContextFromMessages(req.Messages)
		// Check if there are images in the last message
		lastMsg := req.Messages[len(req.Messages)-1]
		if images, ok := lastMsg["images"].([]any); ok && len(images) > 0 {
			hasImages = true
		}
	}

	// Summarize context if it exceeds token threshold
	if a.contextSummarizer != nil && a.contextSummarizer.ShouldSummarize(contextStr) {
		summarizedContext, err := a.contextSummarizer.Summarize(ctx, contextStr)
		if err != nil {
			log.Warnw("Context summarization failed, using original", "error", err)
		} else {
			contextStr = summarizedContext
		}
	}

	// Get current date for temporal context
	currentDate := time.Now().Format("Monday, January 2, 2006")

	// Build permission context for LLM if permissions are configured
	permissionContext := ""
	if !isEmpty {
		permissionContext = fmt.Sprintf("\n**Current Request Context**:\n- Caller ID: %s\n- Slack User ID: %s\n- Can Save to Memory: %t\n", callerID, slackUserID, canSave)
	}

	// Build user greeting if name available
	userGreeting := ""
	if req.UserRealName != "" {
		userGreeting = fmt.Sprintf("\n**User**: %s (Slack: @%s)\n", req.UserRealName, req.UserName)
	} else if req.UserName != "" {
		userGreeting = fmt.Sprintf("\n**User**: @%s\n", req.UserName)
	}

	// Create instruction for the agent
	var instruction string
	if hasImages {
		// Special instruction when images are present
		instruction = fmt.Sprintf(`You are a Knowledge Assistant. The user has shared an image with this message in a technical/business context.

**Current Date**: %s%s%s

Current Thread Context:
%s

User Message: %s

**IMPORTANT**: There is an image attached to this message. Please:
1. ANALYZE the image focusing on technical/business content:
   - Architecture diagrams: Identify components, services, databases, connections, data flows
   - Error screenshots: Extract error messages, stack traces, error codes, affected systems
   - Infrastructure diagrams: Note servers, networks, IPs, ports, deployment configurations
   - Code/Config screenshots: Capture code snippets, configurations, command outputs
   - Workflow diagrams: Document process steps, decision points, actors
   - Documentation: Extract key technical concepts, APIs, specifications

2. If the user is documenting something (e.g., "Esta es nuestra arquitectura", "This error is blocking us"), use save_to_memory to store:
   - Clear description of what the image shows
   - ALL visible text, labels, error messages, component names
   - Technical relationships and connections
   - Context provided by the user

3. If the user is asking a question, search memory first, then analyze the current image

4. Always respond in the same language the user is using

Please analyze the image and provide your response now.`, currentDate, permissionContext, userGreeting, contextStr, req.Question)
	} else if contextStr != "" {
		instruction = fmt.Sprintf(`You are a Knowledge Assistant helping to answer a question.

**Current Date**: %s%s%s

Current Thread Context:
%s

Question: %s

Please answer the question by:
1. Using search_memory to find relevant information from past conversations
2. Considering the current thread context if relevant
3. Providing a clear, helpful answer based on available knowledge
4. If you can't find relevant information in memory, say so and provide a general answer if possible

Please provide your answer now.`, currentDate, permissionContext, userGreeting, contextStr, req.Question)
	} else {
		instruction = fmt.Sprintf(`You are a Knowledge Assistant helping to answer a question.

**Current Date**: %s%s%s

Question: %s

Please answer the question by:
1. Using search_memory to find relevant information from past conversations
2. Providing a clear, helpful answer based on available knowledge
3. If you can't find relevant information in memory, say so

Please provide your answer now.`, currentDate, permissionContext, userGreeting, req.Question)
	}

	// Create user message with images if available
	userMsg := a.buildContentWithImages(instruction, req.Messages)

	// Log content structure for debugging
	log.Debugw("Content structure",
		"parts_count", len(userMsg.Parts),
	)
	for i, part := range userMsg.Parts {
		if part.Text != "" {
			log.Debugw("Content part", "index", i, "type", "text", "length", len(part.Text))
		} else if part.InlineData != nil {
			log.Debugw("Content part", "index", i, "type", "image", "mime", part.InlineData.MIMEType, "bytes", len(part.InlineData.Data))
		}
	}

	// Run agent to answer the question
	var responseText string     // Accumulates full response for final answer
	var generationOutput string // Tracks current generation's output only
	var currentGeneration *traces.Observation

	log.Infow("Running agent for query", "user_id", userID, "session_id", sessionID)
	eventCount := 0
	for event, err := range a.runner.Run(ctx, userID, sessionID, userMsg, agent.RunConfig{}) {
		if err != nil {
			log.Errorw("Runner error during query", "error", err)
			trace.End(false, fmt.Sprintf("Query failed: %v", err))
			return nil, fmt.Errorf("query failed: %w", err)
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
			trace.End(false, fmt.Sprintf("Agent error: %s - %s", event.ErrorCode, event.ErrorMessage))
			return nil, fmt.Errorf("agent error: %s - %s", event.ErrorCode, event.ErrorMessage)
		}

		// Process event content
		if event.Content != nil {
			log.Debugw("Processing event content",
				"parts_count", len(event.Content.Parts),
				"role", event.Content.Role,
			)

			// Start generation if we have token usage (indicates LLM call)
			if usageTokens != nil && currentGeneration == nil {
				currentGeneration = trace.StartGeneration(a.config.Anthropic.Model, instruction)
				generationOutput = "" // Reset for new generation
			}

			for i, part := range event.Content.Parts {
				// Text content
				if part.Text != "" {
					log.Debugw("Text part",
						"index", i,
						"length", len(part.Text),
						"preview", truncateString(part.Text, 100),
					)

					// Collect response text for final answer
					responseText += part.Text

					// Track this generation's output separately
					if currentGeneration != nil {
						generationOutput += part.Text
					}
				}

				// Tool call
				if part.FunctionCall != nil {
					log.Infow("Agent calling tool",
						"tool", part.FunctionCall.Name,
						"args_count", len(part.FunctionCall.Args),
					)

					// Track tool call in Langfuse
					trace.StartToolCall(part.FunctionCall.Name, part.FunctionCall.Args)
				}

				// Tool response
				if part.FunctionResponse != nil {
					log.Infow("Tool response received",
						"tool", part.FunctionResponse.Name,
						"has_response", part.FunctionResponse.Response != nil,
					)

					// Log detailed response for A2A debugging
					if part.FunctionResponse.Name == "transfer_to_agent" {
						log.Debugw("transfer_to_agent response details",
							"response", part.FunctionResponse.Response,
						)
					}

					// End tool call in Langfuse
					trace.EndToolCall(part.FunctionResponse.Name, part.FunctionResponse.Response, nil)
				}
			}

			// End generation if we have token usage
			if usageTokens != nil && currentGeneration != nil {
				promptTokens := int(usageTokens.PromptTokenCount)
				completionTokens := int(usageTokens.CandidatesTokenCount)

				// Pass only this generation's output, not accumulated text
				trace.EndGeneration(
					currentGeneration,
					generationOutput,
					promptTokens,
					completionTokens,
				)

				currentGeneration = nil // Reset for next generation
				generationOutput = ""   // Reset output tracker
			}
		}
	}

	// Calculate total cost and log summary
	promptTokens, completionTokens, totalTokens := trace.GetAccumulatedTokens()
	totalCost := trace.CalculateTotalCost(
		a.config.Anthropic.Model,
		a.config.Langfuse.InputCostPer1M,
		a.config.Langfuse.OutputCostPer1M,
	)
	traceSummary := trace.GetSummary()

	traceSummaryFields := []any{
		"trace_id", trace.TraceID,
		"prompt_tokens", promptTokens,
		"completion_tokens", completionTokens,
		"total_tokens", totalTokens,
		"total_cost_usd", fmt.Sprintf("$%.6f", totalCost),
		"tool_calls_count", traceSummary["tool_calls_count"],
		"generations_count", traceSummary["generations_count"],
	}
	if req.UserName != "" {
		traceSummaryFields = append(traceSummaryFields, "user_name", req.UserName)
	}
	log.Infow("Query trace summary", traceSummaryFields...)

	completionLogFields := []any{
		"caller_id", callerID,
		"total_events", eventCount,
		"response_length", len(responseText),
	}
	if req.UserName != "" {
		completionLogFields = append(completionLogFields, "user_name", req.UserName)
	}
	if slackUserID != "" {
		completionLogFields = append(completionLogFields, "slack_user_id", slackUserID)
	}
	log.Infow("Query completed successfully", completionLogFields...)

	// End Langfuse trace
	trace.End(true, responseText)

	// Clean response if enabled (removes agent narration, keeps substantive content)
	finalResponse := responseText
	if a.responseCleaner != nil {
		cleanedResponse, err := a.responseCleaner.Clean(ctx, responseText)
		if err != nil {
			log.Warnw("Response cleaning failed, using original", "error", err)
		} else {
			finalResponse = cleanedResponse
		}
	}

	return &QueryResponse{
		Success: true,
		Answer:  finalResponse,
	}, nil
}

# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## System Overview

Knowledge Agent is an intelligent AI assistant that helps teams capture and retrieve institutional knowledge from Slack conversations. It uses Claude Sonnet 4.5 via ADK (Agent Development Kit) with PostgreSQL+pgvector for semantic search.

**Interaction model:**
- Users mention the bot naturally: `@bot <message>`
- Can attach images (screenshots, diagrams, charts) for visual analysis
- Can share URLs for content fetching and analysis
- The LLM intelligently decides whether to:
  - Search the knowledge base (search_memory tool)
  - Save valuable information (save_to_memory tool)
  - Fetch and analyze URLs (fetch_url tool)
  - Just respond conversationally
  - Or do multiple things at once
- Automatically responds in the user's language (Spanish, English, etc.)

**Architecture (unified server design):**
```
                    knowledge-agent
                          │
        ┌─────────────────┴─────────────────┐
        │                                   │
   Port 8081                          Slack Bridge
   (Unified Server)                      (:8080)
        │                                   │
   ┌────┴────────────────┐                 │
   │/api/query (auth)    │                 │
   │/api/ingest (auth)   │                 │
   │/a2a/invoke (auth)   │                 │
   │/.well-known/agent-card (public)       │
   │/health, /metrics (public)             │
   └─────────────────────┘                 │
        │                                   │
        └─────────────────┬─────────────────┘
                          │
                    ADK Agent
                    - llmagent + tools
                    - sub-agents (A2A)
                    - session service
                          │
                    PostgreSQL+pgvector
```

**Port breakdown:**
- **8080**: Slack Webhook Bridge (receives Slack events)
- **8081**: Unified HTTP server (all endpoints with proper auth)

## Development Commands

### Setup and Infrastructure
```bash
# Initial setup (required for adk-utils-go dependency)
go mod download && go mod tidy

# Start infrastructure (PostgreSQL, Redis, Ollama)
make docker-up


# Check service health
make docker-health
```

### Running Services

**Two modes available:**

**Socket Mode (Development):**
```bash
# Set in .env:
SLACK_MODE=socket
SLACK_APP_TOKEN=xapp-...

# Run
make dev
```

**Webhook Mode (Production):**
```bash
# Set in .env:
SLACK_MODE=webhook
SLACK_SIGNING_SECRET=...

# Run
make dev
```

**Service-specific:**
```bash
make dev-agent      # Knowledge Agent only (:8081)
make dev-slack      # Slack Bridge only (:8080)
```

### Testing
```bash
# Unit tests
make test

# Integration tests (requires services running)
make integration-test

# Test specific endpoints
make test-webhook   # Test thread ingestion endpoint
make test-query     # Test query endpoint

# Custom test files
make test-webhook-custom FILE=mythread.json
make test-query-custom FILE=myquery.json
```

### Database Operations
```bash
# PostgreSQL shell
make db-shell
# Common queries:
SELECT COUNT(*) FROM memories;
SELECT content FROM memories ORDER BY created_at DESC LIMIT 10;

# Redis shell
make redis-shell
# Common commands: KEYS *, GET <key>

# Check Ollama models
make ollama-models
```

### Build and Cleanup
```bash
make build          # Build both binaries to bin/
make clean          # Remove build artifacts
make fmt            # Format Go code
make lint           # Run linter (requires golangci-lint)
```

## Critical Architecture Details

### Two-Service Architecture

**Why two services?**
- **Slack Bridge (`cmd/slack-bot`)**: Handles Slack integration, webhook verification, event routing
- **Knowledge Agent (`cmd/agent`)**: Core AI logic, ADK integration, memory management, LLM decision-making

This separation allows:
- Independent scaling
- Testing Knowledge Agent without Slack
- Easy integration with other platforms (Discord, Teams) by replacing the bridge

### ADK Integration (`internal/agent/agent.go`)

Uses `adk-utils-go` library (third-party, not official Google ADK):
```go
// Key components initialized in New():
sessionService := sessionredis.NewRedisSessionService(...)  // Redis sessions
memoryService := memorypostgres.NewPostgresMemoryService(...)  // PostgreSQL with pgvector
llmModel := genaianthropic.New(...)  // Anthropic Claude
memoryToolset := memorytools.NewToolset(...)  // search_memory, save_to_memory
runner := runner.New(...)  // ADK runner orchestrates agent execution
```

**Session creation patterns:**
- Query: `query-{channel_id}-{timestamp}` - unique per user interaction
- Ingest: `ingest-{channel_id}-{thread_ts}` - unique per thread (still used by /api/ingest-thread endpoint)

### Simplified Event Handling (`internal/slack/handler.go`)

```go
// No command parsing - always send to agent
func (h *Handler) handleAppMention(event *slackevents.AppMentionEvent) {
    message := stripBotMention(event.Text)  // Remove @bot mention
    h.sendToAgent(ctx, event, message)       // Send to agent with thread context
}
```

The agent receives the user's message and full thread context, then decides what to do based on the SystemPrompt.

### Agent System Prompt (`internal/agent/prompts.go`)

The `SystemPrompt` constant defines agent behavior - it's LLM-driven, not rule-based:
- Agent analyzes user intent and conversation context
- Decides when to **search** (user asking question), **save** (valuable information shared), or **both**
- **Language matching**: Always responds in the same language the user uses
- No explicit commands needed - context-driven decisions

**Important**: Agent intelligently decides what to do based on the situation, not hardcoded commands.

### Memory Tools

**`save_to_memory`**:
- Called by agent when conversation contains valuable information
- Generates embeddings via Ollama (nomic-embed-text, 768-dim)
- Stores in PostgreSQL with metadata (channel_id, thread_ts, timestamps)
- Agent decides what's worth saving based on context

**`search_memory`**:
- Agent calls when user asks questions or needs information
- Converts query to embedding
- PostgreSQL pgvector performs semantic similarity search
- Returns relevant memories to agent for synthesis
- Agent may search and save in the same interaction

**`fetch_url`**:
- Fetches and analyzes content from URLs
- Useful for documentation, blog posts, web pages
- Returns text content cleaned from HTML
- 30-second timeout, max 10,000 characters
- Supports redirects and provides final URL
- Tool implemented in `internal/tools/webfetch.go`

### MCP Integration

**Model Context Protocol (MCP)** enables the agent to access external data sources through standardized tools.

**Architecture**:
```
Agent Startup
  ↓
Load MCP Config (config.yaml)
  ↓
For each enabled MCP server:
  - Create transport (Command/SSE/Streamable)
  - Create mcptoolset via ADK
  - Append to agent toolsets
  ↓
LLM receives ALL tools (memory + web + MCP)
  ↓
Claude intelligently selects and uses tools
```

**Key Components**:
- **MCP Factory** (`internal/mcp/factory.go`): Creates MCP toolsets from configuration
- **Transport Types**:
  - **Command**: Local executables via stdio (filesystem, sqlite, git)
  - **SSE**: Server-Sent Events over HTTP (GitHub, Google Drive)
  - **Streamable**: HTTP streaming (custom MCP servers)
- **ADK Support**: Native `mcptoolset` in ADK v0.3.0
- **MCP Go SDK**: `github.com/modelcontextprotocol/go-sdk` v0.7.0

**Configuration** (config.yaml):
```yaml
mcp:
  enabled: true
  servers:
    - name: "filesystem"
      description: "Local file operations"
      enabled: true
      transport_type: "command"
      command:
        path: "npx"
        args: ["-y", "@modelcontextprotocol/server-filesystem", "/workspace"]
      tool_filter: ["read_file", "list_directory"]
      timeout: 30
```

**Tool Discovery**:
- Each MCP server exposes tools (e.g., filesystem: read_file, write_file, list_directory)
- Tools are discovered at startup and added to agent's toolset
- Optional `tool_filter` restricts available tools
- Claude sees all tools and decides which to use

**Graceful Degradation**:
- Failed MCP servers log warnings but don't prevent agent startup
- Agent continues with successfully initialized servers
- Missing MCP servers don't break existing functionality

**Common Use Cases**:
1. **Documentation Ingestion**: "Read all .md files from /docs and save key information"
2. **GitHub Integration**: "List open PRs in myorg/myrepo and summarize"
3. **Database Queries**: "Query analytics.db for last week's errors"
4. **Multi-Source**: "Compare our docs with GitHub wiki and save differences"

**Files**:
- `internal/mcp/factory.go` - MCP toolset creation and transport handling
- `internal/config/config.go` - MCP configuration structs (lines 44-97)
- `internal/agent/agent.go` - MCP integration (lines 159-194)
- `docs/MCP_INTEGRATION.md` - Complete MCP guide
- `examples/mcp/` - Example configurations

**Security Considerations**:
- Use `token_env` for credentials (never hardcode)
- Apply `tool_filter` to limit agent capabilities
- Restrict filesystem paths to specific directories
- Monitor MCP tool usage in Langfuse

### A2A Tool Integration

**Agent-to-Agent (A2A)** enables integration with other agents using the standard A2A protocol from Google ADK.

**Two Integration Modes:**

1. **Inbound A2A** (this agent as server): Unified on port 8081
   - Other agents call `http://localhost:8081/a2a/invoke`
   - Agent card at `/.well-known/agent-card.json` (public, no auth)
   - Uses same authentication as `/api/*` endpoints

2. **Outbound A2A** (calling other agents): Via Sub-Agents using `remoteagent.NewA2A`
   - Standard A2A protocol support
   - Automatic agent card discovery
   - LLM decides when to delegate to sub-agents

**Architecture**:
```
Agent Startup
  ↓
Load A2A Config (config.yaml)
  ↓
Setup inbound A2A endpoints:
  - /.well-known/agent-card.json (public)
  - /a2a/invoke (authenticated)
  ↓
For each sub_agent:
  - Create remoteagent.NewA2A(AgentCardSource: endpoint)
  - Returns agent.Agent interface
  ↓
llmagent.New(SubAgents: [...])
  ↓
LLM sees sub-agents as delegation options
```

**Key Components**:
- **A2A Handler** (`internal/server/a2a_handler.go`): Creates A2A endpoints using standard ADK libraries
- **Sub-Agents** (`internal/a2a/subagents.go`): Creates remote agents using `remoteagent.NewA2A`
- **Loop Prevention** (`internal/a2a/middleware.go`): Inbound loop detection via headers

**Configuration** (config.yaml):
```yaml
a2a:
  enabled: true
  self_name: "knowledge-agent"  # For loop prevention
  max_call_depth: 5

  # Sub-agents (RECOMMENDED): Standard A2A protocol
  sub_agents:
    - name: "metrics_agent"
      description: "Query Prometheus metrics and analyze performance data"
      endpoint: "http://metrics-agent:9000"  # Agent card source URL
      timeout: 30

    - name: "logs_agent"
      description: "Search and analyze application logs"
      endpoint: "http://logs-agent:9000"
```

**Loop Prevention Headers** (inbound requests):
| Header | Description |
|--------|-------------|
| `X-Request-ID` | Unique ID for the original request |
| `X-Call-Chain` | CSV list of agents in the chain |
| `X-Call-Depth` | Current depth (checked against max_call_depth) |

**Graceful Degradation**:
- Failed sub-agents log warnings but don't prevent startup
- Agent continues with successfully initialized sub-agents
- Similar pattern to MCP toolset creation

**Files**:
- `internal/server/a2a_handler.go` - A2A inbound endpoints (invoke, agent-card)
- `internal/a2a/subagents.go` - Creates sub-agents via remoteagent.NewA2A
- `internal/a2a/context.go` - Call chain context and headers
- `internal/a2a/middleware.go` - Inbound loop prevention middleware
- `internal/config/config.go` - A2A configuration structs
- `docs/A2A_TOOLS.md` - Complete A2A guide

**Common Use Cases**:
1. **Log Analysis**: "What errors happened in the payment service?" → delegates to logs_agent
2. **Metrics Queries**: "Show CPU usage for the last hour" → delegates to metrics_agent
3. **On-Call Management**: "Who is on-call this week?" → delegates to alerts_agent

### Multimodal Capabilities

**Image Analysis**:
- Slack images automatically downloaded and passed to agent
- Images encoded as base64 and included in genai.Content parts
- Claude Sonnet 4.5 natively supports image understanding
- Can analyze screenshots, diagrams, charts, errors, architecture diagrams
- Code in `internal/slack/client.go` (download) and `internal/agent/agent.go` (multimodal content)

## Logging Standards

**CRITICAL: ALWAYS use zap logger - NEVER use print statements**

This codebase maintains 100% structured logging discipline. All logging must use the zap logger.

### Correct Usage

```go
log := logger.Get()
log.Infow("Processing query", "channel_id", channelID, "user", userID)
log.Errorw("Failed to save memory", "error", err, "content", content)
log.Debugw("Fetched user info", "user_id", userID, "name", userName)
```

### Logger Initialization Pattern

```go
// In main.go - initialize once at startup
func main() {
    logConfig := logger.Config{
        Level:  cfg.Log.Level,   // "debug", "info", "warn", "error"
        Format: cfg.Log.Format,  // "json" or "console"
    }
    if err := logger.Initialize(logConfig); err != nil {
        log.Fatalf("Failed to initialize logger: %v", err)
    }
    defer logger.Sync()  // Flush buffers on shutdown

    log := logger.Get()
    log.Infow("Service starting", "version", version)
}
```

### Key Principles

1. **Always structured fields**: Use key-value pairs, never string interpolation
2. **Get logger instance**: `log := logger.Get()` in each function
3. **Appropriate levels**: Debug for development, Info for important events, Error for failures
4. **Context in fields**: Include relevant context (user_id, channel_id, error) as fields
5. **No print statements**: fmt.Println(), log.Println(), print() are FORBIDDEN

### Examples from Codebase

```go
// Good - structured logging
log.Infow("Query processed successfully",
    "channel_id", req.ChannelID,
    "duration_ms", time.Since(start).Milliseconds(),
    "result_length", len(response))

// Good - error logging with context
log.Errorw("Failed to fetch thread messages",
    "error", err,
    "channel_id", event.Channel,
    "thread_ts", event.ThreadTimeStamp)

// Bad - NEVER do this ❌
fmt.Printf("Processing query for channel %s\n", channelID)
log.Println("Error:", err)
```

### Implementation Files

- `internal/logger/logger.go` - Logger initialization and singleton
- All service files use `logger.Get()` for logging

## Authentication Architecture

The Knowledge Agent implements a **two-level authentication system** for maximum flexibility and security.

### Level 1: Internal Authentication (Slack Bridge ↔ Knowledge Agent)

**Purpose**: Secure communication between trusted internal services.

**Implementation**:
- **Header**: `X-Internal-Token`
- **Config**: `INTERNAL_AUTH_TOKEN` environment variable
- **Caller ID**: `"slack-bridge"`
- **Code**: `internal/server/middleware.go` lines 40-61

```go
// Check internal token first
internalToken := r.Header.Get("X-Internal-Token")
if internalToken != "" && internalToken == expectedToken {
    callerID = "slack-bridge"
    log.Debugw("Authenticated via internal token")
    next.ServeHTTP(w, r)
    return
}
```

**When to use**:
- Slack Bridge calling Knowledge Agent endpoints
- Any internal service within your infrastructure
- High-trust scenarios where you control both services

### Level 2: A2A External Authentication (External Services → Knowledge Agent)

**Purpose**: Allow external agents or services to integrate with Knowledge Agent.

**Implementation**:
- **Header**: `X-API-Key`
- **Config**: `A2A_API_KEYS` JSON map (e.g., `{"ka_rootagent":"root-agent"}`)
- **Caller ID**: Value from JSON map (e.g., `"root-agent"`)
- **Code**: `internal/server/middleware.go` lines 64-82

```go
// Check A2A API key
apiKey := r.Header.Get("X-API-Key")
if apiKey != "" {
    if callerID, ok := a.apiKeys[apiKey]; ok {
        log.Debugw("Authenticated via A2A API key", "caller_id", callerID)
        next.ServeHTTP(w, r)
        return
    }
}
```

**When to use**:
- External AI agents calling the Knowledge Agent
- Third-party integrations
- Multi-agent systems
- See `docs/A2A_TOOLS.md` for complete A2A tool integration guide

### Level 3: Legacy Slack Signature (Fallback)

**Purpose**: Support direct Slack webhook requests (legacy).

**Implementation**:
- **Header**: `X-Slack-Signature`, `X-Slack-Request-Timestamp`
- **Caller ID**: `"slack-direct"`
- **Code**: `internal/server/middleware.go` lines 85-100

### Open Mode (No Authentication)

If neither `INTERNAL_AUTH_TOKEN` nor `A2A_API_KEYS` is configured:
- Authentication is **disabled**
- All requests are allowed
- Caller ID: `"unauthenticated"`
- **Not recommended for production**

### Authentication Flow Diagram

```
Request arrives at Knowledge Agent
    ↓
Check X-Internal-Token → Match? → CallerID = "slack-bridge" ✓
    ↓ No match
Check X-API-Key → Match in map? → CallerID = map value ✓
    ↓ No match
Check X-Slack-Signature → Valid? → CallerID = "slack-direct" ✓
    ↓ No match
Check if auth required → No? → CallerID = "unauthenticated" ✓
    ↓ Auth required
Return 401 Unauthorized ✗
```

### Configuration Example

```bash
# Internal authentication (Slack Bridge)
INTERNAL_AUTH_TOKEN=your-secure-random-token

# A2A authentication (external agents)
A2A_API_KEYS='{"ka_rootagent":"root-agent","ka_analytics":"analytics-agent"}'

# Slack Bridge uses this to call Knowledge Agent
SLACK_BRIDGE_API_KEY=ka_slackbridge
```

### Security Best Practices

1. **Always use HTTPS in production**
2. **Generate strong random tokens** (use `openssl rand -base64 32`)
3. **Rotate API keys periodically**
4. **Use different tokens for each environment** (dev, staging, prod)
5. **Never commit tokens to version control**
6. **Monitor failed authentication attempts** in logs

## Permissions System

The Knowledge Agent implements **granular permission control** for the `save_to_memory` tool to prevent unauthorized knowledge base modifications.

### How It Works

The permissions system dynamically injects rules into the SystemPrompt based on configuration, controlling who can save memories.

### Configuration

**File**: `internal/config/config.go`

```go
type PermissionsConfig struct {
    AllowedSlackUsers []string `yaml:"allowed_slack_users"`  // Whitelist of Slack user IDs
    AdminCallerIDs    []string `yaml:"admin_caller_ids"`     // Caller IDs with full access
}
```

**Example** (`config.yaml`):
```yaml
permissions:
  allowed_slack_users:
    - U01ABC123DE  # John Doe
    - U02XYZ789GH  # Jane Smith
  admin_caller_ids:
    - root-agent   # External agent with full access
    - analytics-agent
```

### Permission Rules

1. **Slack User Whitelist** (`allowed_slack_users`):
   - Only these Slack user IDs can trigger `save_to_memory`
   - Checked via `slack_user_id` in query metadata
   - Empty list = no restrictions

2. **Admin Caller IDs** (`admin_caller_ids`):
   - These authenticated caller IDs bypass user restrictions
   - Useful for external agents that should always be able to save
   - Example: analytics agent saving processed insights

3. **No Config = Open Mode**:
   - If both lists are empty, all users can save
   - Recommended only for development

### Implementation

**File**: `internal/agent/prompts_builder.go`

The `BuildSystemPromptWithPermissions()` function dynamically injects permission rules:

```go
func BuildSystemPromptWithPermissions(basePrompt string, cfg *config.PermissionsConfig) string {
    if len(cfg.AllowedSlackUsers) == 0 && len(cfg.AdminCallerIDs) == 0 {
        return basePrompt  // No restrictions
    }

    permissionSection := buildPermissionSection(cfg)

    // Inject after "### When to SAVE" section
    insertPoint := "### When to SAVE (use save_to_memory):"
    return strings.Replace(basePrompt, insertPoint,
        insertPoint+"\n"+permissionSection, 1)
}
```

**Injected prompt section example**:
```
**PERMISSION CHECK REQUIRED**:
Before using save_to_memory, verify:
- If slack_user_id provided: Must be in [U01ABC123DE, U02XYZ789GH]
- OR caller_id must be in [root-agent, analytics-agent]

If permission denied, respond: "I cannot save this information as you don't have permission. Contact your admin."
```

### Agent Behavior

The LLM (Claude) receives these permission rules in its system prompt and enforces them:

1. **Check user ID**: If `slack_user_id` is in the whitelist → allow save
2. **Check caller ID**: If `caller_id` is in admin list → allow save
3. **Deny otherwise**: Respond with permission denied message
4. **Never hard error**: Agent gracefully explains the restriction to the user

### Verification Flow

```
User: "@bot Remember that our API key is abc123"
    ↓
Agent receives: {
    question: "Remember that our API key is abc123",
    metadata: { slack_user_id: "U01ABC123DE", caller_id: "slack-bridge" }
}
    ↓
Agent checks SystemPrompt permissions:
    - Is U01ABC123DE in allowed_slack_users? YES ✓
    ↓
Agent calls save_to_memory tool
    ↓
Agent responds: "I've saved that information about the API key."
```

### Logging

Permission checks are logged for audit:

```go
log.Debugw("Permission check for save_to_memory",
    "slack_user_id", slackUserID,
    "caller_id", callerID,
    "allowed", isAllowed)
```

### Testing Permissions

```bash
# User IN whitelist (should work)
curl -X POST http://localhost:8081/api/query \
  -H "X-API-Key: ka_test" \
  -d '{
    "question": "Remember our deployment is on AWS",
    "slack_user_id": "U01ABC123DE"
  }'

# User NOT in whitelist (should be denied)
curl -X POST http://localhost:8081/api/query \
  -H "X-API-Key: ka_test" \
  -d '{
    "question": "Remember our deployment is on AWS",
    "slack_user_id": "U99UNKNOWN"
  }'

# Admin caller ID (should work regardless of user)
curl -X POST http://localhost:8081/api/query \
  -H "X-API-Key: ka_rootagent" \
  -d '{
    "question": "Remember our deployment is on AWS",
    "caller_id": "root-agent"
  }'
```

## SystemPrompt Architecture

The SystemPrompt is the **most critical component** of the Knowledge Agent - it defines all agent behavior, decision-making, and tool usage.

### Dynamic Prompt Construction

The system prompt is not static. It's built dynamically at agent initialization:

**File**: `internal/agent/agent.go` (New() function, ~line 200)

```go
// 1. Load base prompt (static or configurable)
systemPrompt := prompts.SystemPrompt  // or from prompt manager

// 2. Inject permission rules dynamically
systemPromptWithPermissions := prompts.BuildSystemPromptWithPermissions(
    systemPrompt,
    &cfg.Permissions,
)

// 3. Create LLM agent with final prompt
llmAgent, err := llmagent.New(llmagent.Config{
    Instruction: systemPromptWithPermissions,
    // ...
})
```

### Prompt Components

**Base Prompt** (`internal/agent/prompts.go`):
- Agent identity and capabilities
- Tool descriptions and when to use them
- Language matching instructions
- Response format guidelines
- Memory search and save logic

**Permission Rules** (injected dynamically):
- User whitelist for save_to_memory
- Admin caller ID bypass rules
- Permission denied message templates

**Runtime Instructions** (per-query):
- Current date/time
- Thread context (previous messages)
- User's question
- Metadata (channel_id, user info)

### Instruction Building per Query

**File**: `internal/agent/agent.go` (Query method, ~line 430)

Each query constructs a specific instruction:

```go
instruction := fmt.Sprintf(`
**Current Date**: %s

Current Thread Context:
%s

**User Question**: %s

**Instructions**:
- Analyze the question and thread context
- Decide if you need to search_memory, save_to_memory, fetch_url, or just respond
- Always respond in the same language as the user
- Be helpful and conversational
`, currentDate, contextStr, req.Question)
```

This instruction is sent to the runner along with the system prompt.

### LLM Decision Making

The agent (Claude Sonnet 4.5) receives:
1. **System Prompt**: Overall behavior and tool rules
2. **Instruction**: Specific query context and user question
3. **Tool Definitions**: Available tools (search_memory, save_to_memory, fetch_url)

Claude then decides:
- **What tools to call** (if any)
- **Tool parameters** (query, content, URL)
- **Response to user** (conversational, in their language)

### Key Prompt Sections

**Identity**:
```
You are a Knowledge Management Assistant for a team's Slack workspace.
Your role is to help capture and retrieve institutional knowledge.
```

**Language Matching** (CRITICAL):
```
**LANGUAGE MATCHING**: ALWAYS respond in the same language the user uses.
- If user writes in Spanish → respond in Spanish
- If user writes in English → respond in English
- Detect language from the user's question, not from thread history
```

**Tool Usage Logic**:
```
### When to SEARCH (use search_memory):
- User asks a question that might be in the knowledge base
- User wants to recall past information
- User says "do you remember...", "what do we know about..."

### When to SAVE (use save_to_memory):
- Conversation contains valuable information worth keeping
- Decisions, procedures, facts, solutions, configurations shared
- User explicitly asks to remember something

### When to FETCH (use fetch_url):
- User shares a URL to analyze
- Documentation or blog post needs to be retrieved
```

### Modifying Agent Behavior

To change how the agent behaves:

1. **Edit the base prompt** in `internal/agent/prompts.go`
2. **Test with real Claude interactions** (most important)
3. **Consider adding new tools** if behavior requires external actions
4. **Update permission injection logic** if access control changes

### Prompt Testing

The prompt is what Claude sees - test changes carefully:

```bash
# Test search behavior
curl -X POST http://localhost:8081/api/query \
  -d '{"question": "What is our deployment process?"}'

# Test save behavior
curl -X POST http://localhost:8081/api/query \
  -d '{"question": "Remember: we deploy to AWS ECS every Tuesday"}'

# Test language matching
curl -X POST http://localhost:8081/api/query \
  -d '{"question": "¿Cuál es nuestro proceso de deployment?"}'
```

### Common Pitfalls

1. **Over-constraining the prompt**: Let Claude use its reasoning, don't over-specify
2. **Ignoring language matching**: Always test in multiple languages
3. **Forgetting permission rules**: Permissions are injected dynamically, not in base prompt
4. **Not testing with real interactions**: The prompt is what Claude sees - test it!

## Slack Integration Patterns

### Complete Event Flow

```
1. Slack Event occurs (user mentions @bot)
    ↓
2. Slack sends webhook to /slack/events (Slack Bridge :8080)
    ↓
3. Handler verifies Slack signature (if webhook mode)
    ↓
4. Handler processes event (app_mention, message in thread)
    ↓
5. Handler fetches thread context (GetThreadMessages)
    ↓
6. Handler sends to Knowledge Agent (/api/query endpoint :8081)
    ↓
7. Agent authenticates request (X-Internal-Token)
    ↓
8. Agent builds instruction with thread context
    ↓
9. Agent calls ADK runner with system prompt + instruction
    ↓
10. Claude (Sonnet 4.5) decides actions (search, save, respond)
    ↓
11. Agent executes tools (if needed)
    ↓
12. Agent returns response to Slack Bridge
    ↓
13. Slack Bridge posts message to Slack channel/thread
```

### Key Integration Points

**1. Event Reception** (`internal/slack/handler.go` - HandleEvents)
- Webhook mode: HTTP POST from Slack
- Socket mode: WebSocket message from Slack

**2. Thread Context Gathering** (`internal/slack/client.go` - GetThreadMessages)
- Fetches all messages in thread (up to 1000)
- Includes message text, user IDs, timestamps
- Preserves conversation history for agent

**3. Agent Communication** (`internal/slack/handler.go` - sendToAgent)
- POST to `/api/query` endpoint
- Includes question, thread context, metadata
- Authenticated via `X-Internal-Token` or `X-API-Key`

**4. Response Posting** (`internal/slack/client.go` - PostMessage)
- Uses Slack Web API (chat.postMessage)
- Posts to correct channel and thread
- Handles errors and retries

### Message Context Structure

```go
type QueryRequest struct {
    Question  string           `json:"question"`      // User's message (bot mention stripped)
    ThreadTS  string           `json:"thread_ts"`     // Thread timestamp (for threading)
    ChannelID string           `json:"channel_id"`    // Slack channel ID
    Messages  []map[string]any `json:"messages"`      // Thread history

    // Metadata for permissions and personalization
    SlackUserID string `json:"slack_user_id"`  // User who triggered event
    CallerID    string `json:"caller_id"`      // Authenticated caller
}
```

**Messages structure**:
```json
[
    {
        "user": "U01ABC123",
        "text": "What is our deployment process?",
        "ts": "1234567890.123456",
        "is_bot": false
    },
    {
        "user": "U01BOT456",
        "text": "Our deployment process involves...",
        "ts": "1234567891.234567",
        "is_bot": true
    }
]
```

### Image Handling

**File**: `internal/slack/client.go` (downloadSlackFile)

1. **Detect image in event**:
   ```go
   if len(event.Files) > 0 {
       for _, file := range event.Files {
           if strings.HasPrefix(file.Mimetype, "image/") {
               // Download image
           }
       }
   }
   ```

2. **Download via Slack API**:
   - Uses bot token for authentication
   - Downloads file bytes
   - Validates image type via magic bytes

3. **Send to agent**:
   ```go
   // Encode as base64 and include in query
   imageData := base64.StdEncoding.EncodeToString(data)
   ```

4. **Agent processes**:
   - Claude Sonnet 4.5 receives image in genai.Content
   - Can analyze screenshots, diagrams, error messages, etc.

### URL Handling

**Pattern**: User shares URL in message

1. **Agent receives question with URL**
2. **Agent decides to use fetch_url tool**
3. **Tool fetches URL content** (internal/tools/webfetch.go)
4. **Agent analyzes content and responds**

Example:
```
User: "@bot analyze this article https://example.com/post"
    ↓
Agent: Uses fetch_url("https://example.com/post")
    ↓
Agent: Reads article content
    ↓
Agent: Responds with summary/analysis
```

### Error Handling Patterns

```go
// Slack API errors
if err := h.client.PostMessage(channel, threadTS, response); err != nil {
    log.Errorw("Failed to post message to Slack",
        "error", err,
        "channel", channel,
        "thread_ts", threadTS)
    // Don't retry - user already waited, log and move on
}

// Agent errors
resp, err := http.Post(agentURL, "application/json", body)
if err != nil {
    log.Errorw("Failed to call agent", "error", err)
    h.client.PostMessage(channel, threadTS,
        "Sorry, I encountered an error. Please try again.")
    return
}
```

### Threading Behavior

- **New mention**: Create new thread (or respond in channel if already in thread)
- **Reply in thread**: Continue thread conversation
- **Thread context**: Always sent to agent for coherent conversations

### Rate Limiting Considerations

Slack has rate limits:
- **Tier 1**: 1 request per second (postMessage)
- **Tier 2**: 20 requests per minute (conversations.history)

The agent handles this via:
- **Request throttling**: Not yet implemented (TODO in refactoring)
- **Error handling**: Logs and notifies user on rate limit errors
- **Retries**: Exponential backoff on specific errors

### Testing Integration

```bash
# Test direct Slack Bridge endpoint
curl -X POST http://localhost:8080/slack/events \
  -H "Content-Type: application/json" \
  -d @examples/slack_event.json

# Test Knowledge Agent query endpoint
curl -X POST http://localhost:8081/api/query \
  -H "X-Internal-Token: your-token" \
  -H "Content-Type: application/json" \
  -d '{
    "question": "What is our deployment process?",
    "channel_id": "C01ABC123",
    "thread_ts": "1234567890.123456",
    "slack_user_id": "U01ABC123"
  }'
```

## Configuration

### Agent Personalization

The agent can be personalized with a custom name in `config.yaml`:

```yaml
agent_name: Anton  # Your team's unique identity (default: "Knowledge Agent")
```

**How it works:**
- Set in `config.yaml` (primary method)
- Can be overridden with `AGENT_NAME` environment variable (optional)
- Injected into system prompt: `"You are Anton, a Knowledge Management Assistant"`
- Default is "Knowledge Agent" if not specified
- Implementation: `internal/agent/prompts_builder.go` (`BuildSystemPrompt` function)
- Config struct: `internal/config/config.go` (Config.AgentName field)

**Examples:** Anton (Freepik Tech), Ghost (Destiny reference), Cortex (brain), Sage (wisdom), Echo (memory)

Environment variables (`.env.example`):
- `ANTHROPIC_API_KEY` - Required, from console.anthropic.com
- `ANTHROPIC_MODEL` - Default: claude-sonnet-4-5-20250929
- `SLACK_BOT_TOKEN` - xoxb-* token from api.slack.com/apps
- `SLACK_MODE` - "socket" (dev) or "webhook" (prod). Default: webhook
- `SLACK_APP_TOKEN` - xapp-* token for socket mode (optional, required if SLACK_MODE=socket)
- `SLACK_SIGNING_SECRET` - For webhook verification (optional, required if SLACK_MODE=webhook)
- `POSTGRES_URL` - PostgreSQL connection string
- `REDIS_ADDR` - Redis address (default: localhost:6379)
- `OLLAMA_BASE_URL` - Ollama API for embeddings (default: http://localhost:11434/v1)
- `EMBEDDING_MODEL` - Default: nomic-embed-text (768 dimensions)

### Socket Mode vs Webhook Mode

**Socket Mode** (`SLACK_MODE=socket`):
- Uses WebSocket connection to Slack
- No public endpoint required
- Perfect for local development
- Requires `SLACK_APP_TOKEN` (App-Level Token with `connections:write` scope)
- Code: `internal/slack/socket_handler.go`

**Webhook Mode** (`SLACK_MODE=webhook`):
- Uses HTTP webhooks from Slack Event Subscriptions
- Requires public HTTPS endpoint
- Recommended for production (scalable, stateless)
- Requires `SLACK_SIGNING_SECRET` for request verification
- Code: `internal/slack/handler.go` (HandleEvents method)

Both modes use the same handler logic after receiving events - the only difference is the transport layer.

### Agent-to-Agent (A2A) Authentication

The Knowledge Agent supports optional API key authentication for securing access from external agents.

**Configuration Variables:**
- `A2A_API_KEYS` - JSON map of API keys to caller IDs (Knowledge Agent)
  - Example: `A2A_API_KEYS='{"ka_rootagent":"root-agent","ka_slackbridge":"slack-bridge"}'`
  - If omitted or empty: Open mode (no authentication required)
  - If configured: Authentication required via `X-API-Key` header or Slack signature
- `SLACK_BRIDGE_API_KEY` - API key for Slack Bridge to authenticate with Knowledge Agent
  - Example: `SLACK_BRIDGE_API_KEY=ka_slackbridge`
  - Must match a key in `A2A_API_KEYS` if authentication is enabled

**Authentication Flow:**
```
External Agent → X-API-Key: ka_rootagent → Knowledge Agent
Slack Bridge   → X-API-Key: ka_slackbridge → Knowledge Agent
```

**Implementation Files:**
- `internal/server/middleware.go` - Authentication middleware
- `internal/config/config.go` - APIKeys configuration
- `internal/slack/handler.go` - Slack Bridge sends API key in requests

**See `docs/A2A_TOOLS.md` for complete A2A tool integration guide**

### Langfuse Observability Configuration

Langfuse provides comprehensive observability for LLM interactions, tracking tokens, costs, tool usage, and performance.

**SDK Used:** `github.com/git-hulk/langfuse-go v0.1.0` (modern, feature-complete)

**Configuration Variables:**
```yaml
langfuse:
  enabled: true                          # Enable/disable Langfuse tracing
  public_key: ${LANGFUSE_PUBLIC_KEY}     # Get from Langfuse dashboard
  secret_key: ${LANGFUSE_SECRET_KEY}     # Get from Langfuse dashboard
  host: https://cloud.langfuse.com       # Langfuse host (cloud or self-hosted)
  input_cost_per_1m: 3.0                 # Cost per 1M input tokens in USD (Claude Sonnet 4.5)
  output_cost_per_1m: 15.0               # Cost per 1M output tokens in USD (Claude Sonnet 4.5)
```

**What Gets Tracked:**
- **Traces**: Complete query execution with automatic latency calculation
- **Generations**: Individual LLM generations with model name, input/output, token usage
- **Tool Calls**: Every tool invocation (search_memory, save_to_memory, fetch_url) as Observations
- **Tokens**: Prompt tokens, completion tokens, total tokens (accumulated across calls)
- **Costs**: TotalCost field calculated automatically from token usage and pricing
- **Metadata**: User info (user_name, user_real_name), caller_id, channel_id, thread_ts, all custom fields
- **Timing**: Automatic latency tracking per trace, per observation (generation/tool)
- **User ID**: Trace.UserID set to user_name for user-level analytics

**Langfuse Integration Architecture:**

```
ADK Runner Events
  ↓
StartGeneration() → Create generation observation
  ↓
StartToolCall() → Create tool observation
  ↓
EndGeneration() → Set output, usage (tokens), End()
  ↓
EndToolCall() → Set output, error status, End()
  ↓
trace.End() → Calculate latency, set TotalCost, submit to batch processor
  ↓
git-hulk SDK → Efficient batch processing, automatic flush on Close()
```

**Key Files:**
- `internal/observability/langfuse.go` - Complete Langfuse integration (350 lines)
- `internal/agent/agent.go` - ADK event processing with Langfuse tracking (lines 608-755)

**How It Works:**

1. **Trace Creation** (`StartQueryTrace`):
   - Creates trace with `client.StartTrace(ctx, "knowledge-agent-query")`
   - Sets Input (question), Tags, Metadata, UserID (from user_name)

2. **Generation Tracking**:
   - When ADK event has `UsageMetadata`, starts generation: `trace.StartGeneration(modelName, input)`
   - On next event with tokens, ends generation: `trace.EndGeneration(gen, output, promptTokens, completionTokens)`
   - Automatically accumulates tokens across multiple generations

3. **Tool Tracking**:
   - On `FunctionCall`: `trace.StartToolCall(toolName, args)`
   - On `FunctionResponse`: `trace.EndToolCall(toolName, output, err)`
   - Tracks tool execution time and errors

4. **Cost Calculation**:
   - Accumulates all tokens from generations
   - Calculates: `(promptTokens / 1M * inputCost) + (completionTokens / 1M * outputCost)`
   - Sets `trace.TotalCost` field

5. **Trace Completion** (`trace.End()`):
   - Sets trace Output with success status, answer, tokens, costs
   - SDK automatically calculates Latency (milliseconds)
   - Submits to batch processor for efficient ingestion

6. **Graceful Shutdown** (`client.Close()`):
   - Automatically flushes all pending traces
   - Waits for batch processor to complete

**Advantages of git-hulk SDK:**
- ✅ Clean, modern API (StartTrace, StartGeneration, End)
- ✅ Automatic batch processing with efficient flushing
- ✅ Native support for Generations, Spans, Tools, Events
- ✅ Built-in TotalCost field for cost tracking
- ✅ Automatic latency calculation
- ✅ No "batch": null errors (proper SDK implementation)
- ✅ Comprehensive API: Prompts, Models, Datasets, Sessions, Scores
- ✅ Graceful shutdown with automatic flush

**Common Operations:**

View traces in Langfuse UI:
- Navigate to your project
- See traces with: Input (question), Output (answer, tokens, costs), Metadata (user info)
- Drill down into Generations (LLM calls) and Observations (tool calls)
- Filter by user (UserID), tags, time range
- Analyze costs per user, per query type

**Troubleshooting:**

**Issue: Traces not appearing in Langfuse**
- Check `enabled: true` in config
- Verify LANGFUSE_PUBLIC_KEY and LANGFUSE_SECRET_KEY are set
- Check logs for "Langfuse tracing enabled" message
- Verify host URL is correct (https://cloud.langfuse.com or self-hosted)

**Issue: Costs showing as $0.00**
- Check `input_cost_per_1m` and `output_cost_per_1m` are configured
- Verify tokens are being captured (check logs for "prompt_tokens", "completion_tokens")
- Ensure model name is passed to CalculateTotalCost()

**Issue: User names not appearing**
- Kill zombie processes: `make cleanup`
- Verify Slack API scopes include `users:read`
- Check logs for "User info fetched" message

### User Personalization (Slack Names)

The agent fetches and uses Slack user information for personalized responses.

**How It Works:**
1. Socket/Webhook handler receives `app_mention` event with `user_id`
2. Handler calls Slack API `GetUserInfo(user_id)` to fetch:
   - `Name`: Slack username (e.g., "dfradejas")
   - `RealName`: Full name (e.g., "Daniel Fradejas")
3. Both names are included in query request to agent
4. Agent adds names to instruction context for LLM
5. LLM uses real name naturally in responses

**Code Flow:**
```go
// internal/slack/handler.go (line 138-156)
userInfo, err := h.client.GetUserInfo(event.User)
if err != nil {
    log.Warnw("Failed to fetch user info", ...)
} else {
    userName = userInfo.Name          // @username
    userRealName = userInfo.RealName  // John Doe
}

// Include in query request
queryRequest := map[string]any{
    "question":       message,
    "user_name":      userName,
    "user_real_name": userRealName,
    // ...
}
```

**LLM Integration:**
```go
// internal/agent/agent.go (line 511-518)
userGreeting := ""
if req.UserRealName != "" {
    userGreeting = fmt.Sprintf("\n**User**: %s (Slack: @%s)\n",
        req.UserRealName, req.UserName)
}
instruction = fmt.Sprintf(`...%s...`, userGreeting, ...)
```

**System Prompt Guidance:**
```markdown
### For Responses:
- **PERSONALIZATION**: If you know the user's name, use it naturally
  - Example: "Hi John, let me search for that..."
  - Don't overuse - once at the beginning is enough
```

**Langfuse Metadata:**
- `user_name`: "dfradejas"
- `user_real_name`: "Daniel Fradejas"
- `slack_user_id`: "U074TAU31NK"

**Required Slack Scope:**
- `users:read` - To call `users.info` API method

## Testing Without Slack

The Knowledge Agent can be tested independently:

```bash
# Terminal 1: Start infrastructure and agent
make docker-up
make dev-agent

# Terminal 2: Test endpoints directly
# Without authentication (open mode)
curl -X POST http://localhost:8081/api/query \
  -H "Content-Type: application/json" \
  -d @examples/test_query.json

# With authentication (if A2A_API_KEYS is configured)
curl -X POST http://localhost:8081/api/query \
  -H "Content-Type: application/json" \
  -H "X-API-Key: ka_rootagent" \
  -d @examples/test_query.json
```

## Common Patterns

### Adding New Endpoints to Knowledge Agent

1. Define request/response structs in `internal/agent/agent.go`
2. Implement handler method on `Agent` struct
3. Register HTTP handler in `cmd/agent/main.go`
4. Update system prompt in `internal/agent/prompts.go` if agent behavior changes

### Modifying Agent Behavior

1. Update `SystemPrompt` in `internal/agent/prompts.go` - this is what Claude sees
2. Test with actual Claude interactions (system prompt is critical)
3. Consider creating new tool if behavior requires external actions

### Working with Memory Service

Memory service (from adk-utils-go) abstracts PostgreSQL+pgvector:
- Automatically generates embeddings using configured embedding model
- Handles vector storage and similarity search
- Metadata stored as JSONB for flexible querying

Don't directly interact with PostgreSQL - use the memory service methods.

### Graceful Shutdown Pattern

The unified binary (`cmd/knowledge-agent/main.go`) implements a multi-step graceful shutdown with timeouts to prevent hanging:

**CRITICAL**: Never use `defer agentInstance.Close()` as it can block indefinitely. Always use explicit shutdown with timeout.

**Correct pattern:**
```go
// 1. NO defer on agent
agentInstance, err := agent.New(ctx, cfg)
// Don't: defer agentInstance.Close() ❌

// 2. Shutdown HTTP server first (with timeout)
shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
httpServer.Shutdown(shutdownCtx)

// 3. Close agent with timeout for safety
closeDone := make(chan error, 1)
go func() {
    closeDone <- agentInstance.Close()
}()

select {
case err := <-closeDone:
    // Clean shutdown
case <-time.After(5 * time.Second):
    log.Warn("Agent close timeout - forcing shutdown")
}
```

**Why this matters:**
- `agent.Close()` calls multiple blocking operations: PostgreSQL, Redis, Langfuse flush, file watcher
- Without timeout, Ctrl+C can hang indefinitely
- HTTP server shutdown first prevents new requests during cleanup
- 5-second timeout ensures process always terminates

**Applied in all modes:**
- `runAgentOnly()` - Single agent service
- `runSlackBotOnly()` - Only Slack bridge (no agent to close)
- `runBothServices()` - Both services with coordinated shutdown

## Troubleshooting

### Zombie Processes After Ctrl+C

**Problem**: After stopping the agent with Ctrl+C, old processes continue running and receive Slack events.

**Symptoms:**
- Requests reach agent but logs don't show socket handler events
- `user_name` and `user_real_name` are empty in Langfuse traces
- Multiple go processes visible in `ps aux | grep knowledge-agent`

**Root Cause:**
- Old code versions (before unified binary migration) running as `go run cmd/agent/main.go & go run cmd/slack-bot/main.go`
- These processes don't terminate properly on Ctrl+C
- They continue listening to Slack and forwarding requests

**Solution:**
```bash
# Quick cleanup
make cleanup

# Or manual cleanup
./scripts/cleanup-processes.sh

# Or nuclear option
killall -9 go
```

**Prevention:**
- Always use the unified binary: `make dev CONFIG=config.yaml`
- Makefile now includes `trap 'kill 0' EXIT` and `exec` to prevent zombies
- If Ctrl+C doesn't work, use `make cleanup` before restarting

**Verification:**
```bash
# Should show ONLY your current process
ps aux | grep -E "knowledge-agent|cmd/agent|cmd/slack-bot" | grep -v grep
```

### Langfuse "batch": null Errors

**Problem**: Langfuse logs show repeated validation errors:
```
Invalid input: expected array, received null
path: ["batch"]
```

**Symptoms:**
- Traces appear in Langfuse UI
- Output shows "undefined"
- Metadata may be incomplete
- Errors repeat on every query

**Root Causes:**

1. **SDK Limitation**: `henomis/langfuse-go v0.0.3` is a basic, community-maintained SDK with limited functionality:
   - No direct `trace-update` event type (only `trace-create`)
   - Requires dispatching new `trace-create` with same ID to update
   - Buffering can cause empty batches if not flushed properly

2. **Double Dispatch Without Flush**:
   - Trace created at start (with Input)
   - Trace updated at end (with Output) but not flushed
   - SDK sends empty batch between these events

**Current Implementation:**
```go
// internal/observability/langfuse.go End() method
// Dispatch trace-create with same ID = update
qt.tracer.client.Trace(&model.Trace{
    ID:        qt.trace.ID,  // Same ID updates existing trace
    Name:      qt.trace.Name,
    Timestamp: &now,
    Metadata:  qt.metadata,
    Tags:      qt.trace.Tags,
    Input:     qt.trace.Input,
    Output:    output,  // Complete output data
})

// Flush immediately to avoid buffering issues
qt.tracer.client.Flush(qt.tracer.ctx)
```

**Known Issue:**
- Despite proper implementation, SDK v0.0.3 occasionally sends null batches
- This is a known limitation of the SDK version
- Traces still work correctly in Langfuse - errors are cosmetic
- Data (tokens, costs, tool_calls) appears correctly in traces

**Workarounds:**
1. **Ignore the errors** - They don't affect functionality
2. **Filter Langfuse logs** - Focus on application logs
3. **Wait for SDK update** - Check for newer versions: `go list -m -versions github.com/henomis/langfuse-go`

**Future Solutions:**
- Migrate to official Anthropic SDK when available
- Contribute fixes to henomis/langfuse-go
- Implement direct Langfuse API calls (bypass SDK)

### Missing User Names in Traces

**Problem**: `user_name` and `user_real_name` are empty ("") in Langfuse metadata.

**Diagnosis Steps:**

1. **Check if user info is being fetched:**
```bash
# Look for these logs at DEBUG level
make dev CONFIG=config.yaml
# Send a message
# Look for: "User info fetched" with name and real_name
```

2. **Verify no zombie processes:**
```bash
ps aux | grep -E "cmd/agent|cmd/slack-bot" | grep -v grep
# Should show NOTHING (old binaries are gone)
```

3. **Check Slack API permissions:**
```bash
# App should have users:read scope
# Verify in api.slack.com/apps → OAuth & Permissions
```

4. **Test user info API directly:**
```bash
curl -H "Authorization: Bearer $SLACK_BOT_TOKEN" \
  "https://slack.com/api/users.info?user=U074TAU31NK"
```

**Common Causes:**
- ✅ **Zombie processes** - Old code without user fetching (use `make cleanup`)
- ✅ **Missing Slack scope** - Add `users:read` and reinstall app
- ✅ **API rate limiting** - Slack throttling requests (check response headers)
- ✅ **Invalid user ID** - User deleted or bot can't access (check error logs)

### Noisy Slack SDK Logs

**Problem**: Console flooded with `slack-go/slack/socketmode` debug messages.

**Solution**: Debug mode is now disabled by default in socket handler:
```go
// internal/slack/socket_handler.go
client := socketmode.New(api, socketmode.OptionDebug(false))
```

**If you need debug logs:**
```go
// Temporarily enable for troubleshooting
client := socketmode.New(api, socketmode.OptionDebug(true))
```

**Log Levels:**
- `INFO`: App mentions, query requests, trace summaries
- `DEBUG`: Socket events, user info fetches, thread messages
- `WARN`: Failed operations (user info fetch, memory search)
- `ERROR`: Critical failures (database errors, API failures)

**Change log level:**
```yaml
# config.yaml
log:
  level: debug  # debug, info, warn, error
```

## Important Files

- `cmd/knowledge-agent/main.go` - Unified binary (Agent + Slack Bridge), supports `--mode` flag (all/agent/slack-bot)
- `internal/agent/agent.go` - Core agent implementation, ADK initialization, Query() and IngestThread() methods
- `internal/agent/prompts.go` - **CRITICAL** System prompt defining agent behavior, language handling, and tool usage decisions
- `internal/slack/handler.go` - Slack event handling, forwards all messages to Knowledge Agent with thread context
- `internal/config/config.go` - Configuration loading from environment variables
- `internal/mcp/factory.go` - MCP toolset creation, transport handling, authentication

## Dependencies Note

**adk-utils-go**: This is NOT the official Google ADK. It's a third-party library (`github.com/achetronic/adk-utils-go`) that provides:
- Anthropic client integration
- Redis session service
- PostgreSQL memory service with pgvector
- Memory tools (search_memory, save_to_memory)

The `go.mod` uses a replace directive to pull from master branch. Run `go mod download && go mod tidy` after clone.

## Documentation

**IMPORTANT**: All documentation MUST be in the `docs/` directory. Consolidate content instead of creating multiple files per feature.

### Current Documentation Structure

- `docs/CONFIGURATION.md` - Complete configuration guide (includes A2A, Slack scopes, MCP, command line flags)
- `docs/MCP_INTEGRATION.md` - Model Context Protocol integration guide (filesystems, GitHub, databases)
- `docs/OPERATIONS.md` - Logging, traceability, and observability
- `docs/SECURITY.md` - Authentication, authorization, and permissions system
- `docs/TESTING.md` - Testing guide and test suite documentation
- `docs/IMPLEMENTATION_SUMMARY.md` - Implementation overview
- `docs/USAGE_GUIDE.md` - End-user guide for natural interaction with the agent

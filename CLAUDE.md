# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## System Overview

Knowledge Agent is an intelligent AI assistant that helps teams capture and retrieve institutional knowledge from Slack conversations. It uses Claude Sonnet 4.5 via ADK (Agent Development Kit) with PostgreSQL+pgvector for semantic search.

**Interaction model:**
- Users mention the bot naturally: `@bot <message>`
- Can attach images (screenshots, diagrams, charts) for visual analysis
- Can share URLs for content fetching and analysis
- The LLM intelligently decides whether to search, save, fetch URLs, or respond conversationally
- Automatically responds in the user's language (Spanish, English, etc.)

**Architecture (unified binary):**
```
┌─────────────────────────────────────────────────────────┐
│              knowledge-agent (unified binary)           │
│                                                         │
│  --mode all (default)  │  --mode agent  │ --mode slack  │
│  ┌──────────┬────────┐ │  ┌──────────┐  │ ┌──────────┐  │
│  │Agent:8081│Slack   │ │  │Agent:8081│  │ │Slack:8080│  │
│  │          │:8080   │ │  └──────────┘  │ └──────────┘  │
│  └──────────┴────────┘ │                │               │
└─────────────────────────────────────────────────────────┘
                          │
        ┌─────────────────┴─────────────────┐
   Port 8081 (Agent)                   Port 8080 (Slack)
   /api/query (auth)                   /slack/events
   /api/query/stream (SSE, auth)       Socket mode listener
   /a2a/invoke (auth)
   /.well-known/agent-card (public)
   /health, /metrics, /ready, /live
                          │
                    ADK Agent + Tools
                          │
                    PostgreSQL+pgvector
```

**Port breakdown:**
- **8080**: Slack Webhook Bridge (receives Slack events)
- **8081**: Agent HTTP server (API endpoints with auth)

## Development Commands

### Setup and Infrastructure
```bash
go mod download && go mod tidy     # Initial setup
make docker-up                      # Start PostgreSQL, Redis, Ollama
make docker-health                  # Check service health
```

### Running Services
```bash
make dev                # Run unified binary (--mode all)
make dev-agent          # Agent only (:8081)
make dev-slack          # Slack Bridge only (:8080)
```

**Slack Modes:**
- Socket Mode (dev): Set `SLACK_MODE=socket` + `SLACK_APP_TOKEN`
- Webhook Mode (prod): Set `SLACK_MODE=webhook` + `SLACK_SIGNING_SECRET`

### Testing
```bash
make test               # Unit tests
make integration-test   # Integration tests
make test-query         # Test query endpoint
```

### Database Operations
```bash
make db-shell           # PostgreSQL shell
make redis-shell        # Redis shell
make ollama-models      # Check Ollama models
```

### Build and Cleanup
```bash
make build              # Build binary to bin/
make clean              # Remove artifacts
make fmt && make lint   # Format and lint
make cleanup            # Kill zombie processes
```

## Critical Architecture Details

### Unified Binary Architecture

The codebase uses a **single binary** (`cmd/knowledge-agent/main.go`) that runs in three modes:

| Mode | Flag | Ports | Use Case |
|------|------|-------|----------|
| All | `--mode all` | 8080+8081 | Default, development |
| Agent | `--mode agent` | 8081 | Testing without Slack |
| Slack | `--mode slack-bot` | 8080 | Custom bridge scenarios |

This allows:
- Independent testing of each component
- Single process for simple deployments
- Easy integration with other platforms by replacing the Slack bridge

### ADK Integration (`internal/agent/agent.go`)

Uses `adk-utils-go` library (third-party, not official Google ADK):
```go
sessionService := sessionredis.NewRedisSessionService(...)  // Redis sessions
memoryService := memorypostgres.NewPostgresMemoryService(...) // PostgreSQL+pgvector
llmModel := genaianthropic.New(...)  // Anthropic Claude
memoryToolset := memorytools.NewToolset(...)  // search_memory, save_to_memory
runner := runner.New(...)  // ADK runner orchestrates agent execution
```

**Request ID patterns:**
- Query: `query-{channel_id}-{timestamp}` - unique per interaction
- Ingest: `ingest-{channel_id}-{thread_ts}` - unique per thread

### Agent System Prompt (`internal/agent/prompts.go`)

The `SystemPrompt` constant defines agent behavior - it's LLM-driven, not rule-based:
- Agent analyzes user intent and conversation context
- Decides when to **search**, **save**, or **both**
- **Language matching**: Always responds in user's language
- No explicit commands needed - context-driven decisions

### Memory Tools

| Tool | When Used | Implementation |
|------|-----------|----------------|
| `save_to_memory` | Valuable information shared | `internal/tools/` via adk-utils-go |
| `search_memory` | User asks questions | PostgreSQL pgvector similarity search |
| `fetch_url` | URLs shared for analysis | `internal/tools/webfetch.go` |

> **Pre-Search**: `search_memory` is automatically executed **before** the LLM loop to provide relevant memory context upfront. Results are injected into the prompt, and the LLM can search again with different terms if needed. Pre-search has a 3-second timeout and returns max 5 results.

### MCP Integration

Model Context Protocol enables access to external data sources. **Full guide: `docs/MCP_INTEGRATION.md`**

Quick reference:
```yaml
mcp:
  enabled: true
  servers:
    - name: "filesystem"
      transport_type: "command"
      command: { path: "npx", args: ["-y", "@modelcontextprotocol/server-filesystem", "/workspace"] }
```

Key files: `internal/mcp/factory.go`, `internal/config/config.go`

### A2A Tool Integration

Agent-to-Agent protocol for multi-agent systems. **Full guide: `docs/A2A_TOOLS.md`**

Quick reference:
```yaml
a2a:
  enabled: true
  self_name: "knowledge-agent"
  sub_agents:
    - name: "metrics_agent"
      endpoint: "http://metrics-agent:9000"
```

Key files: `internal/server/a2a_handler.go`, `internal/a2a/toolset.go`

### Multimodal Capabilities

- Slack images automatically downloaded and passed to agent as base64
- Claude Sonnet 4.5 natively supports image understanding
- Code: `internal/slack/client.go` (download), `internal/agent/agent.go` (multimodal)

## Logging Standards

**CRITICAL: ALWAYS use zap logger - NEVER use print statements**

```go
log := logger.Get()
log.Infow("Processing query", "channel_id", channelID, "user", userID)
log.Errorw("Failed to save memory", "error", err, "content", content)
```

Key principles:
1. Always use structured fields (key-value pairs)
2. Get logger with `log := logger.Get()` in each function
3. Appropriate levels: Debug (dev), Info (events), Error (failures)
4. **FORBIDDEN**: `fmt.Println()`, `log.Println()`, `print()`

## Authentication & Security

**Full guide: `docs/SECURITY_GUIDE.md`**

Quick reference:

| Method | Header | Use Case |
|--------|--------|----------|
| Internal Token | `X-Internal-Token` | Slack Bridge → Agent (trusted) |
| JWT Bearer | `Authorization: Bearer <token>` | API Gateway / Identity Provider (email+groups) |
| API Key | `X-API-Key` | External agents (A2A) |
| Slack Signature | `X-Slack-Signature` | Direct Slack webhooks (legacy) |
| Open Mode | (none configured) | Development only |

**API Key format** (`API_KEYS` env var):
```bash
# New format with roles:
API_KEYS='{"ka_secret_key":{"caller_id":"my-caller","role":"write"}}'
# Legacy format (assumes role="write"):
API_KEYS='{"ka_secret_key":"caller-id"}'
#          ↑ API key (secret)  ↑ Caller ID (for logging)
```

**Security notes:**
- `X-Slack-User-Id` header only accepted from internal token (prevents spoofing)
- External API keys cannot pass Slack user identity
- Roles: `write` (full access) or `read` (no save_to_memory)

## Permissions System

Controls who can use `save_to_memory` based on JWT claims (email and groups). **Full guide: `docs/SECURITY_GUIDE.md`**

```yaml
permissions:
  groups_claim_path: "groups"  # JWT claim path for groups (or "realm_access.roles" for Keycloak)
  allowed_emails:
    - value: "admin@company.com"
      role: "write"
    - value: "viewer@company.com"
      role: "read"
  allowed_groups:
    - value: "/google-workspace/devops@company.com"  # Google Workspace group
      role: "write"
    - value: "knowledge-readers"
      role: "read"
```

Roles: `write` (full access) or `read` (no save_to_memory)

Implementation: `internal/agent/permissions.go`, `internal/agent/permission_memory_service.go`

## Configuration

**Full guide: `docs/CONFIGURATION.md`**

Essential environment variables:
```bash
ANTHROPIC_API_KEY=...          # Required
SLACK_ENABLED=true|false       # Default: true (set false for API-only mode)
SLACK_BOT_TOKEN=xoxb-...       # Required if SLACK_ENABLED=true
SLACK_MODE=socket|webhook      # Default: webhook
POSTGRES_URL=...               # Required
REDIS_ADDR=localhost:6379      # Default
OLLAMA_BASE_URL=http://localhost:11434/v1  # Default
```

Config file (`config.yaml`) supports all options with YAML syntax.

## Observability

**Full guide: `docs/OBSERVABILITY.md`**

Langfuse integration tracks:
- Traces (complete query execution)
- Generations (LLM calls with tokens/costs)
- Tool calls (search_memory, save_to_memory, fetch_url)

```yaml
langfuse:
  enabled: true
  public_key: ${LANGFUSE_PUBLIC_KEY}
  secret_key: ${LANGFUSE_SECRET_KEY}
```

Key file: `internal/observability/langfuse.go`

## Slack Integration Patterns

### Event Flow
```
Slack Event → /slack/events (8080) → Verify signature → Fetch thread context
    → POST /api/query (8081) → ADK Runner → Claude decides actions
    → Execute tools → Return response → Post to Slack
```

### Key Components
- `internal/slack/handler.go` - Event handling, sends to agent
- `internal/slack/client.go` - Slack API, thread fetching, image download
- `internal/slack/socket_handler.go` - Socket mode implementation

### Message Context Structure
```go
type QueryRequest struct {
    Question    string           `json:"question"`
    ThreadTS    string           `json:"thread_ts"`
    ChannelID   string           `json:"channel_id"`
    Messages    []map[string]any `json:"messages"`
    SlackUserID string           `json:"slack_user_id"`
    UserName    string           `json:"user_name"`
    UserRealName string          `json:"user_real_name"`
}
```

## Testing Without Slack

```bash
# Terminal 1
make docker-up && make dev-agent

# Terminal 2 - Standard query
curl -X POST http://localhost:8081/api/query \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-key" \
  -d '{"question": "What is our deployment process?"}'

# Terminal 2 - Streaming query (SSE)
curl -N -X POST http://localhost:8081/api/query/stream \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-key" \
  -d '{"question": "What is our deployment process?"}'
```

## Common Patterns

### Adding New Endpoints
1. Define request/response structs in `internal/agent/agent.go`
2. Implement handler method on `Agent` struct
3. Register HTTP handler in `cmd/knowledge-agent/main.go`

### Modifying Agent Behavior
1. Edit `SystemPrompt` in `internal/agent/prompts.go`
2. Test with real Claude interactions
3. Consider adding new tools if needed

### Graceful Shutdown Pattern

**CRITICAL**: Never use `defer agentInstance.Close()` - it can block indefinitely.

```go
// Correct pattern in cmd/knowledge-agent/main.go
agentInstance, _ := agent.New(ctx, cfg)

// On shutdown signal:
agentServer.SetNotReady()  // Stop accepting traffic

shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
httpServer.Shutdown(shutdownCtx)

// Close agent with timeout
closeDone := make(chan error, 1)
go func() { closeDone <- agentInstance.Close() }()
select {
case <-closeDone: // Clean shutdown
case <-time.After(5*time.Second): log.Warn("Timeout - forcing shutdown")
}
```

## Troubleshooting

### Zombie Processes After Ctrl+C
```bash
make cleanup                    # Kill zombie processes
ps aux | grep knowledge-agent   # Verify only current process
```

### Langfuse Issues
- Traces not appearing: Check `enabled: true` and API keys
- Costs $0: Verify `input_cost_per_1m` and `output_cost_per_1m` configured
- Empty user names: Run `make cleanup`, check `users:read` Slack scope

### Noisy Logs
```yaml
# config.yaml
log:
  level: info  # debug, info, warn, error
```

## Important Files

| File | Purpose |
|------|---------|
| `cmd/knowledge-agent/main.go` | Unified binary, `--mode` flag |
| `internal/agent/agent.go` | Core agent, ADK initialization |
| `internal/agent/prompts.go` | **CRITICAL** System prompt |
| `internal/slack/handler.go` | Slack event handling |
| `internal/config/config.go` | Configuration structs |
| `internal/mcp/factory.go` | MCP toolset creation |
| `internal/server/middleware.go` | Authentication middleware |
| `internal/a2a/toolset.go` | A2A toolset (query_<agent_name> tools) |
| `internal/a2a/query_extractor.go` | Query extraction for sub-agents |

## Dependencies Note

**adk-utils-go**: Third-party library (`github.com/achetronic/adk-utils-go`) providing:
- Anthropic client integration
- Redis session service
- PostgreSQL memory service with pgvector
- Memory tools (search_memory, save_to_memory)

Run `go mod download && go mod tidy` after clone.

## Documentation

All detailed documentation is in `docs/`:

| File | Content |
|------|---------|
| `docs/CONFIGURATION.md` | Complete configuration guide |
| `docs/SECURITY_GUIDE.md` | Authentication, authorization, permissions |
| `docs/A2A_TOOLS.md` | Agent-to-Agent integration |
| `docs/MCP_INTEGRATION.md` | Model Context Protocol |
| `docs/OBSERVABILITY.md` | Langfuse, metrics, tracing |
| `docs/OPERATIONS.md` | Logging, operations |
| `docs/TESTING.md` | Testing guide |
| `docs/USAGE_GUIDE.md` | End-user guide |

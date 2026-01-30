# Configuration Guide

This comprehensive guide covers all configuration aspects of Knowledge Agent, including file-based configuration, environment variables, Slack setup, A2A authentication, and command-line flags.

## Table of Contents

1. [Unified Binary Architecture](#unified-binary-architecture)
2. [Configuration Methods](#configuration-methods)
3. [Configuration File Format](#configuration-file-format)
4. [Slack Configuration](#slack-configuration)
5. [Agent-to-Agent (A2A) Authentication](#agent-to-agent-a2a-authentication)
6. [Command Line Flags](#command-line-flags)
7. [Multi-Environment Setup](#multi-environment-setup)
8. [Docker & Kubernetes](#docker--kubernetes)
9. [Best Practices](#best-practices)
10. [Troubleshooting](#troubleshooting)

---

## Unified Binary Architecture

Knowledge Agent uses a **single unified binary** (`knowledge-agent`) that can run in three different modes via the `--mode` flag.

### Available Modes

#### 1. All-in-One Mode (default)

Runs both services in a single process.

```bash
./bin/knowledge-agent --config config.yaml --mode all
# or simply (default):
./bin/knowledge-agent --config config.yaml
```

**When to use**:
- Single-server deployments
- Development and testing
- Small-scale production
- Simplified Docker deployments

**Advantages**:
- ✅ Single process to manage
- ✅ Simpler deployment
- ✅ Lower resource overhead
- ✅ Automatic service coordination

#### 2. Agent-Only Mode

Runs only the Knowledge Agent service (port 8081).

```bash
./bin/knowledge-agent --config config.yaml --mode agent
```

**When to use**:
- Microservices architecture
- Independent scaling of agent logic
- Multiple Slack bridges connecting to one agent
- Testing agent without Slack integration

**Exposes**:
- `GET /health` - Health check
- `GET /metrics` - Metrics
- `POST /api/query` - Query endpoint
- `POST /api/ingest-thread` - Ingestion endpoint

#### 3. Slack Bridge Mode

Runs only the Slack Bridge service (port 8080).

```bash
./bin/knowledge-agent --config config.yaml --mode slack-bot
```

**When to use**:
- Microservices architecture
- Independent scaling of Slack integration
- Multiple Slack workspaces sharing one agent
- Hot-reload Slack bridge without affecting agent

**Exposes**:
- `POST /slack/events` - Slack webhook endpoint

### Architecture Comparison

**Single Process (mode=all)**:
```
┌─────────────────────────────────┐
│   knowledge-agent (unified)     │
│                                 │
│  ┌──────────┐   ┌────────────┐ │
│  │  Slack   │──▶│   Agent    │ │
│  │  Bridge  │   │   Logic    │ │
│  │  :8080   │   │   :8081    │ │
│  └──────────┘   └────────────┘ │
└─────────────────────────────────┘
```

**Distributed (mode=agent + mode=slack-bot)**:
```
┌──────────────────┐       ┌──────────────────┐
│  knowledge-agent │       │  knowledge-agent │
│   (slack-bot)    │       │     (agent)      │
│                  │       │                  │
│  ┌────────────┐  │       │  ┌────────────┐  │
│  │   Slack    │──┼──────▶│  │   Agent    │  │
│  │   Bridge   │  │ HTTP  │  │   Logic    │  │
│  │   :8080    │  │       │  │   :8081    │  │
│  └────────────┘  │       │  └────────────┘  │
└──────────────────┘       └──────────────────┘
```

### Configuration

The `--mode` flag can be set via:

1. **Command line** (highest priority):
   ```bash
   ./bin/knowledge-agent --mode agent
   ```

2. **Environment variable**:
   ```bash
   export MODE=slack-bot
   ./bin/knowledge-agent
   ```

3. **Default**: `all` (if not specified)

### Deployment Examples

**Development (all-in-one)**:
```bash
make dev  # Runs with mode=all by default
```

**Production (distributed)**:
```bash
# Terminal 1: Agent
./bin/knowledge-agent --config /etc/agent/config.yaml --mode agent

# Terminal 2: Slack Bridge
./bin/knowledge-agent --config /etc/agent/config.yaml --mode slack-bot
```

**Docker Compose (distributed)**:
```yaml
services:
  agent:
    image: knowledge-agent:latest
    command: ["--config", "/config/config.yaml", "--mode", "agent"]

  slack-bridge:
    image: knowledge-agent:latest
    command: ["--config", "/config/config.yaml", "--mode", "slack-bot"]
```

**Kubernetes (distributed with scaling)**:
```yaml
# Agent deployment (scale for heavy workload)
apiVersion: apps/v1
kind: Deployment
metadata:
  name: knowledge-agent
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: agent
        image: knowledge-agent:latest
        args: ["--config", "/config/config.yaml", "--mode", "agent"]

---
# Slack Bridge deployment (usually single instance)
apiVersion: apps/v1
kind: Deployment
metadata:
  name: slack-bridge
spec:
  replicas: 1
  template:
    spec:
      containers:
      - name: bridge
        image: knowledge-agent:latest
        args: ["--config", "/config/config.yaml", "--mode", "slack-bot"]
```

---

## Configuration Methods

Knowledge Agent supports three configuration methods with the following priority:

1. **Config file via --config flag** (highest priority)
2. **config.yaml in current directory**
3. **Environment variables** (fallback)

## Method 1: Config File with Flag

Use the `--config` flag to specify a custom configuration file location.

### Examples

```bash
# Development with custom config (all-in-one)
./bin/knowledge-agent --config config.dev.yaml

# Production with absolute path (all-in-one)
./bin/knowledge-agent --config /etc/knowledge-agent/production.yaml

# Staging environment (all-in-one)
./bin/knowledge-agent --config configs/staging.yaml

# Distributed deployment (agent only)
./bin/knowledge-agent --config configs/production.yaml --mode agent

# Distributed deployment (Slack bridge only)
./bin/knowledge-agent --config configs/production.yaml --mode slack-bot
```

### Use Cases

- **Multi-environment setups**: Different configs for dev/staging/prod
- **Docker/Kubernetes**: Mount config as volume and reference it
- **Testing**: Use test-specific configuration files
- **Security**: Store configs in secure locations outside repo

## Method 2: Default config.yaml

Place a `config.yaml` file in the current working directory.

### Setup

```bash
# Copy example
cp config.yaml.example config.yaml

# Edit with your values
vim config.yaml

# Start service (auto-detects config.yaml, runs in all-in-one mode)
./bin/knowledge-agent

# Or with explicit mode
./bin/knowledge-agent --mode agent      # Agent only
./bin/knowledge-agent --mode slack-bot  # Slack bridge only
```

### Use Cases

- **Simple deployments**: Single environment, straightforward setup
- **Local development**: Quick start without flags
- **Docker Compose**: Mount config.yaml as volume

## Method 3: Environment Variables

Traditional environment variable configuration (fully backward compatible).

### Setup

```bash
# Create .env file
cp .env.example .env

# Edit with your values
vim .env

# Load environment and start
source .env
./bin/knowledge-agent  # Runs in all-in-one mode
```

### Use Cases

- **Legacy deployments**: Existing setups using env vars
- **12-factor apps**: Environment-based configuration
- **CI/CD**: Inject secrets via environment
- **PaaS deployments**: Platforms that manage env vars (Heroku, Cloud Run)

## Configuration File Format

### Structure

```yaml
# Anthropic API Configuration
anthropic:
  api_key: ${ANTHROPIC_API_KEY}  # Reference env var
  model: claude-sonnet-4-5-20250929

# Slack Configuration
slack:
  bot_token: ${SLACK_BOT_TOKEN}
  signing_secret: ${SLACK_SIGNING_SECRET}
  app_token: ${SLACK_APP_TOKEN}
  mode: socket  # or "webhook"
  bridge_api_key: ${SLACK_BRIDGE_API_KEY}

# Database Configuration
postgres:
  url: ${POSTGRES_URL}

redis:
  addr: localhost:6379
  ttl: 24h

# Ollama for Embeddings
ollama:
  base_url: http://localhost:11434/v1
  embedding_model: nomic-embed-text

# RAG Configuration
rag:
  chunk_size: 2000
  chunk_overlap: 1
  messages_per_chunk: 5
  similarity_threshold: 0.7
  max_results: 5

# Server Configuration
server:
  agent_port: 8081
  slack_bot_port: 8080

  # Rate Limiting (hardcoded defaults: 10 req/s, burst 20)
  # Note: Rate and burst are not currently configurable via config.yaml
  # Only trusted_proxies can be configured

  # Trusted proxies for X-Forwarded-For header
  # X-Forwarded-For is ONLY trusted when request comes from these IPs/CIDRs
  # This prevents IP spoofing attacks from untrusted sources
  # Leave empty to never trust X-Forwarded-For (use RemoteAddr only)
  trusted_proxies:
    - "10.0.0.0/8"       # Private network (kubernetes, docker)
    - "172.16.0.0/12"    # Private network
    - "192.168.0.0/16"   # Private network
    # - "203.0.113.1"    # Specific proxy IP

# Logging
log:
  level: info      # debug, info, warn, error
  format: console  # json, console
  output_path: stdout

# A2A Authentication (optional)
# Format: client_id: secret_token
# The secret is sent in X-API-Key header, client_id is used for logging
a2a_api_keys:
  root-agent: ka_secret_rootagent
  slack-bridge: ka_secret_slackbridge

# MCP Integration (optional)
mcp:
  enabled: true
  servers:
    - name: "filesystem"
      description: "Local filesystem operations"
      enabled: true
      transport_type: "command"
      command:
        path: "npx"
        args:
          - "-y"
          - "@modelcontextprotocol/server-filesystem"
          - "/workspace"
      tool_filter:
        - "read_file"
        - "list_directory"
      timeout: 30

    - name: "github"
      description: "GitHub repository access"
      enabled: false
      transport_type: "sse"
      endpoint: "https://api.github.com/mcp"
      auth:
        type: "bearer"
        token_env: "GITHUB_PAT"
      timeout: 30
```

### Environment Variable References

You can reference environment variables using `${VAR_NAME}` syntax:

```yaml
anthropic:
  api_key: ${ANTHROPIC_API_KEY}  # Loads from env var
```

This is useful for:
- **Secrets**: Keep sensitive values in environment, not in files
- **Dynamic configuration**: Change values without editing files
- **Security**: Avoid committing secrets to version control

---

## MCP Configuration

Model Context Protocol (MCP) integration enables the Knowledge Agent to access external data sources like filesystems, GitHub, databases, and more.

### Basic Setup

```yaml
mcp:
  enabled: true
  servers:
    - name: "filesystem"
      description: "Local filesystem operations"
      enabled: true
      transport_type: "command"
      command:
        path: "npx"
        args: ["-y", "@modelcontextprotocol/server-filesystem", "/workspace"]
      timeout: 30
```

### Configuration Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `enabled` | bool | No (default: false) | Enable MCP integration |
| `servers` | array | No | List of MCP servers to connect to |
| `servers[].name` | string | Yes | Server identifier for logging |
| `servers[].description` | string | No | Human-readable description |
| `servers[].enabled` | bool | No (default: true) | Enable this specific server |
| `servers[].transport_type` | string | Yes | "command", "sse", or "streamable" |
| `servers[].command` | object | Conditional | Required for command transport |
| `servers[].endpoint` | string | Conditional | Required for sse/streamable transport |
| `servers[].auth` | object | No | Authentication configuration |
| `servers[].tool_filter` | array | No | Whitelist of tool names (empty = all) |
| `servers[].timeout` | int | No (default: 30) | Connection timeout in seconds |

### Transport Types

#### Command Transport (Local)

For local MCP servers (npm packages, scripts):

```yaml
transport_type: "command"
command:
  path: "npx"
  args:
    - "-y"
    - "@modelcontextprotocol/server-filesystem"
    - "/workspace"
  env:
    DEBUG: "true"
```

#### SSE Transport (Remote)

For remote MCP servers over HTTP:

```yaml
transport_type: "sse"
endpoint: "https://api.github.com/mcp"
auth:
  type: "bearer"
  token_env: "GITHUB_PAT"
```

#### Streamable Transport (Custom)

For custom MCP implementations:

```yaml
transport_type: "streamable"
endpoint: "https://internal-mcp.company.com/v1"
auth:
  type: "bearer"
  token_env: "INTERNAL_API_KEY"
```

### Authentication

#### Bearer Token (Recommended)

```yaml
auth:
  type: "bearer"
  token_env: "GITHUB_PAT"  # Environment variable
```

Set the token:
```bash
export GITHUB_PAT=ghp_your_token_here
```

#### Basic Authentication

```yaml
auth:
  type: "basic"
  username: "myuser"
  password: "mypass"
```

### Tool Filtering

Restrict which MCP tools can be used:

```yaml
tool_filter:
  - "read_file"
  - "list_directory"
  - "search_files"
```

Empty or omitted = all tools allowed.

### Common MCP Servers

| Server | Package | Use Case |
|--------|---------|----------|
| Filesystem | `@modelcontextprotocol/server-filesystem` | File operations |
| GitHub | `@modelcontextprotocol/server-github` | Repository access |
| SQLite | `@modelcontextprotocol/server-sqlite` | Database queries |
| Google Drive | `@modelcontextprotocol/server-gdrive` | Document access |

Install with:
```bash
npm install -g @modelcontextprotocol/server-filesystem
# or use npx (no installation needed)
```

### Example Configurations

See `examples/mcp/` for complete examples:
- `config-filesystem.yaml` - Local filesystem access
- `config-github.yaml` - GitHub integration
- `config-multiple.yaml` - Multiple MCP servers

### Complete Documentation

For comprehensive MCP integration guide, see: **[docs/MCP_INTEGRATION.md](MCP_INTEGRATION.md)**

---

## Response Cleaner Configuration

The Response Cleaner uses Claude Haiku to clean agent narration from responses before sending them to users. This removes internal process descriptions like "Let me transfer you to..." or "I'm going to search for...".

### Basic Setup

```yaml
response_cleaner:
  enabled: true
  model: claude-haiku-4-5-20251001  # Default model
```

### Configuration Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable response cleaning |
| `model` | string | `claude-haiku-4-5-20251001` | Claude model to use for cleaning |

### How It Works

1. Agent generates response (may include narration about tools, transfers, etc.)
2. If response > 200 characters, cleaner is invoked
3. Haiku removes meta-commentary while preserving substantive content
4. Cleaned response is returned to user

### What Gets Removed

- Transfer phrases: "I'm going to transfer you to...", "The metrics agent says..."
- Process narration: "Let me search for that...", "I'm checking the database..."
- Redundant greetings and repetitions
- Meta-comments about tool usage

### What's Preserved

- All factual information and data
- Technical details and context
- Follow-up questions to the user
- The original language

### Requirements

- `ANTHROPIC_API_KEY` must be set (uses Anthropic API directly)
- Adds ~1-2 seconds latency per cleaned response
- Uses Haiku tokens (cost-effective for this use case)

### Graceful Degradation

- If API key is not set, cleaner is disabled with a warning
- If cleaning fails, original response is returned
- If cleaner returns empty response, original is used
- Responses < 200 characters skip cleaning (already concise)

---

## Context Summarizer Configuration

The Context Summarizer uses Claude Haiku to compress long conversation contexts before sending them to the main LLM. This prevents context window issues and reduces token costs when threads get very long.

### Basic Setup

```yaml
context_summarizer:
  enabled: true
  model: claude-haiku-4-5-20251001  # Default model
  token_threshold: 100000  # Summarize contexts above this estimated token count
```

### Configuration Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable context summarization |
| `model` | string | `claude-haiku-4-5-20251001` | Claude model to use for summarization |
| `token_threshold` | int | `8000` | Token threshold above which context is summarized |

### How It Works

1. Thread context is built from conversation messages
2. Token count is estimated (~4 characters per token)
3. If estimated tokens exceed threshold, summarizer is invoked
4. Haiku compresses context while preserving critical information
5. Compressed context is used in the LLM instruction

### What Gets Preserved

- Decisions and conclusions
- Technical details: configs, IPs, ports, service names
- Error messages and resolutions
- Numerical data and metrics
- Code snippets, commands, file paths
- Names, dates, and deadlines
- Action items and commitments

### What Gets Removed

- Repetitive greetings and pleasantries
- Redundant back-and-forth exchanges
- Filler text and conversational padding
- Duplicate information
- Meta-discussion about the conversation

### Requirements

- `ANTHROPIC_API_KEY` must be set
- Adds latency for contexts above threshold (typically 2-5 seconds)
- Uses Haiku tokens (cost-effective for compression)

### Graceful Degradation

- If API key is not set, summarizer is disabled with a warning
- If summarization fails, original context is used
- If summarizer returns empty response, original is used
- Contexts below threshold are passed through unchanged

---

## Retry Configuration

Both A2A and MCP integrations support configurable retry behavior for transient failures (502, 503, 504, 429, timeouts, connection errors).

### Basic Setup

```yaml
# A2A retry configuration
a2a:
  enabled: true
  retry:
    enabled: true
    max_retries: 3
    initial_delay: 500ms
    max_delay: 30s
    backoff_multiplier: 2.0
  sub_agents:
    - name: metrics_agent
      # ...

# MCP retry configuration
mcp:
  enabled: true
  retry:
    enabled: true
    max_retries: 3
    initial_delay: 500ms
    max_delay: 30s
    backoff_multiplier: 2.0
  servers:
    - name: github
      # ...
```

### Configuration Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `true` | Enable retry logic |
| `max_retries` | int | `3` | Maximum number of retry attempts |
| `initial_delay` | duration | `500ms` | Initial delay before first retry |
| `max_delay` | duration | `30s` | Maximum delay between retries |
| `backoff_multiplier` | float | `2.0` | Multiplier for exponential backoff |

### Retryable Errors

The retry logic automatically retries on:

**HTTP Status Codes:**
- 502 Bad Gateway
- 503 Service Unavailable
- 504 Gateway Timeout
- 429 Too Many Requests

**Network Errors:**
- Connection refused
- Connection reset
- Connection timeout
- DNS resolution failures
- I/O timeouts
- Broken pipe / EOF

### Backoff Strategy

Uses exponential backoff with jitter:
1. First retry: `initial_delay` (e.g., 500ms)
2. Second retry: `initial_delay * backoff_multiplier` (e.g., 1s)
3. Third retry: `initial_delay * backoff_multiplier²` (e.g., 2s)
4. ...capped at `max_delay`

Jitter of ±25% is added to prevent thundering herd.

### Logging

Retry attempts are logged for observability:
```
INFO  A2A retry attempt  agent=metrics_agent attempt=1 max_retries=3 delay_ms=500
WARN  A2A request failed with retryable status  agent=metrics_agent status_code=503
INFO  MCP HTTP retry attempt  server=github attempt=2 max_retries=3 delay_ms=1000
```

---

## A2A Inbound Configuration

The A2A (Agent-to-Agent) protocol endpoints are integrated into the main HTTP server on port 8081, allowing other ADK agents to call this agent.

### Basic Setup

```yaml
a2a:
  enabled: true
  # Public URL for agent discovery (used in agent card)
  # Required when deploying behind a reverse proxy or load balancer
  agent_url: http://knowledge-agent:8081
```

### A2A Endpoints

When `a2a.enabled: true`, the following endpoints are added to port 8081:

| Endpoint | Auth | Description |
|----------|------|-------------|
| `GET /.well-known/agent-card.json` | Public | Agent card for A2A discovery |
| `POST /a2a/invoke` | Required | A2A protocol invocation |

### Authentication

The `/a2a/invoke` endpoint uses the same authentication as `/api/*` endpoints:

- If `a2a_api_keys` is configured → All requests require `X-API-Key` header
- If `a2a_api_keys` is empty → Open mode (no authentication)

The agent card (`/.well-known/agent-card.json`) is always public to allow agent discovery.

### Unified Architecture

```
Port 8081 (Unified Server)
├── /api/query           (authenticated)
├── /api/ingest-thread   (authenticated)
├── /a2a/invoke          (authenticated)
├── /.well-known/agent-card.json (public)
├── /health              (public)
└── /metrics             (public)
```

---

## A2A Sub-agents Configuration

Knowledge Agent can delegate tasks to other ADK agents using the sub-agents pattern.

### Basic Setup

```yaml
a2a:
  enabled: true
  self_name: knowledge-agent  # Used for loop prevention
  max_call_depth: 5           # Maximum nested agent calls

  sub_agents:
    - name: metrics_agent
      description: "Query Prometheus metrics and analyze performance data"
      endpoint: http://metrics-agent:9000
      timeout: 30

    - name: logs_agent
      description: "Search and analyze application logs from Loki"
      endpoint: http://logs-agent:9000
      timeout: 30
```

### Configuration Fields

**A2A Section:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable A2A integration |
| `self_name` | string | `""` | This agent's name (for loop prevention) |
| `max_call_depth` | int | `5` | Maximum call chain depth |
| `sub_agents` | array | `[]` | List of remote agents |

**Sub-agent Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Sub-agent identifier |
| `description` | string | Yes | Describes capabilities (used by LLM) |
| `endpoint` | string | Yes | Agent card source URL |
| `timeout` | int | No | Reserved for future use (default: 30) |

### How Sub-agents Work

1. At startup, `remoteagent.NewA2A` creates remote agent wrappers
2. Sub-agents are added to the LLM agent
3. LLM automatically decides when to delegate based on descriptions
4. Delegation uses standard A2A protocol

### Complete Documentation

For comprehensive A2A integration guide, see: **[docs/A2A_TOOLS.md](A2A_TOOLS.md)**

---

## Best Practices

### Secrets Management

✅ **DO**:
- Use `${ENV_VAR}` references for secrets in YAML
- Store secrets in environment or secret managers
- Use different configs per environment
- Keep config.yaml in .gitignore if it contains secrets

❌ **DON'T**:
- Commit API keys or tokens to git
- Store production secrets in config files
- Use same config for all environments

### File Organization

```
# Recommended structure
knowledge-agent/
├── .env.example          # Template for env vars
├── config.yaml.example   # Template for YAML config
├── configs/              # Environment-specific configs (gitignored if they have secrets)
│   ├── development.yaml
│   ├── staging.yaml
│   └── production.yaml
└── .gitignore           # Ignore actual config files with secrets
```

### Version Control

```gitignore
# .gitignore
config.yaml
configs/*.yaml
!config.yaml.example
.env
!.env.example
```

### Development Best Practices

✅ Use `config.yaml` or custom config with `--config` flag
✅ Keep secrets in environment variables referenced with `${VAR}`
✅ Use make commands for convenience

```bash
# Good
make dev CONFIG=config.dev.yaml

# Also good
./bin/agent --config config.dev.yaml
```

### Production Best Practices

✅ Use explicit `--config` flag with absolute paths
✅ Store config in standard location (`/etc/knowledge-agent/`)
✅ Use environment variables for secrets
✅ Set proper file permissions (644 for config, 600 for secrets)

```bash
# Good
./bin/agent --config /etc/knowledge-agent/production.yaml

# Also good (with systemd)
ExecStart=/usr/local/bin/agent --config /etc/knowledge-agent/production.yaml
```

### Multi-Environment Best Practices

✅ Use separate config files per environment
✅ Name them clearly (`development.yaml`, `production.yaml`)
✅ Store in `configs/` directory
✅ Add configs with secrets to `.gitignore`

```
configs/
├── development.yaml    # Can commit (uses ${ENV} refs)
├── staging.yaml        # Can commit
├── production.yaml     # Can commit
└── local.yaml          # Don't commit (in .gitignore)
```

---

## Troubleshooting

### Config file not found

```
Error: config file not found: my-config.yaml
```

**Solution**: Check the path is correct relative to current directory or use absolute path.

### Environment variables not expanded

```yaml
anthropic:
  api_key: ${ANTHROPIC_API_KEY}  # Shows as literal string
```

**Solution**: Ensure the environment variable is set before starting the service:
```bash
export ANTHROPIC_API_KEY=sk-ant-xxx
./bin/agent --config config.yaml
```

### Required values missing

```
Error: required key ANTHROPIC_API_KEY missing value
```

**Solution**: Either:
1. Set the environment variable
2. Put the value directly in config.yaml (not recommended for secrets)
3. Use .env file and source it

### Permission denied reading config

```
Error: failed to read config: open production.yaml: permission denied
```

**Solution**: Check file permissions:
```bash
chmod 644 config.yaml
```

### Config not being used

**Symptom**: Changes to config.yaml not reflected

**Check**:
1. Are you using the right config file?
2. Did you restart the service?
3. Is there a `--config` flag overriding it?

**Debug**:
```bash
# See what config is loaded
./bin/agent --config my-config.yaml 2>&1 | grep "Configuration loaded"
```

### Environment variables not expanding

**Symptom**: `${ANTHROPIC_API_KEY}` appears as literal string

**Solution**: Ensure env var is set before starting:
```bash
export ANTHROPIC_API_KEY=sk-ant-xxx
./bin/agent --config config.yaml
```

**Or use .env file**:
```bash
source .env
./bin/agent --config config.yaml
```

### Relative vs Absolute Paths

**Issue**: Config file works from project root but not from bin/

**Solution**: Use absolute paths or paths relative to where you run from:
```bash
# From project root
./bin/agent --config config.yaml

# From bin/
./agent --config ../config.yaml

# Absolute path (works from anywhere)
./agent --config /home/user/knowledge-agent/config.yaml
```

### Slack Authentication Issues

#### Missing Scope Error

```
"error": "failed to get user info: missing_scope"
```

**Solution**: Add the `users:read` scope in Slack app settings and reinstall the app.

#### Socket Mode Not Connecting

**Symptom**: Slack bot not responding, socket connection fails

**Check**:
1. Verify `SLACK_APP_TOKEN` is set (xapp-... token)
2. Ensure Socket Mode is enabled in app settings
3. Check App-Level Token has `connections:write` scope
4. Verify `slack.mode` is set to "socket"

#### Webhook Verification Failed

**Symptom**: Slack events not received, verification fails

**Check**:
1. Verify `SLACK_SIGNING_SECRET` is correct
2. Ensure webhook URL is publicly accessible via HTTPS
3. Check Event Subscriptions are enabled
4. Verify bot events are subscribed (app_mention, etc.)

### A2A Authentication Issues

See the [A2A Troubleshooting](#a2a-troubleshooting) section above for detailed solutions.

---

## Summary

| Method | Priority | Use Case | Command |
|--------|----------|----------|---------|
| `--config` flag | 1 (highest) | Multi-env, production, custom paths | `./bin/agent --config prod.yaml` |
| `config.yaml` | 2 | Simple deployments, local dev | `./bin/agent` |
| Environment vars | 3 (fallback) | Legacy, 12-factor, CI/CD | `./bin/agent` |

Choose the method that best fits your deployment needs!

## Docker & Kubernetes

### Docker Compose

```yaml
# docker-compose.yml
services:
  agent:
    image: knowledge-agent:latest
    command: ["--config", "/config/production.yaml", "--mode", "agent"]
    volumes:
      - ./configs/production.yaml:/config/production.yaml:ro
    environment:
      ANTHROPIC_API_KEY: ${ANTHROPIC_API_KEY}
      SLACK_BOT_TOKEN: ${SLACK_BOT_TOKEN}

  slack-bot:
    image: knowledge-agent:latest
    command: ["--config", "/config/production.yaml", "--mode", "slack-bot"]
    volumes:
      - ./configs/production.yaml:/config/production.yaml:ro
    environment:
      SLACK_BOT_TOKEN: ${SLACK_BOT_TOKEN}
      SLACK_SIGNING_SECRET: ${SLACK_SIGNING_SECRET}

  # Alternative: Run both services in a single container
  all-in-one:
    image: knowledge-agent:latest
    command: ["--config", "/config/production.yaml", "--mode", "all"]
    volumes:
      - ./configs/production.yaml:/config/production.yaml:ro
    environment:
      ANTHROPIC_API_KEY: ${ANTHROPIC_API_KEY}
      SLACK_BOT_TOKEN: ${SLACK_BOT_TOKEN}
      SLACK_SIGNING_SECRET: ${SLACK_SIGNING_SECRET}
```

### Kubernetes ConfigMap

```yaml
# configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: knowledge-agent-config
data:
  config.yaml: |
    log:
      level: info
      format: json
    server:
      agent_port: 8081
      slack_bot_port: 8080
    redis:
      addr: redis-service:6379
    postgres:
      url: postgres://user:pass@postgres-service:5432/knowledge_agent
    ollama:
      base_url: http://ollama-service:11434/v1
    # ... rest of config
```

```yaml
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: knowledge-agent
spec:
  replicas: 2
  template:
    spec:
      containers:
      - name: agent
        image: knowledge-agent:latest
        command: ["./agent", "--config", "/config/config.yaml"]
        ports:
        - containerPort: 8081
        volumeMounts:
        - name: config
          mountPath: /config
        env:
        - name: ANTHROPIC_API_KEY
          valueFrom:
            secretKeyRef:
              name: knowledge-agent-secrets
              key: anthropic-api-key
      volumes:
      - name: config
        configMap:
          name: knowledge-agent-config
```

### Docker Example with Environment-Specific Configs

```bash
# Dockerfile
FROM golang:1.21 AS builder
WORKDIR /app
COPY . .
RUN make build

FROM debian:bookworm-slim
COPY --from=builder /app/bin/agent /usr/local/bin/
COPY --from=builder /app/bin/slack-bot /usr/local/bin/
CMD ["agent", "--config", "/config/production.yaml"]
```

```bash
# Build and run
docker build -t knowledge-agent:latest .
docker run -v $(pwd)/configs/production.yaml:/config/production.yaml:ro \
  -e ANTHROPIC_API_KEY=$ANTHROPIC_API_KEY \
  knowledge-agent:latest
```

## Configuration Examples

### Development: Socket Mode + Debug Logs

```yaml
# config.dev.yaml
log:
  level: debug
  format: console
  output_path: stdout

slack:
  bot_token: ${SLACK_BOT_TOKEN}
  app_token: ${SLACK_APP_TOKEN}
  mode: socket

anthropic:
  api_key: ${ANTHROPIC_API_KEY}
  model: claude-sonnet-4-5-20250929

redis:
  addr: localhost:6379

postgres:
  url: postgres://dev:dev@localhost:5432/knowledge_agent_dev
```

```bash
./bin/agent --config config.dev.yaml
./bin/slack-bot --config config.dev.yaml
```

### Production: Webhook Mode + JSON Logs

```yaml
# config.prod.yaml
log:
  level: info
  format: json
  output_path: /var/log/knowledge-agent/app.log

slack:
  bot_token: ${SLACK_BOT_TOKEN}
  signing_secret: ${SLACK_SIGNING_SECRET}
  mode: webhook
  bridge_api_key: ${SLACK_BRIDGE_API_KEY}

anthropic:
  api_key: ${ANTHROPIC_API_KEY}
  model: claude-sonnet-4-5-20250929

server:
  agent_port: 8081
  slack_bot_port: 8080

redis:
  addr: redis-cluster:6379
  ttl: 24h

postgres:
  url: ${POSTGRES_URL}

a2a_api_keys:
  slack-bridge: ka_secret_slackbridge
  root-agent: ka_secret_rootagent
```

```bash
./bin/agent --config /etc/knowledge-agent/config.prod.yaml
./bin/slack-bot --config /etc/knowledge-agent/config.prod.yaml
```

### Testing: Custom Ports + Test Database

```yaml
# config.test.yaml
log:
  level: debug
  format: console

server:
  agent_port: 9081
  slack_bot_port: 9080

redis:
  addr: localhost:6380

postgres:
  url: postgres://test:test@localhost:5433/knowledge_agent_test

ollama:
  base_url: http://localhost:11434/v1

slack:
  bot_token: ${SLACK_BOT_TOKEN}
  mode: socket
  app_token: ${SLACK_APP_TOKEN}
```

```bash
./bin/agent --config config.test.yaml
./bin/slack-bot --config config.test.yaml
```

---

## Slack Configuration

### Required Scopes

When creating or updating your Slack App at https://api.slack.com/apps, you need these Bot Token Scopes under **OAuth & Permissions**:

#### Basic Scopes (Required)

1. **`app_mentions:read`** - Receive mentions to the bot
2. **`chat:write`** - Send messages
3. **`channels:history`** - Read public channel history
4. **`groups:history`** - Read private channel history
5. **`im:history`** - Read direct messages
6. **`mpim:history`** - Read group messages

#### User Information Scope

7. **`users:read`** - **REQUIRED** to get user information (real name, username)
   - Without this scope, you'll get error: `missing_scope`
   - Needed for personalized responses with names

#### File and Image Scopes

8. **`files:read`** - Read shared files
   - Required for image/diagram analysis

### How to Add Scopes

1. Go to https://api.slack.com/apps
2. Select your app
3. Go to **OAuth & Permissions** in the sidebar
4. Under **Scopes** → **Bot Token Scopes**, click **Add an OAuth Scope**
5. Add each scope from the list above
6. **IMPORTANT**: After adding new scopes:
   - Go to **Install App** in the sidebar
   - Click **Reinstall to Workspace**
   - Authorize the new permissions
7. Copy the new **Bot User OAuth Token** (starts with `xoxb-`)
8. Update `SLACK_BOT_TOKEN` in your config

### Verify Current Scopes

```bash
# Using Slack API
curl -H "Authorization: Bearer xoxb-YOUR-TOKEN" \
  https://slack.com/api/auth.test
```

### Common Errors

#### Missing Scope Error

```
"error": "failed to get user info: missing_scope"
```

**Solution**: Add the `users:read` scope and reinstall the app in the workspace.

### Event Subscriptions

Make sure you also have configured:
- **Request URL**: Your webhook URL (for webhook mode)
- **Subscribe to bot events**:
  - `app_mention` - When the bot is mentioned
  - `message.im` - Direct messages (optional)

### Slack Mode Configuration

#### Socket Mode (Development)

Best for local development - no public endpoint required.

**Configuration**:
```yaml
slack:
  mode: socket
  app_token: ${SLACK_APP_TOKEN}  # xapp-... token
  bot_token: ${SLACK_BOT_TOKEN}
```

**Setup Steps**:
1. Go to **Socket Mode** in the app settings
2. Enable Socket Mode
3. Generate an **App-Level Token** with scope `connections:write`
4. Use that token as `SLACK_APP_TOKEN`

**Pros**:
- No public HTTPS endpoint needed
- Perfect for local development
- Easy to test

**Cons**:
- WebSocket connection (less scalable)
- Not recommended for production

#### Webhook Mode (Production)

Best for production - scalable and stateless.

**Configuration**:
```yaml
slack:
  mode: webhook
  signing_secret: ${SLACK_SIGNING_SECRET}
  bot_token: ${SLACK_BOT_TOKEN}
```

**Setup Steps**:
1. Deploy your service with public HTTPS endpoint
2. Go to **Event Subscriptions** in app settings
3. Enable Events and set **Request URL**: `https://your-domain.com/slack/events`
4. Subscribe to bot events: `app_mention`, etc.
5. Copy the **Signing Secret** from **Basic Information**
6. Use that as `SLACK_SIGNING_SECRET`

**Pros**:
- Scalable, stateless
- Recommended for production
- Standard HTTP webhooks

**Cons**:
- Requires public HTTPS endpoint
- More setup for local development

### Token Summary

| Token | Format | Used For | Config Variable |
|-------|--------|----------|-----------------|
| Bot User OAuth Token | `xoxb-...` | Making API calls | `SLACK_BOT_TOKEN` |
| App-Level Token | `xapp-...` | Socket Mode connection | `SLACK_APP_TOKEN` (socket mode only) |
| Signing Secret | `<hex-string>` | Webhook verification | `SLACK_SIGNING_SECRET` (webhook mode only) |

---

## Agent-to-Agent (A2A) Authentication

The Knowledge Agent exposes HTTP endpoints that can be called by external agents or the Slack Bridge. Authentication is optional but recommended for production.

### Overview

**Authentication Modes**:
- **Development mode**: No authentication (set `A2A_API_KEYS` empty or omit it)
- **Production mode**: API key authentication (configure `A2A_API_KEYS`)

**Supported Authentication Methods**:
1. **API Key Authentication** (Recommended) - via `X-API-Key` header
2. **Slack Signature Authentication** - via `X-Slack-Signature` and `X-Slack-Request-Timestamp` headers

### Configuration

#### Knowledge Agent (.env or config.yaml)

Configure which API keys are accepted:

```yaml
# config.yaml
# Format: client_id: secret_token
a2a_api_keys:
  root-agent: ka_secret_rootagent
  slack-bridge: ka_secret_slackbridge
```

Or via environment:
```bash
# .env
A2A_API_KEYS='{"ka_rootagent":"root-agent","ka_slackbridge":"slack-bridge"}'
```

- **Key format**: `ka_` prefix followed by unique identifier
- **Value format**: Human-readable caller ID for logging
- **Empty/omitted**: Open mode (no authentication required)

#### Slack Bridge (.env or config.yaml)

Configure the API key the Slack Bridge uses:

```yaml
# config.yaml
slack:
  bridge_api_key: ${SLACK_BRIDGE_API_KEY}
```

Or via environment:
```bash
# .env
SLACK_BRIDGE_API_KEY=ka_slackbridge
```

**Important**: The API key must be included in the Knowledge Agent's `A2A_API_KEYS` configuration.

### API Endpoints

#### POST /api/query

Query the knowledge base and get AI-powered responses.

**Authentication**: Required (if A2A_API_KEYS is configured)

**Request Example**:
```bash
curl -X POST http://localhost:8081/api/query \
  -H "Content-Type: application/json" \
  -H "X-API-Key: ka_rootagent" \
  -d '{
    "question": "How do we handle production deployments?",
    "channel_id": "external",
    "thread_ts": "optional-thread-id",
    "messages": [
      {
        "user": "U123",
        "text": "Previous context message",
        "ts": "1234567890.123456"
      }
    ]
  }'
```

**Request Fields**:
- `question` (required): The user's question or message
- `channel_id` (optional): Channel identifier (use "external" for non-Slack sources)
- `thread_ts` (optional): Thread identifier for grouping related queries
- `messages` (optional): Previous conversation context

**Response**:
```json
{
  "success": true,
  "answer": "Based on the knowledge base, our deployment process involves...",
  "session_id": "query-external-1234567890"
}
```

#### POST /api/ingest-thread

Save conversation threads to the knowledge base.

**Authentication**: Required (if A2A_API_KEYS is configured)

**Request Example**:
```bash
curl -X POST http://localhost:8081/api/ingest-thread \
  -H "Content-Type: application/json" \
  -H "X-API-Key: ka_rootagent" \
  -d '{
    "thread_ts": "1234567890.123456",
    "channel_id": "external",
    "messages": [
      {
        "user": "U123",
        "text": "We should document our new deployment process",
        "ts": "1234567890.123456",
        "type": "message"
      }
    ]
  }'
```

**Response**:
```json
{
  "success": true,
  "message": "Thread ingested successfully",
  "session_id": "ingest-external-1234567890"
}
```

### Security Best Practices

1. **Generate secure API keys**:
   ```bash
   # Generate a secure random key
   openssl rand -hex 32
   # Use format: ka_<generated-key>
   ```

2. **Rotate keys periodically**: Update `A2A_API_KEYS` and restart the service

3. **Use HTTPS in production**: Never send API keys over unencrypted connections

4. **Limit key scope**: Create separate keys for each external agent

5. **Monitor access**: Check logs for `caller_id` to audit API usage
   ```
   [Query] caller=root-agent question='...' channel=external
   [IngestThread] caller=slack-bridge thread=... channel=C123
   ```

### Client Examples

#### Python Client

```python
import requests

class KnowledgeAgentClient:
    def __init__(self, base_url, api_key):
        self.base_url = base_url
        self.api_key = api_key

    def query(self, question, channel_id="external", context=None):
        """Query the knowledge base"""
        url = f"{self.base_url}/api/query"
        headers = {
            "Content-Type": "application/json",
            "X-API-Key": self.api_key
        }
        payload = {
            "question": question,
            "channel_id": channel_id
        }
        if context:
            payload["messages"] = context

        response = requests.post(url, json=payload, headers=headers)
        response.raise_for_status()
        return response.json()

# Usage
client = KnowledgeAgentClient(
    base_url="http://localhost:8081",
    api_key="ka_rootagent"
)

result = client.query("What is our deployment process?")
print(result["answer"])
```

#### Go Client

```go
package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "net/http"
)

type KnowledgeAgentClient struct {
    baseURL string
    apiKey  string
    client  *http.Client
}

func NewClient(baseURL, apiKey string) *KnowledgeAgentClient {
    return &KnowledgeAgentClient{
        baseURL: baseURL,
        apiKey:  apiKey,
        client:  &http.Client{},
    }
}

func (c *KnowledgeAgentClient) Query(question, channelID string) (map[string]interface{}, error) {
    payload := map[string]interface{}{
        "question":   question,
        "channel_id": channelID,
    }

    body, _ := json.Marshal(payload)
    req, _ := http.NewRequest("POST", c.baseURL+"/api/query", bytes.NewBuffer(body))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("X-API-Key", c.apiKey)

    resp, err := c.client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    var result map[string]interface{}
    json.NewDecoder(resp.Body).Decode(&result)
    return result, nil
}

func main() {
    client := NewClient("http://localhost:8081", "ka_rootagent")
    result, _ := client.Query("What is our deployment process?", "external")
    fmt.Println(result["answer"])
}
```

### Integration with ADK-Based Agents

If your agent uses ADK (Agent Development Kit), you can integrate the Knowledge Agent as a custom tool.

#### Architecture

```
┌─────────────────────────┐
│  Your Agent (ADK)       │
│  ┌──────────────────┐   │
│  │  LLM (Claude)    │   │
│  └────────┬─────────┘   │
│           │              │
│  ┌────────▼─────────┐   │
│  │  Custom Tool:    │   │
│  │  query_knowledge │   │──HTTP──┐
│  └──────────────────┘   │        │
└─────────────────────────┘        │
                                   │ X-API-Key: ka_rootagent
                                   │
                                   ▼
                    ┌──────────────────────────┐
                    │  Knowledge Agent (ADK)   │
                    │  - search_memory         │
                    │  - save_to_memory        │
                    │  - fetch_url             │
                    └──────────────────────────┘
```

See the detailed integration code examples in the original A2A documentation for implementing custom tools with ADK.

### A2A Troubleshooting

#### 401 Unauthorized

**Problem**: API key is invalid or missing

**Solutions**:
1. Check that `X-API-Key` header is being sent
2. Verify the key exists in Knowledge Agent's `A2A_API_KEYS`
3. Check for typos in the API key
4. Ensure Knowledge Agent was restarted after updating `A2A_API_KEYS`

#### No Authentication Required (Open Mode)

**Problem**: Want to enable authentication but requests work without API key

**Solution**: Set `A2A_API_KEYS` in Knowledge Agent's config and restart

#### Slack Bridge Can't Reach Knowledge Agent

**Problem**: Slack Bridge returns "Could not reach Knowledge Agent"

**Solutions**:
1. Verify Knowledge Agent is running: `curl http://localhost:8081/health`
2. Check `AGENT_URL` in Slack Bridge configuration
3. Ensure `SLACK_BRIDGE_API_KEY` is configured if authentication is enabled
4. Check logs for authentication errors

### A2A Logging

All authenticated requests log the caller ID:

```
[Query] caller=root-agent question='What is our deployment process?' channel=external
[Query] caller=slack-bridge question='How do I deploy?' channel=C123ABC
[IngestThread] caller=slack-bridge thread=1234567890.123456 channel=C123ABC messages=5
```

Use these logs to:
- Monitor API usage per agent
- Debug authentication issues
- Audit knowledge base access
- Track which agents are querying specific information

---

## Command Line Flags

### Quick Reference

```bash
# Use default config.yaml (if exists) or env vars
./bin/agent
./bin/slack-bot

# Use specific config file
./bin/agent --config my-config.yaml
./bin/slack-bot --config /etc/knowledge-agent/production.yaml

# With make (development)
make dev                              # Default behavior
make dev CONFIG=config.dev.yaml       # Custom config
make dev-agent CONFIG=config.test.yaml
```

### Configuration Priority

The system loads configuration in this order:

1. **Explicit --config flag** (highest priority)
   - If provided and file exists → use it
   - If provided but file not found → ERROR

2. **config.yaml in current directory**
   - If exists → use it
   - If not exists → continue to next

3. **Environment variables** (fallback)
   - Load from environment
   - Compatible with .env files

### Flag Usage Examples

#### Development

```bash
# Option 1: Use .env file (traditional)
cp .env.example .env
vim .env
make dev

# Option 2: Use config.yaml
cp config.yaml.example config.yaml
vim config.yaml
make dev

# Option 3: Use custom config
cp config.yaml.example config.dev.yaml
vim config.dev.yaml
make dev CONFIG=config.dev.yaml
```

#### Production

```bash
# Build binaries
make build

# Run with production config
./bin/agent --config /etc/knowledge-agent/production.yaml
./bin/slack-bot --config /etc/knowledge-agent/production.yaml
```

#### Multi-Environment

```bash
# Directory structure
configs/
├── development.yaml
├── staging.yaml
└── production.yaml

# Development
./bin/agent --config configs/development.yaml

# Staging
./bin/agent --config configs/staging.yaml

# Production
./bin/agent --config configs/production.yaml
```

### Makefile Usage

#### Standard Commands

```bash
# Both services with default config
make dev

# Both services with custom config
make dev CONFIG=my-config.yaml

# Agent only with custom config
make dev-agent CONFIG=configs/test.yaml

# Slack bot only with custom config
make dev-slack CONFIG=configs/development.yaml
```

#### Environment-Specific

```bash
# Development environment
make dev CONFIG=configs/development.yaml

# Staging environment
make dev CONFIG=configs/staging.yaml

# Testing
make dev CONFIG=configs/test.yaml
```

### Validation

#### Check what config is loaded

```bash
# The services log on startup:
# "Configuration loaded from: /path/to/config.yaml"
# or
# "Configuration loaded from: environment variables"
```

#### Test config file

```bash
# Try loading the config (will fail if config has errors)
./bin/agent --config test.yaml
# Look for: "Configuration loaded from: test.yaml"
```

### Flag Error Messages

#### File not found

```
Error: config file not found: my-config.yaml
```

**Solution**: Check path relative to current directory or use absolute path.

#### Permission denied

```
Error: failed to read config: open config.yaml: permission denied
```

**Solution**: Check file permissions
```bash
chmod 644 config.yaml
```

#### Invalid YAML

```
Error: failed to unmarshal config: yaml: line 5: mapping values are not allowed in this context
```

**Solution**: Check YAML syntax, ensure proper indentation.

---

## Multi-Environment Setup

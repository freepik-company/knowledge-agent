# Configuration Guide

This comprehensive guide covers all configuration aspects of Knowledge Agent, including file-based configuration, environment variables, Slack setup, A2A authentication, and command-line flags.

## Table of Contents

1. [Configuration Methods](#configuration-methods)
2. [Configuration File Format](#configuration-file-format)
3. [Slack Configuration](#slack-configuration)
4. [Agent-to-Agent (A2A) Authentication](#agent-to-agent-a2a-authentication)
5. [Command Line Flags](#command-line-flags)
6. [Multi-Environment Setup](#multi-environment-setup)
7. [Docker & Kubernetes](#docker--kubernetes)
8. [Best Practices](#best-practices)
9. [Troubleshooting](#troubleshooting)

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
# Development with custom config
./bin/agent --config config.dev.yaml
./bin/slack-bot --config config.dev.yaml

# Production with absolute path
./bin/agent --config /etc/knowledge-agent/production.yaml
./bin/slack-bot --config /etc/knowledge-agent/production.yaml

# Staging environment
./bin/agent --config configs/staging.yaml
./bin/slack-bot --config configs/staging.yaml
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

# Start services (auto-detects config.yaml)
./bin/agent
./bin/slack-bot
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
./bin/agent
./bin/slack-bot
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

# Server Ports
server:
  agent_port: 8081
  slack_bot_port: 8080

# Logging
log:
  level: info      # debug, info, warn, error
  format: console  # json, console
  output_path: stdout

# A2A Authentication (optional)
a2a_api_keys:
  ka_rootagent: root-agent
  ka_slackbridge: slack-bridge

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
    command: ["./agent", "--config", "/config/production.yaml"]
    volumes:
      - ./configs/production.yaml:/config/production.yaml:ro
    environment:
      ANTHROPIC_API_KEY: ${ANTHROPIC_API_KEY}
      SLACK_BOT_TOKEN: ${SLACK_BOT_TOKEN}

  slack-bot:
    image: knowledge-agent:latest
    command: ["./slack-bot", "--config", "/config/production.yaml"]
    volumes:
      - ./configs/production.yaml:/config/production.yaml:ro
    environment:
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
  ka_slackbridge: slack-bridge
  ka_rootagent: root-agent
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
a2a_api_keys:
  ka_rootagent: root-agent
  ka_slackbridge: slack-bridge
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

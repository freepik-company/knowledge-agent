# A2A Tool Integration Guide

This document describes how to configure Knowledge Agent to call tools exposed by other agents using Agent-to-Agent (A2A) communication.

## Overview

A2A tool integration allows Knowledge Agent to leverage capabilities from other agents in your infrastructure. For example:
- Call a **logs-agent** to search application logs
- Query a **metrics-agent** for performance data
- Page on-call via an **oncall-agent**

The LLM intelligently decides when to use these tools based on user queries, just like it uses the built-in `search_memory` and `save_to_memory` tools.

## Architecture

```
knowledge-agent                    external-agent
     │                                  │
     │  ┌──────────────────────────┐   │
     │  │ A2A Toolset              │   │
     │  │ - search_logs            │───│── HTTP POST /api/query
     │  │ - get_error_context      │   │   + Authentication
     │  └──────────────────────────┘   │   + Loop Prevention Headers
     │                                  │
     │  Headers propagated:            │
     │  X-Request-ID: uuid             │
     │  X-Call-Chain: knowledge-agent  │
     │  X-Call-Depth: 1                │
```

## Configuration

Add the `a2a` section to your `config.yaml`:

```yaml
a2a:
  enabled: true
  self_name: "knowledge-agent"  # This agent's identifier for loop prevention
  max_call_depth: 5             # Maximum nested agent calls (prevents infinite loops)

  agents:
    # Agent with API Key authentication
    - name: "logs-agent"
      description: "Search and analyze application logs"
      endpoint: "http://logs-agent:8081"
      timeout: 30
      auth:
        type: "api_key"
        header: "X-API-Key"
        key_env: "LOGS_AGENT_API_KEY"
      tools:
        - name: "search_logs"
          description: "Search logs by query, time range, and severity level"
        - name: "get_error_context"
          description: "Get surrounding log context for a specific error"

    # Agent with Bearer token authentication
    - name: "metrics-agent"
      description: "Query metrics from Prometheus/Grafana"
      endpoint: "http://metrics-agent:8081"
      timeout: 30
      auth:
        type: "bearer"
        token_env: "METRICS_AGENT_TOKEN"
      tools:
        - name: "query_metrics"
          description: "Query time-series metrics with PromQL"

    # Agent with OAuth2 (Keycloak) authentication
    - name: "oncall-agent"
      description: "Manage on-call schedules and paging"
      endpoint: "http://oncall-agent:8081"
      timeout: 30
      auth:
        type: "oauth2"
        token_url: "https://keycloak.example.com/realms/agents/protocol/openid-connect/token"
        client_id_env: "ONCALL_CLIENT_ID"
        client_secret_env: "ONCALL_CLIENT_SECRET"
        scopes: ["agent:call"]
      tools:
        - name: "get_oncall_schedule"
          description: "Get the current on-call schedule"
        - name: "page_oncall"
          description: "Send an alert to the current on-call person"

    # Agent without authentication (internal/trusted network)
    - name: "internal-agent"
      description: "Internal service health checks"
      endpoint: "http://internal-agent:8081"
      timeout: 30
      auth:
        type: "none"
      tools:
        - name: "health_check"
          description: "Check health of internal services"
```

## Authentication Types

| Type | Description | Required Config |
|------|-------------|-----------------|
| `api_key` | Custom header with API key | `header`, `key_env` |
| `bearer` | `Authorization: Bearer <token>` | `token_env` |
| `oauth2` | OAuth2 client credentials flow | `token_url`, `client_id_env`, `client_secret_env`, `scopes` |
| `none` | No authentication | - |

### API Key Authentication

```yaml
auth:
  type: "api_key"
  header: "X-API-Key"           # Header name (default: X-API-Key)
  key_env: "LOGS_AGENT_API_KEY" # Environment variable with the key
```

### Bearer Token Authentication

```yaml
auth:
  type: "bearer"
  token_env: "METRICS_AGENT_TOKEN" # Environment variable with the token
```

### OAuth2 Client Credentials

```yaml
auth:
  type: "oauth2"
  token_url: "https://keycloak.example.com/realms/agents/protocol/openid-connect/token"
  client_id_env: "ONCALL_CLIENT_ID"
  client_secret_env: "ONCALL_CLIENT_SECRET"
  scopes: ["agent:call", "read:schedule"]  # Optional scopes
```

> **⚠️ Security Requirement**: The `token_url` **must use HTTPS**. HTTP URLs are rejected to protect OAuth2 credentials in transit.

The OAuth2 implementation:
- Uses client credentials grant type
- **Requires HTTPS** for token endpoint (HTTP is rejected)
- Caches tokens until 30 seconds before expiry
- Automatically refreshes expired tokens

## Loop Prevention

A2A calls include headers to prevent infinite loops between agents:

| Header | Description | Example |
|--------|-------------|---------|
| `X-Request-ID` | Unique ID for the original request | `550e8400-e29b-41d4-a716-446655440000` |
| `X-Call-Chain` | Comma-separated list of agents in the chain | `knowledge-agent,logs-agent` |
| `X-Call-Depth` | Current depth in the call chain | `2` |

### How Loop Prevention Works

1. When a request arrives, the middleware extracts the call chain from headers
2. If `self_name` is already in the chain → **508 Loop Detected**
3. If `X-Call-Depth >= max_call_depth` → **508 Loop Detected**
4. Otherwise, add `self_name` to the chain and continue

### Testing Loop Prevention

```bash
# This should return 508 Loop Detected
curl -X POST http://localhost:8081/api/query \
  -H "Content-Type: application/json" \
  -H "X-Call-Chain: knowledge-agent" \
  -H "X-Call-Depth: 1" \
  -d '{"question": "test"}'
```

## Security

### Endpoint Validation (SSRF Protection)

A2A endpoints are validated before use to prevent Server-Side Request Forgery attacks.

**Blocked endpoints:**

| Pattern | Reason |
|---------|--------|
| `169.254.169.254` | AWS/Azure/GCP metadata service |
| `169.254.*.*` | Link-local addresses (metadata range) |
| `metadata.google.internal` | GCP metadata service |
| `file://`, `ftp://`, `gopher://` | Unsafe URL schemes |

**Allowed endpoints:**

| Pattern | Reason |
|---------|--------|
| `http://`, `https://` | Standard protocols |
| `localhost`, `127.0.0.1` | Internal agent communication |
| `10.x.x.x`, `192.168.x.x` | Private networks (expected for A2A) |

> **Note**: A2A is designed for internal agent communication. Internal/private IPs are intentionally allowed since agents typically communicate within the same network.

**Example blocked configuration:**
```yaml
# ❌ This will fail validation
agents:
  - name: "malicious"
    endpoint: "http://169.254.169.254/latest/meta-data"
```

**Error message:**
```
failed to create A2A toolset: invalid endpoint for agent malicious: access to cloud metadata service '169.254.169.254' is not allowed
```

## Environment Variables

Set these in your `.env` file or deployment configuration:

```bash
# Generate secure values - DO NOT use placeholders in production

# API Key authentication
LOGS_AGENT_API_KEY=$(openssl rand -hex 32)

# Bearer token authentication
METRICS_AGENT_TOKEN=$(openssl rand -hex 32)

# OAuth2 authentication (obtain from your IdP admin console)
ONCALL_CLIENT_ID=<from-keycloak-admin-console>
ONCALL_CLIENT_SECRET=<from-keycloak-admin-console>
```

> **⚠️ Never commit real credentials**. Use environment variables or secret management (Vault, AWS Secrets Manager, Kubernetes Secrets, etc.).

## Tool Execution

When the LLM decides to use an A2A tool:

1. The tool receives the query from the LLM
2. Knowledge Agent sends an HTTP POST to the remote agent's `/api/query` endpoint
3. The request includes:
   - Authentication headers (based on config)
   - Loop prevention headers
   - The query as JSON body
4. The remote agent processes the query
5. The response is returned to the LLM for further processing

### Request Format

```json
{
  "question": "Search for errors in the last hour",
  "metadata": {
    "tool_name": "search_logs"
  }
}
```

### Response Format

```json
{
  "success": true,
  "answer": "Found 15 errors in the last hour..."
}
```

## Graceful Degradation

A2A toolset creation follows the same graceful degradation pattern as MCP:

- If an agent fails to initialize (e.g., missing credentials), it's skipped
- Other agents continue to work
- Warnings are logged but the agent starts successfully

```log
# Example log output
WARN  Failed to create A2A toolset, skipping  agent=oncall-agent error="OAuth2 client ID env var not set"
INFO  A2A toolsets created successfully  count=2
```

## Troubleshooting

### Agent not receiving requests

1. Check that `a2a.enabled: true` in config
2. Verify `endpoint` is correct and reachable
3. Check authentication configuration

### 401 Unauthorized errors

1. Verify environment variables are set:
   ```bash
   echo $LOGS_AGENT_API_KEY
   ```
2. Check that the header name matches what the remote agent expects
3. For OAuth2, verify token URL is accessible

### 508 Loop Detected errors

This is expected behavior when:
- The same agent is called twice in a request chain
- The call depth exceeds `max_call_depth`

If unexpected, check:
1. Your agent topology for circular dependencies
2. That `self_name` is unique across all agents

### Timeouts

1. Increase `timeout` in agent config
2. Check network connectivity to the remote agent
3. Verify the remote agent is responding in time

### Missing tools in LLM

1. Check logs for "A2A toolset created" messages
2. Verify `tools` array in config has entries with `name` and `description`
3. Restart the agent to reload configuration

## Example Use Case

User asks: "What errors happened in the payment service in the last hour?"

1. LLM decides to use `search_logs` tool from `logs-agent`
2. Knowledge Agent calls logs-agent with the query
3. logs-agent searches logs and returns results
4. LLM synthesizes the response for the user

```
User: What errors happened in the payment service in the last hour?

Agent: [Calls search_logs tool with query]

logs-agent: Found 15 errors in the payment service:
- 10x "Payment gateway timeout" (10:00-10:30)
- 3x "Invalid card number" (10:15-10:45)
- 2x "Insufficient funds" (10:30-11:00)

Agent: In the last hour, there were 15 errors in the payment service:
- **10 payment gateway timeouts** between 10:00-10:30 (likely network issue)
- **3 invalid card number errors** (user input validation)
- **2 insufficient funds errors** (expected business errors)

The gateway timeouts cluster suggests a potential infrastructure issue around 10:15.
```

## Related Documentation

- [SECURITY.md](SECURITY.md) - Authentication, permissions, and SSRF protection
- [CONFIGURATION.md](CONFIGURATION.md) - Full configuration reference
- [MCP_INTEGRATION.md](MCP_INTEGRATION.md) - Similar pattern for MCP tools
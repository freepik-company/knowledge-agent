# A2A Integration Guide

This document describes how to configure Knowledge Agent for Agent-to-Agent (A2A) communication using the Google ADK standard.

## Overview

Knowledge Agent supports A2A in two ways:

1. **Inbound A2A** (Port 8081): Other agents can call this agent using the standard A2A protocol
2. **Outbound A2A** (Sub-agents): This agent can delegate tasks to other ADK agents

## Architecture

```
                    External ADK Agents
                           │
                           │ A2A Protocol
                           ▼
┌──────────────────────────────────────────────────────────┐
│                   knowledge-agent                         │
│                                                          │
│  ┌───────────────────────────────────────────────────┐   │
│  │            Unified HTTP Server (Port 8081)        │   │
│  │                                                   │   │
│  │  /api/query        (authenticated)               │   │
│  │  /api/ingest       (authenticated)               │   │
│  │  /a2a/invoke       (authenticated)               │   │
│  │  /.well-known/agent-card.json  (public)          │   │
│  │  /health, /metrics (public)                      │   │
│  └────────────────────────┬──────────────────────────┘   │
│                           │                              │
│                     ┌─────▼─────┐                        │
│                     │ LLM Agent │                        │
│                     │ (Claude)  │                        │
│                     └─────┬─────┘                        │
│                           │                              │
│            ┌──────────────┼──────────────┐               │
│            │              │              │               │
│            ▼              ▼              ▼               │
│      ┌──────────┐  ┌──────────┐  ┌──────────┐           │
│      │ Memory   │  │   MCP    │  │ SubAgents │           │
│      │ Tools    │  │ Toolsets │  │ (A2A)    │           │
│      └──────────┘  └──────────┘  └────┬─────┘           │
│                                       │                  │
└───────────────────────────────────────┼──────────────────┘
                                        │
                                        │ A2A Protocol
                                        ▼
                               ┌─────────────────┐
                               │ Remote Agents   │
                               │ (metrics, logs) │
                               └─────────────────┘
```

## Inbound A2A

The A2A protocol endpoints are integrated into the main HTTP server on port 8081, allowing other ADK agents to call this agent.

### Configuration

```yaml
# config.yaml
a2a:
  enabled: true
  # Public URL for agent discovery (used in agent card)
  agent_url: http://knowledge-agent:8081
```

### Exposed Endpoints

| Endpoint | Auth | Description |
|----------|------|-------------|
| `GET /.well-known/agent-card.json` | Public | Agent card for discovery |
| `POST /a2a/invoke` | Required | A2A protocol invocation |

### Authentication

The `/a2a/invoke` endpoint uses the same `api_keys` authentication as other API endpoints:

```yaml
# config.yaml
api_keys:
  root-agent: ${A2A_ROOT_AGENT_TOKEN}
  metrics-agent: ${A2A_METRICS_TOKEN}
```

**How it works:**
- If `api_keys` is configured → All A2A requests require `X-API-Key` header
- If `api_keys` is empty → Open mode (no authentication)
- The agent card (`/.well-known/agent-card.json`) is always public for discovery

**Request example:**
```bash
curl -X POST http://localhost:8081/a2a/invoke \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{"method": "message/send", "params": {...}}'
```

### Agent Card Discovery

Other agents can discover this agent's capabilities:

```bash
curl http://localhost:8081/.well-known/agent-card.json
```

## Outbound A2A (Sub-agents)

Knowledge Agent can delegate tasks to other ADK agents using the sub-agents pattern. Sub-agents are integrated using `remoteagent.NewA2A` from Google ADK.

### Configuration

```yaml
# config.yaml
a2a:
  enabled: true
  self_name: knowledge-agent  # Used for loop prevention

  # Maximum call chain depth (prevents infinite loops)
  max_call_depth: 5

  # Sub-agents: Remote ADK agents that this agent can delegate to
  sub_agents:
    - name: metrics_agent
      description: "Query Prometheus metrics and analyze performance data"
      endpoint: http://metrics-agent:9000  # Agent card source URL
      timeout: 30

    - name: logs_agent
      description: "Search and analyze application logs from Loki"
      endpoint: http://logs-agent:9000
      timeout: 30

    - name: alerts_agent
      description: "Get current alerts and manage on-call schedules"
      endpoint: http://alerts-agent:9000
      timeout: 30
```

### How Sub-agents Work

1. At startup, Knowledge Agent creates remote agent wrappers using `remoteagent.NewA2A`
2. These are added as sub-agents to the LLM agent
3. The LLM (Claude) automatically decides when to delegate to each sub-agent
4. Delegation uses the standard A2A protocol

**Key characteristics:**
- **Lazy initialization**: Agent card is fetched when first used, not at startup
- **Graceful degradation**: If a sub-agent fails to initialize, others continue working
- **Automatic delegation**: LLM decides based on sub-agent descriptions

### Example Use Case

User asks: "What errors happened in the payment service in the last hour?"

1. LLM receives the question
2. LLM decides to delegate to `logs_agent` based on description
3. Knowledge Agent calls logs-agent via A2A protocol
4. logs-agent searches logs and returns results
5. LLM synthesizes the response for the user

```
User: What errors happened in the payment service in the last hour?

Agent: [Delegates to logs_agent]

logs_agent: Found 15 errors in the payment service:
- 10x "Payment gateway timeout" (10:00-10:30)
- 3x "Invalid card number" (10:15-10:45)
- 2x "Insufficient funds" (10:30-11:00)

Agent: In the last hour, there were 15 errors in the payment service:
- *10 payment gateway timeouts* between 10:00-10:30 (likely network issue)
- *3 invalid card number errors* (user input validation)
- *2 insufficient funds errors* (expected business errors)

The gateway timeouts cluster suggests a potential infrastructure issue around 10:15.
```

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

## Graceful Degradation

Sub-agent creation follows a graceful degradation pattern:

- If a sub-agent fails to initialize (e.g., endpoint unreachable), it's skipped
- Other sub-agents continue to work
- Warnings are logged but the agent starts successfully

```log
# Example log output
WARN  Failed to create remote agent, skipping  agent=oncall-agent error="connection refused"
INFO  A2A sub-agents created successfully  count=2
```

## Async Sub-Agent Invocation

For long-running sub-agent tasks (5-15 minutes), Knowledge Agent supports **async invocation**. This allows the LLM to launch tasks in the background without blocking the conversation.

### When to Use Async Invocation

- **Coding agents** that take several minutes to complete
- **Analysis agents** that process large amounts of data
- **Any sub-agent task** that would timeout with synchronous invocation (>30s)

### Configuration

```yaml
# config.yaml
a2a:
  enabled: true
  sub_agents:
    - name: coding_agent
      description: "Write and modify code based on requirements"
      endpoint: http://coding-agent:9000
      timeout: 30

  # Enable async sub-agent invocation
  async:
    enabled: true
    timeout: 15m              # Max wait time for sub-agent response
    callback_enabled: true    # Re-invoke agent with result for processing
    post_to_slack: true       # Post results directly to Slack thread
```

### Configuration Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable async sub-agent tool |
| `timeout` | duration | `15m` | Maximum time to wait for sub-agent response |
| `callback_enabled` | bool | `true` | Re-invoke agent with result when complete |
| `post_to_slack` | bool | `true` | Post results directly to Slack thread |

### How It Works

1. **User asks for long-running task**: "Hey @bot, ask coding-agent to implement feature X"
2. **LLM calls `async_invoke_agent`**: Provides agent name, task, and Slack context
3. **Task launches in background**: LLM responds immediately "Task sent to coding_agent"
4. **Sub-agent processes task**: Runs for 5-15 minutes
5. **Result posted to Slack**: When complete, result appears in the thread
6. **Optional callback**: Agent is re-invoked to process result (e.g., save to memory)

### The `async_invoke_agent` Tool

When async is enabled, the LLM gets access to a new tool:

```json
{
  "name": "async_invoke_agent",
  "description": "Invoke a sub-agent asynchronously for long-running tasks",
  "parameters": {
    "agent_name": "Name of the sub-agent to invoke",
    "task": "The task description for the sub-agent",
    "channel_id": "Slack channel ID for posting results",
    "thread_ts": "Slack thread timestamp for posting results",
    "session_id": "Optional session ID for callback processing"
  }
}
```

### Example Flow

```
User: @bot ask coding-agent to implement a new /health endpoint

Agent: [Calls async_invoke_agent]
       - agent_name: "coding_agent"
       - task: "Implement a new /health endpoint..."
       - channel_id: "C123ABC"
       - thread_ts: "1234567890.123456"

Agent Response: "I've sent the task to coding_agent. The result will be posted
                here when complete (this may take several minutes)."

[5 minutes later, in the same Slack thread]

Bot: *Task completed by coding_agent*

    I've implemented the /health endpoint with the following changes:
    - Added handler in internal/server/health.go
    - Registered route in cmd/main.go
    - Added tests in internal/server/health_test.go
    ...

[If callback_enabled: Agent is re-invoked to process this result]
```

### Retry Configuration

Async invocations support retry for transient failures:

```yaml
a2a:
  retry:
    enabled: true
    max_retries: 3
    initial_delay: 500ms
    max_delay: 30s
    backoff_multiplier: 2.0
```

See [CONFIGURATION.md](CONFIGURATION.md#retry-configuration) for details.

### Concurrency Limits

- **Maximum concurrent tasks**: 100 per agent instance
- Tasks beyond this limit return an error
- Tasks are tracked and cleaned up on completion or timeout

### Graceful Shutdown

On agent shutdown:
1. New async tasks are rejected
2. Running tasks are cancelled via context
3. Agent waits up to 30 seconds for tasks to complete

---

## Troubleshooting

### Sub-agent not receiving requests

1. Check that `a2a.enabled: true` in config
2. Verify `endpoint` is correct and reachable
3. Check that the remote agent exposes `/.well-known/agent-card.json`

### 508 Loop Detected errors

This is expected behavior when:
- The same agent is called twice in a request chain
- The call depth exceeds `max_call_depth`

If unexpected, check:
1. Your agent topology for circular dependencies
2. That `self_name` is unique across all agents

### Timeouts

1. Increase `timeout` in sub-agent config
2. Check network connectivity to the remote agent
3. Verify the remote agent is responding in time

> **Note**: The `timeout` field in sub-agent configuration is reserved for future use. Currently, `remoteagent.NewA2A` manages timeouts internally.

### LLM not using sub-agents

1. Check logs for "A2A sub-agents created successfully" message
2. Verify sub-agent `description` clearly describes its capabilities
3. Restart the agent to reload configuration

## Related Documentation

- [SECURITY_GUIDE.md](SECURITY_GUIDE.md) - Authentication, permissions, and SSRF protection
- [CONFIGURATION.md](CONFIGURATION.md) - Full configuration reference
- [API_REFERENCE.md](API_REFERENCE.md) - REST API documentation

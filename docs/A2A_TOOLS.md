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
  # Description is auto-discovered from the agent-card
  sub_agents:
    - name: metrics_agent
      endpoint: http://metrics-agent:9000
      timeout: 180

    - name: logs_agent
      endpoint: http://logs-agent:9000
      timeout: 180

    - name: alerts_agent
      endpoint: http://alerts-agent:9000
      timeout: 180
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

## Context Cleaner

When delegating tasks to sub-agents, Knowledge Agent automatically extracts the relevant query from the conversation context. This reduces token consumption and improves sub-agent focus.

### Configuration

```yaml
# config.yaml
a2a:
  enabled: true

  # Query extractor: extracts relevant queries before sending to sub-agents
  query_extractor:
    enabled: true                         # Enable query extraction (default: true)
    model: claude-haiku-4-5-20251001      # Model for extraction (default: Haiku)
    # Note: 10 second timeout for extraction calls
```

### How It Works

1. Before sending a request to a sub-agent, the interceptor extracts text from the A2A payload
2. **Auto-Discovery**: The sub-agent's `agent-card` description is fetched automatically during initialization
3. If the agent-card has a `description`, Haiku uses it to extract only the relevant query for that specific agent
4. If no description is available, Haiku performs generic summarization (1-3 sentences)
5. If extraction fails or the result is longer, the original payload is used (graceful degradation)

### Auto-Discovery with Agent Card

When the sub-agent's agent-card includes a description, the context cleaner uses it for targeted query extraction:

**With agent description (targeted extraction):**
```
Agent Purpose (from agent-card):
"Search and analyze logs from Loki. Find errors, patterns, and anomalies."

From this context, extract ONLY the specific request relevant for this agent.
Focus on what this agent can actually do based on its purpose.
```

**Without agent description (generic summarization):**
```
Create a clear, concise summary that identifies the main task.
Include only essential context needed to complete the task.
```

### Benefits

- **Targeted extraction**: Sub-agents receive queries specific to their capabilities
- **Reduced token consumption**: No full conversation history, just the relevant request
- **Improved sub-agent performance**: Clear, focused queries are easier to process
- **Cost optimization**: Haiku extraction is much cheaper than sending full context
- **Zero configuration**: Agent descriptions are auto-discovered from agent-cards

### Example

**Original context (1200 tokens):**
```
User in channel C123 said: "Hey team, we've been having issues with the payment
service. The error rate increased yesterday around 3pm. Sarah mentioned it might
be related to the new deployment. Can someone check the metrics? Also, John said
he saw some timeout errors in the logs. We need to figure out what's happening."
```

**After extraction for logs_agent (agent-card: "Search and analyze logs from Loki"):**
```
Search for timeout errors in payment service logs from yesterday around 3pm.
```

**After extraction for metrics_agent (agent-card: "Query Prometheus metrics"):**
```
Check error rate metrics for payment service, focusing on yesterday around 3pm.
```

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

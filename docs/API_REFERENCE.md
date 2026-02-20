# API Reference

Complete REST API documentation for Knowledge Agent.

## Table of Contents

- [Overview](#overview)
- [Authentication](#authentication)
- [Endpoints](#endpoints)
  - [Health Check](#get-health)
  - [Metrics](#get-metrics)
  - [Agent Run (Blocking)](#post-agentrun)
  - [Agent Run SSE (Streaming)](#post-agentrun_sse)
  - [A2A Protocol](#a2a-protocol-endpoints)
- [Error Handling](#error-handling)
- [Rate Limiting](#rate-limiting)
- [Examples](#examples)

---

## Overview

Knowledge Agent uses the **ADK (Agent Development Kit) standard REST protocol** for agent execution. Any ADK-compatible client can connect without custom adapters.

### Port 8081 - Agent Server

- **Public endpoints** (no authentication): `/health`, `/ready`, `/live`, `/metrics`, `/.well-known/agent-card.json`
- **Protected endpoints** (authentication required): `/agent/run`, `/agent/run_sse`, `/a2a/invoke`

**Base URL**: `http://localhost:8081` (default)

**Content-Type**: `application/json`

---

## Authentication

Protected endpoints require authentication via one of these methods:

### 1. Internal Token (Slack Bridge -> Agent)

For trusted internal services.

**Header**: `X-Internal-Token: <token>`

**Configuration**:
```bash
INTERNAL_AUTH_TOKEN=your-secure-random-token
```

**Caller ID**: `slack-bridge`

### 2. JWT Bearer Token (Keycloak / Identity Provider)

For requests authenticated via an upstream API Gateway or identity provider.

**Header**: `Authorization: Bearer <jwt-token>`

The JWT is parsed (not cryptographically validated -- assumes upstream validation) to extract:
- **Email**: Used as caller ID and for permission checks (`allowed_emails`)
- **Groups**: Used for group-based permission checks (`allowed_groups`)

**Caller ID**: `preferred_username` from JWT, or email if not available

**Configuration**:
```yaml
permissions:
  groups_claim_path: "groups"  # Path in JWT to extract groups
  # For Keycloak realm roles: "realm_access.roles"
```

### 3. API Key (External A2A)

For external agents or services.

**Header**: `X-API-Key: <api-key>`

**Configuration**:
```bash
API_KEYS='{"ka_rootagent":{"caller_id":"root-agent","role":"write"}}'
```

**Caller ID**: Mapped `caller_id` from `API_KEYS` (e.g., `root-agent`)

### 4. Slack Signature (Legacy)

Direct webhooks from Slack (legacy mode).

**Headers**:
- `X-Slack-Signature: <signature>`
- `X-Slack-Request-Timestamp: <timestamp>`

**Caller ID**: `slack-direct`

### 5. Open Mode (Development)

If neither `INTERNAL_AUTH_TOKEN` nor `API_KEYS` is configured, authentication is disabled.

**Caller ID**: `unauthenticated`

**Warning**: Not recommended for production.

---

## Endpoints

### GET /health

Health check endpoint for load balancers and monitoring.

**Authentication**: None (public)

**Response**: `200 OK`

```json
{
  "status": "ok",
  "service": "knowledge-agent"
}
```

**Example**:
```bash
curl http://localhost:8081/health
```

---

### GET /metrics

Prometheus metrics endpoint in text exposition format.

**Authentication**: None (public)

**Response**: `200 OK` (text/plain)

**Format**: Prometheus text format

**Example**:
```bash
curl http://localhost:8081/metrics
```

**See**: [PROMETHEUS_METRICS.md](PROMETHEUS_METRICS.md) for complete metrics documentation

---

### POST /agent/run

Execute the agent with a blocking JSON response. Uses the **ADK standard RunAgentRequest** format.

**Authentication**: Required

**Rate Limit**: 10 requests/second per IP, burst of 20

#### Request Headers

| Header | Required | Description |
|--------|----------|-------------|
| `Content-Type` | Yes | Must be `application/json` |
| `X-Internal-Token` | Conditional | Internal authentication token |
| `Authorization` | Conditional | JWT Bearer token (`Bearer <token>`) |
| `X-API-Key` | Conditional | API key for A2A access |

#### Request Body

```json
{
  "appName": "knowledge-agent",
  "userId": "user@example.com",
  "sessionId": "session-abc123",
  "newMessage": {
    "role": "user",
    "parts": [
      {"text": "What is our deployment process?"}
    ]
  }
}
```

#### Request Schema

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `appName` | string | No | Application name (default: `knowledge-agent`) |
| `userId` | string | No | User identifier for session and memory scoping. Auto-resolved from auth context if not provided |
| `sessionId` | string | No | Session identifier for conversation continuity. Auto-generated if not provided |
| `newMessage` | object | **Yes** | The user message in ADK Content format |
| `newMessage.role` | string | **Yes** | Must be `"user"` |
| `newMessage.parts` | array | **Yes** | Array of content parts |
| `streaming` | boolean | No | Set to `true` for SSE streaming (use `/agent/run_sse` instead) |

#### Content Parts

Text part:
```json
{"text": "Your question here"}
```

Image part (inline data):
```json
{
  "inlineData": {
    "mimeType": "image/png",
    "data": "base64-encoded-image-data"
  }
}
```

#### Response

The response follows the ADK standard format. The agent's text response is contained within the `content` field of the response.

**Success** (`200 OK`): ADK RunAgentResponse with the agent's answer.

**Error** (`401 Unauthorized`):
```json
{
  "error": "Authentication required"
}
```

**Error** (`429 Too Many Requests`):
```json
{
  "error": "Rate limit exceeded. Please try again later."
}
```

**Error** (`500 Internal Server Error`):
```json
{
  "error": "Internal server error"
}
```

#### Examples

**Minimal query**:
```bash
curl -X POST http://localhost:8081/agent/run \
  -H "Content-Type: application/json" \
  -H "X-API-Key: ka_rootagent" \
  -d '{
    "appName": "knowledge-agent",
    "userId": "test-user",
    "newMessage": {
      "role": "user",
      "parts": [{"text": "What is our deployment process?"}]
    }
  }'
```

**Query with session continuity**:
```bash
curl -X POST http://localhost:8081/agent/run \
  -H "Content-Type: application/json" \
  -H "X-API-Key: ka_rootagent" \
  -d '{
    "appName": "knowledge-agent",
    "userId": "test-user",
    "sessionId": "my-session-123",
    "newMessage": {
      "role": "user",
      "parts": [{"text": "Tell me more about the rollback procedure"}]
    }
  }'
```

**Query with JWT authentication**:
```bash
curl -X POST http://localhost:8081/agent/run \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer eyJhbGciOiJSUzI1NiIs..." \
  -d '{
    "appName": "knowledge-agent",
    "newMessage": {
      "role": "user",
      "parts": [{"text": "How do we deploy to staging?"}]
    }
  }'
```

---

### POST /agent/run_sse

Streaming version of `/agent/run` using **Server-Sent Events (SSE)**. Returns the agent's response in real-time using the ADK standard SSE format.

**Authentication**: Required (same as `/agent/run`)

**Rate Limit**: 10 requests/second per IP, burst of 20

#### Request Body

Same schema as [`POST /agent/run`](#post-agentrun). Set `streaming: true` in the body.

```json
{
  "appName": "knowledge-agent",
  "userId": "test-user",
  "newMessage": {
    "role": "user",
    "parts": [{"text": "What is our deployment process?"}]
  },
  "streaming": true
}
```

#### Response

**Content-Type**: `text/event-stream`

The SSE stream follows the ADK standard format. Events are sent as `data:` lines with JSON payloads containing the agent's incremental response.

#### Examples

**Streaming query**:
```bash
curl -N -X POST http://localhost:8081/agent/run_sse \
  -H "Content-Type: application/json" \
  -H "X-API-Key: ka_rootagent" \
  -d '{
    "appName": "knowledge-agent",
    "userId": "test-user",
    "newMessage": {
      "role": "user",
      "parts": [{"text": "What is our deployment process?"}]
    },
    "streaming": true
  }'
```

**Error responses** (before SSE stream starts):

If the request fails validation before streaming begins, standard HTTP error responses are returned:

| Code | Condition |
|------|-----------|
| `401` | Authentication failed |
| `429` | Rate limit exceeded |
| `500` | Internal server error (session, marshal, etc.) |

---

## A2A Protocol Endpoints

Standard A2A protocol endpoints (all on port 8081).

### GET /.well-known/agent-card.json

Agent card for A2A discovery.

**Authentication**: None (public for discovery)

**Response**: `200 OK`

```json
{
  "name": "knowledge-agent",
  "description": "Knowledge management assistant",
  "url": "http://localhost:8081/a2a/invoke",
  "capabilities": {
    "streaming": true
  }
}
```

**Example**:
```bash
curl http://localhost:8081/.well-known/agent-card.json
```

---

### POST /a2a/invoke

A2A protocol invocation endpoint.

**Authentication**: Required (if `api_keys` configured)

**Request Headers**:

| Header | Required | Description |
|--------|----------|-------------|
| `Content-Type` | Yes | Must be `application/json` |
| `X-API-Key` | Conditional | API key for A2A access |

**Request Body**:

```json
{
  "method": "message/send",
  "params": {
    "message": {
      "role": "user",
      "parts": [
        {"text": "What is our deployment process?"}
      ]
    }
  }
}
```

**Response**: A2A protocol response (streaming or non-streaming)

**Example**:
```bash
curl -X POST http://localhost:8081/a2a/invoke \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{
    "method": "message/send",
    "params": {
      "message": {
        "role": "user",
        "parts": [{"text": "What is our deployment process?"}]
      }
    }
  }'
```

---

## Error Handling

All endpoints return JSON error responses with appropriate HTTP status codes.

### HTTP Status Codes

| Code | Meaning | When Used |
|------|---------|-----------|
| `200` | OK | Request succeeded |
| `400` | Bad Request | Invalid request body or missing required fields |
| `401` | Unauthorized | Authentication failed or missing |
| `429` | Too Many Requests | Rate limit exceeded |
| `500` | Internal Server Error | Unexpected server error |

---

## Rate Limiting

Protected endpoints (`/agent/run`, `/agent/run_sse`, `/a2a/invoke`) are rate-limited per IP address:

- **Rate**: 10 requests per second
- **Burst**: 20 requests (token bucket)

**Algorithm**: Token bucket with automatic refill

**Response**: `429 Too Many Requests` when limit exceeded

**Headers**: No `X-RateLimit-*` headers currently exposed

### X-Forwarded-For Support

Rate limiting respects `X-Forwarded-For` header when behind a proxy:

- Takes **rightmost IP** (RFC 7239 compliant)
- Ignores untrusted proxy IPs

---

## Examples

### Complete Integration Example (bash)

```bash
#!/bin/bash

# Configuration
AGENT_URL="http://localhost:8081"
API_KEY="ka_rootagent"

# 1. Health check
echo "Checking health..."
curl -s "$AGENT_URL/health" | jq .

# 2. Blocking query
echo -e "\nQuerying knowledge base..."
curl -s -X POST "$AGENT_URL/agent/run" \
  -H "Content-Type: application/json" \
  -H "X-API-Key: $API_KEY" \
  -d '{
    "appName": "knowledge-agent",
    "userId": "test-user",
    "newMessage": {
      "role": "user",
      "parts": [{"text": "What is our deployment process?"}]
    }
  }' | jq .

# 3. Streaming query (SSE)
echo -e "\nStreaming query..."
curl -N -s -X POST "$AGENT_URL/agent/run_sse" \
  -H "Content-Type: application/json" \
  -H "X-API-Key: $API_KEY" \
  -d '{
    "appName": "knowledge-agent",
    "userId": "test-user",
    "newMessage": {
      "role": "user",
      "parts": [{"text": "What is our deployment process?"}]
    },
    "streaming": true
  }'

echo -e "\nDone!"
```

### Python Integration Example

```python
import requests
import json as json_mod


class KnowledgeAgentClient:
    def __init__(self, base_url: str, api_key: str):
        self.base_url = base_url.rstrip('/')
        self.api_key = api_key
        self.headers = {
            'Content-Type': 'application/json',
            'X-API-Key': api_key
        }

    def health(self) -> dict:
        """Check agent health."""
        resp = requests.get(f'{self.base_url}/health')
        resp.raise_for_status()
        return resp.json()

    def run(self, question: str, user_id: str = "api-user",
            session_id: str | None = None) -> dict:
        """Execute agent with blocking response (ADK /agent/run)."""
        payload = {
            'appName': 'knowledge-agent',
            'userId': user_id,
            'newMessage': {
                'role': 'user',
                'parts': [{'text': question}]
            }
        }
        if session_id:
            payload['sessionId'] = session_id

        resp = requests.post(
            f'{self.base_url}/agent/run',
            headers=self.headers,
            json=payload
        )
        resp.raise_for_status()
        return resp.json()

    def run_stream(self, question: str, user_id: str = "api-user",
                   session_id: str | None = None):
        """Execute agent with SSE streaming (ADK /agent/run_sse)."""
        payload = {
            'appName': 'knowledge-agent',
            'userId': user_id,
            'newMessage': {
                'role': 'user',
                'parts': [{'text': question}]
            },
            'streaming': True
        }
        if session_id:
            payload['sessionId'] = session_id

        with requests.post(
            f'{self.base_url}/agent/run_sse',
            headers=self.headers,
            json=payload,
            stream=True
        ) as resp:
            resp.raise_for_status()
            for line in resp.iter_lines(decode_unicode=True):
                if line and line.startswith('data: '):
                    yield json_mod.loads(line[6:])


# Usage
if __name__ == '__main__':
    client = KnowledgeAgentClient(
        base_url='http://localhost:8081',
        api_key='ka_rootagent'
    )

    # Health check
    print("Health:", client.health())

    # Blocking query
    result = client.run("What is our deployment process?")
    print("Result:", result)

    # Streaming query
    print("Streaming:")
    for event in client.run_stream("What is our deployment process?"):
        print(event)
```

---

## See Also

- [CONFIGURATION.md](CONFIGURATION.md) - Full configuration guide
- [SECURITY_GUIDE.md](SECURITY_GUIDE.md) - Authentication and permissions
- [OPERATIONS.md](OPERATIONS.md) - Logging and observability
- [CLAUDE.md](../CLAUDE.md) - Development guide

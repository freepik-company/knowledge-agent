# API Reference

Complete REST API documentation for Knowledge Agent.

## Table of Contents

- [Overview](#overview)
- [Authentication](#authentication)
- [Endpoints](#endpoints)
  - [Health Check](#get-health)
  - [Metrics](#get-metrics)
  - [Query](#post-apiquery)
  - [Ingest Thread](#post-apiingest-thread)
- [Error Handling](#error-handling)
- [Rate Limiting](#rate-limiting)
- [Examples](#examples)

---

## Overview

The Knowledge Agent exposes a REST API with four endpoints:

- **Public endpoints** (no authentication): `/health`, `/metrics`
- **Protected endpoints** (authentication required): `/api/query`, `/api/ingest-thread`

**Base URL**: `http://localhost:8081` (default)

**Content-Type**: `application/json`

---

## Authentication

Protected endpoints require authentication via one of these methods:

### 1. Internal Token (Slack Bridge → Agent)

For trusted internal services.

**Header**: `X-Internal-Token: <token>`

**Configuration**:
```bash
INTERNAL_AUTH_TOKEN=your-secure-random-token
```

**Caller ID**: `slack-bridge`

### 2. API Key (External A2A)

For external agents or services.

**Header**: `X-API-Key: <api-key>`

**Configuration**:
```bash
A2A_API_KEYS='{"ka_rootagent":"root-agent","ka_analytics":"analytics-agent"}'
```

**Caller ID**: Mapped value from `A2A_API_KEYS` (e.g., `root-agent`)

### 3. Slack Signature (Legacy)

Direct webhooks from Slack (legacy mode).

**Headers**:
- `X-Slack-Signature: <signature>`
- `X-Slack-Request-Timestamp: <timestamp>`

**Caller ID**: `slack-direct`

### 4. Open Mode (Development)

If neither `INTERNAL_AUTH_TOKEN` nor `A2A_API_KEYS` is configured, authentication is disabled.

**Caller ID**: `unauthenticated`

⚠️ **Not recommended for production**

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

**Example response**:
```
# HELP knowledge_agent_queries_total Total number of queries processed
# TYPE knowledge_agent_queries_total counter
knowledge_agent_queries_total 1234

# HELP knowledge_agent_query_errors_total Total number of query errors
# TYPE knowledge_agent_query_errors_total counter
knowledge_agent_query_errors_total 34

# HELP knowledge_agent_query_latency_seconds Query latency in seconds
# TYPE knowledge_agent_query_latency_seconds histogram
knowledge_agent_query_latency_seconds_bucket{le="0.005"} 120
knowledge_agent_query_latency_seconds_bucket{le="0.01"} 450
...

# HELP knowledge_agent_memory_saves_total Total number of memory save operations
# TYPE knowledge_agent_memory_saves_total counter
knowledge_agent_memory_saves_total 567

# HELP knowledge_agent_tokens_used_total Total number of LLM tokens used
# TYPE knowledge_agent_tokens_used_total counter
knowledge_agent_tokens_used_total 1500000
```

**Example**:
```bash
curl http://localhost:8081/metrics
```

**See**: [PROMETHEUS_METRICS.md](PROMETHEUS_METRICS.md) for complete metrics documentation

---

### POST /api/query

Query the knowledge base with natural language questions.

**Authentication**: Required

**Rate Limit**: 10 requests/second per IP, burst of 20

#### Request Headers

| Header | Required | Description |
|--------|----------|-------------|
| `Content-Type` | Yes | Must be `application/json` |
| `X-Internal-Token` | Conditional | Internal authentication token |
| `X-API-Key` | Conditional | API key for A2A access |
| `X-Slack-User-Id` | Optional | Slack user ID for permissions |

#### Request Body

```json
{
  "question": "What is our deployment process?",
  "channel_id": "C01ABC123",
  "thread_ts": "1234567890.123456",
  "messages": [
    {
      "user": "U01USER123",
      "text": "How do we deploy?",
      "ts": "1234567890.123456",
      "type": "message"
    }
  ],
  "user_name": "john",
  "user_real_name": "John Doe",
  "slack_user_id": "U01USER123",
  "images": [
    {
      "name": "diagram.png",
      "mime_type": "image/png",
      "data": "base64-encoded-image-data"
    }
  ]
}
```

#### Request Schema

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `question` | string | **Yes** | User's question |
| `channel_id` | string | No | Slack channel ID (for context) |
| `thread_ts` | string | No | Thread timestamp (for threading) |
| `messages` | array | No | Thread context messages |
| `user_name` | string | No | Slack username (e.g., "john") |
| `user_real_name` | string | No | Real name (e.g., "John Doe") |
| `slack_user_id` | string | No | Slack user ID (for permissions) |
| `images` | array | No | Base64-encoded images for multimodal analysis |

**Messages array schema**:
```json
{
  "user": "string",        // Slack user ID
  "text": "string",        // Message text
  "ts": "string",          // Timestamp
  "type": "string",        // Message type (usually "message")
  "images": [...]          // Optional: attached images
}
```

**Images array schema**:
```json
{
  "name": "string",        // Filename
  "mime_type": "string",   // MIME type (e.g., "image/png")
  "data": "string"         // Base64-encoded image data
}
```

#### Response Body

**Success** (`200 OK`):
```json
{
  "success": true,
  "answer": "Our deployment process involves...",
  "memories_used": 3,
  "tool_calls": [
    {
      "tool": "search_memory",
      "args": {
        "query": "deployment process"
      }
    }
  ]
}
```

**Error** (`400 Bad Request`):
```json
{
  "error": "question is required"
}
```

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

#### Response Schema

| Field | Type | Description |
|-------|------|-------------|
| `success` | boolean | Whether query succeeded |
| `answer` | string | Agent's response (if success) |
| `message` | string | Error message (if not success) |
| `memories_used` | integer | Number of memories searched (optional) |
| `tool_calls` | array | Tools invoked by agent (optional) |

#### Examples

**Minimal query**:
```bash
curl -X POST http://localhost:8081/api/query \
  -H "Content-Type: application/json" \
  -H "X-API-Key: ka_rootagent" \
  -d '{
    "question": "What is our deployment process?"
  }'
```

**Query with thread context**:
```bash
curl -X POST http://localhost:8081/api/query \
  -H "Content-Type: application/json" \
  -H "X-Internal-Token: your-token" \
  -H "X-Slack-User-Id: U01USER123" \
  -d '{
    "question": "How do we deploy to staging?",
    "channel_id": "C01ABC123",
    "thread_ts": "1234567890.123456",
    "user_name": "john",
    "user_real_name": "John Doe",
    "messages": [
      {
        "user": "U01USER123",
        "text": "I need help with deployment",
        "ts": "1234567890.123456",
        "type": "message"
      }
    ]
  }'
```

**Query with image**:
```bash
# First, encode image to base64
IMAGE_DATA=$(base64 -i screenshot.png)

curl -X POST http://localhost:8081/api/query \
  -H "Content-Type: application/json" \
  -H "X-API-Key: ka_rootagent" \
  -d "{
    \"question\": \"What error is shown in this screenshot?\",
    \"images\": [
      {
        \"name\": \"screenshot.png\",
        \"mime_type\": \"image/png\",
        \"data\": \"$IMAGE_DATA\"
      }
    ]
  }"
```

---

### POST /api/ingest-thread

Ingest a Slack thread into the knowledge base.

**Authentication**: Required

**Rate Limit**: 10 requests/second per IP, burst of 20

#### Request Headers

| Header | Required | Description |
|--------|----------|-------------|
| `Content-Type` | Yes | Must be `application/json` |
| `X-Internal-Token` | Conditional | Internal authentication token |
| `X-API-Key` | Conditional | API key for A2A access |
| `X-Slack-User-Id` | Optional | Slack user ID for permissions |

#### Request Body

```json
{
  "thread_ts": "1234567890.123456",
  "channel_id": "C01ABC123",
  "messages": [
    {
      "user": "U01USER123",
      "text": "We deploy using GitHub Actions",
      "ts": "1234567890.123456",
      "type": "message"
    },
    {
      "user": "U02USER456",
      "text": "The workflow is in .github/workflows/deploy.yml",
      "ts": "1234567891.234567",
      "type": "message"
    }
  ]
}
```

#### Request Schema

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `thread_ts` | string | **Yes** | Thread timestamp (unique identifier) |
| `channel_id` | string | **Yes** | Slack channel ID |
| `messages` | array | **Yes** | Thread messages (non-empty) |

**Messages array schema**: Same as `/api/query`

#### Response Body

**Success** (`200 OK`):
```json
{
  "success": true,
  "message": "Thread ingested successfully",
  "memories_saved": 3
}
```

**Error** (`400 Bad Request`):
```json
{
  "error": "thread_ts and channel_id are required"
}
```

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

#### Response Schema

| Field | Type | Description |
|-------|------|-------------|
| `success` | boolean | Whether ingestion succeeded |
| `message` | string | Success/error message |
| `memories_saved` | integer | Number of memories saved (optional) |

#### Example

```bash
curl -X POST http://localhost:8081/api/ingest-thread \
  -H "Content-Type: application/json" \
  -H "X-Internal-Token: your-token" \
  -d '{
    "thread_ts": "1234567890.123456",
    "channel_id": "C01ABC123",
    "messages": [
      {
        "user": "U01USER123",
        "text": "Our API uses JWT authentication",
        "ts": "1234567890.123456",
        "type": "message"
      },
      {
        "user": "U02USER456",
        "text": "Token expiry is 24 hours",
        "ts": "1234567891.234567",
        "type": "message"
      }
    ]
  }'
```

---

## Error Handling

All endpoints return JSON error responses with appropriate HTTP status codes.

### Error Response Format

```json
{
  "error": "Error message describing what went wrong"
}
```

### HTTP Status Codes

| Code | Meaning | When Used |
|------|---------|-----------|
| `200` | OK | Request succeeded |
| `400` | Bad Request | Invalid request body or missing required fields |
| `401` | Unauthorized | Authentication failed or missing |
| `405` | Method Not Allowed | Wrong HTTP method (e.g., GET on POST endpoint) |
| `429` | Too Many Requests | Rate limit exceeded |
| `500` | Internal Server Error | Unexpected server error |

### Common Errors

**Missing required field**:
```json
{
  "error": "question is required"
}
```

**Authentication failure**:
```json
{
  "error": "Authentication required"
}
```

**Invalid JSON**:
```json
{
  "error": "Invalid request: invalid character 'x' looking for beginning of value"
}
```

**Rate limit exceeded**:
```json
{
  "error": "Rate limit exceeded. Please try again later."
}
```

---

## Rate Limiting

Protected endpoints (`/api/query`, `/api/ingest-thread`) are rate-limited per IP address:

- **Rate**: 10 requests per second
- **Burst**: 20 requests (token bucket)

**Algorithm**: Token bucket with automatic refill

**Response**: `429 Too Many Requests` when limit exceeded

**Headers**: No `X-RateLimit-*` headers currently exposed

### X-Forwarded-For Support

Rate limiting respects `X-Forwarded-For` header when behind a proxy:

- Takes **rightmost IP** (RFC 7239 compliant)
- Ignores untrusted proxy IPs

**Example**:
```
X-Forwarded-For: client-ip, proxy1-ip, proxy2-ip
```
→ Rate limited by `proxy2-ip` (most trusted)

---

## Examples

### Complete Integration Example

```bash
#!/bin/bash

# Configuration
AGENT_URL="http://localhost:8081"
API_KEY="ka_rootagent"

# 1. Health check
echo "Checking health..."
curl -s "$AGENT_URL/health" | jq .

# 2. Get metrics
echo -e "\nFetching metrics..."
curl -s "$AGENT_URL/metrics" | jq .

# 3. Query knowledge base
echo -e "\nQuerying knowledge base..."
curl -s -X POST "$AGENT_URL/api/query" \
  -H "Content-Type: application/json" \
  -H "X-API-Key: $API_KEY" \
  -d '{
    "question": "What is our deployment process?"
  }' | jq .

# 4. Ingest thread
echo -e "\nIngesting thread..."
curl -s -X POST "$AGENT_URL/api/ingest-thread" \
  -H "Content-Type: application/json" \
  -H "X-API-Key: $API_KEY" \
  -d '{
    "thread_ts": "1234567890.123456",
    "channel_id": "C01ABC123",
    "messages": [
      {
        "user": "U01USER123",
        "text": "Our deployment uses GitHub Actions with automatic rollback",
        "ts": "1234567890.123456",
        "type": "message"
      }
    ]
  }' | jq .

echo -e "\nDone!"
```

### Python Integration Example

```python
import requests
import base64

class KnowledgeAgentClient:
    def __init__(self, base_url: str, api_key: str):
        self.base_url = base_url.rstrip('/')
        self.api_key = api_key
        self.headers = {
            'Content-Type': 'application/json',
            'X-API-Key': api_key
        }

    def health(self) -> dict:
        """Check agent health"""
        resp = requests.get(f'{self.base_url}/health')
        resp.raise_for_status()
        return resp.json()

    def metrics(self) -> dict:
        """Get agent metrics"""
        resp = requests.get(f'{self.base_url}/metrics')
        resp.raise_for_status()
        return resp.json()

    def query(self, question: str, **kwargs) -> dict:
        """Query the knowledge base"""
        payload = {'question': question, **kwargs}
        resp = requests.post(
            f'{self.base_url}/api/query',
            headers=self.headers,
            json=payload
        )
        resp.raise_for_status()
        return resp.json()

    def query_with_image(self, question: str, image_path: str) -> dict:
        """Query with an image attachment"""
        with open(image_path, 'rb') as f:
            image_data = base64.b64encode(f.read()).decode('utf-8')

        payload = {
            'question': question,
            'images': [{
                'name': image_path,
                'mime_type': 'image/png',
                'data': image_data
            }]
        }
        resp = requests.post(
            f'{self.base_url}/api/query',
            headers=self.headers,
            json=payload
        )
        resp.raise_for_status()
        return resp.json()

    def ingest_thread(self, thread_ts: str, channel_id: str, messages: list) -> dict:
        """Ingest a thread into knowledge base"""
        payload = {
            'thread_ts': thread_ts,
            'channel_id': channel_id,
            'messages': messages
        }
        resp = requests.post(
            f'{self.base_url}/api/ingest-thread',
            headers=self.headers,
            json=payload
        )
        resp.raise_for_status()
        return resp.json()

# Usage
if __name__ == '__main__':
    client = KnowledgeAgentClient(
        base_url='http://localhost:8081',
        api_key='ka_rootagent'
    )

    # Health check
    print("Health:", client.health())

    # Query
    result = client.query("What is our deployment process?")
    print("Answer:", result['answer'])

    # Query with image
    result = client.query_with_image(
        "What error is shown in this screenshot?",
        "error-screenshot.png"
    )
    print("Analysis:", result['answer'])
```

---

## See Also

- [CONFIGURATION.md](CONFIGURATION.md) - Full configuration guide
- [SECURITY.md](SECURITY.md) - Authentication and permissions
- [OPERATIONS.md](OPERATIONS.md) - Logging and observability
- [CLAUDE.md](../CLAUDE.md) - Development guide

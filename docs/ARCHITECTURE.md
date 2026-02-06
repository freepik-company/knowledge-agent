# Architecture Documentation

This document describes the internal architecture of Knowledge Agent, including detailed request flow diagrams for Slack and A2A interactions.

## Table of Contents

- [System Overview](#system-overview)
- [Component Architecture](#component-architecture)
- [Request Flow: Slack](#request-flow-slack)
- [Request Flow: A2A (Agent-to-Agent)](#request-flow-a2a-agent-to-agent)
- [Component Details](#component-details)
- [Data Flow Diagram](#data-flow-diagram)

---

## System Overview

Knowledge Agent is a unified binary that can run in three modes:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                     knowledge-agent (unified binary)                        │
│                                                                             │
│   --mode all (default)      --mode agent         --mode slack-bot           │
│   ┌─────────┬─────────┐     ┌─────────┐          ┌─────────┐               │
│   │  Agent  │  Slack  │     │  Agent  │          │  Slack  │               │
│   │  :8081  │  :8080  │     │  :8081  │          │  :8080  │               │
│   └─────────┴─────────┘     └─────────┘          └─────────┘               │
└─────────────────────────────────────────────────────────────────────────────┘
```

| Mode | Ports | Use Case |
|------|-------|----------|
| `all` | 8080 + 8081 | Production, development (default) |
| `agent` | 8081 | API-only testing, headless mode |
| `slack-bot` | 8080 | Custom bridge scenarios |

---

## Component Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              EXTERNAL SOURCES                               │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│    ┌──────────────┐              ┌──────────────┐                          │
│    │    Slack     │              │  A2A Client  │                          │
│    │   (Events)   │              │   (Agent)    │                          │
│    └──────┬───────┘              └──────┬───────┘                          │
│           │                             │                                   │
│           ▼                             │                                   │
│    ┌──────────────┐                     │                                   │
│    │ Slack Bridge │                     │                                   │
│    │   :8080      │                     │                                   │
│    │              │                     │                                   │
│    │ /slack/events│                     │                                   │
│    └──────┬───────┘                     │                                   │
│           │                             │                                   │
│           │ X-Internal-Token            │ X-API-Key                         │
│           │                             │                                   │
│           ▼                             ▼                                   │
│    ┌────────────────────────────────────────────────────────────────┐      │
│    │                      Agent Server :8081                        │      │
│    ├────────────────────────────────────────────────────────────────┤      │
│    │  PUBLIC ENDPOINTS                                              │      │
│    │  ├─ /health, /ready, /live   (health checks)                  │      │
│    │  ├─ /metrics                  (Prometheus)                     │      │
│    │  └─ /.well-known/agent-card.json (A2A discovery)              │      │
│    ├────────────────────────────────────────────────────────────────┤      │
│    │  PROTECTED ENDPOINTS (Auth + Rate Limit + Loop Prevention)     │      │
│    │  ├─ /api/query               (question answering & ingestion)  │      │
│    │  ├─ /api/query/stream        (SSE streaming responses)         │      │
│    │  └─ /a2a/invoke              (A2A JSON-RPC)                    │      │
│    └────────────────────────────────────────────────────────────────┘      │
│                                    │                                        │
│                                    ▼                                        │
│    ┌────────────────────────────────────────────────────────────────┐      │
│    │                         ADK Agent                              │      │
│    │  ┌─────────────────────────────────────────────────────────┐  │      │
│    │  │  LLM Agent (Claude Sonnet 4.5)                          │  │      │
│    │  │  ├─ System Prompt (prompts.go)                          │  │      │
│    │  │  ├─ Memory Tools (search_memory, save_to_memory)        │  │      │
│    │  │  ├─ Web Tools (fetch_url)                               │  │      │
│    │  │  ├─ MCP Toolsets (optional external tools)              │  │      │
│    │  │  └─ A2A Sub-Agents (remote agents via query_<name> tools)│  │      │
│    │  └─────────────────────────────────────────────────────────┘  │      │
│    │                           │                                    │      │
│    │  ┌─────────────────────────────────────────────────────────┐  │      │
│    │  │  Runner (Parallel Wrapper optional)                     │  │      │
│    │  │  ├─ Session Management (Redis)                          │  │      │
│    │  │  ├─ Memory Management (PostgreSQL + pgvector)           │  │      │
│    │  │  └─ Tool Execution & Orchestration                      │  │      │
│    │  └─────────────────────────────────────────────────────────┘  │      │
│    └────────────────────────────────────────────────────────────────┘      │
│                                    │                                        │
│                                    ▼                                        │
│    ┌────────────────────────────────────────────────────────────────┐      │
│    │                     INFRASTRUCTURE                             │      │
│    │                                                                │      │
│    │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐        │      │
│    │  │   Redis      │  │  PostgreSQL  │  │   Ollama     │        │      │
│    │  │  Sessions    │  │   pgvector   │  │  Embeddings  │        │      │
│    │  │              │  │   (Memory)   │  │              │        │      │
│    │  └──────────────┘  └──────────────┘  └──────────────┘        │      │
│    │                                                                │      │
│    │  ┌──────────────┐  ┌──────────────┐                          │      │
│    │  │  Anthropic   │  │   Langfuse   │                          │      │
│    │  │    API       │  │   (Tracing)  │                          │      │
│    │  │  (Claude)    │  │              │                          │      │
│    │  └──────────────┘  └──────────────┘                          │      │
│    └────────────────────────────────────────────────────────────────┘      │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## Request Flow: Slack

This diagram shows the complete flow of a Slack message from user to response.

```
┌───────────────────────────────────────────────────────────────────────────────┐
│                          SLACK REQUEST FLOW                                   │
└───────────────────────────────────────────────────────────────────────────────┘

 User                Slack              Slack Bridge         Agent Server          ADK Agent
  │                   │                     │                     │                    │
  │  @bot message     │                     │                     │                    │
  ├──────────────────►│                     │                     │                    │
  │                   │                     │                     │                    │
  │                   │  Event (app_mention │                     │                    │
  │                   │  or message)        │                     │                    │
  │                   ├────────────────────►│                     │                    │
  │                   │                     │                     │                    │
  │                   │                     │ 1. Verify signature │                    │
  │                   │                     │    (SigningSecret)  │                    │
  │                   │                     │                     │                    │
  │                   │                     │ 2. Parse event      │                    │
  │                   │                     │    (app_mention/DM) │                    │
  │                   │                     │                     │                    │
  │                   │                     │ 3. Schedule ack     │                    │
  │                   │                     │    (if >2s)         │                    │
  │                   │                     │                     │                    │
  │                   │                     │ 4. Fetch user info  │                    │
  │                   │◄────────────────────│    (users.info)     │                    │
  │                   │                     │                     │                    │
  │                   │                     │ 5. Fetch thread     │                    │
  │                   │◄────────────────────│    (conversations.  │                    │
  │                   │                     │     replies)        │                    │
  │                   │                     │                     │                    │
  │                   │                     │ 6. Download images  │                    │
  │                   │◄────────────────────│    (if present)     │                    │
  │                   │                     │                     │                    │
  │                   │                     │ 7. Resolve usernames│                    │
  │                   │◄────────────────────│    in thread        │                    │
  │                   │                     │                     │                    │
  │                   │                     │                     │                    │
  │                   │                     │ POST /api/query     │                    │
  │                   │                     │ X-Internal-Token    │                    │
  │                   │                     │ X-Slack-User-Id     │                    │
  │                   │                     ├────────────────────►│                    │
  │                   │                     │                     │                    │
  │                   │                     │                     │ 8. Auth middleware │
  │                   │                     │                     │    (verify token)  │
  │                   │                     │                     │                    │
  │                   │                     │                     │ 9. Rate limiting   │
  │                   │                     │                     │                    │
  │                   │                     │                     │ 10. Parse request  │
  │                   │                     │                     │                    │
  │                   │                     │                     │ Query(ctx, req)    │
  │                   │                     │                     ├───────────────────►│
  │                   │                     │                     │                    │
  │                   │                     │                     │         11. Create/resume session
  │                   │                     │                     │             (Redis)│
  │                   │                     │                     │                    │
  │                   │                     │                     │         12. Pre-search memory
  │                   │                     │                     │             (3s timeout, max 5)
  │                   │                     │                     │                    │
  │                   │                     │                     │         13. Build prompt with context
  │                   │                     │                     │             + pre-search results
  │                   │                     │                     │             + images (if any)
  │                   │                     │                     │                    │
  │                   │                     │                     │         14. Run LLM (Claude)
  │                   │                     │                     │                    │
  │                   │                     │                     │             ┌──────┴──────┐
  │                   │                     │                     │             │  LLM Loop   │
  │                   │                     │                     │             │             │
  │                   │                     │                     │             │ ┌─────────┐ │
  │                   │                     │                     │             │ │ Think   │ │
  │                   │                     │                     │             │ └────┬────┘ │
  │                   │                     │                     │             │      │      │
  │                   │                     │                     │             │      ▼      │
  │                   │                     │                     │             │ ┌─────────┐ │
  │                   │                     │                     │             │ │ Tool?   │─┼─► Yes: Execute tool
  │                   │                     │                     │             │ └────┬────┘ │   (search_memory,
  │                   │                     │                     │             │      │ No   │    save_to_memory,
  │                   │                     │                     │             │      ▼      │    fetch_url, etc.)
  │                   │                     │                     │             │ ┌─────────┐ │
  │                   │                     │                     │             │ │Response │ │
  │                   │                     │                     │             │ └─────────┘ │
  │                   │                     │                     │             └──────┬──────┘
  │                   │                     │                     │                    │
  │                   │                     │                     │◄───────────────────┤
  │                   │                     │                     │   QueryResponse    │
  │                   │                     │                     │                    │
  │                   │                     │◄────────────────────┤                    │
  │                   │                     │   JSON Response     │                    │
  │                   │                     │                     │                    │
  │                   │                     │ 15. Format response │                    │
  │                   │                     │     for Slack       │                    │
  │                   │                     │                     │                    │
  │                   │  PostMessage        │                     │                    │
  │                   │◄────────────────────│                     │                    │
  │                   │                     │                     │                    │
  │◄──────────────────│  Bot response       │                     │                    │
  │                   │                     │                     │                    │
```

### Slack Flow Steps Explained

| Step | Component | Description |
|------|-----------|-------------|
| 1 | Slack Bridge | Verify `X-Slack-Signature` using `SLACK_SIGNING_SECRET` |
| 2 | Slack Bridge | Parse event type: `app_mention` (channel) or `message` (DM) |
| 3 | Slack Bridge | Schedule acknowledgment message (sent only if processing >2s) |
| 4 | Slack Bridge | Fetch user info via `users.info` API for personalization |
| 5 | Slack Bridge | Fetch thread messages via `conversations.replies` |
| 6 | Slack Bridge | Download attached images, encode as base64 |
| 7 | Slack Bridge | Batch resolve user IDs to display names |
| 8 | Agent Server | Validate `X-Internal-Token` (trusted internal auth) |
| 9 | Agent Server | Apply rate limiting (10 req/s, burst 20) |
| 10 | Agent Server | Parse JSON body, validate required fields |
| 11 | ADK Agent | Create or resume session in Redis |
| 12 | ADK Agent | **Pre-search memory**: Execute `search_memory` programmatically (3s timeout, max 5 results) |
| 13 | ADK Agent | Build prompt with thread context + pre-search results + images |
| 14 | ADK Agent | Execute LLM with tool loop |
| 15 | Slack Bridge | Convert markdown to Slack format, post response |

---

## Request Flow: A2A (Agent-to-Agent)

This diagram shows the flow when another agent invokes Knowledge Agent via A2A protocol.

```
┌───────────────────────────────────────────────────────────────────────────────┐
│                          A2A REQUEST FLOW                                     │
└───────────────────────────────────────────────────────────────────────────────┘

 External Agent        Agent Server                ADK Agent            A2A Libraries
       │                    │                          │                     │
       │                    │                          │                     │
       │ GET /.well-known/  │                          │                     │
       │ agent-card.json    │                          │                     │
       ├───────────────────►│                          │                     │
       │                    │                          │                     │
       │◄───────────────────┤ AgentCard (public)       │                     │
       │   {name, skills,   │                          │                     │
       │    capabilities,   │                          │                     │
       │    url: /a2a/invoke}                          │                     │
       │                    │                          │                     │
       │                    │                          │                     │
       │ POST /a2a/invoke   │                          │                     │
       │ X-API-Key: ka_xxx  │                          │                     │
       │ Content-Type:      │                          │                     │
       │  application/json  │                          │                     │
       │ {jsonrpc: "2.0",   │                          │                     │
       │  method: "...",    │                          │                     │
       │  params: {...}}    │                          │                     │
       ├───────────────────►│                          │                     │
       │                    │                          │                     │
       │                    │ 1. Rate limiting         │                     │
       │                    │                          │                     │
       │                    │ 2. Loop prevention       │                     │
       │                    │    (check X-A2A-Hops,    │                     │
       │                    │     X-A2A-Agent-Chain)   │                     │
       │                    │                          │                     │
       │                    │ 3. Auth middleware       │                     │
       │                    │    (validate X-API-Key,  │                     │
       │                    │     set caller_id, role) │                     │
       │                    │                          │                     │
       │                    │ 4. JSON-RPC dispatch     │                     │
       │                    ├─────────────────────────►│                     │
       │                    │                          │                     │
       │                    │                          │ 5. Parse A2A request│
       │                    │                          ├────────────────────►│
       │                    │                          │    (a2asrv.Handler) │
       │                    │                          │                     │
       │                    │                          │◄────────────────────┤
       │                    │                          │    Task/Message     │
       │                    │                          │                     │
       │                    │                          │ 6. Create session   │
       │                    │                          │    (user_id from    │
       │                    │                          │     knowledge_scope)│
       │                    │                          │                     │
       │                    │                          │ 7. Build prompt     │
       │                    │                          │    (no Slack ctx)   │
       │                    │                          │                     │
       │                    │                          │ 8. Run LLM          │
       │                    │                          │    (same loop as    │
       │                    │                          │     Slack flow)     │
       │                    │                          │                     │
       │                    │                          │ 9. May call         │
       │                    │                          │    sub-agents       │
       │                    │                          │    (query_<name>    │
       │                    │                          │     tools)          │
       │                    │                          │                     │
       │                    │◄─────────────────────────┤                     │
       │                    │   A2A Response           │                     │
       │                    │                          │                     │
       │◄───────────────────┤ JSON-RPC Response        │                     │
       │   {jsonrpc: "2.0", │                          │                     │
       │    result: {...},  │                          │                     │
       │    id: "..."}      │                          │                     │
       │                    │                          │                     │
```

### A2A Flow Steps Explained

| Step | Component | Description |
|------|-----------|-------------|
| 1 | Agent Server | Rate limiting (10 req/s, burst 20) |
| 2 | Agent Server | Loop prevention: check `X-A2A-Hops` and `X-A2A-Agent-Chain` headers |
| 3 | Agent Server | Validate `X-API-Key`, extract `caller_id` and `role` |
| 4 | Agent Server | Dispatch to A2A JSON-RPC handler |
| 5 | A2A Libraries | Parse JSON-RPC request via `a2asrv.Handler` |
| 6 | ADK Agent | Create session with user_id from `knowledge_scope` config |
| 7 | ADK Agent | Build prompt (no thread context, no Slack identity) |
| 8 | ADK Agent | Execute LLM with tool loop |
| 9 | ADK Agent | May invoke sub-agents via `query_<name>` tools (no handoff) |

### A2A vs Slack: Key Differences

| Aspect | Slack Flow | A2A Flow |
|--------|------------|----------|
| **Authentication** | `X-Internal-Token` (trusted) | `X-API-Key` or `Authorization: Bearer` |
| **User Identity** | `X-Slack-User-Id` passed | No user identity (service auth) |
| **Thread Context** | Full thread history | Single message |
| **Images** | Downloaded from Slack | Not supported |
| **Permissions** | User-level (allowed_slack_users) | Service-level (admin_caller_ids) |
| **Protocol** | REST JSON | JSON-RPC 2.0 |
| **Response** | Posted to Slack | Returned to caller |

---

## Component Details

### 1. Slack Bridge (`internal/slack/handler.go`)

**Responsibilities:**
- Receive and verify Slack events
- Fetch thread context and user information
- Download and encode images
- Forward requests to Agent Server
- Format and post responses to Slack

**Key Methods:**
```
HandleEvents()      → Entry point for /slack/events
handleAppMention()  → Process @bot mentions in channels
handleDirectMessage() → Process DMs (no @mention needed)
sendToAgent()       → Forward to Agent Server with context
```

### 2. Agent Server (`internal/server/agent_server.go`)

**Responsibilities:**
- HTTP routing and middleware
- Authentication (internal token, API keys, Slack signature)
- Rate limiting
- A2A loop prevention
- Request validation

**Endpoints:**
| Endpoint | Auth | Description |
|----------|------|-------------|
| `/health`, `/ready`, `/live` | Public | Health checks |
| `/metrics` | Public | Prometheus metrics |
| `/.well-known/agent-card.json` | Public | A2A agent discovery |
| `/api/query` | Protected | Query knowledge base or ingest threads (via `intent` field) |
| `/api/query/stream` | Protected | Streaming version of `/api/query` via SSE |
| `/a2a/invoke` | Protected | A2A JSON-RPC endpoint |

### 3. ADK Agent (`internal/agent/agent.go`)

**Responsibilities:**
- Initialize LLM client (Anthropic Claude)
- Manage sessions (Redis)
- Manage memory (PostgreSQL + pgvector)
- Execute tools
- Run LLM inference loop

**Components:**
```
llmAgent        → LLM configuration and tools
runner          → ADK runner for orchestration
sessionService  → Redis session management
memoryService   → PostgreSQL memory with embeddings
```

### 4. Authentication Middleware (`internal/server/middleware.go`)

**Auth Methods (checked in order):**
1. **Internal Token** (`X-Internal-Token`): Trusted, can pass `X-Slack-User-Id`
2. **JWT Bearer** (`Authorization: Bearer <token>`): Extracts email/groups for permissions
3. **API Key** (`X-API-Key`): Untrusted for user identity, has role (read/write)
4. **Slack Signature** (`X-Slack-Signature`): Legacy direct webhook auth
5. **Open Mode**: No auth required (development only)

### 5. A2A Handler (`internal/server/a2a_handler.go`)

**Responsibilities:**
- Serve agent card for discovery
- Handle JSON-RPC invocations
- Execute via ADK executor

---

## Data Flow Diagram

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              DATA STORES                                    │
└─────────────────────────────────────────────────────────────────────────────┘

                    ┌─────────────────────────────────────┐
                    │              Agent                   │
                    │                                     │
                    │  ┌───────────┐    ┌───────────┐   │
                    │  │   Query   │    │  Ingest   │   │
                    │  └─────┬─────┘    └─────┬─────┘   │
                    │        │                │         │
                    └────────┼────────────────┼─────────┘
                             │                │
              ┌──────────────┼────────────────┼──────────────┐
              │              │                │              │
              ▼              ▼                ▼              │
    ┌─────────────┐  ┌─────────────┐  ┌─────────────┐       │
    │   Redis     │  │ PostgreSQL  │  │   Ollama    │       │
    │             │  │             │  │             │       │
    │  Sessions   │  │  Memories   │  │ Embeddings  │       │
    │  (TTL-based)│  │  (pgvector) │  │ (nomic-embed│       │
    │             │  │             │  │  -text)     │       │
    │ ┌─────────┐ │  │ ┌─────────┐ │  │             │       │
    │ │ Session │ │  │ │ Memory  │ │  │             │       │
    │ │ history │ │  │ │ chunks  │ │  │             │       │
    │ │ (JSON)  │ │  │ │ + vector│ │  │             │       │
    │ └─────────┘ │  │ └─────────┘ │  │             │       │
    └─────────────┘  └─────────────┘  └─────────────┘       │
          │                │                │               │
          │                │                │               │
          ▼                ▼                ▼               │
    ┌─────────────────────────────────────────────────────┐ │
    │                    Tools                             │ │
    │                                                      │ │
    │  search_memory  ───► Query pgvector (semantic)      │ │
    │  save_to_memory ───► Generate embedding → Store     │ │
    │  fetch_url      ───► HTTP fetch → Extract content   │ │
    │  query_<name>      → Call remote A2A agent (no handoff) │
    │                                                      │ │
    └─────────────────────────────────────────────────────┘ │
                                                            │
                              ┌──────────────────────────────┘
                              │
                              ▼
                    ┌─────────────────┐
                    │    Anthropic    │
                    │    API         │
                    │                │
                    │ Claude Sonnet  │
                    │ 4.5            │
                    └─────────────────┘
```

### Data Store Responsibilities

| Store | Data | Lifetime | Purpose |
|-------|------|----------|---------|
| **Redis** | Session history, conversation state | TTL (configurable) | Short-term context |
| **PostgreSQL** | Memory chunks + embeddings | Permanent | Long-term knowledge |
| **Ollama** | Embedding vectors | N/A (compute) | Semantic search |
| **Anthropic** | LLM responses | N/A (compute) | Intelligence |

---

## Key Files Reference

| File | Purpose |
|------|---------|
| `cmd/knowledge-agent/main.go` | Entry point, mode selection |
| `internal/agent/agent.go` | Core agent initialization and execution |
| `internal/agent/prompts.go` | System prompt definition |
| `internal/slack/handler.go` | Slack event handling |
| `internal/server/agent_server.go` | HTTP server and routing |
| `internal/server/middleware.go` | Authentication middleware |
| `internal/server/a2a_handler.go` | A2A protocol handlers |
| `internal/config/config.go` | Configuration structures |

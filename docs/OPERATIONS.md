# Operations Guide

Complete operational guide for Knowledge Agent: logging, traceability, and observability.

## Logging

Knowledge Agent uses **Uber Zap** for high-performance structured logging.

### Log Levels

#### debug
Very detailed logging, includes:
- ADK runner events
- Tool calls
- Tool responses
- Message content (truncated)
- Event counts
- Processing details

**When to use**: Debugging issues, feature development, investigating agent behavior.

#### info (default)
Standard operational logging:
- Requests received
- Successful operations
- Service start/stop
- Configuration loaded
- Important tool calls (save_to_memory, search_memory)

**When to use**: Production, normal operations.

#### warn
Abnormal but recoverable situations:
- Recoverable errors
- Fallbacks
- Failed connections that will be retried

**When to use**: Production with alerting.

#### error
Non-recoverable errors:
- Request failures
- Agent errors
- Infrastructure problems

**When to use**: Always in production.

### Output Formats

#### console (human-readable)
```
2026-01-24T15:30:45.123+0100    INFO    agent/agent.go:386    Running agent for query
2026-01-24T15:30:47.456+0100    INFO    agent/agent.go:420    Agent calling tool    {"tool": "search_memory", "args_count": 2}
```

**When to use**: Local development, debugging.

#### json (structured)
```json
{"level":"info","ts":"2026-01-24T15:30:45.123+0100","caller":"agent/agent.go:386","msg":"Running agent for query"}
{"level":"info","ts":"2026-01-24T15:30:47.456+0100","caller":"agent/agent.go:420","msg":"Agent calling tool","tool":"search_memory","args_count":2}
```

**When to use**: Production, integration with logging systems (ELK, Splunk, Datadog).

### Configuration

#### Environment Variables (.env)

```bash
# Development with debugging
LOG_LEVEL=debug
LOG_FORMAT=console
LOG_OUTPUT=stdout

# Production
LOG_LEVEL=info
LOG_FORMAT=json
LOG_OUTPUT=/var/log/knowledge-agent.log
```

#### Config File (config.yaml)

```yaml
log:
  level: debug
  format: console
  output_path: stdout
```

#### Runtime Override

```bash
# Temporary override
LOG_LEVEL=debug make dev

# Or with binaries directly
LOG_LEVEL=debug ./bin/knowledge-agent
```

### Output Destinations

#### stdout (default)
Logs to standard output.

```yaml
log:
  output_path: stdout
```

#### stderr
Logs to standard error (useful for separating logs from normal output).

```yaml
log:
  output_path: stderr
```

#### File
Logs to file (important: log rotation NOT included, use logrotate or similar).

```yaml
log:
  output_path: /var/log/knowledge-agent.log
```

### Important Logs

#### During Query (LOG_LEVEL=debug)

```
INFO    Running agent for query
DEBUG   Runner event received    {"event_number": 1, ...}
INFO    Agent calling tool       {"tool": "search_memory"}
DEBUG   Tool response received   {"tool": "search_memory"}
DEBUG   Text part               {"length": 1234, "preview": "..."}
INFO    Query completed         {"total_events": 5, "response_length": 1234}
```

#### During Ingestion (LOG_LEVEL=debug)

```
INFO    Running agent for thread ingestion
INFO    Agent calling tool during ingestion    {"tool": "save_to_memory"}
DEBUG   Memory save detected    {"total_saves": 1}
INFO    Thread ingestion completed    {"memories_saved": 3, "total_events": 8}
```

### Best Practices

1. **Development**: `LOG_LEVEL=debug LOG_FORMAT=console`
2. **Staging**: `LOG_LEVEL=info LOG_FORMAT=json`
3. **Production**: `LOG_LEVEL=info LOG_FORMAT=json LOG_OUTPUT=/var/log/...`
4. **Debugging Issues**: Temporarily increase to `debug` and grep for the problem
5. **Log Rotation**: Use logrotate for log files
6. **Monitoring**: Integrate JSON logs with your observability system

---

## Traceability

Knowledge Agent implements comprehensive traceability to track who makes requests and from where.

### Traceability Levels

#### 1. Caller ID (Authentication Source)

**Purpose**: Identifies the service/source making the request to the agent.

**Values**:
- `slack-bridge` - Requests from Slack Bridge (authenticated with internal token)
- `root-agent` - Direct requests from root orchestration agent (A2A)
- `monitoring` - Direct requests from monitoring service (A2A)
- `external-service` - Direct requests from other external services (A2A)
- `slack-direct` - Legacy: Direct webhooks from Slack (authenticated with Slack signature)
- `unauthenticated` - Requests when authentication is disabled (dev mode)

**How it works**:
- Set by `AuthMiddleware` based on authentication method used
- Stored in request context via `ctxutil.CallerIDKey`
- Retrieved using `ctxutil.CallerID(ctx)`

**Logged in**:
- Agent query processing
- Agent thread ingestion
- Server request handling

#### 2. Slack User ID (End User)

**Purpose**: Identifies the actual Slack user who initiated the request (when coming through Slack Bridge).

**Format**: Slack User ID (e.g., `U123ABC456`)

**How it works**:
1. Slack Bridge receives event from Slack with `event.User`
2. Bridge adds `X-Slack-User-Id` header to request to Agent
3. `AuthMiddleware` captures header and stores in context via `ctxutil.SlackUserIDKey`
4. Retrieved using `ctxutil.SlackUserID(ctx)`

**Logged in**:
- Slack Bridge event reception
- Agent query processing (if present)
- Agent thread ingestion (if present)

**Note**: Only present for requests coming through Slack Bridge. Empty for direct A2A requests.

### Log Examples

#### Slack User Request

```
INFO  slack/handler.go  Slack event received
      user=U123ABC456
      thread_ts=1234567890.123
      channel=C123XYZ
      message="How do we deploy?"

INFO  agent/agent.go  Processing query
      caller_id=slack-bridge
      slack_user_id=U123ABC456
      question="How do we deploy?"
      channel_id=C123XYZ

INFO  agent/agent.go  Query completed successfully
      caller_id=slack-bridge
      slack_user_id=U123ABC456
      total_events=5
      response_length=234
```

#### Direct A2A Request

```
INFO  agent/agent.go  Processing query
      caller_id=root-agent
      question="What's our deployment process?"
      channel_id=api-channel

INFO  agent/agent.go  Query completed successfully
      caller_id=root-agent
      total_events=3
      response_length=456
```

#### Unauthenticated Request (Development)

```
INFO  agent/agent.go  Processing query
      caller_id=unauthenticated
      question="test query"
      channel_id=test

INFO  agent/agent.go  Query completed successfully
      caller_id=unauthenticated
      total_events=2
      response_length=123
```

### Implementation Details

#### Context Utilities Package

**File**: `internal/ctxutil/context.go`

Provides shared context key definitions and accessor functions:

```go
// Context keys
const (
    CallerIDKey    contextKey = "caller_id"
    SlackUserIDKey contextKey = "slack_user_id"
)

// Accessor functions
func CallerID(ctx context.Context) string
func SlackUserID(ctx context.Context) string
```

This package prevents import cycles by providing a shared location for context utilities.

#### Data Flow

```
Slack User (U123ABC456)
  ↓
Slack API
  ↓
Slack Bridge (handler.go)
  - Receives event.User
  - Logs: user=U123ABC456
  - HTTP Request to Agent:
    - X-Internal-Token: <internal_token>
    - X-Slack-User-Id: U123ABC456
  ↓
Agent (middleware.go)
  - Validates X-Internal-Token
  - Captures X-Slack-User-Id
  - Sets context:
    - caller_id=slack-bridge
    - slack_user_id=U123ABC456
  ↓
Agent (agent.go)
  - Extracts from context using ctxutil
  - Logs both values
  - Processes query
```

### Best Practices

#### 1. Always Check Both IDs

When logging agent operations, always include caller_id and slack_user_id (if present):

```go
logFields := []interface{}{
    "caller_id", ctxutil.CallerID(ctx),
    "operation", "my_operation",
}
if slackUserID := ctxutil.SlackUserID(ctx); slackUserID != "" {
    logFields = append(logFields, "slack_user_id", slackUserID)
}
log.Infow("Operation started", logFields...)
```

#### 2. Use Structured Logging

Always use structured fields, not string concatenation:

✅ **Good**:
```go
log.Infow("Query received",
    "caller_id", callerID,
    "slack_user_id", slackUserID,
    "question", question,
)
```

❌ **Bad**:
```go
log.Infof("Query from %s (user %s): %s", callerID, slackUserID, question)
```

#### 3. Consistent Field Names

Always use these exact field names:
- `caller_id` (not `caller`, `source`, `client_id`)
- `slack_user_id` (not `user_id`, `slack_id`, `user`)

#### 4. Extract Once, Use Many

Extract context values once at the beginning of the function:

```go
func (a *Agent) Query(ctx context.Context, req QueryRequest) (*QueryResponse, error) {
    // Extract once
    callerID := ctxutil.CallerID(ctx)
    slackUserID := ctxutil.SlackUserID(ctx)

    // Use throughout function
    log.Infow("Starting query", "caller_id", callerID, "slack_user_id", slackUserID)
    // ... processing ...
    log.Infow("Completed query", "caller_id", callerID, "slack_user_id", slackUserID)
}
```

---

## Integration with Logging Systems

### ELK Stack

```yaml
# filebeat.yml
filebeat.inputs:
- type: log
  paths:
    - /var/log/knowledge-agent/*.log
  json.keys_under_root: true
  json.add_error_key: true
```

### Datadog

```yaml
# datadog.yaml
logs:
  - type: file
    path: /var/log/knowledge-agent/*.log
    service: knowledge-agent
    source: go
```

### CloudWatch

Use AWS CloudWatch agent with JSON parsing.

---

## Troubleshooting

### No DEBUG logs visible

Verify that `LOG_LEVEL=debug` is configured:

```bash
# Check env var
echo $LOG_LEVEL

# Force debug
LOG_LEVEL=debug ./bin/knowledge-agent
```

### Logs not appearing in file

Check permissions:

```bash
# Check if file is writable
touch /var/log/knowledge-agent.log
chmod 644 /var/log/knowledge-agent.log
```

### Too many logs in production

Use more restrictive level:

```bash
LOG_LEVEL=warn  # Only warnings and errors
LOG_LEVEL=error # Only errors
```

### Missing Slack User ID

**Symptom**: Logs show `caller_id=slack-bridge` but no `slack_user_id`

**Possible Causes**:
1. Slack Bridge not sending `X-Slack-User-Id` header
2. Slack event without `User` field
3. Middleware not capturing header

**Debug**:
```bash
# Check Slack Bridge logs
grep "Slack event received" logs | grep -v "user="
```

### Wrong Caller ID

**Symptom**: Expected `slack-bridge` but see `root-agent`

**Possible Causes**:
1. Request using incorrect authentication method (X-API-Key instead of X-Internal-Token)
2. Multiple authentication headers present
3. Incorrect configuration

**Debug**:
```bash
# Check authentication logs
grep "Invalid.*attempt" logs

# Verify configuration
grep -A 5 "auth:" config.yaml
```

---

## See Also

- [SECURITY.md](SECURITY.md) - Authentication and authorization
- [CONFIGURATION.md](CONFIGURATION.md) - System configuration
- [TESTING.md](TESTING.md) - Testing and QA
- [../CLAUDE.md](../CLAUDE.md) - System architecture

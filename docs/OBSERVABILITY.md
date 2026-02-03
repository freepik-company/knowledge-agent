# Observability Guide

Comprehensive guide to observability and monitoring in Knowledge Agent, including Langfuse integration, structured logging, and metrics.

## Table of Contents

1. [Langfuse Observability](#langfuse-observability)
2. [Structured Logging](#structured-logging)
3. [Metrics](#metrics)
4. [Troubleshooting](#troubleshooting)

---

## Langfuse Observability

Langfuse provides comprehensive LLM observability, tracking every interaction with Claude AI including costs, tokens, tool usage, and performance.

### What is Langfuse?

[Langfuse](https://langfuse.com) is an open-source LLM observability platform that helps you:
- Track token usage and costs across all LLM interactions
- Monitor performance and latency
- Debug tool calls and generations
- Analyze user behavior and query patterns
- Identify expensive or problematic queries

### SDK Information

**Knowledge Agent uses:** `github.com/git-hulk/langfuse-go v0.1.0`

This is a **modern, feature-complete, community-maintained SDK** with:
- ✅ Clean API (StartTrace, StartGeneration, End)
- ✅ Native support for Generations, Spans, Tools, Events
- ✅ Automatic batch processing with efficient flushing
- ✅ Built-in TotalCost field for cost tracking
- ✅ Graceful shutdown with automatic flush
- ✅ Comprehensive API: Prompts, Models, Datasets, Sessions, Scores

**Note:** Langfuse does NOT have an official Go SDK. For official support, see their [Python SDK](https://github.com/langfuse/langfuse-python) or [JavaScript SDK](https://github.com/langfuse/langfuse-js).

### Configuration

#### Basic Setup

```yaml
# config.yaml
langfuse:
  enabled: true
  public_key: ${LANGFUSE_PUBLIC_KEY}     # Get from Langfuse project settings
  secret_key: ${LANGFUSE_SECRET_KEY}     # Get from Langfuse project settings
  host: https://cloud.langfuse.com       # Or https://us.cloud.langfuse.com for US

  # Model pricing (used for automatic cost calculation)
  input_cost_per_1m: 3.0    # Input tokens per 1M (Claude Sonnet 4.5 = $3)
  output_cost_per_1m: 15.0  # Output tokens per 1M (Claude Sonnet 4.5 = $15)
```

#### Environment Variables

```bash
# .env
LANGFUSE_ENABLED=true
LANGFUSE_PUBLIC_KEY=pk_lf_xxx
LANGFUSE_SECRET_KEY=sk_lf_xxx
LANGFUSE_HOST=https://cloud.langfuse.com
```

#### Getting Your API Keys

1. Sign up at [https://cloud.langfuse.com](https://cloud.langfuse.com) (or self-host)
2. Create a project
3. Go to **Settings** → **API Keys**
4. Copy your **Public Key** and **Secret Key**
5. Add them to your config

### What Gets Tracked

#### 1. Traces

Every query creates a trace capturing the complete execution flow:

**Fields:**
- **Input**: User's question
- **Output**: AI response with metadata (tokens, costs, success status)
- **Latency**: Total execution time (milliseconds)
- **TotalCost**: Calculated cost in USD
- **UserID**: Set to user_name from Slack for user-level analytics
- **Metadata**: channel_id, thread_ts, caller_id, user_real_name
- **Tags**: ["query", "knowledge-agent"]

**Example Trace:**
```json
{
  "id": "trace-abc123",
  "name": "knowledge-agent-query",
  "input": {
    "question": "How do we deploy to production?"
  },
  "output": {
    "success": true,
    "answer": "Based on our documentation...",
    "duration_ms": 2500,
    "prompt_tokens": 5000,
    "completion_tokens": 200,
    "total_tokens": 5200,
    "generations_count": 2,
    "tool_calls_count": 3
  },
  "latency": 2500,
  "totalCost": 0.018,
  "userId": "dfradejas",
  "metadata": {
    "caller_id": "slack-bridge",
    "channel_id": "C12345",
    "thread_ts": "1234567890.123456",
    "user_name": "dfradejas",
    "user_real_name": "Daniel Fradejas"
  }
}
```

#### 2. Generations

Each LLM call (Claude API request) is tracked as a Generation observation:

**Fields:**
- **Model**: claude-sonnet-4-5-20250929
- **Input**: Complete prompt sent to LLM
- **Output**: Text response from LLM
- **Usage**: Token counts (input, output, total)
- **Timing**: Start time, end time, duration

**Example Generation:**
```json
{
  "id": "gen-xyz789",
  "type": "GENERATION",
  "name": "generation-1",
  "model": "claude-sonnet-4-5-20250929",
  "input": "System: You are a Knowledge Management Assistant...",
  "output": "Based on our documentation, deployments...",
  "usage": {
    "input": 5000,
    "output": 200,
    "total": 5200,
    "unit": "TOKENS"
  },
  "startTime": "2026-01-24T10:30:00.123Z",
  "endTime": "2026-01-24T10:30:02.456Z"
}
```

#### 3. Tool Calls (Observations)

Every tool invocation is tracked as a TOOL observation:

**Supported Tools:**
- `search_memory` - Semantic search in knowledge base
- `save_to_memory` - Save information to knowledge base
- `fetch_url` - Fetch and analyze web content

**Fields:**
- **Name**: Tool name (e.g., "search_memory")
- **Input**: Tool arguments
- **Output**: Tool result
- **Level**: DEFAULT (success) or ERROR (failed)
- **StatusMessage**: Error message if failed
- **Timing**: Start time, end time, duration

**Example Tool Call:**
```json
{
  "id": "tool-123",
  "type": "TOOL",
  "name": "search_memory",
  "input": {
    "query": "production deployment",
    "limit": 5
  },
  "output": {
    "results": [
      {"content": "Our deployment process...", "score": 0.92}
    ]
  },
  "level": "DEFAULT",
  "startTime": "2026-01-24T10:30:00.500Z",
  "endTime": "2026-01-24T10:30:00.750Z"
}
```

### Architecture

```
ADK Runner Events
  ↓
1. event.UsageMetadata detected → StartGeneration()
  ↓
2. part.FunctionCall detected → StartToolCall()
  ↓
3. part.FunctionResponse detected → EndToolCall()
  ↓
4. Next event with UsageMetadata → EndGeneration()
  ↓
5. All events processed → trace.End()
  ↓
git-hulk SDK → Batch processor → Langfuse API
```

**Key Implementation Points:**
- Generations start when `UsageMetadata` is detected
- Generations end when next `UsageMetadata` arrives
- Tool calls tracked immediately on function call/response
- Tokens accumulated across multiple generations
- Costs calculated once at trace end
- `trace.End()` triggers batch submission

### Cost Tracking

#### Automatic Cost Calculation

Costs are calculated automatically using configured pricing:

```go
inputCost = (promptTokens / 1_000_000) * inputCostPer1M
outputCost = (completionTokens / 1_000_000) * outputCostPer1M
totalCost = inputCost + outputCost
```

**Example:**
- Prompt tokens: 5,000
- Completion tokens: 200
- Input cost: $3 per 1M tokens
- Output cost: $15 per 1M tokens

```
inputCost = (5000 / 1,000,000) * 3 = $0.015
outputCost = (200 / 1,000,000) * 15 = $0.003
totalCost = $0.018
```

#### Update Pricing for Different Models

```yaml
langfuse:
  # Claude Opus 4.5
  input_cost_per_1m: 15.0
  output_cost_per_1m: 75.0

  # Claude Haiku 4
  input_cost_per_1m: 0.8
  output_cost_per_1m: 4.0

  # GPT-4 Turbo
  input_cost_per_1m: 10.0
  output_cost_per_1m: 30.0
```

### Using Langfuse UI

#### Viewing Traces

1. Log in to [https://cloud.langfuse.com](https://cloud.langfuse.com)
2. Select your project
3. Click **Traces** in the sidebar
4. See all queries with:
   - Question (Input)
   - Answer preview (Output)
   - Total cost
   - Latency
   - Token counts
   - User ID (from Slack username)

#### Analyzing Costs

**Per User:**
- Filter by UserID
- See total costs per user
- Identify expensive users/queries

**Per Time Period:**
- Use date range filter
- Analyze cost trends
- Budget forecasting

**Per Query Type:**
- Use Tags or Metadata filters
- Compare costs across different query patterns

#### Debugging Issues

**Slow Queries:**
1. Sort by Latency (descending)
2. Click on slow trace
3. See Generations and Tool Calls timeline
4. Identify bottleneck (LLM, tool, search)

**Failed Queries:**
1. Filter by Metadata: `success = false`
2. Check Output for error messages
3. Review Tool Calls for errors (Level = ERROR)

**Token-Heavy Queries:**
1. Sort by Total Tokens (descending)
2. Click on trace
3. Check Generation Input size
4. Optimize prompts or context

### Performance Impact

**Minimal Overhead:**
- Trace creation: < 1ms
- Event tracking: < 0.1ms per event
- Batch processing: asynchronous, non-blocking
- Network I/O: batched, efficient

**Best Practices:**
- Keep `enabled: true` in production
- Use for cost monitoring and debugging
- Archive old traces in Langfuse for historical analysis

### Troubleshooting

#### Traces Not Appearing

**Symptom:** No traces in Langfuse UI

**Checks:**
1. Verify `enabled: true` in config
2. Check LANGFUSE_PUBLIC_KEY and LANGFUSE_SECRET_KEY
3. Verify host URL (https://cloud.langfuse.com or self-hosted)
4. Check logs for "Langfuse tracing enabled" message
5. Look for error logs about Langfuse connection

**Debug:**
```bash
# Check if Langfuse is enabled
grep "Langfuse tracing enabled" logs.txt

# Check for Langfuse errors
grep "Langfuse\|langfuse" logs.txt | grep -i "error\|warn"
```

#### Costs Showing $0.00

**Symptom:** TotalCost field is always $0.00

**Causes:**
1. `input_cost_per_1m` or `output_cost_per_1m` set to 0
2. Tokens not being captured (check Generation usage)
3. Model name mismatch (costs only calculated if model known)

**Solutions:**
1. Set pricing in config: `input_cost_per_1m: 3.0`, `output_cost_per_1m: 15.0`
2. Check logs for "prompt_tokens", "completion_tokens"
3. Verify model name matches pricing config

#### User Names Not Appearing

**Symptom:** UserID is empty in traces

**Causes:**
1. Slack user info fetching failed
2. Old process running without user info code
3. Missing `users:read` scope in Slack app

**Solutions:**
1. Kill zombie processes: `make cleanup`
2. Add `users:read` scope and reinstall Slack app
3. Check logs for "User info fetched" message

#### High Latency from Langfuse

**Symptom:** Requests slow when Langfuse is enabled

**Causes:**
1. Network connectivity issues
2. Self-hosted Langfuse instance slow
3. Synchronous flush being called

**Solutions:**
1. Use Langfuse cloud (faster than most self-hosted)
2. Ensure batch processing is asynchronous (default)
3. Only flush on shutdown, not per trace

---

## Structured Logging

Knowledge Agent uses **Uber's Zap** for high-performance structured logging.

### Configuration

```yaml
log:
  level: info          # debug, info, warn, error
  format: console      # json, console
  output_path: stdout  # stdout, stderr, /path/to/file.log
```

### Log Levels

- **DEBUG**: Verbose operation details, content structure, token counts
- **INFO**: Request lifecycle, successful operations, startup/shutdown
- **WARN**: Recoverable errors, missing optional data, degraded service
- **ERROR**: Request failures, service errors, unrecoverable issues

### Log Format

**Console (Development):**
```
2026-01-24T10:30:00.123Z  INFO  Query request received  {"caller": "slack-bridge", "question": "deploy"}
```

**JSON (Production):**
```json
{"level":"info","ts":"2026-01-24T10:30:00.123Z","msg":"Query request received","caller":"slack-bridge","question":"deploy"}
```

### Key Fields

**All Logs:**
- `level` - Log level (debug/info/warn/error)
- `ts` - Timestamp (ISO 8601)
- `msg` - Human-readable message

**Query Logs:**
- `caller_id` - Authentication caller (slack-bridge, root-agent, etc.)
- `user_name` - Slack username (e.g., "dfradejas")
- `user_real_name` - Full name (e.g., "Daniel Fradejas")
- `question` - User's question
- `channel_id` - Slack channel ID
- `thread_ts` - Slack thread timestamp
- `trace_id` - Langfuse trace ID (if enabled)

**Performance Logs:**
- `duration_ms` - Operation duration in milliseconds
- `prompt_tokens` - Input tokens to LLM
- `completion_tokens` - Output tokens from LLM
- `total_tokens` - Sum of input + output tokens
- `total_cost_usd` - Cost in USD (formatted)

### Example Log Queries

**Find all queries by user:**
```bash
# Console format
grep '"user_name":"dfradejas"' logs.txt

# JSON format (with jq)
jq 'select(.user_name=="dfradejas")' logs.jsonl
```

**Find expensive queries (> $0.01):**
```bash
# Requires JSON format
jq 'select(.total_cost_usd > 0.01)' logs.jsonl
```

**Find slow queries (> 5 seconds):**
```bash
jq 'select(.duration_ms > 5000)' logs.jsonl
```

---

## Metrics

Knowledge Agent exposes basic metrics via the `/metrics` endpoint.

### Endpoint

```bash
curl http://localhost:8081/metrics
```

### Available Metrics

**Current Metrics:**
```json
{
  "uptime_seconds": 3600.5,
  "queries": {
    "total": 150,
    "errors": 3,
    "error_rate_percent": 2.0,
    "avg_latency_ms": 1250.5
  },
  "memory": {
    "saves": 45,
    "searches": 120,
    "errors": 2,
    "error_rate_percent": 1.2
  },
  "pre_search": {
    "total": 148,
    "errors": 2,
    "error_rate_percent": 1.35,
    "avg_latency_ms": 85.3
  },
  "tools": {
    "total": 320,
    "errors": 5,
    "error_rate_percent": 1.56
  },
  "a2a": {
    "total": 25,
    "errors": 1,
    "error_rate_percent": 4.0
  },
  "tokens_used": 1250000
}
```

**Pre-Search Metrics:**
- `pre_search.total` - Number of automatic memory searches executed before LLM loop
- `pre_search.errors` - Failed pre-searches (timeouts after 3s, database errors)
- `pre_search.avg_latency_ms` - Average search latency in milliseconds

> **Note**: Pre-search runs automatically on every query to provide memory context upfront. It has a 3-second timeout and limits results to 5 entries to avoid blocking the main request.

**Prometheus Metrics:**

Full Prometheus-compatible metrics are available at `/metrics`. See [PROMETHEUS_METRICS.md](PROMETHEUS_METRICS.md) for complete documentation including:
- Query latency histograms
- Tool execution metrics by name
- A2A sub-agent call metrics
- Pre-search latency distribution

---

## Troubleshooting

### General Observability Issues

#### No Logs Appearing

**Check:**
1. Log level not too restrictive (`level: debug` for verbose)
2. Output path is writable
3. Logger initialized properly (check startup logs)

#### JSON Logs Not Parsing

**Common Issues:**
1. Mixed format (some console, some JSON)
2. Multi-line log messages
3. Custom fields not JSON-serializable

**Solution:**
```yaml
log:
  format: json  # Ensure consistent format
  level: info
```

#### High Log Volume

**Reduce verbosity:**
```yaml
log:
  level: warn  # Only warnings and errors
```

**Use log sampling:**
```go
// Future enhancement
logger.Sampling(rate: 0.1)  // Sample 10% of logs
```

### Performance Issues

#### High Latency

**Check:**
1. Langfuse causing delays? (Disable temporarily)
2. Logging to slow disk? (Use stdout in production)
3. Too much DEBUG logging? (Set level: info)

**Profile:**
```bash
# Check latency distribution
jq '.duration_ms' logs.jsonl | sort -n | tail -20
```

#### High Memory Usage

**Common Causes:**
1. Large context windows (many messages in thread)
2. Big images being processed
3. Log buffers not flushing

**Monitor:**
```bash
# Check memory in logs
grep "memory" logs.txt

# Or use system tools
ps aux | grep knowledge-agent
```

---

## Best Practices

### Development

✅ Use console logs for readability
✅ Set level to debug for verbose output
✅ Enable Langfuse for cost tracking
✅ Check traces in Langfuse UI after each query

```yaml
log:
  level: debug
  format: console
  output_path: stdout

langfuse:
  enabled: true
```

### Production

✅ Use JSON logs for log aggregators
✅ Set level to info (or warn for high-traffic)
✅ Enable Langfuse for monitoring and debugging
✅ Use stdout (let container runtime handle logs)
✅ Set up log aggregation (ELK, CloudWatch, etc.)

```yaml
log:
  level: info
  format: json
  output_path: stdout

langfuse:
  enabled: true
  host: https://cloud.langfuse.com
```

### Cost Optimization

✅ Monitor costs per user in Langfuse
✅ Set budget alerts
✅ Optimize prompts to reduce tokens
✅ Cache frequent queries (future enhancement)
✅ Use cheaper models for simple queries (future enhancement)

---

## Integration with External Tools

### Log Aggregation

**Elasticsearch + Kibana:**
```yaml
log:
  format: json
  output_path: /var/log/knowledge-agent/app.jsonl
```

Then use Filebeat to ship to Elasticsearch.

**CloudWatch Logs (AWS):**
```yaml
log:
  format: json
  output_path: stdout
```

Container logs automatically shipped to CloudWatch.

**Datadog:**
```yaml
log:
  format: json
  output_path: stdout
```

Use Datadog agent to collect logs.

### Monitoring

**Prometheus (Future):**
```bash
# /metrics endpoint will be Prometheus-compatible
curl http://localhost:8081/metrics
```

**Grafana Dashboards:**
- Query rate, error rate, latency
- Cost per user/per day
- Token usage trends
- Tool usage distribution

---

## Summary

Knowledge Agent provides comprehensive observability through:

1. **Langfuse** - LLM-specific observability (costs, tokens, tools, performance)
2. **Zap Logging** - Structured, high-performance application logs
3. **Metrics** - Basic operational metrics (query counts, errors, tokens)

**Together, these provide:**
- Complete visibility into LLM operations
- Cost tracking and optimization
- Performance monitoring and debugging
- User behavior analytics
- Production-ready observability stack

For questions or issues, check the [Troubleshooting](#troubleshooting) section or file an issue on GitHub.

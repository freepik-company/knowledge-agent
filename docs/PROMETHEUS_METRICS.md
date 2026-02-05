# Prometheus Metrics

Complete guide to Prometheus metrics exported by Knowledge Agent.

## Table of Contents

- [Overview](#overview)
- [Endpoints](#endpoints)
- [Knowledge Agent Metrics](#knowledge-agent-metrics)
- [Slack Bridge Metrics](#slack-bridge-metrics)
- [Standard Go Metrics](#standard-go-metrics)
- [Example Queries](#example-queries)
- [Grafana Dashboards](#grafana-dashboards)
- [Alerting Rules](#alerting-rules)

---

## Overview

Knowledge Agent exposes Prometheus metrics on `/metrics` endpoint for both services:

- **Knowledge Agent** (port 8081): Core agent metrics (queries, memory, tokens)
- **Slack Bridge** (port 8080): Slack integration metrics (events, API calls, forwards)

**Format**: Prometheus text exposition format
**Authentication**: Public endpoint (no authentication required)

---

## Endpoints

### Knowledge Agent

```
http://localhost:8081/metrics
```

**Available in modes**: `agent`, `all`

### Slack Bridge

```
http://localhost:8080/metrics
```

**Available in modes**: `slack-bot`, `all`

---

## Knowledge Agent Metrics

### Query Metrics

#### `knowledge_agent_queries_total`
- **Type**: Counter
- **Description**: Total number of queries processed
- **Use case**: Track overall query volume

```promql
# Query rate per second
rate(knowledge_agent_queries_total[5m])
```

#### `knowledge_agent_query_errors_total`
- **Type**: Counter
- **Description**: Total number of query errors
- **Use case**: Track query failures

```promql
# Error rate percentage
100 * rate(knowledge_agent_query_errors_total[5m]) / rate(knowledge_agent_queries_total[5m])
```

#### `knowledge_agent_query_latency_seconds`
- **Type**: Histogram
- **Buckets**: 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10 seconds
- **Description**: Query processing latency distribution
- **Use case**: Monitor query performance

```promql
# 95th percentile latency
histogram_quantile(0.95, rate(knowledge_agent_query_latency_seconds_bucket[5m]))

# Average latency
rate(knowledge_agent_query_latency_seconds_sum[5m]) / rate(knowledge_agent_query_latency_seconds_count[5m])
```

### Tool Execution Metrics

#### `knowledge_agent_tool_calls_total{tool_name, status}`
- **Type**: Counter
- **Labels**: `tool_name` (e.g., "search_memory", "save_to_memory", "fetch_url"), `status` (success/error)
- **Description**: Total tool calls by tool name and status
- **Use case**: Track tool usage and failures

```promql
# Tool calls per minute by tool
sum by (tool_name) (rate(knowledge_agent_tool_calls_total[5m]) * 60)

# Tool error rate by tool
sum by (tool_name) (rate(knowledge_agent_tool_calls_total{status="error"}[5m]))
  / sum by (tool_name) (rate(knowledge_agent_tool_calls_total[5m]))
```

#### `knowledge_agent_tool_latency_seconds{tool_name}`
- **Type**: Histogram
- **Buckets**: 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30 seconds
- **Description**: Tool execution latency by tool name
- **Use case**: Monitor tool performance

```promql
# P95 tool latency by tool
histogram_quantile(0.95, sum by (tool_name, le) (rate(knowledge_agent_tool_latency_seconds_bucket[5m])))
```

### A2A Sub-Agent Metrics

#### `knowledge_agent_a2a_calls_total{sub_agent, status}`
- **Type**: Counter
- **Labels**: `sub_agent` (e.g., "logs_agent", "metrics_agent"), `status` (success/error)
- **Description**: Total A2A calls by sub-agent and status
- **Use case**: Track sub-agent communication

```promql
# A2A calls per minute by sub-agent
sum by (sub_agent) (rate(knowledge_agent_a2a_calls_total[5m]) * 60)
```

#### `knowledge_agent_a2a_latency_seconds{sub_agent}`
- **Type**: Histogram
- **Buckets**: 0.5, 1, 2.5, 5, 10, 30, 60 seconds
- **Description**: A2A sub-agent call latency
- **Use case**: Monitor sub-agent performance

```promql
# P95 A2A latency by sub-agent
histogram_quantile(0.95, sum by (sub_agent, le) (rate(knowledge_agent_a2a_latency_seconds_bucket[5m])))
```

### Ingest Metrics

#### `knowledge_agent_ingest_total`
- **Type**: Counter
- **Description**: Total number of ingest operations
- **Use case**: Track knowledge ingestion volume

```promql
# Ingest operations per hour
rate(knowledge_agent_ingest_total[5m]) * 3600
```

#### `knowledge_agent_ingest_errors_total`
- **Type**: Counter
- **Description**: Total number of ingest errors
- **Use case**: Monitor ingestion reliability

#### `knowledge_agent_ingest_latency_seconds`
- **Type**: Histogram
- **Description**: Ingest latency distribution
- **Use case**: Monitor ingestion performance

### Pre-Search Memory

Pre-search is a programmatic memory search executed automatically before the LLM loop to provide relevant context upfront.

#### `knowledge_agent_presearch_total`
- **Type**: Counter
- **Description**: Total number of pre-search memory operations
- **Use case**: Track automatic memory lookups before LLM processing

```promql
# Pre-search rate per minute
rate(knowledge_agent_presearch_total[5m]) * 60
```

#### `knowledge_agent_presearch_errors_total`
- **Type**: Counter
- **Description**: Total number of pre-search errors (timeouts, database failures)
- **Use case**: Monitor pre-search reliability

```promql
# Pre-search error rate percentage
100 * rate(knowledge_agent_presearch_errors_total[5m]) / rate(knowledge_agent_presearch_total[5m])
```

#### `knowledge_agent_presearch_latency_seconds`
- **Type**: Histogram
- **Buckets**: 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 3 seconds
- **Description**: Pre-search latency distribution (hard timeout at 3 seconds)
- **Use case**: Monitor search performance and identify slow queries

```promql
# P95 pre-search latency
histogram_quantile(0.95, rate(knowledge_agent_presearch_latency_seconds_bucket[5m]))

# Average pre-search latency
rate(knowledge_agent_presearch_latency_seconds_sum[5m]) / rate(knowledge_agent_presearch_latency_seconds_count[5m])
```

### Process Metrics

#### `knowledge_agent_process_start_time_seconds`
- **Type**: Gauge
- **Description**: Process start time in Unix epoch seconds
- **Use case**: Calculate uptime

```promql
# Uptime in hours
(time() - knowledge_agent_process_start_time_seconds) / 3600
```

---

## Slack Bridge Metrics

### Event Processing

#### `slack_bridge_events_total{event_type}`
- **Type**: Counter
- **Labels**: `event_type` (e.g., "app_mention", "message", etc.)
- **Description**: Total Slack events received by type
- **Use case**: Track Slack activity

```promql
# Events per minute by type
sum by (event_type) (rate(slack_bridge_events_total[5m]) * 60)
```

#### `slack_bridge_event_errors_total`
- **Type**: Counter
- **Description**: Total event processing errors
- **Use case**: Monitor event handling reliability

```promql
# Event error rate
rate(slack_bridge_event_errors_total[5m])
```

### Slack API Calls

#### `slack_bridge_api_calls_total{method}`
- **Type**: Counter
- **Labels**: `method` (e.g., "users.info", "conversations.replies", "chat.postMessage")
- **Description**: Total Slack API calls by method
- **Use case**: Track API usage and rate limits

```promql
# API calls per minute by method
sum by (method) (rate(slack_bridge_api_calls_total[5m]) * 60)
```

#### `slack_bridge_api_errors_total{method}`
- **Type**: Counter
- **Labels**: `method`
- **Description**: Total Slack API errors by method
- **Use case**: Monitor API reliability

```promql
# API error rate by method
sum by (method) (rate(slack_bridge_api_errors_total[5m]))
  / sum by (method) (rate(slack_bridge_api_calls_total[5m]))
```

### Agent Communication

#### `slack_bridge_agent_forwards_total`
- **Type**: Counter
- **Description**: Total requests forwarded to Knowledge Agent
- **Use case**: Track bridge-to-agent traffic

```promql
# Forwards per minute
rate(slack_bridge_agent_forwards_total[5m]) * 60
```

#### `slack_bridge_agent_forward_errors_total`
- **Type**: Counter
- **Description**: Total errors forwarding to Knowledge Agent
- **Use case**: Monitor agent connectivity

```promql
# Forward error rate
100 * rate(slack_bridge_agent_forward_errors_total[5m]) / rate(slack_bridge_agent_forwards_total[5m])
```

---

## Standard Go Metrics

Both services also expose standard Go runtime metrics from Prometheus client:

### Goroutines
```promql
go_goroutines{service="knowledge-agent"}
```

### Memory
```promql
# Heap memory in use (MB)
go_memstats_heap_inuse_bytes / 1024 / 1024

# Total allocated memory (MB)
go_memstats_alloc_bytes / 1024 / 1024

# GC pause time
rate(go_gc_duration_seconds_sum[5m])
```

### Process
```promql
# CPU seconds
rate(process_cpu_seconds_total[5m])

# Open file descriptors
process_open_fds

# Memory usage (MB)
process_resident_memory_bytes / 1024 / 1024
```

---

## Example Queries

### System Health

**Overall success rate**:
```promql
(sum(rate(knowledge_agent_queries_total[5m])) - sum(rate(knowledge_agent_query_errors_total[5m])))
  / sum(rate(knowledge_agent_queries_total[5m])) * 100
```

**Agent availability** (using up metric from Prometheus):
```promql
up{job="knowledge-agent"}
```

### Performance

**Query latency percentiles**:
```promql
# p50
histogram_quantile(0.50, rate(knowledge_agent_query_latency_seconds_bucket[5m]))

# p95
histogram_quantile(0.95, rate(knowledge_agent_query_latency_seconds_bucket[5m]))

# p99
histogram_quantile(0.99, rate(knowledge_agent_query_latency_seconds_bucket[5m]))
```

**Slow queries** (> 2 seconds):
```promql
sum(rate(knowledge_agent_query_latency_seconds_bucket{le="2"}[5m]))
  / sum(rate(knowledge_agent_query_latency_seconds_count[5m]))
```

### Usage Patterns

**Query volume trends**:
```promql
# Queries per hour
sum(rate(knowledge_agent_queries_total[1h]) * 3600)

# Peak queries per minute (max over 1 hour)
max_over_time(rate(knowledge_agent_queries_total[5m])[1h:5m]) * 60
```

**Tool usage by type**:
```promql
# Tool calls by tool name
sum by (tool_name) (rate(knowledge_agent_tool_calls_total[1h]) * 3600)
```

**A2A sub-agent activity**:
```promql
# Sub-agent calls per hour
sum by (sub_agent) (rate(knowledge_agent_a2a_calls_total[1h]) * 3600)
```

### Slack Integration

**Most active Slack event types**:
```promql
topk(5, sum by (event_type) (rate(slack_bridge_events_total[1h])))
```

**Slack API health by method**:
```promql
(sum by (method) (rate(slack_bridge_api_calls_total[5m]))
  - sum by (method) (rate(slack_bridge_api_errors_total[5m])))
  / sum by (method) (rate(slack_bridge_api_calls_total[5m]))
```

---

## Grafana Dashboards

### Dashboard 1: Knowledge Agent Overview

**Panels**:

1. **Query Rate** (Graph):
   ```promql
   sum(rate(knowledge_agent_queries_total[5m])) * 60
   ```

2. **Error Rate** (Stat with threshold):
   ```promql
   100 * sum(rate(knowledge_agent_query_errors_total[5m])) / sum(rate(knowledge_agent_queries_total[5m]))
   ```

3. **Latency Percentiles** (Graph):
   ```promql
   histogram_quantile(0.50, rate(knowledge_agent_query_latency_seconds_bucket[5m]))
   histogram_quantile(0.95, rate(knowledge_agent_query_latency_seconds_bucket[5m]))
   histogram_quantile(0.99, rate(knowledge_agent_query_latency_seconds_bucket[5m]))
   ```

4. **Tool Calls** (Graph):
   ```promql
   sum by (tool_name) (rate(knowledge_agent_tool_calls_total[5m])) * 60
   ```

5. **A2A Sub-Agent Calls** (Graph):
   ```promql
   sum by (sub_agent) (rate(knowledge_agent_a2a_calls_total[5m])) * 60
   ```

6. **Uptime** (Stat):
   ```promql
   (time() - knowledge_agent_process_start_time_seconds) / 3600
   ```

### Dashboard 2: Slack Bridge Monitoring

**Panels**:

1. **Slack Events** (Graph):
   ```promql
   sum by (event_type) (rate(slack_bridge_events_total[5m]) * 60)
   ```

2. **API Call Rate** (Graph):
   ```promql
   sum by (method) (rate(slack_bridge_api_calls_total[5m]) * 60)
   ```

3. **API Error Rate** (Heatmap):
   ```promql
   sum by (method) (rate(slack_bridge_api_errors_total[5m]))
   ```

4. **Agent Forwards** (Graph):
   ```promql
   rate(slack_bridge_agent_forwards_total[5m]) * 60
   rate(slack_bridge_agent_forward_errors_total[5m]) * 60
   ```

5. **Forward Success Rate** (Gauge):
   ```promql
   100 * (1 - sum(rate(slack_bridge_agent_forward_errors_total[5m])) / sum(rate(slack_bridge_agent_forwards_total[5m])))
   ```

---

## Alerting Rules

### Critical Alerts

**High Error Rate**:
```yaml
- alert: HighQueryErrorRate
  expr: |
    100 * rate(knowledge_agent_query_errors_total[5m]) / rate(knowledge_agent_queries_total[5m]) > 10
  for: 5m
  labels:
    severity: critical
  annotations:
    summary: "High query error rate"
    description: "Query error rate is {{ $value | humanizePercentage }} (threshold: 10%)"
```

**Agent Down**:
```yaml
- alert: KnowledgeAgentDown
  expr: up{job="knowledge-agent"} == 0
  for: 1m
  labels:
    severity: critical
  annotations:
    summary: "Knowledge Agent is down"
    description: "Knowledge Agent has been down for more than 1 minute"
```

### Warning Alerts

**High Latency**:
```yaml
- alert: HighQueryLatency
  expr: |
    histogram_quantile(0.95, rate(knowledge_agent_query_latency_seconds_bucket[5m])) > 5
  for: 10m
  labels:
    severity: warning
  annotations:
    summary: "High query latency"
    description: "P95 latency is {{ $value }}s (threshold: 5s)"
```

**Tool Execution Errors**:
```yaml
- alert: ToolExecutionErrors
  expr: |
    sum by (tool_name) (rate(knowledge_agent_tool_calls_total{status="error"}[5m]))
      / sum by (tool_name) (rate(knowledge_agent_tool_calls_total[5m])) > 0.1
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "Tool execution errors for {{ $labels.tool_name }}"
    description: "Error rate: {{ $value | humanizePercentage }} (threshold: 10%)"
```

**Slack API Errors**:
```yaml
- alert: SlackAPIErrors
  expr: |
    sum by (method) (rate(slack_bridge_api_errors_total[5m]))
      / sum by (method) (rate(slack_bridge_api_calls_total[5m])) > 0.05
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "Slack API errors for {{ $labels.method }}"
    description: "Error rate: {{ $value | humanizePercentage }} (threshold: 5%)"
```

---

## See Also

- [ServiceMonitor Setup](../deployments/servicemonitors/README.md)
- [OPERATIONS.md](OPERATIONS.md) - Logging and traceability
- [API_REFERENCE.md](API_REFERENCE.md) - REST API documentation

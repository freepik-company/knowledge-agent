# ServiceMonitor Manifests

These ServiceMonitor manifests configure Prometheus Operator to scrape metrics from Knowledge Agent services.

## Prerequisites

- Prometheus Operator installed in your cluster
- Services deployed with matching labels

## Installation

### Option 1: Apply to cluster-control-system (Recommended)

Copy these manifests to your `cluster-control-system` repository and apply them:

```bash
# Copy to cluster-control-system repo
cp knowledge-agent.yaml /path/to/cluster-control-system/prometheus/servicemonitors/
cp slack-bridge.yaml /path/to/cluster-control-system/prometheus/servicemonitors/

# Apply via your GitOps process (ArgoCD, Flux, etc.)
# Or manually:
kubectl apply -f /path/to/cluster-control-system/prometheus/servicemonitors/
```

### Option 2: Apply directly

```bash
kubectl apply -f knowledge-agent.yaml
kubectl apply -f slack-bridge.yaml
```

## Configuration

### Update Namespace

If your Knowledge Agent is deployed in a namespace other than `default`, update the `namespaceSelector` in both files:

```yaml
spec:
  namespaceSelector:
    matchNames:
      - your-namespace  # Change this
```

### Service Labels

Ensure your Kubernetes Services have the correct labels that match the ServiceMonitor selectors:

**Knowledge Agent Service** (`knowledge-agent.yaml`):
```yaml
apiVersion: v1
kind: Service
metadata:
  name: knowledge-agent
  labels:
    app: knowledge-agent
    component: agent
spec:
  ports:
    - name: http
      port: 8081
      targetPort: 8081
  selector:
    app: knowledge-agent
    component: agent
```

**Slack Bridge Service** (`slack-bridge.yaml`):
```yaml
apiVersion: v1
kind: Service
metadata:
  name: knowledge-agent-slack-bridge
  labels:
    app: knowledge-agent
    component: slack-bridge
spec:
  ports:
    - name: http
      port: 8080
      targetPort: 8080
  selector:
    app: knowledge-agent
    component: slack-bridge
```

## Metrics Exposed

### Knowledge Agent Metrics (`:8081/metrics`)

**Query Metrics:**
- `knowledge_agent_queries_total` - Total queries processed
- `knowledge_agent_query_errors_total` - Total query errors
- `knowledge_agent_query_latency_seconds` - Query latency histogram

**Memory Operations:**
- `knowledge_agent_memory_saves_total` - Total memory saves
- `knowledge_agent_memory_searches_total` - Total memory searches
- `knowledge_agent_memory_errors_total` - Total memory operation errors

**URL Fetching:**
- `knowledge_agent_url_fetches_total` - Total URL fetches
- `knowledge_agent_url_fetch_errors_total` - Total URL fetch errors

**Token Usage:**
- `knowledge_agent_tokens_used_total` - Total LLM tokens used

**Process:**
- `knowledge_agent_process_start_time_seconds` - Process start time (for uptime)

### Slack Bridge Metrics (`:8080/metrics`)

**Event Processing:**
- `slack_bridge_events_total{event_type}` - Slack events received by type
- `slack_bridge_event_errors_total` - Event processing errors

**Slack API Calls:**
- `slack_bridge_api_calls_total{method}` - Slack API calls by method
- `slack_bridge_api_errors_total{method}` - Slack API errors by method

**Agent Communication:**
- `slack_bridge_agent_forwards_total` - Requests forwarded to agent
- `slack_bridge_agent_forward_errors_total` - Forward errors

**Standard Go Metrics** (both services):
- `go_goroutines` - Number of goroutines
- `go_memstats_*` - Memory statistics
- `process_*` - Process metrics (CPU, memory, file descriptors)

## Verification

### Check ServiceMonitor Status

```bash
# Check if ServiceMonitors are created
kubectl get servicemonitors -n cluster-control-system

# Describe for details
kubectl describe servicemonitor knowledge-agent -n cluster-control-system
kubectl describe servicemonitor knowledge-agent-slack-bridge -n cluster-control-system
```

### Check Prometheus Targets

Access Prometheus UI and go to **Status → Targets** to verify:
- `serviceMonitor/cluster-control-system/knowledge-agent/0` - Should be UP
- `serviceMonitor/cluster-control-system/knowledge-agent-slack-bridge/0` - Should be UP

### Query Metrics

In Prometheus UI, try these queries:

```promql
# Query rate over 5 minutes
rate(knowledge_agent_queries_total[5m])

# Query error rate
rate(knowledge_agent_query_errors_total[5m]) / rate(knowledge_agent_queries_total[5m])

# 95th percentile latency
histogram_quantile(0.95, rate(knowledge_agent_query_latency_seconds_bucket[5m]))

# Slack events by type
rate(slack_bridge_events_total[5m])

# Agent forward error rate
rate(slack_bridge_agent_forward_errors_total[5m]) / rate(slack_bridge_agent_forwards_total[5m])
```

## Troubleshooting

### ServiceMonitor Not Picking Up Targets

1. **Check selector labels match**:
   ```bash
   kubectl get svc -l app=knowledge-agent --show-labels
   ```

2. **Check Prometheus Operator logs**:
   ```bash
   kubectl logs -n monitoring -l app.kubernetes.io/name=prometheus-operator
   ```

3. **Verify Prometheus ServiceMonitor selector**:
   ```bash
   kubectl get prometheus -n monitoring -o yaml | grep serviceMonitorSelector
   ```

### Metrics Not Appearing

1. **Check endpoint is accessible**:
   ```bash
   kubectl port-forward svc/knowledge-agent 8081:8081
   curl http://localhost:8081/metrics
   ```

2. **Check Prometheus scrape config**:
   ```bash
   kubectl get secret prometheus-kube-prometheus-prometheus -n monitoring -o yaml
   ```

### Permission Issues

Ensure Prometheus has RBAC permissions to discover services and pods:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: prometheus
rules:
- apiGroups: [""]
  resources:
  - nodes
  - nodes/metrics
  - services
  - endpoints
  - pods
  verbs: ["get", "list", "watch"]
```

## Example Dashboards

### Grafana Dashboard Queries

**Query Success Rate:**
```promql
sum(rate(knowledge_agent_queries_total[5m])) - sum(rate(knowledge_agent_query_errors_total[5m]))
```

**Average Query Latency:**
```promql
rate(knowledge_agent_query_latency_seconds_sum[5m]) / rate(knowledge_agent_query_latency_seconds_count[5m])
```

**Memory Operations:**
```promql
sum by (operation) (rate(knowledge_agent_memory_saves_total[5m]))
sum by (operation) (rate(knowledge_agent_memory_searches_total[5m]))
```

**Slack Bridge Health:**
```promql
rate(slack_bridge_events_total[5m])
rate(slack_bridge_api_errors_total[5m])
rate(slack_bridge_agent_forward_errors_total[5m])
```

## See Also

- [Prometheus Operator Documentation](https://prometheus-operator.dev/)
- [ServiceMonitor Specification](https://prometheus-operator.dev/docs/operator/api/#monitoring.coreos.com/v1.ServiceMonitor)
- [Prometheus Relabeling](https://prometheus.io/docs/prometheus/latest/configuration/configuration/#relabel_config)

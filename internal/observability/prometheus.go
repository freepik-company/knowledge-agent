package observability

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Prometheus metrics - registered globally
var (
	// Query metrics
	queryTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "knowledge_agent_queries_total",
		Help: "Total number of queries processed",
	})

	queryErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "knowledge_agent_query_errors_total",
		Help: "Total number of query errors",
	})

	queryLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "knowledge_agent_query_latency_seconds",
		Help:    "Query latency in seconds",
		Buckets: prometheus.DefBuckets, // 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10
	})

	// Process start time for uptime calculation
	processStartTime = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "knowledge_agent_process_start_time_seconds",
		Help: "Process start time in Unix epoch seconds",
	})

	// Slack Bridge metrics
	slackEventsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "slack_bridge_events_total",
		Help: "Total number of Slack events received by type",
	}, []string{"event_type"})

	slackEventsErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "slack_bridge_event_errors_total",
		Help: "Total number of Slack event processing errors",
	})

	slackAPICallsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "slack_bridge_api_calls_total",
		Help: "Total number of Slack API calls by method",
	}, []string{"method"})

	slackAPIErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "slack_bridge_api_errors_total",
		Help: "Total number of Slack API errors by method",
	}, []string{"method"})

	agentForwardsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "slack_bridge_agent_forwards_total",
		Help: "Total number of requests forwarded to Knowledge Agent",
	})

	agentForwardErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "slack_bridge_agent_forward_errors_total",
		Help: "Total number of errors forwarding to Knowledge Agent",
	})

	// Tool execution metrics
	toolLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "knowledge_agent_tool_latency_seconds",
		Help:    "Tool execution latency by tool name",
		Buckets: []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
	}, []string{"tool_name"})

	toolCalls = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "knowledge_agent_tool_calls_total",
		Help: "Total tool calls by tool name and status",
	}, []string{"tool_name", "status"})

	// A2A sub-agent metrics
	a2aCallsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "knowledge_agent_a2a_calls_total",
		Help: "Total A2A calls by sub-agent and status",
	}, []string{"sub_agent", "status"})

	a2aLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "knowledge_agent_a2a_latency_seconds",
		Help:    "A2A sub-agent call latency",
		Buckets: []float64{0.5, 1, 2.5, 5, 10, 30, 60},
	}, []string{"sub_agent"})

	// Ingest metrics
	ingestTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "knowledge_agent_ingest_total",
		Help: "Total number of ingest operations",
	})

	ingestErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "knowledge_agent_ingest_errors_total",
		Help: "Total number of ingest errors",
	})

	ingestLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "knowledge_agent_ingest_latency_seconds",
		Help:    "Ingest latency in seconds",
		Buckets: prometheus.DefBuckets,
	})

	// Pre-search metrics (programmatic search before LLM loop)
	preSearchTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "knowledge_agent_presearch_total",
		Help: "Total number of pre-search memory operations",
	})

	preSearchErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "knowledge_agent_presearch_errors_total",
		Help: "Total number of pre-search errors",
	})

	preSearchLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "knowledge_agent_presearch_latency_seconds",
		Help:    "Pre-search memory latency in seconds",
		Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 3}, // Fast operations, 3s is timeout
	})
)

// init registers the process start time
func init() {
	processStartTime.SetToCurrentTime()
}

// recordQueryPrometheus records query metrics to Prometheus
func (m *Metrics) recordQueryPrometheus(duration time.Duration, err error) {
	queryTotal.Inc()
	queryLatency.Observe(duration.Seconds())

	if err != nil {
		queryErrors.Inc()
	}
}

// recordToolCallPrometheus records tool call metrics to Prometheus
func recordToolCallPrometheus(toolName string, duration time.Duration, success bool) {
	toolLatency.WithLabelValues(toolName).Observe(duration.Seconds())
	status := "success"
	if !success {
		status = "error"
	}
	toolCalls.WithLabelValues(toolName, status).Inc()
}

// recordA2ACallPrometheus records A2A sub-agent call metrics to Prometheus
func recordA2ACallPrometheus(subAgent string, duration time.Duration, success bool) {
	a2aLatency.WithLabelValues(subAgent).Observe(duration.Seconds())
	status := "success"
	if !success {
		status = "error"
	}
	a2aCallsTotal.WithLabelValues(subAgent, status).Inc()
}

// recordIngestPrometheus records ingest metrics to Prometheus
func (m *Metrics) recordIngestPrometheus(duration time.Duration, err error) {
	ingestTotal.Inc()
	ingestLatency.Observe(duration.Seconds())

	if err != nil {
		ingestErrors.Inc()
	}
}

// recordPreSearchPrometheus records pre-search metrics to Prometheus
func (m *Metrics) recordPreSearchPrometheus(duration time.Duration, success bool) {
	preSearchTotal.Inc()
	preSearchLatency.Observe(duration.Seconds())

	if !success {
		preSearchErrors.Inc()
	}
}

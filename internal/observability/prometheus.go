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

	// Memory operation metrics
	memorySaves = promauto.NewCounter(prometheus.CounterOpts{
		Name: "knowledge_agent_memory_saves_total",
		Help: "Total number of memory save operations",
	})

	memorySearches = promauto.NewCounter(prometheus.CounterOpts{
		Name: "knowledge_agent_memory_searches_total",
		Help: "Total number of memory search operations",
	})

	memoryErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "knowledge_agent_memory_errors_total",
		Help: "Total number of memory operation errors",
	})

	// URL fetch metrics
	urlFetches = promauto.NewCounter(prometheus.CounterOpts{
		Name: "knowledge_agent_url_fetches_total",
		Help: "Total number of URL fetch operations",
	})

	urlFetchErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "knowledge_agent_url_fetch_errors_total",
		Help: "Total number of URL fetch errors",
	})

	// Token usage
	tokensUsed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "knowledge_agent_tokens_used_total",
		Help: "Total number of LLM tokens used",
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

// recordMemorySavePrometheus records memory save to Prometheus
func (m *Metrics) recordMemorySavePrometheus(success bool) {
	memorySaves.Inc()
	if !success {
		memoryErrors.Inc()
	}
}

// recordMemorySearchPrometheus records memory search to Prometheus
func (m *Metrics) recordMemorySearchPrometheus(success bool) {
	memorySearches.Inc()
	if !success {
		memoryErrors.Inc()
	}
}

// recordURLFetchPrometheus records URL fetch to Prometheus
func (m *Metrics) recordURLFetchPrometheus(success bool) {
	urlFetches.Inc()
	if !success {
		urlFetchErrors.Inc()
	}
}

// recordTokensPrometheus records tokens used to Prometheus
func (m *Metrics) recordTokensPrometheus(tokens int64) {
	tokensUsed.Add(float64(tokens))
}

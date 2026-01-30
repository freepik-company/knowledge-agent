package observability

import (
	"sync"
	"sync/atomic"
	"time"
)

// Metrics holds application metrics
type Metrics struct {
	// Query metrics
	queryCount        atomic.Int64
	queryErrorCount   atomic.Int64
	queryLatencySum   atomic.Int64 // in milliseconds
	queryLatencyCount atomic.Int64

	// Memory operation metrics
	memorySaveCount   atomic.Int64
	memorySearchCount atomic.Int64
	memoryErrorCount  atomic.Int64

	// URL fetch metrics
	urlFetchCount      atomic.Int64
	urlFetchErrorCount atomic.Int64

	// Token usage (if available from LLM responses)
	tokensUsed atomic.Int64

	// Uptime
	startTime time.Time
	mu        sync.RWMutex
}

// global singleton instance
var globalMetrics *Metrics
var metricsOnce sync.Once

// GetMetrics returns the global metrics instance
func GetMetrics() *Metrics {
	metricsOnce.Do(func() {
		globalMetrics = &Metrics{
			startTime: time.Now(),
		}
	})
	return globalMetrics
}

// RecordQuery records a query execution
func (m *Metrics) RecordQuery(duration time.Duration, err error) {
	m.queryCount.Add(1)
	m.queryLatencySum.Add(duration.Milliseconds())
	m.queryLatencyCount.Add(1)

	if err != nil {
		m.queryErrorCount.Add(1)
	}

	// Record to Prometheus
	m.recordQueryPrometheus(duration, err)
}

// RecordMemorySave records a memory save operation
func (m *Metrics) RecordMemorySave(success bool) {
	m.memorySaveCount.Add(1)
	if !success {
		m.memoryErrorCount.Add(1)
	}

	// Record to Prometheus
	m.recordMemorySavePrometheus(success)
}

// RecordMemorySearch records a memory search operation
func (m *Metrics) RecordMemorySearch(success bool) {
	m.memorySearchCount.Add(1)
	if !success {
		m.memoryErrorCount.Add(1)
	}

	// Record to Prometheus
	m.recordMemorySearchPrometheus(success)
}

// RecordURLFetch records a URL fetch operation
func (m *Metrics) RecordURLFetch(success bool) {
	m.urlFetchCount.Add(1)
	if !success {
		m.urlFetchErrorCount.Add(1)
	}

	// Record to Prometheus
	m.recordURLFetchPrometheus(success)
}

// RecordTokensUsed records tokens used by the LLM
func (m *Metrics) RecordTokensUsed(tokens int64) {
	m.tokensUsed.Add(tokens)

	// Record to Prometheus
	m.recordTokensPrometheus(tokens)
}

// GetStats returns a snapshot of current metrics
func (m *Metrics) GetStats() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Calculate average query latency
	var avgLatency float64
	if count := m.queryLatencyCount.Load(); count > 0 {
		avgLatency = float64(m.queryLatencySum.Load()) / float64(count)
	}

	// Calculate query error rate
	var queryErrorRate float64
	if count := m.queryCount.Load(); count > 0 {
		queryErrorRate = float64(m.queryErrorCount.Load()) / float64(count) * 100
	}

	// Calculate memory error rate
	var memoryErrorRate float64
	totalMemoryOps := m.memorySaveCount.Load() + m.memorySearchCount.Load()
	if totalMemoryOps > 0 {
		memoryErrorRate = float64(m.memoryErrorCount.Load()) / float64(totalMemoryOps) * 100
	}

	// Calculate uptime
	uptime := time.Since(m.startTime)

	return map[string]any{
		"uptime_seconds": uptime.Seconds(),
		"queries": map[string]any{
			"total":              m.queryCount.Load(),
			"errors":             m.queryErrorCount.Load(),
			"error_rate_percent": queryErrorRate,
			"avg_latency_ms":     avgLatency,
		},
		"memory": map[string]any{
			"saves":              m.memorySaveCount.Load(),
			"searches":           m.memorySearchCount.Load(),
			"errors":             m.memoryErrorCount.Load(),
			"error_rate_percent": memoryErrorRate,
		},
		"url_fetch": map[string]any{
			"total":  m.urlFetchCount.Load(),
			"errors": m.urlFetchErrorCount.Load(),
		},
		"tokens_used": m.tokensUsed.Load(),
	}
}

// Reset resets all metrics (useful for testing)
func (m *Metrics) Reset() {
	m.queryCount.Store(0)
	m.queryErrorCount.Store(0)
	m.queryLatencySum.Store(0)
	m.queryLatencyCount.Store(0)
	m.memorySaveCount.Store(0)
	m.memorySearchCount.Store(0)
	m.memoryErrorCount.Store(0)
	m.urlFetchCount.Store(0)
	m.urlFetchErrorCount.Store(0)
	m.tokensUsed.Store(0)

	m.mu.Lock()
	m.startTime = time.Now()
	m.mu.Unlock()
}

// Slack Bridge metrics (no internal storage, only Prometheus)

// RecordSlackEvent records a Slack event received
func RecordSlackEvent(eventType string, success bool) {
	slackEventsTotal.WithLabelValues(eventType).Inc()
	if !success {
		slackEventsErrors.Inc()
	}
}

// RecordSlackAPICall records a Slack API call
func RecordSlackAPICall(method string, success bool) {
	slackAPICallsTotal.WithLabelValues(method).Inc()
	if !success {
		slackAPIErrors.WithLabelValues(method).Inc()
	}
}

// RecordAgentForward records a request forwarded to the agent
func RecordAgentForward(success bool) {
	agentForwardsTotal.Inc()
	if !success {
		agentForwardErrors.Inc()
	}
}

package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"knowledge-agent/internal/logger"

	"github.com/redis/go-redis/v9"
)

// ReadinessState tracks whether the server is ready to accept traffic
type ReadinessState struct {
	ready atomic.Bool
}

// NewReadinessState creates a new readiness state (starts as not ready)
func NewReadinessState() *ReadinessState {
	return &ReadinessState{}
}

// SetReady marks the server as ready to accept traffic
func (r *ReadinessState) SetReady() {
	r.ready.Store(true)
}

// SetNotReady marks the server as not ready (shutting down)
func (r *ReadinessState) SetNotReady() {
	r.ready.Store(false)
}

// IsReady returns whether the server is ready
func (r *ReadinessState) IsReady() bool {
	return r.ready.Load()
}

// ReadinessHandler returns an HTTP handler for readiness checks
// Returns 200 if ready, 503 if not ready (shutting down or starting up)
func ReadinessHandler(state *ReadinessState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		response := map[string]any{
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}

		if state.IsReady() {
			response["ready"] = true
			response["status"] = "accepting_traffic"
			w.WriteHeader(http.StatusOK)
		} else {
			response["ready"] = false
			response["status"] = "not_accepting_traffic"
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		json.NewEncoder(w).Encode(response)
	}
}

// LivenessHandler returns an HTTP handler for liveness checks
// Always returns 200 if the process is running (not deadlocked)
func LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"alive":     true,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	}
}

// HealthChecker interface for checking health of a dependency
type HealthChecker interface {
	Check(ctx context.Context) error
	Name() string
}

// PostgresHealthChecker checks PostgreSQL database health
type PostgresHealthChecker struct {
	db *sql.DB
}

func NewPostgresHealthChecker(db *sql.DB) *PostgresHealthChecker {
	return &PostgresHealthChecker{db: db}
}

func (p *PostgresHealthChecker) Name() string {
	return "postgres"
}

func (p *PostgresHealthChecker) Check(ctx context.Context) error {
	if p.db == nil {
		return fmt.Errorf("database connection is nil")
	}
	return p.db.PingContext(ctx)
}

// RedisHealthChecker checks Redis health
type RedisHealthChecker struct {
	client *redis.Client
}

func NewRedisHealthChecker(client *redis.Client) *RedisHealthChecker {
	return &RedisHealthChecker{client: client}
}

func (r *RedisHealthChecker) Name() string {
	return "redis"
}

func (r *RedisHealthChecker) Check(ctx context.Context) error {
	if r.client == nil {
		return fmt.Errorf("redis client is nil")
	}
	return r.client.Ping(ctx).Err()
}

// OllamaHealthChecker checks Ollama API health
type OllamaHealthChecker struct {
	baseURL string
}

func NewOllamaHealthChecker(baseURL string) *OllamaHealthChecker {
	return &OllamaHealthChecker{baseURL: baseURL}
}

func (o *OllamaHealthChecker) Name() string {
	return "ollama"
}

func (o *OllamaHealthChecker) Check(ctx context.Context) error {
	client := &http.Client{
		Timeout: 3 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", o.baseURL+"/api/tags", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

// HealthCheckResponse represents the response from a health check endpoint
type HealthCheckResponse struct {
	Status       string                      `json:"status"` // "healthy", "degraded", or "unhealthy"
	Service      string                      `json:"service"`
	Mode         string                      `json:"mode,omitempty"`
	Dependencies map[string]DependencyStatus `json:"dependencies,omitempty"`
	Timestamp    string                      `json:"timestamp"`
}

// DependencyStatus represents the status of a single dependency
type DependencyStatus struct {
	Status string `json:"status"` // "healthy" or "unhealthy"
	Error  string `json:"error,omitempty"`
}

// HealthCheckHandler returns an HTTP handler for health checks
// If checkers are provided, it performs comprehensive health checks
// Otherwise, it returns a simple "healthy" response
func HealthCheckHandler(serviceName, mode string, checkers ...HealthChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log := logger.Get()

		response := HealthCheckResponse{
			Status:    "healthy",
			Service:   serviceName,
			Mode:      mode,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}

		// If no checkers provided, return simple healthy response
		if len(checkers) == 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			if err := json.NewEncoder(w).Encode(response); err != nil {
				log.Errorw("Failed to encode health response", "error", err)
			}
			return
		}

		// Check all dependencies
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		dependencies := make(map[string]DependencyStatus)
		hasUnhealthy := false
		hasDegraded := false

		for _, checker := range checkers {
			err := checker.Check(ctx)
			if err != nil {
				dependencies[checker.Name()] = DependencyStatus{
					Status: "unhealthy",
					Error:  err.Error(),
				}
				hasUnhealthy = true
				log.Warnw("Dependency unhealthy",
					"dependency", checker.Name(),
					"error", err,
				)
			} else {
				dependencies[checker.Name()] = DependencyStatus{
					Status: "healthy",
				}
			}
		}

		response.Dependencies = dependencies

		// Determine overall status
		statusCode := http.StatusOK
		if hasUnhealthy {
			// If any critical dependency is down, mark as degraded
			// (we could still serve some requests)
			response.Status = "degraded"
			hasDegraded = true
			statusCode = http.StatusOK // Still return 200, but status is "degraded"
		}

		// Return 503 only if service is completely unavailable
		// (currently we don't have this logic, but could add it)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Errorw("Failed to encode health response", "error", err)
		}

		if hasDegraded {
			log.Warnw("Health check shows degraded status",
				"service", serviceName,
				"dependencies", dependencies,
			)
		}
	}
}

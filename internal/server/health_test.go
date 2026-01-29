package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockHealthChecker implements HealthChecker for testing
type mockHealthChecker struct {
	name string
	err  error
}

func (m *mockHealthChecker) Name() string {
	return m.name
}

func (m *mockHealthChecker) Check(ctx context.Context) error {
	return m.err
}

func TestHealthCheckHandler_NoCheckers(t *testing.T) {
	handler := HealthCheckHandler("test-service", "test-mode")

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}

	var response HealthCheckResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response.Status != "healthy" {
		t.Errorf("got status %q, want %q", response.Status, "healthy")
	}
	if response.Service != "test-service" {
		t.Errorf("got service %q, want %q", response.Service, "test-service")
	}
	if response.Mode != "test-mode" {
		t.Errorf("got mode %q, want %q", response.Mode, "test-mode")
	}
	if response.Timestamp == "" {
		t.Error("timestamp should not be empty")
	}
}

func TestHealthCheckHandler_AllHealthy(t *testing.T) {
	checkers := []HealthChecker{
		&mockHealthChecker{name: "db", err: nil},
		&mockHealthChecker{name: "cache", err: nil},
	}

	handler := HealthCheckHandler("test-service", "", checkers...)

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}

	var response HealthCheckResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response.Status != "healthy" {
		t.Errorf("got status %q, want %q", response.Status, "healthy")
	}

	if len(response.Dependencies) != 2 {
		t.Errorf("got %d dependencies, want 2", len(response.Dependencies))
	}

	for name, dep := range response.Dependencies {
		if dep.Status != "healthy" {
			t.Errorf("dependency %q: got status %q, want %q", name, dep.Status, "healthy")
		}
	}
}

func TestHealthCheckHandler_Degraded(t *testing.T) {
	checkers := []HealthChecker{
		&mockHealthChecker{name: "db", err: nil},
		&mockHealthChecker{name: "cache", err: errors.New("connection refused")},
	}

	handler := HealthCheckHandler("test-service", "", checkers...)

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Still returns 200 for degraded (service can still handle some requests)
	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}

	var response HealthCheckResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response.Status != "degraded" {
		t.Errorf("got status %q, want %q", response.Status, "degraded")
	}

	// Check healthy dependency
	dbDep := response.Dependencies["db"]
	if dbDep.Status != "healthy" {
		t.Errorf("db dependency: got status %q, want %q", dbDep.Status, "healthy")
	}

	// Check unhealthy dependency
	cacheDep := response.Dependencies["cache"]
	if cacheDep.Status != "unhealthy" {
		t.Errorf("cache dependency: got status %q, want %q", cacheDep.Status, "unhealthy")
	}
	if cacheDep.Error != "connection refused" {
		t.Errorf("cache dependency: got error %q, want %q", cacheDep.Error, "connection refused")
	}
}

func TestHealthCheckHandler_ContentType(t *testing.T) {
	handler := HealthCheckHandler("test-service", "")

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("got Content-Type %q, want %q", contentType, "application/json")
	}
}

func TestPostgresHealthChecker(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		checker := NewPostgresHealthChecker(nil)

		if checker.Name() != "postgres" {
			t.Errorf("got name %q, want %q", checker.Name(), "postgres")
		}

		err := checker.Check(context.Background())
		if err == nil {
			t.Error("expected error for nil db")
		}
	})
}

func TestRedisHealthChecker(t *testing.T) {
	t.Run("nil client", func(t *testing.T) {
		checker := NewRedisHealthChecker(nil)

		if checker.Name() != "redis" {
			t.Errorf("got name %q, want %q", checker.Name(), "redis")
		}

		err := checker.Check(context.Background())
		if err == nil {
			t.Error("expected error for nil client")
		}
	})
}

func TestOllamaHealthChecker(t *testing.T) {
	t.Run("name", func(t *testing.T) {
		checker := NewOllamaHealthChecker("http://localhost:11434")

		if checker.Name() != "ollama" {
			t.Errorf("got name %q, want %q", checker.Name(), "ollama")
		}
	})

	t.Run("invalid URL", func(t *testing.T) {
		checker := NewOllamaHealthChecker("not-a-valid-url")

		err := checker.Check(context.Background())
		if err == nil {
			t.Error("expected error for invalid URL")
		}
	})

	t.Run("connection refused", func(t *testing.T) {
		// Use a port that's unlikely to be in use
		checker := NewOllamaHealthChecker("http://localhost:59999")

		err := checker.Check(context.Background())
		if err == nil {
			t.Error("expected error for connection refused")
		}
	})
}

func TestHealthCheckResponse_JSON(t *testing.T) {
	response := HealthCheckResponse{
		Status:    "healthy",
		Service:   "test",
		Mode:      "dev",
		Timestamp: "2024-01-01T00:00:00Z",
		Dependencies: map[string]DependencyStatus{
			"db": {Status: "healthy"},
		},
	}

	data, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded HealthCheckResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Status != response.Status {
		t.Errorf("status mismatch: got %q, want %q", decoded.Status, response.Status)
	}
	if decoded.Service != response.Service {
		t.Errorf("service mismatch: got %q, want %q", decoded.Service, response.Service)
	}
	if decoded.Mode != response.Mode {
		t.Errorf("mode mismatch: got %q, want %q", decoded.Mode, response.Mode)
	}
}

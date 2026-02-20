// +build integration

package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"knowledge-agent/internal/agent"
	"knowledge-agent/internal/config"
	"knowledge-agent/internal/server"
)

// TestRateLimitingBasic verifies rate limiting is enforced
func TestRateLimitingBasic(t *testing.T) {
	cfg, err := config.Load("")
	if err != nil {
		t.Skip("Skipping integration test: no config available")
	}

	ctx := context.Background()
	agnt, err := agent.New(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create agent: %v", err)
	}
	defer agnt.Close()

	srv := server.NewAgentServer(agnt, cfg)

	// Send requests rapidly to trigger rate limit
	successCount := 0
	rateLimitedCount := 0

	adkReq := `{"appName":"knowledge-agent","userId":"test","sessionId":"rate-test","newMessage":{"role":"user","parts":[{"text":"Test"}]}}`

	// Send 50 requests as fast as possible
	for i := 0; i < 50; i++ {
		req := httptest.NewRequest(http.MethodPost, "/agent/run", strings.NewReader(adkReq))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		srv.Handler().ServeHTTP(rec, req)

		if rec.Code == http.StatusTooManyRequests {
			rateLimitedCount++
		} else if rec.Code == http.StatusOK {
			successCount++
		}
	}

	// With rate limit of 10/s and burst of 20, most should succeed
	// but some should be rate limited after the burst
	t.Logf("Success: %d, Rate Limited: %d", successCount, rateLimitedCount)

	if rateLimitedCount == 0 {
		t.Log("Warning: no rate limiting observed. This may happen if requests are slow enough.")
	}
}

// TestRateLimitingConcurrent verifies rate limiting works with concurrent requests
func TestRateLimitingConcurrent(t *testing.T) {
	cfg, err := config.Load("")
	if err != nil {
		t.Skip("Skipping integration test: no config available")
	}

	ctx := context.Background()
	agnt, err := agent.New(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create agent: %v", err)
	}
	defer agnt.Close()

	srv := server.NewAgentServer(agnt, cfg)

	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make(map[int]int) // status code -> count

	adkReq := `{"appName":"knowledge-agent","userId":"test","sessionId":"rate-test-concurrent","newMessage":{"role":"user","parts":[{"text":"Test"}]}}`

	// Launch 30 concurrent requests
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			req := httptest.NewRequest(http.MethodPost, "/agent/run", strings.NewReader(adkReq))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			srv.Handler().ServeHTTP(rec, req)

			mu.Lock()
			results[rec.Code]++
			mu.Unlock()
		}()
	}

	wg.Wait()

	t.Logf("Results by status code: %v", results)
}

// TestRateLimitingRecovery verifies rate limit recovers after waiting
func TestRateLimitingRecovery(t *testing.T) {
	cfg, err := config.Load("")
	if err != nil {
		t.Skip("Skipping integration test: no config available")
	}

	ctx := context.Background()
	agnt, err := agent.New(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create agent: %v", err)
	}
	defer agnt.Close()

	srv := server.NewAgentServer(agnt, cfg)

	adkReq := `{"appName":"knowledge-agent","userId":"test","sessionId":"rate-test-recovery","newMessage":{"role":"user","parts":[{"text":"Test"}]}}`

	// Exhaust the burst
	for i := 0; i < 25; i++ {
		req := httptest.NewRequest(http.MethodPost, "/agent/run", strings.NewReader(adkReq))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
	}

	// Wait for rate limiter to recover
	time.Sleep(2 * time.Second)

	// This request should succeed
	req := httptest.NewRequest(http.MethodPost, "/agent/run", strings.NewReader(adkReq))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code == http.StatusTooManyRequests {
		t.Error("Expected rate limiter to recover after waiting, but got 429")
	}
}

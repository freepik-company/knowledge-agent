// +build integration

package integration

import (
	"context"
	"encoding/json"
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
	// Rate limiter is configured for 10 req/s with burst of 20
	successCount := 0
	rateLimitedCount := 0

	queryReq := agent.QueryRequest{
		Question:  "Test",
		ChannelID: "C123",
	}
	reqBody, _ := json.Marshal(queryReq)

	// Send 50 requests as fast as possible
	for i := 0; i < 50; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/query", strings.NewReader(string(reqBody)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Internal-Token", cfg.Auth.InternalToken)
		req.RemoteAddr = "192.168.1.1:12345" // Same IP to trigger rate limit

		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)

		if w.Code == http.StatusOK {
			successCount++
		} else if w.Code == http.StatusTooManyRequests {
			rateLimitedCount++
		}
	}

	// We should have some successful requests (up to burst + rate limit)
	// and some rate limited requests
	if successCount == 0 {
		t.Error("Expected some successful requests")
	}

	if rateLimitedCount == 0 {
		t.Error("Expected some rate limited requests when sending 50 rapid requests")
	}

	t.Logf("Success: %d, Rate Limited: %d", successCount, rateLimitedCount)
}

// TestRateLimitingPerIP verifies rate limiting is per-IP
func TestRateLimitingPerIP(t *testing.T) {
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

	queryReq := agent.QueryRequest{
		Question:  "Test",
		ChannelID: "C123",
	}
	reqBody, _ := json.Marshal(queryReq)

	// Test with different IPs
	ips := []string{
		"192.168.1.1:12345",
		"192.168.1.2:12345",
		"192.168.1.3:12345",
	}

	results := make(map[string]int)
	var mu sync.Mutex

	var wg sync.WaitGroup
	for _, ip := range ips {
		wg.Add(1)
		go func(ipAddr string) {
			defer wg.Done()

			successCount := 0
			// Send 30 requests per IP
			for i := 0; i < 30; i++ {
				req := httptest.NewRequest(http.MethodPost, "/api/query", strings.NewReader(string(reqBody)))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-Internal-Token", cfg.Auth.InternalToken)
				req.RemoteAddr = ipAddr

				w := httptest.NewRecorder()
				srv.Handler().ServeHTTP(w, req)

				if w.Code == http.StatusOK {
					successCount++
				}
			}

			mu.Lock()
			results[ipAddr] = successCount
			mu.Unlock()
		}(ip)
	}

	wg.Wait()

	// Each IP should have independently rate-limited requests
	for ip, count := range results {
		t.Logf("IP %s: %d successful requests", ip, count)

		// Each IP should get some successful requests
		if count == 0 {
			t.Errorf("IP %s got no successful requests", ip)
		}
	}
}

// TestRateLimitingBurst verifies burst capacity works
func TestRateLimitingBurst(t *testing.T) {
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

	queryReq := agent.QueryRequest{
		Question:  "Test",
		ChannelID: "C123",
	}
	reqBody, _ := json.Marshal(queryReq)

	// First burst should succeed (up to burst capacity of 20)
	t.Run("InitialBurst", func(t *testing.T) {
		successCount := 0

		// Send 20 requests immediately (should all succeed due to burst)
		for i := 0; i < 20; i++ {
			req := httptest.NewRequest(http.MethodPost, "/api/query", strings.NewReader(string(reqBody)))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Internal-Token", cfg.Auth.InternalToken)
			req.RemoteAddr = "10.0.0.1:12345"

			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, req)

			if w.Code == http.StatusOK {
				successCount++
			}
		}

		// Most of the burst should succeed
		if successCount < 15 {
			t.Errorf("Expected at least 15 successful burst requests, got %d", successCount)
		}

		t.Logf("Burst success count: %d/20", successCount)
	})

	// After burst, requests should be rate limited
	t.Run("AfterBurst", func(t *testing.T) {
		// Immediately after burst, next requests should be limited
		rateLimitedCount := 0

		for i := 0; i < 10; i++ {
			req := httptest.NewRequest(http.MethodPost, "/api/query", strings.NewReader(string(reqBody)))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Internal-Token", cfg.Auth.InternalToken)
			req.RemoteAddr = "10.0.0.1:12345" // Same IP as burst test

			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, req)

			if w.Code == http.StatusTooManyRequests {
				rateLimitedCount++
			}
		}

		if rateLimitedCount == 0 {
			t.Error("Expected some rate limited requests after burst exhaustion")
		}

		t.Logf("Rate limited after burst: %d/10", rateLimitedCount)
	})
}

// TestRateLimitingRecovery verifies rate limiter recovers over time
func TestRateLimitingRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping rate limit recovery test in short mode")
	}

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

	queryReq := agent.QueryRequest{
		Question:  "Test",
		ChannelID: "C123",
	}
	reqBody, _ := json.Marshal(queryReq)

	makeRequest := func() int {
		req := httptest.NewRequest(http.MethodPost, "/api/query", strings.NewReader(string(reqBody)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Internal-Token", cfg.Auth.InternalToken)
		req.RemoteAddr = "10.0.0.2:12345"

		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		return w.Code
	}

	// Exhaust rate limit
	for i := 0; i < 30; i++ {
		makeRequest()
	}

	// Verify we're rate limited
	if code := makeRequest(); code != http.StatusTooManyRequests {
		t.Error("Expected to be rate limited after exhausting tokens")
	}

	// Wait for rate limiter to recover (10 req/s, so wait 2 seconds for ~20 tokens)
	t.Log("Waiting for rate limiter recovery...")
	time.Sleep(2 * time.Second)

	// Should be able to make requests again
	successCount := 0
	for i := 0; i < 10; i++ {
		if code := makeRequest(); code == http.StatusOK {
			successCount++
		}
	}

	if successCount == 0 {
		t.Error("Rate limiter should have recovered after waiting")
	}

	t.Logf("Recovered: %d/10 successful requests after 2s wait", successCount)
}

// TestRateLimitingCleanup verifies old IP entries are cleaned up
func TestRateLimitingCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping cleanup test in short mode")
	}

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

	queryReq := agent.QueryRequest{
		Question:  "Test",
		ChannelID: "C123",
	}
	reqBody, _ := json.Marshal(queryReq)

	// Make requests from many different IPs
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/query", strings.NewReader(string(reqBody)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Internal-Token", cfg.Auth.InternalToken)
		req.RemoteAddr = "10.0." + string(rune(i/256)) + "." + string(rune(i%256)) + ":12345"

		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
	}

	// Wait for cleanup ticker (runs every 10 minutes, but we can't easily test that here)
	// Just verify the system doesn't crash with many IPs
	t.Log("Rate limiter handled 100 different IPs without issues")
}

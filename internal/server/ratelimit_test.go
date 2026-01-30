package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/time/rate"
)

func TestParseTrustedProxies(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		wantLen int
		wantIPs []string // IPs that should be trusted
		wantNot []string // IPs that should NOT be trusted
	}{
		{
			name:    "empty list",
			input:   []string{},
			wantLen: 0,
		},
		{
			name:    "single IPv4",
			input:   []string{"10.0.0.1"},
			wantLen: 1,
			wantIPs: []string{"10.0.0.1"},
			wantNot: []string{"10.0.0.2", "192.168.1.1"},
		},
		{
			name:    "IPv4 CIDR",
			input:   []string{"10.0.0.0/24"},
			wantLen: 1,
			wantIPs: []string{"10.0.0.1", "10.0.0.254"},
			wantNot: []string{"10.0.1.1", "192.168.1.1"},
		},
		{
			name:    "multiple entries",
			input:   []string{"10.0.0.1", "192.168.0.0/16"},
			wantLen: 2,
			wantIPs: []string{"10.0.0.1", "192.168.1.1", "192.168.255.255"},
			wantNot: []string{"10.0.0.2", "172.16.0.1"},
		},
		{
			name:    "IPv6",
			input:   []string{"::1"},
			wantLen: 1,
			wantIPs: []string{"::1"},
			wantNot: []string{"::2", "10.0.0.1"},
		},
		{
			name:    "invalid entries ignored",
			input:   []string{"invalid", "10.0.0.1", "also-invalid"},
			wantLen: 1,
			wantIPs: []string{"10.0.0.1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseTrustedProxies(tt.input)
			if len(result) != tt.wantLen {
				t.Errorf("parseTrustedProxies() got %d entries, want %d", len(result), tt.wantLen)
			}

			// Create a rate limiter to use isTrustedProxy
			rl := &RateLimiter{trustedProxies: result}

			for _, ip := range tt.wantIPs {
				if !rl.isTrustedProxy(ip) {
					t.Errorf("isTrustedProxy(%q) = false, want true", ip)
				}
			}

			for _, ip := range tt.wantNot {
				if rl.isTrustedProxy(ip) {
					t.Errorf("isTrustedProxy(%q) = true, want false", ip)
				}
			}
		})
	}
}

func TestRateLimiter_isTrustedProxy(t *testing.T) {
	rl := NewRateLimiter(10, 20, []string{"10.0.0.0/8", "192.168.1.1"})
	defer rl.Close()

	tests := []struct {
		ip   string
		want bool
	}{
		{"10.0.0.1", true},
		{"10.255.255.255", true},
		{"192.168.1.1", true},
		{"192.168.1.2", false},
		{"172.16.0.1", false},
		{"invalid-ip", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			if got := rl.isTrustedProxy(tt.ip); got != tt.want {
				t.Errorf("isTrustedProxy(%q) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

func TestRateLimiter_getLimiter(t *testing.T) {
	rl := NewRateLimiter(10, 20, nil)
	defer rl.Close()

	// First call creates a new limiter
	l1 := rl.getLimiter("1.2.3.4")
	if l1 == nil {
		t.Fatal("getLimiter returned nil")
	}

	// Second call returns same limiter
	l2 := rl.getLimiter("1.2.3.4")
	if l1 != l2 {
		t.Error("getLimiter should return same limiter for same IP")
	}

	// Different IP gets different limiter
	l3 := rl.getLimiter("5.6.7.8")
	if l1 == l3 {
		t.Error("getLimiter should return different limiter for different IP")
	}
}

func TestRateLimiter_Middleware_BasicRateLimiting(t *testing.T) {
	// 2 requests per second, burst of 3
	rl := NewRateLimiter(2, 3, nil)
	defer rl.Close()

	handler := rl.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Make 3 requests quickly (should all succeed due to burst)
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "1.2.3.4:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Request %d: got status %d, want %d", i, rec.Code, http.StatusOK)
		}
	}

	// 4th request should be rate limited
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "1.2.3.4:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("4th request: got status %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
}

func TestRateLimiter_Middleware_TrustedProxy(t *testing.T) {
	// Create rate limiter with trusted proxy
	rl := NewRateLimiter(100, 100, []string{"10.0.0.1"})
	defer rl.Close()

	handler := rl.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Request from trusted proxy with X-Forwarded-For
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345" // Trusted proxy
	req.Header.Set("X-Forwarded-For", "203.0.113.1, 10.0.0.1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRateLimiter_Middleware_UntrustedProxyIgnoresForwardedFor(t *testing.T) {
	// Create rate limiter with NO trusted proxies
	rl := NewRateLimiter(100, 100, nil)
	defer rl.Close()

	// Track which IP was used for rate limiting
	var seenIP string
	handler := rl.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Request with X-Forwarded-For but from untrusted source
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "1.2.3.4:12345" // Direct client (not a trusted proxy)
	req.Header.Set("X-Forwarded-For", "spoofed-ip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// The request should succeed (we're just checking it doesn't crash)
	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}

	// The limiter should have been created for the direct IP, not the spoofed one
	rl.mu.RLock()
	_, hasDirectIP := rl.limiters["1.2.3.4"]
	_, hasSpoofedIP := rl.limiters["spoofed-ip"]
	rl.mu.RUnlock()

	if !hasDirectIP {
		t.Error("Rate limiter should use direct IP when no trusted proxies configured")
	}
	if hasSpoofedIP {
		t.Error("Rate limiter should NOT use spoofed X-Forwarded-For IP")
	}

	_ = seenIP // unused but left for documentation
}

func TestRateLimiter_Middleware_XForwardedForParsing(t *testing.T) {
	rl := NewRateLimiter(100, 100, []string{"10.0.0.1"})
	defer rl.Close()

	tests := []struct {
		name           string
		remoteAddr     string
		forwardedFor   string
		wantLimiterKey string
	}{
		{
			name:           "single IP in X-Forwarded-For",
			remoteAddr:     "10.0.0.1:12345",
			forwardedFor:   "203.0.113.1",
			wantLimiterKey: "203.0.113.1",
		},
		{
			name:           "multiple IPs in X-Forwarded-For",
			remoteAddr:     "10.0.0.1:12345",
			forwardedFor:   "203.0.113.1, 10.0.0.2, 10.0.0.1",
			wantLimiterKey: "203.0.113.1",
		},
		{
			name:           "IP with spaces",
			remoteAddr:     "10.0.0.1:12345",
			forwardedFor:   "  203.0.113.1  ",
			wantLimiterKey: "203.0.113.1",
		},
		{
			name:           "not from trusted proxy",
			remoteAddr:     "1.2.3.4:12345",
			forwardedFor:   "spoofed",
			wantLimiterKey: "1.2.3.4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear limiters for clean test
			rl.mu.Lock()
			rl.limiters = make(map[string]*rate.Limiter)
			rl.mu.Unlock()

			handler := rl.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.forwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.forwardedFor)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			rl.mu.RLock()
			_, hasExpectedKey := rl.limiters[tt.wantLimiterKey]
			rl.mu.RUnlock()

			if !hasExpectedKey {
				t.Errorf("Expected limiter for key %q not found", tt.wantLimiterKey)
			}
		})
	}
}

func TestRateLimiter_Close(t *testing.T) {
	rl := NewRateLimiter(10, 20, nil)

	// Should not panic
	rl.Close()

	// Calling close again should not panic
	rl.Close()
}

func TestNewRateLimiter(t *testing.T) {
	rl := NewRateLimiter(5.5, 10, []string{"10.0.0.0/8"})
	defer rl.Close()

	if rl.rate != 5.5 {
		t.Errorf("rate = %v, want 5.5", rl.rate)
	}
	if rl.burst != 10 {
		t.Errorf("burst = %d, want 10", rl.burst)
	}
	if len(rl.trustedProxies) != 1 {
		t.Errorf("trustedProxies length = %d, want 1", len(rl.trustedProxies))
	}
	if rl.limiters == nil {
		t.Error("limiters map should be initialized")
	}
	if rl.cleanup == nil {
		t.Error("cleanup ticker should be initialized")
	}
}

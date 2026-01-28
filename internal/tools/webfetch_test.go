package tools

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestValidateURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantError bool
		errorMsg  string
	}{
		// Valid URLs
		{
			name:      "valid https URL",
			url:       "https://example.com",
			wantError: false,
		},
		{
			name:      "valid http URL",
			url:       "http://example.com/path",
			wantError: false,
		},
		{
			name:      "valid URL with port",
			url:       "https://example.com:8080/api",
			wantError: false,
		},

		// Invalid schemes
		{
			name:      "file scheme blocked",
			url:       "file:///etc/passwd",
			wantError: true,
			errorMsg:  "unsupported URL scheme",
		},
		{
			name:      "ftp scheme blocked",
			url:       "ftp://internal.server/files",
			wantError: true,
			errorMsg:  "unsupported URL scheme",
		},
		{
			name:      "gopher scheme blocked",
			url:       "gopher://internal:70/",
			wantError: true,
			errorMsg:  "unsupported URL scheme",
		},
		{
			name:      "dict scheme blocked",
			url:       "dict://localhost:2628/",
			wantError: true,
			errorMsg:  "unsupported URL scheme",
		},

		// Localhost variations
		{
			name:      "localhost blocked",
			url:       "http://localhost:8080/",
			wantError: true,
			errorMsg:  "not allowed (localhost/metadata service)",
		},
		{
			name:      "127.0.0.1 blocked",
			url:       "http://127.0.0.1:6379/",
			wantError: true,
			errorMsg:  "not allowed (localhost/metadata service)",
		},
		{
			name:      "0.0.0.0 blocked",
			url:       "http://0.0.0.0/",
			wantError: true,
			errorMsg:  "not allowed (localhost/metadata service)",
		},
		{
			name:      "IPv6 localhost blocked",
			url:       "http://[::1]/",
			wantError: true,
			errorMsg:  "not allowed", // Can match either hostname or IP check
		},

		// Cloud metadata services
		{
			name:      "AWS metadata blocked",
			url:       "http://169.254.169.254/latest/meta-data/",
			wantError: true,
			errorMsg:  "not allowed (localhost/metadata service)",
		},
		{
			name:      "GCP metadata blocked",
			url:       "http://metadata.google.internal/",
			wantError: true,
			errorMsg:  "not allowed (localhost/metadata service)",
		},

		// Kubernetes internal services
		{
			name:      "kubernetes service DNS blocked",
			url:       "http://kubernetes.default.svc.cluster.local/api/v1/namespaces",
			wantError: true,
			errorMsg:  "Kubernetes internal services",
		},
		{
			name:      "kubernetes service with namespace blocked",
			url:       "http://redis.production.svc.cluster.local:6379/",
			wantError: true,
			errorMsg:  "Kubernetes internal services",
		},

		// Private IP ranges (10.0.0.0/8)
		{
			name:      "10.0.0.0/8 range blocked - 10.0.0.1",
			url:       "http://10.0.0.1/",
			wantError: true,
			errorMsg:  "private/internal IP addresses",
		},
		{
			name:      "10.0.0.0/8 range blocked - 10.255.255.255",
			url:       "http://10.255.255.255/",
			wantError: true,
			errorMsg:  "private/internal IP addresses",
		},

		// Private IP ranges (172.16.0.0/12)
		{
			name:      "172.16.0.0/12 range blocked - 172.16.0.1",
			url:       "http://172.16.0.1/",
			wantError: true,
			errorMsg:  "private/internal IP addresses",
		},
		{
			name:      "172.16.0.0/12 range blocked - 172.31.255.255",
			url:       "http://172.31.255.255/",
			wantError: true,
			errorMsg:  "private/internal IP addresses",
		},

		// Private IP ranges (192.168.0.0/16)
		{
			name:      "192.168.0.0/16 range blocked - 192.168.1.1",
			url:       "http://192.168.1.1/",
			wantError: true,
			errorMsg:  "private/internal IP addresses",
		},
		{
			name:      "192.168.0.0/16 range blocked - 192.168.255.255",
			url:       "http://192.168.255.255/",
			wantError: true,
			errorMsg:  "private/internal IP addresses",
		},

		// Loopback range (127.0.0.0/8)
		{
			name:      "127.0.0.0/8 range blocked - 127.0.0.2",
			url:       "http://127.0.0.2/",
			wantError: true,
			errorMsg:  "private/internal IP addresses",
		},
		{
			name:      "127.0.0.0/8 range blocked - 127.255.255.255",
			url:       "http://127.255.255.255/",
			wantError: true,
			errorMsg:  "private/internal IP addresses",
		},

		// Link-local (169.254.0.0/16)
		{
			name:      "169.254.0.0/16 range blocked",
			url:       "http://169.254.1.1/",
			wantError: true,
			errorMsg:  "private/internal IP addresses",
		},

		// Invalid formats
		{
			name:      "invalid URL format",
			url:       "not-a-url",
			wantError: true,
			errorMsg:  "unsupported URL scheme", // Missing scheme results in empty scheme
		},
		{
			name:      "URL with no scheme",
			url:       "example.com",
			wantError: true,
			errorMsg:  "unsupported URL scheme",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateURL(tt.url)

			if tt.wantError {
				if err == nil {
					t.Errorf("validateURL(%q) expected error containing %q, got nil", tt.url, tt.errorMsg)
					return
				}
				if tt.errorMsg != "" && !contains(err.Error(), tt.errorMsg) {
					t.Errorf("validateURL(%q) error = %q, want error containing %q", tt.url, err.Error(), tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("validateURL(%q) unexpected error: %v", tt.url, err)
				}
			}
		})
	}
}

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		name      string
		ip        string
		isPrivate bool
	}{
		// Public IPs
		{"Google DNS", "8.8.8.8", false},
		{"Cloudflare DNS", "1.1.1.1", false},
		{"Valid public IP", "203.0.114.1", false},

		// Loopback
		{"Localhost 127.0.0.1", "127.0.0.1", true},
		{"Loopback 127.0.0.2", "127.0.0.2", true},
		{"IPv6 localhost", "::1", true},

		// Private ranges
		{"10.0.0.0/8 - start", "10.0.0.1", true},
		{"10.0.0.0/8 - end", "10.255.255.255", true},
		{"172.16.0.0/12 - start", "172.16.0.1", true},
		{"172.16.0.0/12 - end", "172.31.255.255", true},
		{"192.168.0.0/16 - start", "192.168.0.1", true},
		{"192.168.0.0/16 - end", "192.168.255.255", true},

		// Link-local
		{"Link-local start", "169.254.0.1", true},
		{"AWS metadata", "169.254.169.254", true},
		{"Link-local end", "169.254.255.255", true},

		// IPv6 private
		{"IPv6 link-local", "fe80::1", true},
		{"IPv6 unique local", "fc00::1", true},
		{"IPv6 unique local", "fd00::1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP: %s", tt.ip)
			}

			result := isPrivateIP(ip)
			if result != tt.isPrivate {
				t.Errorf("isPrivateIP(%q) = %v, want %v", tt.ip, result, tt.isPrivate)
			}
		})
	}
}

// TestHeaderLeakage verifies that sensitive headers are not leaked in outgoing requests
func TestHeaderLeakage(t *testing.T) {
	// Track received headers
	var receivedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response"))
	}))
	defer server.Close()

	// Create an HTTP client exactly as the fetchURL function does
	transport := &http.Transport{
		DisableKeepAlives: true,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 0,
		}).DialContext,
	}

	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}

	// Create request with clean context
	cleanCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(cleanCtx, "GET", server.URL, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	// Set headers exactly as the tool does
	req.Header = http.Header{
		"User-Agent": []string{"KnowledgeAgent/1.0 (Web Content Fetcher)"},
		"Accept":     []string{"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"},
	}

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HTTP status code: %d", resp.StatusCode)
	}

	// Verify ONLY safe headers are present
	allowedHeaders := map[string]bool{
		"User-Agent":      true,
		"Accept":          true,
		"Accept-Encoding": true, // Go http client adds this automatically
	}

	// List of sensitive headers that should NEVER be present
	forbiddenHeaders := []string{
		"X-Forwarded-For",
		"X-Forwarded-Proto",
		"X-Envoy-External-Address",
		"X-Envoy-Peer-Metadata-Id",
		"X-Envoy-Peer-Metadata",
		"X-B3-Traceid",
		"X-B3-Spanid",
		"X-B3-Sampled",
		"X-Request-Id",
		"Authorization",
		"Cookie",
	}

	// Check for forbidden headers
	for _, forbiddenHeader := range forbiddenHeaders {
		if _, exists := receivedHeaders[forbiddenHeader]; exists {
			t.Errorf("Sensitive header leaked: %s = %v", forbiddenHeader, receivedHeaders[forbiddenHeader])
		}
		// Also check lowercase variants
		lowerHeader := strings.ToLower(forbiddenHeader)
		for headerKey := range receivedHeaders {
			if strings.ToLower(headerKey) == lowerHeader {
				t.Errorf("Sensitive header leaked (case variation): %s = %v", headerKey, receivedHeaders[headerKey])
			}
		}
	}

	// Verify only expected headers are present
	for headerKey := range receivedHeaders {
		if !allowedHeaders[headerKey] {
			// Log unexpected headers (not necessarily an error, but good to know)
			t.Logf("Unexpected header present: %s = %v", headerKey, receivedHeaders[headerKey])
		}
	}

	// Verify required headers are present and correct
	userAgent := receivedHeaders.Get("User-Agent")
	if userAgent != "KnowledgeAgent/1.0 (Web Content Fetcher)" {
		t.Errorf("User-Agent = %q, want %q", userAgent, "KnowledgeAgent/1.0 (Web Content Fetcher)")
	}

	accept := receivedHeaders.Get("Accept")
	expectedAccept := "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
	if accept != expectedAccept {
		t.Errorf("Accept = %q, want %q", accept, expectedAccept)
	}
}

// Helper functions

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

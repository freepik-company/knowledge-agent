package tools

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// WebFetchToolset provides tools for fetching and analyzing web content
type WebFetchToolset struct {
	tools []tool.Tool
}

// NewWebFetchToolset creates a new web fetch toolset
func NewWebFetchToolset() (*WebFetchToolset, error) {
	ts := &WebFetchToolset{}

	// Create fetch_url tool
	fetchTool, err := functiontool.New(
		functiontool.Config{
			Name:        "fetch_url",
			Description: "Fetch and read content from a URL. Use this to analyze web pages, documentation, or any online content the user shares. Returns the content as text for analysis.",
		},
		ts.fetchURL,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create fetch_url tool: %w", err)
	}

	ts.tools = []tool.Tool{fetchTool}
	return ts, nil
}

// Name returns the name of the toolset
func (ts *WebFetchToolset) Name() string {
	return "web_fetch_toolset"
}

// Tools returns the list of web fetch tools
func (ts *WebFetchToolset) Tools(ctx agent.ReadonlyContext) ([]tool.Tool, error) {
	return ts.tools, nil
}

// FetchURLArgs are the arguments for the fetch_url tool
type FetchURLArgs struct {
	// URL is the web address to fetch
	URL string `json:"url"`
	// MaxLength is the maximum number of characters to return (optional, default 10000)
	MaxLength int `json:"max_length,omitempty"`
}

// FetchURLResult is the result of the fetch_url tool
type FetchURLResult struct {
	// Success indicates if the fetch was successful
	Success bool `json:"success"`
	// Content is the fetched content
	Content string `json:"content,omitempty"`
	// Error message if fetch failed
	Error string `json:"error,omitempty"`
	// ContentType is the MIME type of the content
	ContentType string `json:"content_type,omitempty"`
	// URL is the final URL after redirects
	FinalURL string `json:"final_url,omitempty"`
}

// validateURL performs SSRF protection checks on the URL
func validateURL(rawURL string) error {
	// Parse URL
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}

	// 1. Validate scheme - only allow http and https
	scheme := strings.ToLower(parsedURL.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("unsupported URL scheme '%s': only http and https are allowed", parsedURL.Scheme)
	}

	// 2. Block empty or invalid hostnames
	hostname := parsedURL.Hostname()
	if hostname == "" {
		return fmt.Errorf("URL must have a valid hostname")
	}

	// 3. Block localhost and special hostnames
	blockedHosts := []string{
		"localhost",
		"127.0.0.1",
		"0.0.0.0",
		"[::1]",
		"metadata.google.internal", // GCP metadata service
		"169.254.169.254",           // AWS/Azure metadata service
	}
	for _, blocked := range blockedHosts {
		if strings.EqualFold(hostname, blocked) {
			return fmt.Errorf("access to '%s' is not allowed (localhost/metadata service)", hostname)
		}
	}

	// 4. Block Kubernetes internal DNS
	if strings.HasSuffix(strings.ToLower(hostname), ".svc.cluster.local") {
		return fmt.Errorf("access to Kubernetes internal services (*.svc.cluster.local) is not allowed")
	}

	// 5. Resolve hostname to IP addresses and validate
	ips, err := net.LookupIP(hostname)
	if err != nil {
		// If DNS resolution fails, allow it to fail naturally during HTTP request
		// This prevents DNS errors from being used as a side channel
		return nil
	}

	// 6. Check if any resolved IP is in a private range
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return fmt.Errorf("access to private/internal IP addresses is not allowed (resolved to %s)", ip.String())
		}
	}

	return nil
}

// isPrivateIP checks if an IP address is in a private/internal range
func isPrivateIP(ip net.IP) bool {
	// Private IPv4 ranges
	privateIPv4Ranges := []string{
		"10.0.0.0/8",        // Private network
		"172.16.0.0/12",     // Private network
		"192.168.0.0/16",    // Private network
		"127.0.0.0/8",       // Loopback
		"169.254.0.0/16",    // Link-local (AWS/Azure metadata)
		"0.0.0.0/8",         // Current network
		"100.64.0.0/10",     // Shared address space (Carrier-grade NAT)
		"192.0.0.0/24",      // IETF Protocol Assignments
		"192.0.2.0/24",      // Documentation (TEST-NET-1)
		"198.18.0.0/15",     // Benchmarking
		"198.51.100.0/24",   // Documentation (TEST-NET-2)
		"203.0.113.0/24",    // Documentation (TEST-NET-3)
		"224.0.0.0/4",       // Multicast
		"240.0.0.0/4",       // Reserved
		"255.255.255.255/32", // Broadcast
	}

	// Check IPv4 ranges
	for _, cidr := range privateIPv4Ranges {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if network.Contains(ip) {
			return true
		}
	}

	// Check IPv6 loopback and link-local
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}

	// Check IPv6 private ranges
	if ip.To4() == nil { // IPv6
		// fc00::/7 - Unique Local Addresses (ULA)
		_, ulaNetwork, _ := net.ParseCIDR("fc00::/7")
		if ulaNetwork.Contains(ip) {
			return true
		}
	}

	return false
}

// fetchURL fetches content from a URL
func (ts *WebFetchToolset) fetchURL(ctx tool.Context, args FetchURLArgs) (FetchURLResult, error) {
	if args.URL == "" {
		return FetchURLResult{
			Success: false,
			Error:   "URL cannot be empty",
		}, nil
	}

	// SSRF Protection: Validate URL before fetching
	if err := validateURL(args.URL); err != nil {
		return FetchURLResult{
			Success: false,
			Error:   fmt.Sprintf("URL validation failed: %v", err),
		}, nil
	}

	// Set default max length
	maxLength := args.MaxLength
	if maxLength <= 0 {
		maxLength = 10000
	}

	// Create HTTP client with timeout and custom transport to prevent header leakage
	transport := &http.Transport{
		// Disable keep-alives to ensure clean connections
		DisableKeepAlives: true,
		// Set reasonable timeouts
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 0,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}

	// Create request with a clean context (not propagating any metadata)
	// Use context.Background() to ensure no metadata from the incoming request is leaked
	cleanCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(cleanCtx, "GET", args.URL, nil)
	if err != nil {
		return FetchURLResult{
			Success: false,
			Error:   fmt.Sprintf("Failed to create request: %v", err),
		}, nil
	}

	// CRITICAL: Set ONLY safe headers - do not propagate any headers from incoming requests
	// This prevents leaking internal infrastructure metadata (Istio, Envoy, K8s, etc.)
	req.Header = http.Header{
		"User-Agent": []string{"KnowledgeAgent/1.0 (Web Content Fetcher)"},
		"Accept":     []string{"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"},
	}

	// Fetch URL
	resp, err := client.Do(req)
	if err != nil {
		return FetchURLResult{
			Success: false,
			Error:   fmt.Sprintf("Failed to fetch URL: %v", err),
		}, nil
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return FetchURLResult{
			Success:  false,
			Error:    fmt.Sprintf("HTTP %d: %s", resp.StatusCode, resp.Status),
			FinalURL: resp.Request.URL.String(),
		}, nil
	}

	// Read content
	content, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxLength)))
	if err != nil {
		return FetchURLResult{
			Success:  false,
			Error:    fmt.Sprintf("Failed to read content: %v", err),
			FinalURL: resp.Request.URL.String(),
		}, nil
	}

	// Get content type
	contentType := resp.Header.Get("Content-Type")

	// Clean up HTML if it's HTML content
	contentStr := string(content)
	if strings.Contains(strings.ToLower(contentType), "text/html") {
		contentStr = cleanHTML(contentStr)
	}

	return FetchURLResult{
		Success:     true,
		Content:     contentStr,
		ContentType: contentType,
		FinalURL:    resp.Request.URL.String(),
	}, nil
}

// cleanHTML removes HTML tags and extracts readable text
func cleanHTML(html string) string {
	// Remove script and style tags
	html = removeHTMLTags(html, "script")
	html = removeHTMLTags(html, "style")

	// Remove HTML tags
	html = strings.ReplaceAll(html, "<", " <")
	html = strings.ReplaceAll(html, ">", "> ")

	// Simple tag removal (not perfect but good enough)
	var result strings.Builder
	inTag := false
	for _, char := range html {
		if char == '<' {
			inTag = true
		} else if char == '>' {
			inTag = false
		} else if !inTag {
			result.WriteRune(char)
		}
	}

	// Clean up whitespace
	text := result.String()
	text = strings.ReplaceAll(text, "\n\n\n", "\n\n")
	text = strings.ReplaceAll(text, "  ", " ")

	return strings.TrimSpace(text)
}

// removeHTMLTags removes all tags of a specific type
func removeHTMLTags(html, tagName string) string {
	startTag := "<" + tagName
	endTag := "</" + tagName + ">"

	for {
		start := strings.Index(strings.ToLower(html), strings.ToLower(startTag))
		if start == -1 {
			break
		}

		end := strings.Index(strings.ToLower(html[start:]), strings.ToLower(endTag))
		if end == -1 {
			break
		}

		html = html[:start] + html[start+end+len(endTag):]
	}

	return html
}

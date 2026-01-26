package tools

import (
	"fmt"
	"io"
	"net/http"
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

// fetchURL fetches content from a URL
func (ts *WebFetchToolset) fetchURL(ctx tool.Context, args FetchURLArgs) (FetchURLResult, error) {
	if args.URL == "" {
		return FetchURLResult{
			Success: false,
			Error:   "URL cannot be empty",
		}, nil
	}

	// Set default max length
	maxLength := args.MaxLength
	if maxLength <= 0 {
		maxLength = 10000
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", args.URL, nil)
	if err != nil {
		return FetchURLResult{
			Success: false,
			Error:   fmt.Sprintf("Failed to create request: %v", err),
		}, nil
	}

	// Set user agent
	req.Header.Set("User-Agent", "KnowledgeAgent/1.0 (Web Content Fetcher)")

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

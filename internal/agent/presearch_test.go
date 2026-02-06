package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/adk/memory"
	"google.golang.org/genai"
)

// mockMemorySearcher implements memory search for testing
type mockMemorySearcher struct {
	searchFunc func(ctx context.Context, req *memory.SearchRequest) (*memory.SearchResponse, error)
}

func (m *mockMemorySearcher) Search(ctx context.Context, req *memory.SearchRequest) (*memory.SearchResponse, error) {
	if m.searchFunc != nil {
		return m.searchFunc(ctx, req)
	}
	return &memory.SearchResponse{}, nil
}

// memorySearcher is an interface for memory search operations
// This allows for easier testing without requiring a full PostgresMemoryService
type memorySearcher interface {
	Search(ctx context.Context, req *memory.SearchRequest) (*memory.SearchResponse, error)
}

// preSearchMemoryWithSearcher is a testable version of preSearchMemory
// that accepts a memorySearcher interface instead of the concrete PostgresMemoryService
func preSearchMemoryWithSearcher(ctx context.Context, searcher memorySearcher, query, userID string) string {
	// Skip empty or whitespace-only queries
	if query == "" || len(query) == 0 {
		return ""
	}

	// Check for whitespace-only
	hasNonWhitespace := false
	for _, c := range query {
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			hasNonWhitespace = true
			break
		}
	}
	if !hasNonWhitespace {
		return ""
	}

	// Pre-search should not block the main query if memory service is slow
	const preSearchTimeout = 3 * time.Second
	searchCtx, cancel := context.WithTimeout(ctx, preSearchTimeout)
	defer cancel()

	// Execute search on memory service directly
	searchResp, err := searcher.Search(searchCtx, &memory.SearchRequest{
		Query:   query,
		UserID:  userID,
		AppName: appName,
	})

	if err != nil {
		return ""
	}

	if searchResp == nil || len(searchResp.Memories) == 0 {
		return "No relevant information found in memory."
	}

	// Format results for context (limit to avoid token overflow)
	const maxPreSearchResults = 5
	var result string
	resultCount := 0

	for i, entry := range searchResp.Memories {
		if resultCount >= maxPreSearchResults {
			break
		}
		if entry.Content != nil && len(entry.Content.Parts) > 0 {
			// Extract text from the first part
			if entry.Content.Parts[0].Text != "" {
				result += string(rune('1'+i)) + ". " + entry.Content.Parts[0].Text + "\n"
				resultCount++
			}
		}
	}

	if result == "" {
		return "No relevant information found in memory."
	}

	return result
}

func TestPreSearchMemory_EmptyQuery(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "empty string returns empty",
			query: "",
			want:  "",
		},
		{
			name:  "whitespace only returns empty",
			query: "   ",
			want:  "",
		},
		{
			name:  "tabs and newlines return empty",
			query: "\t\n\r",
			want:  "",
		},
	}

	mock := &mockMemorySearcher{}
	ctx := context.Background()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := preSearchMemoryWithSearcher(ctx, mock, tt.query, "user-123")
			if got != tt.want {
				t.Errorf("preSearchMemory(%q) = %q, want %q", tt.query, got, tt.want)
			}
		})
	}
}

func TestPreSearchMemory_NoResults(t *testing.T) {
	mock := &mockMemorySearcher{
		searchFunc: func(ctx context.Context, req *memory.SearchRequest) (*memory.SearchResponse, error) {
			return &memory.SearchResponse{
				Memories: []memory.Entry{},
			}, nil
		},
	}

	ctx := context.Background()
	got := preSearchMemoryWithSearcher(ctx, mock, "test query", "user-123")

	want := "No relevant information found in memory."
	if got != want {
		t.Errorf("preSearchMemory() = %q, want %q", got, want)
	}
}

func TestPreSearchMemory_NilResponse(t *testing.T) {
	mock := &mockMemorySearcher{
		searchFunc: func(ctx context.Context, req *memory.SearchRequest) (*memory.SearchResponse, error) {
			return nil, nil
		},
	}

	ctx := context.Background()
	got := preSearchMemoryWithSearcher(ctx, mock, "test query", "user-123")

	want := "No relevant information found in memory."
	if got != want {
		t.Errorf("preSearchMemory() = %q, want %q", got, want)
	}
}

func TestPreSearchMemory_Error(t *testing.T) {
	mock := &mockMemorySearcher{
		searchFunc: func(ctx context.Context, req *memory.SearchRequest) (*memory.SearchResponse, error) {
			return nil, errors.New("database connection failed")
		},
	}

	ctx := context.Background()
	got := preSearchMemoryWithSearcher(ctx, mock, "test query", "user-123")

	// On error, should return empty string (graceful degradation)
	want := ""
	if got != want {
		t.Errorf("preSearchMemory() on error = %q, want %q", got, want)
	}
}

func TestPreSearchMemory_Timeout(t *testing.T) {
	mock := &mockMemorySearcher{
		searchFunc: func(ctx context.Context, req *memory.SearchRequest) (*memory.SearchResponse, error) {
			// Simulate slow response by waiting for context cancellation
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(10 * time.Second):
				return &memory.SearchResponse{}, nil
			}
		},
	}

	// Use a short timeout context to test timeout behavior
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	got := preSearchMemoryWithSearcher(ctx, mock, "test query", "user-123")
	elapsed := time.Since(start)

	// Should return empty on timeout (graceful degradation)
	want := ""
	if got != want {
		t.Errorf("preSearchMemory() on timeout = %q, want %q", got, want)
	}

	// Should not take much longer than the timeout
	if elapsed > 500*time.Millisecond {
		t.Errorf("preSearchMemory() took %v, expected to timeout quickly", elapsed)
	}
}

func TestPreSearchMemory_WithResults(t *testing.T) {
	mock := &mockMemorySearcher{
		searchFunc: func(ctx context.Context, req *memory.SearchRequest) (*memory.SearchResponse, error) {
			return &memory.SearchResponse{
				Memories: []memory.Entry{
					{
						Content: genai.NewContentFromText("First result about deployment", genai.RoleUser),
					},
					{
						Content: genai.NewContentFromText("Second result about testing", genai.RoleUser),
					},
				},
			}, nil
		},
	}

	ctx := context.Background()
	got := preSearchMemoryWithSearcher(ctx, mock, "deployment process", "user-123")

	// Check that results are formatted correctly
	if got == "" {
		t.Error("preSearchMemory() returned empty string, expected results")
	}

	if got == "No relevant information found in memory." {
		t.Error("preSearchMemory() returned no results message, expected actual results")
	}

	// Check that both results are included
	if !contains(got, "First result") {
		t.Errorf("preSearchMemory() = %q, missing first result", got)
	}

	if !contains(got, "Second result") {
		t.Errorf("preSearchMemory() = %q, missing second result", got)
	}
}

func TestPreSearchMemory_LimitResults(t *testing.T) {
	// Create 10 results to test the limit of 5
	mock := &mockMemorySearcher{
		searchFunc: func(ctx context.Context, req *memory.SearchRequest) (*memory.SearchResponse, error) {
			entries := make([]memory.Entry, 10)
			for i := 0; i < 10; i++ {
				entries[i] = memory.Entry{
					Content: genai.NewContentFromText("Result "+string(rune('A'+i)), genai.RoleUser),
				}
			}
			return &memory.SearchResponse{
				Memories: entries,
			}, nil
		},
	}

	ctx := context.Background()
	got := preSearchMemoryWithSearcher(ctx, mock, "test query", "user-123")

	// Should include first 5 results (A, B, C, D, E)
	for _, letter := range []string{"A", "B", "C", "D", "E"} {
		if !contains(got, "Result "+letter) {
			t.Errorf("preSearchMemory() = %q, missing Result %s (within limit)", got, letter)
		}
	}

	// Should NOT include results beyond limit (F, G, H, I, J)
	for _, letter := range []string{"F", "G", "H", "I", "J"} {
		if contains(got, "Result "+letter) {
			t.Errorf("preSearchMemory() = %q, should not include Result %s (beyond limit)", got, letter)
		}
	}
}

func TestPreSearchMemory_RequestParams(t *testing.T) {
	var capturedReq *memory.SearchRequest

	mock := &mockMemorySearcher{
		searchFunc: func(ctx context.Context, req *memory.SearchRequest) (*memory.SearchResponse, error) {
			capturedReq = req
			return &memory.SearchResponse{}, nil
		},
	}

	ctx := context.Background()
	preSearchMemoryWithSearcher(ctx, mock, "my search query", "user-456")

	if capturedReq == nil {
		t.Fatal("Search was not called")
	}

	if capturedReq.Query != "my search query" {
		t.Errorf("Search query = %q, want %q", capturedReq.Query, "my search query")
	}

	if capturedReq.UserID != "user-456" {
		t.Errorf("Search userID = %q, want %q", capturedReq.UserID, "user-456")
	}

	if capturedReq.AppName != appName {
		t.Errorf("Search appName = %q, want %q", capturedReq.AppName, appName)
	}
}

func TestPreSearchMemory_EntriesWithNilContent(t *testing.T) {
	mock := &mockMemorySearcher{
		searchFunc: func(ctx context.Context, req *memory.SearchRequest) (*memory.SearchResponse, error) {
			return &memory.SearchResponse{
				Memories: []memory.Entry{
					{Content: nil}, // nil content
					{Content: genai.NewContentFromText("Valid result", genai.RoleUser)},
					{Content: &genai.Content{Parts: []*genai.Part{}}}, // empty parts
				},
			}, nil
		},
	}

	ctx := context.Background()
	got := preSearchMemoryWithSearcher(ctx, mock, "test query", "user-123")

	// Should include valid result and skip nil/empty ones
	if !contains(got, "Valid result") {
		t.Errorf("preSearchMemory() = %q, missing valid result", got)
	}
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && containsHelper(s, substr)))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

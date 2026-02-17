package slack

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"knowledge-agent/internal/logger"

	"github.com/slack-go/slack"
)

// threadCacheEntry stores cached thread messages with expiration
type threadCacheEntry struct {
	messages  []Message
	timestamp time.Time
}

// Client wraps the Slack API client
type Client struct {
	api         *slack.Client
	token       string
	maxFileSize int64
	httpClient  *http.Client // Shared HTTP client for file downloads (thread-safe)

	// Thread message cache
	threadCache   map[string]*threadCacheEntry
	cacheMu       sync.RWMutex
	cacheTTL      time.Duration
	cacheMaxSize  int
	cleanupTicker *time.Ticker
	cleanupDone   chan struct{}
}

// Message represents a Slack message
type Message struct {
	User      string    `json:"user"`
	Text      string    `json:"text"`
	Timestamp string    `json:"ts"`
	ThreadTS  string    `json:"thread_ts,omitempty"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
	Files     []File    `json:"files,omitempty"`
}

// File represents a Slack file attachment
type File struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	MIMEType string `json:"mimetype"`
	URL      string `json:"url_private"`
	Size     int    `json:"size"`
}

// User represents a Slack user
type User struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	RealName string `json:"real_name"`
	Email    string `json:"email"` // Requires users:read.email scope
}

// ClientConfig holds configuration for the Slack client
type ClientConfig struct {
	Token           string
	MaxFileSize     int64
	ThreadCacheTTL  time.Duration // How long to cache thread messages (default: 5m)
	ThreadCacheSize int           // Max threads to keep in cache (default: 100)
}

// NewClient creates a new Slack client with thread message caching
func NewClient(cfg ClientConfig) *Client {
	// Apply defaults if not specified
	cacheTTL := cfg.ThreadCacheTTL
	if cacheTTL <= 0 {
		cacheTTL = 5 * time.Minute
	}
	cacheMaxSize := cfg.ThreadCacheSize
	if cacheMaxSize <= 0 {
		cacheMaxSize = 100
	}

	c := &Client{
		api:         slack.New(cfg.Token),
		token:       cfg.Token,
		maxFileSize: cfg.MaxFileSize,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		threadCache:   make(map[string]*threadCacheEntry),
		cacheTTL:      cacheTTL,
		cacheMaxSize:  cacheMaxSize,
		cleanupTicker: time.NewTicker(1 * time.Minute),
		cleanupDone:   make(chan struct{}),
	}

	// Start cache cleanup goroutine
	go c.cacheCleanupRoutine()

	return c
}

// Close stops the cache cleanup routine
func (c *Client) Close() error {
	if c.cleanupTicker != nil {
		c.cleanupTicker.Stop()
		close(c.cleanupDone)
	}
	return nil
}

// cacheCleanupRoutine periodically removes expired cache entries
func (c *Client) cacheCleanupRoutine() {
	for {
		select {
		case <-c.cleanupTicker.C:
			c.cleanupExpiredCache()
		case <-c.cleanupDone:
			return
		}
	}
}

// cleanupExpiredCache removes expired entries from the cache
func (c *Client) cleanupExpiredCache() {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	now := time.Now()
	removed := 0

	for key, entry := range c.threadCache {
		if now.Sub(entry.timestamp) > c.cacheTTL {
			delete(c.threadCache, key)
			removed++
		}
	}

	if removed > 0 {
		log := logger.Get()
		log.Debugw("Cleaned up expired thread cache entries",
			"removed", removed,
			"remaining", len(c.threadCache))
	}
}

// FetchThreadMessages fetches all messages in a thread with caching
func (c *Client) FetchThreadMessages(channelID, threadTS string) ([]Message, error) {
	log := logger.Get()
	cacheKey := fmt.Sprintf("%s:%s", channelID, threadTS)

	// Check cache first
	c.cacheMu.RLock()
	if entry, found := c.threadCache[cacheKey]; found {
		if time.Since(entry.timestamp) <= c.cacheTTL {
			c.cacheMu.RUnlock()
			log.Debugw("Thread messages served from cache",
				"channel_id", channelID,
				"thread_ts", threadTS,
				"message_count", len(entry.messages))
			return entry.messages, nil
		}
	}
	c.cacheMu.RUnlock()

	// Cache miss or expired - fetch from Slack API
	log.Debugw("Fetching thread messages from Slack API",
		"channel_id", channelID,
		"thread_ts", threadTS)

	var allMessages []Message
	cursor := ""

	for {
		// Fetch messages with pagination
		msgs, hasMore, nextCursor, err := c.api.GetConversationReplies(&slack.GetConversationRepliesParameters{
			ChannelID: channelID,
			Timestamp: threadTS,
			Cursor:    cursor,
			Limit:     100, // Max per page
		})

		if err != nil {
			return nil, fmt.Errorf("failed to fetch thread messages: %w", err)
		}

		// Convert to our Message type
		for _, msg := range msgs {
			// Parse timestamp
			ts, err := parseSlackTimestamp(msg.Timestamp)
			if err != nil {
				log.Warnw("Failed to parse timestamp, using current time",
					"timestamp", msg.Timestamp,
					"error", err,
				)
				ts = time.Now()
			}

			// Extract files if present
			var files []File
			for _, f := range msg.Files {
				files = append(files, File{
					ID:       f.ID,
					Name:     f.Name,
					MIMEType: f.Mimetype,
					URL:      f.URLPrivate,
					Size:     f.Size,
				})
			}

			// Get message text - combine main text + attachments
			var textParts []string

			// Add main text if present
			if msg.Text != "" {
				textParts = append(textParts, msg.Text)
			}

			// Extract text from attachments (common for bot messages like alerts)
			if len(msg.Attachments) > 0 {
				for _, att := range msg.Attachments {
					// Pretext goes first (usually context/header)
					if att.Pretext != "" {
						textParts = append(textParts, att.Pretext)
					}
					// Main attachment text
					if att.Text != "" {
						textParts = append(textParts, att.Text)
					} else if att.Fallback != "" {
						// Fallback is a plain-text summary
						textParts = append(textParts, att.Fallback)
					}
				}
			}

			// Extract text from Block Kit blocks (workflow forms, rich messages)
			if len(msg.Blocks.BlockSet) > 0 {
				if blockText := extractBlockText(msg.Blocks); blockText != "" {
					textParts = append(textParts, blockText)
				}
			}

			text := strings.Join(textParts, "\n")

			// Determine user identifier (handle bot messages)
			user := msg.User
			if user == "" && msg.BotID != "" {
				user = fmt.Sprintf("bot:%s", msg.BotID)
				if msg.Username != "" {
					user = fmt.Sprintf("bot:%s", msg.Username)
				}
			}

			allMessages = append(allMessages, Message{
				User:      user,
				Text:      text,
				Timestamp: msg.Timestamp,
				ThreadTS:  msg.ThreadTimestamp,
				Type:      msg.Type,
				CreatedAt: ts,
				Files:     files,
			})
		}

		if !hasMore {
			break
		}

		cursor = nextCursor
	}

	// Store in cache
	c.cacheMu.Lock()
	// Enforce max cache size (LRU-style: remove oldest if at limit)
	if len(c.threadCache) >= c.cacheMaxSize {
		var oldestKey string
		var oldestTime time.Time
		for key, entry := range c.threadCache {
			if oldestKey == "" || entry.timestamp.Before(oldestTime) {
				oldestKey = key
				oldestTime = entry.timestamp
			}
		}
		delete(c.threadCache, oldestKey)
		log.Debugw("Evicted oldest cache entry", "key", oldestKey)
	}

	c.threadCache[cacheKey] = &threadCacheEntry{
		messages:  allMessages,
		timestamp: time.Now(),
	}
	c.cacheMu.Unlock()

	log.Debugw("Thread messages cached",
		"channel_id", channelID,
		"thread_ts", threadTS,
		"message_count", len(allMessages))

	return allMessages, nil
}

// MaxSlackMessageLength is the maximum length for a single Slack message
// Slack's actual limit is 40,000 but we use a slightly lower value to be safe
const MaxSlackMessageLength = 39000

// PostMessage posts a message to a Slack channel/thread
// If the message exceeds Slack's limit, it will be split into multiple messages
func (c *Client) PostMessage(channelID, threadTS, text string) error {
	log := logger.Get()

	// Check if message needs to be split
	if len(text) <= MaxSlackMessageLength {
		return c.postSingleMessage(channelID, threadTS, text)
	}

	// Split long messages
	log.Infow("Message exceeds Slack limit, splitting into chunks",
		"total_length", len(text),
		"max_length", MaxSlackMessageLength,
	)

	chunks := splitMessage(text, MaxSlackMessageLength)
	log.Debugw("Message split into chunks", "chunk_count", len(chunks))

	for i, chunk := range chunks {
		// Add continuation indicator for chunks after the first
		if i > 0 {
			chunk = fmt.Sprintf("_(continued %d/%d)_\n%s", i+1, len(chunks), chunk)
		}

		if err := c.postSingleMessage(channelID, threadTS, chunk); err != nil {
			log.Errorw("Failed to post message chunk",
				"chunk", i+1,
				"total_chunks", len(chunks),
				"error", err,
			)
			return fmt.Errorf("failed to post message chunk %d/%d: %w", i+1, len(chunks), err)
		}

		// Small delay between chunks to avoid rate limiting
		if i < len(chunks)-1 {
			time.Sleep(500 * time.Millisecond)
		}
	}

	return nil
}

// postSingleMessage posts a single message with retry logic
func (c *Client) postSingleMessage(channelID, threadTS, text string) error {
	log := logger.Get()
	maxRetries := 3
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		options := []slack.MsgOption{
			slack.MsgOptionText(text, false),
		}

		// If threadTS is provided, post as a thread reply
		if threadTS != "" {
			options = append(options, slack.MsgOptionTS(threadTS))
		}

		_, _, err := c.api.PostMessage(channelID, options...)
		if err == nil {
			return nil
		}

		lastErr = err

		// Check if error is retryable
		errStr := err.Error()
		isRetryable := strings.Contains(errStr, "EOF") ||
			strings.Contains(errStr, "connection reset") ||
			strings.Contains(errStr, "timeout") ||
			strings.Contains(errStr, "rate_limited")

		if !isRetryable {
			log.Errorw("Non-retryable error posting message",
				"error", err,
				"text_length", len(text),
			)
			return fmt.Errorf("failed to post message: %w", err)
		}

		log.Warnw("Retryable error posting message",
			"attempt", attempt,
			"max_retries", maxRetries,
			"error", err,
		)

		if attempt < maxRetries {
			// Exponential backoff: 1s, 2s, 4s
			delay := time.Duration(1<<uint(attempt-1)) * time.Second
			time.Sleep(delay)
		}
	}

	return fmt.Errorf("failed to post message after %d attempts: %w", maxRetries, lastErr)
}

// splitMessage splits a long message into chunks, trying to break at natural points
func splitMessage(text string, maxLength int) []string {
	if len(text) <= maxLength {
		return []string{text}
	}

	var chunks []string
	remaining := text

	for len(remaining) > 0 {
		if len(remaining) <= maxLength {
			chunks = append(chunks, remaining)
			break
		}

		// Find a good break point (prefer paragraph, then sentence, then word)
		chunk := remaining[:maxLength]
		breakPoint := maxLength

		// Try to break at paragraph (double newline)
		if idx := strings.LastIndex(chunk, "\n\n"); idx > maxLength/2 {
			breakPoint = idx + 2
		} else if idx := strings.LastIndex(chunk, "\n"); idx > maxLength/2 {
			// Try to break at single newline
			breakPoint = idx + 1
		} else if idx := strings.LastIndex(chunk, ". "); idx > maxLength/2 {
			// Try to break at sentence
			breakPoint = idx + 2
		} else if idx := strings.LastIndex(chunk, " "); idx > maxLength/2 {
			// Try to break at word
			breakPoint = idx + 1
		}
		// If no good break point found, just cut at maxLength

		chunks = append(chunks, strings.TrimSpace(remaining[:breakPoint]))
		remaining = strings.TrimSpace(remaining[breakPoint:])
	}

	return chunks
}

// GetUserInfo retrieves user information including email (requires users:read.email scope)
func (c *Client) GetUserInfo(userID string) (*User, error) {
	user, err := c.api.GetUserInfo(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}

	return &User{
		ID:       user.ID,
		Name:     user.Name,
		RealName: user.RealName,
		Email:    user.Profile.Email, // Requires users:read.email scope
	}, nil
}

// GetUserNames fetches display names for multiple user IDs
// Returns a map of userID -> displayName (uses RealName if available, falls back to Name)
// Errors are logged but don't stop processing - missing users will not be in the map
func (c *Client) GetUserNames(userIDs []string) map[string]string {
	log := logger.Get()
	result := make(map[string]string)

	// Deduplicate user IDs
	seen := make(map[string]bool)
	uniqueIDs := make([]string, 0, len(userIDs))
	for _, id := range userIDs {
		if id != "" && !seen[id] {
			seen[id] = true
			uniqueIDs = append(uniqueIDs, id)
		}
	}

	for _, userID := range uniqueIDs {
		user, err := c.api.GetUserInfo(userID)
		if err != nil {
			log.Debugw("Failed to get user info, using ID",
				"user_id", userID,
				"error", err,
			)
			continue
		}

		// Prefer RealName, fall back to Name, then to ID
		displayName := user.RealName
		if displayName == "" {
			displayName = user.Name
		}
		if displayName == "" {
			displayName = userID
		}
		result[userID] = displayName
	}

	log.Debugw("Fetched user names",
		"requested", len(uniqueIDs),
		"resolved", len(result),
	)

	return result
}

// GetChannelInfo retrieves channel information
func (c *Client) GetChannelInfo(channelID string) (*slack.Channel, error) {
	channel, err := c.api.GetConversationInfo(&slack.GetConversationInfoInput{
		ChannelID: channelID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get channel info: %w", err)
	}

	return channel, nil
}

// DownloadFile downloads a file from Slack
func (c *Client) DownloadFile(fileURL string) ([]byte, error) {
	if fileURL == "" {
		return nil, fmt.Errorf("file URL is empty")
	}

	req, err := http.NewRequest("GET", fileURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add Slack authorization header
	req.Header.Add("Authorization", "Bearer "+c.token)

	// Use shared HTTP client (timeout configured at client creation)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download file: status %d (URL: %s)", resp.StatusCode, fileURL)
	}

	// Check Content-Length header if available
	if resp.ContentLength > 0 && resp.ContentLength > c.maxFileSize {
		return nil, fmt.Errorf("file too large: %d bytes (max %d)", resp.ContentLength, c.maxFileSize)
	}

	// Read with size limit
	data, err := io.ReadAll(io.LimitReader(resp.Body, c.maxFileSize))
	if err != nil {
		return nil, fmt.Errorf("failed to read file data: %w", err)
	}

	// Verify we didn't hit the limit (file might be larger than limit)
	if int64(len(data)) == c.maxFileSize {
		// Try to read one more byte to see if there's more
		oneByte := make([]byte, 1)
		if n, _ := resp.Body.Read(oneByte); n > 0 {
			return nil, fmt.Errorf("file exceeds maximum size of %d bytes", c.maxFileSize)
		}
	}

	return data, nil
}

// IsImage checks if a file is an image based on MIME type
func (f *File) IsImage() bool {
	return strings.HasPrefix(f.MIMEType, "image/")
}

// extractBlockText extracts readable text from Block Kit blocks.
// Workflow Forms and rich messages use Block Kit instead of plain text.
func extractBlockText(blocks slack.Blocks) string {
	var parts []string

	for _, block := range blocks.BlockSet {
		switch b := block.(type) {
		case *slack.HeaderBlock:
			if b.Text != nil && b.Text.Text != "" {
				parts = append(parts, b.Text.Text)
			}

		case *slack.SectionBlock:
			if b.Text != nil && b.Text.Text != "" {
				parts = append(parts, b.Text.Text)
			}
			for _, field := range b.Fields {
				if field != nil && field.Text != "" {
					parts = append(parts, field.Text)
				}
			}

		case *slack.RichTextBlock:
			if text := extractRichTextElements(b.Elements); text != "" {
				parts = append(parts, text)
			}

		case *slack.ContextBlock:
			for _, elem := range b.ContextElements.Elements {
				if textObj, ok := elem.(*slack.TextBlockObject); ok {
					if textObj.Text != "" {
						parts = append(parts, textObj.Text)
					}
				}
			}
		}
	}

	return strings.Join(parts, "\n")
}

// extractRichTextElements recursively extracts text from RichTextElement slices.
func extractRichTextElements(elements []slack.RichTextElement) string {
	var parts []string

	for _, elem := range elements {
		switch e := elem.(type) {
		case *slack.RichTextSection:
			var sectionParts []string
			for _, se := range e.Elements {
				switch el := se.(type) {
				case *slack.RichTextSectionTextElement:
					if el.Text != "" {
						sectionParts = append(sectionParts, el.Text)
					}
				case *slack.RichTextSectionLinkElement:
					if el.Text != "" {
						sectionParts = append(sectionParts, el.Text)
					} else if el.URL != "" {
						sectionParts = append(sectionParts, el.URL)
					}
				}
			}
			if len(sectionParts) > 0 {
				parts = append(parts, strings.Join(sectionParts, ""))
			}

		case *slack.RichTextList:
			for _, child := range e.Elements {
				if childText := extractRichTextElements([]slack.RichTextElement{child}); childText != "" {
					parts = append(parts, "- "+childText)
				}
			}
		}
	}

	return strings.Join(parts, "\n")
}

// parseSlackTimestamp parses Slack timestamp (format: "1234567890.123456")
func parseSlackTimestamp(ts string) (time.Time, error) {
	var sec, nsec int64
	_, err := fmt.Sscanf(ts, "%d.%d", &sec, &nsec)
	if err != nil {
		return time.Time{}, err
	}

	// Slack timestamps have 6 decimal places (microseconds)
	return time.Unix(sec, nsec*1000), nil
}

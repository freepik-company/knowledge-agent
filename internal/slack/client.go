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
}

// NewClient creates a new Slack client with thread message caching
func NewClient(token string, maxFileSize int64) *Client {
	c := &Client{
		api:           slack.New(token),
		token:         token,
		maxFileSize:   maxFileSize,
		threadCache:   make(map[string]*threadCacheEntry),
		cacheTTL:      5 * time.Minute,  // Cache thread messages for 5 minutes
		cacheMaxSize:  100,               // Max 100 threads cached
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

			allMessages = append(allMessages, Message{
				User:      msg.User,
				Text:      msg.Text,
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

// PostMessage posts a message to a Slack channel/thread
func (c *Client) PostMessage(channelID, threadTS, text string) error {
	options := []slack.MsgOption{
		slack.MsgOptionText(text, false),
	}

	// If threadTS is provided, post as a thread reply
	if threadTS != "" {
		options = append(options, slack.MsgOptionTS(threadTS))
	}

	_, _, err := c.api.PostMessage(channelID, options...)
	if err != nil {
		return fmt.Errorf("failed to post message: %w", err)
	}

	return nil
}

// GetUserInfo retrieves user information
func (c *Client) GetUserInfo(userID string) (*User, error) {
	user, err := c.api.GetUserInfo(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}

	return &User{
		ID:       user.ID,
		Name:     user.Name,
		RealName: user.RealName,
	}, nil
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
	req, err := http.NewRequest("GET", fileURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add Slack authorization header
	req.Header.Add("Authorization", "Bearer "+c.token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download file: status %d", resp.StatusCode)
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

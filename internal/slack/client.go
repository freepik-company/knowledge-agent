package slack

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"knowledge-agent/internal/logger"

	"github.com/slack-go/slack"
)

// Client wraps the Slack API client
type Client struct {
	api         *slack.Client
	token       string
	maxFileSize int64
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

// NewClient creates a new Slack client
func NewClient(token string, maxFileSize int64) *Client {
	return &Client{
		api:         slack.New(token),
		token:       token,
		maxFileSize: maxFileSize,
	}
}

// FetchThreadMessages fetches all messages in a thread
func (c *Client) FetchThreadMessages(channelID, threadTS string) ([]Message, error) {
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
				logger.Get().Warnw("Failed to parse timestamp, using current time",
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

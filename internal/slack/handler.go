package slack

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/slack-go/slack/slackevents"
	"knowledge-agent/internal/config"
	"knowledge-agent/internal/logger"
	"knowledge-agent/internal/observability"
)

// Handler handles Slack events and bridges them to the Knowledge Agent
type Handler struct {
	config        *config.Config
	client        *Client
	agentURL      string // URL of the Knowledge Agent service
	internalToken string // Internal token for authenticating with the Knowledge Agent
}

// NewHandler creates a new Slack event handler
func NewHandler(cfg *config.Config, agentURL string) *Handler {
	return &Handler{
		config: cfg,
		client: NewClient(ClientConfig{
			Token:           cfg.Slack.BotToken,
			MaxFileSize:     cfg.Slack.MaxFileSize,
			ThreadCacheTTL:  cfg.Slack.ThreadCacheTTL,
			ThreadCacheSize: cfg.Slack.ThreadCacheMaxSize,
		}),
		agentURL:      agentURL,
		internalToken: cfg.Auth.InternalToken,
	}
}

// Close releases resources held by the handler
func (h *Handler) Close() error {
	if h.client != nil {
		return h.client.Close()
	}
	return nil
}

// HandleEvents handles incoming Slack events
func (h *Handler) HandleEvents(w http.ResponseWriter, r *http.Request) {
	log := logger.Get()

	// Verify request signature
	if err := VerifySlackRequest(r, h.config.Slack.SigningSecret); err != nil {
		log.Warnw("Signature verification failed", "error", err)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse event
	body := make([]byte, r.ContentLength)
	n, err := io.ReadFull(r.Body, body)
	if err != nil && err != io.ErrUnexpectedEOF {
		log.Errorw("Failed to read request body", "error", err, "bytes_read", n)
		http.Error(w, "Failed to read request", http.StatusBadRequest)
		return
	}
	body = body[:n]

	eventsAPIEvent, err := slackevents.ParseEvent(json.RawMessage(body), slackevents.OptionNoVerifyToken())
	if err != nil {
		log.Errorw("Failed to parse event", "error", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	// Handle URL verification challenge
	if eventsAPIEvent.Type == slackevents.URLVerification {
		var challenge *slackevents.ChallengeResponse
		if err := json.Unmarshal(body, &challenge); err != nil {
			log.Errorw("Failed to unmarshal challenge", "error", err)
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(challenge.Challenge))
		return
	}

	// Handle callback events
	if eventsAPIEvent.Type == slackevents.CallbackEvent {
		innerEvent := eventsAPIEvent.InnerEvent
		log.Infow("Webhook: Processing callback event",
			"inner_type", innerEvent.Type,
		)

		switch ev := innerEvent.Data.(type) {
		case *slackevents.AppMentionEvent:
			log.Infow("Webhook: AppMentionEvent detected",
				"user", ev.User,
				"channel", ev.Channel,
				"text", ev.Text,
			)
			observability.RecordSlackEvent("app_mention", true)
			go h.handleAppMention(ev)
		default:
			log.Debugw("Unhandled event type", "type", innerEvent.Type)
			observability.RecordSlackEvent(innerEvent.Type, true)
		}
	}

	w.WriteHeader(http.StatusOK)
}

// handleAppMention handles app mention events from Slack
func (h *Handler) handleAppMention(event *slackevents.AppMentionEvent) {
	log := logger.Get()
	log.Debugw("Processing app mention",
		"user", event.User,
		"channel", event.Channel,
	)

	// Create context with timeout for async operations
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Strip bot mention and get the user's message
	message := stripBotMention(event.Text)

	// Always send to agent - LLM will decide what to do
	h.sendToAgent(ctx, event, message)

	log.Debugw("App mention processed", "user", event.User)
}

// sendToAgent sends a user message to the Knowledge Agent
func (h *Handler) sendToAgent(ctx context.Context, event *slackevents.AppMentionEvent, message string) {
	log := logger.Get()

	// Determine thread timestamp
	threadTS := event.ThreadTimeStamp
	if threadTS == "" {
		threadTS = event.TimeStamp
	}

	log.Infow("Slack event received",
		"user_id", event.User,
		"thread_ts", threadTS,
		"channel", event.Channel,
	)

	// 1. Fetch user information for personalization
	var userName, userRealName string
	userInfo, err := h.client.GetUserInfo(event.User)
	if err != nil {
		log.Warnw("Failed to fetch user info (continuing without name)",
			"user_id", event.User,
			"error", err,
		)
		observability.RecordSlackAPICall("users.info", false)
	} else {
		userName = userInfo.Name         // @username
		userRealName = userInfo.RealName // John Doe
		log.Debugw("User info fetched",
			"user_id", event.User,
			"name", userName,
			"real_name", userRealName,
		)
		observability.RecordSlackAPICall("users.info", true)
	}

	// 2. Fetch current thread for context
	log.Debugw("Fetching thread messages")
	messages, err := h.client.FetchThreadMessages(event.Channel, threadTS)
	if err != nil {
		log.Errorw("Failed to fetch thread", "error", err, "channel", event.Channel, "thread_ts", threadTS)
		observability.RecordSlackAPICall("conversations.replies", false)
		h.client.PostMessage(event.Channel, threadTS,
			fmt.Sprintf("Error: Could not fetch thread messages: %v", err))
		return
	}
	observability.RecordSlackAPICall("conversations.replies", true)

	log.Debugw("Fetched thread messages", "count", len(messages))

	// 3. Transform messages to format for agent
	// Track image stats for logging
	maxImagesPerThread := h.config.Slack.MaxImagesPerThread
	if maxImagesPerThread <= 0 {
		maxImagesPerThread = 10 // Default limit to prevent payload bloat
	}
	totalImagesFound := 0
	totalImagesDownloaded := 0
	totalImageBytes := 0

	messageData := make([]map[string]any, len(messages))
	for i, msg := range messages {
		msgData := map[string]any{
			"user": msg.User,
			"text": msg.Text,
			"ts":   msg.Timestamp,
			"type": msg.Type,
		}

		// Include images if present (respect max limit across all messages)
		if len(msg.Files) > 0 {
			var images []map[string]any
			for _, file := range msg.Files {
				if file.IsImage() {
					totalImagesFound++

					// Check if we've hit the limit
					if totalImagesDownloaded >= maxImagesPerThread {
						log.Debugw("Skipping image due to limit",
							"name", file.Name,
							"current_count", totalImagesDownloaded,
							"max", maxImagesPerThread,
						)
						continue
					}

					log.Debugw("Downloading image",
						"name", file.Name,
						"mime_type", file.MIMEType,
						"size", file.Size,
						"url_present", file.URL != "",
					)

					// Download image
					imageData, err := h.client.DownloadFile(file.URL)
					if err != nil {
						log.Warnw("Failed to download image",
							"name", file.Name,
							"url", file.URL,
							"error", err,
						)
						continue
					}

					// Check if data is valid
					if len(imageData) == 0 {
						log.Warnw("Image data is empty", "name", file.Name)
						continue
					}

					// Encode to base64
					base64Data := base64.StdEncoding.EncodeToString(imageData)
					images = append(images, map[string]any{
						"name":      file.Name,
						"mime_type": file.MIMEType,
						"data":      base64Data,
					})

					totalImagesDownloaded++
					totalImageBytes += len(imageData)

					log.Debugw("Image downloaded successfully",
						"name", file.Name,
						"raw_bytes", len(imageData),
					)
				}
			}
			if len(images) > 0 {
				msgData["images"] = images
			}
		}

		messageData[i] = msgData
	}

	// Log image processing summary
	if totalImagesFound > 0 {
		log.Infow("Thread images processed",
			"found", totalImagesFound,
			"downloaded", totalImagesDownloaded,
			"total_bytes", totalImageBytes,
			"skipped_limit", totalImagesFound-totalImagesDownloaded,
		)
	}

	// 4. Translate user IDs to names for better context
	// Extract all unique user IDs from messages
	userIDs := make([]string, 0, len(messages))
	for _, msg := range messages {
		if msg.User != "" {
			userIDs = append(userIDs, msg.User)
		}
	}

	// Fetch user names in batch
	userNames := h.client.GetUserNames(userIDs)

	// Update message data with resolved user names
	for i := range messageData {
		if userID, ok := messageData[i]["user"].(string); ok && userID != "" {
			if displayName, found := userNames[userID]; found {
				messageData[i]["user_name"] = displayName
			}
		}
	}

	log.Debugw("Thread user names resolved",
		"total_users", len(userIDs),
		"resolved", len(userNames),
	)

	queryRequest := map[string]any{
		"question":       message,
		"thread_ts":      threadTS,
		"channel_id":     event.Channel,
		"messages":       messageData,
		"user_name":      userName,     // @username for display
		"user_real_name": userRealName, // Real name for personalization
	}

	// 4. Send to Knowledge Agent
	forwardLogFields := []any{
		"channel_id", event.Channel,
	}
	if userName != "" {
		forwardLogFields = append(forwardLogFields, "user_name", userName)
	}
	log.Debugw("Forwarding to Knowledge Agent", forwardLogFields...)
	reqBody, err := json.Marshal(queryRequest)
	if err != nil {
		log.Errorw("Failed to marshal request", "error", err)
		h.client.PostMessage(event.Channel, threadTS,
			fmt.Sprintf("Error: Could not prepare request: %v", err))
		return
	}

	// Log payload size for debugging
	payloadSize := len(reqBody)
	log.Debugw("Request payload prepared",
		"size_bytes", payloadSize,
		"size_kb", payloadSize/1024,
	)

	// Create request with context for proper timeout handling
	req, err := http.NewRequestWithContext(ctx, "POST",
		fmt.Sprintf("%s/api/query", h.agentURL),
		bytes.NewBuffer(reqBody))
	if err != nil {
		log.Errorw("Failed to create request", "error", err)
		h.client.PostMessage(event.Channel, threadTS,
			fmt.Sprintf("Error: Could not create request: %v", err))
		return
	}

	req.Header.Set("Content-Type", "application/json")

	// Add internal token for authentication (if configured)
	if h.internalToken != "" {
		req.Header.Set("X-Internal-Token", h.internalToken)
	}

	// Add Slack user ID for traceability
	if event.User != "" {
		req.Header.Set("X-Slack-User-Id", event.User)
	}

	// Send request - context controls the timeout (5 min)
	// No Client.Timeout to avoid conflicts with context cancellation
	client := &http.Client{}
	resp, err := client.Do(req)

	if err != nil {
		// Distinguish different error types for better debugging
		errMsg := "Could not reach Knowledge Agent"
		if ctx.Err() != nil {
			log.Errorw("Request canceled or timed out",
				"error", err,
				"context_error", ctx.Err(),
				"payload_kb", payloadSize/1024,
			)
			errMsg = "Request timed out - the operation took too long"
		} else if strings.Contains(err.Error(), "EOF") {
			log.Errorw("Connection closed unexpectedly (EOF)",
				"error", err,
				"payload_kb", payloadSize/1024,
				"hint", "Server may have closed connection due to large payload or timeout",
			)
			errMsg = "Connection closed unexpectedly. The request may be too large or the server is overloaded"
		} else {
			log.Errorw("Failed to call Knowledge Agent",
				"error", err,
				"payload_kb", payloadSize/1024,
			)
		}
		observability.RecordAgentForward(false)
		h.client.PostMessage(event.Channel, threadTS,
			fmt.Sprintf("Error: %s: %v", errMsg, err))
		return
	}
	defer resp.Body.Close()

	// Record successful forward (will record error later if status != 200)
	if resp.StatusCode == http.StatusOK {
		observability.RecordAgentForward(true)
	} else {
		observability.RecordAgentForward(false)
	}

	// Log response status
	log.Debugw("Agent response received",
		"status_code", resp.StatusCode,
		"content_type", resp.Header.Get("Content-Type"),
	)

	// Check status code
	if resp.StatusCode != http.StatusOK {
		body := make([]byte, 1024)
		n, _ := resp.Body.Read(body)
		log.Errorw("Agent returned error status",
			"status_code", resp.StatusCode,
			"body_preview", string(body[:n]),
		)
		h.client.PostMessage(event.Channel, threadTS,
			fmt.Sprintf("Error: Knowledge Agent returned status %d", resp.StatusCode))
		return
	}

	// 4. Parse response
	var agentResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&agentResp); err != nil {
		log.Errorw("Failed to decode agent response",
			"error", err,
			"status_code", resp.StatusCode,
			"content_type", resp.Header.Get("Content-Type"),
		)
		h.client.PostMessage(event.Channel, threadTS,
			"Error: Invalid response format from Knowledge Agent")
		return
	}

	// 5. Send answer back to Slack
	success, ok := agentResp["success"].(bool)
	if !ok {
		log.Error("Invalid response format: missing or invalid 'success' field")
		h.client.PostMessage(event.Channel, threadTS,
			"Error: Invalid response format from Knowledge Agent")
		return
	}

	if success {
		answer, ok := agentResp["answer"].(string)
		if !ok {
			log.Error("Invalid response format: missing or invalid 'answer' field")
			h.client.PostMessage(event.Channel, threadTS,
				"Error: No answer received from Knowledge Agent")
			return
		}

		// Format the answer for Slack (convert markdown)
		formattedAnswer := FormatMessageForSlack(answer)

		log.Info("Agent responded successfully")
		h.client.PostMessage(event.Channel, threadTS, formattedAnswer)
	} else {
		errorMsg, ok := agentResp["message"].(string)
		if !ok {
			errorMsg = "Unknown error"
		}
		log.Warnw("Agent returned error", "error", errorMsg)

		// Format error message
		formattedError := FormatMessageForSlack(fmt.Sprintf("*Error:*\n%s", errorMsg))
		h.client.PostMessage(event.Channel, threadTS, formattedError)
	}
}

// stripBotMention removes bot mention from the message text
func stripBotMention(text string) string {
	// Remove bot mention (format: <@BOTID>)
	re := regexp.MustCompile(`<@[A-Z0-9]+>`)
	cleanText := re.ReplaceAllString(text, "")
	return strings.TrimSpace(cleanText)
}

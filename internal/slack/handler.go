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
	agentURL      string        // URL of the Knowledge Agent service
	internalToken string        // Internal token for authenticating with the Knowledge Agent
	botUserID     string        // Bot's own user ID (for filtering self-messages in DMs)
	ackGenerator  *AckGenerator // Generator for contextual acknowledgment messages
	httpClient    *http.Client  // Shared HTTP client for agent communication (thread-safe)
}

// NewHandler creates a new Slack event handler
func NewHandler(cfg *config.Config, agentURL string) *Handler {
	log := logger.Get()

	client := NewClient(ClientConfig{
		Token:           cfg.Slack.BotToken,
		MaxFileSize:     cfg.Slack.MaxFileSize,
		ThreadCacheTTL:  cfg.Slack.ThreadCacheTTL,
		ThreadCacheSize: cfg.Slack.ThreadCacheMaxSize,
	})

	h := &Handler{
		config:        cfg,
		client:        client,
		agentURL:      agentURL,
		internalToken: cfg.Auth.InternalToken,
		ackGenerator:  NewAckGenerator(cfg.Anthropic.APIKey, cfg.Slack.Ack),
		httpClient:    &http.Client{}, // Shared, reusable HTTP client (context controls timeout)
	}

	// Initialize bot user ID for DM filtering
	if err := h.initBotUserID(); err != nil {
		log.Warnw("Failed to get bot user ID (DM self-filtering may not work)",
			"error", err,
		)
	}

	return h
}

// initBotUserID fetches and stores the bot's own user ID
func (h *Handler) initBotUserID() error {
	log := logger.Get()

	authResp, err := h.client.api.AuthTest()
	if err != nil {
		return fmt.Errorf("auth.test failed: %w", err)
	}

	h.botUserID = authResp.UserID
	log.Infow("Bot user ID initialized", "bot_user_id", h.botUserID)
	return nil
}

// GetClient returns the underlying Slack client for use by other components
// This allows the async sub-agent tool to post messages directly to Slack
func (h *Handler) GetClient() *Client {
	return h.client
}

// ensureBotUserID ensures the bot user ID is initialized (lazy init with retry)
// This is called before processing DMs to handle cases where initial init failed
func (h *Handler) ensureBotUserID() {
	if h.botUserID != "" {
		return // Already initialized
	}

	log := logger.Get()
	log.Debug("Bot user ID not initialized, attempting lazy initialization")

	if err := h.initBotUserID(); err != nil {
		log.Warnw("Lazy initialization of bot user ID failed",
			"error", err,
		)
	}
}

// DMChannelPrefix is the prefix for direct message channel IDs in Slack
const DMChannelPrefix = "D"

// AckDelayThreshold is the minimum processing time before sending an acknowledgment
// If the response arrives faster than this, no ack is sent to avoid message spam
const AckDelayThreshold = 2 * time.Second

// isDMChannel checks if a channel ID represents a direct message channel
// DM channel IDs start with "D" (e.g., D01ABC123)
func isDMChannel(channelID string) bool {
	return strings.HasPrefix(channelID, DMChannelPrefix)
}

// isBotMessage checks if a message was sent by the bot itself
func (h *Handler) isBotMessage(userID string) bool {
	return h.botUserID != "" && userID == h.botUserID
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
		case *slackevents.MessageEvent:
			// Handle direct messages (DMs) - no @mention needed
			if h.shouldHandleDirectMessage(ev) {
				log.Infow("Webhook: DirectMessage detected",
					"user", ev.User,
					"channel", ev.Channel,
					"text", ev.Text,
				)
				observability.RecordSlackEvent("direct_message", true)
				go h.handleDirectMessage(ev)
			}
		default:
			log.Debugw("Unhandled event type", "type", innerEvent.Type)
			observability.RecordSlackEvent(innerEvent.Type, true)
		}
	}

	w.WriteHeader(http.StatusOK)
}

// scheduleProcessingAck schedules an acknowledgment message to be sent after AckDelayThreshold
// Returns a cancel function that should be called when processing completes to prevent the ack
// from being sent if the response arrived quickly (better UX - no message spam)
func (h *Handler) scheduleProcessingAck(channelID, threadTS, userMessage string) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		select {
		case <-time.After(AckDelayThreshold):
			// Processing is taking a while, send the ack
			h.sendProcessingAckNow(ctx, channelID, threadTS, userMessage)
		case <-ctx.Done():
			// Processing completed before threshold, no ack needed
		}
	}()

	return cancel
}

// sendProcessingAckNow sends the acknowledgment message immediately
func (h *Handler) sendProcessingAckNow(ctx context.Context, channelID, threadTS, userMessage string) {
	log := logger.Get()

	// Generate contextual ack message using Haiku
	ackMessage := h.ackGenerator.GenerateAck(ctx, userMessage)

	if err := h.client.PostMessage(channelID, threadTS, ackMessage); err != nil {
		// Don't fail the request if ack fails - just log warning and continue
		log.Warnw("Failed to send processing acknowledgment",
			"channel", channelID,
			"thread_ts", threadTS,
			"error", err,
		)
	}
}

// handleAppMention handles app mention events from Slack
func (h *Handler) handleAppMention(event *slackevents.AppMentionEvent) {
	log := logger.Get()
	log.Debugw("Processing app mention",
		"user", event.User,
		"channel", event.Channel,
	)

	// Determine thread timestamp for responses
	threadTS := event.ThreadTimeStamp
	if threadTS == "" {
		threadTS = event.TimeStamp
	}

	// Strip bot mention and get the user's message
	message := stripBotMention(event.Text)

	// Schedule acknowledgment (only sent if processing takes > AckDelayThreshold)
	cancelAck := h.scheduleProcessingAck(event.Channel, threadTS, message)
	defer cancelAck() // Cancel ack if response arrives quickly

	// Create context with timeout for async operations
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

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
	var userName, userRealName, userEmail string
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
		userEmail = userInfo.Email       // user@example.com (requires users:read.email scope)
		log.Debugw("User info fetched",
			"user_id", event.User,
			"name", userName,
			"real_name", userRealName,
			"has_email", userEmail != "",
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
			":warning: No pude obtener el contexto de este hilo. Por favor, intenta de nuevo.")
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
		"query":           message,
		"thread_ts":       threadTS,
		"channel_id":      event.Channel,
		"messages":        messageData,
		"user_name":       userName,     // @username for display
		"user_real_name":  userRealName, // Real name for personalization
		"user_email":      userEmail,    // Email for Keycloak identity propagation
		"filter_thinking": true,         // Exclude intermediate "thinking" text from Slack responses
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
			":gear: Tuve un problema preparando tu solicitud. Por favor, intenta de nuevo.")
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
			":gear: Tuve un problema preparando tu solicitud. Por favor, intenta de nuevo.")
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

	// Add user email for membership verification
	if userEmail != "" {
		req.Header.Set("X-User-Email", userEmail)
	}

	// Send request - context controls the timeout (5 min)
	// Using shared httpClient to avoid TCP connection leaks
	resp, err := h.httpClient.Do(req)

	if err != nil {
		// Log detailed technical error for debugging
		log.Errorw("Failed to call Knowledge Agent",
			"error", err,
			"context_error", ctx.Err(),
			"payload_kb", payloadSize/1024,
		)
		observability.RecordAgentForward(false)
		// Send user-friendly error message (no technical details)
		h.client.PostMessage(event.Channel, threadTS, FormatUserError(err, 0))
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
		n, readErr := resp.Body.Read(body)
		if readErr != nil && readErr != io.EOF {
			log.Warnw("Failed to read error response body", "error", readErr)
		}
		log.Errorw("Agent returned error status",
			"status_code", resp.StatusCode,
			"body_preview", string(body[:n]),
		)
		// Send user-friendly error message based on status code
		h.client.PostMessage(event.Channel, threadTS, FormatUserError(nil, resp.StatusCode))
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
			":gear: Recibi una respuesta que no pude procesar. Por favor, intenta de nuevo.")
		return
	}

	// 5. Send answer back to Slack
	success, ok := agentResp["success"].(bool)
	if !ok {
		log.Errorw("Invalid response format: missing or invalid 'success' field",
			"response_keys", getMapKeys(agentResp),
			"channel_id", event.Channel,
		)
		h.client.PostMessage(event.Channel, threadTS,
			":gear: Recibi una respuesta que no pude procesar. Por favor, intenta de nuevo.")
		return
	}

	if success {
		answer, ok := agentResp["answer"].(string)
		if !ok {
			log.Errorw("Invalid response format: missing or invalid 'answer' field",
				"response_keys", getMapKeys(agentResp),
				"channel_id", event.Channel,
			)
			h.client.PostMessage(event.Channel, threadTS,
				":thinking_face: No recibi una respuesta completa. Por favor, intenta de nuevo.")
			return
		}

		// Format the answer for Slack (convert markdown)
		formattedAnswer := FormatMessageForSlack(answer)

		log.Infow("Agent responded successfully",
			"channel_id", event.Channel,
			"answer_length", len(answer),
		)
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

// shouldHandleDirectMessage determines if a MessageEvent should be handled as a DM
func (h *Handler) shouldHandleDirectMessage(event *slackevents.MessageEvent) bool {
	// Only handle DM channels (channel_type "im" or channel ID starts with "D")
	if event.ChannelType != "im" && !isDMChannel(event.Channel) {
		return false
	}

	// Ensure bot user ID is initialized (lazy init if startup failed)
	h.ensureBotUserID()

	// Ignore messages from the bot itself
	if h.isBotMessage(event.User) {
		return false
	}

	// Ignore message subtypes (edits, deletes, bot_message, etc.)
	// We only want regular user messages
	if event.SubType != "" {
		return false
	}

	// Ignore empty messages
	if strings.TrimSpace(event.Text) == "" {
		return false
	}

	return true
}

// handleDirectMessage handles direct message events from Slack (no @mention needed)
func (h *Handler) handleDirectMessage(event *slackevents.MessageEvent) {
	log := logger.Get()
	log.Debugw("Processing direct message",
		"user", event.User,
		"channel", event.Channel,
	)

	// Determine thread timestamp for responses
	threadTS := event.ThreadTimeStamp
	if threadTS == "" {
		threadTS = event.TimeStamp
	}

	// Schedule acknowledgment (only sent if processing takes > AckDelayThreshold)
	cancelAck := h.scheduleProcessingAck(event.Channel, threadTS, event.Text)
	defer cancelAck() // Cancel ack if response arrives quickly

	// Create context with timeout for async operations
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Convert MessageEvent to AppMentionEvent-like data for sendToAgent
	// The message text doesn't need bot mention stripping in DMs
	appMentionEvent := &slackevents.AppMentionEvent{
		User:            event.User,
		Channel:         event.Channel,
		Text:            event.Text,
		TimeStamp:       event.TimeStamp,
		ThreadTimeStamp: event.ThreadTimeStamp,
	}

	// Send to agent
	h.sendToAgent(ctx, appMentionEvent, event.Text)

	log.Debugw("Direct message processed", "user", event.User)
}

// stripBotMention removes bot mention from the message text
func stripBotMention(text string) string {
	// Remove bot mention (format: <@BOTID>)
	re := regexp.MustCompile(`<@[A-Z0-9]+>`)
	cleanText := re.ReplaceAllString(text, "")
	return strings.TrimSpace(cleanText)
}

// getMapKeys returns the keys of a map for logging purposes
func getMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

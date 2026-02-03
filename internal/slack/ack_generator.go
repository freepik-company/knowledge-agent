package slack

import (
	"context"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"knowledge-agent/internal/logger"
)

const (
	// AckModel is the model used for generating acknowledgment messages
	AckModel = "claude-haiku-4-5-20251001"
	// AckTimeout is the maximum time to wait for ack generation
	AckTimeout = 2 * time.Second
	// AckMaxTokens is the maximum tokens for the ack response
	AckMaxTokens = 50
)

// AckGenerator generates contextual acknowledgment messages using Haiku
type AckGenerator struct {
	client  anthropic.Client
	enabled bool
}

// NewAckGenerator creates a new acknowledgment generator
func NewAckGenerator(apiKey string) *AckGenerator {
	if apiKey == "" {
		return &AckGenerator{enabled: false}
	}

	client := anthropic.NewClient(option.WithAPIKey(apiKey))
	return &AckGenerator{
		client:  client,
		enabled: true,
	}
}

// GenerateAck generates a contextual acknowledgment message based on the user's input
// Returns a fallback message if generation fails
func (g *AckGenerator) GenerateAck(ctx context.Context, userMessage string) string {
	log := logger.Get()

	if g == nil || !g.enabled {
		return defaultAckMessage()
	}

	// Create a context with timeout for fast response
	ctx, cancel := context.WithTimeout(ctx, AckTimeout)
	defer cancel()

	prompt := fmt.Sprintf(`Generate a short and natural response (max 15 words) to indicate you are processing this message.
Be casual and friendly. Use appropriate emojis. Don't use "Processing" or generic phrases.
Respond ONLY with the message, no explanations.
IMPORTANT: Respond in the SAME LANGUAGE as the user's message.

Examples:
- User: "The ai-audio logs are throwing 5xx" → ":eyes: Let me check what's up with ai-audio..."
- User: "How do we deploy to production?" → ":thinking_face: Let me find info about deployments..."
- User: "There's an error in the payment service" → ":mag: Investigating the payment service..."

User message: %s`, userMessage)

	message, err := g.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(AckModel),
		MaxTokens: AckMaxTokens,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})

	if err != nil {
		log.Debugw("Failed to generate contextual ack, using fallback",
			"error", err,
		)
		return defaultAckMessage()
	}

	// Extract text from response
	for _, block := range message.Content {
		if block.Type == "text" {
			return block.Text
		}
	}

	return defaultAckMessage()
}

// defaultAckMessage returns the fallback acknowledgment message
func defaultAckMessage() string {
	return ":mag: Give me a moment..."
}

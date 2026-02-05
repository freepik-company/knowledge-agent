package slack

import (
	"context"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"knowledge-agent/internal/config"
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

// DefaultAckPrompt is the default prompt used for generating acknowledgment messages
const DefaultAckPrompt = `Generate a short and natural response (max 15 words) to indicate you are processing this message.
Be casual and friendly. Use appropriate emojis. Don't use "Processing" or generic phrases.
Respond ONLY with the message, no explanations.
IMPORTANT: Respond in the SAME LANGUAGE as the user's message.

Examples:
- User: "The ai-audio logs are throwing 5xx" → ":eyes: Let me check what's up with ai-audio..."
- User: "How do we deploy to production?" → ":thinking_face: Let me find info about deployments..."
- User: "There's an error in the payment service" → ":mag: Investigating the payment service..."

User message: %s`

// DefaultAckMessage is the default fallback message when ack generation fails
const DefaultAckMessage = ":mag: Give me a moment..."

// AckGenerator generates contextual acknowledgment messages using Haiku
type AckGenerator struct {
	client         anthropic.Client
	enabled        bool
	prompt         string
	defaultMessage string
}

// NewAckGenerator creates a new acknowledgment generator
func NewAckGenerator(apiKey string, ackCfg config.AckConfig) *AckGenerator {
	// Determine the default message
	defaultMsg := ackCfg.DefaultMessage
	if defaultMsg == "" {
		defaultMsg = DefaultAckMessage
	}

	// If API key is missing or ack generation is explicitly disabled
	if apiKey == "" || !ackCfg.Enabled {
		return &AckGenerator{
			enabled:        false,
			defaultMessage: defaultMsg,
		}
	}

	// Determine the prompt to use
	prompt := ackCfg.Prompt
	if prompt == "" {
		prompt = DefaultAckPrompt
	}

	client := anthropic.NewClient(option.WithAPIKey(apiKey))
	return &AckGenerator{
		client:         client,
		enabled:        true,
		prompt:         prompt,
		defaultMessage: defaultMsg,
	}
}

// GenerateAck generates a contextual acknowledgment message based on the user's input
// Returns a fallback message if generation fails
func (g *AckGenerator) GenerateAck(ctx context.Context, userMessage string) string {
	log := logger.Get()

	if g == nil || !g.enabled {
		return g.getDefaultMessage()
	}

	// Create a context with timeout for fast response
	ctx, cancel := context.WithTimeout(ctx, AckTimeout)
	defer cancel()

	// Build prompt with user message
	prompt := fmt.Sprintf(g.prompt, userMessage)

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
		return g.getDefaultMessage()
	}

	// Extract text from response
	for _, block := range message.Content {
		if block.Type == "text" {
			return block.Text
		}
	}

	return g.getDefaultMessage()
}

// getDefaultMessage returns the configured fallback acknowledgment message
func (g *AckGenerator) getDefaultMessage() string {
	if g == nil || g.defaultMessage == "" {
		return DefaultAckMessage
	}
	return g.defaultMessage
}

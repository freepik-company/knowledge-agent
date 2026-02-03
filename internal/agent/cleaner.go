package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"knowledge-agent/internal/config"
	"knowledge-agent/internal/logger"
)

const cleanerPrompt = `Your task is to clean this AI agent response by removing unnecessary narration and debugging data.

REMOVE:
- Phrases about agent transfers ("I will transfer you", "the metrics agent says", "I will consult")
- Redundant or repeated greetings
- Explanations about which tool will be used
- Repetitions of the same information
- Meta-comments about the process ("let me search", "I will verify")
- YAML or JSON debugging blocks at the start of the response (context:, discovered:, results:, observations:, errors:)
- Internal structured data not meant for the end user

KEEP INTACT:
- User-formatted text (tables, lists, explanations)
- All substantive information, data, and figures IN READABLE FORMAT
- Relevant context to understand the response
- Important technical details presented clearly
- Follow-up questions to the user (if any)

IMPORTANT:
- Respond ONLY with the cleaned text
- Do NOT add explanations about what you removed
- Keep the same language as the original response
- If there is a YAML/JSON block followed by formatted text, return ONLY the formatted text
- If the response is already clean, return it unchanged

Response to clean:
%s`

// ResponseCleaner cleans agent responses before sending to users
type ResponseCleaner struct {
	client  anthropic.Client
	model   string
	enabled bool
}

// NewResponseCleaner creates a new response cleaner
func NewResponseCleaner(cfg *config.Config) *ResponseCleaner {
	if !cfg.ResponseCleaner.Enabled {
		return &ResponseCleaner{enabled: false}
	}

	// Validate API key exists before creating client
	if cfg.Anthropic.APIKey == "" {
		log := logger.Get()
		log.Warn("Response cleaner disabled: ANTHROPIC_API_KEY not set")
		return &ResponseCleaner{enabled: false}
	}

	client := anthropic.NewClient(
		option.WithAPIKey(cfg.Anthropic.APIKey),
	)

	model := cfg.ResponseCleaner.Model
	if model == "" {
		model = "claude-haiku-4-5-20251001"
	}

	return &ResponseCleaner{
		client:  client,
		model:   model,
		enabled: true,
	}
}

// Clean cleans the response by removing agent narration
func (rc *ResponseCleaner) Clean(ctx context.Context, response string) (string, error) {
	log := logger.Get()

	if !rc.enabled {
		return response, nil
	}

	// Skip cleaning for short responses (likely already clean)
	if len(response) < 200 {
		log.Debugw("Response too short, skipping cleaning", "length", len(response))
		return response, nil
	}

	log.Debugw("Cleaning response with Haiku",
		"original_length", len(response),
		"model", rc.model,
	)

	startTime := time.Now()

	prompt := fmt.Sprintf(cleanerPrompt, response)

	// Add timeout to prevent indefinite blocking
	cleanCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	message, err := rc.client.Messages.New(cleanCtx, anthropic.MessageNewParams{
		Model:     anthropic.Model(rc.model),
		MaxTokens: 4096,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		log.Warnw("Failed to clean response, returning original",
			"error", err,
		)
		return response, nil // Return original on error, don't fail the request
	}

	// Extract text from response
	var cleanedResponse string
	for _, block := range message.Content {
		if block.Type == "text" {
			cleanedResponse += block.Text
		}
	}

	// Validate cleaner didn't return empty response
	if cleanedResponse == "" {
		log.Warnw("Cleaner returned empty response, using original",
			"original_length", len(response),
		)
		return response, nil
	}

	duration := time.Since(startTime)
	log.Infow("Response cleaned",
		"original_length", len(response),
		"cleaned_length", len(cleanedResponse),
		"duration_ms", duration.Milliseconds(),
		"input_tokens", message.Usage.InputTokens,
		"output_tokens", message.Usage.OutputTokens,
	)

	return cleanedResponse, nil
}

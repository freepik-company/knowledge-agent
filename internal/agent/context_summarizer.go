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

const summarizerPrompt = `Compress this conversation context while preserving critical information.

PRESERVE (keep exactly as written):
- Decisions and conclusions reached
- Technical details: configs, IPs, ports, service names, versions
- Error messages and their resolutions
- Numerical data, metrics, and statistics
- Code snippets, commands, and file paths
- Names, dates, and deadlines mentioned
- Action items and commitments

REMOVE:
- Repetitive greetings and pleasantries
- Redundant back-and-forth exchanges
- Filler text and conversational padding
- Duplicate information
- Meta-discussion about the conversation itself

IMPORTANT:
- Output ONLY the compressed context, no explanations
- Maintain the original language (Spanish, English, etc.)
- Target approximately 50%% of the original size
- Keep the chronological flow of events
- Use bullet points for clarity when appropriate

Context to compress:
%s`

// ContextSummarizer summarizes long conversation contexts before sending to the LLM
type ContextSummarizer struct {
	client         anthropic.Client
	model          string
	tokenThreshold int
	enabled        bool
}

// NewContextSummarizer creates a new context summarizer
func NewContextSummarizer(cfg *config.Config) *ContextSummarizer {
	if !cfg.ContextSummarizer.Enabled {
		return &ContextSummarizer{enabled: false}
	}

	// Validate API key exists before creating client
	if cfg.Anthropic.APIKey == "" {
		log := logger.Get()
		log.Warn("Context summarizer disabled: ANTHROPIC_API_KEY not set")
		return &ContextSummarizer{enabled: false}
	}

	client := anthropic.NewClient(
		option.WithAPIKey(cfg.Anthropic.APIKey),
	)

	model := cfg.ContextSummarizer.Model
	if model == "" {
		model = "claude-haiku-4-5-20251001"
	}

	tokenThreshold := cfg.ContextSummarizer.TokenThreshold
	if tokenThreshold <= 0 {
		tokenThreshold = 8000
	}

	return &ContextSummarizer{
		client:         client,
		model:          model,
		tokenThreshold: tokenThreshold,
		enabled:        true,
	}
}

// EstimateTokens provides a rough estimate of token count
// Uses approximation of ~4 characters per token for English/Spanish text
func (cs *ContextSummarizer) EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	// Rough estimate: 1 token ≈ 4 characters for mixed text
	return len(text) / 4
}

// ShouldSummarize determines if the context exceeds the token threshold
func (cs *ContextSummarizer) ShouldSummarize(context string) bool {
	if !cs.enabled {
		return false
	}
	return cs.EstimateTokens(context) > cs.tokenThreshold
}

// Summarize compresses the context using Claude Haiku
func (cs *ContextSummarizer) Summarize(ctx context.Context, contextStr string) (string, error) {
	log := logger.Get()

	if !cs.enabled {
		return contextStr, nil
	}

	originalTokens := cs.EstimateTokens(contextStr)

	// Skip if below threshold
	if originalTokens <= cs.tokenThreshold {
		log.Debugw("Context below threshold, skipping summarization",
			"estimated_tokens", originalTokens,
			"threshold", cs.tokenThreshold,
		)
		return contextStr, nil
	}

	log.Infow("Summarizing context with Haiku",
		"original_length", len(contextStr),
		"estimated_tokens", originalTokens,
		"threshold", cs.tokenThreshold,
		"model", cs.model,
	)

	startTime := time.Now()

	prompt := fmt.Sprintf(summarizerPrompt, contextStr)

	// Add timeout to prevent indefinite blocking
	summarizeCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	message, err := cs.client.Messages.New(summarizeCtx, anthropic.MessageNewParams{
		Model:     anthropic.Model(cs.model),
		MaxTokens: 8192, // Allow enough tokens for compressed output
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		log.Warnw("Failed to summarize context, using original",
			"error", err,
			"original_length", len(contextStr),
		)
		return contextStr, nil // Return original on error, don't fail the request
	}

	// Extract text from response
	var summarizedContext string
	for _, block := range message.Content {
		if block.Type == "text" {
			summarizedContext += block.Text
		}
	}

	// Validate summarizer didn't return empty response
	if summarizedContext == "" {
		log.Warnw("Summarizer returned empty response, using original",
			"original_length", len(contextStr),
		)
		return contextStr, nil
	}

	duration := time.Since(startTime)
	summarizedTokens := cs.EstimateTokens(summarizedContext)
	compressionRatio := float64(len(summarizedContext)) / float64(len(contextStr)) * 100

	log.Infow("Context summarized",
		"original_length", len(contextStr),
		"summarized_length", len(summarizedContext),
		"original_tokens", originalTokens,
		"summarized_tokens", summarizedTokens,
		"compression_ratio", fmt.Sprintf("%.1f%%", compressionRatio),
		"duration_ms", duration.Milliseconds(),
		"input_tokens", message.Usage.InputTokens,
		"output_tokens", message.Usage.OutputTokens,
	)

	return summarizedContext, nil
}

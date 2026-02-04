package a2a

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"knowledge-agent/internal/config"
	"knowledge-agent/internal/logger"
	"knowledge-agent/internal/observability"
)

const (
	defaultQueryExtractorModel = "claude-haiku-4-5-20251001"

	// queryExtractorPromptWithDescription is used when the agent-card has a description
	// This allows for more targeted query extraction based on what the agent does
	queryExtractorPromptWithDescription = `You are extracting the relevant query for the "%s" agent.

Agent Purpose (from agent-card):
%s

From this conversation context, extract ONLY the specific request/query that is relevant for this agent.
Focus on what this agent can actually do based on its purpose.
Output ONLY the extracted query, nothing else. No preamble, no explanation.

Context:
---
%s
---

Extracted query:`

	// queryExtractorPromptGeneric is the fallback when no agent description is available
	queryExtractorPromptGeneric = `You are a task summarizer. Your job is to extract and condense the essential task from a conversation context.

Given the following conversation or task description, create a clear, concise summary that:
1. Identifies the main task or question being asked
2. Includes only the essential context needed to complete the task
3. Removes any redundant information or conversation history
4. Preserves specific details like names, dates, or technical terms that are relevant

Output ONLY the summarized task/question in 1-3 sentences. Do not include any preamble or explanation.

Context to summarize:
%s`
)

// queryExtractorInterceptor implements a2aclient.CallInterceptor to summarize
// context before sending to sub-agents. This reduces token consumption and
// improves sub-agent performance by providing focused context.
type queryExtractorInterceptor struct {
	a2aclient.PassthroughInterceptor
	agentName        string
	agentDescription string // From agent-card, used for targeted query extraction
	client           anthropic.Client
	model            string
	enabled          bool
}

// NewQueryExtractorInterceptor creates a new query extractor interceptor.
// It reads the Anthropic API key from ANTHROPIC_API_KEY env var.
// The agentDescription parameter comes from the resolved agent-card and enables
// more targeted query extraction based on what the agent actually does.
func NewQueryExtractorInterceptor(agentName, agentDescription string, cfg config.A2AQueryExtractorConfig) *queryExtractorInterceptor {
	log := logger.Get()

	model := cfg.Model
	if model == "" {
		model = defaultQueryExtractorModel
	}

	// Get API key from environment
	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	if anthropicKey == "" {
		log.Warnw("Query extractor disabled: ANTHROPIC_API_KEY not set",
			"agent", agentName,
		)
		return &queryExtractorInterceptor{
			agentName:        agentName,
			agentDescription: agentDescription,
			enabled:          false,
		}
	}

	client := anthropic.NewClient(option.WithAPIKey(anthropicKey))

	log.Debugw("Query extractor initialized",
		"agent", agentName,
		"has_description", agentDescription != "",
		"description_len", len(agentDescription),
	)

	return &queryExtractorInterceptor{
		agentName:        agentName,
		agentDescription: agentDescription,
		client:           client,
		model:            model,
		enabled:          true,
	}
}

// Before intercepts the request and summarizes the context before sending to sub-agent.
// IMPORTANT: We modify the *a2a.MessageSendParams directly because the a2aclient
// uses the original pointer, not req.Payload, when calling the transport.
func (ci *queryExtractorInterceptor) Before(ctx context.Context, req *a2aclient.Request) (context.Context, error) {
	log := logger.Get()

	// Skip if not enabled
	if !ci.enabled {
		return ctx, nil
	}

	// Skip if no payload
	if req.Payload == nil {
		return ctx, nil
	}

	// Type assert to *a2a.MessageSendParams - this is what SendMessage receives
	params, ok := req.Payload.(*a2a.MessageSendParams)
	if !ok {
		log.Debugw("Query extractor skipped: payload is not *a2a.MessageSendParams",
			"agent", ci.agentName,
			"payload_type", fmt.Sprintf("%T", req.Payload),
		)
		return ctx, nil
	}

	// Skip if no message
	if params.Message == nil || len(params.Message.Parts) == 0 {
		log.Debugw("Query extractor skipped: no message or parts",
			"agent", ci.agentName,
		)
		return ctx, nil
	}

	// Extract all text from parts
	// Note: Parts are stored as values (a2a.TextPart), not pointers (*a2a.TextPart)
	var texts []string
	for _, part := range params.Message.Parts {
		if textPart, ok := part.(a2a.TextPart); ok && textPart.Text != "" {
			texts = append(texts, textPart.Text)
		}
	}

	originalText := strings.Join(texts, "\n")
	if originalText == "" {
		log.Debugw("Query extractor skipped: no text found in parts",
			"agent", ci.agentName,
		)
		return ctx, nil
	}

	originalLen := len(originalText)
	originalPartsCount := len(params.Message.Parts)

	log.Debugw("Query extractor: inspecting payload",
		"agent", ci.agentName,
		"parts_count", originalPartsCount,
		"text_length", originalLen,
		"text_preview", truncateString(originalText, 200),
	)

	// Call Haiku to summarize the context
	summarized, err := ci.summarizeContext(ctx, originalText)
	if err != nil {
		// Graceful degradation: log warning and continue with original
		log.Warnw("Query extractor failed, using original payload",
			"agent", ci.agentName,
			"error", err,
		)
		return ctx, nil
	}

	// Only replace if the summary is shorter
	if len(summarized) >= originalLen {
		log.Debugw("Query extractor skipped: summary not shorter than original",
			"agent", ci.agentName,
			"original_length", originalLen,
			"summary_length", len(summarized),
		)
		return ctx, nil
	}

	// CRITICAL: Modify the Message.Parts directly on the original pointer
	// This ensures the modification is used by the transport
	// Note: Parts must be values (a2a.TextPart), not pointers (*a2a.TextPart)
	params.Message.Parts = a2a.ContentParts{
		a2a.TextPart{
			Text: summarized,
		},
	}

	log.Infow("Query extracted for sub-agent",
		"agent", ci.agentName,
		"original_length", originalLen,
		"original_parts", originalPartsCount,
		"cleaned_length", len(summarized),
		"cleaned_parts", 1,
		"reduction_percent", fmt.Sprintf("%.1f%%", float64(originalLen-len(summarized))/float64(originalLen)*100),
		"cleaned_text", summarized,
	)

	// Record A2A call in Langfuse trace if available
	if qt := observability.QueryTraceFromContext(ctx); qt != nil {
		qt.RecordA2ACall(ci.agentName, originalText, summarized, 0)
	}

	return ctx, nil
}

// summarizeContext calls Haiku to create a concise summary of the context
// If agentDescription is available, uses a targeted prompt for better extraction
func (ci *queryExtractorInterceptor) summarizeContext(ctx context.Context, text string) (string, error) {
	var prompt string
	if ci.agentDescription != "" {
		// Use targeted prompt with agent description from agent-card
		prompt = fmt.Sprintf(queryExtractorPromptWithDescription, ci.agentName, ci.agentDescription, text)
	} else {
		// Fallback to generic summarization
		prompt = fmt.Sprintf(queryExtractorPromptGeneric, text)
	}

	// Add timeout to prevent indefinite blocking (10s is enough for summarization)
	cleanCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	message, err := ci.client.Messages.New(cleanCtx, anthropic.MessageNewParams{
		Model:     anthropic.Model(ci.model),
		MaxTokens: 500,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to summarize context: %w", err)
	}

	// Extract text from response
	var result strings.Builder
	for _, block := range message.Content {
		if block.Type == "text" {
			result.WriteString(block.Text)
		}
	}

	return strings.TrimSpace(result.String()), nil
}

// truncateString truncates a string to maxLen and adds "..." if truncated
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

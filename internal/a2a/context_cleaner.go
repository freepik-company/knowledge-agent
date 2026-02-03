package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"knowledge-agent/internal/config"
	"knowledge-agent/internal/logger"
)

const (
	defaultContextCleanerModel = "claude-haiku-4-5-20251001"

	// contextCleanerPromptWithDescription is used when the agent-card has a description
	// This allows for more targeted query extraction based on what the agent does
	contextCleanerPromptWithDescription = `You are extracting the relevant query for the "%s" agent.

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

	// contextCleanerPromptGeneric is the fallback when no agent description is available
	contextCleanerPromptGeneric = `You are a task summarizer. Your job is to extract and condense the essential task from a conversation context.

Given the following conversation or task description, create a clear, concise summary that:
1. Identifies the main task or question being asked
2. Includes only the essential context needed to complete the task
3. Removes any redundant information or conversation history
4. Preserves specific details like names, dates, or technical terms that are relevant

Output ONLY the summarized task/question in 1-3 sentences. Do not include any preamble or explanation.

Context to summarize:
%s`
)

// contextCleanerInterceptor implements a2aclient.CallInterceptor to summarize
// context before sending to sub-agents. This reduces token consumption and
// improves sub-agent performance by providing focused context.
type contextCleanerInterceptor struct {
	a2aclient.PassthroughInterceptor
	agentName        string
	agentDescription string // From agent-card, used for targeted query extraction
	client           anthropic.Client
	model            string
	enabled          bool
}

// NewContextCleanerInterceptor creates a new context cleaner interceptor.
// It reads the Anthropic API key from ANTHROPIC_API_KEY env var.
// The agentDescription parameter comes from the resolved agent-card and enables
// more targeted query extraction based on what the agent actually does.
func NewContextCleanerInterceptor(agentName, agentDescription string, cfg config.A2AContextCleanerConfig) *contextCleanerInterceptor {
	log := logger.Get()

	model := cfg.Model
	if model == "" {
		model = defaultContextCleanerModel
	}

	// Get API key from environment
	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	if anthropicKey == "" {
		log.Warnw("Context cleaner disabled: ANTHROPIC_API_KEY not set",
			"agent", agentName,
		)
		return &contextCleanerInterceptor{
			agentName:        agentName,
			agentDescription: agentDescription,
			enabled:          false,
		}
	}

	client := anthropic.NewClient(option.WithAPIKey(anthropicKey))

	log.Debugw("Context cleaner initialized",
		"agent", agentName,
		"has_description", agentDescription != "",
		"description_len", len(agentDescription),
	)

	return &contextCleanerInterceptor{
		agentName:        agentName,
		agentDescription: agentDescription,
		client:           client,
		model:            model,
		enabled:          true,
	}
}

// Before intercepts the request and summarizes the context before sending to sub-agent
func (ci *contextCleanerInterceptor) Before(ctx context.Context, req *a2aclient.Request) (context.Context, error) {
	log := logger.Get()

	// Skip if not enabled
	if !ci.enabled {
		return ctx, nil
	}

	// Skip if no payload
	if req.Payload == nil {
		return ctx, nil
	}

	// Extract text from the A2A payload
	originalText := ci.extractTextFromPayload(req.Payload)
	if originalText == "" {
		log.Debugw("Context cleaner skipped: no text found in payload",
			"agent", ci.agentName,
		)
		return ctx, nil
	}

	originalLen := len(originalText)

	// Call Haiku to summarize the context
	summarized, err := ci.summarizeContext(ctx, originalText)
	if err != nil {
		// Graceful degradation: log warning and continue with original
		log.Warnw("Context cleaner failed, using original payload",
			"agent", ci.agentName,
			"error", err,
		)
		return ctx, nil
	}

	// Only replace if the summary is shorter
	if len(summarized) >= originalLen {
		log.Debugw("Context cleaner skipped: summary not shorter than original",
			"agent", ci.agentName,
			"original_length", originalLen,
			"summary_length", len(summarized),
		)
		return ctx, nil
	}

	// Replace the payload with the summarized version
	newPayload, replaced := ci.replaceTextInPayload(req.Payload, summarized)
	if replaced {
		req.Payload = newPayload
		log.Infow("Context cleaned for sub-agent",
			"agent", ci.agentName,
			"original_length", originalLen,
			"cleaned_length", len(summarized),
			"reduction_percent", fmt.Sprintf("%.1f%%", float64(originalLen-len(summarized))/float64(originalLen)*100),
		)
	} else {
		log.Debugw("Context cleaner: could not replace text in payload",
			"agent", ci.agentName,
		)
	}

	return ctx, nil
}

// summarizeContext calls Haiku to create a concise summary of the context
// If agentDescription is available, uses a targeted prompt for better extraction
func (ci *contextCleanerInterceptor) summarizeContext(ctx context.Context, text string) (string, error) {
	var prompt string
	if ci.agentDescription != "" {
		// Use targeted prompt with agent description from agent-card
		prompt = fmt.Sprintf(contextCleanerPromptWithDescription, ci.agentName, ci.agentDescription, text)
	} else {
		// Fallback to generic summarization
		prompt = fmt.Sprintf(contextCleanerPromptGeneric, text)
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

// extractTextFromPayload extracts text content from an A2A payload.
// The payload can be various A2A message types.
func (ci *contextCleanerInterceptor) extractTextFromPayload(payload any) string {
	log := logger.Get()

	// Convert payload to JSON for inspection
	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return ""
	}

	// Log the payload structure for debugging
	log.Debugw("Context cleaner: inspecting payload structure",
		"agent", ci.agentName,
		"payload_json", string(jsonBytes),
	)

	// Parse as generic map to find text content
	var data map[string]any
	if err := json.Unmarshal(jsonBytes, &data); err != nil {
		return ""
	}

	// Look for common A2A message structures
	// A2A typically uses: message.parts[].text or params.message.parts[].text
	text := ci.findTextInMap(data)

	log.Debugw("Context cleaner: extracted text",
		"agent", ci.agentName,
		"text_length", len(text),
		"text_preview", truncateString(text, 200),
	)

	return text
}

// truncateString truncates a string to maxLen and adds "..." if truncated
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// findTextInMap recursively searches for text content in a map structure
func (ci *contextCleanerInterceptor) findTextInMap(data map[string]any) string {
	var texts []string

	// Direct "text" field
	if text, ok := data["text"].(string); ok && text != "" {
		texts = append(texts, text)
	}

	// Check "parts" array (A2A message format)
	if parts, ok := data["parts"].([]any); ok {
		for _, part := range parts {
			if partMap, ok := part.(map[string]any); ok {
				if text, ok := partMap["text"].(string); ok && text != "" {
					texts = append(texts, text)
				}
			}
		}
	}

	// Check nested "message" object (standard A2A format)
	if message, ok := data["message"].(map[string]any); ok {
		if text := ci.findTextInMap(message); text != "" {
			texts = append(texts, text)
		}
	}

	// Check nested "new_message" object (ADK Launcher format)
	if newMessage, ok := data["new_message"].(map[string]any); ok {
		if text := ci.findTextInMap(newMessage); text != "" {
			texts = append(texts, text)
		}
	}

	// Check nested "params" object
	if params, ok := data["params"].(map[string]any); ok {
		if text := ci.findTextInMap(params); text != "" {
			texts = append(texts, text)
		}
	}

	return strings.Join(texts, "\n")
}

// replaceTextInPayload replaces text content in an A2A payload with the summarized version.
// Returns the new payload and whether the replacement was successful.
func (ci *contextCleanerInterceptor) replaceTextInPayload(payload any, newText string) (any, bool) {
	// Convert to JSON, modify, and back
	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return payload, false
	}

	var data map[string]any
	if err := json.Unmarshal(jsonBytes, &data); err != nil {
		return payload, false
	}

	// Replace text in the structure
	if !ci.replaceTextInMap(data, newText) {
		return payload, false
	}

	// Return the modified map as the new payload
	return data, true
}

// replaceTextInMap replaces text content in a map structure with new text.
// Replaces ALL text parts with a single summarized text, removing redundant parts.
func (ci *contextCleanerInterceptor) replaceTextInMap(data map[string]any, newText string) bool {
	// Check "parts" array first (most common A2A format)
	if parts, ok := data["parts"].([]any); ok && len(parts) > 0 {
		// Replace first text part with summary, remove all other text parts
		newParts := make([]any, 0, 1)
		replaced := false
		for _, part := range parts {
			if partMap, ok := part.(map[string]any); ok {
				if _, hasText := partMap["text"]; hasText {
					if !replaced {
						// Keep only the first text part with summarized content
						partMap["text"] = newText
						newParts = append(newParts, partMap)
						replaced = true
					}
					// Skip other text parts (they're redundant after summarization)
				} else {
					// Keep non-text parts (files, etc.)
					newParts = append(newParts, part)
				}
			}
		}
		if replaced {
			data["parts"] = newParts
			return true
		}
	}

	// Check nested "message" object (standard A2A format)
	if message, ok := data["message"].(map[string]any); ok {
		if ci.replaceTextInMap(message, newText) {
			return true
		}
	}

	// Check nested "new_message" object (ADK Launcher format)
	if newMessage, ok := data["new_message"].(map[string]any); ok {
		if ci.replaceTextInMap(newMessage, newText) {
			return true
		}
	}

	// Check nested "params" object
	if params, ok := data["params"].(map[string]any); ok {
		if ci.replaceTextInMap(params, newText) {
			return true
		}
	}

	// Direct "text" field as fallback
	if _, ok := data["text"].(string); ok {
		data["text"] = newText
		return true
	}

	return false
}

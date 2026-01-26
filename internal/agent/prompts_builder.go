package agent

import (
	"fmt"
	"strings"

	"knowledge-agent/internal/config"
)

// BuildSystemPrompt generates the complete system prompt with agent name and permissions
// It personalizes the prompt with the configured agent name (e.g., "Anton", "Ghost", etc.)
// and injects permission rules for save_to_memory tool
func BuildSystemPrompt(basePrompt string, agentName string, permissionsConfig *config.PermissionsConfig) string {
	// Step 1: Personalize agent name
	prompt := personalizeAgentName(basePrompt, agentName)

	// Step 2: Inject permissions
	prompt = BuildSystemPromptWithPermissions(prompt, permissionsConfig)

	return prompt
}

// personalizeAgentName replaces the generic "Knowledge Management Assistant" with the configured name
func personalizeAgentName(basePrompt string, agentName string) string {
	if agentName == "" || agentName == "Knowledge Agent" {
		return basePrompt // Use default
	}

	// Replace the generic introduction with personalized name
	prompt := strings.Replace(
		basePrompt,
		"You are a Knowledge Management Assistant",
		fmt.Sprintf("You are %s, a Knowledge Management Assistant", agentName),
		1,
	)

	return prompt
}

// BuildSystemPromptWithPermissions generates the system prompt
// Note: Permissions are NOT injected into the prompt for security reasons
// The agent should attempt save_to_memory and handle permission errors gracefully
func BuildSystemPromptWithPermissions(basePrompt string, cfg *config.PermissionsConfig) string {
	// No longer inject permission lists into prompt
	// Permissions are enforced at the tool wrapper level
	// The agent will receive errors when permissions are denied
	return basePrompt
}

func buildPermissionSection(cfg *config.PermissionsConfig) string {
	// Deprecated - permissions are now enforced at tool wrapper level
	// This function is kept for backward compatibility but returns empty string
	return ""
}

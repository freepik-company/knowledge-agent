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

// BuildSystemPromptWithPermissions generates the system prompt with permission rules injected
// It takes a base prompt (from prompt manager or default) and injects permission rules
func BuildSystemPromptWithPermissions(basePrompt string, cfg *config.PermissionsConfig) string {
	// If permissions are configured, add restrictions
	if len(cfg.AllowedSlackUsers) > 0 || len(cfg.AdminCallerIDs) > 0 {
		permissionSection := buildPermissionSection(cfg)
		// Insert permission section after "When to SAVE" section
		insertPoint := "### When to SAVE (use save_to_memory):"
		basePrompt = strings.Replace(basePrompt, insertPoint, insertPoint+"\n"+permissionSection, 1)
	}

	return basePrompt
}

func buildPermissionSection(cfg *config.PermissionsConfig) string {
	var sb strings.Builder

	sb.WriteString("\n**IMPORTANT - SAVE PERMISSIONS:**\n")
	sb.WriteString("Before using save_to_memory, verify that the current user has permission:\n\n")

	if len(cfg.AdminCallerIDs) > 0 {
		sb.WriteString(fmt.Sprintf("- Admin caller IDs (always allowed): %s\n", strings.Join(cfg.AdminCallerIDs, ", ")))
	}

	if len(cfg.AllowedSlackUsers) > 0 {
		sb.WriteString(fmt.Sprintf("- Allowed Slack users: %s\n", strings.Join(cfg.AllowedSlackUsers, ", ")))
	}

	sb.WriteString("\nIf the current request is NOT from an allowed user or admin caller:\n")
	sb.WriteString("- DO NOT use save_to_memory\n")
	sb.WriteString("- Respond politely: \"I'm sorry, but only authorized users can save information to the knowledge base.\"\n")
	sb.WriteString("- You can still use search_memory and fetch_url tools for all users\n\n")

	return sb.String()
}

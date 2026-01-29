package slack

import (
	"regexp"
	"strings"
)

// FormatMessageForSlack converts Claude's markdown to Slack's mrkdwn format
func FormatMessageForSlack(text string) string {
	// Convert markdown headers to bold (Slack doesn't support headers)
	// ## Header → *Header*
	// # Header → *Header*
	headerRegex := regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
	text = headerRegex.ReplaceAllString(text, "*$1*")

	// Convert **bold** to *bold* (Slack format)
	// This handles both inline and standalone bold text
	boldRegex := regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
	text = boldRegex.ReplaceAllString(text, "*$1*")

	// Note: We don't convert italic syntax because:
	// 1. Slack already supports _italic_ with underscores
	// 2. The previous regex was consuming boundary characters and losing text
	// 3. Code with underscores like `fotosgratis.re_og` was being corrupted
	// If Claude uses *italic* (single asterisks), Slack interprets it as bold,
	// but that's acceptable since we instructed Claude to use underscores for italic

	// Convert code blocks ```code``` to `code` (Slack doesn't support triple backticks well in basic messages)
	codeBlockRegex := regexp.MustCompile("```[\\s\\S]*?```")
	text = codeBlockRegex.ReplaceAllStringFunc(text, func(match string) string {
		// Remove the ``` and add single backticks
		code := strings.Trim(match, "`")
		code = strings.TrimSpace(code)
		return "`" + code + "`"
	})

	// Ensure lists are properly formatted
	// Convert "- item" or "* item" to "• item" for better Slack display
	listRegex := regexp.MustCompile(`(?m)^[\-\*]\s+`)
	text = listRegex.ReplaceAllString(text, "• ")

	// Convert numbered lists to have proper spacing
	numberedListRegex := regexp.MustCompile(`(?m)^(\d+)\.\s+`)
	text = numberedListRegex.ReplaceAllString(text, "$1. ")

	// Clean up excessive whitespace
	// Replace 3+ newlines with exactly 2 newlines
	text = regexp.MustCompile(`\n{3,}`).ReplaceAllString(text, "\n\n")

	// Trim leading/trailing whitespace
	text = strings.TrimSpace(text)

	return text
}

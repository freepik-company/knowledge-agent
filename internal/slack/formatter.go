package slack

import (
	"regexp"
	"strings"
)

// FormatMessageForSlack converts Claude's markdown to Slack's mrkdwn format
func FormatMessageForSlack(text string) string {
	// Convert markdown tables to readable format (must be done first)
	text = convertMarkdownTables(text)

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

// convertMarkdownTables converts markdown tables to a readable Slack format
// Example input:
//
//	| # | Error | Count |
//	|---|-------|-------|
//	| 1 | Error X | 163K |
//
// Example output:
//
//	*#* | *Error* | *Count*
//	1 | Error X | 163K
func convertMarkdownTables(text string) string {
	lines := strings.Split(text, "\n")
	var result []string
	var inTable bool
	var headers []string

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Check if this is a table line (starts and ends with |)
		if strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|") {
			// Check if this is a separator line (contains only |, -, :, and spaces)
			if isTableSeparator(trimmed) {
				// Skip separator lines
				continue
			}

			// Parse the cells
			cells := parseTableRow(trimmed)

			if !inTable {
				// This is the header row
				inTable = true
				headers = cells
				// Format headers as bold
				var boldHeaders []string
				for _, h := range headers {
					if h != "" {
						boldHeaders = append(boldHeaders, "*"+h+"*")
					}
				}
				result = append(result, strings.Join(boldHeaders, " | "))
			} else {
				// This is a data row
				result = append(result, strings.Join(cells, " | "))
			}
		} else {
			// Not a table line
			if inTable {
				// End of table, add a blank line for separation
				inTable = false
				headers = nil
			}
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

// isTableSeparator checks if a line is a markdown table separator (e.g., |---|---|)
func isTableSeparator(line string) bool {
	// Remove all valid separator characters
	cleaned := strings.ReplaceAll(line, "|", "")
	cleaned = strings.ReplaceAll(cleaned, "-", "")
	cleaned = strings.ReplaceAll(cleaned, ":", "")
	cleaned = strings.ReplaceAll(cleaned, " ", "")
	// If nothing remains, it's a separator
	return cleaned == ""
}

// parseTableRow extracts cells from a markdown table row
func parseTableRow(line string) []string {
	// Remove leading and trailing |
	line = strings.Trim(line, "|")
	// Split by |
	parts := strings.Split(line, "|")
	// Trim whitespace from each cell
	var cells []string
	for _, p := range parts {
		cells = append(cells, strings.TrimSpace(p))
	}
	return cells
}

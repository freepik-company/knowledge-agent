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
// Handles both formats:
//
//	Standard:  | # | Error | Count |
//	Compact:   # | Error | Count
//
// Output removes separator lines and makes headers bold
func convertMarkdownTables(text string) string {
	lines := strings.Split(text, "\n")
	var result []string
	i := 0

	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Check if this could be a table header (has | and content)
		if hasTableContent(trimmed) {
			// Look ahead to see if next line is a separator
			if i+1 < len(lines) && isTableSeparator(strings.TrimSpace(lines[i+1])) {
				// This is a table! Process header
				cells := parseTableRow(trimmed)
				var boldHeaders []string
				for _, h := range cells {
					h = strings.TrimSpace(h)
					if h != "" {
						boldHeaders = append(boldHeaders, "*"+h+"*")
					}
				}
				result = append(result, strings.Join(boldHeaders, " | "))
				i += 2 // Skip header and separator

				// Process data rows
				for i < len(lines) {
					dataLine := lines[i]
					dataTrimmed := strings.TrimSpace(dataLine)
					if hasTableContent(dataTrimmed) && !isTableSeparator(dataTrimmed) {
						dataCells := parseTableRow(dataTrimmed)
						result = append(result, strings.Join(dataCells, " | "))
						i++
					} else {
						break
					}
				}
				continue
			}
		}

		// Not a table line, keep as-is
		result = append(result, line)
		i++
	}

	return strings.Join(result, "\n")
}

// hasTableContent checks if a line has table content (| with actual text)
func hasTableContent(line string) bool {
	if !strings.Contains(line, "|") {
		return false
	}
	// Must have actual content, not just pipes/dashes/colons/spaces
	cleaned := strings.ReplaceAll(line, "|", "")
	cleaned = strings.ReplaceAll(cleaned, "-", "")
	cleaned = strings.ReplaceAll(cleaned, ":", "")
	cleaned = strings.TrimSpace(cleaned)
	return len(cleaned) > 0
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

package slack

import (
	"testing"
)

func TestFormatMessageForSlack(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Convert bold markdown",
			input:    "Sí, según la información que tengo guardada, **Alby Hernandez sí tiene barba**.",
			expected: "Sí, según la información que tengo guardada, *Alby Hernandez sí tiene barba*.",
		},
		{
			name:     "Convert multiple bold sections",
			input:    "**Name**: Alby\n**Has beard**: Yes",
			expected: "*Name*: Alby\n*Has beard*: Yes",
		},
		{
			name:     "Convert bullet lists",
			input:    "Items:\n- First item\n- Second item\n* Third item",
			expected: "Items:\n• First item\n• Second item\n• Third item",
		},
		{
			name:     "Convert numbered lists",
			input:    "Steps:\n1. First step\n2. Second step\n3. Third step",
			expected: "Steps:\n1. First step\n2. Second step\n3. Third step",
		},
		{
			name:     "Clean excessive whitespace",
			input:    "Line 1\n\n\n\nLine 2",
			expected: "Line 1\n\nLine 2",
		},
		{
			name:     "Trim leading and trailing whitespace",
			input:    "  \n  Text content  \n  ",
			expected: "Text content",
		},
		{
			name:     "Complex message with mixed formatting",
			input:    "Según la información guardada:\n\n**Alby Hernandez** tiene las siguientes características:\n- Barba completa\n- Color oscuro\n\nEspecíficamente, tiene una barba completa y bien cuidada.",
			expected: "Según la información guardada:\n\n*Alby Hernandez* tiene las siguientes características:\n• Barba completa\n• Color oscuro\n\nEspecíficamente, tiene una barba completa y bien cuidada.",
		},
		{
			name:     "Convert markdown headers",
			input:    "## Información General que tengo registrada:\n\nContent here",
			expected: "*Información General que tengo registrada:*\n\nContent here",
		},
		{
			name:     "Convert multiple header levels",
			input:    "# Title\n## Subtitle\n### Section",
			expected: "*Title*\n*Subtitle*\n*Section*",
		},
		{
			name:     "Convert code blocks",
			input:    "Here is code:\n```\nfunction test() {\n  return true;\n}\n```",
			expected: "Here is code:\n`function test() {\n  return true;\n}`",
		},
		{
			name:     "Preserve underscores in code",
			input:    "The domain is `fotosgratis.re_og` with underscore",
			expected: "The domain is `fotosgratis.re_og` with underscore",
		},
		{
			name:     "Preserve text with underscores - no character loss",
			input:    "Check out _this_ link: `fotosgratis.re_og`",
			expected: "Check out _this_ link: `fotosgratis.re_og`",
		},
		{
			name:     "Preserve variable names with underscores",
			input:    "The variable `user_name` and `user_id` are important",
			expected: "The variable `user_name` and `user_id` are important",
		},
		{
			name:     "Text with multiple underscores preserved",
			input:    "Config: `SOME_VAR_NAME_HERE` is set to `OTHER_VAR`",
			expected: "Config: `SOME_VAR_NAME_HERE` is set to `OTHER_VAR`",
		},
		{
			name:     "Convert simple markdown table",
			input:    "| Name | Value |\n|------|-------|\n| foo | bar |",
			expected: "*Name* | *Value*\nfoo | bar",
		},
		{
			name:     "Convert table with numbers",
			input:    "| # | Error | Count |\n|---|-------|-------|\n| 1 | Error X | 163K |\n| 2 | Error Y | 50K |",
			expected: "*#* | *Error* | *Count*\n1 | Error X | 163K\n2 | Error Y | 50K",
		},
		{
			name:     "Table with text before and after",
			input:    "Here are the results:\n\n| Column A | Column B |\n|----------|----------|\n| Value 1 | Value 2 |\n\nThat's all!",
			expected: "Here are the results:\n\n*Column A* | *Column B*\nValue 1 | Value 2\n\nThat's all!",
		},
		{
			name:     "Text without tables unchanged",
			input:    "No tables here, just text with | pipe character",
			expected: "No tables here, just text with | pipe character",
		},
		{
			name:     "Table with alignment markers",
			input:    "| Left | Center | Right |\n|:-----|:------:|------:|\n| L | C | R |",
			expected: "*Left* | *Center* | *Right*\nL | C | R",
		},
		{
			name:     "Table without leading/trailing pipes",
			input:    "# | Error | Count\n---|---|---\n1 | Error X | 163K\n2 | Error Y | 50K",
			expected: "*#* | *Error* | *Count*\n1 | Error X | 163K\n2 | Error Y | 50K",
		},
		{
			name:     "Table with percentage column",
			input:    "# | Error | Ocurrencias | % Aprox\n---|---|---|---\n1 | Validation exception | 163,678 | 29.8%",
			expected: "*#* | *Error* | *Ocurrencias* | *% Aprox*\n1 | Validation exception | 163,678 | 29.8%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatMessageForSlack(tt.input)
			if result != tt.expected {
				t.Errorf("FormatMessageForSlack() got:\n%q\n\nwant:\n%q", result, tt.expected)
			}
		})
	}
}

package slack

import (
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

func TestSplitMessage(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		maxLength  int
		wantChunks int
		verify     func(t *testing.T, chunks []string)
	}{
		{
			name:       "Short message - no split needed",
			text:       "Hello, world!",
			maxLength:  100,
			wantChunks: 1,
		},
		{
			name:       "Exact max length - no split needed",
			text:       strings.Repeat("a", 100),
			maxLength:  100,
			wantChunks: 1,
		},
		{
			name:       "Split at paragraph boundary",
			text:       strings.Repeat("a", 50) + "\n\n" + strings.Repeat("b", 50),
			maxLength:  60,
			wantChunks: 2,
			verify: func(t *testing.T, chunks []string) {
				if !strings.HasSuffix(chunks[0], "a") {
					t.Error("First chunk should end with 'a' characters")
				}
				if !strings.HasPrefix(chunks[1], "b") {
					t.Error("Second chunk should start with 'b' characters")
				}
			},
		},
		{
			name:       "Split at newline when no paragraph",
			text:       strings.Repeat("a", 50) + "\n" + strings.Repeat("b", 50),
			maxLength:  60,
			wantChunks: 2,
		},
		{
			name:       "Split at sentence boundary",
			text:       strings.Repeat("a", 40) + ". " + strings.Repeat("b", 40),
			maxLength:  50,
			wantChunks: 2,
		},
		{
			name:       "Split at word boundary",
			text:       strings.Repeat("a", 40) + " " + strings.Repeat("b", 40),
			maxLength:  50,
			wantChunks: 2,
		},
		{
			name:       "Multiple chunks needed",
			text:       strings.Repeat("Hello world. ", 100),
			maxLength:  100,
			wantChunks: 15, // 1300 chars / ~100 chars per chunk
			verify: func(t *testing.T, chunks []string) {
				for i, chunk := range chunks {
					if len(chunk) > 100 {
						t.Errorf("Chunk %d exceeds max length: %d > 100", i, len(chunk))
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks := splitMessage(tt.text, tt.maxLength)

			if len(chunks) != tt.wantChunks {
				t.Errorf("splitMessage() returned %d chunks, want %d", len(chunks), tt.wantChunks)
			}

			// Verify all chunks are within max length
			for i, chunk := range chunks {
				if len(chunk) > tt.maxLength {
					t.Errorf("Chunk %d exceeds max length: %d > %d", i, len(chunk), tt.maxLength)
				}
			}

			// Verify content is preserved (rejoin and compare)
			rejoined := strings.Join(chunks, "")
			// Remove spaces that were trimmed
			originalClean := strings.ReplaceAll(tt.text, "\n\n", "")
			originalClean = strings.ReplaceAll(originalClean, "\n", "")
			originalClean = strings.ReplaceAll(originalClean, " ", "")
			rejoinedClean := strings.ReplaceAll(rejoined, " ", "")

			if len(rejoinedClean) < len(originalClean)-len(chunks) {
				t.Errorf("Content may have been lost during split")
			}

			// Run custom verification if provided
			if tt.verify != nil {
				tt.verify(t, chunks)
			}
		})
	}
}

func TestExtractBlockText(t *testing.T) {
	tests := []struct {
		name     string
		blocks   slack.Blocks
		contains []string
		empty    bool
	}{
		{
			name: "HeaderBlock with title",
			blocks: slack.Blocks{
				BlockSet: []slack.Block{
					&slack.HeaderBlock{
						Type: slack.MBTHeader,
						Text: &slack.TextBlockObject{Type: "plain_text", Text: "Ticket: Server down"},
					},
				},
			},
			contains: []string{"Ticket: Server down"},
		},
		{
			name: "SectionBlock with text and fields",
			blocks: slack.Blocks{
				BlockSet: []slack.Block{
					&slack.SectionBlock{
						Type: slack.MBTSection,
						Text: &slack.TextBlockObject{Type: "mrkdwn", Text: "New request submitted"},
						Fields: []*slack.TextBlockObject{
							{Type: "mrkdwn", Text: "*Priority:*\nHigh"},
							{Type: "mrkdwn", Text: "*Assignee:*\nTeam A"},
						},
					},
				},
			},
			contains: []string{"New request submitted", "*Priority:*\nHigh", "*Assignee:*\nTeam A"},
		},
		{
			name: "RichTextBlock with text elements",
			blocks: slack.Blocks{
				BlockSet: []slack.Block{
					&slack.RichTextBlock{
						Type: slack.MBTRichText,
						Elements: []slack.RichTextElement{
							&slack.RichTextSection{
								Type: slack.RTESection,
								Elements: []slack.RichTextSectionElement{
									&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "Hello "},
									&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "world"},
								},
							},
						},
					},
				},
			},
			contains: []string{"Hello world"},
		},
		{
			name: "RichTextBlock with link element",
			blocks: slack.Blocks{
				BlockSet: []slack.Block{
					&slack.RichTextBlock{
						Type: slack.MBTRichText,
						Elements: []slack.RichTextElement{
							&slack.RichTextSection{
								Type: slack.RTESection,
								Elements: []slack.RichTextSectionElement{
									&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "See "},
									&slack.RichTextSectionLinkElement{Type: slack.RTSELink, URL: "https://example.com", Text: "docs"},
								},
							},
						},
					},
				},
			},
			contains: []string{"See docs"},
		},
		{
			name: "RichTextBlock link without text falls back to URL",
			blocks: slack.Blocks{
				BlockSet: []slack.Block{
					&slack.RichTextBlock{
						Type: slack.MBTRichText,
						Elements: []slack.RichTextElement{
							&slack.RichTextSection{
								Type: slack.RTESection,
								Elements: []slack.RichTextSectionElement{
									&slack.RichTextSectionLinkElement{Type: slack.RTSELink, URL: "https://example.com"},
								},
							},
						},
					},
				},
			},
			contains: []string{"https://example.com"},
		},
		{
			name: "RichTextList with items",
			blocks: slack.Blocks{
				BlockSet: []slack.Block{
					&slack.RichTextBlock{
						Type: slack.MBTRichText,
						Elements: []slack.RichTextElement{
							&slack.RichTextList{
								Type:  slack.RTEList,
								Style: "bullet",
								Elements: []slack.RichTextElement{
									&slack.RichTextSection{
										Type: slack.RTESection,
										Elements: []slack.RichTextSectionElement{
											&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "Item one"},
										},
									},
									&slack.RichTextSection{
										Type: slack.RTESection,
										Elements: []slack.RichTextSectionElement{
											&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "Item two"},
										},
									},
								},
							},
						},
					},
				},
			},
			contains: []string{"- Item one", "- Item two"},
		},
		{
			name: "ContextBlock with text elements",
			blocks: slack.Blocks{
				BlockSet: []slack.Block{
					&slack.ContextBlock{
						Type: slack.MBTContext,
						ContextElements: slack.ContextElements{
							Elements: []slack.MixedElement{
								&slack.TextBlockObject{Type: "mrkdwn", Text: "Submitted by John"},
							},
						},
					},
				},
			},
			contains: []string{"Submitted by John"},
		},
		{
			name:  "Empty blocks",
			blocks: slack.Blocks{BlockSet: []slack.Block{}},
			empty: true,
		},
		{
			name: "Mixed block types (workflow form pattern)",
			blocks: slack.Blocks{
				BlockSet: []slack.Block{
					&slack.HeaderBlock{
						Type: slack.MBTHeader,
						Text: &slack.TextBlockObject{Type: "plain_text", Text: "Bug Report"},
					},
					&slack.SectionBlock{
						Type: slack.MBTSection,
						Fields: []*slack.TextBlockObject{
							{Type: "mrkdwn", Text: "*Severity:*\nCritical"},
							{Type: "mrkdwn", Text: "*Component:*\nAuth"},
						},
					},
					&slack.ContextBlock{
						Type: slack.MBTContext,
						ContextElements: slack.ContextElements{
							Elements: []slack.MixedElement{
								&slack.TextBlockObject{Type: "mrkdwn", Text: "Created: 2024-01-15"},
							},
						},
					},
				},
			},
			contains: []string{"Bug Report", "*Severity:*\nCritical", "*Component:*\nAuth", "Created: 2024-01-15"},
		},
		{
			name: "SectionBlock with nil text and fields only",
			blocks: slack.Blocks{
				BlockSet: []slack.Block{
					&slack.SectionBlock{
						Type: slack.MBTSection,
						Text: nil,
						Fields: []*slack.TextBlockObject{
							{Type: "mrkdwn", Text: "Field only"},
						},
					},
				},
			},
			contains: []string{"Field only"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractBlockText(tt.blocks)

			if tt.empty {
				if result != "" {
					t.Errorf("expected empty result, got %q", result)
				}
				return
			}

			for _, want := range tt.contains {
				if !strings.Contains(result, want) {
					t.Errorf("result %q does not contain %q", result, want)
				}
			}
		})
	}
}

func TestSplitMessageSlackLimit(t *testing.T) {
	// Test with Slack's actual limit
	longText := strings.Repeat("This is a test message with some content. ", 2000)
	chunks := splitMessage(longText, MaxSlackMessageLength)

	for i, chunk := range chunks {
		if len(chunk) > MaxSlackMessageLength {
			t.Errorf("Chunk %d exceeds Slack limit: %d > %d", i, len(chunk), MaxSlackMessageLength)
		}
	}

	t.Logf("Split %d chars into %d chunks", len(longText), len(chunks))
}

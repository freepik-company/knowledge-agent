package agent

import (
	"encoding/base64"
	"fmt"
	"slices"
	"strings"

	"knowledge-agent/internal/logger"

	"google.golang.org/genai"
)

// buildContentWithImages creates a Content object that includes both text and images
func (a *Agent) buildContentWithImages(text string, messages []map[string]any) *genai.Content {
	log := logger.Get()
	parts := []*genai.Part{genai.NewPartFromText(text)}

	// Add images from the last message (the one the user is responding to)
	if len(messages) > 0 {
		lastMsg := messages[len(messages)-1]
		if images, ok := lastMsg["images"].([]any); ok {
			for _, img := range images {
				if imgMap, ok := img.(map[string]any); ok {
					mimeType := getStringFromMap(imgMap, "mime_type")
					base64Data := getStringFromMap(imgMap, "data")

					if mimeType != "" && base64Data != "" {
						// Decode base64
						imageData, err := base64.StdEncoding.DecodeString(base64Data)
						if err != nil {
							log.Warnw("Failed to decode image", "error", err)
							continue
						}

						// Validate image size (Anthropic max is 5MB)
						maxSize := 5 * 1024 * 1024 // 5MB
						if len(imageData) > maxSize {
							log.Warnw("Image too large",
								"size_bytes", len(imageData),
								"max_bytes", maxSize,
							)
							continue
						}

						// Normalize MIME type for Anthropic
						// Remove any parameters (e.g., "; charset=utf-8") and normalize case
						normalizedMIME := strings.TrimSpace(strings.ToLower(mimeType))
						if idx := strings.Index(normalizedMIME, ";"); idx > 0 {
							normalizedMIME = strings.TrimSpace(normalizedMIME[:idx])
						}

						// Normalize variations
						switch normalizedMIME {
						case "image/jpg":
							normalizedMIME = "image/jpeg"
						}

						// Validate it's a supported format
						supportedFormats := []string{"image/jpeg", "image/png", "image/gif", "image/webp"}
						if !slices.Contains(supportedFormats, normalizedMIME) {
							log.Warnw("Unsupported image format",
								"normalized", normalizedMIME,
								"original", mimeType,
							)
							continue
						}

						// Validate image data has correct magic bytes
						if err := validateImageMagicBytes(imageData, normalizedMIME); err != nil {
							log.Warnw("Invalid image data",
								"error", err,
								"first_16_bytes_hex", fmt.Sprintf("% x", imageData[:min(16, len(imageData))]),
							)
							continue
						}

						log.Debugw("Adding image to content",
							"mime_type", normalizedMIME,
							"original_mime", mimeType,
							"size_bytes", len(imageData),
						)

						// Add image part
						parts = append(parts, &genai.Part{
							InlineData: &genai.Blob{
								Data:     imageData,
								MIMEType: normalizedMIME,
							},
						})
					}
				}
			}
		}
	}

	return &genai.Content{
		Parts: parts,
		Role:  genai.RoleUser,
	}
}

// validateImageMagicBytes checks if image data has the correct file format signature
func validateImageMagicBytes(data []byte, mimeType string) error {
	if len(data) < 16 {
		return fmt.Errorf("image data too small: %d bytes", len(data))
	}

	switch mimeType {
	case "image/png":
		// PNG magic bytes: 89 50 4E 47 0D 0A 1A 0A
		if !(data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47) {
			return fmt.Errorf("invalid PNG header (expected 89 50 4E 47, got %02x %02x %02x %02x)",
				data[0], data[1], data[2], data[3])
		}
	case "image/jpeg":
		// JPEG magic bytes: FF D8 FF
		if !(data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF) {
			return fmt.Errorf("invalid JPEG header (expected FF D8 FF, got %02x %02x %02x)",
				data[0], data[1], data[2])
		}
	case "image/gif":
		// GIF magic bytes: "GIF87a" or "GIF89a"
		if !(data[0] == 'G' && data[1] == 'I' && data[2] == 'F' && data[3] == '8') {
			return fmt.Errorf("invalid GIF header")
		}
	case "image/webp":
		// WebP magic bytes: "RIFF" ... "WEBP"
		if !(data[0] == 'R' && data[1] == 'I' && data[2] == 'F' && data[3] == 'F') {
			return fmt.Errorf("invalid WebP header (expected RIFF)")
		}
	}

	return nil
}

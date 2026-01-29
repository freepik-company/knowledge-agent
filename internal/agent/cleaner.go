package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"knowledge-agent/internal/config"
	"knowledge-agent/internal/logger"
)

const cleanerPrompt = `Tu tarea es limpiar esta respuesta de un agente de IA eliminando la narración innecesaria sobre el proceso interno.

ELIMINA:
- Frases sobre transferencias entre agentes ("te voy a transferir", "el agente de métricas dice", "voy a consultar")
- Saludos redundantes o repetidos
- Explicaciones sobre qué herramienta va a usar
- Repeticiones de la misma información
- Meta-comentarios sobre el proceso ("déjame buscar", "voy a verificar")

MANTÉN INTACTO:
- Toda la información sustancial, datos y cifras
- El contexto relevante para entender la respuesta
- Detalles técnicos importantes
- Preguntas de seguimiento al usuario (si las hay)

IMPORTANTE:
- Responde SOLO con el texto limpio
- NO añadas explicaciones sobre lo que eliminaste
- Mantén el mismo idioma que la respuesta original
- Si la respuesta ya está limpia, devuélvela sin cambios

Respuesta a limpiar:
%s`

// ResponseCleaner cleans agent responses before sending to users
type ResponseCleaner struct {
	client  anthropic.Client
	model   string
	enabled bool
}

// NewResponseCleaner creates a new response cleaner
func NewResponseCleaner(cfg *config.Config) *ResponseCleaner {
	if !cfg.ResponseCleaner.Enabled {
		return &ResponseCleaner{enabled: false}
	}

	client := anthropic.NewClient(
		option.WithAPIKey(cfg.Anthropic.APIKey),
	)

	model := cfg.ResponseCleaner.Model
	if model == "" {
		model = "claude-haiku-4-5-20251001"
	}

	return &ResponseCleaner{
		client:  client,
		model:   model,
		enabled: true,
	}
}

// Clean cleans the response by removing agent narration
func (rc *ResponseCleaner) Clean(ctx context.Context, response string) (string, error) {
	log := logger.Get()

	if !rc.enabled {
		return response, nil
	}

	// Skip cleaning for short responses (likely already clean)
	if len(response) < 200 {
		log.Debugw("Response too short, skipping cleaning", "length", len(response))
		return response, nil
	}

	log.Debugw("Cleaning response with Haiku",
		"original_length", len(response),
		"model", rc.model,
	)

	startTime := time.Now()

	prompt := fmt.Sprintf(cleanerPrompt, response)

	message, err := rc.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(rc.model),
		MaxTokens: 4096,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		log.Warnw("Failed to clean response, returning original",
			"error", err,
		)
		return response, nil // Return original on error, don't fail the request
	}

	// Extract text from response
	var cleanedResponse string
	for _, block := range message.Content {
		if block.Type == "text" {
			cleanedResponse += block.Text
		}
	}

	duration := time.Since(startTime)
	log.Infow("Response cleaned",
		"original_length", len(response),
		"cleaned_length", len(cleanedResponse),
		"duration_ms", duration.Milliseconds(),
		"input_tokens", message.Usage.InputTokens,
		"output_tokens", message.Usage.OutputTokens,
	)

	return cleanedResponse, nil
}

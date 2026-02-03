package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/kelseyhightower/envconfig"
	"github.com/spf13/viper"
)

// LoadFromYAML loads config from YAML file with env var substitution
func LoadFromYAML(path string) (*Config, error) {
	v := viper.New()

	// Set config file
	v.SetConfigFile(path)
	v.SetConfigType("yaml")

	// Enable env var substitution
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Read config
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	// Expand env vars in string values
	expandEnvVars(v)

	// Unmarshal into struct
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Apply default values for empty fields
	applyDefaults(&cfg)

	// Parse API keys if provided as JSON string from environment variable
	// Supports two formats:
	// New format: {"ka_key": {"caller_id": "name", "role": "write"}}
	// Legacy format: {"ka_key": "caller_id"} (assumes role="write")
	if apiKeysStr := v.GetString("api_keys"); apiKeysStr != "" && strings.HasPrefix(strings.TrimSpace(apiKeysStr), "{") {
		apiKeys, err := parseAPIKeysJSON(apiKeysStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse api_keys JSON: %w", err)
		}
		cfg.APIKeys = apiKeys
	}

	// Initialize empty map if not set
	if cfg.APIKeys == nil {
		cfg.APIKeys = make(map[string]APIKeyConfig)
	}

	return &cfg, nil
}

// expandEnvVars recursively expands ${VAR} and ${VAR:default} references in all values
// including strings, arrays/slices, and nested structures
func expandEnvVars(v *viper.Viper) {
	for _, key := range v.AllKeys() {
		val := v.Get(key)
		expanded := expandEnvValue(val)
		if expanded != nil {
			v.Set(key, expanded)
		}
	}
}

// expandEnvValue recursively expands environment variables in any value type
func expandEnvValue(val any) any {
	if val == nil {
		return nil
	}

	switch v := val.(type) {
	case string:
		if strings.Contains(v, "${") {
			return expandEnvString(v)
		}
		return v

	case []any:
		// Handle arrays/slices ([]any is alias for []interface{})
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = expandEnvValue(item)
		}
		return result

	case []string:
		// Handle string slices
		result := make([]string, len(v))
		for i, item := range v {
			if strings.Contains(item, "${") {
				result[i] = expandEnvString(item)
			} else {
				result[i] = item
			}
		}
		return result

	case map[string]any:
		// Handle nested maps (map[string]any is alias for map[string]interface{})
		result := make(map[string]any, len(v))
		for k, item := range v {
			result[k] = expandEnvValue(item)
		}
		return result

	default:
		// Return as-is for other types (int, bool, etc.)
		return val
	}
}

// expandEnvString expands environment variable references in a string
// Supports three formats:
//   - ${VAR} - standard format, returns empty string if not set
//   - ${VAR:default} - with default value if VAR is not set or empty
//   - ${ENV:VAR} - explicit format for clarity (legacy, same as ${VAR})
func expandEnvString(s string) string {
	result := s

	// First handle ${ENV:VAR} format (legacy)
	for {
		start := strings.Index(result, "${ENV:")
		if start == -1 {
			break
		}

		end := strings.Index(result[start:], "}")
		if end == -1 {
			break
		}
		end += start

		// Extract variable name
		varName := result[start+6 : end] // Skip "${ENV:"
		varValue := os.Getenv(varName)

		// Replace ${ENV:VAR} with value
		result = result[:start] + varValue + result[end+1:]
	}

	// Handle ${VAR:default} and ${VAR} formats with custom expansion
	// This allows for default values which os.ExpandEnv doesn't support
	result = expandWithDefaults(result)

	return result
}

// expandWithDefaults expands ${VAR} and ${VAR:default} patterns
func expandWithDefaults(s string) string {
	result := s

	for {
		start := strings.Index(result, "${")
		if start == -1 {
			break
		}

		end := strings.Index(result[start:], "}")
		if end == -1 {
			break
		}
		end += start

		// Extract content between ${ and }
		content := result[start+2 : end]

		var varValue string
		if colonIdx := strings.Index(content, ":"); colonIdx != -1 {
			// Has default value: ${VAR:default}
			varName := content[:colonIdx]
			defaultValue := content[colonIdx+1:]

			varValue = os.Getenv(varName)
			if varValue == "" {
				varValue = defaultValue
			}
		} else {
			// Simple variable: ${VAR}
			varValue = os.Getenv(content)
		}

		// Replace ${...} with value
		result = result[:start] + varValue + result[end+1:]
	}

	return result
}

// applyDefaults applies default values to empty config fields
func applyDefaults(cfg *Config) {
	// Agent name default
	if cfg.AgentName == "" {
		cfg.AgentName = "Knowledge Agent"
	}

	// Logging defaults
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
	if cfg.Log.Format == "" {
		cfg.Log.Format = "console"
	}
	if cfg.Log.OutputPath == "" {
		cfg.Log.OutputPath = "stdout"
	}

	// Server defaults
	if cfg.Server.AgentPort == 0 {
		cfg.Server.AgentPort = 8081
	}
	if cfg.Server.SlackBotPort == 0 {
		cfg.Server.SlackBotPort = 8080
	}

	// Redis defaults
	if cfg.Redis.Addr == "" {
		cfg.Redis.Addr = "localhost:6379"
	}
	if cfg.Redis.TTL == 0 {
		cfg.Redis.TTL = 24 * 3600 * 1000000000 // 24h in nanoseconds
	}

	// Ollama defaults
	if cfg.Ollama.BaseURL == "" {
		cfg.Ollama.BaseURL = "http://localhost:11434/v1"
	}
	if cfg.Ollama.EmbeddingModel == "" {
		cfg.Ollama.EmbeddingModel = "nomic-embed-text"
	}

	// RAG defaults
	if cfg.RAG.ChunkSize == 0 {
		cfg.RAG.ChunkSize = 2000
	}
	if cfg.RAG.ChunkOverlap == 0 {
		cfg.RAG.ChunkOverlap = 1
	}
	if cfg.RAG.MessagesPerChunk == 0 {
		cfg.RAG.MessagesPerChunk = 5
	}
	if cfg.RAG.SimilarityThreshold == 0 {
		cfg.RAG.SimilarityThreshold = 0.7
	}
	if cfg.RAG.MaxResults == 0 {
		cfg.RAG.MaxResults = 5
	}

	// Slack defaults
	if cfg.Slack.Mode == "" {
		cfg.Slack.Mode = "webhook"
	}
}

// LoadFromEnv loads from environment variables (legacy method)
func LoadFromEnv() (*Config, error) {
	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, fmt.Errorf("failed to process config: %w", err)
	}

	// Parse API keys from environment variable (JSON format)
	// Supports both new and legacy formats
	if keysJSON := os.Getenv("API_KEYS"); keysJSON != "" {
		apiKeys, err := parseAPIKeysJSON(keysJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to parse API_KEYS: %w", err)
		}
		cfg.APIKeys = apiKeys
	}

	return &cfg, nil
}

// parseAPIKeysJSON parses API keys from JSON, supporting both formats:
// New format: {"ka_key": {"caller_id": "name", "role": "write"}}
// Legacy format: {"ka_key": "caller_id"} (assumes role="write" for backwards compatibility)
func parseAPIKeysJSON(jsonStr string) (map[string]APIKeyConfig, error) {
	result := make(map[string]APIKeyConfig)

	// First try to parse as new format
	var newFormat map[string]APIKeyConfig
	if err := json.Unmarshal([]byte(jsonStr), &newFormat); err == nil {
		// Validate and set defaults
		for key, cfg := range newFormat {
			if cfg.CallerID == "" {
				return nil, fmt.Errorf("api_keys[%s]: caller_id is required", key)
			}
			if cfg.Role == "" {
				cfg.Role = "write" // Default to write for backwards compatibility
			}
			if cfg.Role != "read" && cfg.Role != "write" {
				return nil, fmt.Errorf("api_keys[%s]: role must be 'read' or 'write', got '%s'", key, cfg.Role)
			}
			result[key] = cfg
		}
		return result, nil
	}

	// Try legacy format: {"key": "caller_id"}
	var legacyFormat map[string]string
	if err := json.Unmarshal([]byte(jsonStr), &legacyFormat); err != nil {
		return nil, fmt.Errorf("invalid JSON format: %w", err)
	}

	// Convert legacy format to new format (assume role="write")
	for key, callerID := range legacyFormat {
		result[key] = APIKeyConfig{
			CallerID: callerID,
			Role:     "write", // Legacy keys get write access for backwards compatibility
		}
	}

	return result, nil
}

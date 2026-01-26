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

	// Parse A2A API keys if provided as JSON string
	// This handles the case where a2a_api_keys: ${ENV:A2A_API_KEYS} expands to JSON
	if apiKeysStr := v.GetString("a2a_api_keys"); apiKeysStr != "" && strings.HasPrefix(strings.TrimSpace(apiKeysStr), "{") {
		var apiKeys map[string]string
		if err := json.Unmarshal([]byte(apiKeysStr), &apiKeys); err != nil {
			return nil, fmt.Errorf("failed to parse a2a_api_keys JSON: %w", err)
		}
		cfg.APIKeys = apiKeys
	}

	// Initialize empty map if not set
	if cfg.APIKeys == nil {
		cfg.APIKeys = make(map[string]string)
	}

	return &cfg, nil
}

// expandEnvVars recursively expands ${VAR} and ${ENV:VAR} references in all string values
func expandEnvVars(v *viper.Viper) {
	for _, key := range v.AllKeys() {
		val := v.GetString(key)
		if strings.Contains(val, "${") {
			expanded := expandEnvString(val)
			v.Set(key, expanded)
		}
	}
}

// expandEnvString expands environment variable references in a string
// Supports two formats:
//   - ${VAR} - standard format (compatible with os.ExpandEnv)
//   - ${ENV:VAR} - explicit format for clarity in config files
func expandEnvString(s string) string {
	// First handle ${ENV:VAR} format
	result := s
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

	// Then handle standard ${VAR} format
	result = os.ExpandEnv(result)

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

	// Parse A2A API keys from environment variable (JSON format)
	// Example: A2A_API_KEYS='{"ka_abc123":"root-agent","ka_xyz":"slack-bridge"}'
	if keysJSON := os.Getenv("A2A_API_KEYS"); keysJSON != "" {
		if err := json.Unmarshal([]byte(keysJSON), &cfg.APIKeys); err != nil {
			return nil, fmt.Errorf("failed to parse A2A_API_KEYS: %w", err)
		}
	}

	return &cfg, nil
}

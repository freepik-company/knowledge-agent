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

	// Parse A2A API keys if provided in config
	if cfg.APIKeys == nil {
		cfg.APIKeys = make(map[string]string)
	}

	return &cfg, nil
}

// expandEnvVars recursively expands ${VAR} references in all string values
func expandEnvVars(v *viper.Viper) {
	for _, key := range v.AllKeys() {
		val := v.GetString(key)
		if strings.Contains(val, "${") {
			expanded := os.ExpandEnv(val)
			v.Set(key, expanded)
		}
	}
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

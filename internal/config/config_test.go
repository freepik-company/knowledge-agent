package config

import (
	"os"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	// Set required environment variables
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	os.Setenv("SLACK_BOT_TOKEN", "xoxb-test")
	os.Setenv("SLACK_SIGNING_SECRET", "test-secret")
	os.Setenv("POSTGRES_URL", "postgres://localhost/test")

	// Clean up after test
	defer func() {
		os.Unsetenv("ANTHROPIC_API_KEY")
		os.Unsetenv("SLACK_BOT_TOKEN")
		os.Unsetenv("SLACK_SIGNING_SECRET")
		os.Unsetenv("POSTGRES_URL")
	}()

	// Load with empty config path (uses env vars)
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if cfg.Anthropic.APIKey != "test-key" {
		t.Errorf("Expected API key 'test-key', got '%s'", cfg.Anthropic.APIKey)
	}

	if cfg.Redis.TTL != 24*time.Hour {
		t.Errorf("Expected default TTL 24h, got %v", cfg.Redis.TTL)
	}
}

func TestA2AConfig_ParallelExecution(t *testing.T) {
	// Test that parallel execution config fields have correct defaults
	cfg := &A2AConfig{}

	// Default values should be false and 0
	if cfg.ParallelExecution {
		t.Error("ParallelExecution should default to false")
	}
	if cfg.MaxConcurrentCalls != 0 {
		t.Errorf("MaxConcurrentCalls should default to 0, got: %d", cfg.MaxConcurrentCalls)
	}

	// Test with explicit values
	cfg = &A2AConfig{
		Enabled:            true,
		SelfName:           "test-agent",
		ParallelExecution:  true,
		MaxConcurrentCalls: 10,
		SubAgents: []A2ASubAgentConfig{
			{Name: "agent1", Endpoint: "http://localhost:9000"},
			{Name: "agent2", Endpoint: "http://localhost:9001"},
		},
	}

	if !cfg.ParallelExecution {
		t.Error("ParallelExecution should be true")
	}
	if cfg.MaxConcurrentCalls != 10 {
		t.Errorf("MaxConcurrentCalls should be 10, got: %d", cfg.MaxConcurrentCalls)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config with Slack enabled",
			config: Config{
				Anthropic: AnthropicConfig{APIKey: "key"},
				Slack:     SlackConfig{Enabled: true, BotToken: "token", SigningSecret: "secret", Mode: "webhook"},
				Postgres:  PostgresConfig{URL: "postgres://localhost/db"},
			},
			wantErr: false,
		},
		{
			name: "valid config with Slack disabled",
			config: Config{
				Anthropic: AnthropicConfig{APIKey: "key"},
				Slack:     SlackConfig{Enabled: false}, // No token needed when disabled
				Postgres:  PostgresConfig{URL: "postgres://localhost/db"},
			},
			wantErr: false,
		},
		{
			name: "missing API key",
			config: Config{
				Slack:    SlackConfig{Enabled: true, BotToken: "token", SigningSecret: "secret", Mode: "webhook"},
				Postgres: PostgresConfig{URL: "postgres://localhost/db"},
			},
			wantErr: true,
		},
		{
			name: "missing Slack token when enabled",
			config: Config{
				Anthropic: AnthropicConfig{APIKey: "key"},
				Slack:     SlackConfig{Enabled: true, SigningSecret: "secret", Mode: "webhook"},
				Postgres:  PostgresConfig{URL: "postgres://localhost/db"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

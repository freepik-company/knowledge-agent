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

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				Anthropic: AnthropicConfig{APIKey: "key"},
				Slack:     SlackConfig{BotToken: "token", SigningSecret: "secret", Mode: "webhook"},
				Postgres:  PostgresConfig{URL: "postgres://localhost/db"},
			},
			wantErr: false,
		},
		{
			name: "missing API key",
			config: Config{
				Slack:    SlackConfig{BotToken: "token", SigningSecret: "secret"},
				Postgres: PostgresConfig{URL: "postgres://localhost/db"},
			},
			wantErr: true,
		},
		{
			name: "missing Slack token",
			config: Config{
				Anthropic: AnthropicConfig{APIKey: "key"},
				Slack:     SlackConfig{SigningSecret: "secret"},
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

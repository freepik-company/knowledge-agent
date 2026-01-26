package config

import (
	"os"
	"testing"
)

func TestLoadFromYAML(t *testing.T) {
	// Create a temporary config file
	content := `
anthropic:
  api_key: sk-ant-test-key-123
  model: claude-sonnet-4-5-20250929

slack:
  bot_token: xoxb-test
  mode: socket
  app_token: xapp-test
  signing_secret: ""
  bridge_api_key: ""

postgres:
  url: postgres://localhost/test

redis:
  addr: localhost:6379
  ttl: 24h

ollama:
  base_url: http://localhost:11434/v1
  embedding_model: nomic-embed-text

server:
  agent_port: 8081
  slack_bot_port: 8080

log:
  level: info
  format: console
  output_path: stdout
`

	tmpfile, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatal(err)
	}

	// Load the config
	cfg, err := LoadFromYAML(tmpfile.Name())
	if err != nil {
		t.Fatalf("LoadFromYAML failed: %v", err)
	}

	// Verify the values were loaded
	if cfg.Anthropic.APIKey != "sk-ant-test-key-123" {
		t.Errorf("Expected APIKey 'sk-ant-test-key-123', got '%s'", cfg.Anthropic.APIKey)
	}

	if cfg.Anthropic.Model != "claude-sonnet-4-5-20250929" {
		t.Errorf("Expected Model 'claude-sonnet-4-5-20250929', got '%s'", cfg.Anthropic.Model)
	}

	if cfg.Slack.BotToken != "xoxb-test" {
		t.Errorf("Expected BotToken 'xoxb-test', got '%s'", cfg.Slack.BotToken)
	}

	if cfg.Slack.Mode != "socket" {
		t.Errorf("Expected Mode 'socket', got '%s'", cfg.Slack.Mode)
	}

	if cfg.Postgres.URL != "postgres://localhost/test" {
		t.Errorf("Expected URL 'postgres://localhost/test', got '%s'", cfg.Postgres.URL)
	}

	// Test validation
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() should pass but failed: %v", err)
	}
}

func TestLoadFromYAMLWithEnvVars(t *testing.T) {
	// Set env var
	os.Setenv("TEST_API_KEY", "sk-ant-from-env")
	defer os.Unsetenv("TEST_API_KEY")

	content := `
anthropic:
  api_key: ${TEST_API_KEY}
  model: claude-sonnet-4-5-20250929

slack:
  bot_token: xoxb-test
  mode: socket
  app_token: xapp-test

postgres:
  url: postgres://localhost/test
`

	tmpfile, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatal(err)
	}

	// Load the config
	cfg, err := LoadFromYAML(tmpfile.Name())
	if err != nil {
		t.Fatalf("LoadFromYAML failed: %v", err)
	}

	// Verify env var was expanded
	if cfg.Anthropic.APIKey != "sk-ant-from-env" {
		t.Errorf("Expected APIKey 'sk-ant-from-env' (from env), got '%s'", cfg.Anthropic.APIKey)
	}
}

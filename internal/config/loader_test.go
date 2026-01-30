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

func TestParseAPIKeysJSON_NewFormat(t *testing.T) {
	jsonStr := `{
		"secret-key-1": {"caller_id": "client-1", "role": "write"},
		"secret-key-2": {"caller_id": "client-2", "role": "read"}
	}`

	result, err := parseAPIKeysJSON(jsonStr)
	if err != nil {
		t.Fatalf("parseAPIKeysJSON failed: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("Expected 2 keys, got %d", len(result))
	}

	key1 := result["secret-key-1"]
	if key1.CallerID != "client-1" || key1.Role != "write" {
		t.Errorf("key1: expected caller_id=client-1, role=write, got %+v", key1)
	}

	key2 := result["secret-key-2"]
	if key2.CallerID != "client-2" || key2.Role != "read" {
		t.Errorf("key2: expected caller_id=client-2, role=read, got %+v", key2)
	}
}

func TestParseAPIKeysJSON_LegacyFormat(t *testing.T) {
	// Legacy format: {"key": "caller_id"} should default to role="write"
	jsonStr := `{"ka_abc123": "root-agent", "ka_xyz": "slack-bridge"}`

	result, err := parseAPIKeysJSON(jsonStr)
	if err != nil {
		t.Fatalf("parseAPIKeysJSON failed: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("Expected 2 keys, got %d", len(result))
	}

	key1 := result["ka_abc123"]
	if key1.CallerID != "root-agent" || key1.Role != "write" {
		t.Errorf("key1: expected caller_id=root-agent, role=write (default), got %+v", key1)
	}

	key2 := result["ka_xyz"]
	if key2.CallerID != "slack-bridge" || key2.Role != "write" {
		t.Errorf("key2: expected caller_id=slack-bridge, role=write (default), got %+v", key2)
	}
}

func TestParseAPIKeysJSON_DefaultRole(t *testing.T) {
	// New format without role should default to "write"
	jsonStr := `{"secret-key": {"caller_id": "client-1"}}`

	result, err := parseAPIKeysJSON(jsonStr)
	if err != nil {
		t.Fatalf("parseAPIKeysJSON failed: %v", err)
	}

	key := result["secret-key"]
	if key.Role != "write" {
		t.Errorf("Expected default role 'write', got '%s'", key.Role)
	}
}

func TestParseAPIKeysJSON_InvalidRole(t *testing.T) {
	jsonStr := `{"secret-key": {"caller_id": "client-1", "role": "admin"}}`

	_, err := parseAPIKeysJSON(jsonStr)
	if err == nil {
		t.Error("Expected error for invalid role 'admin', got nil")
	}
}

func TestParseAPIKeysJSON_MissingCallerID(t *testing.T) {
	jsonStr := `{"secret-key": {"role": "write"}}`

	_, err := parseAPIKeysJSON(jsonStr)
	if err == nil {
		t.Error("Expected error for missing caller_id, got nil")
	}
}

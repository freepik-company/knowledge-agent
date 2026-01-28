package config

import (
	"fmt"
	"os"
	"time"
)

// Config holds all configuration for the application
type Config struct {
	AgentName   string            `yaml:"agent_name" mapstructure:"agent_name" envconfig:"AGENT_NAME" default:"Knowledge Agent"` // Custom name for this agent instance (e.g., "Anton", "Ghost", etc.)
	Anthropic   AnthropicConfig   `yaml:"anthropic" mapstructure:"anthropic"`
	Slack       SlackConfig       `yaml:"slack" mapstructure:"slack"`
	Postgres    PostgresConfig    `yaml:"postgres" mapstructure:"postgres"`
	Redis       RedisConfig       `yaml:"redis" mapstructure:"redis"`
	Ollama      OllamaConfig      `yaml:"ollama" mapstructure:"ollama"`
	RAG         RAGConfig         `yaml:"rag" mapstructure:"rag"`
	Server      ServerConfig      `yaml:"server" mapstructure:"server"`
	Log         LogConfig         `yaml:"log" mapstructure:"log"`
	Auth        AuthConfig        `yaml:"auth" mapstructure:"auth"`
	Permissions PermissionsConfig `yaml:"permissions" mapstructure:"permissions"`
	Prompt      PromptConfig      `yaml:"prompt" mapstructure:"prompt"`
	Langfuse    LangfuseConfig    `yaml:"langfuse" mapstructure:"langfuse"`
	MCP         MCPConfig         `yaml:"mcp" mapstructure:"mcp"`
	A2A         A2AConfig         `yaml:"a2a" mapstructure:"a2a"` // Agent-to-Agent tool integration
	APIKeys     map[string]string `yaml:"a2a_api_keys" mapstructure:"a2a_api_keys"` // Maps client ID to secret token for external A2A access (e.g., "root-agent" -> "ka_secret_abc123")
}

// AuthConfig holds authentication configuration
type AuthConfig struct {
	InternalToken string `yaml:"internal_token" mapstructure:"internal_token" envconfig:"INTERNAL_AUTH_TOKEN"` // Shared secret between slack-bot and agent
}

// PermissionsConfig holds permissions configuration for memory operations
type PermissionsConfig struct {
	AllowedSlackUsers []string `yaml:"allowed_slack_users" mapstructure:"allowed_slack_users"` // List of Slack User IDs allowed to save to memory
	AdminCallerIDs    []string `yaml:"admin_caller_ids" mapstructure:"admin_caller_ids"`       // List of caller IDs with admin permissions (can save without restrictions)
}

// PromptConfig holds prompt configuration
type PromptConfig struct {
	BasePrompt      string `yaml:"base_prompt" mapstructure:"base_prompt"`                                                           // The system prompt (loaded from config)
	TemplatePath    string `yaml:"template_path" mapstructure:"template_path" envconfig:"PROMPT_TEMPLATE_PATH"`                      // Path to external prompt file (overrides base_prompt if specified)
	EnableHotReload bool   `yaml:"enable_hot_reload" mapstructure:"enable_hot_reload" envconfig:"PROMPT_HOT_RELOAD" default:"false"` // Enable hot reload in development (only with template_path)
}

// LangfuseConfig holds Langfuse observability configuration
type LangfuseConfig struct {
	Enabled         bool    `yaml:"enabled" mapstructure:"enabled" envconfig:"LANGFUSE_ENABLED" default:"false"`                             // Enable Langfuse integration
	PublicKey       string  `yaml:"public_key" mapstructure:"public_key" envconfig:"LANGFUSE_PUBLIC_KEY"`                                    // Langfuse public key
	SecretKey       string  `yaml:"secret_key" mapstructure:"secret_key" envconfig:"LANGFUSE_SECRET_KEY"`                                    // Langfuse secret key
	Host            string  `yaml:"host" mapstructure:"host" envconfig:"LANGFUSE_HOST" default:"https://cloud.langfuse.com"`                 // Langfuse host URL
	InputCostPer1M  float64 `yaml:"input_cost_per_1m" mapstructure:"input_cost_per_1m" default:"3.0"`                                        // Cost per 1M input tokens in USD (default: Claude Sonnet 4.5)
	OutputCostPer1M float64 `yaml:"output_cost_per_1m" mapstructure:"output_cost_per_1m" default:"15.0"`                                     // Cost per 1M output tokens in USD (default: Claude Sonnet 4.5)
}

// MCPConfig holds Model Context Protocol configuration
type MCPConfig struct {
	Enabled bool              `yaml:"enabled" mapstructure:"enabled" envconfig:"MCP_ENABLED" default:"false"` // Enable MCP integration
	Servers []MCPServerConfig `yaml:"servers" mapstructure:"servers"`                                         // List of MCP servers to connect to
}

// MCPServerConfig holds configuration for a single MCP server
type MCPServerConfig struct {
	Name          string            `yaml:"name" mapstructure:"name"`                                 // Server name (for logging and identification)
	Description   string            `yaml:"description" mapstructure:"description"`                   // Human-readable description
	Enabled       bool              `yaml:"enabled" mapstructure:"enabled" default:"true"`            // Enable this server
	TransportType string            `yaml:"transport_type" mapstructure:"transport_type"`             // "command", "sse", or "streamable"
	Command       *MCPCommandConfig `yaml:"command,omitempty" mapstructure:"command"`                 // Command configuration (for command transport)
	Endpoint      string            `yaml:"endpoint,omitempty" mapstructure:"endpoint"`               // HTTP endpoint (for sse/streamable transport)
	Auth          *MCPAuthConfig    `yaml:"auth,omitempty" mapstructure:"auth"`                       // Authentication configuration
	ToolFilter    []string          `yaml:"tool_filter,omitempty" mapstructure:"tool_filter"`         // List of tool names to include (empty = all tools)
	Timeout       int               `yaml:"timeout" mapstructure:"timeout" default:"30"`              // Connection timeout in seconds
}

// MCPCommandConfig holds configuration for command-based MCP transport
type MCPCommandConfig struct {
	Path string            `yaml:"path" mapstructure:"path"`         // Executable path
	Args []string          `yaml:"args,omitempty" mapstructure:"args"` // Command arguments
	Env  map[string]string `yaml:"env,omitempty" mapstructure:"env"`   // Additional environment variables
}

// MCPAuthConfig holds authentication configuration for MCP servers
type MCPAuthConfig struct {
	Type     string `yaml:"type" mapstructure:"type"`                             // "bearer", "basic", or "oauth2"
	TokenEnv string `yaml:"token_env,omitempty" mapstructure:"token_env"`         // Environment variable containing token
	Token    string `yaml:"token,omitempty" mapstructure:"token"`                 // Token value (not recommended, use token_env instead)
	Username string `yaml:"username,omitempty" mapstructure:"username"`           // Username (for basic auth)
	Password string `yaml:"password,omitempty" mapstructure:"password"`           // Password (for basic auth)
}

// A2AConfig holds Agent-to-Agent tool integration configuration
type A2AConfig struct {
	Enabled      bool             `yaml:"enabled" mapstructure:"enabled" envconfig:"A2A_ENABLED" default:"false"` // Enable A2A tool integration
	SelfName     string           `yaml:"self_name" mapstructure:"self_name"`                                      // This agent's identifier for loop prevention
	MaxCallDepth int              `yaml:"max_call_depth" mapstructure:"max_call_depth" default:"5"`                // Maximum call chain depth
	Agents       []A2AAgentConfig `yaml:"agents" mapstructure:"agents"`                                            // List of external agents to connect to
}

// A2AAgentConfig holds configuration for a single external agent
type A2AAgentConfig struct {
	Name        string           `yaml:"name" mapstructure:"name"`               // Agent name (for logging and identification)
	Description string           `yaml:"description" mapstructure:"description"` // Human-readable description
	Endpoint    string           `yaml:"endpoint" mapstructure:"endpoint"`       // HTTP endpoint (e.g., http://logs-agent:8081)
	Timeout     int              `yaml:"timeout" mapstructure:"timeout" default:"30"` // Request timeout in seconds
	Auth        A2AAuthConfig    `yaml:"auth" mapstructure:"auth"`               // Authentication configuration
	Tools       []A2AToolConfig  `yaml:"tools" mapstructure:"tools"`             // List of tools this agent provides
}

// A2AAuthConfig holds authentication configuration for an external agent
type A2AAuthConfig struct {
	Type            string   `yaml:"type" mapstructure:"type"`                                           // "api_key", "bearer", "oauth2", or "none"
	Header          string   `yaml:"header,omitempty" mapstructure:"header"`                             // Header name for api_key auth (e.g., "X-API-Key")
	KeyEnv          string   `yaml:"key_env,omitempty" mapstructure:"key_env"`                           // Environment variable containing API key
	TokenEnv        string   `yaml:"token_env,omitempty" mapstructure:"token_env"`                       // Environment variable containing bearer token
	TokenURL        string   `yaml:"token_url,omitempty" mapstructure:"token_url"`                       // OAuth2 token endpoint URL
	ClientIDEnv     string   `yaml:"client_id_env,omitempty" mapstructure:"client_id_env"`               // Environment variable containing OAuth2 client ID
	ClientSecretEnv string   `yaml:"client_secret_env,omitempty" mapstructure:"client_secret_env"`       // Environment variable containing OAuth2 client secret
	Scopes          []string `yaml:"scopes,omitempty" mapstructure:"scopes"`                             // OAuth2 scopes to request
}

// A2AToolConfig holds configuration for a tool provided by an external agent
type A2AToolConfig struct {
	Name        string `yaml:"name" mapstructure:"name"`               // Tool name (e.g., "search_logs")
	Description string `yaml:"description" mapstructure:"description"` // Tool description for LLM
}

// AnthropicConfig holds Anthropic API configuration
type AnthropicConfig struct {
	APIKey string `yaml:"api_key" mapstructure:"api_key" envconfig:"ANTHROPIC_API_KEY" required:"true"`
	Model  string `yaml:"model" mapstructure:"model" envconfig:"ANTHROPIC_MODEL" default:"claude-sonnet-4-5-20250929"`
}

// SlackConfig holds Slack API configuration
type SlackConfig struct {
	BotToken      string `yaml:"bot_token" mapstructure:"bot_token" envconfig:"SLACK_BOT_TOKEN" required:"true"`
	SigningSecret string `yaml:"signing_secret" mapstructure:"signing_secret" envconfig:"SLACK_SIGNING_SECRET"`                 // Only required for webhook mode
	AppToken      string `yaml:"app_token" mapstructure:"app_token" envconfig:"SLACK_APP_TOKEN"`                                // Only required for socket mode
	Mode          string `yaml:"mode" mapstructure:"mode" envconfig:"SLACK_MODE" default:"webhook"`                             // "webhook" or "socket"
	MaxFileSize   int64  `yaml:"max_file_size" mapstructure:"max_file_size" envconfig:"SLACK_MAX_FILE_SIZE" default:"10485760"` // 10 MB default
}

// PostgresConfig holds PostgreSQL configuration
type PostgresConfig struct {
	URL string `yaml:"url" mapstructure:"url" envconfig:"POSTGRES_URL" required:"true"`
}

// RedisConfig holds Redis configuration
type RedisConfig struct {
	Addr     string        `yaml:"addr" mapstructure:"addr" envconfig:"REDIS_ADDR" default:"localhost:6379"`
	Password string        `yaml:"password" mapstructure:"password" envconfig:"REDIS_PASSWORD"`
	TTL      time.Duration `yaml:"ttl" mapstructure:"ttl" envconfig:"REDIS_TTL" default:"24h"`
}

// OllamaConfig holds Ollama configuration
type OllamaConfig struct {
	BaseURL        string `yaml:"base_url" mapstructure:"base_url" envconfig:"OLLAMA_BASE_URL" default:"http://localhost:11434/v1"`
	EmbeddingModel string `yaml:"embedding_model" mapstructure:"embedding_model" envconfig:"EMBEDDING_MODEL" default:"nomic-embed-text"`
}

// RAGConfig holds RAG pipeline configuration
type RAGConfig struct {
	ChunkSize           int     `yaml:"chunk_size" mapstructure:"chunk_size" envconfig:"RAG_CHUNK_SIZE" default:"2000"`
	ChunkOverlap        int     `yaml:"chunk_overlap" mapstructure:"chunk_overlap" envconfig:"RAG_CHUNK_OVERLAP" default:"1"`
	MessagesPerChunk    int     `yaml:"messages_per_chunk" mapstructure:"messages_per_chunk" envconfig:"RAG_MESSAGES_PER_CHUNK" default:"5"`
	SimilarityThreshold float64 `yaml:"similarity_threshold" mapstructure:"similarity_threshold" envconfig:"RAG_SIMILARITY_THRESHOLD" default:"0.7"`
	MaxResults          int     `yaml:"max_results" mapstructure:"max_results" envconfig:"RAG_MAX_RESULTS" default:"5"`
	KnowledgeScope      string  `yaml:"knowledge_scope" mapstructure:"knowledge_scope" envconfig:"KNOWLEDGE_SCOPE" default:"shared"` // "shared": global knowledge base, "channel": per-channel isolation, "user": per-user isolation
}

// ServerConfig holds HTTP server configuration
type ServerConfig struct {
	AgentPort      int `yaml:"agent_port" mapstructure:"agent_port" envconfig:"AGENT_PORT" default:"8081"`
	SlackBotPort   int `yaml:"slack_bot_port" mapstructure:"slack_bot_port" envconfig:"SLACK_BOT_PORT" default:"8080"`
	ReadTimeout    int `yaml:"read_timeout" mapstructure:"read_timeout" envconfig:"SERVER_READ_TIMEOUT" default:"30"`   // seconds
	WriteTimeout   int `yaml:"write_timeout" mapstructure:"write_timeout" envconfig:"SERVER_WRITE_TIMEOUT" default:"180"` // seconds (3 minutes for long operations)
	RequestTimeout int `yaml:"request_timeout" mapstructure:"request_timeout" envconfig:"SERVER_REQUEST_TIMEOUT" default:"120"` // seconds (2 minutes for agent operations)
}

// LogConfig holds logging configuration
type LogConfig struct {
	Level      string `yaml:"level" mapstructure:"level" envconfig:"LOG_LEVEL" default:"info"`
	Format     string `yaml:"format" mapstructure:"format" envconfig:"LOG_FORMAT" default:"console"`
	OutputPath string `yaml:"output_path" mapstructure:"output_path" envconfig:"LOG_OUTPUT" default:"stdout"`
}

// Load tries to load config from specified path, config.yaml, or environment variables
// If configPath is provided and exists, it will be used
// Otherwise, it tries config.yaml in current directory
// Finally, falls back to environment variables
func Load(configPath string) (*Config, error) {
	// If configPath is explicitly provided, use it
	if configPath != "" {
		if _, err := os.Stat(configPath); err == nil {
			return LoadFromYAML(configPath)
		}
		// If path was explicitly provided but doesn't exist, return error
		return nil, fmt.Errorf("config file not found: %s", configPath)
	}

	// Try default config.yaml in current directory
	if _, err := os.Stat("config.yaml"); err == nil {
		return LoadFromYAML("config.yaml")
	}

	// Fallback to environment variables (legacy)
	return LoadFromEnv()
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Anthropic.APIKey == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY is required")
	}
	if c.Slack.BotToken == "" {
		return fmt.Errorf("SLACK_BOT_TOKEN is required")
	}

	// Validate mode-specific requirements
	if c.Slack.Mode == "socket" {
		if c.Slack.AppToken == "" {
			return fmt.Errorf("SLACK_APP_TOKEN is required for socket mode")
		}
	} else if c.Slack.Mode == "webhook" {
		if c.Slack.SigningSecret == "" {
			return fmt.Errorf("SLACK_SIGNING_SECRET is required for webhook mode")
		}
	} else {
		return fmt.Errorf("SLACK_MODE must be either 'socket' or 'webhook', got: %s", c.Slack.Mode)
	}

	if c.Postgres.URL == "" {
		return fmt.Errorf("POSTGRES_URL is required")
	}

	// Validate MCP configuration
	if c.MCP.Enabled {
		for i, server := range c.MCP.Servers {
			if server.Name == "" {
				return fmt.Errorf("mcp.servers[%d]: name is required", i)
			}
			if server.TransportType == "" {
				return fmt.Errorf("mcp.servers[%d] (%s): transport_type is required", i, server.Name)
			}
			if server.TransportType != "command" && server.TransportType != "sse" && server.TransportType != "streamable" {
				return fmt.Errorf("mcp.servers[%d] (%s): transport_type must be 'command', 'sse', or 'streamable'", i, server.Name)
			}
			if server.TransportType == "command" {
				if server.Command == nil {
					return fmt.Errorf("mcp.servers[%d] (%s): command configuration is required for command transport", i, server.Name)
				}
				if server.Command.Path == "" {
					return fmt.Errorf("mcp.servers[%d] (%s): command.path is required", i, server.Name)
				}
			}
			if (server.TransportType == "sse" || server.TransportType == "streamable") && server.Endpoint == "" {
				return fmt.Errorf("mcp.servers[%d] (%s): endpoint is required for %s transport", i, server.Name, server.TransportType)
			}
		}
	}

	// Validate A2A configuration
	if c.A2A.Enabled {
		if c.A2A.SelfName == "" {
			return fmt.Errorf("a2a.self_name is required when A2A is enabled")
		}
		if c.A2A.MaxCallDepth <= 0 {
			c.A2A.MaxCallDepth = 5 // Default value
		}
		for i, agent := range c.A2A.Agents {
			if agent.Name == "" {
				return fmt.Errorf("a2a.agents[%d]: name is required", i)
			}
			if agent.Endpoint == "" {
				return fmt.Errorf("a2a.agents[%d] (%s): endpoint is required", i, agent.Name)
			}
			if len(agent.Tools) == 0 {
				return fmt.Errorf("a2a.agents[%d] (%s): at least one tool must be configured", i, agent.Name)
			}
			// Validate auth type
			validAuthTypes := map[string]bool{"api_key": true, "bearer": true, "oauth2": true, "none": true, "": true}
			if !validAuthTypes[agent.Auth.Type] {
				return fmt.Errorf("a2a.agents[%d] (%s): auth.type must be 'api_key', 'bearer', 'oauth2', or 'none'", i, agent.Name)
			}
			// Validate auth-specific requirements
			switch agent.Auth.Type {
			case "api_key":
				if agent.Auth.Header == "" {
					return fmt.Errorf("a2a.agents[%d] (%s): auth.header is required for api_key auth", i, agent.Name)
				}
				if agent.Auth.KeyEnv == "" {
					return fmt.Errorf("a2a.agents[%d] (%s): auth.key_env is required for api_key auth", i, agent.Name)
				}
			case "bearer":
				if agent.Auth.TokenEnv == "" {
					return fmt.Errorf("a2a.agents[%d] (%s): auth.token_env is required for bearer auth", i, agent.Name)
				}
			case "oauth2":
				if agent.Auth.TokenURL == "" {
					return fmt.Errorf("a2a.agents[%d] (%s): auth.token_url is required for oauth2 auth", i, agent.Name)
				}
				if agent.Auth.ClientIDEnv == "" {
					return fmt.Errorf("a2a.agents[%d] (%s): auth.client_id_env is required for oauth2 auth", i, agent.Name)
				}
				if agent.Auth.ClientSecretEnv == "" {
					return fmt.Errorf("a2a.agents[%d] (%s): auth.client_secret_env is required for oauth2 auth", i, agent.Name)
				}
			}
			// Validate tools
			for j, tool := range agent.Tools {
				if tool.Name == "" {
					return fmt.Errorf("a2a.agents[%d] (%s).tools[%d]: name is required", i, agent.Name, j)
				}
				if tool.Description == "" {
					return fmt.Errorf("a2a.agents[%d] (%s).tools[%d] (%s): description is required", i, agent.Name, j, tool.Name)
				}
			}
		}
	}

	return nil
}

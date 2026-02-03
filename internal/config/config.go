package config

import (
	"fmt"
	"os"
	"time"
)

// Config holds all configuration for the application
type Config struct {
	AgentName         string                  `yaml:"agent_name" mapstructure:"agent_name" envconfig:"AGENT_NAME" default:"Knowledge Agent"` // Custom name for this agent instance (e.g., "Anton", "Ghost", etc.)
	Anthropic         AnthropicConfig         `yaml:"anthropic" mapstructure:"anthropic"`
	Slack             SlackConfig             `yaml:"slack" mapstructure:"slack"`
	Postgres          PostgresConfig          `yaml:"postgres" mapstructure:"postgres"`
	Redis             RedisConfig             `yaml:"redis" mapstructure:"redis"`
	Ollama            OllamaConfig            `yaml:"ollama" mapstructure:"ollama"`
	RAG               RAGConfig               `yaml:"rag" mapstructure:"rag"`
	Server            ServerConfig            `yaml:"server" mapstructure:"server"`
	Log               LogConfig               `yaml:"log" mapstructure:"log"`
	Auth              AuthConfig              `yaml:"auth" mapstructure:"auth"`
	Permissions       PermissionsConfig       `yaml:"permissions" mapstructure:"permissions"`
	Prompt            PromptConfig            `yaml:"prompt" mapstructure:"prompt"`
	Langfuse          LangfuseConfig          `yaml:"langfuse" mapstructure:"langfuse"`
	MCP               MCPConfig               `yaml:"mcp" mapstructure:"mcp"`
	A2A               A2AConfig               `yaml:"a2a" mapstructure:"a2a"`                               // Agent-to-Agent tool integration (also configures inbound A2A endpoints)
	Parallel          ParallelConfig          `yaml:"parallel" mapstructure:"parallel"`                     // Parallel tool execution configuration
	ResponseCleaner   ResponseCleanerConfig   `yaml:"response_cleaner" mapstructure:"response_cleaner"`     // Clean responses before sending to user
	ContextSummarizer ContextSummarizerConfig `yaml:"context_summarizer" mapstructure:"context_summarizer"` // Summarize long contexts before sending to LLM
	APIKeys           map[string]APIKeyConfig `yaml:"api_keys" mapstructure:"api_keys"`                     // API keys with caller_id and role for authentication
	Tools             ToolsConfig             `yaml:"tools" mapstructure:"tools"`                           // Tool-specific configuration
}

// ToolsConfig holds configuration for agent tools
type ToolsConfig struct {
	WebFetch WebFetchConfig `yaml:"web_fetch" mapstructure:"web_fetch"` // Web fetch tool configuration
}

// RetryConfig configures retry behavior for transient failures
type RetryConfig struct {
	Enabled           bool          `yaml:"enabled" mapstructure:"enabled" default:"true"`                      // Enable retry logic
	MaxRetries        int           `yaml:"max_retries" mapstructure:"max_retries" default:"3"`                 // Maximum number of retry attempts
	InitialDelay      time.Duration `yaml:"initial_delay" mapstructure:"initial_delay" default:"500ms"`         // Initial delay before first retry
	MaxDelay          time.Duration `yaml:"max_delay" mapstructure:"max_delay" default:"30s"`                   // Maximum delay between retries
	BackoffMultiplier float64       `yaml:"backoff_multiplier" mapstructure:"backoff_multiplier" default:"2.0"` // Multiplier for exponential backoff
}

// WebFetchConfig holds configuration for the web fetch tool
type WebFetchConfig struct {
	Timeout          time.Duration `yaml:"timeout" mapstructure:"timeout" envconfig:"WEBFETCH_TIMEOUT" default:"30s"`                                    // HTTP request timeout
	DefaultMaxLength int           `yaml:"default_max_length" mapstructure:"default_max_length" envconfig:"WEBFETCH_DEFAULT_MAX_LENGTH" default:"10000"` // Default max content length
}

// ResponseCleanerConfig holds configuration for cleaning responses before sending to users
type ResponseCleanerConfig struct {
	Enabled bool   `yaml:"enabled" mapstructure:"enabled" default:"false"`                 // Enable response cleaning
	Model   string `yaml:"model" mapstructure:"model" default:"claude-haiku-4-5-20251001"` // Model to use for cleaning (default: Haiku for speed/cost)
}

// ContextSummarizerConfig holds configuration for summarizing long conversation contexts
type ContextSummarizerConfig struct {
	Enabled        bool   `yaml:"enabled" mapstructure:"enabled" default:"false"`                 // Enable context summarization
	Model          string `yaml:"model" mapstructure:"model" default:"claude-haiku-4-5-20251001"` // Model to use for summarization (default: Haiku for speed/cost)
	TokenThreshold int    `yaml:"token_threshold" mapstructure:"token_threshold" default:"8000"`  // Token threshold above which context is summarized
}

// AuthConfig holds authentication configuration
type AuthConfig struct {
	InternalToken string `yaml:"internal_token" mapstructure:"internal_token" envconfig:"INTERNAL_AUTH_TOKEN"` // Shared secret between slack-bot and agent
}

// APIKeyConfig holds configuration for a single API key
type APIKeyConfig struct {
	CallerID string `yaml:"caller_id" mapstructure:"caller_id" json:"caller_id"` // Identifier for this caller (used in logs and permissions)
	Role     string `yaml:"role" mapstructure:"role" json:"role"`                // "write" (read+write) or "read" (read-only, no save_to_memory)
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
	Enabled         bool    `yaml:"enabled" mapstructure:"enabled" envconfig:"LANGFUSE_ENABLED" default:"false"`             // Enable Langfuse integration
	PublicKey       string  `yaml:"public_key" mapstructure:"public_key" envconfig:"LANGFUSE_PUBLIC_KEY"`                    // Langfuse public key
	SecretKey       string  `yaml:"secret_key" mapstructure:"secret_key" envconfig:"LANGFUSE_SECRET_KEY"`                    // Langfuse secret key
	Host            string  `yaml:"host" mapstructure:"host" envconfig:"LANGFUSE_HOST" default:"https://cloud.langfuse.com"` // Langfuse host URL
	InputCostPer1M  float64 `yaml:"input_cost_per_1m" mapstructure:"input_cost_per_1m" default:"3.0"`                        // Cost per 1M input tokens in USD (default: Claude Sonnet 4.5)
	OutputCostPer1M float64 `yaml:"output_cost_per_1m" mapstructure:"output_cost_per_1m" default:"15.0"`                     // Cost per 1M output tokens in USD (default: Claude Sonnet 4.5)
}

// MCPConfig holds Model Context Protocol configuration
type MCPConfig struct {
	Enabled bool              `yaml:"enabled" mapstructure:"enabled" envconfig:"MCP_ENABLED" default:"false"` // Enable MCP integration
	Retry   RetryConfig       `yaml:"retry" mapstructure:"retry"`                                             // Retry configuration for MCP tool calls
	Servers []MCPServerConfig `yaml:"servers" mapstructure:"servers"`                                         // List of MCP servers to connect to
}

// MCPServerConfig holds configuration for a single MCP server
type MCPServerConfig struct {
	Name          string            `yaml:"name" mapstructure:"name"`                         // Server name (for logging and identification)
	Description   string            `yaml:"description" mapstructure:"description"`           // Human-readable description
	Enabled       bool              `yaml:"enabled" mapstructure:"enabled" default:"true"`    // Enable this server
	TransportType string            `yaml:"transport_type" mapstructure:"transport_type"`     // "command", "sse", or "streamable"
	Command       *MCPCommandConfig `yaml:"command,omitempty" mapstructure:"command"`         // Command configuration (for command transport)
	Endpoint      string            `yaml:"endpoint,omitempty" mapstructure:"endpoint"`       // HTTP endpoint (for sse/streamable transport)
	Auth          *MCPAuthConfig    `yaml:"auth,omitempty" mapstructure:"auth"`               // Authentication configuration
	ToolFilter    []string          `yaml:"tool_filter,omitempty" mapstructure:"tool_filter"` // List of tool names to include (empty = all tools)
	Timeout       int               `yaml:"timeout" mapstructure:"timeout" default:"30"`      // Connection timeout in seconds
}

// MCPCommandConfig holds configuration for command-based MCP transport
type MCPCommandConfig struct {
	Path       string            `yaml:"path" mapstructure:"path"`                         // Executable path
	Args       []string          `yaml:"args,omitempty" mapstructure:"args"`               // Command arguments
	Env        map[string]string `yaml:"env,omitempty" mapstructure:"env"`                 // Additional environment variables (static values)
	InheritEnv []string          `yaml:"inherit_env,omitempty" mapstructure:"inherit_env"` // Env var names to inherit from pod environment
}

// MCPAuthConfig holds authentication configuration for MCP servers
type MCPAuthConfig struct {
	Type     string `yaml:"type" mapstructure:"type"`                     // "bearer", "basic", or "oauth2"
	TokenEnv string `yaml:"token_env,omitempty" mapstructure:"token_env"` // Environment variable containing token
	Token    string `yaml:"token,omitempty" mapstructure:"token"`         // Token value (not recommended, use token_env instead)
	Username string `yaml:"username,omitempty" mapstructure:"username"`   // Username (for basic auth)
	Password string `yaml:"password,omitempty" mapstructure:"password"`   // Password (for basic auth)
}

// A2AConfig holds Agent-to-Agent tool integration configuration
type A2AConfig struct {
	Enabled        bool                    `yaml:"enabled" mapstructure:"enabled" envconfig:"A2A_ENABLED" default:"false"` // Enable A2A tool integration
	SelfName       string                  `yaml:"self_name" mapstructure:"self_name"`                                     // This agent's identifier for loop prevention
	MaxCallDepth   int                     `yaml:"max_call_depth" mapstructure:"max_call_depth" default:"5"`               // Maximum call chain depth
	Polling        bool                    `yaml:"polling" mapstructure:"polling" default:"true"`                          // Use polling instead of streaming for sub-agents (required for large responses)
	AgentURL       string                  `yaml:"agent_url" mapstructure:"agent_url"`                                     // Public URL for this agent (for A2A discovery/agent card)
	Retry          RetryConfig             `yaml:"retry" mapstructure:"retry"`                                             // Retry configuration for A2A calls
	SubAgents      []A2ASubAgentConfig     `yaml:"sub_agents" mapstructure:"sub_agents"`                                   // List of remote ADK agents to integrate as sub-agents
	ContextCleaner A2AContextCleanerConfig `yaml:"context_cleaner" mapstructure:"context_cleaner"`                         // Context cleaner for sub-agent requests
}

// A2AContextCleanerConfig holds configuration for the A2A context cleaner interceptor
type A2AContextCleanerConfig struct {
	Enabled bool   `yaml:"enabled" mapstructure:"enabled" default:"true"`                  // Enable context cleaning before sending to sub-agents
	Model   string `yaml:"model" mapstructure:"model" default:"claude-haiku-4-5-20251001"` // Model to use for summarization
}

// A2ASubAgentConfig holds configuration for a remote ADK agent as sub-agent
type A2ASubAgentConfig struct {
	Name        string        `yaml:"name" mapstructure:"name"`               // Agent name (used in LLM instructions)
	Description string        `yaml:"description" mapstructure:"description"` // Human-readable description for LLM
	Endpoint    string        `yaml:"endpoint" mapstructure:"endpoint"`       // Agent card source URL (e.g., http://metrics-agent:9000)
	Auth        A2AAuthConfig `yaml:"auth" mapstructure:"auth"`               // Authentication configuration (api_key, bearer, or none)
	// NOTE: Timeout is not currently supported by remoteagent.NewA2A
	// The ADK library uses its own internal timeouts for A2A communication
	// This field is kept for future compatibility when/if ADK adds timeout support
	Timeout int `yaml:"timeout" mapstructure:"timeout" default:"30"` // Reserved for future use (not currently applied)
}

// A2AAuthConfig holds authentication configuration for an external agent
type A2AAuthConfig struct {
	Type     string `yaml:"type" mapstructure:"type"`                     // "api_key", "bearer", or "none"
	Header   string `yaml:"header,omitempty" mapstructure:"header"`       // Header name for api_key auth (e.g., "X-API-Key")
	KeyEnv   string `yaml:"key_env,omitempty" mapstructure:"key_env"`     // Environment variable containing API key
	TokenEnv string `yaml:"token_env,omitempty" mapstructure:"token_env"` // Environment variable containing bearer token
}

// ParallelConfig holds configuration for parallel tool execution
type ParallelConfig struct {
	Enabled         bool          `yaml:"enabled" mapstructure:"enabled" envconfig:"PARALLEL_ENABLED" default:"true"`                     // Enable parallel tool execution
	MaxParallelism  int           `yaml:"max_parallelism" mapstructure:"max_parallelism" envconfig:"PARALLEL_MAX" default:"5"`            // Maximum number of tools to execute in parallel
	ToolTimeout     time.Duration `yaml:"tool_timeout" mapstructure:"tool_timeout" envconfig:"PARALLEL_TOOL_TIMEOUT" default:"120s"`      // Timeout for individual tool execution
	SequentialTools []string      `yaml:"sequential_tools" mapstructure:"sequential_tools"`                                               // Tools that must execute sequentially (e.g., save_to_memory after search)
}

// AnthropicConfig holds Anthropic API configuration
type AnthropicConfig struct {
	APIKey string `yaml:"api_key" mapstructure:"api_key" envconfig:"ANTHROPIC_API_KEY" required:"true"`
	Model  string `yaml:"model" mapstructure:"model" envconfig:"ANTHROPIC_MODEL" default:"claude-sonnet-4-5-20250929"`
}

// SlackConfig holds Slack API configuration
type SlackConfig struct {
	Enabled            bool          `yaml:"enabled" mapstructure:"enabled" envconfig:"SLACK_ENABLED" default:"true"`                                          // Enable Slack integration (default: true for backwards compatibility)
	BotToken           string        `yaml:"bot_token" mapstructure:"bot_token" envconfig:"SLACK_BOT_TOKEN"`                                                   // Required only if enabled
	SigningSecret      string        `yaml:"signing_secret" mapstructure:"signing_secret" envconfig:"SLACK_SIGNING_SECRET"`                                    // Only required for webhook mode
	AppToken           string        `yaml:"app_token" mapstructure:"app_token" envconfig:"SLACK_APP_TOKEN"`                                                   // Only required for socket mode
	Mode               string        `yaml:"mode" mapstructure:"mode" envconfig:"SLACK_MODE" default:"webhook"`                                                // "webhook" or "socket"
	MaxFileSize        int64         `yaml:"max_file_size" mapstructure:"max_file_size" envconfig:"SLACK_MAX_FILE_SIZE" default:"10485760"`                    // 10 MB default
	ThreadCacheTTL     time.Duration `yaml:"thread_cache_ttl" mapstructure:"thread_cache_ttl" envconfig:"SLACK_THREAD_CACHE_TTL" default:"5m"`                 // How long to cache thread messages
	ThreadCacheMaxSize int           `yaml:"thread_cache_max_size" mapstructure:"thread_cache_max_size" envconfig:"SLACK_THREAD_CACHE_MAX_SIZE" default:"100"` // Max threads to keep in cache
	MaxImagesPerThread int           `yaml:"max_images_per_thread" mapstructure:"max_images_per_thread" envconfig:"SLACK_MAX_IMAGES_PER_THREAD" default:"10"`  // Max images to download per thread
	MaxThreadMessages  int           `yaml:"max_thread_messages" mapstructure:"max_thread_messages" envconfig:"SLACK_MAX_THREAD_MESSAGES" default:"0"`         // Max messages fallback limit (0 = no limit, smart trimming by bot mention is primary)
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
	AgentPort      int      `yaml:"agent_port" mapstructure:"agent_port" envconfig:"AGENT_PORT" default:"8081"`
	SlackBotPort   int      `yaml:"slack_bot_port" mapstructure:"slack_bot_port" envconfig:"SLACK_BOT_PORT" default:"8080"`
	ReadTimeout    int      `yaml:"read_timeout" mapstructure:"read_timeout" envconfig:"SERVER_READ_TIMEOUT" default:"30"`           // seconds
	WriteTimeout   int      `yaml:"write_timeout" mapstructure:"write_timeout" envconfig:"SERVER_WRITE_TIMEOUT" default:"180"`       // seconds (3 minutes for long operations)
	RequestTimeout int      `yaml:"request_timeout" mapstructure:"request_timeout" envconfig:"SERVER_REQUEST_TIMEOUT" default:"120"` // seconds (2 minutes for agent operations)
	TrustedProxies []string `yaml:"trusted_proxies" mapstructure:"trusted_proxies"`                                                  // List of trusted proxy IPs/CIDRs for X-Forwarded-For (empty = don't trust any)
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

	// Only validate Slack config if Slack is enabled
	if c.Slack.Enabled {
		if c.Slack.BotToken == "" {
			return fmt.Errorf("SLACK_BOT_TOKEN is required when Slack is enabled")
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

		// Validate sub_agents
		for i, subAgent := range c.A2A.SubAgents {
			if subAgent.Name == "" {
				return fmt.Errorf("a2a.sub_agents[%d]: name is required", i)
			}
			if subAgent.Endpoint == "" {
				return fmt.Errorf("a2a.sub_agents[%d] (%s): endpoint is required", i, subAgent.Name)
			}
			if subAgent.Description == "" {
				return fmt.Errorf("a2a.sub_agents[%d] (%s): description is required", i, subAgent.Name)
			}
		}
	}

	return nil
}

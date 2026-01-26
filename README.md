# Knowledge Agent

[![Go Version](https://img.shields.io/github/go-mod/go-version/freepik-company/knowledge-agent)](https://go.dev/)
[![License](https://img.shields.io/github/license/freepik-company/knowledge-agent)](LICENSE)
[![Release](https://img.shields.io/github/v/release/freepik-company/knowledge-agent)](https://github.com/freepik-company/knowledge-agent/releases)
[![Docker](https://img.shields.io/badge/docker-ghcr.io-blue)](https://ghcr.io/freepik-company/knowledge-agent)

Intelligent knowledge management system for teams. Built with Go, Claude Sonnet 4.5, PostgreSQL+pgvector, Redis, and Slack.

**✨ Key Differentiator**: Unlike traditional chatbots, Knowledge Agent intelligently decides when to search, save, or just respond - no commands needed.

## Features

### Core Capabilities
- 🎭 **Personalizable**: Give your agent a custom name (Anton, Ghost, Cortex, etc.) via config
- 🧠 **Auto-learning**: Analyzes conversations and saves valuable information without explicit commands
- 🔍 **Semantic Search**: Find past conversations using natural language (pgvector-powered)
- 🖼️ **Image Analysis**: Understands technical diagrams, error screenshots, architecture diagrams
- 🌐 **URL Fetching**: Analyzes documentation and web content automatically
- 🌍 **Multilingual**: Responds in Spanish, English, or any language the user writes in
- 📅 **Temporal Context**: Automatically adds dates to memories ("esta semana" → actual date)

### Security & Control
- 🔐 **Permission Control**: Restrict who can save to memory (by Slack user or service)
- 🔑 **A2A Authentication**: API key-based auth for external agents
- 🛡️ **Two-tier Auth**: Internal (Slack Bridge) + External (A2A) authentication

### Observability & Integration
- 📊 **LLM Observability**: Track costs, tokens, and performance with Langfuse integration
- 🔌 **MCP Support**: Extend with Model Context Protocol servers (filesystem, GitHub, etc.)
- 🐳 **Production-Ready**: Docker Compose, Kubernetes/Helm support, auto-migrations

## Quick Start

### Docker Compose (Recommended)

```bash
# 1. Clone repository
git clone https://github.com/freepik-company/knowledge-agent.git
cd knowledge-agent

# 2. Configure
cp config-example.yaml config.yaml
# Edit config.yaml:
#   - Add your API keys (Anthropic, Slack, etc.)
#   - Personalize agent_name (optional)

# 3. Start full stack (Postgres, Redis, Ollama, Agent)
make docker-stack

# 4. Pull embedding model (first time only)
docker exec knowledge-agent-ollama ollama pull nomic-embed-text

# 5. Agent is ready!
# Access via Slack or API at http://localhost:8081
```

### Local Development

```bash
# 1. Start infrastructure only
make docker-up

# 2. Run migrations
make migrate

# 3. Run agent locally
make dev

# Socket Mode (no ngrok needed) is default
# For Webhook Mode, see docs/CONFIGURATION.md
```

**Prerequisites**:
- Go 1.24+
- Docker & Docker Compose
- Slack workspace with bot configured ([Setup Guide](docs/CONFIGURATION.md#slack-configuration))

## Usage

```
@bot how do we deploy?                    # Ask questions
@bot remember deployments are on Tuesdays # Save information
[upload diagram] @bot this is our arch    # Analyze images
@bot check this doc https://...           # Fetch URLs
```

## Architecture

```
Slack → Webhook Bridge (:8080) → Agent (:8081) → Claude + ADK
                                              ↓
                                    PostgreSQL + Redis
```

## Commands

### Development
```bash
make dev              # Run both agent and slack bridge locally
make dev-agent        # Run only agent (no Slack)
make dev-slack        # Run only Slack bridge
make test             # Run tests
make build            # Build binaries
```

### Docker
```bash
make docker-stack     # Start full stack (recommended)
make docker-up        # Start infrastructure only
make docker-down      # Stop infrastructure
make docker-rebuild   # Rebuild and restart agent
make docker-health    # Check service health
```

### Database
```bash
make migrate          # Run migrations (manual)
make db-shell         # PostgreSQL shell
make redis-shell      # Redis shell
```

## Configuration

### Agent Personalization

Give your agent a custom identity:

```yaml
# config.yaml
agent_name: Anton  # Your team's unique name for the agent
```

Examples: `Anton`, `Ghost`, `Cortex`, `Sage`, `Echo`, `Lore`

The agent will introduce itself with this name and your team can build a unique identity around it.

### Slack Modes

**Socket Mode** (development):
```
SLACK_MODE=socket
SLACK_APP_TOKEN=xapp-...
```

**Webhook Mode** (production):
```
SLACK_MODE=webhook
SLACK_SIGNING_SECRET=...
```

See `.env.example` for all options.

## Observability (Optional)

Track LLM costs, performance, and usage with [Langfuse](https://langfuse.com):

```yaml
# config.yaml
langfuse:
  enabled: true
  public_key: ${LANGFUSE_PUBLIC_KEY}
  secret_key: ${LANGFUSE_SECRET_KEY}
  host: https://cloud.langfuse.com
  input_cost_per_1m: 3.0    # Claude Sonnet 4.5 pricing
  output_cost_per_1m: 15.0
```

**What you get:**
- ✅ Token usage and cost tracking per query
- ✅ LLM generations with full prompt/response
- ✅ Tool call tracing (search_memory, save_to_memory, fetch_url)
- ✅ Per-user cost analytics
- ✅ Performance monitoring and debugging

**SDK**: Uses `github.com/git-hulk/langfuse-go` (community-maintained, feature-complete)

See `docs/OBSERVABILITY.md` for complete guide.

## Security & Authentication

The Knowledge Agent implements a two-tier authentication model:

### Internal Authentication (Slack Bridge ↔ Agent)
Shared secret token for secure communication between slack-bot and agent services.

```bash
# Generate token
INTERNAL_AUTH_TOKEN=$(openssl rand -hex 32)

# Set in both services
export INTERNAL_AUTH_TOKEN=<your-token>
```

### External A2A Authentication (External Services → Agent)
API key authentication for direct API access from external services.

```bash
# Knowledge Agent .env
A2A_API_KEYS='{"root-agent":"ka_secret_abc123","external-service":"ka_secret_def456"}'
```

**Authentication Modes**:
- **Production** (recommended): Set both `INTERNAL_AUTH_TOKEN` and `A2A_API_KEYS`
- **Development** (open access): Leave both empty

**Example A2A Usage**:
```bash
# External service accessing agent (use the secret from config)
curl -X POST http://localhost:8081/api/query \
  -H "Content-Type: application/json" \
  -H "X-API-Key: ka_secret_abc123" \
  -d '{"question":"How do we deploy?"}'
```

See `docs/A2A.md` for complete integration guide and `docs/SECURITY.md` for detailed security configuration.

## MCP Integration (Model Context Protocol)

Extend the agent with MCP servers for filesystem, GitHub, databases, and more.

```yaml
# config.yaml
mcp:
  enabled: true
  servers:
    - name: "filesystem"
      transport_type: "command"
      command:
        path: "npx"
        args: ["-y", "@modelcontextprotocol/server-filesystem", "/workspace"]
```

**Automatic npm Package Installation**:
- **Docker**: Packages auto-detected from config.yaml and installed at startup
- **Local**: `npm install -g @modelcontextprotocol/server-filesystem`

See `docs/MCP_INTEGRATION.md` for complete guide and examples.

## Documentation

### Getting Started
- 📖 [Usage Guide](docs/USAGE_GUIDE.md) - End-user guide for interacting with the agent
- ⚙️ [Configuration](docs/CONFIGURATION.md) - Complete configuration reference
- 🚀 [Deployment](deployments/README.md) - Docker, Kubernetes, and production setup

### Advanced
- 🔌 [MCP Integration](docs/MCP_INTEGRATION.md) - Extend with external data sources
- 🔐 [Security](docs/SECURITY.md) - Authentication and permissions
- 🤖 [A2A Integration](docs/CONFIGURATION.md#agent-to-agent-a2a-authentication) - Agent-to-Agent API integration
- 📊 [Observability](docs/OPERATIONS.md) - Langfuse integration and monitoring

### Development
- 🛠️ [Claude Code Guide](CLAUDE.md) - Development with Claude Code
- 🗄️ [Production PostgreSQL](docs/PRODUCTION_POSTGRESQL.md) - pgvector setup for cloud providers
- 📝 [Implementation Summary](docs/IMPLEMENTATION_SUMMARY.md) - Architecture overview

## Contributing

We welcome contributions! Please see our [Contributing Guide](CONTRIBUTING.md) (coming soon).

Key areas for contribution:
- 🐛 Bug fixes and improvements
- 📚 Documentation enhancements
- 🔌 New MCP server integrations
- 🌍 Translations
- ✨ Feature requests (open an issue first)

## Support

- 🐛 [Report a Bug](https://github.com/freepik-company/knowledge-agent/issues/new?template=bug_report.md)
- 💡 [Request a Feature](https://github.com/freepik-company/knowledge-agent/issues/new?template=feature_request.md)
- 💬 [Discussions](https://github.com/freepik-company/knowledge-agent/discussions)
- 📧 Email: [sre@freepik.com]

## License

[MIT License](LICENSE) - Feel free to use in your projects!

## Acknowledgments

Built with:
- [Google ADK](https://github.com/google/generative-ai-go) - Agent Development Kit
- [ADK Utils Go](https://github.com/achetronic/adk-utils-go) - ADK Utils Library in Go made by @achetronic
- [Anthropic Claude](https://www.anthropic.com/claude) - LLM provider
- [pgvector](https://github.com/pgvector/pgvector) - Vector similarity search
- [Model Context Protocol](https://modelcontextprotocol.io) - Tool integration standard

---

**Made with ❤️ by the Freepik Technology Team**

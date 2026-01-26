# Docker Deployment

This directory contains Docker deployment configurations for the Knowledge Agent.

## Prerequisites

The Knowledge Agent requires **PostgreSQL with pgvector extension** for semantic search.

- ✅ **Development:** Using `docker-compose` (included in this directory) - pgvector is pre-installed in the `pgvector/pgvector:pg16` image
- ⚠️ **Production:** When using external PostgreSQL (AWS RDS, Azure, self-hosted), you must install pgvector first

**See:** `../docs/PRODUCTION_POSTGRESQL.md` for complete production setup guide

## Quick Start

### 1. Setup Environment

```bash
# Copy environment template
cp .env.example .env

# Edit .env with your values (Anthropic API key, Slack tokens, etc.)
vim .env
```

### 2. Start Full Stack

```bash
# From project root:
make docker-stack
```

This starts:
- ✅ PostgreSQL (port 5432)
- ✅ Redis (port 6379)
- ✅ Ollama (port 11434)
- ✅ Knowledge Agent (ports 8080, 8081)

### 3. Verify

```bash
# Check health
make docker-health

# View logs
make docker-stack-logs

# View agent logs only
make docker-logs-agent
```

## Available Commands

### Stack Management

```bash
# Start full stack (infrastructure + agent)
make docker-stack

# Stop full stack
make docker-stack-down

# Restart full stack
make docker-stack-restart

# View logs (all services)
make docker-stack-logs

# View agent logs only
make docker-logs-agent
```

### Infrastructure Only

```bash
# Start only infrastructure (postgres, redis, ollama)
make docker-up

# Stop infrastructure
make docker-down

# Check service health
make docker-health
```

### Agent Management

```bash
# Build agent Docker image
make docker-build

# Rebuild and restart agent
make docker-rebuild
```

### Cleanup

```bash
# Stop and remove containers (keeps volumes)
make docker-stack-down

# Clean up all Docker resources (including volumes)
make docker-prune
```

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│ Docker Network: knowledge-agent-network                 │
├─────────────────────────────────────────────────────────┤
│                                                           │
│  ┌──────────────┐   ┌──────────────┐   ┌──────────────┐│
│  │ PostgreSQL   │   │   Redis      │   │   Ollama     ││
│  │ :5432        │   │   :6379      │   │   :11434     ││
│  │ (pgvector)   │   │ (sessions)   │   │ (embeddings) ││
│  └──────┬───────┘   └──────┬───────┘   └──────┬───────┘│
│         │                  │                   │        │
│         └──────────────────┼───────────────────┘        │
│                            │                            │
│                   ┌────────▼────────┐                   │
│                   │ Knowledge Agent │                   │
│                   │   (Unified)     │                   │
│                   │                 │                   │
│                   │  :8080 Slack    │                   │
│                   │  :8081 Agent    │                   │
│                   └─────────────────┘                   │
│                                                           │
└─────────────────────────────────────────────────────────┘
```

## Configuration

### Using config.yaml (Recommended)

Mount your `config.yaml` file:

```yaml
# docker-compose.yml
services:
  agent:
    volumes:
      - ../config.yaml:/app/config/config.yaml:ro
```

The agent will automatically use it if mounted at `/app/config/config.yaml`.

### Using Environment Variables

All settings from `config.yaml` can be overridden via environment variables in `.env`:

```bash
# .env
ANTHROPIC_API_KEY=sk-ant-api03-xxxxx
SLACK_BOT_TOKEN=xoxb-xxxxx
LOG_LEVEL=debug
```

### Hybrid Approach (Best for Production)

Use `config.yaml` for structure and `.env` for secrets:

```yaml
# config.yaml
anthropic:
  api_key: ${ANTHROPIC_API_KEY}  # From .env
  model: claude-sonnet-4-5-20250929

slack:
  bot_token: ${SLACK_BOT_TOKEN}  # From .env
  mode: socket
```

```bash
# .env
ANTHROPIC_API_KEY=sk-ant-api03-xxxxx
SLACK_BOT_TOKEN=xoxb-xxxxx
```

## MCP Integration with Docker

The Docker image includes Node.js/npm for running MCP servers. You can install MCP packages in two ways:

### Option 1: Runtime Installation (Recommended for Kubernetes/Helm)

Install npm packages when the container starts using the `MCP_NPM_PACKAGES` environment variable:

```yaml
# docker-compose.yml
services:
  agent:
    environment:
      MCP_NPM_PACKAGES: "@modelcontextprotocol/server-filesystem @modelcontextprotocol/server-sqlite"
```

```yaml
# Kubernetes/Helm values.yaml
env:
  - name: MCP_NPM_PACKAGES
    value: "@modelcontextprotocol/server-filesystem @modelcontextprotocol/server-sqlite @modelcontextprotocol/server-github"
```

**Advantages:**
- ✅ Flexible - change packages without rebuilding image
- ✅ Smaller base image
- ✅ Perfect for Kubernetes/Helm deployments

**Disadvantages:**
- ⚠️ Slightly slower startup (packages install on first start)
- ⚠️ Requires internet connection at startup

### Option 2: Build-time Installation (Faster startup)

Install packages during Docker image build using build args:

```yaml
# docker-compose.yml
services:
  agent:
    build:
      args:
        NPM_PACKAGES: "@modelcontextprotocol/server-filesystem @modelcontextprotocol/server-sqlite"
```

```bash
# Command line
docker build --build-arg NPM_PACKAGES="@modelcontextprotocol/server-filesystem @modelcontextprotocol/server-sqlite" .
```

**Advantages:**
- ✅ Faster startup (packages pre-installed)
- ✅ No internet required at startup
- ✅ Better for air-gapped environments

**Disadvantages:**
- ⚠️ Larger image size
- ⚠️ Must rebuild image to change packages

### Option 3: Hybrid (Build-time + Runtime)

Combine both approaches - common packages at build-time, dynamic ones at runtime:

```yaml
# docker-compose.yml
services:
  agent:
    build:
      args:
        NPM_PACKAGES: "@modelcontextprotocol/server-filesystem"  # Always needed
    environment:
      MCP_NPM_PACKAGES: "@modelcontextprotocol/server-github"  # Optional, environment-specific
```

### Examples

See `deployments/examples/` for complete examples:
- `docker-compose.mcp.yaml` - Both build-time and runtime examples
- `helm-values.yaml` - Kubernetes/Helm configuration

## Database Migrations

Migrations run automatically when the Knowledge Agent starts. The application checks for pending migrations and applies them before starting the agent service.

**How it works:**
- Migration files are embedded in the application binary
- A `schema_migrations` table tracks applied migrations
- Each migration runs in a transaction (atomic, rollback on error)
- Safe to run multiple times - already-applied migrations are skipped

**Migration files location:** `internal/migrations/sql/`

**View migration status:**
```bash
# Check PostgreSQL for applied migrations
make db-shell
# Then in psql:
SELECT * FROM schema_migrations ORDER BY version;
```

**Add new migrations:**
1. Create new file: `internal/migrations/sql/00X_description.sql`
2. Version numbers must be sequential (001, 002, 003, etc.)
3. Rebuild and restart - new migrations apply automatically

## Ollama Models

Download embedding model:

```bash
# Enter Ollama container
docker exec -it knowledge-agent-ollama ollama pull nomic-embed-text

# Verify
docker exec knowledge-agent-ollama ollama list
```

## Scaling

### Separate Agent and Slack Bridge

For high-traffic deployments, run agent and slack-bot separately:

1. Uncomment `agent-only` and `slack-bot-only` services in `docker-compose.yml`
2. Comment out the unified `agent` service
3. Start:

```bash
cd deployments
docker-compose up -d agent-only slack-bot-only
```

### Multiple Agent Instances

Run multiple agent instances behind a load balancer:

```bash
# Scale agent to 3 instances
cd deployments
docker-compose up -d --scale agent-only=3
```

Configure load balancer (nginx, traefik) to distribute requests.

## Troubleshooting

### Agent Not Starting

```bash
# Check logs
make docker-logs-agent

# Common issues:
# - Missing API key: Check .env has ANTHROPIC_API_KEY
# - Database not ready: Wait for postgres to be healthy
# - Port conflict: Check if ports 8080/8081 are free
```

### Database Connection Refused

```bash
# Check PostgreSQL
docker exec knowledge-agent-postgres pg_isready -U postgres

# If unhealthy, restart
docker-compose restart postgres

# View postgres logs
docker-compose logs postgres
```

### Ollama Not Responding

```bash
# Check Ollama
docker exec knowledge-agent-ollama ollama list

# If empty, pull model
docker exec knowledge-agent-ollama ollama pull nomic-embed-text
```

### MCP Packages Not Installing

```bash
# Check npm is available in container
docker exec knowledge-agent npm --version

# Check MCP_NPM_PACKAGES value
docker exec knowledge-agent env | grep MCP_NPM_PACKAGES

# View installation logs
docker logs knowledge-agent | grep "npm"
```

### High Memory Usage

Ollama can use significant memory for embeddings:

```yaml
# docker-compose.yml - limit resources
services:
  ollama:
    deploy:
      resources:
        limits:
          memory: 4G
```

## Production Checklist

### Database
- [ ] **PostgreSQL has pgvector extension installed** (see `docs/PRODUCTION_POSTGRESQL.md`)
- [ ] Verify: `CREATE EXTENSION IF NOT EXISTS vector;` works
- [ ] Database user has CREATE permission (for migrations)
- [ ] Back up PostgreSQL data regularly
- [ ] Monitor disk usage (PostgreSQL, Ollama models)

### Security
- [ ] Set strong `INTERNAL_AUTH_TOKEN`
- [ ] Configure `A2A_API_KEYS` with unique tokens
- [ ] Use HTTPS reverse proxy (nginx, caddy)
- [ ] Use Docker secrets for sensitive data
- [ ] Use `sslmode=require` in PostgreSQL connection string

### Configuration
- [ ] Use `webhook` mode for Slack (not `socket`)
- [ ] Set `LOG_FORMAT=json` for structured logging
- [ ] Configure `LOG_LEVEL=info` (not debug)
- [ ] Enable Langfuse for observability
- [ ] Pre-install MCP packages (build-time or runtime)

### Operations
- [ ] Set up monitoring (Prometheus, Grafana)
- [ ] Configure log aggregation (ELK, Loki)
- [ ] Set resource limits in `docker-compose.yml`
- [ ] Enable automatic restarts: `restart: unless-stopped`
- [ ] Set up alerts for service health

## Security

### Don't Commit Secrets

```gitignore
# .gitignore
.env
deployments/.env
```

### Use Docker Secrets (Swarm)

```yaml
# docker-compose.yml
secrets:
  anthropic_key:
    file: ./secrets/anthropic_key.txt

services:
  agent:
    secrets:
      - anthropic_key
```

### Network Isolation

Services communicate via internal Docker network. Only expose necessary ports.

### Read-Only Root Filesystem

```yaml
# docker-compose.yml
services:
  agent:
    read_only: true
    tmpfs:
      - /tmp
```

## Monitoring

### Health Checks

All services have health checks:

```bash
# Check all
docker ps --format "table {{.Names}}\t{{.Status}}"

# Check specific service
docker inspect knowledge-agent --format='{{.State.Health.Status}}'
```

### Logs

```bash
# Tail all logs
make docker-stack-logs

# View specific service
docker-compose logs -f agent

# Export logs
docker-compose logs > logs.txt
```

### Metrics

Expose Prometheus metrics:

```go
// Add to agent
http.Handle("/metrics", promhttp.Handler())
```

## Backup & Restore

### PostgreSQL

```bash
# Backup
docker exec knowledge-agent-postgres pg_dump -U postgres knowledge_agent > backup.sql

# Restore
docker exec -i knowledge-agent-postgres psql -U postgres knowledge_agent < backup.sql
```

### Volumes

```bash
# Backup volumes
docker run --rm \
  -v knowledge-agent_postgres_data:/data \
  -v $(pwd):/backup \
  alpine tar czf /backup/postgres_backup.tar.gz /data

# Restore
docker run --rm \
  -v knowledge-agent_postgres_data:/data \
  -v $(pwd):/backup \
  alpine tar xzf /backup/postgres_backup.tar.gz -C /
```

## References

- **Docker Compose Docs**: https://docs.docker.com/compose/
- **pgvector**: https://github.com/pgvector/pgvector
- **Ollama**: https://ollama.ai/
- **MCP Specification**: https://spec.modelcontextprotocol.io
- **Knowledge Agent Docs**: `../docs/`

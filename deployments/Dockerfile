# Multi-stage Dockerfile for Knowledge Agent (Unified Binary)

# Stage 1: Builder
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make gcc musl-dev

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build unified binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo \
    -ldflags="-w -s" \
    -o /bin/knowledge-agent ./cmd/knowledge-agent/main.go

# Stage 2: Runtime (default - runs all services)
FROM alpine:latest AS runtime

# Install runtime dependencies
# - ca-certificates: for HTTPS requests
# - wget: for health checks
# - nodejs npm: for MCP servers (optional but recommended)
RUN apk --no-cache add ca-certificates wget nodejs npm

WORKDIR /app

# Copy unified binary
COPY --from=builder /bin/knowledge-agent /app/knowledge-agent

# Copy entrypoint script
COPY deployments/docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod +x /app/docker-entrypoint.sh

# Create directories for config, logs, and MCP workspace
RUN mkdir -p /app/config /app/logs /tmp/mcp-workspace

# =============================================================================
# NPM Package Pre-installation (Build-time - Optional)
# =============================================================================
# Pre-install npm packages at BUILD time using build args
#
# Example:
#   docker build --build-arg NPM_PACKAGES="@modelcontextprotocol/server-filesystem @modelcontextprotocol/server-sqlite" .
#
# Or in docker-compose.yml:
#   build:
#     args:
#       NPM_PACKAGES: "@modelcontextprotocol/server-filesystem @modelcontextprotocol/server-sqlite"
#
# Or in Helm values.yaml:
#   image:
#     buildArgs:
#       NPM_PACKAGES: "@modelcontextprotocol/server-filesystem @modelcontextprotocol/server-sqlite"
# =============================================================================
ARG NPM_PACKAGES=""
RUN if [ -n "$NPM_PACKAGES" ]; then \
        echo "📦 Installing npm packages at build time: $NPM_PACKAGES"; \
        npm install -g $NPM_PACKAGES || echo "⚠️  Package installation failed"; \
        echo "✅ Packages installed at build time"; \
    fi

# Expose both ports (agent and slack-bot)
EXPOSE 8080 8081

# Default environment variables
ENV MODE=all
ENV CONFIG_PATH=/app/config/config.yaml
# Optional: Install npm packages at runtime (overrides build-time)
# ENV MCP_NPM_PACKAGES=""

# Health check (checks agent service)
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8081/health || exit 1

# Use entrypoint script for flexible startup
ENTRYPOINT ["/app/docker-entrypoint.sh"]

# Stage 3: Agent-only (for separate scaling)
FROM runtime AS agent

ENV MODE=agent
EXPOSE 8081

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8081/health || exit 1

# Stage 4: Slack-bot-only (for separate scaling)
FROM runtime AS slack-bot

ENV MODE=slack-bot
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Legacy stages for backward compatibility (deprecated)
# These build separate binaries from the old cmd/agent and cmd/slack-bot
FROM builder AS builder-legacy
RUN if [ -f ./cmd/agent/main.go ]; then \
      CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /bin/agent-legacy ./cmd/agent/main.go; \
    fi
RUN if [ -f ./cmd/slack-bot/main.go ]; then \
      CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /bin/slack-bot-legacy ./cmd/slack-bot/main.go; \
    fi

.PHONY: help build package package-signature dev docker-up docker-down docker-logs test integration-test clean

# Load .env file for development
ifneq (,$(wildcard ./.env))
    include .env
    export
endif

# Docker image name (can be overridden by CI)
IMG ?= knowledge-agent:latest

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

build: ## Build unified binary
	@echo "🔨 Building unified binary..."
	@mkdir -p bin
	@go build -o bin/knowledge-agent cmd/knowledge-agent/main.go
	@echo "✅ Build complete: bin/knowledge-agent"
	@echo ""
	@echo "Usage:"
	@echo "  ./bin/knowledge-agent --mode all        # Run both services (default)"
	@echo "  ./bin/knowledge-agent --mode agent      # Run agent only"
	@echo "  ./bin/knowledge-agent --mode slack-bot  # Run Slack bridge only"

package: ## Create release package (requires GOOS, GOARCH, PACKAGE_NAME env vars)
	@echo "📦 Creating release package..."
	@mkdir -p dist
	@tar -czf dist/$(PACKAGE_NAME) -C bin knowledge-agent
	@echo "✅ Package created: dist/$(PACKAGE_NAME)"

package-signature: ## Create MD5 signature for package (requires PACKAGE_NAME env var)
	@echo "🔏 Creating package signature..."
	@cd dist && md5sum $(PACKAGE_NAME) > $(PACKAGE_NAME).md5
	@echo "✅ Signature created: dist/$(PACKAGE_NAME).md5"

dev: ## Run all services (unified binary). Optional: CONFIG=config.yaml
	@echo "🚀 Starting Knowledge Agent system (unified binary)..."
	@echo ""
	@echo "Starting services:"
	@echo "  • Knowledge Agent (port 8081)"
	@echo "  • Slack Webhook Bridge (port 8080)"
	@trap 'kill 0' EXIT; \
	CONFIG_FILE=$${CONFIG:-config.yaml}; \
	if [ -f "$$CONFIG_FILE" ]; then \
		echo "  • Using config: $$CONFIG_FILE"; \
		cd cmd/knowledge-agent && exec go run main.go --config ../../$$CONFIG_FILE --mode all; \
	else \
		echo "  • No config file (using environment variables)"; \
		cd cmd/knowledge-agent && exec go run main.go --mode all; \
	fi

dev-agent: ## Run agent service only (unified binary). Optional: CONFIG=config.yaml
	@echo "🤖 Starting Knowledge Agent..."
	@trap 'kill 0' EXIT; \
	if [ -n "$(CONFIG)" ]; then \
		echo "Using config: $(CONFIG)"; \
		cd cmd/knowledge-agent && exec go run main.go --config ../../$(CONFIG) --mode agent; \
	else \
		cd cmd/knowledge-agent && exec go run main.go --mode agent; \
	fi

dev-slack: ## Run slack bridge only (unified binary). Optional: CONFIG=config.yaml
	@echo "💬 Starting Slack Webhook Bridge..."
	@trap 'kill 0' EXIT; \
	if [ -n "$(CONFIG)" ]; then \
		echo "Using config: $(CONFIG)"; \
		cd cmd/knowledge-agent && exec go run main.go --config ../../$(CONFIG) --mode slack-bot; \
	else \
		cd cmd/knowledge-agent && exec go run main.go --mode slack-bot; \
	fi

docker-up: ## Start Docker infrastructure only (postgres, redis, ollama)
	@echo "🐳 Starting Docker infrastructure services..."
	@cd deployments && docker-compose up -d postgres redis ollama
	@echo "✅ Infrastructure services started"
	@echo "⏳ Waiting for services to be healthy..."
	@sleep 3
	@$(MAKE) docker-health

docker-down: ## Stop all Docker services
	@echo "🛑 Stopping Docker services..."
	@cd deployments && docker-compose down
	@echo "✅ Services stopped"

docker-logs: ## Show Docker logs (all services)
	@cd deployments && docker-compose logs -f

docker-logs-agent: ## Show agent logs only
	@cd deployments && docker-compose logs -f agent

docker-health: ## Check health of Docker services
	@echo "🔍 Checking service health..."
	@docker exec knowledge-agent-postgres pg_isready -U postgres && echo "✅ PostgreSQL healthy" || echo "❌ PostgreSQL unhealthy"
	@docker exec knowledge-agent-redis redis-cli ping > /dev/null && echo "✅ Redis healthy" || echo "❌ Redis unhealthy"
	@curl -s http://localhost:11434/api/tags > /dev/null && echo "✅ Ollama healthy" || echo "❌ Ollama unhealthy"
	@if docker ps --format '{{.Names}}' | grep -q knowledge-agent; then \
		curl -s http://localhost:8081/health > /dev/null && echo "✅ Agent healthy" || echo "❌ Agent unhealthy"; \
	fi

docker-compose-build: ## Build Docker image for agent (via docker-compose)
	@echo "🔨 Building Docker image..."
	@cd deployments && docker-compose build agent
	@echo "✅ Image built successfully"

docker-stack: ## Start full stack (infrastructure + agent) in Docker
	@echo "🚀 Starting full Knowledge Agent stack in Docker..."
	@echo ""
	@echo "Starting services:"
	@echo "  • PostgreSQL (port 5432)"
	@echo "  • Redis (port 6379)"
	@echo "  • Ollama (port 11434)"
	@echo "  • Knowledge Agent (ports 8080, 8081)"
	@echo ""
	@cd deployments && docker-compose up -d
	@echo ""
	@echo "⏳ Waiting for services to be healthy..."
	@sleep 5
	@$(MAKE) docker-health
	@echo ""
	@echo "✅ Full stack is running!"
	@echo ""
	@echo "Endpoints:"
	@echo "  • Agent API:      http://localhost:8081"
	@echo "  • Slack Bridge:   http://localhost:8080"
	@echo "  • Health Check:   http://localhost:8081/health"
	@echo ""
	@echo "View logs with: make docker-stack-logs"

docker-stack-down: ## Stop full stack (all services)
	@echo "🛑 Stopping full Knowledge Agent stack..."
	@cd deployments && docker-compose down
	@echo "✅ Stack stopped"

docker-stack-logs: ## Show logs from full stack
	@cd deployments && docker-compose logs -f

docker-stack-restart: ## Restart full stack
	@echo "🔄 Restarting full stack..."
	@$(MAKE) docker-stack-down
	@sleep 2
	@$(MAKE) docker-stack

docker-rebuild: ## Rebuild and restart agent container
	@echo "🔨 Rebuilding agent..."
	@cd deployments && docker-compose build agent
	@echo "🔄 Restarting agent..."
	@cd deployments && docker-compose up -d agent
	@echo "✅ Agent rebuilt and restarted"
	@echo ""
	@echo "View logs with: make docker-logs-agent"

docker-prune: ## Clean up Docker resources (images, volumes, etc.)
	@echo "🧹 Cleaning up Docker resources..."
	@docker system prune -af --volumes
	@echo "✅ Docker resources cleaned"

test: ## Run unit tests
	@echo "🧪 Running unit tests..."
	@go test -v -race -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out

integration-test: ## Run integration tests (requires running services)
	@echo "🧪 Running integration tests..."
	@go test -v -race -tags=integration ./tests/integration/...

integration-test-short: ## Run integration tests (skip long tests)
	@echo "🧪 Running integration tests (short mode)..."
	@go test -v -race -tags=integration -short ./tests/integration/...

integration-test-username: ## Run user name integration tests
	@echo "🧪 Running username integration tests..."
	@go test -v -tags=integration ./tests/integration/ -run TestUserName

integration-test-binary: ## Run binary mode integration tests
	@echo "🧪 Running binary mode tests..."
	@go test -v -tags=integration ./tests/integration/ -run TestBinaryMode

integration-test-prompt: ## Run prompt reload integration tests
	@echo "🧪 Running prompt reload tests..."
	@go test -v -tags=integration ./tests/integration/ -run TestPrompt

integration-test-ratelimit: ## Run rate limiting integration tests
	@echo "🧪 Running rate limiting tests..."
	@go test -v -tags=integration ./tests/integration/ -run TestRateLimiting

test-webhook: ## Test the webhook endpoint with example data
	@echo "🔧 Testing webhook endpoint..."
	@cd examples && ./test_webhook.sh

test-webhook-custom: ## Test webhook with custom JSON file (usage: make test-webhook-custom FILE=mythread.json)
	@echo "🔧 Testing webhook with custom file..."
	@cd examples && ./test_webhook.sh ../$(FILE)

test-query: ## Test the query endpoint with example data
	@echo "🔍 Testing query endpoint..."
	@cd examples && ./test_query.sh

test-query-custom: ## Test query with custom JSON file (usage: make test-query-custom FILE=myquery.json)
	@echo "🔍 Testing query with custom file..."
	@cd examples && ./test_query.sh ../$(FILE)

test-auth: ## Test authentication (requires agent running)
	@echo "🔒 Testing authentication..."
	@./scripts/test-auth.sh

gen-token: ## Generate authentication token (usage: make gen-token TYPE=internal or make gen-token TYPE=a2a SERVICE=myservice)
	@./scripts/generate-auth-token.sh $(TYPE) $(SERVICE)

clean: ## Clean build artifacts
	@echo "🧹 Cleaning..."
	@rm -rf bin/
	@rm -f coverage.out
	@echo "✅ Clean complete"

cleanup: ## Kill all knowledge-agent processes (use if stuck after Ctrl+C)
	@./scripts/cleanup-processes.sh

setup: ## Initial development environment setup
	@./scripts/setup-dev.sh

deps: ## Install/update dependencies
	@echo "📦 Updating dependencies..."
	@go mod download
	@go mod tidy
	@echo "✅ Dependencies updated"

fmt: ## Format code
	@echo "🎨 Formatting code..."
	@go fmt ./...
	@echo "✅ Code formatted"

lint: ## Run linter (requires golangci-lint)
	@echo "🔍 Running linter..."
	@golangci-lint run ./...
	@echo "✅ Lint complete"

db-shell: ## Open PostgreSQL shell
	@docker exec -it knowledge-agent-postgres psql -U postgres -d knowledge_agent

redis-shell: ## Open Redis shell
	@docker exec -it knowledge-agent-redis redis-cli

ollama-models: ## List Ollama models
	@curl -s http://localhost:11434/api/tags | jq '.models'

# Docker build targets
docker-build: ## Build Docker image (unified binary)
	@echo "🐳 Building Docker image..."
	@docker build -t $(IMG) -f deployments/Dockerfile --target runtime .
	@echo "✅ Docker image built: $(IMG)"
	@echo ""
	@echo "Run with different modes:"
	@echo "  docker run -e MODE=all $(IMG)        # Both services"
	@echo "  docker run -e MODE=agent $(IMG)      # Agent only"
	@echo "  docker run -e MODE=slack-bot $(IMG)  # Slack bridge only"

docker-push: ## Push Docker image to registry
	@echo "📤 Pushing Docker image..."
	@docker push $(IMG)
	@echo "✅ Image pushed: $(IMG)"

docker-buildx: ## Build and push multi-arch Docker image (amd64, arm64)
	@echo "🐳 Building multi-arch Docker image with buildx..."
	@docker buildx build \
		--platform linux/amd64,linux/arm64 \
		--tag $(IMG) \
		--tag $(shell echo $(IMG) | sed 's/:.*/:latest/') \
		--file deployments/Dockerfile \
		--target runtime \
		--push \
		.
	@echo "✅ Multi-arch image built and pushed: $(IMG)"
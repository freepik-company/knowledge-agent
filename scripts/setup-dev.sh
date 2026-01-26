#!/bin/bash

set -e

echo "🚀 Setting up Knowledge Agent development environment..."

# Check prerequisites
echo "📋 Checking prerequisites..."

if ! command -v docker &> /dev/null; then
    echo "❌ Docker is not installed. Please install Docker Desktop."
    exit 1
fi

if ! command -v go &> /dev/null; then
    echo "❌ Go is not installed. Please install Go 1.24+."
    exit 1
fi

GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
echo "✅ Docker installed"
echo "✅ Go $GO_VERSION installed"

# Create .env from template if not exists
if [ ! -f .env ]; then
    echo "📝 Creating .env file from template..."
    cp .env.example .env
    echo "⚠️  Please edit .env file with your API keys before continuing."
    echo "   Required: ANTHROPIC_API_KEY, SLACK_BOT_TOKEN, SLACK_SIGNING_SECRET"
    read -p "Press enter when ready to continue..."
fi

# Start Docker services
echo "🐳 Starting Docker services..."
cd deployments
docker-compose up -d postgres redis ollama
cd ..

# Wait for services to be healthy
echo "⏳ Waiting for services to be healthy..."
sleep 5

# Check PostgreSQL
echo "🔍 Checking PostgreSQL..."
until docker exec knowledge-agent-postgres pg_isready -U postgres > /dev/null 2>&1; do
    echo "   Waiting for PostgreSQL..."
    sleep 2
done
echo "✅ PostgreSQL is ready"

# Check Redis
echo "🔍 Checking Redis..."
until docker exec knowledge-agent-redis redis-cli ping > /dev/null 2>&1; do
    echo "   Waiting for Redis..."
    sleep 2
done
echo "✅ Redis is ready"

# Check Ollama
echo "🔍 Checking Ollama..."
until curl -s http://localhost:11434/api/tags > /dev/null 2>&1; do
    echo "   Waiting for Ollama..."
    sleep 2
done
echo "✅ Ollama is ready"

# Pull Ollama model
echo "📥 Pulling nomic-embed-text model (this may take a few minutes)..."
docker exec knowledge-agent-ollama ollama pull nomic-embed-text
echo "✅ Embedding model ready"

# Run database migrations
echo "🗄️  Running database migrations..."
./scripts/migrate-db.sh

# Install Go dependencies
echo "📦 Installing Go dependencies..."
go mod download
go mod tidy

# Build binaries
echo "🔨 Building binaries..."
mkdir -p bin
go build -o bin/agent cmd/agent/main.go 2>/dev/null || echo "⚠️  Agent not yet implemented, skipping..."
go build -o bin/slack-bot cmd/slack-bot/main.go 2>/dev/null || echo "⚠️  Slack bot not yet implemented, skipping..."

echo ""
echo "✅ Development environment setup complete!"
echo ""
echo "📚 Next steps:"
echo "   1. Edit .env file with your API keys"
echo "   2. Run 'make dev' to start development servers"
echo "   3. For Socket Mode: No additional setup needed"
echo "   4. For Webhook Mode: Configure public domain in Slack Event Subscriptions"
echo ""

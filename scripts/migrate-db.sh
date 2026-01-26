#!/bin/bash

set -e

echo "🗄️  Running database migrations..."

# Check if PostgreSQL is running
if ! docker exec knowledge-agent-postgres pg_isready -U postgres > /dev/null 2>&1; then
    echo "❌ PostgreSQL is not running. Start it with: make docker-up"
    exit 1
fi

# Run migrations in order
for migration in migrations/*.sql; do
    echo "   Applying $(basename $migration)..."
    docker exec -i knowledge-agent-postgres psql -U postgres -d knowledge_agent < "$migration"
done

echo "✅ Migrations completed successfully"

# Verify pgvector extension
echo "🔍 Verifying pgvector extension..."
docker exec knowledge-agent-postgres psql -U postgres -d knowledge_agent -c "SELECT extname, extversion FROM pg_extension WHERE extname = 'vector';"

echo "✅ Database setup complete"

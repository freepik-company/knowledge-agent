#!/bin/bash

# Test the Knowledge Agent query endpoint
# Usage: ./test_query.sh [path/to/query.json]

set -e

AGENT_URL="${AGENT_URL:-http://localhost:8081}"
QUERY_FILE="${1:-test_query.json}"

echo "🔍 Testing Knowledge Agent Query Endpoint"
echo "=========================================="
echo ""
echo "Agent URL: $AGENT_URL"
echo "Query file: $QUERY_FILE"
echo ""

if [ ! -f "$QUERY_FILE" ]; then
    echo "❌ Error: Query file not found: $QUERY_FILE"
    exit 1
fi

echo "📤 Sending query to agent..."
echo ""

response=$(curl -s -X POST \
    -H "Content-Type: application/json" \
    -d @"$QUERY_FILE" \
    "$AGENT_URL/api/query")

echo "📥 Response from agent:"
echo "======================"
echo ""
echo "$response" | jq '.'

echo ""

# Check if successful
success=$(echo "$response" | jq -r '.success')
if [ "$success" = "true" ]; then
    echo "✅ Query answered successfully!"
    echo ""
    echo "Answer:"
    echo "$response" | jq -r '.answer'
else
    echo "❌ Query failed"
    echo ""
    echo "Error:"
    echo "$response" | jq -r '.message'
fi

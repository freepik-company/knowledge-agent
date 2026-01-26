#!/bin/bash
#
# Test authentication for Knowledge Agent
#
# Usage:
#   ./scripts/test-auth.sh
#

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
AGENT_URL="http://localhost:8081"
INTERNAL_TOKEN="e02dcab50bd6956a870c7fe6cf276179765559f47b499b300b79773513ea3866"
VALID_API_KEY="ka_rootagent_secret_abc123"

echo "======================================"
echo "  Knowledge Agent Authentication Tests"
echo "======================================"
echo ""

# Check if agent is running
echo "Checking if agent is running..."
if ! curl -s --max-time 2 "$AGENT_URL/health" > /dev/null 2>&1; then
    echo -e "${RED}❌ Agent is not running on $AGENT_URL${NC}"
    echo "Start it with: make dev-agent"
    exit 1
fi
echo -e "${GREEN}✓ Agent is running${NC}"
echo ""

# Test 1: No authentication
echo "======================================"
echo "Test 1: No Authentication (should fail)"
echo "======================================"
RESPONSE=$(curl -s -w "\n%{http_code}" -X POST "$AGENT_URL/api/query" \
  -H "Content-Type: application/json" \
  -d '{"question":"test without auth","channel_id":"test"}')

HTTP_CODE=$(echo "$RESPONSE" | tail -n 1)
BODY=$(echo "$RESPONSE" | sed '$d')

echo "HTTP Status: $HTTP_CODE"
echo "Response: $BODY"

if [ "$HTTP_CODE" = "401" ]; then
    echo -e "${GREEN}✓ PASS: Correctly rejected (401)${NC}"
else
    echo -e "${RED}✗ FAIL: Expected 401, got $HTTP_CODE${NC}"
fi
echo ""

# Test 2: Invalid API Key
echo "======================================"
echo "Test 2: Invalid API Key (should fail)"
echo "======================================"
RESPONSE=$(curl -s -w "\n%{http_code}" -X POST "$AGENT_URL/api/query" \
  -H "Content-Type: application/json" \
  -H "X-API-Key: ka_invalid_key_12345" \
  -d '{"question":"test with invalid key","channel_id":"test"}')

HTTP_CODE=$(echo "$RESPONSE" | tail -n 1)
BODY=$(echo "$RESPONSE" | sed '$d')

echo "HTTP Status: $HTTP_CODE"
echo "Response: $BODY"

if [ "$HTTP_CODE" = "401" ]; then
    echo -e "${GREEN}✓ PASS: Correctly rejected (401)${NC}"
else
    echo -e "${RED}✗ FAIL: Expected 401, got $HTTP_CODE${NC}"
fi
echo ""

# Test 3: Valid API Key
echo "======================================"
echo "Test 3: Valid API Key (should succeed)"
echo "======================================"
RESPONSE=$(curl -s -w "\n%{http_code}" -X POST "$AGENT_URL/api/query" \
  -H "Content-Type: application/json" \
  -H "X-API-Key: $VALID_API_KEY" \
  -d '{"question":"¿Qué información tienes guardada?","channel_id":"test"}')

HTTP_CODE=$(echo "$RESPONSE" | tail -n 1)
BODY=$(echo "$RESPONSE" | sed '$d')

echo "HTTP Status: $HTTP_CODE"
echo "Response (truncated): $(echo "$BODY" | head -c 200)..."

if [ "$HTTP_CODE" = "200" ]; then
    echo -e "${GREEN}✓ PASS: Successfully authenticated (200)${NC}"
else
    echo -e "${RED}✗ FAIL: Expected 200, got $HTTP_CODE${NC}"
fi
echo ""

# Test 4: Valid Internal Token
echo "======================================"
echo "Test 4: Valid Internal Token (should succeed)"
echo "======================================"
RESPONSE=$(curl -s -w "\n%{http_code}" -X POST "$AGENT_URL/api/query" \
  -H "Content-Type: application/json" \
  -H "X-Internal-Token: $INTERNAL_TOKEN" \
  -d '{"question":"test from slack-bot","channel_id":"test"}')

HTTP_CODE=$(echo "$RESPONSE" | tail -n 1)
BODY=$(echo "$RESPONSE" | sed '$d')

echo "HTTP Status: $HTTP_CODE"
echo "Response (truncated): $(echo "$BODY" | head -c 200)..."

if [ "$HTTP_CODE" = "200" ]; then
    echo -e "${GREEN}✓ PASS: Successfully authenticated (200)${NC}"
else
    echo -e "${RED}✗ FAIL: Expected 200, got $HTTP_CODE${NC}"
fi
echo ""

# Test 5: Invalid Internal Token
echo "======================================"
echo "Test 5: Invalid Internal Token (should fail)"
echo "======================================"
RESPONSE=$(curl -s -w "\n%{http_code}" -X POST "$AGENT_URL/api/query" \
  -H "Content-Type: application/json" \
  -H "X-Internal-Token: invalid_token_abc123" \
  -d '{"question":"test with bad token","channel_id":"test"}')

HTTP_CODE=$(echo "$RESPONSE" | tail -n 1)
BODY=$(echo "$RESPONSE" | sed '$d')

echo "HTTP Status: $HTTP_CODE"
echo "Response: $BODY"

if [ "$HTTP_CODE" = "401" ]; then
    echo -e "${GREEN}✓ PASS: Correctly rejected (401)${NC}"
else
    echo -e "${RED}✗ FAIL: Expected 401, got $HTTP_CODE${NC}"
fi
echo ""

# Test 6: Ingest endpoint with valid auth
echo "======================================"
echo "Test 6: Ingest Thread with Valid API Key"
echo "======================================"
RESPONSE=$(curl -s -w "\n%{http_code}" -X POST "$AGENT_URL/api/ingest-thread" \
  -H "Content-Type: application/json" \
  -H "X-API-Key: $VALID_API_KEY" \
  -d '{
    "thread_ts": "test-thread-123",
    "channel_id": "test-channel",
    "messages": [
      {"user": "U123", "text": "Test message 1", "ts": "1234567890.123", "type": "message"},
      {"user": "U456", "text": "Test message 2", "ts": "1234567891.123", "type": "message"}
    ]
  }')

HTTP_CODE=$(echo "$RESPONSE" | tail -n 1)
BODY=$(echo "$RESPONSE" | sed '$d')

echo "HTTP Status: $HTTP_CODE"
echo "Response (truncated): $(echo "$BODY" | head -c 200)..."

if [ "$HTTP_CODE" = "200" ]; then
    echo -e "${GREEN}✓ PASS: Successfully authenticated (200)${NC}"
else
    echo -e "${RED}✗ FAIL: Expected 200, got $HTTP_CODE${NC}"
fi
echo ""

# Summary
echo "======================================"
echo "  Test Summary"
echo "======================================"
echo ""
echo "Authentication is working correctly if:"
echo "  • Tests 1, 2, 5 returned 401 (rejected)"
echo "  • Tests 3, 4, 6 returned 200 (accepted)"
echo ""
echo "Security model:"
echo "  ✓ Internal Token: Slack-bot → Agent communication"
echo "  ✓ API Keys: External services → Agent communication"
echo "  ✗ No auth: Blocked (401 Unauthorized)"
echo ""

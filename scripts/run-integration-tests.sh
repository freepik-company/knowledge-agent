#!/bin/bash

set -e

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${YELLOW}🧪 Knowledge Agent Integration Test Runner${NC}"
echo ""

# Check if services are required
CHECK_SERVICES=${CHECK_SERVICES:-true}

if [ "$CHECK_SERVICES" = "true" ]; then
    echo -e "${YELLOW}Checking required services...${NC}"

    # Check PostgreSQL
    if ! docker exec knowledge-agent-postgres pg_isready -U postgres > /dev/null 2>&1; then
        echo -e "${RED}❌ PostgreSQL not running${NC}"
        echo "Start with: make docker-up"
        exit 1
    fi
    echo -e "${GREEN}✅ PostgreSQL running${NC}"

    # Check Redis
    if ! docker exec knowledge-agent-redis redis-cli ping > /dev/null 2>&1; then
        echo -e "${RED}❌ Redis not running${NC}"
        echo "Start with: make docker-up"
        exit 1
    fi
    echo -e "${GREEN}✅ Redis running${NC}"

    # Check Ollama
    if ! curl -s http://localhost:11434/api/tags > /dev/null 2>&1; then
        echo -e "${YELLOW}⚠️  Ollama not running (may affect some tests)${NC}"
    else
        echo -e "${GREEN}✅ Ollama running${NC}"
    fi

    # Check if agent is running
    if curl -s http://localhost:8081/health > /dev/null 2>&1; then
        echo -e "${GREEN}✅ Knowledge Agent running${NC}"
    else
        echo -e "${YELLOW}⚠️  Knowledge Agent not running${NC}"
        echo "Some tests may fail. Start with: make dev"
    fi

    echo ""
fi

# Parse test type
TEST_TYPE=${1:-all}

case $TEST_TYPE in
    all)
        echo -e "${YELLOW}Running all integration tests...${NC}"
        go test -v -race -tags=integration ./tests/integration/...
        ;;
    short)
        echo -e "${YELLOW}Running integration tests (short mode)...${NC}"
        go test -v -race -tags=integration -short ./tests/integration/...
        ;;
    username)
        echo -e "${YELLOW}Running username integration tests...${NC}"
        go test -v -tags=integration ./tests/integration/ -run TestUserName
        ;;
    binary)
        echo -e "${YELLOW}Running binary mode tests...${NC}"
        go test -v -tags=integration ./tests/integration/ -run TestBinaryMode
        ;;
    prompt)
        echo -e "${YELLOW}Running prompt reload tests...${NC}"
        go test -v -tags=integration ./tests/integration/ -run TestPrompt
        ;;
    ratelimit)
        echo -e "${YELLOW}Running rate limiting tests...${NC}"
        go test -v -tags=integration ./tests/integration/ -run TestRateLimiting
        ;;
    *)
        echo -e "${RED}Unknown test type: $TEST_TYPE${NC}"
        echo ""
        echo "Usage: $0 [test-type]"
        echo ""
        echo "Test types:"
        echo "  all         - Run all integration tests (default)"
        echo "  short       - Run all tests, skip long ones"
        echo "  username    - Run username integration tests"
        echo "  binary      - Run binary mode tests"
        echo "  prompt      - Run prompt reload tests"
        echo "  ratelimit   - Run rate limiting tests"
        echo ""
        echo "Examples:"
        echo "  $0 all"
        echo "  $0 short"
        echo "  $0 username"
        echo ""
        echo "Skip service checks:"
        echo "  CHECK_SERVICES=false $0 all"
        exit 1
        ;;
esac

RESULT=$?

if [ $RESULT -eq 0 ]; then
    echo ""
    echo -e "${GREEN}✅ All tests passed!${NC}"
else
    echo ""
    echo -e "${RED}❌ Some tests failed${NC}"
    exit $RESULT
fi

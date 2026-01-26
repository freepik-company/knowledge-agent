#!/bin/bash
#
# Test script to verify graceful shutdown works properly
# Tests that the service shuts down within a reasonable time (10 seconds) after receiving SIGINT
#

set -e

echo "🧪 Testing graceful shutdown..."
echo ""

# Build the binary first
echo "📦 Building binary..."
go build -o bin/knowledge-agent cmd/knowledge-agent/main.go
echo "✅ Build complete"
echo ""

# Test function
test_shutdown() {
    local mode=$1
    echo "Testing shutdown in mode: $mode"

    # Start the service in background
    ./bin/knowledge-agent --mode "$mode" --config config.yaml &
    local pid=$!

    # Give it time to start
    sleep 3
    echo "  Service started (PID: $pid)"

    # Send SIGINT (Ctrl+C)
    echo "  Sending SIGINT..."
    kill -INT $pid

    # Wait for shutdown with timeout
    local timeout=10
    local elapsed=0
    while kill -0 $pid 2>/dev/null; do
        sleep 1
        elapsed=$((elapsed + 1))
        if [ $elapsed -ge $timeout ]; then
            echo "  ❌ FAILED: Service did not shut down within ${timeout}s"
            kill -9 $pid 2>/dev/null || true
            return 1
        fi
    done

    echo "  ✅ PASSED: Service shut down in ${elapsed}s"
    echo ""
    return 0
}

# Ensure services are available
if ! docker ps | grep -q postgres; then
    echo "⚠️  Warning: PostgreSQL not running. Start with 'make docker-up'"
    echo "   Continuing anyway (will test timeout behavior)..."
    echo ""
fi

# Test different modes
# Note: We only test agent mode since others may not have infrastructure running
echo "Testing agent-only mode:"
if test_shutdown "agent"; then
    echo "✅ All shutdown tests passed!"
    exit 0
else
    echo "❌ Shutdown test failed"
    exit 1
fi

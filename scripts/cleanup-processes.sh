#!/bin/bash

# Cleanup script for knowledge-agent processes
# Use this if processes are stuck after Ctrl+C

echo "🧹 Cleaning up knowledge-agent processes..."

# Kill all go run processes
pkill -f "go run.*knowledge-agent" 2>/dev/null && echo "  ✓ Killed go run processes" || echo "  • No go run processes found"

# Kill any knowledge-agent binaries
pkill -f "bin/knowledge-agent" 2>/dev/null && echo "  ✓ Killed binary processes" || echo "  • No binary processes found"

# Kill old cmd/agent and cmd/slack-bot processes (legacy)
pkill -f "cmd/agent.*go run" 2>/dev/null && echo "  ✓ Killed legacy agent processes" || true
pkill -f "cmd/slack-bot.*go run" 2>/dev/null && echo "  ✓ Killed legacy slack-bot processes" || true

# Show remaining processes
REMAINING=$(ps aux | grep -E "knowledge-agent|cmd/agent|cmd/slack-bot" | grep -v grep | wc -l | tr -d ' ')

if [ "$REMAINING" -eq "0" ]; then
    echo ""
    echo "✅ All knowledge-agent processes cleaned up"
else
    echo ""
    echo "⚠️  Some processes still running:"
    ps aux | grep -E "knowledge-agent|cmd/agent|cmd/slack-bot" | grep -v grep
    echo ""
    echo "Run with sudo if needed: sudo ./scripts/cleanup-processes.sh"
fi

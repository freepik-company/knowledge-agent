#!/bin/bash
#
# Generate authentication tokens for Knowledge Agent
#
# Usage:
#   ./scripts/generate-auth-token.sh internal
#   ./scripts/generate-auth-token.sh a2a monitoring
#

set -e

TYPE=${1:-internal}

case "$TYPE" in
  internal)
    echo "Generating internal authentication token..."
    echo ""
    TOKEN=$(openssl rand -hex 32)
    echo "INTERNAL_AUTH_TOKEN=$TOKEN"
    echo ""
    echo "Add this to both agent and slack-bot .env files:"
    echo "  INTERNAL_AUTH_TOKEN=$TOKEN"
    ;;

  a2a)
    SERVICE=${2:-service}
    echo "Generating A2A secret for client: $SERVICE"
    echo ""
    SECRET=$(openssl rand -hex 16)
    KEY="ka_${SERVICE}_${SECRET:0:8}"
    echo "Client ID: $SERVICE"
    echo "Secret:    $KEY"
    echo ""
    echo "Add this to agent a2a_api_keys configuration:"
    echo "  $SERVICE: $KEY"
    echo ""
    echo "Example config.yaml:"
    echo "  a2a_api_keys:"
    echo "    $SERVICE: $KEY"
    echo ""
    echo "External service should send:"
    echo "  X-API-Key: $KEY"
    ;;

  *)
    echo "Usage: $0 {internal|a2a} [service_name]"
    echo ""
    echo "Examples:"
    echo "  $0 internal              # Generate internal token"
    echo "  $0 a2a monitoring        # Generate A2A key for monitoring service"
    echo "  $0 a2a analytics         # Generate A2A key for analytics service"
    exit 1
    ;;
esac

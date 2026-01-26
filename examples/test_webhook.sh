#!/bin/bash
#
# Script de prueba para el endpoint /api/ingest-thread
#
# Uso:
#   ./test_webhook.sh                    # Usa ejemplo incluido
#   ./test_webhook.sh custom_thread.json # Usa archivo custom

set -e

# Colores para output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Configuración
AGENT_URL="${KNOWLEDGE_AGENT_URL:-http://localhost:8081}"
ENDPOINT="${AGENT_URL}/api/ingest-thread"

echo -e "${YELLOW}╔══════════════════════════════════════════════════════════╗${NC}"
echo -e "${YELLOW}║       Knowledge Agent - Webhook Test                     ║${NC}"
echo -e "${YELLOW}╚══════════════════════════════════════════════════════════╝${NC}"
echo ""

# Verificar que el agente esté corriendo
echo "🔍 Checking if Knowledge Agent is running..."
if ! curl -s "${AGENT_URL}/health" > /dev/null 2>&1; then
    echo -e "${RED}❌ ERROR: Knowledge Agent is not running at ${AGENT_URL}${NC}"
    echo "   Start it with: make dev"
    exit 1
fi

echo -e "${GREEN}✅ Knowledge Agent is running${NC}"
echo ""

# Determinar qué archivo JSON usar
if [ $# -eq 0 ]; then
    # Usar ejemplo incluido
    JSON_FILE="test_thread.json"
else
    # Usar archivo proporcionado
    JSON_FILE="$1"
fi

# Verificar que el archivo existe
if [ ! -f "$JSON_FILE" ]; then
    echo -e "${RED}❌ ERROR: File not found: ${JSON_FILE}${NC}"
    exit 1
fi

echo "📄 Using thread data from: ${JSON_FILE}"
echo ""

# Mostrar un preview del thread
THREAD_TS=$(jq -r '.thread_ts' "$JSON_FILE")
CHANNEL_ID=$(jq -r '.channel_id' "$JSON_FILE")
MSG_COUNT=$(jq '.messages | length' "$JSON_FILE")

echo "Thread Information:"
echo "  • Thread ID: ${THREAD_TS}"
echo "  • Channel: ${CHANNEL_ID}"
echo "  • Messages: ${MSG_COUNT}"
echo ""

# Enviar request
echo "🚀 Sending thread to Knowledge Agent..."
echo "   Endpoint: ${ENDPOINT}"
echo ""

RESPONSE=$(curl -s -w "\nHTTP_STATUS:%{http_code}" \
    -X POST "${ENDPOINT}" \
    -H "Content-Type: application/json" \
    -d @"${JSON_FILE}")

HTTP_STATUS=$(echo "$RESPONSE" | grep "HTTP_STATUS" | cut -d: -f2)
BODY=$(echo "$RESPONSE" | sed '/HTTP_STATUS/d')

echo "Response:"
echo "$BODY" | jq '.'
echo ""

# Verificar respuesta
if [ "$HTTP_STATUS" -eq 200 ]; then
    SUCCESS=$(echo "$BODY" | jq -r '.success')
    MEMORIES=$(echo "$BODY" | jq -r '.memories_added')
    MESSAGE=$(echo "$BODY" | jq -r '.message')

    if [ "$SUCCESS" = "true" ]; then
        echo -e "${GREEN}✅ SUCCESS!${NC}"
        echo "   Memories added: ${MEMORIES}"
        echo ""
        echo "Agent Response:"
        echo "${MESSAGE}" | fold -w 70 -s
        echo ""

        # Sugerencia de verificación
        echo -e "${YELLOW}💡 Verify in database:${NC}"
        echo "   make db-shell"
        echo "   SELECT COUNT(*) FROM memories WHERE app_name='knowledge-agent';"
    else
        echo -e "${RED}❌ FAILED${NC}"
        echo "   Message: ${MESSAGE}"
        exit 1
    fi
else
    echo -e "${RED}❌ HTTP ERROR: ${HTTP_STATUS}${NC}"
    echo "$BODY"
    exit 1
fi

echo ""
echo -e "${GREEN}╔══════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║                 Test Completed Successfully               ║${NC}"
echo -e "${GREEN}╚══════════════════════════════════════════════════════════╝${NC}"

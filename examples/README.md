# Ejemplos de Integración

Esta carpeta contiene ejemplos de cómo integrar diferentes sistemas con el Knowledge Agent.

## Slack Integration (Python)

**Archivo**: `slack_webhook_example.py`

Ejemplo completo de cómo integrar Slack con el Knowledge Agent:

### Características

- ✅ Obtiene threads completos de Slack
- ✅ Envía al Knowledge Agent vía webhook
- ✅ Incluye servidor Flask para recibir eventos de Slack
- ✅ Soporta shortcuts de Slack
- ✅ Postea confirmación en el thread

### Requisitos

```bash
pip install slack-sdk flask requests
```

### Configuración

```bash
export SLACK_BOT_TOKEN="xoxb-your-token"
export SLACK_SIGNING_SECRET="your-secret"
export KNOWLEDGE_AGENT_URL="http://localhost:8081"
```

### Uso como Script

Ingestar un thread específico:

```bash
python slack_webhook_example.py C01234567 1234567890.123456
```

Donde:
- `C01234567` = channel_id
- `1234567890.123456` = thread_ts

### Uso como Servidor

Recibir webhooks de Slack:

```bash
python slack_webhook_example.py
```

Esto inicia un servidor Flask en `http://localhost:3000`

Configurar en Slack:
1. Ir a api.slack.com/apps → Tu App
2. Event Subscriptions → Request URL: `https://tu-dominio.com/slack/events`
3. Subscribe to bot events: `app_mention`

### Flujo de Trabajo

```
Usuario en Slack: @bot ingest
        ↓
Flask recibe webhook de Slack
        ↓
Obtiene thread completo con Slack API
        ↓
POST a Knowledge Agent /api/ingest-thread
        ↓
Agente procesa y guarda en PostgreSQL
        ↓
Postea confirmación en Slack
```

## Otros Ejemplos (Próximamente)

### JavaScript/Node.js

Script para integrar desde Node.js con Slack Bolt.

### Zapier/Make Integration

Configuración para integrar con Zapier o Make.com sin código.

### Discord Integration

Ejemplo de integración con Discord.

### Microsoft Teams

Ejemplo de integración con Teams.

## Testing

### Test Webhook Directo

Sin necesidad de Slack, puedes probar el endpoint directamente:

```bash
curl -X POST http://localhost:8081/api/ingest-thread \
  -H "Content-Type: application/json" \
  -d '{
    "thread_ts": "test-123",
    "channel_id": "test-channel",
    "messages": [
      {
        "user": "U123",
        "text": "This is a test message",
        "ts": "1234567890.123456"
      },
      {
        "user": "U456",
        "text": "This is a reply",
        "ts": "1234567891.123456"
      }
    ]
  }'
```

## Soporte

Para más información, ver:
- `WEBHOOK_API.md` - Documentación completa del API
- `README.md` - Documentación principal del proyecto

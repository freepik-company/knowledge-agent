# Operations Guide

Guía operacional completa para Knowledge Agent: logging, traceability y observabilidad.

## Logging

El Knowledge Agent utiliza **Uber Zap** para logging estructurado con alto rendimiento.

### Niveles de Log

#### debug
Logging muy detallado, incluye:
- Eventos del runner ADK
- Llamadas a herramientas (tool calls)
- Respuestas de herramientas
- Contenido de mensajes (truncado)
- Conteo de eventos
- Detalles de procesamiento

**Cuándo usar**: Debugging de problemas, desarrollo de features, investigación de comportamiento del agente.

#### info (default)
Logging operacional estándar:
- Peticiones recibidas
- Operaciones exitosas
- Inicio/parada de servicios
- Configuración cargada
- Tool calls importantes (save_to_memory, search_memory)

**Cuándo usar**: Producción, operaciones normales.

#### warn
Situaciones anormales pero recuperables:
- Errores recuperables
- Fallbacks
- Conexiones fallidas que se reintentarán

**Cuándo usar**: Producción con alerting.

#### error
Errores no recuperables:
- Fallos en peticiones
- Errores de agente
- Problemas de infraestructura

**Cuándo usar**: Siempre en producción.

### Formatos de Output

#### console (human-readable)
```
2026-01-24T15:30:45.123+0100    INFO    agent/agent.go:386    Running agent for query
2026-01-24T15:30:47.456+0100    INFO    agent/agent.go:420    Agent calling tool    {"tool": "search_memory", "args_count": 2}
```

**Cuándo usar**: Desarrollo local, debugging.

#### json (structured)
```json
{"level":"info","ts":"2026-01-24T15:30:45.123+0100","caller":"agent/agent.go:386","msg":"Running agent for query"}
{"level":"info","ts":"2026-01-24T15:30:47.456+0100","caller":"agent/agent.go:420","msg":"Agent calling tool","tool":"search_memory","args_count":2}
```

**Cuándo usar**: Producción, integración con sistemas de logging (ELK, Splunk, Datadog).

### Configuración

#### Variables de Entorno (.env)

```bash
# Desarrollo con debugging
LOG_LEVEL=debug
LOG_FORMAT=console
LOG_OUTPUT=stdout

# Producción
LOG_LEVEL=info
LOG_FORMAT=json
LOG_OUTPUT=/var/log/knowledge-agent.log
```

#### Config File (config.yaml)

```yaml
log:
  level: debug
  format: console
  output_path: stdout
```

#### Runtime Override

```bash
# Override temporalmente
LOG_LEVEL=debug make dev

# O con binarios directamente
LOG_LEVEL=debug ./bin/knowledge-agent
```

### Output Destinations

#### stdout (default)
Logs a consola estándar.

```yaml
log:
  output_path: stdout
```

#### stderr
Logs a error estándar (útil para separar logs de output normal).

```yaml
log:
  output_path: stderr
```

#### File
Logs a archivo (importante: rotación de logs NO incluida, usar logrotate o similar).

```yaml
log:
  output_path: /var/log/knowledge-agent.log
```

### Logs Importantes

#### Durante Query (LOG_LEVEL=debug)

```
INFO    Running agent for query
DEBUG   Runner event received    {"event_number": 1, ...}
INFO    Agent calling tool       {"tool": "search_memory"}
DEBUG   Tool response received   {"tool": "search_memory"}
DEBUG   Text part               {"length": 1234, "preview": "..."}
INFO    Query completed         {"total_events": 5, "response_length": 1234}
```

#### Durante Ingestion (LOG_LEVEL=debug)

```
INFO    Running agent for thread ingestion
INFO    Agent calling tool during ingestion    {"tool": "save_to_memory"}
DEBUG   Memory save detected    {"total_saves": 1}
INFO    Thread ingestion completed    {"memories_saved": 3, "total_events": 8}
```

### Best Practices

1. **Development**: `LOG_LEVEL=debug LOG_FORMAT=console`
2. **Staging**: `LOG_LEVEL=info LOG_FORMAT=json`
3. **Production**: `LOG_LEVEL=info LOG_FORMAT=json LOG_OUTPUT=/var/log/...`
4. **Debugging Issues**: Temporalmente aumentar a `debug` y hacer grep del problema
5. **Log Rotation**: Usar logrotate para archivos de log
6. **Monitoring**: Integrar logs JSON con tu sistema de observabilidad

---

## Traceability

El Knowledge Agent implementa traceability comprehensiva para rastrear quién hace requests y desde dónde.

### Niveles de Traceability

#### 1. Caller ID (Authentication Source)

**Purpose**: Identifica el servicio/fuente haciendo el request al agente.

**Values**:
- `slack-bridge` - Requests desde Slack Bridge (autenticado con internal token)
- `root-agent` - Requests directos desde root orchestration agent (A2A)
- `monitoring` - Requests directos desde servicio de monitoring (A2A)
- `external-service` - Requests directos desde otros servicios externos (A2A)
- `slack-direct` - Legacy: Webhooks directos desde Slack (autenticado con Slack signature)
- `unauthenticated` - Requests cuando autenticación está deshabilitada (dev mode)

**How it works**:
- Set por `AuthMiddleware` basado en método de autenticación usado
- Almacenado en request context via `ctxutil.CallerIDKey`
- Obtenido usando `ctxutil.CallerID(ctx)`

**Logged in**:
- Agent query processing
- Agent thread ingestion
- Server request handling

#### 2. Slack User ID (End User)

**Purpose**: Identifica el usuario real de Slack que inició el request (cuando viene a través de Slack Bridge).

**Format**: Slack User ID (e.g., `U123ABC456`)

**How it works**:
1. Slack Bridge recibe evento de Slack con `event.User`
2. Bridge agrega header `X-Slack-User-Id` al request al Agent
3. `AuthMiddleware` captura header y almacena en context via `ctxutil.SlackUserIDKey`
4. Obtenido usando `ctxutil.SlackUserID(ctx)`

**Logged in**:
- Slack Bridge event reception
- Agent query processing (si presente)
- Agent thread ingestion (si presente)

**Note**: Solo presente para requests viniendo a través de Slack Bridge. Vacío para requests A2A directos.

### Log Examples

#### Slack User Request

```
INFO  slack/handler.go  Slack event received
      user=U123ABC456
      thread_ts=1234567890.123
      channel=C123XYZ
      message="¿Cómo desplegamos?"

INFO  agent/agent.go  Processing query
      caller_id=slack-bridge
      slack_user_id=U123ABC456
      question="¿Cómo desplegamos?"
      channel_id=C123XYZ

INFO  agent/agent.go  Query completed successfully
      caller_id=slack-bridge
      slack_user_id=U123ABC456
      total_events=5
      response_length=234
```

#### Direct A2A Request

```
INFO  agent/agent.go  Processing query
      caller_id=root-agent
      question="What's our deployment process?"
      channel_id=api-channel

INFO  agent/agent.go  Query completed successfully
      caller_id=root-agent
      total_events=3
      response_length=456
```

#### Unauthenticated Request (Development)

```
INFO  agent/agent.go  Processing query
      caller_id=unauthenticated
      question="test query"
      channel_id=test

INFO  agent/agent.go  Query completed successfully
      caller_id=unauthenticated
      total_events=2
      response_length=123
```

### Implementation Details

#### Context Utilities Package

**File**: `internal/ctxutil/context.go`

Proporciona definiciones de context keys compartidas y funciones accessor:

```go
// Context keys
const (
    CallerIDKey    contextKey = "caller_id"
    SlackUserIDKey contextKey = "slack_user_id"
)

// Accessor functions
func CallerID(ctx context.Context) string
func SlackUserID(ctx context.Context) string
```

Este package previene import cycles proporcionando una ubicación compartida para context utilities.

#### Data Flow

```
Slack User (U123ABC456)
  ↓
Slack API
  ↓
Slack Bridge (handler.go)
  - Recibe event.User
  - Logs: user=U123ABC456
  - HTTP Request al Agent:
    - X-Internal-Token: <internal_token>
    - X-Slack-User-Id: U123ABC456
  ↓
Agent (middleware.go)
  - Valida X-Internal-Token
  - Captura X-Slack-User-Id
  - Sets context:
    - caller_id=slack-bridge
    - slack_user_id=U123ABC456
  ↓
Agent (agent.go)
  - Extrae de context usando ctxutil
  - Logs ambos valores
  - Procesa query
```

### Best Practices

#### 1. Always Check Both IDs

Cuando logueas operaciones del agente, siempre incluir caller_id y slack_user_id (si presente):

```go
logFields := []interface{}{
    "caller_id", ctxutil.CallerID(ctx),
    "operation", "my_operation",
}
if slackUserID := ctxutil.SlackUserID(ctx); slackUserID != "" {
    logFields = append(logFields, "slack_user_id", slackUserID)
}
log.Infow("Operation started", logFields...)
```

#### 2. Use Structured Logging

Siempre usar campos estructurados, no concatenación de strings:

✅ **Good**:
```go
log.Infow("Query received",
    "caller_id", callerID,
    "slack_user_id", slackUserID,
    "question", question,
)
```

❌ **Bad**:
```go
log.Infof("Query from %s (user %s): %s", callerID, slackUserID, question)
```

#### 3. Consistent Field Names

Siempre usar estos nombres de field exactos:
- `caller_id` (no `caller`, `source`, `client_id`)
- `slack_user_id` (no `user_id`, `slack_id`, `user`)

#### 4. Extract Once, Use Many

Extraer context values una vez al inicio de la función:

```go
func (a *Agent) Query(ctx context.Context, req QueryRequest) (*QueryResponse, error) {
    // Extract once
    callerID := ctxutil.CallerID(ctx)
    slackUserID := ctxutil.SlackUserID(ctx)

    // Use throughout function
    log.Infow("Starting query", "caller_id", callerID, "slack_user_id", slackUserID)
    // ... processing ...
    log.Infow("Completed query", "caller_id", callerID, "slack_user_id", slackUserID)
}
```

---

## Integración con Sistemas de Logging

### ELK Stack

```yaml
# filebeat.yml
filebeat.inputs:
- type: log
  paths:
    - /var/log/knowledge-agent/*.log
  json.keys_under_root: true
  json.add_error_key: true
```

### Datadog

```yaml
# datadog.yaml
logs:
  - type: file
    path: /var/log/knowledge-agent/*.log
    service: knowledge-agent
    source: go
```

### CloudWatch

Usar AWS CloudWatch agent con JSON parsing.

---

## Troubleshooting

### No veo logs de DEBUG

Verificar que `LOG_LEVEL=debug` esté configurado:

```bash
# Check env var
echo $LOG_LEVEL

# Forzar debug
LOG_LEVEL=debug ./bin/knowledge-agent
```

### Logs no aparecen en archivo

Verificar permisos:

```bash
# Check if file is writable
touch /var/log/knowledge-agent.log
chmod 644 /var/log/knowledge-agent.log
```

### Demasiados logs en producción

Usar level más restrictivo:

```bash
LOG_LEVEL=warn  # Solo warnings y errors
LOG_LEVEL=error # Solo errors
```

### Missing Slack User ID

**Symptom**: Logs muestran `caller_id=slack-bridge` pero no `slack_user_id`

**Possible Causes**:
1. Slack Bridge no enviando header `X-Slack-User-Id`
2. Slack event sin field `User`
3. Middleware no capturando header

**Debug**:
```bash
# Check Slack Bridge logs
grep "Slack event received" logs | grep -v "user="
```

### Wrong Caller ID

**Symptom**: Esperabas `slack-bridge` pero ves `root-agent`

**Possible Causes**:
1. Request usando método de autenticación incorrecto (X-API-Key en vez de X-Internal-Token)
2. Múltiples authentication headers presentes
3. Configuración incorrecta

**Debug**:
```bash
# Check authentication logs
grep "Invalid.*attempt" logs

# Verify configuration
grep -A 5 "auth:" config.yaml
```

---

## Ver También

- [SECURITY.md](SECURITY.md) - Autenticación y autorización
- [CONFIGURATION.md](CONFIGURATION.md) - Configuración del sistema
- [TESTING.md](TESTING.md) - Testing y QA
- [../CLAUDE.md](../CLAUDE.md) - Arquitectura del sistema

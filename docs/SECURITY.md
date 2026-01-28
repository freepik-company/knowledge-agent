# Security Guide

## Authentication Model

The Knowledge Agent implements a two-tier authentication model to secure different types of access:

### 1. Internal Authentication (Slack Bridge ↔ Agent)

**Purpose**: Secure the communication between the Slack Bridge service and the Knowledge Agent service.

**Method**: Shared secret token via `X-Internal-Token` header

**Configuration**:
```yaml
# config.yaml
auth:
  internal_token: ${INTERNAL_AUTH_TOKEN}
```

**Environment Variable**:
```bash
# Generate a secure token
INTERNAL_AUTH_TOKEN=$(openssl rand -hex 32)
```

**How it works**:
1. Both `slack-bot` and `agent` services share the same `INTERNAL_AUTH_TOKEN`
2. Slack Bridge sends `X-Internal-Token` header with every request to Agent
3. Agent validates the token before processing requests
4. If token matches → request is authenticated as `caller_id: slack-bridge`
5. If token doesn't match → 401 Unauthorized

**Security characteristics**:
- Token is never exposed in API documentation or external configurations
- Only known by the two internal services
- Can be rotated without affecting external A2A clients
- Not stored in `a2a_api_keys` (which might be visible to external services)

### 2. External A2A Authentication (External Services → Agent)

**Purpose**: Secure direct API access from external agents or services.

**Method**: API keys via `X-API-Key` header

**Configuration**:
```yaml
# config.yaml
a2a_api_keys:
  root-agent: ka_secret_abc123
  external-service: ka_secret_def456
  monitoring: ka_secret_ghi789
```

**Environment Variable**:
```bash
# JSON format - Maps client_id to secret
A2A_API_KEYS='{"root-agent":"ka_secret_abc123","external-service":"ka_secret_def456"}'
```

**How it works**:
1. External services send `X-API-Key` header with their secret token
2. Agent searches for which `client_id` has that secret in `a2a_api_keys` map
3. If secret matches → request is authenticated with that `client_id`
4. If secret not found → 401 Unauthorized

**Security characteristics**:
- Each external service gets its own client_id and secret token
- Client ID is used for logging and tracing (secrets are never logged)
- Secrets can be individually rotated by updating the value for that client_id
- Client ID appears in logs, making audit trails clear without exposing secrets

### 3. Slack Signature Authentication (Legacy)

**Purpose**: Verify requests come directly from Slack (when not using Slack Bridge).

**Method**: HMAC signature via `X-Slack-Signature` header

**Configuration**:
```yaml
slack:
  signing_secret: ${SLACK_SIGNING_SECRET}
```

**Note**: This is legacy support. In the standard architecture, Slack events go through the Slack Bridge, which uses internal token authentication.

## Authentication Flow

```
┌─────────────────────────────────────────────────────────────┐
│ Request arrives at Agent                                     │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
         ┌───────────────────────────────┐
         │ Has X-Internal-Token header?  │
         └───────────┬───────────────────┘
                     │
        ┌────────────┴────────────┐
        │ YES                     │ NO
        ▼                         ▼
┌──────────────────┐    ┌─────────────────────┐
│ Token matches    │    │ Has X-API-Key       │
│ internal_token?  │    │ header?             │
└────┬─────────┬───┘    └──────┬──────────────┘
     │         │               │
   YES        NO          ┌────┴────┐
     │         │          │ YES     │ NO
     │         │          ▼         ▼
     │         │   ┌─────────┐   ┌──────────────────┐
     │         │   │ Key in  │   │ Has Slack        │
     │         │   │ APIKeys?│   │ Signature?       │
     │         │   └────┬────┘   └─────┬────────────┘
     │         │        │              │
     │         │    ┌───┴───┐      ┌───┴───┐
     │         │    │ YES   │ NO   │ YES   │ NO
     │         │    ▼       ▼      ▼       ▼
     ▼         ▼    ▼       ▼      ▼       ▼
┌────────┐ ┌─────────────────────┐ ┌──────────────┐
│ ALLOW  │ │    401 Unauthorized  │ │ Check if     │
│ as     │ │                      │ │ auth enabled │
│ slack- │ │                      │ └──────┬───────┘
│ bridge │ │                      │        │
└────────┘ └──────────────────────┘   ┌────┴────┐
                                       │ Enabled │ Disabled
                                       ▼         ▼
                                   ┌──────┐  ┌─────────────┐
                                   │ 401  │  │ ALLOW as    │
                                   │      │  │ unauthenticated │
                                   └──────┘  └─────────────┘
```

## Security Modes

### Production Mode (Recommended)

**Configuration**:
```yaml
auth:
  internal_token: ${INTERNAL_AUTH_TOKEN}  # Required

a2a_api_keys:
  root-agent: ${A2A_SECRET_ROOT}
  monitoring: ${A2A_SECRET_MONITORING}
  # Add other external services as needed
```

**Behavior**:
- Slack Bridge → Agent: Requires internal token
- External services → Agent: Requires secret token from a2a_api_keys
- Unauthenticated requests → 401 Unauthorized

**Use for**: Production deployments, staging environments

### Development Mode (Open Access)

**Configuration**:
```yaml
auth:
  internal_token: ""  # Empty or omitted

a2a_api_keys: {}  # Empty or omitted
```

**Behavior**:
- All requests are allowed without authentication
- Caller ID is set to "unauthenticated"

**Use for**: Local development, testing

**Warning**: Never use this mode in production or with sensitive data.

### Hybrid Mode (Internal Only)

**Configuration**:
```yaml
auth:
  internal_token: ${INTERNAL_AUTH_TOKEN}  # Set

a2a_api_keys: {}  # Empty
```

**Behavior**:
- Slack Bridge → Agent: Requires internal token
- External services → Agent: Blocked (401)

**Use for**: Deployments where only Slack access is needed

## Token Generation

### Internal Token

Generate a cryptographically secure random token:

```bash
# 32-byte hex token (64 characters)
openssl rand -hex 32
```

Example output:
```
f8e9d7c6b5a4938271605f4e3d2c1b0a9f8e7d6c5b4a3928170615e4d3c2b1a0
```

Set in environment:
```bash
export INTERNAL_AUTH_TOKEN=f8e9d7c6b5a4938271605f4e3d2c1b0a9f8e7d6c5b4a3928170615e4d3c2b1a0
```

### A2A API Keys

**Client ID naming convention:**
Use descriptive, kebab-case identifiers:

```
<service-name>
```

Examples:
- `root-agent` - Root orchestration agent
- `monitoring` - Monitoring system
- `analytics` - Analytics service
- `backup-automation` - Backup automation

**Secret token format:**
Use the generation script or follow this pattern:

```bash
# Generate secret for a client
./scripts/generate-auth-token.sh a2a monitoring

# Or manually:
echo "ka_monitoring_$(openssl rand -hex 8)"
```

## Best Practices

### Token Management

1. **Rotation**: Rotate internal token periodically (e.g., every 90 days)
2. **Storage**: Store tokens in secure secret management (Vault, AWS Secrets Manager, etc.)
3. **Environment**: Never commit tokens to version control
4. **References**: Use `${ENV_VAR}` references in config.yaml, not hardcoded values

### Access Control

1. **Principle of Least Privilege**: Only grant A2A keys to services that need them
2. **Caller ID Tracking**: Use meaningful caller IDs for audit trails
3. **Key Revocation**: Remove unused keys immediately from configuration

### Monitoring

1. **Failed Authentication Attempts**: Monitor logs for `"Invalid API key attempt"` and `"Invalid internal token attempt"`
2. **Caller ID Usage**: Track which services are accessing the agent
3. **Alert on Anomalies**: Set up alerts for unusual authentication patterns

### Deployment

1. **Separate Secrets**: Use different tokens/keys for dev, staging, and production
2. **Secret Management**: Use your platform's secret management (Kubernetes Secrets, Docker Secrets, etc.)
3. **Least Exposure**: Only expose tokens to services that need them

## Example Secure Deployment

### Docker Compose

```yaml
version: '3.8'

services:
  agent:
    image: knowledge-agent:latest
    environment:
      INTERNAL_AUTH_TOKEN: ${INTERNAL_AUTH_TOKEN}
      A2A_API_KEYS: '${A2A_API_KEYS}'
    secrets:
      - anthropic_api_key
      - postgres_url

  slack-bot:
    image: knowledge-agent-slack:latest
    environment:
      INTERNAL_AUTH_TOKEN: ${INTERNAL_AUTH_TOKEN}
    secrets:
      - slack_bot_token
      - slack_app_token

secrets:
  anthropic_api_key:
    external: true
  postgres_url:
    external: true
  slack_bot_token:
    external: true
  slack_app_token:
    external: true
```

### Kubernetes

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: knowledge-agent-secrets
type: Opaque
data:
  internal-auth-token: <base64-encoded-token>
  a2a-api-keys: <base64-encoded-json>
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: knowledge-agent
spec:
  template:
    spec:
      containers:
      - name: agent
        env:
        - name: INTERNAL_AUTH_TOKEN
          valueFrom:
            secretKeyRef:
              name: knowledge-agent-secrets
              key: internal-auth-token
        - name: A2A_API_KEYS
          valueFrom:
            secretKeyRef:
              name: knowledge-agent-secrets
              key: a2a-api-keys
```

## Security Checklist

- [ ] Generated strong random token for `INTERNAL_AUTH_TOKEN`
- [ ] Configured `internal_token` in both agent and slack-bot
- [ ] Added A2A API keys for external services
- [ ] Used `${ENV_VAR}` references in config.yaml (not hardcoded values)
- [ ] Verified `config.yaml` is in `.gitignore`
- [ ] No tokens committed to version control
- [ ] Tokens stored in secure secret management
- [ ] Different tokens for dev/staging/prod
- [ ] Monitoring logs for authentication failures
- [ ] Documented which services have which API keys
- [ ] Tested authentication works (401 on invalid credentials)
- [ ] Tested authentication bypass fails (no open access in production)

## Troubleshooting

### "Invalid internal token attempt"

**Cause**: Slack Bridge and Agent have different `INTERNAL_AUTH_TOKEN` values

**Solution**:
1. Verify both services use the same environment variable
2. Check for typos in token value
3. Restart both services after changing token

### "Authentication required but not provided"

**Cause**: Request has no authentication headers, but authentication is enabled

**Solution**:
1. For Slack Bridge: Ensure `INTERNAL_AUTH_TOKEN` is set
2. For A2A access: Include `X-API-Key` header in request
3. For development: Set both `internal_token` and `a2a_api_keys` to empty to enable open mode

### "Invalid API key"

**Cause**: API key not found in `a2a_api_keys` map

**Solution**:
1. Verify the key exists in configuration
2. Check for typos in API key
3. Ensure configuration was reloaded after adding key

---

## Permissions System (save_to_memory Control)

The Knowledge Agent implements **fine-grained permissions** to control who can save information to the knowledge base.

### Configuration

```yaml
permissions:
  # Lista de Slack User IDs permitidos para guardar
  allowed_slack_users:
    - U02ABC123  # john.doe
    - U03DEF456  # jane.smith

  # Lista de caller IDs con permisos de administrador (siempre pueden guardar)
  admin_caller_ids:
    - root-agent      # Servicio A2A raíz
    - monitoring      # Servicio de monitoreo
```

### Permission Modes

#### Restrictive Mode (Default - Empty Lists Deny All)

When permission lists are empty or not configured, **no one** can save:

```yaml
permissions:
  allowed_slack_users: []
  admin_caller_ids: []
```

**Result**: All save attempts are blocked. Secure by default.

#### Controlled Access Mode

When you configure specific users/services, **only they** can save:

```yaml
permissions:
  allowed_slack_users:
    - U02ABC123  # Only this Slack user
  admin_caller_ids:
    - root-agent  # Only this A2A service
```

**Result**:
- ✅ User U02ABC123 → Can save
- ✅ Service root-agent → Can save
- ❌ Other Slack users → **Cannot** save
- ❌ Other A2A services → **Cannot** save

### How It Works

1. **Authentication Layer**: Request authenticated → caller_id + slack_user_id set in context
2. **Permission Check**: When `save_to_memory` tool is called:
   - Check if caller_id is in `admin_caller_ids` → Allow
   - Check if slack_user_id is in `allowed_slack_users` → Allow
   - Otherwise → **Block with error**
3. **Error to Agent**: Permission error returned to LLM agent, which informs user

### Implementation

Permissions are enforced at the **tool level** using a wrapper pattern:

```go
// internal/agent/permission_memory_service.go
type PermissionMemoryService struct {
    baseService       memory.Service
    permissionChecker *permissions.MemoryPermissionChecker
    contextHolder     *contextHolder
}

func (s *PermissionMemoryService) AddSession(ctx context.Context, sess session.Session) error {
    // Get request context with caller_id and slack_user_id
    requestCtx := s.contextHolder.GetContext()

    // Check permissions FIRST
    canSave, reason := s.permissionChecker.CanSaveToMemory(requestCtx)

    if !canSave {
        return fmt.Errorf("⛔ Permisos insuficientes. Razón: %s", reason)
    }

    // Proceed with save
    return s.baseService.AddSession(ctx, sess)
}
```

### Use Cases

#### Case 1: Only Admin Users

```yaml
permissions:
  allowed_slack_users:
    - U02JOHN123  # John (admin)
    - U03JANE456  # Jane (admin)
  admin_caller_ids:
    - root-agent  # A2A services always allowed
```

**Behavior**:
- John and Jane can save from Slack
- Other users can only search (search_memory)
- A2A services (root-agent) can always save

#### Case 2: Only A2A Services

```yaml
permissions:
  allowed_slack_users: []  # No Slack users
  admin_caller_ids:
    - root-agent
    - monitoring
    - backup-service
```

**Behavior**:
- **No** Slack user can save
- Only authorized A2A services can save
- Slack users can only search

#### Case 3: Specific Team

```yaml
permissions:
  allowed_slack_users:
    - U02JOHN123   # Tech Lead
    - U03JANE456   # Senior Dev
    - U04BOB789    # DevOps
  admin_caller_ids:
    - root-agent
```

**Behavior**:
- Only technical team can save knowledge
- Rest of company can search and query

### Logging & Auditing

Every request logs permission status:

```log
INFO  agent/permission_memory_service.go  save_to_memory permission granted
      caller_id=slack-bridge
      slack_user_id=U02JOHN123
      can_save=true
      permission_reason="slack_user_id 'U02JOHN123' is allowed"
      session_id=query-C123XYZ-1234567890
```

**Blocked save:**
```log
WARN  agent/permission_memory_service.go  save_to_memory BLOCKED: insufficient permissions
      caller_id=slack-bridge
      slack_user_id=U05GUEST999
      can_save=false
      permission_reason="slack_user_id 'U05GUEST999' is not authorized to save to memory"
      session_id=query-C123XYZ-1234567891
```

### Agent Behavior

#### Authorized User

```
User: @bot save that we deploy on Tuesdays

Bot: ✅ I've saved the information about deployments.
```

#### Unauthorized User

```
User: @bot save that we deploy on Tuesdays

Bot: ⛔ Permisos insuficientes. Solo los usuarios autorizados
     pueden guardar información en la base de conocimiento.
     Razón: slack_user_id 'U05GUEST999' is not authorized to save to memory
```

The agent receives the error from the tool and communicates it to the user.

### Getting Slack User IDs

**Method 1: Hover over user in Slack**
1. Open Slack
2. Hover over user's name
3. Click "View full profile"
4. Click the ⋮ menu → "Copy member ID"

**Method 2: From logs**
Check logs when a user makes a request:
```bash
grep "slack_user_id" logs | grep "U0"
```

**Method 3: Slack API**
```bash
curl -H "Authorization: Bearer $SLACK_BOT_TOKEN" \
  https://slack.com/api/users.list | jq '.members[] | {id, name, real_name}'
```

### Security Notes

- **Restrictive by default**: Empty lists deny all saves
- **Admin override**: admin_caller_ids always bypass user checks
- **Tool-level enforcement**: Cannot be bypassed by prompt engineering
- **Audit trail**: All permission checks logged with context
- **A2A compatibility**: Works with both Slack and direct API access

---

## SSRF Protection (fetch_url Tool)

The Knowledge Agent implements comprehensive **Server-Side Request Forgery (SSRF) protection** for the `fetch_url` tool to prevent attackers from scanning or accessing internal infrastructure.

### What is SSRF?

SSRF vulnerabilities occur when an application fetches remote resources based on user input without proper validation. Attackers can exploit this to:
- Scan internal network services (databases, caches, APIs)
- Access cloud metadata services (AWS, GCP, Azure)
- Enumerate Kubernetes cluster services
- Bypass firewall restrictions

### Protection Mechanisms

The `fetch_url` tool in `internal/tools/webfetch.go` implements multiple layers of protection:

#### 1. URL Scheme Validation

Only `http://` and `https://` schemes are allowed:

```
✅ Allowed:
  - https://example.com
  - http://api.example.com:8080

❌ Blocked:
  - file:///etc/passwd
  - ftp://internal.server/files
  - gopher://localhost:70/
  - dict://localhost:2628/
```

#### 2. Hostname Blocklist

Specific dangerous hostnames are explicitly blocked:

```
❌ Blocked Hostnames:
  - localhost
  - 127.0.0.1
  - 0.0.0.0
  - [::1] (IPv6 localhost)
  - metadata.google.internal (GCP metadata)
  - 169.254.169.254 (AWS/Azure metadata)
```

#### 3. Kubernetes Internal Service DNS Blocking

All Kubernetes internal service DNS names are blocked:

```
❌ Blocked Patterns:
  - *.svc.cluster.local
  - kubernetes.default.svc.cluster.local
  - redis.production.svc.cluster.local
  - postgres.staging.svc.cluster.local
```

#### 4. Private IP Range Filtering

After hostname DNS resolution, all private/internal IP ranges are blocked:

**IPv4 Private Ranges:**
```
❌ Blocked IP Ranges:
  - 10.0.0.0/8        (Private network)
  - 172.16.0.0/12     (Private network)
  - 192.168.0.0/16    (Private network)
  - 127.0.0.0/8       (Loopback)
  - 169.254.0.0/16    (Link-local / Cloud metadata)
  - 0.0.0.0/8         (Current network)
  - 100.64.0.0/10     (Carrier-grade NAT)
  - 192.0.0.0/24      (IETF Protocol Assignments)
  - 192.0.2.0/24      (TEST-NET-1)
  - 198.18.0.0/15     (Benchmarking)
  - 198.51.100.0/24   (TEST-NET-2)
  - 203.0.113.0/24    (TEST-NET-3)
  - 224.0.0.0/4       (Multicast)
  - 240.0.0.0/4       (Reserved)
```

**IPv6 Private Ranges:**
```
❌ Blocked IP Ranges:
  - ::1/128           (Loopback)
  - fe80::/10         (Link-local)
  - fc00::/7          (Unique Local Addresses)
```

### Implementation Details

**Validation function** (`internal/tools/webfetch.go:77-142`):

```go
func validateURL(rawURL string) error {
    // 1. Parse URL
    parsedURL, err := url.Parse(rawURL)

    // 2. Validate scheme (http/https only)
    if scheme != "http" && scheme != "https" {
        return error
    }

    // 3. Block localhost and metadata services
    if hostname == "localhost" || hostname == "169.254.169.254" {
        return error
    }

    // 4. Block Kubernetes internal DNS
    if strings.HasSuffix(hostname, ".svc.cluster.local") {
        return error
    }

    // 5. Resolve DNS and check IP ranges
    ips, err := net.LookupIP(hostname)
    for _, ip := range ips {
        if isPrivateIP(ip) {
            return error
        }
    }

    return nil
}
```

**Integration** (`internal/tools/webfetch.go:189-194`):

```go
func (ts *WebFetchToolset) fetchURL(ctx tool.Context, args FetchURLArgs) (FetchURLResult, error) {
    // SSRF Protection: Validate URL before fetching
    if err := validateURL(args.URL); err != nil {
        return FetchURLResult{
            Success: false,
            Error:   fmt.Sprintf("URL validation failed: %v", err),
        }, nil
    }

    // Proceed with HTTP request...
}
```

### Testing

Comprehensive test suite in `internal/tools/webfetch_test.go`:

```bash
# Run SSRF protection tests
go test -v ./internal/tools/... -run TestValidateURL

# Test private IP detection
go test -v ./internal/tools/... -run TestIsPrivateIP
```

**Test coverage:**
- ✅ Valid public URLs (example.com, api.example.com)
- ✅ Scheme validation (file://, ftp://, gopher://)
- ✅ Localhost variations (localhost, 127.0.0.1, ::1)
- ✅ Cloud metadata (169.254.169.254, metadata.google.internal)
- ✅ Kubernetes services (*.svc.cluster.local)
- ✅ Private IP ranges (10.x, 172.16.x, 192.168.x)
- ✅ Link-local addresses (169.254.x.x)
- ✅ IPv6 private addresses

### Attack Scenarios Prevented

#### Scenario 1: Internal Service Scanning

**Attack attempt:**
```
User: @bot fetch this URL: http://localhost:6379/
```

**Agent response:**
```
❌ URL validation failed: access to 'localhost' is not allowed (localhost/metadata service)
```

#### Scenario 2: Cloud Metadata Access

**Attack attempt:**
```
User: @bot analyze http://169.254.169.254/latest/meta-data/iam/security-credentials/
```

**Agent response:**
```
❌ URL validation failed: access to '169.254.169.254' is not allowed (localhost/metadata service)
```

#### Scenario 3: Kubernetes Service Enumeration

**Attack attempt:**
```
User: @bot fetch http://kubernetes.default.svc.cluster.local/api/v1/namespaces
```

**Agent response:**
```
❌ URL validation failed: access to Kubernetes internal services (*.svc.cluster.local) is not allowed
```

#### Scenario 4: Private Network Scanning

**Attack attempt:**
```
User: @bot check this URL: http://192.168.1.1/admin
```

**Agent response:**
```
❌ URL validation failed: access to private/internal IP addresses is not allowed (resolved to 192.168.1.1)
```

#### Scenario 5: File System Access

**Attack attempt:**
```
User: @bot read file:///etc/passwd
```

**Agent response:**
```
❌ URL validation failed: unsupported URL scheme 'file': only http and https are allowed
```

### Security Best Practices

1. **Defense in Depth**: Multiple validation layers (scheme, hostname, DNS, IP range)
2. **Fail Closed**: Unknown or invalid URLs are rejected
3. **Clear Error Messages**: Users understand why URL was rejected (without exposing internal details)
4. **DNS Resolution**: Validates resolved IPs, not just hostnames (prevents DNS rebinding)
5. **No Bypass**: Validation happens before HTTP client creation

### Limitations

**Known limitations:**
- DNS rebinding attacks with very short TTLs are still theoretically possible
- IPv6 address format tricks (e.g., IPv4-mapped IPv6) are mitigated by checking all resolved IPs
- Time-of-check-time-of-use (TOCTOU) race conditions are possible if DNS changes between validation and fetch

**Mitigations:**
- HTTP client has 30-second timeout
- No redirect following to different hosts
- All resolved IPs are validated

### Monitoring & Alerts

**Log SSRF attempts:**

```bash
# Search for blocked URL attempts
grep "URL validation failed" logs/agent.log

# Monitor for patterns
grep "private/internal IP" logs/agent.log
grep "Kubernetes internal services" logs/agent.log
grep "localhost/metadata service" logs/agent.log
```

**Recommended alerts:**
- Alert on >10 SSRF attempts per hour from same user
- Alert on attempts to access cloud metadata services
- Alert on attempts to access Kubernetes API

### References

- [CWE-918: Server-Side Request Forgery (SSRF)](https://cwe.mitre.org/data/definitions/918.html)
- [OWASP SSRF Prevention Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html)
- [RFC 1918: Private Address Space](https://datatracker.ietf.org/doc/html/rfc1918)

---

## See Also

- [A2A_TOOLS.md](A2A_TOOLS.md) - A2A Tool Integration Guide
- [CONFIGURATION.md](CONFIGURATION.md) - Full configuration reference
- [CLAUDE.md](../CLAUDE.md) - General system architecture

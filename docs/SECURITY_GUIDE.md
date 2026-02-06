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
- Not stored in `api_keys` (which might be visible to external services)

### 2. JWT Bearer Authentication (API Gateway / Identity Provider)

**Purpose**: Authenticate requests from users or services that have been validated by an upstream API Gateway or identity provider (Keycloak, Auth0, Azure AD, etc.).

**Method**: JWT token via `Authorization: Bearer <token>` header

**How it works**:
1. Upstream API Gateway validates the JWT cryptographically
2. Knowledge Agent receives the request with the validated JWT
3. JWT is parsed (not re-validated) to extract `email` and `groups` claims
4. Email is used as caller ID and for permission checks (`allowed_emails`)
5. Groups are used for group-based permission checks (`allowed_groups`)
6. User gets `role: write` by default (fine-grained control via permissions system)

**Configuration**:
```yaml
# config.yaml
permissions:
  groups_claim_path: "groups"  # Path in JWT to extract groups
  # For Keycloak realm roles: "realm_access.roles"
```

**Example JWT claims used**:
```json
{
  "preferred_username": "john.doe",
  "email": "john.doe@company.com",
  "groups": ["/google-workspace/devops@company.com", "/google-workspace/all@company.com"]
}
```

**Security characteristics**:
- JWT is NOT validated cryptographically (assumes upstream API Gateway has already validated it)
- Only the `email`, `preferred_username`, and groups (at the configured claim path) are extracted
- Caller ID is set to `preferred_username` (or `email` if username not present)
- Groups enable the full permissions system (allowed_groups with write/read roles)

**Example request**:
```bash
curl -X POST http://localhost:8081/api/query \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9..." \
  -d '{"query": "What is our deployment process?"}'
```

### 3. External A2A Authentication (External Services → Agent)

**Purpose**: Secure direct API access from external agents or services.

**Method**: API keys via `X-API-Key` header

**Configuration**:
```yaml
# config.yaml - New format with roles
api_keys:
  ka_secret_abc123:
    caller_id: root-agent
    role: write
  ka_secret_def456:
    caller_id: external-service
    role: read  # Read-only (cannot save_to_memory)
```

**Environment Variable**:
```bash
# JSON format - Maps API key (secret) to config
# New format with roles:
API_KEYS='{"ka_secret_abc123":{"caller_id":"root-agent","role":"write"},"ka_secret_def456":{"caller_id":"external-service","role":"read"}}'

# Legacy format (assumes role="write"):
API_KEYS='{"ka_secret_abc123":"root-agent","ka_secret_def456":"external-service"}'
```

**How it works**:
1. External services send `X-API-Key` header with their secret token
2. Agent searches for which `client_id` has that secret in `api_keys` map
3. If secret matches → request is authenticated with that `client_id`
4. If secret not found → 401 Unauthorized

**Security characteristics**:
- Each external service gets its own client_id and secret token
- Client ID is used for logging and tracing (secrets are never logged)
- Secrets can be individually rotated by updating the value for that client_id
- Client ID appears in logs, making audit trails clear without exposing secrets

### 4. A2A Protocol Authentication

**Purpose**: Secure the A2A protocol endpoints (`/a2a/invoke`).

**Method**: API key via `X-API-Key` header (same as External A2A authentication)

**Configuration**:
```yaml
api_keys:
  ka_secret_abc123:
    caller_id: root-agent
    role: write
  ka_secret_def456:
    caller_id: metrics-agent
    role: write
```

**How it works**:
1. Request arrives at `/a2a/invoke`
2. HTTP middleware validates `X-API-Key` header against `api_keys`
3. If key is valid → request proceeds with authenticated caller
4. If key is invalid → `401 Unauthorized`
5. If `api_keys` is empty → Open mode (no authentication)

**Note**: The agent card at `/.well-known/agent-card.json` is always public to allow agent discovery.

**Security characteristics**:
- Uses constant-time comparison to prevent timing attacks
- Case-insensitive header matching (X-API-Key, x-api-key, etc.)
- Same authentication as `/api/*` endpoints

### 5. Slack Signature Authentication (Legacy)

**Purpose**: Verify requests come directly from Slack (when not using Slack Bridge).

**Method**: HMAC signature via `X-Slack-Signature` header

**Configuration**:
```yaml
slack:
  signing_secret: ${SLACK_SIGNING_SECRET}
```

**Note**: This is legacy support. In the standard architecture, Slack events go through the Slack Bridge, which uses internal token authentication.

## Authentication Flow

### Port 8081 (Custom HTTP)

```
┌─────────────────────────────────────────────────────────────┐
│ Request arrives at Agent (Port 8081)                         │
│ (JWT from Authorization header is parsed early if present)   │
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
│ Token matches    │    │ Has valid JWT       │
│ internal_token?  │    │ (Bearer token)?     │
└────┬─────────┬───┘    └──────┬──────────────┘
     │         │               │
   YES        NO          ┌────┴────┐
     │         │          │ YES     │ NO
     │         │          ▼         ▼
     │         │   ┌─────────┐   ┌──────────────────┐
     │         │   │ ALLOW   │   │ Has X-API-Key    │
     │         │   │ as JWT  │   │ header?           │
     │         │   │ user    │   └─────┬────────────┘
     │         │   │ (email/ │         │
     │         │   │ groups) │    ┌────┴────┐
     │         │   └─────────┘    │ YES     │ NO
     │         │                  ▼         ▼
     │         │           ┌─────────┐   ┌──────────────────┐
     │         │           │ Key in  │   │ Has Slack        │
     │         │           │ APIKeys?│   │ Signature?       │
     │         │           └────┬────┘   └─────┬────────────┘
     │         │                │              │
     │         │            ┌───┴───┐      ┌───┴───┐
     │         │            │ YES   │ NO   │ YES   │ NO
     ▼         ▼            ▼       ▼      ▼       ▼
┌────────┐ ┌───────────────────────────┐ ┌──────────────┐
│ ALLOW  │ │      401 Unauthorized      │ │ Check if     │
│ as     │ │                            │ │ auth enabled │
│ slack- │ │                            │ └──────┬───────┘
│ bridge │ │                            │        │
└────────┘ └────────────────────────────┘   ┌────┴────┐
                                             │ Enabled │ Disabled
                                             ▼         ▼
                                         ┌──────┐  ┌─────────────┐
                                         │ 401  │  │ ALLOW as    │
                                         │      │  │ unauthenticated │
                                         └──────┘  └─────────────┘
```

### A2A Endpoint (/a2a/invoke)

The `/a2a/invoke` and `/api/query/stream` endpoints follow the same authentication flow as `/api/query`.
The agent card at `/.well-known/agent-card.json` is always public (no auth) for agent discovery.

## Security Modes

### Production Mode (Recommended)

**Configuration**:
```yaml
auth:
  internal_token: ${INTERNAL_AUTH_TOKEN}  # Required

api_keys:
  root-agent: ${A2A_SECRET_ROOT}
  monitoring: ${A2A_SECRET_MONITORING}
  # Add other external services as needed
```

**Behavior**:
- Slack Bridge → Agent: Requires internal token
- External services → Agent: Requires secret token from api_keys
- Unauthenticated requests → 401 Unauthorized

**Use for**: Production deployments, staging environments

### Development Mode (Open Access)

**Configuration**:
```yaml
auth:
  internal_token: ""  # Empty or omitted

api_keys: {}  # Empty or omitted
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

api_keys: {}  # Empty
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
      API_KEYS: '${API_KEYS}'
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
        - name: API_KEYS
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
3. For development: Set both `internal_token` and `api_keys` to empty to enable open mode

### "Invalid API key"

**Cause**: API key not found in `api_keys` map

**Solution**:
1. Verify the key exists in configuration
2. Check for typos in API key
3. Ensure configuration was reloaded after adding key

---

## Rate Limiting

The Knowledge Agent implements **per-IP rate limiting** to prevent abuse and ensure fair resource usage.

### Configuration

Rate limiting is enabled by default with the following settings:

| Setting | Value | Description |
|---------|-------|-------------|
| Rate | 10 requests/second | Token refill rate |
| Burst | 20 requests | Maximum burst capacity |
| Algorithm | Token Bucket | Standard rate limiting algorithm |

> **Note**: Rate and burst values are currently hardcoded. Only `trusted_proxies` can be configured.

### Trusted Proxies Configuration

When running behind a load balancer or reverse proxy, you must configure `trusted_proxies` to correctly identify client IPs:

```yaml
# config.yaml
server:
  agent_port: 8081
  slack_bot_port: 8080

  # Trust X-Forwarded-For header ONLY from these IPs/CIDRs
  trusted_proxies:
    - "10.0.0.0/8"       # Kubernetes/Docker networks
    - "172.16.0.0/12"    # Private network
    - "192.168.0.0/16"   # Private network
    - "203.0.113.50"     # Specific proxy IP
```

### How Trusted Proxies Work

1. **Request arrives** with `RemoteAddr: 10.0.0.5:12345`
2. **Check if RemoteAddr is trusted**: Is `10.0.0.5` in `trusted_proxies`?
3. **If trusted**: Use the **first IP** from `X-Forwarded-For` header as client IP
4. **If NOT trusted**: Ignore `X-Forwarded-For`, use `RemoteAddr` directly

This prevents **IP spoofing attacks** where an attacker sends:
```
X-Forwarded-For: 1.2.3.4
```
If the request doesn't come from a trusted proxy, the spoofed header is ignored.

### Example Scenarios

**Scenario 1: Direct connection (no proxy)**
```
RemoteAddr: 203.0.113.100:12345
X-Forwarded-For: (none)
trusted_proxies: []

→ Rate limit by: 203.0.113.100
```

**Scenario 2: Behind trusted proxy**
```
RemoteAddr: 10.0.0.5:12345  (load balancer)
X-Forwarded-For: 203.0.113.100, 10.0.0.5
trusted_proxies: ["10.0.0.0/8"]

→ Rate limit by: 203.0.113.100  (real client IP)
```

**Scenario 3: Spoofing attempt (untrusted source)**
```
RemoteAddr: 203.0.113.100:12345  (attacker)
X-Forwarded-For: 1.2.3.4  (spoofed)
trusted_proxies: ["10.0.0.0/8"]

→ Rate limit by: 203.0.113.100  (spoofed header ignored)
```

### Rate Limit Response

When rate limit is exceeded, the server returns:

**HTTP Status**: `429 Too Many Requests`

**Response Body**:
```json
{
  "success": false,
  "message": "Rate limit exceeded. Please try again later."
}
```

### Monitoring

Check logs for rate limiting events:

```bash
# Look for rate limit warnings
grep "rate limit" logs/agent.log
```

### Security Best Practices

1. **Always configure trusted_proxies** in production when behind a load balancer
2. **Never trust X-Forwarded-For by default** - leave `trusted_proxies` empty if not using a proxy
3. **Use specific IPs/CIDRs** rather than broad ranges when possible
4. **Monitor rate limit hits** to detect potential attacks or misconfigured clients

---

## Permissions System (save_to_memory Control)

The Knowledge Agent implements **fine-grained permissions** to control who can save information to the knowledge base using **JWT claims** (email and groups).

### Configuration

```yaml
permissions:
  # Path in JWT token to extract groups (depends on your identity provider)
  groups_claim_path: "groups"  # Default. For Keycloak realm roles: "realm_access.roles"

  # List of emails with permissions
  allowed_emails:
    - value: "admin@company.com"
      role: "write"    # Can save to memory
    - value: "viewer@company.com"
      role: "read"     # Can only search memory

  # List of groups from JWT with permissions
  allowed_groups:
    - value: "/google-workspace/devops@company.com"  # Google Workspace group
      role: "write"
    - value: "knowledge-admins"  # Keycloak group or realm role
      role: "write"
    - value: "knowledge-readers"
      role: "read"
```

### Permission Modes

#### Open Mode (No Restrictions)

When permission lists are empty, **everyone with role=write can save**:

```yaml
permissions:
  allowed_emails: []
  allowed_groups: []
```

**Result**: Any authenticated user with write role can save.

#### Controlled Access Mode

When you configure specific emails/groups, **only they** can save:

```yaml
permissions:
  groups_claim_path: "groups"
  allowed_emails:
    - value: "admin@company.com"
      role: "write"
  allowed_groups:
    - value: "devops-team"
      role: "write"
```

**Result**:
- ✅ admin@company.com → Can save
- ✅ Users in "devops-team" group → Can save
- ❌ Other users → **Cannot** save (even with JWT)

### How It Works

1. **Authentication Layer**: JWT parsed → email and groups extracted to context
2. **Permission Check**: When `save_to_memory` tool is called:
   - Check if role="read" in context → Block (read-only access)
   - Check if email is in `allowed_emails` with role="write" → Allow
   - Check if any user group is in `allowed_groups` with role="write" → Allow
   - Otherwise → **Block with error**
3. **Error to Agent**: Permission error returned to LLM agent, which informs user

### Implementation

Permissions are enforced at the **tool level** using a wrapper pattern:

```go
// internal/agent/permission_memory_service.go
type PermissionMemoryService struct {
    baseService       memory.Service
    permissionChecker *MemoryPermissionChecker
    contextHolder     *contextHolder
}

func (s *PermissionMemoryService) AddSession(ctx context.Context, sess session.Session) error {
    requestCtx := s.resolvePermissionContext(ctx)

    // Check permissions FIRST
    canSave, reason := s.permissionChecker.CanSaveToMemory(requestCtx)

    if !canSave {
        return fmt.Errorf("⛔ Insufficient permissions. Reason: %s", reason)
    }

    return s.baseService.AddSession(ctx, sess)
}
```

### Use Cases

#### Case 1: Team-Based Access (Google Workspace)

```yaml
permissions:
  groups_claim_path: "groups"
  allowed_groups:
    - value: "/google-workspace/devops@company.com"
      role: "write"
    - value: "/google-workspace/engineering@company.com"
      role: "write"
    - value: "/google-workspace/all@company.com"
      role: "read"
```

**Behavior**:
- DevOps and Engineering teams can save
- Everyone else can only search

#### Case 2: Keycloak Realm Roles

```yaml
permissions:
  groups_claim_path: "realm_access.roles"
  allowed_groups:
    - value: "knowledge-admin"
      role: "write"
    - value: "knowledge-user"
      role: "read"
```

**Behavior**:
- Users with `knowledge-admin` realm role can save
- Users with `knowledge-user` role can only search

#### Case 3: Named Users Only

```yaml
permissions:
  allowed_emails:
    - value: "john.doe@company.com"
      role: "write"
    - value: "jane.smith@company.com"
      role: "write"
  allowed_groups: []
```

**Behavior**:
- Only John and Jane can save knowledge
- Rest of company can only search

### Logging & Auditing

Every request logs permission status:

```log
INFO  agent/permission_memory_service.go  save_to_memory permission granted
      caller_id=john.doe@company.com
      user_email=john.doe@company.com
      user_groups=["/google-workspace/devops@company.com", "/google-workspace/all@company.com"]
      can_save=true
      permission_reason="group '/google-workspace/devops@company.com' has write permission"
      session_id=query-C123XYZ-1234567890
```

**Blocked save:**
```log
WARN  agent/permission_memory_service.go  save_to_memory BLOCKED: insufficient permissions
      caller_id=guest@company.com
      user_email=guest@company.com
      user_groups=["/google-workspace/all@company.com"]
      can_save=false
      permission_reason="no matching email or group found in allowed permissions"
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

Bot: ⛔ Insufficient permissions. Only authorized users can save
     information to the knowledge base.
```

The agent receives the error from the tool and communicates it to the user.

### Configuring Groups Claim Path

The `groups_claim_path` depends on your identity provider:

| Provider | Typical Path | Example JWT Claim |
|----------|-------------|-------------------|
| Google Workspace (via Keycloak) | `groups` | `"groups": ["/google-workspace/dev@company.com"]` |
| Keycloak Realm Roles | `realm_access.roles` | `"realm_access": {"roles": ["admin"]}` |
| Keycloak Groups | `groups` | `"groups": ["/admins", "/developers"]` |
| Auth0 | `https://myapp/groups` | Custom claim path |
| Azure AD | `groups` | `"groups": ["guid1", "guid2"]` |

### Security Notes

- **JWT-based**: Permissions extracted from validated JWT tokens
- **Role precedence**: `role="read"` in context blocks saves regardless of email/group
- **Tool-level enforcement**: Cannot be bypassed by prompt engineering
- **Audit trail**: All permission checks logged with email and groups
- **A2A identity propagation**: User identity flows through sub-agent calls

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

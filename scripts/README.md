# Scripts Directory

Utility scripts for Knowledge Agent development and operations.

## Available Scripts

### Authentication & Security

#### `generate-auth-token.sh`
Generate secure authentication tokens.

**Usage:**
```bash
# Generate internal token (Slack-bot ↔ Agent)
./scripts/generate-auth-token.sh internal
# or
make gen-token TYPE=internal

# Generate A2A API key (External services → Agent)
./scripts/generate-auth-token.sh a2a monitoring
# or
make gen-token TYPE=a2a SERVICE=monitoring
```

**Examples:**
```bash
# Internal token
$ make gen-token TYPE=internal
Generating internal authentication token...

INTERNAL_AUTH_TOKEN=e02dcab50bd6956a870c7fe6cf276179765559f47b499b300b79773513ea3866

Add this to both agent and slack-bot .env files:
  INTERNAL_AUTH_TOKEN=e02dcab50bd6956a870c7fe6cf276179765559f47b499b300b79773513ea3866

# A2A API key
$ make gen-token TYPE=a2a SERVICE=analytics
Generating A2A API key for service: analytics

API Key: ka_analytics_f8e9d7c6b5a49382

Add this to agent a2a_api_keys configuration:
  ka_analytics_f8e9d7c6b5a49382: analytics
```

#### `test-auth.sh`
Test authentication endpoints to verify security configuration.

**Usage:**
```bash
./scripts/test-auth.sh
# or
make test-auth
```

**Tests:**
1. ❌ No authentication (should fail with 401)
2. ❌ Invalid API key (should fail with 401)
3. ✅ Valid API key (should succeed with 200)
4. ✅ Valid internal token (should succeed with 200)
5. ❌ Invalid internal token (should fail with 401)
6. ✅ Ingest endpoint with valid auth (should succeed with 200)

**Expected Output:**
```
======================================
  Knowledge Agent Authentication Tests
======================================

Checking if agent is running...
✓ Agent is running

======================================
Test 1: No Authentication (should fail)
======================================
HTTP Status: 401
Response: {"message":"Authentication required","success":false}
✓ PASS: Correctly rejected (401)

[... more tests ...]

======================================
  Test Summary
======================================

Authentication is working correctly if:
  • Tests 1, 2, 5 returned 401 (rejected)
  • Tests 3, 4, 6 returned 200 (accepted)

Security model:
  ✓ Internal Token: Slack-bot → Agent communication
  ✓ API Keys: External services → Agent communication
  ✗ No auth: Blocked (401 Unauthorized)
```

### Database

Run database migrations.

**Usage:**
```bash
# or
```

Applies all migrations from `migrations/` directory to PostgreSQL.

### Development

#### `setup-dev.sh`
Initial development environment setup (if exists).

**Usage:**
```bash
./scripts/setup-dev.sh
# or
make setup
```

## Quick Reference

### Common Tasks

**Generate tokens for new deployment:**
```bash
# 1. Generate internal token
make gen-token TYPE=internal

# 2. Add to .env
echo "INTERNAL_AUTH_TOKEN=<generated-token>" >> .env

# 3. Generate A2A keys for external services
make gen-token TYPE=a2a SERVICE=rootagent
make gen-token TYPE=a2a SERVICE=monitoring

# 4. Add to config.yaml
```

**Test authentication:**
```bash
# Start agent
make dev-agent

# In another terminal
make test-auth
```

**Migrate database:**
```bash
# Make sure PostgreSQL is running
make docker-up

# Run migrations
```

## See Also

- [docs/SECURITY_GUIDE.md](../docs/SECURITY_GUIDE.md) - Complete security guide
- [README.md](../README.md) - Main project documentation
- [CLAUDE.md](../CLAUDE.md) - Development guide

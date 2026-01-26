# Security Policy

## Supported Versions

We release patches for security vulnerabilities. Currently supported versions:

| Version | Supported          |
| ------- | ------------------ |
| 0.x.x   | :white_check_mark: |

## Reporting a Vulnerability

We take the security of Knowledge Agent seriously. If you believe you have found a security vulnerability, please report it to us as described below.

### Please DO NOT:

- Open a public GitHub issue for security vulnerabilities
- Disclose the vulnerability publicly before it has been addressed

### Please DO:

**Report security vulnerabilities via GitHub Security Advisories:**

1. Go to https://github.com/freepik-company/knowledge-agent/security/advisories
2. Click "Report a vulnerability"
3. Fill in the details about the vulnerability

**Or email us directly at:**
- Email: security@freepik.com
- Subject: [SECURITY] Knowledge Agent - Brief description

### What to Include:

- Description of the vulnerability
- Steps to reproduce the issue
- Possible impact of the vulnerability
- Suggested fix (if you have one)

### Response Timeline:

- **Initial Response**: Within 48 hours
- **Status Update**: Within 7 days
- **Fix Timeline**: Depends on severity
  - Critical: Within 7 days
  - High: Within 14 days
  - Medium: Within 30 days
  - Low: Within 60 days

## Security Best Practices

### For Users:

1. **Use Strong Secrets**
   ```bash
   # Generate secure tokens
   openssl rand -hex 32
   ```

2. **Rotate Credentials Regularly**
   - API keys every 90 days
   - Internal tokens every 6 months
   - Database passwords every 6 months

3. **Enable Authentication**
   ```yaml
   # Always use authentication in production
   internal_auth_token: ${INTERNAL_AUTH_TOKEN}
   a2a_api_keys: ${A2A_API_KEYS}
   ```

4. **Use HTTPS in Production**
   - Never expose HTTP endpoints publicly
   - Use TLS certificates for Slack webhooks
   - Verify Slack signature validation is enabled

5. **Restrict Network Access**
   - PostgreSQL: Only from application
   - Redis: Only from application
   - Ollama: Only from application
   - Knowledge Agent API: Only from authorized sources

6. **Keep Dependencies Updated**
   ```bash
   # Check for updates
   go list -u -m all

   # Update dependencies
   go get -u ./...
   go mod tidy
   ```

7. **Monitor Logs for Suspicious Activity**
   - Failed authentication attempts
   - Unusual query patterns
   - Database errors
   - Rate limit violations

### Configuration Security:

**NEVER commit secrets to git:**
```yaml
# ❌ WRONG - Hardcoded secrets
anthropic:
  api_key: sk-ant-api03-xxxxx

# ✅ CORRECT - Environment variables
anthropic:
  api_key: ${ANTHROPIC_API_KEY}
```

**Use .gitignore properly:**
```gitignore
.env
.env.*
!.env.example
config.yaml
config-local.yaml
secrets/
*.key
*.pem
```

**Set proper file permissions:**
```bash
# Restrict config file access
chmod 600 config.yaml

# Restrict secret files
chmod 600 .env
```

### Database Security:

1. **Use Strong Passwords**
   ```bash
   # Generate strong password
   openssl rand -base64 32
   ```

2. **Enable SSL/TLS for PostgreSQL**
   ```bash
   POSTGRES_URL=postgres://user:pass@host:5432/db?sslmode=require
   ```

3. **Restrict Database User Permissions**
   ```sql
   -- Application user should NOT have SUPERUSER
   CREATE USER knowledge_agent WITH PASSWORD 'strong-password';
   GRANT CONNECT ON DATABASE knowledge_agent TO knowledge_agent;
   GRANT USAGE ON SCHEMA public TO knowledge_agent;
   GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO knowledge_agent;
   ```

4. **Regular Backups**
   ```bash
   # Automated daily backups
   pg_dump $POSTGRES_URL > backup-$(date +%Y%m%d).sql
   ```

### Slack Security:

1. **Verify Webhook Signatures**
   ```yaml
   slack:
     signing_secret: ${SLACK_SIGNING_SECRET}
   ```

2. **Use Minimal OAuth Scopes**
   - Required: `chat:write`, `app_mentions:read`, `channels:history`, `users:read`
   - Avoid: `admin.*`, `files:write`, `chat:write.public`

3. **Restrict Bot Access**
   - Only add bot to necessary channels
   - Use private channels for sensitive discussions

### MCP Security:

1. **Validate MCP Server Commands**
   ```yaml
   # ✅ SAFE - Known npm package
   mcp:
     servers:
       - name: filesystem
         command:
           path: "npx"
           args: ["-y", "@modelcontextprotocol/server-filesystem", "/allowed/path"]

   # ❌ DANGEROUS - Arbitrary commands
   # Don't allow user-provided commands without validation
   ```

2. **Restrict Filesystem Access**
   ```yaml
   # Limit filesystem MCP to specific directories
   command:
     args: ["-y", "@modelcontextprotocol/server-filesystem", "/workspace"]
   # NOT: /
   ```

3. **Use Authentication for Remote MCPs**
   ```yaml
   - name: github
     endpoint: "https://api.github.com/mcp"
     auth:
       type: bearer
       token_env: GITHUB_PAT
   ```

## Known Security Considerations

### 1. LLM Prompt Injection

**Risk**: Users could attempt to manipulate the agent's behavior through crafted prompts.

**Mitigation**:
- System prompt is isolated from user input
- Tool calls are validated
- Permissions system restricts who can save to memory
- All user input is logged for audit

### 2. Sensitive Information in Memory

**Risk**: Users might accidentally save secrets, passwords, or PII to the knowledge base.

**Mitigation**:
- Use permissions system to restrict who can save
- Implement content filtering (future enhancement)
- Regular audits of saved memories
- Clear retention policies

### 3. API Rate Limiting

**Risk**: Abuse of API endpoints could lead to high costs or service degradation.

**Mitigation**:
- Implement rate limiting per user/caller
- Monitor API usage in Langfuse
- Set cost alerts
- Use authentication to track callers

### 4. Embedding Model Access

**Risk**: Ollama endpoint could be abused if exposed.

**Mitigation**:
- Run Ollama on localhost only
- Use Docker network isolation
- Don't expose Ollama port publicly

### 5. PostgreSQL Injection

**Risk**: SQL injection through memory content or metadata.

**Mitigation**:
- Use parameterized queries (ADK uses prepared statements)
- Validate metadata before storing
- Restrict database user permissions

## Security Updates

Security updates will be published as GitHub Security Advisories and tagged releases. Subscribe to:

- GitHub: Watch → Custom → Security alerts
- RSS: https://github.com/freepik-company/knowledge-agent/releases.atom

## Acknowledgments

We appreciate the security research community's efforts to responsibly disclose vulnerabilities. Contributors who report valid security issues will be:

- Acknowledged in the security advisory (unless they prefer to remain anonymous)
- Credited in the release notes
- Added to our SECURITY_CONTRIBUTORS.md hall of fame

## Contact

For security concerns, contact:
- Email: sre@freepik.com
- GitHub Security Advisories: https://github.com/freepik-company/knowledge-agent/security/advisories
- PGP Key: Available on request

---

Thank you for helping keep Knowledge Agent and our users safe!

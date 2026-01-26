# MCP Integration Guide

## Overview

The Knowledge Agent integrates with **Model Context Protocol (MCP)** to enable conversational access to external data sources like filesystems, GitHub, Google Drive, databases, and more. MCP servers provide tools that Claude can intelligently use to fetch, analyze, and save information.

**Key Benefits:**
- ✅ **Conversational Access**: "Read the deployment docs from Google Drive and save key steps"
- ✅ **Intelligent Tool Selection**: Claude decides which MCP tools to use based on context
- ✅ **Automatic Knowledge Capture**: Agent saves valuable information to memory automatically
- ✅ **Flexible Configuration**: Enable/disable servers via YAML, no code changes needed
- ✅ **Graceful Degradation**: Failed servers don't prevent agent startup

## Architecture

```
User Query
  ↓
Knowledge Agent (ADK Runner)
  ↓
Claude Sonnet 4.5 (LLM)
  ↓
Tool Selection (search_memory, save_to_memory, fetch_url, MCP tools...)
  ↓
MCP Transport Layer (Command/SSE/Streamable)
  ↓
MCP Servers (Filesystem, GitHub, SQLite, etc.)
  ↓
External Data Sources
```

### Key Components

1. **MCP Factory** (`internal/mcp/factory.go`): Creates MCP toolsets from configuration
2. **Transport Types**: Command (stdio), SSE, Streamable HTTP
3. **ADK Integration**: Native `mcptoolset` support in ADK v0.3.0
4. **MCP Go SDK**: `github.com/modelcontextprotocol/go-sdk` (v0.7.0)

### How It Works

1. **Startup**: Agent loads MCP config and creates transports
2. **Tool Discovery**: Each MCP server exposes tools (read_file, list_repositories, etc.)
3. **LLM Receives Tools**: Claude sees all available tools in its context
4. **Intelligent Usage**: Claude decides which tools to call based on user query
5. **Response**: Agent executes tools and synthesizes response

## Configuration

### Basic Configuration

Add to your `config.yaml`:

```yaml
mcp:
  enabled: true
  servers:
    - name: "filesystem"
      description: "Local filesystem operations"
      enabled: true
      transport_type: "command"
      command:
        path: "npx"
        args:
          - "-y"
          - "@modelcontextprotocol/server-filesystem"
          - "/workspace"
      timeout: 30
```

### Configuration Fields

#### MCPConfig

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable MCP integration |
| `servers` | []MCPServerConfig | `[]` | List of MCP servers |

#### MCPServerConfig

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Server identifier (for logs) |
| `description` | string | No | Human-readable description |
| `enabled` | bool | No (default: true) | Enable this server |
| `transport_type` | string | Yes | "command", "sse", or "streamable" |
| `command` | MCPCommandConfig | Conditional | Required for command transport |
| `endpoint` | string | Conditional | Required for sse/streamable |
| `auth` | MCPAuthConfig | No | Authentication config |
| `tool_filter` | []string | No | Whitelist of tool names (empty = all) |
| `timeout` | int | No (default: 30) | Connection timeout in seconds |

#### MCPCommandConfig

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `path` | string | Yes | Executable path (e.g., "npx", "/usr/bin/python") |
| `args` | []string | No | Command arguments |
| `env` | map[string]string | No | Additional environment variables |

#### MCPAuthConfig

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | Yes | "bearer", "basic", or "oauth2" |
| `token_env` | string | Conditional | Environment variable name (recommended) |
| `token` | string | Conditional | Token value (not recommended) |
| `username` | string | Conditional | Username (for basic auth) |
| `password` | string | Conditional | Password (for basic auth) |

### Transport Types

#### 1. Command Transport

Runs local executables via stdio (most common for npm packages).

```yaml
transport_type: "command"
command:
  path: "npx"
  args:
    - "-y"
    - "@modelcontextprotocol/server-filesystem"
    - "/workspace"
  env:
    DEBUG: "true"
```

**Use Cases**: Filesystem, SQLite, Git, local scripts

**Advantages**:
- Fast (local execution)
- No network required
- Simple setup

**Disadvantages**:
- Requires local installation
- Limited to local resources

#### 2. SSE Transport

Server-Sent Events over HTTP (for remote MCP servers).

```yaml
transport_type: "sse"
endpoint: "https://api.github.com/mcp"
auth:
  type: "bearer"
  token_env: "GITHUB_PAT"
```

**Use Cases**: GitHub, Google APIs, remote services

**Advantages**:
- Remote access
- Centralized management
- No local installation

**Disadvantages**:
- Network latency
- Requires authentication
- Internet dependency

#### 3. Streamable Transport

HTTP streaming protocol (for custom MCP servers).

```yaml
transport_type: "streamable"
endpoint: "https://internal-mcp.company.com/v1"
auth:
  type: "bearer"
  token_env: "INTERNAL_API_KEY"
```

**Use Cases**: Internal services, custom MCP implementations

## Authentication

### Bearer Token (Recommended)

```yaml
auth:
  type: "bearer"
  token_env: "GITHUB_PAT"  # Environment variable
```

Set environment variable:
```bash
export GITHUB_PAT=ghp_your_token_here
```

### Basic Authentication

```yaml
auth:
  type: "basic"
  username: "myuser"
  password: "mypass"  # Or use environment variable
```

### OAuth2

```yaml
auth:
  type: "oauth2"
  token_env: "OAUTH_TOKEN"
```

**Note**: OAuth2 refresh is not yet implemented. Use long-lived tokens.

## Tool Filtering

Restrict which MCP tools the agent can use:

```yaml
tool_filter:
  - "read_file"
  - "list_directory"
  - "search_files"
# Empty or omitted = all tools allowed
```

**Why Filter?**
- Security: Prevent write operations
- Performance: Reduce tool discovery overhead
- Compliance: Limit agent capabilities

**Example Filters:**

```yaml
# Read-only filesystem
tool_filter:
  - "read_file"
  - "list_directory"
  - "search_files"

# GitHub read-only
tool_filter:
  - "list_repositories"
  - "get_file_contents"
  - "search_repositories"
  - "list_issues"

# Database queries only (no writes)
tool_filter:
  - "read_query"
  - "list_tables"
  - "describe_table"
```

## Available MCP Servers

### Official Servers

| Server | Package | Transport | Use Case |
|--------|---------|-----------|----------|
| Filesystem | `@modelcontextprotocol/server-filesystem` | Command | File operations |
| GitHub | `@modelcontextprotocol/server-github` | SSE | Repository access |
| Google Drive | `@modelcontextprotocol/server-gdrive` | SSE | Document access |
| SQLite | `@modelcontextprotocol/server-sqlite` | Command | Database queries |
| Postgres | `@modelcontextprotocol/server-postgres` | Command | Database queries |
| Memory | `@modelcontextprotocol/server-memory` | Command | Knowledge graph |
| Brave Search | `@modelcontextprotocol/server-brave-search` | SSE | Web search |
| Slack | `@modelcontextprotocol/server-slack` | SSE | Workspace data |

### Installation

```bash
# Filesystem
npm install -g @modelcontextprotocol/server-filesystem

# GitHub
npm install -g @modelcontextprotocol/server-github

# SQLite
npm install -g @modelcontextprotocol/server-sqlite

# Or use npx (no installation needed)
# npx automatically downloads and runs packages
```

## Usage Examples

### Filesystem Operations

**Config:**
```yaml
mcp:
  enabled: true
  servers:
    - name: "filesystem"
      transport_type: "command"
      command:
        path: "npx"
        args: ["-y", "@modelcontextprotocol/server-filesystem", "/docs"]
```

**Queries:**
- "What files are in /docs?"
- "Read the deployment guide and summarize it"
- "Find all markdown files containing 'API'"
- "Read all .md files and save important information to memory"

### GitHub Integration

**Config:**
```yaml
mcp:
  enabled: true
  servers:
    - name: "github"
      transport_type: "sse"
      endpoint: "https://api.github.com/mcp"
      auth:
        type: "bearer"
        token_env: "GITHUB_PAT"
```

**Queries:**
- "List open PRs in myorg/myrepo"
- "Get the README from that repository"
- "What are the recent issues in the project?"
- "Summarize PR #123 and save key changes to memory"

### Database Queries

**Config:**
```yaml
mcp:
  enabled: true
  servers:
    - name: "sqlite"
      transport_type: "command"
      command:
        path: "npx"
        args: ["-y", "@modelcontextprotocol/server-sqlite", "/data/analytics.db"]
```

**Queries:**
- "What tables are in the database?"
- "Query user activity for last week"
- "Find top 10 errors from logs table"
- "Analyze patterns and save insights to memory"

### Multiple Servers

**Config:**
```yaml
mcp:
  enabled: true
  servers:
    - name: "filesystem"
      transport_type: "command"
      command:
        path: "npx"
        args: ["-y", "@modelcontextprotocol/server-filesystem", "/docs"]

    - name: "github"
      transport_type: "sse"
      endpoint: "https://api.github.com/mcp"
      auth:
        type: "bearer"
        token_env: "GITHUB_PAT"
```

**Queries:**
- "Compare our deployment docs with the process in GitHub repo xyz/abc"
- "Read local architecture.md and the GitHub wiki, then save key decisions"

## Error Handling

### Graceful Degradation

Failed MCP servers don't prevent agent startup:

```
WARN  Failed to create MCP toolset, skipping  server=github error=connection timeout
INFO  MCP toolsets created successfully  count=2
```

The agent starts with available servers only.

### Common Errors

#### Command Not Found

```
Error: failed to create MCP toolset: exec: "npx": executable file not found
```

**Solution**: Install Node.js and npm
```bash
brew install node  # macOS
apt install nodejs npm  # Ubuntu
```

#### Permission Denied

```
Error: permission denied: /restricted-path
```

**Solution**: Check filesystem permissions or change path

#### Authentication Failed

```
Error: 401 Unauthorized
```

**Solution**: Verify token is set and valid
```bash
echo $GITHUB_PAT  # Should output your token
```

#### Timeout

```
Error: context deadline exceeded
```

**Solution**: Increase timeout in config
```yaml
timeout: 60  # seconds
```

### Debugging

Enable debug logging:

```yaml
log:
  level: debug
```

Check logs for MCP-related messages:

```bash
grep "MCP" logs/agent.log
grep "toolset" logs/agent.log
```

## Security Considerations

### 1. Token Management

✅ **DO:**
- Store tokens in environment variables
- Use `token_env` in config
- Rotate tokens periodically
- Use minimal permissions

❌ **DON'T:**
- Commit tokens to Git
- Use `token` field in config
- Share tokens across environments
- Use admin/root tokens

### 2. Filesystem Access

✅ **DO:**
- Restrict to specific directories
- Use read-only where possible
- Apply tool filters
- Audit file access

❌ **DON'T:**
- Allow root directory access
- Enable write operations without oversight
- Trust user-provided paths blindly

### 3. Tool Filtering

✅ **DO:**
- Whitelist specific tools
- Use least privilege principle
- Review tool list regularly
- Document allowed operations

❌ **DON'T:**
- Enable all tools by default
- Allow dangerous operations (delete, format)
- Skip tool filter in production

### 4. Network Security

✅ **DO:**
- Use HTTPS endpoints
- Verify SSL certificates
- Set reasonable timeouts
- Monitor network usage

❌ **DON'T:**
- Use HTTP in production
- Disable SSL verification
- Set unlimited timeouts
- Expose internal endpoints

## Performance Optimization

### Startup Time

Each MCP server adds ~1-2 seconds to startup. Minimize by:

1. Only enable needed servers
2. Use tool filters
3. Consider lazy loading (future enhancement)

### Request Latency

**Command transport** (~10-100ms):
- Fastest option
- Local execution
- No network overhead

**SSE transport** (~100-1000ms):
- Network latency
- API rate limits
- Consider caching

### Tool Discovery

Tools are discovered at startup and cached. To optimize:

1. Use `tool_filter` to reduce discovery overhead
2. Disable unused servers
3. Monitor tool count in logs

### Resource Usage

**Memory:**
- Each MCP server: ~10-50 MB
- Command processes: Persistent until agent shutdown
- Monitor with: `ps aux | grep npx`

**CPU:**
- Idle: Minimal (<1%)
- Active: Depends on tool operations
- File operations: Low
- Database queries: Medium
- API calls: Low

## Observability

### Langfuse Integration

MCP tool calls are tracked in Langfuse:

1. **Tool Observations**: Each MCP tool call logged
2. **Latency Tracking**: Time spent in MCP operations
3. **Error Tracking**: Failed tool calls with errors
4. **Cost Attribution**: Token usage per MCP query

View in Langfuse UI:
- Filter by tool name (e.g., "read_file")
- Analyze usage patterns
- Monitor error rates
- Track costs per MCP server

### Metrics

Monitor these metrics:

- MCP server count (startup)
- Tool discovery count (startup)
- Tool call frequency (runtime)
- Tool error rate (runtime)
- Tool latency percentiles (runtime)

### Logging

MCP-related logs:

```
INFO  MCP integration enabled  servers=3
INFO  Creating MCP toolset  server=filesystem transport=command
INFO  MCP toolset created successfully  server=filesystem
INFO  MCP toolsets created successfully  count=3
WARN  Failed to create MCP toolset, skipping  server=github error=timeout
```

## Testing

### Local Testing

1. **Install MCP Server:**
```bash
npm install -g @modelcontextprotocol/server-filesystem
```

2. **Create Test Directory:**
```bash
mkdir -p /tmp/test-workspace
echo "Test file" > /tmp/test-workspace/test.txt
```

3. **Configure:**
```yaml
mcp:
  enabled: true
  servers:
    - name: "filesystem"
      transport_type: "command"
      command:
        path: "npx"
        args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp/test-workspace"]
```

4. **Start Agent:**
```bash
make dev
```

5. **Test Query:**
```bash
curl -X POST http://localhost:8081/api/query \
  -H "X-API-Key: your-key" \
  -d '{"question": "List files in the workspace"}'
```

### Verification

Check logs for:
```
INFO  MCP toolset created successfully  server=filesystem
```

Check Langfuse for:
- Tool observations (e.g., "list_directory")
- Successful responses
- Latency metrics

### Integration Testing

See `examples/mcp/README.md` for comprehensive test scenarios.

## Troubleshooting

### MCP Not Loading

**Symptoms**: No MCP tools available

**Check:**
1. `mcp.enabled: true` in config
2. At least one server enabled
3. No errors in startup logs
4. MCP server installed

### Tools Not Working

**Symptoms**: Tool calls fail

**Check:**
1. Tool name in `tool_filter`
2. Correct transport type
3. Valid authentication
4. Network connectivity (for SSE)
5. File permissions (for filesystem)

### Performance Issues

**Symptoms**: Slow responses

**Check:**
1. Network latency (for SSE)
2. Large file operations
3. Database query complexity
4. Increase timeout
5. Use tool filters

### Authentication Errors

**Symptoms**: 401/403 errors

**Check:**
1. Token environment variable set
2. Token not expired
3. Correct permissions
4. Valid endpoint URL

## Migration Guide

### From Direct Integrations

If you previously implemented direct integrations (e.g., GitHub API client), migrate to MCP:

**Before:**
```go
// Custom GitHub client
githubClient := github.NewClient(token)
repos, err := githubClient.ListRepos()
```

**After:**
```yaml
# config.yaml
mcp:
  enabled: true
  servers:
    - name: "github"
      transport_type: "sse"
      auth:
        type: "bearer"
        token_env: "GITHUB_PAT"
```

**Benefits:**
- Less code to maintain
- Standardized interface
- More tools available
- Easier to add new sources

### From Legacy Tools

Replace custom tools with MCP equivalents:

| Legacy Tool | MCP Equivalent | Server |
|-------------|----------------|--------|
| Custom file reader | `read_file` | Filesystem |
| GitHub API wrapper | `get_file_contents` | GitHub |
| Database query tool | `read_query` | SQLite/Postgres |
| Web scraper | `fetch_url` | Existing (keep) |

## Future Enhancements

Planned improvements:

1. **Dynamic Registration**: Add/remove MCP servers without restart
2. **Health Checks**: Periodic MCP server health monitoring
3. **Rate Limiting**: Per-server rate limits
4. **Caching**: Cache tool results (configurable)
5. **OAuth2 Refresh**: Automatic token refresh
6. **Custom Transports**: Support for custom transport types
7. **Tool Telemetry**: Enhanced metrics per tool
8. **Marketplace**: Pre-configured MCP server templates

## Resources

- **MCP Specification**: https://spec.modelcontextprotocol.io
- **Official Servers**: https://github.com/modelcontextprotocol/servers
- **MCP Go SDK**: https://github.com/modelcontextprotocol/go-sdk
- **ADK Documentation**: https://pkg.go.dev/google.golang.org/adk
- **Examples**: `examples/mcp/`

## Support

For issues:
1. Check logs: `grep MCP logs/agent.log`
2. Verify configuration: `cat config.yaml | grep -A 20 "^mcp:"`
3. Test MCP server directly: `npx @modelcontextprotocol/server-filesystem /tmp`
4. Report issues: GitHub issues with logs and config

---

**Next Steps:**
1. Start with filesystem MCP (simplest)
2. Test basic queries
3. Add more servers incrementally
4. Monitor usage in Langfuse
5. Optimize with tool filters

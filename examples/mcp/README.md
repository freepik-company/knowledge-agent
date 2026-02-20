# MCP Integration Examples

This directory contains example configurations for integrating MCP (Model Context Protocol) servers with the Knowledge Agent.

## Quick Start

### 1. Filesystem Access (Simplest)

```bash
# Install the filesystem MCP server
npm install -g @modelcontextprotocol/server-filesystem

# Create a test directory
mkdir -p /tmp/test-workspace
echo "Hello MCP!" > /tmp/test-workspace/hello.txt

# Copy the example config
cp examples/mcp/config-filesystem.yaml config.yaml

# Edit config.yaml to set your workspace path
# Change "/workspace" to "/tmp/test-workspace"

# Start the agent
make dev

# Test queries
curl -X POST http://localhost:8081/agent/run \
  -H "X-API-Key: your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"appName":"knowledge-agent","userId":"test","newMessage":{"role":"user","parts":[{"text":"What files are in the workspace?"}]}}'

curl -X POST http://localhost:8081/agent/run \
  -H "X-API-Key: your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"appName":"knowledge-agent","userId":"test","newMessage":{"role":"user","parts":[{"text":"Read hello.txt and tell me what it says"}]}}'
```

### 2. GitHub Integration

```bash
# Install GitHub MCP server
npm install -g @modelcontextprotocol/server-github

# Get a GitHub Personal Access Token (PAT)
# Go to: https://github.com/settings/tokens
# Create token with "repo" permissions

# Set environment variable
export GITHUB_PAT=ghp_your_token_here

# Copy and configure
cp examples/mcp/config-github.yaml config.yaml

# Start the agent
make dev

# Test queries
curl -X POST http://localhost:8081/agent/run \
  -H "X-API-Key: your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"appName":"knowledge-agent","userId":"test","newMessage":{"role":"user","parts":[{"text":"List my GitHub repositories"}]}}'
```

### 3. Multiple MCP Servers

```bash
# Install multiple servers
npm install -g @modelcontextprotocol/server-filesystem
npm install -g @modelcontextprotocol/server-sqlite

# Copy config
cp examples/mcp/config-multiple.yaml config.yaml

# Edit paths and enable/disable servers as needed

# Start the agent
make dev
```

## Available MCP Servers

### Official MCP Servers

1. **Filesystem** (`@modelcontextprotocol/server-filesystem`)
   - Read, write, list, search files
   - Local command transport
   - Best for: Documentation ingestion, file analysis

2. **GitHub** (`@modelcontextprotocol/server-github`)
   - Repository access, issues, PRs
   - SSE transport
   - Requires: GitHub PAT
   - Best for: Code review, issue tracking

3. **Google Drive** (`@modelcontextprotocol/server-gdrive`)
   - Document access, search
   - OAuth2 transport
   - Requires: Google OAuth credentials
   - Best for: Organizational knowledge

4. **SQLite** (`@modelcontextprotocol/server-sqlite`)
   - Database queries, schema inspection
   - Local command transport
   - Best for: Data analysis, reporting

5. **Postgres** (`@modelcontextprotocol/server-postgres`)
   - Database access, queries
   - Local command transport
   - Best for: Production data queries

6. **Memory** (`@modelcontextprotocol/server-memory`)
   - Knowledge graph storage
   - Local command transport
   - Best for: Long-term entity relationships

### Community MCP Servers

- **Brave Search**: Web search integration
- **Puppeteer**: Browser automation
- **Sentry**: Error tracking integration
- **Slack**: Workspace integration
- And many more at: https://github.com/modelcontextprotocol/servers

## Configuration Reference

### Transport Types

1. **Command** (`transport_type: "command"`)
   - Runs local executables via stdio
   - Best for: npm packages, local scripts
   - Example: filesystem, sqlite, git

2. **SSE** (`transport_type: "sse"`)
   - Server-Sent Events over HTTP
   - Best for: Remote MCP servers
   - Example: GitHub, Google APIs

3. **Streamable** (`transport_type: "streamable"`)
   - HTTP streaming protocol
   - Best for: Custom MCP servers
   - Example: Internal services

### Authentication

```yaml
auth:
  type: "bearer"           # or "basic", "oauth2"
  token_env: "API_TOKEN"   # Environment variable (recommended)
  # OR
  token: "secret"          # Direct token (not recommended)
```

### Tool Filtering

```yaml
tool_filter:
  - "read_file"    # Only allow specific tools
  - "list_directory"
  - "search_files"
# Empty = allow all tools
```

## Use Cases

### 1. Documentation Ingestion

**Scenario**: Ingest team documentation from Google Drive

```yaml
mcp:
  enabled: true
  servers:
    - name: "gdrive"
      transport_type: "sse"
      endpoint: "https://gdrive-mcp.example.com"
      auth:
        type: "oauth2"
        token_env: "GDRIVE_TOKEN"
```

**Query**: "@bot Read all documents in the 'Engineering' folder and save important procedures to memory"

### 2. Code Review Assistance

**Scenario**: Analyze pull requests and answer questions

```yaml
mcp:
  enabled: true
  servers:
    - name: "github"
      transport_type: "sse"
      auth:
        type: "bearer"
        token_env: "GITHUB_PAT"
```

**Query**: "@bot What are the open PRs in myorg/myrepo? Summarize the changes."

### 3. Local File Analysis

**Scenario**: Analyze project structure and save insights

```yaml
mcp:
  enabled: true
  servers:
    - name: "filesystem"
      transport_type: "command"
      command:
        path: "npx"
        args: ["-y", "@modelcontextprotocol/server-filesystem", "/project"]
```

**Query**: "@bot Analyze all .md files in /project/docs and save key architecture decisions"

### 4. Database Queries

**Scenario**: Query analytics database for insights

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

**Query**: "@bot What were the top 10 errors last week? Save the patterns to memory."

## Troubleshooting

### MCP Server Not Found

```
Error: failed to create MCP toolset: command not found
```

**Solution**: Install the MCP server globally

```bash
npm install -g @modelcontextprotocol/server-filesystem
# or use npx (downloads on demand)
```

### Permission Denied

```
Error: failed to create transport: permission denied
```

**Solution**: Check file permissions and authentication

```bash
# For filesystem
chmod +r /path/to/workspace

# For GitHub
echo $GITHUB_PAT  # Should not be empty
```

### Timeout Errors

```
Warning: MCP server timeout
```

**Solution**: Increase timeout in config

```yaml
timeout: 60  # seconds
```

### Tools Not Appearing

**Solution**: Check logs and tool filter

```bash
# Check agent logs
grep "MCP toolset" logs/agent.log

# Remove tool_filter to allow all tools
# tool_filter: []  # or omit entirely
```

## Security Best Practices

1. **Use Environment Variables**: Never commit tokens to config files
2. **Limit Tool Access**: Use `tool_filter` to restrict operations
3. **Restrict Paths**: For filesystem, use specific directories
4. **Rotate Tokens**: Periodically refresh API tokens
5. **Monitor Usage**: Check logs for unexpected tool calls
6. **Principle of Least Privilege**: Only enable needed MCP servers

## Performance Considerations

- **Startup Time**: Each MCP server adds ~1-2s to startup
- **Request Latency**: Command transport is faster than HTTP
- **Tool Discovery**: Cached after first connection
- **Graceful Degradation**: Failed servers don't block agent startup

## Next Steps

1. Start with `config-filesystem.yaml` for simplest setup
2. Test basic queries to verify MCP integration
3. Add more servers as needed (one at a time)
4. Use `tool_filter` to optimize performance
5. Monitor Langfuse for MCP tool usage analytics

## Resources

- MCP Specification: https://spec.modelcontextprotocol.io
- Official Servers: https://github.com/modelcontextprotocol/servers
- MCP Go SDK: https://github.com/modelcontextprotocol/go-sdk
- ADK Documentation: https://pkg.go.dev/google.golang.org/adk

## Support

For issues or questions:
- Knowledge Agent: https://github.com/yourusername/knowledge-agent/issues
- MCP Specification: https://github.com/modelcontextprotocol/specification/issues

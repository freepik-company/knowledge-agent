# Integration Tests

This directory contains integration tests for the Knowledge Agent system. These tests verify end-to-end functionality of new features and require a running system.

## Test Files

### 1. `username_test.go`
Tests user name integration with Slack:
- Verifies user names are properly included in query requests
- Tests queries with and without user name information
- Validates agent uses user names in responses

### 2. `binary_modes_test.go`
Tests the unified binary's different operational modes:
- **Mode: all** - Both services running (agent + slack-bot)
- **Mode: agent** - Only agent service
- **Mode: slack-bot** - Only Slack bridge service
- Graceful shutdown verification
- Port binding verification

### 3. `prompt_reload_test.go`
Tests the prompt manager and hot reload functionality:
- Hot reload when file changes (development mode)
- Loading from `base_prompt` configuration
- Loading from `template_path` file
- Priority: template_path > base_prompt > default
- Concurrent access safety
- Disabled hot reload behavior

### 4. `rate_limiting_test.go`
Tests rate limiting middleware:
- Basic rate limiting enforcement (10 req/s, burst 20)
- Per-IP rate limiting (independent limits)
- Burst capacity verification
- Rate limiter recovery over time
- Old IP entries cleanup

## Running Integration Tests

### Prerequisites

1. **Services Running**: Start infrastructure and agent:
   ```bash
   make docker-up
   make dev
   ```

2. **Configuration**: Ensure `config.yaml` or environment variables are set with valid credentials.

### Run All Integration Tests

```bash
make integration-test
```

Or directly with go:
```bash
go test -v -race -tags=integration ./tests/integration/...
```

### Run Specific Test

```bash
# User name integration
go test -v -tags=integration ./tests/integration/ -run TestUserName

# Binary modes
go test -v -tags=integration ./tests/integration/ -run TestBinaryMode

# Prompt reload
go test -v -tags=integration ./tests/integration/ -run TestPromptHotReload

# Rate limiting
go test -v -tags=integration ./tests/integration/ -run TestRateLimiting
```

### Run in Short Mode (Skip Long Tests)

Some tests are slow (binary startup, rate limit recovery). Skip them with:
```bash
go test -v -tags=integration -short ./tests/integration/...
```

## Test Configuration

Integration tests use the same configuration as the main application:
- Configuration loaded from `config.yaml` or environment variables
- Tests are skipped if no valid config is available
- Some tests require specific services (PostgreSQL, Redis, Ollama)

## Build Tags

All integration tests use the `integration` build tag to separate them from unit tests:
```go
// +build integration
```

This ensures:
- Unit tests run fast: `go test ./...`
- Integration tests run on demand: `go test -tags=integration ./...`

## CI/CD Integration

### Recommended CI Pipeline

```yaml
# Example GitHub Actions
test:
  runs-on: ubuntu-latest
  services:
    postgres:
      image: postgres:17
      env:
        POSTGRES_PASSWORD: postgres
      options: >-
        --health-cmd pg_isready
        --health-interval 10s
        --health-timeout 5s
        --health-retries 5
    redis:
      image: redis:7-alpine
      options: >-
        --health-cmd "redis-cli ping"
        --health-interval 10s
        --health-timeout 5s
        --health-retries 5

  steps:
    - uses: actions/checkout@v3
    - uses: actions/setup-go@v4
      with:
        go-version: '1.22'

    - name: Run Integration Tests
      run: |
        make integration-test
      env:
        ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
        POSTGRES_URL: postgresql://postgres:postgres@localhost:5432/knowledge_agent
        REDIS_ADDR: localhost:6379
```

## Troubleshooting

### Tests Fail with "no config available"
- Ensure `config.yaml` exists or environment variables are set
- Run `make setup` to create config from template

### Tests Fail with Connection Refused
- Ensure services are running: `make docker-up`
- Check service health: `make docker-health`
- Verify ports are not in use: `lsof -i :8080` and `lsof -i :8081`

### Rate Limiting Tests Fail
- Rate limiter state persists between tests
- Run tests individually if needed
- Wait a few seconds between test runs

### Binary Mode Tests Fail
- Binary must be built first: `make build`
- Ensure ports 8080 and 8081 are free
- Check for zombie processes: `ps aux | grep knowledge-agent`

### Prompt Reload Tests Fail
- File watcher (fsnotify) may need a few seconds to detect changes
- Tests use temporary directories that are cleaned up automatically
- Check file permissions if tests fail consistently

## Writing New Integration Tests

When adding new integration tests:

1. **Use Build Tag**:
   ```go
   // +build integration
   ```

2. **Skip if No Config**:
   ```go
   cfg, err := config.Load("")
   if err != nil {
       t.Skip("Skipping integration test: no config available")
   }
   ```

3. **Clean Up Resources**:
   ```go
   defer agent.Close()
   defer server.Close()
   ```

4. **Use Short Flag for Long Tests**:
   ```go
   if testing.Short() {
       t.Skip("Skipping long test in short mode")
   }
   ```

5. **Test Timeouts**:
   ```go
   ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
   defer cancel()
   ```

## Test Coverage

Current integration test coverage:
- ✅ User name fetching and usage
- ✅ Unified binary modes (all, agent, slack-bot)
- ✅ Graceful shutdown
- ✅ Prompt hot reload
- ✅ Prompt manager configuration priority
- ✅ Rate limiting (basic, per-IP, burst, recovery)
- ✅ Concurrent access safety

## Next Steps

Additional integration tests to consider:
- [ ] Metrics collection and exposure
- [ ] Health check dependencies (PostgreSQL, Redis, Ollama)
- [ ] A2A authentication flows
- [ ] Image processing and multimodal queries
- [ ] URL fetching and analysis
- [ ] Memory save and search operations

**Note on Langfuse:** The system now uses `github.com/git-hulk/langfuse-go v0.1.0` for observability. See `docs/OBSERVABILITY.md` for details. Integration tests for Langfuse tracing are planned.

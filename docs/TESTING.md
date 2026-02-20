# Testing Guide

This document describes the testing strategy and how to run tests for the Knowledge Agent project.

## Table of Contents

- [Test Structure](#test-structure)
- [Running Tests](#running-tests)
- [Unit Tests](#unit-tests)
- [Integration Tests](#integration-tests)
- [Test Coverage](#test-coverage)
- [Writing Tests](#writing-tests)
- [CI/CD Integration](#cicd-integration)

## Test Structure

```
knowledge-agent/
├── internal/
│   ├── agent/
│   │   ├── presearch_test.go    # Pre-search memory unit tests
│   │   └── session_test.go      # Session management tests
│   ├── config/
│   │   └── config_test.go       # Unit tests for config
│   ├── server/
│   │   ├── adk_middleware_test.go # ADK middleware helper tests
│   │   ├── agent_server_test.go  # Server and health endpoint tests
│   │   ├── middleware_test.go    # Auth middleware tests
│   │   └── ratelimit_test.go    # Rate limiter tests
│   ├── slack/
│   │   ├── formatter_test.go    # Unit tests for Slack formatter
│   │   └── verification_test.go # Unit tests for Slack verification
│   └── ...
└── tests/
    └── integration/
        ├── username_test.go         # User name integration tests
        ├── binary_modes_test.go     # Binary mode tests
        ├── prompt_reload_test.go    # Prompt reload tests
        ├── rate_limiting_test.go    # Rate limiting tests
        └── README.md                # Integration test documentation
```

## Running Tests

### All Tests

```bash
# Run all unit tests
make test

# Run all integration tests (requires services)
make integration-test

# Run all tests (unit + integration)
make test && make integration-test
```

### Unit Tests Only

```bash
# Run with coverage
go test -v -race -coverprofile=coverage.out ./...

# View coverage report
go tool cover -html=coverage.out

# Run specific package
go test -v ./internal/config/...
```

### Integration Tests

```bash
# All integration tests
make integration-test

# Short mode (skip long tests)
make integration-test-short

# Specific test suites
make integration-test-username   # User name tests
make integration-test-binary     # Binary mode tests
make integration-test-prompt     # Prompt reload tests
make integration-test-ratelimit  # Rate limiting tests

# Using the script
./scripts/run-integration-tests.sh all
./scripts/run-integration-tests.sh short
./scripts/run-integration-tests.sh username
```

## Unit Tests

Unit tests are fast, isolated tests that don't require external services.

### Location

Unit tests live alongside the code they test:
```
internal/config/config.go
internal/config/config_test.go
```

### Running Unit Tests

```bash
# All unit tests
go test ./...

# Specific package
go test ./internal/config/

# With race detector
go test -race ./...

# With coverage
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

### Example Unit Test

```go
package config

import (
    "testing"
)

func TestConfigValidation(t *testing.T) {
    tests := []struct {
        name    string
        config  Config
        wantErr bool
    }{
        {
            name: "valid config",
            config: Config{
                Anthropic: AnthropicConfig{APIKey: "test"},
                Slack:     SlackConfig{BotToken: "xoxb-test"},
                Postgres:  PostgresConfig{URL: "postgres://..."},
            },
            wantErr: false,
        },
        {
            name: "missing API key",
            config: Config{
                Slack:    SlackConfig{BotToken: "xoxb-test"},
                Postgres: PostgresConfig{URL: "postgres://..."},
            },
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := tt.config.Validate()
            if (err != nil) != tt.wantErr {
                t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}
```

## Integration Tests

Integration tests verify end-to-end functionality and require running services.

### Prerequisites

1. Start infrastructure:
   ```bash
   make docker-up
   ```

2. Migrations run automatically on startup (no manual step needed)

3. Start agent (for some tests):
   ```bash
   make dev
   ```

### Test Suites

#### 1. Username Integration Tests

Tests that user names are properly fetched and used:

```bash
make integration-test-username
```

Tests:
- Query with user name and real name
- Query without user names (fallback)
- User name included in agent instructions

#### 2. Binary Mode Tests

Tests unified binary operational modes:

```bash
make integration-test-binary
```

Tests:
- Mode: all (both services)
- Mode: agent (agent only)
- Mode: slack-bot (bridge only)
- Graceful shutdown
- Port binding verification

#### 3. Prompt Reload Tests

Tests prompt manager and hot reload:

```bash
make integration-test-prompt
```

Tests:
- Hot reload when file changes
- Loading from base_prompt
- Loading from template_path
- Configuration priority
- Concurrent access safety

#### 4. Rate Limiting Tests

Tests rate limiting middleware:

```bash
make integration-test-ratelimit
```

Tests:
- Basic rate limiting (10 req/s, burst 20)
- Per-IP rate limiting
- Burst capacity
- Rate limiter recovery
- Cleanup of old entries

### Skipping Long Tests

Some integration tests are slow (binary startup, rate limit recovery). Skip them with:

```bash
make integration-test-short
```

Or:
```bash
go test -tags=integration -short ./tests/integration/...
```

## Test Coverage

### Current Coverage

```bash
# Generate coverage report
make test

# View in browser
go tool cover -html=coverage.out
```

### Coverage by Package

| Package | Coverage | Status |
|---------|----------|--------|
| internal/config | ~85% | ✅ Good |
| internal/logger | ~80% | ✅ Good |
| internal/slack | ~70% | 🟡 Needs improvement |
| internal/agent | ~60% | 🟡 Needs improvement |
| internal/server | ~50% | 🔴 Low |

### Coverage Goals

- Critical packages (config, auth): > 80%
- Business logic (agent, slack): > 70%
- Utilities (logger, metrics): > 60%

## Writing Tests

### Unit Test Guidelines

1. **Test file naming**: `*_test.go`
2. **Test function naming**: `TestXxx` or `TestXxx_Yyy`
3. **Use table-driven tests** for multiple scenarios
4. **Mock external dependencies**
5. **Use testify/assert** for assertions (optional)
6. **Keep tests focused** - one concept per test

Example:
```go
func TestPromptManager_GetPrompt(t *testing.T) {
    tests := []struct {
        name   string
        config *PromptConfig
        want   string
    }{
        {
            name: "base prompt",
            config: &PromptConfig{BasePrompt: "test"},
            want: "test",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            m, err := NewManager(tt.config)
            if err != nil {
                t.Fatal(err)
            }
            defer m.Close()

            got := m.GetPrompt()
            if got != tt.want {
                t.Errorf("got %v, want %v", got, tt.want)
            }
        })
    }
}
```

### Integration Test Guidelines

1. **Use build tag**: `// +build integration`
2. **Skip if no config**: Check for required services
3. **Clean up resources**: Use `defer` for cleanup
4. **Use short flag**: For long-running tests
5. **Test timeouts**: Always use context with timeout
6. **Real dependencies**: Use actual services, not mocks

Example:
```go
// +build integration

package integration

import (
    "context"
    "testing"
    "time"
)

func TestFeature(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping long test in short mode")
    }

    cfg, err := config.Load("")
    if err != nil {
        t.Skip("Skipping: no config available")
    }

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    // Setup
    agent, err := agent.New(ctx, cfg)
    if err != nil {
        t.Fatalf("Setup failed: %v", err)
    }
    defer agent.Close()

    // Test
    result, err := agent.DoSomething(ctx)
    if err != nil {
        t.Errorf("DoSomething failed: %v", err)
    }

    // Verify
    if result != expected {
        t.Errorf("got %v, want %v", result, expected)
    }
}
```

## CI/CD Integration

### GitHub Actions Example

```yaml
name: Tests

on: [push, pull_request]

jobs:
  unit-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.24'

      - name: Run unit tests
        run: make test

  integration-tests:
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
        ports:
          - 5432:5432

      redis:
        image: redis:7-alpine
        options: >-
          --health-cmd "redis-cli ping"
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5
        ports:
          - 6379:6379

    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.24'

      - name: Run integration tests
        run: make integration-test
        env:
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
          POSTGRES_URL: postgresql://postgres:postgres@localhost:5432/knowledge_agent?sslmode=disable
          REDIS_ADDR: localhost:6379
```

### Pre-commit Hook

Add to `.git/hooks/pre-commit`:
```bash
#!/bin/bash
make test
if [ $? -ne 0 ]; then
    echo "Unit tests failed. Commit aborted."
    exit 1
fi
```

## Troubleshooting

### Tests Fail with "connection refused"

Services not running:
```bash
make docker-up
make docker-health
```

### Tests Fail with "no config available"

Create config file:
```bash
cp config-example.yaml config.yaml
# Edit with your credentials
```

### Integration Tests Timeout

Increase timeout or run in short mode:
```bash
make integration-test-short
```

### Rate Limiting Tests Fail

Rate limiter state persists. Wait a few seconds between runs:
```bash
sleep 5 && make integration-test-ratelimit
```

### Binary Mode Tests Fail

Ports already in use:
```bash
# Check for existing processes
lsof -i :8080
lsof -i :8081

# Kill if needed
kill -9 <PID>
```

## Best Practices

1. **Write tests first** (TDD) when possible
2. **Keep tests simple** and focused
3. **Use descriptive test names** that explain what is being tested
4. **Don't test external libraries** - trust them
5. **Mock external dependencies** in unit tests
6. **Use real services** in integration tests
7. **Always clean up** resources (defer)
8. **Use table-driven tests** for multiple scenarios
9. **Test error cases** not just happy paths
10. **Run tests before committing** (`make test`)

## Resources

- [Go Testing Package](https://pkg.go.dev/testing)
- [Table-Driven Tests in Go](https://go.dev/wiki/TableDrivenTests)
- [Testify Assert](https://github.com/stretchr/testify)
- [Integration Tests README](../tests/integration/README.md)

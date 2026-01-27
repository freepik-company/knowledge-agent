# Contributing to Knowledge Agent

Thank you for your interest in contributing to Knowledge Agent! We welcome contributions from the community.

## Code of Conduct

This project adheres to a code of conduct. By participating, you are expected to uphold this code. Please report unacceptable behavior to [your-email].

## How Can I Contribute?

### Reporting Bugs

Before creating bug reports, please check existing issues to avoid duplicates. When creating a bug report, include:

- **Clear title and description**
- **Steps to reproduce**
- **Expected vs actual behavior**
- **Environment details** (OS, Go version, Docker version)
- **Logs and error messages**

Use the [bug report template](.github/ISSUE_TEMPLATE/bug_report.md).

### Suggesting Features

Feature requests are welcome! Please:

1. **Check existing feature requests** to avoid duplicates
2. **Explain the problem** you're trying to solve
3. **Describe the proposed solution** in detail
4. **Consider alternatives** you've thought of

Use the [feature request template](.github/ISSUE_TEMPLATE/feature_request.md).

### Pull Requests

1. **Fork the repository** and create a branch from `main`
2. **Make your changes** with clear, descriptive commits
3. **Follow the coding style** (run `make fmt` and `make lint`)
4. **Add tests** for new functionality
5. **Update documentation** as needed
6. **Ensure tests pass** (`make test`)
7. **Submit a pull request**

#### Commit Message Convention

We follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: Add MCP integration for GitHub
fix: Resolve memory leak in session manager
docs: Update deployment guide
refactor: Simplify authentication middleware
test: Add integration tests for A2A API
chore: Update dependencies
```

This allows automatic changelog generation and semantic versioning.

**Types**:
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation only
- `refactor`: Code change that neither fixes a bug nor adds a feature
- `perf`: Performance improvement
- `test`: Adding or updating tests
- `chore`: Changes to build process or auxiliary tools
- `ci`: CI/CD changes

**Breaking Changes**: Add `!` after type or add `BREAKING:` in commit body:
```
feat!: Change config structure for MCP servers

BREAKING: Config format changed, see migration guide
```

## Development Setup

### Prerequisites

- Go 1.24+
- Docker & Docker Compose
- Make
- Git

### Setup

```bash
# Clone your fork
git clone https://github.com/YOUR_USERNAME/knowledge-agent.git
cd knowledge-agent

# Add upstream remote
git remote add upstream https://github.com/freepik-company/knowledge-agent.git

# Create config
cp config-example.yaml config.yaml
# Fill in your test credentials

# Start infrastructure
make docker-up

# Run migrations

# Run tests
make test

# Run locally
make dev
```

### Project Structure

```
knowledge-agent/
├── cmd/
│   └── knowledge-agent/    # Main application
├── internal/
│   ├── agent/              # Core agent logic (ADK integration)
│   ├── slack/              # Slack integration
│   ├── server/             # HTTP servers
│   ├── config/             # Configuration loading
│   ├── logger/             # Structured logging
│   ├── migrations/         # Database migrations
│   └── tools/              # Custom tools (webfetch)
├── deployments/            # Docker, Kubernetes configs
├── docs/                   # Documentation
├── examples/               # Example configs
├── migrations/             # SQL migration files (deprecated, see internal/migrations)
└── tests/                  # Integration tests
```

### Coding Standards

- **Go style**: Follow [Effective Go](https://golang.org/doc/effective_go.html)
- **Formatting**: Run `make fmt` before committing
- **Linting**: Run `make lint` (requires golangci-lint)
- **Comments**: Document exported functions, types, and packages
- **Error handling**: Always handle errors explicitly
- **Logging**: Use structured logging (`logger.Get()`)

### Testing

```bash
# Run unit tests
make test

# Run integration tests (requires services)
make integration-test

# Test specific package
go test ./internal/agent/...

# Run with coverage
go test -cover ./...
```

### Documentation

When adding features or changing behavior:

1. **Update relevant docs** in `docs/`
2. **Update README.md** if user-facing
3. **Add examples** in `examples/` if helpful
4. **Update CLAUDE.md** for development guidance

## Areas for Contribution

### High Priority

- 🐛 **Bug fixes** - Always welcome
- 📚 **Documentation improvements** - Clarifications, examples, translations
- ✅ **Tests** - Increase coverage, add edge cases

### Feature Ideas

- 🔌 **MCP server integrations** - New data sources (Notion, Jira, Confluence)
- 🌍 **Internationalization** - Better language handling
- 📊 **Analytics** - Usage statistics, insights
- 🔍 **Search improvements** - Better ranking, filters
- 🎨 **Slack UI enhancements** - Interactive messages, slash commands

### Before Starting Major Work

For significant changes:

1. **Open an issue** to discuss your idea
2. **Wait for maintainer feedback** before implementing
3. **Break into smaller PRs** if possible

This prevents wasted effort if the approach needs adjustment.

## Review Process

1. **Automated checks** must pass (tests, linting)
2. **Maintainer review** (usually within 48 hours)
3. **Address feedback** in the same PR
4. **Squash commits** if requested
5. **Merge** by maintainers

## Recognition

Contributors are recognized in:
- GitHub Contributors page
- Release notes for significant contributions
- Special thanks in documentation

## Questions?

- 💬 [Discussions](https://github.com/freepik-company/knowledge-agent/discussions) - Ask questions
- 📧 Email maintainers at [sre@freepik.com]
- 🐦 Twitter: [@freepik](https://twitter.com/freepik)

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).

---

Thank you for making Knowledge Agent better! 🙏

# Database Migrations & Entrypoint Enhancements

This document describes two critical enhancements to the Knowledge Agent deployment:

1. **Automatic config-driven npm package installation** in Docker entrypoint
2. **Application-managed database migrations** instead of PostgreSQL container auto-migration

---

## 1. Enhanced Docker Entrypoint with Config Parsing

### Problem Solved

Previously, users had to manually specify npm packages in environment variables or build args. This was redundant since the packages were already defined in `config.yaml` MCP server configurations.

### Solution

The entrypoint script now **automatically extracts npm packages from `config.yaml`** when using `npx` commands.

### How It Works

```yaml
# config.yaml
mcp:
  enabled: true
  servers:
    - name: "filesystem"
      command:
        path: "npx"
        args:
          - "-y"
          - "@modelcontextprotocol/server-filesystem"  # ← Auto-extracted!
          - "/tmp/mcp-workspace"
```

The entrypoint script:
1. Checks if `CONFIG_PATH` points to a valid config file
2. Parses YAML to find MCP servers with `path: "npx"`
3. Extracts package names from `args` (after `-y` flag)
4. Installs all extracted packages: `npm install -g <packages>`

### Three Installation Modes

**Mode 1: Auto-detection (Recommended)**
```yaml
# docker-compose.yml
volumes:
  - ../config.yaml:/app/config/config.yaml:ro
```
No env vars needed - packages extracted from config automatically.

**Mode 2: Manual env var**
```yaml
# docker-compose.yml
environment:
  MCP_NPM_PACKAGES: "@modelcontextprotocol/server-sqlite"
```
Useful for ad-hoc testing or packages not in config.

**Mode 3: Build-time installation**
```yaml
# docker-compose.yml
build:
  args:
    NPM_PACKAGES: "@modelcontextprotocol/server-filesystem"
```
Faster startup, but requires image rebuild to change packages.

### Implementation

**File**: `deployments/docker-entrypoint.sh`

Key function:
```sh
extract_npm_packages_from_config() {
    local config_file="$1"
    # Parses YAML to find:
    #   path: "npx"
    #   args:
    #     - "-y"
    #     - "@modelcontextprotocol/server-filesystem"
    # Returns: "@modelcontextprotocol/server-filesystem"
}
```

---

## 2. Application-Managed Database Migrations

### Problem Solved

Previously, migrations ran via PostgreSQL's `docker-entrypoint-initdb.d` mechanism:
- ❌ Only runs on first container start (empty volume)
- ❌ Requires mounting migration files separately
- ❌ No migration tracking or rollback
- ❌ Fails silently if migrations already applied

### Solution

Migrations are now embedded in the application and run at startup before the agent initializes.

### How It Works

```
Application Startup
  ↓
Load Configuration
  ↓
Run Database Migrations ← NEW
  ↓
Initialize Agent (ADK, memory service, etc.)
  ↓
Start HTTP Server
```

### Features

✅ **Embedded migrations** - Compiled into binary, no external files needed
✅ **Tracking table** - `schema_migrations` tracks applied migrations
✅ **Transactional** - Each migration runs in a transaction (atomic, rollback on error)
✅ **Idempotent** - Safe to run multiple times, skips already-applied migrations
✅ **Automatic** - Runs on every startup, applies only pending migrations
✅ **Ordered execution** - Migrations apply in version order (001, 002, 003...)

### Implementation

**Migration Runner**: `internal/migrations/runner.go`

```go
type Runner struct {
    db *sql.DB
}

// Run executes all pending migrations
func (r *Runner) Run(ctx context.Context) error {
    // 1. Create schema_migrations table (if not exists)
    // 2. Load embedded migration files
    // 3. Check which migrations already applied
    // 4. Execute pending migrations in transactions
    // 5. Record each migration in schema_migrations table
}
```

**Migration Files**: `internal/migrations/sql/`

```
001_init_pgvector.sql       - Create pgvector extension, memories table
002_slack_metadata.sql      - Add Slack-specific indexes and views
003_drop_unused_memories... - Clean up old tables
```

**Embedded with go:embed**:
```go
//go:embed sql/*.sql
var migrationFiles embed.FS
```

**Integration in main.go**:
```go
// Run database migrations (only for modes that need database access)
if mode == ModeAgent || mode == ModeAll {
    if err := runMigrations(ctx, cfg); err != nil {
        log.Fatalw("Failed to run database migrations", "error", err)
    }
}
```

### Migration Tracking

**Table**: `schema_migrations`

```sql
CREATE TABLE schema_migrations (
    version INTEGER PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    applied_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);
```

**View applied migrations**:
```bash
make db-shell
# In psql:
SELECT * FROM schema_migrations ORDER BY version;
```

**Example output**:
```
 version |          name           |         applied_at
---------+-------------------------+----------------------------
       1 | init_pgvector           | 2025-01-26 10:30:15+00
       2 | slack_metadata          | 2025-01-26 10:30:16+00
       3 | drop_unused_memories... | 2025-01-26 10:30:16+00
```

### Adding New Migrations

1. **Create migration file** in `internal/migrations/sql/`:
   ```
   004_add_tags_to_memories.sql
   ```

2. **Write SQL** (use `IF EXISTS` for safety):
   ```sql
   ALTER TABLE memory_entries
   ADD COLUMN IF NOT EXISTS tags TEXT[];

   CREATE INDEX IF NOT EXISTS idx_memory_entries_tags
   ON memory_entries USING GIN(tags);
   ```

3. **Rebuild and restart**:
   ```bash
   make build
   make dev
   ```

4. **Verify**:
   ```bash
   make db-shell
   SELECT * FROM schema_migrations WHERE version = 4;
   ```

### Migration Best Practices

1. **Always use IF EXISTS/IF NOT EXISTS** - Makes migrations idempotent
2. **Sequential version numbers** - 001, 002, 003 (not dates)
3. **Descriptive names** - `add_tags_to_memories`, not `update_schema`
4. **One logical change per migration** - Don't mix unrelated changes
5. **Test rollback** - Write down migration (optional but recommended)

### Logs

**Successful migration run**:
```
INFO  Starting database migrations...
INFO  Migrations loaded  count=3
DEBUG Migration already applied, skipping  version=1 name=init_pgvector
DEBUG Migration already applied, skipping  version=2 name=slack_metadata
DEBUG Migration already applied, skipping  version=3 name=drop_unused_memories_table
INFO  Database schema is up to date
```

**New migration applied**:
```
INFO  Starting database migrations...
INFO  Migrations loaded  count=4
INFO  Applying migration  version=4 name=add_tags_to_memories
INFO  Migration applied successfully  version=4 name=add_tags_to_memories duration_ms=45
INFO  Migrations completed successfully  executed=1
```

### Error Handling

**Migration failure**:
- Transaction rolls back (no partial application)
- Application fails to start (fail-fast principle)
- Logs show detailed error with version and SQL

**Example**:
```
ERROR Failed to run database migrations
  error=failed to execute migration 4_add_tags: syntax error at or near "TABL"
```

**Recovery**:
1. Fix SQL in migration file
2. Rebuild and restart
3. Migration re-attempts (transaction rolled back, not recorded)

---

## Docker Compose Changes

### Before

```yaml
postgres:
  volumes:
    - postgres_data:/var/lib/postgresql/data
    - ../migrations:/docker-entrypoint-initdb.d  # ← Removed
```

### After

```yaml
postgres:
  volumes:
    - postgres_data:/var/lib/postgresql/data
    # Migrations handled by application ✅
```

---

## Benefits Summary

### Entrypoint Enhancement
- ✅ **DRY principle** - Package list in one place (config.yaml)
- ✅ **Zero redundancy** - No duplicate env vars needed
- ✅ **Kubernetes-friendly** - ConfigMap changes auto-detected
- ✅ **Flexible** - Still supports manual env vars for overrides

### Application Migrations
- ✅ **Reliable** - Always runs, always tracked
- ✅ **Self-contained** - No external files needed
- ✅ **Safe** - Transactional, rollback on error
- ✅ **Auditable** - Full history in `schema_migrations` table
- ✅ **Portable** - Works in Docker, Kubernetes, bare metal

---

## Testing

### Test Entrypoint Package Extraction

```bash
# Create test config
cat > /tmp/test-config.yaml <<EOF
mcp:
  enabled: true
  servers:
    - name: "filesystem"
      command:
        path: "npx"
        args:
          - "-y"
          - "@modelcontextprotocol/server-filesystem"
          - "/tmp/test"
EOF

# Test extraction
docker run --rm \
  -v /tmp/test-config.yaml:/app/config/config.yaml:ro \
  -e CONFIG_PATH=/app/config/config.yaml \
  knowledge-agent:latest

# Should see:
# 🔍 Scanning config.yaml for npm packages...
#    Found packages: @modelcontextprotocol/server-filesystem
# 📦 Installing npm packages...
```

### Test Migrations

```bash
# Fresh database
docker volume rm knowledge-agent_postgres_data

# Start stack
make docker-stack

# Check logs
make docker-logs-agent | grep migration

# Should see:
# INFO  Starting database migrations...
# INFO  Migrations loaded  count=3
# INFO  Applying migration  version=1 name=init_pgvector
# INFO  Migration applied successfully  version=1
# ...
```

---

## Troubleshooting

### Entrypoint Issues

**Problem**: Packages not installing

**Check**:
```bash
docker exec knowledge-agent env | grep CONFIG_PATH
docker exec knowledge-agent cat /app/config/config.yaml | grep -A 10 "mcp:"
```

**Solution**: Ensure config.yaml is mounted and MCP servers use `path: "npx"`

### Migration Issues

**Problem**: "relation already exists" errors

**Cause**: Old migrations ran via PostgreSQL init scripts

**Solution**:
```sql
-- Manually create tracking table and mark migrations as applied
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    applied_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO schema_migrations (version, name) VALUES
    (1, 'init_pgvector'),
    (2, 'slack_metadata'),
    (3, 'drop_unused_memories_table');
```

**Problem**: Application won't start due to migration error

**Solution**:
1. Check logs for specific SQL error
2. Fix migration file
3. Rebuild and restart
4. If needed, manually rollback in psql and remove from schema_migrations

---

## Production Considerations

### pgvector Requirement

The first migration creates the `vector` extension:
```sql
CREATE EXTENSION IF NOT EXISTS vector;
```

**In development:** Works automatically (using `pgvector/pgvector:pg16` image)

**In production:** PostgreSQL must have pgvector installed BEFORE the app starts.

The application now **verifies pgvector availability** before running migrations. If pgvector is missing, it fails with detailed installation instructions for your platform (AWS RDS, Azure, self-hosted, etc.).

**See:** `docs/PRODUCTION_POSTGRESQL.md` for complete production setup guide

### Migration Behavior

When deployed to production with a fresh PostgreSQL instance:

1. App starts
2. Verifies pgvector extension available
3. Creates `schema_migrations` tracking table
4. Runs all migrations in order (001, 002, 003...)
5. Each migration recorded in `schema_migrations`
6. Starts agent service

**Important:** Ensure your database user has `CREATE` permission for the first deployment (to create extension and tables). After initial setup, can revoke if desired.

## References

- **Entrypoint**: `deployments/docker-entrypoint.sh`
- **Migrations**: `internal/migrations/runner.go`
- **Migration Files**: `internal/migrations/sql/*.sql`
- **Integration**: `cmd/knowledge-agent/main.go` (runMigrations function)
- **Production Setup**: `docs/PRODUCTION_POSTGRESQL.md`

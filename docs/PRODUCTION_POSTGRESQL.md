# PostgreSQL Production Setup

This guide explains how to prepare PostgreSQL for the Knowledge Agent in production environments.

## Critical Requirement: pgvector Extension

The Knowledge Agent **requires the pgvector extension** for semantic search. This must be installed on your PostgreSQL server before the application starts.

### Why pgvector?

The agent stores knowledge with **768-dimensional vector embeddings** for semantic similarity search. PostgreSQL with pgvector provides:
- Efficient vector storage (native `vector` data type)
- Fast similarity search (cosine distance with HNSW indexes)
- Integration with standard SQL queries

---

## Installation by Platform

### 🐳 Docker (Development/Testing)

**Use the official pgvector image:**

```yaml
# docker-compose.yml
postgres:
  image: pgvector/pgvector:pg16
  environment:
    POSTGRES_USER: postgres
    POSTGRES_PASSWORD: postgres
    POSTGRES_DB: knowledge_agent
```

**Advantages:**
- ✅ pgvector pre-installed
- ✅ No additional setup required
- ✅ Matches development environment

---

### ☁️ AWS RDS PostgreSQL

**Version requirement:** PostgreSQL 15.2+ or 16.1+

**Step 1: Enable pgvector in Parameter Group**

1. Go to **RDS Console** → **Parameter Groups**
2. Edit your parameter group or create a new one
3. Find `shared_preload_libraries` parameter
4. Add `vector` to the list:
   ```
   pg_stat_statements,vector
   ```
5. Save changes

**Step 2: Reboot RDS Instance**

The parameter change requires a reboot:
```bash
aws rds reboot-db-instance --db-instance-identifier your-instance-name
```

**Step 3: Create Extension in Database**

Connect as master user and run:
```sql
CREATE EXTENSION IF NOT EXISTS vector;
```

**Verification:**
```sql
SELECT * FROM pg_available_extensions WHERE name = 'vector';

-- Test vector operations
SELECT '[1,2,3]'::vector <-> '[4,5,6]'::vector AS distance;
```

**Terraform Example:**
```hcl
resource "aws_db_parameter_group" "knowledge_agent" {
  name   = "knowledge-agent-pg16"
  family = "postgres16"

  parameter {
    name  = "shared_preload_libraries"
    value = "pg_stat_statements,vector"
  }
}

resource "aws_db_instance" "knowledge_agent" {
  identifier          = "knowledge-agent-db"
  engine              = "postgres"
  engine_version      = "16.1"
  instance_class      = "db.t3.medium"
  parameter_group_name = aws_db_parameter_group.knowledge_agent.name

  # ... other settings
}
```

**References:**
- [AWS RDS User Guide](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/Appendix.PostgreSQL.CommonDBATasks.Extensions.html)
- [pgvector AWS Blog](https://aws.amazon.com/blogs/database/building-ai-powered-search-in-postgresql-using-amazon-sagemaker-and-pgvector/)

---

### ☁️ Azure Database for PostgreSQL

**Version requirement:** PostgreSQL 11+

**Step 1: Enable pgvector in Server Parameters**

1. Go to **Azure Portal** → **Azure Database for PostgreSQL**
2. Select your server → **Server parameters**
3. Search for `azure.extensions`
4. Add `VECTOR` to allowed extensions
5. Search for `shared_preload_libraries`
6. Add `vector` to the list
7. Click **Save**

**Step 2: Restart Server**

Parameters require server restart:
- Click **Restart** button in Azure Portal
- Or via Azure CLI:
  ```bash
  az postgres server restart \
    --resource-group myResourceGroup \
    --name myserver
  ```

**Step 3: Create Extension in Database**

Connect to your database and run:
```sql
CREATE EXTENSION IF NOT EXISTS vector;
```

**Azure CLI Example:**
```bash
# Update server parameters
az postgres server configuration set \
  --resource-group myResourceGroup \
  --server-name myserver \
  --name azure.extensions \
  --value VECTOR,pg_stat_statements

az postgres server configuration set \
  --resource-group myResourceGroup \
  --server-name myserver \
  --name shared_preload_libraries \
  --value vector,pg_stat_statements

# Restart
az postgres server restart \
  --resource-group myResourceGroup \
  --name myserver
```

**References:**
- [Azure PostgreSQL Extensions](https://learn.microsoft.com/en-us/azure/postgresql/flexible-server/concepts-extensions)

---

### ☁️ Google Cloud SQL PostgreSQL

**Status:** ❌ **pgvector NOT YET SUPPORTED**

Google Cloud SQL for PostgreSQL does not currently support the pgvector extension.

**Workarounds:**
1. Use **Cloud SQL for PostgreSQL + Manual VM** with pgvector
2. Use **Google Cloud Run** with **Supabase** (has pgvector)
3. Use **Google Kubernetes Engine (GKE)** with self-hosted PostgreSQL + pgvector

**Alternative:** Consider **Vertex AI Vector Search** for embeddings and Cloud SQL for metadata (requires architecture changes).

---

### 🔧 Self-Hosted PostgreSQL

**Requirements:**
- PostgreSQL 12+ (recommended: 15 or 16)
- Build tools (gcc, make, postgresql-dev packages)

#### Ubuntu/Debian

```bash
# Install PostgreSQL 16
sudo apt update
sudo apt install -y postgresql-16 postgresql-server-dev-16

# Install pgvector
sudo apt install -y postgresql-16-pgvector

# Or from source:
cd /tmp
git clone --branch v0.5.1 https://github.com/pgvector/pgvector.git
cd pgvector
make
sudo make install

# Restart PostgreSQL
sudo systemctl restart postgresql
```

#### RHEL/CentOS/Fedora

```bash
# Install PostgreSQL 16
sudo dnf install -y postgresql16-server postgresql16-devel

# Initialize database (first time only)
sudo /usr/pgsql-16/bin/postgresql-16-setup initdb

# Install pgvector
sudo dnf install -y pgvector_16

# Or from source:
cd /tmp
git clone --branch v0.5.1 https://github.com/pgvector/pgvector.git
cd pgvector
make
sudo make install

# Start PostgreSQL
sudo systemctl enable postgresql-16
sudo systemctl start postgresql-16
```

#### macOS (Homebrew)

```bash
# Install PostgreSQL
brew install postgresql@16

# Install pgvector
brew install pgvector

# Start PostgreSQL
brew services start postgresql@16
```

#### From Source (All Platforms)

```bash
# Install prerequisites
# - PostgreSQL development headers (postgresql-server-dev)
# - gcc, make, git

# Clone and build pgvector
git clone --branch v0.5.1 https://github.com/pgvector/pgvector.git
cd pgvector
make
sudo make install

# Verify installation
ls /usr/lib/postgresql/*/lib/vector.so
ls /usr/share/postgresql/*/extension/vector*
```

#### Enable Extension

```sql
-- Connect to your database
psql -U postgres -d knowledge_agent

-- Create extension
CREATE EXTENSION IF NOT EXISTS vector;

-- Verify
SELECT * FROM pg_extension WHERE extname = 'vector';
```

---

### ☁️ Other Managed Services

#### Supabase
**Status:** ✅ **pgvector ENABLED BY DEFAULT**

No setup required - pgvector is pre-installed.

#### Heroku Postgres
**Status:** ✅ **Available on Standard plans and above**

```bash
# Enable extension (automatic on Standard+)
heroku pg:psql -a your-app
CREATE EXTENSION vector;
```

#### Render PostgreSQL
**Status:** ✅ **Available**

pgvector is pre-installed. Create extension in your database:
```sql
CREATE EXTENSION vector;
```

#### DigitalOcean Managed PostgreSQL
**Status:** ✅ **Available on PostgreSQL 15+**

Connect to your database and run:
```sql
CREATE EXTENSION vector;
```

#### Railway
**Status:** ✅ **Available**

pgvector is pre-installed. Create extension:
```sql
CREATE EXTENSION vector;
```

---

## Application Behavior

### Automatic Verification

When the Knowledge Agent starts, it **automatically verifies pgvector availability**:

```
INFO  Starting database migrations...
DEBUG Verifying pgvector extension availability...
INFO  pgvector extension verified successfully
```

### If pgvector is Missing

The application will **fail to start** with a detailed error message:

```
ERROR Failed to run database migrations
  error=pgvector extension not found

The Knowledge Agent requires the pgvector extension for semantic search.

Installation instructions by platform:
[... detailed instructions ...]
```

This **fail-fast** approach ensures:
- ✅ Clear error messages with actionable instructions
- ✅ No silent failures or degraded functionality
- ✅ Prevents data corruption from missing vector support

---

## Verification Checklist

Before deploying the Knowledge Agent to production:

- [ ] PostgreSQL version is 12+ (recommended: 15 or 16)
- [ ] pgvector extension is installed on the server
- [ ] `shared_preload_libraries` includes `vector` (if required by platform)
- [ ] PostgreSQL server restarted after configuration changes
- [ ] Extension created in target database: `CREATE EXTENSION vector;`
- [ ] Database user has sufficient permissions (SUPERUSER or CREATE on database)
- [ ] Test vector operations work:
  ```sql
  SELECT '[1,2,3]'::vector <-> '[4,5,6]'::vector AS distance;
  ```

---

## Troubleshooting

### Error: "could not open extension control file"

**Cause:** pgvector is not installed on the PostgreSQL server.

**Solution:** Install pgvector following platform-specific instructions above.

### Error: "permission denied to create extension"

**Cause:** Database user lacks CREATE privileges.

**Solution:** Grant privileges:
```sql
-- As superuser
GRANT CREATE ON DATABASE knowledge_agent TO your_user;

-- Or make user superuser (not recommended for production)
ALTER USER your_user WITH SUPERUSER;
```

### Error: "extension "vector" is not available"

**Causes:**
1. pgvector not installed
2. `shared_preload_libraries` not configured
3. PostgreSQL not restarted after configuration

**Solution:**
1. Install pgvector
2. Add `vector` to `shared_preload_libraries`
3. Restart PostgreSQL
4. Run `CREATE EXTENSION vector;`

### Verification Commands

```sql
-- Check if extension is available
SELECT * FROM pg_available_extensions WHERE name = 'vector';

-- Check if extension is installed
SELECT * FROM pg_extension WHERE extname = 'vector';

-- Test vector operations
SELECT '[1,2,3]'::vector <-> '[4,5,6]'::vector;

-- Check shared_preload_libraries
SHOW shared_preload_libraries;
```

---

## Performance Tuning

Once pgvector is installed, consider these optimizations:

### Index Configuration

```sql
-- HNSW index (fast queries, slower inserts)
CREATE INDEX ON memory_entries
USING hnsw (embedding vector_cosine_ops)
WITH (m = 16, ef_construction = 64);

-- Adjust parameters:
-- m: max connections per layer (16-32, higher = better recall, more memory)
-- ef_construction: search depth (64-128, higher = better index quality)
```

### PostgreSQL Configuration

```ini
# postgresql.conf

# Memory
shared_buffers = 4GB          # 25% of RAM
effective_cache_size = 12GB   # 75% of RAM
work_mem = 64MB               # For vector operations

# Connections
max_connections = 100

# Vector-specific (if needed)
max_parallel_workers_per_gather = 4
```

### Monitoring

```sql
-- Check index usage
SELECT schemaname, tablename, indexname, idx_scan, idx_tup_read, idx_tup_fetch
FROM pg_stat_user_indexes
WHERE indexname LIKE '%embedding%';

-- Check table size
SELECT pg_size_pretty(pg_total_relation_size('memory_entries'));

-- Check vector index size
SELECT pg_size_pretty(pg_relation_size('idx_memory_entries_embedding'));
```

---

## Migration Path

### From Development to Production

1. **Export development data** (optional):
   ```bash
   pg_dump -h localhost -U postgres -d knowledge_agent > backup.sql
   ```

2. **Setup production PostgreSQL** with pgvector (following guides above)

3. **Configure connection string** in production:
   ```yaml
   # config.yaml or environment variables
   postgres:
     url: postgres://user:pass@prod-db.example.com:5432/knowledge_agent?sslmode=require
   ```

4. **Deploy application** - migrations run automatically on first start

5. **Import data** (if needed):
   ```bash
   psql -h prod-db.example.com -U user -d knowledge_agent < backup.sql
   ```

### Zero-Downtime Migration

For existing production systems:

1. Install pgvector on production database
2. Create extension during maintenance window
3. Deploy new version with migrations enabled
4. Migrations run automatically, no manual SQL needed

---

## Security Considerations

### Connection Security

```yaml
# Always use SSL in production
postgres:
  url: postgres://user:pass@db.example.com:5432/knowledge_agent?sslmode=require
```

### User Permissions

```sql
-- Create dedicated user (recommended)
CREATE USER knowledge_agent WITH PASSWORD 'secure_password';

-- Grant minimal required permissions
GRANT CONNECT ON DATABASE knowledge_agent TO knowledge_agent;
GRANT USAGE ON SCHEMA public TO knowledge_agent;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO knowledge_agent;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO knowledge_agent;

-- Grant CREATE for migrations (first deploy only)
GRANT CREATE ON DATABASE knowledge_agent TO knowledge_agent;
-- Revoke after initial migration:
-- REVOKE CREATE ON DATABASE knowledge_agent FROM knowledge_agent;
```

### Extension Creation Privilege

The user needs one-time SUPERUSER or CREATE ON DATABASE to create the pgvector extension:

**Option 1: Temporary SUPERUSER**
```sql
-- Before first deploy
ALTER USER knowledge_agent WITH SUPERUSER;

-- After extension created, revoke
ALTER USER knowledge_agent WITH NOSUPERUSER;
```

**Option 2: DBA creates extension**
```sql
-- DBA runs before deployment
CREATE EXTENSION IF NOT EXISTS vector;

-- Application user only needs table permissions
```

---

## Cost Optimization

### AWS RDS Sizing

For **1M vectors** (768 dimensions each):
- **Storage:** ~3 GB (vectors) + ~1 GB (metadata, indexes) = **4 GB**
- **Recommended:** db.t3.medium (2 vCPU, 4 GB RAM) - $73/month
- **High traffic:** db.r6g.large (2 vCPU, 16 GB RAM) - $146/month

### Azure Database Sizing

For **1M vectors**:
- **Recommended:** General Purpose, 2 vCores, 8 GB RAM - ~$150/month
- **High traffic:** Memory Optimized, 4 vCores, 32 GB RAM - ~$400/month

### Self-Hosted Sizing

For **1M vectors**:
- **Minimum:** 4 GB RAM, 2 CPU cores, 20 GB SSD
- **Recommended:** 8 GB RAM, 4 CPU cores, 50 GB SSD
- **High traffic:** 16 GB RAM, 8 CPU cores, 100 GB NVMe SSD

---

## References

- **pgvector GitHub:** https://github.com/pgvector/pgvector
- **pgvector Documentation:** https://github.com/pgvector/pgvector#readme
- **PostgreSQL Extensions:** https://www.postgresql.org/docs/current/extend-extensions.html
- **HNSW Algorithm:** https://arxiv.org/abs/1603.09320

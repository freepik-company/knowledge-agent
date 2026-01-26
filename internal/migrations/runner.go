package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strings"
	"time"

	"knowledge-agent/internal/logger"
)

//go:embed sql/*.sql
var migrationFiles embed.FS

// Runner manages database migrations
type Runner struct {
	db *sql.DB
}

// Migration represents a single migration file
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// NewRunner creates a new migration runner
func NewRunner(db *sql.DB) *Runner {
	return &Runner{
		db: db,
	}
}

// Run executes all pending migrations
func (r *Runner) Run(ctx context.Context) error {
	log := logger.Get()
	// Verify pgvector extension is available
	if err := r.verifyPgvectorAvailable(ctx); err != nil {
		return err // Error already formatted with instructions
	}

	// Create migrations tracking table
	if err := r.createMigrationsTable(ctx); err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// Load migration files
	migrations, err := r.loadMigrations()
	if err != nil {
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	if len(migrations) == 0 {
		log.Warn("No migration files found")
		return nil
	}

	// Get applied migrations
	applied, err := r.getAppliedMigrations(ctx)
	if err != nil {
		return fmt.Errorf("failed to get applied migrations: %w", err)
	}

	// Execute pending migrations
	executed := 0
	for _, migration := range migrations {
		if _, ok := applied[migration.Version]; ok {
			log.Debugw("Skipping applied migration",
				"version", migration.Version)
			continue
		}

		if err := r.executeMigration(ctx, migration); err != nil {
			return fmt.Errorf("migration %d_%s failed: %w",
				migration.Version, migration.Name, err)
		}

		executed++
	}

	if executed == 0 {
		log.Info("✅ Database schema up to date")
	} else {
		log.Infow("✅ Applied migrations", "count", executed)
	}

	return nil
}

// verifyPgvectorAvailable checks if pgvector extension is available
func (r *Runner) verifyPgvectorAvailable(ctx context.Context) error {
	log := logger.Get()
	log.Debug("Verifying pgvector extension...")

	// Try to create the extension
	_, err := r.db.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS vector;")
	if err != nil {
		errMsg := err.Error()

		// Check for common pgvector missing errors
		if strings.Contains(errMsg, "could not access file") ||
			strings.Contains(errMsg, "could not open extension control file") ||
			strings.Contains(errMsg, "extension \"vector\" is not available") {

			log.Error("❌ pgvector extension not installed on PostgreSQL server")

			// Short, actionable error message
			return fmt.Errorf(`pgvector extension required but not found

Quick fix by platform:

🐳 Docker: Use pgvector image
   image: pgvector/pgvector:pg16

☁️  AWS RDS: Install via Parameter Group
   shared_preload_libraries = 'vector'
   (requires reboot)

☁️  Azure: Enable in Server Parameters
   azure.extensions = 'VECTOR'
   (requires restart)

🔧 Self-hosted:
   apt install postgresql-16-pgvector  # Ubuntu/Debian
   yum install pgvector_16              # RHEL/CentOS

📖 Full guide: docs/PRODUCTION_POSTGRESQL.md

Error: %s`, errMsg)
		}

		// Permission error
		if strings.Contains(errMsg, "permission denied") {
			return fmt.Errorf("database user lacks permission to create extensions\n\nRun as superuser: GRANT CREATE ON DATABASE knowledge_agent TO your_user;\n\nError: %s", errMsg)
		}

		// Other unknown error
		return fmt.Errorf("failed to verify pgvector: %w", err)
	}

	log.Info("✅ pgvector extension ready")
	return nil
}

// createMigrationsTable creates the migrations tracking table
func (r *Runner) createMigrationsTable(ctx context.Context) error {
	query := `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			applied_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_schema_migrations_applied_at
		ON schema_migrations(applied_at DESC);
	`

	_, err := r.db.ExecContext(ctx, query)
	return err
}

// loadMigrations loads all migration files from embedded filesystem
func (r *Runner) loadMigrations() ([]Migration, error) {
	log := logger.Get()
	entries, err := migrationFiles.ReadDir("sql")
	if err != nil {
		return nil, err
	}

	var migrations []Migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		// Parse filename: 001_init_pgvector.sql
		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) != 2 {
			log.Warnw("Skipping migration file with invalid name format",
				"filename", entry.Name())
			continue
		}

		var version int
		if _, err := fmt.Sscanf(parts[0], "%d", &version); err != nil {
			log.Warnw("Skipping migration file with invalid version number",
				"filename", entry.Name(),
				"error", err)
			continue
		}

		// Read SQL content
		content, err := migrationFiles.ReadFile("sql/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("failed to read migration file %s: %w", entry.Name(), err)
		}

		// Extract name (without version and extension)
		name := strings.TrimSuffix(parts[1], ".sql")

		migrations = append(migrations, Migration{
			Version: version,
			Name:    name,
			SQL:     string(content),
		})
	}

	// Sort by version
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	return migrations, nil
}

// getAppliedMigrations returns a set of applied migration versions
func (r *Runner) getAppliedMigrations(ctx context.Context) (map[int]bool, error) {
	query := "SELECT version FROM schema_migrations ORDER BY version"
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[int]bool)
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		applied[version] = true
	}

	return applied, rows.Err()
}

// executeMigration executes a single migration within a transaction
func (r *Runner) executeMigration(ctx context.Context, migration Migration) error {
	log := logger.Get()

	start := time.Now()

	// Start transaction
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Rollback if not committed

	// Execute migration SQL
	if _, err := tx.ExecContext(ctx, migration.SQL); err != nil {
		return fmt.Errorf("failed to execute SQL: %w", err)
	}

	// Record migration
	recordQuery := `
		INSERT INTO schema_migrations (version, name)
		VALUES ($1, $2)
	`
	if _, err := tx.ExecContext(ctx, recordQuery, migration.Version, migration.Name); err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	duration := time.Since(start)
	log.Infow("  ↳ Applied migration",
		"version", migration.Version,
		"duration_ms", duration.Milliseconds())

	return nil
}

// GetAppliedVersions returns all applied migration versions
func (r *Runner) GetAppliedVersions(ctx context.Context) ([]int, error) {
	applied, err := r.getAppliedMigrations(ctx)
	if err != nil {
		return nil, err
	}

	versions := make([]int, 0, len(applied))
	for version := range applied {
		versions = append(versions, version)
	}
	sort.Ints(versions)

	return versions, nil
}

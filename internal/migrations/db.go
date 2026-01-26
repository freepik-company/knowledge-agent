package migrations

import (
	"database/sql"
	"fmt"
	"net/url"

	_ "github.com/lib/pq" // PostgreSQL driver
)

// OpenDB opens a database connection for migrations
func OpenDB(connString string) (*sql.DB, error) {
	// Parse connection string to validate
	if _, err := url.Parse(connString); err != nil {
		return nil, fmt.Errorf("invalid connection string: %w", err)
	}

	// Open database connection
	db, err := sql.Open("postgres", connString)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}

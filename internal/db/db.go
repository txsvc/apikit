package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// DB wraps a *sql.DB connection to a SQLite database.
type DB struct {
	SqlDB *sql.DB
}

// Open validates path, creates the parent directory if needed,
// opens the SQLite file, and initializes the database schema.
func Open(path string) (*DB, error) {
	// Input validation — before any filesystem access.
	if path == "" {
		return nil, errors.New("db: path must not be empty")
	}

	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		return nil, fmt.Errorf("db: path %q is a directory, not a file", path)
	}

	// Create parent directory if needed.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}

	// Open the SQLite connection.
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	// Initialize: pool, WAL, foreign keys, schema.
	if err := initDB(sqlDB, false); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("db: failed to open database at %q: %w", path, err)
	}

	return &DB{SqlDB: sqlDB}, nil
}

// OpenMemory opens an in-memory SQLite database with full initialization
// (skipping WAL mode). Each call returns an independent isolated instance.
func OpenMemory() (*DB, error) {
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}

	if err := initDB(sqlDB, true); err != nil {
		sqlDB.Close()
		return nil, err
	}

	return &DB{SqlDB: sqlDB}, nil
}

// initDB applies all post-connection initialization shared by Open and
// OpenMemory: pool settings, WAL mode (file databases only), foreign key
// PRAGMA, and schema creation.
func initDB(sqlDB *sql.DB, skipWAL bool) error {
	// Step 1: Connection pool — single connection for SQLite.
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	// Step 2: WAL mode (file databases only).
	if !skipWAL {
		var mode string
		if err := sqlDB.QueryRow("PRAGMA journal_mode=WAL").Scan(&mode); err != nil {
			return err
		}
		if mode != "wal" {
			return fmt.Errorf("db: failed to enable WAL mode: journal_mode is %q", mode)
		}
	}

	// Step 3: Foreign key enforcement.
	if _, err := sqlDB.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return err
	}

	// Step 4: Schema creation in a single DEFERRED transaction.
	tx, err := sqlDB.Begin()
	if err != nil {
		return err
	}
	for _, stmt := range schemaStatements {
		if _, err := tx.Exec(stmt); err != nil {
			tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

// Close closes the underlying *sql.DB connection.
func (d *DB) Close() error {
	return d.SqlDB.Close()
}

// Ping checks that the database connection is alive.
func (d *DB) Ping(ctx context.Context) error {
	return d.SqlDB.PingContext(ctx)
}

// WithTx begins a DEFERRED transaction, calls fn with the transaction handle,
// commits if fn returns nil, and rolls back if fn returns a non-nil error.
// If fn returns an error and rollback also fails, the rollback error is
// silently discarded and the original error from fn is returned.
func (d *DB) WithTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := d.SqlDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	if fnErr := fn(tx); fnErr != nil {
		_ = tx.Rollback()
		return fnErr
	}

	return tx.Commit()
}

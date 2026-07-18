package db

import (
	"context"
	"database/sql"

	_ "modernc.org/sqlite"
)

// DB wraps a *sql.DB connection to a SQLite database.
type DB struct {
	SqlDB *sql.DB
}

// Open validates path, creates the parent directory if needed,
// opens the SQLite file, and initializes the database schema.
func Open(path string) (*DB, error) {
	return nil, nil
}

// OpenMemory opens an in-memory SQLite database with full initialization
// (skipping WAL mode). Each call returns an independent isolated instance.
func OpenMemory() (*DB, error) {
	return nil, nil
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
	return nil
}

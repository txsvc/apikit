// Package db provides SQLite database management for apikit.
package db

import (
	"context"
	"database/sql"
	"errors"
)

// ErrNotFound is the sentinel error returned when a queried row does not exist.
var ErrNotFound = errors.New("not found")

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

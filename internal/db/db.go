// Package db provides SQLite database management for apikit.
package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Sentinel errors for database operations.
var (
	// ErrNotFound is returned by callers when a query produces no rows.
	ErrNotFound = errors.New("db: not found")
	// ErrConflict is returned when an INSERT or UPDATE violates a UNIQUE or PRIMARY KEY constraint.
	ErrConflict = errors.New("db: conflict")
	// ErrDatabaseLocked is returned when SQLite returns SQLITE_BUSY or SQLITE_LOCKED.
	ErrDatabaseLocked = errors.New("db: database locked")
)

// TimeFormat is the canonical timestamp format for all database timestamp strings.
const TimeFormat = "2006-01-02T15:04:05Z"

// sqliteErrorCode is the interface used by WrapError to extract SQLite error
// codes without depending on the concrete *sqlite.Error type. This allows
// WrapError to work with both the actual driver error and synthetic test errors.
type sqliteErrorCode interface {
	error
	Code() int
}

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

// WrapError maps raw SQLite error codes to sentinel errors.
// It returns nil if err is nil, returns the appropriate sentinel for known
// SQLite error codes, and returns err unchanged for unknown error codes.
func WrapError(err error) error {
	return err
}

// FormatTime truncates t to whole-second precision, converts to UTC,
// and formats using TimeFormat.
func FormatTime(t time.Time) string {
	return ""
}

// ParseTime parses the input string using TimeFormat and returns the
// resulting time.Time in UTC.
func ParseTime(s string) (time.Time, error) {
	return time.Time{}, nil
}

// Package db provides SQLite database management for apikit.
package db

import (
	"errors"

	sqlite3 "modernc.org/sqlite/lib"
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

// sqliteErrorCode is the interface used by WrapError to extract SQLite error
// codes without depending on the concrete *sqlite.Error type. This allows
// WrapError to work with both the actual driver error and synthetic test errors.
type sqliteErrorCode interface {
	error
	Code() int
}

// WrapError maps raw SQLite error codes to sentinel errors.
// It returns nil if err is nil, returns the appropriate sentinel for known
// SQLite error codes, and returns err unchanged for unknown error codes.
// WrapError is a pure function: it inspects only the error code and performs
// no I/O, opens no database connections, and has no side effects.
func WrapError(err error) error {
	if err == nil {
		return nil
	}

	var sqlErr sqliteErrorCode
	if errors.As(err, &sqlErr) {
		switch sqlErr.Code() {
		case sqlite3.SQLITE_CONSTRAINT_UNIQUE, sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY, sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY:
			return ErrConflict
		case sqlite3.SQLITE_BUSY, sqlite3.SQLITE_LOCKED:
			return ErrDatabaseLocked
		}
	}

	return err
}

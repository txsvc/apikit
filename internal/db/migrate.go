package db

import (
	"database/sql"
	"io"
)

// MigrateEmailIndex creates the idx_users_email unique index on the
// users.email column. It is called during server startup after initDB.
//
// Behavior:
//  1. Attempts CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users(email).
//  2. If the statement fails because duplicate email values exist, queries for
//     all offending email addresses and writes a FATAL log message listing each
//     address and its row count to logWriter, then returns a non-nil error.
//  3. Never automatically deduplicates or modifies user rows.
//
// The logWriter receives diagnostic output including FATAL messages. Callers
// pass os.Stderr in production; tests pass a bytes.Buffer.
func MigrateEmailIndex(sqlDB *sql.DB, logWriter io.Writer) error {
	// Stub — implementation in task group 4.
	return nil
}

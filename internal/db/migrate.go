package db

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
)

// MigrateEmailIndex creates the idx_users_email unique index on the
// users.email column. It is called during server startup after initDB.
//
// Behavior:
//  1. Checks for duplicate email values in the users table.
//  2. If duplicates exist, writes a FATAL log message listing each offending
//     email address and its row count to logWriter, then returns a non-nil error.
//     No rows are modified or deleted — the operator must resolve duplicates manually.
//  3. If no duplicates exist, executes CREATE UNIQUE INDEX IF NOT EXISTS
//     idx_users_email ON users(email).
//
// The logWriter receives diagnostic output including FATAL messages. Callers
// pass os.Stderr in production; tests pass a bytes.Buffer.
func MigrateEmailIndex(sqlDB *sql.DB, logWriter io.Writer) error {
	// Step 1: Check for duplicate email values before attempting the index.
	duplicates, err := findDuplicateEmails(sqlDB)
	if err != nil {
		fmt.Fprintf(logWriter, "FATAL migration failed: %v\n", err)
		return fmt.Errorf("migration: failed to check for duplicate emails: %w", err)
	}

	// Step 2: If duplicates found, log FATAL and return error without modifying data.
	if len(duplicates) > 0 {
		var parts []string
		for _, d := range duplicates {
			parts = append(parts, fmt.Sprintf("%s (%d rows)", d.email, d.count))
		}
		msg := fmt.Sprintf(
			"FATAL migration failed: duplicate emails detected — deduplicate before restarting: %s",
			strings.Join(parts, ", "),
		)
		fmt.Fprintln(logWriter, msg)
		return errors.New(msg)
	}

	// Step 3: No duplicates — create the unique index.
	_, err = sqlDB.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users(email)")
	if err != nil {
		fmt.Fprintf(logWriter, "FATAL migration failed: %v\n", err)
		return fmt.Errorf("migration: failed to create idx_users_email index: %w", err)
	}

	return nil
}

// emailDuplicate holds an email address and the number of rows sharing it.
type emailDuplicate struct {
	email string
	count int
}

// findDuplicateEmails queries the users table for email addresses that appear
// more than once and returns them with their row counts.
func findDuplicateEmails(sqlDB *sql.DB) ([]emailDuplicate, error) {
	rows, err := sqlDB.Query(
		"SELECT email, COUNT(*) FROM users GROUP BY email HAVING COUNT(*) > 1",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var duplicates []emailDuplicate
	for rows.Next() {
		var d emailDuplicate
		if err := rows.Scan(&d.email, &d.count); err != nil {
			return nil, err
		}
		duplicates = append(duplicates, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return duplicates, nil
}

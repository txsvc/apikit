package db

import (
	"bytes"
	"strings"
	"testing"
)

// ========================================================================
// Spec 16 Task 3.1: Migration unit tests for idx_users_email unique index
// Test Spec: TS-16-19, TS-16-20, TS-16-21, TS-16-38, TS-16-39, TS-16-40
// Requirements: 16-REQ-6, 16-REQ-13
// ========================================================================

// insertTestUserForMigration is a test helper that inserts a user row with
// the minimum required columns. Each call must use a unique (id, username,
// provider, provider_id) combination; email may be duplicated to test
// migration failure scenarios.
func insertTestUserForMigration(t *testing.T, database *DB, id, username, email, provider, providerID string) {
	t.Helper()
	_, err := database.SqlDB.Exec(
		`INSERT INTO users (id, username, email, provider, provider_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`,
		id, username, email, provider, providerID,
	)
	if err != nil {
		t.Fatalf("failed to insert test user %s: %v", id, err)
	}
}

// dropEmailIndex drops the idx_users_email index to simulate a pre-migration
// database state. This is needed because schema.go now includes the index for
// new databases, but migration tests must verify behavior on databases that
// were created before the index existed.
func dropEmailIndex(t *testing.T, database *DB) {
	t.Helper()
	_, err := database.SqlDB.Exec("DROP INDEX IF EXISTS idx_users_email")
	if err != nil {
		t.Fatalf("failed to drop idx_users_email index: %v", err)
	}
}

// TestMigrateEmailIndex_CleanSchema verifies that the migration succeeds on
// a clean schema with no existing users, and that the idx_users_email unique
// index is created on the users.email column.
//
// Test Spec: TS-16-19, TS-16-38
// Requirement: 16-REQ-6.1, 16-REQ-13.1
func TestMigrateEmailIndex_CleanSchema(t *testing.T) {
	database, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	defer database.Close()

	var logBuf bytes.Buffer
	err = MigrateEmailIndex(database.SqlDB, &logBuf)
	if err != nil {
		t.Fatalf("MigrateEmailIndex on clean schema returned error = %v; want nil", err)
	}

	// Verify that the idx_users_email index exists on the users table.
	var indexName string
	queryErr := database.SqlDB.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='users' AND name='idx_users_email'`,
	).Scan(&indexName)
	if queryErr != nil {
		t.Fatalf("idx_users_email index not found after migration: %v", queryErr)
	}
	if indexName != "idx_users_email" {
		t.Errorf("index name = %q; want %q", indexName, "idx_users_email")
	}

	// Verify the index is UNIQUE by checking that the index entry has
	// unique=1 in the index_list pragma.
	rows, err := database.SqlDB.Query("PRAGMA index_list('users')")
	if err != nil {
		t.Fatalf("PRAGMA index_list('users') error = %v", err)
	}
	defer rows.Close()

	var foundUnique bool
	for rows.Next() {
		var seq int
		var name string
		var unique int
		var origin string
		var partial int
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			t.Fatalf("failed to scan index list: %v", err)
		}
		if name == "idx_users_email" {
			foundUnique = true
			if unique != 1 {
				t.Errorf("idx_users_email unique = %d; want 1 (unique)", unique)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows iteration error: %v", err)
	}
	if !foundUnique {
		t.Error("idx_users_email not found in PRAGMA index_list('users')")
	}

	// No FATAL messages should appear in log output.
	if logBuf.Len() > 0 {
		logStr := logBuf.String()
		if strings.Contains(logStr, "FATAL") {
			t.Errorf("unexpected FATAL log output on clean schema: %s", logStr)
		}
	}
}

// TestMigrateEmailIndex_DuplicateEmails verifies that the migration returns
// a non-nil error and logs a FATAL message listing all duplicate email
// addresses when the users table contains duplicate email values.
//
// Test Spec: TS-16-20, TS-16-39
// Requirement: 16-REQ-6.2, 16-REQ-13.2
func TestMigrateEmailIndex_DuplicateEmails(t *testing.T) {
	database, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	defer database.Close()

	// Drop the index to simulate a pre-migration database state.
	dropEmailIndex(t, database)

	// Insert users with duplicate email addresses.
	insertTestUserForMigration(t, database, "user-1", "alice1", "alice@example.com", "github", "gh-1")
	insertTestUserForMigration(t, database, "user-2", "alice2", "alice@example.com", "github", "gh-2")
	insertTestUserForMigration(t, database, "user-3", "bob1", "bob@example.com", "github", "gh-3")
	insertTestUserForMigration(t, database, "user-4", "bob2", "bob@example.com", "github", "gh-4")

	var logBuf bytes.Buffer
	err = MigrateEmailIndex(database.SqlDB, &logBuf)

	// Migration must return a non-nil error.
	if err == nil {
		t.Fatal("MigrateEmailIndex with duplicate emails returned nil error; want non-nil")
	}

	// Log output must contain the FATAL message prefix.
	logStr := logBuf.String()
	if !strings.Contains(logStr, "FATAL migration failed: duplicate emails detected") {
		t.Errorf("log output missing FATAL prefix; got:\n%s", logStr)
	}

	// Log output must list each offending email with its row count.
	if !strings.Contains(logStr, "alice@example.com (2 rows)") {
		t.Errorf("log output missing 'alice@example.com (2 rows)'; got:\n%s", logStr)
	}
	if !strings.Contains(logStr, "bob@example.com (2 rows)") {
		t.Errorf("log output missing 'bob@example.com (2 rows)'; got:\n%s", logStr)
	}
}

// TestMigrateEmailIndex_NoAutoDedup verifies that after a migration failure
// due to duplicate emails, all original rows remain unchanged in the users
// table — the migration never automatically deduplicates or modifies user
// rows.
//
// Test Spec: TS-16-21
// Requirement: 16-REQ-6.3
func TestMigrateEmailIndex_NoAutoDedup(t *testing.T) {
	database, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	defer database.Close()

	// Drop the index to simulate a pre-migration database state.
	dropEmailIndex(t, database)

	// Insert users with duplicate email addresses.
	insertTestUserForMigration(t, database, "user-1", "alice1", "alice@example.com", "github", "gh-1")
	insertTestUserForMigration(t, database, "user-2", "alice2", "alice@example.com", "github", "gh-2")

	var logBuf bytes.Buffer
	_ = MigrateEmailIndex(database.SqlDB, &logBuf)

	// After migration failure, both duplicate rows must still exist.
	var rowCount int
	err = database.SqlDB.QueryRow(
		"SELECT COUNT(*) FROM users WHERE email = ?", "alice@example.com",
	).Scan(&rowCount)
	if err != nil {
		t.Fatalf("failed to count users: %v", err)
	}
	if rowCount != 2 {
		t.Errorf("row count for 'alice@example.com' = %d; want 2 (migration should not auto-deduplicate)", rowCount)
	}
}

// TestMigrateEmailIndex_UniqueEmails verifies that the migration succeeds
// when all existing email values are unique, producing no false positives.
// The idx_users_email index is created and no error is returned.
//
// Test Spec: TS-16-40
// Requirement: 16-REQ-13.3
func TestMigrateEmailIndex_UniqueEmails(t *testing.T) {
	database, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	defer database.Close()

	// Insert users with unique email addresses.
	insertTestUserForMigration(t, database, "user-1", "alice", "alice@example.com", "github", "gh-1")
	insertTestUserForMigration(t, database, "user-2", "bob", "bob@example.com", "github", "gh-2")
	insertTestUserForMigration(t, database, "user-3", "carol", "carol@example.com", "github", "gh-3")

	var logBuf bytes.Buffer
	err = MigrateEmailIndex(database.SqlDB, &logBuf)
	if err != nil {
		t.Fatalf("MigrateEmailIndex with unique emails returned error = %v; want nil", err)
	}

	// Verify that the idx_users_email index exists.
	var indexName string
	queryErr := database.SqlDB.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='users' AND name='idx_users_email'`,
	).Scan(&indexName)
	if queryErr != nil {
		t.Fatalf("idx_users_email index not found after migration: %v", queryErr)
	}

	// No FATAL messages should appear in log output.
	logStr := logBuf.String()
	if strings.Contains(logStr, "FATAL") {
		t.Errorf("unexpected FATAL log output with unique emails: %s", logStr)
	}
}

// TestMigrateEmailIndex_Idempotent verifies that running the migration
// multiple times on a clean database succeeds each time (IF NOT EXISTS).
//
// Correctness Property: 16-PROP-5
// Requirement: 16-REQ-6.E2
func TestMigrateEmailIndex_Idempotent(t *testing.T) {
	database, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	defer database.Close()

	// Insert a user with a unique email to ensure the table has data.
	insertTestUserForMigration(t, database, "user-1", "alice", "alice@example.com", "github", "gh-1")

	// Run migration twice — both calls must succeed.
	var logBuf bytes.Buffer
	if err := MigrateEmailIndex(database.SqlDB, &logBuf); err != nil {
		t.Fatalf("first MigrateEmailIndex returned error = %v; want nil", err)
	}

	logBuf.Reset()
	if err := MigrateEmailIndex(database.SqlDB, &logBuf); err != nil {
		t.Fatalf("second MigrateEmailIndex returned error = %v; want nil", err)
	}
}

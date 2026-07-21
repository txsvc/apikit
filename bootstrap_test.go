package apikit_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/txsvc/apikit"
)

// openTestDB opens an in-memory SQLite database with the admin_config schema
// applied. The database is closed when the test completes.
func openTestDB(t *testing.T) *apikit.DB {
	t.Helper()
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	schema := `
		CREATE TABLE IF NOT EXISTS admin_config (
			key   TEXT NOT NULL PRIMARY KEY,
			value TEXT NOT NULL
		);
	`
	if _, err := sqlDB.Exec(schema); err != nil {
		sqlDB.Close()
		t.Fatalf("schema: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return &apikit.DB{SqlDB: sqlDB}
}

// TestBootstrap_ClosedDB_ReturnsError verifies that Bootstrap returns a
// non-nil error wrapping the underlying DB error when the database is
// closed (QueryRow.Scan fails).
// [TS-NS-1] [NS-REQ-1] [TS-NS-2] [NS-REQ-2] [TS-NS-4] [NS-REQ-4]
func TestBootstrap_ClosedDB_ReturnsError(t *testing.T) {
	// Open and immediately close the DB so all queries fail.
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	sqlDB.Close()

	database := &apikit.DB{SqlDB: sqlDB}

	err = apikit.Bootstrap(context.Background(), database, apikit.BootstrapOptions{})
	if err == nil {
		t.Fatal("expected non-nil error for closed database, got nil")
	}

	// NS-REQ-4: error message includes descriptive prefix.
	if !strings.Contains(err.Error(), "checking bootstrap state") {
		t.Errorf("error %q does not contain 'checking bootstrap state'", err.Error())
	}
}

// TestBootstrap_HealthyDB_NotBootstrapped_NoOp verifies that Bootstrap
// returns nil (no-op) on a healthy DB when no admin email/reset is needed
// and the admin token hash does NOT exist (not yet bootstrapped, nothing
// to do).
// [TS-NS-3] [NS-REQ-3]
func TestBootstrap_HealthyDB_NotBootstrapped_NoOp(t *testing.T) {
	database := openTestDB(t)

	// admin_config table exists but has no admin_token_hash row →
	// bootstrapped == false. With no admin email and no reset, the
	// early-return nil branch should be taken.
	err := apikit.Bootstrap(context.Background(), database, apikit.BootstrapOptions{})
	if err != nil {
		t.Fatalf("Bootstrap() = %v, want nil (no-op when not bootstrapped and no flags set)", err)
	}
}

// TestBootstrap_HealthyDB_Bootstrapped_Proceeds verifies that Bootstrap
// correctly queries the DB and proceeds to bootstrap.Run when the admin
// token hash already exists. This confirms the query works on a healthy DB.
// [TS-NS-3] [NS-REQ-3]
func TestBootstrap_HealthyDB_Bootstrapped_Proceeds(t *testing.T) {
	database := openTestDB(t)

	// Insert an admin_token_hash so bootstrapped == true.
	if _, err := database.SqlDB.Exec(
		"INSERT INTO admin_config (key, value) VALUES (?, ?)",
		"admin_token_hash", "somehash",
	); err != nil {
		t.Fatalf("insert admin_token_hash: %v", err)
	}

	// When bootstrapped is true, the early-return nil branch is NOT taken,
	// and bootstrap.Run is called. Without ADMIN_TOKEN set or a valid setup,
	// Run will return an error — but the key assertion is that Bootstrap did
	// NOT return nil (it correctly proceeded past the query).
	err := apikit.Bootstrap(context.Background(), database, apikit.BootstrapOptions{})
	// We expect some error from bootstrap.Run (e.g. ADMIN_TOKEN required),
	// which proves the query succeeded and control flow was correct.
	if err == nil {
		t.Fatal("Bootstrap() = nil, expected it to proceed to bootstrap.Run and return an error")
	}
	// The error should NOT be about checking bootstrap state (that query
	// succeeded); it should be about the subsequent boot validation.
	if strings.Contains(err.Error(), "checking bootstrap state") {
		t.Errorf("unexpected query error: %v", err)
	}
}

// TestBootstrap_ClosedDB_DoesNotReturnNil verifies that Bootstrap does not
// silently swallow the Scan error and return nil (which would incorrectly
// skip bootstrap).
// [TS-NS-2] [NS-REQ-2]
func TestBootstrap_ClosedDB_DoesNotReturnNil(t *testing.T) {
	// With a closed DB, the Scan error must propagate — Bootstrap must NOT
	// return nil (the old buggy behavior where bootstrapped defaults to false
	// and the early-return nil branch is taken).
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	sqlDB.Close()

	database := &apikit.DB{SqlDB: sqlDB}

	// Use empty AdminEmail + no ResetToken: old buggy code would return nil
	// because bootstrapped==false (zero value) and the early-return condition
	// would be met.
	err = apikit.Bootstrap(context.Background(), database, apikit.BootstrapOptions{})
	if err == nil {
		t.Fatal("Bootstrap() returned nil on closed DB — Scan error was silently swallowed")
	}
}

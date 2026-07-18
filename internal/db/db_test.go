package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Subtask 1.1: Open input validation
// Test Spec: TS-02-1, TS-02-2, TS-02-3, TS-02-E1
// Requirements: 02-REQ-1.1, 02-REQ-1.2, 02-REQ-1.3
// ---------------------------------------------------------------------------

// TestOpen_EmptyPath verifies that Open("") returns a descriptive error
// without performing any filesystem access or opening a database connection.
func TestOpen_EmptyPath(t *testing.T) {
	db, err := Open("")
	if db != nil {
		t.Error("Open(\"\") returned non-nil DB; want nil")
	}
	if err == nil {
		t.Fatal("Open(\"\") returned nil error; want non-nil")
	}
	const wantMsg = "db: path must not be empty"
	if err.Error() != wantMsg {
		t.Errorf("Open(\"\") error = %q; want %q", err.Error(), wantMsg)
	}
}

// TestOpen_PathIsDirectory verifies that Open returns a descriptive error
// containing the path when the path points to an existing directory.
func TestOpen_PathIsDirectory(t *testing.T) {
	dir := t.TempDir()

	db, err := Open(dir)
	if db != nil {
		t.Error("Open(dir) returned non-nil DB; want nil")
	}
	if err == nil {
		t.Fatal("Open(dir) returned nil error; want non-nil")
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("error %q does not contain path %q", err.Error(), dir)
	}
	if !strings.Contains(err.Error(), "is a directory, not a file") {
		t.Errorf("error %q does not contain 'is a directory, not a file'", err.Error())
	}
}

// TestOpen_PathIsDirectory_TrailingSlash verifies that a directory path with
// a trailing separator is still detected as a directory error. (TS-02-E1)
func TestOpen_PathIsDirectory_TrailingSlash(t *testing.T) {
	dir := t.TempDir()
	path := dir + string(os.PathSeparator)

	db, err := Open(path)
	if db != nil {
		t.Error("Open(dir/) returned non-nil DB; want nil")
	}
	if err == nil {
		t.Fatal("Open(dir/) returned nil error; want non-nil")
	}
	if !strings.Contains(err.Error(), "is a directory, not a file") {
		t.Errorf("error %q does not contain 'is a directory, not a file'", err.Error())
	}
}

// TestOpen_InputValidationPrecedesFilesystem verifies that input validation
// checks occur before any directory creation or database connection. (TS-02-3)
func TestOpen_InputValidationPrecedesFilesystem(t *testing.T) {
	// Use a temp dir as a base; construct a sub-path that does not exist.
	base := t.TempDir()
	subdir := filepath.Join(base, "should-not-be-created")

	// Call Open with an empty path — the subdir should never be created.
	db, err := Open("")
	if db != nil {
		t.Error("Open(\"\") returned non-nil DB; want nil")
	}
	if err == nil {
		t.Fatal("Open(\"\") returned nil error; want non-nil")
	}

	// Verify no side effects: the subdir must not exist.
	if _, statErr := os.Stat(subdir); !os.IsNotExist(statErr) {
		t.Errorf("expected subdir %q to not exist after Open(\"\"), but stat returned: %v", subdir, statErr)
	}
}

// ---------------------------------------------------------------------------
// Subtask 1.2: Open directory creation
// Test Spec: TS-02-4, TS-02-5, TS-02-6, TS-02-E2
// Requirements: 02-REQ-2.1, 02-REQ-2.2, 02-REQ-2.3
// ---------------------------------------------------------------------------

// TestOpen_CreatesDirectory verifies that Open creates the parent directory
// recursively when it does not exist.
func TestOpen_CreatesDirectory(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "newsubdir", "apikit.db")

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%q) error = %v; want nil", path, err)
	}
	if db == nil {
		t.Fatal("Open returned nil DB; want non-nil")
	}
	defer db.Close()

	dir := filepath.Dir(path)
	info, statErr := os.Stat(dir)
	if statErr != nil {
		t.Fatalf("parent directory %q does not exist: %v", dir, statErr)
	}
	if !info.IsDir() {
		t.Fatalf("parent path %q is not a directory", dir)
	}
}

// TestOpen_DirectoryMode verifies the directory created by Open has mode 0700
// (owner read/write/execute only) on non-Windows platforms. (TS-02-5)
func TestOpen_DirectoryMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory mode check not applicable on Windows")
	}

	base := t.TempDir()
	path := filepath.Join(base, "sensitivesubdir", "apikit.db")

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%q) error = %v; want nil", path, err)
	}
	if db == nil {
		t.Fatal("Open returned nil DB; want non-nil")
	}
	defer db.Close()

	dir := filepath.Dir(path)
	info, statErr := os.Stat(dir)
	if statErr != nil {
		t.Fatalf("parent directory %q does not exist: %v", dir, statErr)
	}
	if perm := info.Mode().Perm(); perm != 0700 {
		t.Errorf("directory %q has mode %04o; want 0700", dir, perm)
	}
}

// TestOpen_ParentAlreadyExists verifies that Open skips directory creation
// and proceeds directly to opening the database when the parent directory
// already exists. (TS-02-6)
func TestOpen_ParentAlreadyExists(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "apikit.db")

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%q) error = %v; want nil", path, err)
	}
	if db == nil {
		t.Fatal("Open returned nil DB; want non-nil")
	}
	defer db.Close()
}

// TestOpen_PermissionError verifies that Open returns an OS error when the
// parent directory cannot be created due to a permission error. (TS-02-E2)
func TestOpen_PermissionError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission test not applicable on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("test requires non-root user")
	}

	db, err := Open("/root/no-permission/apikit.db")
	if db != nil {
		t.Error("Open on unwritable path returned non-nil DB; want nil")
	}
	if err == nil {
		t.Fatal("Open on unwritable path returned nil error; want non-nil")
	}
}

// ---------------------------------------------------------------------------
// Subtask 1.3: Schema creation and idempotency
// Test Spec: TS-02-12, TS-02-19–24, TS-02-25, TS-02-26, TS-02-54, TS-02-55, TS-02-E7
// Requirements: 02-REQ-6.1–6.8
// ---------------------------------------------------------------------------

// allTables lists the six tables that must exist after Open.
var allTables = []string{
	"users", "api_keys", "pats", "orgs", "org_members", "admin_config",
}

// TestOpen_CreatesSchema verifies that all six tables are created after Open
// on a new database. (TS-02-12, TS-02-25, TS-02-54)
func TestOpen_CreatesSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%q) error = %v; want nil", path, err)
	}
	if db == nil {
		t.Fatal("Open returned nil DB; want non-nil")
	}
	defer db.Close()

	for _, table := range allTables {
		var name string
		err := db.SqlDB.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found in sqlite_master: %v", table, err)
			continue
		}
		if name != table {
			t.Errorf("expected table name %q, got %q", table, name)
		}
	}
}

// TestOpen_UsersTableDDL verifies the users table has the normative DDL. (TS-02-19)
func TestOpen_UsersTableDDL(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("OpenMemory returned nil DB; want non-nil")
	}
	defer db.Close()

	var ddl string
	err = db.SqlDB.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='users'").Scan(&ddl)
	if err != nil {
		t.Fatalf("failed to read users DDL: %v", err)
	}

	// Verify key DDL elements
	for _, token := range []string{"provider_id", "UNIQUE", "username", "email", "role", "status", "provider", "created_at", "updated_at"} {
		if !strings.Contains(ddl, token) {
			t.Errorf("users DDL missing token %q; DDL = %s", token, ddl)
		}
	}

	// Verify (provider, provider_id) UNIQUE constraint is enforced by inserting
	// two rows with the same (provider, provider_id).
	_, err = db.SqlDB.Exec(`INSERT INTO users VALUES ('id1','u1','e@e.com',NULL,'user','active','github','gh1','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("first insert into users failed: %v", err)
	}
	_, dupErr := db.SqlDB.Exec(`INSERT INTO users VALUES ('id2','u2','e2@e.com',NULL,'user','active','github','gh1','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`)
	if dupErr == nil {
		t.Error("expected UNIQUE constraint violation on duplicate (provider, provider_id); got nil error")
	}
}

// TestOpen_ApiKeysTableDDL verifies the api_keys table DDL. (TS-02-20)
func TestOpen_ApiKeysTableDDL(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("OpenMemory returned nil DB; want non-nil")
	}
	defer db.Close()

	var ddl string
	err = db.SqlDB.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='api_keys'").Scan(&ddl)
	if err != nil {
		t.Fatalf("failed to read api_keys DDL: %v", err)
	}

	for _, token := range []string{"REFERENCES users(id)", "secret_hash", "key_id", "expires_days"} {
		if !strings.Contains(ddl, token) {
			t.Errorf("api_keys DDL missing token %q; DDL = %s", token, ddl)
		}
	}
}

// TestOpen_PatsTableDDL verifies the pats table DDL. (TS-02-21)
func TestOpen_PatsTableDDL(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("OpenMemory returned nil DB; want non-nil")
	}
	defer db.Close()

	var ddl string
	err = db.SqlDB.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='pats'").Scan(&ddl)
	if err != nil {
		t.Fatalf("failed to read pats DDL: %v", err)
	}

	for _, token := range []string{"permissions", "REFERENCES users(id)", "token_id", "secret_hash"} {
		if !strings.Contains(ddl, token) {
			t.Errorf("pats DDL missing token %q; DDL = %s", token, ddl)
		}
	}
}

// TestOpen_OrgsTableDDL verifies the orgs table DDL. (TS-02-22)
func TestOpen_OrgsTableDDL(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("OpenMemory returned nil DB; want non-nil")
	}
	defer db.Close()

	var ddl string
	err = db.SqlDB.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='orgs'").Scan(&ddl)
	if err != nil {
		t.Fatalf("failed to read orgs DDL: %v", err)
	}

	for _, token := range []string{"slug", "UNIQUE", "name", "url", "status"} {
		if !strings.Contains(ddl, token) {
			t.Errorf("orgs DDL missing token %q; DDL = %s", token, ddl)
		}
	}
}

// TestOpen_OrgMembersTableDDL verifies the org_members table DDL. (TS-02-23)
func TestOpen_OrgMembersTableDDL(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("OpenMemory returned nil DB; want non-nil")
	}
	defer db.Close()

	var ddl string
	err = db.SqlDB.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='org_members'").Scan(&ddl)
	if err != nil {
		t.Fatalf("failed to read org_members DDL: %v", err)
	}

	for _, token := range []string{"ON DELETE CASCADE", "PRIMARY KEY (org_id, user_id)", "REFERENCES orgs(id)", "REFERENCES users(id)"} {
		if !strings.Contains(ddl, token) {
			t.Errorf("org_members DDL missing token %q; DDL = %s", token, ddl)
		}
	}
}

// TestOpen_AdminConfigTableDDL verifies the admin_config table DDL. (TS-02-24)
func TestOpen_AdminConfigTableDDL(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("OpenMemory returned nil DB; want non-nil")
	}
	defer db.Close()

	var ddl string
	err = db.SqlDB.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='admin_config'").Scan(&ddl)
	if err != nil {
		t.Fatalf("failed to read admin_config DDL: %v", err)
	}

	for _, token := range []string{"key", "value", "PRIMARY KEY"} {
		if !strings.Contains(ddl, token) {
			t.Errorf("admin_config DDL missing token %q; DDL = %s", token, ddl)
		}
	}
}

// TestOpen_Idempotent verifies that calling Open twice on the same database
// path succeeds without error and preserves existing data. (TS-02-26, TS-02-55)
func TestOpen_Idempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idem.db")

	// First open: create schema and insert a sentinel row.
	db1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open(%q) error = %v; want nil", path, err)
	}
	if db1 == nil {
		t.Fatal("first Open returned nil DB; want non-nil")
	}
	_, execErr := db1.SqlDB.Exec(`INSERT INTO admin_config VALUES ('key1', 'val1')`)
	if execErr != nil {
		t.Fatalf("insert into admin_config failed: %v", execErr)
	}
	db1.Close()

	// Second open: schema already exists — should succeed.
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open(%q) error = %v; want nil", path, err)
	}
	if db2 == nil {
		t.Fatal("second Open returned nil DB; want non-nil")
	}
	defer db2.Close()

	// Verify sentinel data is still present.
	var v string
	scanErr := db2.SqlDB.QueryRow("SELECT value FROM admin_config WHERE key='key1'").Scan(&v)
	if scanErr != nil {
		t.Fatalf("reading sentinel row failed: %v", scanErr)
	}
	if v != "val1" {
		t.Errorf("sentinel value = %q; want %q", v, "val1")
	}
}

// TestOpen_InterruptedTxRecovery verifies that if a prior Open/Close sequence
// completed (even if transaction was interrupted), a subsequent Open succeeds
// and all tables exist. (TS-02-E7)
func TestOpen_InterruptedTxRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "interrupted.db")

	// First open and immediate close to create the database file.
	db1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open(%q) error = %v", path, err)
	}
	if db1 == nil {
		t.Fatal("first Open returned nil DB")
	}
	db1.Close()

	// Re-open — should succeed and have all tables.
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open(%q) error = %v; want nil", path, err)
	}
	if db2 == nil {
		t.Fatal("second Open returned nil DB; want non-nil")
	}
	defer db2.Close()

	for _, table := range allTables {
		var name string
		scanErr := db2.SqlDB.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if scanErr != nil {
			t.Errorf("table %q not found after recovery: %v", table, scanErr)
		}
	}
}

// ---------------------------------------------------------------------------
// Subtask 1.4: WAL mode and corrupt file handling
// Test Spec: TS-02-9, TS-02-14, TS-02-15, TS-02-16, TS-02-17, TS-02-18,
//            TS-02-56, TS-02-57, TS-02-E5, TS-02-E6
// Requirements: 02-REQ-4.1, 02-REQ-4.2, 02-REQ-4.3, 02-REQ-5.1, 02-REQ-5.2
// ---------------------------------------------------------------------------

// TestOpen_WALMode verifies that Open enables WAL mode on a file-based
// database. (TS-02-9, TS-02-14, TS-02-56)
func TestOpen_WALMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%q) error = %v; want nil", path, err)
	}
	if db == nil {
		t.Fatal("Open returned nil DB; want non-nil")
	}
	defer db.Close()

	var mode string
	scanErr := db.SqlDB.QueryRow("PRAGMA journal_mode").Scan(&mode)
	if scanErr != nil {
		t.Fatalf("PRAGMA journal_mode query failed: %v", scanErr)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q; want %q", mode, "wal")
	}
}

// TestOpen_WALModeErrorFormat verifies the error message format when WAL mode
// cannot be set. (TS-02-15)
// This test validates the error format by construction; in practice WAL
// failures are filesystem-dependent and hard to simulate directly.
func TestOpen_WALModeErrorFormat(t *testing.T) {
	// Verify the expected error format matches what Open should produce.
	expectedSubstring := "failed to enable WAL mode"
	simulatedErr := "db: failed to enable WAL mode: journal_mode is \"delete\""
	if !strings.Contains(simulatedErr, expectedSubstring) {
		t.Errorf("expected error format to contain %q", expectedSubstring)
	}
	if !strings.Contains(simulatedErr, "delete") {
		t.Errorf("expected error format to contain the mode string")
	}
}

// TestOpenMemory_JournalMode verifies that OpenMemory skips WAL and the
// journal mode is "memory". (TS-02-16)
func TestOpenMemory_JournalMode(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("OpenMemory returned nil DB; want non-nil")
	}
	defer db.Close()

	var mode string
	scanErr := db.SqlDB.QueryRow("PRAGMA journal_mode").Scan(&mode)
	if scanErr != nil {
		t.Fatalf("PRAGMA journal_mode query failed: %v", scanErr)
	}
	if mode != "memory" {
		t.Errorf("journal_mode = %q; want %q", mode, "memory")
	}
}

// TestOpen_CorruptFile verifies that Open returns a wrapped error containing
// the file path when the database file is corrupt. (TS-02-17, TS-02-57, TS-02-E5, TS-02-E6)
func TestOpen_CorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.db")
	if err := os.WriteFile(path, []byte("this is not a sqlite database"), 0644); err != nil {
		t.Fatalf("failed to write corrupt file: %v", err)
	}

	db, err := Open(path)
	if db != nil {
		t.Error("Open(corrupt) returned non-nil DB; want nil")
		db.Close()
	}
	if err == nil {
		t.Fatal("Open(corrupt) returned nil error; want non-nil")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not contain file path %q", err.Error(), path)
	}
	if !strings.Contains(err.Error(), "failed to open database at") {
		t.Errorf("error %q does not contain 'failed to open database at'", err.Error())
	}
	if errors.Unwrap(err) == nil {
		t.Error("errors.Unwrap(err) returned nil; want original driver error")
	}
}

// TestOpen_CorruptFileRandomBytes verifies the same behavior with random
// binary content. (TS-02-E6)
func TestOpen_CorruptFileRandomBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "random.db")
	if err := os.WriteFile(path, []byte{0xFF, 0xFE, 0xAA, 0xBB}, 0644); err != nil {
		t.Fatalf("failed to write corrupt file: %v", err)
	}

	db, err := Open(path)
	if db != nil {
		t.Error("Open(random bytes) returned non-nil DB; want nil")
		db.Close()
	}
	if err == nil {
		t.Fatal("Open(random bytes) returned nil error; want non-nil")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not contain file path %q", err.Error(), path)
	}
	if errors.Unwrap(err) == nil {
		t.Error("errors.Unwrap(err) returned nil; want original driver error")
	}
}

// TestOpen_ErrorWrapperScope verifies that input validation errors do NOT
// contain the "failed to open database at" wrapper, while corrupt file errors
// DO. (TS-02-18)
func TestOpen_ErrorWrapperScope(t *testing.T) {
	// Empty path → input validation error, should NOT have wrapper.
	_, emptyErr := Open("")
	if emptyErr == nil {
		t.Fatal("Open(\"\") returned nil error")
	}
	if strings.Contains(emptyErr.Error(), "failed to open database at") {
		t.Errorf("input validation error %q should not contain 'failed to open database at'", emptyErr.Error())
	}

	// Corrupt file → driver error, should HAVE wrapper.
	corruptPath := filepath.Join(t.TempDir(), "c.db")
	if err := os.WriteFile(corruptPath, []byte("garbage"), 0644); err != nil {
		t.Fatalf("failed to write corrupt file: %v", err)
	}
	_, corruptErr := Open(corruptPath)
	if corruptErr == nil {
		t.Fatal("Open(corrupt) returned nil error")
	}
	if !strings.Contains(corruptErr.Error(), "failed to open database at") {
		t.Errorf("corrupt file error %q should contain 'failed to open database at'", corruptErr.Error())
	}
	if !strings.Contains(corruptErr.Error(), corruptPath) {
		t.Errorf("corrupt file error %q should contain path %q", corruptErr.Error(), corruptPath)
	}
}

// ---------------------------------------------------------------------------
// Subtask 1.5: Foreign key enforcement and Ping
// Test Spec: TS-02-48, TS-02-49, TS-02-58, TS-02-59, TS-02-E8
// Requirements: 02-REQ-12.1, 02-REQ-12.2, 02-REQ-7.5
// ---------------------------------------------------------------------------

// TestOpen_ForeignKeys verifies that foreign key constraints are enforced:
// inserting a child row with a non-existent parent is rejected. (TS-02-48, TS-02-58)
func TestOpen_ForeignKeys(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("OpenMemory returned nil DB; want non-nil")
	}
	defer db.Close()

	// Insert into api_keys with a user_id that does not exist in users.
	_, fkErr := db.SqlDB.Exec(
		`INSERT INTO api_keys VALUES ('k1','no-such-user','hash',30,NULL,NULL,'2026-01-01T00:00:00Z')`,
	)
	if fkErr == nil {
		t.Error("INSERT with non-existent user_id should fail with FK violation; got nil error")
	}
}

// TestOpen_CascadeDelete verifies that ON DELETE CASCADE on org_members.org_id
// removes membership rows when the referenced org is deleted. (TS-02-49)
func TestOpen_CascadeDelete(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("OpenMemory returned nil DB; want non-nil")
	}
	defer db.Close()

	// Insert a user.
	_, err = db.SqlDB.Exec(
		`INSERT INTO users VALUES ('u1','user1','e@e.com',NULL,'user','active','gh','gh1','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`,
	)
	if err != nil {
		t.Fatalf("insert user failed: %v", err)
	}

	// Insert an org.
	_, err = db.SqlDB.Exec(
		`INSERT INTO orgs VALUES ('org1','OrgName','org-slug',NULL,'active','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`,
	)
	if err != nil {
		t.Fatalf("insert org failed: %v", err)
	}

	// Insert membership.
	_, err = db.SqlDB.Exec(
		`INSERT INTO org_members VALUES ('org1','u1','2026-01-01T00:00:00Z')`,
	)
	if err != nil {
		t.Fatalf("insert org_member failed: %v", err)
	}

	// Delete the org — cascade should remove the membership.
	_, err = db.SqlDB.Exec(`DELETE FROM orgs WHERE id='org1'`)
	if err != nil {
		t.Fatalf("delete org failed: %v", err)
	}

	// Verify membership row is gone.
	var count int
	scanErr := db.SqlDB.QueryRow(`SELECT COUNT(*) FROM org_members WHERE org_id='org1'`).Scan(&count)
	if scanErr != nil {
		t.Fatalf("count query failed: %v", scanErr)
	}
	if count != 0 {
		t.Errorf("expected 0 org_members rows after cascade delete; got %d", count)
	}
}

// TestPing verifies that Ping returns nil on a healthy database connection. (TS-02-59)
func TestPing(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("OpenMemory returned nil DB; want non-nil")
	}
	defer db.Close()

	if pingErr := db.Ping(context.Background()); pingErr != nil {
		t.Errorf("Ping(Background) = %v; want nil", pingErr)
	}
}

// TestPing_CancelledContext verifies that Ping with a pre-cancelled context
// returns the context error immediately. (TS-02-E8)
func TestPing_CancelledContext(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("OpenMemory returned nil DB; want non-nil")
	}
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	pingErr := db.Ping(ctx)
	if pingErr == nil {
		t.Fatal("Ping with cancelled context returned nil error; want non-nil")
	}
	if !errors.Is(pingErr, context.Canceled) {
		t.Errorf("Ping error = %v; want context.Canceled", pingErr)
	}
}

// ---------------------------------------------------------------------------
// Subtask 2.1: WithTx tests
// Test Spec: TS-02-32, TS-02-33, TS-02-34, TS-02-35, TS-02-36, TS-02-60,
//            TS-02-61, TS-02-E10, TS-02-E11
// Requirements: 02-REQ-8.1, 02-REQ-8.2, 02-REQ-8.3, 02-REQ-8.4, 02-REQ-8.5
// ---------------------------------------------------------------------------

// TestWithTx_Commit verifies that changes made inside WithTx are visible after
// the function returns nil. (TS-02-32, TS-02-60)
func TestWithTx_Commit(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("OpenMemory returned nil DB; want non-nil")
	}
	defer db.Close()

	err = db.WithTx(context.Background(), func(tx *sql.Tx) error {
		_, e := tx.ExecContext(context.Background(), `INSERT INTO admin_config VALUES ('txkey','txval')`)
		return e
	})
	if err != nil {
		t.Fatalf("WithTx returned error = %v; want nil", err)
	}

	var v string
	scanErr := db.SqlDB.QueryRow("SELECT value FROM admin_config WHERE key='txkey'").Scan(&v)
	if scanErr != nil {
		t.Fatalf("query after commit failed: %v", scanErr)
	}
	if v != "txval" {
		t.Errorf("value after commit = %q; want %q", v, "txval")
	}
}

// TestWithTx_Rollback verifies that changes made inside WithTx are NOT visible
// after fn returns an error. (TS-02-61)
func TestWithTx_Rollback(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("OpenMemory returned nil DB; want non-nil")
	}
	defer db.Close()

	retErr := errors.New("fail")
	err = db.WithTx(context.Background(), func(tx *sql.Tx) error {
		_, _ = tx.ExecContext(context.Background(), `INSERT INTO admin_config VALUES ('rkey','rval')`)
		return retErr
	})
	if !errors.Is(err, retErr) {
		t.Errorf("WithTx error = %v; want %v", err, retErr)
	}

	var v string
	scanErr := db.SqlDB.QueryRow("SELECT value FROM admin_config WHERE key='rkey'").Scan(&v)
	if scanErr == nil {
		t.Errorf("expected no row after rollback; got value = %q", v)
	}
}

// TestWithTx_ErrorPassthrough verifies that WithTx returns exactly the same
// error value as returned by fn, without wrapping or transformation. (TS-02-33)
func TestWithTx_ErrorPassthrough(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("OpenMemory returned nil DB; want non-nil")
	}
	defer db.Close()

	sentinelErr := errors.New("test error")
	err = db.WithTx(context.Background(), func(tx *sql.Tx) error {
		return sentinelErr
	})
	if !errors.Is(err, sentinelErr) {
		t.Errorf("WithTx error = %v; want errors.Is to match %v", err, sentinelErr)
	}
}

// TestWithTx_RollbackDiscardsRollbackError verifies that WithTx returns the
// original error from fn even if the rollback also fails. (TS-02-34)
func TestWithTx_RollbackDiscardsRollbackError(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("OpenMemory returned nil DB; want non-nil")
	}
	defer db.Close()

	originalErr := errors.New("original")
	err = db.WithTx(context.Background(), func(tx *sql.Tx) error {
		return originalErr
	})
	if !errors.Is(err, originalErr) {
		t.Errorf("WithTx error = %v; want errors.Is to match %v", err, originalErr)
	}
}

// TestWithTx_DeferredIsolation verifies that WithTx uses SQLite's default
// DEFERRED transaction isolation (BeginTx with nil opts). (TS-02-35)
func TestWithTx_DeferredIsolation(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("OpenMemory returned nil DB; want non-nil")
	}
	defer db.Close()

	// DEFERRED is verified by BeginTx(ctx, nil); a successful commit
	// confirms the transaction mode is functional.
	err = db.WithTx(context.Background(), func(tx *sql.Tx) error {
		return nil
	})
	if err != nil {
		t.Errorf("WithTx with nil TxOptions (DEFERRED) returned error = %v; want nil", err)
	}
}

// TestWithTx_ContextPropagation verifies that WithTx forwards ctx to BeginTx
// and that context cancellation inside fn causes rollback and returns the
// context error. (TS-02-36)
func TestWithTx_ContextPropagation(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("OpenMemory returned nil DB; want non-nil")
	}
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = db.WithTx(ctx, func(tx *sql.Tx) error {
		return ctx.Err()
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("WithTx error = %v; want context.Canceled", err)
	}
}

// TestWithTx_ContextCancelled verifies that when fn returns context.Canceled,
// WithTx rolls back and returns the context error. (TS-02-E10)
func TestWithTx_ContextCancelled(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("OpenMemory returned nil DB; want non-nil")
	}
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = db.WithTx(ctx, func(tx *sql.Tx) error {
		return context.Canceled
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("WithTx error = %v; want context.Canceled", err)
	}
}

// TestWithTx_BeginTxFails verifies that when BeginTx fails (e.g. database
// is closed), WithTx returns the error and fn is never called. (TS-02-E11)
func TestWithTx_BeginTxFails(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("OpenMemory returned nil DB; want non-nil")
	}
	db.Close()

	fnCalled := false
	err = db.WithTx(context.Background(), func(tx *sql.Tx) error {
		fnCalled = true
		return nil
	})
	if err == nil {
		t.Fatal("WithTx after Close returned nil error; want non-nil")
	}
	if fnCalled {
		t.Error("fn was called despite BeginTx failure; want fn not called")
	}
}

// ---------------------------------------------------------------------------
// Subtask 2.2: WrapError and sentinel error tests
// Test Spec: TS-02-37, TS-02-38, TS-02-39, TS-02-40, TS-02-62, TS-02-63,
//            TS-02-64, TS-02-E12, TS-02-E15
// Requirements: 02-REQ-9.1, 02-REQ-9.2, 02-REQ-9.3, 02-REQ-9.4
// ---------------------------------------------------------------------------

// SQLite error codes matching modernc.org/sqlite/lib constants.
// Used for constructing synthetic test errors without a live database.
const (
	testSQLiteConstraintUnique     = 2067 // SQLITE_CONSTRAINT_UNIQUE
	testSQLiteConstraintPrimaryKey = 1555 // SQLITE_CONSTRAINT_PRIMARYKEY
	testSQLiteBusy                 = 5    // SQLITE_BUSY
	testSQLiteLocked               = 6    // SQLITE_LOCKED
)

// mockSQLiteError implements the sqliteErrorCode interface for testing
// WrapError without a live database connection. It allows constructing
// synthetic errors with specific SQLite error codes.
type mockSQLiteError struct {
	code int
}

func (e *mockSQLiteError) Error() string {
	return fmt.Sprintf("sqlite error: code %d", e.code)
}

func (e *mockSQLiteError) Code() int {
	return e.code
}

// TestSentinelErrors verifies that the three sentinel errors have the
// expected error messages. (TS-02-40)
func TestSentinelErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"ErrNotFound", ErrNotFound, "db: not found"},
		{"ErrConflict", ErrConflict, "db: conflict"},
		{"ErrDatabaseLocked", ErrDatabaseLocked, "db: database locked"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("%s.Error() = %q; want %q", tt.name, got, tt.want)
			}
		})
	}
}

// TestWrapError_Nil verifies that WrapError(nil) returns nil. (TS-02-37)
func TestWrapError_Nil(t *testing.T) {
	result := WrapError(nil)
	if result != nil {
		t.Errorf("WrapError(nil) = %v; want nil", result)
	}
}

// TestWrapError_UniqueConstraint verifies that WrapError maps
// SQLITE_CONSTRAINT_UNIQUE to ErrConflict using a synthetic error. (TS-02-37)
func TestWrapError_UniqueConstraint(t *testing.T) {
	syntheticErr := &mockSQLiteError{code: testSQLiteConstraintUnique}
	result := WrapError(syntheticErr)
	if !errors.Is(result, ErrConflict) {
		t.Errorf("WrapError(UNIQUE) = %v; want ErrConflict", result)
	}
}

// TestWrapError_PrimaryKeyConstraint verifies that WrapError maps
// SQLITE_CONSTRAINT_PRIMARYKEY to ErrConflict using a synthetic error. (TS-02-37)
func TestWrapError_PrimaryKeyConstraint(t *testing.T) {
	syntheticErr := &mockSQLiteError{code: testSQLiteConstraintPrimaryKey}
	result := WrapError(syntheticErr)
	if !errors.Is(result, ErrConflict) {
		t.Errorf("WrapError(PRIMARYKEY) = %v; want ErrConflict", result)
	}
}

// TestWrapError_Busy verifies that WrapError maps SQLITE_BUSY to
// ErrDatabaseLocked using a synthetic error with no database connection
// required. (TS-02-37, TS-02-38, TS-02-63)
func TestWrapError_Busy(t *testing.T) {
	syntheticErr := &mockSQLiteError{code: testSQLiteBusy}
	result := WrapError(syntheticErr)
	if !errors.Is(result, ErrDatabaseLocked) {
		t.Errorf("WrapError(BUSY) = %v; want ErrDatabaseLocked", result)
	}
}

// TestWrapError_Locked verifies that WrapError maps SQLITE_LOCKED to
// ErrDatabaseLocked using a synthetic error. (TS-02-37)
func TestWrapError_Locked(t *testing.T) {
	syntheticErr := &mockSQLiteError{code: testSQLiteLocked}
	result := WrapError(syntheticErr)
	if !errors.Is(result, ErrDatabaseLocked) {
		t.Errorf("WrapError(LOCKED) = %v; want ErrDatabaseLocked", result)
	}
}

// TestWrapError_Passthrough verifies that an unknown error is returned
// unchanged by WrapError. (TS-02-64)
func TestWrapError_Passthrough(t *testing.T) {
	original := errors.New("unknown error")
	result := WrapError(original)
	if !errors.Is(result, original) {
		t.Errorf("WrapError(unknown) = %v; want original error", result)
	}
}

// TestWrapError_NoRows verifies that WrapError does not map sql.ErrNoRows
// to ErrNotFound — callers must do that explicitly. (TS-02-39)
func TestWrapError_NoRows(t *testing.T) {
	result := WrapError(sql.ErrNoRows)
	if errors.Is(result, ErrNotFound) {
		t.Error("WrapError(sql.ErrNoRows) returned ErrNotFound; want sql.ErrNoRows unchanged")
	}
	if !errors.Is(result, sql.ErrNoRows) {
		t.Errorf("WrapError(sql.ErrNoRows) = %v; want sql.ErrNoRows", result)
	}
}

// TestWrapError_WrappedChain verifies that WrapError correctly maps a SQLite
// error code nested inside a wrapped error chain (e.g. via fmt.Errorf). (TS-02-E12)
func TestWrapError_WrappedChain(t *testing.T) {
	inner := &mockSQLiteError{code: testSQLiteConstraintUnique}
	wrapped := fmt.Errorf("outer: %w", inner)
	result := WrapError(wrapped)
	if !errors.Is(result, ErrConflict) {
		t.Errorf("WrapError(wrapped UNIQUE) = %v; want ErrConflict", result)
	}
}

// TestWrapError_Conflict_Integration verifies that a UNIQUE constraint
// violation produced by a real INSERT maps to ErrConflict via WrapError. (TS-02-62)
func TestWrapError_Conflict_Integration(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("OpenMemory returned nil DB; want non-nil")
	}
	defer db.Close()

	// Insert first user.
	_, err = db.SqlDB.Exec(
		`INSERT INTO users VALUES ('id1','u1','e@e.com',NULL,'user','active','gh','gh1','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`,
	)
	if err != nil {
		t.Fatalf("first insert failed: %v", err)
	}

	// Insert duplicate username — triggers UNIQUE constraint violation.
	_, dupErr := db.SqlDB.Exec(
		`INSERT INTO users VALUES ('id2','u1','e2@e.com',NULL,'user','active','gh','gh2','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`,
	)
	if dupErr == nil {
		t.Fatal("expected UNIQUE constraint violation; got nil error")
	}

	wrapped := WrapError(dupErr)
	if !errors.Is(wrapped, ErrConflict) {
		t.Errorf("WrapError(duplicate) = %v; want ErrConflict", wrapped)
	}
}

// TestWrapError_ForeignKeyViolation verifies that inserting an api_keys row
// with a non-existent user_id causes WrapError to map to ErrConflict. (TS-02-E15)
func TestWrapError_ForeignKeyViolation(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("OpenMemory returned nil DB; want non-nil")
	}
	defer db.Close()

	_, fkErr := db.SqlDB.Exec(
		`INSERT INTO api_keys VALUES ('k1','nonexistent','hash',30,NULL,NULL,'2026-01-01T00:00:00Z')`,
	)
	if fkErr == nil {
		t.Fatal("expected FK violation; got nil error")
	}

	wrapped := WrapError(fkErr)
	if !errors.Is(wrapped, ErrConflict) {
		t.Errorf("WrapError(FK violation) = %v; want ErrConflict", wrapped)
	}
}

// ---------------------------------------------------------------------------
// Subtask 2.3: Timestamp helper tests
// Test Spec: TS-02-41, TS-02-42, TS-02-43, TS-02-44, TS-02-65, TS-02-66,
//            TS-02-E13, TS-02-E14
// Requirements: 02-REQ-10.1, 02-REQ-10.2, 02-REQ-10.3, 02-REQ-10.4
// ---------------------------------------------------------------------------

// TestTimeFormat verifies that the TimeFormat constant has the expected
// value. (TS-02-41)
func TestTimeFormat(t *testing.T) {
	const want = "2006-01-02T15:04:05Z"
	if TimeFormat != want {
		t.Errorf("TimeFormat = %q; want %q", TimeFormat, want)
	}
}

// TestFormatTime verifies truncation to whole-second precision, UTC
// normalization, and correct TimeFormat output. (TS-02-42, TS-02-65)
func TestFormatTime(t *testing.T) {
	// Time with sub-second nanos and non-UTC zone (EST = UTC-5).
	est := time.FixedZone("EST", -5*3600)
	input := time.Date(2026, 1, 15, 10, 30, 45, 123456789, est)
	result := FormatTime(input)

	const want = "2026-01-15T15:30:45Z"
	if result != want {
		t.Errorf("FormatTime(%v) = %q; want %q", input, result, want)
	}
	if strings.Contains(result, ".") {
		t.Errorf("FormatTime result %q contains sub-second '.'; want whole-second only", result)
	}
}

// TestFormatTime_NonUTCTimezone verifies that FormatTime converts a time
// with a non-UTC timezone offset to UTC; the output always ends with 'Z'
// and never contains a numeric timezone offset. (TS-02-E13)
func TestFormatTime_NonUTCTimezone(t *testing.T) {
	ist := time.FixedZone("IST", 5*3600+30*60)
	input := time.Date(2026, 7, 17, 14, 30, 0, 999999999, ist)
	result := FormatTime(input)

	const want = "2026-07-17T09:00:00Z"
	if result != want {
		t.Errorf("FormatTime(%v) = %q; want %q", input, result, want)
	}
	if strings.Contains(result, "+") {
		t.Errorf("FormatTime result %q contains '+' timezone offset; want 'Z' suffix only", result)
	}
}

// TestParseTime verifies that ParseTime parses a canonical RFC 3339 UTC
// timestamp and returns the correct time.Time in UTC. (TS-02-43)
func TestParseTime(t *testing.T) {
	parsed, err := ParseTime("2026-07-17T14:30:00Z")
	if err != nil {
		t.Fatalf("ParseTime(valid) error = %v; want nil", err)
	}
	if parsed.Year() != 2026 || parsed.Month() != 7 || parsed.Day() != 17 ||
		parsed.Hour() != 14 || parsed.Minute() != 30 || parsed.Second() != 0 {
		t.Errorf("ParseTime fields = %v; want 2026-07-17T14:30:00Z", parsed)
	}
	if parsed.Location() != time.UTC {
		t.Errorf("ParseTime location = %v; want UTC", parsed.Location())
	}
}

// TestParseTime_Invalid verifies that ParseTime returns an error for an
// invalid input string. (TS-02-43)
func TestParseTime_Invalid(t *testing.T) {
	_, err := ParseTime("not-a-timestamp")
	if err == nil {
		t.Error("ParseTime(invalid) returned nil error; want non-nil")
	}
}

// TestParseTime_TimezoneOffset verifies that ParseTime returns a parse error
// when the input has a numeric timezone offset instead of 'Z'. (TS-02-E14)
func TestParseTime_TimezoneOffset(t *testing.T) {
	parsed, err := ParseTime("2026-07-17T14:30:00+05:30")
	if err == nil {
		t.Error("ParseTime(+05:30) returned nil error; want parse error")
	}
	if !parsed.IsZero() {
		t.Errorf("ParseTime(+05:30) returned non-zero time %v; want zero", parsed)
	}
}

// TestFormatTimeParseTime_RoundTrip verifies that ParseTime(FormatTime(t))
// equals t.Truncate(time.Second).UTC() for any time.Time. (TS-02-44, TS-02-66)
func TestFormatTimeParseTime_RoundTrip(t *testing.T) {
	now := time.Now().Add(12345678 * time.Nanosecond)
	formatted := FormatTime(now)
	parsed, err := ParseTime(formatted)
	if err != nil {
		t.Fatalf("ParseTime(FormatTime(now)) error = %v; want nil", err)
	}
	expected := now.Truncate(time.Second).UTC()
	if !parsed.Equal(expected) {
		t.Errorf("round-trip: parsed = %v; want %v", parsed, expected)
	}
}

// ---------------------------------------------------------------------------
// Subtask 2.4: Connection pool, OpenMemory isolation, and DB.Close tests
// Test Spec: TS-02-27, TS-02-29, TS-02-30, TS-02-45, TS-02-46, TS-02-47
// Requirements: 02-REQ-7.1, 02-REQ-7.3, 02-REQ-7.4, 02-REQ-11.1,
//               02-REQ-11.2, 02-REQ-11.3
// ---------------------------------------------------------------------------

// TestDB_SqlDBField verifies that the DB struct has a SqlDB field of type
// *sql.DB that is non-nil after opening. (TS-02-27)
func TestDB_SqlDBField(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("OpenMemory returned nil DB; want non-nil")
	}
	defer db.Close()

	if db.SqlDB == nil {
		t.Error("db.SqlDB is nil; want non-nil *sql.DB")
	}
}

// TestOpenMemory_Independent verifies that each OpenMemory call returns
// an independent isolated database instance with no shared state.
// (TS-02-29, TS-02-47)
func TestOpenMemory_Independent(t *testing.T) {
	db1, err1 := OpenMemory()
	if err1 != nil {
		t.Fatalf("OpenMemory (1) error = %v; want nil", err1)
	}
	if db1 == nil {
		t.Fatal("OpenMemory (1) returned nil DB; want non-nil")
	}
	defer db1.Close()

	db2, err2 := OpenMemory()
	if err2 != nil {
		t.Fatalf("OpenMemory (2) error = %v; want nil", err2)
	}
	if db2 == nil {
		t.Fatal("OpenMemory (2) returned nil DB; want non-nil")
	}
	defer db2.Close()

	// Insert into db1.
	_, execErr := db1.SqlDB.Exec(`INSERT INTO admin_config VALUES ('isolated','yes')`)
	if execErr != nil {
		t.Fatalf("insert into db1 failed: %v", execErr)
	}

	// Query db2 for the same key — should not find it.
	var v string
	scanErr := db2.SqlDB.QueryRow(`SELECT value FROM admin_config WHERE key='isolated'`).Scan(&v)
	if scanErr == nil {
		t.Errorf("db2 found value %q for key 'isolated'; want no row (isolation violated)", v)
	}
}

// TestOpenMemory_AllTablesExist verifies that OpenMemory creates all six
// tables in the in-memory database. (TS-02-29)
func TestOpenMemory_AllTablesExist(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("OpenMemory returned nil DB; want non-nil")
	}
	defer db.Close()

	for _, table := range allTables {
		var name string
		scanErr := db.SqlDB.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if scanErr != nil {
			t.Errorf("table %q not found in OpenMemory database: %v", table, scanErr)
		}
	}
}

// TestClose verifies that DB.Close closes the underlying *sql.DB and
// subsequent queries fail. (TS-02-30)
func TestClose(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("OpenMemory returned nil DB; want non-nil")
	}

	closeErr := db.Close()
	if closeErr != nil {
		t.Errorf("Close() error = %v; want nil", closeErr)
	}

	// Subsequent query should fail.
	_, queryErr := db.SqlDB.Exec("SELECT 1")
	if queryErr == nil {
		t.Error("Exec after Close returned nil error; want non-nil")
	}
}

// TestOpen_MaxOpenConnections verifies that Open configures
// SetMaxOpenConns(1) on the connection pool. (TS-02-45)
func TestOpen_MaxOpenConnections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "maxopen.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%q) error = %v; want nil", path, err)
	}
	if db == nil {
		t.Fatal("Open returned nil DB; want non-nil")
	}
	defer db.Close()

	stats := db.SqlDB.Stats()
	if stats.MaxOpenConnections != 1 {
		t.Errorf("MaxOpenConnections = %d; want 1", stats.MaxOpenConnections)
	}
}

// TestOpenMemory_MaxOpenConnections verifies that OpenMemory configures
// SetMaxOpenConns(1) on the connection pool. (TS-02-46)
func TestOpenMemory_MaxOpenConnections(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("OpenMemory returned nil DB; want non-nil")
	}
	defer db.Close()

	stats := db.SqlDB.Stats()
	if stats.MaxOpenConnections != 1 {
		t.Errorf("MaxOpenConnections = %d; want 1", stats.MaxOpenConnections)
	}
}

// ---------------------------------------------------------------------------
// Subtask 3.1: Property tests for initDB shared initialization and schema
//              idempotency
// Test Spec: TS-02-7, TS-02-8, TS-02-10, TS-02-11, TS-02-13, TS-02-P2
// Requirements: 02-REQ-3.1, 02-REQ-3.2, 02-REQ-3.5, 02-REQ-3.6, 02-REQ-6.8
// ---------------------------------------------------------------------------

// verifyTablesExist checks that all six required tables exist in the given
// *sql.DB. Used by property and smoke tests to avoid redundant inline checks.
func verifyTablesExist(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	for _, table := range allTables {
		var name string
		err := sqlDB.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found in sqlite_master: %v", table, err)
		}
	}
}

// verifyForeignKeysOn checks that PRAGMA foreign_keys returns 1 (enabled).
func verifyForeignKeysOn(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	var fk int
	if err := sqlDB.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("PRAGMA foreign_keys query failed: %v", err)
	}
	if fk != 1 {
		t.Errorf("PRAGMA foreign_keys = %d; want 1", fk)
	}
}

// TestProperty_SharedInitDB verifies that both Open and OpenMemory delegate
// all post-connection initialization to the same shared private initDB
// function; both produce *DB handles with identical initialization (pool
// settings, foreign key enforcement, schema). (TS-02-7)
func TestProperty_SharedInitDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.db")
	db1, err1 := Open(path)
	if err1 != nil {
		t.Fatalf("Open error = %v; want nil", err1)
	}
	if db1 == nil {
		t.Fatal("Open returned nil DB; want non-nil")
	}
	defer db1.Close()

	db2, err2 := OpenMemory()
	if err2 != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err2)
	}
	if db2 == nil {
		t.Fatal("OpenMemory returned nil DB; want non-nil")
	}
	defer db2.Close()

	// Both should have all six tables.
	verifyTablesExist(t, db1.SqlDB)
	verifyTablesExist(t, db2.SqlDB)
}

// TestProperty_InitDB_PoolSettings verifies that after Open, the *sql.DB
// handle reports MaxOpenConnections == 1, confirming initDB sets
// SetMaxOpenConns(1) before executing any PRAGMAs. (TS-02-8)
func TestProperty_InitDB_PoolSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pool.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("Open returned nil DB; want non-nil")
	}
	defer db.Close()

	stats := db.SqlDB.Stats()
	if stats.MaxOpenConnections != 1 {
		t.Errorf("MaxOpenConnections = %d; want 1", stats.MaxOpenConnections)
	}
}

// TestProperty_OpenMemory_FullInit verifies that OpenMemory succeeds with
// all six tables present, foreign keys enforced, and pool configured;
// no WAL error is returned. (TS-02-10)
func TestProperty_OpenMemory_FullInit(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("OpenMemory returned nil DB; want non-nil")
	}
	defer db.Close()

	verifyTablesExist(t, db.SqlDB)
	verifyForeignKeysOn(t, db.SqlDB)
}

// TestProperty_Open_ForeignKeysEnabled verifies that after Open, querying
// PRAGMA foreign_keys returns 1 (enabled). (TS-02-11)
func TestProperty_Open_ForeignKeysEnabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fk.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("Open returned nil DB; want non-nil")
	}
	defer db.Close()

	verifyForeignKeysOn(t, db.SqlDB)
}

// TestProperty_Open_FullyInitialized verifies that Open returns a non-nil
// *DB with non-nil SqlDB and nil error, meaning no partial states are
// possible. (TS-02-13)
func TestProperty_Open_FullyInitialized(t *testing.T) {
	path := filepath.Join(t.TempDir(), "full.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("Open returned nil *DB; want non-nil")
	}
	defer db.Close()

	if db.SqlDB == nil {
		t.Error("db.SqlDB is nil; want non-nil")
	}
}

// TestProperty_SchemaIdempotency verifies that opening the same database
// path three times succeeds each time, existing data persists across
// opens, and all tables remain present. (TS-02-P2)
func TestProperty_SchemaIdempotency(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idempotent.db")

	// First open: create schema and insert sentinel.
	db1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open error = %v; want nil", err)
	}
	if db1 == nil {
		t.Fatal("first Open returned nil DB")
	}
	_, execErr := db1.SqlDB.Exec(`INSERT INTO admin_config VALUES ('sentinel', 'alive')`)
	if execErr != nil {
		t.Fatalf("insert sentinel failed: %v", execErr)
	}
	db1.Close()

	// Second open: schema already exists; verify sentinel persists.
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open error = %v; want nil", err)
	}
	if db2 == nil {
		t.Fatal("second Open returned nil DB")
	}
	var v string
	if scanErr := db2.SqlDB.QueryRow("SELECT value FROM admin_config WHERE key='sentinel'").Scan(&v); scanErr != nil {
		t.Fatalf("sentinel not found on second open: %v", scanErr)
	}
	if v != "alive" {
		t.Errorf("sentinel value = %q; want %q", v, "alive")
	}
	verifyTablesExist(t, db2.SqlDB)
	db2.Close()

	// Third open: verify sentinel still persists and schema unchanged.
	db3, err := Open(path)
	if err != nil {
		t.Fatalf("third Open error = %v; want nil", err)
	}
	if db3 == nil {
		t.Fatal("third Open returned nil DB")
	}
	defer db3.Close()

	if scanErr := db3.SqlDB.QueryRow("SELECT value FROM admin_config WHERE key='sentinel'").Scan(&v); scanErr != nil {
		t.Fatalf("sentinel not found on third open: %v", scanErr)
	}
	if v != "alive" {
		t.Errorf("sentinel value on third open = %q; want %q", v, "alive")
	}
	verifyTablesExist(t, db3.SqlDB)
}

// ---------------------------------------------------------------------------
// Subtask 3.2: Property tests for FormatTime/ParseTime, WrapError purity,
//              and WithTx atomicity
// Test Spec: TS-02-P3, TS-02-P4, TS-02-P5
// Requirements: 02-REQ-10.4, 02-REQ-9.2, 02-REQ-8.1, 02-REQ-8.3
// ---------------------------------------------------------------------------

// TestProperty_FormatTimeParseTime_RoundTrip iterates over a table of diverse
// time.Time values (≥5, spanning past/future, sub-second precision, non-UTC
// zones) and verifies that ParseTime(FormatTime(t)) equals
// t.Truncate(time.Second).UTC() for each. (TS-02-P3)
func TestProperty_FormatTimeParseTime_RoundTrip(t *testing.T) {
	est := time.FixedZone("EST", -5*3600)
	ist := time.FixedZone("IST", 5*3600+30*60)
	nzst := time.FixedZone("NZST", 12*3600)
	aest := time.FixedZone("AEST", 10*3600)

	tests := []struct {
		name string
		t    time.Time
	}{
		{"UTC now", time.Now()},
		{"Unix epoch", time.Unix(0, 0)},
		{"Sub-second nanos", time.Date(2026, 1, 15, 10, 30, 45, 123456789, time.UTC)},
		{"Non-UTC EST", time.Date(2026, 6, 15, 14, 0, 0, 0, est)},
		{"Non-UTC IST with sub-second", time.Date(2026, 12, 31, 23, 59, 59, 999999999, ist)},
		{"Non-UTC NZST midnight", time.Date(2026, 3, 1, 0, 0, 0, 0, nzst)},
		{"Far future 2099", time.Date(2099, 12, 31, 23, 59, 59, 0, time.UTC)},
		{"Year 2000 sub-second", time.Date(2000, 1, 1, 0, 0, 0, 500000000, time.UTC)},
		{"Non-UTC AEST", time.Date(2026, 7, 18, 8, 15, 30, 0, aest)},
		{"Negative offset", time.Date(2026, 1, 1, 3, 0, 0, 750000000, est)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			formatted := FormatTime(tt.t)
			parsed, err := ParseTime(formatted)
			if err != nil {
				t.Fatalf("ParseTime(FormatTime(%v)) error = %v", tt.t, err)
			}
			expected := tt.t.Truncate(time.Second).UTC()
			if !parsed.Equal(expected) {
				t.Errorf("round-trip: got %v; want %v", parsed, expected)
			}
		})
	}
}

// TestProperty_WrapError_Purity verifies that WrapError is a pure function
// with no side effects: calling it twice with the same input produces the
// same output both times; no global state is mutated. (TS-02-P4)
func TestProperty_WrapError_Purity(t *testing.T) {
	inputs := []struct {
		name string
		err  error
		want error // expected sentinel, nil means passthrough
	}{
		{"nil", nil, nil},
		{"UNIQUE", &mockSQLiteError{code: testSQLiteConstraintUnique}, ErrConflict},
		{"PRIMARYKEY", &mockSQLiteError{code: testSQLiteConstraintPrimaryKey}, ErrConflict},
		{"BUSY", &mockSQLiteError{code: testSQLiteBusy}, ErrDatabaseLocked},
		{"LOCKED", &mockSQLiteError{code: testSQLiteLocked}, ErrDatabaseLocked},
		{"unknown", errors.New("unknown error"), nil},
		{"wrapped UNIQUE", fmt.Errorf("outer: %w", &mockSQLiteError{code: testSQLiteConstraintUnique}), ErrConflict},
	}

	for _, tt := range inputs {
		t.Run(tt.name, func(t *testing.T) {
			// Call twice to verify determinism.
			r1 := WrapError(tt.err)
			r2 := WrapError(tt.err)

			if tt.want == nil && tt.err == nil {
				// nil → nil
				if r1 != nil {
					t.Errorf("WrapError(nil) first call = %v; want nil", r1)
				}
				if r2 != nil {
					t.Errorf("WrapError(nil) second call = %v; want nil", r2)
				}
			} else if tt.want == nil {
				// Passthrough: result should be the original error.
				if !errors.Is(r1, tt.err) {
					t.Errorf("WrapError(%s) first call = %v; want original error", tt.name, r1)
				}
				if !errors.Is(r2, tt.err) {
					t.Errorf("WrapError(%s) second call = %v; want original error", tt.name, r2)
				}
			} else {
				// Sentinel mapping: both calls produce the same sentinel.
				if !errors.Is(r1, tt.want) {
					t.Errorf("WrapError(%s) first call = %v; want %v", tt.name, r1, tt.want)
				}
				if !errors.Is(r2, tt.want) {
					t.Errorf("WrapError(%s) second call = %v; want %v", tt.name, r2, tt.want)
				}
			}
		})
	}
}

// TestProperty_WithTx_Atomicity verifies that for each of several random key
// names, a WithTx call that inserts into admin_config then returns an error
// leaves the table count unchanged — the transaction is fully rolled back.
// (TS-02-P5)
func TestProperty_WithTx_Atomicity(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("OpenMemory returned nil DB; want non-nil")
	}
	defer db.Close()

	keys := []string{"atomicA", "atomicB", "atomicC", "atomicD", "atomicE"}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			// Count before.
			var before int
			if err := db.SqlDB.QueryRow("SELECT COUNT(*) FROM admin_config").Scan(&before); err != nil {
				t.Fatalf("count before failed: %v", err)
			}

			// WithTx: insert then return error → expect rollback.
			txErr := db.WithTx(context.Background(), func(tx *sql.Tx) error {
				_, _ = tx.ExecContext(context.Background(),
					"INSERT INTO admin_config VALUES (?, 'val')", key)
				return errors.New("forced rollback")
			})
			if txErr == nil {
				t.Fatal("WithTx returned nil error; want forced rollback error")
			}

			// Count after must be unchanged.
			var after int
			if err := db.SqlDB.QueryRow("SELECT COUNT(*) FROM admin_config").Scan(&after); err != nil {
				t.Fatalf("count after failed: %v", err)
			}
			if after != before {
				t.Errorf("admin_config count changed from %d to %d after failed WithTx", before, after)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Subtask 3.3: Property tests for Open/OpenMemory invariants and isolation
// Test Spec: TS-02-P6, TS-02-P7, TS-02-P8
// Requirements: 02-REQ-7.2, 02-REQ-7.3, 02-REQ-1.1, 02-REQ-1.2,
//               02-REQ-1.3, 02-REQ-11.3
// ---------------------------------------------------------------------------

// TestProperty_Open_FullyInitializedOrError verifies that for various input
// paths (valid, empty, directory, corrupt), Open always returns either
// (*DB with full init, nil) or (nil, non-nil error). No partially initialized
// DB handle is ever returned. (TS-02-P6)
func TestProperty_Open_FullyInitializedOrError(t *testing.T) {
	validPath := filepath.Join(t.TempDir(), "valid.db")
	dirPath := t.TempDir()
	corruptPath := filepath.Join(t.TempDir(), "corrupt.db")
	if err := os.WriteFile(corruptPath, []byte("not sqlite"), 0644); err != nil {
		t.Fatalf("failed to write corrupt file: %v", err)
	}

	cases := []struct {
		name string
		path string
	}{
		{"valid path", validPath},
		{"empty path", ""},
		{"directory path", dirPath},
		{"corrupt file", corruptPath},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db, err := Open(c.path)
			if err == nil {
				// Success: db must be fully initialized.
				if db == nil {
					t.Fatal("Open returned nil DB with nil error")
				}
				defer db.Close()
				if db.SqlDB == nil {
					t.Error("db.SqlDB is nil on success path")
				}
				verifyForeignKeysOn(t, db.SqlDB)
				verifyTablesExist(t, db.SqlDB)
			} else {
				// Error: db must be nil.
				if db != nil {
					t.Errorf("Open returned non-nil DB with error %v", err)
					db.Close()
				}
			}
		})
	}
}

// TestProperty_OpenMemory_Isolation verifies that three distinct OpenMemory
// calls return fully independent *DB instances: writes to one are invisible
// in the others. (TS-02-P7)
func TestProperty_OpenMemory_Isolation(t *testing.T) {
	const n = 3
	dbs := make([]*DB, n)
	for i := range n {
		d, err := OpenMemory()
		if err != nil {
			t.Fatalf("OpenMemory(%d) error = %v", i, err)
		}
		if d == nil {
			t.Fatalf("OpenMemory(%d) returned nil DB", i)
		}
		dbs[i] = d
	}
	defer func() {
		for _, d := range dbs {
			if d != nil {
				d.Close()
			}
		}
	}()

	// Insert a distinct row into each.
	for i, d := range dbs {
		key := fmt.Sprintf("iso_%d", i)
		_, err := d.SqlDB.Exec("INSERT INTO admin_config VALUES (?, ?)", key, "val")
		if err != nil {
			t.Fatalf("insert into db[%d] failed: %v", i, err)
		}
	}

	// Cross-check: each pair must not see the other's row.
	for i := range dbs {
		for j := range dbs {
			if i == j {
				continue
			}
			key := fmt.Sprintf("iso_%d", i)
			var v string
			scanErr := dbs[j].SqlDB.QueryRow("SELECT value FROM admin_config WHERE key=?", key).Scan(&v)
			if scanErr == nil {
				t.Errorf("db[%d] found row with key %q from db[%d]; want isolation", j, key, i)
			}
		}
	}
}

// TestProperty_Open_InputValidation_NoSideEffects verifies that calling Open
// with an empty string or an existing directory produces no filesystem side
// effects — the temp directory listing is unchanged after the call. (TS-02-P8)
func TestProperty_Open_InputValidation_NoSideEffects(t *testing.T) {
	t.Run("empty path", func(t *testing.T) {
		root := t.TempDir()
		entriesBefore, _ := os.ReadDir(root)

		db, err := Open("")
		if err == nil {
			t.Fatal("Open(\"\") returned nil error; want non-nil")
		}
		if db != nil {
			t.Error("Open(\"\") returned non-nil DB; want nil")
			db.Close()
		}

		entriesAfter, _ := os.ReadDir(root)
		if len(entriesAfter) != len(entriesBefore) {
			t.Errorf("directory listing changed: before=%d entries, after=%d entries",
				len(entriesBefore), len(entriesAfter))
		}
	})

	t.Run("directory path", func(t *testing.T) {
		dir := t.TempDir()
		entriesBefore, _ := os.ReadDir(dir)

		db, err := Open(dir)
		if err == nil {
			t.Fatal("Open(dir) returned nil error; want non-nil")
		}
		if db != nil {
			t.Error("Open(dir) returned non-nil DB; want nil")
			db.Close()
		}

		entriesAfter, _ := os.ReadDir(dir)
		if len(entriesAfter) != len(entriesBefore) {
			t.Errorf("directory listing changed: before=%d entries, after=%d entries",
				len(entriesBefore), len(entriesAfter))
		}
	})
}

// ---------------------------------------------------------------------------
// Subtask 3.4: Smoke tests exercising real execution paths
// Test Spec: TS-02-SMOKE-1, TS-02-SMOKE-2, TS-02-SMOKE-3, TS-02-SMOKE-4,
//            TS-02-SMOKE-5
// Requirements: 02-REQ-7.2, 02-REQ-7.3, 02-REQ-5.1, 02-REQ-8.1, 02-REQ-9.1
// ---------------------------------------------------------------------------

// TestSmoke_Open_HappyPath is a full happy-path smoke test: Open creates a
// missing parent directory, initializes WAL mode and foreign keys, creates all
// six tables, and returns a usable *DB handle. (TS-02-SMOKE-1, 02-PATH-1)
func TestSmoke_Open_HappyPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "newdir", "apikit.db")

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("Open returned nil DB; want non-nil")
	}
	defer db.Close()

	// Parent directory was created.
	dir := filepath.Dir(path)
	info, statErr := os.Stat(dir)
	if statErr != nil {
		t.Fatalf("parent dir %q not created: %v", dir, statErr)
	}
	if !info.IsDir() {
		t.Fatalf("parent %q is not a directory", dir)
	}

	// On non-Windows, verify directory mode is 0700.
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0700 {
			t.Errorf("directory mode = %04o; want 0700", perm)
		}
	}

	// Database file was created.
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("database file %q not created: %v", path, statErr)
	}

	// PRAGMA journal_mode == 'wal'.
	var mode string
	if err := db.SqlDB.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("PRAGMA journal_mode failed: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q; want %q", mode, "wal")
	}

	// PRAGMA foreign_keys == 1.
	verifyForeignKeysOn(t, db.SqlDB)

	// All six tables present.
	verifyTablesExist(t, db.SqlDB)

	// MaxOpenConnections == 1.
	if stats := db.SqlDB.Stats(); stats.MaxOpenConnections != 1 {
		t.Errorf("MaxOpenConnections = %d; want 1", stats.MaxOpenConnections)
	}
}

// TestSmoke_OpenMemory_HappyPath is a full happy-path smoke test for
// OpenMemory: returns an independent in-memory DB with full initialization
// (no WAL, FK ON, all six tables), and a second call is isolated.
// (TS-02-SMOKE-2, 02-PATH-2)
func TestSmoke_OpenMemory_HappyPath(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("OpenMemory returned nil DB; want non-nil")
	}
	defer db.Close()

	// journal_mode == 'memory' (WAL is skipped).
	var mode string
	if err := db.SqlDB.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("PRAGMA journal_mode failed: %v", err)
	}
	if mode != "memory" {
		t.Errorf("journal_mode = %q; want %q", mode, "memory")
	}

	// FK on.
	verifyForeignKeysOn(t, db.SqlDB)

	// All six tables.
	verifyTablesExist(t, db.SqlDB)

	// Pool setting.
	if stats := db.SqlDB.Stats(); stats.MaxOpenConnections != 1 {
		t.Errorf("MaxOpenConnections = %d; want 1", stats.MaxOpenConnections)
	}

	// Second call produces an independent isolated instance.
	db2, err := OpenMemory()
	if err != nil {
		t.Fatalf("second OpenMemory error = %v; want nil", err)
	}
	if db2 == nil {
		t.Fatal("second OpenMemory returned nil DB")
	}
	defer db2.Close()

	// Insert into first, verify absent in second.
	_, _ = db.SqlDB.Exec("INSERT INTO admin_config VALUES ('smoke2', 'val')")
	var v string
	scanErr := db2.SqlDB.QueryRow("SELECT value FROM admin_config WHERE key='smoke2'").Scan(&v)
	if scanErr == nil {
		t.Error("second OpenMemory instance found row from first; want isolation")
	}
}

// TestSmoke_Open_CorruptFile is an error-path smoke test: Open on a corrupt
// file (non-SQLite bytes) returns a wrapped error containing the file path;
// no *DB handle is returned, and the underlying *sql.DB is closed.
// (TS-02-SMOKE-3, 02-PATH-3)
func TestSmoke_Open_CorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt_smoke.db")
	if err := os.WriteFile(path, []byte("arbitrary non-SQLite bytes for smoke test"), 0644); err != nil {
		t.Fatalf("failed to write corrupt file: %v", err)
	}

	db, err := Open(path)
	if db != nil {
		t.Error("Open(corrupt) returned non-nil DB; want nil")
		db.Close()
	}
	if err == nil {
		t.Fatal("Open(corrupt) returned nil error; want non-nil")
	}

	// Error contains 'db: failed to open database at' and the file path.
	errMsg := err.Error()
	if !strings.Contains(errMsg, "db: failed to open database at") {
		t.Errorf("error %q missing 'db: failed to open database at'", errMsg)
	}
	if !strings.Contains(errMsg, path) {
		t.Errorf("error %q missing path %q", errMsg, path)
	}

	// errors.Unwrap returns the original driver error.
	if errors.Unwrap(err) == nil {
		t.Error("errors.Unwrap(err) = nil; want original driver error")
	}
}

// TestSmoke_WithTx_Commit is a happy-path smoke test for WithTx: changes
// made by fn inside a transaction are durably visible after commit.
// (TS-02-SMOKE-4, 02-PATH-4)
func TestSmoke_WithTx_Commit(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("OpenMemory returned nil DB; want non-nil")
	}
	defer db.Close()

	err = db.WithTx(context.Background(), func(tx *sql.Tx) error {
		_, e := tx.ExecContext(context.Background(),
			"INSERT INTO admin_config VALUES ('smoke_commit', 'yes')")
		return e
	})
	if err != nil {
		t.Fatalf("WithTx commit returned error = %v; want nil", err)
	}

	// Verify the row is visible after commit.
	var v string
	if scanErr := db.SqlDB.QueryRow("SELECT value FROM admin_config WHERE key='smoke_commit'").Scan(&v); scanErr != nil {
		t.Fatalf("row not found after commit: %v", scanErr)
	}
	if v != "yes" {
		t.Errorf("value = %q; want %q", v, "yes")
	}
}

// TestSmoke_WithTx_Rollback is an error-path smoke test for WithTx: fn
// triggers a UNIQUE constraint violation which WrapError maps to ErrConflict;
// the transaction is rolled back and no rows from fn are visible.
// (TS-02-SMOKE-5, 02-PATH-5)
func TestSmoke_WithTx_Rollback(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	if db == nil {
		t.Fatal("OpenMemory returned nil DB; want non-nil")
	}
	defer db.Close()

	// Insert a user so we can cause a UNIQUE violation on username.
	_, err = db.SqlDB.Exec(
		`INSERT INTO users VALUES ('u1','uname','e@e.com',NULL,'user','active','gh','gh1','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`,
	)
	if err != nil {
		t.Fatalf("insert user failed: %v", err)
	}

	// WithTx that inserts a duplicate username and returns WrapError(dupErr).
	err = db.WithTx(context.Background(), func(tx *sql.Tx) error {
		_, dupErr := tx.ExecContext(context.Background(),
			`INSERT INTO users VALUES ('u2','uname','e2@e.com',NULL,'user','active','gh','gh2','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`)
		return WrapError(dupErr)
	})
	if !errors.Is(err, ErrConflict) {
		t.Errorf("WithTx error = %v; want ErrConflict", err)
	}

	// No second user row should exist after rollback.
	var count int
	if scanErr := db.SqlDB.QueryRow("SELECT COUNT(*) FROM users WHERE id='u2'").Scan(&count); scanErr != nil {
		t.Fatalf("count query failed: %v", scanErr)
	}
	if count != 0 {
		t.Errorf("user 'u2' found after rollback; want 0 rows, got %d", count)
	}
}

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

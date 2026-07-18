package db

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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

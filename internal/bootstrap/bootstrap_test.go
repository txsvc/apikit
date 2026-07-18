package bootstrap_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	_ "modernc.org/sqlite"

	"github.com/txsvc/apikit/internal/bootstrap"
)

// ---------------------------------------------------------------------------
// Test helpers (subtask 1.1)
// ---------------------------------------------------------------------------

// openMemoryDB opens an in-memory SQLite database with the users and
// admin_config tables matching the 02_database_layer schema.
// The database is automatically closed when the test completes.
func openMemoryDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("openMemoryDB: open: %v", err)
	}
	schema := `
		CREATE TABLE IF NOT EXISTS users (
			id          TEXT NOT NULL PRIMARY KEY,
			username    TEXT NOT NULL UNIQUE,
			email       TEXT NOT NULL,
			full_name   TEXT,
			role        TEXT NOT NULL DEFAULT 'user',
			status      TEXT NOT NULL DEFAULT 'active',
			provider    TEXT NOT NULL,
			provider_id TEXT NOT NULL,
			created_at  TEXT NOT NULL,
			updated_at  TEXT NOT NULL,
			UNIQUE (provider, provider_id)
		);
		CREATE TABLE IF NOT EXISTS admin_config (
			key   TEXT NOT NULL PRIMARY KEY,
			value TEXT NOT NULL
		);
	`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		t.Fatalf("openMemoryDB: schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// insertUser inserts a dummy user into the users table.
func insertUser(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO users (id, username, email, provider, provider_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"u-test-001", "testuser", "test@example.com", "github", "gh-12345",
		"2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z",
	)
	if err != nil {
		t.Fatalf("insertUser: %v", err)
	}
}

// setAdminConfig stores a key-value pair in the admin_config table.
func setAdminConfig(t *testing.T, db *sql.DB, key, value string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT OR REPLACE INTO admin_config (key, value) VALUES (?, ?)`,
		key, value,
	)
	if err != nil {
		t.Fatalf("setAdminConfig(%q): %v", key, err)
	}
}

// queryAdminConfig reads a value from the admin_config table.
// Returns empty string if the key does not exist.
func queryAdminConfig(t *testing.T, db *sql.DB, key string) string {
	t.Helper()
	var value string
	err := db.QueryRow("SELECT value FROM admin_config WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return ""
	}
	if err != nil {
		t.Fatalf("queryAdminConfig(%q): %v", key, err)
	}
	return value
}

// hexSHA256 returns the hex-encoded SHA-256 hash of s.
func hexSHA256(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// newTestLogger creates a logrus logger with a test hook for capturing entries.
func newTestLogger() (*logrus.Logger, *test.Hook) {
	logger, hook := test.NewNullLogger()
	logger.SetLevel(logrus.TraceLevel) // capture all levels
	return logger, hook
}

// fileExists reports whether the named file exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// readFileBytes reads the named file and returns its contents.
// It fails the test if the file cannot be read.
func readFileBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readFileBytes(%q): %v", path, err)
	}
	return data
}

// errReader implements io.Reader and always returns an error.
type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, errors.New("simulated entropy failure")
}

// makeParams is a convenience for constructing BootstrapParams with common defaults.
func makeParams(db *sql.DB, adminEmail string, resetToken bool, configDir string, logger *logrus.Logger) bootstrap.BootstrapParams {
	return bootstrap.BootstrapParams{
		DB:          db,
		AdminEmail:  adminEmail,
		ResetToken:  resetToken,
		ConfigDir:   configDir,
		TokenPrefix: "ak",
		Logger:      logger,
	}
}

// ---------------------------------------------------------------------------
// Tests: First Boot Detection (subtask 1.2)
// ---------------------------------------------------------------------------

// TestRun_FirstBoot_UserCountQueried verifies that Run queries
// SELECT COUNT(*) FROM users to determine boot type before any other action.
// [TS-04-1] [04-REQ-1.1]
func TestRun_FirstBoot_UserCountQueried(t *testing.T) {
	db := openMemoryDB(t)
	logger, _ := newTestLogger()
	params := makeParams(db, "admin@example.com", false, t.TempDir(), logger)

	err := bootstrap.Run(context.Background(), params)
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	// The zero-user branch was executed: verify admin_email was stored.
	v := queryAdminConfig(t, db, "admin_email")
	if v != "admin@example.com" {
		t.Errorf("admin_email = %q, want %q", v, "admin@example.com")
	}
}

// TestRun_FirstBoot_Classification verifies that Run classifies zero users
// as a first boot and executes the first boot sequence.
// [TS-04-2] [04-REQ-1.2]
func TestRun_FirstBoot_Classification(t *testing.T) {
	db := openMemoryDB(t)
	logger, _ := newTestLogger()
	params := makeParams(db, "admin@example.com", false, t.TempDir(), logger)

	err := bootstrap.Run(context.Background(), params)
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	v := queryAdminConfig(t, db, "admin_email")
	if v != "admin@example.com" {
		t.Errorf("admin_email = %q, want %q; first boot sequence did not execute", v, "admin@example.com")
	}
}

// TestRun_SubsequentBoot_Classification verifies that Run classifies the
// startup as a subsequent boot when users exist, and does not re-run the
// first boot sequence.
// [TS-04-3] [04-REQ-1.3]
func TestRun_SubsequentBoot_Classification(t *testing.T) {
	db := openMemoryDB(t)
	insertUser(t, db)
	logger, _ := newTestLogger()
	tmpDir := t.TempDir()

	// Create a known token and store its hash in admin_config.
	token := "ak_admin_" + strings.Repeat("ab", 32) // 64 hex chars
	hash := hexSHA256(token)
	setAdminConfig(t, db, "admin_token_hash", hash)

	// Set ADMIN_TOKEN env var to match.
	t.Setenv("ADMIN_TOKEN", token)

	params := makeParams(db, "", false, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	// Verify the first boot sequence did NOT run: no new token file written.
	tokenPath := filepath.Join(tmpDir, "admin_token")
	if fileExists(tokenPath) {
		t.Error("admin_token file should not be written on subsequent boot")
	}
}

// TestRun_BrokenDB_UserCountFails verifies that Run returns a non-nil error
// wrapping a database error when SELECT COUNT(*) FROM users fails.
// [TS-04-E1] [04-REQ-1.E1]
func TestRun_BrokenDB_UserCountFails(t *testing.T) {
	// Open and immediately close the DB to make all queries fail.
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.Close()

	logger, _ := newTestLogger()
	params := makeParams(db, "admin@example.com", false, t.TempDir(), logger)

	err = bootstrap.Run(context.Background(), params)
	if err == nil {
		t.Fatal("expected non-nil error for broken/closed database")
	}
}

// ---------------------------------------------------------------------------
// Tests: Email Requirement and Storage (subtask 1.3)
// ---------------------------------------------------------------------------

// TestRun_FirstBoot_NoEmail verifies that Run returns a non-nil error
// containing 'admin-email' when first boot is detected and AdminEmail is empty.
// [TS-04-4] [04-REQ-2.1]
func TestRun_FirstBoot_NoEmail(t *testing.T) {
	db := openMemoryDB(t)
	logger, _ := newTestLogger()
	params := makeParams(db, "", false, t.TempDir(), logger)

	err := bootstrap.Run(context.Background(), params)
	if err == nil {
		t.Fatal("expected non-nil error for empty AdminEmail on first boot")
	}
	if !strings.Contains(err.Error(), "admin-email") {
		t.Errorf("error %q does not contain 'admin-email'", err.Error())
	}
}

// TestRun_FirstBoot_EmailStored verifies that Run stores the provided
// AdminEmail in admin_config under key 'admin_email' on first boot.
// [TS-04-5] [04-REQ-2.2]
func TestRun_FirstBoot_EmailStored(t *testing.T) {
	db := openMemoryDB(t)
	logger, _ := newTestLogger()
	params := makeParams(db, "admin@example.com", false, t.TempDir(), logger)

	err := bootstrap.Run(context.Background(), params)
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	v := queryAdminConfig(t, db, "admin_email")
	if v != "admin@example.com" {
		t.Errorf("admin_email = %q, want %q", v, "admin@example.com")
	}
}

// ---------------------------------------------------------------------------
// Tests: Token Generation, Hashing, and File Writing (subtask 1.4)
// ---------------------------------------------------------------------------

// TestRun_FirstBoot_TokenFormat verifies that the generated admin token
// matches the pattern ^<prefix>_admin_[0-9a-f]{64}$.
// [TS-04-6] [04-REQ-2.3]
func TestRun_FirstBoot_TokenFormat(t *testing.T) {
	db := openMemoryDB(t)
	logger, _ := newTestLogger()
	tmpDir := t.TempDir()
	params := makeParams(db, "admin@example.com", false, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	tokenPath := filepath.Join(tmpDir, "admin_token")
	content := string(readFileBytes(t, tokenPath))

	pattern := `^ak_admin_[0-9a-f]{64}$`
	matched, err := regexp.MatchString(pattern, content)
	if err != nil {
		t.Fatalf("regexp.MatchString: %v", err)
	}
	if !matched {
		t.Errorf("token %q does not match pattern %s", content, pattern)
	}
}

// TestRun_FirstBoot_TokenHashStored verifies that the SHA-256 hash of the
// generated token is stored in admin_config under key 'admin_token_hash'.
// [TS-04-7] [04-REQ-2.4]
func TestRun_FirstBoot_TokenHashStored(t *testing.T) {
	db := openMemoryDB(t)
	logger, _ := newTestLogger()
	tmpDir := t.TempDir()
	params := makeParams(db, "admin@example.com", false, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	token := string(readFileBytes(t, filepath.Join(tmpDir, "admin_token")))
	expectedHash := hexSHA256(token)
	storedHash := queryAdminConfig(t, db, "admin_token_hash")

	if storedHash != expectedHash {
		t.Errorf("stored hash = %q, want %q", storedHash, expectedHash)
	}
	if len(storedHash) != 64 {
		t.Errorf("hash length = %d, want 64", len(storedHash))
	}
	hashPattern := regexp.MustCompile(`^[0-9a-f]{64}$`)
	if !hashPattern.MatchString(storedHash) {
		t.Errorf("hash %q is not 64 lowercase hex characters", storedHash)
	}
}

// TestRun_FirstBoot_TokenFilePermissions verifies that the admin_token file
// is written with file mode 0600.
// [TS-04-8] [04-REQ-2.5]
func TestRun_FirstBoot_TokenFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping: file permissions not enforced on Windows")
	}

	db := openMemoryDB(t)
	logger, _ := newTestLogger()
	tmpDir := t.TempDir()
	params := makeParams(db, "admin@example.com", false, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	tokenPath := filepath.Join(tmpDir, "admin_token")
	fi, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatalf("os.Stat(%q): %v", tokenPath, err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %04o, want 0600", perm)
	}
}

// TestRun_FirstBoot_WarnLog verifies that Run logs the absolute path of the
// admin_token file at warn level after writing it on first boot.
// [TS-04-9] [04-REQ-2.6]
func TestRun_FirstBoot_WarnLog(t *testing.T) {
	db := openMemoryDB(t)
	logger, hook := newTestLogger()
	tmpDir := t.TempDir()
	params := makeParams(db, "admin@example.com", false, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	absPath, err := filepath.Abs(filepath.Join(tmpDir, "admin_token"))
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}

	var found bool
	for _, entry := range hook.AllEntries() {
		if entry.Level == logrus.WarnLevel && strings.Contains(entry.Message, absPath) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no warn-level log entry containing %q found; entries: %v",
			absPath, hook.AllEntries())
	}
}

// TestRun_FirstBoot_ReturnsNil verifies that Run returns nil when the full
// first boot sequence completes without error.
// [TS-04-10] [04-REQ-2.7]
func TestRun_FirstBoot_ReturnsNil(t *testing.T) {
	db := openMemoryDB(t)
	logger, _ := newTestLogger()
	params := makeParams(db, "admin@example.com", false, t.TempDir(), logger)

	err := bootstrap.Run(context.Background(), params)
	if err != nil {
		t.Errorf("Run() = %v, want nil", err)
	}

	// Also verify the first boot sequence actually completed: admin_email must
	// be stored for this to count as a full successful first boot.
	v := queryAdminConfig(t, db, "admin_email")
	if v != "admin@example.com" {
		t.Errorf("admin_email = %q, want %q; first boot did not fully complete", v, "admin@example.com")
	}
}

// TestRun_FirstBoot_NoTrailingNewline verifies that the admin_token file
// contains the raw plaintext token with no trailing newline.
// [TS-04-11] [04-REQ-2.8]
func TestRun_FirstBoot_NoTrailingNewline(t *testing.T) {
	db := openMemoryDB(t)
	logger, _ := newTestLogger()
	tmpDir := t.TempDir()
	params := makeParams(db, "admin@example.com", false, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	content := readFileBytes(t, filepath.Join(tmpDir, "admin_token"))
	if len(content) == 0 {
		t.Fatal("admin_token file is empty")
	}
	if content[len(content)-1] == '\n' {
		t.Error("admin_token file has trailing newline")
	}
}

// ---------------------------------------------------------------------------
// Tests: Error Paths (subtask 1.5)
// ---------------------------------------------------------------------------

// TestRun_FirstBoot_RandFailure verifies that Run returns a non-nil error
// when crypto/rand fails, and no token or hash is written.
// [TS-04-E2] [04-REQ-2.E1]
func TestRun_FirstBoot_RandFailure(t *testing.T) {
	db := openMemoryDB(t)
	logger, _ := newTestLogger()
	tmpDir := t.TempDir()

	// Inject a broken rand reader.
	bootstrap.SetRandReader(errReader{})
	defer bootstrap.ResetRandReader()

	params := makeParams(db, "admin@example.com", false, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err == nil {
		t.Fatal("expected non-nil error when rand reader fails")
	}

	// Verify no token hash was stored.
	if v := queryAdminConfig(t, db, "admin_token_hash"); v != "" {
		t.Errorf("admin_token_hash should not be stored on rand failure, got %q", v)
	}

	// Verify no token file was written.
	tokenPath := filepath.Join(tmpDir, "admin_token")
	if fileExists(tokenPath) {
		t.Error("admin_token file should not exist after rand failure")
	}
}

// TestRun_FirstBoot_FileWriteFailure verifies that Run returns a non-nil error
// wrapping the OS error when writing the admin_token file fails.
// [TS-04-E3] [04-REQ-2.E2]
func TestRun_FirstBoot_FileWriteFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping: file permission behavior differs on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("skipping: test requires non-root user")
	}

	db := openMemoryDB(t)
	logger, _ := newTestLogger()

	// Create a read-only directory so file writes fail.
	readOnlyDir := t.TempDir()
	if err := os.Chmod(readOnlyDir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() {
		os.Chmod(readOnlyDir, 0o755) // restore for cleanup
	})

	params := makeParams(db, "admin@example.com", false, readOnlyDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err == nil {
		t.Fatal("expected non-nil error for read-only ConfigDir")
	}
}

// TestRun_FirstBoot_DBWriteFailure verifies that Run returns a non-nil error
// when storing admin_email or admin_token_hash in admin_config fails.
// [TS-04-E4] [04-REQ-2.E3]
func TestRun_FirstBoot_DBWriteFailure(t *testing.T) {
	db := openMemoryDB(t)
	logger, _ := newTestLogger()
	tmpDir := t.TempDir()

	// Drop admin_config table so INSERT fails.
	if _, err := db.Exec("DROP TABLE admin_config"); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	params := makeParams(db, "admin@example.com", false, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err == nil {
		t.Fatal("expected non-nil error when admin_config table is missing")
	}
}

// TestRun_NeverCallsOsExit verifies that Run never calls os.Exit or log.Fatal;
// all errors are returned to the caller as non-nil error values.
// [TS-04-E5] [04-REQ-2.E4]
func TestRun_NeverCallsOsExit(t *testing.T) {
	// Part 1: Run returns an error instead of calling os.Exit.
	// If Run called os.Exit, this test would terminate and never reach the
	// assertions below.
	db := openMemoryDB(t)
	logger, _ := newTestLogger()
	params := makeParams(db, "", false, t.TempDir(), logger)

	err := bootstrap.Run(context.Background(), params)
	if err == nil {
		t.Fatal("expected non-nil error for empty AdminEmail on first boot")
	}

	// Part 2: Static analysis of the source file.
	source, readErr := os.ReadFile("bootstrap.go")
	if readErr != nil {
		t.Fatalf("failed to read bootstrap.go: %v", readErr)
	}
	src := string(source)
	if strings.Contains(src, "os.Exit") {
		t.Error("bootstrap.go contains os.Exit call")
	}
	if strings.Contains(src, "log.Fatal") {
		t.Error("bootstrap.go contains log.Fatal call")
	}
}

// ---------------------------------------------------------------------------
// Tests: Subsequent Boot – File-Presence Guard and Env Var (subtask 2.1)
// ---------------------------------------------------------------------------

// TestRun_SubsequentBoot_FileExists verifies that Run returns an error
// containing 'admin_token file exists' when the admin_token file is present
// on disk during a subsequent boot.
// [TS-04-17] [04-REQ-4.1]
func TestRun_SubsequentBoot_FileExists(t *testing.T) {
	db := openMemoryDB(t)
	insertUser(t, db)
	tmpDir := t.TempDir()
	logger, _ := newTestLogger()

	// Write an admin_token file so the file-presence guard should trigger.
	token := "ak_admin_" + strings.Repeat("ab", 32)
	tokenPath := filepath.Join(tmpDir, "admin_token")
	if err := os.WriteFile(tokenPath, []byte(token), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Store the matching hash in admin_config.
	setAdminConfig(t, db, "admin_token_hash", hexSHA256(token))

	params := makeParams(db, "", false, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err == nil {
		t.Fatal("expected non-nil error when admin_token file exists on subsequent boot")
	}
	if !strings.Contains(err.Error(), "admin_token file exists") {
		t.Errorf("error %q does not contain 'admin_token file exists'", err.Error())
	}
}

// TestRun_SubsequentBoot_NoEnvVar verifies that Run returns an error
// containing 'ADMIN_TOKEN environment variable is required' when
// ADMIN_TOKEN is empty or unset on a subsequent boot.
// [TS-04-18] [04-REQ-4.2]
func TestRun_SubsequentBoot_NoEnvVar(t *testing.T) {
	db := openMemoryDB(t)
	insertUser(t, db)
	tmpDir := t.TempDir()
	logger, _ := newTestLogger()

	// Ensure admin_token_hash is stored so we get past the missing-hash check.
	setAdminConfig(t, db, "admin_token_hash", "somehash")

	// Unset ADMIN_TOKEN.
	t.Setenv("ADMIN_TOKEN", "")

	params := makeParams(db, "", false, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err == nil {
		t.Fatal("expected non-nil error when ADMIN_TOKEN is unset on subsequent boot")
	}
	if !strings.Contains(err.Error(), "ADMIN_TOKEN environment variable is required") {
		t.Errorf("error %q does not contain 'ADMIN_TOKEN environment variable is required'", err.Error())
	}
}

// TestRun_SubsequentBoot_HashComparison verifies that Run computes the
// SHA-256 of ADMIN_TOKEN and compares it against the stored hash,
// returning nil when they match.
// [TS-04-19] [04-REQ-4.3]
func TestRun_SubsequentBoot_HashComparison(t *testing.T) {
	db := openMemoryDB(t)
	insertUser(t, db)
	tmpDir := t.TempDir()
	logger, _ := newTestLogger()

	// Use a known token and store its correct SHA-256 hash.
	token := "ak_admin_" + strings.Repeat("ab", 32)
	setAdminConfig(t, db, "admin_token_hash", hexSHA256(token))

	t.Setenv("ADMIN_TOKEN", token)

	params := makeParams(db, "", false, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err != nil {
		t.Errorf("Run() = %v, want nil (hash comparison should succeed)", err)
	}
}

// ---------------------------------------------------------------------------
// Tests: Subsequent Boot – Hash Mismatch, Email Ignore, Success, Missing Hash
// (subtask 2.2)
// ---------------------------------------------------------------------------

// TestRun_SubsequentBoot_WrongToken verifies that Run returns an error
// containing 'does not match' when the SHA-256 hash of ADMIN_TOKEN does
// not match the stored admin_token_hash.
// [TS-04-20] [04-REQ-4.4]
func TestRun_SubsequentBoot_WrongToken(t *testing.T) {
	db := openMemoryDB(t)
	insertUser(t, db)
	tmpDir := t.TempDir()
	logger, _ := newTestLogger()

	// Store the hash of the "correct" token.
	correctToken := "ak_admin_" + strings.Repeat("ab", 32)
	setAdminConfig(t, db, "admin_token_hash", hexSHA256(correctToken))

	// Set ADMIN_TOKEN to a different (wrong) token.
	wrongToken := "ak_admin_" + strings.Repeat("cd", 32)
	if correctToken == wrongToken {
		t.Fatal("test setup: tokens must differ")
	}
	t.Setenv("ADMIN_TOKEN", wrongToken)

	params := makeParams(db, "", false, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err == nil {
		t.Fatal("expected non-nil error when ADMIN_TOKEN does not match stored hash")
	}
	if !strings.Contains(err.Error(), "does not match") {
		t.Errorf("error %q does not contain 'does not match'", err.Error())
	}
}

// TestRun_SubsequentBoot_IgnoresAdminEmail verifies that Run silently
// ignores AdminEmail on a subsequent boot: it returns nil, does not
// modify the stored admin_email, and does not log a warning about it.
// [TS-04-21] [04-REQ-4.5]
func TestRun_SubsequentBoot_IgnoresAdminEmail(t *testing.T) {
	db := openMemoryDB(t)
	insertUser(t, db)
	tmpDir := t.TempDir()
	logger, hook := newTestLogger()

	// Set up a valid subsequent boot with existing admin_email.
	token := "ak_admin_" + strings.Repeat("ab", 32)
	setAdminConfig(t, db, "admin_token_hash", hexSHA256(token))
	setAdminConfig(t, db, "admin_email", "original@example.com")

	t.Setenv("ADMIN_TOKEN", token)

	// Pass a different AdminEmail — should be silently ignored.
	params := makeParams(db, "new@example.com", false, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	// Verify admin_email was NOT changed.
	v := queryAdminConfig(t, db, "admin_email")
	if v != "original@example.com" {
		t.Errorf("admin_email = %q, want %q (should not be modified on subsequent boot)",
			v, "original@example.com")
	}

	// Verify no warning logged about admin_email.
	for _, entry := range hook.AllEntries() {
		if entry.Level == logrus.WarnLevel && strings.Contains(entry.Message, "admin_email") {
			t.Errorf("unexpected warn-level log about admin_email: %q", entry.Message)
		}
	}
}

// TestRun_SubsequentBoot_Success verifies that Run returns nil when the
// subsequent boot hash comparison succeeds.
// [TS-04-22] [04-REQ-4.6]
func TestRun_SubsequentBoot_Success(t *testing.T) {
	db := openMemoryDB(t)
	insertUser(t, db)
	tmpDir := t.TempDir()
	logger, _ := newTestLogger()

	token := "ak_admin_" + strings.Repeat("ab", 32)
	setAdminConfig(t, db, "admin_token_hash", hexSHA256(token))

	t.Setenv("ADMIN_TOKEN", token)

	params := makeParams(db, "", false, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err != nil {
		t.Errorf("Run() = %v, want nil", err)
	}
}

// TestRun_SubsequentBoot_NoStoredHash verifies that Run returns an error
// instructing the operator to run with --reset-admin-token when
// admin_token_hash is absent from admin_config on a subsequent boot.
// [TS-04-23] [04-REQ-4.7]
func TestRun_SubsequentBoot_NoStoredHash(t *testing.T) {
	db := openMemoryDB(t)
	insertUser(t, db)
	tmpDir := t.TempDir()
	logger, _ := newTestLogger()

	// Do NOT set admin_token_hash in admin_config.
	t.Setenv("ADMIN_TOKEN", "ak_admin_whatever")

	params := makeParams(db, "", false, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err == nil {
		t.Fatal("expected non-nil error when admin_token_hash is absent")
	}
	if !strings.Contains(err.Error(), "no admin token hash found") {
		t.Errorf("error %q does not contain 'no admin token hash found'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Tests: Subsequent Boot Error Paths (subtask 2.3)
// ---------------------------------------------------------------------------

// TestRun_SubsequentBoot_DBReadFailure verifies that Run returns a non-nil
// error wrapping the database error when reading admin_token_hash from
// admin_config fails during a subsequent boot.
// [TS-04-E7] [04-REQ-4.E1]
func TestRun_SubsequentBoot_DBReadFailure(t *testing.T) {
	db := openMemoryDB(t)
	insertUser(t, db)
	tmpDir := t.TempDir()
	logger, _ := newTestLogger()

	// Drop admin_config table so reads from it fail.
	if _, err := db.Exec("DROP TABLE admin_config"); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	t.Setenv("ADMIN_TOKEN", "some_token")

	params := makeParams(db, "", false, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err == nil {
		t.Fatal("expected non-nil error when admin_config read fails")
	}
}

// TestConstantTimeCompare_UsedInSource verifies that the token hash
// comparison uses subtle.ConstantTimeCompare to prevent timing
// side-channel attacks, via both source inspection and functional checks.
// [TS-04-E8] [04-REQ-4.E2]
func TestConstantTimeCompare_UsedInSource(t *testing.T) {
	// Part 1: Static analysis — source must import and use subtle.ConstantTimeCompare.
	source, err := os.ReadFile("bootstrap.go")
	if err != nil {
		t.Fatalf("failed to read bootstrap.go: %v", err)
	}
	src := string(source)

	if !strings.Contains(src, "subtle.ConstantTimeCompare") {
		t.Error("bootstrap.go does not contain 'subtle.ConstantTimeCompare'")
	}
	if !strings.Contains(src, `"crypto/subtle"`) {
		t.Error("bootstrap.go does not import \"crypto/subtle\"")
	}

	// Part 2: Functional checks via Run — same hash compares equal,
	// different hashes compare unequal.
	t.Run("same_hash_matches", func(t *testing.T) {
		db := openMemoryDB(t)
		insertUser(t, db)
		tmpDir := t.TempDir()
		logger, _ := newTestLogger()

		token := "ak_admin_" + strings.Repeat("ee", 32)
		setAdminConfig(t, db, "admin_token_hash", hexSHA256(token))
		t.Setenv("ADMIN_TOKEN", token)

		params := makeParams(db, "", false, tmpDir, logger)
		if err := bootstrap.Run(context.Background(), params); err != nil {
			t.Errorf("Run() = %v, want nil (same hash should match)", err)
		}
	})

	t.Run("different_hash_rejects", func(t *testing.T) {
		db := openMemoryDB(t)
		insertUser(t, db)
		tmpDir := t.TempDir()
		logger, _ := newTestLogger()

		correctToken := "ak_admin_" + strings.Repeat("ee", 32)
		wrongToken := "ak_admin_" + strings.Repeat("ff", 32)
		setAdminConfig(t, db, "admin_token_hash", hexSHA256(correctToken))
		t.Setenv("ADMIN_TOKEN", wrongToken)

		params := makeParams(db, "", false, tmpDir, logger)
		if err := bootstrap.Run(context.Background(), params); err == nil {
			t.Error("expected non-nil error when hashes differ")
		}
	})
}

// ---------------------------------------------------------------------------
// Tests: Token Rotation Happy Path (subtask 2.4)
// ---------------------------------------------------------------------------

// TestRun_ResetToken_GeneratesToken verifies that Run generates a new
// admin token when ResetToken is true, regardless of user count.
// [TS-04-24] [04-REQ-5.1]
func TestRun_ResetToken_GeneratesToken(t *testing.T) {
	db := openMemoryDB(t)
	insertUser(t, db) // subsequent boot context
	tmpDir := t.TempDir()
	logger, _ := newTestLogger()

	params := makeParams(db, "", true, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	// Verify the admin_token file was written with valid format.
	tokenPath := filepath.Join(tmpDir, "admin_token")
	content := string(readFileBytes(t, tokenPath))

	pattern := `^ak_admin_[0-9a-f]{64}$`
	matched, err := regexp.MatchString(pattern, content)
	if err != nil {
		t.Fatalf("regexp.MatchString: %v", err)
	}
	if !matched {
		t.Errorf("token %q does not match pattern %s", content, pattern)
	}
}

// TestRun_ResetToken_InvalidatesOldHash verifies that Run overwrites
// admin_token_hash in admin_config with the SHA-256 hash of the new
// token during token rotation, invalidating the old token.
// [TS-04-25] [04-REQ-5.2]
func TestRun_ResetToken_InvalidatesOldHash(t *testing.T) {
	db := openMemoryDB(t)
	insertUser(t, db)
	tmpDir := t.TempDir()
	logger, _ := newTestLogger()

	// Store an old hash.
	oldToken := "ak_admin_" + strings.Repeat("ab", 32)
	oldHash := hexSHA256(oldToken)
	setAdminConfig(t, db, "admin_token_hash", oldHash)

	params := makeParams(db, "", true, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	// Read the new token from the file.
	newToken := string(readFileBytes(t, filepath.Join(tmpDir, "admin_token")))

	// Verify the stored hash matches the new token.
	newHash := queryAdminConfig(t, db, "admin_token_hash")
	expectedHash := hexSHA256(newToken)
	if newHash != expectedHash {
		t.Errorf("stored hash = %q, want SHA-256(%q) = %q", newHash, newToken, expectedHash)
	}

	// Verify the old hash was invalidated.
	if newHash == oldHash {
		t.Error("admin_token_hash was not updated during rotation (still equals old hash)")
	}
}

// TestRun_ResetToken_FilePermissions verifies that Run writes the admin_token
// file with mode 0600 during token rotation.
// [TS-04-26] [04-REQ-5.3]
func TestRun_ResetToken_FilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping: file permissions not enforced on Windows")
	}

	db := openMemoryDB(t)
	insertUser(t, db)
	tmpDir := t.TempDir()
	logger, _ := newTestLogger()

	params := makeParams(db, "", true, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	tokenPath := filepath.Join(tmpDir, "admin_token")
	fi, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatalf("os.Stat(%q): %v", tokenPath, err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %04o, want 0600", perm)
	}

	// Also verify the content is a valid token format.
	content := string(readFileBytes(t, tokenPath))
	pattern := `^ak_admin_[0-9a-f]{64}$`
	matched, err := regexp.MatchString(pattern, content)
	if err != nil {
		t.Fatalf("regexp.MatchString: %v", err)
	}
	if !matched {
		t.Errorf("token %q does not match pattern %s", content, pattern)
	}
}

// TestRun_ResetToken_WarnLog verifies that Run logs the admin_token file
// path at warn level after writing the token file during token rotation.
// [TS-04-27] [04-REQ-5.4]
func TestRun_ResetToken_WarnLog(t *testing.T) {
	db := openMemoryDB(t)
	insertUser(t, db)
	tmpDir := t.TempDir()
	logger, hook := newTestLogger()

	params := makeParams(db, "", true, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	absPath, err := filepath.Abs(filepath.Join(tmpDir, "admin_token"))
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}

	var found bool
	for _, entry := range hook.AllEntries() {
		if entry.Level == logrus.WarnLevel && strings.Contains(entry.Message, absPath) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no warn-level log entry containing %q found; entries: %v",
			absPath, hook.AllEntries())
	}
}

// TestRun_ResetToken_SkipsGuards verifies that Run skips the file-presence
// guard and ADMIN_TOKEN env var check during a reset boot, returning nil
// even when ADMIN_TOKEN is unset.
// [TS-04-28] [04-REQ-5.5]
func TestRun_ResetToken_SkipsGuards(t *testing.T) {
	db := openMemoryDB(t)
	insertUser(t, db)
	tmpDir := t.TempDir()
	logger, _ := newTestLogger()

	// Deliberately unset ADMIN_TOKEN — should not matter during reset.
	t.Setenv("ADMIN_TOKEN", "")

	params := makeParams(db, "", true, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err != nil {
		t.Errorf("Run() = %v, want nil (reset boot should skip guards)", err)
	}
}

// ---------------------------------------------------------------------------
// Tests: Token Rotation Next-Restart Behavior and Error Paths (subtask 2.5)
// ---------------------------------------------------------------------------

// TestRun_ResetToken_NextRestart_FileGuardFires verifies that on the next
// restart after token rotation (without --reset-admin-token), the
// file-presence guard and ADMIN_TOKEN validation apply normally.
// [TS-04-29] [04-REQ-5.6]
func TestRun_ResetToken_NextRestart_FileGuardFires(t *testing.T) {
	db := openMemoryDB(t)
	insertUser(t, db)
	tmpDir := t.TempDir()
	logger, _ := newTestLogger()

	// Simulate post-rotation state: token file left on disk.
	token := "ak_admin_" + strings.Repeat("ab", 32)
	tokenPath := filepath.Join(tmpDir, "admin_token")
	if err := os.WriteFile(tokenPath, []byte(token), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	setAdminConfig(t, db, "admin_token_hash", hexSHA256(token))

	// Next restart: ResetToken=false, ADMIN_TOKEN unset.
	t.Setenv("ADMIN_TOKEN", "")

	params := makeParams(db, "", false, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err == nil {
		t.Fatal("expected non-nil error because admin_token file is present (file-presence guard)")
	}
	if !strings.Contains(err.Error(), "admin_token file exists") {
		t.Errorf("error %q does not contain 'admin_token file exists'", err.Error())
	}
}

// TestRun_ResetToken_RandFailure verifies that Run returns a non-nil error
// wrapping the crypto/rand error during token rotation, without modifying
// the old admin_token_hash.
// [TS-04-E9] [04-REQ-5.E1]
func TestRun_ResetToken_RandFailure(t *testing.T) {
	db := openMemoryDB(t)
	insertUser(t, db)
	tmpDir := t.TempDir()
	logger, _ := newTestLogger()

	// Store an existing hash that should remain unchanged.
	oldToken := "ak_admin_" + strings.Repeat("ab", 32)
	oldHash := hexSHA256(oldToken)
	setAdminConfig(t, db, "admin_token_hash", oldHash)

	// Inject a broken rand reader.
	bootstrap.SetRandReader(errReader{})
	defer bootstrap.ResetRandReader()

	params := makeParams(db, "", true, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err == nil {
		t.Fatal("expected non-nil error when rand reader fails during rotation")
	}

	// Verify the old hash was NOT modified.
	storedHash := queryAdminConfig(t, db, "admin_token_hash")
	if storedHash != oldHash {
		t.Errorf("admin_token_hash = %q, want %q (should be unchanged after rand failure)",
			storedHash, oldHash)
	}
}

// TestRun_ResetToken_FileWriteFailure verifies that Run returns a non-nil
// error wrapping the OS error when writing the admin_token file fails
// during token rotation.
// [TS-04-E10] [04-REQ-5.E2]
func TestRun_ResetToken_FileWriteFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping: file permission behavior differs on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("skipping: test requires non-root user")
	}

	db := openMemoryDB(t)
	insertUser(t, db)
	logger, _ := newTestLogger()

	// Create a read-only directory so file writes fail.
	readOnlyDir := t.TempDir()
	if err := os.Chmod(readOnlyDir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() {
		os.Chmod(readOnlyDir, 0o755) // restore for cleanup
	})

	params := makeParams(db, "", true, readOnlyDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err == nil {
		t.Fatal("expected non-nil error for read-only ConfigDir during rotation")
	}
}

// TestRun_ResetToken_DBWriteFailure verifies that Run returns a non-nil
// error wrapping the database error when updating admin_token_hash fails
// during token rotation.
// [TS-04-E11] [04-REQ-5.E3]
func TestRun_ResetToken_DBWriteFailure(t *testing.T) {
	db := openMemoryDB(t)
	insertUser(t, db)
	tmpDir := t.TempDir()
	logger, _ := newTestLogger()

	// Drop admin_config table so writes to it fail during rotation.
	if _, err := db.Exec("DROP TABLE admin_config"); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	params := makeParams(db, "", true, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err == nil {
		t.Fatal("expected non-nil error when admin_config write fails during rotation")
	}
}

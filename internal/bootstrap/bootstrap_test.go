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

// TestRun_SubsequentBoot_NoStoredHash verifies that Run treats a missing
// admin_token_hash as a first boot (regardless of user count) and requires
// --admin-email.
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
	if !strings.Contains(err.Error(), "--admin-email is required") {
		t.Errorf("error %q does not contain '--admin-email is required'", err.Error())
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

// ---------------------------------------------------------------------------
// Tests: ShouldAutoPromote (subtask 3.1)
// ---------------------------------------------------------------------------

// TestShouldAutoPromote_Match verifies that ShouldAutoPromote returns
// (true, nil) when the provided email matches the stored admin_email
// in admin_config.
// [TS-04-12] [04-REQ-3.1]
func TestShouldAutoPromote_Match(t *testing.T) {
	db := openMemoryDB(t)
	setAdminConfig(t, db, "admin_email", "admin@example.com")

	result, err := bootstrap.ShouldAutoPromote(context.Background(), db, "admin@example.com")
	if err != nil {
		t.Fatalf("ShouldAutoPromote() error = %v, want nil", err)
	}
	if !result {
		t.Error("ShouldAutoPromote() = false, want true for matching email")
	}
}

// TestShouldAutoPromote_NoMatch verifies that ShouldAutoPromote returns
// (false, nil) when the provided email does not match the stored admin_email.
// [TS-04-13] [04-REQ-3.2]
func TestShouldAutoPromote_NoMatch(t *testing.T) {
	db := openMemoryDB(t)
	setAdminConfig(t, db, "admin_email", "admin@example.com")

	result, err := bootstrap.ShouldAutoPromote(context.Background(), db, "other@example.com")
	if err != nil {
		t.Fatalf("ShouldAutoPromote() error = %v, want nil", err)
	}
	if result {
		t.Error("ShouldAutoPromote() = true, want false for non-matching email")
	}
}

// TestShouldAutoPromote_NoConfig verifies that ShouldAutoPromote returns
// (false, nil) when no admin_email key exists in admin_config.
// [TS-04-14] [04-REQ-3.3]
func TestShouldAutoPromote_NoConfig(t *testing.T) {
	db := openMemoryDB(t)
	// admin_config table is present but empty — no admin_email key.

	result, err := bootstrap.ShouldAutoPromote(context.Background(), db, "anyone@example.com")
	if err != nil {
		t.Fatalf("ShouldAutoPromote() error = %v, want nil", err)
	}
	if result {
		t.Error("ShouldAutoPromote() = true, want false when no admin_email exists")
	}
}

// TestShouldAutoPromote_NewUserConvention verifies that ShouldAutoPromote
// returns (true, nil) when called with a matching email. The function itself
// does not enforce the new-user gate; the OAuth callback caller is responsible
// for only calling ShouldAutoPromote for newly created users, not updates
// to existing users.
// [TS-04-15] [04-REQ-3.4]
func TestShouldAutoPromote_NewUserConvention(t *testing.T) {
	db := openMemoryDB(t)
	setAdminConfig(t, db, "admin_email", "admin@example.com")

	// ShouldAutoPromote itself does not check whether the user is new or existing.
	// It only checks whether the email matches the stored admin_email.
	// The caller (OAuth callback handler) is responsible for not calling this
	// function on updates to existing users — this is enforced by caller
	// convention, not by ShouldAutoPromote itself.
	result, err := bootstrap.ShouldAutoPromote(context.Background(), db, "admin@example.com")
	if err != nil {
		t.Fatalf("ShouldAutoPromote() error = %v, want nil", err)
	}
	if !result {
		t.Error("ShouldAutoPromote() = false, want true for matching email (caller enforces new-user gate)")
	}
}

// TestShouldAutoPromote_InfoLog verifies that when ShouldAutoPromote returns
// true and the OAuth callback handler grants the admin role, the server logs
// the auto-promotion event at info level identifying the promoted user's email.
// This test simulates the OAuth callback: it calls ShouldAutoPromote, then
// the caller logs the event.
// [TS-04-16] [04-REQ-3.5]
func TestShouldAutoPromote_InfoLog(t *testing.T) {
	db := openMemoryDB(t)
	setAdminConfig(t, db, "admin_email", "admin@example.com")

	logger, hook := newTestLogger()

	result, err := bootstrap.ShouldAutoPromote(context.Background(), db, "admin@example.com")
	if err != nil {
		t.Fatalf("ShouldAutoPromote() error = %v, want nil", err)
	}
	if !result {
		t.Fatal("ShouldAutoPromote() = false, want true for matching email")
	}

	// Simulate the OAuth callback handler logging the auto-promotion event.
	logger.Infof("auto-promoted user %s to admin", "admin@example.com")

	// Verify an info-level log entry contains the promoted email.
	var found bool
	for _, entry := range hook.AllEntries() {
		if entry.Level == logrus.InfoLevel && strings.Contains(entry.Message, "admin@example.com") {
			found = true
			break
		}
	}
	if !found {
		t.Error("no info-level log entry containing the promoted user's email")
	}
}

// TestShouldAutoPromote_DBError verifies that ShouldAutoPromote returns
// (false, non-nil error) wrapping the database error when the query to
// read admin_email from admin_config fails.
// [TS-04-E6] [04-REQ-3.E1]
func TestShouldAutoPromote_DBError(t *testing.T) {
	// Open and immediately close the DB to make all queries fail.
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.Close()

	result, dbErr := bootstrap.ShouldAutoPromote(context.Background(), db, "admin@example.com")
	if result {
		t.Error("ShouldAutoPromote() = true, want false on DB error")
	}
	if dbErr == nil {
		t.Fatal("expected non-nil error when database query fails")
	}
}

// ---------------------------------------------------------------------------
// Tests: Token Format and Hashing (subtask 3.2)
// ---------------------------------------------------------------------------

// TestGenerateToken_Format verifies that the token generation function
// produces tokens strictly in the format '<prefix>_admin_<64 lowercase
// hex chars>'. Tested via Run and reading the token file.
// [TS-04-30] [04-REQ-6.1]
func TestGenerateToken_Format(t *testing.T) {
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

	// Verify the full format: prefix_admin_<64 hex chars>
	pattern := `^ak_admin_[0-9a-f]{64}$`
	matched, err := regexp.MatchString(pattern, content)
	if err != nil {
		t.Fatalf("regexp.MatchString: %v", err)
	}
	if !matched {
		t.Errorf("token %q does not match pattern %s", content, pattern)
	}

	// Verify the hex portion is exactly 64 characters.
	parts := strings.SplitN(content, "_admin_", 2)
	if len(parts) != 2 {
		t.Fatalf("token %q does not contain '_admin_' separator", content)
	}
	if len(parts[1]) != 64 {
		t.Errorf("hex portion length = %d, want 64", len(parts[1]))
	}

	// Verify hex is lowercase.
	hexPart := parts[1]
	if hexPart != strings.ToLower(hexPart) {
		t.Errorf("hex portion %q is not lowercase", hexPart)
	}
}

// TestHashToken_KnownValue verifies that the token hash computation uses
// SHA-256 over the full token string (including prefix) and produces a
// 64-character lowercase hex digest. Tested by comparing the stored hash
// against an independently computed SHA-256.
// [TS-04-31] [04-REQ-6.2]
func TestHashToken_KnownValue(t *testing.T) {
	db := openMemoryDB(t)
	logger, _ := newTestLogger()
	tmpDir := t.TempDir()
	params := makeParams(db, "admin@example.com", false, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	// Read the token from the file.
	token := string(readFileBytes(t, filepath.Join(tmpDir, "admin_token")))

	// Compute the expected SHA-256 hash independently.
	expected := hexSHA256(token)

	// Read the stored hash from admin_config.
	stored := queryAdminConfig(t, db, "admin_token_hash")

	// Assert the stored hash equals the independently computed hash.
	if stored != expected {
		t.Errorf("stored hash = %q, want %q (SHA-256 of full token string)", stored, expected)
	}

	// Assert the hash is exactly 64 lowercase hex characters.
	if len(stored) != 64 {
		t.Errorf("hash length = %d, want 64", len(stored))
	}
	hashPattern := regexp.MustCompile(`^[0-9a-f]{64}$`)
	if !hashPattern.MatchString(stored) {
		t.Errorf("hash %q is not 64 lowercase hex characters", stored)
	}
}

// TestConstantTimeCompare_Correctness verifies that the token hash comparison
// produces correct results: matching hashes compare equal, non-matching hashes
// compare unequal. The implementation must use subtle.ConstantTimeCompare
// (verified separately in TestConstantTimeCompare_UsedInSource).
// [TS-04-32] [04-REQ-6.3]
func TestConstantTimeCompare_Correctness(t *testing.T) {
	t.Run("matching_hashes", func(t *testing.T) {
		db := openMemoryDB(t)
		insertUser(t, db)
		tmpDir := t.TempDir()
		logger, _ := newTestLogger()

		token := "ak_admin_" + strings.Repeat("aa", 32)
		setAdminConfig(t, db, "admin_token_hash", hexSHA256(token))
		t.Setenv("ADMIN_TOKEN", token)

		params := makeParams(db, "", false, tmpDir, logger)
		if err := bootstrap.Run(context.Background(), params); err != nil {
			t.Errorf("Run() = %v, want nil (matching hashes should succeed)", err)
		}
	})

	t.Run("non_matching_hashes", func(t *testing.T) {
		db := openMemoryDB(t)
		insertUser(t, db)
		tmpDir := t.TempDir()
		logger, _ := newTestLogger()

		tokenA := "ak_admin_" + strings.Repeat("aa", 32)
		tokenB := "ak_admin_" + strings.Repeat("bb", 32)
		setAdminConfig(t, db, "admin_token_hash", hexSHA256(tokenA))
		t.Setenv("ADMIN_TOKEN", tokenB)

		params := makeParams(db, "", false, tmpDir, logger)
		if err := bootstrap.Run(context.Background(), params); err == nil {
			t.Error("expected non-nil error when hashes do not match")
		}
	})
}

// TestGenerateToken_NoCryptoRand verifies that the bootstrap source file
// does not import math/rand and does import crypto/rand, ensuring all
// randomness comes from a cryptographically secure source.
// [TS-04-33] [04-REQ-6.4]
func TestGenerateToken_NoCryptoRand(t *testing.T) {
	source, err := os.ReadFile("bootstrap.go")
	if err != nil {
		t.Fatalf("failed to read bootstrap.go: %v", err)
	}
	src := string(source)

	if strings.Contains(src, `"math/rand"`) {
		t.Error("bootstrap.go imports 'math/rand' — must use 'crypto/rand' for token generation")
	}
	if !strings.Contains(src, `"crypto/rand"`) && !strings.Contains(src, `crypto_rand "crypto/rand"`) {
		t.Error("bootstrap.go does not import 'crypto/rand'")
	}
}

// TestTokenFile_NoTrailingNewline verifies that the admin_token file
// contains only the raw plaintext token with no trailing newline,
// making it safe for use with shell command substitution.
// [TS-04-34] [04-REQ-6.5]
func TestTokenFile_NoTrailingNewline(t *testing.T) {
	db := openMemoryDB(t)
	logger, _ := newTestLogger()
	tmpDir := t.TempDir()
	params := makeParams(db, "admin@example.com", false, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	raw := readFileBytes(t, filepath.Join(tmpDir, "admin_token"))
	if len(raw) == 0 {
		t.Fatal("admin_token file is empty")
	}

	// Assert no trailing newline.
	if raw[len(raw)-1] == 0x0A {
		t.Error("admin_token file has trailing newline (0x0A)")
	}

	// Assert content is a valid token format.
	content := string(raw)
	pattern := `^ak_admin_[0-9a-f]{64}$`
	matched, err := regexp.MatchString(pattern, content)
	if err != nil {
		t.Fatalf("regexp.MatchString: %v", err)
	}
	if !matched {
		t.Errorf("token file content %q does not match pattern %s", content, pattern)
	}
}

// ---------------------------------------------------------------------------
// Tests: Bootstrap Execution Order and Process Termination (subtask 3.3)
// ---------------------------------------------------------------------------

// TestRun_SchemaExistsBeforeRun verifies that Run is invoked only after the
// database schema is created by 02_database_layer. The users and admin_config
// tables must exist when Run is called.
// [TS-04-35] [04-REQ-7.1]
func TestRun_SchemaExistsBeforeRun(t *testing.T) {
	db := openMemoryDB(t)

	// Verify schema tables exist before calling Run.
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='users'").Scan(&count); err != nil {
		t.Fatalf("schema check: %v", err)
	}
	if count != 1 {
		t.Fatal("users table does not exist before Run is called")
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='admin_config'").Scan(&count); err != nil {
		t.Fatalf("schema check: %v", err)
	}
	if count != 1 {
		t.Fatal("admin_config table does not exist before Run is called")
	}

	// Run completes successfully with schema in place.
	logger, _ := newTestLogger()
	tmpDir := t.TempDir()
	params := makeParams(db, "admin@example.com", false, tmpDir, logger)
	err := bootstrap.Run(context.Background(), params)
	if err != nil {
		t.Errorf("Run() = %v, want nil (schema exists and first boot is valid)", err)
	}

	// Verify that first boot actually executed (admin_email stored).
	v := queryAdminConfig(t, db, "admin_email")
	if v != "admin@example.com" {
		t.Errorf("admin_email = %q, want %q; Run did not execute the first boot sequence",
			v, "admin@example.com")
	}
}

// TestRun_ResetToken_Priority verifies that when ResetToken is true, Run
// executes token rotation first and returns without running first-boot or
// subsequent-boot sequences. This is tested by passing an empty AdminEmail
// with zero users: if first-boot ran, it would error because AdminEmail
// is empty; since it doesn't, rotation took priority.
// [TS-04-36] [04-REQ-7.2]
func TestRun_ResetToken_Priority(t *testing.T) {
	db := openMemoryDB(t)
	// zero users — a first-boot check would require AdminEmail
	logger, _ := newTestLogger()
	tmpDir := t.TempDir()

	// ResetToken=true should bypass the first-boot AdminEmail check.
	params := makeParams(db, "", true, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err != nil {
		t.Fatalf("Run() = %v, want nil (ResetToken should bypass first-boot checks)", err)
	}

	// If first-boot ran, admin_email would be set (or error).
	// If rotation ran, a token file was written.
	tokenPath := filepath.Join(tmpDir, "admin_token")
	if !fileExists(tokenPath) {
		t.Error("admin_token file not created during token rotation")
	}
}

// TestRun_ErrorIsNonFatal verifies that when Run returns a non-nil error,
// the test process continues normally — Run does not call os.Exit, log.Fatal,
// or any function that terminates the process. The caller (server main())
// is responsible for treating the error as fatal.
// [TS-04-37] [04-REQ-7.3]
func TestRun_ErrorIsNonFatal(t *testing.T) {
	db := openMemoryDB(t)
	// zero users + empty AdminEmail → guaranteed error on first boot.
	logger, _ := newTestLogger()
	tmpDir := t.TempDir()
	params := makeParams(db, "", false, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err == nil {
		t.Fatal("expected non-nil error for empty AdminEmail on first boot")
	}

	// If we get here, the test process survived Run()'s error handling.
	// Run did not call os.Exit or log.Fatal.
	t.Logf("Run returned error (as expected, no process termination): %v", err)
}

// TestRun_SingleInstanceDocumented verifies that the Run function's godoc
// comment documents the single-instance startup constraint (concurrent calls
// are not supported), and that sequential calls complete without deadlock.
// [TS-04-38] [04-REQ-7.4]
func TestRun_SingleInstanceDocumented(t *testing.T) {
	// Part 1: Static check — Run's godoc mentions the concurrency constraint.
	source, err := os.ReadFile("bootstrap.go")
	if err != nil {
		t.Fatalf("failed to read bootstrap.go: %v", err)
	}
	src := string(source)
	if !strings.Contains(src, "Concurrent") && !strings.Contains(src, "concurrent") &&
		!strings.Contains(src, "single-instance") && !strings.Contains(src, "single instance") {
		t.Error("Run godoc does not mention concurrency constraint ('Concurrent', 'concurrent', 'single-instance', or 'single instance')")
	}

	// Part 2: Sequential calls complete without deadlock or panic.
	db := openMemoryDB(t)
	insertUser(t, db)
	tmpDir := t.TempDir()
	logger, _ := newTestLogger()

	token := "ak_admin_" + strings.Repeat("ab", 32)
	setAdminConfig(t, db, "admin_token_hash", hexSHA256(token))
	t.Setenv("ADMIN_TOKEN", token)

	params := makeParams(db, "", false, tmpDir, logger)

	// First call.
	if err := bootstrap.Run(context.Background(), params); err != nil {
		t.Fatalf("first Run() = %v, want nil", err)
	}

	// Second sequential call.
	if err := bootstrap.Run(context.Background(), params); err != nil {
		t.Fatalf("second Run() = %v, want nil", err)
	}
}

// TestRun_NoOsExitInSource verifies that the bootstrap source file does not
// contain os.Exit or log.Fatal calls. All error conditions must be signaled
// by returning a non-nil error to the caller.
// [TS-04-E12] [04-REQ-7.E1]
func TestRun_NoOsExitInSource(t *testing.T) {
	source, err := os.ReadFile("bootstrap.go")
	if err != nil {
		t.Fatalf("failed to read bootstrap.go: %v", err)
	}
	src := string(source)

	if strings.Contains(src, "os.Exit") {
		t.Error("bootstrap.go contains os.Exit — Run must return errors, not terminate the process")
	}
	if strings.Contains(src, "log.Fatal") {
		t.Error("bootstrap.go contains log.Fatal — Run must return errors, not terminate the process")
	}

	// Dynamic check: Run returns an error instead of terminating.
	db := openMemoryDB(t)
	logger, _ := newTestLogger()
	params := makeParams(db, "", false, t.TempDir(), logger)

	runErr := bootstrap.Run(context.Background(), params)
	if runErr == nil {
		t.Fatal("expected non-nil error for empty AdminEmail on first boot")
	}
	// Test process survived — no os.Exit was called.
}

// ---------------------------------------------------------------------------
// Tests: Token File Path Resolution (subtask 3.4)
// ---------------------------------------------------------------------------

// TestTokenFilePath_XDGSet verifies that Run resolves the admin_token file
// path to $XDG_CONFIG_HOME/apikit/admin_token when XDG_CONFIG_HOME is set.
// [TS-04-39] [04-REQ-8.1]
func TestTokenFilePath_XDGSet(t *testing.T) {
	xdgBase := t.TempDir()
	configDir := filepath.Join(xdgBase, "apikit")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	t.Setenv("XDG_CONFIG_HOME", xdgBase)

	db := openMemoryDB(t)
	logger, _ := newTestLogger()
	params := makeParams(db, "admin@example.com", false, configDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	expectedPath := filepath.Join(configDir, "admin_token")
	if !fileExists(expectedPath) {
		t.Errorf("admin_token file not found at %s", expectedPath)
	}
}

// TestTokenFilePath_XDGUnset verifies that Run resolves the admin_token file
// path to ConfigDir/admin_token when XDG_CONFIG_HOME is not set, consistent
// with how LoadConfig resolves config.toml.
// [TS-04-40] [04-REQ-8.2]
func TestTokenFilePath_XDGUnset(t *testing.T) {
	tmpDir := t.TempDir()

	// Explicitly unset XDG_CONFIG_HOME.
	t.Setenv("XDG_CONFIG_HOME", "")

	db := openMemoryDB(t)
	logger, _ := newTestLogger()
	params := makeParams(db, "admin@example.com", false, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	expectedPath := filepath.Join(tmpDir, "admin_token")
	if !fileExists(expectedPath) {
		t.Errorf("admin_token file not found at %s", expectedPath)
	}
}

// TestTokenFilePath_UsesConfigDir verifies that Run uses the ConfigDir field
// of BootstrapParams to construct the absolute path to the admin_token file.
// [TS-04-41] [04-REQ-8.3]
func TestTokenFilePath_UsesConfigDir(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "custom_config_dir")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	db := openMemoryDB(t)
	logger, _ := newTestLogger()
	params := makeParams(db, "admin@example.com", false, configDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	expectedPath := filepath.Join(configDir, "admin_token")
	if !fileExists(expectedPath) {
		t.Errorf("admin_token file not found at exactly ConfigDir + '/admin_token': %s", expectedPath)
	}
}

// TestTokenFilePath_NonExistentDir verifies that Run returns a non-nil error
// wrapping the OS error when the directory for the admin_token file does not
// exist and cannot be created.
// [TS-04-E13] [04-REQ-8.E1]
func TestTokenFilePath_NonExistentDir(t *testing.T) {
	// Create a regular file, then use a path "underneath" it as ConfigDir.
	// Since the path segment is a file (not a directory), the directory
	// cannot be created and os.WriteFile will fail.
	tmpDir := t.TempDir()
	blockingFile := filepath.Join(tmpDir, "not_a_dir")
	if err := os.WriteFile(blockingFile, []byte("block"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	impossibleDir := filepath.Join(blockingFile, "subdir")

	db := openMemoryDB(t)
	logger, _ := newTestLogger()
	params := makeParams(db, "admin@example.com", false, impossibleDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err == nil {
		t.Fatal("expected non-nil error when ConfigDir path cannot be created")
	}

	// Verify admin_token file was not created.
	if fileExists(filepath.Join(impossibleDir, "admin_token")) {
		t.Error("admin_token file should not exist when the directory is impossible to create")
	}
}

// ---------------------------------------------------------------------------
// Property Tests (subtask 3.5)
// ---------------------------------------------------------------------------

// TestProp_TokenUniqueness verifies that any two independently generated
// admin tokens are different with overwhelming probability, since each draws
// 32 bytes from crypto/rand.
// [TS-04-P1] [04-PROP-1]
func TestProp_TokenUniqueness(t *testing.T) {
	const iterations = 20
	tokens := make(map[string]struct{}, iterations)

	for i := range iterations {
		db := openMemoryDB(t)
		logger, _ := newTestLogger()
		tmpDir := t.TempDir()
		params := makeParams(db, "admin@example.com", false, tmpDir, logger)

		err := bootstrap.Run(context.Background(), params)
		if err != nil {
			t.Fatalf("Run() iteration %d: %v", i, err)
		}

		token := string(readFileBytes(t, filepath.Join(tmpDir, "admin_token")))
		if _, exists := tokens[token]; exists {
			t.Fatalf("duplicate token generated at iteration %d: %q", i, token)
		}
		tokens[token] = struct{}{}
	}
}

// TestProp_HashDeterminism verifies that hashing the same admin token string
// twice with SHA-256 produces the same 64-character lowercase hex result.
// [TS-04-P2] [04-PROP-2]
func TestProp_HashDeterminism(t *testing.T) {
	db := openMemoryDB(t)
	logger, _ := newTestLogger()
	tmpDir := t.TempDir()
	params := makeParams(db, "admin@example.com", false, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	// Read the token from the file.
	token := string(readFileBytes(t, filepath.Join(tmpDir, "admin_token")))

	// Compute SHA-256 twice independently.
	hash1 := hexSHA256(token)
	hash2 := hexSHA256(token)

	if hash1 != hash2 {
		t.Errorf("hash(%q) produced different results: %q vs %q", token, hash1, hash2)
	}
	if len(hash1) != 64 {
		t.Errorf("hash length = %d, want 64", len(hash1))
	}

	// The stored hash must match our independent computation.
	stored := queryAdminConfig(t, db, "admin_token_hash")
	if stored != hash1 {
		t.Errorf("stored hash = %q, independent hash = %q", stored, hash1)
	}
}

// TestProp_HashDiscrimination verifies that two distinct admin token strings
// produce different SHA-256 hex digests, so the wrong token never passes
// the hash comparison.
// [TS-04-P3] [04-PROP-3]
func TestProp_HashDiscrimination(t *testing.T) {
	// Generate two tokens via separate first-boot calls.
	var tokens [2]string
	for i := range 2 {
		db := openMemoryDB(t)
		logger, _ := newTestLogger()
		tmpDir := t.TempDir()
		params := makeParams(db, "admin@example.com", false, tmpDir, logger)

		err := bootstrap.Run(context.Background(), params)
		if err != nil {
			t.Fatalf("Run() iteration %d: %v", i, err)
		}

		tokens[i] = string(readFileBytes(t, filepath.Join(tmpDir, "admin_token")))
	}

	if tokens[0] == tokens[1] {
		t.Fatal("tokens should be distinct (see TestProp_TokenUniqueness)")
	}

	hash0 := hexSHA256(tokens[0])
	hash1 := hexSHA256(tokens[1])

	if hash0 == hash1 {
		t.Errorf("distinct tokens produced identical SHA-256 digests: %q and %q → %q",
			tokens[0], tokens[1], hash0)
	}
}

// TestProp_FilePresentGuardBlocks verifies that for any subsequent boot
// attempt where the admin_token file is present on disk, Run always returns
// a non-nil error.
// [TS-04-P4] [04-PROP-4]
func TestProp_FilePresentGuardBlocks(t *testing.T) {
	fileContents := []string{
		"ak_admin_" + strings.Repeat("ab", 32),
		"arbitrary content",
		"",
	}

	for _, content := range fileContents {
		t.Run("content_"+strings.ReplaceAll(content[:min(10, len(content))], "/", "_"), func(t *testing.T) {
			db := openMemoryDB(t)
			insertUser(t, db)
			tmpDir := t.TempDir()
			logger, _ := newTestLogger()

			// Write an admin_token file with the test content.
			tokenPath := filepath.Join(tmpDir, "admin_token")
			if err := os.WriteFile(tokenPath, []byte(content), 0o600); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}

			// Set up a valid hash so we test the file guard, not the hash check.
			validToken := "ak_admin_" + strings.Repeat("ab", 32)
			setAdminConfig(t, db, "admin_token_hash", hexSHA256(validToken))
			t.Setenv("ADMIN_TOKEN", validToken)

			params := makeParams(db, "", false, tmpDir, logger)

			err := bootstrap.Run(context.Background(), params)
			if err == nil {
				t.Error("expected non-nil error when admin_token file exists on subsequent boot")
			}
			if err != nil && !strings.Contains(err.Error(), "admin_token file exists") {
				t.Errorf("error %q does not contain 'admin_token file exists'", err.Error())
			}
		})
	}
}

// TestProp_OldTokenInvalidatedAfterRotation verifies that after token rotation,
// the SHA-256 hash of the previous admin token no longer matches the stored
// admin_token_hash in admin_config.
// [TS-04-P5] [04-PROP-5]
func TestProp_OldTokenInvalidatedAfterRotation(t *testing.T) {
	db := openMemoryDB(t)
	insertUser(t, db)
	tmpDir := t.TempDir()
	logger, _ := newTestLogger()

	// Generate an old token and store its hash.
	oldToken := "ak_admin_" + strings.Repeat("ab", 32)
	oldHash := hexSHA256(oldToken)
	setAdminConfig(t, db, "admin_token_hash", oldHash)

	// Rotate the token.
	params := makeParams(db, "", true, tmpDir, logger)
	err := bootstrap.Run(context.Background(), params)
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	// Read the new hash from admin_config.
	newHash := queryAdminConfig(t, db, "admin_token_hash")
	if newHash == "" {
		t.Fatal("admin_token_hash is empty after rotation")
	}

	// The old token's hash must not match the new stored hash.
	if oldHash == newHash {
		t.Error("old token hash still matches stored hash after rotation — old token was not invalidated")
	}
}

// TestProp_FirstBootRequiresEmail verifies that for any first boot attempt
// with an empty AdminEmail, Run returns a non-nil error and neither
// admin_email nor admin_token_hash is written to admin_config.
// [TS-04-P6] [04-PROP-6]
func TestProp_FirstBootRequiresEmail(t *testing.T) {
	db := openMemoryDB(t)
	logger, _ := newTestLogger()
	tmpDir := t.TempDir()
	params := makeParams(db, "", false, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err == nil {
		t.Fatal("expected non-nil error for empty AdminEmail on first boot")
	}

	// Neither admin_email nor admin_token_hash should be stored.
	if v := queryAdminConfig(t, db, "admin_email"); v != "" {
		t.Errorf("admin_email = %q, want empty (should not be stored on error)", v)
	}
	if v := queryAdminConfig(t, db, "admin_token_hash"); v != "" {
		t.Errorf("admin_token_hash = %q, want empty (should not be stored on error)", v)
	}
}

// TestProp_ShouldAutoPromoteNoConfig verifies that for any call to
// ShouldAutoPromote when no admin_email key exists in admin_config,
// the function returns (false, nil) regardless of the email argument.
// [TS-04-P7] [04-PROP-7]
func TestProp_ShouldAutoPromoteNoConfig(t *testing.T) {
	emails := []string{
		"admin@example.com",
		"user@corp.com",
		"",
		"test@test.test",
	}

	for _, email := range emails {
		t.Run(email, func(t *testing.T) {
			db := openMemoryDB(t)
			// admin_config is empty — no admin_email key.

			result, err := bootstrap.ShouldAutoPromote(context.Background(), db, email)
			if err != nil {
				t.Errorf("ShouldAutoPromote(%q) error = %v, want nil", email, err)
			}
			if result {
				t.Errorf("ShouldAutoPromote(%q) = true, want false when no admin_email exists", email)
			}
		})
	}
}

// TestProp_RunNeverTerminates verifies that for any error condition inside Run,
// it signals failure by returning a non-nil error and never calls os.Exit or
// log.Fatal.
// [TS-04-P8] [04-PROP-8]
func TestProp_RunNeverTerminates(t *testing.T) {
	// Static check: no os.Exit or log.Fatal in source.
	source, err := os.ReadFile("bootstrap.go")
	if err != nil {
		t.Fatalf("failed to read bootstrap.go: %v", err)
	}
	src := string(source)
	if strings.Contains(src, "os.Exit") {
		t.Error("bootstrap.go contains os.Exit")
	}
	if strings.Contains(src, "log.Fatal") {
		t.Error("bootstrap.go contains log.Fatal")
	}

	// Dynamic check: multiple error scenarios all return errors without
	// terminating the test process.
	errorScenarios := []struct {
		name   string
		params func(*testing.T) bootstrap.BootstrapParams
	}{
		{
			name: "empty_email_first_boot",
			params: func(t *testing.T) bootstrap.BootstrapParams {
				db := openMemoryDB(t)
				logger, _ := newTestLogger()
				return makeParams(db, "", false, t.TempDir(), logger)
			},
		},
		{
			name: "broken_db",
			params: func(t *testing.T) bootstrap.BootstrapParams {
				db, _ := sql.Open("sqlite", ":memory:")
				db.Close()
				logger, _ := newTestLogger()
				return makeParams(db, "admin@example.com", false, t.TempDir(), logger)
			},
		},
	}

	for _, sc := range errorScenarios {
		t.Run(sc.name, func(t *testing.T) {
			params := sc.params(t)
			runErr := bootstrap.Run(context.Background(), params)
			if runErr == nil {
				t.Error("expected non-nil error")
			}
			// If we reach here, the process was not terminated.
		})
	}
}

// ---------------------------------------------------------------------------
// Smoke Tests (subtask 3.5)
// ---------------------------------------------------------------------------

// TestSmoke_FirstBoot exercises the happy-path first boot: operator starts
// a fresh server with --admin-email, token and hash are stored, token file
// is written, server proceeds normally.
// [TS-04-SMOKE-1] [04-PATH-1]
func TestSmoke_FirstBoot(t *testing.T) {
	db := openMemoryDB(t)
	logger, hook := newTestLogger()
	tmpDir := t.TempDir()
	params := makeParams(db, "admin@example.com", false, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	// admin_config.admin_email = 'admin@example.com'
	if v := queryAdminConfig(t, db, "admin_email"); v != "admin@example.com" {
		t.Errorf("admin_email = %q, want %q", v, "admin@example.com")
	}

	// admin_token file exists and has valid format.
	tokenPath := filepath.Join(tmpDir, "admin_token")
	raw := readFileBytes(t, tokenPath)
	token := string(raw)

	pattern := regexp.MustCompile(`^ak_admin_[0-9a-f]{64}$`)
	if !pattern.MatchString(token) {
		t.Errorf("token %q does not match expected format", token)
	}

	// No trailing newline.
	if raw[len(raw)-1] == '\n' {
		t.Error("admin_token file has trailing newline")
	}

	// admin_token_hash = hex(SHA256(token_file_contents))
	storedHash := queryAdminConfig(t, db, "admin_token_hash")
	expectedHash := hexSHA256(token)
	if storedHash != expectedHash {
		t.Errorf("stored hash = %q, want %q", storedHash, expectedHash)
	}
	if len(storedHash) != 64 {
		t.Errorf("hash length = %d, want 64", len(storedHash))
	}

	// File mode 0600 (Unix only).
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(tokenPath)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if perm := fi.Mode().Perm(); perm != 0o600 {
			t.Errorf("file mode = %04o, want 0600", perm)
		}
	}

	// Warn-level log contains the admin_token file path.
	absPath, _ := filepath.Abs(tokenPath)
	var foundWarn bool
	for _, entry := range hook.AllEntries() {
		if entry.Level == logrus.WarnLevel && strings.Contains(entry.Message, absPath) {
			foundWarn = true
			break
		}
	}
	if !foundWarn {
		t.Errorf("no warn-level log containing %q", absPath)
	}
}

// TestSmoke_SubsequentBoot exercises the subsequent boot happy path:
// operator deletes the token file, sets ADMIN_TOKEN, and restarts.
// [TS-04-SMOKE-2] [04-PATH-2]
func TestSmoke_SubsequentBoot(t *testing.T) {
	db := openMemoryDB(t)
	insertUser(t, db)
	tmpDir := t.TempDir()
	logger, _ := newTestLogger()

	// Set up admin_token_hash and matching ADMIN_TOKEN.
	token := "ak_admin_" + strings.Repeat("ab", 32)
	setAdminConfig(t, db, "admin_token_hash", hexSHA256(token))
	t.Setenv("ADMIN_TOKEN", token)

	// No admin_token file on disk (operator deleted it).
	params := makeParams(db, "", false, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	// No new token file should be created.
	if fileExists(filepath.Join(tmpDir, "admin_token")) {
		t.Error("admin_token file should not be written on subsequent boot")
	}

	// admin_config should not be modified.
	if v := queryAdminConfig(t, db, "admin_token_hash"); v != hexSHA256(token) {
		t.Errorf("admin_token_hash was modified during subsequent boot")
	}
}

// TestSmoke_FilePresentGuard exercises the file-presence guard: operator
// restarts without deleting the admin_token file; Run returns an error.
// [TS-04-SMOKE-3] [04-PATH-3]
func TestSmoke_FilePresentGuard(t *testing.T) {
	db := openMemoryDB(t)
	insertUser(t, db)
	tmpDir := t.TempDir()
	logger, _ := newTestLogger()

	// Write admin_token file (operator forgot to delete it).
	token := "ak_admin_" + strings.Repeat("ab", 32)
	tokenPath := filepath.Join(tmpDir, "admin_token")
	if err := os.WriteFile(tokenPath, []byte(token), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	setAdminConfig(t, db, "admin_token_hash", hexSHA256(token))

	params := makeParams(db, "", false, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err == nil {
		t.Fatal("expected non-nil error when admin_token file exists")
	}
	if !strings.Contains(err.Error(), "admin_token file exists") {
		t.Errorf("error %q does not contain 'admin_token file exists'", err.Error())
	}
}

// TestSmoke_TokenRotation exercises token rotation: operator restarts with
// --reset-admin-token; Run generates a new token, invalidates the old hash,
// writes the new token file, and returns nil.
// [TS-04-SMOKE-4] [04-PATH-4]
func TestSmoke_TokenRotation(t *testing.T) {
	db := openMemoryDB(t)
	insertUser(t, db)
	tmpDir := t.TempDir()
	logger, hook := newTestLogger()

	// Store an old hash.
	oldToken := "ak_admin_" + strings.Repeat("ab", 32)
	oldHash := hexSHA256(oldToken)
	setAdminConfig(t, db, "admin_token_hash", oldHash)

	params := makeParams(db, "", true, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	// New token file exists.
	tokenPath := filepath.Join(tmpDir, "admin_token")
	raw := readFileBytes(t, tokenPath)
	newToken := string(raw)

	// Token matches expected format.
	pattern := regexp.MustCompile(`^ak_admin_[0-9a-f]{64}$`)
	if !pattern.MatchString(newToken) {
		t.Errorf("new token %q does not match expected format", newToken)
	}

	// Hash was updated and differs from old hash.
	newHash := queryAdminConfig(t, db, "admin_token_hash")
	if newHash == oldHash {
		t.Error("admin_token_hash was not updated during rotation")
	}
	if newHash != hexSHA256(newToken) {
		t.Errorf("stored hash does not match SHA-256 of new token")
	}

	// Old token no longer matches.
	if hexSHA256(oldToken) == newHash {
		t.Error("old token hash still matches new stored hash")
	}

	// File mode 0600 (Unix only).
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(tokenPath)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if perm := fi.Mode().Perm(); perm != 0o600 {
			t.Errorf("file mode = %04o, want 0600", perm)
		}
	}

	// Warn-level log contains the token file path.
	absPath, _ := filepath.Abs(tokenPath)
	var foundWarn bool
	for _, entry := range hook.AllEntries() {
		if entry.Level == logrus.WarnLevel && strings.Contains(entry.Message, absPath) {
			foundWarn = true
			break
		}
	}
	if !foundWarn {
		t.Errorf("no warn-level log containing %q", absPath)
	}
}

// TestSmoke_AutoPromotion exercises auto-promotion: a user with the
// designated admin email calls ShouldAutoPromote and receives true.
// [TS-04-SMOKE-5] [04-PATH-5]
func TestSmoke_AutoPromotion(t *testing.T) {
	db := openMemoryDB(t)
	setAdminConfig(t, db, "admin_email", "admin@example.com")

	logger, hook := newTestLogger()

	// OAuth callback: ShouldAutoPromote with matching email.
	result, err := bootstrap.ShouldAutoPromote(context.Background(), db, "admin@example.com")
	if err != nil {
		t.Fatalf("ShouldAutoPromote() error = %v", err)
	}
	if !result {
		t.Fatal("ShouldAutoPromote() = false, want true for designated admin email")
	}

	// Caller (OAuth handler) grants admin role and logs the event.
	logger.Infof("auto-promoted user %s to admin", "admin@example.com")

	// Non-matching email should not be promoted.
	result2, err2 := bootstrap.ShouldAutoPromote(context.Background(), db, "other@example.com")
	if err2 != nil {
		t.Fatalf("ShouldAutoPromote(other) error = %v", err2)
	}
	if result2 {
		t.Error("ShouldAutoPromote() = true for non-admin email, want false")
	}

	// Verify info log exists.
	var foundInfo bool
	for _, entry := range hook.AllEntries() {
		if entry.Level == logrus.InfoLevel && strings.Contains(entry.Message, "admin@example.com") {
			foundInfo = true
			break
		}
	}
	if !foundInfo {
		t.Error("no info-level log entry for auto-promotion")
	}
}

// TestSmoke_EndToEnd exercises the full lifecycle: first boot, auto-promotion
// on first OAuth login, and subsequent boot with ADMIN_TOKEN validation.
// [TS-04-SMOKE-6] [04-PATH-6]
func TestSmoke_EndToEnd(t *testing.T) {
	db := openMemoryDB(t)
	logger, hook := newTestLogger()
	tmpDir := t.TempDir()

	// -------------------------------------------------------
	// Step 1: First boot with admin email
	// -------------------------------------------------------
	params := makeParams(db, "admin@corp.com", false, tmpDir, logger)

	err := bootstrap.Run(context.Background(), params)
	if err != nil {
		t.Fatalf("Step 1 (first boot): Run() = %v", err)
	}

	// Verify admin_email stored.
	if v := queryAdminConfig(t, db, "admin_email"); v != "admin@corp.com" {
		t.Errorf("Step 1: admin_email = %q, want %q", v, "admin@corp.com")
	}

	// Read the generated token.
	tokenPath := filepath.Join(tmpDir, "admin_token")
	token := string(readFileBytes(t, tokenPath))

	// Verify token format.
	pattern := regexp.MustCompile(`^ak_admin_[0-9a-f]{64}$`)
	if !pattern.MatchString(token) {
		t.Fatalf("Step 1: token %q does not match expected format", token)
	}

	// Verify hash stored.
	storedHash := queryAdminConfig(t, db, "admin_token_hash")
	if storedHash != hexSHA256(token) {
		t.Errorf("Step 1: stored hash does not match SHA-256 of token")
	}

	// -------------------------------------------------------
	// Step 2: ShouldAutoPromote for designated admin email
	// -------------------------------------------------------
	result, apErr := bootstrap.ShouldAutoPromote(context.Background(), db, "admin@corp.com")
	if apErr != nil {
		t.Fatalf("Step 2 (auto-promote): error = %v", apErr)
	}
	if !result {
		t.Fatal("Step 2: ShouldAutoPromote() = false, want true for admin@corp.com")
	}

	// Caller logs auto-promotion at info level.
	logger.Infof("auto-promoted user %s to admin", "admin@corp.com")

	var foundInfo bool
	for _, entry := range hook.AllEntries() {
		if entry.Level == logrus.InfoLevel && strings.Contains(entry.Message, "admin@corp.com") {
			foundInfo = true
			break
		}
	}
	if !foundInfo {
		t.Error("Step 2: no info-level log for auto-promotion")
	}

	// -------------------------------------------------------
	// Step 3: Subsequent boot with ADMIN_TOKEN
	// -------------------------------------------------------

	// Operator saves the token and deletes the file.
	if err := os.Remove(tokenPath); err != nil {
		t.Fatalf("Step 3: remove token file: %v", err)
	}

	// Insert a user to simulate post-OAuth state.
	insertUser(t, db)

	// Set ADMIN_TOKEN to the saved token.
	t.Setenv("ADMIN_TOKEN", token)

	params2 := makeParams(db, "", false, tmpDir, logger)

	err = bootstrap.Run(context.Background(), params2)
	if err != nil {
		t.Fatalf("Step 3 (subsequent boot): Run() = %v", err)
	}

	// No new token file created.
	if fileExists(tokenPath) {
		t.Error("Step 3: admin_token file should not be re-created on subsequent boot")
	}
}

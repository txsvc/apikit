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

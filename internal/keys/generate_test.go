package keys_test

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/txsvc/apikit"
	"github.com/txsvc/apikit/internal/db"
	"github.com/txsvc/apikit/internal/keys"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// testDB creates an in-memory database for testing and returns the *db.DB.
// The database is closed automatically when the test completes.
func testDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("db.OpenMemory() error = %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

// insertTestUser inserts a minimal user row so api_keys FK is satisfied.
func insertTestUser(t *testing.T, sqlDB *sql.DB, userID string) {
	t.Helper()
	_, err := sqlDB.Exec(
		`INSERT INTO users (id, username, email, role, status, provider, provider_id, created_at, updated_at)
		 VALUES (?, ?, 'test@example.com', 'user', 'active', 'github', ?, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		userID, "user_"+userID, "gh_"+userID,
	)
	if err != nil {
		t.Fatalf("insertTestUser(%q) failed: %v", userID, err)
	}
}

// testLogger returns an echo.Logger suitable for testing.
func testLogger() echo.Logger {
	e := echo.New()
	e.Logger.SetOutput(io.Discard)
	return e.Logger
}

// failingReader is a mock io.Reader that always returns an error.
// It tracks the number of times Read is called.
type failingReader struct {
	callCount int
}

func (r *failingReader) Read(_ []byte) (int, error) {
	r.callCount++
	return 0, errors.New("simulated rand failure")
}

// collisionReader provides deterministic bytes for the first Read calls
// and then fails on a configurable call number. Used to simulate key_id
// collisions followed by rand failure.
type collisionReader struct {
	callCount  int
	failOnCall int    // fail on this Read call number (1-indexed; 0 = never)
	data       []byte // bytes to return for successful reads
	pos        int
}

func (r *collisionReader) Read(p []byte) (int, error) {
	r.callCount++
	if r.failOnCall > 0 && r.callCount >= r.failOnCall {
		return 0, errors.New("simulated rand failure on retry")
	}
	if r.pos >= len(r.data) {
		return 0, errors.New("collisionReader: exhausted data")
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// =========================================================================
// Subtask 1.1: Random string generation and key format tests
// Test Spec: TS-10-1, TS-10-2
// Requirements: 10-REQ-1.1, 10-REQ-1.2
// =========================================================================

// TestGenerateAPIKey_KeyFormat verifies that GenerateAPIKey returns a FullKey
// matching the regex ^ak_[a-zA-Z0-9]{8}_[a-zA-Z0-9]{32}$, with KeyID exactly
// 8 characters and the secret segment exactly 32 characters.
// TS-10-1 (Requirement: 10-REQ-1.1)
func TestGenerateAPIKey_KeyFormat(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-001")

	result, err := keys.GenerateAPIKey(database.SqlDB, "user-001", 90, testLogger())
	if err != nil {
		t.Fatalf("GenerateAPIKey() error = %v; want nil", err)
	}
	if result == nil {
		t.Fatal("GenerateAPIKey() returned nil result; want non-nil")
	}

	// Verify full key matches canonical format.
	keyPattern := regexp.MustCompile(`^ak_[a-zA-Z0-9]{8}_[a-zA-Z0-9]{32}$`)
	if !keyPattern.MatchString(result.FullKey) {
		t.Errorf("FullKey %q does not match pattern ^ak_[a-zA-Z0-9]{8}_[a-zA-Z0-9]{32}$", result.FullKey)
	}

	// Verify FullKey splits into exactly 3 segments.
	parts := strings.Split(result.FullKey, "_")
	if len(parts) != 3 {
		t.Fatalf("FullKey %q splits into %d parts; want 3", result.FullKey, len(parts))
	}

	// Verify prefix is 'ak'.
	if parts[0] != "ak" {
		t.Errorf("FullKey prefix = %q; want %q", parts[0], "ak")
	}

	// Verify key_id length is 8.
	if len(parts[1]) != 8 {
		t.Errorf("key_id length = %d; want 8", len(parts[1]))
	}

	// Verify secret length is 32.
	if len(parts[2]) != 32 {
		t.Errorf("secret length = %d; want 32", len(parts[2]))
	}

	// Verify KeyID matches the key_id segment.
	if result.KeyID != parts[1] {
		t.Errorf("KeyID = %q; want %q (from FullKey)", result.KeyID, parts[1])
	}
}

// TestGenerateAPIKey_AlphanumericCharset verifies that KeyID and secret
// contain only alphanumeric characters from [0-9A-Za-z].
// TS-10-2 (Requirement: 10-REQ-1.2)
func TestGenerateAPIKey_AlphanumericCharset(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-002")

	result, err := keys.GenerateAPIKey(database.SqlDB, "user-002", 30, testLogger())
	if err != nil {
		t.Fatalf("GenerateAPIKey() error = %v; want nil", err)
	}
	if result == nil {
		t.Fatal("GenerateAPIKey() returned nil result; want non-nil")
	}

	// Verify KeyID contains only [0-9A-Za-z].
	alnumPattern := regexp.MustCompile(`^[0-9A-Za-z]+$`)
	if !alnumPattern.MatchString(result.KeyID) {
		t.Errorf("KeyID %q contains characters outside [0-9A-Za-z]", result.KeyID)
	}
	if len(result.KeyID) != 8 {
		t.Errorf("KeyID length = %d; want 8", len(result.KeyID))
	}

	// Extract secret from FullKey and verify it.
	parts := strings.Split(result.FullKey, "_")
	if len(parts) != 3 {
		t.Fatalf("FullKey %q splits into %d parts; want 3", result.FullKey, len(parts))
	}
	secret := parts[2]
	if !alnumPattern.MatchString(secret) {
		t.Errorf("secret %q contains characters outside [0-9A-Za-z]", secret)
	}
	if len(secret) != 32 {
		t.Errorf("secret length = %d; want 32", len(secret))
	}
}

// =========================================================================
// Subtask 1.2: crypto/rand failure tests
// Test Spec: TS-10-3, TS-10-E1
// Requirements: 10-REQ-1.3, 10-REQ-1.E1
// =========================================================================

// TestGenerateAPIKey_RandFailure verifies that a crypto/rand.Read() failure
// causes GenerateAPIKey to return a non-nil error immediately with no retries
// and no database writes.
// TS-10-3 (Requirement: 10-REQ-1.3)
func TestGenerateAPIKey_RandFailure(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-003")

	// Inject a failing rand reader.
	mock := &failingReader{}
	restore := keys.SetRandReader(mock)
	defer restore()

	result, err := keys.GenerateAPIKey(database.SqlDB, "user-003", 90, testLogger())

	// Expect non-nil error.
	if err == nil {
		t.Fatal("GenerateAPIKey() with failing rand returned nil error; want non-nil")
	}

	// Expect nil result.
	if result != nil {
		t.Errorf("GenerateAPIKey() with failing rand returned non-nil result: %+v", result)
	}

	// Verify no rows were inserted into api_keys.
	var count int
	scanErr := database.SqlDB.QueryRow(
		"SELECT COUNT(*) FROM api_keys WHERE user_id = ?", "user-003",
	).Scan(&count)
	if scanErr != nil {
		t.Fatalf("count query failed: %v", scanErr)
	}
	if count != 0 {
		t.Errorf("api_keys row count = %d; want 0 (no writes on rand failure)", count)
	}

	// Verify rand was called exactly once (no retries).
	if mock.callCount != 1 {
		t.Errorf("rand Read call count = %d; want 1 (no retries on rand failure)", mock.callCount)
	}
}

// TestGenerateAPIKey_RandFailureDuringRetry verifies that a crypto/rand.Read()
// failure during a retry attempt (after key_id collision) is immediately fatal
// with no further retries.
// TS-10-E1 (Requirement: 10-REQ-1.E1)
func TestGenerateAPIKey_RandFailureDuringRetry(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-E1")

	// Pre-insert a key with a known key_id to force a collision.
	// The charset maps byte 0 to '0' (first character). Providing 8 zero bytes
	// produces key_id "00000000" which we pre-insert to force collision.
	collidingKeyID := makeTestKeyID(0)
	_, err := database.SqlDB.Exec(
		`INSERT INTO api_keys (key_id, user_id, secret_hash, expires_days, created_at)
		 VALUES (?, 'user-E1', 'fakehash', 90, '2026-01-01T00:00:00Z')`,
		collidingKeyID,
	)
	if err != nil {
		t.Fatalf("pre-insert collision key failed: %v", err)
	}

	// Set up a mock reader: provide enough bytes for one full generation attempt
	// (key_id=8 + secret=32 = 40 bytes of zeros → colliding key_id), then fail.
	data := make([]byte, 40)
	mock := &collisionReader{
		failOnCall: 3, // fail on the 3rd Read call (first retry for key_id)
		data:       data,
	}
	restore := keys.SetRandReader(mock)
	defer restore()

	result, genErr := keys.GenerateAPIKey(database.SqlDB, "user-E1", 90, testLogger())

	// Expect non-nil error.
	if genErr == nil {
		t.Fatal("GenerateAPIKey() with collision+rand failure returned nil error; want non-nil")
	}

	// Expect nil result.
	if result != nil {
		t.Errorf("GenerateAPIKey() returned non-nil result: %+v", result)
	}

	// Verify no new rows were inserted for user-E1 beyond the pre-inserted one.
	var count int
	scanErr := database.SqlDB.QueryRow(
		"SELECT COUNT(*) FROM api_keys WHERE user_id = ?", "user-E1",
	).Scan(&count)
	if scanErr != nil {
		t.Fatalf("count query failed: %v", scanErr)
	}
	// There should be at most the pre-inserted row (which may have been revoked
	// but not deleted).
	if count > 1 {
		t.Errorf("api_keys row count for user-E1 = %d; want <= 1 (only pre-inserted row)", count)
	}
}

// =========================================================================
// Subtask 1.3: SHA-256 hashing and secret hash storage tests
// Test Spec: TS-10-9, TS-10-43
// Requirements: 10-REQ-2.6, 10-REQ-10.1
// =========================================================================

// TestGenerateAPIKey_SecretHash verifies that the SecretHash field in
// APIKeyResult equals the lowercase hex SHA-256 of the secret portion of
// FullKey, and that the database row contains the same hash.
// TS-10-9 (Requirement: 10-REQ-2.6)
func TestGenerateAPIKey_SecretHash(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-009")

	result, err := keys.GenerateAPIKey(database.SqlDB, "user-009", 30, testLogger())
	if err != nil {
		t.Fatalf("GenerateAPIKey() error = %v; want nil", err)
	}
	if result == nil {
		t.Fatal("GenerateAPIKey() returned nil result; want non-nil")
	}

	// Extract secret from FullKey.
	parts := strings.Split(result.FullKey, "_")
	if len(parts) != 3 {
		t.Fatalf("FullKey %q splits into %d parts; want 3", result.FullKey, len(parts))
	}
	secret := parts[2]

	// Verify FullKey == ak_<KeyID>_<secret>.
	expectedFullKey := "ak_" + result.KeyID + "_" + secret
	if result.FullKey != expectedFullKey {
		t.Errorf("FullKey = %q; want %q", result.FullKey, expectedFullKey)
	}

	// Compute expected hash.
	h := sha256.Sum256([]byte(secret))
	expectedHash := hex.EncodeToString(h[:])

	// Verify SecretHash matches.
	if result.SecretHash != expectedHash {
		t.Errorf("SecretHash = %q; want %q", result.SecretHash, expectedHash)
	}

	// Verify SecretHash is exactly 64 lowercase hex chars.
	if len(result.SecretHash) != 64 {
		t.Errorf("SecretHash length = %d; want 64", len(result.SecretHash))
	}
	hexPattern := regexp.MustCompile(`^[0-9a-f]{64}$`)
	if !hexPattern.MatchString(result.SecretHash) {
		t.Errorf("SecretHash %q does not match ^[0-9a-f]{64}$", result.SecretHash)
	}

	// Verify database row matches.
	var dbKeyID, dbUserID, dbSecretHash string
	var dbExpiresDays int
	scanErr := database.SqlDB.QueryRow(
		"SELECT key_id, user_id, secret_hash, expires_days FROM api_keys WHERE key_id = ?",
		result.KeyID,
	).Scan(&dbKeyID, &dbUserID, &dbSecretHash, &dbExpiresDays)
	if scanErr != nil {
		t.Fatalf("query api_keys row failed: %v", scanErr)
	}

	if dbKeyID != result.KeyID {
		t.Errorf("DB key_id = %q; want %q", dbKeyID, result.KeyID)
	}
	if dbUserID != "user-009" {
		t.Errorf("DB user_id = %q; want %q", dbUserID, "user-009")
	}
	if dbSecretHash != result.SecretHash {
		t.Errorf("DB secret_hash = %q; want %q", dbSecretHash, result.SecretHash)
	}
	if dbExpiresDays != 30 {
		t.Errorf("DB expires_days = %d; want 30", dbExpiresDays)
	}
}

// TestGenerateAPIKey_PlaintextSecretNotStored verifies that the plaintext
// secret is never written to any column in the api_keys table — only the
// SHA-256 hash is stored.
// TS-10-43 (Requirement: 10-REQ-10.1)
func TestGenerateAPIKey_PlaintextSecretNotStored(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-035")

	result, err := keys.GenerateAPIKey(database.SqlDB, "user-035", 90, testLogger())
	if err != nil {
		t.Fatalf("GenerateAPIKey() error = %v; want nil", err)
	}
	if result == nil {
		t.Fatal("GenerateAPIKey() returned nil result; want non-nil")
	}

	// Extract the plaintext secret.
	parts := strings.Split(result.FullKey, "_")
	if len(parts) != 3 {
		t.Fatalf("FullKey %q splits into %d parts; want 3", result.FullKey, len(parts))
	}
	secret := parts[2]

	// Verify secret_hash equals hex(sha256(secret)).
	h := sha256.Sum256([]byte(secret))
	expectedHash := hex.EncodeToString(h[:])

	var dbSecretHash string
	scanErr := database.SqlDB.QueryRow(
		"SELECT secret_hash FROM api_keys WHERE key_id = ?", result.KeyID,
	).Scan(&dbSecretHash)
	if scanErr != nil {
		t.Fatalf("query secret_hash failed: %v", scanErr)
	}
	if dbSecretHash != expectedHash {
		t.Errorf("DB secret_hash = %q; want %q", dbSecretHash, expectedHash)
	}
	if len(dbSecretHash) != 64 {
		t.Errorf("DB secret_hash length = %d; want 64", len(dbSecretHash))
	}
	hexPattern := regexp.MustCompile(`^[0-9a-f]{64}$`)
	if !hexPattern.MatchString(dbSecretHash) {
		t.Errorf("DB secret_hash %q does not match ^[0-9a-f]{64}$", dbSecretHash)
	}

	// Verify no column in the api_keys row contains the plaintext secret.
	var keyID, userID, createdAt string
	var expiresDays int
	var expiresAt, revokedAt sql.NullString
	scanErr = database.SqlDB.QueryRow(
		`SELECT key_id, user_id, secret_hash, expires_days, expires_at, created_at, revoked_at
		 FROM api_keys WHERE key_id = ?`, result.KeyID,
	).Scan(&keyID, &userID, &dbSecretHash, &expiresDays, &expiresAt, &createdAt, &revokedAt)
	if scanErr != nil {
		t.Fatalf("full row query failed: %v", scanErr)
	}

	columnsToCheck := map[string]string{
		"key_id":     keyID,
		"user_id":    userID,
		"created_at": createdAt,
	}
	for colName, colVal := range columnsToCheck {
		if strings.Contains(colVal, secret) {
			t.Errorf("column %q contains plaintext secret", colName)
		}
	}

	// The secret_hash must NOT be the plaintext secret itself.
	if dbSecretHash == secret {
		t.Error("secret_hash equals plaintext secret; want SHA-256 hash")
	}
}

// =========================================================================
// Subtask 1.4: Expiry calculation tests
// Test Spec: TS-10-5, TS-10-11
// Requirements: 10-REQ-2.2, 10-REQ-2.8
// =========================================================================

// TestGenerateAPIKey_InvalidExpiresDays verifies that GenerateAPIKey returns
// a non-nil error immediately when expiresDays is not in {0, 30, 60, 90},
// without touching the database.
// TS-10-5 (Requirement: 10-REQ-2.2)
func TestGenerateAPIKey_InvalidExpiresDays(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-005")

	invalidValues := []int{1, 15, 45, 100, -1, 365}
	for _, days := range invalidValues {
		t.Run(fmt.Sprintf("expiresDays=%d", days), func(t *testing.T) {
			result, err := keys.GenerateAPIKey(database.SqlDB, "user-005", days, testLogger())

			if err == nil {
				t.Errorf("GenerateAPIKey(expiresDays=%d) returned nil error; want non-nil", days)
			}
			if result != nil {
				t.Errorf("GenerateAPIKey(expiresDays=%d) returned non-nil result; want nil", days)
			}
		})
	}

	// Verify no rows were inserted for any invalid value.
	var count int
	scanErr := database.SqlDB.QueryRow(
		"SELECT COUNT(*) FROM api_keys WHERE user_id = ?", "user-005",
	).Scan(&count)
	if scanErr != nil {
		t.Fatalf("count query failed: %v", scanErr)
	}
	if count != 0 {
		t.Errorf("api_keys row count = %d; want 0 (no writes for invalid expiresDays)", count)
	}
}

// TestGenerateAPIKey_ExpiryCalculation verifies ExpiresAt for all valid
// expiresDays values: nil for 0, and approximately now + N*24h for 30/60/90.
// TS-10-11 (Requirement: 10-REQ-2.8)
func TestGenerateAPIKey_ExpiryCalculation(t *testing.T) {
	// Test expiresDays=0 → ExpiresAt==nil and expires_at IS NULL in DB.
	t.Run("expiresDays=0", func(t *testing.T) {
		database := testDB(t)
		insertTestUser(t, database.SqlDB, "user-exp-0")

		result, err := keys.GenerateAPIKey(database.SqlDB, "user-exp-0", 0, testLogger())
		if err != nil {
			t.Fatalf("GenerateAPIKey(0) error = %v; want nil", err)
		}
		if result == nil {
			t.Fatal("GenerateAPIKey(0) returned nil result; want non-nil")
		}
		if result.ExpiresAt != nil {
			t.Errorf("ExpiresAt = %v; want nil for expiresDays=0", result.ExpiresAt)
		}

		// Verify expires_at IS NULL in DB.
		var expiresAt sql.NullString
		scanErr := database.SqlDB.QueryRow(
			"SELECT expires_at FROM api_keys WHERE key_id = ?", result.KeyID,
		).Scan(&expiresAt)
		if scanErr != nil {
			t.Fatalf("query expires_at failed: %v", scanErr)
		}
		if expiresAt.Valid {
			t.Errorf("DB expires_at = %q; want NULL", expiresAt.String)
		}
	})

	// Test expiresDays in {30, 60, 90} → ExpiresAt approximately now + N*24h.
	testCases := []struct {
		days  int
		hours int
	}{
		{30, 720},
		{60, 1440},
		{90, 2160},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("expiresDays=%d", tc.days), func(t *testing.T) {
			database := testDB(t)
			userID := fmt.Sprintf("user-exp-%d", tc.days)
			insertTestUser(t, database.SqlDB, userID)

			before := time.Now().UTC()
			result, err := keys.GenerateAPIKey(database.SqlDB, userID, tc.days, testLogger())
			if err != nil {
				t.Fatalf("GenerateAPIKey(%d) error = %v; want nil", tc.days, err)
			}
			if result == nil {
				t.Fatalf("GenerateAPIKey(%d) returned nil result; want non-nil", tc.days)
			}
			if result.ExpiresAt == nil {
				t.Fatalf("ExpiresAt = nil; want non-nil for expiresDays=%d", tc.days)
			}

			// Verify ExpiresAt is approximately before + N*24h within 2 seconds.
			expectedExpiry := before.Add(time.Duration(tc.hours) * time.Hour)
			diff := result.ExpiresAt.Sub(expectedExpiry)
			if diff < -2*time.Second || diff > 2*time.Second {
				t.Errorf("ExpiresAt diff from expected = %v; want within 2s (expected ~%v, got %v)",
					diff, expectedExpiry, result.ExpiresAt)
			}
		})
	}
}

// =========================================================================
// Subtask 1.5: APIKeyResult signature and public re-exports
// Test Spec: TS-10-4, TS-10-13, TS-10-14
// Requirements: 10-REQ-2.1, 10-REQ-3.1, 10-REQ-3.2
// =========================================================================

// TestGenerateAPIKey_AllFieldsPopulated verifies that GenerateAPIKey returns
// (*APIKeyResult, nil) with all fields populated on success.
// TS-10-4 (Requirement: 10-REQ-2.1)
func TestGenerateAPIKey_AllFieldsPopulated(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-004")

	result, err := keys.GenerateAPIKey(database.SqlDB, "user-004", 90, testLogger())
	if err != nil {
		t.Fatalf("GenerateAPIKey() error = %v; want nil", err)
	}
	if result == nil {
		t.Fatal("GenerateAPIKey() returned nil result; want non-nil")
	}

	// Verify all fields are non-zero/non-nil.
	if result.FullKey == "" {
		t.Error("FullKey is empty; want non-empty")
	}
	if result.KeyID == "" {
		t.Error("KeyID is empty; want non-empty")
	}
	if result.SecretHash == "" {
		t.Error("SecretHash is empty; want non-empty")
	}
	if result.ExpiresAt == nil {
		t.Error("ExpiresAt is nil; want non-nil for expiresDays=90")
	}
}

// TestPublicAPI_GenerateAPIKeyDelegation verifies that apikit.GenerateAPIKey
// delegates to keys.GenerateAPIKey and apikit.APIKeyResult is a type alias
// for keys.APIKeyResult.
// TS-10-13 (Requirement: 10-REQ-3.1)
func TestPublicAPI_GenerateAPIKeyDelegation(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-012")

	result, err := apikit.GenerateAPIKey(database.SqlDB, "user-012", 90, testLogger())
	if err != nil {
		t.Fatalf("apikit.GenerateAPIKey() error = %v; want nil", err)
	}
	if result == nil {
		t.Fatal("apikit.GenerateAPIKey() returned nil result; want non-nil")
	}

	// Verify type alias: assign without cast.
	// apikit.APIKeyResult should be identical to keys.APIKeyResult.
	var r apikit.APIKeyResult = *result
	if r.FullKey != result.FullKey {
		t.Errorf("type alias FullKey = %q; want %q", r.FullKey, result.FullKey)
	}
	if r.KeyID != result.KeyID {
		t.Errorf("type alias KeyID = %q; want %q", r.KeyID, result.KeyID)
	}
}

// TestPublicAPI_NoExtraExports verifies that the apikit root package does
// not expose any symbols from internal/keys other than APIKeyResult and
// GenerateAPIKey. This is validated by scanning the root package source
// files for unexpected key-related exported symbols.
// TS-10-14 (Requirement: 10-REQ-3.2)
func TestPublicAPI_NoExtraExports(t *testing.T) {
	// Find the root package directory by walking up from the test directory.
	rootDir, err := findRootPackageDir()
	if err != nil {
		t.Fatalf("failed to find root package dir: %v", err)
	}

	entries, err := os.ReadDir(rootDir)
	if err != nil {
		t.Fatalf("os.ReadDir(%q) error = %v", rootDir, err)
	}

	// Symbols that must NOT be exported from the root package.
	forbiddenSymbols := []string{
		"ErrKeyRevoked",
		"ErrKeyExpired",
		"ErrInvalidExpiry",
		"RegisterKeyHandlers",
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		// Skip test files.
		if strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}

		content, readErr := os.ReadFile(filepath.Join(rootDir, entry.Name()))
		if readErr != nil {
			t.Fatalf("read %q: %v", entry.Name(), readErr)
		}
		src := string(content)

		for _, sym := range forbiddenSymbols {
			// Check for exported declarations: var, func, type, const.
			patterns := []string{
				"var " + sym,
				"func " + sym,
				"type " + sym,
				sym + " =",
			}
			for _, pat := range patterns {
				if strings.Contains(src, pat) {
					t.Errorf("root package file %q exports forbidden symbol %q (found %q)",
						entry.Name(), sym, pat)
				}
			}
		}
	}

	// Positive check: verify expected symbols ARE present.
	expectedSymbols := []string{"APIKeyResult", "GenerateAPIKey"}
	foundSymbols := make(map[string]bool)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		content, _ := os.ReadFile(filepath.Join(rootDir, entry.Name()))
		src := string(content)
		for _, sym := range expectedSymbols {
			if strings.Contains(src, sym) {
				foundSymbols[sym] = true
			}
		}
	}
	for _, sym := range expectedSymbols {
		if !foundSymbols[sym] {
			t.Errorf("expected symbol %q not found in root package", sym)
		}
	}
}

// findRootPackageDir locates the root apikit package directory by walking
// up from the current working directory.
func findRootPackageDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	// Walk up looking for go.mod.
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("could not find go.mod in any parent directory")
		}
		dir = parent
	}
}

// =========================================================================
// Subtask 1.6: Type assertion transaction detection and no-package-level-logger
// Test Spec: TS-10-E5, TS-10-41
// Requirements: 10-REQ-2.E4, 10-REQ-9.1
// =========================================================================

// TestTypeAssertion_DBDetection verifies that the source code of
// GenerateAPIKey contains the type assertion pattern '_, ok := tx.(*sql.DB)'
// for distinguishing *sql.DB from *sql.Tx, and does not introduce any
// wrapper type or TxDetector interface.
// TS-10-E5 (Requirement: 10-REQ-2.E4)
func TestTypeAssertion_DBDetection(t *testing.T) {
	// Read the source file containing GenerateAPIKey.
	source, err := os.ReadFile("generate.go")
	if err != nil {
		t.Fatalf("failed to read generate.go: %v", err)
	}
	src := string(source)

	// Verify the type assertion pattern is present.
	if !strings.Contains(src, "_, ok := tx.(*sql.DB)") {
		// Also accept the single-value form.
		if !strings.Contains(src, ", ok := tx.(*sql.DB)") {
			t.Error("generate.go does not contain type assertion '_, ok := tx.(*sql.DB)'; " +
				"GenerateAPIKey must use type assertion to detect *sql.DB")
		}
	}

	// Verify no wrapper type is introduced.
	if strings.Contains(src, "type txWrapper") {
		t.Error("generate.go introduces 'type txWrapper'; want direct type assertion without wrappers")
	}
	if strings.Contains(src, "type TxDetector") {
		t.Error("generate.go introduces 'type TxDetector'; want direct type assertion without wrappers")
	}
}

// TestNoPackageLevelLogger verifies that the internal/keys package uses no
// package-level logger singleton; GenerateAPIKey accepts an explicit
// logger echo.Logger parameter, and handlers use c.Logger().
// TS-10-41 (Requirement: 10-REQ-9.1)
func TestNoPackageLevelLogger(t *testing.T) {
	// Read all Go source files in internal/keys/.
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("os.ReadDir(.) error = %v", err)
	}

	globalLoggerPattern := regexp.MustCompile(`var\s+\w*[Ll]og\w*\s+echo\.Logger`)
	stdLogPattern := regexp.MustCompile(`var\s+\w*[Ll]og\w*\s+=\s+log\.New`)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		// Skip test files.
		if strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}

		content, readErr := os.ReadFile(entry.Name())
		if readErr != nil {
			t.Fatalf("read %q: %v", entry.Name(), readErr)
		}
		src := string(content)

		if globalLoggerPattern.MatchString(src) {
			t.Errorf("file %q contains package-level echo.Logger variable; want no logger singletons",
				entry.Name())
		}
		if stdLogPattern.MatchString(src) {
			t.Errorf("file %q contains package-level log.New logger; want no logger singletons",
				entry.Name())
		}
	}

	// Verify GenerateAPIKey signature includes 'logger echo.Logger' parameter.
	source, err := os.ReadFile("generate.go")
	if err != nil {
		t.Fatalf("failed to read generate.go: %v", err)
	}
	if !strings.Contains(string(source), "logger echo.Logger") {
		t.Error("GenerateAPIKey signature does not contain 'logger echo.Logger' parameter")
	}
}

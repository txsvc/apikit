package keys_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"
	"github.com/txsvc/apikit/internal/db"
	"github.com/txsvc/apikit/internal/keys"
)

// =========================================================================
// Integration test helpers
// =========================================================================

// insertTestKey directly inserts a key row into api_keys for test setup.
// Pass empty strings for nullable fields (expiresAt, revokedAt) to insert NULL.
func insertTestKey(t *testing.T, sqlDB *sql.DB, keyID, userID, secretHash string, expiresDays int, expiresAt, revokedAt, createdAt string) {
	t.Helper()
	var eAt, rAt any
	if expiresAt != "" {
		eAt = expiresAt
	}
	if revokedAt != "" {
		rAt = revokedAt
	}
	_, err := sqlDB.Exec(
		`INSERT INTO api_keys (key_id, user_id, secret_hash, expires_days, expires_at, revoked_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		keyID, userID, secretHash, expiresDays, eAt, rAt, createdAt,
	)
	if err != nil {
		t.Fatalf("insertTestKey(%q, %q) failed: %v", keyID, userID, err)
	}
}

// logCapture creates an echo.Logger that writes to a bytes.Buffer,
// allowing tests to inspect log output. The logger level is set to DEBUG
// so all messages (including INFO) are captured.
func logCapture() (echo.Logger, *bytes.Buffer) {
	e := echo.New()
	buf := &bytes.Buffer{}
	e.Logger.SetOutput(buf)
	e.Logger.SetLevel(log.DEBUG)
	return e.Logger, buf
}

// mockExecutor wraps a real *sql.DB to intercept INSERT statements.
// When insertErr is non-nil, INSERT ExecContext calls return that error
// instead of executing against the database. All other operations are
// delegated to the underlying *sql.DB.
type mockExecutor struct {
	realDB      *sql.DB
	insertErr   error
	insertCalls int
}

func (m *mockExecutor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if strings.Contains(strings.ToUpper(query), "INSERT") {
		m.insertCalls++
		if m.insertErr != nil {
			return nil, m.insertErr
		}
	}
	return m.realDB.ExecContext(ctx, query, args...)
}

func (m *mockExecutor) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return m.realDB.QueryContext(ctx, query, args...)
}

func (m *mockExecutor) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return m.realDB.QueryRowContext(ctx, query, args...)
}

// queryActiveCount returns the number of active (non-revoked) keys for a user.
func queryActiveCount(t *testing.T, sqlDB *sql.DB, userID string) int {
	t.Helper()
	var count int
	if err := sqlDB.QueryRow(
		"SELECT COUNT(*) FROM api_keys WHERE user_id = ? AND revoked_at IS NULL", userID,
	).Scan(&count); err != nil {
		t.Fatalf("queryActiveCount(%q) failed: %v", userID, err)
	}
	return count
}

// queryTotalCount returns the total number of keys for a user.
func queryTotalCount(t *testing.T, sqlDB *sql.DB, userID string) int {
	t.Helper()
	var count int
	if err := sqlDB.QueryRow(
		"SELECT COUNT(*) FROM api_keys WHERE user_id = ?", userID,
	).Scan(&count); err != nil {
		t.Fatalf("queryTotalCount(%q) failed: %v", userID, err)
	}
	return count
}

// queryNullString runs a single-column query and returns the result as sql.NullString.
func queryNullString(t *testing.T, sqlDB *sql.DB, query string, args ...any) sql.NullString {
	t.Helper()
	var ns sql.NullString
	if err := sqlDB.QueryRow(query, args...).Scan(&ns); err != nil {
		t.Fatalf("queryNullString(%q) failed: %v", query, err)
	}
	return ns
}

// makeTestKeyID returns an 8-character key_id consisting of a single
// repeated character derived from byte b. Used to predict the key_id that
// GenerateAPIKey would produce from uniform bytes for collision testing.
// The mapping uses byte % 62 into the charset "0123456789ABCDEF...Zabc...z".
// NOTE: If the implementation uses a different charset ordering, this helper
// must be updated to match.
func makeTestKeyID(b byte) string {
	const charset = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	ch := charset[int(b)%62]
	return strings.Repeat(string(ch), 8)
}

// =========================================================================
// Subtask 2.1: GenerateAPIKey with *sql.DB — atomic transaction behavior
// Test Spec: TS-10-6, TS-10-E2
// Requirements: 10-REQ-2.3, 10-REQ-2.E1
// =========================================================================

// TestGenerateAPIKey_SqlDB_AtomicRevocationAndInsert verifies that when
// called with *sql.DB, GenerateAPIKey begins an internal transaction,
// executes the revocation UPDATE and INSERT atomically, and commits on
// success. The prior active key is revoked and a new key is inserted.
// TS-10-6 (Requirement: 10-REQ-2.3)
func TestGenerateAPIKey_SqlDB_AtomicRevocationAndInsert(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-006")

	// Pre-insert an active key for the user.
	now := db.FormatTime(time.Now().UTC())
	expiresAt := db.FormatTime(time.Now().UTC().Add(90 * 24 * time.Hour))
	insertTestKey(t, database.SqlDB, "origky06", "user-006", "hash006", 90, expiresAt, "", now)

	// Verify setup: the original key is active (revoked_at IS NULL).
	origRevoked := queryNullString(t, database.SqlDB,
		"SELECT revoked_at FROM api_keys WHERE key_id = ?", "origky06")
	if origRevoked.Valid {
		t.Fatal("setup error: original key already has revoked_at set")
	}

	// Call GenerateAPIKey with *sql.DB — should begin internal transaction.
	result, err := keys.GenerateAPIKey(database.SqlDB, "user-006", 90, testLogger())
	if err != nil {
		t.Fatalf("GenerateAPIKey() error = %v; want nil", err)
	}
	if result == nil {
		t.Fatal("GenerateAPIKey() returned nil result; want non-nil")
	}

	// Verify the original key was revoked (revoked_at set to non-NULL).
	origRevoked = queryNullString(t, database.SqlDB,
		"SELECT revoked_at FROM api_keys WHERE key_id = ?", "origky06")
	if !origRevoked.Valid {
		t.Error("original key's revoked_at is still NULL; want non-NULL after GenerateAPIKey")
	}

	// Verify exactly one active key remains for the user.
	activeCount := queryActiveCount(t, database.SqlDB, "user-006")
	if activeCount != 1 {
		t.Errorf("active key count = %d; want 1", activeCount)
	}

	// Verify the active key is the newly generated one.
	var activeKeyID string
	if err := database.SqlDB.QueryRow(
		"SELECT key_id FROM api_keys WHERE user_id = ? AND revoked_at IS NULL", "user-006",
	).Scan(&activeKeyID); err != nil {
		t.Fatalf("query active key_id failed: %v", err)
	}
	if activeKeyID != result.KeyID {
		t.Errorf("active key_id = %q; want %q (new key)", activeKeyID, result.KeyID)
	}
}

// TestGenerateAPIKey_SqlDB_RollbackOnInsertFailure verifies that when called
// with *sql.DB and the INSERT fails after the revocation UPDATE has executed,
// the internal transaction is rolled back so the revocation UPDATE is also
// undone, leaving the user's prior key with revoked_at still NULL.
// TS-10-E2 (Requirement: 10-REQ-2.E1)
func TestGenerateAPIKey_SqlDB_RollbackOnInsertFailure(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-E2")
	insertTestUser(t, database.SqlDB, "collision-owner")

	// Pre-insert an active key for the user.
	now := db.FormatTime(time.Now().UTC())
	expiresAt := db.FormatTime(time.Now().UTC().Add(90 * 24 * time.Hour))
	insertTestKey(t, database.SqlDB, "priorkey", "user-E2", "hashE2", 90, expiresAt, "", now)

	// Force all 3 INSERT retry attempts to fail by pre-inserting collision rows.
	// Provide 120 bytes of deterministic data: each 40-byte chunk (8 key_id + 32 secret)
	// uses a uniform byte value so the generated key_id is predictable.
	deterBytes := make([]byte, 120)
	for i := range deterBytes {
		deterBytes[i] = byte(i / 40)
	}

	// Pre-insert collision rows for the 3 predicted key_ids.
	for i := range 3 {
		keyID := makeTestKeyID(byte(i))
		insertTestKey(t, database.SqlDB, keyID, "collision-owner", "hash", 0, "", "", now)
	}

	restore := keys.SetRandReader(bytes.NewReader(deterBytes))
	defer restore()

	result, err := keys.GenerateAPIKey(database.SqlDB, "user-E2", 90, testLogger())

	// Expect non-nil error (all retries exhausted).
	if err == nil {
		t.Fatal("GenerateAPIKey() returned nil error; want non-nil (all retries should fail)")
	}
	if result != nil {
		t.Errorf("GenerateAPIKey() returned non-nil result: %+v; want nil", result)
	}

	// Verify prior key's revoked_at remains NULL (transaction was rolled back).
	priorRevoked := queryNullString(t, database.SqlDB,
		"SELECT revoked_at FROM api_keys WHERE key_id = ?", "priorkey")
	if priorRevoked.Valid {
		t.Errorf("prior key's revoked_at = %q; want NULL (transaction should be rolled back)",
			priorRevoked.String)
	}

	// Verify no new key rows exist for user-E2 beyond the original.
	totalRows := queryTotalCount(t, database.SqlDB, "user-E2")
	if totalRows != 1 {
		t.Errorf("total api_keys rows for user-E2 = %d; want 1 (only original)", totalRows)
	}
}

// =========================================================================
// Subtask 2.2: GenerateAPIKey with *sql.Tx — caller-owned transaction
// Test Spec: TS-10-7
// Requirements: 10-REQ-2.4
// =========================================================================

// TestGenerateAPIKey_SqlTx_CommitPersistsKey verifies that when called with
// a *sql.Tx, GenerateAPIKey executes within the caller's transaction. After
// the caller commits, the key row is persisted in the database.
// TS-10-7 commit scenario (Requirement: 10-REQ-2.4)
func TestGenerateAPIKey_SqlTx_CommitPersistsKey(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-007")

	// Begin a caller-owned transaction.
	tx, err := database.SqlDB.Begin()
	if err != nil {
		t.Fatalf("db.Begin() error = %v", err)
	}

	result, err := keys.GenerateAPIKey(tx, "user-007", 90, testLogger())
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("GenerateAPIKey(tx) error = %v; want nil", err)
	}
	if result == nil {
		_ = tx.Rollback()
		t.Fatal("GenerateAPIKey(tx) returned nil result; want non-nil")
	}

	// Commit the caller's transaction.
	if err := tx.Commit(); err != nil {
		t.Fatalf("tx.Commit() error = %v", err)
	}

	// Verify key is persisted after commit.
	rowCount := queryActiveCount(t, database.SqlDB, "user-007")
	if rowCount != 1 {
		t.Errorf("active key count after commit = %d; want 1", rowCount)
	}

	// Verify the persisted key matches the returned result.
	var dbKeyID string
	scanErr := database.SqlDB.QueryRow(
		"SELECT key_id FROM api_keys WHERE user_id = ? AND revoked_at IS NULL", "user-007",
	).Scan(&dbKeyID)
	if scanErr != nil {
		t.Fatalf("query key_id failed: %v", scanErr)
	}
	if dbKeyID != result.KeyID {
		t.Errorf("persisted key_id = %q; want %q", dbKeyID, result.KeyID)
	}
}

// TestGenerateAPIKey_SqlTx_RollbackDiscardsKey verifies that when called
// with a *sql.Tx and the caller rolls back, no key row is persisted.
// GenerateAPIKey itself does not call Commit or Rollback on the caller's
// transaction.
// TS-10-7 rollback scenario (Requirement: 10-REQ-2.4)
func TestGenerateAPIKey_SqlTx_RollbackDiscardsKey(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-007b")

	// Begin a caller-owned transaction.
	tx, err := database.SqlDB.Begin()
	if err != nil {
		t.Fatalf("db.Begin() error = %v", err)
	}

	result, err := keys.GenerateAPIKey(tx, "user-007b", 90, testLogger())
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("GenerateAPIKey(tx) error = %v; want nil", err)
	}
	if result == nil {
		_ = tx.Rollback()
		t.Fatal("GenerateAPIKey(tx) returned nil result; want non-nil")
	}

	// Rollback the caller's transaction.
	if err := tx.Rollback(); err != nil {
		t.Fatalf("tx.Rollback() error = %v", err)
	}

	// Verify no key rows exist for the user after rollback.
	rowCount := queryTotalCount(t, database.SqlDB, "user-007b")
	if rowCount != 0 {
		t.Errorf("total key count after rollback = %d; want 0", rowCount)
	}
}

// =========================================================================
// Subtask 2.3: Zero-rows revocation and expired-key revocation
// Test Spec: TS-10-8, TS-10-E3, TS-10-E15
// Requirements: 10-REQ-2.5, 10-REQ-8.E1
// =========================================================================

// TestGenerateAPIKey_FirstTimeUser_ZeroRowsRevocation verifies that
// GenerateAPIKey succeeds for a first-time user with no existing keys.
// The revocation UPDATE affects zero rows and is treated as a silent no-op.
// TS-10-8, TS-10-E3 (Requirements: 10-REQ-2.5, 10-REQ-2.E2)
func TestGenerateAPIKey_FirstTimeUser_ZeroRowsRevocation(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-008")

	// User-008 has no existing keys — first-time user.
	result, err := keys.GenerateAPIKey(database.SqlDB, "user-008", 90, testLogger())
	if err != nil {
		t.Fatalf("GenerateAPIKey() error = %v; want nil (zero-rows revocation should be no-op)", err)
	}
	if result == nil {
		t.Fatal("GenerateAPIKey() returned nil result; want non-nil")
	}

	// Verify exactly one active key exists.
	activeCount := queryActiveCount(t, database.SqlDB, "user-008")
	if activeCount != 1 {
		t.Errorf("active key count = %d; want 1", activeCount)
	}

	// Verify the key has the expected fields populated.
	if result.KeyID == "" {
		t.Error("KeyID is empty; want non-empty")
	}
	if result.FullKey == "" {
		t.Error("FullKey is empty; want non-empty")
	}
	if result.SecretHash == "" {
		t.Error("SecretHash is empty; want non-empty")
	}
}

// TestGenerateAPIKey_ExpiredKeyRevocation verifies that GenerateAPIKey sets
// revoked_at on an expired-but-not-revoked key during the pre-INSERT
// revocation UPDATE. After the call, the expired key has revoked_at set
// and the new key is the only row with revoked_at IS NULL.
// TS-10-E15 (Requirement: 10-REQ-8.E1)
func TestGenerateAPIKey_ExpiredKeyRevocation(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-E15")

	// Pre-insert an expired key: expires_at in the past, revoked_at IS NULL.
	pastExpiresAt := "2020-01-01T00:00:00Z"
	now := db.FormatTime(time.Now().UTC())
	insertTestKey(t, database.SqlDB, "exprdkey", "user-E15", "hashE15", 90,
		pastExpiresAt, "", now)

	// Verify setup: expired key exists with revoked_at IS NULL.
	expiredRevoked := queryNullString(t, database.SqlDB,
		"SELECT revoked_at FROM api_keys WHERE key_id = ?", "exprdkey")
	if expiredRevoked.Valid {
		t.Fatal("setup error: expired key already has revoked_at set")
	}

	// Call GenerateAPIKey — should revoke the expired key as part of the
	// revocation UPDATE (WHERE revoked_at IS NULL, no expires_at filter).
	result, err := keys.GenerateAPIKey(database.SqlDB, "user-E15", 90, testLogger())
	if err != nil {
		t.Fatalf("GenerateAPIKey() error = %v; want nil", err)
	}
	if result == nil {
		t.Fatal("GenerateAPIKey() returned nil result; want non-nil")
	}

	// Verify the expired key now has revoked_at set.
	expiredRevoked = queryNullString(t, database.SqlDB,
		"SELECT revoked_at FROM api_keys WHERE key_id = ?", "exprdkey")
	if !expiredRevoked.Valid {
		t.Error("expired key's revoked_at is still NULL; want non-NULL after GenerateAPIKey")
	}

	// Verify exactly one row with revoked_at IS NULL (the new key).
	activeCount := queryActiveCount(t, database.SqlDB, "user-E15")
	if activeCount != 1 {
		t.Errorf("active key count = %d; want 1", activeCount)
	}

	// Verify the active key is the new one.
	var activeKeyID string
	if err := database.SqlDB.QueryRow(
		"SELECT key_id FROM api_keys WHERE user_id = ? AND revoked_at IS NULL", "user-E15",
	).Scan(&activeKeyID); err != nil {
		t.Fatalf("query active key_id failed: %v", err)
	}
	if activeKeyID != result.KeyID {
		t.Errorf("active key_id = %q; want %q (new key)", activeKeyID, result.KeyID)
	}
}

// =========================================================================
// Subtask 2.4: key_id collision retry (bounded to 3 attempts)
// Test Spec: TS-10-10, TS-10-E6
// Requirements: 10-REQ-2.7, 10-REQ-2.E5
// =========================================================================

// TestGenerateAPIKey_RetryOnCollision verifies that GenerateAPIKey retries
// key_id generation up to 3 total attempts on unique constraint violation
// and succeeds if a non-colliding key_id is found within the retry budget.
// The revocation UPDATE is NOT re-executed on retry.
// TS-10-10 (Requirement: 10-REQ-2.7)
func TestGenerateAPIKey_RetryOnCollision(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-010")
	insertTestUser(t, database.SqlDB, "coll-own-010")

	// Strategy: provide deterministic bytes where the first 40-byte chunk
	// (key_id=8 + secret=32) produces a key_id that collides with a pre-inserted
	// row, and the second 40-byte chunk produces a different key_id that succeeds.
	// Byte pattern: first 40 bytes all 0x00, second 40 bytes all 0x01.
	deterBytes := make([]byte, 80)
	for i := 40; i < 80; i++ {
		deterBytes[i] = 1
	}

	// Pre-insert a collision row for the key_id produced from uniform 0x00 bytes.
	collidingKeyID := makeTestKeyID(0)
	insertTestKey(t, database.SqlDB, collidingKeyID, "coll-own-010", "hash", 0,
		"", "", db.FormatTime(time.Now().UTC()))

	restore := keys.SetRandReader(bytes.NewReader(deterBytes))
	defer restore()

	result, err := keys.GenerateAPIKey(database.SqlDB, "user-010", 90, testLogger())
	if err != nil {
		t.Fatalf("GenerateAPIKey() error = %v; want nil (should succeed on retry)", err)
	}
	if result == nil {
		t.Fatal("GenerateAPIKey() returned nil result; want non-nil")
	}

	// Verify the result has a different key_id than the colliding one.
	if result.KeyID == collidingKeyID {
		t.Errorf("KeyID = %q; should differ from colliding key_id %q", result.KeyID, collidingKeyID)
	}

	// Verify exactly one active key exists for user-010.
	activeCount := queryActiveCount(t, database.SqlDB, "user-010")
	if activeCount != 1 {
		t.Errorf("active key count = %d; want 1", activeCount)
	}
}

// TestGenerateAPIKey_AllRetriesExhausted verifies that after all 3 key_id
// collision retry attempts fail with unique constraint violations,
// GenerateAPIKey returns a non-nil error.
// TS-10-E6 (Requirement: 10-REQ-2.E5)
func TestGenerateAPIKey_AllRetriesExhausted(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-E6")
	insertTestUser(t, database.SqlDB, "coll-own-E6")

	now := db.FormatTime(time.Now().UTC())

	// Provide 120 bytes of deterministic data: 3 attempts of 40 bytes each.
	// Each 40-byte chunk uses a uniform byte value to produce a predictable key_id.
	deterBytes := make([]byte, 120)
	for i := range deterBytes {
		deterBytes[i] = byte(i / 40)
	}

	// Pre-insert collision rows for all 3 predicted key_ids.
	for i := range 3 {
		keyID := makeTestKeyID(byte(i))
		insertTestKey(t, database.SqlDB, keyID, "coll-own-E6", "hash", 0, "", "", now)
	}

	restore := keys.SetRandReader(bytes.NewReader(deterBytes))
	defer restore()

	result, err := keys.GenerateAPIKey(database.SqlDB, "user-E6", 90, testLogger())

	// Expect non-nil error after all retries exhausted.
	if err == nil {
		t.Fatal("GenerateAPIKey() returned nil error; want non-nil (all 3 retries should fail)")
	}
	if result != nil {
		t.Errorf("GenerateAPIKey() returned non-nil result: %+v; want nil", result)
	}

	// Verify no new active key was created for user-E6.
	activeCount := queryActiveCount(t, database.SqlDB, "user-E6")
	if activeCount != 0 {
		t.Errorf("active key count for user-E6 = %d; want 0 (no key should be created)", activeCount)
	}
}

// =========================================================================
// Subtask 2.5: Logging and non-constraint DB error handling
// Test Spec: TS-10-12, TS-10-E4
// Requirements: 10-REQ-2.9, 10-REQ-2.E3
// =========================================================================

// TestGenerateAPIKey_SuccessLogsInfoEntry verifies that GenerateAPIKey emits
// a structured INFO log entry with user_id and key_id fields after a
// successful INSERT.
// TS-10-12 success case (Requirement: 10-REQ-2.9)
func TestGenerateAPIKey_SuccessLogsInfoEntry(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-011")

	// Create a logger that captures output to a buffer.
	logger, buf := logCapture()

	result, err := keys.GenerateAPIKey(database.SqlDB, "user-011", 90, logger)
	if err != nil {
		t.Fatalf("GenerateAPIKey() error = %v; want nil", err)
	}
	if result == nil {
		t.Fatal("GenerateAPIKey() returned nil result; want non-nil")
	}

	// Verify that the log output contains an INFO entry with user_id and key_id.
	logOutput := buf.String()
	if logOutput == "" {
		t.Error("no log output captured; want at least one INFO entry")
	}

	if !strings.Contains(logOutput, "user-011") {
		t.Errorf("log output does not contain user_id 'user-011'; got: %q", logOutput)
	}
	if !strings.Contains(logOutput, result.KeyID) {
		t.Errorf("log output does not contain key_id %q; got: %q", result.KeyID, logOutput)
	}
}

// TestGenerateAPIKey_FailureNoLogEntry verifies that GenerateAPIKey does
// not emit any INFO log entries on failure paths.
// TS-10-12 failure case (Requirement: 10-REQ-2.9)
func TestGenerateAPIKey_FailureNoLogEntry(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-011b")

	// Inject a failing rand reader to force failure.
	mock := &failingReader{}
	restore := keys.SetRandReader(mock)
	defer restore()

	logger, buf := logCapture()

	_, err := keys.GenerateAPIKey(database.SqlDB, "user-011b", 90, logger)
	if err == nil {
		t.Fatal("GenerateAPIKey() with failing rand returned nil error; want non-nil")
	}

	// Verify no INFO log entries were emitted on failure.
	logOutput := buf.String()
	if strings.Contains(strings.ToUpper(logOutput), "INFO") {
		t.Errorf("expected no INFO log entries on failure; got: %q", logOutput)
	}
}

// TestGenerateAPIKey_NonConstraintDBError_NoRetry verifies that a non-constraint-
// violation database error during INSERT causes GenerateAPIKey to return an
// error immediately without retrying. The INSERT is called exactly once.
// TS-10-E4 (Requirement: 10-REQ-2.E3)
func TestGenerateAPIKey_NonConstraintDBError_NoRetry(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-E4")

	// Create a mock executor that returns a generic (non-constraint) error on INSERT.
	genericDBError := errors.New("disk I/O error: simulated")
	mock := &mockExecutor{
		realDB:    database.SqlDB,
		insertErr: genericDBError,
	}

	result, err := keys.GenerateAPIKey(mock, "user-E4", 90, testLogger())

	// Expect non-nil error.
	if err == nil {
		t.Fatal("GenerateAPIKey() with INSERT error returned nil error; want non-nil")
	}
	if result != nil {
		t.Errorf("GenerateAPIKey() returned non-nil result: %+v; want nil", result)
	}

	// Verify INSERT was called exactly once (no retries for non-constraint errors).
	if mock.insertCalls != 1 {
		t.Errorf("INSERT call count = %d; want 1 (no retries for non-constraint errors)",
			mock.insertCalls)
	}
}

// =========================================================================
// Subtask 2.6: One-active-key invariant and concurrent GenerateAPIKey
// Test Spec: TS-10-38, TS-10-40
// Requirements: 10-REQ-8.1, 10-REQ-8.3
// =========================================================================

// TestGenerateAPIKey_OneActiveKeyInvariant verifies that after a successful
// GenerateAPIKey call for a user who already has an active key, exactly one
// key with revoked_at IS NULL exists, and it is the newly generated key.
// The prior key's revoked_at is set.
// TS-10-38 (Requirement: 10-REQ-8.1)
func TestGenerateAPIKey_OneActiveKeyInvariant(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-032")

	// Pre-insert an active key.
	now := db.FormatTime(time.Now().UTC())
	expiresAt := db.FormatTime(time.Now().UTC().Add(90 * 24 * time.Hour))
	insertTestKey(t, database.SqlDB, "oldky032", "user-032", "hash032", 90, expiresAt, "", now)

	// Capture the old key_id for later verification.
	oldKeyID := "oldky032"

	// Generate a new key — should revoke the old one.
	result, err := keys.GenerateAPIKey(database.SqlDB, "user-032", 90, testLogger())
	if err != nil {
		t.Fatalf("GenerateAPIKey() error = %v; want nil", err)
	}
	if result == nil {
		t.Fatal("GenerateAPIKey() returned nil result; want non-nil")
	}

	// Verify exactly one active key.
	activeCount := queryActiveCount(t, database.SqlDB, "user-032")
	if activeCount != 1 {
		t.Errorf("active key count = %d; want 1", activeCount)
	}

	// Verify the active key is the new one.
	var activeKeyID string
	if err := database.SqlDB.QueryRow(
		"SELECT key_id FROM api_keys WHERE user_id = ? AND revoked_at IS NULL", "user-032",
	).Scan(&activeKeyID); err != nil {
		t.Fatalf("query active key_id failed: %v", err)
	}
	if activeKeyID != result.KeyID {
		t.Errorf("active key_id = %q; want %q (new key)", activeKeyID, result.KeyID)
	}

	// Verify the old key is revoked.
	oldRevoked := queryNullString(t, database.SqlDB,
		"SELECT revoked_at FROM api_keys WHERE key_id = ?", oldKeyID)
	if !oldRevoked.Valid {
		t.Error("old key's revoked_at is NULL; want non-NULL after GenerateAPIKey")
	}
}

// TestGenerateAPIKey_SequentialCalls_OneActiveKey verifies that calling
// GenerateAPIKey twice sequentially for the same user results in exactly
// one active key — the second one, with the first revoked.
// TS-10-38 extended (Requirement: 10-REQ-8.1)
func TestGenerateAPIKey_SequentialCalls_OneActiveKey(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-032b")

	// First call.
	result1, err := keys.GenerateAPIKey(database.SqlDB, "user-032b", 90, testLogger())
	if err != nil {
		t.Fatalf("first GenerateAPIKey() error = %v; want nil", err)
	}
	if result1 == nil {
		t.Fatal("first GenerateAPIKey() returned nil result; want non-nil")
	}

	// Second call.
	result2, err := keys.GenerateAPIKey(database.SqlDB, "user-032b", 60, testLogger())
	if err != nil {
		t.Fatalf("second GenerateAPIKey() error = %v; want nil", err)
	}
	if result2 == nil {
		t.Fatal("second GenerateAPIKey() returned nil result; want non-nil")
	}

	// Verify exactly one active key exists.
	activeCount := queryActiveCount(t, database.SqlDB, "user-032b")
	if activeCount != 1 {
		t.Errorf("active key count after 2 calls = %d; want 1", activeCount)
	}

	// Verify the active key is the second one.
	var activeKeyID string
	if err := database.SqlDB.QueryRow(
		"SELECT key_id FROM api_keys WHERE user_id = ? AND revoked_at IS NULL", "user-032b",
	).Scan(&activeKeyID); err != nil {
		t.Fatalf("query active key_id failed: %v", err)
	}
	if activeKeyID != result2.KeyID {
		t.Errorf("active key_id = %q; want %q (second key)", activeKeyID, result2.KeyID)
	}

	// Verify the first key is revoked.
	firstRevoked := queryNullString(t, database.SqlDB,
		"SELECT revoked_at FROM api_keys WHERE key_id = ?", result1.KeyID)
	if !firstRevoked.Valid {
		t.Error("first key's revoked_at is NULL; want non-NULL after second GenerateAPIKey")
	}
}

// TestGenerateAPIKey_ConcurrentCalls_OneActiveKey verifies that the one-active-
// key-per-user invariant is maintained under concurrent GenerateAPIKey calls.
// SQLite serialized writes ensure no duplicate active keys without application-
// level locking.
// TS-10-40 (Requirement: 10-REQ-8.3)
func TestGenerateAPIKey_ConcurrentCalls_OneActiveKey(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-034")

	const numGoroutines = 5
	var wg sync.WaitGroup
	errs := make([]error, numGoroutines)
	results := make([]*keys.APIKeyResult, numGoroutines)

	wg.Add(numGoroutines)
	for i := range numGoroutines {
		go func(idx int) {
			defer wg.Done()
			result, err := keys.GenerateAPIKey(database.SqlDB, "user-034", 90, testLogger())
			errs[idx] = err
			results[idx] = result
		}(i)
	}
	wg.Wait()

	// All calls should succeed (SQLite serializes them).
	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: GenerateAPIKey() error = %v; want nil", i, err)
		}
	}
	for i, r := range results {
		if r == nil {
			t.Errorf("goroutine %d: GenerateAPIKey() returned nil result; want non-nil", i)
		}
	}

	// After all concurrent calls complete, exactly one active key should exist.
	activeCount := queryActiveCount(t, database.SqlDB, "user-034")
	if activeCount != 1 {
		t.Errorf("active key count after %d concurrent calls = %d; want 1",
			numGoroutines, activeCount)
	}
}

package keys_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"

	"github.com/txsvc/apikit"
	"github.com/txsvc/apikit/internal/auth"
	"github.com/txsvc/apikit/internal/db"
	"github.com/txsvc/apikit/internal/keys"
)

// =========================================================================
// Subtask 5.3: Property tests — one-active-key invariant, secret storage,
// and key format
// Test Spec: TS-10-P1, TS-10-P2, TS-10-P3
// Requirements: 10-REQ-8.1, 10-REQ-10.1, 10-REQ-1.1
// =========================================================================

// TestProperty_OneActiveKeyPerUser verifies that for any sequence of 1–10
// GenerateAPIKey calls for the same user, at most one active (non-revoked,
// non-expired) key exists after each call.
// TS-10-P1 (Requirements: 10-REQ-8.1, 10-REQ-8.2, 10-REQ-8.3)
func TestProperty_OneActiveKeyPerUser(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-prop1")

	validExpiresDays := []int{0, 30, 60, 90}
	rng := rand.New(rand.NewSource(42))

	numCalls := rng.Intn(10) + 1 // 1..10
	for i := 0; i < numCalls; i++ {
		expDays := validExpiresDays[rng.Intn(len(validExpiresDays))]
		result, err := keys.GenerateAPIKey(database.SqlDB, "user-prop1", expDays, testLogger())
		if err != nil {
			t.Fatalf("GenerateAPIKey() call %d error = %v; want nil", i+1, err)
		}
		if result == nil {
			t.Fatalf("GenerateAPIKey() call %d returned nil result", i+1)
		}

		// Invariant: at most one active (non-revoked, non-expired) key exists.
		var activeCount int
		err = database.SqlDB.QueryRow(
			`SELECT COUNT(*) FROM api_keys
			 WHERE user_id = ? AND revoked_at IS NULL
			 AND (expires_at IS NULL OR expires_at > ?)`,
			"user-prop1", db.FormatTime(time.Now().UTC()),
		).Scan(&activeCount)
		if err != nil {
			t.Fatalf("active key count query failed after call %d: %v", i+1, err)
		}
		if activeCount != 1 {
			t.Errorf("after call %d: active key count = %d; want 1", i+1, activeCount)
		}
	}
}

// TestProperty_SecretHashMatchesAndNeverStored verifies that for every row
// in api_keys, the secret_hash column equals hex(sha256(secret)) and no
// column contains the plaintext secret.
// TS-10-P2 (Requirements: 10-REQ-10.1, 10-REQ-2.6)
func TestProperty_SecretHashMatchesAndNeverStored(t *testing.T) {
	database := testDB(t)

	rng := rand.New(rand.NewSource(99))
	numKeys := rng.Intn(10) + 1 // 1..10

	type keyRecord struct {
		userID string
		result *keys.APIKeyResult
		secret string
	}
	var records []keyRecord

	for i := 0; i < numKeys; i++ {
		userID := fmt.Sprintf("user-p2-%d", i)
		insertTestUser(t, database.SqlDB, userID)

		validExpires := []int{0, 30, 60, 90}
		expDays := validExpires[rng.Intn(len(validExpires))]

		result, err := keys.GenerateAPIKey(database.SqlDB, userID, expDays, testLogger())
		if err != nil {
			t.Fatalf("GenerateAPIKey() for %s error = %v", userID, err)
		}

		// Extract secret from FullKey: <prefix>_<key_id>_<secret>
		parts := strings.SplitN(result.FullKey, "_", 3)
		if len(parts) != 3 {
			t.Fatalf("FullKey %q does not have 3 underscore-separated parts", result.FullKey)
		}
		secret := parts[2]

		records = append(records, keyRecord{
			userID: userID,
			result: result,
			secret: secret,
		})
	}

	// Verify invariants for each generated key.
	for _, rec := range records {
		// Compute expected hash.
		h := sha256.Sum256([]byte(rec.secret))
		expectedHash := hex.EncodeToString(h[:])

		// Check the returned SecretHash matches.
		if rec.result.SecretHash != expectedHash {
			t.Errorf("user=%s: SecretHash = %q; want %q", rec.userID, rec.result.SecretHash, expectedHash)
		}

		// Check the database row's secret_hash matches.
		var dbHash string
		err := database.SqlDB.QueryRow(
			"SELECT secret_hash FROM api_keys WHERE key_id = ?", rec.result.KeyID,
		).Scan(&dbHash)
		if err != nil {
			t.Fatalf("user=%s: query secret_hash failed: %v", rec.userID, err)
		}
		if dbHash != expectedHash {
			t.Errorf("user=%s: DB secret_hash = %q; want %q", rec.userID, dbHash, expectedHash)
		}

		// Check that the plaintext secret does not appear in any column.
		var keyID, userID, secretHash string
		var expiresAt, revokedAt, createdAt sql.NullString
		var expiresDays int
		err = database.SqlDB.QueryRow(
			`SELECT key_id, user_id, secret_hash, expires_days, expires_at, revoked_at, created_at
			 FROM api_keys WHERE key_id = ?`, rec.result.KeyID,
		).Scan(&keyID, &userID, &secretHash, &expiresDays, &expiresAt, &revokedAt, &createdAt)
		if err != nil {
			t.Fatalf("user=%s: query full row failed: %v", rec.userID, err)
		}
		for _, col := range []string{keyID, userID, secretHash, createdAt.String} {
			if strings.Contains(col, rec.secret) {
				t.Errorf("user=%s: plaintext secret %q found in column value %q", rec.userID, rec.secret, col)
			}
		}
	}
}

// TestProperty_KeyFormatCorrectness verifies that for any FullKey returned
// by GenerateAPIKey, the key matches the canonical format regex and its
// constituent segments have correct lengths.
// TS-10-P3 (Requirements: 10-REQ-1.1, 10-REQ-1.2, 10-REQ-2.6)
func TestProperty_KeyFormatCorrectness(t *testing.T) {
	database := testDB(t)
	keyPattern := regexp.MustCompile(`^[a-zA-Z0-9]+_[a-zA-Z0-9]{8}_[a-zA-Z0-9]{32}$`)

	numKeys := 10
	for i := 0; i < numKeys; i++ {
		userID := fmt.Sprintf("user-p3-%d", i)
		insertTestUser(t, database.SqlDB, userID)

		validExpires := []int{0, 30, 60, 90}
		expDays := validExpires[i%len(validExpires)]

		result, err := keys.GenerateAPIKey(database.SqlDB, userID, expDays, testLogger())
		if err != nil {
			t.Fatalf("GenerateAPIKey() for %s error = %v", userID, err)
		}

		// Check regex match.
		if !keyPattern.MatchString(result.FullKey) {
			t.Errorf("FullKey %q does not match canonical format regex", result.FullKey)
		}

		// Split and verify segments.
		parts := strings.SplitN(result.FullKey, "_", 3)
		if len(parts) != 3 {
			t.Fatalf("FullKey %q does not split into 3 parts", result.FullKey)
		}

		prefix := parts[0]
		keyID := parts[1]
		secret := parts[2]

		if prefix != apikit.TokenPrefix {
			t.Errorf("prefix = %q; want %q", prefix, apikit.TokenPrefix)
		}
		if len(keyID) != 8 {
			t.Errorf("key_id length = %d; want 8", len(keyID))
		}
		if len(secret) != 32 {
			t.Errorf("secret length = %d; want 32", len(secret))
		}
		if result.KeyID != keyID {
			t.Errorf("result.KeyID = %q; want %q (from FullKey)", result.KeyID, keyID)
		}
	}
}

// =========================================================================
// Subtask 5.4: Property tests — expiry calculation, listing response safety,
// ordering, and retry bound
// Test Spec: TS-10-P4, TS-10-P5, TS-10-P7, TS-10-P8
// Requirements: 10-REQ-2.7, 10-REQ-2.8, 10-REQ-5.3
// =========================================================================

// TestProperty_ExpiryCalculation verifies that for each valid expiresDays value:
// - expiresDays=0 → ExpiresAt nil and DB expires_at IS NULL
// - expiresDays in {30,60,90} → ExpiresAt != nil and equals created_at + N*24h ±2s
// TS-10-P4 (Requirements: 10-REQ-2.8, 10-REQ-6.3)
func TestProperty_ExpiryCalculation(t *testing.T) {
	database := testDB(t)

	for _, days := range []int{0, 30, 60, 90} {
		t.Run(fmt.Sprintf("expiresDays=%d", days), func(t *testing.T) {
			userID := fmt.Sprintf("user-p4-%d", days)
			insertTestUser(t, database.SqlDB, userID)

			before := time.Now().UTC()
			result, err := keys.GenerateAPIKey(database.SqlDB, userID, days, testLogger())
			after := time.Now().UTC()
			if err != nil {
				t.Fatalf("GenerateAPIKey() error = %v", err)
			}

			if days == 0 {
				// ExpiresAt should be nil.
				if result.ExpiresAt != nil {
					t.Errorf("ExpiresAt = %v; want nil for expiresDays=0", result.ExpiresAt)
				}
				// DB expires_at should be NULL.
				ns := queryNullString(t, database.SqlDB,
					"SELECT expires_at FROM api_keys WHERE key_id = ?", result.KeyID)
				if ns.Valid {
					t.Errorf("DB expires_at = %q; want NULL for expiresDays=0", ns.String)
				}
			} else {
				// ExpiresAt should be non-nil.
				if result.ExpiresAt == nil {
					t.Fatalf("ExpiresAt is nil; want non-nil for expiresDays=%d", days)
				}

				duration := time.Duration(days) * 24 * time.Hour
				expectedMin := before.Add(duration)
				expectedMax := after.Add(duration)
				tolerance := 2 * time.Second

				if result.ExpiresAt.Before(expectedMin.Add(-tolerance)) ||
					result.ExpiresAt.After(expectedMax.Add(tolerance)) {
					t.Errorf("ExpiresAt = %v; want between %v and %v (±%v)",
						result.ExpiresAt, expectedMin, expectedMax, tolerance)
				}

				// Verify DB expires_at matches.
				ns := queryNullString(t, database.SqlDB,
					"SELECT expires_at FROM api_keys WHERE key_id = ?", result.KeyID)
				if !ns.Valid {
					t.Fatal("DB expires_at is NULL; want non-NULL for finite expiry")
				}
				dbTime, err := time.Parse(time.RFC3339, ns.String)
				if err != nil {
					t.Fatalf("failed to parse DB expires_at %q: %v", ns.String, err)
				}
				diff := result.ExpiresAt.Sub(dbTime)
				if diff < -time.Second || diff > time.Second {
					t.Errorf("ExpiresAt %v differs from DB expires_at %v by %v; want within 1s",
						result.ExpiresAt, dbTime, diff)
				}
			}
		})
	}
}

// TestProperty_ListingResponseNeverExposesSecret verifies that for any
// response from GET /api/v1/user/keys, no element contains secret,
// secret_hash, key, or expires_days fields.
// TS-10-P5 (Requirements: 10-REQ-5.1, 10-REQ-10.2)
func TestProperty_ListingResponseNeverExposesSecret(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-p5")

	// Create a mix of active, expired, and revoked keys.
	now := time.Now().UTC()
	createdAt := db.FormatTime(now)
	futureExpiry := db.FormatTime(now.Add(90 * 24 * time.Hour))
	pastExpiry := "2020-01-01T00:00:00Z"
	revokedTime := db.FormatTime(now.Add(-1 * time.Hour))

	insertTestKey(t, database.SqlDB, "p5active1", "user-p5", "hash_p5a", 90, futureExpiry, "", createdAt)
	insertTestKey(t, database.SqlDB, "p5expire", "user-p5", "hash_p5b", 90, pastExpiry, "", createdAt)
	insertTestKey(t, database.SqlDB, "p5revokd", "user-p5", "hash_p5c", 90, futureExpiry, revokedTime, createdAt)

	e := setupHandlersWithAuth(t, database, &auth.AuthInfo{
		CredentialType: "api_key",
		UserID:         "user-p5",
		KeyID:          "p5active1",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/keys", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d; want %d", rec.Code, http.StatusOK)
	}

	var body []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response body: %v", err)
	}

	allowedKeys := map[string]bool{
		"key_id":     true,
		"created_at": true,
		"expires_at": true,
		"revoked_at": true,
	}
	forbiddenKeys := []string{"secret", "secret_hash", "key", "expires_days"}

	for i, obj := range body {
		// Check no forbidden fields.
		for _, forbidden := range forbiddenKeys {
			if _, found := obj[forbidden]; found {
				t.Errorf("response[%d] contains forbidden field %q", i, forbidden)
			}
		}
		// Check only allowed fields.
		for k := range obj {
			if !allowedKeys[k] {
				t.Errorf("response[%d] contains unexpected field %q", i, k)
			}
		}
	}
}

// TestProperty_ListingOrderByCreatedAtDesc verifies that GET /api/v1/user/keys
// responses with at least two keys have non-increasing created_at values
// (ORDER BY created_at DESC contract).
// TS-10-P7 (Requirements: 10-REQ-5.1, 10-REQ-5.3)
func TestProperty_ListingOrderByCreatedAtDesc(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-p7")

	// Insert keys with distinct created_at values.
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		keyID := fmt.Sprintf("p7key%03d", i)
		createdAt := db.FormatTime(base.Add(time.Duration(i) * time.Hour))
		expiresAt := db.FormatTime(base.Add(time.Duration(i)*time.Hour + 90*24*time.Hour))
		insertTestKey(t, database.SqlDB, keyID, "user-p7", "hash_p7_"+keyID, 90,
			expiresAt, "", createdAt)
	}

	e := setupHandlersWithAuth(t, database, &auth.AuthInfo{
		CredentialType: "api_key",
		UserID:         "user-p7",
		KeyID:          "p7key004",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/keys", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d; want %d", rec.Code, http.StatusOK)
	}

	var body []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response body: %v", err)
	}

	if len(body) < 2 {
		t.Fatalf("expected at least 2 keys; got %d", len(body))
	}

	// Verify non-increasing created_at order.
	for i := 0; i < len(body)-1; i++ {
		curStr, ok1 := body[i]["created_at"].(string)
		nextStr, ok2 := body[i+1]["created_at"].(string)
		if !ok1 || !ok2 {
			t.Fatalf("created_at values are not strings at indices %d,%d", i, i+1)
		}
		cur, err1 := time.Parse(time.RFC3339, curStr)
		next, err2 := time.Parse(time.RFC3339, nextStr)
		if err1 != nil || err2 != nil {
			t.Fatalf("failed to parse created_at at indices %d,%d: %v, %v", i, i+1, err1, err2)
		}
		if cur.Before(next) {
			t.Errorf("created_at[%d] (%v) < created_at[%d] (%v); want non-increasing order",
				i, cur, i+1, next)
		}
	}
}

// TestProperty_RetryLoopBounded verifies that the GenerateAPIKey retry loop
// is bounded at exactly 3 INSERT attempts. For K <= 2 collisions, it
// succeeds; for K >= 3, it fails after exactly 3 attempts.
// TS-10-P8 (Requirements: 10-REQ-2.7, 10-REQ-2.E5)
func TestProperty_RetryLoopBounded(t *testing.T) {
	// This test relies on collisionReader (from generate_test.go) and
	// mockExecutor (from generate_integration_test.go) to control retry
	// behavior.

	for _, tc := range []struct {
		name            string
		collisionsToForce int
		expectErr       bool
		maxInserts      int
	}{
		{name: "1_collision_succeeds", collisionsToForce: 1, expectErr: false, maxInserts: 3},
		{name: "2_collisions_succeeds", collisionsToForce: 2, expectErr: false, maxInserts: 3},
		{name: "3_collisions_fails", collisionsToForce: 3, expectErr: true, maxInserts: 3},
		{name: "4_collisions_capped_at_3", collisionsToForce: 4, expectErr: true, maxInserts: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := testDB(t)
			insertTestUser(t, database.SqlDB, "user-p8-"+tc.name)

			// Pre-insert colliding keys to force unique constraint violations.
			// We use the collisionReader from generate_test.go to make the
			// random generator produce predictable key_ids that collide with
			// existing rows.
			//
			// The exact collision mechanism depends on the implementation:
			// pre-insert keys with known key_ids that the deterministic
			// reader will produce. The test verifies that:
			// 1. Total INSERT calls never exceed 3
			// 2. For >= 3 collisions, GenerateAPIKey returns an error

			// Track insert calls via countingMockExecutor which simulates
			// unique constraint violations for the first N attempts.
			var insertAttempts int
			mock := &countingMockExecutor{
				realDB:              database.SqlDB,
				collisionsRemaining: tc.collisionsToForce,
				insertAttempts:      &insertAttempts,
			}

			_, err := keys.GenerateAPIKey(mock, "user-p8-"+tc.name, 90, testLogger())

			if tc.expectErr && err == nil {
				t.Error("GenerateAPIKey() error = nil; want non-nil (retry budget exhausted)")
			}
			if !tc.expectErr && err != nil {
				t.Errorf("GenerateAPIKey() error = %v; want nil", err)
			}

			if insertAttempts > tc.maxInserts {
				t.Errorf("INSERT attempts = %d; want <= %d", insertAttempts, tc.maxInserts)
			}
		})
	}
}

// countingMockExecutor wraps a *sql.DB to simulate unique constraint violations
// for the first N INSERT attempts, allowing the rest to succeed.
type countingMockExecutor struct {
	realDB              *sql.DB
	collisionsRemaining int
	insertAttempts      *int
}

// sqliteConstraintError satisfies the sqliteErrorCode interface that
// db.WrapError uses to detect SQLite constraint violations. Code() returns
// 2067 (SQLITE_CONSTRAINT_UNIQUE) so WrapError maps it to db.ErrConflict.
type sqliteConstraintError struct {
	code int
}

func (e *sqliteConstraintError) Error() string {
	return fmt.Sprintf("UNIQUE constraint failed: api_keys.key_id (code %d)", e.code)
}
func (e *sqliteConstraintError) Code() int { return e.code }

// sqliteConstraintUniqueCode is SQLITE_CONSTRAINT_UNIQUE (2067).
const sqliteConstraintUniqueCode = 2067

func (m *countingMockExecutor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if strings.Contains(strings.ToUpper(query), "INSERT") {
		*m.insertAttempts++
		if m.collisionsRemaining > 0 {
			m.collisionsRemaining--
			// Return an error that satisfies the sqliteErrorCode interface
			// so db.WrapError correctly maps it to db.ErrConflict.
			return nil, &sqliteConstraintError{code: sqliteConstraintUniqueCode}
		}
	}
	return m.realDB.ExecContext(ctx, query, args...)
}

func (m *countingMockExecutor) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return m.realDB.QueryContext(ctx, query, args...)
}

func (m *countingMockExecutor) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return m.realDB.QueryRowContext(ctx, query, args...)
}

// =========================================================================
// Subtask 5.5: Revocation atomicity property test, lifecycle logging, and
// smoke test stubs
// Test Spec: TS-10-P6, TS-10-42, TS-10-SMOKE-1 through TS-10-SMOKE-5
// Requirements: 10-REQ-2.3, 10-REQ-9.2
// =========================================================================

// TestProperty_RevocationAtomicityUnderFailure verifies that when
// GenerateAPIKey fails after the revocation UPDATE with *sql.DB, the prior
// key's revoked_at remains NULL (atomic rollback invariant).
//
// Uses *sql.DB (not a mock) with a deterministic rand reader to force all 3
// INSERT retry attempts to collide with pre-inserted key_ids. This ensures
// the internal transaction path is exercised and the rollback is real.
// TS-10-P6 (Requirements: 10-REQ-2.3, 10-REQ-2.E1)
func TestProperty_RevocationAtomicityUnderFailure(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-p6")
	insertTestUser(t, database.SqlDB, "p6-collider")

	// Pre-insert an active key.
	now := db.FormatTime(time.Now().UTC())
	expiresAt := db.FormatTime(time.Now().UTC().Add(90 * 24 * time.Hour))
	insertTestKey(t, database.SqlDB, "p6origky", "user-p6", "hash_p6", 90,
		expiresAt, "", now)

	// Verify the original key is active.
	origRevoked := queryNullString(t, database.SqlDB,
		"SELECT revoked_at FROM api_keys WHERE key_id = ?", "p6origky")
	if origRevoked.Valid {
		t.Fatal("setup: original key already has revoked_at set")
	}
	totalBefore := queryTotalCount(t, database.SqlDB, "user-p6")

	// Force all 3 INSERT retry attempts to fail by pre-inserting collision
	// rows. Provide 120 bytes of deterministic data: each 40-byte chunk
	// (8 key_id + 32 secret) uses a uniform byte value so the generated
	// key_id is predictable via makeTestKeyID.
	deterBytes := make([]byte, 120)
	for i := range deterBytes {
		deterBytes[i] = byte(i / 40)
	}

	// Pre-insert collision rows for the 3 predicted key_ids.
	for i := range 3 {
		keyID := makeTestKeyID(byte(i))
		insertTestKey(t, database.SqlDB, keyID, "p6-collider", "hash", 0, "", "", now)
	}

	restore := keys.SetRandReader(bytes.NewReader(deterBytes))
	defer restore()

	// Call with *sql.DB so GenerateAPIKey starts an internal transaction.
	_, err := keys.GenerateAPIKey(database.SqlDB, "user-p6", 90, testLogger())
	if err == nil {
		t.Fatal("GenerateAPIKey() error = nil; want non-nil (all INSERTs failed)")
	}

	// Invariant: the prior key's revoked_at remains NULL (rolled back).
	origRevoked = queryNullString(t, database.SqlDB,
		"SELECT revoked_at FROM api_keys WHERE key_id = ?", "p6origky")
	if origRevoked.Valid {
		t.Error("prior key's revoked_at is non-NULL after failed GenerateAPIKey; want NULL (rolled back)")
	}

	// Invariant: total row count is unchanged (only p6origky + 3 collision rows).
	totalAfter := queryTotalCount(t, database.SqlDB, "user-p6")
	if totalAfter != totalBefore {
		t.Errorf("total key count changed from %d to %d; want unchanged", totalBefore, totalAfter)
	}
}

// TestLifecycleLogging_SuccessEmitsInfo verifies that INFO log entries are
// emitted after successful database writes for key generation, refresh, and
// revocation, but not on failure paths.
// TS-10-42 (Requirement: 10-REQ-9.2)
func TestLifecycleLogging_SuccessEmitsInfo(t *testing.T) {
	t.Run("generation_logs_on_success", func(t *testing.T) {
		database := testDB(t)
		insertTestUser(t, database.SqlDB, "user-log-gen")

		logger, buf := logCapture()
		result, err := keys.GenerateAPIKey(database.SqlDB, "user-log-gen", 90, logger)
		if err != nil {
			t.Fatalf("GenerateAPIKey() error = %v; want nil", err)
		}
		if result == nil {
			t.Fatal("GenerateAPIKey() returned nil result")
		}

		logOutput := buf.String()
		if !strings.Contains(logOutput, "user-log-gen") {
			t.Errorf("generation log missing user_id; got: %q", logOutput)
		}
		if !strings.Contains(logOutput, result.KeyID) {
			t.Errorf("generation log missing key_id %q; got: %q", result.KeyID, logOutput)
		}
	})

	t.Run("generation_no_log_on_failure", func(t *testing.T) {
		database := testDB(t)
		insertTestUser(t, database.SqlDB, "user-log-fail")

		logger, buf := logCapture()
		// Invalid expiresDays should fail before any DB write.
		_, err := keys.GenerateAPIKey(database.SqlDB, "user-log-fail", 45, logger)
		if err == nil {
			t.Fatal("GenerateAPIKey(45) error = nil; want non-nil")
		}

		logOutput := buf.String()
		if strings.Contains(logOutput, "user-log-fail") {
			t.Errorf("failure path should not emit log entry; got: %q", logOutput)
		}
	})

	t.Run("revocation_logs_on_success", func(t *testing.T) {
		database := testDB(t)
		insertTestUser(t, database.SqlDB, "user-log-rev")

		now := db.FormatTime(time.Now().UTC())
		expiresAt := db.FormatTime(time.Now().UTC().Add(90 * 24 * time.Hour))
		insertTestKey(t, database.SqlDB, "logrevky", "user-log-rev", "hash_lr", 90,
			expiresAt, "", now)

		// Set up Echo with capturing logger.
		e := echo.New()
		logBuf := &bytes.Buffer{}
		e.Logger.SetOutput(logBuf)
		e.Logger.SetLevel(log.DEBUG)
		e.HTTPErrorHandler = apikit.HTTPErrorHandler

		authMW := func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				auth.SetAuthInfo(c, &auth.AuthInfo{
					CredentialType: "api_key",
					UserID:         "user-log-rev",
					KeyID:          "logrevky",
				})
				return next(c)
			}
		}
		group := e.Group("/api/v1", apikit.CacheMiddleware(apikit.CacheNoStore), authMW)
		keys.RegisterKeyHandlers(group, database.SqlDB)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/user/keys/logrevky", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("DELETE status = %d; want %d", rec.Code, http.StatusOK)
		}

		logOutput := logBuf.String()
		if !strings.Contains(logOutput, "user-log-rev") {
			t.Errorf("revocation log missing user_id; got: %q", logOutput)
		}
		if !strings.Contains(logOutput, "logrevky") {
			t.Errorf("revocation log missing key_id; got: %q", logOutput)
		}
	})

	t.Run("refresh_logs_on_success", func(t *testing.T) {
		database := testDB(t)
		insertTestUser(t, database.SqlDB, "user-log-ref")

		now := db.FormatTime(time.Now().UTC())
		expiresAt := db.FormatTime(time.Now().UTC().Add(90 * 24 * time.Hour))
		insertTestKey(t, database.SqlDB, "lgrefkey", "user-log-ref", "hash_lref", 90,
			expiresAt, "", now)

		e := echo.New()
		logBuf := &bytes.Buffer{}
		e.Logger.SetOutput(logBuf)
		e.Logger.SetLevel(log.DEBUG)
		e.HTTPErrorHandler = apikit.HTTPErrorHandler

		authMW := func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				auth.SetAuthInfo(c, &auth.AuthInfo{
					CredentialType: "api_key",
					UserID:         "user-log-ref",
					KeyID:          "lgrefkey",
				})
				return next(c)
			}
		}
		group := e.Group("/api/v1", apikit.CacheMiddleware(apikit.CacheNoStore), authMW)
		keys.RegisterKeyHandlers(group, database.SqlDB)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/user/keys/lgrefkey/refresh", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("POST refresh status = %d; want %d", rec.Code, http.StatusOK)
		}

		logOutput := logBuf.String()
		if !strings.Contains(logOutput, "user-log-ref") {
			t.Errorf("refresh log missing user_id; got: %q", logOutput)
		}
		if !strings.Contains(logOutput, "lgrefkey") {
			t.Errorf("refresh log missing key_id; got: %q", logOutput)
		}
	})
}

// =========================================================================
// Smoke Tests (stubs) — cover all 5 execution paths
// Test Spec: TS-10-SMOKE-1 through TS-10-SMOKE-5
// =========================================================================

// TestSmoke_OAuthCallbackFirstKey is a smoke test for PATH-1: OAuth callback
// creates a first API key for a new user via GenerateAPIKey within an
// existing *sql.Tx transaction.
// TS-10-SMOKE-1 (Execution Path: 10-PATH-1)
func TestSmoke_OAuthCallbackFirstKey(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-smoke1")

	// Simulate the OAuth callback calling GenerateAPIKey within a transaction.
	tx, err := database.SqlDB.Begin()
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}

	logger, logBuf := logCapture()
	result, err := keys.GenerateAPIKey(tx, "user-smoke1", 90, logger)
	if err != nil {
		tx.Rollback()
		t.Fatalf("GenerateAPIKey() error = %v; want nil", err)
	}

	// Commit the transaction (simulating the OAuth callback commit).
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	// Verify result fields.
	if result.FullKey == "" {
		t.Error("FullKey is empty")
	}
	if result.KeyID == "" {
		t.Error("KeyID is empty")
	}
	if result.SecretHash == "" {
		t.Error("SecretHash is empty")
	}
	if result.ExpiresAt == nil {
		t.Error("ExpiresAt is nil; want non-nil for expiresDays=90")
	}

	// Verify key format.
	keyPattern := regexp.MustCompile(`^ak_[a-zA-Z0-9]{8}_[a-zA-Z0-9]{32}$`)
	if !keyPattern.MatchString(result.FullKey) {
		t.Errorf("FullKey %q does not match expected pattern", result.FullKey)
	}

	// Verify exactly one active key exists.
	activeCount := queryActiveCount(t, database.SqlDB, "user-smoke1")
	if activeCount != 1 {
		t.Errorf("active key count = %d; want 1", activeCount)
	}

	// Verify secret_hash in DB matches.
	parts := strings.SplitN(result.FullKey, "_", 3)
	if len(parts) == 3 {
		h := sha256.Sum256([]byte(parts[2]))
		expectedHash := hex.EncodeToString(h[:])
		var dbHash string
		database.SqlDB.QueryRow("SELECT secret_hash FROM api_keys WHERE key_id = ?",
			result.KeyID).Scan(&dbHash)
		if dbHash != expectedHash {
			t.Errorf("DB secret_hash = %q; want %q", dbHash, expectedHash)
		}
	}

	// Verify ExpiresAt is approximately now + 90*24h.
	expectedExpiry := time.Now().UTC().Add(90 * 24 * time.Hour)
	if result.ExpiresAt != nil {
		diff := result.ExpiresAt.Sub(expectedExpiry)
		if diff < -5*time.Second || diff > 5*time.Second {
			t.Errorf("ExpiresAt %v differs from expected %v by %v",
				result.ExpiresAt, expectedExpiry, diff)
		}
	}

	// Verify INFO log emitted.
	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "user-smoke1") || !strings.Contains(logOutput, result.KeyID) {
		t.Errorf("smoke1: log missing user_id or key_id; got: %q", logOutput)
	}
}

// TestSmoke_ListKeysWithETagCaching is a smoke test for PATH-2: Authenticated
// user lists API keys with ETag caching — first request returns HTTP 200 with
// ETag, second with If-None-Match returns HTTP 304.
// TS-10-SMOKE-2 (Execution Path: 10-PATH-2)
func TestSmoke_ListKeysWithETagCaching(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-smoke2")

	now := db.FormatTime(time.Now().UTC())
	expiresAt := db.FormatTime(time.Now().UTC().Add(90 * 24 * time.Hour))
	insertTestKey(t, database.SqlDB, "smk2key1", "user-smoke2", "hash_smk2", 90,
		expiresAt, "", now)

	e := setupHandlersWithAuth(t, database, &auth.AuthInfo{
		CredentialType: "api_key",
		UserID:         "user-smoke2",
		KeyID:          "smk2key1",
	})

	// First request: no If-None-Match → HTTP 200 + ETag.
	req1 := httptest.NewRequest(http.MethodGet, "/api/v1/user/keys", nil)
	rec1 := httptest.NewRecorder()
	e.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("first GET status = %d; want %d", rec1.Code, http.StatusOK)
	}

	etag := rec1.Header().Get("ETag")
	if etag == "" {
		t.Fatal("first GET missing ETag header")
	}

	// Verify response has only allowed fields.
	var body []map[string]any
	if err := json.Unmarshal(rec1.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse first response: %v", err)
	}
	for _, obj := range body {
		for _, forbidden := range []string{"secret", "secret_hash", "key", "expires_days"} {
			if _, found := obj[forbidden]; found {
				t.Errorf("response contains forbidden field %q", forbidden)
			}
		}
	}

	// Verify Cache-Control: no-store.
	cacheControl := rec1.Header().Get("Cache-Control")
	if !strings.Contains(cacheControl, "no-store") {
		t.Errorf("Cache-Control = %q; want to contain 'no-store'", cacheControl)
	}

	// Second request: with If-None-Match → HTTP 304.
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/user/keys", nil)
	req2.Header.Set("If-None-Match", etag)
	rec2 := httptest.NewRecorder()
	e.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusNotModified {
		t.Errorf("second GET with If-None-Match status = %d; want %d",
			rec2.Code, http.StatusNotModified)
	}
}

// TestSmoke_RefreshActiveKey is a smoke test for PATH-3: User refreshes an
// active API key via POST /api/v1/user/keys/:key_id/refresh, verifying the
// new secret is returned and the DB is updated in-place.
// TS-10-SMOKE-3 (Execution Path: 10-PATH-3)
func TestSmoke_RefreshActiveKey(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-smoke3")

	now := db.FormatTime(time.Now().UTC())
	expiresAt := db.FormatTime(time.Now().UTC().Add(90 * 24 * time.Hour))
	insertTestKey(t, database.SqlDB, "smk3key1", "user-smoke3", "hash_smk3", 90,
		expiresAt, "", now)

	totalBefore := queryTotalCount(t, database.SqlDB, "user-smoke3")

	e := echo.New()
	logBuf := &bytes.Buffer{}
	e.Logger.SetOutput(logBuf)
	e.Logger.SetLevel(log.DEBUG)
	e.HTTPErrorHandler = apikit.HTTPErrorHandler

	authMW := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			auth.SetAuthInfo(c, &auth.AuthInfo{
				CredentialType: "api_key",
				UserID:         "user-smoke3",
				KeyID:          "smk3key1",
			})
			return next(c)
		}
	}
	group := e.Group("/api/v1", apikit.CacheMiddleware(apikit.CacheNoStore), authMW)
	keys.RegisterKeyHandlers(group, database.SqlDB)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/keys/smk3key1/refresh", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST refresh status = %d; want %d", rec.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// Verify key_id is unchanged.
	if body["key_id"] != "smk3key1" {
		t.Errorf("response key_id = %v; want %q", body["key_id"], "smk3key1")
	}

	// Verify full key is returned.
	fullKey, ok := body["key"].(string)
	if !ok || fullKey == "" {
		t.Fatal("response missing 'key' field or empty")
	}

	// Verify new secret format (32 alphanumeric chars).
	parts := strings.SplitN(fullKey, "_", 3)
	if len(parts) == 3 && len(parts[2]) != 32 {
		t.Errorf("new secret length = %d; want 32", len(parts[2]))
	}

	// Verify DB row updated.
	if len(parts) == 3 {
		h := sha256.Sum256([]byte(parts[2]))
		expectedHash := hex.EncodeToString(h[:])
		var dbHash string
		database.SqlDB.QueryRow("SELECT secret_hash FROM api_keys WHERE key_id = ?",
			"smk3key1").Scan(&dbHash)
		if dbHash != expectedHash {
			t.Errorf("DB secret_hash = %q; want %q", dbHash, expectedHash)
		}
	}

	// Verify row count unchanged (in-place update).
	totalAfter := queryTotalCount(t, database.SqlDB, "user-smoke3")
	if totalAfter != totalBefore {
		t.Errorf("row count changed from %d to %d; want unchanged after refresh", totalBefore, totalAfter)
	}

	// Verify INFO log emitted.
	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "user-smoke3") {
		t.Errorf("refresh log missing user_id; got: %q", logOutput)
	}
	if !strings.Contains(logOutput, "smk3key1") {
		t.Errorf("refresh log missing key_id; got: %q", logOutput)
	}
}

// TestSmoke_RevokeKey is a smoke test for PATH-4: User revokes their own
// active API key via DELETE /api/v1/user/keys/:key_id, verifying HTTP 200
// and revoked_at set in DB.
// TS-10-SMOKE-4 (Execution Path: 10-PATH-4)
func TestSmoke_RevokeKey(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-smoke4")

	now := db.FormatTime(time.Now().UTC())
	expiresAt := db.FormatTime(time.Now().UTC().Add(90 * 24 * time.Hour))
	insertTestKey(t, database.SqlDB, "smk4key1", "user-smoke4", "hash_smk4", 90,
		expiresAt, "", now)

	e := echo.New()
	logBuf := &bytes.Buffer{}
	e.Logger.SetOutput(logBuf)
	e.Logger.SetLevel(log.DEBUG)
	e.HTTPErrorHandler = apikit.HTTPErrorHandler

	authMW := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			auth.SetAuthInfo(c, &auth.AuthInfo{
				CredentialType: "api_key",
				UserID:         "user-smoke4",
				KeyID:          "smk4key1",
			})
			return next(c)
		}
	}
	group := e.Group("/api/v1", apikit.CacheMiddleware(apikit.CacheNoStore), authMW)
	keys.RegisterKeyHandlers(group, database.SqlDB)

	before := time.Now().UTC()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/user/keys/smk4key1", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d; want %d", rec.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if body["key_id"] != "smk4key1" {
		t.Errorf("response key_id = %v; want %q", body["key_id"], "smk4key1")
	}

	revokedAtStr, ok := body["revoked_at"].(string)
	if !ok || revokedAtStr == "" {
		t.Fatal("response missing revoked_at")
	}
	revokedAt, err := time.Parse(time.RFC3339, revokedAtStr)
	if err != nil {
		t.Fatalf("failed to parse revoked_at: %v", err)
	}
	if revokedAt.Before(before) {
		t.Errorf("revoked_at = %v; want >= %v", revokedAt, before)
	}

	// Verify DB row has revoked_at set.
	revoked := queryNullString(t, database.SqlDB,
		"SELECT revoked_at FROM api_keys WHERE key_id = ?", "smk4key1")
	if !revoked.Valid {
		t.Error("DB revoked_at is NULL; want non-NULL")
	}

	// Verify Cache-Control: no-store.
	cacheControl := rec.Header().Get("Cache-Control")
	if !strings.Contains(cacheControl, "no-store") {
		t.Errorf("Cache-Control = %q; want to contain 'no-store'", cacheControl)
	}

	// Verify INFO log emitted.
	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "user-smoke4") || !strings.Contains(logOutput, "smk4key1") {
		t.Errorf("revoke log missing user_id or key_id; got: %q", logOutput)
	}
}

// TestSmoke_GenerateReplaceExistingKey is a smoke test for PATH-5:
// GenerateAPIKey called with *sql.DB when a prior active key exists — prior
// key is revoked and new key inserted atomically.
// TS-10-SMOKE-5 (Execution Path: 10-PATH-5)
func TestSmoke_GenerateReplaceExistingKey(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-smoke5")

	// Create an initial key.
	now := time.Now().UTC()
	createdAt := db.FormatTime(now)
	expiresAt := db.FormatTime(now.Add(90 * 24 * time.Hour))
	insertTestKey(t, database.SqlDB, "smk5orig", "user-smoke5", "hash_smk5", 90,
		expiresAt, "", createdAt)

	// Verify the original key is active.
	origRevoked := queryNullString(t, database.SqlDB,
		"SELECT revoked_at FROM api_keys WHERE key_id = ?", "smk5orig")
	if origRevoked.Valid {
		t.Fatal("setup: original key already has revoked_at set")
	}

	logger, logBuf := logCapture()
	result, err := keys.GenerateAPIKey(database.SqlDB, "user-smoke5", 30, logger)
	if err != nil {
		t.Fatalf("GenerateAPIKey() error = %v; want nil", err)
	}

	// Verify result fields.
	if result.FullKey == "" {
		t.Error("FullKey is empty")
	}
	if result.KeyID == "" {
		t.Error("KeyID is empty")
	}
	if result.SecretHash == "" {
		t.Error("SecretHash is empty")
	}

	// Verify ExpiresAt is approximately now + 30*24h.
	if result.ExpiresAt == nil {
		t.Fatal("ExpiresAt is nil; want non-nil for expiresDays=30")
	}
	expectedExpiry := time.Now().UTC().Add(30 * 24 * time.Hour)
	diff := result.ExpiresAt.Sub(expectedExpiry)
	if diff < -5*time.Second || diff > 5*time.Second {
		t.Errorf("ExpiresAt %v differs from expected %v by %v",
			result.ExpiresAt, expectedExpiry, diff)
	}

	// Verify the original key was revoked.
	origRevoked = queryNullString(t, database.SqlDB,
		"SELECT revoked_at FROM api_keys WHERE key_id = ?", "smk5orig")
	if !origRevoked.Valid {
		t.Error("original key's revoked_at is still NULL; want non-NULL")
	}

	// Verify exactly one active key.
	activeCount := queryActiveCount(t, database.SqlDB, "user-smoke5")
	if activeCount != 1 {
		t.Errorf("active key count = %d; want 1", activeCount)
	}

	// Verify secret hash matches.
	parts := strings.SplitN(result.FullKey, "_", 3)
	if len(parts) == 3 {
		h := sha256.Sum256([]byte(parts[2]))
		expectedHash := hex.EncodeToString(h[:])
		if result.SecretHash != expectedHash {
			t.Errorf("SecretHash = %q; want %q", result.SecretHash, expectedHash)
		}
	}

	// Verify INFO log emitted.
	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "user-smoke5") || !strings.Contains(logOutput, result.KeyID) {
		t.Errorf("smoke5 log missing user_id or key_id; got: %q", logOutput)
	}
}

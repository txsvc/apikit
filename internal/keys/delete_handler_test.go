package keys_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
// Subtask 5.1: DELETE happy path, validation errors, and expired-key deletion
// Test Spec: TS-10-30, TS-10-31, TS-10-32, TS-10-33, TS-10-34
// Requirements: 10-REQ-7.1, 10-REQ-7.2, 10-REQ-7.3, 10-REQ-7.4, 10-REQ-7.5
// =========================================================================

// TestDeleteKey_HappyPath verifies that DELETE /api/v1/user/keys/:key_id
// sets revoked_at in the database and returns HTTP 200 with key_id and
// revoked_at (RFC 3339 UTC timestamp).
// TS-10-30 (Requirement: 10-REQ-7.1)
func TestDeleteKey_HappyPath(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-025")

	now := time.Now().UTC()
	createdAt := db.FormatTime(now)
	expiresAt := db.FormatTime(now.Add(90 * 24 * time.Hour))
	insertTestKey(t, database.SqlDB, "delkey01", "user-025", "hash025", 90,
		expiresAt, "", createdAt)

	e := setupHandlersWithAuth(t, database, &auth.AuthInfo{
		CredentialType: "api_key",
		UserID:         "user-025",
		KeyID:          "delkey01",
	})

	before := time.Now().UTC()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/user/keys/delkey01", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d; want %d", rec.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response body: %v", err)
	}

	// Verify response key_id.
	if body["key_id"] != "delkey01" {
		t.Errorf("body.key_id = %v; want %q", body["key_id"], "delkey01")
	}

	// Verify response revoked_at is non-null and is a valid RFC 3339 timestamp.
	revokedAtStr, ok := body["revoked_at"].(string)
	if !ok || revokedAtStr == "" {
		t.Fatalf("body.revoked_at is missing or not a string; got %v", body["revoked_at"])
	}
	revokedAt, err := time.Parse(time.RFC3339, revokedAtStr)
	if err != nil {
		t.Fatalf("failed to parse revoked_at %q: %v", revokedAtStr, err)
	}
	if revokedAt.Before(before) {
		t.Errorf("revoked_at = %v; want >= %v", revokedAt, before)
	}

	// Verify database row has revoked_at set.
	var dbRevokedAt string
	scanErr := database.SqlDB.QueryRow(
		"SELECT revoked_at FROM api_keys WHERE key_id = ?", "delkey01",
	).Scan(&dbRevokedAt)
	if scanErr != nil {
		t.Fatalf("query revoked_at failed: %v", scanErr)
	}
	if dbRevokedAt == "" {
		t.Error("DB revoked_at is empty; want non-empty after DELETE")
	}
}

// TestDeleteKey_NotFound verifies that DELETE /api/v1/user/keys/:key_id
// returns HTTP 404 'key not found' when the key_id does not match any row,
// without format validation on the path parameter.
// TS-10-31 (Requirement: 10-REQ-7.2)
func TestDeleteKey_NotFound(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-026")
	// Insert a key so the user has auth, but query a nonexistent key_id.
	now := db.FormatTime(time.Now().UTC())
	expiresAt := db.FormatTime(time.Now().UTC().Add(90 * 24 * time.Hour))
	insertTestKey(t, database.SqlDB, "exist026", "user-026", "hash026", 90,
		expiresAt, "", now)

	e := setupHandlersWithAuth(t, database, &auth.AuthInfo{
		CredentialType: "api_key",
		UserID:         "user-026",
		KeyID:          "exist026",
	})

	keyIDs := []string{"doesnotexist", "abc", "!!bad!!", "way_too_long_key_id"}
	for _, keyID := range keyIDs {
		t.Run("keyID="+keyID, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodDelete, "/api/v1/user/keys/"+keyID, nil)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Fatalf("DELETE nonexistent key_id %q status = %d; want %d",
					keyID, rec.Code, http.StatusNotFound)
			}

			var errResp map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
				t.Fatalf("failed to parse error response: %v", err)
			}
			errObj, ok := errResp["error"].(map[string]any)
			if !ok {
				t.Fatalf("response missing 'error' object; got: %v", errResp)
			}
			if msg, ok := errObj["message"].(string); !ok || msg != "key not found" {
				t.Errorf("error.message = %v; want %q", errObj["message"], "key not found")
			}
		})
	}
}

// TestDeleteKey_WrongUser verifies that DELETE /api/v1/user/keys/:key_id
// returns HTTP 404 (not 403) when the key belongs to a different user, to
// prevent information leakage.
// TS-10-32 (Requirement: 10-REQ-7.3)
func TestDeleteKey_WrongUser(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-C")
	insertTestUser(t, database.SqlDB, "user-D")

	now := db.FormatTime(time.Now().UTC())
	expiresAt := db.FormatTime(time.Now().UTC().Add(90 * 24 * time.Hour))
	// Key belongs to user-C.
	insertTestKey(t, database.SqlDB, "keyCxxxx", "user-C", "hashC", 90,
		expiresAt, "", now)
	// user-D has their own key for auth.
	insertTestKey(t, database.SqlDB, "keyDxxxx", "user-D", "hashD", 90,
		expiresAt, "", now)

	// Authenticated as user-D, attempting to DELETE user-C's key.
	e := setupHandlersWithAuth(t, database, &auth.AuthInfo{
		CredentialType: "api_key",
		UserID:         "user-D",
		KeyID:          "keyDxxxx",
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/user/keys/keyCxxxx", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// Must be 404, NOT 403.
	if rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE wrong user status = %d; want %d (not 403)", rec.Code, http.StatusNotFound)
	}

	var errResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	errObj, ok := errResp["error"].(map[string]any)
	if !ok {
		t.Fatalf("response missing 'error' object; got: %v", errResp)
	}
	if msg, ok := errObj["message"].(string); !ok || msg != "key not found" {
		t.Errorf("error.message = %v; want %q", errObj["message"], "key not found")
	}
}

// TestDeleteKey_AlreadyRevoked verifies that DELETE /api/v1/user/keys/:key_id
// returns HTTP 400 'key is already revoked' when the key's revoked_at is
// already set.
// TS-10-33 (Requirement: 10-REQ-7.4)
func TestDeleteKey_AlreadyRevoked(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-027")

	now := db.FormatTime(time.Now().UTC())
	expiresAt := db.FormatTime(time.Now().UTC().Add(90 * 24 * time.Hour))
	revokedAt := db.FormatTime(time.Now().UTC().Add(-1 * time.Hour))
	// Already revoked key.
	insertTestKey(t, database.SqlDB, "alrdyrev", "user-027", "hash027", 90,
		expiresAt, revokedAt, now)
	// Need a separate active key for auth.
	insertTestKey(t, database.SqlDB, "actky027", "user-027", "hash027b", 90,
		expiresAt, "", now)

	e := setupHandlersWithAuth(t, database, &auth.AuthInfo{
		CredentialType: "api_key",
		UserID:         "user-027",
		KeyID:          "actky027",
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/user/keys/alrdyrev", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("DELETE already-revoked key status = %d; want %d", rec.Code, http.StatusBadRequest)
	}

	var errResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	errObj, ok := errResp["error"].(map[string]any)
	if !ok {
		t.Fatalf("response missing 'error' object; got: %v", errResp)
	}
	if msg, ok := errObj["message"].(string); !ok || msg != "key is already revoked" {
		t.Errorf("error.message = %v; want %q", errObj["message"], "key is already revoked")
	}
}

// TestDeleteKey_ExpiredButNotRevoked verifies that DELETE /api/v1/user/keys/:key_id
// permits revoking an expired-but-not-explicitly-revoked key, setting
// revoked_at explicitly for audit trail purposes.
// TS-10-34 (Requirement: 10-REQ-7.5)
func TestDeleteKey_ExpiredButNotRevoked(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-028")

	now := db.FormatTime(time.Now().UTC())
	futureExpiresAt := db.FormatTime(time.Now().UTC().Add(90 * 24 * time.Hour))
	// Expired key: expires_at in the past, revoked_at IS NULL.
	insertTestKey(t, database.SqlDB, "expnotrev", "user-028", "hash028", 90,
		"2020-01-01T00:00:00Z", "", now)
	// Active key for auth.
	insertTestKey(t, database.SqlDB, "actky028", "user-028", "hash028b", 90,
		futureExpiresAt, "", now)

	e := setupHandlersWithAuth(t, database, &auth.AuthInfo{
		CredentialType: "api_key",
		UserID:         "user-028",
		KeyID:          "actky028",
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/user/keys/expnotrev", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE expired-but-not-revoked key status = %d; want %d", rec.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response body: %v", err)
	}

	if body["key_id"] != "expnotrev" {
		t.Errorf("body.key_id = %v; want %q", body["key_id"], "expnotrev")
	}
	if body["revoked_at"] == nil {
		t.Error("body.revoked_at is nil; want non-nil")
	}

	// Verify DB row has revoked_at set.
	revoked := queryNullString(t, database.SqlDB,
		"SELECT revoked_at FROM api_keys WHERE key_id = ?", "expnotrev")
	if !revoked.Valid {
		t.Error("DB revoked_at is NULL; want non-NULL after DELETE on expired key")
	}
}

// =========================================================================
// Subtask 5.2: DELETE logging, auth, self-revocation, and DB error
// Test Spec: TS-10-35, TS-10-36, TS-10-37, TS-10-E14
// Requirements: 10-REQ-7.6, 10-REQ-7.7, 10-REQ-7.8, 10-REQ-7.E1
// =========================================================================

// TestDeleteKey_LogsInfoEntry verifies that a successful DELETE emits a
// structured INFO log entry with user_id and key_id fields via c.Logger().
// TS-10-35 (Requirement: 10-REQ-7.6)
func TestDeleteKey_LogsInfoEntry(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-029")

	now := db.FormatTime(time.Now().UTC())
	expiresAt := db.FormatTime(time.Now().UTC().Add(90 * 24 * time.Hour))
	insertTestKey(t, database.SqlDB, "logdelky", "user-029", "hash029", 90,
		expiresAt, "", now)

	// Create Echo with a capturing logger.
	e := echo.New()
	logBuf := &bytes.Buffer{}
	e.Logger.SetOutput(logBuf)
	e.Logger.SetLevel(log.DEBUG)
	e.HTTPErrorHandler = apikit.HTTPErrorHandler

	authMW := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			auth.SetAuthInfo(c, &auth.AuthInfo{
				CredentialType: "api_key",
				UserID:         "user-029",
				KeyID:          "logdelky",
			})
			return next(c)
		}
	}
	group := e.Group("/api/v1", apikit.CacheMiddleware(apikit.CacheNoStore), authMW)
	keys.RegisterKeyHandlers(group, database.SqlDB)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/user/keys/logdelky", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d; want %d", rec.Code, http.StatusOK)
	}

	// Verify log output contains user_id and key_id.
	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "user-029") {
		t.Errorf("log output does not contain user_id 'user-029'; got: %q", logOutput)
	}
	if !strings.Contains(logOutput, "logdelky") {
		t.Errorf("log output does not contain key_id 'logdelky'; got: %q", logOutput)
	}
}

// TestDeleteKey_SelfRevocation verifies that a user can DELETE the key they
// are currently authenticating with, completing with HTTP 200. The key is
// marked as revoked in the database.
// TS-10-36 (Requirement: 10-REQ-7.7)
func TestDeleteKey_SelfRevocation(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-030")

	now := db.FormatTime(time.Now().UTC())
	expiresAt := db.FormatTime(time.Now().UTC().Add(90 * 24 * time.Hour))
	insertTestKey(t, database.SqlDB, "selfrevk", "user-030", "hash030", 90,
		expiresAt, "", now)

	// Authenticated with the same key being revoked.
	e := setupHandlersWithAuth(t, database, &auth.AuthInfo{
		CredentialType: "api_key",
		UserID:         "user-030",
		KeyID:          "selfrevk",
	})

	// First request: self-revocation should succeed.
	req1 := httptest.NewRequest(http.MethodDelete, "/api/v1/user/keys/selfrevk", nil)
	rec1 := httptest.NewRecorder()
	e.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("self-revocation DELETE status = %d; want %d", rec1.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.Unmarshal(rec1.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response body: %v", err)
	}
	if body["revoked_at"] == nil {
		t.Error("body.revoked_at is nil; want non-nil after self-revocation")
	}

	// Verify the key is revoked in the database.
	revoked := queryNullString(t, database.SqlDB,
		"SELECT revoked_at FROM api_keys WHERE key_id = ?", "selfrevk")
	if !revoked.Valid {
		t.Error("DB revoked_at is NULL; want non-NULL after self-revocation")
	}

	// Note: TS-10-36 also specifies that a subsequent request with the same
	// key should be rejected by auth middleware (HTTP 401). Since the auth
	// middleware is mocked in these tests (injecting user_id directly), and
	// the test spec says "authentication failures handled by auth middleware",
	// we verify the key is marked revoked in the DB. The middleware behavior
	// is tested in the auth middleware tests (spec 05).
}

// TestDeleteKey_APIKeyAuth verifies that DELETE /api/v1/user/keys/:key_id
// accepts authentication via API key and returns HTTP 200.
// TS-10-37 part 1 (Requirement: 10-REQ-7.8)
func TestDeleteKey_APIKeyAuth(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-031")

	now := db.FormatTime(time.Now().UTC())
	expiresAt := db.FormatTime(time.Now().UTC().Add(90 * 24 * time.Hour))
	insertTestKey(t, database.SqlDB, "delkeyAA", "user-031", "hash031a", 90,
		expiresAt, "", now)

	e := setupHandlersWithAuth(t, database, &auth.AuthInfo{
		CredentialType: "api_key",
		UserID:         "user-031",
		KeyID:          "delkeyAA",
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/user/keys/delkeyAA", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("DELETE with API key auth status = %d; want %d", rec.Code, http.StatusOK)
	}
}

// TestDeleteKey_PATWithKeysManageAuth verifies that DELETE /api/v1/user/keys/:key_id
// accepts authentication via PAT with keys:manage permission and returns HTTP 200.
// TS-10-37 part 2 (Requirement: 10-REQ-7.8)
func TestDeleteKey_PATWithKeysManageAuth(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-031b")

	now := db.FormatTime(time.Now().UTC())
	expiresAt := db.FormatTime(time.Now().UTC().Add(90 * 24 * time.Hour))
	insertTestKey(t, database.SqlDB, "delkeyBB", "user-031b", "hash031b", 90,
		expiresAt, "", now)

	e := setupHandlersWithAuth(t, database, &auth.AuthInfo{
		CredentialType: "pat",
		UserID:         "user-031b",
		TokenID:        "pat-token-031",
		Permissions:    []string{"keys:manage"},
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/user/keys/delkeyBB", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("DELETE with PAT keys:manage auth status = %d; want %d", rec.Code, http.StatusOK)
	}
}

// TestDeleteKey_DBError verifies that a database error during DELETE SELECT
// or UPDATE returns HTTP 500 'internal server error'.
// TS-10-E14 (Requirement: 10-REQ-7.E1)
func TestDeleteKey_DBError(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-E14")

	now := db.FormatTime(time.Now().UTC())
	expiresAt := db.FormatTime(time.Now().UTC().Add(90 * 24 * time.Hour))
	insertTestKey(t, database.SqlDB, "dbkyE14x", "user-E14", "hashE14", 90,
		expiresAt, "", now)

	// Close the database to force all queries to fail.
	database.SqlDB.Close()

	e := echo.New()
	e.HTTPErrorHandler = apikit.HTTPErrorHandler
	authMW := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			auth.SetAuthInfo(c, &auth.AuthInfo{
				CredentialType: "api_key",
				UserID:         "user-E14",
				KeyID:          "dbkyE14x",
			})
			return next(c)
		}
	}
	group := e.Group("/api/v1", apikit.CacheMiddleware(apikit.CacheNoStore), authMW)
	keys.RegisterKeyHandlers(group, database.SqlDB)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/user/keys/dbkyE14x", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("DELETE with DB error status = %d; want %d",
			rec.Code, http.StatusInternalServerError)
	}

	var errResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	errObj, ok := errResp["error"].(map[string]any)
	if !ok {
		t.Fatalf("response missing 'error' object; got: %v", errResp)
	}
	if msg, ok := errObj["message"].(string); !ok || msg != "internal server error" {
		t.Errorf("error.message = %v; want %q", errObj["message"], "internal server error")
	}
}

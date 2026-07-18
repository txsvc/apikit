package auth

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	_ "modernc.org/sqlite"

	"github.com/txsvc/apikit/internal/db"
)

// ========================================================================
// Test Helpers — Database Setup
// ========================================================================

// testSchema contains the DDL for the tables needed by the auth middleware.
// Mirrors the 02_database_layer schema.
const testSchema = `
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
	CREATE TABLE IF NOT EXISTS api_keys (
		key_id       TEXT    NOT NULL PRIMARY KEY,
		user_id      TEXT    NOT NULL REFERENCES users(id),
		secret_hash  TEXT    NOT NULL,
		expires_days INTEGER NOT NULL,
		expires_at   TEXT,
		revoked_at   TEXT,
		created_at   TEXT    NOT NULL
	);
	CREATE TABLE IF NOT EXISTS pats (
		token_id     TEXT    NOT NULL PRIMARY KEY,
		user_id      TEXT    NOT NULL REFERENCES users(id),
		name         TEXT    NOT NULL,
		secret_hash  TEXT    NOT NULL,
		permissions  TEXT    NOT NULL,
		expires_days INTEGER NOT NULL,
		expires_at   TEXT,
		revoked_at   TEXT,
		created_at   TEXT    NOT NULL
	);
	CREATE TABLE IF NOT EXISTS admin_config (
		key   TEXT NOT NULL PRIMARY KEY,
		value TEXT NOT NULL
	);
`

// openTestDB opens an in-memory SQLite database with the auth-relevant
// schema and foreign keys enabled. Returns a *db.DB wrapper suitable for
// passing to NewAuthMiddleware. The database is closed on test cleanup.
func openTestDB(t *testing.T) *db.DB {
	t.Helper()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("openTestDB: open: %v", err)
	}

	// Enable foreign keys (SQLite default is off).
	if _, err := sqlDB.Exec("PRAGMA foreign_keys = ON"); err != nil {
		sqlDB.Close()
		t.Fatalf("openTestDB: enable FK: %v", err)
	}

	if _, err := sqlDB.Exec(testSchema); err != nil {
		sqlDB.Close()
		t.Fatalf("openTestDB: schema: %v", err)
	}

	t.Cleanup(func() { sqlDB.Close() })
	return &db.DB{SqlDB: sqlDB}
}

// hexSHA256 returns the hex-encoded SHA-256 hash of s.
func hexSHA256(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// insertAdminConfig inserts a key-value pair into the admin_config table.
func insertAdminConfig(t *testing.T, database *db.DB, key, value string) {
	t.Helper()
	_, err := database.SqlDB.Exec(
		`INSERT OR REPLACE INTO admin_config (key, value) VALUES (?, ?)`,
		key, value,
	)
	if err != nil {
		t.Fatalf("insertAdminConfig(%q): %v", key, err)
	}
}

// insertUser inserts a user row into the users table.
func insertUser(t *testing.T, database *db.DB, userID, role, status string) {
	t.Helper()
	_, err := database.SqlDB.Exec(
		`INSERT INTO users (id, username, email, role, status, provider, provider_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, "user_"+userID, userID+"@example.com", role, status,
		"github", "gh_"+userID,
		"2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z",
	)
	if err != nil {
		t.Fatalf("insertUser(%q): %v", userID, err)
	}
}

// insertAPIKey inserts an API key row into the api_keys table.
// expiresAt and revokedAt may be empty strings for NULL values.
func insertAPIKey(t *testing.T, database *db.DB, keyID, userID, secretHash, expiresAt, revokedAt string) {
	t.Helper()

	var expiresAtVal, revokedAtVal interface{}
	if expiresAt != "" {
		expiresAtVal = expiresAt
	}
	if revokedAt != "" {
		revokedAtVal = revokedAt
	}

	_, err := database.SqlDB.Exec(
		`INSERT INTO api_keys (key_id, user_id, secret_hash, expires_days, expires_at, revoked_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		keyID, userID, secretHash, 0, expiresAtVal, revokedAtVal,
		"2026-01-01T00:00:00Z",
	)
	if err != nil {
		t.Fatalf("insertAPIKey(%q): %v", keyID, err)
	}
}

// runMiddlewareWithDB creates an Echo instance with the auth middleware using
// the provided database and sends a GET request with the specified headers.
// Returns the response recorder.
func runMiddlewareWithDB(t *testing.T, database *db.DB, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	e := echo.New()
	registry := NewPermissionRegistry()
	mw := NewAuthMiddleware(database, registry)

	handler := func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	e.GET("/test", handler, mw)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	return rec
}

// capturedContext holds the Echo context captured from a handler invocation.
type capturedContext struct {
	Called bool
	Ctx    echo.Context
}

// runMiddlewareWithCapture creates an Echo instance with the auth middleware
// using the provided database, sends a GET request, and captures the Echo
// context from the handler if it is invoked. Returns the response recorder
// and the captured context.
func runMiddlewareWithCapture(t *testing.T, database *db.DB, headers map[string]string) (*httptest.ResponseRecorder, *capturedContext) {
	t.Helper()

	e := echo.New()
	registry := NewPermissionRegistry()
	mw := NewAuthMiddleware(database, registry)

	captured := &capturedContext{}
	handler := func(c echo.Context) error {
		captured.Called = true
		captured.Ctx = c
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	e.GET("/test", handler, mw)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	return rec, captured
}

// assertErrorResponseDB checks that the response has the expected HTTP status
// code and error message in the standard JSON error envelope.
func assertErrorResponseDB(t *testing.T, rec *httptest.ResponseRecorder, expectedCode int, expectedMessage string) {
	t.Helper()

	if rec.Code != expectedCode {
		t.Errorf("expected HTTP status %d, got %d", expectedCode, rec.Code)
	}

	var resp errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse error response body: %v\nbody: %s", err, rec.Body.String())
	}

	if resp.Error.Code != expectedCode {
		t.Errorf("expected error code %d in body, got %d", expectedCode, resp.Error.Code)
	}

	if resp.Error.Message != expectedMessage {
		t.Errorf("expected error message %q, got %q", expectedMessage, resp.Error.Message)
	}
}

// ========================================================================
// 2.1 Admin Token Validation Tests (REQ-4)
// ========================================================================

// TestMiddleware_AdminTokenShortHex verifies that an admin token with a hex
// suffix shorter than 64 characters is rejected with HTTP 401 "invalid
// credentials" without any database query.
//
// Test Spec: TS-05-13, TS-05-E7
// Requirement: 05-REQ-4.1, 05-REQ-4.E2
func TestMiddleware_AdminTokenShortHex(t *testing.T) {
	database := openTestDB(t)

	rec := runMiddlewareWithDB(t, database, map[string]string{
		"Authorization": "Bearer ak_admin_tooshort",
	})
	assertErrorResponseDB(t, rec, http.StatusUnauthorized, "invalid credentials")
}

// TestMiddleware_AdminTokenLongHex verifies that an admin token with a hex
// suffix longer than 64 characters is rejected with HTTP 401 "invalid
// credentials" without any database query.
//
// Test Spec: TS-05-E7 (extended)
// Requirement: 05-REQ-4.E2
func TestMiddleware_AdminTokenLongHex(t *testing.T) {
	database := openTestDB(t)

	// 66 hex chars — 2 more than allowed.
	longHex := strings.Repeat("ab", 33)
	rec := runMiddlewareWithDB(t, database, map[string]string{
		"Authorization": "Bearer ak_admin_" + longHex,
	})
	assertErrorResponseDB(t, rec, http.StatusUnauthorized, "invalid credentials")
}

// TestMiddleware_AdminTokenNonHexChars verifies that an admin token with
// non-hex characters in the 64-char suffix is rejected.
//
// Requirement: 05-REQ-4.1
func TestMiddleware_AdminTokenNonHexChars(t *testing.T) {
	database := openTestDB(t)

	// 64 chars but with 'zz' which are not valid hex.
	badHex := strings.Repeat("ab", 30) + "zzzz"
	rec := runMiddlewareWithDB(t, database, map[string]string{
		"Authorization": "Bearer ak_admin_" + badHex,
	})
	assertErrorResponseDB(t, rec, http.StatusUnauthorized, "invalid credentials")
}

// TestMiddleware_ValidAdminToken verifies that a valid admin token (matching
// hash in admin_config) results in HTTP 200 and proper AuthInfo injection.
// The SHA-256 hash is computed over the full token string including the prefix.
//
// Test Spec: TS-05-14, TS-05-15
// Requirement: 05-REQ-4.2, 05-REQ-4.3
func TestMiddleware_ValidAdminToken(t *testing.T) {
	database := openTestDB(t)

	// Build a valid admin token: ak_admin_<64 hex chars>
	fullToken := "ak_admin_" + strings.Repeat("ab", 32)

	// Store the SHA-256 hash of the FULL token string (including prefix).
	storedHash := hexSHA256(fullToken)
	insertAdminConfig(t, database, "admin_token_hash", storedHash)

	rec, captured := runMiddlewareWithCapture(t, database, map[string]string{
		"Authorization": "Bearer " + fullToken,
	})

	// The handler must be called (HTTP 200).
	if rec.Code != http.StatusOK {
		t.Errorf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	if !captured.Called {
		t.Fatal("expected next handler to be called, but it was not")
	}

	// Verify AuthInfo was injected with correct fields.
	auth := GetAuthInfo(captured.Ctx)
	if auth == nil {
		t.Fatal("GetAuthInfo returned nil; expected non-nil AuthInfo")
	}
	if auth.CredentialType != "admin_token" {
		t.Errorf("CredentialType = %q, want %q", auth.CredentialType, "admin_token")
	}
	if auth.UserID != "" {
		t.Errorf("UserID = %q, want empty string", auth.UserID)
	}
	if auth.Role != "admin" {
		t.Errorf("Role = %q, want %q", auth.Role, "admin")
	}
	if auth.KeyID != "" {
		t.Errorf("KeyID = %q, want empty string", auth.KeyID)
	}
	if auth.TokenID != "" {
		t.Errorf("TokenID = %q, want empty string", auth.TokenID)
	}
	if auth.Permissions != nil {
		t.Errorf("Permissions = %v, want nil", auth.Permissions)
	}
}

// TestMiddleware_AdminTokenMissingHash verifies that when the admin_token_hash
// row is missing from admin_config (db.ErrNotFound), the middleware returns
// HTTP 401 with "invalid credentials".
//
// Test Spec: TS-05-16
// Requirement: 05-REQ-4.4
func TestMiddleware_AdminTokenMissingHash(t *testing.T) {
	database := openTestDB(t)
	// Do not insert any admin_config row — simulates missing hash.

	fullToken := "ak_admin_" + strings.Repeat("ab", 32)
	rec := runMiddlewareWithDB(t, database, map[string]string{
		"Authorization": "Bearer " + fullToken,
	})
	assertErrorResponseDB(t, rec, http.StatusUnauthorized, "invalid credentials")
}

// TestMiddleware_InvalidAdminToken verifies that when the admin token hash
// does not match the stored admin_token_hash, the middleware returns HTTP 401
// with "invalid credentials".
//
// Test Spec: TS-05-17
// Requirement: 05-REQ-4.5
func TestMiddleware_InvalidAdminToken(t *testing.T) {
	database := openTestDB(t)

	// Store a hash for a different token value.
	insertAdminConfig(t, database, "admin_token_hash", hexSHA256("wrong_token_value"))

	fullToken := "ak_admin_" + strings.Repeat("ab", 32)
	rec := runMiddlewareWithDB(t, database, map[string]string{
		"Authorization": "Bearer " + fullToken,
	})
	assertErrorResponseDB(t, rec, http.StatusUnauthorized, "invalid credentials")
}

// TestMiddleware_AdminConfigDBError verifies that when the admin_config
// database query fails with a non-ErrNotFound error, the middleware returns
// HTTP 500 with "internal server error".
//
// Test Spec: TS-05-E6
// Requirement: 05-REQ-4.E1
func TestMiddleware_AdminConfigDBError(t *testing.T) {
	database := openTestDB(t)

	// Drop the admin_config table to force a query error (not ErrNotFound).
	_, err := database.SqlDB.Exec("DROP TABLE admin_config")
	if err != nil {
		t.Fatalf("failed to drop admin_config table: %v", err)
	}

	fullToken := "ak_admin_" + strings.Repeat("ab", 32)
	rec := runMiddlewareWithDB(t, database, map[string]string{
		"Authorization": "Bearer " + fullToken,
	})
	assertErrorResponseDB(t, rec, http.StatusInternalServerError, "internal server error")
}

// ========================================================================
// 2.2 API Key Validation Tests — Happy Path, Revocation, Expiry (REQ-5)
// ========================================================================

// TestMiddleware_ValidAPIKey verifies that a valid API key with a non-blocked
// user results in HTTP 200 and correct AuthInfo injection with CredentialType,
// UserID, Role, and KeyID populated.
//
// Test Spec: TS-05-23
// Requirement: 05-REQ-5.6
func TestMiddleware_ValidAPIKey(t *testing.T) {
	database := openTestDB(t)

	insertUser(t, database, "uid-abc", "user", "active")
	insertAPIKey(t, database, "goodkey", "uid-abc", hexSHA256("goodsecret"), "", "")

	rec, captured := runMiddlewareWithCapture(t, database, map[string]string{
		"Authorization": "Bearer ak_goodkey_goodsecret",
	})

	if rec.Code != http.StatusOK {
		t.Errorf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	if !captured.Called {
		t.Fatal("expected next handler to be called, but it was not")
	}

	auth := GetAuthInfo(captured.Ctx)
	if auth == nil {
		t.Fatal("GetAuthInfo returned nil; expected non-nil AuthInfo")
	}
	if auth.CredentialType != "api_key" {
		t.Errorf("CredentialType = %q, want %q", auth.CredentialType, "api_key")
	}
	if auth.UserID != "uid-abc" {
		t.Errorf("UserID = %q, want %q", auth.UserID, "uid-abc")
	}
	if auth.Role != "user" {
		t.Errorf("Role = %q, want %q", auth.Role, "user")
	}
	if auth.KeyID != "goodkey" {
		t.Errorf("KeyID = %q, want %q", auth.KeyID, "goodkey")
	}
	if auth.TokenID != "" {
		t.Errorf("TokenID = %q, want empty string", auth.TokenID)
	}
	if auth.Permissions != nil {
		t.Errorf("Permissions = %v, want nil", auth.Permissions)
	}
}

// TestMiddleware_APIKeyNotFound verifies that when the key_id is not found in
// the api_keys table, the middleware returns HTTP 401 "invalid credentials".
//
// Test Spec: TS-05-18
// Requirement: 05-REQ-5.1
func TestMiddleware_APIKeyNotFound(t *testing.T) {
	database := openTestDB(t)
	// No api_key rows — key_id "unknownkey" will not be found.

	rec := runMiddlewareWithDB(t, database, map[string]string{
		"Authorization": "Bearer ak_unknownkey_somesecret",
	})
	assertErrorResponseDB(t, rec, http.StatusUnauthorized, "invalid credentials")
}

// TestMiddleware_RevokedAPIKey verifies that when the matched API key has a
// non-NULL revoked_at, the middleware returns HTTP 401 "credential revoked".
//
// Test Spec: TS-05-19
// Requirement: 05-REQ-5.2
func TestMiddleware_RevokedAPIKey(t *testing.T) {
	database := openTestDB(t)

	insertUser(t, database, "uid1", "user", "active")
	insertAPIKey(t, database, "revokedkey", "uid1", hexSHA256("mysecret"), "", "2025-01-01T00:00:00Z")

	rec := runMiddlewareWithDB(t, database, map[string]string{
		"Authorization": "Bearer ak_revokedkey_mysecret",
	})
	assertErrorResponseDB(t, rec, http.StatusUnauthorized, "credential revoked")
}

// TestMiddleware_ExpiredAPIKey verifies that when the API key's expires_at is
// non-NULL and in the past, the middleware returns HTTP 401 "credential expired".
//
// Test Spec: TS-05-20
// Requirement: 05-REQ-5.3
func TestMiddleware_ExpiredAPIKey(t *testing.T) {
	database := openTestDB(t)

	insertUser(t, database, "uid1", "user", "active")
	insertAPIKey(t, database, "expiredkey", "uid1", hexSHA256("mysecret"), "2000-01-01T00:00:00Z", "")

	rec := runMiddlewareWithDB(t, database, map[string]string{
		"Authorization": "Bearer ak_expiredkey_mysecret",
	})
	assertErrorResponseDB(t, rec, http.StatusUnauthorized, "credential expired")
}

// TestMiddleware_InvalidAPIKeySecret verifies that when the API key's secret
// hash does not match the computed hash of the provided secret, the middleware
// returns HTTP 401 "invalid credentials".
//
// Test Spec: TS-05-21
// Requirement: 05-REQ-5.4
func TestMiddleware_InvalidAPIKeySecret(t *testing.T) {
	database := openTestDB(t)

	insertUser(t, database, "uid1", "user", "active")
	// Store hash of "correctsecret" but provide "wrongsecret" in the token.
	insertAPIKey(t, database, "mykey", "uid1", hexSHA256("correctsecret"), "", "")

	rec := runMiddlewareWithDB(t, database, map[string]string{
		"Authorization": "Bearer ak_mykey_wrongsecret",
	})
	assertErrorResponseDB(t, rec, http.StatusUnauthorized, "invalid credentials")
}

// TestMiddleware_ValidAPIKey_AdminRole verifies that a valid API key belonging
// to an admin-role user correctly populates AuthInfo.Role as "admin".
//
// Requirement: 05-REQ-5.6
func TestMiddleware_ValidAPIKey_AdminRole(t *testing.T) {
	database := openTestDB(t)

	insertUser(t, database, "admin-uid", "admin", "active")
	insertAPIKey(t, database, "adminkey", "admin-uid", hexSHA256("adminsecret"), "", "")

	rec, captured := runMiddlewareWithCapture(t, database, map[string]string{
		"Authorization": "Bearer ak_adminkey_adminsecret",
	})

	if rec.Code != http.StatusOK {
		t.Errorf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	if !captured.Called {
		t.Fatal("expected next handler to be called, but it was not")
	}

	auth := GetAuthInfo(captured.Ctx)
	if auth == nil {
		t.Fatal("GetAuthInfo returned nil; expected non-nil AuthInfo")
	}
	if auth.Role != "admin" {
		t.Errorf("Role = %q, want %q", auth.Role, "admin")
	}
	if auth.CredentialType != "api_key" {
		t.Errorf("CredentialType = %q, want %q", auth.CredentialType, "api_key")
	}
}

// TestMiddleware_APIKey_NotExpiredYet verifies that an API key with a future
// expires_at is accepted and authentication succeeds.
//
// Requirement: 05-REQ-5.3 (converse: non-expired key should pass)
func TestMiddleware_APIKey_NotExpiredYet(t *testing.T) {
	database := openTestDB(t)

	insertUser(t, database, "uid1", "user", "active")
	// expires_at far in the future.
	insertAPIKey(t, database, "futurekey", "uid1", hexSHA256("futuresecret"), "2099-12-31T23:59:59Z", "")

	rec, captured := runMiddlewareWithCapture(t, database, map[string]string{
		"Authorization": "Bearer ak_futurekey_futuresecret",
	})

	if rec.Code != http.StatusOK {
		t.Errorf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	if !captured.Called {
		t.Fatal("expected next handler to be called for non-expired key")
	}

	auth := GetAuthInfo(captured.Ctx)
	if auth == nil {
		t.Fatal("GetAuthInfo returned nil; expected non-nil AuthInfo")
	}
	if auth.CredentialType != "api_key" {
		t.Errorf("CredentialType = %q, want %q", auth.CredentialType, "api_key")
	}
}

// ========================================================================
// 2.3 API Key Validation Tests — User Checks and DB Errors (REQ-5 edge cases)
// ========================================================================

// TestMiddleware_BlockedUser_APIKey verifies that when the API key is valid
// but the owning user has status "blocked", the middleware returns HTTP 403
// with "user is blocked".
//
// Test Spec: TS-05-22
// Requirement: 05-REQ-5.5
func TestMiddleware_BlockedUser_APIKey(t *testing.T) {
	database := openTestDB(t)

	insertUser(t, database, "blockeduid", "user", "blocked")
	insertAPIKey(t, database, "mykey", "blockeduid", hexSHA256("mysecret"), "", "")

	rec := runMiddlewareWithDB(t, database, map[string]string{
		"Authorization": "Bearer ak_mykey_mysecret",
	})
	assertErrorResponseDB(t, rec, http.StatusForbidden, "user is blocked")
}

// TestMiddleware_APIKeyDBError verifies that when the api_keys database query
// fails with a non-ErrNotFound error, the middleware returns HTTP 500 with
// "internal server error".
//
// Test Spec: TS-05-E8
// Requirement: 05-REQ-5.E1
func TestMiddleware_APIKeyDBError(t *testing.T) {
	database := openTestDB(t)

	// Drop the api_keys table to force a query error (not ErrNotFound).
	_, err := database.SqlDB.Exec("DROP TABLE api_keys")
	if err != nil {
		t.Fatalf("failed to drop api_keys table: %v", err)
	}

	rec := runMiddlewareWithDB(t, database, map[string]string{
		"Authorization": "Bearer ak_mykey_mysecret",
	})
	assertErrorResponseDB(t, rec, http.StatusInternalServerError, "internal server error")
}

// TestMiddleware_UsersDBError_AfterAPIKey verifies that when the api_keys
// lookup succeeds but the users table query fails, the middleware returns
// HTTP 500 with "internal server error".
//
// Test Spec: TS-05-E9
// Requirement: 05-REQ-5.E2
func TestMiddleware_UsersDBError_AfterAPIKey(t *testing.T) {
	database := openTestDB(t)

	// Insert API key directly without FK enforcement so we can drop users table.
	// First disable FK checks temporarily.
	_, err := database.SqlDB.Exec("PRAGMA foreign_keys = OFF")
	if err != nil {
		t.Fatalf("failed to disable FK: %v", err)
	}

	// Insert an api_key row that references a user_id. The users table lookup
	// will fail because we'll drop the table.
	_, err = database.SqlDB.Exec(
		`INSERT INTO api_keys (key_id, user_id, secret_hash, expires_days, expires_at, revoked_at, created_at)
		 VALUES (?, ?, ?, ?, NULL, NULL, ?)`,
		"mykey", "uid1", hexSHA256("mysecret"), 0, "2026-01-01T00:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert api_key failed: %v", err)
	}

	// Drop the users table to force a query error when looking up the user.
	_, err = database.SqlDB.Exec("DROP TABLE users")
	if err != nil {
		t.Fatalf("failed to drop users table: %v", err)
	}

	// Re-enable FK checks (won't matter since users table is gone).
	_, _ = database.SqlDB.Exec("PRAGMA foreign_keys = ON")

	rec := runMiddlewareWithDB(t, database, map[string]string{
		"Authorization": "Bearer ak_mykey_mysecret",
	})
	assertErrorResponseDB(t, rec, http.StatusInternalServerError, "internal server error")
}

// TestMiddleware_APIKeyMalformedToken verifies that when an API key token
// cannot be split into exactly two components (key_id and secret), the
// middleware returns HTTP 401 with "unrecognized token format".
//
// Test Spec: TS-05-E10
// Requirement: 05-REQ-5.E3
func TestMiddleware_APIKeyMalformedToken(t *testing.T) {
	database := openTestDB(t)

	// Token has the correct prefix "ak_" but only one component after it
	// (no underscore separator between key_id and secret).
	rec := runMiddlewareWithDB(t, database, map[string]string{
		"Authorization": "Bearer ak_onlyonepart",
	})
	assertErrorResponseDB(t, rec, http.StatusUnauthorized, "unrecognized token format")
}

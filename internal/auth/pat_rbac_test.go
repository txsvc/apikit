package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	_ "modernc.org/sqlite"

	"github.com/txsvc/apikit/internal/db"
)

// ========================================================================
// Test Helpers — PAT and RBAC
// ========================================================================

// insertPATRow inserts a PAT row into the pats table.
// permissionsJSON is the raw JSON array string for the permissions column
// (e.g. `["keys:read","tokens:manage"]` or `[]`).
// expiresAt and revokedAt may be empty strings for NULL values.
func insertPATRow(t *testing.T, database *db.DB, tokenID, userID, secretHash, permissionsJSON, expiresAt, revokedAt string) {
	t.Helper()

	var expiresAtVal, revokedAtVal interface{}
	if expiresAt != "" {
		expiresAtVal = expiresAt
	}
	if revokedAt != "" {
		revokedAtVal = revokedAt
	}

	_, err := database.SqlDB.Exec(
		`INSERT INTO pats (token_id, user_id, name, secret_hash, permissions, expires_days, expires_at, revoked_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tokenID, userID, "test-pat-"+tokenID, secretHash, permissionsJSON,
		0, expiresAtVal, revokedAtVal, "2026-01-01T00:00:00Z",
	)
	if err != nil {
		t.Fatalf("insertPATRow(%q): %v", tokenID, err)
	}
}

// newEchoContext creates a bare echo.Context backed by an httptest recorder,
// suitable for testing RBAC helpers in isolation (without the middleware).
func newEchoContext() (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	return c, rec
}

// setAuthInfo injects an AuthInfo struct into the Echo request's
// context.Context using the unexported authInfoKey. This mirrors what
// the auth middleware does after successful authentication via
// setAuthInfoContext. Using context.WithValue ensures the AuthInfo is
// stored under the typed key, not accessible via plain string keys.
func setAuthInfo(c echo.Context, info *AuthInfo) {
	ctx := context.WithValue(c.Request().Context(), authInfoKey, info)
	c.SetRequest(c.Request().WithContext(ctx))
}

// assertHTTPError checks that err is a non-nil *echo.HTTPError with the
// expected status code and message.
func assertHTTPError(t *testing.T, err error, expectedCode int, expectedMessage string) {
	t.Helper()

	if err == nil {
		t.Fatalf("expected non-nil error with HTTP %d %q, got nil", expectedCode, expectedMessage)
	}

	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected *echo.HTTPError, got %T: %v", err, err)
	}

	if he.Code != expectedCode {
		t.Errorf("expected HTTP status %d, got %d", expectedCode, he.Code)
	}

	msg, ok := he.Message.(string)
	if !ok {
		t.Fatalf("expected string message, got %T: %v", he.Message, he.Message)
	}

	if msg != expectedMessage {
		t.Errorf("expected error message %q, got %q", expectedMessage, msg)
	}
}

// ========================================================================
// 3.1 PAT Validation Tests — Happy Path and Revocation/Expiry (REQ-6)
// ========================================================================

// TestMiddleware_PATNotFound verifies that when the PAT token_id is not found
// in the pats table (returns db.ErrNotFound), the middleware returns HTTP 401
// with "invalid credentials".
//
// Test Spec: TS-05-24
// Requirement: 05-REQ-6.1
func TestMiddleware_PATNotFound(t *testing.T) {
	database := openTestDB(t)
	// No PAT rows — token_id "unknowntok" will not be found.

	rec := runMiddlewareWithDB(t, database, map[string]string{
		"Authorization": "Bearer ak_pat_unknowntok_somesecret",
	})
	assertErrorResponseDB(t, rec, http.StatusUnauthorized, "invalid credentials")
}

// TestMiddleware_RevokedPAT verifies that when the matched PAT has a non-NULL
// revoked_at, the middleware returns HTTP 401 with "credential revoked".
//
// Test Spec: TS-05-25
// Requirement: 05-REQ-6.2
func TestMiddleware_RevokedPAT(t *testing.T) {
	database := openTestDB(t)

	insertUser(t, database, "uid1", "user", "active")
	insertPATRow(t, database, "revokedtok", "uid1", hexSHA256("mysecret"),
		`["keys:read"]`, "", "2025-01-01T00:00:00Z")

	rec := runMiddlewareWithDB(t, database, map[string]string{
		"Authorization": "Bearer ak_pat_revokedtok_mysecret",
	})
	assertErrorResponseDB(t, rec, http.StatusUnauthorized, "credential revoked")
}

// TestMiddleware_ExpiredPAT verifies that when the PAT's expires_at is non-NULL
// and in the past, the middleware returns HTTP 401 with "credential expired".
//
// Test Spec: TS-05-26
// Requirement: 05-REQ-6.3
func TestMiddleware_ExpiredPAT(t *testing.T) {
	database := openTestDB(t)

	insertUser(t, database, "uid1", "user", "active")
	insertPATRow(t, database, "expiredtok", "uid1", hexSHA256("mysecret"),
		`["keys:read"]`, "2000-01-01T00:00:00Z", "")

	rec := runMiddlewareWithDB(t, database, map[string]string{
		"Authorization": "Bearer ak_pat_expiredtok_mysecret",
	})
	assertErrorResponseDB(t, rec, http.StatusUnauthorized, "credential expired")
}

// TestMiddleware_InvalidPATSecret verifies that when the PAT's secret hash does
// not match the computed hash of the provided secret, the middleware returns
// HTTP 401 with "invalid credentials".
//
// Test Spec: TS-05-27
// Requirement: 05-REQ-6.4
func TestMiddleware_InvalidPATSecret(t *testing.T) {
	database := openTestDB(t)

	insertUser(t, database, "uid1", "user", "active")
	// Store hash of "correctsecret" but provide "wrongsecret" in the token.
	insertPATRow(t, database, "mytok", "uid1", hexSHA256("correctsecret"),
		`["keys:read"]`, "", "")

	rec := runMiddlewareWithDB(t, database, map[string]string{
		"Authorization": "Bearer ak_pat_mytok_wrongsecret",
	})
	assertErrorResponseDB(t, rec, http.StatusUnauthorized, "invalid credentials")
}

// TestMiddleware_ValidPAT verifies that a valid PAT with a non-blocked user
// results in HTTP 200 and correct AuthInfo injection with CredentialType "pat",
// UserID, Role, TokenID, and Permissions populated.
//
// Test Spec: TS-05-29
// Requirement: 05-REQ-6.6
func TestMiddleware_ValidPAT(t *testing.T) {
	database := openTestDB(t)

	insertUser(t, database, "uid-xyz", "user", "active")
	insertPATRow(t, database, "goodtok", "uid-xyz", hexSHA256("goodsecret"),
		`["keys:read","tokens:read"]`, "", "")

	rec, captured := runMiddlewareWithCapture(t, database, map[string]string{
		"Authorization": "Bearer ak_pat_goodtok_goodsecret",
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
	if auth.CredentialType != "pat" {
		t.Errorf("CredentialType = %q, want %q", auth.CredentialType, "pat")
	}
	if auth.UserID != "uid-xyz" {
		t.Errorf("UserID = %q, want %q", auth.UserID, "uid-xyz")
	}
	if auth.Role != "user" {
		t.Errorf("Role = %q, want %q", auth.Role, "user")
	}
	if auth.TokenID != "goodtok" {
		t.Errorf("TokenID = %q, want %q", auth.TokenID, "goodtok")
	}
	if len(auth.Permissions) != 2 {
		t.Fatalf("Permissions length = %d, want 2", len(auth.Permissions))
	}
	if auth.Permissions[0] != "keys:read" {
		t.Errorf("Permissions[0] = %q, want %q", auth.Permissions[0], "keys:read")
	}
	if auth.Permissions[1] != "tokens:read" {
		t.Errorf("Permissions[1] = %q, want %q", auth.Permissions[1], "tokens:read")
	}
	if auth.KeyID != "" {
		t.Errorf("KeyID = %q, want empty string", auth.KeyID)
	}
}

// ========================================================================
// 3.2 PAT Validation Tests — User Checks and DB Errors (REQ-6 edge cases)
// ========================================================================

// TestMiddleware_BlockedUser_PAT verifies that when the PAT is valid but the
// owning user has status "blocked", the middleware returns HTTP 403 with
// "user is blocked".
//
// Test Spec: TS-05-28
// Requirement: 05-REQ-6.5
func TestMiddleware_BlockedUser_PAT(t *testing.T) {
	database := openTestDB(t)

	insertUser(t, database, "blockeduid", "user", "blocked")
	insertPATRow(t, database, "mytok", "blockeduid", hexSHA256("mysecret"),
		`[]`, "", "")

	rec := runMiddlewareWithDB(t, database, map[string]string{
		"Authorization": "Bearer ak_pat_mytok_mysecret",
	})
	assertErrorResponseDB(t, rec, http.StatusForbidden, "user is blocked")
}

// TestMiddleware_PATDBError verifies that when the pats database query fails
// with a non-ErrNotFound error, the middleware returns HTTP 500 with
// "internal server error".
//
// Test Spec: TS-05-E11
// Requirement: 05-REQ-6.E1
func TestMiddleware_PATDBError(t *testing.T) {
	database := openTestDB(t)

	// Drop the pats table to force a query error (not ErrNotFound).
	_, err := database.SqlDB.Exec("DROP TABLE pats")
	if err != nil {
		t.Fatalf("failed to drop pats table: %v", err)
	}

	rec := runMiddlewareWithDB(t, database, map[string]string{
		"Authorization": "Bearer ak_pat_mytok_mysecret",
	})
	assertErrorResponseDB(t, rec, http.StatusInternalServerError, "internal server error")
}

// TestMiddleware_UsersDBError_AfterPAT verifies that when the PAT lookup
// succeeds but the users table query fails, the middleware returns HTTP 500
// with "internal server error".
//
// Test Spec: TS-05-E12
// Requirement: 05-REQ-6.E2
func TestMiddleware_UsersDBError_AfterPAT(t *testing.T) {
	database := openTestDB(t)

	// Disable FK checks so we can insert a PAT and then drop users.
	_, err := database.SqlDB.Exec("PRAGMA foreign_keys = OFF")
	if err != nil {
		t.Fatalf("failed to disable FK: %v", err)
	}

	// Insert a PAT row that references a user_id.
	_, err = database.SqlDB.Exec(
		`INSERT INTO pats (token_id, user_id, name, secret_hash, permissions, expires_days, expires_at, revoked_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, NULL, NULL, ?)`,
		"goodtok", "uid1", "test-pat", hexSHA256("goodsecret"), `[]`, 0, "2026-01-01T00:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert PAT failed: %v", err)
	}

	// Drop the users table to force a query error when looking up the user.
	_, err = database.SqlDB.Exec("DROP TABLE users")
	if err != nil {
		t.Fatalf("failed to drop users table: %v", err)
	}

	// Re-enable FK checks.
	_, _ = database.SqlDB.Exec("PRAGMA foreign_keys = ON")

	rec := runMiddlewareWithDB(t, database, map[string]string{
		"Authorization": "Bearer ak_pat_goodtok_goodsecret",
	})
	assertErrorResponseDB(t, rec, http.StatusInternalServerError, "internal server error")
}

// TestMiddleware_PATMalformedToken verifies that a PAT token that cannot be
// split into exactly two components (token_id and secret) after removing the
// pat_ infix is rejected with HTTP 401 "unrecognized token format" without
// performing a database lookup.
//
// Test Spec: TS-05-E13
// Requirement: 05-REQ-6.E3
func TestMiddleware_PATMalformedToken(t *testing.T) {
	database := openTestDB(t)

	// Token starts with the PAT prefix but has no underscore separator after
	// pat_ infix — only one component instead of token_id_secret.
	rec := runMiddlewareWithDB(t, database, map[string]string{
		"Authorization": "Bearer ak_pat_onlytokenid",
	})
	assertErrorResponseDB(t, rec, http.StatusUnauthorized, "unrecognized token format")
}

// ========================================================================
// 3.3 RequireAdmin and RequireOwnerOrAdmin Tests (REQ-7)
// ========================================================================

// TestRequireAdmin_AdminToken verifies that RequireAdmin returns nil when
// the authenticated credential is an admin token.
//
// Test Spec: TS-05-30
// Requirement: 05-REQ-7.1
func TestRequireAdmin_AdminToken(t *testing.T) {
	c, _ := newEchoContext()
	setAuthInfo(c, &AuthInfo{CredentialType: "admin_token", Role: "admin"})

	err := RequireAdmin(c)
	if err != nil {
		t.Errorf("RequireAdmin with admin_token: expected nil, got %v", err)
	}
}

// TestRequireAdmin_AdminAPIKey verifies that RequireAdmin returns nil when
// the authenticated credential is an API key with admin role.
//
// Requirement: 05-REQ-7.1
func TestRequireAdmin_AdminAPIKey(t *testing.T) {
	c, _ := newEchoContext()
	setAuthInfo(c, &AuthInfo{CredentialType: "api_key", Role: "admin", UserID: "uid-admin"})

	err := RequireAdmin(c)
	if err != nil {
		t.Errorf("RequireAdmin with admin api_key: expected nil, got %v", err)
	}
}

// TestRequireAdmin_RegularAPIKey verifies that RequireAdmin returns HTTP 403
// with "forbidden" when the authenticated credential is a regular-user API key.
//
// Test Spec: TS-05-31
// Requirement: 05-REQ-7.2
func TestRequireAdmin_RegularAPIKey(t *testing.T) {
	c, _ := newEchoContext()
	setAuthInfo(c, &AuthInfo{CredentialType: "api_key", Role: "user", UserID: "uid-abc"})

	err := RequireAdmin(c)
	assertHTTPError(t, err, http.StatusForbidden, "forbidden")
}

// TestRequireAdmin_PAT verifies that RequireAdmin returns HTTP 403 with
// "forbidden" when the authenticated credential is a PAT, regardless of role.
// This test covers both user-role and admin-role PATs to ensure PATs are
// never treated as admin-level credentials.
//
// Requirement: 05-REQ-7.2
// Note: Addresses critical reviewer finding — admin-role PATs must NOT
// bypass RequireAdmin.
func TestRequireAdmin_PAT(t *testing.T) {
	tests := []struct {
		name string
		role string
	}{
		{name: "user role PAT", role: "user"},
		{name: "admin role PAT", role: "admin"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newEchoContext()
			setAuthInfo(c, &AuthInfo{
				CredentialType: "pat",
				Role:           tt.role,
				UserID:         "uid-abc",
				TokenID:        "tok1",
				Permissions:    []string{"keys:read"},
			})

			err := RequireAdmin(c)
			assertHTTPError(t, err, http.StatusForbidden, "forbidden")
		})
	}
}

// TestRequireAdmin_NoAuthInfo verifies that RequireAdmin treats the request as
// unauthenticated and returns HTTP 403 with "forbidden" when no AuthInfo is
// present in the context (GetAuthInfo returns nil).
//
// Test Spec: TS-05-E14
// Requirement: 05-REQ-7.E1
func TestRequireAdmin_NoAuthInfo(t *testing.T) {
	c, _ := newEchoContext()
	// No AuthInfo injected.

	err := RequireAdmin(c)
	assertHTTPError(t, err, http.StatusForbidden, "forbidden")
}

// TestRequireOwnerOrAdmin_Owner verifies that RequireOwnerOrAdmin returns nil
// when the resourceOwnerID matches the authenticated user's UUID.
//
// Test Spec: TS-05-32
// Requirement: 05-REQ-7.3
func TestRequireOwnerOrAdmin_Owner(t *testing.T) {
	c, _ := newEchoContext()
	setAuthInfo(c, &AuthInfo{CredentialType: "api_key", Role: "user", UserID: "uid-abc"})

	err := RequireOwnerOrAdmin(c, "uid-abc")
	if err != nil {
		t.Errorf("RequireOwnerOrAdmin with matching owner: expected nil, got %v", err)
	}
}

// TestRequireOwnerOrAdmin_Admin verifies that RequireOwnerOrAdmin returns nil
// when the authenticated credential is admin-level, even if the
// resourceOwnerID does not match.
//
// Requirement: 05-REQ-7.3
func TestRequireOwnerOrAdmin_Admin(t *testing.T) {
	c, _ := newEchoContext()
	setAuthInfo(c, &AuthInfo{CredentialType: "admin_token", Role: "admin", UserID: ""})

	err := RequireOwnerOrAdmin(c, "uid-other")
	if err != nil {
		t.Errorf("RequireOwnerOrAdmin with admin credential: expected nil, got %v", err)
	}
}

// TestRequireOwnerOrAdmin_Forbidden verifies that RequireOwnerOrAdmin returns
// HTTP 403 with "forbidden" when the user is not the resource owner and not
// an admin.
//
// Test Spec: TS-05-33
// Requirement: 05-REQ-7.4
func TestRequireOwnerOrAdmin_Forbidden(t *testing.T) {
	c, _ := newEchoContext()
	setAuthInfo(c, &AuthInfo{CredentialType: "api_key", Role: "user", UserID: "uid-abc"})

	err := RequireOwnerOrAdmin(c, "uid-other")
	assertHTTPError(t, err, http.StatusForbidden, "forbidden")
}

// TestRequireOwnerOrAdmin_EmptyOwnerID verifies that an empty resourceOwnerID
// is treated as non-matching: non-admin gets HTTP 403 "forbidden", admin
// gets nil.
//
// Test Spec: TS-05-E15
// Requirement: 05-REQ-7.E2
func TestRequireOwnerOrAdmin_EmptyOwnerID(t *testing.T) {
	// Non-admin with empty resourceOwnerID should get 403.
	t.Run("non-admin", func(t *testing.T) {
		c, _ := newEchoContext()
		setAuthInfo(c, &AuthInfo{
			CredentialType: "api_key",
			Role:           "user",
			UserID:         "uid-abc",
		})

		err := RequireOwnerOrAdmin(c, "")
		assertHTTPError(t, err, http.StatusForbidden, "forbidden")
	})

	// Admin with empty resourceOwnerID should succeed.
	t.Run("admin", func(t *testing.T) {
		c, _ := newEchoContext()
		setAuthInfo(c, &AuthInfo{
			CredentialType: "admin_token",
			Role:           "admin",
			UserID:         "",
		})

		err := RequireOwnerOrAdmin(c, "")
		if err != nil {
			t.Errorf("RequireOwnerOrAdmin admin with empty ownerID: expected nil, got %v", err)
		}
	})
}

// ========================================================================
// 3.4 RequirePermission Tests (REQ-7)
// ========================================================================

// TestRequirePermission_AdminToken verifies that RequirePermission returns nil
// without checking PAT permissions when the credential is an admin token.
//
// Test Spec: TS-05-34
// Requirement: 05-REQ-7.5
func TestRequirePermission_AdminToken(t *testing.T) {
	c, _ := newEchoContext()
	setAuthInfo(c, &AuthInfo{CredentialType: "admin_token", Role: "admin"})

	err := RequirePermission(c, "keys", "manage")
	if err != nil {
		t.Errorf("RequirePermission with admin_token: expected nil, got %v", err)
	}
}

// TestRequirePermission_APIKey verifies that RequirePermission returns nil
// without checking PAT permissions when the credential is an API key
// (regardless of role).
//
// Test Spec: TS-05-34
// Requirement: 05-REQ-7.5
func TestRequirePermission_APIKey(t *testing.T) {
	c, _ := newEchoContext()
	setAuthInfo(c, &AuthInfo{CredentialType: "api_key", Role: "user", UserID: "uid-abc"})

	err := RequirePermission(c, "keys", "manage")
	if err != nil {
		t.Errorf("RequirePermission with api_key: expected nil, got %v", err)
	}
}

// TestRequirePermission_PAT_Granted verifies that RequirePermission returns nil
// when the PAT includes the requested resource_type:action permission.
//
// Test Spec: TS-05-35
// Requirement: 05-REQ-7.6
func TestRequirePermission_PAT_Granted(t *testing.T) {
	c, _ := newEchoContext()
	setAuthInfo(c, &AuthInfo{
		CredentialType: "pat",
		Role:           "user",
		UserID:         "uid-abc",
		TokenID:        "tok1",
		Permissions:    []string{"keys:read", "tokens:manage"},
	})

	err := RequirePermission(c, "keys", "read")
	if err != nil {
		t.Errorf("RequirePermission with granted permission: expected nil, got %v", err)
	}
}

// TestRequirePermission_PAT_Denied verifies that RequirePermission returns
// HTTP 403 with "insufficient permissions" when the PAT does not include the
// requested resource_type:action permission.
//
// Test Spec: TS-05-36
// Requirement: 05-REQ-7.7
func TestRequirePermission_PAT_Denied(t *testing.T) {
	c, _ := newEchoContext()
	setAuthInfo(c, &AuthInfo{
		CredentialType: "pat",
		Role:           "user",
		UserID:         "uid-abc",
		TokenID:        "tok1",
		Permissions:    []string{"keys:read"},
	})

	err := RequirePermission(c, "keys", "manage")
	assertHTTPError(t, err, http.StatusForbidden, "insufficient permissions")
}

// TestRequirePermission_PAT_EmptyPermissions verifies that RequirePermission
// returns HTTP 403 with "insufficient permissions" when the PAT has an empty
// Permissions list and any permission is requested.
//
// Requirement: 05-REQ-7.7
func TestRequirePermission_PAT_EmptyPermissions(t *testing.T) {
	c, _ := newEchoContext()
	setAuthInfo(c, &AuthInfo{
		CredentialType: "pat",
		Role:           "user",
		UserID:         "uid-abc",
		TokenID:        "tok1",
		Permissions:    []string{},
	})

	err := RequirePermission(c, "keys", "read")
	assertHTTPError(t, err, http.StatusForbidden, "insufficient permissions")
}

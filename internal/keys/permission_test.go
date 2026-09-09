package keys_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/txsvc/apikit/internal/auth"
	"github.com/txsvc/apikit/internal/db"
)

// assertInsufficientPermissions checks for the 403 "insufficient permissions"
// envelope.
func assertInsufficientPermissions(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d; want %d; body = %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	var errResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	errObj, ok := errResp["error"].(map[string]any)
	if !ok {
		t.Fatalf("response missing 'error' object; got: %v", errResp)
	}
	if msg, _ := errObj["message"].(string); msg != "insufficient permissions" {
		t.Errorf("error.message = %q; want %q", msg, "insufficient permissions")
	}
}

// TestListKeys_PATWithoutKeysRead verifies that GET /user/keys rejects a PAT
// that does not hold keys:read with HTTP 403 "insufficient permissions".
// PAT scoping must be enforced by the handler; the auth middleware only
// validates the credential.
func TestListKeys_PATWithoutKeysRead(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-perm-1")
	now := db.FormatTime(time.Now().UTC())
	insertTestKey(t, database.SqlDB, "permky01", "user-perm-1", "hashperm1", 0, "", "", now)

	e := setupHandlersWithAuth(t, database, &auth.AuthInfo{
		CredentialType: "pat",
		UserID:         "user-perm-1",
		TokenID:        "pat-perm-1",
		Permissions:    []string{"users:read", "keys:manage"},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/keys", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assertInsufficientPermissions(t, rec)
}

// TestDeleteKey_PATWithoutKeysManage verifies that DELETE /user/keys/:key_id
// rejects a PAT that does not hold keys:manage with HTTP 403 and leaves the
// key un-revoked. A read-only PAT must not be able to revoke the owner's
// API key.
func TestDeleteKey_PATWithoutKeysManage(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-perm-2")
	now := db.FormatTime(time.Now().UTC())
	insertTestKey(t, database.SqlDB, "permky02", "user-perm-2", "hashperm2", 0, "", "", now)

	e := setupHandlersWithAuth(t, database, &auth.AuthInfo{
		CredentialType: "pat",
		UserID:         "user-perm-2",
		TokenID:        "pat-perm-2",
		Permissions:    []string{"keys:read", "users:read"},
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/user/keys/permky02", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assertInsufficientPermissions(t, rec)

	var revokedAt *string
	if err := database.SqlDB.QueryRow(
		"SELECT revoked_at FROM api_keys WHERE key_id = ?", "permky02",
	).Scan(&revokedAt); err != nil {
		t.Fatalf("query revoked_at failed: %v", err)
	}
	if revokedAt != nil {
		t.Errorf("key was revoked by a PAT without keys:manage; revoked_at = %q", *revokedAt)
	}
}

// TestKeys_PATPermissionsNotRequiredForAPIKey verifies that API key
// credentials bypass the PAT permission checks on list and revoke.
func TestKeys_PATPermissionsNotRequiredForAPIKey(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-perm-3")
	now := db.FormatTime(time.Now().UTC())
	insertTestKey(t, database.SqlDB, "permky03", "user-perm-3", "hashperm3", 0, "", "", now)

	e := setupHandlersWithAuth(t, database, &auth.AuthInfo{
		CredentialType: "api_key",
		UserID:         "user-perm-3",
		KeyID:          "permky03",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/keys", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d; want 200; body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/user/keys/permky03", nil)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d; want 200; body = %s", rec.Code, rec.Body.String())
	}
}

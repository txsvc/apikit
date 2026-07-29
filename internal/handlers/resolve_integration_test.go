package handlers_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/txsvc/apikit/internal/db"
	"github.com/txsvc/apikit/internal/handlers"
)

// ========================================================================
// Integration Test Helpers
// ========================================================================

// setupIntegrationServer creates an Echo instance with both user and org
// handlers registered on a single group with admin auth middleware, backed
// by an in-memory SQLite database via OpenMemory. Returns the Echo instance
// and the raw *sql.DB handle.
//
// This setup mirrors the real server wiring but runs entirely in-process,
// satisfying 16-REQ-12.E1: integration tests use in-memory SQLite and do
// not require a running server process; runnable with 'go test ./internal/handlers/...'.
func setupIntegrationServer(t *testing.T) (*echo.Echo, *sql.DB) {
	t.Helper()

	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	e := echo.New()
	g := e.Group("")
	g.Use(adminAuthMiddleware("test-admin-uuid"))
	handlers.RegisterUserHandlers(g, database.SqlDB)
	handlers.RegisterOrgHandlers(g, database.SqlDB)

	return e, database.SqlDB
}

// Test data constants used across integration tests.
const (
	integrationUserID   = "550e8400-e29b-41d4-a716-446655440000"
	integrationUsername  = "alice"
	integrationEmail    = "alice@example.com"
	integrationOrgID    = "660e8400-e29b-41d4-a716-446655440001"
	integrationOrgSlug  = "acme-corp"
	integrationOrgName  = "Acme Corp"
)

// ========================================================================
// Task 2.1: User endpoint — selector variant integration tests
// Test Spec: TS-16-10, TS-16-35
// Requirements: 16-REQ-3.1, 16-REQ-3.E1, 16-REQ-3.E2, 16-REQ-3.E3,
//
//	16-REQ-12.1
//
// ========================================================================

// TestUserEndpoint_UUIDSelector verifies that user endpoints accept a UUID
// selector and return 200 OK with the user resource — backward compatibility.
//
// Test Spec: TS-16-35 (UUID variant)
// Requirement: 16-REQ-3.E1, 16-PROP-7
func TestUserEndpoint_UUIDSelector(t *testing.T) {
	e, sqlDB := setupIntegrationServer(t)
	insertTestUser(t, sqlDB, integrationUserID, integrationUsername, integrationEmail, "github", "gh-001")

	// GET /users/{uuid} — should return 200 with user resource.
	rec := sendJSON(t, e, http.MethodGet, "/users/"+integrationUserID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /users/{uuid}: got status %d, want %d; body: %s",
			rec.Code, http.StatusOK, rec.Body.String())
	}

	user := parseUserResponse(t, rec)
	if user.ID != integrationUserID {
		t.Errorf("response id = %q; want %q", user.ID, integrationUserID)
	}
	if user.Username != integrationUsername {
		t.Errorf("response username = %q; want %q", user.Username, integrationUsername)
	}

	// Verify additional endpoint families accept the UUID selector.
	uuidEndpoints := []struct {
		name   string
		method string
		path   string
	}{
		{"GET /users/:id/keys", http.MethodGet, "/users/" + integrationUserID + "/keys"},
		{"GET /users/:id/tokens", http.MethodGet, "/users/" + integrationUserID + "/tokens"},
	}
	for _, tc := range uuidEndpoints {
		t.Run(tc.name, func(t *testing.T) {
			rec := sendJSON(t, e, tc.method, tc.path, "")
			if rec.Code != http.StatusOK {
				t.Errorf("%s: got status %d, want %d; body: %s",
					tc.name, rec.Code, http.StatusOK, rec.Body.String())
			}
		})
	}
}

// TestUserEndpoint_UsernameSelector verifies that user endpoints accept a
// username selector, resolve it to the canonical UUID via resolveUserID, and
// return 200 OK with the user resource — identical to the UUID response.
//
// Test Spec: TS-16-10
// Requirement: 16-REQ-3.1, 16-REQ-3.E3
//
// Currently FAILS: handlers call uuid.Parse directly and return 400 for
// non-UUID selectors. After resolver integration (task group 6), this test
// should pass.
func TestUserEndpoint_UsernameSelector(t *testing.T) {
	e, sqlDB := setupIntegrationServer(t)
	insertTestUser(t, sqlDB, integrationUserID, integrationUsername, integrationEmail, "github", "gh-001")

	// Test across multiple user endpoint families (TS-16-35).
	endpoints := []struct {
		name   string
		method string
		path   string
	}{
		{"GET /users/:id", http.MethodGet, "/users/" + integrationUsername},
		{"POST /users/:id/promote", http.MethodPost, "/users/" + integrationUsername + "/promote"},
		{"POST /users/:id/block", http.MethodPost, "/users/" + integrationUsername + "/block"},
		{"GET /users/:id/keys", http.MethodGet, "/users/" + integrationUsername + "/keys"},
		{"GET /users/:id/tokens", http.MethodGet, "/users/" + integrationUsername + "/tokens"},
	}

	for _, tc := range endpoints {
		t.Run(tc.name, func(t *testing.T) {
			rec := sendJSON(t, e, tc.method, tc.path, "")
			if rec.Code != http.StatusOK {
				t.Errorf("%s with username selector: got status %d, want %d; body: %s",
					tc.name, rec.Code, http.StatusOK, rec.Body.String())
			}
		})
	}

	// Verify GET /users/:id body contains the resolved canonical UUID.
	rec := sendJSON(t, e, http.MethodGet, "/users/"+integrationUsername, "")
	if rec.Code == http.StatusOK {
		user := parseUserResponse(t, rec)
		if user.ID != integrationUserID {
			t.Errorf("response id = %q; want %q", user.ID, integrationUserID)
		}
		if user.Username != integrationUsername {
			t.Errorf("response username = %q; want %q", user.Username, integrationUsername)
		}
	}
}

// TestUserEndpoint_EmailSelector verifies that user endpoints accept an email
// selector (containing '@'), resolve it to the canonical UUID via resolveUserID,
// and return 200 OK with the user resource.
//
// Test Spec: TS-16-10 (email variant), TS-16-35
// Requirement: 16-REQ-3.1, 16-REQ-3.E2
//
// Currently FAILS: uuid.Parse rejects the email → 400.
func TestUserEndpoint_EmailSelector(t *testing.T) {
	e, sqlDB := setupIntegrationServer(t)
	insertTestUser(t, sqlDB, integrationUserID, integrationUsername, integrationEmail, "github", "gh-001")

	// Test across multiple user endpoint families (TS-16-35).
	endpoints := []struct {
		name   string
		method string
		path   string
	}{
		{"GET /users/:id", http.MethodGet, "/users/" + integrationEmail},
		{"POST /users/:id/promote", http.MethodPost, "/users/" + integrationEmail + "/promote"},
		{"GET /users/:id/keys", http.MethodGet, "/users/" + integrationEmail + "/keys"},
		{"GET /users/:id/tokens", http.MethodGet, "/users/" + integrationEmail + "/tokens"},
	}

	for _, tc := range endpoints {
		t.Run(tc.name, func(t *testing.T) {
			rec := sendJSON(t, e, tc.method, tc.path, "")
			if rec.Code != http.StatusOK {
				t.Errorf("%s with email selector: got status %d, want %d; body: %s",
					tc.name, rec.Code, http.StatusOK, rec.Body.String())
			}
		})
	}

	// Verify GET /users/:id body contains the resolved canonical UUID.
	rec := sendJSON(t, e, http.MethodGet, "/users/"+integrationEmail, "")
	if rec.Code == http.StatusOK {
		user := parseUserResponse(t, rec)
		if user.ID != integrationUserID {
			t.Errorf("response id = %q; want %q", user.ID, integrationUserID)
		}
	}
}

// ========================================================================
// Task 2.2: User endpoint — not-found and error integration tests
// Test Spec: TS-16-11, TS-16-12, TS-16-35
// Requirements: 16-REQ-3.2, 16-REQ-3.3, 16-REQ-3.E4, 16-REQ-12.1
// ========================================================================

// TestUserEndpoint_UnknownSelector verifies that user endpoints return HTTP
// 404 with 'user not found' when the selector is syntactically valid but
// matches no user record, regardless of selector type (UUID, username, email).
//
// Test Spec: TS-16-11, TS-16-35 (unknown selector variant)
// Requirement: 16-REQ-3.2, 16-REQ-3.E4
//
// The unknown-UUID subtest passes now (backward compat). The unknown-username
// and unknown-email subtests FAIL because uuid.Parse rejects non-UUID
// selectors → 400 instead of the expected 404.
func TestUserEndpoint_UnknownSelector(t *testing.T) {
	e, sqlDB := setupIntegrationServer(t)
	// Seed a user so the DB is non-empty; unknown selectors should still 404.
	insertTestUser(t, sqlDB, integrationUserID, integrationUsername, integrationEmail, "github", "gh-001")

	unknownSelectors := []struct {
		name     string
		selector string
	}{
		{"unknown UUID", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"},
		{"unknown username", "nonexistent-user"},
		{"unknown email", "nobody@example.com"},
	}

	for _, tc := range unknownSelectors {
		t.Run(tc.name, func(t *testing.T) {
			rec := sendJSON(t, e, http.MethodGet, "/users/"+tc.selector, "")
			assertErrorResponse(t, rec, http.StatusNotFound, "user not found")
		})
	}
}

// TestUserEndpoint_DatabaseError verifies that user endpoints return HTTP 500
// with 'internal server error' when resolveUserID returns a non-ErrNoRows
// database error.
//
// Test Spec: TS-16-12
// Requirement: 16-REQ-3.3
//
// Currently FAILS: uuid.Parse("alice") rejects → 400 before the handler
// reaches the database. After resolver integration, resolveUserID will query
// the (broken) database and propagate the error → 500.
func TestUserEndpoint_DatabaseError(t *testing.T) {
	e, sqlDB := setupIntegrationServer(t)

	// Drop the users table to produce a real database error on query.
	if _, err := sqlDB.Exec("DROP TABLE users"); err != nil {
		t.Fatalf("failed to drop users table: %v", err)
	}

	rec := sendJSON(t, e, http.MethodGet, "/users/"+integrationUsername, "")
	assertErrorResponse(t, rec, http.StatusInternalServerError, "internal server error")
}

// ========================================================================
// Task 2.3: Org endpoint — selector variant integration tests
// Test Spec: TS-16-13, TS-16-14, TS-16-15, TS-16-36
// Requirements: 16-REQ-4.1, 16-REQ-4.2, 16-REQ-4.3, 16-REQ-4.E1,
//
//	16-REQ-4.E2, 16-REQ-4.E3, 16-REQ-12.2
//
// ========================================================================

// TestOrgEndpoint_UUIDSelector verifies that org endpoints accept a UUID
// selector and return 200 OK with the org resource — backward compatibility.
//
// Test Spec: TS-16-36 (UUID variant)
// Requirement: 16-REQ-4.E1, 16-PROP-7
func TestOrgEndpoint_UUIDSelector(t *testing.T) {
	e, sqlDB := setupIntegrationServer(t)
	insertTestOrg(t, sqlDB, integrationOrgID, integrationOrgName, integrationOrgSlug, "https://acme.example.com", "active")

	// GET /orgs/{uuid} — should return 200 with org resource.
	rec := sendJSON(t, e, http.MethodGet, "/orgs/"+integrationOrgID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /orgs/{uuid}: got status %d, want %d; body: %s",
			rec.Code, http.StatusOK, rec.Body.String())
	}

	var org handlers.OrgResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &org); err != nil {
		t.Fatalf("failed to parse OrgResponse: %v; body: %s", err, rec.Body.String())
	}
	if org.ID != integrationOrgID {
		t.Errorf("response id = %q; want %q", org.ID, integrationOrgID)
	}
	if org.Slug != integrationOrgSlug {
		t.Errorf("response slug = %q; want %q", org.Slug, integrationOrgSlug)
	}
}

// TestOrgEndpoint_SlugSelector verifies that org endpoints accept a slug
// selector, resolve it to the canonical UUID via resolveOrgID, and return
// 200 OK with the org resource — identical to the UUID response.
//
// Test Spec: TS-16-13, TS-16-36 (slug variant)
// Requirement: 16-REQ-4.1, 16-REQ-4.E2
//
// Currently FAILS: uuid.Parse rejects the slug → 400.
func TestOrgEndpoint_SlugSelector(t *testing.T) {
	e, sqlDB := setupIntegrationServer(t)
	insertTestOrg(t, sqlDB, integrationOrgID, integrationOrgName, integrationOrgSlug, "https://acme.example.com", "active")
	// Seed a user (needed as org member for member listing test subtests).
	insertTestUser(t, sqlDB, integrationUserID, integrationUsername, integrationEmail, "github", "gh-001")

	// Test across multiple org endpoint families (TS-16-36).
	endpoints := []struct {
		name   string
		method string
		path   string
	}{
		{"GET /orgs/:id", http.MethodGet, "/orgs/" + integrationOrgSlug},
		{"POST /orgs/:id/block", http.MethodPost, "/orgs/" + integrationOrgSlug + "/block"},
		{"POST /orgs/:id/unblock", http.MethodPost, "/orgs/" + integrationOrgSlug + "/unblock"},
		{"GET /orgs/:id/members", http.MethodGet, "/orgs/" + integrationOrgSlug + "/members"},
	}

	for _, tc := range endpoints {
		t.Run(tc.name, func(t *testing.T) {
			rec := sendJSON(t, e, tc.method, tc.path, "")
			if rec.Code != http.StatusOK {
				t.Errorf("%s with slug selector: got status %d, want %d; body: %s",
					tc.name, rec.Code, http.StatusOK, rec.Body.String())
			}
		})
	}

	// Verify GET /orgs/:id body contains the resolved canonical UUID.
	rec := sendJSON(t, e, http.MethodGet, "/orgs/"+integrationOrgSlug, "")
	if rec.Code == http.StatusOK {
		var org handlers.OrgResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &org); err != nil {
			t.Fatalf("failed to parse OrgResponse: %v; body: %s", err, rec.Body.String())
		}
		if org.ID != integrationOrgID {
			t.Errorf("response id = %q; want %q", org.ID, integrationOrgID)
		}
		if org.Slug != integrationOrgSlug {
			t.Errorf("response slug = %q; want %q", org.Slug, integrationOrgSlug)
		}
	}
}

// TestOrgEndpoint_UnknownSelector verifies that org endpoints return HTTP 404
// with 'organization not found' when the selector matches no org record.
//
// Test Spec: TS-16-14, TS-16-36 (unknown variant)
// Requirement: 16-REQ-4.2, 16-REQ-4.E3
//
// The unknown-UUID subtest passes now (backward compat). The unknown-slug
// subtest FAILS: uuid.Parse rejects → 400 instead of 404.
func TestOrgEndpoint_UnknownSelector(t *testing.T) {
	e, sqlDB := setupIntegrationServer(t)
	insertTestOrg(t, sqlDB, integrationOrgID, integrationOrgName, integrationOrgSlug, "", "active")

	unknownSelectors := []struct {
		name     string
		selector string
	}{
		{"unknown UUID", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"},
		{"unknown slug", "nonexistent-slug"},
	}

	for _, tc := range unknownSelectors {
		t.Run(tc.name, func(t *testing.T) {
			rec := sendJSON(t, e, http.MethodGet, "/orgs/"+tc.selector, "")
			assertErrorResponse(t, rec, http.StatusNotFound, "organization not found")
		})
	}
}

// TestOrgEndpoint_DatabaseError verifies that org endpoints return HTTP 500
// with 'internal server error' when resolveOrgID returns a non-ErrNoRows
// database error.
//
// Test Spec: TS-16-15
// Requirement: 16-REQ-4.3
//
// Currently FAILS: uuid.Parse("acme-corp") rejects → 400 before hitting DB.
func TestOrgEndpoint_DatabaseError(t *testing.T) {
	e, sqlDB := setupIntegrationServer(t)

	// Drop org_members first (FK dependency) then orgs to produce DB errors.
	if _, err := sqlDB.Exec("DROP TABLE org_members"); err != nil {
		t.Fatalf("failed to drop org_members table: %v", err)
	}
	if _, err := sqlDB.Exec("DROP TABLE orgs"); err != nil {
		t.Fatalf("failed to drop orgs table: %v", err)
	}

	rec := sendJSON(t, e, http.MethodGet, "/orgs/"+integrationOrgSlug, "")
	assertErrorResponse(t, rec, http.StatusInternalServerError, "internal server error")
}

// ========================================================================
// Task 2.4: Two-ID member endpoint integration tests
// Test Spec: TS-16-16, TS-16-17, TS-16-18, TS-16-37
// Requirements: 16-REQ-5.1, 16-REQ-5.2, 16-REQ-5.3, 16-REQ-5.E1,
//
//	16-REQ-5.E2, 16-REQ-5.E3, 16-REQ-12.3
//
// ========================================================================

// TestMemberEndpoint_UUIDAndUUID verifies that two-ID member endpoints
// accept UUID selectors for both {id} and {user_id} — backward compatibility.
// The handler should proceed identically to the pre-change behavior.
//
// Test Spec: TS-16-37 (uuid+uuid variant)
// Requirement: 16-REQ-5.1, 16-PROP-7
func TestMemberEndpoint_UUIDAndUUID(t *testing.T) {
	e, sqlDB := setupIntegrationServer(t)
	insertTestOrg(t, sqlDB, integrationOrgID, integrationOrgName, integrationOrgSlug, "", "active")
	insertTestUser(t, sqlDB, integrationUserID, integrationUsername, integrationEmail, "github", "gh-001")

	// PUT /orgs/{uuid}/members/{uuid} — add member, expect 204.
	rec := sendJSON(t, e, http.MethodPut, "/orgs/"+integrationOrgID+"/members/"+integrationUserID, "{}")
	if rec.Code != http.StatusNoContent {
		t.Errorf("PUT /orgs/{uuid}/members/{uuid}: got status %d, want %d; body: %s",
			rec.Code, http.StatusNoContent, rec.Body.String())
	}

	// Verify membership exists via GET /orgs/{uuid}/members.
	rec = sendJSON(t, e, http.MethodGet, "/orgs/"+integrationOrgID+"/members", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /orgs/{uuid}/members: got status %d, want %d", rec.Code, http.StatusOK)
	}

	var members []struct {
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &members); err != nil {
		t.Fatalf("failed to parse members response: %v", err)
	}
	found := false
	for _, m := range members {
		if m.UserID == integrationUserID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("membership for user %s not found in org %s", integrationUserID, integrationOrgID)
	}

	// DELETE /orgs/{uuid}/members/{uuid} — remove member, expect 204.
	rec = sendJSON(t, e, http.MethodDelete, "/orgs/"+integrationOrgID+"/members/"+integrationUserID, "")
	if rec.Code != http.StatusNoContent {
		t.Errorf("DELETE /orgs/{uuid}/members/{uuid}: got status %d, want %d; body: %s",
			rec.Code, http.StatusNoContent, rec.Body.String())
	}
}

// TestMemberEndpoint_SlugAndUsername verifies that two-ID member endpoints
// resolve both {id} as an org slug and {user_id} as a username independently,
// proceeding with both canonical UUIDs.
//
// Test Spec: TS-16-37 (slug+username variant)
// Requirement: 16-REQ-5.1, 16-REQ-5.E1
//
// Currently FAILS: uuid.Parse rejects the slug → 400.
func TestMemberEndpoint_SlugAndUsername(t *testing.T) {
	e, sqlDB := setupIntegrationServer(t)
	insertTestOrg(t, sqlDB, integrationOrgID, integrationOrgName, integrationOrgSlug, "", "active")
	insertTestUser(t, sqlDB, integrationUserID, integrationUsername, integrationEmail, "github", "gh-001")

	// PUT /orgs/{slug}/members/{username} — add member.
	rec := sendJSON(t, e, http.MethodPut, "/orgs/"+integrationOrgSlug+"/members/"+integrationUsername, "{}")
	if rec.Code != http.StatusNoContent {
		t.Errorf("PUT /orgs/{slug}/members/{username}: got status %d, want %d; body: %s",
			rec.Code, http.StatusNoContent, rec.Body.String())
	}

	// Verify membership exists using UUID-based query.
	verifyMembershipExists(t, sqlDB, integrationOrgID, integrationUserID)

	// DELETE /orgs/{slug}/members/{username} — remove member.
	rec = sendJSON(t, e, http.MethodDelete, "/orgs/"+integrationOrgSlug+"/members/"+integrationUsername, "")
	if rec.Code != http.StatusNoContent {
		t.Errorf("DELETE /orgs/{slug}/members/{username}: got status %d, want %d; body: %s",
			rec.Code, http.StatusNoContent, rec.Body.String())
	}
}

// TestMemberEndpoint_SlugAndEmail verifies that two-ID member endpoints
// resolve {id} as an org slug and {user_id} as a user email independently.
//
// Test Spec: TS-16-16, TS-16-37 (slug+email variant)
// Requirement: 16-REQ-5.1, 16-REQ-5.E2
//
// Currently FAILS: uuid.Parse rejects the slug → 400.
func TestMemberEndpoint_SlugAndEmail(t *testing.T) {
	e, sqlDB := setupIntegrationServer(t)
	insertTestOrg(t, sqlDB, integrationOrgID, integrationOrgName, integrationOrgSlug, "", "active")
	insertTestUser(t, sqlDB, integrationUserID, integrationUsername, integrationEmail, "github", "gh-001")

	// PUT /orgs/{slug}/members/{email} — add member.
	rec := sendJSON(t, e, http.MethodPut, "/orgs/"+integrationOrgSlug+"/members/"+integrationEmail, "{}")
	if rec.Code != http.StatusNoContent {
		t.Errorf("PUT /orgs/{slug}/members/{email}: got status %d, want %d; body: %s",
			rec.Code, http.StatusNoContent, rec.Body.String())
	}

	// Verify membership exists using UUID-based query.
	verifyMembershipExists(t, sqlDB, integrationOrgID, integrationUserID)
}

// TestMemberEndpoint_UnknownOrg verifies that two-ID member endpoints return
// HTTP 404 with 'organization not found' when resolveOrgID returns ErrNoRows,
// without attempting to resolve the user selector.
//
// Test Spec: TS-16-17, TS-16-37 (unknown org variant)
// Requirement: 16-REQ-5.2, 16-REQ-5.E3
//
// Currently FAILS: uuid.Parse("nonexistent-org") rejects → 400 instead of 404.
func TestMemberEndpoint_UnknownOrg(t *testing.T) {
	e, sqlDB := setupIntegrationServer(t)
	// Seed user but no org matching the selector.
	insertTestUser(t, sqlDB, integrationUserID, integrationUsername, integrationEmail, "github", "gh-001")

	rec := sendJSON(t, e, http.MethodPut, "/orgs/nonexistent-org/members/"+integrationUsername, "{}")
	assertErrorResponse(t, rec, http.StatusNotFound, "organization not found")
}

// TestMemberEndpoint_UnknownUser verifies that two-ID member endpoints return
// HTTP 404 with 'user not found' when the org resolves successfully but
// resolveUserID returns ErrNoRows.
//
// Test Spec: TS-16-18, TS-16-37 (unknown user variant)
// Requirement: 16-REQ-5.3
//
// Currently FAILS: uuid.Parse("acme-corp") rejects the slug → 400 for the
// org before the handler ever reaches user resolution.
func TestMemberEndpoint_UnknownUser(t *testing.T) {
	e, sqlDB := setupIntegrationServer(t)
	// Seed org but no user matching the selector.
	insertTestOrg(t, sqlDB, integrationOrgID, integrationOrgName, integrationOrgSlug, "", "active")

	rec := sendJSON(t, e, http.MethodPut, "/orgs/"+integrationOrgSlug+"/members/nobody", "{}")
	assertErrorResponse(t, rec, http.StatusNotFound, "user not found")
}

// ========================================================================
// Helpers
// ========================================================================

// verifyMembershipExists queries the org_members table directly to confirm
// that a membership row exists for the given org and user UUIDs.
func verifyMembershipExists(t *testing.T, sqlDB *sql.DB, orgID, userID string) {
	t.Helper()

	var exists int
	err := sqlDB.QueryRow(
		"SELECT 1 FROM org_members WHERE org_id = ? AND user_id = ?",
		orgID, userID,
	).Scan(&exists)
	if err != nil {
		t.Errorf("membership for user %s in org %s not found: %v", userID, orgID, err)
	}
}

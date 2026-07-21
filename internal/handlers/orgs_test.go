package handlers_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/txsvc/apikit"
	"github.com/txsvc/apikit/internal/db"
	"github.com/txsvc/apikit/internal/handlers"
)

// ========================================================================
// Org Test Helpers
// ========================================================================

// setupOrgAdminTestServer creates an Echo instance with RegisterOrgHandlers
// registered on a group with admin auth middleware and CacheMiddleware(CacheNoStore).
// Returns the Echo instance and the raw *sql.DB handle.
func setupOrgAdminTestServer(t *testing.T) (*echo.Echo, *sql.DB) {
	t.Helper()

	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	e := echo.New()
	// Use a group that mimics the APIGroup setup: CacheNoStore + admin auth.
	g := e.Group("", apikit.CacheMiddleware(apikit.CacheNoStore))
	g.Use(adminAuthMiddleware("test-admin-uuid"))
	handlers.RegisterOrgHandlers(g, database.SqlDB)

	return e, database.SqlDB
}

// insertTestOrg inserts an organization directly into the orgs table for test setup.
func insertTestOrg(t *testing.T, sqlDB *sql.DB, id, name, slug, url, status string) {
	t.Helper()

	now := "2024-01-01T00:00:00Z"
	_, err := sqlDB.Exec(
		`INSERT INTO orgs (id, name, slug, url, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, name, slug, url, status, now, now,
	)
	if err != nil {
		t.Fatalf("failed to insert test org: %v", err)
	}
}

// insertTestOrgMember inserts a membership row directly into org_members.
func insertTestOrgMember(t *testing.T, sqlDB *sql.DB, orgID, userID string) {
	t.Helper()

	now := "2024-01-01T00:00:00Z"
	_, err := sqlDB.Exec(
		`INSERT INTO org_members (org_id, user_id, created_at)
		 VALUES (?, ?, ?)`,
		orgID, userID, now,
	)
	if err != nil {
		t.Fatalf("failed to insert test org member: %v", err)
	}
}

// ========================================================================
// Task 1.1: TestRegisterOrgHandlers — route mounting, Cache-Control, nil DB
// Test Spec: TS-08-1, TS-08-2, TS-08-E1
// Requirements: 08-REQ-1.1, 08-REQ-1.2, 08-REQ-1.E1
// ========================================================================

// TestRegisterOrgHandlers_Routes verifies that RegisterOrgHandlers registers
// all 10 expected organization routes on the Echo group with the correct HTTP
// methods and paths.
//
// Test Spec: TS-08-1
// Requirement: 08-REQ-1.1
func TestRegisterOrgHandlers_Routes(t *testing.T) {
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	defer database.Close()

	e := echo.New()
	g := e.Group("")
	handlers.RegisterOrgHandlers(g, database.SqlDB)

	// The 10 expected (method, path) pairs from the spec.
	expected := map[string]bool{
		"POST /orgs":                          false,
		"GET /orgs":                           false,
		"GET /orgs/:id":                       false,
		"PATCH /orgs/:id":                     false,
		"DELETE /orgs/:id":                    false,
		"POST /orgs/:id/block":                false,
		"POST /orgs/:id/unblock":              false,
		"GET /orgs/:id/members":               false,
		"PUT /orgs/:id/members/:user_id":      false,
		"DELETE /orgs/:id/members/:user_id":   false,
	}

	routes := e.Routes()
	for _, r := range routes {
		key := r.Method + " " + r.Path
		if _, ok := expected[key]; ok {
			expected[key] = true
		}
	}

	found := 0
	for key, registered := range expected {
		if !registered {
			t.Errorf("expected route %s was not registered", key)
		} else {
			found++
		}
	}

	if found != 10 {
		t.Errorf("expected 10 routes to be registered, found %d", found)
	}
}

// TestRegisterOrgHandlers_CacheControl verifies that org endpoint responses
// carry the Cache-Control: no-store header inherited from the group middleware.
// The response must come from an actual registered route (non-404/405 status).
//
// Test Spec: TS-08-2
// Requirement: 08-REQ-1.2
func TestRegisterOrgHandlers_CacheControl(t *testing.T) {
	e, _ := setupOrgAdminTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/orgs", nil)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// The route must actually be registered (not 404/405).
	if rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed {
		t.Fatalf("GET /orgs returned %d; route is not registered", rec.Code)
	}

	cc := rec.Header().Get("Cache-Control")
	if cc != "no-store" {
		t.Errorf("expected Cache-Control header %q, got %q", "no-store", cc)
	}
}

// TestRegisterOrgHandlers_NilDB verifies that RegisterOrgHandlers panics
// immediately when passed a nil *sql.DB, before any routes are registered.
//
// Test Spec: TS-08-E1
// Requirement: 08-REQ-1.E1
func TestRegisterOrgHandlers_NilDB(t *testing.T) {
	e := echo.New()
	g := e.Group("")

	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		handlers.RegisterOrgHandlers(g, nil)
	}()

	if !panicked {
		t.Fatal("expected RegisterOrgHandlers to panic with nil *sql.DB, but it did not panic")
	}
}

// ========================================================================
// Task 1.2: TestValidateSlug — slug validation rules
// Test Spec: TS-08-59, TS-08-60, TS-08-61, TS-08-62, TS-08-63
// Requirements: 08-REQ-12.1, 08-REQ-12.2, 08-REQ-12.3, 08-REQ-12.4, 08-REQ-12.5
// ========================================================================

// TestValidateSlug_Valid verifies that a slug consisting of valid lowercase
// alphanumeric characters, hyphens, and underscores passes slug validation.
//
// Test Spec: TS-08-59
// Requirement: 08-REQ-12.1
func TestValidateSlug_Valid(t *testing.T) {
	err := handlers.ValidateSlug("my-valid_slug123")
	if err != nil {
		t.Errorf("expected valid slug to pass validation, got error: %v", err)
	}
}

// TestValidateSlug_MaxLength verifies that a slug of exactly 128 characters
// passes validation (boundary condition).
//
// Requirement: 08-REQ-12.1
func TestValidateSlug_MaxLength(t *testing.T) {
	slug := strings.Repeat("a", 128)
	err := handlers.ValidateSlug(slug)
	if err != nil {
		t.Errorf("expected 128-character slug to pass validation, got error: %v", err)
	}
}

// TestValidateSlug_UpperCase verifies that a slug containing uppercase letters
// is rejected with 'invalid slug format'.
//
// Test Spec: TS-08-60
// Requirement: 08-REQ-12.2
func TestValidateSlug_UpperCase(t *testing.T) {
	err := handlers.ValidateSlug("MySlug")
	if err == nil {
		t.Fatal("expected slug with uppercase letters to be rejected, got nil error")
	}
	if err.Error() != "invalid slug format" {
		t.Errorf("expected error message %q, got %q", "invalid slug format", err.Error())
	}
}

// TestValidateSlug_Spaces verifies that a slug containing spaces is rejected.
//
// Requirement: 08-REQ-12.2
func TestValidateSlug_Spaces(t *testing.T) {
	err := handlers.ValidateSlug("has space")
	if err == nil {
		t.Fatal("expected slug with spaces to be rejected, got nil error")
	}
	if err.Error() != "invalid slug format" {
		t.Errorf("expected error message %q, got %q", "invalid slug format", err.Error())
	}
}

// TestValidateSlug_SpecialChars verifies that a slug containing characters
// outside [a-z0-9_-] is rejected.
//
// Requirement: 08-REQ-12.2
func TestValidateSlug_SpecialChars(t *testing.T) {
	err := handlers.ValidateSlug("slug@invalid")
	if err == nil {
		t.Fatal("expected slug with special characters to be rejected, got nil error")
	}
	if err.Error() != "invalid slug format" {
		t.Errorf("expected error message %q, got %q", "invalid slug format", err.Error())
	}
}

// TestValidateSlug_LeadingHyphen verifies that a slug starting with a hyphen
// is rejected with 'invalid slug format'.
//
// Test Spec: TS-08-61
// Requirement: 08-REQ-12.3
func TestValidateSlug_LeadingHyphen(t *testing.T) {
	err := handlers.ValidateSlug("-bad-start")
	if err == nil {
		t.Fatal("expected slug starting with hyphen to be rejected, got nil error")
	}
	if err.Error() != "invalid slug format" {
		t.Errorf("expected error message %q, got %q", "invalid slug format", err.Error())
	}
}

// TestValidateSlug_LeadingUnderscore verifies that a slug starting with an
// underscore is rejected with 'invalid slug format'.
//
// Test Spec: TS-08-61
// Requirement: 08-REQ-12.3
func TestValidateSlug_LeadingUnderscore(t *testing.T) {
	err := handlers.ValidateSlug("_bad-start")
	if err == nil {
		t.Fatal("expected slug starting with underscore to be rejected, got nil error")
	}
	if err.Error() != "invalid slug format" {
		t.Errorf("expected error message %q, got %q", "invalid slug format", err.Error())
	}
}

// TestValidateSlug_TrailingHyphen verifies that a slug ending with a hyphen
// is rejected with 'invalid slug format'.
//
// Test Spec: TS-08-62
// Requirement: 08-REQ-12.4
func TestValidateSlug_TrailingHyphen(t *testing.T) {
	err := handlers.ValidateSlug("bad-end-")
	if err == nil {
		t.Fatal("expected slug ending with hyphen to be rejected, got nil error")
	}
	if err.Error() != "invalid slug format" {
		t.Errorf("expected error message %q, got %q", "invalid slug format", err.Error())
	}
}

// TestValidateSlug_TrailingUnderscore verifies that a slug ending with an
// underscore is rejected with 'invalid slug format'.
//
// Test Spec: TS-08-62
// Requirement: 08-REQ-12.4
func TestValidateSlug_TrailingUnderscore(t *testing.T) {
	err := handlers.ValidateSlug("bad-end_")
	if err == nil {
		t.Fatal("expected slug ending with underscore to be rejected, got nil error")
	}
	if err.Error() != "invalid slug format" {
		t.Errorf("expected error message %q, got %q", "invalid slug format", err.Error())
	}
}

// TestValidateSlug_TooLong verifies that a slug longer than 128 characters
// is rejected with 'invalid slug format'.
//
// Test Spec: TS-08-63
// Requirement: 08-REQ-12.5
func TestValidateSlug_TooLong(t *testing.T) {
	slug := strings.Repeat("a", 129)
	err := handlers.ValidateSlug(slug)
	if err == nil {
		t.Fatal("expected slug longer than 128 characters to be rejected, got nil error")
	}
	if err.Error() != "invalid slug format" {
		t.Errorf("expected error message %q, got %q", "invalid slug format", err.Error())
	}
}

// TestValidateSlug_Empty verifies that an empty slug is rejected.
//
// Requirement: 08-REQ-12.1 (length between 1 and 128)
func TestValidateSlug_Empty(t *testing.T) {
	err := handlers.ValidateSlug("")
	if err == nil {
		t.Fatal("expected empty slug to be rejected, got nil error")
	}
	if err.Error() != "invalid slug format" {
		t.Errorf("expected error message %q, got %q", "invalid slug format", err.Error())
	}
}

// ========================================================================
// Task 1.3: TestIsOrgMember — membership lookup
// Test Spec: TS-08-64, TS-08-65, TS-08-E16
// Requirements: 08-REQ-13.1, 08-REQ-13.2, 08-REQ-13.E1
// ========================================================================

// TestIsOrgMember_True verifies that isOrgMember returns (true, nil) when a
// matching row exists in org_members.
//
// Test Spec: TS-08-64
// Requirement: 08-REQ-13.1
func TestIsOrgMember_True(t *testing.T) {
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	defer database.Close()

	orgID := "org-uuid-001"
	userID := testUUID("user-uuid-001")

	// Insert prerequisite user and org rows (FK constraints require them).
	insertTestUser(t, database.SqlDB, userID, "testuser", "test@example.com", "github", "gh-001")
	insertTestOrg(t, database.SqlDB, orgID, "Test Org", "test-org", "", "active")
	insertTestOrgMember(t, database.SqlDB, orgID, userID)

	result, err := handlers.IsOrgMember(database.SqlDB, orgID, userID)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !result {
		t.Error("expected isOrgMember to return true, got false")
	}
}

// TestIsOrgMember_False verifies that isOrgMember returns (false, nil) when no
// matching row exists in org_members.
//
// Test Spec: TS-08-65
// Requirement: 08-REQ-13.2
func TestIsOrgMember_False(t *testing.T) {
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	defer database.Close()

	result, err := handlers.IsOrgMember(database.SqlDB, "org-uuid-nonexistent", "user-uuid-nonexistent")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result {
		t.Error("expected isOrgMember to return false, got true")
	}
}

// TestIsOrgMember_DBError verifies that isOrgMember returns (false, non-nil error)
// when the database query fails, and does not panic.
//
// Test Spec: TS-08-E16
// Requirement: 08-REQ-13.E1
func TestIsOrgMember_DBError(t *testing.T) {
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}

	// Close the database to force a query error.
	database.Close()

	result, err := handlers.IsOrgMember(database.SqlDB, "org-uuid", "user-uuid")
	if result {
		t.Error("expected isOrgMember to return false on DB error, got true")
	}
	if err == nil {
		t.Error("expected isOrgMember to return a non-nil error on DB failure, got nil")
	}
}

// ========================================================================
// Org Create Test Helpers
// ========================================================================

// setupOrgNonAdminTestServer creates an Echo instance with RegisterOrgHandlers
// registered on a group with non-admin auth middleware and CacheMiddleware(CacheNoStore).
// Returns the Echo instance and the raw *sql.DB handle.
func setupOrgNonAdminTestServer(t *testing.T) (*echo.Echo, *sql.DB) {
	t.Helper()

	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	e := echo.New()
	g := e.Group("", apikit.CacheMiddleware(apikit.CacheNoStore))
	g.Use(nonAdminAuthMiddleware(testUUID("non-admin-user-uuid")))
	handlers.RegisterOrgHandlers(g, database.SqlDB)

	return e, database.SqlDB
}

// parseOrgResponse parses the response body as an OrgResponse JSON object.
func parseOrgResponse(t *testing.T, rec *httptest.ResponseRecorder) handlers.OrgResponse {
	t.Helper()

	var org handlers.OrgResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &org); err != nil {
		t.Fatalf("failed to parse OrgResponse: %v\nbody: %s", err, rec.Body.String())
	}
	return org
}

// ========================================================================
// Task 2.1: TestCreateOrg — happy-path and validation tests for POST /orgs
// Test Spec: TS-08-3, TS-08-4, TS-08-5, TS-08-6
// Requirements: 08-REQ-2.1, 08-REQ-2.2, 08-REQ-2.3, 08-REQ-2.4
// ========================================================================

// TestCreateOrg_Success verifies that a valid POST /orgs request from an admin
// creates an organization and returns HTTP 201 with a correct OrgResponse.
// The id must be a valid UUID v4, status must be 'active', created_at must
// equal updated_at, and both must be RFC 3339 UTC timestamps.
//
// Test Spec: TS-08-3
// Requirement: 08-REQ-2.1
func TestCreateOrg_Success(t *testing.T) {
	e, _ := setupOrgAdminTestServer(t)

	body := `{"name":"Acme Corp","slug":"acme-corp","url":"https://acme.example.com"}`
	rec := sendJSON(t, e, http.MethodPost, "/orgs", body)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected HTTP 201, got %d; body: %s", rec.Code, rec.Body.String())
	}

	org := parseOrgResponse(t, rec)

	if !isUUID(org.ID) {
		t.Errorf("expected id to be a valid UUID, got %q", org.ID)
	}
	if org.Name != "Acme Corp" {
		t.Errorf("expected name %q, got %q", "Acme Corp", org.Name)
	}
	if org.Slug != "acme-corp" {
		t.Errorf("expected slug %q, got %q", "acme-corp", org.Slug)
	}
	if org.URL != "https://acme.example.com" {
		t.Errorf("expected url %q, got %q", "https://acme.example.com", org.URL)
	}
	if org.Status != "active" {
		t.Errorf("expected status %q, got %q", "active", org.Status)
	}
	if org.CreatedAt != org.UpdatedAt {
		t.Errorf("expected created_at (%q) to equal updated_at (%q)", org.CreatedAt, org.UpdatedAt)
	}
	if !isRFC3339(org.CreatedAt) {
		t.Errorf("expected created_at to be RFC 3339 UTC, got %q", org.CreatedAt)
	}
}

// TestCreateOrg_OptionalURL verifies that POST /orgs without a url field
// defaults url to an empty string in the response.
//
// Requirement: 08-REQ-2.1
func TestCreateOrg_OptionalURL(t *testing.T) {
	e, _ := setupOrgAdminTestServer(t)

	body := `{"name":"No URL Corp","slug":"no-url-corp"}`
	rec := sendJSON(t, e, http.MethodPost, "/orgs", body)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected HTTP 201, got %d; body: %s", rec.Code, rec.Body.String())
	}

	org := parseOrgResponse(t, rec)

	if org.URL != "" {
		t.Errorf("expected url to be empty string when omitted, got %q", org.URL)
	}
}

// TestCreateOrg_MissingName verifies that POST /orgs with a whitespace-only
// name returns HTTP 400 with error message 'name is required'.
//
// Test Spec: TS-08-4
// Requirement: 08-REQ-2.2
func TestCreateOrg_MissingName(t *testing.T) {
	e, _ := setupOrgAdminTestServer(t)

	body := `{"name":"   ","slug":"acme-corp"}`
	rec := sendJSON(t, e, http.MethodPost, "/orgs", body)

	assertErrorResponse(t, rec, http.StatusBadRequest, "name is required")
}

// TestCreateOrg_EmptyName verifies that POST /orgs with an empty name
// returns HTTP 400 with error message 'name is required'.
//
// Requirement: 08-REQ-2.2
func TestCreateOrg_EmptyName(t *testing.T) {
	e, _ := setupOrgAdminTestServer(t)

	body := `{"name":"","slug":"acme-corp"}`
	rec := sendJSON(t, e, http.MethodPost, "/orgs", body)

	assertErrorResponse(t, rec, http.StatusBadRequest, "name is required")
}

// TestCreateOrg_MissingSlug verifies that POST /orgs without a slug field
// returns HTTP 400 with error message 'slug is required'.
//
// Test Spec: TS-08-5
// Requirement: 08-REQ-2.3
func TestCreateOrg_MissingSlug(t *testing.T) {
	e, _ := setupOrgAdminTestServer(t)

	body := `{"name":"Acme Corp"}`
	rec := sendJSON(t, e, http.MethodPost, "/orgs", body)

	assertErrorResponse(t, rec, http.StatusBadRequest, "slug is required")
}

// TestCreateOrg_EmptySlug verifies that POST /orgs with an empty slug
// returns HTTP 400 with error message 'slug is required'.
//
// Requirement: 08-REQ-2.3
func TestCreateOrg_EmptySlug(t *testing.T) {
	e, _ := setupOrgAdminTestServer(t)

	body := `{"name":"Acme Corp","slug":""}`
	rec := sendJSON(t, e, http.MethodPost, "/orgs", body)

	assertErrorResponse(t, rec, http.StatusBadRequest, "slug is required")
}

// TestCreateOrg_InvalidSlug verifies that POST /orgs with a slug containing
// invalid characters returns HTTP 400 with error message 'invalid slug format'.
//
// Test Spec: TS-08-6
// Requirement: 08-REQ-2.4
func TestCreateOrg_InvalidSlug(t *testing.T) {
	e, _ := setupOrgAdminTestServer(t)

	body := `{"name":"Acme Corp","slug":"Acme Corp!"}`
	rec := sendJSON(t, e, http.MethodPost, "/orgs", body)

	assertErrorResponse(t, rec, http.StatusBadRequest, "invalid slug format")
}

// ========================================================================
// Task 2.2: TestCreateOrg — conflict, auth, and error tests for POST /orgs
// Test Spec: TS-08-7, TS-08-8, TS-08-9, TS-08-E2, TS-08-E3, TS-08-E4, TS-08-E5
// Requirements: 08-REQ-2.5, 08-REQ-2.6, 08-REQ-2.7, 08-REQ-2.E1,
//               08-REQ-2.E2, 08-REQ-2.E3, 08-REQ-2.E4
// ========================================================================

// TestCreateOrg_DuplicateName verifies that POST /orgs with a name that already
// exists returns HTTP 409 with error message 'organization name already exists'.
//
// Test Spec: TS-08-7
// Requirement: 08-REQ-2.5
func TestCreateOrg_DuplicateName(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	// Pre-insert an org with name 'Acme Corp'.
	insertTestOrg(t, sqlDB, "existing-org-uuid-1", "Acme Corp", "acme-corp", "", "active")

	// Try to create another org with the same name but a different slug.
	body := `{"name":"Acme Corp","slug":"different-slug"}`
	rec := sendJSON(t, e, http.MethodPost, "/orgs", body)

	assertErrorResponse(t, rec, http.StatusConflict, "organization name already exists")
}

// TestCreateOrg_DuplicateSlug verifies that POST /orgs with a slug that already
// exists returns HTTP 409 with error message 'organization slug already exists'.
//
// Test Spec: TS-08-8
// Requirement: 08-REQ-2.6
func TestCreateOrg_DuplicateSlug(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	// Pre-insert an org with slug 'acme-corp'.
	insertTestOrg(t, sqlDB, "existing-org-uuid-2", "Existing Corp", "acme-corp", "", "active")

	// Try to create another org with the same slug but a different name.
	body := `{"name":"New Corp","slug":"acme-corp"}`
	rec := sendJSON(t, e, http.MethodPost, "/orgs", body)

	assertErrorResponse(t, rec, http.StatusConflict, "organization slug already exists")
}

// TestCreateOrg_NonAdmin verifies that POST /orgs from a non-admin authenticated
// user returns HTTP 403 with error message 'forbidden'.
//
// Test Spec: TS-08-9
// Requirement: 08-REQ-2.7
func TestCreateOrg_NonAdmin(t *testing.T) {
	e, _ := setupOrgNonAdminTestServer(t)

	body := `{"name":"Acme Corp","slug":"acme-corp"}`
	rec := sendJSON(t, e, http.MethodPost, "/orgs", body)

	assertErrorResponse(t, rec, http.StatusForbidden, "forbidden")
}

// TestCreateOrg_SlugTooLong verifies that POST /orgs with a slug of exactly
// 129 characters returns HTTP 400 with error message 'invalid slug format'.
//
// Test Spec: TS-08-E2
// Requirement: 08-REQ-2.E1
func TestCreateOrg_SlugTooLong(t *testing.T) {
	e, _ := setupOrgAdminTestServer(t)

	slug := strings.Repeat("a", 129)
	body := `{"name":"Test Org","slug":"` + slug + `"}`
	rec := sendJSON(t, e, http.MethodPost, "/orgs", body)

	assertErrorResponse(t, rec, http.StatusBadRequest, "invalid slug format")
}

// TestCreateOrg_SlugBoundaryChars verifies that POST /orgs with slugs
// that start or end with a hyphen or underscore returns HTTP 400 with error
// message 'invalid slug format'.
//
// Test Spec: TS-08-E3
// Requirement: 08-REQ-2.E2
func TestCreateOrg_SlugBoundaryChars(t *testing.T) {
	invalidSlugs := []string{"-invalid", "invalid-", "_invalid", "invalid_"}

	for _, slug := range invalidSlugs {
		t.Run("slug="+slug, func(t *testing.T) {
			e, _ := setupOrgAdminTestServer(t)

			body := `{"name":"Test Org","slug":"` + slug + `"}`
			rec := sendJSON(t, e, http.MethodPost, "/orgs", body)

			assertErrorResponse(t, rec, http.StatusBadRequest, "invalid slug format")
		})
	}
}

// TestCreateOrg_DBError verifies that POST /orgs returns HTTP 500 with
// error message 'internal server error' when the database INSERT fails
// with a non-UNIQUE constraint error, and does not leak raw SQL error text.
//
// Test Spec: TS-08-E4
// Requirement: 08-REQ-2.E3
func TestCreateOrg_DBError(t *testing.T) {
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}

	e := echo.New()
	g := e.Group("", apikit.CacheMiddleware(apikit.CacheNoStore))
	g.Use(adminAuthMiddleware("test-admin-uuid"))
	handlers.RegisterOrgHandlers(g, database.SqlDB)

	// Close the database AFTER registering handlers to simulate a DB failure.
	database.Close()

	body := `{"name":"Test Org","slug":"test-org"}`
	rec := sendJSON(t, e, http.MethodPost, "/orgs", body)

	assertErrorResponse(t, rec, http.StatusInternalServerError, "internal server error")

	// Ensure no internal DB error details are exposed in the response.
	rawBody := rec.Body.String()
	lowered := strings.ToLower(rawBody)
	if strings.Contains(lowered, "sql") || strings.Contains(lowered, "sqlite") || strings.Contains(lowered, "database") {
		t.Errorf("response body appears to leak internal error details: %s", rawBody)
	}
}

// TestCreateOrg_InvalidJSON verifies that POST /orgs with a malformed JSON body
// returns HTTP 400 with a descriptive error message.
//
// Test Spec: TS-08-E5
// Requirement: 08-REQ-2.E4
func TestCreateOrg_InvalidJSON(t *testing.T) {
	e, _ := setupOrgAdminTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/orgs", strings.NewReader("not valid json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected HTTP 400, got %d; body: %s", rec.Code, rec.Body.String())
	}

	resp := parseErrorResponse(t, rec)

	if resp.Error.Code != http.StatusBadRequest {
		t.Errorf("expected error code 400, got %d", resp.Error.Code)
	}
	if resp.Error.Message == "" {
		t.Error("expected a non-empty error message")
	}
}

// ========================================================================
// Task 3: Additional Helpers for List and Get Organization Tests
// ========================================================================

// setupOrgNonAdminTestServerWithUserID creates an Echo instance with
// RegisterOrgHandlers registered on a group with non-admin auth middleware
// using the specified user ID and CacheMiddleware(CacheNoStore).
// Returns the Echo instance and the raw *sql.DB handle.
func setupOrgNonAdminTestServerWithUserID(t *testing.T, userID string) (*echo.Echo, *sql.DB) {
	t.Helper()

	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	e := echo.New()
	g := e.Group("", apikit.CacheMiddleware(apikit.CacheNoStore))
	g.Use(nonAdminAuthMiddleware(userID))
	handlers.RegisterOrgHandlers(g, database.SqlDB)

	return e, database.SqlDB
}

// ========================================================================
// Task 3.1: TestListOrgs — GET /orgs list handler
// Test Spec: TS-08-10, TS-08-11, TS-08-12, TS-08-13, TS-08-E6
// Requirements: 08-REQ-3.1, 08-REQ-3.2, 08-REQ-3.3, 08-REQ-3.4, 08-REQ-3.E1
// ========================================================================

// TestListOrgs_ExcludesBlocked verifies that GET /orgs without the
// include_blocked parameter returns only active organizations ordered by
// name ascending. Blocked organizations are absent from the result.
//
// Test Spec: TS-08-10
// Requirement: 08-REQ-3.1
func TestListOrgs_ExcludesBlocked(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	// Seed 3 orgs: 2 active, 1 blocked. Names chosen to verify alphabetical order.
	insertTestOrg(t, sqlDB, "org-uuid-zebra", "Zebra Corp", "zebra-corp", "", "active")
	insertTestOrg(t, sqlDB, "org-uuid-alpha", "Alpha Corp", "alpha-corp", "", "active")
	insertTestOrg(t, sqlDB, "org-uuid-beta", "Beta Corp", "beta-corp", "", "blocked")

	rec := sendGet(t, e, "/orgs")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	orgs := parseOrgsResponse(t, rec)
	if len(orgs) != 2 {
		t.Fatalf("expected 2 active orgs, got %d", len(orgs))
	}

	// Verify ordering: Alpha Corp first, Zebra Corp second.
	if orgs[0].Name != "Alpha Corp" {
		t.Errorf("expected first org name %q, got %q", "Alpha Corp", orgs[0].Name)
	}
	if orgs[1].Name != "Zebra Corp" {
		t.Errorf("expected second org name %q, got %q", "Zebra Corp", orgs[1].Name)
	}

	// Verify blocked org is absent.
	for _, org := range orgs {
		if org.Name == "Beta Corp" {
			t.Error("expected 'Beta Corp' (blocked) to be absent from the result")
		}
	}
}

// TestListOrgs_IncludesBlocked verifies that GET /orgs?include_blocked=true
// returns all organizations including blocked ones, ordered by name ascending.
//
// Test Spec: TS-08-11
// Requirement: 08-REQ-3.2
func TestListOrgs_IncludesBlocked(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	insertTestOrg(t, sqlDB, "org-uuid-alpha", "Alpha Corp", "alpha-corp", "", "active")
	insertTestOrg(t, sqlDB, "org-uuid-beta", "Beta Corp", "beta-corp", "", "blocked")

	rec := sendGet(t, e, "/orgs?include_blocked=true")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	orgs := parseOrgsResponse(t, rec)

	hasAlpha := false
	hasBeta := false
	for _, org := range orgs {
		if org.Name == "Alpha Corp" {
			hasAlpha = true
		}
		if org.Name == "Beta Corp" {
			hasBeta = true
		}
	}
	if !hasAlpha {
		t.Error("expected 'Alpha Corp' in the result")
	}
	if !hasBeta {
		t.Error("expected 'Beta Corp' in the result")
	}
}

// TestListOrgs_Empty verifies that GET /orgs returns HTTP 200 with an empty
// JSON array when no organizations exist in the orgs table.
//
// Test Spec: TS-08-12
// Requirement: 08-REQ-3.3
func TestListOrgs_Empty(t *testing.T) {
	e, _ := setupOrgAdminTestServer(t)

	rec := sendGet(t, e, "/orgs")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// The response must be exactly "[]", not "null" or empty.
	body := strings.TrimSpace(rec.Body.String())
	if body != "[]" {
		t.Errorf("expected response body to be '[]', got %q", body)
	}
}

// TestListOrgs_OrderedByName verifies that GET /orgs returns organizations
// ordered alphabetically by name ascending, regardless of insertion order.
//
// Test Spec: TS-08-10 (ordering aspect)
// Requirement: 08-REQ-3.1
func TestListOrgs_OrderedByName(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	// Insert in reverse alphabetical order to verify sorting.
	insertTestOrg(t, sqlDB, "org-uuid-zebra", "Zebra Corp", "zebra-corp", "", "active")
	insertTestOrg(t, sqlDB, "org-uuid-alpha", "Alpha Corp", "alpha-corp", "", "active")

	rec := sendGet(t, e, "/orgs")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	orgs := parseOrgsResponse(t, rec)
	if len(orgs) < 2 {
		t.Fatalf("expected at least 2 orgs, got %d", len(orgs))
	}

	if orgs[0].Name != "Alpha Corp" {
		t.Errorf("expected first org to be 'Alpha Corp', got %q", orgs[0].Name)
	}
	if orgs[1].Name != "Zebra Corp" {
		t.Errorf("expected second org to be 'Zebra Corp', got %q", orgs[1].Name)
	}
}

// TestListOrgs_NonAdmin verifies that GET /orgs from a non-admin authenticated
// user returns HTTP 403 with error message 'forbidden'.
//
// Test Spec: TS-08-13
// Requirement: 08-REQ-3.4
func TestListOrgs_NonAdmin(t *testing.T) {
	e, _ := setupOrgNonAdminTestServer(t)

	rec := sendGet(t, e, "/orgs")

	assertErrorResponse(t, rec, http.StatusForbidden, "forbidden")
}

// TestListOrgs_DBError verifies that GET /orgs returns HTTP 500 with error
// message 'internal server error' when the database query fails.
//
// Test Spec: TS-08-E6
// Requirement: 08-REQ-3.E1
func TestListOrgs_DBError(t *testing.T) {
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}

	e := echo.New()
	g := e.Group("", apikit.CacheMiddleware(apikit.CacheNoStore))
	g.Use(adminAuthMiddleware("test-admin-uuid"))
	handlers.RegisterOrgHandlers(g, database.SqlDB)

	// Close the database AFTER registering handlers to simulate a DB failure.
	database.Close()

	rec := sendGet(t, e, "/orgs")

	assertErrorResponse(t, rec, http.StatusInternalServerError, "internal server error")
}

// ========================================================================
// Task 3.2: TestGetOrg — GET /orgs/:id get handler
// Test Spec: TS-08-14, TS-08-15, TS-08-16, TS-08-17, TS-08-18, TS-08-19, TS-08-E7
// Requirements: 08-REQ-4.1, 08-REQ-4.2, 08-REQ-4.3, 08-REQ-4.4,
//               08-REQ-4.5, 08-REQ-4.6, 08-REQ-4.E1
// ========================================================================

// TestGetOrg_AsAdmin verifies that GET /orgs/:id from an admin with a valid
// org UUID returns HTTP 200 with the correct OrgResponse fields and a
// non-empty ETag response header.
//
// Test Spec: TS-08-14
// Requirement: 08-REQ-4.1
func TestGetOrg_AsAdmin(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	orgID := "a0000001-0000-4000-8000-000000000001"
	insertTestOrg(t, sqlDB, orgID, "Admin Org", "admin-org", "https://admin.example.com", "active")

	rec := sendGet(t, e, "/orgs/"+orgID)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	org := parseOrgResponse(t, rec)

	if org.ID != orgID {
		t.Errorf("expected id %q, got %q", orgID, org.ID)
	}
	if org.Name != "Admin Org" {
		t.Errorf("expected name %q, got %q", "Admin Org", org.Name)
	}
	if org.Slug != "admin-org" {
		t.Errorf("expected slug %q, got %q", "admin-org", org.Slug)
	}
	if org.URL != "https://admin.example.com" {
		t.Errorf("expected url %q, got %q", "https://admin.example.com", org.URL)
	}

	// ETag header must be set.
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Error("expected ETag response header to be set, but it was empty")
	}
}

// TestGetOrg_AsMember verifies that GET /orgs/:id from a non-admin user
// who is a member of the organization returns HTTP 200 with the OrgResponse.
//
// Test Spec: TS-08-15
// Requirement: 08-REQ-4.2
func TestGetOrg_AsMember(t *testing.T) {
	memberUserID := testUUID("member-user-uuid-1")
	e, sqlDB := setupOrgNonAdminTestServerWithUserID(t, memberUserID)

	orgID := "a0000002-0000-4000-8000-000000000002"

	// Insert the user, org, and membership records (FK constraints require all).
	insertTestUser(t, sqlDB, memberUserID, "member", "member@example.com", "github", "gh-member")
	insertTestOrg(t, sqlDB, orgID, "Member Org", "member-org", "", "active")
	insertTestOrgMember(t, sqlDB, orgID, memberUserID)

	rec := sendGet(t, e, "/orgs/"+orgID)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	org := parseOrgResponse(t, rec)

	if org.ID != orgID {
		t.Errorf("expected id %q, got %q", orgID, org.ID)
	}
	if org.Name != "Member Org" {
		t.Errorf("expected name %q, got %q", "Member Org", org.Name)
	}
}

// TestGetOrg_NotMember verifies that GET /orgs/:id from a non-admin user
// who is NOT a member of the organization returns HTTP 403 with error
// message 'forbidden'.
//
// Test Spec: TS-08-16
// Requirement: 08-REQ-4.3
func TestGetOrg_NotMember(t *testing.T) {
	nonMemberUserID := testUUID("non-member-user-uuid-1")
	e, sqlDB := setupOrgNonAdminTestServerWithUserID(t, nonMemberUserID)

	orgID := "a0000003-0000-4000-8000-000000000003"

	// Insert the user and org but NO membership row.
	insertTestUser(t, sqlDB, nonMemberUserID, "outsider", "outsider@example.com", "github", "gh-outsider")
	insertTestOrg(t, sqlDB, orgID, "Forbidden Org", "forbidden-org", "", "active")

	rec := sendGet(t, e, "/orgs/"+orgID)

	assertErrorResponse(t, rec, http.StatusForbidden, "forbidden")
}

// TestGetOrg_NotFound verifies that GET /orgs/:id for a non-existent
// organization UUID returns HTTP 404 with error message 'organization not found'.
//
// Test Spec: TS-08-17
// Requirement: 08-REQ-4.4
func TestGetOrg_NotFound(t *testing.T) {
	e, _ := setupOrgAdminTestServer(t)

	// Use a valid UUID that does not exist in the orgs table.
	rec := sendGet(t, e, "/orgs/00000000-0000-0000-0000-000000000000")

	assertErrorResponse(t, rec, http.StatusNotFound, "organization not found")
}

// TestGetOrg_InvalidID verifies that GET /orgs/:id with a path parameter
// that is not a valid UUID returns HTTP 400 with error message
// 'invalid organization id'.
//
// Test Spec: TS-08-18
// Requirement: 08-REQ-4.5
func TestGetOrg_InvalidID(t *testing.T) {
	e, _ := setupOrgAdminTestServer(t)

	rec := sendGet(t, e, "/orgs/not-a-uuid")

	assertErrorResponse(t, rec, http.StatusBadRequest, "invalid organization id")
}

// TestGetOrg_ETag verifies that GET /orgs/:id with an If-None-Match header
// matching the current ETag returns HTTP 304 with no body.
//
// Test Spec: TS-08-19
// Requirement: 08-REQ-4.6
func TestGetOrg_ETag(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	orgID := "a0000004-0000-4000-8000-000000000004"
	insertTestOrg(t, sqlDB, orgID, "ETag Org", "etag-org", "", "active")

	// First request: get the ETag from the response.
	rec1 := sendGet(t, e, "/orgs/"+orgID)

	if rec1.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 on first request, got %d; body: %s", rec1.Code, rec1.Body.String())
	}

	etag := rec1.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag header to be set on first request")
	}

	// Second request: send the ETag as If-None-Match; expect 304.
	rec2 := sendGetWithHeaders(t, e, "/orgs/"+orgID, map[string]string{
		"If-None-Match": etag,
	})

	if rec2.Code != http.StatusNotModified {
		t.Errorf("expected HTTP 304, got %d", rec2.Code)
	}
	if rec2.Body.Len() != 0 {
		t.Errorf("expected empty body for 304 response, got %q", rec2.Body.String())
	}
}

// TestGetOrg_MembershipDBError verifies that GET /orgs/:id returns HTTP 500
// with error message 'internal server error' when the org lookup succeeds
// but the isOrgMember query fails with a database error. The org data must
// NOT be returned.
//
// Test Spec: TS-08-E7
// Requirement: 08-REQ-4.E1
func TestGetOrg_MembershipDBError(t *testing.T) {
	nonAdminUserID := testUUID("dberr-member-user-uuid")

	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	orgID := "a0000005-0000-4000-8000-000000000005"

	// Insert user and org while the database is intact.
	insertTestUser(t, database.SqlDB, nonAdminUserID, "dberr-user", "dberr@example.com", "github", "gh-dberr")
	insertTestOrg(t, database.SqlDB, orgID, "DBErr Org", "dberr-org", "", "active")

	// Drop the org_members table so the isOrgMember query fails
	// while the orgs table query still succeeds.
	_, err = database.SqlDB.Exec("DROP TABLE org_members")
	if err != nil {
		t.Fatalf("failed to drop org_members table: %v", err)
	}

	e := echo.New()
	g := e.Group("", apikit.CacheMiddleware(apikit.CacheNoStore))
	g.Use(nonAdminAuthMiddleware(nonAdminUserID))
	handlers.RegisterOrgHandlers(g, database.SqlDB)

	rec := sendGet(t, e, "/orgs/"+orgID)

	assertErrorResponse(t, rec, http.StatusInternalServerError, "internal server error")
}

// ========================================================================
// Task 4.1: TestUpdateOrg — PATCH /orgs/:id update handler
// Test Spec: TS-08-20, TS-08-21, TS-08-22, TS-08-23, TS-08-24, TS-08-25,
//            TS-08-26, TS-08-E8, TS-08-E9
// Requirements: 08-REQ-5.1, 08-REQ-5.2, 08-REQ-5.3, 08-REQ-5.4, 08-REQ-5.5,
//               08-REQ-5.6, 08-REQ-5.7, 08-REQ-5.E1, 08-REQ-5.E2
// ========================================================================

// TestUpdateOrg_Name verifies that PATCH /orgs/:id with a valid name update
// returns HTTP 200 with the updated name, an unchanged slug, and an
// updated_at value that is >= the original.
//
// Test Spec: TS-08-20
// Requirement: 08-REQ-5.1
func TestUpdateOrg_Name(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	orgID := "b0000001-0000-4000-8000-000000000001"
	insertTestOrg(t, sqlDB, orgID, "Acme Corp", "acme-corp", "https://acme.example.com", "active")

	// Retrieve original to compare updated_at and slug.
	origRec := sendGet(t, e, "/orgs/"+orgID)
	if origRec.Code != http.StatusOK {
		t.Fatalf("setup: expected HTTP 200 on GET, got %d; body: %s", origRec.Code, origRec.Body.String())
	}
	original := parseOrgResponse(t, origRec)

	body := `{"name":"Acme Corporation"}`
	rec := sendJSON(t, e, http.MethodPatch, "/orgs/"+orgID, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	updated := parseOrgResponse(t, rec)

	if updated.Name != "Acme Corporation" {
		t.Errorf("expected name %q, got %q", "Acme Corporation", updated.Name)
	}
	if updated.Slug != original.Slug {
		t.Errorf("expected slug to remain %q, got %q", original.Slug, updated.Slug)
	}

	origTime, err1 := time.Parse(time.RFC3339, original.UpdatedAt)
	updTime, err2 := time.Parse(time.RFC3339, updated.UpdatedAt)
	if err1 != nil || err2 != nil {
		t.Fatalf("failed to parse updated_at timestamps: orig=%v upd=%v", err1, err2)
	}
	if updTime.Before(origTime) {
		t.Errorf("expected updated_at %v >= original %v", updTime, origTime)
	}
}

// TestUpdateOrg_URL verifies that PATCH /orgs/:id with a valid url update
// returns HTTP 200 with the updated URL.
//
// Requirement: 08-REQ-5.1
func TestUpdateOrg_URL(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	orgID := "b0000002-0000-4000-8000-000000000002"
	insertTestOrg(t, sqlDB, orgID, "URL Corp", "url-corp", "https://old.example.com", "active")

	body := `{"url":"https://new.example.com"}`
	rec := sendJSON(t, e, http.MethodPatch, "/orgs/"+orgID, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	updated := parseOrgResponse(t, rec)

	if updated.URL != "https://new.example.com" {
		t.Errorf("expected url %q, got %q", "https://new.example.com", updated.URL)
	}
	// Name must remain unchanged.
	if updated.Name != "URL Corp" {
		t.Errorf("expected name to remain %q, got %q", "URL Corp", updated.Name)
	}
}

// TestUpdateOrg_BothFields verifies that PATCH /orgs/:id with both name and
// url updates returns HTTP 200 with both fields updated.
//
// Requirement: 08-REQ-5.1
func TestUpdateOrg_BothFields(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	orgID := "b0000003-0000-4000-8000-000000000003"
	insertTestOrg(t, sqlDB, orgID, "Both Corp", "both-corp", "https://both.example.com", "active")

	body := `{"name":"Both Updated","url":"https://updated.example.com"}`
	rec := sendJSON(t, e, http.MethodPatch, "/orgs/"+orgID, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	updated := parseOrgResponse(t, rec)

	if updated.Name != "Both Updated" {
		t.Errorf("expected name %q, got %q", "Both Updated", updated.Name)
	}
	if updated.URL != "https://updated.example.com" {
		t.Errorf("expected url %q, got %q", "https://updated.example.com", updated.URL)
	}
}

// TestUpdateOrg_SlugIgnored verifies that PATCH /orgs/:id with a slug field
// in the body silently ignores it and keeps the original slug.
//
// Test Spec: TS-08-21
// Requirement: 08-REQ-5.2
func TestUpdateOrg_SlugIgnored(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	orgID := "b0000004-0000-4000-8000-000000000004"
	insertTestOrg(t, sqlDB, orgID, "Slug Corp", "original-slug", "", "active")

	body := `{"name":"New Name","slug":"new-slug"}`
	rec := sendJSON(t, e, http.MethodPatch, "/orgs/"+orgID, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	updated := parseOrgResponse(t, rec)

	if updated.Name != "New Name" {
		t.Errorf("expected name %q, got %q", "New Name", updated.Name)
	}
	if updated.Slug != "original-slug" {
		t.Errorf("expected slug to remain %q, got %q", "original-slug", updated.Slug)
	}
}

// TestUpdateOrg_EmptyBody verifies that PATCH /orgs/:id with a body
// containing no recognized fields (both name and url absent) returns
// HTTP 400 with error message 'no fields to update'.
//
// Test Spec: TS-08-22
// Requirement: 08-REQ-5.3
func TestUpdateOrg_EmptyBody(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	orgID := "b0000005-0000-4000-8000-000000000005"
	insertTestOrg(t, sqlDB, orgID, "EmptyBody Corp", "emptybody-corp", "", "active")

	body := `{}`
	rec := sendJSON(t, e, http.MethodPatch, "/orgs/"+orgID, body)

	assertErrorResponse(t, rec, http.StatusBadRequest, "no fields to update")
}

// TestUpdateOrg_DuplicateName verifies that PATCH /orgs/:id with a name
// that conflicts with another org returns HTTP 409 with error message
// 'organization name already exists'.
//
// Test Spec: TS-08-23
// Requirement: 08-REQ-5.4
func TestUpdateOrg_DuplicateName(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	targetOrgID := "b0000006-0000-4000-8000-000000000006"
	insertTestOrg(t, sqlDB, targetOrgID, "Target Corp", "target-corp", "", "active")
	insertTestOrg(t, sqlDB, "b0000007-0000-4000-8000-000000000007", "Taken Name Corp", "taken-name-corp", "", "active")

	body := `{"name":"Taken Name Corp"}`
	rec := sendJSON(t, e, http.MethodPatch, "/orgs/"+targetOrgID, body)

	assertErrorResponse(t, rec, http.StatusConflict, "organization name already exists")
}

// TestUpdateOrg_NotFound verifies that PATCH /orgs/:id for a non-existent
// organization returns HTTP 404 with error message 'organization not found'.
//
// Test Spec: TS-08-24
// Requirement: 08-REQ-5.5
func TestUpdateOrg_NotFound(t *testing.T) {
	e, _ := setupOrgAdminTestServer(t)

	nonExistentUUID := "00000000-0000-0000-0000-000000000000"
	body := `{"name":"New Name"}`
	rec := sendJSON(t, e, http.MethodPatch, "/orgs/"+nonExistentUUID, body)

	assertErrorResponse(t, rec, http.StatusNotFound, "organization not found")
}

// TestUpdateOrg_InvalidID verifies that PATCH /orgs/:id with an invalid
// UUID path parameter returns HTTP 400 with error message
// 'invalid organization id'.
//
// Test Spec: TS-08-25
// Requirement: 08-REQ-5.6
func TestUpdateOrg_InvalidID(t *testing.T) {
	e, _ := setupOrgAdminTestServer(t)

	body := `{"name":"New Name"}`
	rec := sendJSON(t, e, http.MethodPatch, "/orgs/not-a-uuid", body)

	assertErrorResponse(t, rec, http.StatusBadRequest, "invalid organization id")
}

// TestUpdateOrg_NonAdmin verifies that PATCH /orgs/:id from a non-admin
// user returns HTTP 403 with error message 'forbidden'.
//
// Test Spec: TS-08-26
// Requirement: 08-REQ-5.7
func TestUpdateOrg_NonAdmin(t *testing.T) {
	e, sqlDB := setupOrgNonAdminTestServer(t)

	orgID := "b0000008-0000-4000-8000-000000000008"
	insertTestOrg(t, sqlDB, orgID, "NonAdmin Corp", "nonadmin-corp", "", "active")

	body := `{"name":"New Name"}`
	rec := sendJSON(t, e, http.MethodPatch, "/orgs/"+orgID, body)

	assertErrorResponse(t, rec, http.StatusForbidden, "forbidden")
}

// TestUpdateOrg_EmptyName verifies that PATCH /orgs/:id with a name that
// is empty after whitespace trimming returns HTTP 400 with error message
// 'name is required'.
//
// Test Spec: TS-08-E8
// Requirement: 08-REQ-5.E1
func TestUpdateOrg_EmptyName(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	orgID := "b0000009-0000-4000-8000-000000000009"
	insertTestOrg(t, sqlDB, orgID, "EmptyName Corp", "emptyname-corp", "", "active")

	body := `{"name":"   "}`
	rec := sendJSON(t, e, http.MethodPatch, "/orgs/"+orgID, body)

	assertErrorResponse(t, rec, http.StatusBadRequest, "name is required")
}

// TestUpdateOrg_DBError verifies that PATCH /orgs/:id returns HTTP 500
// with error message 'internal server error' when the DB UPDATE fails
// with a non-UNIQUE database error. Uses a SQLite BEFORE UPDATE trigger
// to simulate a database failure on the UPDATE statement while allowing
// the initial org lookup SELECT to succeed.
//
// Test Spec: TS-08-E9
// Requirement: 08-REQ-5.E2
func TestUpdateOrg_DBError(t *testing.T) {
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	e := echo.New()
	g := e.Group("", apikit.CacheMiddleware(apikit.CacheNoStore))
	g.Use(adminAuthMiddleware("test-admin-uuid"))
	handlers.RegisterOrgHandlers(g, database.SqlDB)

	orgID := "b000000a-0000-4000-8000-00000000000a"
	insertTestOrg(t, database.SqlDB, orgID, "DBError Corp", "dberror-corp", "", "active")

	// Install a BEFORE UPDATE trigger that raises a generic DB error.
	// The org lookup (SELECT) succeeds, but the UPDATE statement fails.
	_, err = database.SqlDB.Exec(`
		CREATE TRIGGER fail_orgs_update BEFORE UPDATE ON orgs
		BEGIN SELECT RAISE(FAIL, 'simulated db error'); END
	`)
	if err != nil {
		t.Fatalf("failed to create trigger: %v", err)
	}

	body := `{"name":"New Name"}`
	rec := sendJSON(t, e, http.MethodPatch, "/orgs/"+orgID, body)

	assertErrorResponse(t, rec, http.StatusInternalServerError, "internal server error")
}

// ========================================================================
// Task 4.2: TestDeleteOrg — DELETE /orgs/:id handler
// Test Spec: TS-08-27, TS-08-28, TS-08-29, TS-08-30, TS-08-E10
// Requirements: 08-REQ-6.1, 08-REQ-6.2, 08-REQ-6.3, 08-REQ-6.4, 08-REQ-6.E1
// ========================================================================

// TestDeleteOrg_Success verifies that DELETE /orgs/:id from an admin
// deletes the org, cascade-deletes org_members rows, preserves the user
// row, and returns HTTP 204 with no body.
//
// Test Spec: TS-08-27
// Requirement: 08-REQ-6.1
func TestDeleteOrg_Success(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	orgID := "c0000001-0000-4000-8000-000000000001"
	userID := "c0000001-0000-4000-8000-100000000001"

	insertTestUser(t, sqlDB, userID, "deleteuser", "delete@example.com", "github", "gh-del")
	insertTestOrg(t, sqlDB, orgID, "Delete Corp", "delete-corp", "", "active")
	insertTestOrgMember(t, sqlDB, orgID, userID)

	rec := sendDelete(t, e, "/orgs/"+orgID)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected HTTP 204, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Body must be empty.
	if rec.Body.Len() != 0 {
		t.Errorf("expected empty body for 204 response, got %q", rec.Body.String())
	}

	// Verify org row is absent.
	var orgCount int
	err := sqlDB.QueryRow("SELECT COUNT(*) FROM orgs WHERE id = ?", orgID).Scan(&orgCount)
	if err != nil {
		t.Fatalf("failed to query orgs table: %v", err)
	}
	if orgCount != 0 {
		t.Errorf("expected org row to be deleted, found %d rows", orgCount)
	}

	// Verify org_members rows are absent (cascade delete).
	var memberCount int
	err = sqlDB.QueryRow("SELECT COUNT(*) FROM org_members WHERE org_id = ?", orgID).Scan(&memberCount)
	if err != nil {
		t.Fatalf("failed to query org_members table: %v", err)
	}
	if memberCount != 0 {
		t.Errorf("expected org_members rows to be cascade-deleted, found %d rows", memberCount)
	}

	// Verify user row is preserved.
	var userCount int
	err = sqlDB.QueryRow("SELECT COUNT(*) FROM users WHERE id = ?", userID).Scan(&userCount)
	if err != nil {
		t.Fatalf("failed to query users table: %v", err)
	}
	if userCount != 1 {
		t.Errorf("expected user row to be preserved, found %d rows", userCount)
	}
}

// TestDeleteOrg_CascadesMembers verifies that deleting an organization
// with multiple members cascade-deletes all org_members rows for that org.
//
// Test Spec: TS-08-27
// Requirement: 08-REQ-6.1 (cascade aspect)
func TestDeleteOrg_CascadesMembers(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	orgID := "c0000002-0000-4000-8000-000000000002"
	user1ID := "c0000002-0000-4000-8000-100000000001"
	user2ID := "c0000002-0000-4000-8000-100000000002"

	insertTestUser(t, sqlDB, user1ID, "user1", "user1@example.com", "github", "gh-u1")
	insertTestUser(t, sqlDB, user2ID, "user2", "user2@example.com", "github", "gh-u2")
	insertTestOrg(t, sqlDB, orgID, "Cascade Corp", "cascade-corp", "", "active")
	insertTestOrgMember(t, sqlDB, orgID, user1ID)
	insertTestOrgMember(t, sqlDB, orgID, user2ID)

	rec := sendDelete(t, e, "/orgs/"+orgID)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected HTTP 204, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var memberCount int
	err := sqlDB.QueryRow("SELECT COUNT(*) FROM org_members WHERE org_id = ?", orgID).Scan(&memberCount)
	if err != nil {
		t.Fatalf("failed to query org_members table: %v", err)
	}
	if memberCount != 0 {
		t.Errorf("expected all org_members rows to be cascade-deleted, found %d rows", memberCount)
	}
}

// TestDeleteOrg_UsersPreserved verifies that deleting an organization
// does not affect user rows in the users table.
//
// Test Spec: TS-08-27
// Requirement: 08-REQ-6.1 (users unaffected aspect)
func TestDeleteOrg_UsersPreserved(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	orgID := "c0000003-0000-4000-8000-000000000003"
	userID := "c0000003-0000-4000-8000-100000000001"

	insertTestUser(t, sqlDB, userID, "preserved", "preserved@example.com", "github", "gh-prsv")
	insertTestOrg(t, sqlDB, orgID, "Preserve Corp", "preserve-corp", "", "active")
	insertTestOrgMember(t, sqlDB, orgID, userID)

	rec := sendDelete(t, e, "/orgs/"+orgID)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected HTTP 204, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// User row must still exist.
	var userCount int
	err := sqlDB.QueryRow("SELECT COUNT(*) FROM users WHERE id = ?", userID).Scan(&userCount)
	if err != nil {
		t.Fatalf("failed to query users table: %v", err)
	}
	if userCount != 1 {
		t.Errorf("expected user row to be preserved after org deletion, found %d rows", userCount)
	}
}

// TestDeleteOrg_NotFound verifies that DELETE /orgs/:id for a non-existent
// organization returns HTTP 404 with error message 'organization not found'.
//
// Test Spec: TS-08-28
// Requirement: 08-REQ-6.2
func TestDeleteOrg_NotFound(t *testing.T) {
	e, _ := setupOrgAdminTestServer(t)

	nonExistentUUID := "00000000-0000-0000-0000-000000000000"
	rec := sendDelete(t, e, "/orgs/"+nonExistentUUID)

	assertErrorResponse(t, rec, http.StatusNotFound, "organization not found")
}

// TestDeleteOrg_InvalidID verifies that DELETE /orgs/:id with an invalid
// UUID path parameter returns HTTP 400 with error message
// 'invalid organization id'.
//
// Test Spec: TS-08-29
// Requirement: 08-REQ-6.3
func TestDeleteOrg_InvalidID(t *testing.T) {
	e, _ := setupOrgAdminTestServer(t)

	rec := sendDelete(t, e, "/orgs/not-a-uuid")

	assertErrorResponse(t, rec, http.StatusBadRequest, "invalid organization id")
}

// TestDeleteOrg_NonAdmin verifies that DELETE /orgs/:id from a non-admin
// user returns HTTP 403 with error message 'forbidden'.
//
// Test Spec: TS-08-30
// Requirement: 08-REQ-6.4
func TestDeleteOrg_NonAdmin(t *testing.T) {
	e, sqlDB := setupOrgNonAdminTestServer(t)

	orgID := "c0000004-0000-4000-8000-000000000004"
	insertTestOrg(t, sqlDB, orgID, "NonAdmin Del Corp", "nonadmin-del-corp", "", "active")

	rec := sendDelete(t, e, "/orgs/"+orgID)

	assertErrorResponse(t, rec, http.StatusForbidden, "forbidden")
}

// TestDeleteOrg_DBError verifies that DELETE /orgs/:id returns HTTP 500
// with error message 'internal server error' when the DB DELETE fails
// with a database error. Uses a SQLite BEFORE DELETE trigger to simulate
// the failure.
//
// Test Spec: TS-08-E10
// Requirement: 08-REQ-6.E1
func TestDeleteOrg_DBError(t *testing.T) {
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	e := echo.New()
	g := e.Group("", apikit.CacheMiddleware(apikit.CacheNoStore))
	g.Use(adminAuthMiddleware("test-admin-uuid"))
	handlers.RegisterOrgHandlers(g, database.SqlDB)

	orgID := "c0000005-0000-4000-8000-000000000005"
	insertTestOrg(t, database.SqlDB, orgID, "DBError Del Corp", "dberror-del-corp", "", "active")

	// Install a BEFORE DELETE trigger that raises a generic DB error.
	// This allows any prior org lookup to succeed while the DELETE fails.
	_, err = database.SqlDB.Exec(`
		CREATE TRIGGER fail_orgs_delete BEFORE DELETE ON orgs
		BEGIN SELECT RAISE(FAIL, 'simulated db error'); END
	`)
	if err != nil {
		t.Fatalf("failed to create trigger: %v", err)
	}

	rec := sendDelete(t, e, "/orgs/"+orgID)

	assertErrorResponse(t, rec, http.StatusInternalServerError, "internal server error")
}

// ========================================================================
// Task 5.1: TestBlockOrg — POST /orgs/:id/block handler
// Test Spec: TS-08-31, TS-08-32, TS-08-33, TS-08-34, TS-08-35, TS-08-E11
// Requirements: 08-REQ-7.1, 08-REQ-7.2, 08-REQ-7.3, 08-REQ-7.4,
//               08-REQ-7.5, 08-REQ-7.E1
// ========================================================================

// TestBlockOrg_Success verifies that POST /orgs/:id/block on an active
// organization returns HTTP 200 with status='blocked' and an updated_at
// value that is >= the original.
//
// Test Spec: TS-08-31
// Requirement: 08-REQ-7.1
func TestBlockOrg_Success(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	orgID := "d0000001-0000-4000-8000-000000000001"
	insertTestOrg(t, sqlDB, orgID, "Block Corp", "block-corp", "", "active")

	// Retrieve original to compare updated_at.
	origRec := sendGet(t, e, "/orgs/"+orgID)
	if origRec.Code != http.StatusOK {
		t.Fatalf("setup: expected HTTP 200 on GET, got %d; body: %s", origRec.Code, origRec.Body.String())
	}
	original := parseOrgResponse(t, origRec)

	rec := sendPost(t, e, "/orgs/"+orgID+"/block")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	body := parseOrgResponse(t, rec)

	if body.Status != "blocked" {
		t.Errorf("expected status %q, got %q", "blocked", body.Status)
	}

	origTime, err1 := time.Parse(time.RFC3339, original.UpdatedAt)
	updTime, err2 := time.Parse(time.RFC3339, body.UpdatedAt)
	if err1 != nil || err2 != nil {
		t.Fatalf("failed to parse updated_at timestamps: orig=%v upd=%v", err1, err2)
	}
	if updTime.Before(origTime) {
		t.Errorf("expected updated_at %v >= original %v", updTime, origTime)
	}
}

// TestBlockOrg_UpdatesTimestamp verifies that blocking an active organization
// changes the updated_at field compared to the original created_at.
//
// Requirement: 08-REQ-7.1
func TestBlockOrg_UpdatesTimestamp(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	orgID := "d0000002-0000-4000-8000-000000000002"
	insertTestOrg(t, sqlDB, orgID, "Timestamp Corp", "timestamp-corp", "", "active")

	rec := sendPost(t, e, "/orgs/"+orgID+"/block")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	body := parseOrgResponse(t, rec)

	createdTime, err1 := time.Parse(time.RFC3339, body.CreatedAt)
	updatedTime, err2 := time.Parse(time.RFC3339, body.UpdatedAt)
	if err1 != nil || err2 != nil {
		t.Fatalf("failed to parse timestamps: created=%v updated=%v", err1, err2)
	}
	if !updatedTime.After(createdTime) && !updatedTime.Equal(createdTime) {
		t.Errorf("expected updated_at (%v) >= created_at (%v)", updatedTime, createdTime)
	}
}

// TestBlockOrg_Idempotent verifies that POST /orgs/:id/block on an
// already-blocked organization is idempotent: returns HTTP 200, status
// remains 'blocked', and updated_at does not change between the two calls.
//
// Test Spec: TS-08-32
// Requirement: 08-REQ-7.2
func TestBlockOrg_Idempotent(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	orgID := "d0000003-0000-4000-8000-000000000003"
	insertTestOrg(t, sqlDB, orgID, "Idempotent Corp", "idempotent-corp", "", "active")

	// First block call.
	firstRec := sendPost(t, e, "/orgs/"+orgID+"/block")
	if firstRec.Code != http.StatusOK {
		t.Fatalf("first block: expected HTTP 200, got %d; body: %s", firstRec.Code, firstRec.Body.String())
	}
	firstBody := parseOrgResponse(t, firstRec)

	if firstBody.Status != "blocked" {
		t.Fatalf("first block: expected status %q, got %q", "blocked", firstBody.Status)
	}

	// Second block call (already blocked).
	secondRec := sendPost(t, e, "/orgs/"+orgID+"/block")
	if secondRec.Code != http.StatusOK {
		t.Fatalf("second block: expected HTTP 200, got %d; body: %s", secondRec.Code, secondRec.Body.String())
	}
	secondBody := parseOrgResponse(t, secondRec)

	if secondBody.Status != "blocked" {
		t.Errorf("second block: expected status %q, got %q", "blocked", secondBody.Status)
	}
	if secondBody.UpdatedAt != firstBody.UpdatedAt {
		t.Errorf("expected updated_at to be unchanged between calls: first=%q second=%q",
			firstBody.UpdatedAt, secondBody.UpdatedAt)
	}
}

// TestBlockOrg_NotFound verifies that POST /orgs/:id/block for a
// non-existent organization UUID returns HTTP 404 with error message
// 'organization not found'.
//
// Test Spec: TS-08-33
// Requirement: 08-REQ-7.3
func TestBlockOrg_NotFound(t *testing.T) {
	e, _ := setupOrgAdminTestServer(t)

	nonExistentUUID := "00000000-0000-0000-0000-000000000000"
	rec := sendPost(t, e, "/orgs/"+nonExistentUUID+"/block")

	assertErrorResponse(t, rec, http.StatusNotFound, "organization not found")
}

// TestBlockOrg_InvalidID verifies that POST /orgs/:id/block with an invalid
// UUID path parameter returns HTTP 400 with error message
// 'invalid organization id'.
//
// Test Spec: TS-08-34
// Requirement: 08-REQ-7.4
func TestBlockOrg_InvalidID(t *testing.T) {
	e, _ := setupOrgAdminTestServer(t)

	rec := sendPost(t, e, "/orgs/not-a-uuid/block")

	assertErrorResponse(t, rec, http.StatusBadRequest, "invalid organization id")
}

// TestBlockOrg_NonAdmin verifies that POST /orgs/:id/block from a non-admin
// user returns HTTP 403 with error message 'forbidden'.
//
// Test Spec: TS-08-35
// Requirement: 08-REQ-7.5
func TestBlockOrg_NonAdmin(t *testing.T) {
	e, sqlDB := setupOrgNonAdminTestServer(t)

	orgID := "d0000004-0000-4000-8000-000000000004"
	insertTestOrg(t, sqlDB, orgID, "NonAdmin Block Corp", "nonadmin-block-corp", "", "active")

	rec := sendPost(t, e, "/orgs/"+orgID+"/block")

	assertErrorResponse(t, rec, http.StatusForbidden, "forbidden")
}

// TestBlockOrg_DBError verifies that POST /orgs/:id/block returns HTTP 500
// with error message 'internal server error' when the DB UPDATE fails
// with a database error. Uses a SQLite BEFORE UPDATE trigger to simulate
// the failure while allowing the initial org lookup SELECT to succeed.
//
// Test Spec: TS-08-E11
// Requirement: 08-REQ-7.E1
func TestBlockOrg_DBError(t *testing.T) {
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	e := echo.New()
	g := e.Group("", apikit.CacheMiddleware(apikit.CacheNoStore))
	g.Use(adminAuthMiddleware("test-admin-uuid"))
	handlers.RegisterOrgHandlers(g, database.SqlDB)

	orgID := "d0000005-0000-4000-8000-000000000005"
	insertTestOrg(t, database.SqlDB, orgID, "DBError Block Corp", "dberror-block-corp", "", "active")

	// Install a BEFORE UPDATE trigger that raises a generic DB error.
	// The org lookup (SELECT) succeeds, but the UPDATE statement fails.
	_, err = database.SqlDB.Exec(`
		CREATE TRIGGER fail_orgs_block_update BEFORE UPDATE ON orgs
		BEGIN SELECT RAISE(FAIL, 'simulated db error'); END
	`)
	if err != nil {
		t.Fatalf("failed to create trigger: %v", err)
	}

	rec := sendPost(t, e, "/orgs/"+orgID+"/block")

	assertErrorResponse(t, rec, http.StatusInternalServerError, "internal server error")
}

// ========================================================================
// Task 5.2: TestUnblockOrg — POST /orgs/:id/unblock handler
// Test Spec: TS-08-36, TS-08-37, TS-08-38, TS-08-39, TS-08-40, TS-08-E12
// Requirements: 08-REQ-8.1, 08-REQ-8.2, 08-REQ-8.3, 08-REQ-8.4,
//               08-REQ-8.5, 08-REQ-8.E1
// ========================================================================

// TestUnblockOrg_Success verifies that POST /orgs/:id/unblock on a blocked
// organization returns HTTP 200 with status='active' and an updated_at
// value that is >= the blocked updated_at.
//
// Test Spec: TS-08-36
// Requirement: 08-REQ-8.1
func TestUnblockOrg_Success(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	orgID := "e0000001-0000-4000-8000-000000000001"
	insertTestOrg(t, sqlDB, orgID, "Unblock Corp", "unblock-corp", "", "blocked")

	// Retrieve the blocked org to compare updated_at.
	blockedRec := sendGet(t, e, "/orgs/"+orgID)
	if blockedRec.Code != http.StatusOK {
		t.Fatalf("setup: expected HTTP 200 on GET, got %d; body: %s", blockedRec.Code, blockedRec.Body.String())
	}
	blocked := parseOrgResponse(t, blockedRec)

	rec := sendPost(t, e, "/orgs/"+orgID+"/unblock")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	body := parseOrgResponse(t, rec)

	if body.Status != "active" {
		t.Errorf("expected status %q, got %q", "active", body.Status)
	}

	blockedTime, err1 := time.Parse(time.RFC3339, blocked.UpdatedAt)
	updTime, err2 := time.Parse(time.RFC3339, body.UpdatedAt)
	if err1 != nil || err2 != nil {
		t.Fatalf("failed to parse updated_at timestamps: blocked=%v upd=%v", err1, err2)
	}
	if updTime.Before(blockedTime) {
		t.Errorf("expected updated_at %v >= blocked updated_at %v", updTime, blockedTime)
	}
}

// TestUnblockOrg_Idempotent verifies that POST /orgs/:id/unblock on an
// already-active organization is idempotent: returns HTTP 200, status
// remains 'active', and updated_at is unchanged from before the call.
//
// Test Spec: TS-08-37
// Requirement: 08-REQ-8.2
func TestUnblockOrg_Idempotent(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	orgID := "e0000002-0000-4000-8000-000000000002"
	insertTestOrg(t, sqlDB, orgID, "Idempotent Unblock Corp", "idempotent-unblock-corp", "", "active")

	// Retrieve original active org to capture its updated_at.
	origRec := sendGet(t, e, "/orgs/"+orgID)
	if origRec.Code != http.StatusOK {
		t.Fatalf("setup: expected HTTP 200 on GET, got %d; body: %s", origRec.Code, origRec.Body.String())
	}
	original := parseOrgResponse(t, origRec)

	// Unblock an already-active org.
	rec := sendPost(t, e, "/orgs/"+orgID+"/unblock")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	body := parseOrgResponse(t, rec)

	if body.Status != "active" {
		t.Errorf("expected status %q, got %q", "active", body.Status)
	}
	if body.UpdatedAt != original.UpdatedAt {
		t.Errorf("expected updated_at to be unchanged: original=%q unblocked=%q",
			original.UpdatedAt, body.UpdatedAt)
	}
}

// TestUnblockOrg_NotFound verifies that POST /orgs/:id/unblock for a
// non-existent organization UUID returns HTTP 404 with error message
// 'organization not found'.
//
// Test Spec: TS-08-38
// Requirement: 08-REQ-8.3
func TestUnblockOrg_NotFound(t *testing.T) {
	e, _ := setupOrgAdminTestServer(t)

	nonExistentUUID := "00000000-0000-0000-0000-000000000000"
	rec := sendPost(t, e, "/orgs/"+nonExistentUUID+"/unblock")

	assertErrorResponse(t, rec, http.StatusNotFound, "organization not found")
}

// TestUnblockOrg_InvalidID verifies that POST /orgs/:id/unblock with an
// invalid UUID path parameter returns HTTP 400 with error message
// 'invalid organization id'.
//
// Test Spec: TS-08-39
// Requirement: 08-REQ-8.4
func TestUnblockOrg_InvalidID(t *testing.T) {
	e, _ := setupOrgAdminTestServer(t)

	rec := sendPost(t, e, "/orgs/not-a-uuid/unblock")

	assertErrorResponse(t, rec, http.StatusBadRequest, "invalid organization id")
}

// TestUnblockOrg_NonAdmin verifies that POST /orgs/:id/unblock from a
// non-admin user returns HTTP 403 with error message 'forbidden'.
//
// Test Spec: TS-08-40
// Requirement: 08-REQ-8.5
func TestUnblockOrg_NonAdmin(t *testing.T) {
	e, sqlDB := setupOrgNonAdminTestServer(t)

	orgID := "e0000003-0000-4000-8000-000000000003"
	insertTestOrg(t, sqlDB, orgID, "NonAdmin Unblock Corp", "nonadmin-unblock-corp", "", "blocked")

	rec := sendPost(t, e, "/orgs/"+orgID+"/unblock")

	assertErrorResponse(t, rec, http.StatusForbidden, "forbidden")
}

// TestUnblockOrg_DBError verifies that POST /orgs/:id/unblock returns
// HTTP 500 with error message 'internal server error' when the DB UPDATE
// fails with a database error. Uses a SQLite BEFORE UPDATE trigger to
// simulate the failure while allowing the initial org lookup SELECT to succeed.
//
// Test Spec: TS-08-E12
// Requirement: 08-REQ-8.E1
func TestUnblockOrg_DBError(t *testing.T) {
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	e := echo.New()
	g := e.Group("", apikit.CacheMiddleware(apikit.CacheNoStore))
	g.Use(adminAuthMiddleware("test-admin-uuid"))
	handlers.RegisterOrgHandlers(g, database.SqlDB)

	orgID := "e0000004-0000-4000-8000-000000000004"
	insertTestOrg(t, database.SqlDB, orgID, "DBError Unblock Corp", "dberror-unblock-corp", "", "blocked")

	// Install a BEFORE UPDATE trigger that raises a generic DB error.
	// The org lookup (SELECT) succeeds, but the UPDATE statement fails.
	_, err = database.SqlDB.Exec(`
		CREATE TRIGGER fail_orgs_unblock_update BEFORE UPDATE ON orgs
		BEGIN SELECT RAISE(FAIL, 'simulated db error'); END
	`)
	if err != nil {
		t.Fatalf("failed to create trigger: %v", err)
	}

	rec := sendPost(t, e, "/orgs/"+orgID+"/unblock")

	assertErrorResponse(t, rec, http.StatusInternalServerError, "internal server error")
}

// ========================================================================
// Task 6 Helpers
// ========================================================================

// sendPut sends an HTTP PUT request with no body to the given Echo instance
// and returns the response recorder.
func sendPut(t *testing.T, e *echo.Echo, path string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPut, path, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	return rec
}

// parseMembersResponse parses the response body as a JSON array of
// OrgMemberResponse objects.
func parseMembersResponse(t *testing.T, rec *httptest.ResponseRecorder) []handlers.OrgMemberResponse {
	t.Helper()

	var members []handlers.OrgMemberResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &members); err != nil {
		t.Fatalf("failed to parse members list response: %v\nbody: %s", err, rec.Body.String())
	}
	return members
}

// insertTestUserWithRole inserts a user directly into the users table with
// the specified role for test setup.
func insertTestUserWithRole(t *testing.T, sqlDB *sql.DB, id, username, email, provider, providerID, role string) {
	t.Helper()
	if _, err := uuid.Parse(id); err != nil {
		id = testUUID(id)
	}

	now := "2024-01-01T00:00:00Z"
	_, err := sqlDB.Exec(
		`INSERT INTO users (id, username, email, full_name, role, status, provider, provider_id, created_at, updated_at)
		 VALUES (?, ?, ?, '', ?, 'active', ?, ?, ?, ?)`,
		id, username, email, role, provider, providerID, now, now,
	)
	if err != nil {
		t.Fatalf("failed to insert test user with role: %v", err)
	}
}

// setupOrgAuthTestServer creates an Echo instance with RegisterOrgHandlers
// registered on a group with a middleware that returns HTTP 401 when no
// Authorization header is provided. This simulates the real auth middleware
// behavior for testing that all org endpoints require authentication.
func setupOrgAuthTestServer(t *testing.T) (*echo.Echo, *sql.DB) {
	t.Helper()

	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	e := echo.New()
	g := e.Group("", apikit.CacheMiddleware(apikit.CacheNoStore))
	// Simulate auth middleware that rejects unauthenticated requests with 401.
	g.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			authHeader := c.Request().Header.Get("Authorization")
			if authHeader == "" {
				return apikit.WriteAPIError(c, http.StatusUnauthorized, "missing authorization header")
			}
			return next(c)
		}
	})
	handlers.RegisterOrgHandlers(g, database.SqlDB)

	return e, database.SqlDB
}

// ========================================================================
// Task 6.1: TestListMembers — GET /orgs/:id/members handler
// Test Spec: TS-08-41, TS-08-42, TS-08-43, TS-08-44, TS-08-45, TS-08-46,
//            TS-08-E13
// Requirements: 08-REQ-9.1, 08-REQ-9.2, 08-REQ-9.3, 08-REQ-9.4,
//               08-REQ-9.5, 08-REQ-9.6, 08-REQ-9.E1
// ========================================================================

// TestListMembers_AsAdmin verifies that GET /orgs/:id/members from an admin
// with a valid org UUID returns HTTP 200 with all member details ordered
// alphabetically by username. First element is 'alice', second is 'bob'.
// Each member includes user_id, username, email, role, and joined_at fields.
//
// Test Spec: TS-08-41
// Requirement: 08-REQ-9.1
func TestListMembers_AsAdmin(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	orgID := "f0000001-0000-4000-8000-000000000001"
	aliceID := "f1000001-0000-4000-8000-000000000001"
	bobID := "f1000002-0000-4000-8000-000000000002"

	// Insert org and two users (alice and bob).
	insertTestOrg(t, sqlDB, orgID, "Members Org", "members-org", "", "active")
	insertTestUserWithRole(t, sqlDB, aliceID, "alice", "alice@example.com", "github", "gh-alice", "user")
	insertTestUserWithRole(t, sqlDB, bobID, "bob", "bob@example.com", "github", "gh-bob", "admin")

	// Add both as members.
	insertTestOrgMember(t, sqlDB, orgID, aliceID)
	insertTestOrgMember(t, sqlDB, orgID, bobID)

	rec := sendGet(t, e, "/orgs/"+orgID+"/members")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	members := parseMembersResponse(t, rec)

	if len(members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(members))
	}

	// Verify ordering: alice first, bob second (alphabetical by username).
	if members[0].Username != "alice" {
		t.Errorf("expected first member username %q, got %q", "alice", members[0].Username)
	}
	if members[1].Username != "bob" {
		t.Errorf("expected second member username %q, got %q", "bob", members[1].Username)
	}

	// Verify all required fields are present for each member.
	for _, m := range members {
		if m.UserID == "" {
			t.Error("expected user_id to be non-empty")
		}
		if m.Username == "" {
			t.Error("expected username to be non-empty")
		}
		if m.Email == "" {
			t.Error("expected email to be non-empty")
		}
		if m.Role != "admin" && m.Role != "user" {
			t.Errorf("expected role to be 'admin' or 'user', got %q", m.Role)
		}
		if !isRFC3339(m.CreatedAt) {
			t.Errorf("expected created_at (joined_at) to be RFC 3339 UTC, got %q", m.CreatedAt)
		}
	}
}

// TestListMembers_AsMember verifies that GET /orgs/:id/members from a
// non-admin user who is a member of the org returns HTTP 200 with a JSON
// array of OrgMemberResponse objects.
//
// Test Spec: TS-08-42
// Requirement: 08-REQ-9.2
func TestListMembers_AsMember(t *testing.T) {
	memberUserID := "f1000003-0000-4000-8000-000000000003"
	e, sqlDB := setupOrgNonAdminTestServerWithUserID(t, memberUserID)

	orgID := "f0000002-0000-4000-8000-000000000002"

	// Insert user, org, and membership.
	insertTestUser(t, sqlDB, memberUserID, "member", "member@example.com", "github", "gh-member")
	insertTestOrg(t, sqlDB, orgID, "Member List Org", "member-list-org", "", "active")
	insertTestOrgMember(t, sqlDB, orgID, memberUserID)

	rec := sendGet(t, e, "/orgs/"+orgID+"/members")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	members := parseMembersResponse(t, rec)

	if len(members) == 0 {
		t.Error("expected at least one member in response")
	}
}

// TestListMembers_NotMember verifies that GET /orgs/:id/members from a
// non-admin user who is NOT a member of the org returns HTTP 403 with
// error message 'forbidden'.
//
// Test Spec: TS-08-43
// Requirement: 08-REQ-9.3
func TestListMembers_NotMember(t *testing.T) {
	nonMemberUserID := "f1000004-0000-4000-8000-000000000004"
	e, sqlDB := setupOrgNonAdminTestServerWithUserID(t, nonMemberUserID)

	orgID := "f0000003-0000-4000-8000-000000000003"

	// Insert user and org but NO membership row.
	insertTestUser(t, sqlDB, nonMemberUserID, "outsider", "outsider@example.com", "github", "gh-outsider")
	insertTestOrg(t, sqlDB, orgID, "NonMember List Org", "nonmember-list-org", "", "active")

	rec := sendGet(t, e, "/orgs/"+orgID+"/members")

	assertErrorResponse(t, rec, http.StatusForbidden, "forbidden")
}

// TestListMembers_OrgNotFound verifies that GET /orgs/:id/members for a
// non-existent organization UUID returns HTTP 404 with error message
// 'organization not found'.
//
// Test Spec: TS-08-44
// Requirement: 08-REQ-9.4
func TestListMembers_OrgNotFound(t *testing.T) {
	e, _ := setupOrgAdminTestServer(t)

	nonExistentUUID := "00000000-0000-0000-0000-000000000000"
	rec := sendGet(t, e, "/orgs/"+nonExistentUUID+"/members")

	assertErrorResponse(t, rec, http.StatusNotFound, "organization not found")
}

// TestListMembers_Empty verifies that GET /orgs/:id/members for an org
// with no members returns HTTP 200 with an empty JSON array [].
//
// Test Spec: TS-08-45
// Requirement: 08-REQ-9.5
func TestListMembers_Empty(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	orgID := "f0000004-0000-4000-8000-000000000004"
	insertTestOrg(t, sqlDB, orgID, "Empty Members Org", "empty-members-org", "", "active")

	rec := sendGet(t, e, "/orgs/"+orgID+"/members")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// The response must be exactly "[]", not "null" or empty.
	body := strings.TrimSpace(rec.Body.String())
	if body != "[]" {
		t.Errorf("expected response body to be '[]', got %q", body)
	}
}

// TestListMembers_InvalidID verifies that GET /orgs/:id/members with an
// invalid UUID path parameter returns HTTP 400 with error message
// 'invalid organization id'.
//
// Test Spec: TS-08-46
// Requirement: 08-REQ-9.6
func TestListMembers_InvalidID(t *testing.T) {
	e, _ := setupOrgAdminTestServer(t)

	rec := sendGet(t, e, "/orgs/not-a-uuid/members")

	assertErrorResponse(t, rec, http.StatusBadRequest, "invalid organization id")
}

// TestListMembers_IncludesUserDetails verifies that each member in the
// GET /orgs/:id/members response includes the user_id, username, email,
// role, and joined_at (created_at) fields with correct values.
//
// Test Spec: TS-08-41 (detail aspect)
// Requirement: 08-REQ-9.1
func TestListMembers_IncludesUserDetails(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	orgID := "f0000005-0000-4000-8000-000000000005"
	userID := "f1000005-0000-4000-8000-000000000005"

	insertTestOrg(t, sqlDB, orgID, "Details Org", "details-org", "", "active")
	insertTestUserWithRole(t, sqlDB, userID, "detailuser", "detail@example.com", "github", "gh-detail", "admin")
	insertTestOrgMember(t, sqlDB, orgID, userID)

	rec := sendGet(t, e, "/orgs/"+orgID+"/members")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	members := parseMembersResponse(t, rec)

	if len(members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(members))
	}

	m := members[0]
	if m.UserID != userID {
		t.Errorf("expected user_id %q, got %q", userID, m.UserID)
	}
	if m.Username != "detailuser" {
		t.Errorf("expected username %q, got %q", "detailuser", m.Username)
	}
	if m.Email != "detail@example.com" {
		t.Errorf("expected email %q, got %q", "detail@example.com", m.Email)
	}
	if m.Role != "admin" {
		t.Errorf("expected role %q, got %q", "admin", m.Role)
	}
	if !isRFC3339(m.CreatedAt) {
		t.Errorf("expected created_at to be RFC 3339 UTC, got %q", m.CreatedAt)
	}
}

// TestListMembers_DBError verifies that GET /orgs/:id/members returns
// HTTP 500 with error message 'internal server error' when the org
// lookup succeeds but the org_members JOIN users query fails.
//
// Test Spec: TS-08-E13
// Requirement: 08-REQ-9.E1
func TestListMembers_DBError(t *testing.T) {
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	orgID := "f0000006-0000-4000-8000-000000000006"

	// Insert org while database is intact.
	insertTestOrg(t, database.SqlDB, orgID, "DBErr Members Org", "dberr-members-org", "", "active")

	// Drop the org_members table so the join query fails while org lookup succeeds.
	_, err = database.SqlDB.Exec("DROP TABLE org_members")
	if err != nil {
		t.Fatalf("failed to drop org_members table: %v", err)
	}

	e := echo.New()
	g := e.Group("", apikit.CacheMiddleware(apikit.CacheNoStore))
	g.Use(adminAuthMiddleware("test-admin-uuid"))
	handlers.RegisterOrgHandlers(g, database.SqlDB)

	rec := sendGet(t, e, "/orgs/"+orgID+"/members")

	assertErrorResponse(t, rec, http.StatusInternalServerError, "internal server error")
}

// ========================================================================
// Task 6.2: TestAddMember — PUT /orgs/:id/members/:user_id handler
// Test Spec: TS-08-47, TS-08-48, TS-08-49, TS-08-50, TS-08-51, TS-08-52,
//            TS-08-53, TS-08-E14
// Requirements: 08-REQ-10.1, 08-REQ-10.2, 08-REQ-10.3, 08-REQ-10.4,
//               08-REQ-10.5, 08-REQ-10.6, 08-REQ-10.7, 08-REQ-10.E1
// ========================================================================

// TestAddMember_Success verifies that PUT /orgs/:id/members/:user_id from
// an admin adds the user to the org and returns HTTP 204 with no body.
// A row with (org_id, user_id) must exist in org_members with a valid
// created_at timestamp.
//
// Test Spec: TS-08-47
// Requirement: 08-REQ-10.1
func TestAddMember_Success(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	orgID := "f0000007-0000-4000-8000-000000000007"
	userID := "f1000006-0000-4000-8000-000000000006"

	insertTestOrg(t, sqlDB, orgID, "Add Member Org", "add-member-org", "", "active")
	insertTestUser(t, sqlDB, userID, "newmember", "new@example.com", "github", "gh-new")

	rec := sendPut(t, e, "/orgs/"+orgID+"/members/"+userID)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected HTTP 204, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Body must be empty.
	if rec.Body.Len() != 0 {
		t.Errorf("expected empty body for 204 response, got %q", rec.Body.String())
	}

	// Verify row exists in org_members.
	var count int
	err := sqlDB.QueryRow(
		"SELECT COUNT(*) FROM org_members WHERE org_id = ? AND user_id = ?",
		orgID, userID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query org_members: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row in org_members for (%s, %s), got %d", orgID, userID, count)
	}
}

// TestAddMember_Idempotent verifies that PUT /orgs/:id/members/:user_id
// for an already-existing member is idempotent: returns HTTP 204 both times,
// and exactly one row exists in org_members.
//
// Test Spec: TS-08-48
// Requirement: 08-REQ-10.2
func TestAddMember_Idempotent(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	orgID := "f0000008-0000-4000-8000-000000000008"
	userID := "f1000007-0000-4000-8000-000000000007"

	insertTestOrg(t, sqlDB, orgID, "Idempotent Org", "idem-member-org", "", "active")
	insertTestUser(t, sqlDB, userID, "idemuser", "idem@example.com", "github", "gh-idem")

	// First PUT: add the member.
	firstRec := sendPut(t, e, "/orgs/"+orgID+"/members/"+userID)
	if firstRec.Code != http.StatusNoContent {
		t.Fatalf("first PUT: expected HTTP 204, got %d; body: %s", firstRec.Code, firstRec.Body.String())
	}

	// Second PUT: same member, should be idempotent.
	secondRec := sendPut(t, e, "/orgs/"+orgID+"/members/"+userID)
	if secondRec.Code != http.StatusNoContent {
		t.Fatalf("second PUT: expected HTTP 204, got %d; body: %s", secondRec.Code, secondRec.Body.String())
	}

	// Verify exactly one row in org_members.
	var count int
	err := sqlDB.QueryRow(
		"SELECT COUNT(*) FROM org_members WHERE org_id = ? AND user_id = ?",
		orgID, userID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query org_members: %v", err)
	}
	if count != 1 {
		t.Errorf("expected exactly 1 row in org_members after two PUTs, got %d", count)
	}
}

// TestAddMember_OrgNotFound verifies that PUT /orgs/:id/members/:user_id
// with a non-existent org UUID returns HTTP 404 with error message
// 'organization not found'.
//
// Test Spec: TS-08-49
// Requirement: 08-REQ-10.3
func TestAddMember_OrgNotFound(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	userID := "f1000008-0000-4000-8000-000000000008"
	insertTestUser(t, sqlDB, userID, "existinguser", "exist@example.com", "github", "gh-exist")

	nonExistentOrgUUID := "00000000-0000-0000-0000-000000000000"
	rec := sendPut(t, e, "/orgs/"+nonExistentOrgUUID+"/members/"+userID)

	assertErrorResponse(t, rec, http.StatusNotFound, "organization not found")
}

// TestAddMember_UserNotFound verifies that PUT /orgs/:id/members/:user_id
// with a non-existent user UUID returns HTTP 404 with error message
// 'user not found'.
//
// Test Spec: TS-08-50
// Requirement: 08-REQ-10.4
func TestAddMember_UserNotFound(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	orgID := "f0000009-0000-4000-8000-000000000009"
	insertTestOrg(t, sqlDB, orgID, "UserNotFound Org", "usernotfound-org", "", "active")

	nonExistentUserUUID := "00000000-0000-0000-0000-000000000001"
	rec := sendPut(t, e, "/orgs/"+orgID+"/members/"+nonExistentUserUUID)

	assertErrorResponse(t, rec, http.StatusNotFound, "user not found")
}

// TestAddMember_InvalidOrgID verifies that PUT /orgs/:id/members/:user_id
// with an invalid UUID for the org path parameter returns HTTP 400 with
// error message 'invalid organization id'.
//
// Test Spec: TS-08-51
// Requirement: 08-REQ-10.5
func TestAddMember_InvalidOrgID(t *testing.T) {
	e, _ := setupOrgAdminTestServer(t)

	validUserUUID := "00000000-0000-0000-0000-000000000001"
	rec := sendPut(t, e, "/orgs/not-a-uuid/members/"+validUserUUID)

	assertErrorResponse(t, rec, http.StatusBadRequest, "invalid organization id")
}

// TestAddMember_InvalidUserID verifies that PUT /orgs/:id/members/:user_id
// with an invalid UUID for the user_id path parameter returns HTTP 400 with
// error message 'invalid user id'.
//
// Test Spec: TS-08-52
// Requirement: 08-REQ-10.6
func TestAddMember_InvalidUserID(t *testing.T) {
	e, _ := setupOrgAdminTestServer(t)

	validOrgUUID := "00000000-0000-0000-0000-000000000001"
	rec := sendPut(t, e, "/orgs/"+validOrgUUID+"/members/not-a-uuid")

	assertErrorResponse(t, rec, http.StatusBadRequest, "invalid user id")
}

// TestAddMember_NonAdmin verifies that PUT /orgs/:id/members/:user_id from
// a non-admin user returns HTTP 403 with error message 'forbidden'.
//
// Test Spec: TS-08-53
// Requirement: 08-REQ-10.7
func TestAddMember_NonAdmin(t *testing.T) {
	e, sqlDB := setupOrgNonAdminTestServer(t)

	orgID := "f000000a-0000-4000-8000-00000000000a"
	userID := "f100000a-0000-4000-8000-00000000000a"

	insertTestOrg(t, sqlDB, orgID, "NonAdmin Add Org", "nonadmin-add-org", "", "active")
	insertTestUser(t, sqlDB, userID, "nonadminadd", "nonadminadd@example.com", "github", "gh-naa")

	rec := sendPut(t, e, "/orgs/"+orgID+"/members/"+userID)

	assertErrorResponse(t, rec, http.StatusForbidden, "forbidden")
}

// TestAddMember_DBError verifies that PUT /orgs/:id/members/:user_id
// returns HTTP 500 with error message 'internal server error' when the
// org and user lookups succeed but the INSERT into org_members fails with
// a generic database error.
//
// Test Spec: TS-08-E14
// Requirement: 08-REQ-10.E1
func TestAddMember_DBError(t *testing.T) {
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	orgID := "f000000b-0000-4000-8000-00000000000b"
	userID := "f100000b-0000-4000-8000-00000000000b"

	// Insert org and user while database is intact.
	insertTestOrg(t, database.SqlDB, orgID, "DBErr Add Org", "dberr-add-org", "", "active")
	insertTestUser(t, database.SqlDB, userID, "dberradder", "dberr@example.com", "github", "gh-dba")

	// Install a BEFORE INSERT trigger on org_members that raises a generic error.
	// The org and user lookups (SELECTs) succeed, but the INSERT fails.
	_, err = database.SqlDB.Exec(`
		CREATE TRIGGER fail_org_members_insert BEFORE INSERT ON org_members
		BEGIN SELECT RAISE(FAIL, 'simulated db error'); END
	`)
	if err != nil {
		t.Fatalf("failed to create trigger: %v", err)
	}

	e := echo.New()
	g := e.Group("", apikit.CacheMiddleware(apikit.CacheNoStore))
	g.Use(adminAuthMiddleware("test-admin-uuid"))
	handlers.RegisterOrgHandlers(g, database.SqlDB)

	rec := sendPut(t, e, "/orgs/"+orgID+"/members/"+userID)

	assertErrorResponse(t, rec, http.StatusInternalServerError, "internal server error")
}

// ========================================================================
// Task 6.3: TestRemoveMember — DELETE /orgs/:id/members/:user_id handler
// Test Spec: TS-08-54, TS-08-55, TS-08-56, TS-08-57, TS-08-58, TS-08-E15
// Requirements: 08-REQ-11.1, 08-REQ-11.2, 08-REQ-11.3, 08-REQ-11.4,
//               08-REQ-11.5, 08-REQ-11.E1
// ========================================================================

// TestRemoveMember_Success verifies that DELETE /orgs/:id/members/:user_id
// from an admin removes the membership and returns HTTP 204 with no body.
// The org_members row is removed but the user row in users remains.
//
// Test Spec: TS-08-54
// Requirement: 08-REQ-11.1
func TestRemoveMember_Success(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	orgID := "f000000c-0000-4000-8000-00000000000c"
	userID := "f100000c-0000-4000-8000-00000000000c"

	insertTestOrg(t, sqlDB, orgID, "Remove Member Org", "remove-member-org", "", "active")
	insertTestUser(t, sqlDB, userID, "removable", "removable@example.com", "github", "gh-rem")
	insertTestOrgMember(t, sqlDB, orgID, userID)

	rec := sendDelete(t, e, "/orgs/"+orgID+"/members/"+userID)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected HTTP 204, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Body must be empty.
	if rec.Body.Len() != 0 {
		t.Errorf("expected empty body for 204 response, got %q", rec.Body.String())
	}

	// Verify org_members row is removed.
	var memberCount int
	err := sqlDB.QueryRow(
		"SELECT COUNT(*) FROM org_members WHERE org_id = ? AND user_id = ?",
		orgID, userID,
	).Scan(&memberCount)
	if err != nil {
		t.Fatalf("failed to query org_members: %v", err)
	}
	if memberCount != 0 {
		t.Errorf("expected org_members row to be removed, found %d rows", memberCount)
	}

	// Verify user row still exists.
	var userCount int
	err = sqlDB.QueryRow("SELECT COUNT(*) FROM users WHERE id = ?", userID).Scan(&userCount)
	if err != nil {
		t.Fatalf("failed to query users: %v", err)
	}
	if userCount != 1 {
		t.Errorf("expected user row to be preserved, found %d rows", userCount)
	}
}

// TestRemoveMember_NotFound verifies that DELETE /orgs/:id/members/:user_id
// when no matching org_members row exists returns HTTP 404 with error
// message 'membership not found'.
//
// Test Spec: TS-08-55
// Requirement: 08-REQ-11.2
func TestRemoveMember_NotFound(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	orgID := "f000000d-0000-4000-8000-00000000000d"
	userID := "f100000d-0000-4000-8000-00000000000d"

	// Insert org and user but NO membership row.
	insertTestOrg(t, sqlDB, orgID, "NoMember Org", "nomember-org", "", "active")
	insertTestUser(t, sqlDB, userID, "nomember", "nomember@example.com", "github", "gh-nm")

	rec := sendDelete(t, e, "/orgs/"+orgID+"/members/"+userID)

	assertErrorResponse(t, rec, http.StatusNotFound, "membership not found")
}

// TestRemoveMember_InvalidOrgID verifies that DELETE /orgs/:id/members/:user_id
// with an invalid UUID for the org path parameter returns HTTP 400 with
// error message 'invalid organization id'.
//
// Test Spec: TS-08-56
// Requirement: 08-REQ-11.3
func TestRemoveMember_InvalidOrgID(t *testing.T) {
	e, _ := setupOrgAdminTestServer(t)

	validUserUUID := "00000000-0000-0000-0000-000000000001"
	rec := sendDelete(t, e, "/orgs/not-a-uuid/members/"+validUserUUID)

	assertErrorResponse(t, rec, http.StatusBadRequest, "invalid organization id")
}

// TestRemoveMember_InvalidUserID verifies that DELETE /orgs/:id/members/:user_id
// with an invalid UUID for the user_id path parameter returns HTTP 400 with
// error message 'invalid user id'.
//
// Test Spec: TS-08-57
// Requirement: 08-REQ-11.4
func TestRemoveMember_InvalidUserID(t *testing.T) {
	e, _ := setupOrgAdminTestServer(t)

	validOrgUUID := "00000000-0000-0000-0000-000000000001"
	rec := sendDelete(t, e, "/orgs/"+validOrgUUID+"/members/not-a-uuid")

	assertErrorResponse(t, rec, http.StatusBadRequest, "invalid user id")
}

// TestRemoveMember_NonAdmin verifies that DELETE /orgs/:id/members/:user_id
// from a non-admin user returns HTTP 403 with error message 'forbidden'.
//
// Test Spec: TS-08-58
// Requirement: 08-REQ-11.5
func TestRemoveMember_NonAdmin(t *testing.T) {
	e, sqlDB := setupOrgNonAdminTestServer(t)

	orgID := "f000000e-0000-4000-8000-00000000000e"
	userID := "f100000e-0000-4000-8000-00000000000e"

	insertTestOrg(t, sqlDB, orgID, "NonAdmin Remove Org", "nonadmin-remove-org", "", "active")
	insertTestUser(t, sqlDB, userID, "nonadminrem", "nonadminrem@example.com", "github", "gh-nar")
	insertTestOrgMember(t, sqlDB, orgID, userID)

	rec := sendDelete(t, e, "/orgs/"+orgID+"/members/"+userID)

	assertErrorResponse(t, rec, http.StatusForbidden, "forbidden")
}

// TestRemoveMember_DBError verifies that DELETE /orgs/:id/members/:user_id
// returns HTTP 500 with error message 'internal server error' when the
// DELETE on org_members fails with a database error.
//
// Test Spec: TS-08-E15
// Requirement: 08-REQ-11.E1
func TestRemoveMember_DBError(t *testing.T) {
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	orgID := "f000000f-0000-4000-8000-00000000000f"
	userID := "f100000f-0000-4000-8000-00000000000f"

	// Insert org, user, and membership while database is intact.
	insertTestOrg(t, database.SqlDB, orgID, "DBErr Remove Org", "dberr-remove-org", "", "active")
	insertTestUser(t, database.SqlDB, userID, "dberrremove", "dberrremove@example.com", "github", "gh-dbr")
	insertTestOrgMember(t, database.SqlDB, orgID, userID)

	// Install a BEFORE DELETE trigger on org_members that raises a generic error.
	_, err = database.SqlDB.Exec(`
		CREATE TRIGGER fail_org_members_delete BEFORE DELETE ON org_members
		BEGIN SELECT RAISE(FAIL, 'simulated db error'); END
	`)
	if err != nil {
		t.Fatalf("failed to create trigger: %v", err)
	}

	e := echo.New()
	g := e.Group("", apikit.CacheMiddleware(apikit.CacheNoStore))
	g.Use(adminAuthMiddleware("test-admin-uuid"))
	handlers.RegisterOrgHandlers(g, database.SqlDB)

	rec := sendDelete(t, e, "/orgs/"+orgID+"/members/"+userID)

	assertErrorResponse(t, rec, http.StatusInternalServerError, "internal server error")
}

// ========================================================================
// Task 6.3: TestOrgAllEndpointsRequireAuth — auth requirement
// Test Spec: TS-08-66
// Requirement: 08-REQ-14.1
// ========================================================================

// TestOrgAllEndpointsRequireAuth verifies that all 10 org endpoint routes
// return HTTP 401 when accessed without a valid authentication credential.
//
// Test Spec: TS-08-66
// Requirement: 08-REQ-14.1
func TestOrgAllEndpointsRequireAuth(t *testing.T) {
	e, _ := setupOrgAuthTestServer(t)

	validUUID := "00000000-0000-0000-0000-000000000001"

	// All 10 org endpoints.
	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/orgs"},
		{http.MethodGet, "/orgs"},
		{http.MethodGet, "/orgs/" + validUUID},
		{http.MethodPatch, "/orgs/" + validUUID},
		{http.MethodDelete, "/orgs/" + validUUID},
		{http.MethodPost, "/orgs/" + validUUID + "/block"},
		{http.MethodPost, "/orgs/" + validUUID + "/unblock"},
		{http.MethodGet, "/orgs/" + validUUID + "/members"},
		{http.MethodPut, "/orgs/" + validUUID + "/members/" + validUUID},
		{http.MethodDelete, "/orgs/" + validUUID + "/members/" + validUUID},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+"_"+ep.path, func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.path, nil)
			// No Authorization header — this is the key condition.
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("%s %s: expected HTTP 401, got %d; body: %s",
					ep.method, ep.path, rec.Code, rec.Body.String())
			}
		})
	}
}

// ========================================================================
// Task 13: Integration Tests
// ========================================================================

// ========================================================================
// Task 13.1: TestOrgLifecycle — full org lifecycle integration
// Test Spec: TS-08-SMOKE-5
// Execution Path: 08-PATH-5
// Requirements: 08-REQ-2.1, 08-REQ-4.1, 08-REQ-5.1, 08-REQ-6.1
// ========================================================================

// TestOrgLifecycle verifies the full organization lifecycle end-to-end:
// POST /orgs → PUT member → PATCH /orgs/:id (name update) →
// GET /orgs/:id (verify new name and updated_at > created_at) →
// DELETE /orgs/:id (204) → GET /orgs/:id (404).
// Verifies slug immutability across PATCH, updated_at increments, and
// org absence after delete.
func TestOrgLifecycle(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	// --- Step 1: Create an organization via POST /orgs ---
	createBody := `{"name":"Lifecycle Corp","slug":"lifecycle-corp","url":"https://lifecycle.example.com"}`
	createRec := sendJSON(t, e, http.MethodPost, "/orgs", createBody)

	if createRec.Code != http.StatusCreated {
		t.Fatalf("POST /orgs: expected HTTP 201, got %d; body: %s", createRec.Code, createRec.Body.String())
	}

	created := parseOrgResponse(t, createRec)

	if !isUUID(created.ID) {
		t.Errorf("expected id to be a valid UUID, got %q", created.ID)
	}
	if created.Status != "active" {
		t.Errorf("expected status %q, got %q", "active", created.Status)
	}
	if created.CreatedAt != created.UpdatedAt {
		t.Errorf("expected created_at (%q) to equal updated_at (%q)", created.CreatedAt, created.UpdatedAt)
	}
	originalSlug := created.Slug

	orgID := created.ID

	// --- Step 2: Add a member (requires a user in the DB) ---
	memberUserID := "a0000010-0000-4000-8000-000000000010"
	insertTestUser(t, sqlDB, memberUserID, "lifecycleuser", "lifecycle@example.com", "github", "gh-lc")

	memberRec := sendPut(t, e, "/orgs/"+orgID+"/members/"+memberUserID)
	if memberRec.Code != http.StatusNoContent {
		t.Fatalf("PUT member: expected HTTP 204, got %d; body: %s", memberRec.Code, memberRec.Body.String())
	}

	// --- Step 3: Update name via PATCH /orgs/:id ---
	patchBody := `{"name":"Lifecycle Corp Updated"}`
	patchRec := sendJSON(t, e, http.MethodPatch, "/orgs/"+orgID, patchBody)

	if patchRec.Code != http.StatusOK {
		t.Fatalf("PATCH /orgs/:id: expected HTTP 200, got %d; body: %s", patchRec.Code, patchRec.Body.String())
	}

	patched := parseOrgResponse(t, patchRec)

	if patched.Name != "Lifecycle Corp Updated" {
		t.Errorf("expected name %q, got %q", "Lifecycle Corp Updated", patched.Name)
	}
	// Slug must be immutable (08-PROP-1).
	if patched.Slug != originalSlug {
		t.Errorf("expected slug to remain %q, got %q", originalSlug, patched.Slug)
	}
	// updated_at must be >= created_at (08-PROP-2).
	createdTime, err1 := time.Parse(time.RFC3339, created.CreatedAt)
	patchedTime, err2 := time.Parse(time.RFC3339, patched.UpdatedAt)
	if err1 != nil || err2 != nil {
		t.Fatalf("failed to parse timestamps: %v / %v", err1, err2)
	}
	if patchedTime.Before(createdTime) {
		t.Errorf("expected updated_at (%v) >= created_at (%v)", patchedTime, createdTime)
	}

	// --- Step 4: GET /orgs/:id — verify updated state ---
	getRec := sendGet(t, e, "/orgs/"+orgID)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET /orgs/:id: expected HTTP 200, got %d; body: %s", getRec.Code, getRec.Body.String())
	}

	fetched := parseOrgResponse(t, getRec)
	if fetched.Name != "Lifecycle Corp Updated" {
		t.Errorf("expected name %q after PATCH, got %q", "Lifecycle Corp Updated", fetched.Name)
	}

	// --- Step 5: DELETE /orgs/:id ---
	deleteRec := sendDelete(t, e, "/orgs/"+orgID)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /orgs/:id: expected HTTP 204, got %d; body: %s", deleteRec.Code, deleteRec.Body.String())
	}

	// --- Step 6: GET /orgs/:id after delete — expect 404 ---
	getAfterDeleteRec := sendGet(t, e, "/orgs/"+orgID)
	assertErrorResponse(t, getAfterDeleteRec, http.StatusNotFound, "organization not found")

	// --- Verify cascade: no rows in org_members for the deleted org ---
	var memberCount int
	err := sqlDB.QueryRow("SELECT COUNT(*) FROM org_members WHERE org_id = ?", orgID).Scan(&memberCount)
	if err != nil {
		t.Fatalf("failed to query org_members: %v", err)
	}
	if memberCount != 0 {
		t.Errorf("expected 0 org_members rows after cascade delete, got %d", memberCount)
	}

	// --- Verify user is preserved ---
	var userCount int
	err = sqlDB.QueryRow("SELECT COUNT(*) FROM users WHERE id = ?", memberUserID).Scan(&userCount)
	if err != nil {
		t.Fatalf("failed to query users: %v", err)
	}
	if userCount != 1 {
		t.Errorf("expected user row to be preserved, found %d", userCount)
	}
}

// ========================================================================
// Task 13.1: TestOrgBlockUnblockCycle — block/unblock with listing behavior
// Test Spec: TS-08-SMOKE-3, TS-08-SMOKE-6
// Execution Path: 08-PATH-3, 08-PATH-6
// Requirements: 08-REQ-3.1, 08-REQ-3.2, 08-REQ-7.1, 08-REQ-8.1
// ========================================================================

// TestOrgBlockUnblockCycle verifies the complete block/unblock lifecycle:
// create org → GET /orgs (visible) → POST /block → GET /orgs (absent) →
// GET /orgs?include_blocked=true (present) → POST /unblock →
// GET /orgs (visible again).
func TestOrgBlockUnblockCycle(t *testing.T) {
	e, _ := setupOrgAdminTestServer(t)

	// --- Step 1: Create an organization ---
	createBody := `{"name":"BlockCycle Corp","slug":"blockcycle-corp"}`
	createRec := sendJSON(t, e, http.MethodPost, "/orgs", createBody)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("POST /orgs: expected HTTP 201, got %d; body: %s", createRec.Code, createRec.Body.String())
	}
	created := parseOrgResponse(t, createRec)
	orgID := created.ID

	// --- Step 2: GET /orgs — org should be visible ---
	listRec1 := sendGet(t, e, "/orgs")
	if listRec1.Code != http.StatusOK {
		t.Fatalf("GET /orgs (before block): expected HTTP 200, got %d", listRec1.Code)
	}
	orgs1 := parseOrgsResponse(t, listRec1)
	if !containsOrgID(orgs1, orgID) {
		t.Error("expected org to be visible in GET /orgs before blocking")
	}

	// --- Step 3: POST /orgs/:id/block ---
	blockRec := sendPost(t, e, "/orgs/"+orgID+"/block")
	if blockRec.Code != http.StatusOK {
		t.Fatalf("POST /block: expected HTTP 200, got %d; body: %s", blockRec.Code, blockRec.Body.String())
	}
	blocked := parseOrgResponse(t, blockRec)
	if blocked.Status != "blocked" {
		t.Errorf("expected status %q, got %q", "blocked", blocked.Status)
	}

	// --- Step 4: GET /orgs (default) — blocked org absent ---
	listRec2 := sendGet(t, e, "/orgs")
	if listRec2.Code != http.StatusOK {
		t.Fatalf("GET /orgs (after block): expected HTTP 200, got %d", listRec2.Code)
	}
	orgs2 := parseOrgsResponse(t, listRec2)
	if containsOrgID(orgs2, orgID) {
		t.Error("expected blocked org to be absent from GET /orgs without include_blocked")
	}

	// --- Step 5: GET /orgs?include_blocked=true — blocked org present ---
	listRec3 := sendGet(t, e, "/orgs?include_blocked=true")
	if listRec3.Code != http.StatusOK {
		t.Fatalf("GET /orgs?include_blocked=true: expected HTTP 200, got %d", listRec3.Code)
	}
	orgs3 := parseOrgsResponse(t, listRec3)
	if !containsOrgID(orgs3, orgID) {
		t.Error("expected blocked org to be present in GET /orgs?include_blocked=true")
	}

	// --- Step 6: POST /orgs/:id/unblock ---
	unblockRec := sendPost(t, e, "/orgs/"+orgID+"/unblock")
	if unblockRec.Code != http.StatusOK {
		t.Fatalf("POST /unblock: expected HTTP 200, got %d; body: %s", unblockRec.Code, unblockRec.Body.String())
	}
	unblocked := parseOrgResponse(t, unblockRec)
	if unblocked.Status != "active" {
		t.Errorf("expected status %q, got %q", "active", unblocked.Status)
	}

	// --- Step 7: GET /orgs — org visible again ---
	listRec4 := sendGet(t, e, "/orgs")
	if listRec4.Code != http.StatusOK {
		t.Fatalf("GET /orgs (after unblock): expected HTTP 200, got %d", listRec4.Code)
	}
	orgs4 := parseOrgsResponse(t, listRec4)
	if !containsOrgID(orgs4, orgID) {
		t.Error("expected unblocked org to be visible in GET /orgs after unblocking")
	}
}

// containsOrgID checks whether the orgs slice contains an org with the given ID.
func containsOrgID(orgs []handlers.OrgResponse, id string) bool {
	for _, org := range orgs {
		if org.ID == id {
			return true
		}
	}
	return false
}

// ========================================================================
// Task 13.2: TestOrgMembershipLifecycle — full membership lifecycle
// Test Spec: TS-08-SMOKE-4
// Execution Path: 08-PATH-4
// Requirements: 08-REQ-9.1, 08-REQ-10.1, 08-REQ-11.1
// ========================================================================

// TestOrgMembershipLifecycle verifies the full membership lifecycle:
// create org → PUT member → GET /members (verify user appears with all
// fields, ordered) → DELETE member → GET /members (verify user absent).
func TestOrgMembershipLifecycle(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	// --- Step 1: Create an organization ---
	createBody := `{"name":"Membership Corp","slug":"membership-corp"}`
	createRec := sendJSON(t, e, http.MethodPost, "/orgs", createBody)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("POST /orgs: expected HTTP 201, got %d; body: %s", createRec.Code, createRec.Body.String())
	}
	created := parseOrgResponse(t, createRec)
	orgID := created.ID

	// --- Step 2: Insert test users (UUIDs must be valid for addOrgMember validation) ---
	aliceID := "a0000020-0000-4000-8000-000000000020"
	bobID := "a0000021-0000-4000-8000-000000000021"
	insertTestUserWithRole(t, sqlDB, aliceID, "alice", "alice@example.com", "github", "gh-alice-m", "user")
	insertTestUserWithRole(t, sqlDB, bobID, "bob", "bob@example.com", "github", "gh-bob-m", "admin")

	// --- Step 3: Add both users as members via PUT ---
	putAlice := sendPut(t, e, "/orgs/"+orgID+"/members/"+aliceID)
	if putAlice.Code != http.StatusNoContent {
		t.Fatalf("PUT alice: expected HTTP 204, got %d; body: %s", putAlice.Code, putAlice.Body.String())
	}

	putBob := sendPut(t, e, "/orgs/"+orgID+"/members/"+bobID)
	if putBob.Code != http.StatusNoContent {
		t.Fatalf("PUT bob: expected HTTP 204, got %d; body: %s", putBob.Code, putBob.Body.String())
	}

	// --- Step 4: GET /orgs/:id/members — verify both appear, ordered by username ---
	listRec := sendGet(t, e, "/orgs/"+orgID+"/members")
	if listRec.Code != http.StatusOK {
		t.Fatalf("GET /members: expected HTTP 200, got %d; body: %s", listRec.Code, listRec.Body.String())
	}

	members := parseMembersResponse(t, listRec)
	if len(members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(members))
	}

	// Ordered by username: alice, bob.
	if members[0].Username != "alice" {
		t.Errorf("expected first member 'alice', got %q", members[0].Username)
	}
	if members[1].Username != "bob" {
		t.Errorf("expected second member 'bob', got %q", members[1].Username)
	}

	// Verify all response fields.
	for _, m := range members {
		if m.UserID == "" {
			t.Error("expected user_id to be non-empty")
		}
		if m.Username == "" {
			t.Error("expected username to be non-empty")
		}
		if m.Email == "" {
			t.Error("expected email to be non-empty")
		}
		if m.Role == "" {
			t.Error("expected role to be non-empty")
		}
		if !isRFC3339(m.CreatedAt) {
			t.Errorf("expected created_at to be RFC 3339 UTC, got %q", m.CreatedAt)
		}
	}

	// --- Step 5: DELETE member (alice) ---
	delAlice := sendDelete(t, e, "/orgs/"+orgID+"/members/"+aliceID)
	if delAlice.Code != http.StatusNoContent {
		t.Fatalf("DELETE alice: expected HTTP 204, got %d; body: %s", delAlice.Code, delAlice.Body.String())
	}

	// --- Step 6: GET /members — alice absent, bob still present ---
	listRec2 := sendGet(t, e, "/orgs/"+orgID+"/members")
	if listRec2.Code != http.StatusOK {
		t.Fatalf("GET /members after delete: expected HTTP 200, got %d; body: %s", listRec2.Code, listRec2.Body.String())
	}

	members2 := parseMembersResponse(t, listRec2)
	if len(members2) != 1 {
		t.Fatalf("expected 1 member after removing alice, got %d", len(members2))
	}
	if members2[0].Username != "bob" {
		t.Errorf("expected remaining member 'bob', got %q", members2[0].Username)
	}
}

// ========================================================================
// Task 13.2: TestOrgDeleteCascade — cascade delete of memberships
// Test Spec: TS-08-SMOKE-5 (cascade aspect)
// Correctness Property: 08-PROP-4
// Requirements: 08-REQ-6.1
// ========================================================================

// TestOrgDeleteCascade verifies that deleting an organization cascade-deletes
// all org_members rows while preserving user rows.
func TestOrgDeleteCascade(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	// --- Step 1: Create org ---
	createBody := `{"name":"Cascade Corp","slug":"cascade-corp"}`
	createRec := sendJSON(t, e, http.MethodPost, "/orgs", createBody)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("POST /orgs: expected HTTP 201, got %d; body: %s", createRec.Code, createRec.Body.String())
	}
	created := parseOrgResponse(t, createRec)
	orgID := created.ID

	// --- Step 2: Insert 3 test users (UUIDs must be valid) ---
	userIDs := []string{
		"a0000030-0000-4000-8000-000000000030",
		"a0000031-0000-4000-8000-000000000031",
		"a0000032-0000-4000-8000-000000000032",
	}
	for i, uid := range userIDs {
		username := "cascadeuser" + strings.Repeat("x", i+1)
		insertTestUserWithRole(t, sqlDB, uid, username, username+"@example.com", "github", "gh-c"+uid[len(uid)-3:], "user")
	}

	// --- Step 3: Add all users as members ---
	for _, uid := range userIDs {
		putRec := sendPut(t, e, "/orgs/"+orgID+"/members/"+uid)
		if putRec.Code != http.StatusNoContent {
			t.Fatalf("PUT member %s: expected HTTP 204, got %d", uid, putRec.Code)
		}
	}

	// Verify 3 members exist.
	var memberCountBefore int
	err := sqlDB.QueryRow("SELECT COUNT(*) FROM org_members WHERE org_id = ?", orgID).Scan(&memberCountBefore)
	if err != nil {
		t.Fatalf("failed to count org_members: %v", err)
	}
	if memberCountBefore != 3 {
		t.Fatalf("expected 3 org_members rows before delete, got %d", memberCountBefore)
	}

	// --- Step 4: DELETE org ---
	deleteRec := sendDelete(t, e, "/orgs/"+orgID)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /orgs/:id: expected HTTP 204, got %d; body: %s", deleteRec.Code, deleteRec.Body.String())
	}

	// --- Step 5: Assert org_members count = 0 for org_id ---
	var memberCountAfter int
	err = sqlDB.QueryRow("SELECT COUNT(*) FROM org_members WHERE org_id = ?", orgID).Scan(&memberCountAfter)
	if err != nil {
		t.Fatalf("failed to count org_members after delete: %v", err)
	}
	if memberCountAfter != 0 {
		t.Errorf("expected 0 org_members rows after cascade delete, got %d", memberCountAfter)
	}

	// --- Step 6: Assert all user rows still exist ---
	for _, uid := range userIDs {
		var userExists int
		err = sqlDB.QueryRow("SELECT COUNT(*) FROM users WHERE id = ?", uid).Scan(&userExists)
		if err != nil {
			t.Fatalf("failed to query user %s: %v", uid, err)
		}
		if userExists != 1 {
			t.Errorf("expected user %s to be preserved after org deletion, found %d rows", uid, userExists)
		}
	}
}

// ========================================================================
// Task 13.2: TestOrgMemberAccess — member vs non-member access control
// Test Spec: TS-08-SMOKE-2
// Execution Path: 08-PATH-2
// Requirements: 08-REQ-4.2, 08-REQ-4.3, 08-REQ-9.2, 08-REQ-9.3
// ========================================================================

// TestOrgMemberAccess verifies that org members can view their org and
// member list, while non-members cannot.
func TestOrgMemberAccess(t *testing.T) {
	memberUserID := testUUID("access-member-uuid-001")
	nonMemberUserID := testUUID("access-nonmember-uuid-002")

	// --- Set up admin server to create the org and add member ---
	adminDB := setupOrgAdminTestServerShared(t)
	orgID := "a1000001-0000-4000-8000-000000000001"

	insertTestOrg(t, adminDB, orgID, "Access Org", "access-org", "", "active")
	insertTestUser(t, adminDB, memberUserID, "accessmember", "member@access.com", "github", "gh-am")
	insertTestUser(t, adminDB, nonMemberUserID, "accessnonmember", "nonmember@access.com", "github", "gh-anm")
	insertTestOrgMember(t, adminDB, orgID, memberUserID)

	// --- Set up member's Echo server (non-admin, shared DB) ---
	eMember := echo.New()
	gMember := eMember.Group("", apikit.CacheMiddleware(apikit.CacheNoStore))
	gMember.Use(nonAdminAuthMiddleware(memberUserID))
	handlers.RegisterOrgHandlers(gMember, adminDB)

	// --- Set up non-member's Echo server (non-admin, shared DB) ---
	eNonMember := echo.New()
	gNonMember := eNonMember.Group("", apikit.CacheMiddleware(apikit.CacheNoStore))
	gNonMember.Use(nonAdminAuthMiddleware(nonMemberUserID))
	handlers.RegisterOrgHandlers(gNonMember, adminDB)

	// --- Member can GET /orgs/:id → 200 ---
	memberGetOrg := sendGet(t, eMember, "/orgs/"+orgID)
	if memberGetOrg.Code != http.StatusOK {
		t.Errorf("member GET /orgs/:id: expected HTTP 200, got %d; body: %s",
			memberGetOrg.Code, memberGetOrg.Body.String())
	}

	// ETag header must be present.
	etag := memberGetOrg.Header().Get("ETag")
	if etag == "" {
		t.Error("expected ETag header to be set on member GET /orgs/:id")
	}

	// --- Non-member cannot GET /orgs/:id → 403 ---
	nonMemberGetOrg := sendGet(t, eNonMember, "/orgs/"+orgID)
	assertErrorResponse(t, nonMemberGetOrg, http.StatusForbidden, "forbidden")

	// --- Member can GET /orgs/:id/members → 200 ---
	memberGetMembers := sendGet(t, eMember, "/orgs/"+orgID+"/members")
	if memberGetMembers.Code != http.StatusOK {
		t.Errorf("member GET /orgs/:id/members: expected HTTP 200, got %d; body: %s",
			memberGetMembers.Code, memberGetMembers.Body.String())
	}

	// --- Non-member cannot GET /orgs/:id/members → 403 ---
	nonMemberGetMembers := sendGet(t, eNonMember, "/orgs/"+orgID+"/members")
	assertErrorResponse(t, nonMemberGetMembers, http.StatusForbidden, "forbidden")

	// --- Conditional GET: member sends If-None-Match with matching ETag → 304 ---
	conditionalRec := sendGetWithHeaders(t, eMember, "/orgs/"+orgID, map[string]string{
		"If-None-Match": etag,
	})
	if conditionalRec.Code != http.StatusNotModified {
		t.Errorf("conditional GET with matching ETag: expected HTTP 304, got %d", conditionalRec.Code)
	}
}

// setupOrgAdminTestServerShared creates an in-memory DB with schema and returns
// the raw *sql.DB handle for use in tests that need to share a DB across
// multiple Echo instances.
func setupOrgAdminTestServerShared(t *testing.T) *sql.DB {
	t.Helper()

	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	return database.SqlDB
}

// ========================================================================
// Task 13.3: TestOrgAdminEndpointsRequireAdmin — admin access enforcement
// Test Spec: TS-08-SMOKE-1 (admin aspect)
// Requirements: 08-REQ-2.7, 08-REQ-3.4, 08-REQ-5.7, 08-REQ-6.4,
//               08-REQ-7.5, 08-REQ-8.5, 08-REQ-10.7, 08-REQ-11.5
// ========================================================================

// TestOrgAdminEndpointsRequireAdmin verifies that all admin-only org
// endpoints return HTTP 403 for non-admin users.
func TestOrgAdminEndpointsRequireAdmin(t *testing.T) {
	e, sqlDB := setupOrgNonAdminTestServer(t)

	// Set up prerequisite data so endpoints don't fail for other reasons.
	orgID := "a2000001-0000-4000-8000-000000000001"
	userID := "a2000001-0000-4000-8000-100000000001"
	insertTestOrg(t, sqlDB, orgID, "Admin Test Org", "admin-test-org", "", "active")
	insertTestUser(t, sqlDB, userID, "admintest", "admintest@example.com", "github", "gh-at")

	// All 7 admin-only endpoints.
	adminEndpoints := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/orgs"},
		{http.MethodGet, "/orgs"},
		{http.MethodPatch, "/orgs/" + orgID},
		{http.MethodDelete, "/orgs/" + orgID},
		{http.MethodPost, "/orgs/" + orgID + "/block"},
		{http.MethodPost, "/orgs/" + orgID + "/unblock"},
		{http.MethodPut, "/orgs/" + orgID + "/members/" + userID},
		{http.MethodDelete, "/orgs/" + orgID + "/members/" + userID},
	}

	for _, ep := range adminEndpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			var rec *httptest.ResponseRecorder
			switch ep.method {
			case http.MethodPost:
				if ep.path == "/orgs" {
					rec = sendJSON(t, e, http.MethodPost, ep.path, `{"name":"X","slug":"x"}`)
				} else {
					rec = sendPost(t, e, ep.path)
				}
			case http.MethodGet:
				rec = sendGet(t, e, ep.path)
			case http.MethodPatch:
				rec = sendJSON(t, e, http.MethodPatch, ep.path, `{"name":"X"}`)
			case http.MethodDelete:
				rec = sendDelete(t, e, ep.path)
			case http.MethodPut:
				rec = sendPut(t, e, ep.path)
			}

			if rec.Code != http.StatusForbidden {
				t.Errorf("%s %s: expected HTTP 403, got %d; body: %s",
					ep.method, ep.path, rec.Code, rec.Body.String())
			}
		})
	}
}

// ========================================================================
// Task 13.3: TestOrgCacheHeaders — Cache-Control on all endpoints
// Test Spec: TS-08-SMOKE-1 (cache aspect)
// Correctness Property: 08-PROP-10
// Requirements: 08-REQ-1.2
// ========================================================================

// TestOrgCacheHeaders verifies that all 10 org endpoints return
// Cache-Control: no-store in their response headers.
func TestOrgCacheHeaders(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	// Pre-insert data so all endpoints get past validation.
	orgID := "a3000001-0000-4000-8000-000000000001"
	userID := "a3000001-0000-4000-8000-100000000001"
	insertTestOrg(t, sqlDB, orgID, "Cache Org", "cache-org", "", "active")
	insertTestUser(t, sqlDB, userID, "cacheuser", "cache@example.com", "github", "gh-cache")
	insertTestOrgMember(t, sqlDB, orgID, userID)

	// All 10 endpoints with methods that produce non-404/405 responses.
	endpoints := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"POST /orgs", http.MethodPost, "/orgs", `{"name":"New Cache Org","slug":"new-cache-org"}`},
		{"GET /orgs", http.MethodGet, "/orgs", ""},
		{"GET /orgs/:id", http.MethodGet, "/orgs/" + orgID, ""},
		{"PATCH /orgs/:id", http.MethodPatch, "/orgs/" + orgID, `{"name":"Cache Org Updated"}`},
		{"POST /orgs/:id/block", http.MethodPost, "/orgs/" + orgID + "/block", ""},
		{"POST /orgs/:id/unblock", http.MethodPost, "/orgs/" + orgID + "/unblock", ""},
		{"GET /orgs/:id/members", http.MethodGet, "/orgs/" + orgID + "/members", ""},
		{"PUT /orgs/:id/members/:user_id", http.MethodPut, "/orgs/" + orgID + "/members/" + userID, ""},
		{"DELETE /orgs/:id/members/:user_id", http.MethodDelete, "/orgs/" + orgID + "/members/" + userID, ""},
		{"DELETE /orgs/:id", http.MethodDelete, "/orgs/" + orgID, ""},
	}

	for _, ep := range endpoints {
		t.Run(ep.name, func(t *testing.T) {
			var req *http.Request
			if ep.body != "" {
				req = httptest.NewRequest(ep.method, ep.path, strings.NewReader(ep.body))
				req.Header.Set("Content-Type", "application/json")
			} else {
				req = httptest.NewRequest(ep.method, ep.path, nil)
			}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			// Skip 404/405 responses which indicate an unregistered route, not a handler response.
			if rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed {
				t.Fatalf("%s returned %d — route not registered", ep.name, rec.Code)
			}

			cc := rec.Header().Get("Cache-Control")
			if cc != "no-store" {
				t.Errorf("%s: expected Cache-Control %q, got %q", ep.name, "no-store", cc)
			}
		})
	}
}

// ========================================================================
// Task 13.3: TestOrgConditionalGet — ETag-based conditional GET
// Test Spec: TS-08-SMOKE-2 (conditional GET aspect)
// Requirements: 08-REQ-4.6
// ========================================================================

// TestOrgConditionalGet verifies ETag-based conditional GET behavior:
// create org (with old timestamp) → GET (capture ETag) → update org →
// GET with old ETag (200, not 304, since ETag changed) →
// GET with new ETag → 304.
func TestOrgConditionalGet(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	// --- Step 1: Insert org directly with an old timestamp ---
	// We insert directly (not via POST) so the initial created_at/updated_at
	// is in the past, ensuring the PATCH's NowUTC() produces a different ETag.
	orgID := "a4000001-0000-4000-8000-000000000001"
	oldTimestamp := "2020-01-01T00:00:00Z"
	_, err := sqlDB.Exec(
		`INSERT INTO orgs (id, name, slug, url, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		orgID, "ETag Corp", "etag-corp", "", "active", oldTimestamp, oldTimestamp,
	)
	if err != nil {
		t.Fatalf("failed to insert org: %v", err)
	}

	// --- Step 2: GET to capture the initial ETag ---
	getRec1 := sendGet(t, e, "/orgs/"+orgID)
	if getRec1.Code != http.StatusOK {
		t.Fatalf("GET (initial): expected HTTP 200, got %d", getRec1.Code)
	}
	oldETag := getRec1.Header().Get("ETag")
	if oldETag == "" {
		t.Fatal("expected ETag header on initial GET")
	}

	// --- Step 3: Update org to change its updated_at (and thus ETag) ---
	patchBody := `{"name":"ETag Corp Updated"}`
	patchRec := sendJSON(t, e, http.MethodPatch, "/orgs/"+orgID, patchBody)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("PATCH: expected HTTP 200, got %d; body: %s", patchRec.Code, patchRec.Body.String())
	}

	// --- Step 4: GET with old ETag → expect 200 (ETag changed) ---
	getRec2 := sendGetWithHeaders(t, e, "/orgs/"+orgID, map[string]string{
		"If-None-Match": oldETag,
	})
	if getRec2.Code != http.StatusOK {
		t.Errorf("GET with stale ETag: expected HTTP 200, got %d", getRec2.Code)
	}
	newETag := getRec2.Header().Get("ETag")
	if newETag == "" {
		t.Fatal("expected ETag header after update GET")
	}
	if newETag == oldETag {
		t.Error("expected new ETag to differ from old ETag after update")
	}

	// --- Step 5: GET with new ETag → expect 304 ---
	getRec3 := sendGetWithHeaders(t, e, "/orgs/"+orgID, map[string]string{
		"If-None-Match": newETag,
	})
	if getRec3.Code != http.StatusNotModified {
		t.Errorf("GET with current ETag: expected HTTP 304, got %d", getRec3.Code)
	}
	if getRec3.Body.Len() != 0 {
		t.Errorf("expected empty body for 304 response, got %q", getRec3.Body.String())
	}
}

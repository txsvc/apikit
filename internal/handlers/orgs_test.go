package handlers_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	userID := "user-uuid-001"

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
	g.Use(nonAdminAuthMiddleware("non-admin-user-uuid"))
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

	orgID := "get-org-uuid-1"
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
	memberUserID := "member-user-uuid-1"
	e, sqlDB := setupOrgNonAdminTestServerWithUserID(t, memberUserID)

	orgID := "member-org-uuid-1"

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
	nonMemberUserID := "non-member-user-uuid-1"
	e, sqlDB := setupOrgNonAdminTestServerWithUserID(t, nonMemberUserID)

	orgID := "forbidden-org-uuid-1"

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

	orgID := "etag-org-uuid-1"
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
	nonAdminUserID := "dberr-member-user-uuid"

	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	orgID := "dberr-org-uuid-1"

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

	orgID := "update-name-org-uuid"
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

	orgID := "update-url-org-uuid"
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

	orgID := "update-both-org-uuid"
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

	orgID := "slug-ignore-org-uuid"
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

	orgID := "empty-body-org-uuid"
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

	targetOrgID := "dup-name-target-uuid"
	insertTestOrg(t, sqlDB, targetOrgID, "Target Corp", "target-corp", "", "active")
	insertTestOrg(t, sqlDB, "dup-name-taken-uuid", "Taken Name Corp", "taken-name-corp", "", "active")

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

	orgID := "nonadmin-update-org-uuid"
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

	orgID := "emptyname-update-org-uuid"
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

	orgID := "dberror-update-org-uuid"
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

	orgID := "delete-org-uuid"
	userID := "delete-user-uuid"

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

	orgID := "cascade-org-uuid"
	user1ID := "cascade-user-uuid-1"
	user2ID := "cascade-user-uuid-2"

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

	orgID := "preserve-org-uuid"
	userID := "preserve-user-uuid"

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

	orgID := "nonadmin-delete-org-uuid"
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

	orgID := "dberror-delete-org-uuid"
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

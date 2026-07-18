package handlers_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

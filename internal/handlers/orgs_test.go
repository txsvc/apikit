package handlers_test

import (
	"database/sql"
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

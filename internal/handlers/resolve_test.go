package handlers_test

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/txsvc/apikit/internal/db"
	"github.com/txsvc/apikit/internal/handlers"
)

// ========================================================================
// Test Helpers
// ========================================================================

// setupResolveTestDB opens an in-memory SQLite database with full schema
// and returns the raw *sql.DB handle. The database is closed automatically
// when the test completes.
func setupResolveTestDB(t *testing.T) *sql.DB {
	t.Helper()

	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	return database.SqlDB
}

// ========================================================================
// Task 1.1: resolveUserID unit tests — UUID and email selectors
// Test Spec: TS-16-1, TS-16-2, TS-16-4
// Requirements: 16-REQ-1, 16-REQ-11
// ========================================================================

// TestResolveUserID_UUID verifies that resolveUserID returns the UUID
// unchanged when called with a valid UUID that exists in the users table.
// This exercises the first heuristic path: UUID detection via uuid.Parse.
//
// Test Spec: TS-16-1
// Requirement: 16-REQ-1.1
func TestResolveUserID_UUID(t *testing.T) {
	sqlDB := setupResolveTestDB(t)

	const (
		userID   = "550e8400-e29b-41d4-a716-446655440000"
		username = "alice"
		email    = "alice@example.com"
	)
	insertTestUser(t, sqlDB, userID, username, email, "github", "gh-001")

	result, err := handlers.ResolveUserID(sqlDB, userID)
	if err != nil {
		t.Fatalf("resolveUserID(%q) returned unexpected error: %v", userID, err)
	}
	if result != userID {
		t.Errorf("resolveUserID(%q) = %q; want %q", userID, result, userID)
	}
}

// TestResolveUserID_Email verifies that resolveUserID queries by the email
// column and returns the matching user's UUID when the selector contains '@'.
// This exercises the second heuristic path: email detection via '@'.
//
// Test Spec: TS-16-2
// Requirement: 16-REQ-1.2
func TestResolveUserID_Email(t *testing.T) {
	sqlDB := setupResolveTestDB(t)

	const (
		userID   = "550e8400-e29b-41d4-a716-446655440000"
		username = "alice"
		email    = "alice@example.com"
	)
	insertTestUser(t, sqlDB, userID, username, email, "github", "gh-001")

	result, err := handlers.ResolveUserID(sqlDB, email)
	if err != nil {
		t.Fatalf("resolveUserID(%q) returned unexpected error: %v", email, err)
	}
	if result != userID {
		t.Errorf("resolveUserID(%q) = %q; want %q", email, result, userID)
	}
}

// TestResolveUserID_UnknownEmail verifies that resolveUserID returns
// sql.ErrNoRows when the selector contains '@' but no user has that email.
// The resolver must not fall back to a username lookup.
//
// Test Spec: TS-16-33 (unknown email case)
// Requirement: 16-REQ-1.E3
func TestResolveUserID_UnknownEmail(t *testing.T) {
	sqlDB := setupResolveTestDB(t)

	// Seed a user so the DB is non-empty; the queried email does not match.
	insertTestUser(t, sqlDB, "550e8400-e29b-41d4-a716-446655440000",
		"alice", "alice@example.com", "github", "gh-001")

	result, err := handlers.ResolveUserID(sqlDB, "nobody@example.com")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("resolveUserID(unknown email) error = %v; want sql.ErrNoRows", err)
	}
	if result != "" {
		t.Errorf("resolveUserID(unknown email) = %q; want empty string", result)
	}
}

// ========================================================================
// Task 1.2: resolveUserID unit tests — fallback and not-found cases
// Test Spec: TS-16-3, TS-16-5, TS-16-32
// Requirements: 16-REQ-1, 16-REQ-11
// ========================================================================

// TestResolveUserID_Username verifies that resolveUserID queries by the
// username column when the selector is a plain string (no '@', not a UUID).
// This exercises the third heuristic path: username fallback.
//
// Test Spec: TS-16-3
// Requirement: 16-REQ-1.3
func TestResolveUserID_Username(t *testing.T) {
	sqlDB := setupResolveTestDB(t)

	const (
		userID   = "550e8400-e29b-41d4-a716-446655440000"
		username = "alice"
		email    = "alice@example.com"
	)
	insertTestUser(t, sqlDB, userID, username, email, "github", "gh-001")

	result, err := handlers.ResolveUserID(sqlDB, username)
	if err != nil {
		t.Fatalf("resolveUserID(%q) returned unexpected error: %v", username, err)
	}
	if result != userID {
		t.Errorf("resolveUserID(%q) = %q; want %q", username, result, userID)
	}
}

// TestResolveUserID_UnknownUsername verifies that resolveUserID returns
// sql.ErrNoRows when the selector is a plain string that matches no
// username in the users table.
//
// Test Spec: TS-16-4, TS-16-33 (unknown username case)
// Requirement: 16-REQ-1.E4
func TestResolveUserID_UnknownUsername(t *testing.T) {
	sqlDB := setupResolveTestDB(t)

	// No users inserted — the table is empty.
	result, err := handlers.ResolveUserID(sqlDB, "nonexistent-user")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("resolveUserID(unknown username) error = %v; want sql.ErrNoRows", err)
	}
	if result != "" {
		t.Errorf("resolveUserID(unknown username) = %q; want empty string", result)
	}
}

// TestResolveUserID_ValidUUIDNotInDB verifies that resolveUserID returns
// sql.ErrNoRows when the selector is a valid UUID string that does not
// correspond to any row in the users table.
//
// Test Spec: TS-16-33 (valid UUID not in DB case)
// Requirement: 16-REQ-1.E2
func TestResolveUserID_ValidUUIDNotInDB(t *testing.T) {
	sqlDB := setupResolveTestDB(t)

	// No users inserted — the UUID will not match any row.
	const missingUUID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	result, err := handlers.ResolveUserID(sqlDB, missingUUID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("resolveUserID(valid UUID not in DB) error = %v; want sql.ErrNoRows", err)
	}
	if result != "" {
		t.Errorf("resolveUserID(valid UUID not in DB) = %q; want empty string", result)
	}
}

// TestResolveUserID_DatabaseError verifies that resolveUserID returns the
// underlying database error (not sql.ErrNoRows) when the database query
// fails for a reason other than "no rows."
//
// This test drops the users table after opening the database, so the
// subsequent query fails with a "no such table" error.
//
// Test Spec: TS-16-5
// Requirement: 16-REQ-1.5
func TestResolveUserID_DatabaseError(t *testing.T) {
	sqlDB := setupResolveTestDB(t)

	// Drop the users table so that any query against it produces a real
	// database error (not sql.ErrNoRows).
	if _, err := sqlDB.Exec("DROP TABLE users"); err != nil {
		t.Fatalf("failed to drop users table: %v", err)
	}

	result, err := handlers.ResolveUserID(sqlDB, "alice")
	if err == nil {
		t.Fatal("resolveUserID on broken DB returned nil error; want non-nil")
	}
	if errors.Is(err, sql.ErrNoRows) {
		t.Errorf("resolveUserID on broken DB returned sql.ErrNoRows; want a different database error")
	}
	if result != "" {
		t.Errorf("resolveUserID on broken DB = %q; want empty string", result)
	}
}

// ========================================================================
// Task 1.3: resolveOrgID unit tests — UUID, slug, and not-found cases
// Test Spec: TS-16-6, TS-16-7, TS-16-8, TS-16-9, TS-16-33
// Requirements: 16-REQ-2, 16-REQ-11
// ========================================================================

// TestResolveOrgID_UUID verifies that resolveOrgID returns the UUID
// unchanged when called with a valid UUID that exists in the orgs table.
// This exercises the first heuristic path: UUID detection via uuid.Parse.
//
// Test Spec: TS-16-6
// Requirement: 16-REQ-2.1
func TestResolveOrgID_UUID(t *testing.T) {
	sqlDB := setupResolveTestDB(t)

	const (
		orgID = "660e8400-e29b-41d4-a716-446655440001"
		slug  = "acme-corp"
	)
	insertTestOrg(t, sqlDB, orgID, "Acme Corp", slug, "https://acme.example.com", "active")

	result, err := handlers.ResolveOrgID(sqlDB, orgID)
	if err != nil {
		t.Fatalf("resolveOrgID(%q) returned unexpected error: %v", orgID, err)
	}
	if result != orgID {
		t.Errorf("resolveOrgID(%q) = %q; want %q", orgID, result, orgID)
	}
}

// TestResolveOrgID_Slug verifies that resolveOrgID queries by the slug
// column and returns the matching org's UUID when the selector is not a UUID.
// This exercises the second heuristic path: slug fallback.
//
// Test Spec: TS-16-7
// Requirement: 16-REQ-2.2
func TestResolveOrgID_Slug(t *testing.T) {
	sqlDB := setupResolveTestDB(t)

	const (
		orgID = "660e8400-e29b-41d4-a716-446655440001"
		slug  = "acme-corp"
	)
	insertTestOrg(t, sqlDB, orgID, "Acme Corp", slug, "https://acme.example.com", "active")

	result, err := handlers.ResolveOrgID(sqlDB, slug)
	if err != nil {
		t.Fatalf("resolveOrgID(%q) returned unexpected error: %v", slug, err)
	}
	if result != orgID {
		t.Errorf("resolveOrgID(%q) = %q; want %q", slug, result, orgID)
	}
}

// TestResolveOrgID_UnknownSlug verifies that resolveOrgID returns
// sql.ErrNoRows when the selector is a plain string that matches no slug
// in the orgs table.
//
// Test Spec: TS-16-8, TS-16-33 (unknown slug case)
// Requirement: 16-REQ-2.3
func TestResolveOrgID_UnknownSlug(t *testing.T) {
	sqlDB := setupResolveTestDB(t)

	// No orgs inserted — the table is empty.
	result, err := handlers.ResolveOrgID(sqlDB, "nonexistent-org")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("resolveOrgID(unknown slug) error = %v; want sql.ErrNoRows", err)
	}
	if result != "" {
		t.Errorf("resolveOrgID(unknown slug) = %q; want empty string", result)
	}
}

// TestResolveOrgID_DatabaseError verifies that resolveOrgID returns the
// underlying database error (not sql.ErrNoRows) when the database query
// fails for a reason other than "no rows."
//
// This test drops the orgs table after opening the database, so the
// subsequent query fails with a "no such table" error.
//
// Test Spec: TS-16-9
// Requirement: 16-REQ-2.4
func TestResolveOrgID_DatabaseError(t *testing.T) {
	sqlDB := setupResolveTestDB(t)

	// Drop the orgs table so that any query against it produces a real
	// database error (not sql.ErrNoRows).
	if _, err := sqlDB.Exec("DROP TABLE org_members"); err != nil {
		t.Fatalf("failed to drop org_members table: %v", err)
	}
	if _, err := sqlDB.Exec("DROP TABLE orgs"); err != nil {
		t.Fatalf("failed to drop orgs table: %v", err)
	}

	result, err := handlers.ResolveOrgID(sqlDB, "acme-corp")
	if err == nil {
		t.Fatal("resolveOrgID on broken DB returned nil error; want non-nil")
	}
	if errors.Is(err, sql.ErrNoRows) {
		t.Errorf("resolveOrgID on broken DB returned sql.ErrNoRows; want a different database error")
	}
	if result != "" {
		t.Errorf("resolveOrgID on broken DB = %q; want empty string", result)
	}
}

// ========================================================================
// Task 1.4: resolveOrgID — '@' selector treated as slug lookup
// Test Spec: TS-16-34
// Requirements: 16-REQ-2.E1, 16-REQ-11
// ========================================================================

// TestResolveOrgID_AtSignTreatedAsSlug verifies that resolveOrgID treats a
// selector containing '@' as a slug lookup rather than applying the email
// heuristic. Organizations do not have email addresses, so the '@' character
// must not trigger email-style lookup logic.
//
// This test calls resolveOrgID with 'weird@slug' (which contains '@') and
// expects sql.ErrNoRows because no org has slug='weird@slug'. The key
// invariant is that the org resolver queries the slug column, not an email
// column, for selectors containing '@'.
//
// Test Spec: TS-16-34
// Requirement: 16-REQ-11.3
func TestResolveOrgID_AtSignTreatedAsSlug(t *testing.T) {
	sqlDB := setupResolveTestDB(t)

	// No org with slug 'weird@slug' exists.
	result, err := handlers.ResolveOrgID(sqlDB, "weird@slug")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("resolveOrgID('weird@slug') error = %v; want sql.ErrNoRows", err)
	}
	if result != "" {
		t.Errorf("resolveOrgID('weird@slug') = %q; want empty string", result)
	}
}

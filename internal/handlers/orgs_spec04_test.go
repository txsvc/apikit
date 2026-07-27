package handlers_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// ========================================================================
// Spec 04 Task 2.1 (TS-04-E5): Edge case — JSON omits owner_id when NULL
// Spec 04 Task 2.2 (TS-04-14, TS-04-15): API response tests for owner_id
// Requirements: 04-REQ-4.3, 04-REQ-4.4, 04-REQ-4.E1
// ========================================================================

// insertTestOrgWithOwner inserts an organization with an explicit owner_id
// directly into the orgs table for test setup. If owner is empty, owner_id
// is set to NULL.
func insertTestOrgWithOwner(t *testing.T, db *sql.DB, id, name, slug, url, status, owner string) {
	t.Helper()

	now := "2024-01-01T00:00:00Z"
	var err error
	if owner == "" {
		_, err = db.Exec(
			`INSERT INTO orgs (id, name, slug, url, owner_id, status, created_at, updated_at)
			 VALUES (?, ?, ?, ?, NULL, ?, ?, ?)`,
			id, name, slug, url, status, now, now,
		)
	} else {
		_, err = db.Exec(
			`INSERT INTO orgs (id, name, slug, url, owner_id, status, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			id, name, slug, url, owner, status, now, now,
		)
	}
	if err != nil {
		t.Fatalf("failed to insert test org with owner_id: %v (hint: owner_id column may not exist yet in schema)", err)
	}
}

// TestPersonalOrg_OrgAPI_GetIncludesOwnerID verifies that GET /orgs/:id
// returns the owner_id field in the JSON response when the org has a
// non-NULL owner_id value set.
//
// Test Spec: TS-04-14
// Requirement: 04-REQ-4.3
func TestPersonalOrg_OrgAPI_GetIncludesOwnerID(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	// Insert an org with owner_id set.
	orgID := uuid.New().String()
	ownerUserID := "user-abc"
	insertTestOrgWithOwner(t, sqlDB, orgID, "Personal Org", "personal-org", "", "active", ownerUserID)

	// GET the org and verify owner_id is in the response.
	rec := sendGet(t, e, "/orgs/"+orgID)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Parse JSON response as raw map to check for owner_id key.
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}

	val, exists := raw["owner_id"]
	if !exists {
		t.Fatal("GET /orgs/:id response does not contain 'owner_id' key; expected owner_id when set")
	}
	if val != ownerUserID {
		t.Errorf("owner_id = %v; want %q", val, ownerUserID)
	}
}

// TestPersonalOrg_OrgAPI_ListIncludesOwnerID verifies that GET /orgs (list)
// includes the owner_id field in each org JSON object when the org has a
// non-NULL owner_id value.
//
// Test Spec: TS-04-14
// Requirement: 04-REQ-4.3
func TestPersonalOrg_OrgAPI_ListIncludesOwnerID(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	// Insert an org with owner_id set.
	orgID := uuid.New().String()
	ownerUserID := "user-abc"
	insertTestOrgWithOwner(t, sqlDB, orgID, "Owned Org", "owned-org", "", "active", ownerUserID)

	// GET /orgs (list) and verify owner_id appears in the response items.
	rec := sendGet(t, e, "/orgs")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var orgs []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &orgs); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}

	if len(orgs) == 0 {
		t.Fatal("GET /orgs returned empty array; expected at least one org")
	}

	// Find the org we inserted and check for owner_id.
	var found bool
	for _, org := range orgs {
		if org["id"] == orgID {
			found = true
			val, exists := org["owner_id"]
			if !exists {
				t.Error("list response org does not contain 'owner_id' key; expected owner_id when set")
			} else if val != ownerUserID {
				t.Errorf("list response owner_id = %v; want %q", val, ownerUserID)
			}
		}
	}
	if !found {
		t.Errorf("org with id %q not found in list response", orgID)
	}
}

// TestPersonalOrg_OrgAPI_UpdateIncludesOwnerID verifies that PATCH /orgs/:id
// returns the owner_id field in the updated org JSON response when the org
// has a non-NULL owner_id value.
//
// Test Spec: TS-04-14
// Requirement: 04-REQ-4.3
func TestPersonalOrg_OrgAPI_UpdateIncludesOwnerID(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	// Insert an org with owner_id set.
	orgID := uuid.New().String()
	ownerUserID := "user-abc"
	insertTestOrgWithOwner(t, sqlDB, orgID, "Update Test Org", "update-test-org", "", "active", ownerUserID)

	// PATCH the org name and verify owner_id is in the response.
	body := `{"name":"Updated Name"}`
	rec := sendJSON(t, e, http.MethodPatch, "/orgs/"+orgID, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}

	val, exists := raw["owner_id"]
	if !exists {
		t.Fatal("PATCH /orgs/:id response does not contain 'owner_id' key; expected owner_id when set")
	}
	if val != ownerUserID {
		t.Errorf("owner_id = %v; want %q", val, ownerUserID)
	}
}

// TestPersonalOrg_OrgAPI_CreateWithOwnerID verifies that POST /orgs accepts
// an optional owner_id field in the request body, stores it on the new org
// record, and returns HTTP 201 with the owner_id included in the response JSON.
//
// Test Spec: TS-04-15
// Requirement: 04-REQ-4.4
func TestPersonalOrg_OrgAPI_CreateWithOwnerID(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	body := `{"name":"My Personal Org","slug":"my-personal-org","owner_id":"user-xyz"}`
	rec := sendJSON(t, e, http.MethodPost, "/orgs", body)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected HTTP 201, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Parse the response JSON as a raw map to check for owner_id.
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}

	// Verify owner_id is in the response.
	val, exists := raw["owner_id"]
	if !exists {
		t.Fatal("POST /orgs response does not contain 'owner_id' key; expected owner_id when provided")
	}
	if val != "user-xyz" {
		t.Errorf("response owner_id = %v; want %q", val, "user-xyz")
	}

	// Verify owner_id is stored in the database.
	orgID, ok := raw["id"].(string)
	if !ok || orgID == "" {
		t.Fatal("response does not contain a valid 'id' field")
	}

	var dbOwnerID *string
	err := sqlDB.QueryRow("SELECT owner_id FROM orgs WHERE id = ?", orgID).Scan(&dbOwnerID)
	if err != nil {
		t.Fatalf("failed to query owner_id from database: %v (hint: owner_id column may not exist yet)", err)
	}
	if dbOwnerID == nil {
		t.Error("database owner_id is NULL; expected 'user-xyz'")
	} else if *dbOwnerID != "user-xyz" {
		t.Errorf("database owner_id = %q; want %q", *dbOwnerID, "user-xyz")
	}
}

// TestPersonalOrg_OrgAPI_CreateWithoutOwnerID verifies that POST /orgs
// without an owner_id field still succeeds (HTTP 201) and the owner_id
// is NULL in the database. The response JSON should omit the owner_id key.
//
// Requirement: 04-REQ-4.4 (optional field)
func TestPersonalOrg_OrgAPI_CreateWithoutOwnerID(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	body := `{"name":"Regular Org","slug":"regular-org"}`
	rec := sendJSON(t, e, http.MethodPost, "/orgs", body)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected HTTP 201, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Parse the response JSON.
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}

	// owner_id should be omitted when not provided (omitempty semantics).
	if _, exists := raw["owner_id"]; exists {
		t.Error("POST /orgs response contains 'owner_id' key when not provided; expected omission")
	}

	// Verify owner_id is NULL in the database.
	orgID, ok := raw["id"].(string)
	if !ok || orgID == "" {
		t.Fatal("response does not contain a valid 'id' field")
	}

	var dbOwnerID *string
	err := sqlDB.QueryRow("SELECT owner_id FROM orgs WHERE id = ?", orgID).Scan(&dbOwnerID)
	if err != nil {
		t.Fatalf("failed to query owner_id from database: %v (hint: owner_id column may not exist yet)", err)
	}
	if dbOwnerID != nil {
		t.Errorf("database owner_id = %q; expected NULL", *dbOwnerID)
	}
}

// TestPersonalOrg_OrgAPI_OmitsOwnerIDWhenNull verifies that GET /orgs/:id
// for an admin-created org (owner_id = NULL) returns a JSON response that
// does NOT contain the "owner_id" key at all, due to Go's omitempty
// semantics on the *string OwnerID field.
//
// Test Spec: TS-04-E5
// Requirement: 04-REQ-4.E1
func TestPersonalOrg_OrgAPI_OmitsOwnerIDWhenNull(t *testing.T) {
	e, sqlDB := setupOrgAdminTestServer(t)

	// Insert an org with owner_id = NULL (admin-created org).
	orgID := uuid.New().String()
	insertTestOrgWithOwner(t, sqlDB, orgID, "Admin Created Org", "admin-created-org", "", "active", "")

	// GET the org.
	rec := sendGet(t, e, "/orgs/"+orgID)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Check raw JSON string does NOT contain "owner_id" key.
	jsonStr := rec.Body.String()
	if strings.Contains(jsonStr, "owner_id") {
		t.Errorf("GET /orgs/:id response contains 'owner_id' for admin-created org (owner_id=NULL); expected omission\nresponse: %s", jsonStr)
	}

	// Also verify via parsed JSON map.
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}
	if _, exists := raw["owner_id"]; exists {
		t.Error("parsed response contains 'owner_id' key when owner_id is NULL; expected omission")
	}
}

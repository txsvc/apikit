package handlers_test

import (
	"database/sql"
	"net/http"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/txsvc/apikit"
	"github.com/txsvc/apikit/internal/db"
	"github.com/txsvc/apikit/internal/handlers"
)

// setupOrgPATTestServer creates an Echo instance with RegisterOrgHandlers
// registered behind a PAT credential carrying the given permissions.
func setupOrgPATTestServer(t *testing.T, userID string, permissions []string) (*echo.Echo, *sql.DB) {
	t.Helper()

	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	e := echo.New()
	g := e.Group("", apikit.CacheMiddleware(apikit.CacheNoStore))
	g.Use(patAuthMiddleware(userID, permissions))
	handlers.RegisterOrgHandlers(g, database.SqlDB)

	return e, database.SqlDB
}

// TestGetOrg_PATWithoutOrgsRead verifies that a PAT lacking orgs:read is
// rejected with 403 "insufficient permissions" on GET /orgs/:id even when
// the owning user is a member of the organization. PAT scoping must be
// enforced independently of membership.
func TestGetOrg_PATWithoutOrgsRead(t *testing.T) {
	memberUserID := testUUID("pat-member-no-orgs-read")
	e, sqlDB := setupOrgPATTestServer(t, memberUserID, []string{"users:read"})

	orgID := "a0000010-0000-4000-8000-000000000010"
	insertTestUser(t, sqlDB, memberUserID, "patmember", "patmember@example.com", "github", "gh-patmember")
	insertTestOrg(t, sqlDB, orgID, "PAT Org", "pat-org", "", "active")
	insertTestOrgMember(t, sqlDB, orgID, memberUserID)

	rec := sendGet(t, e, "/orgs/"+orgID)
	assertErrorResponse(t, rec, http.StatusForbidden, "insufficient permissions")
}

// TestGetOrg_PATWithOrgsRead verifies that a member PAT holding orgs:read
// can read the organization.
func TestGetOrg_PATWithOrgsRead(t *testing.T) {
	memberUserID := testUUID("pat-member-orgs-read")
	e, sqlDB := setupOrgPATTestServer(t, memberUserID, []string{"orgs:read"})

	orgID := "a0000011-0000-4000-8000-000000000011"
	insertTestUser(t, sqlDB, memberUserID, "patmember2", "patmember2@example.com", "github", "gh-patmember2")
	insertTestOrg(t, sqlDB, orgID, "PAT Org 2", "pat-org-2", "", "active")
	insertTestOrgMember(t, sqlDB, orgID, memberUserID)

	rec := sendGet(t, e, "/orgs/"+orgID)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

// TestListOrgMembers_PATWithoutOrgsRead verifies that a PAT lacking orgs:read
// is rejected with 403 "insufficient permissions" on GET /orgs/:id/members.
func TestListOrgMembers_PATWithoutOrgsRead(t *testing.T) {
	memberUserID := testUUID("pat-member-list-no-orgs-read")
	e, sqlDB := setupOrgPATTestServer(t, memberUserID, []string{"users:read", "tokens:read"})

	orgID := "a0000012-0000-4000-8000-000000000012"
	insertTestUser(t, sqlDB, memberUserID, "patmember3", "patmember3@example.com", "github", "gh-patmember3")
	insertTestOrg(t, sqlDB, orgID, "PAT Org 3", "pat-org-3", "", "active")
	insertTestOrgMember(t, sqlDB, orgID, memberUserID)

	rec := sendGet(t, e, "/orgs/"+orgID+"/members")
	assertErrorResponse(t, rec, http.StatusForbidden, "insufficient permissions")
}

// TestListOrgMembers_PATWithOrgsRead verifies that a member PAT holding
// orgs:read can list the organization's members.
func TestListOrgMembers_PATWithOrgsRead(t *testing.T) {
	memberUserID := testUUID("pat-member-list-orgs-read")
	e, sqlDB := setupOrgPATTestServer(t, memberUserID, []string{"orgs:read"})

	orgID := "a0000013-0000-4000-8000-000000000013"
	insertTestUser(t, sqlDB, memberUserID, "patmember4", "patmember4@example.com", "github", "gh-patmember4")
	insertTestOrg(t, sqlDB, orgID, "PAT Org 4", "pat-org-4", "", "active")
	insertTestOrgMember(t, sqlDB, orgID, memberUserID)

	rec := sendGet(t, e, "/orgs/"+orgID+"/members")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

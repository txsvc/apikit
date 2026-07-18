package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"regexp"

	"github.com/labstack/echo/v4"

	"github.com/txsvc/apikit"
	"github.com/txsvc/apikit/internal/auth"
)

// OrgResponse represents the JSON response shape for an organization resource.
type OrgResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	URL       string `json:"url"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// OrgMemberResponse represents the JSON response shape for an organization
// membership record.
type OrgMemberResponse struct {
	OrgID     string `json:"org_id"`
	UserID    string `json:"user_id"`
	Username  string `json:"username"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

// CreateOrgRequest represents the JSON request body for creating an organization.
type CreateOrgRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
	URL  string `json:"url"`
}

// UpdateOrgRequest represents the JSON request body for updating an organization.
type UpdateOrgRequest struct {
	Name *string `json:"name"`
	URL  *string `json:"url"`
}

// orgHandlers holds the database handle shared by all organization management
// handler functions. Methods on this struct are unexported; only
// RegisterOrgHandlers is exported from this file.
type orgHandlers struct {
	db *sql.DB
}

// RegisterOrgHandlers registers all organization management routes on the
// provided Echo group and stores the *sql.DB handle for use by all handler
// functions. Panics if db is nil.
func RegisterOrgHandlers(g *echo.Group, database *sql.DB) {
	if database == nil {
		panic("RegisterOrgHandlers: db must not be nil")
	}

	h := &orgHandlers{db: database}

	// Organization CRUD endpoints.
	g.POST("/orgs", h.createOrg)
	g.GET("/orgs", h.listOrgs)
	g.GET("/orgs/:id", h.getOrg)
	g.PATCH("/orgs/:id", h.updateOrg)
	g.DELETE("/orgs/:id", h.deleteOrg)

	// Organization lifecycle management endpoints.
	g.POST("/orgs/:id/block", h.blockOrg)
	g.POST("/orgs/:id/unblock", h.unblockOrg)

	// Organization membership management endpoints.
	g.GET("/orgs/:id/members", h.listOrgMembers)
	g.PUT("/orgs/:id/members/:user_id", h.addOrgMember)
	g.DELETE("/orgs/:id/members/:user_id", h.removeOrgMember)
}

// requireOrgAdmin is a convenience wrapper that calls auth.RequireAdmin and
// returns a 403 APIError if the user is not an admin.
func requireOrgAdmin(c echo.Context) error {
	if err := auth.RequireAdmin(c); err != nil {
		return apikit.APIError(c, http.StatusForbidden, "forbidden")
	}
	return nil
}

// createOrg handles POST /orgs — creates a new organization.
func (h *orgHandlers) createOrg(c echo.Context) error {
	return apikit.APIError(c, http.StatusNotImplemented, "not implemented")
}

// listOrgs handles GET /orgs — lists all organizations.
func (h *orgHandlers) listOrgs(c echo.Context) error {
	return apikit.APIError(c, http.StatusNotImplemented, "not implemented")
}

// getOrg handles GET /orgs/:id — retrieves a single organization by ID.
func (h *orgHandlers) getOrg(c echo.Context) error {
	return apikit.APIError(c, http.StatusNotImplemented, "not implemented")
}

// updateOrg handles PATCH /orgs/:id — updates an organization's name and/or URL.
func (h *orgHandlers) updateOrg(c echo.Context) error {
	return apikit.APIError(c, http.StatusNotImplemented, "not implemented")
}

// deleteOrg handles DELETE /orgs/:id — deletes an organization.
func (h *orgHandlers) deleteOrg(c echo.Context) error {
	return apikit.APIError(c, http.StatusNotImplemented, "not implemented")
}

// blockOrg handles POST /orgs/:id/block — blocks an organization.
func (h *orgHandlers) blockOrg(c echo.Context) error {
	return apikit.APIError(c, http.StatusNotImplemented, "not implemented")
}

// unblockOrg handles POST /orgs/:id/unblock — unblocks an organization.
func (h *orgHandlers) unblockOrg(c echo.Context) error {
	return apikit.APIError(c, http.StatusNotImplemented, "not implemented")
}

// listOrgMembers handles GET /orgs/:id/members — lists members of an organization.
func (h *orgHandlers) listOrgMembers(c echo.Context) error {
	return apikit.APIError(c, http.StatusNotImplemented, "not implemented")
}

// addOrgMember handles PUT /orgs/:id/members/:user_id — adds a user to an organization.
func (h *orgHandlers) addOrgMember(c echo.Context) error {
	return apikit.APIError(c, http.StatusNotImplemented, "not implemented")
}

// removeOrgMember handles DELETE /orgs/:id/members/:user_id — removes a user from an organization.
func (h *orgHandlers) removeOrgMember(c echo.Context) error {
	return apikit.APIError(c, http.StatusNotImplemented, "not implemented")
}

// slugPattern matches slugs of 2+ characters: starts and ends with [a-z0-9],
// middle characters may include hyphens and underscores. Single-character
// slugs (just [a-z0-9]) are handled by the length-1 branch below.
var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*[a-z0-9]$`)

// validateSlug checks that a slug conforms to the URL-safe format rules:
// lowercase ASCII letters, digits, hyphens, and underscores only; length
// between 1 and 128 characters; must not start or end with a hyphen or
// underscore.
func validateSlug(slug string) error {
	if len(slug) == 0 || len(slug) > 128 {
		return fmt.Errorf("invalid slug format")
	}
	// A single-character slug must be [a-z0-9].
	if len(slug) == 1 {
		c := slug[0]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			return nil
		}
		return fmt.Errorf("invalid slug format")
	}
	if !slugPattern.MatchString(slug) {
		return fmt.Errorf("invalid slug format")
	}
	return nil
}

// isOrgMember queries org_members to check whether a given user belongs to a
// given organization. Returns (true, nil) if a matching row exists,
// (false, nil) if no row exists, and (false, error) on database error.
func isOrgMember(database *sql.DB, orgID, userID string) (bool, error) {
	var exists int
	err := database.QueryRow(
		"SELECT 1 FROM org_members WHERE org_id = ? AND user_id = ? LIMIT 1",
		orgID, userID,
	).Scan(&exists)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

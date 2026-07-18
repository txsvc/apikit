package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/txsvc/apikit"
	"github.com/txsvc/apikit/internal/auth"
	"github.com/txsvc/apikit/internal/db"
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
// Requires admin access. Validates name (non-empty after trimming) and slug
// (URL-safe format). Generates a UUID v4 for the org ID, sets status to
// 'active', and timestamps to NowUTC(). Returns HTTP 201 with OrgResponse.
// Handles UNIQUE constraint violations on name and slug columns separately.
func (h *orgHandlers) createOrg(c echo.Context) error {
	// Auth check: admin only (08-REQ-2.7).
	if err := auth.RequireAdmin(c); err != nil {
		return apikit.APIError(c, http.StatusForbidden, "forbidden")
	}

	// Bind request body (08-REQ-2.E4).
	var req CreateOrgRequest
	if err := c.Bind(&req); err != nil {
		return apikit.APIError(c, http.StatusBadRequest, "invalid request body")
	}

	// Validate name: must be non-empty after trimming (08-REQ-2.2).
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return apikit.APIError(c, http.StatusBadRequest, "name is required")
	}

	// Validate slug: must be non-empty (08-REQ-2.3).
	if req.Slug == "" {
		return apikit.APIError(c, http.StatusBadRequest, "slug is required")
	}

	// Validate slug format (08-REQ-2.4, 08-REQ-2.E1, 08-REQ-2.E2).
	if err := validateSlug(req.Slug); err != nil {
		return apikit.APIError(c, http.StatusBadRequest, "invalid slug format")
	}

	// Build org record with defaults (08-REQ-2.1).
	now := db.FormatTime(time.Now().UTC())
	org := OrgResponse{
		ID:        uuid.New().String(),
		Name:      req.Name,
		Slug:      req.Slug,
		URL:       req.URL,
		Status:    "active",
		CreatedAt: now,
		UpdatedAt: now,
	}

	// INSERT into orgs table.
	_, err := h.db.Exec(
		`INSERT INTO orgs (id, name, slug, url, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		org.ID, org.Name, org.Slug, org.URL, org.Status, org.CreatedAt, org.UpdatedAt,
	)
	if err != nil {
		// Detect UNIQUE constraint violations on name vs slug (08-REQ-2.5, 08-REQ-2.6).
		// Parse the raw error string before wrapping — db.WrapError loses column identity.
		errStr := err.Error()
		if strings.Contains(errStr, "UNIQUE constraint failed: orgs.name") {
			return apikit.APIError(c, http.StatusConflict, "organization name already exists")
		}
		if strings.Contains(errStr, "UNIQUE constraint failed: orgs.slug") {
			return apikit.APIError(c, http.StatusConflict, "organization slug already exists")
		}
		// Any unexpected DB error (08-REQ-2.E3).
		return apikit.APIError(c, http.StatusInternalServerError, "internal server error")
	}

	return c.JSON(http.StatusCreated, org)
}

// listOrgs handles GET /orgs — lists all organizations.
// Requires admin access. By default only active organizations are returned;
// pass include_blocked=true to include blocked organizations. Results are
// ordered by name ascending. Returns an empty JSON array (not null) when no
// organizations match.
func (h *orgHandlers) listOrgs(c echo.Context) error {
	// Auth check: admin only (08-REQ-3.4).
	if err := auth.RequireAdmin(c); err != nil {
		return apikit.APIError(c, http.StatusForbidden, "forbidden")
	}

	// Build query with optional status filter (08-REQ-3.1, 08-REQ-3.2).
	// Use COALESCE for url to handle potential NULL values defensively.
	query := `SELECT id, name, slug, COALESCE(url, '') AS url,
	          status, created_at, updated_at FROM orgs`
	if c.QueryParam("include_blocked") != "true" {
		query += ` WHERE status = 'active'`
	}
	query += ` ORDER BY name ASC`

	rows, err := h.db.Query(query)
	if err != nil {
		return apikit.APIError(c, http.StatusInternalServerError, "internal server error")
	}
	defer rows.Close()

	// Scan rows into a non-nil slice to ensure [] JSON output (08-REQ-3.3).
	orgs := make([]OrgResponse, 0)
	for rows.Next() {
		var o OrgResponse
		if err := rows.Scan(&o.ID, &o.Name, &o.Slug, &o.URL,
			&o.Status, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return apikit.APIError(c, http.StatusInternalServerError, "internal server error")
		}
		orgs = append(orgs, o)
	}
	if err := rows.Err(); err != nil {
		return apikit.APIError(c, http.StatusInternalServerError, "internal server error")
	}

	return c.JSON(http.StatusOK, orgs)
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

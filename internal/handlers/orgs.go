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

	"github.com/txsvc/apikit/internal/apiutil"
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
		return apiutil.WriteAPIError(c, http.StatusForbidden, "forbidden")
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
		return apiutil.WriteAPIError(c, http.StatusForbidden, "forbidden")
	}

	// Bind request body (08-REQ-2.E4).
	var req CreateOrgRequest
	if err := c.Bind(&req); err != nil {
		return apiutil.WriteAPIError(c, http.StatusBadRequest, "invalid request body")
	}

	// Validate name: must be non-empty after trimming (08-REQ-2.2).
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return apiutil.WriteAPIError(c, http.StatusBadRequest, "name is required")
	}

	// Validate slug: must be non-empty (08-REQ-2.3).
	if req.Slug == "" {
		return apiutil.WriteAPIError(c, http.StatusBadRequest, "slug is required")
	}

	// Validate slug format (08-REQ-2.4, 08-REQ-2.E1, 08-REQ-2.E2).
	if err := validateSlug(req.Slug); err != nil {
		return apiutil.WriteAPIError(c, http.StatusBadRequest, "invalid slug format")
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
			return apiutil.WriteAPIError(c, http.StatusConflict, "organization name already exists")
		}
		if strings.Contains(errStr, "UNIQUE constraint failed: orgs.slug") {
			return apiutil.WriteAPIError(c, http.StatusConflict, "organization slug already exists")
		}
		// Any unexpected DB error (08-REQ-2.E3).
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
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
		return apiutil.WriteAPIError(c, http.StatusForbidden, "forbidden")
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
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}
	defer rows.Close()

	// Scan rows into a non-nil slice to ensure [] JSON output (08-REQ-3.3).
	orgs := make([]OrgResponse, 0)
	for rows.Next() {
		var o OrgResponse
		if err := rows.Scan(&o.ID, &o.Name, &o.Slug, &o.URL,
			&o.Status, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
		}
		orgs = append(orgs, o)
	}
	if err := rows.Err(); err != nil {
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	return c.JSON(http.StatusOK, orgs)
}

// getOrg handles GET /orgs/:id — retrieves a single organization by ID.
// Admin users can view any organization. Non-admin users can only view
// organizations they are a member of (checked via isOrgMember). Sets an
// ETag header from updated_at and supports conditional GET via If-None-Match.
func (h *orgHandlers) getOrg(c echo.Context) error {
	// Validate :id path parameter (08-REQ-4.5).
	id := c.Param("id")
	if _, err := uuid.Parse(id); err != nil {
		return apiutil.WriteAPIError(c, http.StatusBadRequest, "invalid organization id")
	}

	// Query the org from the database (08-REQ-4.4).
	var org OrgResponse
	err := h.db.QueryRow(
		`SELECT id, name, slug, COALESCE(url, '') AS url,
		        status, created_at, updated_at
		 FROM orgs WHERE id = ?`, id,
	).Scan(&org.ID, &org.Name, &org.Slug, &org.URL,
		&org.Status, &org.CreatedAt, &org.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return apiutil.WriteAPIError(c, http.StatusNotFound, "organization not found")
		}
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Access control: admin or org member (08-REQ-4.1, 08-REQ-4.2, 08-REQ-4.3).
	if !auth.IsAdmin(c) {
		userID := auth.GetUserID(c)
		isMember, err := isOrgMember(h.db, org.ID, userID)
		if err != nil {
			// DB error on membership check (08-REQ-4.E1).
			return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
		}
		if !isMember {
			return apiutil.WriteAPIError(c, http.StatusForbidden, "forbidden")
		}
	}

	// Parse UpdatedAt string to time.Time for ETag helpers (08-REQ-4.1, 08-REQ-4.6).
	// SetETag/CheckETag accept time.Time, not string.
	updatedAt, err := db.ParseTime(org.UpdatedAt)
	if err != nil {
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	apiutil.SetETag(c, updatedAt)

	// Check If-None-Match for conditional GET (08-REQ-4.6).
	if apiutil.CheckETag(c, updatedAt) {
		return c.NoContent(http.StatusNotModified)
	}

	return c.JSON(http.StatusOK, org)
}

// updateOrg handles PATCH /orgs/:id — updates an organization's name and/or URL.
// Requires admin access. Silently ignores any slug field in the request body
// (slug is immutable after creation). Returns HTTP 400 if no recognized fields
// are provided. Detects UNIQUE constraint violations on name and returns 409.
func (h *orgHandlers) updateOrg(c echo.Context) error {
	// Auth check: admin only (08-REQ-5.7).
	if err := auth.RequireAdmin(c); err != nil {
		return apiutil.WriteAPIError(c, http.StatusForbidden, "forbidden")
	}

	// Validate :id path parameter (08-REQ-5.6).
	id := c.Param("id")
	if _, err := uuid.Parse(id); err != nil {
		return apiutil.WriteAPIError(c, http.StatusBadRequest, "invalid organization id")
	}

	// Bind request body (08-REQ-5.3).
	var req UpdateOrgRequest
	if err := c.Bind(&req); err != nil {
		return apiutil.WriteAPIError(c, http.StatusBadRequest, "invalid request body")
	}

	// Check if there are any recognized fields to update (08-REQ-5.3).
	if req.Name == nil && req.URL == nil {
		return apiutil.WriteAPIError(c, http.StatusBadRequest, "no fields to update")
	}

	// If name is provided, validate it after trimming (08-REQ-5.E1).
	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" {
			return apiutil.WriteAPIError(c, http.StatusBadRequest, "name is required")
		}
		req.Name = &trimmed
	}

	// Verify the org exists (08-REQ-5.5).
	var org OrgResponse
	err := h.db.QueryRow(
		`SELECT id, name, slug, COALESCE(url, '') AS url,
		        status, created_at, updated_at
		 FROM orgs WHERE id = ?`, id,
	).Scan(&org.ID, &org.Name, &org.Slug, &org.URL,
		&org.Status, &org.CreatedAt, &org.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return apiutil.WriteAPIError(c, http.StatusNotFound, "organization not found")
		}
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Build dynamic UPDATE — slug is never in the SET clause (08-REQ-5.2, 08-PROP-1).
	now := db.FormatTime(time.Now().UTC())
	setClauses := []string{"updated_at = ?"}
	args := []interface{}{now}

	if req.Name != nil {
		setClauses = append(setClauses, "name = ?")
		args = append(args, *req.Name)
	}
	if req.URL != nil {
		setClauses = append(setClauses, "url = ?")
		args = append(args, *req.URL)
	}

	args = append(args, id)
	query := "UPDATE orgs SET " + strings.Join(setClauses, ", ") + " WHERE id = ?"

	_, err = h.db.Exec(query, args...)
	if err != nil {
		// Detect UNIQUE constraint violation on name (08-REQ-5.4).
		errStr := err.Error()
		if strings.Contains(errStr, "UNIQUE constraint failed: orgs.name") {
			return apiutil.WriteAPIError(c, http.StatusConflict, "organization name already exists")
		}
		// Any other DB error (08-REQ-5.E2).
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Re-fetch the updated org to return (08-REQ-5.1).
	err = h.db.QueryRow(
		`SELECT id, name, slug, COALESCE(url, '') AS url,
		        status, created_at, updated_at
		 FROM orgs WHERE id = ?`, id,
	).Scan(&org.ID, &org.Name, &org.Slug, &org.URL,
		&org.Status, &org.CreatedAt, &org.UpdatedAt)
	if err != nil {
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	return c.JSON(http.StatusOK, org)
}

// deleteOrg handles DELETE /orgs/:id — deletes an organization.
// Requires admin access. Validates the :id UUID path parameter and executes a
// DELETE against the orgs table. The database ON DELETE CASCADE constraint on
// org_members automatically removes all membership rows for the deleted org;
// user rows in the users table are unaffected. Returns HTTP 204 with no body
// on success, 404 when zero rows are affected, and 500 on database error.
func (h *orgHandlers) deleteOrg(c echo.Context) error {
	// Auth check: admin only (08-REQ-6.4).
	if err := auth.RequireAdmin(c); err != nil {
		return apiutil.WriteAPIError(c, http.StatusForbidden, "forbidden")
	}

	// Validate :id path parameter (08-REQ-6.3).
	id := c.Param("id")
	if _, err := uuid.Parse(id); err != nil {
		return apiutil.WriteAPIError(c, http.StatusBadRequest, "invalid organization id")
	}

	// Execute DELETE (08-REQ-6.1); ON DELETE CASCADE handles org_members.
	result, err := h.db.Exec("DELETE FROM orgs WHERE id = ?", id)
	if err != nil {
		// DB error (08-REQ-6.E1).
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Check rows affected — zero means org not found (08-REQ-6.2).
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}
	if rowsAffected == 0 {
		return apiutil.WriteAPIError(c, http.StatusNotFound, "organization not found")
	}

	return c.NoContent(http.StatusNoContent)
}

// blockOrg handles POST /orgs/:id/block — blocks an organization.
// Requires admin access. Looks up the org by ID; if the org is already
// blocked, returns it as-is (idempotent — no UPDATE, no updated_at change).
// Otherwise sets status='blocked' and updated_at to the current UTC time,
// re-fetches the row, and returns it. Returns HTTP 200 with OrgResponse.
func (h *orgHandlers) blockOrg(c echo.Context) error {
	// Auth check: admin only (08-REQ-7.5).
	if err := auth.RequireAdmin(c); err != nil {
		return apiutil.WriteAPIError(c, http.StatusForbidden, "forbidden")
	}

	// Validate :id path parameter (08-REQ-7.4).
	id := c.Param("id")
	if _, err := uuid.Parse(id); err != nil {
		return apiutil.WriteAPIError(c, http.StatusBadRequest, "invalid organization id")
	}

	// Fetch current org state (08-REQ-7.3).
	var org OrgResponse
	err := h.db.QueryRow(
		`SELECT id, name, slug, COALESCE(url, '') AS url,
		        status, created_at, updated_at
		 FROM orgs WHERE id = ?`, id,
	).Scan(&org.ID, &org.Name, &org.Slug, &org.URL,
		&org.Status, &org.CreatedAt, &org.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return apiutil.WriteAPIError(c, http.StatusNotFound, "organization not found")
		}
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Idempotent: already blocked → return as-is (08-REQ-7.2, 08-PROP-5).
	if org.Status == "blocked" {
		return c.JSON(http.StatusOK, org)
	}

	// Update status to 'blocked' and set updated_at (08-REQ-7.1).
	now := db.FormatTime(time.Now().UTC())
	_, err = h.db.Exec(
		"UPDATE orgs SET status = 'blocked', updated_at = ? WHERE id = ?",
		now, id,
	)
	if err != nil {
		// DB error on UPDATE (08-REQ-7.E1).
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Re-fetch updated org to return.
	err = h.db.QueryRow(
		`SELECT id, name, slug, COALESCE(url, '') AS url,
		        status, created_at, updated_at
		 FROM orgs WHERE id = ?`, id,
	).Scan(&org.ID, &org.Name, &org.Slug, &org.URL,
		&org.Status, &org.CreatedAt, &org.UpdatedAt)
	if err != nil {
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	return c.JSON(http.StatusOK, org)
}

// unblockOrg handles POST /orgs/:id/unblock — unblocks an organization.
// Requires admin access. Looks up the org by ID; if the org is already
// active, returns it as-is (idempotent — no UPDATE, no updated_at change).
// Otherwise sets status='active' and updated_at to the current UTC time,
// re-fetches the row, and returns it. Returns HTTP 200 with OrgResponse.
func (h *orgHandlers) unblockOrg(c echo.Context) error {
	// Auth check: admin only (08-REQ-8.5).
	if err := auth.RequireAdmin(c); err != nil {
		return apiutil.WriteAPIError(c, http.StatusForbidden, "forbidden")
	}

	// Validate :id path parameter (08-REQ-8.4).
	id := c.Param("id")
	if _, err := uuid.Parse(id); err != nil {
		return apiutil.WriteAPIError(c, http.StatusBadRequest, "invalid organization id")
	}

	// Fetch current org state (08-REQ-8.3).
	var org OrgResponse
	err := h.db.QueryRow(
		`SELECT id, name, slug, COALESCE(url, '') AS url,
		        status, created_at, updated_at
		 FROM orgs WHERE id = ?`, id,
	).Scan(&org.ID, &org.Name, &org.Slug, &org.URL,
		&org.Status, &org.CreatedAt, &org.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return apiutil.WriteAPIError(c, http.StatusNotFound, "organization not found")
		}
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Idempotent: already active → return as-is (08-REQ-8.2, 08-PROP-6).
	if org.Status == "active" {
		return c.JSON(http.StatusOK, org)
	}

	// Update status to 'active' and set updated_at (08-REQ-8.1).
	now := db.FormatTime(time.Now().UTC())
	_, err = h.db.Exec(
		"UPDATE orgs SET status = 'active', updated_at = ? WHERE id = ?",
		now, id,
	)
	if err != nil {
		// DB error on UPDATE (08-REQ-8.E1).
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Re-fetch updated org to return.
	err = h.db.QueryRow(
		`SELECT id, name, slug, COALESCE(url, '') AS url,
		        status, created_at, updated_at
		 FROM orgs WHERE id = ?`, id,
	).Scan(&org.ID, &org.Name, &org.Slug, &org.URL,
		&org.Status, &org.CreatedAt, &org.UpdatedAt)
	if err != nil {
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	return c.JSON(http.StatusOK, org)
}

// listOrgMembers handles GET /orgs/:id/members — lists members of an organization.
// Admin users can view any org's members. Non-admin users can only view members
// of organizations they belong to (checked via isOrgMember). Returns a JSON array
// of OrgMemberResponse objects ordered alphabetically by username. Returns an
// empty JSON array (not null) when the org has no members.
func (h *orgHandlers) listOrgMembers(c echo.Context) error {
	// Validate :id path parameter (08-REQ-9.6).
	id := c.Param("id")
	if _, err := uuid.Parse(id); err != nil {
		return apiutil.WriteAPIError(c, http.StatusBadRequest, "invalid organization id")
	}

	// Verify the org exists (08-REQ-9.4).
	var exists int
	err := h.db.QueryRow("SELECT 1 FROM orgs WHERE id = ?", id).Scan(&exists)
	if err != nil {
		if err == sql.ErrNoRows {
			return apiutil.WriteAPIError(c, http.StatusNotFound, "organization not found")
		}
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Access control: admin or org member (08-REQ-9.1, 08-REQ-9.2, 08-REQ-9.3).
	if !auth.IsAdmin(c) {
		userID := auth.GetUserID(c)
		isMember, err := isOrgMember(h.db, id, userID)
		if err != nil {
			return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
		}
		if !isMember {
			return apiutil.WriteAPIError(c, http.StatusForbidden, "forbidden")
		}
	}

	// Query members via JOIN with users, ordered by username ASC (08-REQ-9.1).
	rows, err := h.db.Query(
		`SELECT u.id, u.username, u.email, u.role, om.created_at
		 FROM org_members om
		 JOIN users u ON u.id = om.user_id
		 WHERE om.org_id = ?
		 ORDER BY u.username ASC`, id,
	)
	if err != nil {
		// DB error on join query (08-REQ-9.E1).
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}
	defer rows.Close()

	// Scan into a non-nil empty slice to ensure [] JSON output (08-REQ-9.5).
	members := make([]OrgMemberResponse, 0)
	for rows.Next() {
		var m OrgMemberResponse
		if err := rows.Scan(&m.UserID, &m.Username, &m.Email, &m.Role, &m.CreatedAt); err != nil {
			return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
		}
		m.OrgID = id
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	return c.JSON(http.StatusOK, members)
}

// addOrgMember handles PUT /orgs/:id/members/:user_id — adds a user to an organization.
// Requires admin access. Validates both UUID path parameters. Verifies the org
// and user exist before inserting. On primary key conflict (user already a member),
// returns 204 idempotently without inserting a new row.
func (h *orgHandlers) addOrgMember(c echo.Context) error {
	// Auth check: admin only (08-REQ-10.7).
	if err := auth.RequireAdmin(c); err != nil {
		return apiutil.WriteAPIError(c, http.StatusForbidden, "forbidden")
	}

	// Validate :id path parameter (08-REQ-10.5).
	orgID := c.Param("id")
	if _, err := uuid.Parse(orgID); err != nil {
		return apiutil.WriteAPIError(c, http.StatusBadRequest, "invalid organization id")
	}

	// Validate :user_id path parameter (08-REQ-10.6).
	userID := c.Param("user_id")
	if _, err := uuid.Parse(userID); err != nil {
		return apiutil.WriteAPIError(c, http.StatusBadRequest, "invalid user id")
	}

	// Verify the org exists (08-REQ-10.3).
	var orgExists int
	err := h.db.QueryRow("SELECT 1 FROM orgs WHERE id = ?", orgID).Scan(&orgExists)
	if err != nil {
		if err == sql.ErrNoRows {
			return apiutil.WriteAPIError(c, http.StatusNotFound, "organization not found")
		}
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Verify the user exists (08-REQ-10.4).
	var userExists int
	err = h.db.QueryRow("SELECT 1 FROM users WHERE id = ?", userID).Scan(&userExists)
	if err != nil {
		if err == sql.ErrNoRows {
			return apiutil.WriteAPIError(c, http.StatusNotFound, "user not found")
		}
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// INSERT into org_members (08-REQ-10.1).
	now := db.FormatTime(time.Now().UTC())
	_, err = h.db.Exec(
		"INSERT INTO org_members (org_id, user_id, created_at) VALUES (?, ?, ?)",
		orgID, userID, now,
	)
	if err != nil {
		// Detect PRIMARY KEY conflict for idempotent behavior (08-REQ-10.2, 08-PROP-7).
		errStr := err.Error()
		if strings.Contains(errStr, "UNIQUE constraint failed") ||
			strings.Contains(errStr, "PRIMARY KEY") {
			return c.NoContent(http.StatusNoContent)
		}
		// Any other DB error (08-REQ-10.E1).
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	return c.NoContent(http.StatusNoContent)
}

// removeOrgMember handles DELETE /orgs/:id/members/:user_id — removes a user from an organization.
// Requires admin access. Validates both UUID path parameters. Deletes the
// org_members row matching (org_id, user_id). Returns 404 when zero rows are
// affected. The user's account in the users table is not affected.
func (h *orgHandlers) removeOrgMember(c echo.Context) error {
	// Auth check: admin only (08-REQ-11.5).
	if err := auth.RequireAdmin(c); err != nil {
		return apiutil.WriteAPIError(c, http.StatusForbidden, "forbidden")
	}

	// Validate :id path parameter (08-REQ-11.3).
	orgID := c.Param("id")
	if _, err := uuid.Parse(orgID); err != nil {
		return apiutil.WriteAPIError(c, http.StatusBadRequest, "invalid organization id")
	}

	// Validate :user_id path parameter (08-REQ-11.4).
	userID := c.Param("user_id")
	if _, err := uuid.Parse(userID); err != nil {
		return apiutil.WriteAPIError(c, http.StatusBadRequest, "invalid user id")
	}

	// DELETE the membership row (08-REQ-11.1).
	result, err := h.db.Exec(
		"DELETE FROM org_members WHERE org_id = ? AND user_id = ?",
		orgID, userID,
	)
	if err != nil {
		// DB error (08-REQ-11.E1).
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Check rows affected — zero means membership not found (08-REQ-11.2).
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}
	if rowsAffected == 0 {
		return apiutil.WriteAPIError(c, http.StatusNotFound, "membership not found")
	}

	return c.NoContent(http.StatusNoContent)
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

package handlers

import (
	"database/sql"
	"errors"

	"github.com/labstack/echo/v4"
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

// RegisterOrgHandlers registers all organization management routes on the
// provided Echo group and stores the *sql.DB handle for use by all handler
// functions. Panics if db is nil.
func RegisterOrgHandlers(g *echo.Group, db *sql.DB) {
	// Stub: not implemented.
}

// validateSlug checks that a slug conforms to the URL-safe format rules:
// lowercase ASCII letters, digits, hyphens, and underscores only; length
// between 1 and 128 characters; must not start or end with a hyphen or
// underscore.
func validateSlug(slug string) error {
	// Stub: not implemented.
	return errors.New("not implemented")
}

// isOrgMember queries org_members to check whether a given user belongs to a
// given organization. Returns (true, nil) if a matching row exists,
// (false, nil) if no row exists, and (false, error) on database error.
func isOrgMember(db *sql.DB, orgID, userID string) (bool, error) {
	// Stub: not implemented.
	return false, errors.New("not implemented")
}

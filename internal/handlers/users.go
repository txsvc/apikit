package handlers

import (
	"database/sql"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/txsvc/apikit"
)

// User represents the JSON response object for user endpoints.
type User struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	Email      string `json:"email"`
	FullName   string `json:"full_name"`
	Role       string `json:"role"`
	Status     string `json:"status"`
	Provider   string `json:"provider"`
	ProviderID string `json:"provider_id"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

// CreateUserRequest represents the JSON request body for POST /users.
type CreateUserRequest struct {
	Username   string `json:"username"`
	Email      string `json:"email"`
	Provider   string `json:"provider"`
	ProviderID string `json:"provider_id"`
}

// UpdateUserRequest represents the JSON request body for PATCH /users/:id
// and PATCH /user. FullName is a pointer to distinguish a missing field (nil)
// from an explicitly empty string value.
type UpdateUserRequest struct {
	FullName *string `json:"full_name"`
}

// APIKeyMeta represents the non-secret metadata fields of an API key.
// Secret values (secret_hash) are never included in API responses.
type APIKeyMeta struct {
	KeyID     string  `json:"key_id"`
	UserID    string  `json:"user_id"`
	CreatedAt string  `json:"created_at"`
	ExpiresAt *string `json:"expires_at"`
	RevokedAt *string `json:"revoked_at"`
}

// PATMeta represents the non-secret metadata fields of a personal access token.
// Secret values (secret_hash) are never included in API responses.
type PATMeta struct {
	TokenID     string   `json:"token_id"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
	UserID      string   `json:"user_id"`
	CreatedAt   string   `json:"created_at"`
	ExpiresAt   *string  `json:"expires_at"`
	RevokedAt   *string  `json:"revoked_at"`
}

// userHandlers holds the database handle shared by all user management
// handler functions. Methods on this struct are unexported; only
// RegisterUserHandlers is exported from this file.
type userHandlers struct {
	db *sql.DB
}

// RegisterUserHandlers registers all user management routes on the provided
// Echo group and stores the *sql.DB handle for use by all handler functions.
// All 15 routes are registered:
//
//   - POST   /users
//   - GET    /users
//   - GET    /users/:id
//   - PATCH  /users/:id
//   - POST   /users/:id/promote
//   - POST   /users/:id/demote
//   - POST   /users/:id/block
//   - POST   /users/:id/unblock
//   - GET    /users/:id/keys
//   - DELETE /users/:id/keys/:key_id
//   - GET    /users/:id/tokens
//   - DELETE /users/:id/tokens/:token_id
//   - GET    /user
//   - PATCH  /user
//   - GET    /user/orgs
func RegisterUserHandlers(g *echo.Group, database *sql.DB) {
	h := &userHandlers{db: database}

	// Admin user CRUD endpoints.
	g.POST("/users", h.createUser)
	g.GET("/users", h.listUsers)
	g.GET("/users/:id", h.getUser)
	g.PATCH("/users/:id", h.updateUser)

	// Admin role management endpoints.
	g.POST("/users/:id/promote", h.promoteUser)
	g.POST("/users/:id/demote", h.demoteUser)

	// Admin lifecycle management endpoints.
	g.POST("/users/:id/block", h.blockUser)
	g.POST("/users/:id/unblock", h.unblockUser)

	// Admin credential management endpoints.
	g.GET("/users/:id/keys", h.listUserKeys)
	g.DELETE("/users/:id/keys/:key_id", h.revokeUserKey)
	g.GET("/users/:id/tokens", h.listUserTokens)
	g.DELETE("/users/:id/tokens/:token_id", h.revokeUserToken)

	// Self-service profile and organization endpoints.
	g.GET("/user", h.getOwnProfile)
	g.PATCH("/user", h.updateOwnProfile)
	g.GET("/user/orgs", h.listOwnOrgs)
}

// createUser handles POST /users — creates a new user record.
func (h *userHandlers) createUser(c echo.Context) error {
	return apikit.APIError(c, http.StatusNotImplemented, "not implemented")
}

// listUsers handles GET /users — lists all users, optionally including blocked.
func (h *userHandlers) listUsers(c echo.Context) error {
	return apikit.APIError(c, http.StatusNotImplemented, "not implemented")
}

// getUser handles GET /users/:id — retrieves a single user by ID.
func (h *userHandlers) getUser(c echo.Context) error {
	return apikit.APIError(c, http.StatusNotImplemented, "not implemented")
}

// updateUser handles PATCH /users/:id — updates a user's full_name.
func (h *userHandlers) updateUser(c echo.Context) error {
	return apikit.APIError(c, http.StatusNotImplemented, "not implemented")
}

// promoteUser handles POST /users/:id/promote — sets user role to admin.
func (h *userHandlers) promoteUser(c echo.Context) error {
	return apikit.APIError(c, http.StatusNotImplemented, "not implemented")
}

// demoteUser handles POST /users/:id/demote — sets user role to user.
func (h *userHandlers) demoteUser(c echo.Context) error {
	return apikit.APIError(c, http.StatusNotImplemented, "not implemented")
}

// blockUser handles POST /users/:id/block — sets user status to blocked.
func (h *userHandlers) blockUser(c echo.Context) error {
	return apikit.APIError(c, http.StatusNotImplemented, "not implemented")
}

// unblockUser handles POST /users/:id/unblock — sets user status to active.
func (h *userHandlers) unblockUser(c echo.Context) error {
	return apikit.APIError(c, http.StatusNotImplemented, "not implemented")
}

// listUserKeys handles GET /users/:id/keys — lists API key metadata for a user.
func (h *userHandlers) listUserKeys(c echo.Context) error {
	return apikit.APIError(c, http.StatusNotImplemented, "not implemented")
}

// revokeUserKey handles DELETE /users/:id/keys/:key_id — revokes an API key.
func (h *userHandlers) revokeUserKey(c echo.Context) error {
	return apikit.APIError(c, http.StatusNotImplemented, "not implemented")
}

// listUserTokens handles GET /users/:id/tokens — lists PAT metadata for a user.
func (h *userHandlers) listUserTokens(c echo.Context) error {
	return apikit.APIError(c, http.StatusNotImplemented, "not implemented")
}

// revokeUserToken handles DELETE /users/:id/tokens/:token_id — revokes a PAT.
func (h *userHandlers) revokeUserToken(c echo.Context) error {
	return apikit.APIError(c, http.StatusNotImplemented, "not implemented")
}

// getOwnProfile handles GET /user — retrieves the authenticated user's profile.
func (h *userHandlers) getOwnProfile(c echo.Context) error {
	return apikit.APIError(c, http.StatusNotImplemented, "not implemented")
}

// updateOwnProfile handles PATCH /user — updates the authenticated user's full_name.
func (h *userHandlers) updateOwnProfile(c echo.Context) error {
	return apikit.APIError(c, http.StatusNotImplemented, "not implemented")
}

// listOwnOrgs handles GET /user/orgs — lists the authenticated user's organizations.
func (h *userHandlers) listOwnOrgs(c echo.Context) error {
	return apikit.APIError(c, http.StatusNotImplemented, "not implemented")
}

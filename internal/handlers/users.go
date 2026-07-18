package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/txsvc/apikit"
	"github.com/txsvc/apikit/internal/auth"
	"github.com/txsvc/apikit/internal/db"
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
// Requires admin access. Validates all four required fields, generates a UUID,
// inserts into the users table, and returns HTTP 201 with the created User.
// Detects unique constraint violations on username and (provider, provider_id).
func (h *userHandlers) createUser(c echo.Context) error {
	// Auth check: admin only (07-REQ-2.6, 07-PROP-5).
	if err := auth.RequireAdmin(c); err != nil {
		return apikit.WriteAPIError(c, http.StatusForbidden, "forbidden")
	}

	// Bind request body (07-REQ-2.3).
	var req CreateUserRequest
	if err := c.Bind(&req); err != nil {
		return apikit.WriteAPIError(c, http.StatusBadRequest, "invalid request body")
	}

	// Validate required fields (07-REQ-2.2).
	for _, check := range []struct {
		value string
		field string
	}{
		{req.Username, "username"},
		{req.Email, "email"},
		{req.Provider, "provider"},
		{req.ProviderID, "provider_id"},
	} {
		if check.value == "" {
			return apikit.WriteAPIError(c, http.StatusBadRequest, "missing required field: "+check.field)
		}
	}

	// Build user record with defaults (07-REQ-2.1).
	now := db.FormatTime(time.Now().UTC())
	user := User{
		ID:         uuid.New().String(),
		Username:   req.Username,
		Email:      req.Email,
		FullName:   "",
		Role:       "user",
		Status:     "active",
		Provider:   req.Provider,
		ProviderID: req.ProviderID,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	// INSERT into users table.
	_, err := h.db.Exec(
		`INSERT INTO users (id, username, email, full_name, role, status, provider, provider_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID, user.Username, user.Email, user.FullName, user.Role, user.Status,
		user.Provider, user.ProviderID, user.CreatedAt, user.UpdatedAt,
	)
	if err != nil {
		// Detect unique constraint violations (07-REQ-2.4, 07-REQ-2.5).
		errStr := err.Error()
		if strings.Contains(errStr, "UNIQUE constraint failed: users.username") {
			return apikit.WriteAPIError(c, http.StatusConflict, "username already exists")
		}
		if strings.Contains(errStr, "UNIQUE constraint failed: users.provider") {
			return apikit.WriteAPIError(c, http.StatusConflict, "provider identity already exists")
		}
		// Any unexpected DB error (07-REQ-2.E1).
		return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	return c.JSON(http.StatusCreated, user)
}

// listUsers handles GET /users — lists all users, optionally including blocked.
// Requires admin access. By default only active users are returned; pass
// include_blocked=true to include blocked users. Returns an empty JSON array
// (not null) when no users match.
func (h *userHandlers) listUsers(c echo.Context) error {
	// Auth check: admin only (07-REQ-3.3, 07-PROP-5).
	if err := auth.RequireAdmin(c); err != nil {
		return apikit.WriteAPIError(c, http.StatusForbidden, "forbidden")
	}

	// Build query with optional status filter (07-REQ-3.1, 07-REQ-3.2).
	query := `SELECT id, username, email, COALESCE(full_name, '') AS full_name,
	          role, status, provider, provider_id, created_at, updated_at
	          FROM users`
	if c.QueryParam("include_blocked") != "true" {
		query += ` WHERE status = 'active'`
	}

	rows, err := h.db.Query(query)
	if err != nil {
		return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}
	defer rows.Close()

	// Scan rows into a non-nil slice (07-REQ-3.E1, 07-PROP-6).
	users := make([]User, 0)
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.FullName,
			&u.Role, &u.Status, &u.Provider, &u.ProviderID,
			&u.CreatedAt, &u.UpdatedAt); err != nil {
			return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	return c.JSON(http.StatusOK, users)
}

// getUser handles GET /users/:id — retrieves a single user by ID.
// Requires admin access. Sets the ETag header from user.UpdatedAt and
// supports conditional requests via If-None-Match (returns 304 on cache hit).
func (h *userHandlers) getUser(c echo.Context) error {
	// Auth check: admin only (07-REQ-4.3, 07-PROP-5).
	if err := auth.RequireAdmin(c); err != nil {
		return apikit.WriteAPIError(c, http.StatusForbidden, "forbidden")
	}

	id := c.Param("id")

	var user User
	err := h.db.QueryRow(
		`SELECT id, username, email, COALESCE(full_name, '') AS full_name,
		        role, status, provider, provider_id, created_at, updated_at
		 FROM users WHERE id = ?`, id,
	).Scan(&user.ID, &user.Username, &user.Email, &user.FullName,
		&user.Role, &user.Status, &user.Provider, &user.ProviderID,
		&user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return apikit.WriteAPIError(c, http.StatusNotFound, "user not found")
		}
		return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Parse UpdatedAt string to time.Time for ETag helpers (07-REQ-4.1).
	// SetETag/CheckETag accept time.Time, not string.
	updatedAt, err := db.ParseTime(user.UpdatedAt)
	if err != nil {
		return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	apikit.SetETag(c, updatedAt)

	// Check If-None-Match for conditional GET (07-REQ-4.E1).
	if apikit.CheckETag(c, updatedAt) {
		return c.NoContent(http.StatusNotModified)
	}

	return c.JSON(http.StatusOK, user)
}

// updateUser handles PATCH /users/:id — updates a user's full_name.
// Requires admin access. Uses UpdateUserRequest with FullName *string to
// distinguish a missing field (nil pointer → 400) from an empty string
// (clears the field).
func (h *userHandlers) updateUser(c echo.Context) error {
	// Auth check: admin only (07-REQ-5.4, 07-PROP-5).
	if err := auth.RequireAdmin(c); err != nil {
		return apikit.WriteAPIError(c, http.StatusForbidden, "forbidden")
	}

	id := c.Param("id")

	// Bind request body into UpdateUserRequest (07-REQ-5.2, 07-REQ-5.E2).
	var req UpdateUserRequest
	if err := c.Bind(&req); err != nil {
		return apikit.WriteAPIError(c, http.StatusBadRequest, "invalid request body")
	}

	// Check pointer: nil means field was absent → 400 (07-REQ-5.2).
	if req.FullName == nil {
		return apikit.WriteAPIError(c, http.StatusBadRequest, "missing required field: full_name")
	}

	// Verify the user exists (07-REQ-5.3).
	var exists int
	err := h.db.QueryRow("SELECT 1 FROM users WHERE id = ?", id).Scan(&exists)
	if err != nil {
		if err == sql.ErrNoRows {
			return apikit.WriteAPIError(c, http.StatusNotFound, "user not found")
		}
		return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Update full_name and updated_at (07-REQ-5.1, 07-REQ-5.E1).
	now := db.FormatTime(time.Now().UTC())
	_, err = h.db.Exec(
		`UPDATE users SET full_name = ?, updated_at = ? WHERE id = ?`,
		*req.FullName, now, id,
	)
	if err != nil {
		return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Fetch the updated user to return in the response.
	var user User
	err = h.db.QueryRow(
		`SELECT id, username, email, COALESCE(full_name, '') AS full_name,
		        role, status, provider, provider_id, created_at, updated_at
		 FROM users WHERE id = ?`, id,
	).Scan(&user.ID, &user.Username, &user.Email, &user.FullName,
		&user.Role, &user.Status, &user.Provider, &user.ProviderID,
		&user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	return c.JSON(http.StatusOK, user)
}

// promoteUser handles POST /users/:id/promote — sets user role to admin.
// Requires admin access. Idempotent: if the user already has role='admin',
// returns 200 with the existing user object without modifying the database.
func (h *userHandlers) promoteUser(c echo.Context) error {
	// Auth check: admin only (07-REQ-6.4, 07-PROP-5).
	if err := auth.RequireAdmin(c); err != nil {
		return apikit.WriteAPIError(c, http.StatusForbidden, "forbidden")
	}

	id := c.Param("id")

	// Fetch the target user (07-REQ-6.3).
	var user User
	err := h.db.QueryRow(
		`SELECT id, username, email, COALESCE(full_name, '') AS full_name,
		        role, status, provider, provider_id, created_at, updated_at
		 FROM users WHERE id = ?`, id,
	).Scan(&user.ID, &user.Username, &user.Email, &user.FullName,
		&user.Role, &user.Status, &user.Provider, &user.ProviderID,
		&user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return apikit.WriteAPIError(c, http.StatusNotFound, "user not found")
		}
		return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Idempotent: already admin → return unchanged (07-REQ-6.2).
	if user.Role == "admin" {
		return c.JSON(http.StatusOK, user)
	}

	// Update role to admin (07-REQ-6.1).
	now := db.FormatTime(time.Now().UTC())
	_, err = h.db.Exec(
		`UPDATE users SET role = 'admin', updated_at = ? WHERE id = ?`,
		now, id,
	)
	if err != nil {
		return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	user.Role = "admin"
	user.UpdatedAt = now

	return c.JSON(http.StatusOK, user)
}

// demoteUser handles POST /users/:id/demote — sets user role to user.
// Requires admin access. Idempotent: if the user already has role='user',
// returns 200 with the existing user object without modifying the database.
// Enforces the last-admin safeguard (07-PROP-1): refuses to demote the only
// remaining active admin, returning 409.
func (h *userHandlers) demoteUser(c echo.Context) error {
	// Auth check: admin only (07-REQ-7.5, 07-PROP-5).
	if err := auth.RequireAdmin(c); err != nil {
		return apikit.WriteAPIError(c, http.StatusForbidden, "forbidden")
	}

	id := c.Param("id")

	// Fetch the target user (07-REQ-7.4).
	var user User
	err := h.db.QueryRow(
		`SELECT id, username, email, COALESCE(full_name, '') AS full_name,
		        role, status, provider, provider_id, created_at, updated_at
		 FROM users WHERE id = ?`, id,
	).Scan(&user.ID, &user.Username, &user.Email, &user.FullName,
		&user.Role, &user.Status, &user.Provider, &user.ProviderID,
		&user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return apikit.WriteAPIError(c, http.StatusNotFound, "user not found")
		}
		return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Idempotent: already a regular user → return unchanged (07-REQ-7.2).
	if user.Role == "user" {
		return c.JSON(http.StatusOK, user)
	}

	// Count active admins for the last-admin safeguard (07-REQ-7.3, 07-PROP-1).
	var adminCount int
	err = h.db.QueryRow(
		`SELECT COUNT(*) FROM users WHERE role = 'admin' AND status = 'active'`,
	).Scan(&adminCount)
	if err != nil {
		// Unexpected DB error on COUNT query (07-REQ-7.E1).
		return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	if adminCount <= 1 {
		return apikit.WriteAPIError(c, http.StatusConflict, "cannot demote the last admin")
	}

	// Update role to user (07-REQ-7.1).
	now := db.FormatTime(time.Now().UTC())
	_, err = h.db.Exec(
		`UPDATE users SET role = 'user', updated_at = ? WHERE id = ?`,
		now, id,
	)
	if err != nil {
		return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	user.Role = "user"
	user.UpdatedAt = now

	return c.JSON(http.StatusOK, user)
}

// blockUser handles POST /users/:id/block — sets user status to blocked.
// Requires admin access. Idempotent: if the user already has status='blocked',
// returns 200 with the existing user object without modifying the database.
func (h *userHandlers) blockUser(c echo.Context) error {
	// Auth check: admin only (07-REQ-8.4, 07-PROP-5).
	if err := auth.RequireAdmin(c); err != nil {
		return apikit.WriteAPIError(c, http.StatusForbidden, "forbidden")
	}

	id := c.Param("id")

	// Fetch the target user (07-REQ-8.3).
	var user User
	err := h.db.QueryRow(
		`SELECT id, username, email, COALESCE(full_name, '') AS full_name,
		        role, status, provider, provider_id, created_at, updated_at
		 FROM users WHERE id = ?`, id,
	).Scan(&user.ID, &user.Username, &user.Email, &user.FullName,
		&user.Role, &user.Status, &user.Provider, &user.ProviderID,
		&user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return apikit.WriteAPIError(c, http.StatusNotFound, "user not found")
		}
		return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Idempotent: already blocked → return unchanged (07-REQ-8.2, 07-PROP-2).
	if user.Status == "blocked" {
		return c.JSON(http.StatusOK, user)
	}

	// Update status to blocked (07-REQ-8.1).
	now := db.FormatTime(time.Now().UTC())
	_, err = h.db.Exec(
		`UPDATE users SET status = 'blocked', updated_at = ? WHERE id = ?`,
		now, id,
	)
	if err != nil {
		return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	user.Status = "blocked"
	user.UpdatedAt = now

	return c.JSON(http.StatusOK, user)
}

// unblockUser handles POST /users/:id/unblock — sets user status to active.
// Requires admin access. Idempotent: if the user already has status='active',
// returns 200 with the existing user object without modifying the database.
func (h *userHandlers) unblockUser(c echo.Context) error {
	// Auth check: admin only (07-REQ-9.4, 07-PROP-5).
	if err := auth.RequireAdmin(c); err != nil {
		return apikit.WriteAPIError(c, http.StatusForbidden, "forbidden")
	}

	id := c.Param("id")

	// Fetch the target user (07-REQ-9.3).
	var user User
	err := h.db.QueryRow(
		`SELECT id, username, email, COALESCE(full_name, '') AS full_name,
		        role, status, provider, provider_id, created_at, updated_at
		 FROM users WHERE id = ?`, id,
	).Scan(&user.ID, &user.Username, &user.Email, &user.FullName,
		&user.Role, &user.Status, &user.Provider, &user.ProviderID,
		&user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return apikit.WriteAPIError(c, http.StatusNotFound, "user not found")
		}
		return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Idempotent: already active → return unchanged (07-REQ-9.2, 07-PROP-2).
	if user.Status == "active" {
		return c.JSON(http.StatusOK, user)
	}

	// Update status to active (07-REQ-9.1).
	now := db.FormatTime(time.Now().UTC())
	_, err = h.db.Exec(
		`UPDATE users SET status = 'active', updated_at = ? WHERE id = ?`,
		now, id,
	)
	if err != nil {
		return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	user.Status = "active"
	user.UpdatedAt = now

	return c.JSON(http.StatusOK, user)
}

// listUserKeys handles GET /users/:id/keys — lists API key metadata for a user.
// Requires admin access. Returns only metadata fields (key_id, user_id,
// created_at, expires_at, revoked_at); secret values are never included.
// Returns an empty JSON array (not null) when no keys exist for the user.
func (h *userHandlers) listUserKeys(c echo.Context) error {
	// Auth check: admin only (07-REQ-10.3, 07-PROP-5).
	if err := auth.RequireAdmin(c); err != nil {
		return apikit.WriteAPIError(c, http.StatusForbidden, "forbidden")
	}

	id := c.Param("id")

	// Verify the user exists before querying api_keys (07-REQ-10.2).
	var exists int
	err := h.db.QueryRow("SELECT 1 FROM users WHERE id = ?", id).Scan(&exists)
	if err != nil {
		if err == sql.ErrNoRows {
			return apikit.WriteAPIError(c, http.StatusNotFound, "user not found")
		}
		return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Query only metadata columns — never include secret_hash (07-REQ-10.E1, 07-PROP-4).
	rows, err := h.db.Query(
		`SELECT key_id, user_id, created_at, expires_at, revoked_at
		 FROM api_keys WHERE user_id = ?`, id,
	)
	if err != nil {
		return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}
	defer rows.Close()

	// Scan into a non-nil slice so JSON encodes as [] not null (07-PROP-6).
	keys := make([]APIKeyMeta, 0)
	for rows.Next() {
		var k APIKeyMeta
		if err := rows.Scan(&k.KeyID, &k.UserID, &k.CreatedAt, &k.ExpiresAt, &k.RevokedAt); err != nil {
			return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	return c.JSON(http.StatusOK, keys)
}

// revokeUserKey handles DELETE /users/:id/keys/:key_id — revokes an API key.
// Requires admin access. Sets revoked_at to the current UTC timestamp. If the
// key is already revoked (revoked_at != NULL), returns 204 without modifying
// the record (idempotent, 07-REQ-11.2, 07-PROP-3).
func (h *userHandlers) revokeUserKey(c echo.Context) error {
	// Auth check: admin only (07-REQ-11.4, 07-PROP-5).
	if err := auth.RequireAdmin(c); err != nil {
		return apikit.WriteAPIError(c, http.StatusForbidden, "forbidden")
	}

	id := c.Param("id")
	keyID := c.Param("key_id")

	// Fetch the key matching both key_id and user_id (07-REQ-11.3).
	var revokedAt sql.NullString
	err := h.db.QueryRow(
		`SELECT revoked_at FROM api_keys WHERE key_id = ? AND user_id = ?`,
		keyID, id,
	).Scan(&revokedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return apikit.WriteAPIError(c, http.StatusNotFound, "api key not found")
		}
		return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Idempotent: already revoked → return 204 without a write (07-REQ-11.2, 07-PROP-3).
	if revokedAt.Valid {
		return c.NoContent(http.StatusNoContent)
	}

	// Set revoked_at to the current UTC timestamp (07-REQ-11.1).
	now := db.FormatTime(time.Now().UTC())
	_, err = h.db.Exec(
		`UPDATE api_keys SET revoked_at = ? WHERE key_id = ? AND user_id = ?`,
		now, keyID, id,
	)
	if err != nil {
		return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	return c.NoContent(http.StatusNoContent)
}

// listUserTokens handles GET /users/:id/tokens — lists PAT metadata for a user.
// Requires admin access. Returns only metadata fields (token_id, name,
// permissions, user_id, created_at, expires_at, revoked_at); secret values
// are never included. Returns an empty JSON array (not null) when no tokens
// exist for the user.
func (h *userHandlers) listUserTokens(c echo.Context) error {
	// Auth check: admin only (07-REQ-12.3, 07-PROP-5).
	if err := auth.RequireAdmin(c); err != nil {
		return apikit.WriteAPIError(c, http.StatusForbidden, "forbidden")
	}

	id := c.Param("id")

	// Verify the user exists before querying pats (07-REQ-12.2).
	var exists int
	err := h.db.QueryRow("SELECT 1 FROM users WHERE id = ?", id).Scan(&exists)
	if err != nil {
		if err == sql.ErrNoRows {
			return apikit.WriteAPIError(c, http.StatusNotFound, "user not found")
		}
		return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Query only metadata columns — never include secret_hash (07-REQ-12.E1, 07-PROP-4).
	rows, err := h.db.Query(
		`SELECT token_id, name, permissions, user_id, created_at, expires_at, revoked_at
		 FROM pats WHERE user_id = ?`, id,
	)
	if err != nil {
		return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}
	defer rows.Close()

	// Scan into a non-nil slice so JSON encodes as [] not null (07-PROP-6).
	tokens := make([]PATMeta, 0)
	for rows.Next() {
		var t PATMeta
		var permsJSON string
		if err := rows.Scan(&t.TokenID, &t.Name, &permsJSON, &t.UserID,
			&t.CreatedAt, &t.ExpiresAt, &t.RevokedAt); err != nil {
			return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
		}
		// Parse permissions JSON array into []string.
		if err := json.Unmarshal([]byte(permsJSON), &t.Permissions); err != nil {
			return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
		}
		tokens = append(tokens, t)
	}
	if err := rows.Err(); err != nil {
		return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	return c.JSON(http.StatusOK, tokens)
}

// revokeUserToken handles DELETE /users/:id/tokens/:token_id — revokes a PAT.
// Requires admin access. Sets revoked_at to the current UTC timestamp. If the
// token is already revoked (revoked_at != NULL), returns 204 without modifying
// the record (idempotent, 07-REQ-13.2, 07-PROP-3).
func (h *userHandlers) revokeUserToken(c echo.Context) error {
	// Auth check: admin only (07-REQ-13.4, 07-PROP-5).
	if err := auth.RequireAdmin(c); err != nil {
		return apikit.WriteAPIError(c, http.StatusForbidden, "forbidden")
	}

	id := c.Param("id")
	tokenID := c.Param("token_id")

	// Fetch the token matching both token_id and user_id (07-REQ-13.3).
	var revokedAt sql.NullString
	err := h.db.QueryRow(
		`SELECT revoked_at FROM pats WHERE token_id = ? AND user_id = ?`,
		tokenID, id,
	).Scan(&revokedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return apikit.WriteAPIError(c, http.StatusNotFound, "token not found")
		}
		return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Idempotent: already revoked → return 204 without a write (07-REQ-13.2, 07-PROP-3).
	if revokedAt.Valid {
		return c.NoContent(http.StatusNoContent)
	}

	// Set revoked_at to the current UTC timestamp (07-REQ-13.1).
	now := db.FormatTime(time.Now().UTC())
	_, err = h.db.Exec(
		`UPDATE pats SET revoked_at = ? WHERE token_id = ? AND user_id = ?`,
		now, tokenID, id,
	)
	if err != nil {
		return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	return c.NoContent(http.StatusNoContent)
}

// getOwnProfile handles GET /user — retrieves the authenticated user's profile.
func (h *userHandlers) getOwnProfile(c echo.Context) error {
	return apikit.WriteAPIError(c, http.StatusNotImplemented, "not implemented")
}

// updateOwnProfile handles PATCH /user — updates the authenticated user's full_name.
func (h *userHandlers) updateOwnProfile(c echo.Context) error {
	return apikit.WriteAPIError(c, http.StatusNotImplemented, "not implemented")
}

// listOwnOrgs handles GET /user/orgs — lists the authenticated user's organizations.
func (h *userHandlers) listOwnOrgs(c echo.Context) error {
	return apikit.WriteAPIError(c, http.StatusNotImplemented, "not implemented")
}

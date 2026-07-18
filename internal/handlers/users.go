package handlers

import (
	"database/sql"

	"github.com/labstack/echo/v4"
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

// RegisterUserHandlers registers all user management routes on the provided
// Echo group and stores the *sql.DB handle for use by all handler functions.
func RegisterUserHandlers(g *echo.Group, db *sql.DB) {
	// Stub: not implemented.
}

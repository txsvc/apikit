package apikit

import "time"

// User represents an authenticated user in the system.
type User struct {
	ID         string     `json:"id"`
	Username   string     `json:"username"`
	Email      string     `json:"email"`
	FullName   string     `json:"full_name"`
	Status     string     `json:"status"`
	Role       string     `json:"role"`
	Provider   string     `json:"provider"`
	ProviderID string     `json:"provider_id"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	BlockedAt  *time.Time `json:"blocked_at,omitempty"`
}

// APIKeyMeta is the metadata-only API key object returned in listings.
type APIKeyMeta struct {
	KeyID     string     `json:"key_id"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at"`
}

// APIKeyFull is the full API key object returned on creation or refresh.
type APIKeyFull struct {
	Key       string     `json:"key"`
	KeyID     string     `json:"key_id"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// PAT is the metadata-only personal access token object.
type PAT struct {
	TokenID     string     `json:"token_id"`
	Name        string     `json:"name"`
	Permissions []string   `json:"permissions"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at"`
	RevokedAt   *time.Time `json:"revoked_at"`
}

// PATFull is the full personal access token object returned on creation.
type PATFull struct {
	Token       string     `json:"token"`
	TokenID     string     `json:"token_id"`
	Name        string     `json:"name"`
	Permissions []string   `json:"permissions"`
	ExpiresAt   *time.Time `json:"expires_at"`
}

// Organization represents an organization in the system.
type Organization struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Slug      string     `json:"slug"`
	URL       string     `json:"url,omitempty"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	BlockedAt *time.Time `json:"blocked_at,omitempty"`
}

// OAuthProvider is a provider discovery object returned by GET /auth/providers.
type OAuthProvider struct {
	Name         string `json:"name"`
	AuthorizeURL string `json:"authorize_url"`
}

// AuthCallbackRequest is the request body for POST /auth/callback.
type AuthCallbackRequest struct {
	Provider    string `json:"provider"`
	Code        string `json:"code"`
	RedirectURI string `json:"redirect_uri"`
	Expires     *int   `json:"expires,omitempty"`
}

// AuthCallbackResponse is the response body from POST /auth/callback.
type AuthCallbackResponse struct {
	User   *User       `json:"user"`
	APIKey *APIKeyFull `json:"api_key"`
}

// CreateUserRequest is the request body for creating a new user.
type CreateUserRequest struct {
	Username   string `json:"username"`
	Email      string `json:"email"`
	FullName   string `json:"full_name"`
	Role       string `json:"role"`
	Provider   string `json:"provider"`
	ProviderID string `json:"provider_id"`
}

// UpdateUserRequest is the request body for updating a user.
// FullName is a plain string (not *string) with no omitempty,
// so every PATCH request body always includes the full_name field.
type UpdateUserRequest struct {
	FullName string `json:"full_name"`
}

// CreateTokenRequest is the request body for creating a personal access token.
type CreateTokenRequest struct {
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
	Expires     *int     `json:"expires,omitempty"`
}

// CreateOrgRequest is the request body for creating an organization.
type CreateOrgRequest struct {
	Name string  `json:"name"`
	Slug string  `json:"slug"`
	URL  *string `json:"url,omitempty"`
}

// UpdateOrgRequest is the request body for updating an organization.
type UpdateOrgRequest struct {
	Name *string `json:"name,omitempty"`
	URL  *string `json:"url,omitempty"`
}

// HealthResponse is the response body from health probe endpoints.
type HealthResponse struct {
	Status string `json:"status"`
}

// VersionResponse is the response body from GET /version.
type VersionResponse struct {
	Version    string `json:"version"`
	BuildTime  string `json:"build_time"`
	Commit     string `json:"commit"`
	MountPoint string `json:"mount_point"`
}

// RevokeKeyResponse is the response body from DELETE /user/keys/:key_id.
type RevokeKeyResponse struct {
	KeyID     string    `json:"key_id"`
	RevokedAt time.Time `json:"revoked_at"`
}

// ListUsersOptions configures the ListUsers request.
// Designed to accommodate additional filter fields (e.g., pagination,
// search) in future iterations without breaking existing callers.
type ListUsersOptions struct {
	IncludeBlocked bool
}

// ListOrgsOptions configures the ListOrgs request.
// Designed to accommodate additional filter fields (e.g., pagination,
// search) in future iterations without breaking existing callers.
type ListOrgsOptions struct {
	IncludeBlocked bool
}

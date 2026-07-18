package auth

import (
	"context"

	"github.com/labstack/echo/v4"
)

// contextKey is an unexported string type used for context keys to avoid
// collisions with other packages. External packages cannot construct values
// of this type, preventing accidental overwrites or reads of AuthInfo.
type contextKey string

// authInfoKey is the context key used to store and retrieve AuthInfo in the
// request's context.Context. Because contextKey is unexported, only code in
// this package can construct the matching key.
const authInfoKey contextKey = "auth_info"

// AuthInfo carries authenticated credential information injected into the
// request context by the auth middleware.
type AuthInfo struct {
	CredentialType string
	UserID         string
	Role           string
	KeyID          string
	TokenID        string
	Permissions    []string
}

// setAuthInfoContext stores the AuthInfo in the request's context.Context
// using the unexported typed key. This ensures external packages cannot
// access the AuthInfo via a plain string key.
func setAuthInfoContext(c echo.Context, info *AuthInfo) {
	ctx := context.WithValue(c.Request().Context(), authInfoKey, info)
	c.SetRequest(c.Request().WithContext(ctx))
}

// GetAuthInfo retrieves the AuthInfo struct from the Echo request context.
// Returns nil if no AuthInfo has been injected (e.g. in tests without
// the middleware).
func GetAuthInfo(c echo.Context) *AuthInfo {
	val := c.Request().Context().Value(authInfoKey)
	if val == nil {
		return nil
	}
	auth, ok := val.(*AuthInfo)
	if !ok {
		return nil
	}
	return auth
}

// GetUserID returns the authenticated user's UUID string from the context
// AuthInfo, or an empty string if AuthInfo is nil or UserID is empty
// (admin token case).
func GetUserID(c echo.Context) string {
	auth := GetAuthInfo(c)
	if auth == nil {
		return ""
	}
	return auth.UserID
}

// IsAdmin returns true if the authenticated credential has admin-level access.
// Returns true only for admin tokens or admin-role API keys; returns false
// for PATs regardless of the user's role, and false when AuthInfo is nil.
//
// Per PRD intent (and critical reviewer finding): PATs with admin role must
// NOT be treated as admin-level to preserve PAT scoping.
func IsAdmin(c echo.Context) bool {
	auth := GetAuthInfo(c)
	if auth == nil {
		return false
	}
	return auth.CredentialType == "admin_token" ||
		(auth.CredentialType == "api_key" && auth.Role == "admin")
}

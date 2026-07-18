package auth

import (
	"github.com/labstack/echo/v4"

	"github.com/txsvc/apikit/internal/authctx"
)

// AuthInfo is a type alias for authctx.AuthInfo. The AuthInfo struct and
// context helpers are defined in internal/authctx to break the import cycle
// between internal/keys and internal/auth.
type AuthInfo = authctx.AuthInfo

// setAuthInfoContext stores the AuthInfo in the request's context.Context
// using the unexported typed key. This ensures external packages cannot
// access the AuthInfo via a plain string key.
func setAuthInfoContext(c echo.Context, info *AuthInfo) {
	authctx.SetAuthInfo(c, info)
}

// SetAuthInfo stores the AuthInfo in the request's context.Context, making it
// available via GetAuthInfo, IsAdmin, GetUserID, and the Require* helpers.
// This is the public complement to GetAuthInfo; the auth middleware calls it
// internally, and test code can call it to inject auth state without running
// the full middleware stack.
func SetAuthInfo(c echo.Context, info *AuthInfo) {
	authctx.SetAuthInfo(c, info)
}

// GetAuthInfo retrieves the AuthInfo struct from the Echo request context.
// Returns nil if no AuthInfo has been injected (e.g. in tests without
// the middleware).
func GetAuthInfo(c echo.Context) *AuthInfo {
	return authctx.GetAuthInfo(c)
}

// GetUserID returns the authenticated user's UUID string from the context
// AuthInfo, or an empty string if AuthInfo is nil or UserID is empty
// (admin token case).
func GetUserID(c echo.Context) string {
	return authctx.GetUserID(c)
}

// IsAdmin returns true if the authenticated credential has admin-level access.
// Returns true only for admin tokens or admin-role API keys; returns false
// for PATs regardless of the user's role, and false when AuthInfo is nil.
//
// Per PRD intent (and critical reviewer finding): PATs with admin role must
// NOT be treated as admin-level to preserve PAT scoping.
func IsAdmin(c echo.Context) bool {
	info := GetAuthInfo(c)
	if info == nil {
		return false
	}
	return info.CredentialType == "admin_token" ||
		(info.CredentialType == "api_key" && info.Role == "admin")
}

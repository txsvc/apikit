// Package authctx provides shared authentication context types and helpers.
//
// This package exists to break the import cycle between internal/keys and
// internal/auth: apikit imports internal/keys, internal/auth imports apikit,
// so internal/keys cannot import internal/auth. Both packages import this
// small shared package instead.
package authctx

import (
	"context"

	"github.com/labstack/echo/v4"
)

// contextKey is an unexported string type used for context keys to avoid
// collisions with other packages. External packages cannot construct values
// of this type, preventing accidental overwrites or reads of AuthInfo.
type contextKey string

// authInfoKey is the context key used to store and retrieve AuthInfo in the
// request's context.Context.
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

// SetAuthInfo stores the AuthInfo in the request's context.Context, making it
// available via GetAuthInfo, GetUserID, and the Require* helpers in internal/auth.
func SetAuthInfo(c echo.Context, info *AuthInfo) {
	ctx := context.WithValue(c.Request().Context(), authInfoKey, info)
	c.SetRequest(c.Request().WithContext(ctx))
}

// GetAuthInfo retrieves the AuthInfo struct from the Echo request context.
// Returns nil if no AuthInfo has been injected.
func GetAuthInfo(c echo.Context) *AuthInfo {
	val := c.Request().Context().Value(authInfoKey)
	if val == nil {
		return nil
	}
	info, ok := val.(*AuthInfo)
	if !ok {
		return nil
	}
	return info
}

// GetUserID returns the authenticated user's UUID string from the context
// AuthInfo, or an empty string if AuthInfo is nil or UserID is empty.
func GetUserID(c echo.Context) string {
	info := GetAuthInfo(c)
	if info == nil {
		return ""
	}
	return info.UserID
}

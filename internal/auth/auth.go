package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/txsvc/apikit"
	"github.com/txsvc/apikit/internal/db"
)

// NewAuthMiddleware creates the Echo middleware function for authentication
// and authorization. Panics immediately with a descriptive message if either
// argument is nil, rather than returning a partially-constructed middleware
// that fails at request time (05-REQ-1.E1).
//
// The returned middleware is applied to the APIGroup Echo group, not the root
// router, so that health probes (/healthz, /readyz, /version) and OAuth paths
// (/auth/providers, /auth/callback) remain unprotected (05-REQ-1.2, 05-REQ-1.3).
func NewAuthMiddleware(database *db.DB, registry *PermissionRegistry) echo.MiddlewareFunc {
	if database == nil {
		panic("auth: NewAuthMiddleware requires a non-nil *db.DB")
	}
	if registry == nil {
		panic("auth: NewAuthMiddleware requires a non-nil *PermissionRegistry")
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Step 1: Extract Bearer token from Authorization header.
			header := c.Request().Header.Get("Authorization")
			if header == "" {
				return apikit.APIError(c, http.StatusUnauthorized, "missing authorization header")
			}

			if !strings.HasPrefix(header, "Bearer ") {
				return apikit.APIError(c, http.StatusUnauthorized, "invalid authorization header format")
			}

			token := header[len("Bearer "):]
			if token == "" {
				return apikit.APIError(c, http.StatusUnauthorized, "missing token")
			}

			// Step 2: Detect credential type.
			credType, components, err := parseToken(token)
			if err != nil {
				return apikit.APIError(c, http.StatusUnauthorized, "unrecognized token format")
			}

			// Step 3: Dispatch to credential-type-specific validation.
			var authInfo *AuthInfo
			var validationErr error

			switch credType {
			case "admin_token":
				// Extract hex suffix after '<prefix>_admin_'.
				adminPrefix := apikit.TokenPrefix + "_admin_"
				hexSuffix := token[len(adminPrefix):]
				authInfo, validationErr = validateAdminToken(database, token, hexSuffix)
			case "api_key":
				// components[0] = key_id, components[1] = secret.
				authInfo, validationErr = validateAPIKey(database, components[0], components[1])
			case "pat":
				// components[0] = token_id, components[1] = secret.
				authInfo, validationErr = validatePAT(database, components[0], components[1])
			default:
				return apikit.APIError(c, http.StatusUnauthorized, "unrecognized token format")
			}

			if validationErr != nil {
				if ae, ok := validationErr.(*authError); ok {
					return apikit.APIError(c, ae.Code, ae.Message)
				}
				return apikit.APIError(c, http.StatusInternalServerError, "internal server error")
			}

			// Step 4: Inject AuthInfo into request context and call next handler.
			setAuthInfoContext(c, authInfo)
			return next(c)
		}
	}
}

// parseToken extracts the credential type and components from a raw Bearer
// token string. It checks for the admin pattern first, then PAT, then falls
// back to the API key pattern, using apikit.TokenPrefix for the prefix value.
//
// Returns the credential type ("admin_token", "api_key", or "pat"), the
// parsed components, and a non-nil error if the token is unrecognized.
//
// Note: parseToken classifies admin tokens by prefix alone. Hex suffix
// validation is deferred to validateAdminToken (05-REQ-4.E2).
func parseToken(token string) (string, []string, error) {
	prefix := apikit.TokenPrefix

	// Check 1: Admin token — prefix_admin_<anything>
	adminPrefix := prefix + "_admin_"
	if strings.HasPrefix(token, adminPrefix) {
		// Return the full token as the sole component; hex validation
		// is handled by validateAdminToken.
		return "admin_token", []string{token}, nil
	}

	// Check 2: PAT — prefix_pat_<token_id>_<secret>
	patPrefix := prefix + "_pat_"
	if strings.HasPrefix(token, patPrefix) {
		remainder := token[len(patPrefix):]
		// Split on the first underscore to get token_id and secret.
		idx := strings.Index(remainder, "_")
		if idx <= 0 || idx == len(remainder)-1 {
			return "", nil, errors.New("invalid PAT format: expected <token_id>_<secret>")
		}
		tokenID := remainder[:idx]
		secret := remainder[idx+1:]
		return "pat", []string{tokenID, secret}, nil
	}

	// Check 3: API key — prefix_<key_id>_<secret>
	keyPrefix := prefix + "_"
	if strings.HasPrefix(token, keyPrefix) {
		remainder := token[len(keyPrefix):]
		// Split on the first underscore to get key_id and secret.
		idx := strings.Index(remainder, "_")
		if idx <= 0 || idx == len(remainder)-1 {
			return "", nil, errors.New("invalid API key format: expected <key_id>_<secret>")
		}
		keyID := remainder[:idx]
		secret := remainder[idx+1:]
		return "api_key", []string{keyID, secret}, nil
	}

	return "", nil, errors.New("unrecognized token format")
}

// hashToken computes SHA-256 of the input and returns the result as a
// 64-character lowercase hex-encoded string. Used by all credential
// validation functions. Both computed and stored hashes must be converted
// to []byte before passing to subtle.ConstantTimeCompare.
func hashToken(input string) string {
	h := sha256.Sum256([]byte(input))
	return hex.EncodeToString(h[:])
}

// RequireAdmin returns HTTP 403 with "forbidden" if the authenticated credential
// does not have admin-level access. Returns nil if the credential is admin-level
// (admin token or admin-role API key).
//
// When no AuthInfo is present in the context (GetAuthInfo returns nil), treats
// the request as unauthenticated and returns HTTP 403 "forbidden" (05-REQ-7.E1).
func RequireAdmin(c echo.Context) error {
	if !IsAdmin(c) {
		return echo.NewHTTPError(http.StatusForbidden, "forbidden")
	}
	return nil
}

// RequireOwnerOrAdmin returns HTTP 403 with "forbidden" if the authenticated
// user is neither the resource owner (matching resourceOwnerID) nor an admin.
// Returns nil if the user is the owner or has admin-level access.
//
// An empty resourceOwnerID is treated as non-matching (it does not equal any
// valid UUID), so the check falls through to the admin test (05-REQ-7.E2).
func RequireOwnerOrAdmin(c echo.Context, resourceOwnerID string) error {
	if resourceOwnerID != "" && GetUserID(c) == resourceOwnerID {
		return nil
	}
	return RequireAdmin(c)
}

// RequirePermission returns HTTP 403 with "insufficient permissions" if a PAT
// credential lacks the specified resource_type:action permission. For admin
// tokens and API keys, returns nil without checking permissions (implicit full
// access for their access level). Returns nil if the PAT has the required
// permission (05-REQ-7.5, 05-REQ-7.6).
//
// When no AuthInfo is present in the context, returns HTTP 403 "forbidden"
// to prevent fail-open behavior on unprotected routes (addresses major
// reviewer finding about nil AuthInfo).
func RequirePermission(c echo.Context, resourceType, action string) error {
	auth := GetAuthInfo(c)
	if auth == nil {
		return echo.NewHTTPError(http.StatusForbidden, "forbidden")
	}

	// Admin tokens and API keys carry implicit full permissions for their
	// access level — bypass PAT permission check (05-REQ-7.5).
	if auth.CredentialType == "admin_token" || auth.CredentialType == "api_key" {
		return nil
	}

	// PAT credentials must have the specific permission (05-REQ-7.6, 05-REQ-7.7).
	required := resourceType + ":" + action
	for _, p := range auth.Permissions {
		if p == required {
			return nil
		}
	}
	return echo.NewHTTPError(http.StatusForbidden, "insufficient permissions")
}

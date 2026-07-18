package oauth

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/txsvc/apikit/internal/db"
)

// callbackRequest represents the JSON body of POST /auth/callback.
type callbackRequest struct {
	Provider    string `json:"provider"`
	Code        string `json:"code"`
	RedirectURI string `json:"redirect_uri"`
	Expires     *int   `json:"expires"`
}

// RegisterOAuthHandlers mounts the OAuth endpoints on the given Echo group:
//   - GET  /auth/providers  — lists configured providers (cached 5 min)
//   - POST /auth/callback   — exchanges an authorization code for a user + API key
func RegisterOAuthHandlers(group *echo.Group, registry *Registry, database *db.DB, externalURL string) {
	group.GET("/auth/providers", handleProviders(registry), cachePublicMiddleware)
	group.POST("/auth/callback", handleCallback(registry, database, externalURL))
}

// cachePublicMiddleware sets Cache-Control: public, max-age=300 on the response.
func cachePublicMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		c.Response().Header().Set("Cache-Control", "public, max-age=300")
		return next(c)
	}
}

// handleProviders returns an Echo handler for GET /auth/providers.
// TODO: implement full response in a later task group.
func handleProviders(registry *Registry) echo.HandlerFunc {
	return func(c echo.Context) error {
		_ = registry
		return c.JSON(http.StatusOK, []any{})
	}
}

// handleCallback returns an Echo handler for POST /auth/callback.
// Currently implements request validation only; the full OAuth flow
// (exchange, userinfo, upsert, key generation) is implemented in later
// task groups.
func handleCallback(registry *Registry, database *db.DB, externalURL string) echo.HandlerFunc {
	return func(c echo.Context) error {
		var req callbackRequest
		if err := c.Bind(&req); err != nil {
			return oauthError(c, http.StatusBadRequest, err.Error())
		}

		// Validate required fields.
		if req.Provider == "" {
			return oauthError(c, http.StatusBadRequest, "provider is required")
		}
		if req.Code == "" {
			return oauthError(c, http.StatusBadRequest, "code is required")
		}
		if req.RedirectURI == "" {
			return oauthError(c, http.StatusBadRequest, "redirect_uri is required")
		}

		// Validate and default expires.
		expires := 90
		if req.Expires != nil {
			expires = *req.Expires
			switch expires {
			case 0, 30, 60, 90:
				// valid
			default:
				return oauthError(c, http.StatusBadRequest, "expires must be 0, 30, 60, or 90")
			}
		}

		// Provider lookup.
		provider := registry.Get(req.Provider)
		if provider == nil {
			return oauthError(c, http.StatusBadRequest, "unknown provider: "+req.Provider)
		}

		// Redirect URI validation.
		if err := ValidateRedirectURI(req.RedirectURI, externalURL); err != nil {
			return oauthError(c, http.StatusBadRequest, "redirect_uri is not allowed")
		}

		// TODO: implement Exchange, UserInfo, DB operations in later task groups.
		_ = expires
		_ = database

		return c.JSON(http.StatusOK, nil)
	}
}

// oauthError writes a standard JSON error response envelope.
// Produces the same format as the root apikit.APIError function:
//
//	{"error": {"code": <int>, "message": "<string>"}}
//
// Defined locally to avoid a circular import with the root apikit package.
func oauthError(c echo.Context, code int, message string) error {
	type detail struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	type envelope struct {
		Error detail `json:"error"`
	}
	return c.JSON(code, envelope{
		Error: detail{Code: code, Message: message},
	})
}

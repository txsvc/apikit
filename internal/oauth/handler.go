package oauth

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/txsvc/apikit/internal/bootstrap"
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

// defaultTokenPrefix is the API key prefix used when constructing the full key
// string. Matches the root apikit.TokenPrefix default ("ak"), defined here to
// avoid a circular import from internal/oauth → root apikit.
const defaultTokenPrefix = "ak"

// handleCallback returns an Echo handler for POST /auth/callback.
// It implements the full OAuth callback flow:
//  1. Parse and validate request body
//  2. Look up the provider in the registry
//  3. Validate redirect URI against the allowlist
//  4. Exchange the authorization code for an access token
//  5. Retrieve user info from the identity provider
//  6. Upsert user record and generate API key within a single transaction
//  7. Return the user object and API key in the response
func handleCallback(registry *Registry, database *db.DB, externalURL string) echo.HandlerFunc {
	return func(c echo.Context) error {
		// --- 8.1: Request parsing and validation ---

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

		// --- 8.2: Redirect URI validation, code exchange, user info ---

		// Redirect URI validation.
		if err := ValidateRedirectURI(req.RedirectURI, externalURL); err != nil {
			return oauthError(c, http.StatusBadRequest, "redirect_uri is not allowed")
		}

		// Exchange authorization code for access token.
		ctx := c.Request().Context()
		accessToken, err := provider.Exchange(ctx, req.Code, req.RedirectURI)
		if err != nil {
			return oauthError(c, http.StatusUnauthorized, "authorization code exchange failed")
		}

		// Retrieve user info from the identity provider.
		userInfo, err := provider.UserInfo(ctx, accessToken)
		if err != nil {
			return oauthError(c, http.StatusBadGateway, "failed to retrieve user info from provider")
		}
		// Guard against provider returning nil without error.
		if userInfo == nil {
			return oauthError(c, http.StatusBadGateway, "failed to retrieve user info from provider")
		}

		// Validate email is non-empty.
		if userInfo.Email == "" {
			return oauthError(c, http.StatusBadRequest, "provider returned empty email; email is required")
		}

		// --- 8.3 & 8.4: User upsert, key revocation, key generation in a single transaction ---

		// Capture the current time for consistent timestamps within the transaction.
		now := time.Now().UTC()
		nowStr := db.FormatTime(now)

		// These variables capture data from the transaction for the response.
		var (
			userID      string
			username    string
			email       string
			fullName    *string
			status      string
			role        string
			providerStr string
			providerID  string
			createdAt   string
			updatedAt   string
			apiKeyRes   *APIKeyResult
		)

		txErr := database.WithTx(ctx, func(tx *sql.Tx) error {
			// Query for existing user by (provider, provider_id).
			var existingID, existingUsername, existingEmail, existingRole, existingStatus string
			var existingFullName *string
			var existingCreatedAt, existingUpdatedAt string

			err := tx.QueryRowContext(ctx,
				`SELECT id, username, email, full_name, role, status, created_at, updated_at
				 FROM users WHERE provider = ? AND provider_id = ?`,
				req.Provider, userInfo.ProviderID,
			).Scan(&existingID, &existingUsername, &existingEmail, &existingFullName,
				&existingRole, &existingStatus, &existingCreatedAt, &existingUpdatedAt)

			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				// Unexpected DB error.
				return err
			}

			if err == nil {
				// Existing user found.
				if existingStatus == "blocked" {
					return errUserBlocked
				}

				// Update username, email, updated_at for active user.
				_, updateErr := tx.ExecContext(ctx,
					`UPDATE users SET username = ?, email = ?, updated_at = ? WHERE id = ?`,
					userInfo.Username, userInfo.Email, nowStr, existingID,
				)
				if updateErr != nil {
					return updateErr
				}

				// Populate response data from existing record (updated fields).
				userID = existingID
				username = userInfo.Username
				email = userInfo.Email
				fullName = existingFullName
				status = existingStatus
				role = existingRole
				providerStr = req.Provider
				providerID = userInfo.ProviderID
				createdAt = existingCreatedAt
				updatedAt = nowStr
			} else {
				// New user — determine role.
				newRole := "user"

				promote, promoteErr := bootstrap.ShouldAutoPromote(ctx, tx, userInfo.Email)
				if promoteErr != nil {
					return promoteErr
				}
				if promote {
					var adminCount int
					countErr := tx.QueryRowContext(ctx,
						`SELECT COUNT(*) FROM users WHERE role = 'admin'`,
					).Scan(&adminCount)
					if countErr != nil {
						return countErr
					}
					if adminCount == 0 {
						newRole = "admin"
					}
				}

				// Insert new user.
				newID := uuid.New().String()
				_, insertErr := tx.ExecContext(ctx,
					`INSERT INTO users (id, username, email, full_name, role, status, provider, provider_id, created_at, updated_at)
					 VALUES (?, ?, ?, NULL, ?, 'active', ?, ?, ?, ?)`,
					newID, userInfo.Username, userInfo.Email, newRole,
					req.Provider, userInfo.ProviderID, nowStr, nowStr,
				)
				if insertErr != nil {
					return insertErr
				}

				// Populate response data.
				userID = newID
				username = userInfo.Username
				email = userInfo.Email
				fullName = nil
				status = "active"
				role = newRole
				providerStr = req.Provider
				providerID = userInfo.ProviderID
				createdAt = nowStr
				updatedAt = nowStr
			}

			// --- 8.4: Key revocation and new key generation ---

			// Revoke all active (non-revoked) keys for this user.
			_, revokeErr := tx.ExecContext(ctx,
				`UPDATE api_keys SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`,
				nowStr, userID,
			)
			if revokeErr != nil {
				return revokeErr
			}

			// Generate new API key material.
			apiKeyRes, err = GenerateAPIKey(defaultTokenPrefix, expires)
			if err != nil {
				return err
			}

			// Compute expires_at for storage.
			var expiresAtStr *string
			if apiKeyRes.ExpiresAt != nil {
				s := db.FormatTime(*apiKeyRes.ExpiresAt)
				expiresAtStr = &s
			}

			// Insert new API key record.
			// CRITICAL: api_keys uses key_id as PK (no separate id column).
			// CRITICAL: expires_days (INTEGER NOT NULL) must be included.
			_, keyInsertErr := tx.ExecContext(ctx,
				`INSERT INTO api_keys (key_id, user_id, secret_hash, expires_days, expires_at, created_at)
				 VALUES (?, ?, ?, ?, ?, ?)`,
				apiKeyRes.KeyID, userID, apiKeyRes.SecretHash,
				expires, expiresAtStr, nowStr,
			)
			if keyInsertErr != nil {
				return keyInsertErr
			}

			return nil
		})

		if txErr != nil {
			// Check for blocked user sentinel.
			if errors.Is(txErr, errUserBlocked) {
				return oauthError(c, http.StatusForbidden, "user is blocked")
			}
			// Wrap and return as internal server error.
			_ = db.WrapError(txErr)
			return oauthError(c, http.StatusInternalServerError, "internal server error")
		}

		// --- 8.5: Build and return success response ---

		var expiresAtResp *string
		if apiKeyRes.ExpiresAt != nil {
			s := db.FormatTime(*apiKeyRes.ExpiresAt)
			expiresAtResp = &s
		}

		resp := callbackResponse{
			User: callbackUser{
				ID:         userID,
				Username:   username,
				Email:      email,
				FullName:   fullName,
				Status:     status,
				Role:       role,
				Provider:   providerStr,
				ProviderID: providerID,
				CreatedAt:  createdAt,
				UpdatedAt:  updatedAt,
			},
			APIKey: callbackAPIKey{
				Key:       apiKeyRes.FullKey,
				KeyID:     apiKeyRes.KeyID,
				ExpiresAt: expiresAtResp,
			},
		}

		return c.JSON(http.StatusOK, resp)
	}
}

// errUserBlocked is a sentinel error used within the db.WithTx callback to
// signal that the matched user has status='blocked'. The transaction is rolled
// back and the handler returns HTTP 403.
var errUserBlocked = errors.New("user is blocked")

// oauthError writes a standard JSON error response envelope.
// Produces the same format as the root apikit.WriteAPIError function:
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

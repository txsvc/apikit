package keys

import (
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/txsvc/apikit/internal/authctx"
	"github.com/txsvc/apikit/internal/db"
)

// keyMetadata is the JSON-serializable representation of an API key record
// returned by the list endpoint. Only safe metadata fields are included;
// secret_hash, expires_days, and the plaintext secret are never exposed.
type keyMetadata struct {
	KeyID     string  `json:"key_id"`
	CreatedAt string  `json:"created_at"`
	ExpiresAt *string `json:"expires_at"`
	RevokedAt *string `json:"revoked_at"`
}

// keyHandlers holds the database handle shared by all API key lifecycle
// handler functions. Methods on this struct are unexported; only
// RegisterKeyHandlers is exported.
type keyHandlers struct {
	db *sql.DB
}

// RegisterKeyHandlers registers the three API key lifecycle endpoints on the
// provided Echo group:
//   - GET /user/keys
//   - POST /user/keys/:key_id/refresh
//   - DELETE /user/keys/:key_id
//
// The group is expected to be mounted at /api/v1 with CacheMiddleware(CacheNoStore)
// already applied by the parent, so all three endpoints inherit Cache-Control: no-store.
func RegisterKeyHandlers(group *echo.Group, database *sql.DB) {
	h := &keyHandlers{db: database}

	group.GET("/user/keys", h.listKeys)
	group.POST("/user/keys/:key_id/refresh", h.refreshKey)
	group.DELETE("/user/keys/:key_id", h.revokeKey)
}

// keysETagValue computes a weak ETag string from a timestamp and a revocation
// count. Including the revocation count ensures the ETag changes when a key is
// revoked even if the revocation happens in the same second as the key's
// creation (db.FormatTime truncates to whole seconds).
func keysETagValue(ts time.Time, revokedCount int) string {
	return fmt.Sprintf(`W/"%s-%d"`, ts.UTC().Format(time.RFC3339), revokedCount)
}

// setKeysETag sets the ETag response header for the list-keys endpoint.
func setKeysETag(c echo.Context, ts time.Time, revokedCount int) {
	if ts.IsZero() {
		return
	}
	c.Response().Header().Set("ETag", keysETagValue(ts, revokedCount))
}

// checkKeysETag compares the If-None-Match request header against the
// computed ETag. Returns true if they match (client cache is current).
func checkKeysETag(c echo.Context, ts time.Time, revokedCount int) bool {
	if ts.IsZero() {
		return false
	}
	inm := c.Request().Header.Get("If-None-Match")
	return inm == keysETagValue(ts, revokedCount)
}

// writeAPIError writes a standard JSON error response envelope to the response.
// Format: {"error": {"code": <integer>, "message": "<string>"}}
// Mirrors apikit.WriteAPIError — inlined here to avoid a circular import.
func writeAPIError(c echo.Context, code int, message string) error {
	c.Response().Header().Set("Content-Type", "application/json; charset=utf-8")
	return c.JSON(code, map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

// listKeys handles GET /user/keys — lists all API key metadata for the
// authenticated user, ordered by created_at DESC, with ETag caching support.
func (h *keyHandlers) listKeys(c echo.Context) error {
	userID := authctx.GetUserID(c)

	// Step 1: ETag derivation query.
	// Compute etag_ts as MAX(MAX(created_at, COALESCE(revoked_at, created_at)))
	// across all user keys. Also count revoked keys to ensure the ETag changes
	// on revocation even if it occurs within the same truncated second.
	// Scan etag_ts as sql.NullString to handle the NULL case (no keys).
	var etagTS sql.NullString
	var revokedCount int
	err := h.db.QueryRow(
		`SELECT MAX(MAX(created_at, COALESCE(revoked_at, created_at))),
		        COUNT(CASE WHEN revoked_at IS NOT NULL THEN 1 END)
		 FROM api_keys WHERE user_id = ?`,
		userID,
	).Scan(&etagTS, &revokedCount)
	if err != nil {
		return writeAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Step 2: If we have keys, set ETag and check If-None-Match.
	if etagTS.Valid {
		// Parse the DB timestamp string back to time.Time for ETag computation.
		// The spec prescribes scanning as NullString and reformatting via FormatUTC.
		ts, parseErr := db.ParseTime(etagTS.String)
		if parseErr != nil {
			return writeAPIError(c, http.StatusInternalServerError, "internal server error")
		}
		setKeysETag(c, ts, revokedCount)
		if checkKeysETag(c, ts, revokedCount) {
			return c.NoContent(http.StatusNotModified)
		}
	}

	// Step 3: Query all key metadata for this user.
	rows, err := h.db.Query(
		`SELECT key_id, created_at, expires_at, revoked_at
		 FROM api_keys WHERE user_id = ? ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return writeAPIError(c, http.StatusInternalServerError, "internal server error")
	}
	defer rows.Close()

	// Step 4: Build the response array. Use an initialized empty slice
	// so JSON serialization produces [] instead of null.
	result := []keyMetadata{}
	for rows.Next() {
		var m keyMetadata
		var expiresAt sql.NullString
		var revokedAt sql.NullString

		if err := rows.Scan(&m.KeyID, &m.CreatedAt, &expiresAt, &revokedAt); err != nil {
			return writeAPIError(c, http.StatusInternalServerError, "internal server error")
		}

		// Format timestamps to canonical RFC 3339 UTC output.
		// created_at is NOT NULL so it always has a value.
		if ct, parseErr := db.ParseTime(m.CreatedAt); parseErr == nil {
			m.CreatedAt = ct.UTC().Format(time.RFC3339)
		}

		if expiresAt.Valid {
			formatted := expiresAt.String
			if et, parseErr := db.ParseTime(expiresAt.String); parseErr == nil {
				formatted = et.UTC().Format(time.RFC3339)
			}
			m.ExpiresAt = &formatted
		}

		if revokedAt.Valid {
			formatted := revokedAt.String
			if rt, parseErr := db.ParseTime(revokedAt.String); parseErr == nil {
				formatted = rt.UTC().Format(time.RFC3339)
			}
			m.RevokedAt = &formatted
		}

		result = append(result, m)
	}
	if err := rows.Err(); err != nil {
		return writeAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	return c.JSON(http.StatusOK, result)
}

// refreshKey handles POST /user/keys/:key_id/refresh — generates a new secret
// for an existing, non-revoked, non-expired key.
func (h *keyHandlers) refreshKey(c echo.Context) error {
	return c.JSON(http.StatusInternalServerError, map[string]string{"error": "not implemented"})
}

// revokeKey handles DELETE /user/keys/:key_id — permanently revokes a key.
func (h *keyHandlers) revokeKey(c echo.Context) error {
	return c.JSON(http.StatusInternalServerError, map[string]string{"error": "not implemented"})
}

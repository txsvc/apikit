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
	// Step 1: Check credential type — PAT authentication is rejected.
	info := authctx.GetAuthInfo(c)
	if info != nil && info.CredentialType == "pat" {
		return writeAPIError(c, http.StatusUnauthorized, "API key authentication required")
	}

	userID := authctx.GetUserID(c)
	keyID := c.Param("key_id")

	// Step 2: Look up the key record.
	var dbUserID string
	var dbSecretHash string
	var dbExpiresDays int
	var dbExpiresAt sql.NullString
	var dbRevokedAt sql.NullString
	var dbCreatedAt string

	err := h.db.QueryRow(
		`SELECT key_id, user_id, secret_hash, expires_days, expires_at, revoked_at, created_at
		 FROM api_keys WHERE key_id = ?`,
		keyID,
	).Scan(&keyID, &dbUserID, &dbSecretHash, &dbExpiresDays, &dbExpiresAt, &dbRevokedAt, &dbCreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return writeAPIError(c, http.StatusNotFound, "key not found")
		}
		return writeAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Step 3: Ownership check — return 404 (not 403) to prevent information leakage.
	if dbUserID != userID {
		return writeAPIError(c, http.StatusNotFound, "key not found")
	}

	// Step 4: Check revocation status.
	if dbRevokedAt.Valid {
		return writeAPIError(c, http.StatusBadRequest, "cannot refresh a revoked key")
	}

	// Step 5: Check expiry status.
	if dbExpiresAt.Valid {
		expiresAtTime, parseErr := db.ParseTime(dbExpiresAt.String)
		if parseErr != nil {
			return writeAPIError(c, http.StatusInternalServerError, "internal server error")
		}
		if expiresAtTime.Before(time.Now().UTC()) {
			return writeAPIError(c, http.StatusBadRequest, "cannot refresh an expired key")
		}
	}

	// Step 6: Generate new secret.
	newSecret, err := randAlphanumeric(secretLen)
	if err != nil {
		return writeAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	newSecretHash := hashSecret(newSecret)

	// Step 7: Calculate new expires_at from stored expires_days.
	now := time.Now().UTC()
	// Use RFC3339Nano for created_at to preserve sub-second precision.
	// db.FormatTime truncates to whole seconds, but the refresh flow needs
	// the stored timestamp to faithfully represent the current instant.
	createdAt := now.Format(time.RFC3339Nano)

	var newExpiresAtDB any   // value for DB column (string or nil)
	var newExpiresAt *string // value for JSON response
	if dbExpiresDays > 0 {
		ea := now.Add(time.Duration(dbExpiresDays) * 24 * time.Hour)
		eaStr := db.FormatTime(ea)
		newExpiresAtDB = eaStr
		// Format for JSON response as RFC 3339.
		eaFormatted := ea.UTC().Truncate(time.Second).Format(time.RFC3339)
		newExpiresAt = &eaFormatted
	}

	// Step 8: UPDATE the key record.
	result, err := h.db.Exec(
		`UPDATE api_keys SET secret_hash = ?, expires_at = ?, created_at = ?
		 WHERE key_id = ? AND user_id = ?`,
		newSecretHash, newExpiresAtDB, createdAt, keyID, userID,
	)
	if err != nil {
		return writeAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Step 9: Check rows affected — 0 means race condition (key deleted between SELECT and UPDATE).
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return writeAPIError(c, http.StatusInternalServerError, "internal server error")
	}
	if rowsAffected == 0 {
		return writeAPIError(c, http.StatusNotFound, "key not found")
	}

	// Step 10: Construct full key and log.
	fullKey := fmt.Sprintf("%s_%s_%s", defaultTokenPrefix, keyID, newSecret)
	c.Logger().Infof("api_key_refreshed user_id=%s key_id=%s", userID, keyID)

	// Step 11: Return success response.
	return c.JSON(http.StatusOK, map[string]any{
		"key":        fullKey,
		"key_id":     keyID,
		"expires_at": newExpiresAt,
	})
}

// revokeKey handles DELETE /user/keys/:key_id — permanently revokes a key.
// Accepts authentication via API key or PAT with keys:manage permission
// (enforced by auth middleware). Self-revocation is permitted because the auth
// middleware validates the credential before handler execution and does not
// re-validate mid-flight.
func (h *keyHandlers) revokeKey(c echo.Context) error {
	userID := authctx.GetUserID(c)
	keyID := c.Param("key_id")

	// Step 1: Look up the key record.
	var dbUserID string
	var dbRevokedAt sql.NullString

	err := h.db.QueryRow(
		`SELECT key_id, user_id, revoked_at FROM api_keys WHERE key_id = ?`,
		keyID,
	).Scan(&keyID, &dbUserID, &dbRevokedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return writeAPIError(c, http.StatusNotFound, "key not found")
		}
		return writeAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Step 2: Ownership check — return 404 (not 403) to prevent information leakage.
	if dbUserID != userID {
		return writeAPIError(c, http.StatusNotFound, "key not found")
	}

	// Step 3: Check revocation status — already-revoked keys cannot be revoked again.
	if dbRevokedAt.Valid {
		return writeAPIError(c, http.StatusBadRequest, "key is already revoked")
	}

	// Step 4: No expiry check — expired-but-not-revoked keys are permitted for
	// deletion to make the final disposition explicit in the audit trail.

	// Step 5: Set revoked_at to current UTC timestamp.
	now := time.Now().UTC()
	revokedAtStr := db.FormatTime(now)

	_, err = h.db.Exec(
		`UPDATE api_keys SET revoked_at = ? WHERE key_id = ? AND user_id = ?`,
		revokedAtStr, keyID, userID,
	)
	if err != nil {
		return writeAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Step 6: Emit structured INFO log entry with user_id and key_id.
	c.Logger().Infof("api_key_revoked user_id=%s key_id=%s", userID, keyID)

	// Step 7: Return success response with key_id and revoked_at (RFC 3339 UTC).
	// Use RFC3339Nano to preserve sub-second precision in the response,
	// matching the approach used in the refresh handler. db.FormatTime
	// truncates to whole seconds for DB storage, but the response timestamp
	// must faithfully represent the current instant.
	return c.JSON(http.StatusOK, map[string]string{
		"key_id":     keyID,
		"revoked_at": now.Format(time.RFC3339Nano),
	})
}

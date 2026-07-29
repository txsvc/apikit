package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/txsvc/apikit/internal/apiutil"
	"github.com/txsvc/apikit/internal/auth"
	"github.com/txsvc/apikit/internal/db"
)

// randReader is the source of cryptographic randomness. Defaults to
// crypto/rand.Reader. Overridden in tests via export_test.go to simulate
// crypto/rand failures.
var randReader io.Reader = rand.Reader

// tokenAlphabet is the 36-character set from which both token_id and secret
// characters are drawn: lowercase letters and digits.
const tokenAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// PATHandler holds the database connection and PermissionRegistry dependencies
// for PAT lifecycle operations.
type PATHandler struct {
	database *db.DB
	registry *auth.PermissionRegistry
}

// NewPATHandler constructs a PATHandler with required dependencies.
// Panics with a descriptive message if either parameter is nil.
func NewPATHandler(database *db.DB, registry *auth.PermissionRegistry) *PATHandler {
	if database == nil {
		panic("NewPATHandler: database parameter must not be nil")
	}
	if registry == nil {
		panic("NewPATHandler: registry parameter must not be nil")
	}
	return &PATHandler{
		database: database,
		registry: registry,
	}
}

// CreatePATRequest represents the JSON request body for POST /user/tokens.
type CreatePATRequest struct {
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
	Expires     *int     `json:"expires,omitempty"`
}

// CreatePATResponse represents the HTTP 201 response for PAT creation,
// including the one-time plaintext token.
type CreatePATResponse struct {
	TokenID     string   `json:"token_id"`
	Name        string   `json:"name"`
	Token       string   `json:"token"`
	Permissions []string `json:"permissions"`
	ExpiresAt   *string  `json:"expires_at"`
	CreatedAt   string   `json:"created_at"`
}

// PATResponse represents the HTTP 200 response for list, get, and revoke
// operations — metadata only, never includes the plaintext secret.
type PATResponse struct {
	TokenID     string   `json:"token_id"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
	ExpiresAt   *string  `json:"expires_at"`
	CreatedAt   string   `json:"created_at"`
	RevokedAt   *string  `json:"revoked_at"`
}

// UpdatePATPermissionsRequest represents the JSON request body for
// PUT, PATCH, and DELETE on /user/tokens/:token_id/permissions.
//
// A nil Permissions slice (absent/null JSON field) is distinguishable from an
// empty slice (JSON []) at the handler level: nil means the field was not
// provided, while empty means an explicit empty array was sent. PUT treats
// an empty array as "clear all permissions and auto-revoke"; PATCH and DELETE
// treat it as an error ("permissions are required").
type UpdatePATPermissionsRequest struct {
	Permissions *[]string `json:"permissions"`
}

// patRecord holds the fields read from the pats table by lookupAndGuardPAT.
// It is an internal type used to pass data between helper functions and
// handler methods; it is never serialized to JSON directly.
type patRecord struct {
	tokenID     string
	name        string
	permissions []string
	createdAt   string
	expiresAt   *string
	revokedAt   *string
}

// RegisterRoutes registers POST /user/tokens, GET /user/tokens,
// GET /user/tokens/:token_id, DELETE /user/tokens/:token_id,
// PUT /user/tokens/:token_id/permissions,
// PATCH /user/tokens/:token_id/permissions, and
// DELETE /user/tokens/:token_id/permissions
// on the provided Echo group.
func (h *PATHandler) RegisterRoutes(g *echo.Group) {
	g.POST("/user/tokens", h.createPAT)
	g.GET("/user/tokens", h.listPATs)
	g.GET("/user/tokens/:token_id", h.getPAT)
	g.DELETE("/user/tokens/:token_id", h.revokePAT)
	g.PUT("/user/tokens/:token_id/permissions", h.replacePATPermissions)
	g.PATCH("/user/tokens/:token_id/permissions", h.addPATPermissions)
	g.DELETE("/user/tokens/:token_id/permissions", h.removePATPermissions)
}

// createPAT handles POST /user/tokens — creates a new personal access token.
// Validates the request body, checks privilege escalation for PAT credentials,
// generates a random token_id and secret, hashes the secret, stores the PAT in
// the database, and returns the one-time plaintext token in the response.
func (h *PATHandler) createPAT(c echo.Context) error {
	// Auth check: require tokens:manage permission (09-REQ-1.2).
	if err := auth.RequirePermission(c, "tokens", "manage"); err != nil {
		return apiutil.WriteAPIError(c, http.StatusForbidden, "insufficient permissions")
	}

	// Decode JSON request body (09-REQ-3.7).
	var req CreatePATRequest
	if err := c.Bind(&req); err != nil {
		return apiutil.WriteAPIError(c, http.StatusBadRequest, "invalid request body")
	}

	// Validate name (09-REQ-3.1, 09-REQ-3.2).
	if req.Name == "" {
		return apiutil.WriteAPIError(c, http.StatusBadRequest, "name is required")
	}
	if len(req.Name) > 255 {
		return apiutil.WriteAPIError(c, http.StatusBadRequest, "name must be 255 characters or fewer")
	}

	// Validate permissions (09-REQ-3.3, 09-REQ-3.4, 09-REQ-3.5, 09-REQ-3.E3).
	if len(req.Permissions) == 0 {
		return apiutil.WriteAPIError(c, http.StatusBadRequest, "permissions are required")
	}
	for _, p := range req.Permissions {
		if strings.Count(p, ":") != 1 {
			return apiutil.WriteAPIError(c, http.StatusBadRequest, fmt.Sprintf("invalid permission format: %s", p))
		}
		parts := strings.SplitN(p, ":", 2)
		if !h.registry.IsValid(parts[0], parts[1]) {
			return apiutil.WriteAPIError(c, http.StatusBadRequest, fmt.Sprintf("unknown permission: %s", p))
		}
	}

	// Validate/default expires (09-REQ-3.6, 09-REQ-3.8, 09-REQ-3.E1, 09-REQ-3.E2).
	expiresDays := 90
	if req.Expires != nil {
		expiresDays = *req.Expires
	}
	if expiresDays != 0 && expiresDays != 30 && expiresDays != 60 && expiresDays != 90 {
		return apiutil.WriteAPIError(c, http.StatusBadRequest, "expires must be 0, 30, 60, or 90")
	}

	// Privilege escalation check (09-REQ-4.1, 09-REQ-4.2, 09-REQ-4.3).
	authInfo := auth.GetAuthInfo(c)
	if authInfo != nil && authInfo.CredentialType == "pat" {
		authPerms := make(map[string]bool, len(authInfo.Permissions))
		for _, p := range authInfo.Permissions {
			authPerms[p] = true
		}
		for _, p := range req.Permissions {
			if !authPerms[p] {
				return apiutil.WriteAPIError(c, http.StatusForbidden, fmt.Sprintf("cannot grant permission: %s", p))
			}
		}
	}

	// Generate token_id and secret (09-REQ-2.1, 09-REQ-2.2, 09-REQ-2.E1).
	tokenID, err := generateTokenID()
	if err != nil {
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}
	secret, err := generateSecret()
	if err != nil {
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Hash secret and build full token string (09-REQ-2.3, 09-REQ-2.4).
	secretHash := hashSecret(secret)
	token := fmt.Sprintf("%s_pat_%s_%s", apiutil.TokenPrefix, tokenID, secret)

	// Calculate timestamps (09-REQ-5.4).
	now := time.Now().UTC()
	createdAt := db.FormatTime(now)
	var expiresAt *string
	if expiresDays > 0 {
		ea := db.FormatTime(now.Add(time.Duration(expiresDays) * 24 * time.Hour))
		expiresAt = &ea
	}

	// Serialize permissions to JSON for storage (09-REQ-5.3).
	permsJSON, err := json.Marshal(req.Permissions)
	if err != nil {
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Store in database via transaction with retry on token_id collision.
	userID := auth.GetUserID(c)
	const maxRetries = 3
	for attempt := 0; ; attempt++ {
		err = h.database.WithTx(c.Request().Context(), func(tx *sql.Tx) error {
			_, execErr := tx.Exec(
				`INSERT INTO pats (token_id, user_id, name, secret_hash, permissions, expires_days, expires_at, created_at)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				tokenID, userID, req.Name, secretHash, string(permsJSON), expiresDays, expiresAt, createdAt,
			)
			return execErr
		})
		if err == nil {
			break
		}
		if attempt >= maxRetries-1 || !errors.Is(db.WrapError(err), db.ErrConflict) {
			return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
		}
		tokenID, err = generateTokenID()
		if err != nil {
			return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
		}
		token = fmt.Sprintf("%s_pat_%s_%s", apiutil.TokenPrefix, tokenID, secret)
	}

	// Return HTTP 201 with the one-time response (09-REQ-5.1, 09-REQ-5.2).
	return c.JSON(http.StatusCreated, CreatePATResponse{
		TokenID:     tokenID,
		Name:        req.Name,
		Token:       token,
		Permissions: req.Permissions,
		ExpiresAt:   expiresAt,
		CreatedAt:   createdAt,
	})
}

// listPATs handles GET /user/tokens — lists all PATs belonging to the
// authenticated user, ordered by created_at DESC. Returns all PATs regardless
// of status (active, expired, revoked). Never includes secret_hash, plaintext
// secret, or expires_days in the response.
func (h *PATHandler) listPATs(c echo.Context) error {
	// Auth check: require tokens:read permission (09-REQ-6.5).
	if err := auth.RequirePermission(c, "tokens", "read"); err != nil {
		return apiutil.WriteAPIError(c, http.StatusForbidden, "insufficient permissions")
	}

	// Get authenticated user ID (09-REQ-6.4).
	userID := auth.GetUserID(c)

	// Query all PATs for this user, ordered by created_at DESC (09-REQ-6.1).
	rows, err := h.database.SqlDB.QueryContext(c.Request().Context(),
		`SELECT token_id, name, permissions, created_at, expires_at, revoked_at
		 FROM pats WHERE user_id = ? ORDER BY created_at DESC LIMIT 200`, userID)
	if err != nil {
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}
	defer rows.Close()

	// Build response slice; use empty slice (not nil) so JSON serializes as []
	// rather than null when no rows exist (09-REQ-6.E1).
	results := make([]PATResponse, 0)

	for rows.Next() {
		var (
			tokenID   string
			name      string
			permsJSON string
			createdAt string
			expiresAt sql.NullString
			revokedAt sql.NullString
		)

		if err := rows.Scan(&tokenID, &name, &permsJSON, &createdAt, &expiresAt, &revokedAt); err != nil {
			return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
		}

		// Parse permissions JSON array (09-REQ-6.3 — preserves insertion order).
		var permissions []string
		if err := json.Unmarshal([]byte(permsJSON), &permissions); err != nil {
			return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
		}

		pat := PATResponse{
			TokenID:     tokenID,
			Name:        name,
			Permissions: permissions,
			CreatedAt:   createdAt,
		}

		if expiresAt.Valid {
			pat.ExpiresAt = &expiresAt.String
		}
		if revokedAt.Valid {
			pat.RevokedAt = &revokedAt.String
		}

		results = append(results, pat)
	}

	if err := rows.Err(); err != nil {
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	return c.JSON(http.StatusOK, results)
}

// getPAT handles GET /user/tokens/:token_id — retrieves the metadata of a
// specific PAT by its token_id. Queries using both token_id and user_id to
// enforce user isolation: tokens belonging to other users return 404 (not 403)
// to avoid leaking existence information.
func (h *PATHandler) getPAT(c echo.Context) error {
	// Auth check: require tokens:read permission (09-REQ-7.3).
	if err := auth.RequirePermission(c, "tokens", "read"); err != nil {
		return apiutil.WriteAPIError(c, http.StatusForbidden, "insufficient permissions")
	}

	// Extract path parameter and authenticated user ID (09-REQ-7.1).
	tokenID := c.Param("token_id")
	userID := auth.GetUserID(c)

	// Query pats table by dual-column filter for user isolation (09-REQ-7.1, 09-REQ-7.E1).
	var (
		name      string
		permsJSON string
		createdAt string
		expiresAt sql.NullString
		revokedAt sql.NullString
	)

	err := h.database.SqlDB.QueryRowContext(c.Request().Context(),
		`SELECT name, permissions, created_at, expires_at, revoked_at
		 FROM pats WHERE token_id = ? AND user_id = ?`, tokenID, userID,
	).Scan(&name, &permsJSON, &createdAt, &expiresAt, &revokedAt)

	if err != nil {
		// Both nonexistent and other-user tokens return 404 (09-REQ-7.2, 09-REQ-7.E1).
		if err == sql.ErrNoRows {
			return apiutil.WriteAPIError(c, http.StatusNotFound, "token not found")
		}
		// Any other DB error returns 500 (09-REQ-7.E2).
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Parse permissions JSON array — preserves insertion order (09-PROP-3).
	var permissions []string
	if err := json.Unmarshal([]byte(permsJSON), &permissions); err != nil {
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Build PATResponse (09-REQ-7.1).
	resp := PATResponse{
		TokenID:     tokenID,
		Name:        name,
		Permissions: permissions,
		CreatedAt:   createdAt,
	}

	if expiresAt.Valid {
		resp.ExpiresAt = &expiresAt.String
	}
	if revokedAt.Valid {
		resp.RevokedAt = &revokedAt.String
	}

	return c.JSON(http.StatusOK, resp)
}

// revokePAT handles DELETE /user/tokens/:token_id — permanently revokes a PAT
// by setting its revoked_at timestamp via a conditional UPDATE. The row is never
// deleted, preserving the audit trail. When the UPDATE affects zero rows, a
// follow-up SELECT disambiguates between "token not found" (404) and "token
// already revoked" (400).
func (h *PATHandler) revokePAT(c echo.Context) error {
	// Auth check: require tokens:manage permission (09-REQ-8.3).
	if err := auth.RequirePermission(c, "tokens", "manage"); err != nil {
		return apiutil.WriteAPIError(c, http.StatusForbidden, "insufficient permissions")
	}

	// Extract path parameter and authenticated user ID (09-REQ-8.1).
	tokenID := c.Param("token_id")
	userID := auth.GetUserID(c)

	// Set revocation timestamp.
	now := time.Now().UTC()
	revokedAtStr := db.FormatTime(now)

	// Conditional UPDATE: only revoke if not already revoked (09-REQ-8.1, 09-REQ-8.E1).
	result, err := h.database.SqlDB.ExecContext(c.Request().Context(),
		`UPDATE pats SET revoked_at = ? WHERE token_id = ? AND user_id = ? AND revoked_at IS NULL`,
		revokedAtStr, tokenID, userID,
	)
	if err != nil {
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	if rowsAffected == 1 {
		// Revocation succeeded — query the updated row to build the response.
		var (
			name      string
			permsJSON string
			createdAt string
			expiresAt sql.NullString
			revokedAt sql.NullString
		)

		err := h.database.SqlDB.QueryRowContext(c.Request().Context(),
			`SELECT name, permissions, created_at, expires_at, revoked_at
			 FROM pats WHERE token_id = ? AND user_id = ?`, tokenID, userID,
		).Scan(&name, &permsJSON, &createdAt, &expiresAt, &revokedAt)
		if err != nil {
			return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
		}

		var permissions []string
		if err := json.Unmarshal([]byte(permsJSON), &permissions); err != nil {
			return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
		}

		resp := PATResponse{
			TokenID:     tokenID,
			Name:        name,
			Permissions: permissions,
			CreatedAt:   createdAt,
		}

		if expiresAt.Valid {
			resp.ExpiresAt = &expiresAt.String
		}
		if revokedAt.Valid {
			resp.RevokedAt = &revokedAt.String
		}

		return c.JSON(http.StatusOK, resp)
	}

	// Zero rows affected — disambiguate: not found vs already revoked (09-REQ-8.2).
	var revokedAt sql.NullString
	err = h.database.SqlDB.QueryRowContext(c.Request().Context(),
		`SELECT revoked_at FROM pats WHERE token_id = ? AND user_id = ?`, tokenID, userID,
	).Scan(&revokedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			// No row matches token_id AND user_id — token not found (09-REQ-8.E2).
			return apiutil.WriteAPIError(c, http.StatusNotFound, "token not found")
		}
		// Database error (09-REQ-8.E3).
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Row exists but revoked_at is already set (09-REQ-8.2).
	return apiutil.WriteAPIError(c, http.StatusBadRequest, "token already revoked")
}

// ========================================================================
// Permission modification helpers (spec 17)
// ========================================================================

// requireTokensWriteOrManage checks that the caller holds either
// tokens:write or tokens:manage. API key and admin token credentials
// bypass this check (they have implicit full permissions). Returns true
// if authorized. On failure, writes HTTP 403 to the response and returns false.
func requireTokensWriteOrManage(c echo.Context) bool {
	authInfo := auth.GetAuthInfo(c)
	if authInfo == nil {
		apiutil.WriteAPIError(c, http.StatusForbidden, "insufficient permissions")
		return false
	}
	// API keys and admin tokens have implicit full permissions.
	if authInfo.CredentialType == "admin_token" || authInfo.CredentialType == "api_key" {
		return true
	}
	// PAT credentials must hold tokens:write or tokens:manage.
	for _, p := range authInfo.Permissions {
		if p == "tokens:write" || p == "tokens:manage" {
			return true
		}
	}
	apiutil.WriteAPIError(c, http.StatusForbidden, "insufficient permissions")
	return false
}

// lookupAndGuardPAT queries the pats table by token_id and user_id (enforcing
// user isolation), then checks the revoked_at and expires_at guards.
//
// Returns the patRecord and true on success. On failure, writes the
// appropriate HTTP error to the response and returns nil, false:
//   - HTTP 404 "token not found" if not found or belongs to another user
//   - HTTP 400 "token is revoked" if revoked_at is non-NULL
//   - HTTP 400 "token is expired" if expires_at is non-NULL and <= now
//   - HTTP 500 "internal server error" on database errors
func (h *PATHandler) lookupAndGuardPAT(c echo.Context, tokenID, userID string) (*patRecord, bool) {
	var (
		name      string
		permsJSON string
		createdAt string
		expiresAt sql.NullString
		revokedAt sql.NullString
	)

	err := h.database.SqlDB.QueryRowContext(c.Request().Context(),
		`SELECT name, permissions, created_at, expires_at, revoked_at
		 FROM pats WHERE token_id = ? AND user_id = ?`, tokenID, userID,
	).Scan(&name, &permsJSON, &createdAt, &expiresAt, &revokedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			apiutil.WriteAPIError(c, http.StatusNotFound, "token not found")
			return nil, false
		}
		apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
		return nil, false
	}

	// Guard: revoked token.
	if revokedAt.Valid {
		apiutil.WriteAPIError(c, http.StatusBadRequest, "token is revoked")
		return nil, false
	}

	// Guard: expired token (expires_at <= now is treated as expired).
	if expiresAt.Valid {
		ea, parseErr := db.ParseTime(expiresAt.String)
		if parseErr != nil {
			apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
			return nil, false
		}
		if !ea.After(time.Now().UTC()) {
			apiutil.WriteAPIError(c, http.StatusBadRequest, "token is expired")
			return nil, false
		}
	}

	// Parse permissions JSON.
	var permissions []string
	if err := json.Unmarshal([]byte(permsJSON), &permissions); err != nil {
		apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
		return nil, false
	}

	rec := &patRecord{
		tokenID:     tokenID,
		name:        name,
		permissions: permissions,
		createdAt:   createdAt,
	}
	if expiresAt.Valid {
		rec.expiresAt = &expiresAt.String
	}

	return rec, true
}

// validatePermissions checks that each permission string contains exactly one
// colon (resource_type:action format). If ignoreUnknown is false (for replace
// and add operations), it also checks that each permission is registered in
// the PermissionRegistry. If ignoreUnknown is true (for remove operations),
// unregistered but validly formatted permissions are silently accepted.
//
// Returns true if all permissions pass. On failure, writes an HTTP 400 error
// to the response and returns false.
func (h *PATHandler) validatePermissions(c echo.Context, perms []string, ignoreUnknown bool) bool {
	for _, p := range perms {
		if strings.Count(p, ":") != 1 {
			apiutil.WriteAPIError(c, http.StatusBadRequest,
				fmt.Sprintf("invalid permission format: %s", p))
			return false
		}
		if !ignoreUnknown {
			parts := strings.SplitN(p, ":", 2)
			if !h.registry.IsValid(parts[0], parts[1]) {
				apiutil.WriteAPIError(c, http.StatusBadRequest,
					fmt.Sprintf("unknown permission: %s", p))
				return false
			}
		}
	}
	return true
}

// checkPrivilegeEscalation verifies that a PAT-authenticated caller does not
// grant permissions to a token that the caller's own PAT does not hold.
// Permissions already present on the target token are always permitted (keeping
// existing permissions never triggers escalation). API key and admin token
// credentials skip this check entirely.
//
// Returns true if the check passes. On failure, writes HTTP 403 to the
// response and returns false.
func checkPrivilegeEscalation(c echo.Context, existingPerms, requestedPerms []string) bool {
	authInfo := auth.GetAuthInfo(c)
	// API key and admin token credentials skip escalation check.
	if authInfo == nil || authInfo.CredentialType != "pat" {
		return true
	}

	// Build lookup sets.
	callerPermSet := make(map[string]bool, len(authInfo.Permissions))
	for _, p := range authInfo.Permissions {
		callerPermSet[p] = true
	}

	existingPermSet := make(map[string]bool, len(existingPerms))
	for _, p := range existingPerms {
		existingPermSet[p] = true
	}

	// Check each requested permission: skip if already on the target,
	// otherwise verify the caller holds it.
	for _, p := range requestedPerms {
		if existingPermSet[p] {
			continue // Keeping existing permissions is always allowed.
		}
		if !callerPermSet[p] {
			apiutil.WriteAPIError(c, http.StatusForbidden,
				fmt.Sprintf("cannot grant permission: %s", p))
			return false
		}
	}
	return true
}

// buildPATResponse constructs a PATResponse from a patRecord and a
// permissions slice. This avoids duplicating response-building logic
// across the three permission modification handlers.
func buildPATResponse(rec *patRecord, permissions []string, revokedAt *string) PATResponse {
	resp := PATResponse{
		TokenID:     rec.tokenID,
		Name:        rec.name,
		Permissions: permissions,
		CreatedAt:   rec.createdAt,
		RevokedAt:   revokedAt,
	}
	if rec.expiresAt != nil {
		resp.ExpiresAt = rec.expiresAt
	}
	return resp
}

// ========================================================================
// Permission modification handlers (spec 17)
// ========================================================================

// replacePATPermissions handles PUT /user/tokens/:token_id/permissions —
// replaces all permissions on a PAT with the provided set. An empty
// permissions array triggers auto-revocation. A nil/absent permissions field
// returns HTTP 400 "permissions are required".
func (h *PATHandler) replacePATPermissions(c echo.Context) error {
	// Step 1: Permission check — require tokens:write or tokens:manage.
	if !requireTokensWriteOrManage(c) {
		return nil // Error already written to response.
	}

	// Step 2: Parse request body.
	var req UpdatePATPermissionsRequest
	if err := c.Bind(&req); err != nil {
		return apiutil.WriteAPIError(c, http.StatusBadRequest, "invalid request body")
	}

	// Nil means absent/null — permissions field is required for PUT.
	if req.Permissions == nil {
		return apiutil.WriteAPIError(c, http.StatusBadRequest, "permissions are required")
	}
	perms := *req.Permissions

	// Step 3: Token lookup with user isolation and guard checks.
	tokenID := c.Param("token_id")
	userID := auth.GetUserID(c)
	rec, ok := h.lookupAndGuardPAT(c, tokenID, userID)
	if !ok {
		return nil // Error already written.
	}

	// Step 4: Validate permissions (format + registry) — skip for empty array.
	if len(perms) > 0 {
		if !h.validatePermissions(c, perms, false) {
			return nil
		}
		// Step 5: Privilege escalation check for PAT callers.
		if !checkPrivilegeEscalation(c, rec.permissions, perms) {
			return nil
		}
	}

	// Step 6: Database update within a transaction.
	permsJSON, marshalErr := json.Marshal(perms)
	if marshalErr != nil {
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	now := time.Now().UTC()
	var revokedAtStr *string

	// Auto-revoke if permissions are empty.
	if len(perms) == 0 {
		ra := db.FormatTime(now)
		revokedAtStr = &ra
	}

	txErr := h.database.WithTx(c.Request().Context(), func(tx *sql.Tx) error {
		if revokedAtStr != nil {
			_, execErr := tx.Exec(
				`UPDATE pats SET permissions = ?, revoked_at = ? WHERE token_id = ? AND user_id = ?`,
				string(permsJSON), *revokedAtStr, tokenID, userID,
			)
			return execErr
		}
		_, execErr := tx.Exec(
			`UPDATE pats SET permissions = ? WHERE token_id = ? AND user_id = ?`,
			string(permsJSON), tokenID, userID,
		)
		return execErr
	})
	if txErr != nil {
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Ensure empty permissions serializes as [] not null.
	if perms == nil {
		perms = []string{}
	}

	resp := buildPATResponse(rec, perms, revokedAtStr)
	return c.JSON(http.StatusOK, resp)
}

// addPATPermissions handles PATCH /user/tokens/:token_id/permissions —
// merges new permissions into the existing set by appending only permissions
// not already present. Existing permissions retain their order.
func (h *PATHandler) addPATPermissions(c echo.Context) error {
	// Step 1: Permission check — require tokens:write or tokens:manage.
	if !requireTokensWriteOrManage(c) {
		return nil
	}

	// Step 2: Parse request body.
	var req UpdatePATPermissionsRequest
	if err := c.Bind(&req); err != nil {
		return apiutil.WriteAPIError(c, http.StatusBadRequest, "invalid request body")
	}

	// Nil or empty means error for PATCH.
	if req.Permissions == nil || len(*req.Permissions) == 0 {
		return apiutil.WriteAPIError(c, http.StatusBadRequest, "permissions are required")
	}
	perms := *req.Permissions

	// Step 3: Token lookup with user isolation and guard checks.
	tokenID := c.Param("token_id")
	userID := auth.GetUserID(c)
	rec, ok := h.lookupAndGuardPAT(c, tokenID, userID)
	if !ok {
		return nil
	}

	// Step 4: Validate permissions (format + registry).
	if !h.validatePermissions(c, perms, false) {
		return nil
	}

	// Step 5: Privilege escalation check for PAT callers.
	// For add, all requested permissions are "new" — no existing-set exemption.
	if !checkPrivilegeEscalation(c, rec.permissions, perms) {
		return nil
	}

	// Step 6: Merge — append only permissions not already present.
	existingSet := make(map[string]bool, len(rec.permissions))
	for _, p := range rec.permissions {
		existingSet[p] = true
	}
	merged := make([]string, len(rec.permissions))
	copy(merged, rec.permissions)
	for _, p := range perms {
		if !existingSet[p] {
			merged = append(merged, p)
			existingSet[p] = true
		}
	}

	// Step 7: Database update within a transaction.
	mergedJSON, marshalErr := json.Marshal(merged)
	if marshalErr != nil {
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	txErr := h.database.WithTx(c.Request().Context(), func(tx *sql.Tx) error {
		_, execErr := tx.Exec(
			`UPDATE pats SET permissions = ? WHERE token_id = ? AND user_id = ?`,
			string(mergedJSON), tokenID, userID,
		)
		return execErr
	})
	if txErr != nil {
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	resp := buildPATResponse(rec, merged, nil)
	return c.JSON(http.StatusOK, resp)
}

// removePATPermissions handles DELETE /user/tokens/:token_id/permissions —
// removes specified permissions from the token's existing set. Unregistered
// but validly formatted permissions are silently ignored. If removal results
// in an empty set, the token is auto-revoked.
func (h *PATHandler) removePATPermissions(c echo.Context) error {
	// Step 1: Permission check — require tokens:write or tokens:manage.
	if !requireTokensWriteOrManage(c) {
		return nil
	}

	// Step 2: Parse request body.
	var req UpdatePATPermissionsRequest
	if err := c.Bind(&req); err != nil {
		return apiutil.WriteAPIError(c, http.StatusBadRequest, "invalid request body")
	}

	// Nil or empty means error for DELETE.
	if req.Permissions == nil || len(*req.Permissions) == 0 {
		return apiutil.WriteAPIError(c, http.StatusBadRequest, "permissions are required")
	}
	perms := *req.Permissions

	// Step 3: Token lookup with user isolation and guard checks.
	tokenID := c.Param("token_id")
	userID := auth.GetUserID(c)
	rec, ok := h.lookupAndGuardPAT(c, tokenID, userID)
	if !ok {
		return nil
	}

	// Step 4: Validate permission format only (ignoreUnknown = true for remove).
	if !h.validatePermissions(c, perms, true) {
		return nil
	}

	// Step 5: No privilege escalation check for remove (17-REQ-6.4).

	// Step 6: Compute remaining permissions after removal.
	removeSet := make(map[string]bool, len(perms))
	for _, p := range perms {
		removeSet[p] = true
	}
	remaining := make([]string, 0, len(rec.permissions))
	for _, p := range rec.permissions {
		if !removeSet[p] {
			remaining = append(remaining, p)
		}
	}

	// Step 7: Database update within a transaction.
	remainingJSON, marshalErr := json.Marshal(remaining)
	if marshalErr != nil {
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	now := time.Now().UTC()
	var revokedAtStr *string

	// Auto-revoke if remaining permissions are empty.
	if len(remaining) == 0 {
		ra := db.FormatTime(now)
		revokedAtStr = &ra
	}

	txErr := h.database.WithTx(c.Request().Context(), func(tx *sql.Tx) error {
		if revokedAtStr != nil {
			_, execErr := tx.Exec(
				`UPDATE pats SET permissions = ?, revoked_at = ? WHERE token_id = ? AND user_id = ?`,
				string(remainingJSON), *revokedAtStr, tokenID, userID,
			)
			return execErr
		}
		_, execErr := tx.Exec(
			`UPDATE pats SET permissions = ? WHERE token_id = ? AND user_id = ?`,
			string(remainingJSON), tokenID, userID,
		)
		return execErr
	})
	if txErr != nil {
		return apiutil.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	resp := buildPATResponse(rec, remaining, revokedAtStr)
	return c.JSON(http.StatusOK, resp)
}

// generateTokenID generates a cryptographically random 8-character string
// drawn exclusively from tokenAlphabet using crypto/rand.
func generateTokenID() (string, error) {
	return randomString(8)
}

// generateSecret generates a cryptographically random 32-character string
// drawn exclusively from tokenAlphabet using crypto/rand.
func generateSecret() (string, error) {
	return randomString(32)
}

// randomString generates a cryptographically random string of the given length
// drawn exclusively from tokenAlphabet using rejection sampling to avoid
// modular bias.
func randomString(length int) (string, error) {
	// 252 is the largest multiple of 36 that fits in a byte.
	const maxUnbiased = 252
	result := make([]byte, length)
	buf := make([]byte, 1)
	for i := 0; i < length; {
		if _, err := io.ReadFull(randReader, buf); err != nil {
			return "", err
		}
		if buf[0] >= maxUnbiased {
			continue
		}
		result[i] = tokenAlphabet[buf[0]%byte(len(tokenAlphabet))]
		i++
	}
	return string(result), nil
}

// hashSecret computes the SHA-256 hash of the input string and returns
// it as a lowercase hex-encoded string.
func hashSecret(input string) string {
	digest := sha256.Sum256([]byte(input))
	return hex.EncodeToString(digest[:])
}

package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/txsvc/apikit"
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

// RegisterRoutes registers POST /user/tokens, GET /user/tokens,
// GET /user/tokens/:token_id, and DELETE /user/tokens/:token_id
// on the provided Echo group.
func (h *PATHandler) RegisterRoutes(g *echo.Group) {
	g.POST("/user/tokens", h.createPAT)
	g.GET("/user/tokens", h.listPATs)
	g.GET("/user/tokens/:token_id", h.getPAT)
	g.DELETE("/user/tokens/:token_id", h.revokePAT)
}

// createPAT handles POST /user/tokens — creates a new personal access token.
// Validates the request body, checks privilege escalation for PAT credentials,
// generates a random token_id and secret, hashes the secret, stores the PAT in
// the database, and returns the one-time plaintext token in the response.
func (h *PATHandler) createPAT(c echo.Context) error {
	// Auth check: require tokens:manage permission (09-REQ-1.2).
	if err := auth.RequirePermission(c, "tokens", "manage"); err != nil {
		return apikit.WriteAPIError(c, http.StatusForbidden, "insufficient permissions")
	}

	// Decode JSON request body (09-REQ-3.7).
	var req CreatePATRequest
	if err := c.Bind(&req); err != nil {
		return apikit.WriteAPIError(c, http.StatusBadRequest, "invalid request body")
	}

	// Validate name (09-REQ-3.1, 09-REQ-3.2).
	if req.Name == "" {
		return apikit.WriteAPIError(c, http.StatusBadRequest, "name is required")
	}
	if len(req.Name) > 255 {
		return apikit.WriteAPIError(c, http.StatusBadRequest, "name must be 255 characters or fewer")
	}

	// Validate permissions (09-REQ-3.3, 09-REQ-3.4, 09-REQ-3.5, 09-REQ-3.E3).
	if len(req.Permissions) == 0 {
		return apikit.WriteAPIError(c, http.StatusBadRequest, "permissions are required")
	}
	for _, p := range req.Permissions {
		if strings.Count(p, ":") != 1 {
			return apikit.WriteAPIError(c, http.StatusBadRequest, fmt.Sprintf("invalid permission format: %s", p))
		}
		parts := strings.SplitN(p, ":", 2)
		if !h.registry.IsValid(parts[0], parts[1]) {
			return apikit.WriteAPIError(c, http.StatusBadRequest, fmt.Sprintf("unknown permission: %s", p))
		}
	}

	// Validate/default expires (09-REQ-3.6, 09-REQ-3.8, 09-REQ-3.E1, 09-REQ-3.E2).
	expiresDays := 90
	if req.Expires != nil {
		expiresDays = *req.Expires
	}
	if expiresDays != 0 && expiresDays != 30 && expiresDays != 60 && expiresDays != 90 {
		return apikit.WriteAPIError(c, http.StatusBadRequest, "expires must be 0, 30, 60, or 90")
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
				return apikit.WriteAPIError(c, http.StatusForbidden, fmt.Sprintf("cannot grant permission: %s", p))
			}
		}
	}

	// Generate token_id and secret (09-REQ-2.1, 09-REQ-2.2, 09-REQ-2.E1).
	tokenID, err := generateTokenID()
	if err != nil {
		return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}
	secret, err := generateSecret()
	if err != nil {
		return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Hash secret and build full token string (09-REQ-2.3, 09-REQ-2.4).
	secretHash := hashSecret(secret)
	token := fmt.Sprintf("%s_pat_%s_%s", apikit.TokenPrefix, tokenID, secret)

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
		return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
	}

	// Store in database via transaction (09-REQ-5.1, 09-REQ-5.5, 09-REQ-5.E1).
	userID := auth.GetUserID(c)
	err = h.database.WithTx(c.Request().Context(), func(tx *sql.Tx) error {
		_, execErr := tx.Exec(
			`INSERT INTO pats (token_id, user_id, name, secret_hash, permissions, expires_days, expires_at, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			tokenID, userID, req.Name, secretHash, string(permsJSON), expiresDays, expiresAt, createdAt,
		)
		return execErr
	})
	if err != nil {
		return apikit.WriteAPIError(c, http.StatusInternalServerError, "internal server error")
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

// listPATs handles GET /user/tokens.
func (h *PATHandler) listPATs(c echo.Context) error {
	return apikit.WriteAPIError(c, http.StatusNotImplemented, "not implemented")
}

// getPAT handles GET /user/tokens/:token_id.
func (h *PATHandler) getPAT(c echo.Context) error {
	return apikit.WriteAPIError(c, http.StatusNotImplemented, "not implemented")
}

// revokePAT handles DELETE /user/tokens/:token_id.
func (h *PATHandler) revokePAT(c echo.Context) error {
	return apikit.WriteAPIError(c, http.StatusNotImplemented, "not implemented")
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
// drawn exclusively from tokenAlphabet using the package-level randReader.
func randomString(length int) (string, error) {
	b := make([]byte, length)
	if _, err := io.ReadFull(randReader, b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = tokenAlphabet[b[i]%byte(len(tokenAlphabet))]
	}
	return string(b), nil
}

// hashSecret computes the SHA-256 hash of the input string and returns
// it as a lowercase hex-encoded string.
func hashSecret(input string) string {
	digest := sha256.Sum256([]byte(input))
	return hex.EncodeToString(digest[:])
}

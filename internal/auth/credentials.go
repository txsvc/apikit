package auth

import (
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/txsvc/apikit/internal/db"
)

// authError carries an HTTP status code and message for credential validation
// failures. The middleware translates these into WriteAPIError responses.
type authError struct {
	Code    int
	Message string
}

func (e *authError) Error() string {
	return e.Message
}

// Predefined authError values for common credential validation failures.
var (
	errInvalidCredentials = &authError{Code: http.StatusUnauthorized, Message: "invalid credentials"}
	errCredentialRevoked  = &authError{Code: http.StatusUnauthorized, Message: "credential revoked"}
	errCredentialExpired  = &authError{Code: http.StatusUnauthorized, Message: "credential expired"}
	errUserBlocked        = &authError{Code: http.StatusForbidden, Message: "user is blocked"}
	errInternalServer     = &authError{Code: http.StatusInternalServerError, Message: "internal server error"}
)

// validateAdminToken validates an admin token by checking the hex suffix format,
// computing the SHA-256 hash of the full token string (including prefix), and
// comparing it via crypto/subtle.ConstantTimeCompare against the stored hash
// in the admin_config table.
//
// Returns an AuthInfo with CredentialType "admin_token", empty UserID, and
// Role "admin" on success. Returns an authError on any validation failure.
func validateAdminToken(database *db.DB, fullToken string, hexSuffix string) (*AuthInfo, error) {
	// Step 1: Validate hex suffix is exactly 64 characters of valid hexadecimal.
	// This check is performed BEFORE any database lookup (05-REQ-4.1, 05-REQ-4.E2).
	if len(hexSuffix) != 64 {
		return nil, errInvalidCredentials
	}
	if _, err := hex.DecodeString(hexSuffix); err != nil {
		return nil, errInvalidCredentials
	}

	// Step 2: Compute SHA-256 hash of the full token string (including prefix).
	computedHash := hashToken(fullToken)

	// Step 3: Query admin_config table for the stored admin_token_hash.
	if database.SqlDB == nil {
		log.Printf("auth: database connection is nil")
		return nil, errInternalServer
	}
	var storedHash string
	err := database.SqlDB.QueryRow(
		`SELECT value FROM admin_config WHERE key = ?`, "admin_token_hash",
	).Scan(&storedHash)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// admin_token_hash row missing → treat as invalid credentials (05-REQ-4.4).
			return nil, errInvalidCredentials
		}
		// Non-ErrNotFound database error → log and return 500 (05-REQ-4.E1).
		log.Printf("auth: admin_config query error: %v", err)
		return nil, errInternalServer
	}

	// Step 4: Constant-time comparison of computed hash vs stored hash (05-REQ-4.2).
	if subtle.ConstantTimeCompare([]byte(computedHash), []byte(storedHash)) != 1 {
		return nil, errInvalidCredentials
	}

	// Step 5: Success — return admin AuthInfo (05-REQ-4.3).
	return &AuthInfo{
		CredentialType: "admin_token",
		UserID:         "",
		Role:           "admin",
	}, nil
}

// validateAPIKey validates an API key by looking up the key_id in the api_keys
// table, checking revocation and expiry status, comparing the secret hash via
// crypto/subtle.ConstantTimeCompare, and verifying the owning user is not blocked.
//
// Returns an AuthInfo with CredentialType "api_key", the user's UUID, role,
// and key_id on success. Returns an authError on any validation failure.
func validateAPIKey(database *db.DB, keyID, secret string) (*AuthInfo, error) {
	// Step 1: Query api_keys table by key_id (05-REQ-5.1).
	if database.SqlDB == nil {
		log.Printf("auth: database connection is nil")
		return nil, errInternalServer
	}
	var userID, secretHash string
	var revokedAt, expiresAt sql.NullString
	err := database.SqlDB.QueryRow(
		`SELECT user_id, secret_hash, revoked_at, expires_at FROM api_keys WHERE key_id = ?`,
		keyID,
	).Scan(&userID, &secretHash, &revokedAt, &expiresAt)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// key_id not found → invalid credentials (05-REQ-5.1).
			return nil, errInvalidCredentials
		}
		// Non-ErrNotFound database error → log and return 500 (05-REQ-5.E1).
		log.Printf("auth: api_keys query error: %v", err)
		return nil, errInternalServer
	}

	// Step 2: Check revoked_at — if non-NULL, credential is revoked (05-REQ-5.2).
	if revokedAt.Valid && revokedAt.String != "" {
		return nil, errCredentialRevoked
	}

	// Step 3: Check expires_at — if non-NULL and in the past, credential expired (05-REQ-5.3).
	if expiresAt.Valid && expiresAt.String != "" {
		expTime, parseErr := db.ParseTime(expiresAt.String)
		if parseErr != nil {
			log.Printf("auth: failed to parse expires_at for key %q: %v", keyID, parseErr)
			return nil, errInternalServer
		}
		if time.Now().After(expTime) {
			return nil, errCredentialExpired
		}
	}

	// Step 4: Compute SHA-256 of secret and compare via constant-time comparison (05-REQ-5.4).
	computedHash := hashToken(secret)
	if subtle.ConstantTimeCompare([]byte(computedHash), []byte(secretHash)) != 1 {
		return nil, errInvalidCredentials
	}

	// Step 5: Query users table by user_id and check status (05-REQ-5.5).
	var role, status string
	err = database.SqlDB.QueryRow(
		`SELECT role, status FROM users WHERE id = ?`, userID,
	).Scan(&role, &status)

	if err != nil {
		// Any user lookup failure → log and return 500 (05-REQ-5.E2).
		log.Printf("auth: users query error for user %q: %v", userID, err)
		return nil, errInternalServer
	}

	if status == "blocked" {
		return nil, errUserBlocked
	}

	// Step 6: Success — return API key AuthInfo (05-REQ-5.6).
	return &AuthInfo{
		CredentialType: "api_key",
		UserID:         userID,
		Role:           role,
		KeyID:          keyID,
	}, nil
}

// validatePAT validates a personal access token by looking up the token_id in
// the pats table, checking revocation and expiry status, comparing the secret
// hash via crypto/subtle.ConstantTimeCompare, and verifying the owning user is
// not blocked. The permissions JSON array from the pats row is deserialized
// into a []string for the AuthInfo.
//
// Returns an AuthInfo with CredentialType "pat", the user's UUID, role,
// token_id, and permissions on success. Returns an authError on any validation
// failure.
func validatePAT(database *db.DB, tokenID, secret string) (*AuthInfo, error) {
	// Step 1: Query pats table by token_id (05-REQ-6.1).
	if database.SqlDB == nil {
		log.Printf("auth: database connection is nil")
		return nil, errInternalServer
	}
	var userID, secretHash, permissionsJSON string
	var revokedAt, expiresAt sql.NullString
	err := database.SqlDB.QueryRow(
		`SELECT user_id, secret_hash, permissions, revoked_at, expires_at FROM pats WHERE token_id = ?`,
		tokenID,
	).Scan(&userID, &secretHash, &permissionsJSON, &revokedAt, &expiresAt)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// token_id not found → invalid credentials (05-REQ-6.1).
			return nil, errInvalidCredentials
		}
		// Non-ErrNotFound database error → log and return 500 (05-REQ-6.E1).
		log.Printf("auth: pats query error: %v", err)
		return nil, errInternalServer
	}

	// Step 2: Check revoked_at — if non-NULL, credential is revoked (05-REQ-6.2).
	if revokedAt.Valid && revokedAt.String != "" {
		return nil, errCredentialRevoked
	}

	// Step 3: Check expires_at — if non-NULL and in the past, credential expired (05-REQ-6.3).
	if expiresAt.Valid && expiresAt.String != "" {
		expTime, parseErr := db.ParseTime(expiresAt.String)
		if parseErr != nil {
			log.Printf("auth: failed to parse expires_at for PAT %q: %v", tokenID, parseErr)
			return nil, errInternalServer
		}
		if time.Now().After(expTime) {
			return nil, errCredentialExpired
		}
	}

	// Step 4: Compute SHA-256 of secret and compare via constant-time comparison (05-REQ-6.4).
	computedHash := hashToken(secret)
	if subtle.ConstantTimeCompare([]byte(computedHash), []byte(secretHash)) != 1 {
		return nil, errInvalidCredentials
	}

	// Step 5: Query users table by user_id and check status (05-REQ-6.5).
	var role, status string
	err = database.SqlDB.QueryRow(
		`SELECT role, status FROM users WHERE id = ?`, userID,
	).Scan(&role, &status)

	if err != nil {
		// Any user lookup failure → log and return 500 (05-REQ-6.E2).
		log.Printf("auth: users query error for user %q: %v", userID, err)
		return nil, errInternalServer
	}

	if status == "blocked" {
		return nil, errUserBlocked
	}

	// Step 6: Deserialize permissions JSON array from pats row into []string.
	var permissions []string
	if err := json.Unmarshal([]byte(permissionsJSON), &permissions); err != nil {
		log.Printf("auth: failed to parse permissions JSON for PAT %q: %v", tokenID, err)
		return nil, errInternalServer
	}

	// Step 7: Success — return PAT AuthInfo (05-REQ-6.6).
	return &AuthInfo{
		CredentialType: "pat",
		UserID:         userID,
		Role:           role,
		TokenID:        tokenID,
		Permissions:    permissions,
	}, nil
}

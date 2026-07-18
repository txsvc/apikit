package keys

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/txsvc/apikit/internal/db"
)

const (
	keyIDLen  = 8
	secretLen = 32
	// charset is the 62-character alphanumeric set used for key generation.
	// Ordering: digits, uppercase, lowercase — must match test helpers.
	charset = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	// maxInsertAttempts is the maximum number of INSERT attempts before giving up
	// on key_id collision.
	maxInsertAttempts = 3
	// defaultTokenPrefix matches the root apikit.TokenPrefix default ("ak").
	// Defined here to avoid a circular import (apikit imports internal/keys).
	defaultTokenPrefix = "ak"
)

// randReader is the random reader used for key generation.
// Defaults to crypto/rand.Reader; overridden in tests via export_test.go.
var randReader io.Reader = rand.Reader

// validExpireDays is the set of allowed values for the expiresDays parameter.
var validExpireDays = map[int]bool{0: true, 30: true, 60: true, 90: true}

// APIKeyResult contains the output of a successful API key generation.
type APIKeyResult struct {
	// FullKey is the complete key string: <prefix>_<key_id>_<secret>.
	FullKey string
	// KeyID is the 8-character alphanumeric key identifier.
	KeyID string
	// SecretHash is the lowercase hex-encoded SHA-256 hash of the secret.
	SecretHash string
	// ExpiresAt is the expiry timestamp, or nil for indefinite keys.
	ExpiresAt *time.Time
}

// randAlphanumeric generates a random alphanumeric string of length n
// using the package-level randReader. Characters are drawn from the
// 62-character alphanumeric charset via modular arithmetic (byte % 62).
func randAlphanumeric(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := io.ReadFull(randReader, buf); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = charset[int(buf[i])%len(charset)]
	}
	return string(buf), nil
}

// hashSecret computes the SHA-256 hash of the secret and returns the
// lowercase hex-encoded digest (64 characters).
func hashSecret(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(h[:])
}

// GenerateAPIKey creates a new API key for the given user, revoking any
// existing active keys. It accepts a db.Executor (either *sql.DB or *sql.Tx)
// to support both standalone and caller-managed transaction patterns.
//
// Parameters:
//   - tx: database executor (*sql.DB starts internal transaction; *sql.Tx uses caller's)
//   - userID: the user to generate a key for
//   - expiresDays: validity period in days; must be in {0, 30, 60, 90}
//   - logger: echo.Logger for structured logging
//
// Returns the key result on success, or an error.
func GenerateAPIKey(tx db.Executor, userID string, expiresDays int, logger echo.Logger) (*APIKeyResult, error) {
	// Validate expiresDays before touching the database.
	if !validExpireDays[expiresDays] {
		return nil, fmt.Errorf("invalid expiresDays: %d; must be one of {0, 30, 60, 90}", expiresDays)
	}

	// Detect *sql.DB vs *sql.Tx via type assertion to determine transaction strategy.
	if _, ok := tx.(*sql.DB); ok {
		return generateWithInternalTx(tx.(*sql.DB), userID, expiresDays, logger)
	}

	// tx is *sql.Tx or another Executor — execute within the caller's transaction.
	return generateWithExecutor(tx, userID, expiresDays, logger)
}

// generateWithInternalTx begins an internal transaction on *sql.DB, executes
// the revocation and insert atomically, and commits on success or rolls back
// on failure.
func generateWithInternalTx(sqlDB *sql.DB, userID string, expiresDays int, logger echo.Logger) (*APIKeyResult, error) {
	tx, err := sqlDB.Begin()
	if err != nil {
		return nil, err
	}

	result, err := generateWithExecutor(tx, userID, expiresDays, logger)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return result, nil
}

// generateWithExecutor performs the actual key generation: revokes existing
// keys, generates new key material, and inserts the new key record.
func generateWithExecutor(exec db.Executor, userID string, expiresDays int, logger echo.Logger) (*APIKeyResult, error) {
	ctx := context.Background()
	now := time.Now().UTC()

	// Step 1: Revoke all active keys for this user.
	// This covers both active and expired-but-not-revoked keys.
	// Zero rows affected is a silent no-op (first-time user).
	revokedAt := db.FormatTime(now)
	_, err := exec.ExecContext(ctx,
		"UPDATE api_keys SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL",
		revokedAt, userID,
	)
	if err != nil {
		return nil, err
	}

	// Step 2: Calculate expiry timestamp.
	var expiresAt *time.Time
	var expiresAtDB any // value for DB column (string or nil)
	if expiresDays > 0 {
		ea := now.Add(time.Duration(expiresDays) * 24 * time.Hour)
		expiresAt = &ea
		expiresAtDB = db.FormatTime(ea)
	}

	createdAt := db.FormatTime(now)

	// Step 3: Generate key material and INSERT with retry on key_id collision.
	// The revocation UPDATE is NOT re-executed on retry.
	for attempt := 0; attempt < maxInsertAttempts; attempt++ {
		keyID, err := randAlphanumeric(keyIDLen)
		if err != nil {
			// crypto/rand failure is immediately fatal — no retries.
			return nil, err
		}

		secret, err := randAlphanumeric(secretLen)
		if err != nil {
			// crypto/rand failure is immediately fatal — no retries.
			return nil, err
		}

		secretHash := hashSecret(secret)

		_, insertErr := exec.ExecContext(ctx,
			`INSERT INTO api_keys (key_id, user_id, secret_hash, expires_days, expires_at, created_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			keyID, userID, secretHash, expiresDays, expiresAtDB, createdAt,
		)
		if insertErr != nil {
			wrapped := db.WrapError(insertErr)
			if errors.Is(wrapped, db.ErrConflict) {
				// Unique constraint violation on key_id — retry with new key_id.
				continue
			}
			// Non-constraint error — return immediately, no retry.
			return nil, insertErr
		}

		// Success: build result and log.
		fullKey := defaultTokenPrefix + "_" + keyID + "_" + secret

		logger.Infof("api_key_created user_id=%s key_id=%s", userID, keyID)

		return &APIKeyResult{
			FullKey:    fullKey,
			KeyID:      keyID,
			SecretHash: secretHash,
			ExpiresAt:  expiresAt,
		}, nil
	}

	return nil, errors.New("failed to generate unique key_id after 3 attempts")
}

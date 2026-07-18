package keys

import (
	"crypto/rand"
	"errors"
	"io"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/txsvc/apikit/internal/db"
)

// randReader is the random reader used for key generation.
// Defaults to crypto/rand.Reader; overridden in tests via export_test.go.
var randReader io.Reader = rand.Reader

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
	return nil, errors.New("not implemented")
}

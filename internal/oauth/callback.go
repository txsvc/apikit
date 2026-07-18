package oauth

import (
	"crypto/rand"
	"time"
)

// callbackResponse represents the JSON body returned by a successful
// POST /auth/callback response.
type callbackResponse struct {
	User   callbackUser   `json:"user"`
	APIKey callbackAPIKey `json:"api_key"`
}

// callbackUser holds the user fields included in the callback response.
type callbackUser struct {
	ID         string  `json:"id"`
	Username   string  `json:"username"`
	Email      string  `json:"email"`
	FullName   *string `json:"full_name"`
	Status     string  `json:"status"`
	Role       string  `json:"role"`
	Provider   string  `json:"provider"`
	ProviderID string  `json:"provider_id"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
}

// callbackAPIKey holds the API key fields included in the callback response.
type callbackAPIKey struct {
	Key       string  `json:"key"`
	KeyID     string  `json:"key_id"`
	ExpiresAt *string `json:"expires_at"`
}

// APIKeyResult holds all data needed to insert an API key record into the
// database and format the response. The plaintext Secret is returned to the
// caller exactly once and is never stored.
type APIKeyResult struct {
	KeyID      string
	Secret     string
	SecretHash string
	ExpiresAt  *time.Time
	FullKey    string
}

// GenerateAPIKey generates a new API key with cryptographically secure
// random key_id (8 chars) and secret (32 chars) via crypto/rand. It
// computes the SHA-256 hash of the secret for storage, calculates the
// expiration time, and formats the full key string.
//
// The tokenPrefix is prepended to form the full key: <prefix>_<keyID>_<secret>.
// When expires is 0, ExpiresAt is nil (no expiration). Otherwise, ExpiresAt
// is the current UTC time plus expires days.
//
// Returns an error if crypto/rand fails during key material generation.
func GenerateAPIKey(tokenPrefix string, expires int) (*APIKeyResult, error) {
	keyID, secret, err := GenerateKeyMaterial(rand.Reader)
	if err != nil {
		return nil, err
	}

	secretHash := HashSecret(secret)
	expiresAt := ComputeExpiresAt(expires)
	fullKey := FormatAPIKey(tokenPrefix, keyID, secret)

	return &APIKeyResult{
		KeyID:      keyID,
		Secret:     secret,
		SecretHash: secretHash,
		ExpiresAt:  expiresAt,
		FullKey:    fullKey,
	}, nil
}

// FormatAPIKey constructs the full API key string in the format:
// <prefix>_<keyID>_<secret>.
func FormatAPIKey(prefix, keyID, secret string) string {
	return prefix + "_" + keyID + "_" + secret
}

// ComputeExpiresAt calculates the API key expiration time.
// When expires is 0, returns nil (no expiration / indefinite).
// Otherwise returns the current UTC time plus expires days.
func ComputeExpiresAt(expires int) *time.Time {
	if expires == 0 {
		return nil
	}
	t := time.Now().UTC().Add(time.Duration(expires) * 24 * time.Hour)
	return &t
}

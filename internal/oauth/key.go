package oauth

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
)

const (
	keyIDLength  = 8
	secretLength = 32
	charset      = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

// GenerateKeyMaterial generates a random key_id (8 alphanumeric characters)
// and secret (32 alphanumeric characters) using the provided random reader.
// Uses the reader for cryptographically secure random generation; callers
// should pass crypto/rand.Reader in production.
func GenerateKeyMaterial(rand io.Reader) (keyID string, secret string, err error) {
	keyID, err = randomString(rand, keyIDLength)
	if err != nil {
		return "", "", fmt.Errorf("generating key_id: %w", err)
	}
	secret, err = randomString(rand, secretLength)
	if err != nil {
		return "", "", fmt.Errorf("generating secret: %w", err)
	}
	return keyID, secret, nil
}

// HashSecret computes the SHA-256 hash of a secret string and returns
// the hex-encoded digest. This is the value stored in api_keys.secret_hash.
func HashSecret(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(h[:])
}

// randomString generates a random alphanumeric string of the given length
// using bytes from the provided reader.
func randomString(rand io.Reader, length int) (string, error) {
	buf := make([]byte, length)
	if _, err := io.ReadFull(rand, buf); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = charset[int(buf[i])%len(charset)]
	}
	return string(buf), nil
}

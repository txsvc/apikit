package oauth_test

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"regexp"
	"testing"

	"github.com/txsvc/apikit/internal/oauth"
)

// ========================================================================
// TS-06-43: Key generation uses crypto/rand with unique outputs
// (Requirement: 06-REQ-11.3)
// ========================================================================

// TestKey_CryptoRandUniqueness verifies that GenerateKeyMaterial produces
// unique key_ids across 100 invocations, which demonstrates the use of a
// cryptographically secure random source (crypto/rand). Non-random sources
// would produce collisions.
func TestKey_CryptoRandUniqueness(t *testing.T) {
	ids := make(map[string]bool)

	for i := range 100 {
		keyID, _, err := oauth.GenerateKeyMaterial(rand.Reader)
		if err != nil {
			t.Fatalf("GenerateKeyMaterial() iteration %d error = %v", i, err)
		}
		if ids[keyID] {
			t.Fatalf("GenerateKeyMaterial() produced duplicate key_id %q at iteration %d", keyID, i)
		}
		ids[keyID] = true
	}

	if len(ids) != 100 {
		t.Errorf("expected 100 unique key_ids, got %d", len(ids))
	}
}

// ========================================================================
// TS-06-44: API key format and hash verification
// (Requirement: 06-REQ-11.4)
// ========================================================================

// TestKey_FormatAndHash verifies that the generated key material follows
// the format <TokenPrefix>_<key_id>_<secret> where key_id is 8
// alphanumeric characters, secret is 32 alphanumeric characters, and
// HashSecret produces the SHA-256 hex digest of the plaintext secret.
func TestKey_FormatAndHash(t *testing.T) {
	keyID, secret, err := oauth.GenerateKeyMaterial(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKeyMaterial() error = %v", err)
	}

	// Construct the full key in the expected format.
	fullKey := "ak_" + keyID + "_" + secret

	// Verify format: ak_<8 alnum>_<32 alnum>
	keyPattern := regexp.MustCompile(`^ak_[a-zA-Z0-9]{8}_[a-zA-Z0-9]{32}$`)
	if !keyPattern.MatchString(fullKey) {
		t.Errorf("full key %q does not match pattern ak_<8alnum>_<32alnum>", fullKey)
	}

	// Verify key_id length.
	if len(keyID) != 8 {
		t.Errorf("key_id length = %d, want 8", len(keyID))
	}

	// Verify secret length.
	if len(secret) != 32 {
		t.Errorf("secret length = %d, want 32", len(secret))
	}

	// Verify hash: HashSecret(secret) == hex(sha256(secret))
	expectedHash := sha256.Sum256([]byte(secret))
	expectedHex := hex.EncodeToString(expectedHash[:])
	actualHash := oauth.HashSecret(secret)

	if actualHash != expectedHex {
		t.Errorf("HashSecret(secret) = %q, want %q", actualHash, expectedHex)
	}

	// Verify the hash is NOT the plaintext secret.
	if actualHash == secret {
		t.Error("HashSecret(secret) should not equal the plaintext secret")
	}
}

// ========================================================================
// TS-06-E17: Key generation fails when crypto/rand returns error
// (Requirement: 06-REQ-11.E1)
// ========================================================================

// failingReader is a mock io.Reader that always returns an error.
type failingReader struct{}

func (f *failingReader) Read(_ []byte) (int, error) {
	return 0, errors.New("simulated rand failure")
}

// Compile-time check that failingReader implements io.Reader.
var _ io.Reader = (*failingReader)(nil)

// TestKey_RandReaderError verifies that GenerateKeyMaterial returns an error
// when the random reader fails, producing empty key_id and secret.
func TestKey_RandReaderError(t *testing.T) {
	keyID, secret, err := oauth.GenerateKeyMaterial(&failingReader{})

	if keyID != "" {
		t.Errorf("key_id = %q, want empty string on rand failure", keyID)
	}
	if secret != "" {
		t.Errorf("secret = %q, want empty string on rand failure", secret)
	}
	if err == nil {
		t.Fatal("error = nil, want non-nil error when rand reader fails")
	}
}

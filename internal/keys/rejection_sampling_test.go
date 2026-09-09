package keys_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/txsvc/apikit/internal/keys"
)

// TestGenerateAPIKey_RejectsBiasedBytes verifies that random bytes >= 252
// (which cannot be mapped onto the 62-character charset without modular
// bias) are discarded and replaced by additional bytes from the reader,
// rather than being folded onto the first eight charset characters.
func TestGenerateAPIKey_RejectsBiasedBytes(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-rej-1")

	// key_id: first read of 8 bytes contains two rejected values (252, 255)
	// and six accepted values 0..5 -> "012345"; a top-up read of 2 bytes
	// (6, 7) completes the id as "01234567".
	// secret: 32 bytes of value 10 -> 32 x 'A'.
	data := []byte{252, 0, 1, 255, 2, 3, 4, 5, 6, 7}
	data = append(data, bytes.Repeat([]byte{10}, 32)...)

	restore := keys.SetRandReader(bytes.NewReader(data))
	defer restore()

	result, err := keys.GenerateAPIKey(database.SqlDB, "user-rej-1", 0, testLogger())
	if err != nil {
		t.Fatalf("GenerateAPIKey() error = %v; want nil", err)
	}
	if result.KeyID != "01234567" {
		t.Errorf("KeyID = %q; want %q (rejected bytes must not be mapped)", result.KeyID, "01234567")
	}
	wantSecret := strings.Repeat("A", 32)
	if !strings.HasSuffix(result.FullKey, "_"+wantSecret) {
		t.Errorf("FullKey = %q; want secret suffix %q", result.FullKey, wantSecret)
	}
}

// TestGenerateAPIKey_ExhaustedReaderDuringTopUp verifies that a reader that
// runs out of bytes while topping up rejected values yields an error rather
// than a short key.
func TestGenerateAPIKey_ExhaustedReaderDuringTopUp(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-rej-2")

	// Eight bytes, one of which is rejected; no bytes left for the top-up.
	data := []byte{254, 0, 1, 2, 3, 4, 5, 6}
	restore := keys.SetRandReader(bytes.NewReader(data))
	defer restore()

	result, err := keys.GenerateAPIKey(database.SqlDB, "user-rej-2", 0, testLogger())
	if err == nil {
		t.Fatalf("GenerateAPIKey() error = nil; want non-nil (reader exhausted); result = %+v", result)
	}
}

package bootstrap

import (
	crypto_rand "crypto/rand"
	"io"
)

// SetRandReader replaces the random reader used by token generation.
// Call ResetRandReader in a defer to restore the default.
func SetRandReader(r io.Reader) {
	randReader = r
}

// ResetRandReader restores the default crypto/rand reader.
func ResetRandReader() {
	randReader = crypto_rand.Reader
}

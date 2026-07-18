package keys

import "io"

// SetRandReader overrides the package-level randReader for testing.
// Returns a function that restores the original reader.
func SetRandReader(r io.Reader) func() {
	orig := randReader
	randReader = r
	return func() { randReader = orig }
}

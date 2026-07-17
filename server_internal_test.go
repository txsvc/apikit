package apikit

import (
	"testing"
	"time"
)

// TestShutdown_DrainTimeoutConstant verifies that drainTimeout is defined as
// a named compile-time constant equal to 15 * time.Second in the server package.
// This test uses package apikit (not apikit_test) to access the unexported
// constant directly.
// Covers TS-01-27 (Requirement: 01-REQ-6.2).
func TestShutdown_DrainTimeoutConstant(t *testing.T) {
	expected := 15 * time.Second
	if drainTimeout != expected {
		t.Errorf("drainTimeout = %v, want %v", drainTimeout, expected)
	}
}

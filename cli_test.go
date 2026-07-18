package apikit

import (
	"strings"
	"testing"
)

// =========================================================================
// TS-13-9: apikit.RootCommand() returns non-nil with Use containing 'akc'
// =========================================================================

// TestApikitRootCommandShim verifies that apikit.RootCommand() returns
// a non-nil *cobra.Command whose Use field contains 'akc'.
func TestApikitRootCommandShim(t *testing.T) {
	cmd := RootCommand()
	if cmd == nil {
		t.Fatal("apikit.RootCommand() should return a non-nil *cobra.Command")
	}
	if !strings.Contains(cmd.Use, "akc") {
		t.Errorf("apikit.RootCommand().Use should contain 'akc', got %q", cmd.Use)
	}
}

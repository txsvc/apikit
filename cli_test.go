package apikit

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
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

// =========================================================================
// TS-13-59: Root apikit package exports RootCommand() that delegates to
// internal/cli.RootCommand(); consuming projects can embed via AddCommand.
// Requirement: 13-REQ-13.3
// =========================================================================

// TestEmbeddableRootCommandViaAddCommand verifies that apikit.RootCommand()
// returns a non-nil *cobra.Command that a consuming project can embed into
// its own root command via AddCommand without importing internal/cli.
func TestEmbeddableRootCommandViaAddCommand(t *testing.T) {
	akcCmd := RootCommand()
	if akcCmd == nil {
		t.Fatal("apikit.RootCommand() should return non-nil *cobra.Command")
	}

	// Consuming project embeds the command tree via AddCommand
	consumerRoot := &cobra.Command{Use: "myapp", Short: "My application"}
	consumerRoot.AddCommand(akcCmd)

	// The akc command should be findable as a subcommand by name.
	// RootCommand().Use must contain 'akc' for Find to work.
	found, _, err := consumerRoot.Find([]string{"akc"})
	if err != nil {
		t.Fatalf("should find 'akc' subcommand after AddCommand: %v", err)
	}
	if found == nil {
		t.Fatal("'akc' subcommand not found under consumer root after AddCommand")
	}
}

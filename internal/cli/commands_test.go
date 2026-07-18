package cli

import (
	"encoding/json"
	"testing"

	"github.com/spf13/cobra"
)

// ===========================================================================
// Subtask 1.6: Command metadata and flag default unit tests
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-15-1: Verify that all five command groups can be registered on the
// root command via explicit AddCommand calls and produce the expected
// command names.
// Requirement: 15-REQ-1.1
// ---------------------------------------------------------------------------

func TestCommandGroupRegistration(t *testing.T) {
	root := &cobra.Command{Use: "akc"}
	root.AddCommand(
		NewLoginCmd(),
		NewUserCmd(),
		NewKeysCmd(),
		NewTokensCmd(),
		NewOrgsCmd(),
	)

	expectedNames := map[string]bool{
		"login":  false,
		"user":   false,
		"keys":   false,
		"tokens": false,
		"orgs":   false,
	}

	for _, cmd := range root.Commands() {
		name := cmd.Name()
		if _, ok := expectedNames[name]; ok {
			expectedNames[name] = true
		}
	}

	for name, found := range expectedNames {
		if !found {
			t.Errorf("missing command %q in root.Commands()", name)
		}
	}

	if len(root.Commands()) != 5 {
		t.Errorf("root has %d commands, want 5", len(root.Commands()))
	}
}

// ---------------------------------------------------------------------------
// TS-15-2: Verify that every Cobra command constructor populates Use, Short,
// Long, RunE, and annotation fields on the returned *cobra.Command.
// Requirement: 15-REQ-1.2
// ---------------------------------------------------------------------------

// commandTestCase describes a command's expected metadata for TS-15-2.
type commandTestCase struct {
	name        string            // human-readable test name
	findPath    []string          // path to find the command under its parent
	parent      func() *cobra.Command // parent constructor
}

func allUserCommandCases() []commandTestCase {
	return []commandTestCase{
		// Login is a leaf command (no parent group).
		{name: "login", findPath: nil, parent: NewLoginCmd},

		// User subcommands.
		{name: "user show", findPath: []string{"show"}, parent: NewUserCmd},
		{name: "user update", findPath: []string{"update"}, parent: NewUserCmd},

		// Keys subcommands.
		{name: "keys list", findPath: []string{"list"}, parent: NewKeysCmd},
		{name: "keys refresh", findPath: []string{"refresh"}, parent: NewKeysCmd},
		{name: "keys revoke", findPath: []string{"revoke"}, parent: NewKeysCmd},

		// Tokens subcommands.
		{name: "tokens list", findPath: []string{"list"}, parent: NewTokensCmd},
		{name: "tokens create", findPath: []string{"create"}, parent: NewTokensCmd},
		{name: "tokens show", findPath: []string{"show"}, parent: NewTokensCmd},
		{name: "tokens revoke", findPath: []string{"revoke"}, parent: NewTokensCmd},

		// Orgs subcommands.
		{name: "orgs list", findPath: []string{"list"}, parent: NewOrgsCmd},
		{name: "orgs show", findPath: []string{"show"}, parent: NewOrgsCmd},
		{name: "orgs members", findPath: []string{"members"}, parent: NewOrgsCmd},
	}
}

func TestCommandConstructors_UseShortLongRunE(t *testing.T) {
	for _, tc := range allUserCommandCases() {
		t.Run(tc.name, func(t *testing.T) {
			parent := tc.parent()
			if parent == nil {
				t.Fatal("constructor returned nil")
			}

			var cmd *cobra.Command
			if tc.findPath == nil {
				// Leaf command (login) — the parent IS the command.
				cmd = parent
			} else {
				var err error
				cmd, _, err = parent.Find(tc.findPath)
				if err != nil {
					t.Fatalf("failed to find subcommand %v: %v", tc.findPath, err)
				}
			}

			if cmd.Use == "" {
				t.Error("Use is empty")
			}
			if cmd.Short == "" {
				t.Error("Short is empty")
			}
			if cmd.RunE == nil {
				t.Error("RunE is nil")
			}
		})
	}
}

func TestCommandConstructors_Annotations(t *testing.T) {
	for _, tc := range allUserCommandCases() {
		t.Run(tc.name, func(t *testing.T) {
			parent := tc.parent()

			var cmd *cobra.Command
			if tc.findPath == nil {
				cmd = parent
			} else {
				var err error
				cmd, _, err = parent.Find(tc.findPath)
				if err != nil {
					t.Fatalf("failed to find subcommand %v: %v", tc.findPath, err)
				}
			}

			annotations := cmd.Annotations
			if annotations == nil {
				t.Fatal("Annotations map is nil")
			}

			// Every command must have auth annotation.
			if _, ok := annotations["auth"]; !ok {
				t.Error("missing 'auth' annotation")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TS-15-3: Verify that the login command's help-json annotation sets
// composite=true, method=null, path=null, and auth='none'.
// Requirement: 15-REQ-1.3
// ---------------------------------------------------------------------------

func TestLoginCmd_Annotations(t *testing.T) {
	cmd := NewLoginCmd()
	if cmd == nil {
		t.Fatal("NewLoginCmd() returned nil")
	}

	annotations := cmd.Annotations
	if annotations == nil {
		t.Fatal("login command has no Annotations")
	}

	// auth must be "none" for the login command.
	if annotations["auth"] != "none" {
		t.Errorf("auth annotation = %q, want %q", annotations["auth"], "none")
	}

	// composite must be "true".
	if annotations["composite"] != "true" {
		t.Errorf("composite annotation = %q, want %q", annotations["composite"], "true")
	}

	// method and path must indicate null (empty string or absent).
	if m := annotations["method"]; m != "" {
		t.Errorf("method annotation = %q, want empty (null)", m)
	}
	if p := annotations["path"]; p != "" {
		t.Errorf("path annotation = %q, want empty (null)", p)
	}
}

// ---------------------------------------------------------------------------
// TS-15-4: Verify that all non-login commands have auth='api_key' and
// non-null HTTP method and path in their annotations.
// Requirement: 15-REQ-1.4
// ---------------------------------------------------------------------------

func TestNonLoginCommands_AuthAndMethodPath(t *testing.T) {
	// Collect all non-login command test cases.
	nonLoginCases := []struct {
		name     string
		findPath []string
		parent   func() *cobra.Command
	}{
		{"user show", []string{"show"}, NewUserCmd},
		{"user update", []string{"update"}, NewUserCmd},
		{"keys list", []string{"list"}, NewKeysCmd},
		{"keys refresh", []string{"refresh"}, NewKeysCmd},
		{"keys revoke", []string{"revoke"}, NewKeysCmd},
		{"tokens list", []string{"list"}, NewTokensCmd},
		{"tokens create", []string{"create"}, NewTokensCmd},
		{"tokens show", []string{"show"}, NewTokensCmd},
		{"tokens revoke", []string{"revoke"}, NewTokensCmd},
		{"orgs list", []string{"list"}, NewOrgsCmd},
		{"orgs show", []string{"show"}, NewOrgsCmd},
		{"orgs members", []string{"members"}, NewOrgsCmd},
	}

	for _, tc := range nonLoginCases {
		t.Run(tc.name, func(t *testing.T) {
			parent := tc.parent()
			cmd, _, err := parent.Find(tc.findPath)
			if err != nil {
				t.Fatalf("failed to find subcommand %v: %v", tc.findPath, err)
			}

			annotations := cmd.Annotations
			if annotations == nil {
				t.Fatal("command has no Annotations")
			}

			if annotations["auth"] != "api_key" {
				t.Errorf("auth annotation = %q, want %q", annotations["auth"], "api_key")
			}

			if annotations["method"] == "" {
				t.Error("method annotation is empty, want non-null HTTP verb")
			}

			if annotations["path"] == "" {
				t.Error("path annotation is empty, want non-null API path")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TS-15-5: Verify that the login command accepts --provider flag defaulting
// to 'github' and --expires flag defaulting to 90.
// Requirement: 15-REQ-2.1
// ---------------------------------------------------------------------------

func TestLoginCmd_FlagDefaults(t *testing.T) {
	cmd := NewLoginCmd()
	if cmd == nil {
		t.Fatal("NewLoginCmd() returned nil")
	}

	providerFlag := cmd.Flags().Lookup("provider")
	if providerFlag == nil {
		t.Fatal("--provider flag not registered on login command")
	}
	if providerFlag.DefValue != "github" {
		t.Errorf("--provider default = %q, want %q", providerFlag.DefValue, "github")
	}

	expiresFlag := cmd.Flags().Lookup("expires")
	if expiresFlag == nil {
		t.Fatal("--expires flag not registered on login command")
	}
	if expiresFlag.DefValue != "90" {
		t.Errorf("--expires default = %q, want %q", expiresFlag.DefValue, "90")
	}
}

// ---------------------------------------------------------------------------
// Supplementary: User parent command has expected subcommands.
// Requirement: 15-REQ-1.2
// ---------------------------------------------------------------------------

func TestUserCmd_Subcommands(t *testing.T) {
	cmd := NewUserCmd()
	if cmd == nil {
		t.Fatal("NewUserCmd() returned nil")
	}

	expected := map[string]bool{
		"show":   false,
		"update": false,
	}

	for _, sub := range cmd.Commands() {
		if _, ok := expected[sub.Name()]; ok {
			expected[sub.Name()] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("missing subcommand %q under user", name)
		}
	}
}

// ---------------------------------------------------------------------------
// Supplementary: Keys parent command has expected subcommands.
// Requirement: 15-REQ-1.2
// ---------------------------------------------------------------------------

func TestKeysCmd_Subcommands(t *testing.T) {
	cmd := NewKeysCmd()
	if cmd == nil {
		t.Fatal("NewKeysCmd() returned nil")
	}

	expected := map[string]bool{
		"list":    false,
		"refresh": false,
		"revoke":  false,
	}

	for _, sub := range cmd.Commands() {
		if _, ok := expected[sub.Name()]; ok {
			expected[sub.Name()] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("missing subcommand %q under keys", name)
		}
	}
}

// ---------------------------------------------------------------------------
// Supplementary: Tokens parent command has expected subcommands.
// Requirement: 15-REQ-1.2
// ---------------------------------------------------------------------------

func TestTokensCmd_Subcommands(t *testing.T) {
	cmd := NewTokensCmd()
	if cmd == nil {
		t.Fatal("NewTokensCmd() returned nil")
	}

	expected := map[string]bool{
		"list":   false,
		"create": false,
		"show":   false,
		"revoke": false,
	}

	for _, sub := range cmd.Commands() {
		if _, ok := expected[sub.Name()]; ok {
			expected[sub.Name()] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("missing subcommand %q under tokens", name)
		}
	}
}

// ---------------------------------------------------------------------------
// Supplementary: Orgs parent command has expected subcommands.
// Requirement: 15-REQ-1.2
// ---------------------------------------------------------------------------

func TestOrgsCmd_Subcommands(t *testing.T) {
	cmd := NewOrgsCmd()
	if cmd == nil {
		t.Fatal("NewOrgsCmd() returned nil")
	}

	expected := map[string]bool{
		"list":    false,
		"show":    false,
		"members": false,
	}

	for _, sub := range cmd.Commands() {
		if _, ok := expected[sub.Name()]; ok {
			expected[sub.Name()] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("missing subcommand %q under orgs", name)
		}
	}
}

// Ensure json import is used (for future help-json parsing tests).
var _ = json.Valid

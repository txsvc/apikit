package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// =========================================================================
// Test helper types for help --json output parsing
// =========================================================================

// helpJSONTree is the expected root object for help --json output.
type helpJSONTree struct {
	Name     string            `json:"name"`
	Version  string            `json:"version"`
	Commands []json.RawMessage `json:"commands"`
}

// helpCmdEntry is one element in the help --json commands array.
type helpCmdEntry struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Method      *string           `json:"method"`
	Path        *string           `json:"path"`
	Args        []json.RawMessage `json:"args"`
	Flags       []json.RawMessage `json:"flags"`
	Auth        string            `json:"auth"`
	Composite   bool              `json:"composite,omitempty"`
}

// =========================================================================
// Test helpers for constructing stub command trees
// =========================================================================

// makeAnnotatedLeaf creates a leaf command with RunE and the given annotations.
func makeAnnotatedLeaf(use, short string, annotations map[string]string) *cobra.Command {
	return &cobra.Command{
		Use:         use,
		Short:       short,
		Annotations: annotations,
		RunE:        func(_ *cobra.Command, _ []string) error { return nil },
	}
}

// makeUnannotatedLeaf creates a leaf command with RunE but no Annotations.
func makeUnannotatedLeaf(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE:  func(_ *cobra.Command, _ []string) error { return nil },
	}
}

// makeGroup creates a group command without RunE (parent only).
func makeGroup(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
	}
}

// addTestCommands adds a group + leaf structure to rootCmd for testing.
func addTestCommands(rootCmd *cobra.Command) {
	// Group: 'user' (no RunE)
	userGroup := makeGroup("user", "User management commands")
	userGroup.AddCommand(makeAnnotatedLeaf("show", "Show user details", map[string]string{
		"auth":   "api_key",
		"method": "GET",
		"path":   "/api/v1/user",
	}))
	rootCmd.AddCommand(userGroup)
}

// parseHelpJSONTree parses stdout as a help --json root object.
func parseHelpJSONTree(t *testing.T, stdout string) helpJSONTree {
	t.Helper()
	trimmed := strings.TrimSpace(stdout)
	if len(trimmed) == 0 {
		t.Fatal("help --json should produce output on stdout")
	}
	if !json.Valid([]byte(trimmed)) {
		t.Fatalf("help --json stdout is not valid JSON:\n%s", trimmed)
	}
	var tree helpJSONTree
	if err := json.Unmarshal([]byte(trimmed), &tree); err != nil {
		t.Fatalf("failed to parse help --json output: %v", err)
	}
	return tree
}

// parseHelpCommands parses the commands array from a helpJSONTree.
func parseHelpCommands(t *testing.T, tree helpJSONTree) []helpCmdEntry {
	t.Helper()
	var cmds []helpCmdEntry
	for _, raw := range tree.Commands {
		var cmd helpCmdEntry
		if err := json.Unmarshal(raw, &cmd); err != nil {
			t.Fatalf("failed to parse command entry: %v", err)
		}
		cmds = append(cmds, cmd)
	}
	return cmds
}

// helpCmdNames extracts the name field from each command entry.
func helpCmdNames(cmds []helpCmdEntry) []string {
	names := make([]string, len(cmds))
	for i, cmd := range cmds {
		names[i] = cmd.Name
	}
	return names
}

// containsName checks if a name appears in the names list (exact or substring).
func containsName(names []string, target string) bool {
	for _, n := range names {
		if n == target || strings.HasSuffix(n, " "+target) || strings.Contains(n, target) {
			return true
		}
	}
	return false
}

// =========================================================================
// TS-13-47: Custom help subcommand replaces Cobra's default; outputs full
// JSON tree when --json is set; delegates to Cobra default when --json is
// not set.
// Requirement: 13-REQ-11.1
// =========================================================================

func TestHelpJSONOutputsValidJSONTree(t *testing.T) {
	rootCmd := RootCommand()
	addTestCommands(rootCmd)
	rootCmd.SetArgs([]string{"help", "--json"})

	stdout := captureStdout(t, func() {
		_ = rootCmd.Execute()
	})

	tree := parseHelpJSONTree(t, stdout)
	if tree.Commands == nil {
		t.Error("help --json output must contain a 'commands' array")
	}
}

func TestHelpWithoutJSONDelegatesToCobraDefault(t *testing.T) {
	rootCmd := RootCommand()
	addTestCommands(rootCmd)
	rootCmd.SetArgs([]string{"help"})

	stdout := captureStdout(t, func() {
		_ = rootCmd.Execute()
	})

	trimmed := strings.TrimSpace(stdout)
	if len(trimmed) == 0 {
		t.Fatal("'help' without --json should produce help text output")
	}

	// Without --json, the output should NOT parse as a help JSON tree
	// with a 'commands' array — it should be standard Cobra help text.
	var tree helpJSONTree
	if err := json.Unmarshal([]byte(trimmed), &tree); err == nil && tree.Commands != nil {
		t.Error("'help' without --json should not output a JSON command tree")
	}
}

// =========================================================================
// TS-13-48: help --json output root object has name, version, and commands
// array; each command entry has name/description/method/path/args/flags/auth.
// Requirement: 13-REQ-11.2
// =========================================================================

func TestHelpJSONRootObjectStructure(t *testing.T) {
	rootCmd := RootCommand()
	addTestCommands(rootCmd)
	rootCmd.SetArgs([]string{"help", "--json"})

	stdout := captureStdout(t, func() {
		_ = rootCmd.Execute()
	})

	tree := parseHelpJSONTree(t, stdout)

	// Root object must have non-empty name and version
	if tree.Name == "" {
		t.Error("root object 'name' should not be empty")
	}
	if tree.Version == "" {
		t.Error("root object 'version' should not be empty")
	}
	if tree.Commands == nil {
		t.Fatal("root object 'commands' array is missing")
	}

	// Each command entry must have all required fields
	for i, raw := range tree.Commands {
		var rawMap map[string]json.RawMessage
		if err := json.Unmarshal(raw, &rawMap); err != nil {
			t.Fatalf("command[%d]: failed to unmarshal: %v", i, err)
		}

		for _, field := range []string{"name", "description", "method", "path", "args", "flags", "auth"} {
			if _, ok := rawMap[field]; !ok {
				t.Errorf("command[%d]: missing required field %q", i, field)
			}
		}
	}
}

// =========================================================================
// TS-13-49: help --json walker collects only leaf commands (RunE set and at
// least one Annotations key); excludes group commands without RunE.
// Requirement: 13-REQ-11.3
// =========================================================================

func TestHelpJSONWalkerCollectsOnlyLeafCommands(t *testing.T) {
	rootCmd := RootCommand()

	// Group command 'user' without RunE — should be EXCLUDED
	userGroup := makeGroup("user", "User management commands")
	// Leaf command 'user show' with RunE and Annotations — should be INCLUDED
	userGroup.AddCommand(makeAnnotatedLeaf("show", "Show user details", map[string]string{
		"auth":   "api_key",
		"method": "GET",
		"path":   "/api/v1/user",
	}))
	rootCmd.AddCommand(userGroup)
	rootCmd.SetArgs([]string{"help", "--json"})

	stdout := captureStdout(t, func() {
		_ = rootCmd.Execute()
	})

	tree := parseHelpJSONTree(t, stdout)
	cmds := parseHelpCommands(t, tree)
	names := helpCmdNames(cmds)

	// 'user show' (or the path containing 'show') should be present
	if !containsName(names, "show") {
		t.Errorf("'user show' leaf command should appear in commands; got: %v", names)
	}

	// Bare 'user' group command (without RunE) should NOT be present
	bareUserFound := false
	for _, name := range names {
		// Check for exact 'user' (not 'user show' or similar)
		parts := strings.Fields(name)
		lastPart := parts[len(parts)-1]
		if lastPart == "user" {
			bareUserFound = true
			break
		}
	}
	if bareUserFound {
		t.Error("group command 'user' (without RunE) should not appear in commands array")
	}
}

// =========================================================================
// TS-13-50: Flag descriptors omit 'default' for zero-value defaults
// (bool false, string '', int 0); include 'default' for non-zero defaults
// (bool true). Rule applies uniformly to all flag types including bool.
// Requirement: 13-REQ-11.4
// =========================================================================

func TestHelpJSONFlagDescriptorDefaults(t *testing.T) {
	rootCmd := RootCommand()

	// Create a leaf command with diverse flag configurations
	leafCmd := makeAnnotatedLeaf("flagtest", "Flag test command", map[string]string{
		"auth":   "api_key",
		"method": "GET",
		"path":   "/test/flags",
	})
	leafCmd.Flags().Bool("myflag", false, "A bool flag with false default")
	leafCmd.Flags().Bool("verbose", true, "A bool flag with true default")
	leafCmd.Flags().String("mystr", "", "A string flag with empty default")
	leafCmd.Flags().Int("myint", 0, "An int flag with zero default")
	rootCmd.AddCommand(leafCmd)

	rootCmd.SetArgs([]string{"help", "--json"})

	stdout := captureStdout(t, func() {
		_ = rootCmd.Execute()
	})

	tree := parseHelpJSONTree(t, stdout)
	cmds := parseHelpCommands(t, tree)

	// Find the flagtest command
	var flagTestCmd *helpCmdEntry
	for i, cmd := range cmds {
		if strings.Contains(cmd.Name, "flagtest") {
			flagTestCmd = &cmds[i]
			break
		}
	}
	if flagTestCmd == nil {
		t.Fatal("flagtest command not found in help --json output")
	}

	// Parse flags as raw maps to check for 'default' key presence/absence
	for _, rawFlag := range flagTestCmd.Flags {
		var flagMap map[string]any
		if err := json.Unmarshal(rawFlag, &flagMap); err != nil {
			t.Fatalf("failed to parse flag descriptor: %v", err)
		}

		name, ok := flagMap["name"].(string)
		if !ok {
			continue
		}

		switch name {
		case "--myflag":
			// bool with default false → no 'default' key
			if _, hasDefault := flagMap["default"]; hasDefault {
				t.Error("bool flag with false default should NOT have 'default' key in descriptor")
			}
		case "--verbose":
			// bool with default true → 'default': true
			defaultVal, hasDefault := flagMap["default"]
			if !hasDefault {
				t.Error("bool flag with true default should have 'default' key in descriptor")
			} else if defaultVal != true {
				t.Errorf("verbose flag 'default' should be true, got %v", defaultVal)
			}
		case "--mystr":
			// string with default "" → no 'default' key
			if _, hasDefault := flagMap["default"]; hasDefault {
				t.Error("string flag with empty default should NOT have 'default' key in descriptor")
			}
		case "--myint":
			// int with default 0 → no 'default' key
			if _, hasDefault := flagMap["default"]; hasDefault {
				t.Error("int flag with zero default should NOT have 'default' key in descriptor")
			}
		}
	}
}

// =========================================================================
// TS-13-51: Custom SetHelpFunc outputs single command JSON when --json is
// set; falls back to Cobra default help text rendering without --json.
// Requirement: 13-REQ-11.5
// =========================================================================

func TestSetHelpFuncPerCommandJSONOutput(t *testing.T) {
	rootCmd := RootCommand()
	rootCmd.SetArgs([]string{"version", "--help", "--json"})

	stdout := captureStdout(t, func() {
		_ = rootCmd.Execute()
	})

	trimmed := strings.TrimSpace(stdout)
	if len(trimmed) == 0 {
		t.Fatal("'version --help --json' should produce output on stdout")
	}

	// Should be valid JSON
	if !json.Valid([]byte(trimmed)) {
		t.Fatalf("output should be valid JSON:\n%s", trimmed)
	}

	// Should parse as a single command entry (NOT a full tree)
	var entry helpCmdEntry
	if err := json.Unmarshal([]byte(trimmed), &entry); err != nil {
		t.Fatalf("output should parse as a single command entry: %v", err)
	}

	// The entry's name should reference the version command
	if !strings.Contains(entry.Name, "version") {
		t.Errorf("command entry name should contain 'version', got %q", entry.Name)
	}
}

func TestSetHelpFuncPerCommandDefaultHelp(t *testing.T) {
	rootCmd := RootCommand()
	rootCmd.SetArgs([]string{"version", "--help"})

	stdout := captureStdout(t, func() {
		_ = rootCmd.Execute()
	})

	trimmed := strings.TrimSpace(stdout)
	if len(trimmed) == 0 {
		t.Fatal("'version --help' should produce output on stdout")
	}

	// Without --json, should NOT be a single JSON command entry
	var entry helpCmdEntry
	if err := json.Unmarshal([]byte(trimmed), &entry); err == nil {
		if entry.Name != "" {
			t.Error("'version --help' without --json should produce Cobra help text, not JSON command entry")
		}
	}
}

// =========================================================================
// TS-13-52: help --json walker excludes commands with no keys in Annotations
// (Cobra built-ins or unannotated commands); only annotation-carrying
// commands appear.
// Requirement: 13-REQ-11.6
// =========================================================================

func TestHelpJSONExcludesUnannotatedCommands(t *testing.T) {
	rootCmd := RootCommand()

	// Add an annotated leaf — should be INCLUDED
	rootCmd.AddCommand(makeAnnotatedLeaf("annotated", "An annotated command", map[string]string{
		"auth":   "api_key",
		"method": "GET",
		"path":   "/test",
	}))

	// Add an unannotated leaf (simulating a Cobra built-in) — should be EXCLUDED
	rootCmd.AddCommand(makeUnannotatedLeaf("builtinCmd", "A simulated built-in command"))

	rootCmd.SetArgs([]string{"help", "--json"})

	stdout := captureStdout(t, func() {
		_ = rootCmd.Execute()
	})

	tree := parseHelpJSONTree(t, stdout)
	cmds := parseHelpCommands(t, tree)
	names := helpCmdNames(cmds)

	// 'builtinCmd' must NOT appear
	if containsName(names, "builtinCmd") {
		t.Error("unannotated command 'builtinCmd' should not appear in help --json output")
	}

	// All appearing commands must have at least one annotation key.
	// We verify by checking the 'auth' field is non-empty (since all
	// apikit commands carry at minimum the 'auth' annotation).
	for _, cmd := range cmds {
		if cmd.Auth == "" {
			t.Errorf("command %q appears in output but has empty 'auth' — should only include annotated commands", cmd.Name)
		}
	}
}

// =========================================================================
// TS-13-53: Commands added by consuming projects via AddCommand with at
// least the 'auth' annotation automatically appear in help --json output.
// Requirement: 13-REQ-11.7
// =========================================================================

func TestHelpJSONIncludesConsumingProjectCommands(t *testing.T) {
	rootCmd := RootCommand()

	// Simulate a consuming project adding its own command via AddCommand
	thirdPartyGroup := makeGroup("myapp", "Third-party app commands")
	thirdPartyGroup.AddCommand(makeAnnotatedLeaf("do", "Perform a third-party action", map[string]string{
		"auth":   "api_key",
		"method": "POST",
		"path":   "/third-party/do",
	}))
	rootCmd.AddCommand(thirdPartyGroup)

	rootCmd.SetArgs([]string{"help", "--json"})

	stdout := captureStdout(t, func() {
		_ = rootCmd.Execute()
	})

	tree := parseHelpJSONTree(t, stdout)
	cmds := parseHelpCommands(t, tree)
	names := helpCmdNames(cmds)

	// The consuming project's command should appear without any additional
	// registration step.
	if !containsName(names, "do") {
		t.Errorf("consuming project command 'myapp do' should appear in help --json output; got: %v", names)
	}
}

// =========================================================================
// TS-13-E14: akc help --json, akc help --json user, and akc help user --json
// all produce identical full JSON tree output.
// Requirement: 13-REQ-11.E1
// =========================================================================

func TestHelpJSONIdenticalOutputAllInvocations(t *testing.T) {
	// Three invocation patterns that should all produce identical JSON output.
	invocations := []struct {
		name string
		args []string
	}{
		{"help --json", []string{"help", "--json"}},
		{"help --json user", []string{"help", "--json", "user"}},
		{"help user --json", []string{"help", "user", "--json"}},
	}

	var outputs []string
	for _, inv := range invocations {
		rootCmd := RootCommand()
		addTestCommands(rootCmd)
		rootCmd.SetArgs(inv.args)

		stdout := captureStdout(t, func() {
			_ = rootCmd.Execute()
		})
		outputs = append(outputs, stdout)
	}

	// All three must produce valid JSON
	for i, out := range outputs {
		trimmed := strings.TrimSpace(out)
		if !json.Valid([]byte(trimmed)) {
			t.Fatalf("invocation %q did not produce valid JSON:\n%s", invocations[i].name, trimmed)
		}
	}

	// All three must be identical
	if outputs[0] != outputs[1] {
		t.Errorf("'help --json' and 'help --json user' should produce identical output")
	}
	if outputs[1] != outputs[2] {
		t.Errorf("'help --json user' and 'help user --json' should produce identical output")
	}
}

// =========================================================================
// TS-13-E15: A command with both RunE and child subcommands appears in
// help --json output as a leaf entry.
// Requirement: 13-REQ-11.E2
// =========================================================================

func TestHelpJSONDualModeCommandAppearsAsLeaf(t *testing.T) {
	rootCmd := RootCommand()

	// Create a "dual-mode" command: has RunE AND child subcommands.
	dualCmd := makeAnnotatedLeaf("dual", "A dual-mode command", map[string]string{
		"auth":   "api_key",
		"method": "GET",
		"path":   "/dual",
	})
	// Add a child subcommand
	childCmd := makeAnnotatedLeaf("child", "Child of dual", map[string]string{
		"auth":   "api_key",
		"method": "POST",
		"path":   "/dual/child",
	})
	dualCmd.AddCommand(childCmd)
	rootCmd.AddCommand(dualCmd)

	rootCmd.SetArgs([]string{"help", "--json"})

	stdout := captureStdout(t, func() {
		_ = rootCmd.Execute()
	})

	tree := parseHelpJSONTree(t, stdout)
	cmds := parseHelpCommands(t, tree)
	names := helpCmdNames(cmds)

	// 'dual' should appear as a leaf entry even though it has children
	if !containsName(names, "dual") {
		t.Errorf("dual-mode command (RunE + children) should appear in commands array; got: %v", names)
	}
}

// =========================================================================
// TS-13-60: internal/cli is declared as an internal package, preventing
// direct import from outside the module.
// Requirement: 13-REQ-13.4
// =========================================================================

func TestInternalCLIPackageProtection(t *testing.T) {
	// The CLI implementation must live under internal/cli so that Go's
	// toolchain prevents external modules from importing it directly.
	// External consumers must use the shim at the root apikit package.

	// 1. Verify this package is under internal/cli
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if !strings.Contains(filepath.ToSlash(wd), "internal/cli") {
		t.Errorf("cli package should be under internal/cli directory, got %q", wd)
	}

	// 2. Verify the public shim exists in the root package
	shimPath := filepath.Join(wd, "..", "..", "cli.go")
	shimBytes, err := os.ReadFile(shimPath)
	if err != nil {
		t.Fatalf("root package shim cli.go must exist for public RootCommand() re-export: %v", err)
	}
	shimSrc := string(shimBytes)

	// Shim must import internal/cli
	if !strings.Contains(shimSrc, "internal/cli") {
		t.Error("cli.go must import internal/cli")
	}

	// Shim must export RootCommand
	if !strings.Contains(shimSrc, "func RootCommand()") {
		t.Error("cli.go must export func RootCommand()")
	}
}

// =========================================================================
// TS-13-E17: When server and CLI are built with different TokenPrefix
// values, CLI reads from wrong config directory and produces
// missing-credential errors (no runtime check for mismatch).
// Requirement: 13-REQ-13.E1
// =========================================================================

func TestMismatchedTokenPrefixProducesMissingCredentialError(t *testing.T) {
	// Set CLI TokenPrefix to 'cliprefix'
	savedPrefix := TokenPrefix
	TokenPrefix = "cliprefix"
	t.Cleanup(func() { TokenPrefix = savedPrefix })

	// Set HOME to a temp directory
	tmpHome := t.TempDir()
	savedHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", savedHome) })

	// Unset all credential environment variables
	for _, envKey := range []string{"ENDPOINT_URL", "API_KEY", "USER_ID"} {
		saved, had := os.LookupEnv(envKey)
		os.Unsetenv(envKey)
		t.Cleanup(func() {
			if had {
				os.Setenv(envKey, saved)
			} else {
				os.Unsetenv(envKey)
			}
		})
	}

	// Write valid config to the SERVER's config directory (.srvprefix),
	// NOT the CLI's directory (.cliprefix). The CLI will look in
	// HOME/.cliprefix/ (which doesn't exist) and fail.
	srvConfigDir := filepath.Join(tmpHome, ".srvprefix")
	if err := os.MkdirAll(srvConfigDir, 0700); err != nil {
		t.Fatalf("failed to create server config dir: %v", err)
	}
	srvConfig := "endpoint_url = \"http://localhost:8080\"\nuser_id = \"test-user\"\napi_key = \"test-key\"\n"
	if err := os.WriteFile(filepath.Join(srvConfigDir, "config.toml"), []byte(srvConfig), 0600); err != nil {
		t.Fatalf("failed to write server config.toml: %v", err)
	}

	rootCmd := RootCommand()
	rootCmd.AddCommand(makeAnnotatedLeaf("somecmd", "A command requiring auth", map[string]string{
		"auth":   "api_key",
		"method": "GET",
		"path":   "/test",
	}))
	rootCmd.SetArgs([]string{"somecmd"})

	var execErr error
	stdout := captureStdout(t, func() {
		execErr = rootCmd.Execute()
		if execErr != nil {
			PrintError(execErr)
		}
	})

	// Should fail because the CLI looks in HOME/.cliprefix/ (empty)
	// not HOME/.srvprefix/ (where the valid config is)
	if execErr == nil {
		t.Fatal("expected error due to mismatched TokenPrefix — CLI should not find credentials in wrong config dir")
	}

	// Error message should mention "is not set" (missing credential),
	// NOT a specific prefix-mismatch error
	if !strings.Contains(execErr.Error(), "is not set") {
		t.Errorf("error should mention 'is not set' (missing credential), got: %v", execErr)
	}

	// JSON error envelope should have code 0 (client error)
	var env errorEnvelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("stdout should be valid JSON error envelope: %v\nstdout: %s", err, stdout)
	}
	if env.Error.Code != 0 {
		t.Errorf("error envelope code should be 0 (client error sentinel), got %d", env.Error.Code)
	}

	// Exit code should be 2
	if code := ExitCode(execErr); code != 2 {
		t.Errorf("ExitCode should be 2, got %d", code)
	}
}

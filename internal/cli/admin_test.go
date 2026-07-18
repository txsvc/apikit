package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// TS-14-1: NewAdminCmd returns a *cobra.Command with Use='admin', no RunE,
// and HasSubCommands() true.
// Requirement: 14-REQ-1.1
// ---------------------------------------------------------------------------

func TestNewAdminCmdStructure(t *testing.T) {
	cmd := NewAdminCmd()
	if cmd == nil {
		t.Fatal("NewAdminCmd() returned nil")
	}
	if cmd.Use != "admin" {
		t.Errorf("NewAdminCmd().Use = %q, want %q", cmd.Use, "admin")
	}
	if cmd.RunE != nil {
		t.Error("NewAdminCmd().RunE should be nil (parent group command)")
	}
	if !cmd.HasSubCommands() {
		t.Error("NewAdminCmd().HasSubCommands() = false, want true")
	}
}

// ---------------------------------------------------------------------------
// TS-14-2: The admin users command group registers exactly the eight
// required subcommands: list, show, create, update, promote, demote,
// block, unblock.
// Requirement: 14-REQ-1.2
// ---------------------------------------------------------------------------

func TestAdminUsersSubcommands(t *testing.T) {
	cmd := newAdminUsersCmd()
	if cmd == nil {
		t.Fatal("newAdminUsersCmd() returned nil")
	}

	expected := map[string]bool{
		"list":    false,
		"show":    false,
		"create":  false,
		"update":  false,
		"promote": false,
		"demote":  false,
		"block":   false,
		"unblock": false,
	}

	for _, sub := range cmd.Commands() {
		name := sub.Name()
		if _, ok := expected[name]; !ok {
			t.Errorf("unexpected subcommand %q under admin users", name)
		} else {
			expected[name] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("missing subcommand %q under admin users", name)
		}
	}

	if len(cmd.Commands()) != 8 {
		t.Errorf("admin users has %d subcommands, want 8", len(cmd.Commands()))
	}
}

// ---------------------------------------------------------------------------
// TS-14-3: The admin orgs command group registers list, create, update,
// delete, block, unblock, and a members sub-group containing list, add,
// remove.
// Requirement: 14-REQ-1.3
// ---------------------------------------------------------------------------

func TestAdminOrgsSubcommands(t *testing.T) {
	cmd := newAdminOrgsCmd()
	if cmd == nil {
		t.Fatal("newAdminOrgsCmd() returned nil")
	}

	expectedTop := map[string]bool{
		"list":    false,
		"create":  false,
		"update":  false,
		"delete":  false,
		"block":   false,
		"unblock": false,
		"members": false,
	}

	for _, sub := range cmd.Commands() {
		name := sub.Name()
		if _, ok := expectedTop[name]; !ok {
			t.Errorf("unexpected subcommand %q under admin orgs", name)
		} else {
			expectedTop[name] = true
		}
	}

	for name, found := range expectedTop {
		if !found {
			t.Errorf("missing subcommand %q under admin orgs", name)
		}
	}

	// Check the members sub-group
	var membersCmd *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "members" {
			membersCmd = sub
			break
		}
	}

	if membersCmd == nil {
		t.Fatal("members sub-group not found under admin orgs")
	}

	expectedMembers := map[string]bool{
		"list":   false,
		"add":    false,
		"remove": false,
	}

	for _, sub := range membersCmd.Commands() {
		name := sub.Name()
		if _, ok := expectedMembers[name]; !ok {
			t.Errorf("unexpected subcommand %q under admin orgs members", name)
		} else {
			expectedMembers[name] = true
		}
	}

	for name, found := range expectedMembers {
		if !found {
			t.Errorf("missing subcommand %q under admin orgs members", name)
		}
	}
}

// ---------------------------------------------------------------------------
// TS-14-4: The admin keys command group registers exactly two subcommands:
// list and revoke.
// Requirement: 14-REQ-1.4
// ---------------------------------------------------------------------------

func TestAdminKeysSubcommands(t *testing.T) {
	cmd := newAdminKeysCmd()
	if cmd == nil {
		t.Fatal("newAdminKeysCmd() returned nil")
	}

	expected := map[string]bool{
		"list":   false,
		"revoke": false,
	}

	for _, sub := range cmd.Commands() {
		name := sub.Name()
		if _, ok := expected[name]; !ok {
			t.Errorf("unexpected subcommand %q under admin keys", name)
		} else {
			expected[name] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("missing subcommand %q under admin keys", name)
		}
	}

	if len(cmd.Commands()) != 2 {
		t.Errorf("admin keys has %d subcommands, want 2", len(cmd.Commands()))
	}
}

// ---------------------------------------------------------------------------
// TS-14-5: The admin tokens command group registers exactly two subcommands:
// list and revoke.
// Requirement: 14-REQ-1.5
// ---------------------------------------------------------------------------

func TestAdminTokensSubcommands(t *testing.T) {
	cmd := newAdminTokensCmd()
	if cmd == nil {
		t.Fatal("newAdminTokensCmd() returned nil")
	}

	expected := map[string]bool{
		"list":   false,
		"revoke": false,
	}

	for _, sub := range cmd.Commands() {
		name := sub.Name()
		if _, ok := expected[name]; !ok {
			t.Errorf("unexpected subcommand %q under admin tokens", name)
		} else {
			expected[name] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("missing subcommand %q under admin tokens", name)
		}
	}

	if len(cmd.Commands()) != 2 {
		t.Errorf("admin tokens has %d subcommands, want 2", len(cmd.Commands()))
	}
}

// ---------------------------------------------------------------------------
// TS-14-6: NewAdminCmd is the only exported constructor in the admin CLI
// package; all others are unexported.
// Requirement: 14-REQ-1.6
// ---------------------------------------------------------------------------

func TestOnlyNewAdminCmdExported(t *testing.T) {
	// Use go/parser to inspect all admin*.go production files and verify
	// that the only exported function is NewAdminCmd.
	adminFiles, err := filepath.Glob("admin*.go")
	if err != nil {
		t.Fatalf("failed to glob admin*.go: %v", err)
	}
	if len(adminFiles) == 0 {
		// Try the internal/cli path relative to the module root.
		cwd, _ := os.Getwd()
		t.Fatalf("no admin*.go files found in %s", cwd)
	}

	fset := token.NewFileSet()
	var exportedFuncs []string

	for _, path := range adminFiles {
		// Skip test files.
		if strings.HasSuffix(path, "_test.go") {
			continue
		}

		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("failed to parse %s: %v", path, err)
		}

		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			// Only standalone functions, not methods.
			if fn.Recv != nil {
				continue
			}
			if fn.Name.IsExported() {
				exportedFuncs = append(exportedFuncs, fn.Name.Name)
			}
		}
	}

	if len(exportedFuncs) != 1 || exportedFuncs[0] != "NewAdminCmd" {
		t.Errorf("exported functions in admin*.go = %v, want [NewAdminCmd]", exportedFuncs)
	}
}

// ---------------------------------------------------------------------------
// TS-14-12: No net/http client is constructed or used directly in
// admin*.go production files.
// Requirement: 14-REQ-2.6
// ---------------------------------------------------------------------------

func TestNoDirectHTTPImports(t *testing.T) {
	adminFiles, err := filepath.Glob("admin*.go")
	if err != nil {
		t.Fatalf("failed to glob admin*.go: %v", err)
	}
	if len(adminFiles) == 0 {
		cwd, _ := os.Getwd()
		t.Fatalf("no admin*.go files found in %s", cwd)
	}

	fset := token.NewFileSet()

	for _, path := range adminFiles {
		// Skip test files.
		if strings.HasSuffix(path, "_test.go") {
			continue
		}

		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("failed to parse %s: %v", path, err)
		}

		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if importPath == "net/http" {
				t.Errorf("%s imports \"net/http\" — admin commands must not make direct HTTP calls", path)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// TS-14-1 supplementary: admin parent command tree integrity.
// Verifies the admin command has the four expected sub-groups as children.
// ---------------------------------------------------------------------------

func TestNewAdminCmdSubGroups(t *testing.T) {
	cmd := NewAdminCmd()
	if cmd == nil {
		t.Fatal("NewAdminCmd() returned nil")
	}

	expected := map[string]bool{
		"users":  false,
		"orgs":   false,
		"keys":   false,
		"tokens": false,
	}

	for _, sub := range cmd.Commands() {
		name := sub.Name()
		if _, ok := expected[name]; ok {
			expected[name] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("missing sub-group %q under admin command", name)
		}
	}
}

// Ensure cobra import is used for the *cobra.Command type reference.
var _ *cobra.Command

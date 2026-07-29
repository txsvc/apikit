package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// ========================================================================
// Spec 16 Task 3.3: CLI command Use string tests for flexible resource
// selectors.
// Test Spec: TS-16-25, TS-16-26, TS-16-27
// Requirements: 16-REQ-8.1, 16-REQ-8.2, 16-REQ-8.3
// ========================================================================

// findSubcommand traverses one level of subcommands to find a command by name.
func findSubcommand(t *testing.T, parent interface{ Commands() []*cobra.Command }, name string) *cobra.Command {
	t.Helper()
	for _, sub := range parent.Commands() {
		if sub.Name() == name {
			return sub
		}
	}
	return nil
}

// TestAdminUserCommandUseStrings verifies that admin user commands accepting
// a user positional argument have updated Use strings documenting the accepted
// selector formats (id, username, or email).
//
// Test Spec: TS-16-25
// Requirement: 16-REQ-8.1
func TestAdminUserCommandUseStrings(t *testing.T) {
	adminCmd := NewAdminCmd()

	// Navigate: admin → users
	usersCmd := findSubcommand(t, adminCmd, "users")
	if usersCmd == nil {
		t.Fatal("admin 'users' subcommand not found")
	}

	// Commands that accept a user ID positional argument.
	userSubcmds := []string{"show", "update", "promote", "demote", "block", "unblock"}

	for _, name := range userSubcmds {
		t.Run("users_"+name, func(t *testing.T) {
			cmd := findSubcommand(t, usersCmd, name)
			if cmd == nil {
				t.Fatalf("admin users '%s' subcommand not found", name)
			}

			use := cmd.Use
			// The Use string must document selector formats including
			// username and email as alternatives to UUID.
			if !strings.Contains(use, "username") && !strings.Contains(use, "email") {
				t.Errorf("admin users %s Use = %q; want it to document selector formats (e.g. '<id|username|email>')", name, use)
			}
		})
	}
}

// TestAdminOrgCommandUseStrings verifies that admin org commands accepting
// an org positional argument have updated Use strings documenting the
// accepted selector formats (id or slug).
//
// Test Spec: TS-16-25
// Requirement: 16-REQ-8.1
func TestAdminOrgCommandUseStrings(t *testing.T) {
	adminCmd := NewAdminCmd()

	// Navigate: admin → orgs
	orgsCmd := findSubcommand(t, adminCmd, "orgs")
	if orgsCmd == nil {
		t.Fatal("admin 'orgs' subcommand not found")
	}

	// Commands that accept an org ID positional argument.
	orgSubcmds := []string{"update", "delete", "block", "unblock"}

	for _, name := range orgSubcmds {
		t.Run("orgs_"+name, func(t *testing.T) {
			cmd := findSubcommand(t, orgsCmd, name)
			if cmd == nil {
				t.Fatalf("admin orgs '%s' subcommand not found", name)
			}

			use := cmd.Use
			// The Use string must document that slug is an alternative to UUID.
			if !strings.Contains(use, "slug") {
				t.Errorf("admin orgs %s Use = %q; want it to document selector formats (e.g. '<id|slug>')", name, use)
			}
		})
	}
}

// TestUserFacingOrgCommandUseStrings verifies that user-facing commands
// accepting a user or org positional argument have updated Use strings and
// Long help text documenting the accepted selector formats.
//
// Test Spec: TS-16-26
// Requirement: 16-REQ-8.2
func TestUserFacingOrgCommandUseStrings(t *testing.T) {
	orgsCmd := NewOrgsCmd()

	// "orgs show" accepts an org ID positional argument.
	showCmd := findSubcommand(t, orgsCmd, "show")
	if showCmd == nil {
		t.Fatal("orgs 'show' subcommand not found")
	}

	use := showCmd.Use
	if !strings.Contains(use, "slug") {
		t.Errorf("orgs show Use = %q; want it to document selector formats (e.g. '<id|slug>')", use)
	}

	// Long description should mention UUID or slug.
	long := showCmd.Long
	if !strings.Contains(long, "slug") && !strings.Contains(long, "UUID") {
		t.Errorf("orgs show Long = %q; want it to mention slug or UUID", long)
	}

	// "orgs members" also accepts an org ID positional argument.
	membersCmd := findSubcommand(t, orgsCmd, "members")
	if membersCmd == nil {
		t.Fatal("orgs 'members' subcommand not found")
	}

	use = membersCmd.Use
	if !strings.Contains(use, "slug") {
		t.Errorf("orgs members Use = %q; want it to document selector formats (e.g. '<id|slug>')", use)
	}
}

// TestAdminMembersTwoIDCommandUseStrings verifies that admin two-ID member
// commands (members add, members remove) have Use strings documenting that
// both positional arguments accept selectors.
//
// Test Spec: TS-16-27
// Requirement: 16-REQ-8.3
func TestAdminMembersTwoIDCommandUseStrings(t *testing.T) {
	adminCmd := NewAdminCmd()

	// Navigate: admin → orgs → members
	orgsCmd := findSubcommand(t, adminCmd, "orgs")
	if orgsCmd == nil {
		t.Fatal("admin 'orgs' subcommand not found")
	}
	membersCmd := findSubcommand(t, orgsCmd, "members")
	if membersCmd == nil {
		t.Fatal("admin orgs 'members' subcommand not found")
	}

	// Two-ID commands: add and remove.
	twoIDCmds := []string{"add", "remove"}
	for _, name := range twoIDCmds {
		t.Run("members_"+name, func(t *testing.T) {
			cmd := findSubcommand(t, membersCmd, name)
			if cmd == nil {
				t.Fatalf("admin orgs members '%s' subcommand not found", name)
			}

			use := cmd.Use
			// Must document both org selector (slug) and user selector
			// (username or email) in the Use string.
			if !strings.Contains(use, "slug") {
				t.Errorf("admin orgs members %s Use = %q; want it to document org selector (e.g. '<org_id|slug>')", name, use)
			}
			if !strings.Contains(use, "username") && !strings.Contains(use, "email") {
				t.Errorf("admin orgs members %s Use = %q; want it to document user selector (e.g. '<user_id|username|email>')", name, use)
			}
		})
	}
}

// TestAdminMembersListOrgIDUseString verifies that the admin orgs members
// list command also documents the org selector format.
//
// Test Spec: TS-16-25
// Requirement: 16-REQ-8.1
func TestAdminMembersListOrgIDUseString(t *testing.T) {
	adminCmd := NewAdminCmd()

	orgsCmd := findSubcommand(t, adminCmd, "orgs")
	if orgsCmd == nil {
		t.Fatal("admin 'orgs' subcommand not found")
	}
	membersCmd := findSubcommand(t, orgsCmd, "members")
	if membersCmd == nil {
		t.Fatal("admin orgs 'members' subcommand not found")
	}
	listCmd := findSubcommand(t, membersCmd, "list")
	if listCmd == nil {
		t.Fatal("admin orgs members 'list' subcommand not found")
	}

	use := listCmd.Use
	if !strings.Contains(use, "slug") {
		t.Errorf("admin orgs members list Use = %q; want it to document org selector (e.g. '<id|slug>')", use)
	}
}

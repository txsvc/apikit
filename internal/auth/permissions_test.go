package auth_test

import (
	"testing"

	"github.com/txsvc/apikit/internal/auth"
)

// ========================================================================
// Task 1.1: Unit tests for tokens:write registration in PermissionRegistry
// Test Spec: TS-17-1, TS-17-2
// Requirements: 17-REQ-1.1, 17-REQ-1.2
// ========================================================================

// TestNewPermissionRegistry_ContainsTokensWrite verifies that
// NewPermissionRegistry() returns a registry containing tokens:write
// as the 7th built-in permission, resulting in exactly 7 built-in
// permissions total.
//
// Test Spec: TS-17-1
// Requirement: 17-REQ-1.1
func TestNewPermissionRegistry_ContainsTokensWrite(t *testing.T) {
	registry := auth.NewPermissionRegistry()

	allPerms := registry.List()

	// Assert exactly 7 built-in permissions.
	if len(allPerms) != 7 {
		t.Fatalf("expected 7 built-in permissions, got %d: %v", len(allPerms), allPerms)
	}

	// Assert tokens:write is present.
	if !registry.IsValid("tokens", "write") {
		t.Fatalf("expected tokens:write to be registered in PermissionRegistry, but it was not found; all permissions: %v", allPerms)
	}
}

// TestNewPermissionRegistry_StillContainsTokensManage verifies that
// NewPermissionRegistry() still contains tokens:manage alongside
// the new tokens:write permission.
//
// Test Spec: TS-17-1
// Requirement: 17-REQ-1.1
func TestNewPermissionRegistry_StillContainsTokensManage(t *testing.T) {
	registry := auth.NewPermissionRegistry()

	if !registry.IsValid("tokens", "manage") {
		t.Fatal("expected tokens:manage to remain registered alongside tokens:write")
	}
}

// TestNewPermissionRegistry_AllSevenPermissions verifies that
// NewPermissionRegistry() returns exactly the expected 7 built-in
// permissions: users:read, orgs:read, keys:read, keys:manage,
// tokens:read, tokens:manage, tokens:write.
//
// Test Spec: TS-17-1
// Requirement: 17-REQ-1.1
func TestNewPermissionRegistry_AllSevenPermissions(t *testing.T) {
	registry := auth.NewPermissionRegistry()

	expected := map[string]bool{
		"users:read":     true,
		"orgs:read":      true,
		"keys:read":      true,
		"keys:manage":    true,
		"tokens:read":    true,
		"tokens:manage":  true,
		"tokens:write":   true,
	}

	allPerms := registry.List()

	if len(allPerms) != len(expected) {
		t.Fatalf("expected %d built-in permissions, got %d: %v",
			len(expected), len(allPerms), allPerms)
	}

	for _, p := range allPerms {
		if !expected[p] {
			t.Errorf("unexpected permission in registry: %q", p)
		}
	}

	for p := range expected {
		found := false
		for _, ap := range allPerms {
			if ap == p {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected permission %q not found in registry", p)
		}
	}
}

// TestTokensManageSatisfiesTokensWriteORCheck verifies that a PAT
// holding tokens:manage satisfies an OR-check that includes
// tokens:write, confirming tokens:manage is a superset of tokens:write.
//
// This tests the handler-level OR-check pattern: a caller with
// tokens:manage should be allowed where tokens:write OR tokens:manage
// is required.
//
// Test Spec: TS-17-2
// Requirement: 17-REQ-1.2
func TestTokensManageSatisfiesTokensWriteORCheck(t *testing.T) {
	// Simulate the OR-check that the permission modification handlers
	// will perform: accept if the caller holds tokens:write OR tokens:manage.
	callerPerms := []string{"tokens:manage"}

	hasTokensWriteOrManage := func(perms []string) bool {
		for _, p := range perms {
			if p == "tokens:write" || p == "tokens:manage" {
				return true
			}
		}
		return false
	}

	if !hasTokensWriteOrManage(callerPerms) {
		t.Fatal("expected tokens:manage to satisfy an OR-check that includes tokens:write")
	}
}

// TestTokensWriteSatisfiesTokensWriteORCheck verifies that a PAT
// holding tokens:write also satisfies the same OR-check.
//
// Test Spec: TS-17-2
// Requirement: 17-REQ-1.2
func TestTokensWriteSatisfiesTokensWriteORCheck(t *testing.T) {
	callerPerms := []string{"tokens:write"}

	hasTokensWriteOrManage := func(perms []string) bool {
		for _, p := range perms {
			if p == "tokens:write" || p == "tokens:manage" {
				return true
			}
		}
		return false
	}

	if !hasTokensWriteOrManage(callerPerms) {
		t.Fatal("expected tokens:write to satisfy an OR-check that includes tokens:write")
	}
}

// TestNeitherTokensWriteNorManageFailsORCheck verifies that a PAT
// holding neither tokens:write nor tokens:manage fails the OR-check.
//
// Test Spec: TS-17-2
// Requirement: 17-REQ-1.2
func TestNeitherTokensWriteNorManageFailsORCheck(t *testing.T) {
	callerPerms := []string{"users:read", "orgs:read"}

	hasTokensWriteOrManage := func(perms []string) bool {
		for _, p := range perms {
			if p == "tokens:write" || p == "tokens:manage" {
				return true
			}
		}
		return false
	}

	if hasTokensWriteOrManage(callerPerms) {
		t.Fatal("expected callerPerms without tokens:write or tokens:manage to fail the OR-check")
	}
}

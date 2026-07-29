package apikit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ========================================================================
// Spec 16 Task 3.3: SDK documentation and resolveOrgSlugFromJSON removal
// tests for flexible resource selectors.
// Test Spec: TS-16-28, TS-16-29, TS-16-30, TS-16-31
// Requirements: 16-REQ-9.1, 16-REQ-9.2, 16-REQ-10.1, 16-REQ-10.2
// ========================================================================

// readGoFile reads a Go source file relative to the project root.
// The test CWD is the package directory (root of the module), so
// file paths are relative to that.
func readGoFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	return string(data)
}

// listGoFiles returns all .go file paths in a directory (non-recursive).
func listGoFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read directory %s: %v", dir, err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	return files
}

// ---------------------------------------------------------------------------
// TS-16-28: resolveOrgSlugFromJSON must be deleted from the root package
// ---------------------------------------------------------------------------

// TestResolveOrgSlugFromJSON_Deleted verifies that the resolveOrgSlugFromJSON
// function and all associated tests are removed from cli_resolve.go (or the
// file is deleted entirely if empty).
//
// Test Spec: TS-16-28
// Requirement: 16-REQ-9.1
func TestResolveOrgSlugFromJSON_Deleted(t *testing.T) {
	// Build needle by concatenation so this test file doesn't match itself.
	needle := "resolveOrgSlug" + "FromJSON"

	// Check all non-test Go files in the root apikit package for the function.
	files := listGoFiles(t, ".")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue // skip test files — they may reference the name in assertions
		}
		src := readGoFile(t, f)
		if strings.Contains(src, needle) {
			t.Errorf("file %s still contains '%s'; want function deleted", f, needle)
		}
	}
}

// ---------------------------------------------------------------------------
// TS-16-29: No codebase reference to resolveOrgSlugFromJSON after deletion
// ---------------------------------------------------------------------------

// TestResolveOrgSlugFromJSON_NoReferences verifies that no file in the
// entire repository references resolveOrgSlugFromJSON after deletion.
//
// Test Spec: TS-16-29
// Requirement: 16-REQ-9.2
func TestResolveOrgSlugFromJSON_NoReferences(t *testing.T) {
	// Build the search needle by concatenation to avoid this test file
	// matching itself.
	needle := "resolveOrgSlug" + "FromJSON"

	// Walk all Go files in the repo to find references.
	var occurrences []string
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible paths
		}
		// Skip vendor and hidden directories (but not the root ".").
		if info.IsDir() && path != "." && (info.Name() == "vendor" || strings.HasPrefix(info.Name(), ".")) {
			return filepath.SkipDir
		}
		// Skip test files — we only care about production and test-support code,
		// but this test file itself uses the needle string in test names and
		// comments. Exclude _test.go files from the scan.
		if !info.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			if strings.Contains(string(data), needle) {
				occurrences = append(occurrences, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("filepath.Walk error: %v", err)
	}

	if len(occurrences) > 0 {
		t.Errorf("found %d Go source files still referencing '%s': %v",
			len(occurrences), needle, occurrences)
	}
}

// ---------------------------------------------------------------------------
// TS-16-30: SDK user methods have godoc mentioning UUID, username, or email
// ---------------------------------------------------------------------------

// TestSDKUserMethodGodoc verifies that Go SDK methods accepting a user
// identifier parameter have godoc comments documenting that the parameter
// accepts a UUID, username, or email address. No method signatures are
// changed.
//
// Test Spec: TS-16-30
// Requirement: 16-REQ-10.1
func TestSDKUserMethodGodoc(t *testing.T) {
	src := readGoFile(t, "sdk_admin.go")

	// SDK methods that accept a user identifier.
	userMethods := []string{
		"GetUserByID",
		"UpdateUserByID",
		"PromoteUser",
		"DemoteUser",
		"BlockUser",
		"UnblockUser",
		"ListUserKeys",
		"RevokeUserKey",
		"ListUserTokens",
		"RevokeUserToken",
	}

	for _, method := range userMethods {
		t.Run(method, func(t *testing.T) {
			// Find the godoc comment block preceding the method.
			// Look for the comment lines immediately before "func (c *Client) <Method>".
			idx := strings.Index(src, "func (c *Client) "+method+"(")
			if idx < 0 {
				t.Fatalf("method %s not found in sdk_admin.go", method)
			}

			// Extract the comment block before the method declaration.
			// Walk backwards from idx to find comment lines.
			preceding := src[:idx]
			lines := strings.Split(preceding, "\n")

			// Collect comment lines (// ...) from the end going backwards.
			var commentLines []string
			for i := len(lines) - 1; i >= 0; i-- {
				line := strings.TrimSpace(lines[i])
				if strings.HasPrefix(line, "//") {
					commentLines = append(commentLines, line)
				} else if line == "" {
					// Skip blank lines between comment and func
					continue
				} else {
					break
				}
			}

			comment := strings.Join(commentLines, " ")

			// Godoc must mention that the parameter accepts UUID, username,
			// and email.
			if !strings.Contains(comment, "UUID") {
				t.Errorf("%s godoc missing 'UUID'", method)
			}
			if !strings.Contains(comment, "username") {
				t.Errorf("%s godoc missing 'username'", method)
			}
			if !strings.Contains(comment, "email") {
				t.Errorf("%s godoc missing 'email'", method)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TS-16-31: SDK org methods have godoc mentioning UUID or slug
// ---------------------------------------------------------------------------

// TestSDKOrgMethodGodoc verifies that Go SDK methods accepting an org
// identifier parameter have godoc comments documenting that the parameter
// accepts a UUID or slug. No method signatures are changed.
//
// Test Spec: TS-16-31
// Requirement: 16-REQ-10.2
func TestSDKOrgMethodGodoc(t *testing.T) {
	src := readGoFile(t, "sdk_admin.go")

	// SDK methods that accept an org identifier.
	orgMethods := []string{
		"GetOrg",
		"UpdateOrg",
		"DeleteOrg",
		"BlockOrg",
		"UnblockOrg",
		"ListOrgMembers",
		"AddOrgMember",
		"RemoveOrgMember",
	}

	for _, method := range orgMethods {
		t.Run(method, func(t *testing.T) {
			idx := strings.Index(src, "func (c *Client) "+method+"(")
			if idx < 0 {
				t.Fatalf("method %s not found in sdk_admin.go", method)
			}

			// Extract the comment block before the method declaration.
			preceding := src[:idx]
			lines := strings.Split(preceding, "\n")

			var commentLines []string
			for i := len(lines) - 1; i >= 0; i-- {
				line := strings.TrimSpace(lines[i])
				if strings.HasPrefix(line, "//") {
					commentLines = append(commentLines, line)
				} else if line == "" {
					continue
				} else {
					break
				}
			}

			comment := strings.Join(commentLines, " ")

			// Godoc must mention UUID and slug.
			if !strings.Contains(comment, "UUID") {
				t.Errorf("%s godoc missing 'UUID'", method)
			}
			if !strings.Contains(comment, "slug") {
				t.Errorf("%s godoc missing 'slug'", method)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TS-16-30, TS-16-31 Edge Case: SDK method signatures unchanged
// ---------------------------------------------------------------------------

// TestSDKMethodSignaturesUnchanged verifies that no SDK method signatures,
// parameter types, or return types are changed — only documentation is
// updated.
//
// Edge case: 16-REQ-10.E1
func TestSDKMethodSignaturesUnchanged(t *testing.T) {
	src := readGoFile(t, "sdk_admin.go")

	// Verify key user method signatures are unchanged.
	expectedSigs := []string{
		"func (c *Client) GetUserByID(ctx context.Context, userID string, opts ...RequestOption) (*Response[User], error)",
		"func (c *Client) PromoteUser(ctx context.Context, userID string) (*User, error)",
		"func (c *Client) DemoteUser(ctx context.Context, userID string) (*User, error)",
		"func (c *Client) BlockUser(ctx context.Context, userID string) (*User, error)",
		"func (c *Client) UnblockUser(ctx context.Context, userID string) (*User, error)",
		// Org method signatures.
		"func (c *Client) GetOrg(ctx context.Context, orgID string, opts ...RequestOption) (*Response[Organization], error)",
		"func (c *Client) DeleteOrg(ctx context.Context, orgID string) error",
		"func (c *Client) BlockOrg(ctx context.Context, orgID string) (*Organization, error)",
		"func (c *Client) UnblockOrg(ctx context.Context, orgID string) (*Organization, error)",
		"func (c *Client) AddOrgMember(ctx context.Context, orgID, userID string) error",
		"func (c *Client) RemoveOrgMember(ctx context.Context, orgID, userID string) error",
	}

	for _, sig := range expectedSigs {
		if !strings.Contains(src, sig) {
			t.Errorf("expected method signature not found in sdk_admin.go: %s", sig)
		}
	}
}

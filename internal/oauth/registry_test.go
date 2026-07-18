package oauth_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/txsvc/apikit/internal/oauth"
)

// ========================================================================
// TS-06-4: NewRegistry returns non-nil empty Registry
// (Requirement: 06-REQ-2.1)
// ========================================================================

// TestRegistry_NewRegistryEmpty verifies that NewRegistry returns a non-nil
// *Registry with no registered providers (List() returns empty slice).
func TestRegistry_NewRegistryEmpty(t *testing.T) {
	r := oauth.NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry() returned nil")
	}
	names := r.List()
	if len(names) != 0 {
		t.Errorf("List() = %v, want empty slice", names)
	}
}

// ========================================================================
// TS-06-5: Register adds provider; Get returns same instance
// (Requirement: 06-REQ-2.2)
// ========================================================================

// TestRegistry_RegisterAndGet verifies that Register succeeds with nil
// error and Get by the same name returns the same provider instance.
func TestRegistry_RegisterAndGet(t *testing.T) {
	r := oauth.NewRegistry()
	p := &mockProvider{name: "testprovider"}

	err := r.Register(p)
	if err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}

	got := r.Get("testprovider")
	if got != p {
		t.Errorf("Get(\"testprovider\") = %v, want %v", got, p)
	}
}

// ========================================================================
// TS-06-6: Get returns provider for known name, nil for unknown
// (Requirement: 06-REQ-2.3)
// ========================================================================

// TestRegistry_GetKnownAndUnknown verifies that Get returns the registered
// provider for a known name and nil for an unknown name.
func TestRegistry_GetKnownAndUnknown(t *testing.T) {
	r := oauth.NewRegistry()
	p := &mockProvider{name: "github"}
	_ = r.Register(p)

	if got := r.Get("github"); got == nil {
		t.Error("Get(\"github\") = nil, want non-nil")
	}
	if got := r.Get("unknown"); got != nil {
		t.Errorf("Get(\"unknown\") = %v, want nil", got)
	}
}

// ========================================================================
// TS-06-7: List returns names in alphabetical order
// (Requirement: 06-REQ-2.4)
// ========================================================================

// TestRegistry_ListAlphabetical verifies that List returns all registered
// provider names sorted in alphabetical order.
func TestRegistry_ListAlphabetical(t *testing.T) {
	r := oauth.NewRegistry()
	_ = r.Register(&mockProvider{name: "zebra"})
	_ = r.Register(&mockProvider{name: "alpha"})
	_ = r.Register(&mockProvider{name: "mango"})

	names := r.List()
	want := []string{"alpha", "mango", "zebra"}

	if len(names) != len(want) {
		t.Fatalf("List() length = %d, want %d", len(names), len(want))
	}
	for i, name := range names {
		if name != want[i] {
			t.Errorf("List()[%d] = %q, want %q", i, name, want[i])
		}
	}
}

// ========================================================================
// TS-06-E2: Duplicate registration returns error, original unchanged
// (Requirement: 06-REQ-2.E1)
// ========================================================================

// TestRegistry_RegisterDuplicateError verifies that Register returns a
// non-nil error for a duplicate provider name without replacing the
// existing provider.
func TestRegistry_RegisterDuplicateError(t *testing.T) {
	r := oauth.NewRegistry()
	original := &mockProvider{name: "github"}
	duplicate := &mockProvider{name: "github"}

	if err := r.Register(original); err != nil {
		t.Fatalf("first Register() error = %v, want nil", err)
	}

	err := r.Register(duplicate)
	if err == nil {
		t.Fatal("second Register() error = nil, want non-nil for duplicate")
	}

	// Verify the original provider was not replaced.
	got := r.Get("github")
	if got != original {
		t.Errorf("Get(\"github\") returned different provider after duplicate registration")
	}
}

// ========================================================================
// TS-06-E3: Get("") returns nil
// (Requirement: 06-REQ-2.E2)
// ========================================================================

// TestRegistry_GetEmptyStringReturnsNil verifies that Get returns nil
// when called with an empty string, even when providers exist.
func TestRegistry_GetEmptyStringReturnsNil(t *testing.T) {
	r := oauth.NewRegistry()
	_ = r.Register(&mockProvider{name: "github"})

	result := r.Get("")
	if result != nil {
		t.Errorf("Get(\"\") = %v, want nil", result)
	}
}

// ========================================================================
// TS-06-16: BuildRegistryFromConfig with valid github config
// (Requirement: 06-REQ-4.4)
// ========================================================================

// TestBuildRegistryFromConfig_GitHub verifies that BuildRegistryFromConfig
// with a github provider config returns a registry containing the provider.
func TestBuildRegistryFromConfig_GitHub(t *testing.T) {
	client := &http.Client{Timeout: 30 * time.Second}
	providers := []oauth.ProviderConfig{
		{
			Name:         "github",
			ClientID:     "cid",
			ClientSecret: "csec",
		},
	}

	registry, err := oauth.BuildRegistryFromConfig(providers, client)
	if err != nil {
		t.Fatalf("BuildRegistryFromConfig() error = %v, want nil", err)
	}
	if registry == nil {
		t.Fatal("BuildRegistryFromConfig() returned nil registry")
	}

	got := registry.Get("github")
	if got == nil {
		t.Error("registry.Get(\"github\") = nil, want non-nil")
	}
	if got != nil && got.Name() != "github" {
		t.Errorf("provider.Name() = %q, want %q", got.Name(), "github")
	}
}

// ========================================================================
// TS-06-E8: Unknown provider name returns startup error
// (Requirement: 06-REQ-4.E1)
// ========================================================================

// TestBuildRegistryFromConfig_UnknownProvider verifies that
// BuildRegistryFromConfig returns a non-nil error for an unknown provider
// name (e.g. "gitlab") and no partial registry is exposed.
func TestBuildRegistryFromConfig_UnknownProvider(t *testing.T) {
	client := &http.Client{Timeout: 30 * time.Second}
	providers := []oauth.ProviderConfig{
		{
			Name:         "gitlab",
			ClientID:     "id",
			ClientSecret: "sec",
		},
	}

	registry, err := oauth.BuildRegistryFromConfig(providers, client)
	if err == nil {
		t.Fatal("BuildRegistryFromConfig() error = nil, want non-nil for unknown provider")
	}
	if registry != nil {
		t.Error("BuildRegistryFromConfig() returned non-nil registry for unknown provider")
	}

	// Error message should mention the unknown provider name.
	errMsg := err.Error()
	if !containsStr(errMsg, "gitlab") {
		t.Errorf("error message %q does not mention 'gitlab'", errMsg)
	}
}

// containsStr is a simple helper to check if a string contains a substring.
func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstr(s, substr)
}

func searchSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

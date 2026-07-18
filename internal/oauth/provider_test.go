package oauth_test

import (
	"context"
	"testing"

	"github.com/txsvc/apikit"
	"github.com/txsvc/apikit/internal/oauth"
)

// ========================================================================
// Mock provider for testing
// ========================================================================

// mockProvider implements all four methods of the Provider interface.
type mockProvider struct {
	name string
}

func (m *mockProvider) Name() string { return m.name }

func (m *mockProvider) AuthorizeURL(state, redirectURI string) string { return "" }

func (m *mockProvider) Exchange(_ context.Context, _, _ string) (string, error) {
	return "", nil
}

func (m *mockProvider) UserInfo(_ context.Context, _ string) (*oauth.UserInfo, error) {
	return nil, nil
}

// ========================================================================
// TS-06-1: Provider interface compile-time assertion
// (Requirement: 06-REQ-1.1)
// ========================================================================

// TestProvider_InterfaceSatisfaction verifies that a struct implementing all
// four methods (Name, AuthorizeURL, Exchange, UserInfo) satisfies the
// oauth.Provider interface at compile time.
func TestProvider_InterfaceSatisfaction(t *testing.T) {
	// Compile-time assertion: if mockProvider does not implement Provider,
	// this line will fail to compile.
	var _ oauth.Provider = &mockProvider{}

	// Runtime assertion that the interface variable is usable.
	var p oauth.Provider = &mockProvider{name: "test"}
	if p.Name() != "test" {
		t.Errorf("Name() = %q, want %q", p.Name(), "test")
	}

	// Verify all four methods are callable via the interface.
	_ = p.AuthorizeURL("state", "redirect")
	_, _ = p.Exchange(context.Background(), "code", "redirect")
	_, _ = p.UserInfo(context.Background(), "token")
}

// ========================================================================
// TS-06-2: UserInfo struct field accessibility
// (Requirement: 06-REQ-1.2)
// ========================================================================

// TestUserInfo_FieldAccessibility verifies that UserInfo struct is exported
// with the three required string fields: Username, Email, and ProviderID.
func TestUserInfo_FieldAccessibility(t *testing.T) {
	ui := oauth.UserInfo{
		Username:   "octocat",
		Email:      "octocat@github.com",
		ProviderID: "1",
	}

	if ui.Username != "octocat" {
		t.Errorf("Username = %q, want %q", ui.Username, "octocat")
	}
	if ui.Email != "octocat@github.com" {
		t.Errorf("Email = %q, want %q", ui.Email, "octocat@github.com")
	}
	if ui.ProviderID != "1" {
		t.Errorf("ProviderID = %q, want %q", ui.ProviderID, "1")
	}
}

// ========================================================================
// TS-06-3: Root apikit package type alias interchangeability
// (Requirement: 06-REQ-1.3)
// ========================================================================

// TestProvider_TypeAliasInterchangeability verifies that apikit.Provider and
// oauth.Provider are interchangeable type aliases, and likewise for UserInfo.
func TestProvider_TypeAliasInterchangeability(t *testing.T) {
	// Compile-time check: a value of type oauth.Provider can be assigned
	// to apikit.Provider and vice versa.
	var p apikit.Provider = &mockProvider{name: "alias-test"}
	var q oauth.Provider = p

	if p != q {
		t.Error("apikit.Provider and oauth.Provider values should be identical")
	}

	if p.Name() != "alias-test" {
		t.Errorf("apikit.Provider.Name() = %q, want %q", p.Name(), "alias-test")
	}

	// Verify UserInfo alias interchangeability.
	oauthUI := oauth.UserInfo{Username: "u", Email: "e", ProviderID: "p"}
	var apikitUI apikit.UserInfo = oauthUI

	if apikitUI.Username != oauthUI.Username {
		t.Errorf("UserInfo alias Username mismatch: %q != %q", apikitUI.Username, oauthUI.Username)
	}
	if apikitUI.Email != oauthUI.Email {
		t.Errorf("UserInfo alias Email mismatch: %q != %q", apikitUI.Email, oauthUI.Email)
	}
	if apikitUI.ProviderID != oauthUI.ProviderID {
		t.Errorf("UserInfo alias ProviderID mismatch: %q != %q", apikitUI.ProviderID, oauthUI.ProviderID)
	}
}

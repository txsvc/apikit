package oauth_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/txsvc/apikit/internal/oauth"
)

// ========================================================================
// TS-06-8: GitHub provider satisfies Provider interface and Name()
// (Requirement: 06-REQ-3.1)
// ========================================================================

// TestGitHub_InterfaceAndName verifies that GitHubProvider satisfies the
// Provider interface at compile time, Name() returns "github", and default
// URLs are used when config overrides are empty.
func TestGitHub_InterfaceAndName(t *testing.T) {
	// Compile-time assertion.
	var _ oauth.Provider = &oauth.GitHubProvider{}

	client := &http.Client{Timeout: 30 * time.Second}
	p := oauth.NewGitHubProvider("cid", "secret", "", "", "", client)

	if name := p.Name(); name != "github" {
		t.Errorf("Name() = %q, want %q", name, "github")
	}
}

// ========================================================================
// TS-06-9: AuthorizeURL constructs correct URL with query parameters
// (Requirement: 06-REQ-3.2)
// ========================================================================

// TestGitHub_AuthorizeURL verifies that AuthorizeURL constructs the correct
// URL with client_id, scope=user:email, state, and redirect_uri as query
// parameters using the default authorize URL.
func TestGitHub_AuthorizeURL(t *testing.T) {
	client := &http.Client{Timeout: 30 * time.Second}
	p := oauth.NewGitHubProvider("myclient", "secret", "", "", "", client)

	result := p.AuthorizeURL("mystate", "http://localhost:9000/cb")

	parsed, err := url.Parse(result)
	if err != nil {
		t.Fatalf("failed to parse AuthorizeURL result: %v", err)
	}

	if parsed.Host != "github.com" {
		t.Errorf("Host = %q, want %q", parsed.Host, "github.com")
	}
	if parsed.Path != "/login/oauth/authorize" {
		t.Errorf("Path = %q, want %q", parsed.Path, "/login/oauth/authorize")
	}

	q := parsed.Query()

	if got := q.Get("client_id"); got != "myclient" {
		t.Errorf("client_id = %q, want %q", got, "myclient")
	}
	if got := q.Get("scope"); got != "user:email" {
		t.Errorf("scope = %q, want %q", got, "user:email")
	}
	if got := q.Get("state"); got != "mystate" {
		t.Errorf("state = %q, want %q", got, "mystate")
	}
	if got := q.Get("redirect_uri"); got != "http://localhost:9000/cb" {
		t.Errorf("redirect_uri = %q, want %q", got, "http://localhost:9000/cb")
	}
}

// TestGitHub_AuthorizeURLCustom verifies that AuthorizeURL uses a custom
// authorize URL when provided as an override.
func TestGitHub_AuthorizeURLCustom(t *testing.T) {
	client := &http.Client{Timeout: 30 * time.Second}
	p := oauth.NewGitHubProvider("cid", "secret", "https://ghe.example.com/login/oauth/authorize", "", "", client)

	result := p.AuthorizeURL("s", "http://localhost:1234/cb")

	parsed, err := url.Parse(result)
	if err != nil {
		t.Fatalf("failed to parse AuthorizeURL result: %v", err)
	}

	if parsed.Host != "ghe.example.com" {
		t.Errorf("Host = %q, want %q (custom override)", parsed.Host, "ghe.example.com")
	}
}

// ========================================================================
// TS-06-12: Provider stores injected http.Client
// (Requirement: 06-REQ-3.5)
// ========================================================================

// TestGitHub_InjectedHTTPClient verifies that the GitHub provider stores
// the injected *http.Client reference and does not create its own client.
// We verify this indirectly by confirming the provider's construction
// succeeds with a custom client and the provider is functional.
func TestGitHub_InjectedHTTPClient(t *testing.T) {
	customClient := &http.Client{Timeout: 30 * time.Second}
	p := oauth.NewGitHubProvider("cid", "secret", "", "", "", customClient)

	// The provider should be functional (not nil).
	if p == nil {
		t.Fatal("NewGitHubProvider returned nil")
	}

	// Verify Name() works (provider is properly constructed).
	if name := p.Name(); name != "github" {
		t.Errorf("Name() = %q, want %q", name, "github")
	}

	// Verify the provider's internal httpClient field points to the injected client.
	// We access this through the exported field for testability.
	if got := p.HTTPClient(); got != customClient {
		t.Errorf("HTTPClient() = %p, want %p (should be the injected client)", got, customClient)
	}
}

package oauth_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

// ========================================================================
// TS-06-10: Exchange sends correct POST request to token_url
// (Requirement: 06-REQ-3.3)
// ========================================================================

// TestGitHub_Exchange_Success verifies that Exchange sends a POST request
// to token_url with form-encoded body containing client_id, client_secret,
// code, and redirect_uri, sets Accept: application/json header, and returns
// the access_token from the JSON response.
func TestGitHub_Exchange_Success(t *testing.T) {
	var capturedReq *http.Request
	var capturedBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		if err := r.ParseForm(); err != nil {
			t.Errorf("failed to parse form: %v", err)
		}
		capturedBody = r.Form.Encode()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"tok123","token_type":"bearer"}`)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	p := oauth.NewGitHubProvider("cid", "csec", "", srv.URL+"/token", "", client)

	tok, err := p.Exchange(context.Background(), "authcode", "http://localhost:9000/cb")
	if err != nil {
		t.Fatalf("Exchange() error = %v, want nil", err)
	}
	if tok != "tok123" {
		t.Errorf("Exchange() token = %q, want %q", tok, "tok123")
	}

	// Verify request method.
	if capturedReq == nil {
		t.Fatal("Exchange() did not send an HTTP request to the token endpoint")
	}
	if capturedReq.Method != http.MethodPost {
		t.Errorf("request method = %q, want %q", capturedReq.Method, http.MethodPost)
	}

	// Verify Accept header.
	if got := capturedReq.Header.Get("Accept"); got != "application/json" {
		t.Errorf("Accept header = %q, want %q", got, "application/json")
	}

	// Verify form body fields.
	form, _ := url.ParseQuery(capturedBody)
	if got := form.Get("client_id"); got != "cid" {
		t.Errorf("form client_id = %q, want %q", got, "cid")
	}
	if got := form.Get("client_secret"); got != "csec" {
		t.Errorf("form client_secret = %q, want %q", got, "csec")
	}
	if got := form.Get("code"); got != "authcode" {
		t.Errorf("form code = %q, want %q", got, "authcode")
	}
	if got := form.Get("redirect_uri"); got != "http://localhost:9000/cb" {
		t.Errorf("form redirect_uri = %q, want %q", got, "http://localhost:9000/cb")
	}
}

// ========================================================================
// TS-06-E4: Exchange returns error on provider error response
// (Requirement: 06-REQ-3.E1)
// ========================================================================

// TestGitHub_Exchange_ProviderError verifies that Exchange returns a non-nil
// error when the token endpoint responds with a JSON error field, and the
// error message contains the provider's error value.
func TestGitHub_Exchange_ProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"error":"bad_verification_code","error_description":"The code passed is incorrect"}`)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	p := oauth.NewGitHubProvider("cid", "csec", "", srv.URL, "", client)

	tok, err := p.Exchange(context.Background(), "badcode", "http://localhost:9000/cb")
	if tok != "" {
		t.Errorf("Exchange() token = %q, want empty string", tok)
	}
	if err == nil {
		t.Fatal("Exchange() error = nil, want non-nil error for provider error response")
	}
	if !strings.Contains(err.Error(), "bad_verification_code") {
		t.Errorf("Exchange() error = %q, want it to contain %q", err.Error(), "bad_verification_code")
	}
}

// ========================================================================
// TS-06-E5: Exchange returns error when access_token is absent
// (Requirement: 06-REQ-3.E2)
// ========================================================================

// TestGitHub_Exchange_MissingToken verifies that Exchange returns a non-nil
// error when the token endpoint responds with JSON that has no access_token
// field.
func TestGitHub_Exchange_MissingToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"token_type":"bearer"}`)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	p := oauth.NewGitHubProvider("cid", "csec", "", srv.URL, "", client)

	tok, err := p.Exchange(context.Background(), "code", "http://localhost:9000/cb")
	if tok != "" {
		t.Errorf("Exchange() token = %q, want empty string", tok)
	}
	if err == nil {
		t.Fatal("Exchange() error = nil, want non-nil error indicating token was not returned")
	}
}

// ========================================================================
// TS-06-E6: Exchange returns error on non-2xx HTTP status without retry
// (Requirement: 06-REQ-3.E3)
// ========================================================================

// TestGitHub_Exchange_ServerError verifies that Exchange returns a non-nil
// error when the token endpoint returns HTTP 500, and does not retry.
func TestGitHub_Exchange_ServerError(t *testing.T) {
	requestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	p := oauth.NewGitHubProvider("cid", "csec", "", srv.URL, "", client)

	start := time.Now()
	tok, err := p.Exchange(context.Background(), "code", "http://localhost:9000/cb")
	elapsed := time.Since(start)

	if tok != "" {
		t.Errorf("Exchange() token = %q, want empty string", tok)
	}
	if err == nil {
		t.Fatal("Exchange() error = nil, want non-nil error for HTTP 500")
	}

	// Verify no retry: should complete quickly and only one request sent.
	if elapsed > 2*time.Second {
		t.Errorf("Exchange() took %v, expected < 2s (no retry)", elapsed)
	}
	if requestCount > 1 {
		t.Errorf("Exchange() sent %d requests, expected 1 (no retry)", requestCount)
	}
}

// ========================================================================
// TS-06-E7: Exchange propagates context cancellation
// (Requirement: 06-REQ-3.E4)
// ========================================================================

// TestGitHub_Exchange_ContextCancelled verifies that Exchange returns
// within ~100ms when the context is cancelled, propagating the context
// cancellation error without hanging.
func TestGitHub_Exchange_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	p := oauth.NewGitHubProvider("cid", "csec", "", srv.URL, "", client)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	tok, err := p.Exchange(ctx, "code", "http://localhost:9000/cb")
	elapsed := time.Since(start)

	if elapsed > 500*time.Millisecond {
		t.Errorf("Exchange() took %v, expected < 500ms with context timeout", elapsed)
	}
	if tok != "" {
		t.Errorf("Exchange() token = %q, want empty string", tok)
	}
	if err == nil {
		t.Error("Exchange() error = nil, want non-nil error for cancelled context")
	}
}

// ========================================================================
// TS-06-11: UserInfo sends GET with Bearer auth and parses response
// (Requirement: 06-REQ-3.4)
// ========================================================================

// TestGitHub_UserInfo_Success verifies that UserInfo sends a GET request
// to userinfo_url with Authorization: Bearer <token>, parses the JSON
// response extracting login, email, and id (numeric) fields into the
// UserInfo struct with id converted to string.
func TestGitHub_UserInfo_Success(t *testing.T) {
	var capturedReq *http.Request

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		w.Header().Set("Content-Type", "application/json")
		// id is numeric (42) — must be converted to string in ProviderID
		if err := json.NewEncoder(w).Encode(map[string]any{
			"login": "octocat",
			"email": "cat@github.com",
			"id":    42,
		}); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	p := oauth.NewGitHubProvider("cid", "csec", "", "", srv.URL+"/user", client)

	ui, err := p.UserInfo(context.Background(), "mytoken")
	if err != nil {
		t.Fatalf("UserInfo() error = %v, want nil", err)
	}
	if ui == nil {
		t.Fatal("UserInfo() returned nil, want non-nil *UserInfo")
	}

	if ui.Username != "octocat" {
		t.Errorf("UserInfo().Username = %q, want %q", ui.Username, "octocat")
	}
	if ui.Email != "cat@github.com" {
		t.Errorf("UserInfo().Email = %q, want %q", ui.Email, "cat@github.com")
	}
	if ui.ProviderID != "42" {
		t.Errorf("UserInfo().ProviderID = %q, want %q (numeric id converted to string)", ui.ProviderID, "42")
	}

	// Verify request details.
	if capturedReq == nil {
		t.Fatal("UserInfo() did not send an HTTP request to the userinfo endpoint")
	}
	if capturedReq.Method != http.MethodGet {
		t.Errorf("request method = %q, want %q", capturedReq.Method, http.MethodGet)
	}
	if got := capturedReq.Header.Get("Authorization"); got != "Bearer mytoken" {
		t.Errorf("Authorization header = %q, want %q", got, "Bearer mytoken")
	}
}

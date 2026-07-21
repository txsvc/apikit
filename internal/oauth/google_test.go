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
// TestGoogle_InterfaceAndName verifies that GoogleProvider satisfies the
// Provider interface at compile time, Name() returns "google", and default
// URLs are used when config overrides are empty.
// ========================================================================

func TestGoogle_InterfaceAndName(t *testing.T) {
	var _ oauth.Provider = &oauth.GoogleProvider{}

	client := &http.Client{Timeout: 30 * time.Second}
	p := oauth.NewGoogleProvider("cid", "secret", "", "", "", client)

	if name := p.Name(); name != "google" {
		t.Errorf("Name() = %q, want %q", name, "google")
	}
}

// ========================================================================
// TestGoogle_AuthorizeURL verifies that AuthorizeURL constructs the correct
// URL with client_id, response_type=code, scope=openid email profile,
// state, and redirect_uri as query parameters.
// ========================================================================

func TestGoogle_AuthorizeURL(t *testing.T) {
	client := &http.Client{Timeout: 30 * time.Second}
	p := oauth.NewGoogleProvider("myclient", "secret", "", "", "", client)

	result := p.AuthorizeURL("mystate", "http://localhost:9000/cb")

	parsed, err := url.Parse(result)
	if err != nil {
		t.Fatalf("failed to parse AuthorizeURL result: %v", err)
	}

	if parsed.Host != "accounts.google.com" {
		t.Errorf("Host = %q, want %q", parsed.Host, "accounts.google.com")
	}
	if parsed.Path != "/o/oauth2/v2/auth" {
		t.Errorf("Path = %q, want %q", parsed.Path, "/o/oauth2/v2/auth")
	}

	q := parsed.Query()

	if got := q.Get("client_id"); got != "myclient" {
		t.Errorf("client_id = %q, want %q", got, "myclient")
	}
	if got := q.Get("response_type"); got != "code" {
		t.Errorf("response_type = %q, want %q", got, "code")
	}
	if got := q.Get("scope"); got != "openid email profile" {
		t.Errorf("scope = %q, want %q", got, "openid email profile")
	}
	if got := q.Get("state"); got != "mystate" {
		t.Errorf("state = %q, want %q", got, "mystate")
	}
	if got := q.Get("redirect_uri"); got != "http://localhost:9000/cb" {
		t.Errorf("redirect_uri = %q, want %q", got, "http://localhost:9000/cb")
	}
}

// ========================================================================
// TestGoogle_AuthorizeURLCustom verifies that AuthorizeURL uses a custom
// authorize URL when provided as an override.
// ========================================================================

func TestGoogle_AuthorizeURLCustom(t *testing.T) {
	client := &http.Client{Timeout: 30 * time.Second}
	p := oauth.NewGoogleProvider("cid", "secret", "https://custom.example.com/auth", "", "", client)

	result := p.AuthorizeURL("s", "http://localhost:1234/cb")

	parsed, err := url.Parse(result)
	if err != nil {
		t.Fatalf("failed to parse AuthorizeURL result: %v", err)
	}

	if parsed.Host != "custom.example.com" {
		t.Errorf("Host = %q, want %q (custom override)", parsed.Host, "custom.example.com")
	}
}

// ========================================================================
// TestGoogle_InjectedHTTPClient verifies that the Google provider stores
// the injected *http.Client reference.
// ========================================================================

func TestGoogle_InjectedHTTPClient(t *testing.T) {
	customClient := &http.Client{Timeout: 30 * time.Second}
	p := oauth.NewGoogleProvider("cid", "secret", "", "", "", customClient)

	if p == nil {
		t.Fatal("NewGoogleProvider returned nil")
	}

	if name := p.Name(); name != "google" {
		t.Errorf("Name() = %q, want %q", name, "google")
	}

	if got := p.HTTPClient(); got != customClient {
		t.Errorf("HTTPClient() = %p, want %p (should be the injected client)", got, customClient)
	}
}

// ========================================================================
// TestGoogle_Exchange_Success verifies that Exchange sends a POST request
// to token_url with form-encoded body containing client_id, client_secret,
// code, redirect_uri, and grant_type=authorization_code.
// ========================================================================

func TestGoogle_Exchange_Success(t *testing.T) {
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
	p := oauth.NewGoogleProvider("cid", "csec", "", srv.URL+"/token", "", client)

	tok, err := p.Exchange(context.Background(), "authcode", "http://localhost:9000/cb")
	if err != nil {
		t.Fatalf("Exchange() error = %v, want nil", err)
	}
	if tok != "tok123" {
		t.Errorf("Exchange() token = %q, want %q", tok, "tok123")
	}

	if capturedReq == nil {
		t.Fatal("Exchange() did not send an HTTP request to the token endpoint")
	}
	if capturedReq.Method != http.MethodPost {
		t.Errorf("request method = %q, want %q", capturedReq.Method, http.MethodPost)
	}

	if got := capturedReq.Header.Get("Accept"); got != "application/json" {
		t.Errorf("Accept header = %q, want %q", got, "application/json")
	}

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
	if got := form.Get("grant_type"); got != "authorization_code" {
		t.Errorf("form grant_type = %q, want %q", got, "authorization_code")
	}
}

// ========================================================================
// TestGoogle_Exchange_ProviderError verifies that Exchange returns a
// non-nil error when the token endpoint responds with a JSON error field.
// ========================================================================

func TestGoogle_Exchange_ProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"error":"invalid_grant","error_description":"Code was already redeemed."}`)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	p := oauth.NewGoogleProvider("cid", "csec", "", srv.URL, "", client)

	tok, err := p.Exchange(context.Background(), "badcode", "http://localhost:9000/cb")
	if tok != "" {
		t.Errorf("Exchange() token = %q, want empty string", tok)
	}
	if err == nil {
		t.Fatal("Exchange() error = nil, want non-nil error for provider error response")
	}
	if !strings.Contains(err.Error(), "invalid_grant") {
		t.Errorf("Exchange() error = %q, want it to contain %q", err.Error(), "invalid_grant")
	}
}

// ========================================================================
// TestGoogle_Exchange_MissingToken verifies that Exchange returns a
// non-nil error when the token endpoint responds with JSON that has no
// access_token field.
// ========================================================================

func TestGoogle_Exchange_MissingToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"token_type":"bearer"}`)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	p := oauth.NewGoogleProvider("cid", "csec", "", srv.URL, "", client)

	tok, err := p.Exchange(context.Background(), "code", "http://localhost:9000/cb")
	if tok != "" {
		t.Errorf("Exchange() token = %q, want empty string", tok)
	}
	if err == nil {
		t.Fatal("Exchange() error = nil, want non-nil error indicating token was not returned")
	}
}

// ========================================================================
// TestGoogle_Exchange_ServerError verifies that Exchange returns a non-nil
// error when the token endpoint returns HTTP 500, and does not retry.
// ========================================================================

func TestGoogle_Exchange_ServerError(t *testing.T) {
	requestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	p := oauth.NewGoogleProvider("cid", "csec", "", srv.URL, "", client)

	start := time.Now()
	tok, err := p.Exchange(context.Background(), "code", "http://localhost:9000/cb")
	elapsed := time.Since(start)

	if tok != "" {
		t.Errorf("Exchange() token = %q, want empty string", tok)
	}
	if err == nil {
		t.Fatal("Exchange() error = nil, want non-nil error for HTTP 500")
	}

	if elapsed > 2*time.Second {
		t.Errorf("Exchange() took %v, expected < 2s (no retry)", elapsed)
	}
	if requestCount > 1 {
		t.Errorf("Exchange() sent %d requests, expected 1 (no retry)", requestCount)
	}
}

// ========================================================================
// TestGoogle_Exchange_ContextCancelled verifies that Exchange returns
// within ~100ms when the context is cancelled.
// ========================================================================

func TestGoogle_Exchange_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	p := oauth.NewGoogleProvider("cid", "csec", "", srv.URL, "", client)

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
// TestGoogle_UserInfo_Success verifies that UserInfo sends a GET request
// to userinfo_url with Authorization: Bearer <token>, parses the JSON
// response extracting sub, name, and email fields into UserInfo.
// ========================================================================

func TestGoogle_UserInfo_Success(t *testing.T) {
	var capturedReq *http.Request

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"sub":   "110248495921238986420",
			"name":  "Jane Doe",
			"email": "jane@example.com",
		}); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	p := oauth.NewGoogleProvider("cid", "csec", "", "", srv.URL, client)

	ui, err := p.UserInfo(context.Background(), "mytoken")
	if err != nil {
		t.Fatalf("UserInfo() error = %v, want nil", err)
	}
	if ui == nil {
		t.Fatal("UserInfo() returned nil, want non-nil *UserInfo")
	}

	if ui.Username != "Jane Doe" {
		t.Errorf("UserInfo().Username = %q, want %q", ui.Username, "Jane Doe")
	}
	if ui.Email != "jane@example.com" {
		t.Errorf("UserInfo().Email = %q, want %q", ui.Email, "jane@example.com")
	}
	if ui.ProviderID != "110248495921238986420" {
		t.Errorf("UserInfo().ProviderID = %q, want %q", ui.ProviderID, "110248495921238986420")
	}

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

// ========================================================================
// TestGoogle_UserInfo_EmptyEmail verifies that UserInfo returns an error
// when the userinfo endpoint does not return an email.
// ========================================================================

func TestGoogle_UserInfo_EmptyEmail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"sub":  "12345",
			"name": "No Email User",
		}); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	p := oauth.NewGoogleProvider("cid", "csec", "", "", srv.URL, client)

	ui, err := p.UserInfo(context.Background(), "tok")
	if ui != nil {
		t.Errorf("UserInfo() = %+v, want nil", ui)
	}
	if err == nil {
		t.Fatal("UserInfo() error = nil, want non-nil error for missing email")
	}
	if !strings.Contains(err.Error(), "email") {
		t.Errorf("UserInfo() error = %q, want it to mention email", err.Error())
	}
}

// ========================================================================
// TestGoogle_UserInfo_FallbackUsername verifies that UserInfo uses
// given_name as the username when name is empty.
// ========================================================================

func TestGoogle_UserInfo_FallbackUsername(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"sub":        "12345",
			"given_name": "Jane",
			"email":      "jane@example.com",
		}); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	p := oauth.NewGoogleProvider("cid", "csec", "", "", srv.URL, client)

	ui, err := p.UserInfo(context.Background(), "tok")
	if err != nil {
		t.Fatalf("UserInfo() error = %v, want nil", err)
	}

	if ui.Username != "Jane" {
		t.Errorf("UserInfo().Username = %q, want %q (should fall back to given_name)", ui.Username, "Jane")
	}
}

// ========================================================================
// TestGoogle_UserInfo_ServerError verifies that UserInfo returns a non-nil
// error when the userinfo endpoint returns a non-2xx status.
// ========================================================================

func TestGoogle_UserInfo_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	p := oauth.NewGoogleProvider("cid", "csec", "", "", srv.URL, client)

	ui, err := p.UserInfo(context.Background(), "tok")
	if ui != nil {
		t.Errorf("UserInfo() = %+v, want nil", ui)
	}
	if err == nil {
		t.Fatal("UserInfo() error = nil, want non-nil error for HTTP 403")
	}
}

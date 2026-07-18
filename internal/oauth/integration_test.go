package oauth_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/txsvc/apikit/internal/db"
	"github.com/txsvc/apikit/internal/oauth"
)

// ========================================================================
// Integration test infrastructure
// ========================================================================

// testProvider is a configurable mock provider for integration tests.
// It tracks call counts and captures arguments for assertion.
type testProvider struct {
	name                string
	authURL             string
	exchangeFn          func(ctx context.Context, code, redirectURI string) (string, error)
	userInfoFn          func(ctx context.Context, accessToken string) (*oauth.UserInfo, error)
	exchangeCalls       int
	userInfoCalls       int
	lastExchangeCode    string
	lastExchangeRedirect string
	lastUserInfoToken   string
}

func (p *testProvider) Name() string { return p.name }

func (p *testProvider) AuthorizeURL(_, _ string) string {
	if p.authURL != "" {
		return p.authURL
	}
	return ""
}

func (p *testProvider) Exchange(ctx context.Context, code, redirectURI string) (string, error) {
	p.exchangeCalls++
	p.lastExchangeCode = code
	p.lastExchangeRedirect = redirectURI
	if p.exchangeFn != nil {
		return p.exchangeFn(ctx, code, redirectURI)
	}
	return "test-access-token", nil
}

func (p *testProvider) UserInfo(ctx context.Context, accessToken string) (*oauth.UserInfo, error) {
	p.userInfoCalls++
	p.lastUserInfoToken = accessToken
	if p.userInfoFn != nil {
		return p.userInfoFn(ctx, accessToken)
	}
	return &oauth.UserInfo{
		Username:   "testuser",
		Email:      "test@example.com",
		ProviderID: "12345",
	}, nil
}

// Compile-time check that testProvider implements oauth.Provider.
var _ oauth.Provider = (*testProvider)(nil)

// openTestDB creates an in-memory SQLite database with full schema for testing.
func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// setupIntegrationEcho creates a full Echo server with RegisterOAuthHandlers.
// Pass nil for providers to create an empty registry, nil for database to skip
// database operations, and empty string for externalURL for no external URL.
func setupIntegrationEcho(t *testing.T, providers []oauth.Provider, database *db.DB, externalURL string) *echo.Echo {
	t.Helper()
	e := echo.New()
	registry := oauth.NewRegistry()
	for _, p := range providers {
		if err := registry.Register(p); err != nil {
			t.Fatalf("register provider: %v", err)
		}
	}
	g := e.Group("")
	oauth.RegisterOAuthHandlers(g, registry, database, externalURL)
	return e
}

// getProviders sends a GET /auth/providers request and returns the recorder.
func getProviders(e *echo.Echo) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/auth/providers", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// postCallbackJSON sends a POST /auth/callback with the given JSON body string.
func postCallbackJSON(e *echo.Echo, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/auth/callback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// integrationError represents the JSON error envelope from the callback handler.
type integrationError struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// parseIntegrationError decodes the error response body.
func parseIntegrationError(t *testing.T, body string) integrationError {
	t.Helper()
	var resp integrationError
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("parse error response: %v (body: %s)", err, body)
	}
	return resp
}

// providerEntry represents a single entry in the GET /auth/providers response.
type providerEntry struct {
	Name         string `json:"name"`
	AuthorizeURL string `json:"authorize_url"`
}

// ========================================================================
// TS-06-18: GET /auth/providers returns 200 with provider list
// (Requirement: 06-REQ-5.1)
// ========================================================================

// TestIntegration_ProvidersListWithProvider verifies that GET /auth/providers
// returns HTTP 200 with a JSON array containing name and authorize_url for
// each registered provider.
func TestIntegration_ProvidersListWithProvider(t *testing.T) {
	ghProvider := oauth.NewGitHubProvider("cid", "secret", "", "", "", &http.Client{})
	e := setupIntegrationEcho(t, []oauth.Provider{ghProvider}, nil, "")

	rec := getProviders(e)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body []providerEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse response: %v", err)
	}

	if len(body) != 1 {
		t.Fatalf("len(body) = %d, want 1", len(body))
	}

	if body[0].Name != "github" {
		t.Errorf("name = %q, want %q", body[0].Name, "github")
	}

	if !strings.Contains(body[0].AuthorizeURL, "client_id=cid") {
		t.Errorf("authorize_url %q missing client_id=cid", body[0].AuthorizeURL)
	}
	if !strings.Contains(body[0].AuthorizeURL, "scope=user%3Aemail") {
		t.Errorf("authorize_url %q missing scope=user%%3Aemail", body[0].AuthorizeURL)
	}
}

// ========================================================================
// TS-06-19: GET /auth/providers returns empty array when no providers
// (Requirement: 06-REQ-5.2)
// ========================================================================

// TestIntegration_ProvidersEmptyRegistry verifies that GET /auth/providers
// returns HTTP 200 with an empty JSON array when no providers are registered.
func TestIntegration_ProvidersEmptyRegistry(t *testing.T) {
	e := setupIntegrationEcho(t, nil, nil, "")

	rec := getProviders(e)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body []providerEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse response: %v", err)
	}

	if len(body) != 0 {
		t.Errorf("len(body) = %d, want 0 (empty array)", len(body))
	}
}

// ========================================================================
// TS-06-20: GET /auth/providers includes Cache-Control header
// (Requirement: 06-REQ-5.3)
// ========================================================================

// TestIntegration_ProvidersCacheControl verifies that the GET /auth/providers
// response includes Cache-Control: public, max-age=300.
func TestIntegration_ProvidersCacheControl(t *testing.T) {
	e := setupIntegrationEcho(t, nil, nil, "")

	rec := getProviders(e)

	cc := rec.Header().Get("Cache-Control")
	if cc != "public, max-age=300" {
		t.Errorf("Cache-Control = %q, want %q", cc, "public, max-age=300")
	}
}

// ========================================================================
// TS-06-21: GET /auth/providers does not expose secrets
// (Requirement: 06-REQ-5.4)
// ========================================================================

// TestIntegration_ProvidersNoSecrets verifies that the GET /auth/providers
// response JSON objects contain only 'name' and 'authorize_url' fields.
// No client_secret, token_url, or userinfo_url should appear.
func TestIntegration_ProvidersNoSecrets(t *testing.T) {
	ghProvider := oauth.NewGitHubProvider(
		"cid", "supersecret",
		"", // authorizeURL default
		"https://custom.token.url/token",
		"https://custom.userinfo.url/user",
		&http.Client{},
	)
	e := setupIntegrationEcho(t, []oauth.Provider{ghProvider}, nil, "")

	rec := getProviders(e)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Parse as generic JSON to check field names.
	var body []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse response: %v", err)
	}

	for i, entry := range body {
		for key := range entry {
			if key != "name" && key != "authorize_url" {
				t.Errorf("entry[%d] has unexpected key %q", i, key)
			}
		}
		if _, ok := entry["client_secret"]; ok {
			t.Error("response contains client_secret")
		}
		if _, ok := entry["token_url"]; ok {
			t.Error("response contains token_url")
		}
		if _, ok := entry["userinfo_url"]; ok {
			t.Error("response contains userinfo_url")
		}
	}

	// Verify the raw body does not contain the secret string.
	raw := rec.Body.String()
	if strings.Contains(raw, "supersecret") {
		t.Error("response body contains the literal client_secret value")
	}
}

// ========================================================================
// TS-06-31: Disallowed redirect_uri returns 400 and Exchange is not called
// (Requirement: 06-REQ-7.4)
// ========================================================================

// TestIntegration_CallbackDisallowedRedirectURI verifies that POST /auth/callback
// returns HTTP 400 with 'redirect_uri is not allowed' when the redirect_uri
// fails allowlist validation, and that provider.Exchange is never called.
func TestIntegration_CallbackDisallowedRedirectURI(t *testing.T) {
	p := &testProvider{name: "github"}
	e := setupIntegrationEcho(t, []oauth.Provider{p}, nil, "")

	body := `{"provider":"github","code":"abc","redirect_uri":"https://evil.example.com/steal"}`
	rec := postCallbackJSON(e, body)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	resp := parseIntegrationError(t, rec.Body.String())
	if resp.Error.Message != "redirect_uri is not allowed" {
		t.Errorf("message = %q, want %q", resp.Error.Message, "redirect_uri is not allowed")
	}

	if p.exchangeCalls != 0 {
		t.Errorf("Exchange call count = %d, want 0 (should never be called)", p.exchangeCalls)
	}
}

// ========================================================================
// TS-06-32: Valid request causes Exchange to be called with correct args
// (Requirement: 06-REQ-8.1)
// ========================================================================

// TestIntegration_CallbackExchangeCalled verifies that after successful provider
// lookup and redirect URI validation, the callback handler calls
// provider.Exchange with the correct code and redirect_uri.
func TestIntegration_CallbackExchangeCalled(t *testing.T) {
	p := &testProvider{
		name: "github",
		exchangeFn: func(_ context.Context, code, redirectURI string) (string, error) {
			return "token123", nil
		},
		userInfoFn: func(_ context.Context, _ string) (*oauth.UserInfo, error) {
			return &oauth.UserInfo{
				Username:   "octocat",
				Email:      "cat@github.com",
				ProviderID: "1",
			}, nil
		},
	}
	database := openTestDB(t)
	e := setupIntegrationEcho(t, []oauth.Provider{p}, database, "")

	body := `{"provider":"github","code":"mycode","redirect_uri":"http://localhost:9000/cb"}`
	_ = postCallbackJSON(e, body)

	if p.exchangeCalls == 0 {
		t.Fatal("Exchange was never called; want at least 1 call")
	}
	if p.lastExchangeCode != "mycode" {
		t.Errorf("Exchange code = %q, want %q", p.lastExchangeCode, "mycode")
	}
	if p.lastExchangeRedirect != "http://localhost:9000/cb" {
		t.Errorf("Exchange redirectURI = %q, want %q", p.lastExchangeRedirect, "http://localhost:9000/cb")
	}
}

// ========================================================================
// TS-06-33: Exchange error returns HTTP 401
// (Requirement: 06-REQ-8.2)
// ========================================================================

// TestIntegration_CallbackExchangeError verifies that when provider.Exchange
// returns an error, the callback handler returns HTTP 401 with message
// "authorization code exchange failed".
func TestIntegration_CallbackExchangeError(t *testing.T) {
	p := &testProvider{
		name: "github",
		exchangeFn: func(_ context.Context, _, _ string) (string, error) {
			return "", errors.New("bad code")
		},
	}
	e := setupIntegrationEcho(t, []oauth.Provider{p}, nil, "")

	body := `{"provider":"github","code":"invalid","redirect_uri":"http://localhost:9000/cb"}`
	rec := postCallbackJSON(e, body)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	resp := parseIntegrationError(t, rec.Body.String())
	if resp.Error.Message != "authorization code exchange failed" {
		t.Errorf("message = %q, want %q", resp.Error.Message, "authorization code exchange failed")
	}
}

// ========================================================================
// TS-06-E12: Context cancelled during Exchange returns HTTP 401
// (Requirement: 06-REQ-8.E1)
// ========================================================================

// TestIntegration_CallbackContextCancelledDuringExchange verifies that when
// the request context is cancelled while Exchange is in flight, the handler
// returns HTTP 401 without hanging or leaking goroutines.
func TestIntegration_CallbackContextCancelledDuringExchange(t *testing.T) {
	p := &testProvider{
		name: "github",
		exchangeFn: func(ctx context.Context, _, _ string) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		},
	}
	e := setupIntegrationEcho(t, []oauth.Provider{p}, nil, "")

	body := `{"provider":"github","code":"abc","redirect_uri":"http://localhost:9000/cb"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/callback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	ctx, cancel := context.WithTimeout(req.Context(), 200*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// Handler should return 401 when Exchange returns a context error.
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// ========================================================================
// TS-06-34: UserInfo called with correct access token after Exchange
// (Requirement: 06-REQ-9.1)
// ========================================================================

// TestIntegration_CallbackUserInfoCalled verifies that after a successful
// Exchange, the callback handler calls provider.UserInfo with the access token.
func TestIntegration_CallbackUserInfoCalled(t *testing.T) {
	p := &testProvider{
		name: "github",
		exchangeFn: func(_ context.Context, _, _ string) (string, error) {
			return "tok123", nil
		},
		userInfoFn: func(_ context.Context, accessToken string) (*oauth.UserInfo, error) {
			return &oauth.UserInfo{
				Username:   "octocat",
				Email:      "cat@github.com",
				ProviderID: "1",
			}, nil
		},
	}
	database := openTestDB(t)
	e := setupIntegrationEcho(t, []oauth.Provider{p}, database, "")

	body := `{"provider":"github","code":"abc","redirect_uri":"http://localhost:9000/cb"}`
	_ = postCallbackJSON(e, body)

	if p.userInfoCalls == 0 {
		t.Fatal("UserInfo was never called; want at least 1 call")
	}
	if p.lastUserInfoToken != "tok123" {
		t.Errorf("UserInfo accessToken = %q, want %q", p.lastUserInfoToken, "tok123")
	}
}

// ========================================================================
// TS-06-35: UserInfo error returns HTTP 502
// (Requirement: 06-REQ-9.2)
// ========================================================================

// TestIntegration_CallbackUserInfoError verifies that when provider.UserInfo
// returns an error, the callback handler returns HTTP 502 with message
// "failed to retrieve user info from provider".
func TestIntegration_CallbackUserInfoError(t *testing.T) {
	p := &testProvider{
		name: "github",
		exchangeFn: func(_ context.Context, _, _ string) (string, error) {
			return "token", nil
		},
		userInfoFn: func(_ context.Context, _ string) (*oauth.UserInfo, error) {
			return nil, errors.New("upstream failed")
		},
	}
	e := setupIntegrationEcho(t, []oauth.Provider{p}, nil, "")

	body := `{"provider":"github","code":"abc","redirect_uri":"http://localhost:9000/cb"}`
	rec := postCallbackJSON(e, body)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}

	resp := parseIntegrationError(t, rec.Body.String())
	if resp.Error.Message != "failed to retrieve user info from provider" {
		t.Errorf("message = %q, want %q", resp.Error.Message, "failed to retrieve user info from provider")
	}
}

// ========================================================================
// TS-06-36: UserInfo returns empty Email → HTTP 400
// (Requirement: 06-REQ-9.3)
// ========================================================================

// TestIntegration_CallbackUserInfoEmptyEmail verifies that when UserInfo
// returns a *UserInfo with an empty Email, the callback handler returns
// HTTP 400 with message "provider returned empty email; email is required".
func TestIntegration_CallbackUserInfoEmptyEmail(t *testing.T) {
	p := &testProvider{
		name: "github",
		exchangeFn: func(_ context.Context, _, _ string) (string, error) {
			return "token", nil
		},
		userInfoFn: func(_ context.Context, _ string) (*oauth.UserInfo, error) {
			return &oauth.UserInfo{
				Username:   "octocat",
				Email:      "",
				ProviderID: "1",
			}, nil
		},
	}
	e := setupIntegrationEcho(t, []oauth.Provider{p}, nil, "")

	body := `{"provider":"github","code":"abc","redirect_uri":"http://localhost:9000/cb"}`
	rec := postCallbackJSON(e, body)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	resp := parseIntegrationError(t, rec.Body.String())
	if resp.Error.Message != "provider returned empty email; email is required" {
		t.Errorf("message = %q, want %q", resp.Error.Message, "provider returned empty email; email is required")
	}
}

// ========================================================================
// TS-06-E13: UserInfo upstream 401 returns HTTP 502
// (Requirement: 06-REQ-9.E1)
// ========================================================================

// TestIntegration_CallbackUserInfoUpstream401 verifies that when the identity
// provider's user info API returns 401 (token invalid), the callback handler
// returns HTTP 502 without retrying.
func TestIntegration_CallbackUserInfoUpstream401(t *testing.T) {
	callCount := 0
	p := &testProvider{
		name: "github",
		exchangeFn: func(_ context.Context, _, _ string) (string, error) {
			return "token", nil
		},
		userInfoFn: func(_ context.Context, _ string) (*oauth.UserInfo, error) {
			callCount++
			return nil, errors.New("401 Unauthorized")
		},
	}
	e := setupIntegrationEcho(t, []oauth.Provider{p}, nil, "")

	body := `{"provider":"github","code":"abc","redirect_uri":"http://localhost:9000/cb"}`
	rec := postCallbackJSON(e, body)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}

	resp := parseIntegrationError(t, rec.Body.String())
	if resp.Error.Message != "failed to retrieve user info from provider" {
		t.Errorf("message = %q, want %q", resp.Error.Message, "failed to retrieve user info from provider")
	}

	// Verify no retry — UserInfo should be called exactly once.
	if callCount != 1 {
		t.Errorf("UserInfo call count = %d, want 1 (no retry)", callCount)
	}
}

// ========================================================================
// TS-06-37: New user inserted into users table after successful user info
// (Requirement: 06-REQ-10.1)
// ========================================================================

// TestIntegration_CallbackNewUserInserted verifies that the callback handler
// opens a database transaction and inserts a new user when no matching
// (provider, provider_id) record exists.
func TestIntegration_CallbackNewUserInserted(t *testing.T) {
	database := openTestDB(t)
	p := &testProvider{
		name: "github",
		exchangeFn: func(_ context.Context, _, _ string) (string, error) {
			return "token", nil
		},
		userInfoFn: func(_ context.Context, _ string) (*oauth.UserInfo, error) {
			return &oauth.UserInfo{
				Username:   "newuser",
				Email:      "new@example.com",
				ProviderID: "99",
			}, nil
		},
	}
	e := setupIntegrationEcho(t, []oauth.Provider{p}, database, "")

	body := `{"provider":"github","code":"abc","redirect_uri":"http://localhost:9000/cb"}`
	rec := postCallbackJSON(e, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Verify user was created in the database.
	var id, username, email, provider, providerID string
	err := database.SqlDB.QueryRow(
		"SELECT id, username, email, provider, provider_id FROM users WHERE provider = 'github' AND provider_id = '99'",
	).Scan(&id, &username, &email, &provider, &providerID)
	if err != nil {
		t.Fatalf("query users: %v (user should have been created)", err)
	}

	if id == "" {
		t.Error("user.id is empty; want non-empty UUID")
	}
	if username != "newuser" {
		t.Errorf("username = %q, want %q", username, "newuser")
	}
	if email != "new@example.com" {
		t.Errorf("email = %q, want %q", email, "new@example.com")
	}
}

// ========================================================================
// TS-06-38: New user gets admin role when email matches admin_email
// (Requirement: 06-REQ-10.2)
// ========================================================================

// TestIntegration_CallbackNewUserAdminRole verifies that a new user whose
// email matches admin_email in admin_config gets role='admin' when no admin
// exists yet.
func TestIntegration_CallbackNewUserAdminRole(t *testing.T) {
	database := openTestDB(t)

	// Set admin_email in admin_config.
	_, err := database.SqlDB.Exec(
		"INSERT INTO admin_config (key, value) VALUES ('admin_email', 'admin@example.com')",
	)
	if err != nil {
		t.Fatalf("insert admin_config: %v", err)
	}

	p := &testProvider{
		name: "github",
		exchangeFn: func(_ context.Context, _, _ string) (string, error) {
			return "token", nil
		},
		userInfoFn: func(_ context.Context, _ string) (*oauth.UserInfo, error) {
			return &oauth.UserInfo{
				Username:   "newadmin",
				Email:      "admin@example.com",
				ProviderID: "55",
			}, nil
		},
	}
	e := setupIntegrationEcho(t, []oauth.Provider{p}, database, "")

	body := `{"provider":"github","code":"abc","redirect_uri":"http://localhost:9000/cb"}`
	rec := postCallbackJSON(e, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Verify user was created with admin role.
	var role, status string
	err = database.SqlDB.QueryRow(
		"SELECT role, status FROM users WHERE provider_id = '55'",
	).Scan(&role, &status)
	if err != nil {
		t.Fatalf("query users: %v", err)
	}
	if role != "admin" {
		t.Errorf("role = %q, want %q", role, "admin")
	}
	if status != "active" {
		t.Errorf("status = %q, want %q", status, "active")
	}
}

// ========================================================================
// TS-06-39: Existing active user's fields are updated on re-login
// (Requirement: 06-REQ-10.3)
// ========================================================================

// TestIntegration_CallbackExistingUserUpdated verifies that when an existing
// active user re-logs in, their username, email, and updated_at are updated
// while id, role, status, and created_at remain unchanged.
func TestIntegration_CallbackExistingUserUpdated(t *testing.T) {
	database := openTestDB(t)

	// Pre-insert an existing user.
	originalCreatedAt := db.FormatTime(time.Now().UTC().Add(-24 * time.Hour))
	originalUpdatedAt := db.FormatTime(time.Now().UTC().Add(-1 * time.Hour))
	_, err := database.SqlDB.Exec(
		`INSERT INTO users (id, username, email, full_name, role, status, provider, provider_id, created_at, updated_at)
		 VALUES (?, ?, ?, NULL, ?, ?, ?, ?, ?, ?)`,
		"existing-uuid-77", "oldname", "old@example.com", "user", "active", "github", "77",
		originalCreatedAt, originalUpdatedAt,
	)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	p := &testProvider{
		name: "github",
		exchangeFn: func(_ context.Context, _, _ string) (string, error) {
			return "token", nil
		},
		userInfoFn: func(_ context.Context, _ string) (*oauth.UserInfo, error) {
			return &oauth.UserInfo{
				Username:   "newname",
				Email:      "new@example.com",
				ProviderID: "77",
			}, nil
		},
	}
	e := setupIntegrationEcho(t, []oauth.Provider{p}, database, "")

	body := `{"provider":"github","code":"abc","redirect_uri":"http://localhost:9000/cb"}`
	rec := postCallbackJSON(e, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Verify updated fields.
	var id, username, email, role, status, createdAt, updatedAt string
	err = database.SqlDB.QueryRow(
		"SELECT id, username, email, role, status, created_at, updated_at FROM users WHERE provider_id = '77'",
	).Scan(&id, &username, &email, &role, &status, &createdAt, &updatedAt)
	if err != nil {
		t.Fatalf("query users: %v", err)
	}

	if username != "newname" {
		t.Errorf("username = %q, want %q", username, "newname")
	}
	if email != "new@example.com" {
		t.Errorf("email = %q, want %q", email, "new@example.com")
	}

	// Fields that should NOT change.
	if id != "existing-uuid-77" {
		t.Errorf("id = %q, want %q (should not change)", id, "existing-uuid-77")
	}
	if role != "user" {
		t.Errorf("role = %q, want %q (should not change)", role, "user")
	}
	if status != "active" {
		t.Errorf("status = %q, want %q (should not change)", status, "active")
	}
	if createdAt != originalCreatedAt {
		t.Errorf("created_at = %q, want %q (should not change)", createdAt, originalCreatedAt)
	}
	if updatedAt == originalUpdatedAt {
		t.Error("updated_at should have changed on re-login")
	}
}

// ========================================================================
// TS-06-40: Blocked user returns HTTP 403
// (Requirement: 06-REQ-10.4)
// ========================================================================

// TestIntegration_CallbackBlockedUser verifies that when a blocked user
// attempts to authenticate, the callback handler returns HTTP 403 with
// "user is blocked" and does not update the user or create a new key.
func TestIntegration_CallbackBlockedUser(t *testing.T) {
	database := openTestDB(t)

	// Pre-insert a blocked user.
	now := db.FormatTime(time.Now().UTC())
	_, err := database.SqlDB.Exec(
		`INSERT INTO users (id, username, email, full_name, role, status, provider, provider_id, created_at, updated_at)
		 VALUES (?, ?, ?, NULL, ?, ?, ?, ?, ?, ?)`,
		"blocked-uuid-88", "blockeduser", "blocked@example.com", "user", "blocked", "github", "88",
		now, now,
	)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	// Count existing api_keys for this user.
	var keyCountBefore int
	err = database.SqlDB.QueryRow(
		"SELECT COUNT(*) FROM api_keys WHERE user_id = 'blocked-uuid-88'",
	).Scan(&keyCountBefore)
	if err != nil {
		t.Fatalf("count api_keys before: %v", err)
	}

	p := &testProvider{
		name: "github",
		exchangeFn: func(_ context.Context, _, _ string) (string, error) {
			return "token", nil
		},
		userInfoFn: func(_ context.Context, _ string) (*oauth.UserInfo, error) {
			return &oauth.UserInfo{
				Username:   "blockeduser",
				Email:      "blocked@example.com",
				ProviderID: "88",
			}, nil
		},
	}
	e := setupIntegrationEcho(t, []oauth.Provider{p}, database, "")

	body := `{"provider":"github","code":"abc","redirect_uri":"http://localhost:9000/cb"}`
	rec := postCallbackJSON(e, body)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	resp := parseIntegrationError(t, rec.Body.String())
	if resp.Error.Message != "user is blocked" {
		t.Errorf("message = %q, want %q", resp.Error.Message, "user is blocked")
	}

	// Verify user status unchanged.
	var status string
	err = database.SqlDB.QueryRow(
		"SELECT status FROM users WHERE provider_id = '88'",
	).Scan(&status)
	if err != nil {
		t.Fatalf("query users: %v", err)
	}
	if status != "blocked" {
		t.Errorf("status = %q, want %q (should remain blocked)", status, "blocked")
	}

	// Verify no new api_key was created.
	var keyCountAfter int
	err = database.SqlDB.QueryRow(
		"SELECT COUNT(*) FROM api_keys WHERE user_id = 'blocked-uuid-88'",
	).Scan(&keyCountAfter)
	if err != nil {
		t.Fatalf("count api_keys after: %v", err)
	}
	if keyCountAfter != keyCountBefore {
		t.Errorf("api_keys count changed from %d to %d; want no new keys", keyCountBefore, keyCountAfter)
	}
}

// ========================================================================
// TS-06-E14: DB error returns HTTP 500 with transaction rolled back
// (Requirement: 06-REQ-10.E1)
// ========================================================================

// TestIntegration_CallbackDBError verifies that when a database error occurs
// during user upsert, the callback handler returns HTTP 500 with transaction
// rolled back and no partial data.
func TestIntegration_CallbackDBError(t *testing.T) {
	database := openTestDB(t)

	// Break the DB by renaming a required column to cause INSERT failure.
	_, err := database.SqlDB.Exec("ALTER TABLE users RENAME COLUMN email TO email_old")
	if err != nil {
		t.Fatalf("alter table: %v", err)
	}

	p := &testProvider{
		name: "github",
		exchangeFn: func(_ context.Context, _, _ string) (string, error) {
			return "token", nil
		},
		userInfoFn: func(_ context.Context, _ string) (*oauth.UserInfo, error) {
			return &oauth.UserInfo{
				Username:   "erruser",
				Email:      "err@example.com",
				ProviderID: "999",
			}, nil
		},
	}
	e := setupIntegrationEcho(t, []oauth.Provider{p}, database, "")

	body := `{"provider":"github","code":"abc","redirect_uri":"http://localhost:9000/cb"}`
	rec := postCallbackJSON(e, body)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	resp := parseIntegrationError(t, rec.Body.String())
	if resp.Error.Code != 500 {
		t.Errorf("error.code = %d, want 500", resp.Error.Code)
	}

	// Verify no partial data was written.
	var count int
	err = database.SqlDB.QueryRow(
		"SELECT COUNT(*) FROM users WHERE provider_id = '999'",
	).Scan(&count)
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 0 {
		t.Errorf("users count = %d, want 0 (no partial data after rollback)", count)
	}
}

// ========================================================================
// TS-06-E15: Empty admin_config → new user gets role='user'
// (Requirement: 06-REQ-10.E2)
// ========================================================================

// TestIntegration_CallbackEmptyAdminConfig verifies that when admin_config
// is empty (no admin_email), a new user is created with role='user'.
func TestIntegration_CallbackEmptyAdminConfig(t *testing.T) {
	database := openTestDB(t)

	// Ensure admin_config is empty.
	_, err := database.SqlDB.Exec("DELETE FROM admin_config")
	if err != nil {
		t.Fatalf("clear admin_config: %v", err)
	}

	p := &testProvider{
		name: "github",
		exchangeFn: func(_ context.Context, _, _ string) (string, error) {
			return "token", nil
		},
		userInfoFn: func(_ context.Context, _ string) (*oauth.UserInfo, error) {
			return &oauth.UserInfo{
				Username:   "regularuser",
				Email:      "user@example.com",
				ProviderID: "101",
			}, nil
		},
	}
	e := setupIntegrationEcho(t, []oauth.Provider{p}, database, "")

	body := `{"provider":"github","code":"abc","redirect_uri":"http://localhost:9000/cb"}`
	rec := postCallbackJSON(e, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Verify role from response body.
	var respBody struct {
		User struct {
			Role string `json:"role"`
		} `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &respBody); err == nil && respBody.User.Role != "" {
		if respBody.User.Role != "user" {
			t.Errorf("response user.role = %q, want %q", respBody.User.Role, "user")
		}
	}

	// Verify role from DB.
	var role string
	err = database.SqlDB.QueryRow(
		"SELECT role FROM users WHERE provider_id = '101'",
	).Scan(&role)
	if err != nil {
		t.Fatalf("query users: %v", err)
	}
	if role != "user" {
		t.Errorf("role = %q, want %q", role, "user")
	}
}

// ========================================================================
// TS-06-E16: Admin already exists → new user with matching email gets 'user'
// (Requirement: 06-REQ-10.E3)
// ========================================================================

// TestIntegration_CallbackAdminAlreadyExists verifies that when an admin
// already exists in users, a new user whose email matches admin_email
// gets role='user' (admin auto-grant fires at most once).
func TestIntegration_CallbackAdminAlreadyExists(t *testing.T) {
	database := openTestDB(t)

	// Set admin_email.
	_, err := database.SqlDB.Exec(
		"INSERT INTO admin_config (key, value) VALUES ('admin_email', 'admin@example.com')",
	)
	if err != nil {
		t.Fatalf("insert admin_config: %v", err)
	}

	// Pre-insert an existing admin user.
	now := db.FormatTime(time.Now().UTC())
	_, err = database.SqlDB.Exec(
		`INSERT INTO users (id, username, email, full_name, role, status, provider, provider_id, created_at, updated_at)
		 VALUES (?, ?, ?, NULL, ?, ?, ?, ?, ?, ?)`,
		"existing-admin-uuid", "existingadmin", "other-admin@example.com", "admin", "active", "github", "200",
		now, now,
	)
	if err != nil {
		t.Fatalf("insert existing admin: %v", err)
	}

	p := &testProvider{
		name: "github",
		exchangeFn: func(_ context.Context, _, _ string) (string, error) {
			return "token", nil
		},
		userInfoFn: func(_ context.Context, _ string) (*oauth.UserInfo, error) {
			return &oauth.UserInfo{
				Username:   "secondadmin",
				Email:      "admin@example.com",
				ProviderID: "202",
			}, nil
		},
	}
	e := setupIntegrationEcho(t, []oauth.Provider{p}, database, "")

	body := `{"provider":"github","code":"abc","redirect_uri":"http://localhost:9000/cb"}`
	rec := postCallbackJSON(e, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Verify new user has role='user', not 'admin'.
	var respBody struct {
		User struct {
			Role string `json:"role"`
		} `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &respBody); err == nil && respBody.User.Role != "" {
		if respBody.User.Role != "user" {
			t.Errorf("response user.role = %q, want %q", respBody.User.Role, "user")
		}
	}

	// Also verify via DB query.
	var role string
	err = database.SqlDB.QueryRow(
		"SELECT role FROM users WHERE provider_id = '202'",
	).Scan(&role)
	if err != nil {
		t.Fatalf("query users: %v", err)
	}
	if role != "user" {
		t.Errorf("role = %q, want %q", role, "user")
	}
}

// ========================================================================
// TS-06-41: Active keys revoked on re-login
// (Requirement: 06-REQ-11.1)
// ========================================================================

// TestIntegration_CallbackKeyRevocation verifies that when a user re-logs in,
// all previously active (non-revoked) API keys are revoked (revoked_at set),
// and a new active key is created.
func TestIntegration_CallbackKeyRevocation(t *testing.T) {
	database := openTestDB(t)

	// Pre-insert a user.
	userID := "revoke-test-user"
	now := db.FormatTime(time.Now().UTC())
	expiresAt := db.FormatTime(time.Now().UTC().Add(90 * 24 * time.Hour))
	_, err := database.SqlDB.Exec(
		`INSERT INTO users (id, username, email, full_name, role, status, provider, provider_id, created_at, updated_at)
		 VALUES (?, ?, ?, NULL, ?, ?, ?, ?, ?, ?)`,
		userID, "revokeuser", "revoke@example.com", "user", "active", "github", "300",
		now, now,
	)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	// Pre-insert an active api_key.
	_, err = database.SqlDB.Exec(
		`INSERT INTO api_keys (key_id, user_id, secret_hash, expires_days, expires_at, revoked_at, created_at)
		 VALUES (?, ?, ?, ?, ?, NULL, ?)`,
		"oldkey01", userID, "oldhashvalue", 90, expiresAt, now,
	)
	if err != nil {
		t.Fatalf("insert api_key: %v", err)
	}

	p := &testProvider{
		name: "github",
		exchangeFn: func(_ context.Context, _, _ string) (string, error) {
			return "token", nil
		},
		userInfoFn: func(_ context.Context, _ string) (*oauth.UserInfo, error) {
			return &oauth.UserInfo{
				Username:   "revokeuser",
				Email:      "revoke@example.com",
				ProviderID: "300",
			}, nil
		},
	}
	e := setupIntegrationEcho(t, []oauth.Provider{p}, database, "")

	body := `{"provider":"github","code":"abc","redirect_uri":"http://localhost:9000/cb"}`
	rec := postCallbackJSON(e, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Verify old key was revoked.
	var revokedAt *string
	err = database.SqlDB.QueryRow(
		"SELECT revoked_at FROM api_keys WHERE key_id = 'oldkey01'",
	).Scan(&revokedAt)
	if err != nil {
		t.Fatalf("query old key: %v", err)
	}
	if revokedAt == nil {
		t.Error("old key revoked_at is NULL; want non-NULL timestamp")
	}

	// Verify exactly one new active key exists.
	var activeCount int
	err = database.SqlDB.QueryRow(
		"SELECT COUNT(*) FROM api_keys WHERE user_id = ? AND revoked_at IS NULL",
		userID,
	).Scan(&activeCount)
	if err != nil {
		t.Fatalf("count active keys: %v", err)
	}
	if activeCount != 1 {
		t.Errorf("active key count = %d, want 1", activeCount)
	}
}

// ========================================================================
// TS-06-42: New key has correct format, hash, and expiry
// (Requirement: 06-REQ-11.2)
// ========================================================================

// TestIntegration_CallbackKeyFormatAndExpiry verifies that a newly generated
// API key has the correct format (ak_<8chars>_<32chars>), its secret_hash
// matches SHA256(plaintext_secret), and expires_at is approximately 30 days
// when expires=30.
func TestIntegration_CallbackKeyFormatAndExpiry(t *testing.T) {
	database := openTestDB(t)
	p := &testProvider{
		name: "github",
		exchangeFn: func(_ context.Context, _, _ string) (string, error) {
			return "token", nil
		},
		userInfoFn: func(_ context.Context, _ string) (*oauth.UserInfo, error) {
			return &oauth.UserInfo{
				Username:   "keyuser",
				Email:      "key@example.com",
				ProviderID: "400",
			}, nil
		},
	}
	e := setupIntegrationEcho(t, []oauth.Provider{p}, database, "")

	body := `{"provider":"github","code":"abc","redirect_uri":"http://localhost:9000/cb","expires":30}`
	rec := postCallbackJSON(e, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Parse response to get the API key.
	var respBody struct {
		APIKey struct {
			Key       string  `json:"key"`
			KeyID     string  `json:"key_id"`
			ExpiresAt *string `json:"expires_at"`
		} `json:"api_key"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &respBody); err != nil {
		t.Fatalf("parse response: %v", err)
	}

	keyStr := respBody.APIKey.Key
	if keyStr == "" {
		t.Fatal("api_key.key is empty")
	}

	// Verify format: ak_<8 alnum>_<32 alnum>
	parts := strings.Split(keyStr, "_")
	if len(parts) != 3 {
		t.Fatalf("key has %d parts (split by _), want 3", len(parts))
	}
	if parts[0] != "ak" {
		t.Errorf("key prefix = %q, want %q", parts[0], "ak")
	}
	if len(parts[1]) != 8 {
		t.Errorf("key_id length = %d, want 8", len(parts[1]))
	}
	if len(parts[2]) != 32 {
		t.Errorf("secret length = %d, want 32", len(parts[2]))
	}

	// Verify key_id in response matches key segment.
	if respBody.APIKey.KeyID != parts[1] {
		t.Errorf("api_key.key_id = %q, want %q (from key)", respBody.APIKey.KeyID, parts[1])
	}

	// Verify secret_hash in DB matches SHA256(plaintext_secret).
	expectedHash := sha256.Sum256([]byte(parts[2]))
	expectedHex := hex.EncodeToString(expectedHash[:])

	var dbSecretHash, dbKeyID string
	var dbExpiresAt *string
	err := database.SqlDB.QueryRow(
		"SELECT key_id, secret_hash, expires_at FROM api_keys WHERE revoked_at IS NULL ORDER BY created_at DESC LIMIT 1",
	).Scan(&dbKeyID, &dbSecretHash, &dbExpiresAt)
	if err != nil {
		t.Fatalf("query api_keys: %v", err)
	}

	if dbSecretHash != expectedHex {
		t.Errorf("secret_hash = %q, want %q", dbSecretHash, expectedHex)
	}
	if dbKeyID != parts[1] {
		t.Errorf("db key_id = %q, want %q", dbKeyID, parts[1])
	}

	// Verify expires_at is approximately 30 days from now.
	if dbExpiresAt == nil {
		t.Fatal("expires_at is NULL; want ~30 days from now")
	}
	expiresTime, err := time.Parse(time.RFC3339, *dbExpiresAt)
	if err != nil {
		// Try the db.TimeFormat as fallback.
		expiresTime, err = db.ParseTime(*dbExpiresAt)
		if err != nil {
			t.Fatalf("parse expires_at %q: %v", *dbExpiresAt, err)
		}
	}
	diff := time.Until(expiresTime)
	lower := 29 * 24 * time.Hour
	upper := 31 * 24 * time.Hour
	if diff < lower || diff > upper {
		t.Errorf("expires_at diff = %v, want between 29 and 31 days", diff)
	}
}

// ========================================================================
// TS-06-E18: First login (no prior keys) → revocation no-op, key created
// (Requirement: 06-REQ-11.E2)
// ========================================================================

// TestIntegration_CallbackFirstLoginNoKeys verifies that for a new user's
// first login (no existing keys), the revocation step is a no-op and a new
// key is created successfully.
func TestIntegration_CallbackFirstLoginNoKeys(t *testing.T) {
	database := openTestDB(t)
	p := &testProvider{
		name: "github",
		exchangeFn: func(_ context.Context, _, _ string) (string, error) {
			return "token", nil
		},
		userInfoFn: func(_ context.Context, _ string) (*oauth.UserInfo, error) {
			return &oauth.UserInfo{
				Username:   "firstlogin",
				Email:      "first@example.com",
				ProviderID: "500",
			}, nil
		},
	}
	e := setupIntegrationEcho(t, []oauth.Provider{p}, database, "")

	body := `{"provider":"github","code":"abc","redirect_uri":"http://localhost:9000/cb"}`
	rec := postCallbackJSON(e, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Parse response to verify an API key was returned.
	var respBody struct {
		APIKey struct {
			Key string `json:"key"`
		} `json:"api_key"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &respBody); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if respBody.APIKey.Key == "" {
		t.Error("api_key.key is empty; want non-empty key on first login")
	}

	// Verify a new key exists in the database.
	var keyCount int
	err := database.SqlDB.QueryRow(
		"SELECT COUNT(*) FROM api_keys WHERE revoked_at IS NULL",
	).Scan(&keyCount)
	if err != nil {
		t.Fatalf("count api_keys: %v", err)
	}
	if keyCount < 1 {
		t.Errorf("active key count = %d, want >= 1", keyCount)
	}
}

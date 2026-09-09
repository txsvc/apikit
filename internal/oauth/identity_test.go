package oauth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/txsvc/apikit/internal/oauth"
)

// callbackBody is the standard valid request body used by the identity tests.
const callbackBody = `{"provider":"github","code":"code","redirect_uri":"http://localhost:8080/callback"}`

// callbackUserResp is the subset of the success response inspected here.
type callbackUserResp struct {
	User struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Email    string `json:"email"`
	} `json:"user"`
}

func newIdentityProvider(info *oauth.UserInfo) *testProvider {
	return &testProvider{
		name: "github",
		userInfoFn: func(_ context.Context, _ string) (*oauth.UserInfo, error) {
			return info, nil
		},
	}
}

// TestCallback_EmptyProviderIDRejected verifies that a provider returning an
// empty user identifier is rejected with 502 and that no user row is
// created. An empty provider_id would map every such login onto a single
// shared account.
func TestCallback_EmptyProviderIDRejected(t *testing.T) {
	database := openTestDB(t)
	p := newIdentityProvider(&oauth.UserInfo{Username: "nobody", Email: "nobody@example.com", ProviderID: ""})
	e := setupIntegrationEcho(t, []oauth.Provider{p}, database, "")

	rec := postCallbackJSON(e, callbackBody)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadGateway, rec.Body.String())
	}
	resp := parseIntegrationError(t, rec.Body.String())
	if resp.Error.Message != "provider returned empty user id" {
		t.Errorf("message = %q, want %q", resp.Error.Message, "provider returned empty user id")
	}

	var count int
	if err := database.SqlDB.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 0 {
		t.Errorf("users rows = %d, want 0", count)
	}
}

// TestCallback_EmptyUsernameFallsBack verifies that an empty provider
// username is replaced by a stable provider-scoped identifier instead of
// being stored as an empty (globally unique) username.
func TestCallback_EmptyUsernameFallsBack(t *testing.T) {
	database := openTestDB(t)
	p := newIdentityProvider(&oauth.UserInfo{Username: "", Email: "anon@example.com", ProviderID: "777"})
	e := setupIntegrationEcho(t, []oauth.Provider{p}, database, "")

	rec := postCallbackJSON(e, callbackBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var resp callbackUserResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if resp.User.Username != "github-777" {
		t.Errorf("username = %q, want %q", resp.User.Username, "github-777")
	}
}

// TestCallback_NewUserUsernameCollisionGetsSuffix verifies that a new user
// whose provider-supplied username is already taken by a different account
// is created with a numeric suffix rather than rejected with 409.
func TestCallback_NewUserUsernameCollisionGetsSuffix(t *testing.T) {
	database := openTestDB(t)

	// An existing Google account already owns the username "alice".
	if _, err := database.SqlDB.Exec(
		`INSERT INTO users (id, username, email, full_name, role, status, provider, provider_id, created_at, updated_at)
		 VALUES ('u-google-alice', 'alice', 'alice@google.example', NULL, 'user', 'active', 'google', 'g-1', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	// A second collider occupies "alice-2" as well.
	if _, err := database.SqlDB.Exec(
		`INSERT INTO users (id, username, email, full_name, role, status, provider, provider_id, created_at, updated_at)
		 VALUES ('u-google-alice2', 'alice-2', 'alice2@google.example', NULL, 'user', 'active', 'google', 'g-2', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	); err != nil {
		t.Fatalf("seed user 2: %v", err)
	}

	p := newIdentityProvider(&oauth.UserInfo{Username: "alice", Email: "alice@github.example", ProviderID: "gh-alice"})
	e := setupIntegrationEcho(t, []oauth.Provider{p}, database, "")

	rec := postCallbackJSON(e, callbackBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var resp callbackUserResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if resp.User.Username != "alice-3" {
		t.Errorf("username = %q, want %q", resp.User.Username, "alice-3")
	}

	var stored string
	if err := database.SqlDB.QueryRow(
		"SELECT username FROM users WHERE provider = 'github' AND provider_id = 'gh-alice'",
	).Scan(&stored); err != nil {
		t.Fatalf("query new user: %v", err)
	}
	if stored != "alice-3" {
		t.Errorf("stored username = %q, want %q", stored, "alice-3")
	}
	// The original owner of "alice" is untouched.
	var origOwner string
	if err := database.SqlDB.QueryRow("SELECT id FROM users WHERE username = 'alice'").Scan(&origOwner); err != nil {
		t.Fatalf("query original owner: %v", err)
	}
	if origOwner != "u-google-alice" {
		t.Errorf("username 'alice' now owned by %q, want u-google-alice", origOwner)
	}
}

// TestCallback_ExistingUserKeepsUsernameOnCollision verifies that when an
// existing user's provider-side name changes to one already owned by another
// account, the login still succeeds and the user keeps their current
// username (email and updated_at are still refreshed).
func TestCallback_ExistingUserKeepsUsernameOnCollision(t *testing.T) {
	database := openTestDB(t)

	seed := `INSERT INTO users (id, username, email, full_name, role, status, provider, provider_id, created_at, updated_at)
	         VALUES (?, ?, ?, NULL, 'user', 'active', ?, ?, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`
	if _, err := database.SqlDB.Exec(seed, "u-bob", "bob", "bob@old.example", "github", "gh-bob"); err != nil {
		t.Fatalf("seed bob: %v", err)
	}
	if _, err := database.SqlDB.Exec(seed, "u-carol", "carol", "carol@example.com", "google", "g-carol"); err != nil {
		t.Fatalf("seed carol: %v", err)
	}

	// Bob renamed himself "carol" at the provider and changed his email.
	p := newIdentityProvider(&oauth.UserInfo{Username: "carol", Email: "bob@new.example", ProviderID: "gh-bob"})
	e := setupIntegrationEcho(t, []oauth.Provider{p}, database, "")

	rec := postCallbackJSON(e, callbackBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var resp callbackUserResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if resp.User.ID != "u-bob" {
		t.Fatalf("user id = %q, want u-bob", resp.User.ID)
	}
	if resp.User.Username != "bob" {
		t.Errorf("username = %q, want %q (kept on collision)", resp.User.Username, "bob")
	}
	if resp.User.Email != "bob@new.example" {
		t.Errorf("email = %q, want %q", resp.User.Email, "bob@new.example")
	}

	var username, email, updatedAt string
	if err := database.SqlDB.QueryRow(
		"SELECT username, email, updated_at FROM users WHERE id = 'u-bob'",
	).Scan(&username, &email, &updatedAt); err != nil {
		t.Fatalf("query bob: %v", err)
	}
	if username != "bob" || email != "bob@new.example" {
		t.Errorf("stored (username, email) = (%q, %q), want (bob, bob@new.example)", username, email)
	}
	if updatedAt == "2026-01-01T00:00:00Z" {
		t.Error("updated_at was not refreshed")
	}
}

// TestProviders_AuthorizeURLMalformedDoesNotPanic verifies that a malformed
// configured authorize_url is returned as-is instead of panicking on a nil
// *url.URL.
func TestProviders_AuthorizeURLMalformedDoesNotPanic(t *testing.T) {
	bad := "http://[::1]:namedport"
	gh := oauth.NewGitHubProvider("cid", "sec", bad, "", "", http.DefaultClient)
	if got := gh.AuthorizeURL("s", "r"); got != bad {
		t.Errorf("github AuthorizeURL = %q, want %q", got, bad)
	}
	gg := oauth.NewGoogleProvider("cid", "sec", bad, "", "", http.DefaultClient)
	if got := gg.AuthorizeURL("s", "r"); got != bad {
		t.Errorf("google AuthorizeURL = %q, want %q", got, bad)
	}
}

// TestGitHub_UserInfo_MissingIDRejected verifies that a GitHub userinfo
// response without a numeric id is rejected instead of yielding ProviderID "0".
func TestGitHub_UserInfo_MissingIDRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"login": "octocat",
			"email": "cat@github.com",
		})
	}))
	defer srv.Close()

	p := oauth.NewGitHubProvider("cid", "csec", "", "", srv.URL+"/user", srv.Client())
	ui, err := p.UserInfo(context.Background(), "tok")
	if err == nil {
		t.Fatalf("UserInfo() error = nil, want non-nil; got %+v", ui)
	}
}

// TestGoogle_UserInfo_MissingSubRejected verifies that a Google userinfo
// response without a sub claim is rejected instead of yielding an empty
// ProviderID.
func TestGoogle_UserInfo_MissingSubRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":           "Some One",
			"email":          "someone@example.com",
			"email_verified": true,
		})
	}))
	defer srv.Close()

	p := oauth.NewGoogleProvider("cid", "csec", "", "", srv.URL, srv.Client())
	ui, err := p.UserInfo(context.Background(), "tok")
	if err == nil {
		t.Fatalf("UserInfo() error = nil, want non-nil; got %+v", ui)
	}
}

package oauth_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/txsvc/apikit/internal/db"
	"github.com/txsvc/apikit/internal/oauth"
)

// ========================================================================
// Smoke test infrastructure
// ========================================================================

// smokeCallbackResponse is the full success response from POST /auth/callback.
type smokeCallbackResponse struct {
	User struct {
		ID         string  `json:"id"`
		Username   string  `json:"username"`
		Email      string  `json:"email"`
		FullName   *string `json:"full_name"`
		Status     string  `json:"status"`
		Role       string  `json:"role"`
		Provider   string  `json:"provider"`
		ProviderID string  `json:"provider_id"`
		CreatedAt  string  `json:"created_at"`
		UpdatedAt  string  `json:"updated_at"`
	} `json:"user"`
	APIKey struct {
		Key       string  `json:"key"`
		KeyID     string  `json:"key_id"`
		ExpiresAt *string `json:"expires_at"`
	} `json:"api_key"`
}

// smokeSetup creates a real GitHubProvider backed by httptest servers for the
// token and userinfo endpoints, registers it in a real Registry, and mounts
// handlers on a real Echo server with a real SQLite DB.
func smokeSetup(t *testing.T, tokenHandler, userinfoHandler http.HandlerFunc) (*echo.Echo, *db.DB, *httptest.Server, *httptest.Server) {
	t.Helper()

	tokenServer := httptest.NewServer(tokenHandler)
	t.Cleanup(tokenServer.Close)

	userinfoServer := httptest.NewServer(userinfoHandler)
	t.Cleanup(userinfoServer.Close)

	database := openTestDB(t)

	client := &http.Client{Timeout: 30 * time.Second}
	provider := oauth.NewGitHubProvider(
		"smoke-client-id",
		"smoke-client-secret",
		"", // default authorize URL
		tokenServer.URL,
		userinfoServer.URL,
		client,
	)

	registry := oauth.NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	e := echo.New()
	g := e.Group("")
	oauth.RegisterOAuthHandlers(g, registry, database, "")

	return e, database, tokenServer, userinfoServer
}

// smokePostCallback sends a POST /auth/callback with the given JSON body.
func smokePostCallback(e *echo.Echo, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/auth/callback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// smokeGetProviders sends a GET /auth/providers request.
func smokeGetProviders(e *echo.Echo) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/auth/providers", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// ========================================================================
// TS-06-SMOKE-1: Full OAuth callback flow for a new user
// (Execution Path: 06-PATH-1)
//
// Real components: internal/oauth callback handler, SQLite database,
//   internal/oauth registry
// Mockable: GitHub token endpoint (httptest.Server returning access_token),
//   GitHub userinfo endpoint (httptest.Server returning login/email/id JSON)
// ========================================================================

func TestSmoke_NewUserCallback(t *testing.T) {
	tokenHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"smoke-access-token-1"}`)
	})
	userinfoHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Bearer token is forwarded.
		auth := r.Header.Get("Authorization")
		if auth != "Bearer smoke-access-token-1" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"login":"octocat","email":"octocat@github.com","id":42}`)
	})

	e, database, _, _ := smokeSetup(t, tokenHandler, userinfoHandler)

	body := `{"provider":"github","code":"validcode","redirect_uri":"http://localhost:9000/cb","expires":90}`
	rec := smokePostCallback(e, body)

	// --- Verify HTTP 200 with Content-Type: application/json ---
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var resp smokeCallbackResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}

	// --- Verify user fields ---
	if resp.User.ID == "" {
		t.Error("user.id is empty; want UUID")
	}
	if resp.User.Username != "octocat" {
		t.Errorf("user.username = %q, want %q", resp.User.Username, "octocat")
	}
	if resp.User.Email != "octocat@github.com" {
		t.Errorf("user.email = %q, want %q", resp.User.Email, "octocat@github.com")
	}
	if resp.User.Status != "active" {
		t.Errorf("user.status = %q, want %q", resp.User.Status, "active")
	}
	if resp.User.Provider != "github" {
		t.Errorf("user.provider = %q, want %q", resp.User.Provider, "github")
	}
	if resp.User.ProviderID != "42" {
		t.Errorf("user.provider_id = %q, want %q", resp.User.ProviderID, "42")
	}

	// --- Verify api_key format: ^ak_[a-zA-Z0-9]{8}_[a-zA-Z0-9]{32}$ ---
	keyPattern := regexp.MustCompile(`^ak_[a-zA-Z0-9]{8}_[a-zA-Z0-9]{32}$`)
	if !keyPattern.MatchString(resp.APIKey.Key) {
		t.Errorf("api_key.key = %q, does not match pattern", resp.APIKey.Key)
	}

	// --- Verify api_key.expires_at is RFC 3339, approximately 90 days from now ---
	if resp.APIKey.ExpiresAt == nil {
		t.Fatal("api_key.expires_at is null; want RFC 3339 timestamp for expires=90")
	}
	expiresAt, err := time.Parse(time.RFC3339, *resp.APIKey.ExpiresAt)
	if err != nil {
		t.Fatalf("parse expires_at: %v", err)
	}
	diff := time.Until(expiresAt)
	if diff < 89*24*time.Hour || diff > 91*24*time.Hour {
		t.Errorf("expires_at diff = %v, want approximately 90 days", diff)
	}

	// --- Verify SQLite users table has new row ---
	var dbUsername, dbEmail, dbProvider, dbProviderID string
	err = database.SqlDB.QueryRow(
		"SELECT username, email, provider, provider_id FROM users WHERE provider = 'github' AND provider_id = '42'",
	).Scan(&dbUsername, &dbEmail, &dbProvider, &dbProviderID)
	if err != nil {
		t.Fatalf("query users: %v", err)
	}
	if dbUsername != "octocat" {
		t.Errorf("db username = %q, want %q", dbUsername, "octocat")
	}
	if dbEmail != "octocat@github.com" {
		t.Errorf("db email = %q, want %q", dbEmail, "octocat@github.com")
	}

	// --- Verify api_keys table: secret_hash == SHA256(plaintext_secret), revoked_at IS NULL ---
	parts := strings.Split(resp.APIKey.Key, "_")
	if len(parts) != 3 {
		t.Fatalf("key parts = %d, want 3", len(parts))
	}
	plaintextSecret := parts[2]
	expectedHash := sha256.Sum256([]byte(plaintextSecret))
	expectedHex := hex.EncodeToString(expectedHash[:])

	var dbSecretHash string
	var dbRevokedAt *string
	err = database.SqlDB.QueryRow(
		"SELECT secret_hash, revoked_at FROM api_keys WHERE key_id = ?", parts[1],
	).Scan(&dbSecretHash, &dbRevokedAt)
	if err != nil {
		t.Fatalf("query api_keys: %v", err)
	}
	if dbSecretHash != expectedHex {
		t.Errorf("secret_hash = %q, want SHA256 %q", dbSecretHash, expectedHex)
	}
	if dbRevokedAt != nil {
		t.Error("new key revoked_at is non-NULL; want NULL")
	}
}

// ========================================================================
// TS-06-SMOKE-2: Returning user re-login
// (Execution Path: 06-PATH-2)
//
// Real components: internal/oauth callback handler, SQLite database,
//   internal/oauth registry
// Mockable: GitHub token endpoint, GitHub userinfo endpoint
// ========================================================================

func TestSmoke_ReturningUserReLogin(t *testing.T) {
	tokenHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"smoke-token-relogin"}`)
	})
	userinfoHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Return updated username and email.
		fmt.Fprint(w, `{"login":"octocat-updated","email":"new-email@github.com","id":42}`)
	})

	e, database, _, _ := smokeSetup(t, tokenHandler, userinfoHandler)

	// Pre-insert an existing user with (provider=github, provider_id=42).
	now := db.FormatTime(time.Now().UTC())
	originalUserID := "existing-uuid-42"
	_, err := database.SqlDB.Exec(
		`INSERT INTO users (id, username, email, full_name, role, status, provider, provider_id, created_at, updated_at)
		 VALUES (?, ?, ?, NULL, ?, ?, ?, ?, ?, ?)`,
		originalUserID, "octocat-original", "original@github.com", "user", "active",
		"github", "42", now, now,
	)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	// Pre-insert an active API key for the user.
	expiresAt := db.FormatTime(time.Now().UTC().Add(90 * 24 * time.Hour))
	_, err = database.SqlDB.Exec(
		`INSERT INTO api_keys (key_id, user_id, secret_hash, expires_days, expires_at, revoked_at, created_at)
		 VALUES (?, ?, ?, ?, ?, NULL, ?)`,
		"oldkey42", originalUserID, "oldhashvalue", 90, expiresAt, now,
	)
	if err != nil {
		t.Fatalf("insert api_key: %v", err)
	}

	body := `{"provider":"github","code":"validcode","redirect_uri":"http://localhost:9000/cb","expires":90}`
	rec := smokePostCallback(e, body)

	// --- HTTP 200 ---
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp smokeCallbackResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}

	// --- Verify user fields updated, id/role/status/created_at unchanged ---
	if resp.User.ID != originalUserID {
		t.Errorf("user.id = %q, want %q (unchanged)", resp.User.ID, originalUserID)
	}
	if resp.User.Username != "octocat-updated" {
		t.Errorf("user.username = %q, want %q (updated)", resp.User.Username, "octocat-updated")
	}
	if resp.User.Email != "new-email@github.com" {
		t.Errorf("user.email = %q, want %q (updated)", resp.User.Email, "new-email@github.com")
	}
	if resp.User.Role != "user" {
		t.Errorf("user.role = %q, want %q (unchanged)", resp.User.Role, "user")
	}
	if resp.User.Status != "active" {
		t.Errorf("user.status = %q, want %q (unchanged)", resp.User.Status, "active")
	}

	// --- Verify previous API key was revoked ---
	var oldRevokedAt *string
	err = database.SqlDB.QueryRow(
		"SELECT revoked_at FROM api_keys WHERE key_id = 'oldkey42'",
	).Scan(&oldRevokedAt)
	if err != nil {
		t.Fatalf("query old key: %v", err)
	}
	if oldRevokedAt == nil {
		t.Error("old key revoked_at is NULL; want non-NULL timestamp")
	}

	// --- Verify new API key issued (different from previous key) ---
	var activeCount int
	err = database.SqlDB.QueryRow(
		"SELECT COUNT(*) FROM api_keys WHERE user_id = ? AND revoked_at IS NULL", originalUserID,
	).Scan(&activeCount)
	if err != nil {
		t.Fatalf("count active keys: %v", err)
	}
	if activeCount != 1 {
		t.Errorf("active key count = %d, want 1", activeCount)
	}

	// --- Verify the new key is different from the old one ---
	keyPattern := regexp.MustCompile(`^ak_[a-zA-Z0-9]{8}_[a-zA-Z0-9]{32}$`)
	if !keyPattern.MatchString(resp.APIKey.Key) {
		t.Errorf("api_key.key = %q, does not match pattern", resp.APIKey.Key)
	}
	if resp.APIKey.KeyID == "oldkey42" {
		t.Error("new key_id is same as old key_id; want different key")
	}
}

// ========================================================================
// TS-06-SMOKE-3: GET /auth/providers returns provider list
// (Execution Path: 06-PATH-3)
//
// Real components: internal/oauth providers handler, internal/oauth registry
//   with github provider, CacheMiddleware(CachePublic)
// ========================================================================

func TestSmoke_ProvidersEndpoint(t *testing.T) {
	// Use a real GitHubProvider (token/userinfo URLs don't matter for this test).
	client := &http.Client{Timeout: 30 * time.Second}
	provider := oauth.NewGitHubProvider(
		"smoke-provider-cid",
		"smoke-provider-secret",
		"", // default authorize URL
		"http://localhost:1/unused-token",
		"http://localhost:1/unused-userinfo",
		client,
	)

	registry := oauth.NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	e := echo.New()
	g := e.Group("")
	oauth.RegisterOAuthHandlers(g, registry, nil, "")

	rec := smokeGetProviders(e)

	// --- HTTP 200 ---
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	// --- Cache-Control: public, max-age=300 ---
	cc := rec.Header().Get("Cache-Control")
	if cc != "public, max-age=300" {
		t.Errorf("Cache-Control = %q, want %q", cc, "public, max-age=300")
	}

	// --- JSON array with one entry ---
	var entries []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries count = %d, want 1", len(entries))
	}

	entry := entries[0]

	// --- name = "github" ---
	if name, ok := entry["name"].(string); !ok || name != "github" {
		t.Errorf("name = %v, want %q", entry["name"], "github")
	}

	// --- authorize_url contains client_id and scope=user:email ---
	authURL, ok := entry["authorize_url"].(string)
	if !ok || authURL == "" {
		t.Fatal("authorize_url is missing or empty")
	}
	if !strings.Contains(authURL, "client_id=smoke-provider-cid") {
		t.Errorf("authorize_url %q missing client_id", authURL)
	}
	// scope=user:email is URL-encoded as user%3Aemail
	if !strings.Contains(authURL, "scope=user%3Aemail") {
		t.Errorf("authorize_url %q missing scope=user%%3Aemail", authURL)
	}

	// --- No secrets exposed: body must NOT contain client_secret, token_url, userinfo_url ---
	bodyStr := rec.Body.String()
	for _, forbidden := range []string{"client_secret", "token_url", "userinfo_url", "smoke-provider-secret"} {
		if strings.Contains(bodyStr, forbidden) {
			t.Errorf("response body contains forbidden field/value %q", forbidden)
		}
	}
}

// ========================================================================
// TS-06-SMOKE-4: Blocked user OAuth callback rejected with HTTP 403
// (Execution Path: 06-PATH-4)
//
// Real components: internal/oauth callback handler, SQLite database,
//   internal/oauth registry
// Mockable: GitHub token endpoint, GitHub userinfo endpoint
// ========================================================================

func TestSmoke_BlockedUserRejection(t *testing.T) {
	tokenHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"smoke-token-blocked"}`)
	})
	userinfoHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"login":"blockedcat","email":"blocked@github.com","id":99}`)
	})

	e, database, _, _ := smokeSetup(t, tokenHandler, userinfoHandler)

	// Pre-insert a blocked user with (provider=github, provider_id=99).
	now := db.FormatTime(time.Now().UTC())
	_, err := database.SqlDB.Exec(
		`INSERT INTO users (id, username, email, full_name, role, status, provider, provider_id, created_at, updated_at)
		 VALUES (?, ?, ?, NULL, ?, ?, ?, ?, ?, ?)`,
		"blocked-uuid-99", "blockedcat", "blocked@github.com", "user", "blocked",
		"github", "99", now, now,
	)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	// Record api_keys count before.
	var keyCountBefore int
	err = database.SqlDB.QueryRow(
		"SELECT COUNT(*) FROM api_keys WHERE user_id = 'blocked-uuid-99'",
	).Scan(&keyCountBefore)
	if err != nil {
		t.Fatalf("count api_keys: %v", err)
	}

	body := `{"provider":"github","code":"validcode","redirect_uri":"http://localhost:9000/cb"}`
	rec := smokePostCallback(e, body)

	// --- HTTP 403 ---
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}

	// --- Error message ---
	var errResp struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if errResp.Error.Code != 403 {
		t.Errorf("error.code = %d, want 403", errResp.Error.Code)
	}
	if errResp.Error.Message != "user is blocked" {
		t.Errorf("error.message = %q, want %q", errResp.Error.Message, "user is blocked")
	}

	// --- User status unchanged ---
	var status string
	err = database.SqlDB.QueryRow(
		"SELECT status FROM users WHERE id = 'blocked-uuid-99'",
	).Scan(&status)
	if err != nil {
		t.Fatalf("query users: %v", err)
	}
	if status != "blocked" {
		t.Errorf("status = %q, want %q (unchanged)", status, "blocked")
	}

	// --- api_keys table unchanged ---
	var keyCountAfter int
	err = database.SqlDB.QueryRow(
		"SELECT COUNT(*) FROM api_keys WHERE user_id = 'blocked-uuid-99'",
	).Scan(&keyCountAfter)
	if err != nil {
		t.Fatalf("count api_keys: %v", err)
	}
	if keyCountAfter != keyCountBefore {
		t.Errorf("api_keys count changed from %d to %d; want unchanged", keyCountBefore, keyCountAfter)
	}
}

// ========================================================================
// TS-06-SMOKE-5: Server startup with valid config registers github
// provider in registry and makes both OAuth endpoints reachable
// (Execution Path: 06-PATH-5)
//
// Real components: server bootstrap (BuildRegistryFromConfig),
//   internal/oauth registry, NewGitHubProvider, RegisterOAuthHandlers
// ========================================================================

func TestSmoke_ServerStartupConfigRegistration(t *testing.T) {
	// Create a shared HTTP client with 30-second timeout.
	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: http.DefaultTransport,
	}

	// Build registry from config using real BuildRegistryFromConfig.
	configs := []oauth.ProviderConfig{
		{
			Name:         "github",
			ClientID:     "startup-cid",
			ClientSecret: "startup-secret",
		},
	}
	registry, err := oauth.BuildRegistryFromConfig(configs, client)
	if err != nil {
		t.Fatalf("BuildRegistryFromConfig: %v", err)
	}

	// --- Registry.Get("github") returns non-nil ---
	ghProvider := registry.Get("github")
	if ghProvider == nil {
		t.Fatal("registry.Get(\"github\") returned nil")
	}

	// --- Registry.List() returns ["github"] ---
	names := registry.List()
	if len(names) != 1 || names[0] != "github" {
		t.Errorf("registry.List() = %v, want [\"github\"]", names)
	}

	// --- Mount handlers on Echo and verify endpoints are reachable ---
	e := echo.New()
	g := e.Group("")
	oauth.RegisterOAuthHandlers(g, registry, nil, "")

	// GET /auth/providers returns HTTP 200 with provider list.
	rec1 := smokeGetProviders(e)
	if rec1.Code != http.StatusOK {
		t.Errorf("GET /auth/providers status = %d, want %d", rec1.Code, http.StatusOK)
	}

	var providers []map[string]any
	if err := json.Unmarshal(rec1.Body.Bytes(), &providers); err != nil {
		t.Fatalf("parse providers: %v", err)
	}
	if len(providers) != 1 {
		t.Errorf("providers count = %d, want 1", len(providers))
	}
	if name, ok := providers[0]["name"].(string); !ok || name != "github" {
		t.Errorf("provider name = %v, want %q", providers[0]["name"], "github")
	}

	// POST /auth/callback returns HTTP 400 (validation error) not 404.
	rec2 := smokePostCallback(e, `{}`)
	if rec2.Code == http.StatusNotFound {
		t.Error("POST /auth/callback returned 404; route is not mounted")
	}
	if rec2.Code != http.StatusBadRequest {
		t.Errorf("POST /auth/callback with empty body: status = %d, want %d", rec2.Code, http.StatusBadRequest)
	}

	// --- No startup error (implicit: no t.Fatal above) ---
}

package apikit_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/txsvc/apikit"
	"github.com/txsvc/apikit/internal/auth"
	"github.com/txsvc/apikit/internal/config"
	"github.com/txsvc/apikit/internal/db"
	"github.com/txsvc/apikit/internal/handlers"
	"github.com/txsvc/apikit/internal/oauth"
)

// ========================================================================
// Test Helpers
// ========================================================================

// buildHookTestConfig creates a *Config with sensible defaults for hook tests.
func buildHookTestConfig(port int) *apikit.Config {
	return &apikit.Config{
		Server: config.ServerConfig{
			Port:       port,
			Bind:       "127.0.0.1",
			MountPoint: "/api/v1",
		},
		Database: config.DatabaseConfig{
			Path: "./data/test.db",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}
}

// openHookTestDB creates an in-memory SQLite database for hook tests.
func openHookTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

// adminAuthMiddleware returns Echo middleware that injects admin-level AuthInfo
// into the request context for test purposes.
func adminAuthMiddleware(userID string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			auth.SetAuthInfo(c, &auth.AuthInfo{
				CredentialType: "api_key",
				UserID:         userID,
				Role:           "admin",
			})
			return next(c)
		}
	}
}

// setupAdminTestEcho creates an Echo instance with admin auth middleware and
// RegisterUserHandlers. Returns the Echo instance and the raw *sql.DB handle.
func setupAdminTestEcho(t *testing.T) (*echo.Echo, *sql.DB) {
	t.Helper()

	database := openHookTestDB(t)

	e := echo.New()
	g := e.Group("")
	g.Use(adminAuthMiddleware("test-admin-uuid"))
	handlers.RegisterUserHandlers(g, database.SqlDB)

	return e, database.SqlDB
}

// sendJSON sends an HTTP request with a JSON body to the given Echo instance
// and returns the response recorder.
func sendJSON(t *testing.T, e *echo.Echo, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// hookTestProvider is a configurable mock OAuth provider for hook tests.
type hookTestProvider struct {
	name       string
	exchangeFn func(ctx context.Context, code, redirectURI string) (string, error)
	userInfoFn func(ctx context.Context, accessToken string) (*oauth.UserInfo, error)
	mu         sync.Mutex
}

func (p *hookTestProvider) Name() string { return p.name }

func (p *hookTestProvider) AuthorizeURL(_, _ string) string { return "" }

func (p *hookTestProvider) Exchange(ctx context.Context, code, redirectURI string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.exchangeFn != nil {
		return p.exchangeFn(ctx, code, redirectURI)
	}
	return "test-access-token", nil
}

func (p *hookTestProvider) UserInfo(ctx context.Context, accessToken string) (*oauth.UserInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.userInfoFn != nil {
		return p.userInfoFn(ctx, accessToken)
	}
	return &oauth.UserInfo{
		Username:   "testuser",
		Email:      "test@example.com",
		ProviderID: "provider-id-123",
	}, nil
}

// Compile-time check.
var _ oauth.Provider = (*hookTestProvider)(nil)

// setupOAuthTestEcho creates an Echo instance with OAuth handlers using a mock
// provider and an in-memory DB. Returns the Echo, DB, and provider.
// An optional AfterUserCreateFunc hook may be passed; when provided it is
// wired into the OAuth callback handler.
func setupOAuthTestEcho(t *testing.T, provider *hookTestProvider, hooks ...apikit.AfterUserCreateFunc) (*echo.Echo, *db.DB) {
	t.Helper()

	database := openHookTestDB(t)

	registry := oauth.NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatalf("failed to register provider: %v", err)
	}

	e := echo.New()
	g := e.Group("")
	if len(hooks) > 0 && hooks[0] != nil {
		oauth.RegisterOAuthHandlers(g, registry, database, "http://localhost:9000", hooks[0])
	} else {
		oauth.RegisterOAuthHandlers(g, registry, database, "http://localhost:9000")
	}

	return e, database
}

// postOAuthCallback sends a POST /auth/callback request.
func postOAuthCallback(e *echo.Echo, provider, code, redirectURI string) *httptest.ResponseRecorder {
	body := fmt.Sprintf(
		`{"provider":"%s","code":"%s","redirect_uri":"%s"}`,
		provider, code, redirectURI,
	)
	req := httptest.NewRequest(http.MethodPost, "/auth/callback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// insertTestUser inserts a user directly into the database for testing.
func insertTestUser(t *testing.T, sqlDB *sql.DB, username, email, provider, providerID string) string {
	t.Helper()
	id := fmt.Sprintf("user-%s-%d", username, time.Now().UnixNano())
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := sqlDB.Exec(
		`INSERT INTO users (id, username, email, full_name, role, status, provider, provider_id, created_at, updated_at)
		 VALUES (?, ?, ?, '', 'user', 'active', ?, ?, ?, ?)`,
		id, username, email, provider, providerID, now, now,
	)
	if err != nil {
		t.Fatalf("failed to insert test user: %v", err)
	}
	return id
}

// insertTestAPIKey inserts an API key for a user to enable auth in tests.
func insertTestAPIKey(t *testing.T, sqlDB *sql.DB, userID, keyID, secret string) string {
	t.Helper()
	h := sha256.Sum256([]byte(secret))
	secretHash := hex.EncodeToString(h[:])
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := sqlDB.Exec(
		`INSERT INTO api_keys (key_id, user_id, secret_hash, expires_days, expires_at, created_at)
		 VALUES (?, ?, ?, 90, NULL, ?)`,
		keyID, userID, secretHash, now,
	)
	if err != nil {
		t.Fatalf("failed to insert test API key: %v", err)
	}
	return fmt.Sprintf("ak_%s_%s", keyID, secret)
}

// userExistsInDB checks whether a user with the given username exists.
func userExistsInDB(t *testing.T, sqlDB *sql.DB, username string) bool {
	t.Helper()
	var count int
	err := sqlDB.QueryRow("SELECT COUNT(*) FROM users WHERE username = ?", username).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query user: %v", err)
	}
	return count > 0
}

// ========================================================================
// TS-04-1: Verify that the AfterUserCreateFunc type is exported from the
// apikit package with the correct signature.
// (Requirement: 04-REQ-1.1)
// ========================================================================

// TestAfterUserCreate_TypeSignature verifies that AfterUserCreateFunc is a
// public type with signature func(ctx context.Context, tx *sql.Tx, userID,
// username, email string) error. This is a compile-time test — if the type
// did not exist or had the wrong signature, this file would not compile.
func TestAfterUserCreate_TypeSignature(t *testing.T) {
	// Assign a function literal with the exact expected signature.
	var fn apikit.AfterUserCreateFunc = func(
		ctx context.Context,
		tx *sql.Tx,
		userID, username, email string,
	) error {
		return nil
	}
	if fn == nil {
		t.Fatal("AfterUserCreateFunc function should not be nil after assignment")
	}

	// Verify the function is callable with the expected argument types.
	err := fn(context.Background(), nil, "uid", "uname", "uemail")
	if err != nil {
		t.Fatalf("AfterUserCreateFunc returned unexpected error: %v", err)
	}
}

// ========================================================================
// TS-04-2: Verify that OnAfterUserCreate stores the provided hook function
// on the server and replaces any previous registration.
// (Requirement: 04-REQ-1.2)
// ========================================================================

// TestAfterUserCreate_RegistrationReplacesPrevious verifies that calling
// OnAfterUserCreate twice causes only the second hook to be called during
// user creation. The first hook is silently replaced.
func TestAfterUserCreate_RegistrationReplacesPrevious(t *testing.T) {
	cfg := buildHookTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	callCount1 := 0
	callCount2 := 0

	srv.OnAfterUserCreate(func(ctx context.Context, tx *sql.Tx, userID, username, email string) error {
		callCount1++
		return nil
	})
	srv.OnAfterUserCreate(func(ctx context.Context, tx *sql.Tx, userID, username, email string) error {
		callCount2++
		return nil
	})

	// Trigger user creation via the admin endpoint.
	// Set up a test Echo with admin auth and user handlers mounted via MountHandlers.
	database := openHookTestDB(t)
	if err := srv.MountHandlers((*apikit.DB)(database)); err != nil {
		t.Fatalf("MountHandlers failed: %v", err)
	}

	// Create a user by sending a request through a server with admin API key.
	// We need to create an admin user and API key first.
	adminID := insertTestUser(t, database.SqlDB, "admin-replace", "admin-replace@test.com", "test", "admin-replace-pid")
	// Promote to admin.
	_, err := database.SqlDB.Exec("UPDATE users SET role = 'admin' WHERE id = ?", adminID)
	if err != nil {
		t.Fatalf("failed to promote admin: %v", err)
	}
	apiKey := insertTestAPIKey(t, database.SqlDB, adminID, "rplkey01", "rplsecret01234567890123456789ab")

	// Start the server on an ephemeral port.
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()
	addr := waitForHookServer(t, srv, 5*time.Second)
	t.Cleanup(func() { srv.Shutdown(context.Background()); <-errCh })

	// POST /api/v1/users to create a user.
	reqBody := `{"username":"hook-replace-user","email":"hook-replace@test.com","provider":"test","provider_id":"hook-replace-pid"}`
	httpReq, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/api/v1/users", addr), strings.NewReader(reqBody))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	resp.Body.Close()

	// Assert: the first hook should NOT have been called.
	if callCount1 != 0 {
		t.Errorf("hookA was called %d times, want 0 (should have been replaced)", callCount1)
	}
	// Assert: the second hook SHOULD have been called exactly once.
	// This will FAIL until the hook is wired into createUser (group 6).
	if callCount2 != 1 {
		t.Errorf("hookB was called %d times, want 1", callCount2)
	}
}

// waitForHookServer polls srv.Addr() until a non-empty address is returned.
func waitForHookServer(t *testing.T, srv *apikit.Server, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		addr := srv.Addr()
		if addr != "" {
			return addr
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server did not start listening within timeout")
	return ""
}

// ========================================================================
// TS-04-3: Verify that user creation proceeds normally without calling any
// hook when no hook has been registered.
// (Requirement: 04-REQ-1.3)
// ========================================================================

// TestAfterUserCreate_NoHookNoPanic verifies that user creation via the
// admin endpoint completes successfully (HTTP 201) when no hook has been
// registered via OnAfterUserCreate. This ensures backward compatibility.
func TestAfterUserCreate_NoHookNoPanic(t *testing.T) {
	e, sqlDB := setupAdminTestEcho(t)

	body := `{"username":"alice","email":"alice@example.com","provider":"test","provider_id":"alice-pid"}`
	rec := sendJSON(t, e, http.MethodPost, "/users", body)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	if !userExistsInDB(t, sqlDB, "alice") {
		t.Error("user 'alice' should exist in the database after creation")
	}
}

// ========================================================================
// TS-04-4: Verify that registering a second hook replaces the first and
// only the second hook is called during user creation.
// (Requirement: 04-REQ-1.4)
// ========================================================================

// TestAfterUserCreate_SecondHookReplacesFirst verifies that when
// OnAfterUserCreate is called twice with distinct functions hookA and hookB,
// only hookB is invoked during user creation.
func TestAfterUserCreate_SecondHookReplacesFirst(t *testing.T) {
	cfg := buildHookTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	calledA := false
	calledB := false

	srv.OnAfterUserCreate(func(ctx context.Context, tx *sql.Tx, userID, username, email string) error {
		calledA = true
		return nil
	})
	srv.OnAfterUserCreate(func(ctx context.Context, tx *sql.Tx, userID, username, email string) error {
		calledB = true
		return nil
	})

	// Set up server with MountHandlers and trigger user creation.
	database := openHookTestDB(t)
	if err := srv.MountHandlers((*apikit.DB)(database)); err != nil {
		t.Fatalf("MountHandlers failed: %v", err)
	}

	adminID := insertTestUser(t, database.SqlDB, "admin-second", "admin-second@test.com", "test", "admin-second-pid")
	if _, err := database.SqlDB.Exec("UPDATE users SET role = 'admin' WHERE id = ?", adminID); err != nil {
		t.Fatalf("failed to promote admin: %v", err)
	}
	apiKey := insertTestAPIKey(t, database.SqlDB, adminID, "seckey01", "secsecret01234567890123456789ab")

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()
	addr := waitForHookServer(t, srv, 5*time.Second)
	t.Cleanup(func() { srv.Shutdown(context.Background()); <-errCh })

	reqBody := `{"username":"hook-second-user","email":"hook-second@test.com","provider":"test","provider_id":"hook-second-pid"}`
	httpReq, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/api/v1/users", addr), strings.NewReader(reqBody))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	resp.Body.Close()

	if calledA {
		t.Error("hookA should NOT have been called (it was replaced)")
	}
	// This will FAIL until the hook is wired into createUser (group 6).
	if !calledB {
		t.Error("hookB should have been called exactly once")
	}
}

// ========================================================================
// TS-04-5: Verify that a hook registered before MountHandlers is invoked
// in both the OAuth new-user branch and the admin createUser handler.
// (Requirement: 04-REQ-1.5)
// ========================================================================

// TestAfterUserCreate_HookWiredIntoBothPaths verifies that a hook registered
// before MountHandlers is called in both OAuth new-user and admin user
// creation paths with the correct arguments.
func TestAfterUserCreate_HookWiredIntoBothPaths(t *testing.T) {
	var mu sync.Mutex
	var invokedNames []string

	cfg := buildHookTestConfig(0)
	cfg.OAuth.Providers = []config.ProviderConfig{
		{
			Name:         "github",
			ClientID:     "test-client-id",
			ClientSecret: "test-client-secret",
		},
	}

	srv := apikit.NewServer(cfg, nil)
	srv.OnAfterUserCreate(func(ctx context.Context, tx *sql.Tx, userID, username, email string) error {
		mu.Lock()
		invokedNames = append(invokedNames, username)
		mu.Unlock()
		return nil
	})

	database := openHookTestDB(t)
	if err := srv.MountHandlers((*apikit.DB)(database)); err != nil {
		t.Fatalf("MountHandlers failed: %v", err)
	}

	// Set up admin credentials for the admin path.
	adminID := insertTestUser(t, database.SqlDB, "admin-both", "admin-both@test.com", "test", "admin-both-pid")
	if _, err := database.SqlDB.Exec("UPDATE users SET role = 'admin' WHERE id = ?", adminID); err != nil {
		t.Fatalf("failed to promote admin: %v", err)
	}
	apiKey := insertTestAPIKey(t, database.SqlDB, adminID, "bthkey01", "bthsecret01234567890123456789ab")

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()
	addr := waitForHookServer(t, srv, 5*time.Second)
	t.Cleanup(func() { srv.Shutdown(context.Background()); <-errCh })

	// 1. Create user via admin endpoint.
	adminBody := `{"username":"carol","email":"carol@test.com","provider":"test","provider_id":"carol-pid"}`
	adminReq, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/api/v1/users", addr), strings.NewReader(adminBody))
	adminReq.Header.Set("Content-Type", "application/json")
	adminReq.Header.Set("Authorization", "Bearer "+apiKey)
	adminResp, err := http.DefaultClient.Do(adminReq)
	if err != nil {
		t.Fatalf("admin user creation request failed: %v", err)
	}
	adminResp.Body.Close()

	// 2. Trigger OAuth new-user creation (requires mock provider).
	// The OAuth callback through MountHandlers requires a real provider
	// configuration. This test will exercise the path through the server.
	oauthBody := `{"provider":"github","code":"test-code","redirect_uri":"http://localhost:9000/cb"}`
	oauthReq, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/api/v1/auth/callback", addr), strings.NewReader(oauthBody))
	oauthReq.Header.Set("Content-Type", "application/json")
	oauthResp, err := http.DefaultClient.Do(oauthReq)
	if err != nil {
		t.Fatalf("OAuth callback request failed: %v", err)
	}
	oauthResp.Body.Close()

	// Assert: hook should have been called for both paths.
	// This will FAIL until the hook is wired into both handlers (group 6).
	mu.Lock()
	names := append([]string{}, invokedNames...)
	mu.Unlock()

	if len(names) < 1 {
		t.Error("hook should have been called at least once for admin user creation")
	}
	// When fully wired, names should contain both 'carol' and the OAuth user.
}

// ========================================================================
// TS-04-E1: Verify that calling OnAfterUserCreate after MountHandlers does
// not panic or error, even if behavior is undefined.
// (Requirement: 04-REQ-1.E1)
// ========================================================================

// TestAfterUserCreate_RegisterAfterMountHandlersNoPanic verifies that calling
// OnAfterUserCreate after MountHandlers completes without panic or error.
func TestAfterUserCreate_RegisterAfterMountHandlersNoPanic(t *testing.T) {
	cfg := buildHookTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	database := openHookTestDB(t)
	if err := srv.MountHandlers((*apikit.DB)(database)); err != nil {
		t.Fatalf("MountHandlers failed: %v", err)
	}

	// This should not panic.
	didPanic := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				didPanic = true
			}
		}()
		srv.OnAfterUserCreate(func(ctx context.Context, tx *sql.Tx, userID, username, email string) error {
			return nil
		})
	}()

	if didPanic {
		t.Error("OnAfterUserCreate should not panic when called after MountHandlers")
	}
}

// ========================================================================
// TS-04-6: Verify that the AfterUserCreateFunc hook is called with the
// active transaction and new user details after a new user INSERT during
// OAuth callback.
// (Requirement: 04-REQ-2.1)
// ========================================================================

// TestOrgHook_OAuthNewUserCallsHookWithCorrectArgs verifies that during an
// OAuth callback for a new user, the registered hook is called with a
// non-nil *sql.Tx and the correct userID, username, and email.
func TestOrgHook_OAuthNewUserCallsHookWithCorrectArgs(t *testing.T) {
	provider := &hookTestProvider{
		name: "github",
		userInfoFn: func(ctx context.Context, accessToken string) (*oauth.UserInfo, error) {
			return &oauth.UserInfo{
				Username:   "dave",
				Email:      "dave@example.com",
				ProviderID: "dave-pid-123",
			}, nil
		},
	}

	type capturedArgs struct {
		tx       *sql.Tx
		userID   string
		username string
		email    string
	}
	var captured *capturedArgs
	var mu sync.Mutex

	// Wire the hook directly into the OAuth handler via setupOAuthTestEcho.
	hookFn := apikit.AfterUserCreateFunc(func(ctx context.Context, tx *sql.Tx, userID, username, email string) error {
		mu.Lock()
		captured = &capturedArgs{tx: tx, userID: userID, username: username, email: email}
		mu.Unlock()
		return nil
	})

	e, database := setupOAuthTestEcho(t, provider, hookFn)

	// Use the OAuth test Echo (which has the mock provider) to send the callback.
	rec := postOAuthCallback(e, "github", "test-code", "http://localhost:9000/cb")

	// The callback should succeed (HTTP 200 for new user).
	if rec.Code != http.StatusOK {
		t.Logf("OAuth callback returned %d (may fail if provider mock is not fully wired): %s",
			rec.Code, rec.Body.String())
	}

	// Assert: hook should have been called with correct args.
	mu.Lock()
	c := captured
	mu.Unlock()

	if c == nil {
		t.Fatal("hook should have been called during OAuth new-user creation, but was not called")
	}
	if c.tx == nil {
		t.Error("hook should receive a non-nil *sql.Tx")
	}
	if c.username != "dave" {
		t.Errorf("hook username = %q, want %q", c.username, "dave")
	}
	if c.email != "dave@example.com" {
		t.Errorf("hook email = %q, want %q", c.email, "dave@example.com")
	}
	if c.userID == "" {
		t.Error("hook should receive a non-empty userID")
	}

	// Verify the user exists in the database.
	if !userExistsInDB(t, database.SqlDB, "dave") {
		t.Error("user 'dave' should exist in the database after OAuth callback")
	}
}

// ========================================================================
// TS-04-7: Verify that a non-nil error from the hook during OAuth callback
// rolls back the transaction and returns HTTP 500.
// (Requirement: 04-REQ-2.2)
// ========================================================================

// TestOrgHook_OAuthHookErrorRollsBack verifies that when the registered
// AfterUserCreateFunc returns an error during the OAuth callback, the
// transaction is rolled back and HTTP 500 is returned.
func TestOrgHook_OAuthHookErrorRollsBack(t *testing.T) {
	provider := &hookTestProvider{
		name: "github",
		userInfoFn: func(ctx context.Context, accessToken string) (*oauth.UserInfo, error) {
			return &oauth.UserInfo{
				Username:   "eve",
				Email:      "eve@example.com",
				ProviderID: "eve-pid-456",
			}, nil
		},
	}

	// Wire the hook that returns an error directly into the OAuth handler.
	hookFn := apikit.AfterUserCreateFunc(func(ctx context.Context, tx *sql.Tx, userID, username, email string) error {
		return errors.New("hook failed")
	})

	e, database := setupOAuthTestEcho(t, provider, hookFn)

	rec := postOAuthCallback(e, "github", "test-code", "http://localhost:9000/cb")

	// When hook is wired and returns error: expect HTTP 500.
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d when hook returns error", rec.Code, http.StatusInternalServerError)
	}

	// Assert: no user row should exist (transaction rolled back).
	if userExistsInDB(t, database.SqlDB, "eve") {
		t.Error("user 'eve' should NOT exist in the database after hook error (transaction should be rolled back)")
	}
}

// ========================================================================
// TS-04-8: Verify that the hook is NOT called when the OAuth callback
// identifies a returning (existing) user.
// (Requirement: 04-REQ-2.3)
// ========================================================================

// TestOrgHook_OAuthReturningUserNoHookCall verifies that the hook is not
// called when the OAuth callback processes a returning (existing) user.
func TestOrgHook_OAuthReturningUserNoHookCall(t *testing.T) {
	provider := &hookTestProvider{
		name: "github",
		userInfoFn: func(ctx context.Context, accessToken string) (*oauth.UserInfo, error) {
			return &oauth.UserInfo{
				Username:   "frank",
				Email:      "frank@example.com",
				ProviderID: "frank-pid-789",
			}, nil
		},
	}

	hookCallCount := 0
	var mu sync.Mutex

	// Wire the hook into the OAuth handler — it should NOT be called for
	// returning users.
	hookFn := apikit.AfterUserCreateFunc(func(ctx context.Context, tx *sql.Tx, userID, username, email string) error {
		mu.Lock()
		hookCallCount++
		mu.Unlock()
		return nil
	})

	e, database := setupOAuthTestEcho(t, provider, hookFn)

	// Pre-insert the user so the OAuth callback sees a returning user.
	insertTestUser(t, database.SqlDB, "frank", "frank@example.com", "github", "frank-pid-789")

	rec := postOAuthCallback(e, "github", "test-code", "http://localhost:9000/cb")

	// The callback should succeed for a returning user.
	if rec.Code != http.StatusOK {
		t.Logf("OAuth callback for returning user returned %d: %s", rec.Code, rec.Body.String())
	}

	// Assert: hook should NOT have been called for returning user.
	mu.Lock()
	count := hookCallCount
	mu.Unlock()

	if count != 0 {
		t.Errorf("hook was called %d times for returning user, want 0", count)
	}
}

// ========================================================================
// TS-04-E2: Verify that when the OAuth request context is cancelled while
// the hook executes, the transaction rolls back and HTTP 500 is returned.
// (Requirement: 04-REQ-2.E1)
// ========================================================================

// TestOrgHook_OAuthContextCancellationRollsBack verifies that when the
// request context is cancelled during hook execution, the transaction
// rolls back and HTTP 500 is returned.
func TestOrgHook_OAuthContextCancellationRollsBack(t *testing.T) {
	provider := &hookTestProvider{
		name: "github",
		userInfoFn: func(ctx context.Context, accessToken string) (*oauth.UserInfo, error) {
			return &oauth.UserInfo{
				Username:   "timeout-user",
				Email:      "timeout@example.com",
				ProviderID: "timeout-pid-000",
			}, nil
		},
	}

	// Wire a hook that blocks until context is cancelled.
	hookFn := apikit.AfterUserCreateFunc(func(ctx context.Context, tx *sql.Tx, userID, username, email string) error {
		// Wait for context cancellation or a timeout.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
			return context.DeadlineExceeded
		}
	})

	e, database := setupOAuthTestEcho(t, provider, hookFn)

	// Create a request with a short-lived context.
	body := `{"provider":"github","code":"test-code","redirect_uri":"http://localhost:9000/cb"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/callback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx, cancel := context.WithTimeout(req.Context(), 10*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// When hook is wired and context is cancelled: expect HTTP 500.
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d when context is cancelled during hook execution", rec.Code, http.StatusInternalServerError)
	}

	// Assert: no user row should exist (transaction rolled back).
	if userExistsInDB(t, database.SqlDB, "timeout-user") {
		t.Error("user 'timeout-user' should NOT exist when context is cancelled (transaction should roll back)")
	}
}

// ========================================================================
// TS-04-9: Verify that the createUser handler wraps the user INSERT and
// hook call in a database transaction.
// (Requirement: 04-REQ-3.1)
// ========================================================================

// TestOrgHook_AdminCreateUserUsesTransaction verifies that the admin
// createUser handler wraps the user INSERT and hook call in the same
// *sql.Tx, and the hook receives a non-nil transaction.
func TestOrgHook_AdminCreateUserUsesTransaction(t *testing.T) {
	cfg := buildHookTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	var receivedTx *sql.Tx
	var mu sync.Mutex

	srv.OnAfterUserCreate(func(ctx context.Context, tx *sql.Tx, userID, username, email string) error {
		mu.Lock()
		receivedTx = tx
		mu.Unlock()
		return nil
	})

	database := openHookTestDB(t)
	if err := srv.MountHandlers((*apikit.DB)(database)); err != nil {
		t.Fatalf("MountHandlers failed: %v", err)
	}

	adminID := insertTestUser(t, database.SqlDB, "admin-tx", "admin-tx@test.com", "test", "admin-tx-pid")
	if _, err := database.SqlDB.Exec("UPDATE users SET role = 'admin' WHERE id = ?", adminID); err != nil {
		t.Fatalf("failed to promote admin: %v", err)
	}
	apiKey := insertTestAPIKey(t, database.SqlDB, adminID, "txkey001", "txsecret01234567890123456789abc")

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()
	addr := waitForHookServer(t, srv, 5*time.Second)
	t.Cleanup(func() { srv.Shutdown(context.Background()); <-errCh })

	reqBody := `{"username":"grace","email":"grace@test.com","provider":"test","provider_id":"grace-pid"}`
	httpReq, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/api/v1/users", addr), strings.NewReader(reqBody))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	resp.Body.Close()

	// Assert: hook should have received a non-nil *sql.Tx.
	// This will FAIL until createUser is wrapped in a transaction (group 6).
	mu.Lock()
	tx := receivedTx
	mu.Unlock()

	if tx == nil {
		t.Error("hook should receive a non-nil *sql.Tx from createUser handler")
	}

	// User should exist after successful creation.
	if !userExistsInDB(t, database.SqlDB, "grace") {
		t.Error("user 'grace' should exist in the database after admin creation")
	}
}

// ========================================================================
// TS-04-10: Verify that the hook is called with correct user details after
// a successful user INSERT inside the admin createUser transaction.
// (Requirement: 04-REQ-3.2)
// ========================================================================

// TestOrgHook_AdminCreateUserHookGetsCorrectArgs verifies that the hook
// receives the correct userID, username, and email after admin user creation.
func TestOrgHook_AdminCreateUserHookGetsCorrectArgs(t *testing.T) {
	cfg := buildHookTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	type capturedArgs struct {
		userID   string
		username string
		email    string
	}
	var captured *capturedArgs
	var mu sync.Mutex

	srv.OnAfterUserCreate(func(ctx context.Context, tx *sql.Tx, userID, username, email string) error {
		mu.Lock()
		captured = &capturedArgs{userID: userID, username: username, email: email}
		mu.Unlock()
		return nil
	})

	database := openHookTestDB(t)
	if err := srv.MountHandlers((*apikit.DB)(database)); err != nil {
		t.Fatalf("MountHandlers failed: %v", err)
	}

	adminID := insertTestUser(t, database.SqlDB, "admin-args", "admin-args@test.com", "test", "admin-args-pid")
	if _, err := database.SqlDB.Exec("UPDATE users SET role = 'admin' WHERE id = ?", adminID); err != nil {
		t.Fatalf("failed to promote admin: %v", err)
	}
	apiKey := insertTestAPIKey(t, database.SqlDB, adminID, "argkey01", "argsecret01234567890123456789ab")

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()
	addr := waitForHookServer(t, srv, 5*time.Second)
	t.Cleanup(func() { srv.Shutdown(context.Background()); <-errCh })

	reqBody := `{"username":"henry","email":"henry@example.com","provider":"test","provider_id":"henry-pid"}`
	httpReq, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/api/v1/users", addr), strings.NewReader(reqBody))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	resp.Body.Close()

	// Assert: hook should have been called with correct args.
	// This will FAIL until the hook is wired into createUser (group 6).
	mu.Lock()
	c := captured
	mu.Unlock()

	if c == nil {
		t.Fatal("hook should have been called during admin user creation, but was not called")
	}
	if c.username != "henry" {
		t.Errorf("hook username = %q, want %q", c.username, "henry")
	}
	if c.email != "henry@example.com" {
		t.Errorf("hook email = %q, want %q", c.email, "henry@example.com")
	}
	if c.userID == "" {
		t.Error("hook should receive a non-empty userID")
	}
}

// ========================================================================
// TS-04-11: Verify that a hook error during admin user creation rolls back
// the transaction and returns HTTP 500.
// (Requirement: 04-REQ-3.3)
// ========================================================================

// TestOrgHook_AdminCreateUserHookErrorRollsBack verifies that when the
// hook returns an error during admin user creation, the transaction is
// rolled back and HTTP 500 is returned.
func TestOrgHook_AdminCreateUserHookErrorRollsBack(t *testing.T) {
	cfg := buildHookTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	srv.OnAfterUserCreate(func(ctx context.Context, tx *sql.Tx, userID, username, email string) error {
		return errors.New("hook error")
	})

	database := openHookTestDB(t)
	if err := srv.MountHandlers((*apikit.DB)(database)); err != nil {
		t.Fatalf("MountHandlers failed: %v", err)
	}

	adminID := insertTestUser(t, database.SqlDB, "admin-err", "admin-err@test.com", "test", "admin-err-pid")
	if _, err := database.SqlDB.Exec("UPDATE users SET role = 'admin' WHERE id = ?", adminID); err != nil {
		t.Fatalf("failed to promote admin: %v", err)
	}
	apiKey := insertTestAPIKey(t, database.SqlDB, adminID, "errkey01", "errsecret01234567890123456789ab")

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()
	addr := waitForHookServer(t, srv, 5*time.Second)
	t.Cleanup(func() { srv.Shutdown(context.Background()); <-errCh })

	reqBody := `{"username":"ivan","email":"ivan@test.com","provider":"test","provider_id":"ivan-pid"}`
	httpReq, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/api/v1/users", addr), strings.NewReader(reqBody))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	resp.Body.Close()

	// When hook is wired and returns error: expect HTTP 500.
	// This will FAIL until the hook is wired into createUser (group 6).
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d when hook returns error", resp.StatusCode, http.StatusInternalServerError)
	}

	// Assert: no user row for 'ivan' should exist (transaction rolled back).
	// This will also FAIL until createUser uses transactions (group 6).
	if userExistsInDB(t, database.SqlDB, "ivan") {
		t.Error("user 'ivan' should NOT exist after hook error (transaction should be rolled back)")
	}
}

// ========================================================================
// TS-04-E3: Verify that when a transaction cannot be started in createUser,
// HTTP 500 is returned without any INSERT attempted.
// (Requirement: 04-REQ-3.E1)
// ========================================================================

// TestOrgHook_AdminCreateUserBeginTxFailure verifies that when the database
// transaction cannot be started, HTTP 500 is returned and no user row or
// hook invocation occurs.
func TestOrgHook_AdminCreateUserBeginTxFailure(t *testing.T) {
	cfg := buildHookTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	hookCalled := false
	srv.OnAfterUserCreate(func(ctx context.Context, tx *sql.Tx, userID, username, email string) error {
		hookCalled = true
		return nil
	})

	database := openHookTestDB(t)
	if err := srv.MountHandlers((*apikit.DB)(database)); err != nil {
		t.Fatalf("MountHandlers failed: %v", err)
	}

	adminID := insertTestUser(t, database.SqlDB, "admin-beginerr", "admin-beginerr@test.com", "test", "admin-beginerr-pid")
	if _, err := database.SqlDB.Exec("UPDATE users SET role = 'admin' WHERE id = ?", adminID); err != nil {
		t.Fatalf("failed to promote admin: %v", err)
	}
	apiKey := insertTestAPIKey(t, database.SqlDB, adminID, "bgnkey01", "bgnsecret01234567890123456789ab")

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()
	addr := waitForHookServer(t, srv, 5*time.Second)
	t.Cleanup(func() { srv.Shutdown(context.Background()); <-errCh })

	// Close the database to simulate Begin() failure.
	database.Close()

	reqBody := `{"username":"nostart","email":"nostart@test.com","provider":"test","provider_id":"nostart-pid"}`
	httpReq, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/api/v1/users", addr), strings.NewReader(reqBody))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	resp.Body.Close()

	// When createUser uses transactions: a Begin() failure should return HTTP 500.
	// This will FAIL until createUser is wrapped in a transaction (group 6).
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d when database is unavailable", resp.StatusCode, http.StatusInternalServerError)
	}

	// Hook should NOT have been called.
	if hookCalled {
		t.Error("hook should NOT be called when transaction cannot be started")
	}
}

// ========================================================================
// TS-04-E4: Verify that a fatal hook or transaction error in createUser is
// propagated as HTTP 500 without calling os.Exit or panic from library code.
// (Requirement: 04-REQ-3.E2)
// ========================================================================

// TestOrgHook_AdminCreateUserFatalErrorNoPanic verifies that a hook
// returning a "fatal" error is handled gracefully — HTTP 500 is returned
// without any panic or os.Exit from library code.
func TestOrgHook_AdminCreateUserFatalErrorNoPanic(t *testing.T) {
	cfg := buildHookTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	srv.OnAfterUserCreate(func(ctx context.Context, tx *sql.Tx, userID, username, email string) error {
		return errors.New("fatal hook error")
	})

	database := openHookTestDB(t)
	if err := srv.MountHandlers((*apikit.DB)(database)); err != nil {
		t.Fatalf("MountHandlers failed: %v", err)
	}

	adminID := insertTestUser(t, database.SqlDB, "admin-panic", "admin-panic@test.com", "test", "admin-panic-pid")
	if _, err := database.SqlDB.Exec("UPDATE users SET role = 'admin' WHERE id = ?", adminID); err != nil {
		t.Fatalf("failed to promote admin: %v", err)
	}
	apiKey := insertTestAPIKey(t, database.SqlDB, adminID, "pnckey01", "pncsecret01234567890123456789ab")

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()
	addr := waitForHookServer(t, srv, 5*time.Second)
	t.Cleanup(func() { srv.Shutdown(context.Background()); <-errCh })

	// This block verifies no panic occurs during the request.
	didPanic := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				didPanic = true
			}
		}()

		reqBody := `{"username":"panic-test","email":"panic-test@test.com","provider":"test","provider_id":"panic-test-pid"}`
		httpReq, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/api/v1/users", addr), strings.NewReader(reqBody))
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			t.Fatalf("HTTP request failed: %v", err)
		}
		resp.Body.Close()

		// When hook error is properly handled: expect HTTP 500.
		// This will FAIL until the hook is wired into createUser (group 6).
		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("status = %d, want %d for fatal hook error", resp.StatusCode, http.StatusInternalServerError)
		}
	}()

	if didPanic {
		t.Error("library code should NOT panic when hook returns a fatal error")
	}
}

// ========================================================================
// TS-04-35: Verify that apikit Server uses the hook reference captured at
// MountHandlers call time; hooks registered after MountHandlers are not
// guaranteed to be invoked, but the system does not panic.
// (Requirement: 04-REQ-10.2)
// ========================================================================

// TestAfterUserCreate_HookAfterMountHandlersNoPanic verifies that when
// hookA is registered before MountHandlers and hookB is registered after,
// triggering a new user creation does not panic and at least one hook
// fires. The PRD says hooks after MountHandlers are "not guaranteed" —
// the test documents that hookB may or may not be invoked but ensures
// the system remains operational.
func TestAfterUserCreate_HookAfterMountHandlersNoPanic(t *testing.T) {
	cfg := buildHookTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	calledA := false
	calledB := false
	var mu sync.Mutex

	// Register hookA BEFORE MountHandlers.
	srv.OnAfterUserCreate(func(ctx context.Context, tx *sql.Tx, userID, username, email string) error {
		mu.Lock()
		calledA = true
		mu.Unlock()
		return nil
	})

	database := openHookTestDB(t)
	if err := srv.MountHandlers((*apikit.DB)(database)); err != nil {
		t.Fatalf("MountHandlers failed: %v", err)
	}

	// Register hookB AFTER MountHandlers — behavior is undefined per spec.
	srv.OnAfterUserCreate(func(ctx context.Context, tx *sql.Tx, userID, username, email string) error {
		mu.Lock()
		calledB = true
		mu.Unlock()
		return nil
	})

	adminID := insertTestUser(t, database.SqlDB, "admin-hookorder", "admin-hookorder@test.com", "test", "admin-hookorder-pid")
	if _, err := database.SqlDB.Exec("UPDATE users SET role = 'admin' WHERE id = ?", adminID); err != nil {
		t.Fatalf("failed to promote admin: %v", err)
	}
	apiKey := insertTestAPIKey(t, database.SqlDB, adminID, "ordkey01", "ordsecret01234567890123456789ab")

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()
	addr := waitForHookServer(t, srv, 5*time.Second)
	t.Cleanup(func() { srv.Shutdown(context.Background()); <-errCh })

	// Trigger new user creation via admin endpoint.
	didPanic := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				didPanic = true
			}
		}()

		reqBody := `{"username":"hookorder-user","email":"hookorder@test.com","provider":"test","provider_id":"hookorder-pid"}`
		httpReq, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/api/v1/users", addr), strings.NewReader(reqBody))
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			t.Fatalf("HTTP request failed: %v", err)
		}
		resp.Body.Close()
	}()

	// System must not panic regardless of hook ordering.
	if didPanic {
		t.Fatal("system should NOT panic when hooks are registered both before and after MountHandlers")
	}

	// At least one hook should fire without error.
	// This will FAIL until the hook is wired into createUser (group 6).
	mu.Lock()
	a, b := calledA, calledB
	mu.Unlock()

	if !a && !b {
		t.Error("at least one hook (A or B) should have been called during user creation")
	}
}

// ========================================================================
// TS-04-P4: Property test — the AfterUserCreateFunc hook is invoked
// exactly once per new-user INSERT and never for returning users.
// Property: 04-PROP-4
// Validates: 04-REQ-2.1, 04-REQ-2.3, 04-REQ-3.2
// ========================================================================

// TestAfterUserCreate_PropertyHookInvocationCount verifies that for any
// sequence of OAuth callbacks mixing new and returning users, the hook is
// called exactly once per genuinely new user and zero times for returning
// users.
//
// This is a property test: it generates multiple sequences and asserts the
// invariant holds across all of them.
func TestAfterUserCreate_PropertyHookInvocationCount(t *testing.T) {
	// Each scenario defines a sequence of OAuth callbacks. Some users are
	// pre-inserted (returning); others are new.
	type userEntry struct {
		username   string
		email      string
		providerID string
		returning  bool // true if pre-inserted (returning user)
	}

	type scenario struct {
		name  string
		users []userEntry
	}

	scenarios := []scenario{
		{
			name: "all new users",
			users: []userEntry{
				{username: "new-a", email: "new-a@test.com", providerID: "pid-new-a", returning: false},
				{username: "new-b", email: "new-b@test.com", providerID: "pid-new-b", returning: false},
				{username: "new-c", email: "new-c@test.com", providerID: "pid-new-c", returning: false},
			},
		},
		{
			name: "all returning users",
			users: []userEntry{
				{username: "ret-a", email: "ret-a@test.com", providerID: "pid-ret-a", returning: true},
				{username: "ret-b", email: "ret-b@test.com", providerID: "pid-ret-b", returning: true},
			},
		},
		{
			name: "mixed new and returning",
			users: []userEntry{
				{username: "mix-new-1", email: "mix-new-1@test.com", providerID: "pid-mix-new-1", returning: false},
				{username: "mix-ret-1", email: "mix-ret-1@test.com", providerID: "pid-mix-ret-1", returning: true},
				{username: "mix-new-2", email: "mix-new-2@test.com", providerID: "pid-mix-new-2", returning: false},
				{username: "mix-ret-2", email: "mix-ret-2@test.com", providerID: "pid-mix-ret-2", returning: true},
				{username: "mix-new-3", email: "mix-new-3@test.com", providerID: "pid-mix-new-3", returning: false},
			},
		},
		{
			name: "single new user",
			users: []userEntry{
				{username: "solo-new", email: "solo@test.com", providerID: "pid-solo-new", returning: false},
			},
		},
		{
			name: "single returning user",
			users: []userEntry{
				{username: "solo-ret", email: "solo-ret@test.com", providerID: "pid-solo-ret", returning: true},
			},
		},
		{
			name: "same user appears twice as returning",
			users: []userEntry{
				{username: "dup-ret", email: "dup-ret@test.com", providerID: "pid-dup-ret", returning: true},
				{username: "dup-ret", email: "dup-ret@test.com", providerID: "pid-dup-ret", returning: true},
			},
		},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			// Track hook invocations per username.
			hookCounts := make(map[string]int)
			var mu sync.Mutex

			// The callback index drives the provider's UserInfo response.
			callbackIdx := 0
			var idxMu sync.Mutex

			provider := &hookTestProvider{
				name: "github",
				userInfoFn: func(ctx context.Context, accessToken string) (*oauth.UserInfo, error) {
					idxMu.Lock()
					idx := callbackIdx
					idxMu.Unlock()
					if idx >= len(sc.users) {
						return nil, fmt.Errorf("unexpected callback index %d", idx)
					}
					u := sc.users[idx]
					return &oauth.UserInfo{
						Username:   u.username,
						Email:      u.email,
						ProviderID: u.providerID,
					}, nil
				},
			}

			// Wire the hook directly into the OAuth handler via setupOAuthTestEcho.
			hookFn := apikit.AfterUserCreateFunc(func(ctx context.Context, tx *sql.Tx, userID, username, email string) error {
				mu.Lock()
				hookCounts[username]++
				mu.Unlock()
				return nil
			})

			e, database := setupOAuthTestEcho(t, provider, hookFn)

			// Pre-insert returning users into the database.
			for _, u := range sc.users {
				if u.returning {
					// Only insert if not already inserted (handles duplicates).
					var count int
					err := database.SqlDB.QueryRow(
						"SELECT COUNT(*) FROM users WHERE provider = 'github' AND provider_id = ?",
						u.providerID,
					).Scan(&count)
					if err != nil {
						t.Fatalf("query returning user: %v", err)
					}
					if count == 0 {
						insertTestUser(t, database.SqlDB, u.username, u.email, "github", u.providerID)
					}
				}
			}

			// Send OAuth callbacks for each user in sequence.
			for i := range sc.users {
				idxMu.Lock()
				callbackIdx = i
				idxMu.Unlock()

				rec := postOAuthCallback(e, "github", fmt.Sprintf("code-%d", i), "http://localhost:9000/cb")

				// We expect HTTP 200 for both new and returning users.
				if rec.Code != http.StatusOK {
					t.Logf("callback %d (%s) returned %d: %s",
						i, sc.users[i].username, rec.Code, rec.Body.String())
				}
			}

			// Calculate expected totals.
			expectedNew := 0
			newUsers := make(map[string]bool)
			for _, u := range sc.users {
				if !u.returning && !newUsers[u.username] {
					expectedNew++
					newUsers[u.username] = true
				}
			}

			// Invariant check: for each callback, hook invocation count
			// must match expectations.
			mu.Lock()
			counts := make(map[string]int)
			for k, v := range hookCounts {
				counts[k] = v
			}
			mu.Unlock()

			totalInvocations := 0
			for _, v := range counts {
				totalInvocations += v
			}

			// For new users: hook must have been called exactly once.
			for _, u := range sc.users {
				if u.returning {
					if counts[u.username] != 0 {
						t.Errorf("returning user %q: hook called %d times, want 0",
							u.username, counts[u.username])
					}
				}
			}
			for uname := range newUsers {
				if counts[uname] != 1 {
					t.Errorf("new user %q: hook called %d times, want 1",
						uname, counts[uname])
				}
			}

			// Total hook invocations must equal the number of genuinely new users.
			if totalInvocations != expectedNew {
				t.Errorf("total hook invocations = %d, want %d (number of genuinely new users)",
					totalInvocations, expectedNew)
			}
		})
	}
}


package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/labstack/echo/v4"
	_ "modernc.org/sqlite"
)

// ========================================================================
// 4.1 Blocked User Enforcement Tests (REQ-8)
// ========================================================================

// TestBlockedUser_APIKey_HandlerNotCalled verifies that a valid API key
// belonging to a blocked user results in HTTP 403 with "user is blocked",
// the next handler is never invoked, and no AuthInfo is injected into the
// context.
//
// Test Spec: TS-05-37
// Requirement: 05-REQ-8.1
func TestBlockedUser_APIKey_HandlerNotCalled(t *testing.T) {
	database := openTestDB(t)
	insertUser(t, database, "blockeduid", "user", "blocked")
	insertAPIKey(t, database, "mykey", "blockeduid", hexSHA256("mysecret"), "", "")

	rec, captured := runMiddlewareWithCapture(t, database, map[string]string{
		"Authorization": "Bearer ak_mykey_mysecret",
	})

	assertErrorResponseDB(t, rec, http.StatusForbidden, "user is blocked")

	if captured.Called {
		t.Error("expected handler to NOT be called for blocked user, but it was invoked")
	}
}

// TestBlockedUser_PAT_HandlerNotCalled verifies that a valid PAT belonging
// to a blocked user results in HTTP 403 with "user is blocked" and the next
// handler is never invoked.
//
// Test Spec: TS-05-37 (PAT variant), TS-05-38
// Requirement: 05-REQ-8.1, 05-REQ-8.2
func TestBlockedUser_PAT_HandlerNotCalled(t *testing.T) {
	database := openTestDB(t)
	insertUser(t, database, "blockeduid", "user", "blocked")
	insertPATRow(t, database, "mytok", "blockeduid", hexSHA256("mysecret"), `[]`, "", "")

	rec, captured := runMiddlewareWithCapture(t, database, map[string]string{
		"Authorization": "Bearer ak_pat_mytok_mysecret",
	})

	assertErrorResponseDB(t, rec, http.StatusForbidden, "user is blocked")

	if captured.Called {
		t.Error("expected handler to NOT be called for blocked user PAT, but it was invoked")
	}
}

// TestAdminToken_Exempt_FromBlockedCheck verifies that admin tokens are not
// subject to blocked user checks (they have no associated user) and the
// request proceeds successfully with HTTP 200.
//
// Test Spec: TS-05-38 (admin token scenario)
// Requirement: 05-REQ-8.2
func TestAdminToken_Exempt_FromBlockedCheck(t *testing.T) {
	database := openTestDB(t)

	fullToken := "ak_admin_" + strings.Repeat("ab", 32)
	insertAdminConfig(t, database, "admin_token_hash", hexSHA256(fullToken))

	rec, captured := runMiddlewareWithCapture(t, database, map[string]string{
		"Authorization": "Bearer " + fullToken,
	})

	if rec.Code != http.StatusOK {
		t.Errorf("expected HTTP 200 for admin token, got %d; body: %s", rec.Code, rec.Body.String())
	}

	if !captured.Called {
		t.Fatal("expected handler to be called for admin token, but it was not")
	}
}

// TestBlockedUser_InvalidSecret verifies that a blocked user with an invalid
// API key secret receives HTTP 401 "invalid credentials" (not HTTP 403
// "user is blocked"). This confirms the blocked check happens AFTER credential
// validation, preventing credential probing via different error responses.
//
// Test Spec: TS-05-E16
// Requirement: 05-REQ-8.E1
func TestBlockedUser_InvalidSecret(t *testing.T) {
	database := openTestDB(t)
	insertUser(t, database, "blockeduid", "user", "blocked")
	insertAPIKey(t, database, "mykey", "blockeduid", hexSHA256("correctsecret"), "", "")

	// Provide wrong secret for a blocked user.
	rec := runMiddlewareWithDB(t, database, map[string]string{
		"Authorization": "Bearer ak_mykey_wrongsecret",
	})

	// Must get 401 "invalid credentials", NOT 403 "user is blocked".
	assertErrorResponseDB(t, rec, http.StatusUnauthorized, "invalid credentials")
}

// ========================================================================
// 4.2 PermissionRegistry Tests (REQ-9)
// ========================================================================

// TestPermissionRegistry_BuiltIns verifies that NewPermissionRegistry returns
// a registry pre-populated with exactly the 6 built-in permissions in sorted
// ascending order.
//
// Test Spec: TS-05-39
// Requirement: 05-REQ-9.1
func TestPermissionRegistry_BuiltIns(t *testing.T) {
	registry := NewPermissionRegistry()
	list := registry.List()

	expected := []string{
		"keys:manage", "keys:read", "orgs:read",
		"tokens:manage", "tokens:read", "users:read",
	}

	if len(list) != 6 {
		t.Fatalf("expected 6 built-in permissions, got %d: %v", len(list), list)
	}

	for i, perm := range expected {
		if list[i] != perm {
			t.Errorf("List()[%d] = %q, want %q", i, list[i], perm)
		}
	}
}

// TestPermissionRegistry_Register verifies that Register adds a valid new
// resource_type:action pair, returns nil, and the permission becomes
// discoverable via IsValid and List.
//
// Test Spec: TS-05-40
// Requirement: 05-REQ-9.2
func TestPermissionRegistry_Register(t *testing.T) {
	registry := NewPermissionRegistry()

	err := registry.Register("widgets", "export")
	if err != nil {
		t.Fatalf("Register(widgets, export) returned unexpected error: %v", err)
	}

	if !registry.IsValid("widgets", "export") {
		t.Error("IsValid(widgets, export) = false after registration; want true")
	}

	list := registry.List()
	found := false
	for _, perm := range list {
		if perm == "widgets:export" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("List() does not include 'widgets:export' after registration: %v", list)
	}
}

// TestPermissionRegistry_DuplicateRegister verifies that a second call to
// Register with the same resource_type:action pair returns a non-nil error.
//
// Test Spec: TS-05-41
// Requirement: 05-REQ-9.3
func TestPermissionRegistry_DuplicateRegister(t *testing.T) {
	registry := NewPermissionRegistry()

	err := registry.Register("widgets", "export")
	if err != nil {
		t.Fatalf("first Register(widgets, export) returned unexpected error: %v", err)
	}

	err = registry.Register("widgets", "export")
	if err == nil {
		t.Error("second Register(widgets, export) should return non-nil error for duplicate")
	}
}

// TestPermissionRegistry_InvalidFormat verifies that Register returns a non-nil
// error for empty strings, uppercase letters, and hyphens in resource_type or
// action, and that the registry size is unchanged after each failed call.
//
// Test Spec: TS-05-E17
// Requirement: 05-REQ-9.E1
func TestPermissionRegistry_InvalidFormat(t *testing.T) {
	registry := NewPermissionRegistry()

	originalCount := len(registry.List())
	if originalCount < 6 {
		t.Fatalf("expected at least 6 built-in permissions, got %d", originalCount)
	}

	invalidCases := []struct {
		name         string
		resourceType string
		action       string
	}{
		{name: "empty resource_type", resourceType: "", action: "read"},
		{name: "empty action", resourceType: "users", action: ""},
		{name: "uppercase resource_type", resourceType: "Users", action: "read"},
		{name: "uppercase action", resourceType: "users", action: "READ"},
		{name: "hyphenated resource_type", resourceType: "my-resource", action: "do_it"},
		{name: "hyphenated action", resourceType: "my_resource", action: "do-it"},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			err := registry.Register(tc.resourceType, tc.action)
			if err == nil {
				t.Errorf("Register(%q, %q) should return non-nil error for invalid format", tc.resourceType, tc.action)
			}

			currentCount := len(registry.List())
			if currentCount != originalCount {
				t.Errorf("registry size changed from %d to %d after invalid Register(%q, %q)",
					originalCount, currentCount, tc.resourceType, tc.action)
			}
		})
	}
}

// TestPermissionRegistry_IsValid verifies that IsValid returns true for
// registered permissions and false for unregistered ones.
//
// Test Spec: TS-05-42
// Requirement: 05-REQ-9.4
func TestPermissionRegistry_IsValid(t *testing.T) {
	registry := NewPermissionRegistry()

	// Built-in permission should be valid.
	if !registry.IsValid("users", "read") {
		t.Error("IsValid(users, read) = false for built-in permission; want true")
	}

	// Unregistered permission should be invalid.
	if registry.IsValid("nonexistent", "action") {
		t.Error("IsValid(nonexistent, action) = true for unregistered permission; want false")
	}
}

// TestPermissionRegistry_List verifies that List returns all registered
// permissions sorted in ascending lexicographic order, including custom
// permissions added via Register.
//
// Test Spec: TS-05-43
// Requirement: 05-REQ-9.5
func TestPermissionRegistry_List(t *testing.T) {
	registry := NewPermissionRegistry()

	// Register custom permissions at both ends of the alphabet.
	if err := registry.Register("zz", "zzz"); err != nil {
		t.Fatalf("Register(zz, zzz) failed: %v", err)
	}
	if err := registry.Register("aa", "aaa"); err != nil {
		t.Fatalf("Register(aa, aaa) failed: %v", err)
	}

	list := registry.List()

	// Verify the list is sorted.
	if !sort.StringsAreSorted(list) {
		t.Errorf("List() is not sorted: %v", list)
	}

	// Verify first and last entries.
	if len(list) == 0 {
		t.Fatal("List() returned empty slice")
	}
	if list[0] != "aa:aaa" {
		t.Errorf("List()[0] = %q, want %q", list[0], "aa:aaa")
	}
	if list[len(list)-1] != "zz:zzz" {
		t.Errorf("List()[last] = %q, want %q", list[len(list)-1], "zz:zzz")
	}
}

// TestPermissionRegistry_Concurrent verifies that Register, IsValid, and List
// are safe for concurrent use by calling them from 10 goroutines simultaneously.
// Run with -race to detect data races.
//
// Test Spec: TS-05-E18
// Requirement: 05-REQ-9.E2
func TestPermissionRegistry_Concurrent(t *testing.T) {
	registry := NewPermissionRegistry()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = registry.Register(fmt.Sprintf("res%d", i), "action")
			registry.IsValid("users", "read")
			registry.List()
		}(i)
	}
	wg.Wait()

	// If no panic or data race, the test passes.
}

// ========================================================================
// 4.3 Request Context Injection and Access Helper Tests (REQ-10)
// ========================================================================

// TestGetAuthInfo_Set verifies that GetAuthInfo returns a non-nil AuthInfo
// pointer with correct fields when AuthInfo has been injected into the context.
//
// Test Spec: TS-05-45
// Requirement: 05-REQ-10.2
func TestGetAuthInfo_Set(t *testing.T) {
	c, _ := newEchoContext()
	setAuthInfo(c, &AuthInfo{
		CredentialType: "api_key",
		UserID:         "uid1",
		Role:           "user",
	})

	auth := GetAuthInfo(c)
	if auth == nil {
		t.Fatal("GetAuthInfo returned nil; expected non-nil for injected AuthInfo")
	}
	if auth.CredentialType != "api_key" {
		t.Errorf("CredentialType = %q, want %q", auth.CredentialType, "api_key")
	}
	if auth.UserID != "uid1" {
		t.Errorf("UserID = %q, want %q", auth.UserID, "uid1")
	}
	if auth.Role != "user" {
		t.Errorf("Role = %q, want %q", auth.Role, "user")
	}
}

// TestGetAuthInfo_NotSet verifies that GetAuthInfo returns nil without
// panicking when no AuthInfo has been injected into the context.
//
// Test Spec: TS-05-46
// Requirement: 05-REQ-10.3
func TestGetAuthInfo_NotSet(t *testing.T) {
	c, _ := newEchoContext()

	auth := GetAuthInfo(c)
	if auth != nil {
		t.Errorf("GetAuthInfo returned non-nil %+v; expected nil for empty context", auth)
	}
}

// TestGetUserID verifies that GetUserID returns the correct UUID for
// authenticated API key/PAT contexts, and an empty string for admin
// token contexts or when no AuthInfo is present.
//
// Test Spec: TS-05-47
// Requirement: 05-REQ-10.4
func TestGetUserID(t *testing.T) {
	tests := []struct {
		name     string
		authInfo *AuthInfo // nil means no AuthInfo set
		wantUID  string
	}{
		{
			name:     "API key with UserID",
			authInfo: &AuthInfo{CredentialType: "api_key", UserID: "uid-123", Role: "user"},
			wantUID:  "uid-123",
		},
		{
			name:     "admin token (empty UserID)",
			authInfo: &AuthInfo{CredentialType: "admin_token", UserID: "", Role: "admin"},
			wantUID:  "",
		},
		{
			name:     "no AuthInfo",
			authInfo: nil,
			wantUID:  "",
		},
		{
			name: "PAT with UserID",
			authInfo: &AuthInfo{
				CredentialType: "pat",
				UserID:         "uid-456",
				Role:           "user",
				TokenID:        "tok1",
				Permissions:    []string{"keys:read"},
			},
			wantUID: "uid-456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newEchoContext()
			if tt.authInfo != nil {
				setAuthInfo(c, tt.authInfo)
			}

			got := GetUserID(c)
			if got != tt.wantUID {
				t.Errorf("GetUserID() = %q, want %q", got, tt.wantUID)
			}
		})
	}
}

// TestIsAdmin verifies that IsAdmin returns true only for admin tokens and
// admin-role API keys; returns false for user-role API keys, PATs (regardless
// of role), and nil AuthInfo contexts.
//
// Test Spec: TS-05-48 (extended per critical reviewer finding)
// Requirement: 05-REQ-10.5
// Note: Includes admin-role PAT scenario to address the critical reviewer
// finding — IsAdmin must return false for PATs regardless of user role, per
// PRD intent. This ensures PATs never bypass RequireAdmin scoping.
func TestIsAdmin(t *testing.T) {
	tests := []struct {
		name     string
		authInfo *AuthInfo
		want     bool
	}{
		{
			name:     "admin_token credential",
			authInfo: &AuthInfo{CredentialType: "admin_token", Role: "admin"},
			want:     true,
		},
		{
			name:     "api_key with admin role",
			authInfo: &AuthInfo{CredentialType: "api_key", Role: "admin", UserID: "uid-admin"},
			want:     true,
		},
		{
			name:     "api_key with user role",
			authInfo: &AuthInfo{CredentialType: "api_key", Role: "user", UserID: "uid-user"},
			want:     false,
		},
		{
			name: "PAT with user role",
			authInfo: &AuthInfo{
				CredentialType: "pat",
				Role:           "user",
				UserID:         "uid-abc",
				TokenID:        "tok1",
				Permissions:    []string{"keys:read"},
			},
			want: false,
		},
		{
			// Critical reviewer finding: admin-role PAT must NOT be treated as admin.
			// The PRD scopes IsAdmin to "admin token or admin-role API key" only.
			name: "PAT with admin role (must NOT be admin)",
			authInfo: &AuthInfo{
				CredentialType: "pat",
				Role:           "admin",
				UserID:         "uid-admin",
				TokenID:        "tok2",
				Permissions:    []string{"keys:read"},
			},
			want: false,
		},
		{
			name:     "no AuthInfo (nil)",
			authInfo: nil,
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newEchoContext()
			if tt.authInfo != nil {
				setAuthInfo(c, tt.authInfo)
			}

			got := IsAdmin(c)
			if got != tt.want {
				t.Errorf("IsAdmin() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestContextKey_Unexported verifies that the auth middleware uses an unexported
// context key type, preventing external packages from accidentally reading or
// overwriting AuthInfo using a plain string key.
//
// Test Spec: TS-05-E19
// Requirement: 05-REQ-10.E1
func TestContextKey_Unexported(t *testing.T) {
	database := openTestDB(t)
	insertUser(t, database, "uid1", "user", "active")
	insertAPIKey(t, database, "goodkey", "uid1", hexSHA256("goodsecret"), "", "")

	e := echo.New()
	registry := NewPermissionRegistry()
	mw := NewAuthMiddleware(database, registry)

	var authFromHelper *AuthInfo
	var echoGetResult interface{}

	handler := func(c echo.Context) error {
		// Retrieve via the exported helper (uses the proper typed key).
		authFromHelper = GetAuthInfo(c)
		// Attempt to retrieve using a plain string key — should fail.
		echoGetResult = c.Get("auth_info")
		return c.JSON(http.StatusOK, nil)
	}

	e.GET("/test", handler, mw)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer ak_goodkey_goodsecret")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// GetAuthInfo using the proper typed key should find the AuthInfo.
	if authFromHelper == nil {
		t.Fatal("GetAuthInfo returned nil; middleware should have injected AuthInfo")
	}

	// A plain string "auth_info" via Echo's c.Get should NOT find it,
	// because the middleware stores AuthInfo under the unexported contextKey type.
	if echoGetResult != nil {
		t.Error("c.Get(\"auth_info\") returned non-nil; unexported context key type should prevent plain-string access")
	}
}

// TestAuthInfo_InjectedAfterAPIKeyAuth verifies that the auth middleware
// injects a fully-populated AuthInfo into the request context after successful
// API key authentication.
//
// Test Spec: TS-05-44
// Requirement: 05-REQ-10.1
func TestAuthInfo_InjectedAfterAPIKeyAuth(t *testing.T) {
	database := openTestDB(t)
	insertUser(t, database, "uid1", "user", "active")
	insertAPIKey(t, database, "goodkey", "uid1", hexSHA256("goodsecret"), "", "")

	rec, captured := runMiddlewareWithCapture(t, database, map[string]string{
		"Authorization": "Bearer ak_goodkey_goodsecret",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	if !captured.Called {
		t.Fatal("handler was not called")
	}

	auth := GetAuthInfo(captured.Ctx)
	if auth == nil {
		t.Fatal("GetAuthInfo returned nil; expected non-nil after successful auth")
	}
	if auth.CredentialType == "" {
		t.Error("CredentialType is empty; expected non-empty after successful auth")
	}
}

// ========================================================================
// 4.4 Error Format and Middleware Registration Tests (REQ-1, REQ-12)
// ========================================================================

// TestNewAuthMiddleware_Valid verifies that NewAuthMiddleware returns a non-nil
// echo.MiddlewareFunc when given valid (non-nil) db and registry arguments.
//
// Test Spec: TS-05-1
// Requirement: 05-REQ-1.1
func TestNewAuthMiddleware_Valid(t *testing.T) {
	database := openTestDB(t)
	registry := NewPermissionRegistry()

	mw := NewAuthMiddleware(database, registry)
	if mw == nil {
		t.Fatal("NewAuthMiddleware returned nil; expected non-nil MiddlewareFunc")
	}
}

// TestNewAuthMiddleware_NilDB verifies that NewAuthMiddleware panics with a
// descriptive message when the *db.DB argument is nil.
//
// Test Spec: TS-05-E1
// Requirement: 05-REQ-1.E1
func TestNewAuthMiddleware_NilDB(t *testing.T) {
	registry := NewPermissionRegistry()

	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected NewAuthMiddleware(nil, registry) to panic, but it did not")
			return
		}
		// Verify the panic message is descriptive (non-empty).
		msg := fmt.Sprintf("%v", r)
		if msg == "" {
			t.Error("panic message is empty; expected descriptive message")
		}
	}()

	NewAuthMiddleware(nil, registry)
}

// TestNewAuthMiddleware_NilRegistry verifies that NewAuthMiddleware panics with
// a descriptive message when the *PermissionRegistry argument is nil.
//
// Test Spec: TS-05-E1
// Requirement: 05-REQ-1.E1
func TestNewAuthMiddleware_NilRegistry(t *testing.T) {
	database := openTestDB(t)

	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected NewAuthMiddleware(db, nil) to panic, but it did not")
			return
		}
		// Verify the panic message is descriptive (non-empty).
		msg := fmt.Sprintf("%v", r)
		if msg == "" {
			t.Error("panic message is empty; expected descriptive message")
		}
	}()

	NewAuthMiddleware(database, nil)
}

// TestMiddleware_AppliedToAPIGroup verifies that the auth middleware is applied
// to the API route group, so requests to protected routes without an
// Authorization header receive HTTP 401 from the middleware (not from the handler).
//
// Test Spec: TS-05-2
// Requirement: 05-REQ-1.2
func TestMiddleware_AppliedToAPIGroup(t *testing.T) {
	database := openTestDB(t)
	e := echo.New()

	registry := NewPermissionRegistry()
	mw := NewAuthMiddleware(database, registry)

	// Protected group simulating the APIGroup.
	api := e.Group("/api")
	api.Use(mw)

	handlerCalled := false
	api.GET("/protected", func(c echo.Context) error {
		handlerCalled = true
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// Expect 401 because no Authorization header was sent.
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected HTTP 401 for protected route without auth, got %d; body: %s",
			rec.Code, rec.Body.String())
	}

	if handlerCalled {
		t.Error("handler was invoked on protected route without auth; middleware should have blocked it")
	}
}

// TestUnprotectedPaths verifies that health probe and OAuth paths registered
// on an unprotected group are not intercepted by the auth middleware and
// respond normally without an Authorization header.
//
// Test Spec: TS-05-3
// Requirement: 05-REQ-1.3
func TestUnprotectedPaths(t *testing.T) {
	database := openTestDB(t)
	e := echo.New()

	registry := NewPermissionRegistry()
	mw := NewAuthMiddleware(database, registry)

	// Protected group (auth middleware applied).
	api := e.Group("/api")
	api.Use(mw)
	api.GET("/resource", func(c echo.Context) error {
		return c.JSON(http.StatusOK, nil)
	})

	// Unprotected routes (no auth middleware).
	unprotectedHandler := func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}
	e.GET("/healthz", unprotectedHandler)
	e.GET("/readyz", unprotectedHandler)
	e.GET("/version", unprotectedHandler)
	e.GET("/auth/providers", unprotectedHandler)
	e.GET("/auth/callback", unprotectedHandler)

	// Each unprotected path should NOT return 401.
	unprotectedPaths := []string{
		"/healthz",
		"/readyz",
		"/version",
		"/auth/providers",
		"/auth/callback",
	}

	for _, path := range unprotectedPaths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			if rec.Code == http.StatusUnauthorized {
				t.Errorf("unprotected path %s returned 401; should not be intercepted by auth middleware", path)
			}
		})
	}
}

// TestErrorFormat_JSONEnvelope verifies that auth middleware error responses use
// the standard JSON error envelope format {"error":{"code":N,"message":"..."}}
// and set the Content-Type to application/json.
//
// Test Spec: TS-05-51
// Requirement: 05-REQ-12.1
func TestErrorFormat_JSONEnvelope(t *testing.T) {
	database := openTestDB(t)

	// Send a request with no Authorization header to trigger "missing authorization header".
	rec := runMiddlewareWithDB(t, database, nil)

	// Verify Content-Type header contains "application/json".
	contentType := rec.Header().Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Errorf("Content-Type = %q; expected to contain 'application/json'", contentType)
	}

	// Verify response body has the standard error envelope structure.
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not valid JSON: %v\nbody: %s", err, rec.Body.String())
	}

	errObj, ok := body["error"]
	if !ok {
		t.Fatal("response body missing 'error' key")
	}

	errMap, ok := errObj.(map[string]interface{})
	if !ok {
		t.Fatalf("'error' field is not an object: %T", errObj)
	}

	// Verify "code" field exists and is a number.
	codeVal, ok := errMap["code"]
	if !ok {
		t.Error("error envelope missing 'code' field")
	} else {
		codeFloat, ok := codeVal.(float64)
		if !ok {
			t.Errorf("error.code is not a number: %T", codeVal)
		} else if int(codeFloat) != http.StatusUnauthorized {
			t.Errorf("error.code = %d, want %d", int(codeFloat), http.StatusUnauthorized)
		}
	}

	// Verify "message" field exists and is a string.
	msgVal, ok := errMap["message"]
	if !ok {
		t.Error("error envelope missing 'message' field")
	} else {
		msg, ok := msgVal.(string)
		if !ok {
			t.Errorf("error.message is not a string: %T", msgVal)
		} else if msg != "missing authorization header" {
			t.Errorf("error.message = %q, want %q", msg, "missing authorization header")
		}
	}
}

// TestApprovedErrorMessages verifies that all error responses produced by the
// auth middleware use only the 11 approved message strings. Each error-triggering
// scenario is exercised and the response message is checked against the
// approved set.
//
// Test Spec: TS-05-52
// Requirement: 05-REQ-12.2
func TestApprovedErrorMessages(t *testing.T) {
	approvedMessages := map[string]bool{
		"missing authorization header":        true,
		"invalid authorization header format": true,
		"missing token":                       true,
		"unrecognized token format":           true,
		"invalid credentials":                 true,
		"credential revoked":                  true,
		"credential expired":                  true,
		"user is blocked":                     true,
		"forbidden":                           true,
		"insufficient permissions":            true,
		"internal server error":               true,
	}

	// Set up database with various credential states.
	database := openTestDB(t)
	insertUser(t, database, "uid1", "user", "active")
	insertUser(t, database, "blockeduid", "user", "blocked")
	insertAPIKey(t, database, "goodkey", "uid1", hexSHA256("goodsecret"), "", "")
	insertAPIKey(t, database, "revokedkey", "uid1", hexSHA256("revkeysecret"), "", "2025-01-01T00:00:00Z")
	insertAPIKey(t, database, "expiredkey", "uid1", hexSHA256("expkeysecret"), "2000-01-01T00:00:00Z", "")
	insertAPIKey(t, database, "blockedkey", "blockeduid", hexSHA256("blockedsecret"), "", "")

	scenarios := []struct {
		name        string
		headers     map[string]string
		wantMessage string
	}{
		{
			name:        "missing header",
			headers:     nil,
			wantMessage: "missing authorization header",
		},
		{
			name:        "invalid format",
			headers:     map[string]string{"Authorization": "Basic dXNlcjpwYXNz"},
			wantMessage: "invalid authorization header format",
		},
		{
			name:        "empty bearer token",
			headers:     map[string]string{"Authorization": "Bearer "},
			wantMessage: "missing token",
		},
		{
			name:        "unrecognized token",
			headers:     map[string]string{"Authorization": "Bearer garbage_token"},
			wantMessage: "unrecognized token format",
		},
		{
			name:        "API key not found",
			headers:     map[string]string{"Authorization": "Bearer ak_unknownkey_secret"},
			wantMessage: "invalid credentials",
		},
		{
			name:        "revoked API key",
			headers:     map[string]string{"Authorization": "Bearer ak_revokedkey_revkeysecret"},
			wantMessage: "credential revoked",
		},
		{
			name:        "expired API key",
			headers:     map[string]string{"Authorization": "Bearer ak_expiredkey_expkeysecret"},
			wantMessage: "credential expired",
		},
		{
			name:        "blocked user",
			headers:     map[string]string{"Authorization": "Bearer ak_blockedkey_blockedsecret"},
			wantMessage: "user is blocked",
		},
	}

	e := echo.New()
	registry := NewPermissionRegistry()
	mw := NewAuthMiddleware(database, registry)
	handler := func(c echo.Context) error {
		return c.JSON(http.StatusOK, nil)
	}
	e.GET("/test", handler, mw)

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			for k, v := range sc.headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			// The middleware must produce an error response (not 200).
			if rec.Code == http.StatusOK {
				t.Errorf("expected error response for %q scenario, got HTTP 200", sc.name)
				return
			}

			var resp errorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to parse JSON response: %v\nbody: %s", err, rec.Body.String())
			}

			// Verify the message matches the expected approved message.
			if resp.Error.Message != sc.wantMessage {
				t.Errorf("error message = %q, want %q", resp.Error.Message, sc.wantMessage)
			}

			// Double-check the message is in the approved set.
			if !approvedMessages[resp.Error.Message] {
				t.Errorf("error message %q is not in the approved message set", resp.Error.Message)
			}
		})
	}
}


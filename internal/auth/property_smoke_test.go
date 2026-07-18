package auth

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"testing/quick"

	"github.com/labstack/echo/v4"
	_ "modernc.org/sqlite"

	"github.com/txsvc/apikit/internal/db"
)

// ========================================================================
// 5.1 Property Tests: Detection Order and Constant-Time Comparison
// ========================================================================

// TestProperty_DetectionOrder verifies PROP-1: for any token starting with
// '<TokenPrefix>_pat_', parseToken always returns 'pat' and never 'api_key',
// regardless of content after the infix.
//
// Uses testing/quick to generate random token suffixes.
//
// Test Spec: TS-05-P1
// Requirements: 05-REQ-3.1, 05-REQ-3.3, 05-REQ-3.E3
func TestProperty_DetectionOrder(t *testing.T) {
	// Generate a random alphanumeric string of length 1..50.
	randAlphaNum := func(r *rand.Rand, maxLen int) string {
		const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
		length := r.Intn(maxLen) + 1
		b := make([]byte, length)
		for i := range b {
			b[i] = charset[r.Intn(len(charset))]
		}
		return string(b)
	}

	// Property function: for any token matching 'ak_pat_<x>_<y>', parseToken
	// must return "pat" or an error, never "api_key".
	f := func(seed int64) bool {
		r := rand.New(rand.NewSource(seed))
		tokenID := randAlphaNum(r, 50)
		secret := randAlphaNum(r, 50)
		token := "ak_pat_" + tokenID + "_" + secret

		credType, _, err := parseToken(token)

		// Must be "pat" or error — never "api_key".
		if credType == "api_key" {
			t.Errorf("parseToken(%q) returned 'api_key'; must return 'pat' for pat-prefixed tokens", token)
			return false
		}

		// If no error, must be "pat".
		if err == nil && credType != "pat" {
			t.Errorf("parseToken(%q) = %q, want 'pat'", token, credType)
			return false
		}

		return true
	}

	cfg := &quick.Config{MaxCount: 200}
	if err := quick.Check(f, cfg); err != nil {
		t.Errorf("property check failed: %v", err)
	}
}

// TestProperty_DetectionOrder_EdgeCases tests specific edge cases for
// detection order: PAT tokens with various structures that could superficially
// match API key patterns.
//
// Test Spec: TS-05-P1
// Requirements: 05-REQ-3.1, 05-REQ-3.E3
func TestProperty_DetectionOrder_EdgeCases(t *testing.T) {
	tokens := []string{
		"ak_pat_a_b",                                   // minimal PAT
		"ak_pat_admin_secret",                          // PAT with "admin" as token_id
		"ak_pat_pat_pat",                               // PAT with "pat" as token_id
		"ak_pat_123_456",                               // numeric components
		"ak_pat_x_" + strings.Repeat("a", 64),          // secret that looks like admin hex
	}

	for _, token := range tokens {
		credType, _, err := parseToken(token)
		if credType == "api_key" {
			t.Errorf("parseToken(%q) returned 'api_key'; must be 'pat' for pat-prefixed tokens", token)
		}
		if err == nil && credType != "pat" {
			t.Errorf("parseToken(%q) = %q, want 'pat'", token, credType)
		}
	}
}

// TestProperty_ConstantTimeComparison verifies PROP-2: every hash comparison
// in the auth middleware uses crypto/subtle.ConstantTimeCompare; no hash
// comparison uses == or bytes.Equal.
//
// This is a code-inspection property test that reads all non-test source files
// in the auth package and verifies the absence of unsafe comparison patterns
// and the presence of subtle.ConstantTimeCompare.
//
// Test Spec: TS-05-P2
// Requirements: 05-REQ-11.1, 05-REQ-4.2, 05-REQ-5.4, 05-REQ-6.4
func TestProperty_ConstantTimeComparison(t *testing.T) {
	source := readAuthSourceFiles(t)
	if source == "" {
		t.Fatal("no non-test source files found in the auth package")
	}

	// Count subtle.ConstantTimeCompare occurrences — need at least 3
	// (one for admin token, one for API key, one for PAT).
	subtleCount := strings.Count(source, "subtle.ConstantTimeCompare")
	if subtleCount < 3 {
		t.Errorf("expected at least 3 uses of subtle.ConstantTimeCompare (admin, api_key, pat), found %d", subtleCount)
	}

	// Scan for unsafe comparison patterns on hash-related variables.
	lines := strings.Split(source, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip comments.
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") {
			continue
		}

		// Flag bytes.Equal used with hash-related variables.
		if strings.Contains(trimmed, "bytes.Equal") {
			lower := strings.ToLower(trimmed)
			if strings.Contains(lower, "hash") || strings.Contains(lower, "secret") {
				t.Errorf("line %d: found bytes.Equal used for hash/secret comparison: %s", i+1, trimmed)
			}
		}

		// Flag direct == comparison of hashToken results.
		if strings.Contains(trimmed, "hashToken(") && strings.Contains(trimmed, "==") {
			t.Errorf("line %d: found direct == comparison of hashToken result: %s", i+1, trimmed)
		}
	}

	// Verify the import of crypto/subtle is present (indicating intentional use).
	if !strings.Contains(source, `"crypto/subtle"`) {
		t.Error("crypto/subtle import not found in source files; required for constant-time comparison")
	}
}

// ========================================================================
// 5.2 Property Tests: Blocked User, Admin Token Identity, Registry Append-Only
// ========================================================================

// TestProperty_BlockedUser verifies PROP-3: for any request with a non-revoked,
// non-expired API key or PAT whose owning user has status='blocked', the
// middleware always returns HTTP 403 'user is blocked' and never calls the
// next handler.
//
// Generates random key_id/secret/user_id combos for both credential types.
//
// Test Spec: TS-05-P3
// Requirements: 05-REQ-8.1, 05-REQ-5.5, 05-REQ-6.5
func TestProperty_BlockedUser(t *testing.T) {
	type credCase struct {
		credType string
		tokenFn  func(id, secret string) string
		insertFn func(t *testing.T, database *db.DB, id, userID, secretHash string)
	}

	cases := []credCase{
		{
			credType: "api_key",
			tokenFn:  func(id, secret string) string { return "ak_" + id + "_" + secret },
			insertFn: func(t *testing.T, database *db.DB, id, userID, secretHash string) {
				insertAPIKey(t, database, id, userID, secretHash, "", "")
			},
		},
		{
			credType: "pat",
			tokenFn:  func(id, secret string) string { return "ak_pat_" + id + "_" + secret },
			insertFn: func(t *testing.T, database *db.DB, id, userID, secretHash string) {
				insertPATRow(t, database, id, userID, secretHash, `[]`, "", "")
			},
		},
	}

	for iteration := 0; iteration < 10; iteration++ {
		for _, cc := range cases {
			iterStr := strings.Repeat("x", iteration+1)
			t.Run(cc.credType+"/iter"+iterStr[:1]+string(rune('0'+iteration)), func(t *testing.T) {
				database := openTestDB(t)

				// Generate unique IDs per iteration to avoid collisions.
				userID := "buser" + cc.credType[:1] + string(rune('a'+iteration))
				credID := "cred" + cc.credType[:1] + string(rune('a'+iteration))
				secret := "sec" + string(rune('a'+iteration))

				// Insert user as blocked.
				insertUser(t, database, userID, "user", "blocked")

				// Insert credential with valid hash.
				cc.insertFn(t, database, credID, userID, hexSHA256(secret))

				// Build and send request.
				token := cc.tokenFn(credID, secret)
				rec, captured := runMiddlewareWithCapture(t, database, map[string]string{
					"Authorization": "Bearer " + token,
				})

				// Must return HTTP 403 with "user is blocked".
				if rec.Code != http.StatusForbidden {
					t.Errorf("expected HTTP 403, got %d; body: %s", rec.Code, rec.Body.String())
				}

				var resp errorResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &resp); err == nil {
					if resp.Error.Message != "user is blocked" {
						t.Errorf("expected message 'user is blocked', got %q", resp.Error.Message)
					}
				}

				// Handler must NOT have been called.
				if captured.Called {
					t.Error("handler was invoked for blocked user; expected it to be blocked by middleware")
				}
			})
		}
	}
}

// TestProperty_AdminTokenNoUserID verifies PROP-4: for any request
// authenticated with an admin token, AuthInfo.UserID is always empty and
// AuthInfo.Role is always 'admin'; GetUserID returns empty string.
//
// Generates random 64-char hex strings to form valid admin tokens.
//
// Test Spec: TS-05-P4
// Requirements: 05-REQ-4.3, 05-REQ-10.4
func TestProperty_AdminTokenNoUserID(t *testing.T) {
	// Test with multiple random hex suffixes.
	hexChars := "0123456789abcdef"

	for i := 0; i < 10; i++ {
		t.Run("iter"+string(rune('0'+i)), func(t *testing.T) {
			database := openTestDB(t)

			// Generate a 64-char hex string.
			r := rand.New(rand.NewSource(int64(i * 42)))
			hexBytes := make([]byte, 64)
			for j := range hexBytes {
				hexBytes[j] = hexChars[r.Intn(len(hexChars))]
			}
			hexSuffix := string(hexBytes)

			fullToken := "ak_admin_" + hexSuffix

			// Store the SHA-256 hash of the full token.
			storedHash := hexSHA256(fullToken)
			insertAdminConfig(t, database, "admin_token_hash", storedHash)

			rec, captured := runMiddlewareWithCapture(t, database, map[string]string{
				"Authorization": "Bearer " + fullToken,
			})

			if rec.Code != http.StatusOK {
				t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
			}

			if !captured.Called {
				t.Fatal("handler was not called for valid admin token")
			}

			auth := GetAuthInfo(captured.Ctx)
			if auth == nil {
				t.Fatal("GetAuthInfo returned nil")
			}

			// PROP-4 invariants.
			if auth.UserID != "" {
				t.Errorf("AuthInfo.UserID = %q, want empty string", auth.UserID)
			}
			if auth.Role != "admin" {
				t.Errorf("AuthInfo.Role = %q, want 'admin'", auth.Role)
			}
			if auth.CredentialType != "admin_token" {
				t.Errorf("AuthInfo.CredentialType = %q, want 'admin_token'", auth.CredentialType)
			}

			// GetUserID must return empty string.
			uid := GetUserID(captured.Ctx)
			if uid != "" {
				t.Errorf("GetUserID() = %q, want empty string", uid)
			}
		})
	}
}

// TestProperty_RegistryAppendOnly verifies PROP-5: after any call to
// PermissionRegistry.Register with a valid, non-duplicate permission, IsValid
// returns true for that permission and List includes it; no previously
// registered permission is removed; List is sorted.
//
// Test Spec: TS-05-P5
// Requirements: 05-REQ-9.2, 05-REQ-9.4, 05-REQ-9.5
func TestProperty_RegistryAppendOnly(t *testing.T) {
	registry := NewPermissionRegistry()

	// Start with built-in permissions.
	prevList := registry.List()

	// Register a series of valid permissions.
	newPerms := []struct{ res, act string }{
		{"widgets", "export"},
		{"reports", "generate"},
		{"analytics", "read"},
		{"billing", "manage"},
		{"deployments", "create"},
		{"logs", "view"},
		{"settings", "update"},
		{"teams", "delete"},
	}

	for _, perm := range newPerms {
		beforeList := make([]string, len(prevList))
		copy(beforeList, prevList)

		err := registry.Register(perm.res, perm.act)
		if err != nil {
			t.Fatalf("Register(%q, %q) returned error: %v", perm.res, perm.act, err)
		}

		permStr := perm.res + ":" + perm.act

		// IsValid must return true for the newly registered permission.
		if !registry.IsValid(perm.res, perm.act) {
			t.Errorf("IsValid(%q, %q) = false after registration; want true", perm.res, perm.act)
		}

		afterList := registry.List()

		// New permission must be in the list.
		found := false
		for _, p := range afterList {
			if p == permStr {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("List() does not include %q after registration", permStr)
		}

		// No previously registered permission should be removed.
		for _, prev := range beforeList {
			inAfter := false
			for _, after := range afterList {
				if after == prev {
					inAfter = true
					break
				}
			}
			if !inAfter {
				t.Errorf("previously registered permission %q was removed after registering %q", prev, permStr)
			}
		}

		// List must be sorted.
		if !sort.StringsAreSorted(afterList) {
			t.Errorf("List() is not sorted after registering %q: %v", permStr, afterList)
		}

		prevList = afterList
	}
}

// ========================================================================
// 5.3 Property Tests: RequirePermission Bypass and No-Handler-On-Failure
// ========================================================================

// TestProperty_RequirePermissionBypass verifies PROP-6: RequirePermission
// always returns nil without inspecting the permissions list when
// AuthInfo.CredentialType is 'admin_token' or 'api_key', regardless of what
// permissions are present.
//
// Test Spec: TS-05-P6
// Requirement: 05-REQ-7.5
func TestProperty_RequirePermissionBypass(t *testing.T) {
	credTypes := []string{"admin_token", "api_key"}
	permissionSets := [][]string{
		nil,
		{},
		{"some:perm"},
		{"keys:read"},
		{"keys:read", "tokens:manage"},
	}
	roles := []string{"admin", "user"}
	resourceTypes := []string{"keys", "tokens", "widgets", "nonexistent"}
	actions := []string{"read", "manage", "export", "unknown"}

	for _, credType := range credTypes {
		for _, perms := range permissionSets {
			for _, role := range roles {
				for _, resType := range resourceTypes {
					for _, action := range actions {
						name := credType + "/" + role + "/" + resType + ":" + action
						t.Run(name, func(t *testing.T) {
							c, _ := newEchoContext()
							setAuthInfo(c, &AuthInfo{
								CredentialType: credType,
								Role:           role,
								UserID:         "uid-test",
								Permissions:    perms,
							})

							err := RequirePermission(c, resType, action)
							if err != nil {
								t.Errorf("RequirePermission(%q, %q) with %q credential returned error: %v; want nil",
									resType, action, credType, err)
							}
						})
					}
				}
			}
		}
	}
}

// TestProperty_NoHandlerOnFailure verifies PROP-7: for any request to
// APIGroup that fails token extraction, parsing, or validation, the next
// handler is never called and AuthInfo is never injected into the context.
//
// Spans all error scenarios: missing header, wrong scheme, empty bearer,
// unrecognized format, wrong prefix, invalid credentials, expired, revoked,
// blocked user.
//
// Test Spec: TS-05-P7
// Requirements: 05-REQ-2.1, 05-REQ-2.2, 05-REQ-2.3, 05-REQ-3.5, 05-REQ-4.5,
//
//	05-REQ-5.1, 05-REQ-6.1
func TestProperty_NoHandlerOnFailure(t *testing.T) {
	// Set up database with various credential states.
	database := openTestDB(t)
	insertUser(t, database, "uid1", "user", "active")
	insertUser(t, database, "blockeduid", "user", "blocked")
	insertAPIKey(t, database, "goodkey", "uid1", hexSHA256("goodsecret"), "", "")
	insertAPIKey(t, database, "revokedkey", "uid1", hexSHA256("revsecret"), "", "2025-01-01T00:00:00Z")
	insertAPIKey(t, database, "expiredkey", "uid1", hexSHA256("expsecret"), "2000-01-01T00:00:00Z", "")
	insertAPIKey(t, database, "blockedkey", "blockeduid", hexSHA256("blocksecret"), "", "")
	insertPATRow(t, database, "revokedpat", "uid1", hexSHA256("patrevsecret"), `["keys:read"]`, "", "2025-01-01T00:00:00Z")
	insertPATRow(t, database, "expiredpat", "uid1", hexSHA256("patexpsecret"), `["keys:read"]`, "2000-01-01T00:00:00Z", "")
	insertPATRow(t, database, "blockedpat", "blockeduid", hexSHA256("patblocksecret"), `["keys:read"]`, "", "")

	// Store wrong admin hash so admin token validation fails.
	insertAdminConfig(t, database, "admin_token_hash", hexSHA256("different_admin_token"))

	scenarios := []struct {
		name    string
		headers map[string]string
	}{
		{name: "missing header", headers: nil},
		{name: "empty header", headers: map[string]string{"Authorization": ""}},
		{name: "basic auth", headers: map[string]string{"Authorization": "Basic dXNlcjpwYXNz"}},
		{name: "bearer no space", headers: map[string]string{"Authorization": "Bearer"}},
		{name: "empty bearer", headers: map[string]string{"Authorization": "Bearer "}},
		{name: "unrecognized format", headers: map[string]string{"Authorization": "Bearer totally_garbage"}},
		{name: "wrong prefix", headers: map[string]string{"Authorization": "Bearer wrong_key_secret"}},
		{name: "invalid admin token", headers: map[string]string{"Authorization": "Bearer ak_admin_" + strings.Repeat("ab", 32)}},
		{name: "api key not found", headers: map[string]string{"Authorization": "Bearer ak_noexist_secret"}},
		{name: "api key wrong secret", headers: map[string]string{"Authorization": "Bearer ak_goodkey_wrongsecret"}},
		{name: "revoked api key", headers: map[string]string{"Authorization": "Bearer ak_revokedkey_revsecret"}},
		{name: "expired api key", headers: map[string]string{"Authorization": "Bearer ak_expiredkey_expsecret"}},
		{name: "blocked user api key", headers: map[string]string{"Authorization": "Bearer ak_blockedkey_blocksecret"}},
		{name: "pat not found", headers: map[string]string{"Authorization": "Bearer ak_pat_noexist_secret"}},
		{name: "revoked pat", headers: map[string]string{"Authorization": "Bearer ak_pat_revokedpat_patrevsecret"}},
		{name: "expired pat", headers: map[string]string{"Authorization": "Bearer ak_pat_expiredpat_patexpsecret"}},
		{name: "blocked user pat", headers: map[string]string{"Authorization": "Bearer ak_pat_blockedpat_patblocksecret"}},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			e := echo.New()
			registry := NewPermissionRegistry()
			mw := NewAuthMiddleware(database, registry)

			handlerCalled := false
			var handlerCtx echo.Context

			handler := func(c echo.Context) error {
				handlerCalled = true
				handlerCtx = c
				return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
			}

			e.GET("/test", handler, mw)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			for k, v := range sc.headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			// Response must be an error (4xx or 5xx).
			if rec.Code == http.StatusOK {
				t.Errorf("expected error response, got HTTP 200")
			}

			// Handler must NOT have been called.
			if handlerCalled {
				t.Errorf("handler was invoked for failing request %q", sc.name)
			}

			// If handler was called, AuthInfo should NOT be in context.
			// (If handler wasn't called, handlerCtx is nil, which is fine.)
			if handlerCtx != nil {
				auth := GetAuthInfo(handlerCtx)
				if auth != nil {
					t.Errorf("AuthInfo was injected into context for failing request %q", sc.name)
				}
			}
		})
	}
}

// ========================================================================
// 5.4 Smoke Tests: End-to-End Execution Paths
// ========================================================================

// TestSMOKE_APIKeyAuth exercises PATH-1: end-to-end API key authentication.
// Client sends valid API key, middleware validates it, injects AuthInfo, and
// the handler reads the context successfully.
//
// Test Spec: TS-05-SMOKE-1
// Requirements: 05-REQ-1.2, 05-REQ-5.6
func TestSMOKE_APIKeyAuth(t *testing.T) {
	database := openTestDB(t)
	insertUser(t, database, "uid-smoke1", "user", "active")
	insertAPIKey(t, database, "smokekey", "uid-smoke1", hexSHA256("smokesecret"), "", "")

	e := echo.New()
	registry := NewPermissionRegistry()
	mw := NewAuthMiddleware(database, registry)

	handlerCallCount := 0
	var capturedAuth *AuthInfo

	handler := func(c echo.Context) error {
		handlerCallCount++
		capturedAuth = GetAuthInfo(c)
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	// Apply middleware to a group simulating APIGroup.
	api := e.Group("/api")
	api.Use(mw)
	api.GET("/resource", handler)

	req := httptest.NewRequest(http.MethodGet, "/api/resource", nil)
	req.Header.Set("Authorization", "Bearer ak_smokekey_smokesecret")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// Response is HTTP 200.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Test handler is invoked exactly once.
	if handlerCallCount != 1 {
		t.Fatalf("handler invoked %d times, want 1", handlerCallCount)
	}

	// GetAuthInfo returns correct AuthInfo.
	if capturedAuth == nil {
		t.Fatal("GetAuthInfo returned nil")
	}
	if capturedAuth.CredentialType != "api_key" {
		t.Errorf("CredentialType = %q, want 'api_key'", capturedAuth.CredentialType)
	}
	if capturedAuth.UserID != "uid-smoke1" {
		t.Errorf("UserID = %q, want 'uid-smoke1'", capturedAuth.UserID)
	}
	if capturedAuth.Role != "user" {
		t.Errorf("Role = %q, want 'user'", capturedAuth.Role)
	}
	if capturedAuth.KeyID != "smokekey" {
		t.Errorf("KeyID = %q, want 'smokekey'", capturedAuth.KeyID)
	}

	// No authentication error returned.
	var resp errorResponse
	if json.Unmarshal(rec.Body.Bytes(), &resp) == nil && resp.Error.Message != "" {
		t.Errorf("unexpected error in response: %q", resp.Error.Message)
	}
}

// TestSMOKE_PATAuthWithPermission exercises PATH-2: end-to-end PAT
// authentication with permission check. Client sends valid PAT, middleware
// validates and injects permissions, handler calls RequirePermission and
// proceeds.
//
// Test Spec: TS-05-SMOKE-2
// Requirements: 05-REQ-6.6, 05-REQ-7.5
func TestSMOKE_PATAuthWithPermission(t *testing.T) {
	database := openTestDB(t)
	insertUser(t, database, "uid-smoke2", "user", "active")
	insertPATRow(t, database, "smoketok", "uid-smoke2", hexSHA256("smokepatsecret"),
		`["keys:read","tokens:manage"]`, "", "")

	e := echo.New()
	registry := NewPermissionRegistry()
	mw := NewAuthMiddleware(database, registry)

	handlerCallCount := 0
	var capturedAuth *AuthInfo
	var permissionErr error

	handler := func(c echo.Context) error {
		handlerCallCount++
		capturedAuth = GetAuthInfo(c)
		// Handler calls RequirePermission for a permission the PAT has.
		permissionErr = RequirePermission(c, "keys", "read")
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	api := e.Group("/api")
	api.Use(mw)
	api.GET("/resource", handler)

	req := httptest.NewRequest(http.MethodGet, "/api/resource", nil)
	req.Header.Set("Authorization", "Bearer ak_pat_smoketok_smokepatsecret")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// Response is HTTP 200.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Test handler is invoked exactly once.
	if handlerCallCount != 1 {
		t.Fatalf("handler invoked %d times, want 1", handlerCallCount)
	}

	// GetAuthInfo returns correct AuthInfo.
	if capturedAuth == nil {
		t.Fatal("GetAuthInfo returned nil")
	}
	if capturedAuth.CredentialType != "pat" {
		t.Errorf("CredentialType = %q, want 'pat'", capturedAuth.CredentialType)
	}

	// Permissions contain the expected permission string.
	foundPerm := false
	for _, p := range capturedAuth.Permissions {
		if p == "keys:read" {
			foundPerm = true
			break
		}
	}
	if !foundPerm {
		t.Errorf("Permissions %v does not contain 'keys:read'", capturedAuth.Permissions)
	}

	// RequirePermission returned nil inside the handler.
	if permissionErr != nil {
		t.Errorf("RequirePermission(keys, read) returned error: %v; want nil", permissionErr)
	}
}

// TestSMOKE_AdminTokenBreakGlass exercises PATH-3: end-to-end admin token
// break-glass authentication. Infrastructure operator sends valid admin token,
// middleware validates hash against admin_config and grants full admin access.
//
// Test Spec: TS-05-SMOKE-3
// Requirements: 05-REQ-4.3, 05-REQ-7.1
func TestSMOKE_AdminTokenBreakGlass(t *testing.T) {
	database := openTestDB(t)

	fullToken := "ak_admin_" + strings.Repeat("cd", 32)
	insertAdminConfig(t, database, "admin_token_hash", hexSHA256(fullToken))

	e := echo.New()
	registry := NewPermissionRegistry()
	mw := NewAuthMiddleware(database, registry)

	handlerCallCount := 0
	var capturedAuth *AuthInfo
	var isAdminResult bool
	var requireAdminErr error

	handler := func(c echo.Context) error {
		handlerCallCount++
		capturedAuth = GetAuthInfo(c)
		isAdminResult = IsAdmin(c)
		requireAdminErr = RequireAdmin(c)
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	api := e.Group("/api")
	api.Use(mw)
	api.GET("/admin-endpoint", handler)

	req := httptest.NewRequest(http.MethodGet, "/api/admin-endpoint", nil)
	req.Header.Set("Authorization", "Bearer "+fullToken)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// Response is HTTP 200.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Test handler is invoked exactly once.
	if handlerCallCount != 1 {
		t.Fatalf("handler invoked %d times, want 1", handlerCallCount)
	}

	// AuthInfo checks.
	if capturedAuth == nil {
		t.Fatal("GetAuthInfo returned nil")
	}
	if capturedAuth.CredentialType != "admin_token" {
		t.Errorf("CredentialType = %q, want 'admin_token'", capturedAuth.CredentialType)
	}
	if capturedAuth.UserID != "" {
		t.Errorf("UserID = %q, want empty string", capturedAuth.UserID)
	}
	if capturedAuth.Role != "admin" {
		t.Errorf("Role = %q, want 'admin'", capturedAuth.Role)
	}

	// IsAdmin returns true.
	if !isAdminResult {
		t.Error("IsAdmin() = false, want true")
	}

	// RequireAdmin returns nil inside the handler.
	if requireAdminErr != nil {
		t.Errorf("RequireAdmin() returned error: %v; want nil", requireAdminErr)
	}
}

// TestSMOKE_MissingHeader exercises PATH-4: end-to-end missing Authorization
// header rejection. Client sends request without Authorization header,
// middleware immediately returns HTTP 401 without invoking the handler.
//
// Test Spec: TS-05-SMOKE-4
// Requirements: 05-REQ-2.1
func TestSMOKE_MissingHeader(t *testing.T) {
	database := openTestDB(t)

	e := echo.New()
	registry := NewPermissionRegistry()
	mw := NewAuthMiddleware(database, registry)

	handlerCalled := false
	handler := func(c echo.Context) error {
		handlerCalled = true
		return c.JSON(http.StatusOK, nil)
	}

	api := e.Group("/api")
	api.Use(mw)
	api.GET("/resource", handler)

	req := httptest.NewRequest(http.MethodGet, "/api/resource", nil)
	// No Authorization header.
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// Response is HTTP 401.
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected HTTP 401, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Response body matches expected format.
	var resp errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v\nbody: %s", err, rec.Body.String())
	}
	if resp.Error.Code != http.StatusUnauthorized {
		t.Errorf("error.code = %d, want %d", resp.Error.Code, http.StatusUnauthorized)
	}
	if resp.Error.Message != "missing authorization header" {
		t.Errorf("error.message = %q, want 'missing authorization header'", resp.Error.Message)
	}

	// Handler is never invoked.
	if handlerCalled {
		t.Error("handler was invoked; middleware should have blocked it")
	}

	// Response Content-Type is application/json.
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q; expected 'application/json'", ct)
	}
}

// TestSMOKE_BlockedUser exercises PATH-5: end-to-end blocked user rejection.
// Client with a valid API key (correct secret, not revoked, not expired)
// belonging to a blocked user receives HTTP 403.
//
// Test Spec: TS-05-SMOKE-5
// Requirements: 05-REQ-5.5, 05-REQ-8.1
func TestSMOKE_BlockedUser(t *testing.T) {
	database := openTestDB(t)
	insertUser(t, database, "uid-blocked-smoke", "user", "blocked")
	insertAPIKey(t, database, "blockedsmoke", "uid-blocked-smoke", hexSHA256("blockedsecret"), "", "")

	e := echo.New()
	registry := NewPermissionRegistry()
	mw := NewAuthMiddleware(database, registry)

	handlerCalled := false
	handler := func(c echo.Context) error {
		handlerCalled = true
		return c.JSON(http.StatusOK, nil)
	}

	api := e.Group("/api")
	api.Use(mw)
	api.GET("/resource", handler)

	req := httptest.NewRequest(http.MethodGet, "/api/resource", nil)
	req.Header.Set("Authorization", "Bearer ak_blockedsmoke_blockedsecret")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// Response is HTTP 403.
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected HTTP 403, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Response body is correct.
	var resp errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v\nbody: %s", err, rec.Body.String())
	}
	if resp.Error.Code != http.StatusForbidden {
		t.Errorf("error.code = %d, want %d", resp.Error.Code, http.StatusForbidden)
	}
	if resp.Error.Message != "user is blocked" {
		t.Errorf("error.message = %q, want 'user is blocked'", resp.Error.Message)
	}

	// Handler is never invoked.
	if handlerCalled {
		t.Error("handler was invoked for blocked user; middleware should have blocked it")
	}
}

// TestSMOKE_CustomPermission exercises PATH-6: end-to-end custom permission
// registration and PAT usage. Consuming project registers a custom permission
// at startup, PAT with that permission passes RequirePermission.
//
// Test Spec: TS-05-SMOKE-6
// Requirements: 05-REQ-9.2, 05-REQ-7.5, 05-REQ-6.6
func TestSMOKE_CustomPermission(t *testing.T) {
	database := openTestDB(t)
	insertUser(t, database, "uid-smoke6", "user", "active")
	insertPATRow(t, database, "customtok", "uid-smoke6", hexSHA256("customsecret"),
		`["widgets:export"]`, "", "")

	// Step 1: Create registry and register custom permission.
	registry := NewPermissionRegistry()

	err := registry.Register("widgets", "export")
	if err != nil {
		t.Fatalf("Register('widgets', 'export') returned error: %v", err)
	}

	// Verify registration.
	if !registry.IsValid("widgets", "export") {
		t.Fatal("IsValid('widgets', 'export') = false after registration")
	}

	// All 6 built-in permissions plus the custom one must be present.
	list := registry.List()
	builtIns := []string{"keys:manage", "keys:read", "orgs:read", "tokens:manage", "tokens:read", "users:read"}
	for _, bi := range builtIns {
		found := false
		for _, p := range list {
			if p == bi {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("built-in permission %q missing from List() after custom registration", bi)
		}
	}

	// Step 2: Set up Echo with middleware using the registry.
	e := echo.New()
	mw := NewAuthMiddleware(database, registry)

	var requirePermErr error

	handler := func(c echo.Context) error {
		// Handler calls RequirePermission with the custom permission.
		requirePermErr = RequirePermission(c, "widgets", "export")
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	api := e.Group("/api")
	api.Use(mw)
	api.GET("/widgets", handler)

	// Step 3: Send request with PAT containing 'widgets:export'.
	req := httptest.NewRequest(http.MethodGet, "/api/widgets", nil)
	req.Header.Set("Authorization", "Bearer ak_pat_customtok_customsecret")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// HTTP request receives HTTP 200.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// RequirePermission returns nil inside the handler.
	if requirePermErr != nil {
		t.Errorf("RequirePermission('widgets', 'export') returned error: %v; want nil", requirePermErr)
	}
}

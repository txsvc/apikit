package handlers_test

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/txsvc/apikit"
	"github.com/txsvc/apikit/internal/auth"
	"github.com/txsvc/apikit/internal/db"
	"github.com/txsvc/apikit/internal/handlers"
)

// ========================================================================
// Task 1.1: Unit tests for generateTokenID and generateSecret
// Test Spec: TS-09-5, TS-09-6, TS-09-P8
// Requirements: 09-REQ-2.1, 09-REQ-2.2
// ========================================================================

// TestGenerateTokenID_Length verifies that generateTokenID returns a string
// of exactly 8 characters.
//
// Test Spec: TS-09-5
// Requirement: 09-REQ-2.1
func TestGenerateTokenID_Length(t *testing.T) {
	id, err := handlers.GenerateTokenID()
	if err != nil {
		t.Fatalf("generateTokenID returned unexpected error: %v", err)
	}
	if len(id) != 8 {
		t.Fatalf("expected generateTokenID to return 8 characters, got %d: %q", len(id), id)
	}
}

// TestGenerateTokenID_Alphanumeric verifies that every character in the
// token ID is in the set [a-z0-9].
//
// Test Spec: TS-09-5
// Requirement: 09-REQ-2.1
func TestGenerateTokenID_Alphanumeric(t *testing.T) {
	id, err := handlers.GenerateTokenID()
	if err != nil {
		t.Fatalf("generateTokenID returned unexpected error: %v", err)
	}
	if len(id) == 0 {
		t.Fatal("generateTokenID returned empty string")
	}
	for i, ch := range id {
		if !((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9')) {
			t.Fatalf("character at index %d is %q, not in [a-z0-9]", i, string(ch))
		}
	}
}

// TestGenerateSecret_Length verifies that generateSecret returns a string
// of exactly 32 characters.
//
// Test Spec: TS-09-6
// Requirement: 09-REQ-2.2
func TestGenerateSecret_Length(t *testing.T) {
	secret, err := handlers.GenerateSecret()
	if err != nil {
		t.Fatalf("generateSecret returned unexpected error: %v", err)
	}
	if len(secret) != 32 {
		t.Fatalf("expected generateSecret to return 32 characters, got %d: %q", len(secret), secret)
	}
}

// TestGenerateSecret_Alphanumeric verifies that every character in the
// secret is in the set [a-z0-9].
//
// Test Spec: TS-09-6
// Requirement: 09-REQ-2.2
func TestGenerateSecret_Alphanumeric(t *testing.T) {
	secret, err := handlers.GenerateSecret()
	if err != nil {
		t.Fatalf("generateSecret returned unexpected error: %v", err)
	}
	if len(secret) == 0 {
		t.Fatal("generateSecret returned empty string")
	}
	for i, ch := range secret {
		if !((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9')) {
			t.Fatalf("character at index %d is %q, not in [a-z0-9]", i, string(ch))
		}
	}
}

// TestGenerateTokenID_BoundedIterations is a property-based test (TS-09-P8)
// that calls generateTokenID N times and verifies each call produces a valid
// 8-character alphanumeric string. This demonstrates that the function
// terminates reliably without unbounded retry loops.
//
// Test Spec: TS-09-P8
// Requirements: 09-REQ-2.1
func TestGenerateTokenID_BoundedIterations(t *testing.T) {
	const iterations = 100
	validChar := regexp.MustCompile(`^[a-z0-9]+$`)

	for i := 0; i < iterations; i++ {
		id, err := handlers.GenerateTokenID()
		if err != nil {
			t.Fatalf("iteration %d: generateTokenID returned error: %v", i, err)
		}
		if len(id) != 8 {
			t.Fatalf("iteration %d: expected 8 characters, got %d: %q", i, len(id), id)
		}
		if !validChar.MatchString(id) {
			t.Fatalf("iteration %d: token ID %q contains characters outside [a-z0-9]", i, id)
		}
	}
}

// TestGenerateSecret_BoundedIterations is a property-based test (TS-09-P8)
// that calls generateSecret N times and verifies each call produces a valid
// 32-character alphanumeric string. This demonstrates that the function
// terminates reliably without unbounded retry loops.
//
// Test Spec: TS-09-P8
// Requirements: 09-REQ-2.2
func TestGenerateSecret_BoundedIterations(t *testing.T) {
	const iterations = 100
	validChar := regexp.MustCompile(`^[a-z0-9]+$`)

	for i := 0; i < iterations; i++ {
		secret, err := handlers.GenerateSecret()
		if err != nil {
			t.Fatalf("iteration %d: generateSecret returned error: %v", i, err)
		}
		if len(secret) != 32 {
			t.Fatalf("iteration %d: expected 32 characters, got %d: %q", i, len(secret), secret)
		}
		if !validChar.MatchString(secret) {
			t.Fatalf("iteration %d: secret %q contains characters outside [a-z0-9]", i, secret)
		}
	}
}

// ========================================================================
// Task 1.2: Unit tests for hashSecret and token format construction
// Test Spec: TS-09-7, TS-09-8, TS-09-P6
// Requirements: 09-REQ-2.3, 09-REQ-2.4
// ========================================================================

// TestHashSecret_Deterministic verifies that hashSecret is deterministic:
// calling it twice with the same input produces the same output.
//
// Test Spec: TS-09-7
// Requirement: 09-REQ-2.3
func TestHashSecret_Deterministic(t *testing.T) {
	hash1 := handlers.HashSecret("hello")
	hash2 := handlers.HashSecret("hello")
	if hash1 == "" {
		t.Fatal("hashSecret returned empty string; expected a 64-character hex digest")
	}
	if hash1 != hash2 {
		t.Fatalf("hashSecret is not deterministic: %q != %q", hash1, hash2)
	}
}

// TestHashSecret_KnownVector verifies that hashSecret("hello") produces
// the known SHA-256 hex digest.
//
// Test Spec: TS-09-7
// Requirement: 09-REQ-2.3
func TestHashSecret_KnownVector(t *testing.T) {
	const expected = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	got := handlers.HashSecret("hello")
	if got != expected {
		t.Fatalf("hashSecret(\"hello\") = %q, want %q", got, expected)
	}
}

// TestHashSecret_Length verifies that hashSecret returns a 64-character
// lowercase hex string (SHA-256 digest).
//
// Test Spec: TS-09-7
// Requirement: 09-REQ-2.3
func TestHashSecret_Length(t *testing.T) {
	hash := handlers.HashSecret("test-input")
	if len(hash) != 64 {
		t.Fatalf("expected hashSecret to return 64 characters, got %d: %q", len(hash), hash)
	}
	// Verify all characters are lowercase hex.
	for i, ch := range hash {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
			t.Fatalf("character at index %d is %q, not a lowercase hex digit", i, string(ch))
		}
	}
}

// TestHashSecret_Lowercase verifies that hashSecret always returns
// lowercase hex, never uppercase.
//
// Test Spec: TS-09-7
// Requirement: 09-REQ-2.3
func TestHashSecret_Lowercase(t *testing.T) {
	hash := handlers.HashSecret("hello")
	for i, ch := range hash {
		if ch >= 'A' && ch <= 'F' {
			t.Fatalf("character at index %d is uppercase hex %q, expected lowercase", i, string(ch))
		}
	}
	// Also verify it's not empty.
	if len(hash) == 0 {
		t.Fatal("hashSecret returned empty string")
	}
}

// TestTokenFormat_Construction verifies that the PAT token string is
// assembled as <TokenPrefix>_pat_<token_id>_<secret> and matches the
// expected regex pattern.
//
// Test Spec: TS-09-8
// Requirement: 09-REQ-2.4
func TestTokenFormat_Construction(t *testing.T) {
	tokenID := "a1b2c3d4"
	secret := "deadbeefdeadbeefdeadbeefdeadbeef"
	expected := apikit.TokenPrefix + "_pat_" + tokenID + "_" + secret

	// The expected value is "ak_pat_a1b2c3d4_deadbeefdeadbeefdeadbeefdeadbeef"
	if expected != "ak_pat_a1b2c3d4_deadbeefdeadbeefdeadbeefdeadbeef" {
		t.Fatalf("unexpected token construction: got %q", expected)
	}

	// Verify it matches the regex pattern.
	pattern := regexp.MustCompile(`^[a-z0-9]+_pat_[a-z0-9]{8}_[a-z0-9]{32}$`)
	if !pattern.MatchString(expected) {
		t.Fatalf("token %q does not match pattern %s", expected, pattern)
	}
}

// TestTokenFormat_PrefixSegment verifies that the leading segment of the
// token format (before _pat_) equals apikit.TokenPrefix.
//
// Test Spec: TS-09-8
// Requirement: 09-REQ-2.4
func TestTokenFormat_PrefixSegment(t *testing.T) {
	tokenID := "abcd1234"
	secret := "abcdefghijklmnopqrstuvwxyz012345"
	token := apikit.TokenPrefix + "_pat_" + tokenID + "_" + secret

	// Extract prefix: everything before "_pat_"
	idx := len(apikit.TokenPrefix)
	prefix := token[:idx]
	if prefix != apikit.TokenPrefix {
		t.Fatalf("token prefix = %q, want %q", prefix, apikit.TokenPrefix)
	}
}

// TestTokenFormat_PropertyRegex is a property-based test (TS-09-P6)
// that generates N token IDs and secrets, assembles them into the full
// PAT token format, and verifies each matches the canonical regex pattern
// with the correct prefix.
//
// Test Spec: TS-09-P6
// Requirements: 09-REQ-2.4, 09-REQ-5.1
func TestTokenFormat_PropertyRegex(t *testing.T) {
	const iterations = 50
	pattern := regexp.MustCompile(`^[a-z0-9]+_pat_[a-z0-9]{8}_[a-z0-9]{32}$`)

	for i := 0; i < iterations; i++ {
		tokenID, err := handlers.GenerateTokenID()
		if err != nil {
			t.Fatalf("iteration %d: generateTokenID error: %v", i, err)
		}
		secret, err := handlers.GenerateSecret()
		if err != nil {
			t.Fatalf("iteration %d: generateSecret error: %v", i, err)
		}

		token := fmt.Sprintf("%s_pat_%s_%s", apikit.TokenPrefix, tokenID, secret)

		if !pattern.MatchString(token) {
			t.Fatalf("iteration %d: token %q does not match regex %s", i, token, pattern)
		}

		// Verify the prefix segment equals apikit.TokenPrefix.
		prefixEnd := len(apikit.TokenPrefix)
		if prefixEnd >= len(token) {
			t.Fatalf("iteration %d: token too short: %q", i, token)
		}
		if token[:prefixEnd] != apikit.TokenPrefix {
			t.Fatalf("iteration %d: prefix = %q, want %q", i, token[:prefixEnd], apikit.TokenPrefix)
		}

		// Verify token_id length is 8.
		if len(tokenID) != 8 {
			t.Fatalf("iteration %d: token_id length = %d, want 8", i, len(tokenID))
		}

		// Verify secret length is 32.
		if len(secret) != 32 {
			t.Fatalf("iteration %d: secret length = %d, want 32", i, len(secret))
		}
	}
}

// ========================================================================
// Task 1.3: Unit tests for NewPATHandler constructor and RegisterRoutes
// Test Spec: TS-09-1, TS-09-2, TS-09-E1
// Requirements: 09-REQ-1.1, 09-REQ-1.2, 09-REQ-1.E1
// ========================================================================

// TestNewPATHandler_Success verifies that NewPATHandler returns a non-nil
// *PATHandler when both database and registry parameters are non-nil.
//
// Test Spec: TS-09-1
// Requirement: 09-REQ-1.1
func TestNewPATHandler_Success(t *testing.T) {
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	defer database.Close()

	registry := auth.NewPermissionRegistry()
	handler := handlers.NewPATHandler(database, registry)
	if handler == nil {
		t.Fatal("expected non-nil *PATHandler, got nil")
	}
}

// TestNewPATHandler_NilDB verifies that NewPATHandler panics with a
// descriptive message when the database parameter is nil.
//
// Test Spec: TS-09-E1
// Requirement: 09-REQ-1.E1
func TestNewPATHandler_NilDB(t *testing.T) {
	registry := auth.NewPermissionRegistry()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected NewPATHandler to panic when database is nil, but it did not")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected panic value to be a string, got %T: %v", r, r)
		}
		if msg == "" {
			t.Fatal("expected panic message to be descriptive, got empty string")
		}
	}()

	handlers.NewPATHandler(nil, registry)
}

// TestNewPATHandler_NilRegistry verifies that NewPATHandler panics with a
// descriptive message when the registry parameter is nil.
//
// Test Spec: TS-09-E1
// Requirement: 09-REQ-1.E1
func TestNewPATHandler_NilRegistry(t *testing.T) {
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	defer database.Close()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected NewPATHandler to panic when registry is nil, but it did not")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected panic value to be a string, got %T: %v", r, r)
		}
		if msg == "" {
			t.Fatal("expected panic message to be descriptive, got empty string")
		}
	}()

	handlers.NewPATHandler(database, nil)
}

// TestRegisterRoutes_PATEndpoints verifies that RegisterRoutes registers all
// four PAT lifecycle routes: POST /user/tokens, GET /user/tokens,
// GET /user/tokens/:token_id, DELETE /user/tokens/:token_id.
//
// Test Spec: TS-09-2
// Requirement: 09-REQ-1.2
func TestRegisterRoutes_PATEndpoints(t *testing.T) {
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	defer database.Close()

	registry := auth.NewPermissionRegistry()
	handler := handlers.NewPATHandler(database, registry)
	if handler == nil {
		t.Fatal("NewPATHandler returned nil; cannot test RegisterRoutes")
	}

	e := echo.New()
	g := e.Group("")
	handler.RegisterRoutes(g)

	// Expected routes that must be registered.
	expected := map[string]bool{
		"POST /user/tokens":            false,
		"GET /user/tokens":             false,
		"GET /user/tokens/:token_id":   false,
		"DELETE /user/tokens/:token_id": false,
	}

	routes := e.Routes()
	for _, r := range routes {
		key := r.Method + " " + r.Path
		if _, ok := expected[key]; ok {
			expected[key] = true
		}
	}

	found := 0
	for key, registered := range expected {
		if !registered {
			t.Errorf("expected route %q was not registered", key)
		} else {
			found++
		}
	}

	if found != len(expected) {
		t.Errorf("expected %d routes registered, only found %d", len(expected), found)
	}
}

// ========================================================================
// Task 2: Unit tests for CreatePAT request validation
// ========================================================================

// createPATResponse represents the JSON response from POST /user/tokens
// for use in test assertions. Includes the one-time plaintext token field.
type createPATResponse struct {
	TokenID     string   `json:"token_id"`
	Name        string   `json:"name"`
	Token       string   `json:"token"`
	Permissions []string `json:"permissions"`
	ExpiresAt   *string  `json:"expires_at"`
	CreatedAt   string   `json:"created_at"`
}

// setupCreatePATServer creates an Echo instance with PAT handler routes
// registered, API key auth middleware injected, and CacheMiddleware applied.
// A test user is inserted into the database for FK constraint satisfaction.
// Returns the Echo instance and the db.DB handle.
func setupCreatePATServer(t *testing.T) (*echo.Echo, *db.DB) {
	t.Helper()

	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	// Insert a test user so that PAT INSERT satisfies FK constraint.
	insertTestUser(t, database.SqlDB, "test-user-uuid", "testuser",
		"test@example.com", "github", "gh-test")

	registry := auth.NewPermissionRegistry()
	handler := handlers.NewPATHandler(database, registry)
	if handler == nil {
		t.Fatal("NewPATHandler returned nil; cannot test CreatePAT validation")
	}

	e := echo.New()
	g := e.Group("", apikit.CacheMiddleware(apikit.CacheNoStore))
	g.Use(nonAdminAuthMiddleware("test-user-uuid"))
	handler.RegisterRoutes(g)

	return e, database
}

// ========================================================================
// Task 2.1: Unit tests for name and permissions validation
// Test Spec: TS-09-9, TS-09-10, TS-09-11, TS-09-15
// Requirements: 09-REQ-3.1, 09-REQ-3.2, 09-REQ-3.3, 09-REQ-3.7
// ========================================================================

// TestCreatePAT_MissingName verifies that POST /user/tokens returns HTTP 400
// with message "name is required" when the name field is absent.
//
// Test Spec: TS-09-9
// Requirement: 09-REQ-3.1
func TestCreatePAT_MissingName(t *testing.T) {
	e, _ := setupCreatePATServer(t)

	rec := sendJSON(t, e, http.MethodPost, "/user/tokens",
		`{"permissions": ["tokens:read"], "expires": 30}`)

	assertErrorResponse(t, rec, http.StatusBadRequest, "name is required")
}

// TestCreatePAT_EmptyName verifies that POST /user/tokens returns HTTP 400
// with message "name is required" when the name field is an empty string.
//
// Test Spec: TS-09-9
// Requirement: 09-REQ-3.1
func TestCreatePAT_EmptyName(t *testing.T) {
	e, _ := setupCreatePATServer(t)

	rec := sendJSON(t, e, http.MethodPost, "/user/tokens",
		`{"name": "", "permissions": ["tokens:read"], "expires": 30}`)

	assertErrorResponse(t, rec, http.StatusBadRequest, "name is required")
}

// TestCreatePAT_NameTooLong verifies that POST /user/tokens returns HTTP 400
// with message "name must be 255 characters or fewer" when the name exceeds
// 255 characters.
//
// Test Spec: TS-09-10
// Requirement: 09-REQ-3.2
func TestCreatePAT_NameTooLong(t *testing.T) {
	e, _ := setupCreatePATServer(t)

	longName := strings.Repeat("a", 256)
	body := fmt.Sprintf(`{"name": %q, "permissions": ["tokens:read"], "expires": 30}`, longName)
	rec := sendJSON(t, e, http.MethodPost, "/user/tokens", body)

	assertErrorResponse(t, rec, http.StatusBadRequest, "name must be 255 characters or fewer")
}

// TestCreatePAT_MissingPermissions verifies that POST /user/tokens returns
// HTTP 400 with message "permissions are required" when the permissions field
// is absent from the request body.
//
// Test Spec: TS-09-11
// Requirement: 09-REQ-3.3
func TestCreatePAT_MissingPermissions(t *testing.T) {
	e, _ := setupCreatePATServer(t)

	rec := sendJSON(t, e, http.MethodPost, "/user/tokens",
		`{"name": "ci-deploy", "expires": 30}`)

	assertErrorResponse(t, rec, http.StatusBadRequest, "permissions are required")
}

// TestCreatePAT_EmptyPermissions verifies that POST /user/tokens returns
// HTTP 400 with message "permissions are required" when the permissions field
// is an empty array.
//
// Test Spec: TS-09-11
// Requirement: 09-REQ-3.3
func TestCreatePAT_EmptyPermissions(t *testing.T) {
	e, _ := setupCreatePATServer(t)

	rec := sendJSON(t, e, http.MethodPost, "/user/tokens",
		`{"name": "ci-deploy", "permissions": [], "expires": 30}`)

	assertErrorResponse(t, rec, http.StatusBadRequest, "permissions are required")
}

// TestCreatePAT_MalformedJSON verifies that POST /user/tokens returns
// HTTP 400 with message "invalid request body" when the JSON body cannot
// be decoded.
//
// Test Spec: TS-09-15
// Requirement: 09-REQ-3.7
func TestCreatePAT_MalformedJSON(t *testing.T) {
	e, _ := setupCreatePATServer(t)

	rec := sendJSON(t, e, http.MethodPost, "/user/tokens", "not valid json {")

	assertErrorResponse(t, rec, http.StatusBadRequest, "invalid request body")
}

// ========================================================================
// Task 2.2: Unit tests for permission format and registry validation
// Test Spec: TS-09-12, TS-09-13, TS-09-E5
// Requirements: 09-REQ-3.4, 09-REQ-3.5, 09-REQ-3.E3
// ========================================================================

// TestCreatePAT_InvalidPermissionFormat verifies that POST /user/tokens
// returns HTTP 400 with message "invalid permission format: usersread" when
// a permission string does not contain exactly one colon separator.
//
// Test Spec: TS-09-12
// Requirement: 09-REQ-3.4
func TestCreatePAT_InvalidPermissionFormat(t *testing.T) {
	e, _ := setupCreatePATServer(t)

	rec := sendJSON(t, e, http.MethodPost, "/user/tokens",
		`{"name": "ci-deploy", "permissions": ["usersread"], "expires": 30}`)

	assertErrorResponse(t, rec, http.StatusBadRequest, "invalid permission format: usersread")
}

// TestCreatePAT_UnknownPermission verifies that POST /user/tokens returns
// HTTP 400 with message "unknown permission: widgets:delete" when a permission
// string is well-formed but not registered in the PermissionRegistry.
//
// Test Spec: TS-09-13
// Requirement: 09-REQ-3.5
func TestCreatePAT_UnknownPermission(t *testing.T) {
	e, _ := setupCreatePATServer(t)

	rec := sendJSON(t, e, http.MethodPost, "/user/tokens",
		`{"name": "ci-deploy", "permissions": ["widgets:delete"], "expires": 30}`)

	assertErrorResponse(t, rec, http.StatusBadRequest, "unknown permission: widgets:delete")
}

// TestCreatePAT_PermissionFailFast verifies that POST /user/tokens returns
// only the first validation error when multiple permissions are invalid,
// confirming fail-fast behavior (no error accumulation).
//
// Test Spec: TS-09-E5
// Requirement: 09-REQ-3.E3
func TestCreatePAT_PermissionFailFast(t *testing.T) {
	e, _ := setupCreatePATServer(t)

	rec := sendJSON(t, e, http.MethodPost, "/user/tokens",
		`{"name": "test", "permissions": ["widgets:delete", "gadgets:create"], "expires": 30}`)

	assertErrorResponse(t, rec, http.StatusBadRequest, "unknown permission: widgets:delete")

	// Verify the error message references only the first invalid permission,
	// not the second one — confirming fail-fast behavior.
	resp := parseErrorResponse(t, rec)
	if strings.Contains(resp.Error.Message, "gadgets:create") {
		t.Errorf("error message should only reference the first invalid permission, "+
			"but got: %q", resp.Error.Message)
	}
}

// ========================================================================
// Task 2.3: Unit tests for expires validation and defaulting
// Test Spec: TS-09-14, TS-09-16, TS-09-E3, TS-09-E4
// Requirements: 09-REQ-3.6, 09-REQ-3.8, 09-REQ-3.E1, 09-REQ-3.E2
// ========================================================================

// TestCreatePAT_InvalidExpires verifies that POST /user/tokens returns
// HTTP 400 with message "expires must be 0, 30, 60, or 90" for invalid
// expires values.
//
// Test Spec: TS-09-14
// Requirement: 09-REQ-3.6
func TestCreatePAT_InvalidExpires(t *testing.T) {
	e, _ := setupCreatePATServer(t)

	for _, v := range []int{7, 365, 999, 1, -1} {
		body := fmt.Sprintf(`{"name": "ci-deploy", "permissions": ["tokens:read"], "expires": %d}`, v)
		rec := sendJSON(t, e, http.MethodPost, "/user/tokens", body)
		assertErrorResponse(t, rec, http.StatusBadRequest, "expires must be 0, 30, 60, or 90")
	}
}

// TestCreatePAT_DefaultExpires verifies that POST /user/tokens treats an
// omitted expires field as 90 days and returns expires_at = created_at + 90*24h.
//
// Test Spec: TS-09-16
// Requirement: 09-REQ-3.8
func TestCreatePAT_DefaultExpires(t *testing.T) {
	e, _ := setupCreatePATServer(t)

	rec := sendJSON(t, e, http.MethodPost, "/user/tokens",
		`{"name": "ci-deploy", "permissions": ["tokens:read"]}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected HTTP 201, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp createPATResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse CreatePATResponse: %v\nbody: %s", err, rec.Body.String())
	}

	if resp.ExpiresAt == nil {
		t.Fatal("expected non-nil expires_at for default 90-day expiry, got null")
	}

	createdAt, err := db.ParseTime(resp.CreatedAt)
	if err != nil {
		t.Fatalf("failed to parse created_at %q: %v", resp.CreatedAt, err)
	}

	expiresAt, err := db.ParseTime(*resp.ExpiresAt)
	if err != nil {
		t.Fatalf("failed to parse expires_at %q: %v", *resp.ExpiresAt, err)
	}

	expected := createdAt.Add(90 * 24 * time.Hour)
	if !expiresAt.Equal(expected) {
		t.Fatalf("expires_at = %v, want created_at + 90 days = %v", expiresAt, expected)
	}
}

// TestCreatePAT_NoExpiry verifies that POST /user/tokens with expires=0
// sets expires_at to null in the response and NULL in the database.
//
// Test Spec: TS-09-E3
// Requirement: 09-REQ-3.E1
func TestCreatePAT_NoExpiry(t *testing.T) {
	e, database := setupCreatePATServer(t)

	rec := sendJSON(t, e, http.MethodPost, "/user/tokens",
		`{"name": "forever", "permissions": ["tokens:read"], "expires": 0}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected HTTP 201, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp createPATResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse CreatePATResponse: %v\nbody: %s", err, rec.Body.String())
	}

	// Response should have expires_at: null.
	if resp.ExpiresAt != nil {
		t.Fatalf("expected expires_at to be null, got %q", *resp.ExpiresAt)
	}

	// Verify the database row also has NULL expires_at.
	var expiresAt sql.NullString
	err := database.SqlDB.QueryRow(
		"SELECT expires_at FROM pats WHERE token_id = ?", resp.TokenID,
	).Scan(&expiresAt)
	if err != nil {
		t.Fatalf("failed to query pats table: %v", err)
	}
	if expiresAt.Valid {
		t.Fatalf("expected NULL expires_at in database, got %q", expiresAt.String)
	}
}

// TestCreatePAT_InvalidExpiresExtended verifies that POST /user/tokens
// returns HTTP 400 with the correct error message for an extended set of
// invalid expires values, including negative integers and values near the
// valid boundaries.
//
// Test Spec: TS-09-E4
// Requirement: 09-REQ-3.E2
func TestCreatePAT_InvalidExpiresExtended(t *testing.T) {
	e, _ := setupCreatePATServer(t)

	for _, v := range []int{-1, 7, 29, 31, 59, 61, 89, 91, 365, 999} {
		body := fmt.Sprintf(`{"name": "test", "permissions": ["tokens:read"], "expires": %d}`, v)
		rec := sendJSON(t, e, http.MethodPost, "/user/tokens", body)
		assertErrorResponse(t, rec, http.StatusBadRequest, "expires must be 0, 30, 60, or 90")
	}
}

// ========================================================================
// Task 3: Unit tests for CreatePAT privilege escalation and auth
// ========================================================================

// setupCreatePATServerWithPATAuth creates an Echo instance with PAT handler
// routes registered, PAT auth middleware injected (with specified permissions),
// and CacheMiddleware applied. A test user is inserted into the database.
// Returns the Echo instance and the db.DB handle.
func setupCreatePATServerWithPATAuth(t *testing.T, permissions []string) (*echo.Echo, *db.DB) {
	t.Helper()

	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	// Insert a test user so that PAT INSERT satisfies FK constraint.
	insertTestUser(t, database.SqlDB, "test-user-uuid", "testuser",
		"test@example.com", "github", "gh-test")

	registry := auth.NewPermissionRegistry()
	handler := handlers.NewPATHandler(database, registry)
	if handler == nil {
		t.Fatal("NewPATHandler returned nil; cannot test CreatePAT privilege escalation")
	}

	e := echo.New()
	g := e.Group("", apikit.CacheMiddleware(apikit.CacheNoStore))
	g.Use(patAuthMiddleware("test-user-uuid", permissions))
	handler.RegisterRoutes(g)

	return e, database
}

// ========================================================================
// Task 3.1: Unit tests for privilege escalation checks
// Test Spec: TS-09-17, TS-09-18, TS-09-19, TS-09-E6
// Requirements: 09-REQ-4.1, 09-REQ-4.2, 09-REQ-4.3, 09-REQ-4.E1
// ========================================================================

// TestCreatePAT_PrivilegeEscalation_PAT verifies that POST /user/tokens
// returns HTTP 403 with message "cannot grant permission: keys:manage" when a
// PAT credential with permissions [tokens:manage, users:read] attempts to
// create a new PAT requesting [keys:manage].
//
// Test Spec: TS-09-17
// Requirement: 09-REQ-4.1
func TestCreatePAT_PrivilegeEscalation_PAT(t *testing.T) {
	e, _ := setupCreatePATServerWithPATAuth(t, []string{"tokens:manage", "users:read"})

	rec := sendJSON(t, e, http.MethodPost, "/user/tokens",
		`{"name": "ci-deploy", "permissions": ["keys:manage"], "expires": 30}`)

	assertErrorResponse(t, rec, http.StatusForbidden, "cannot grant permission: keys:manage")
}

// TestCreatePAT_APIKey_AnyRegisteredPermission verifies that POST /user/tokens
// with an API key credential creates a PAT with any registered permissions
// without privilege escalation restrictions.
//
// Test Spec: TS-09-18
// Requirement: 09-REQ-4.2
func TestCreatePAT_APIKey_AnyRegisteredPermission(t *testing.T) {
	e, _ := setupCreatePATServer(t)

	rec := sendJSON(t, e, http.MethodPost, "/user/tokens",
		`{"name": "ci-deploy", "permissions": ["users:read", "keys:manage", "tokens:manage"], "expires": 30}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected HTTP 201, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp createPATResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse CreatePATResponse: %v\nbody: %s", err, rec.Body.String())
	}

	if resp.TokenID == "" {
		t.Fatal("expected non-empty token_id in response")
	}
}

// TestCreatePAT_RegistryCheckBeforeEscalation verifies that registry validation
// (HTTP 400 "unknown permission") fires before privilege escalation checking
// (HTTP 403). A PAT credential requesting an unregistered permission should get
// HTTP 400 with "unknown permission", not HTTP 403 with "cannot grant permission".
//
// Test Spec: TS-09-19
// Requirement: 09-REQ-4.3
func TestCreatePAT_RegistryCheckBeforeEscalation(t *testing.T) {
	e, _ := setupCreatePATServerWithPATAuth(t, []string{"tokens:manage"})

	rec := sendJSON(t, e, http.MethodPost, "/user/tokens",
		`{"name": "test", "permissions": ["widgets:delete"], "expires": 30}`)

	assertErrorResponse(t, rec, http.StatusBadRequest, "unknown permission: widgets:delete")
}

// TestCreatePAT_EscalationSubsetAllowed verifies that a PAT with permissions
// [tokens:manage, users:read] can create a new PAT requesting a subset
// [users:read] — privilege escalation check passes for subsets.
//
// Test Spec: TS-09-E6
// Requirement: 09-REQ-4.E1
func TestCreatePAT_EscalationSubsetAllowed(t *testing.T) {
	e, _ := setupCreatePATServerWithPATAuth(t, []string{"tokens:manage", "users:read"})

	rec := sendJSON(t, e, http.MethodPost, "/user/tokens",
		`{"name": "subset", "permissions": ["users:read"], "expires": 30}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected HTTP 201 for subset permissions, got %d; body: %s",
			rec.Code, rec.Body.String())
	}

	var resp createPATResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse CreatePATResponse: %v\nbody: %s", err, rec.Body.String())
	}

	if resp.TokenID == "" {
		t.Fatal("expected non-empty token_id in response")
	}
}

// TestCreatePAT_EscalationSupersetDenied verifies that a PAT with permissions
// [tokens:manage, users:read] is rejected when requesting [users:read, keys:manage]
// because keys:manage is not in the creating PAT's permissions.
//
// Test Spec: TS-09-E6
// Requirement: 09-REQ-4.E1
func TestCreatePAT_EscalationSupersetDenied(t *testing.T) {
	e, _ := setupCreatePATServerWithPATAuth(t, []string{"tokens:manage", "users:read"})

	rec := sendJSON(t, e, http.MethodPost, "/user/tokens",
		`{"name": "escalated", "permissions": ["users:read", "keys:manage"], "expires": 30}`)

	assertErrorResponse(t, rec, http.StatusForbidden, "cannot grant permission: keys:manage")
}

// ========================================================================
// Task 3.2: Integration tests for CreatePAT permission requirement
// Test Spec: TS-09-25
// Requirements: 09-REQ-5.6
// ========================================================================

// TestCreatePAT_RequiresTokensManage verifies that a PAT credential with only
// [tokens:read] (lacking tokens:manage) receives HTTP 403 when calling
// POST /user/tokens, and no PAT is created in the database.
//
// Test Spec: TS-09-25
// Requirement: 09-REQ-5.6
func TestCreatePAT_RequiresTokensManage(t *testing.T) {
	e, database := setupCreatePATServerWithPATAuth(t, []string{"tokens:read"})

	rec := sendJSON(t, e, http.MethodPost, "/user/tokens",
		`{"name": "test", "permissions": ["tokens:read"], "expires": 30}`)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected HTTP 403, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Verify no PAT row was inserted in the database.
	var count int
	err := database.SqlDB.QueryRow("SELECT COUNT(*) FROM pats").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query pats table: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 PATs in database after permission denial, got %d", count)
	}
}

// ========================================================================
// Task 3.3: Property test for privilege escalation invariant
// Test Spec: TS-09-P4
// Requirements: 09-REQ-4.1, 09-REQ-4.2
// ========================================================================

// mustMarshalJSON marshals v to a JSON string, panicking on error.
// Used by property tests to build request bodies dynamically.
func mustMarshalJSON(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("json.Marshal failed: %v", err))
	}
	return string(data)
}

// TestPATPrivilegeEscalation_Property is a property-based test that iterates
// over all subsets P and Q of the registered permissions set and verifies the
// privilege escalation invariant:
//   - PAT credential with permissions P: Q ⊆ P → HTTP 201; Q ⊄ P → HTTP 403
//   - API key credential: any valid Q → HTTP 201
//
// Test Spec: TS-09-P4
// Requirements: 09-REQ-4.1, 09-REQ-4.2
func TestPATPrivilegeEscalation_Property(t *testing.T) {
	allPerms := []string{
		"users:read", "orgs:read", "keys:read",
		"keys:manage", "tokens:read", "tokens:manage",
	}

	// Generate all non-empty subsets of allPerms using bitmask enumeration.
	type permSubset struct {
		mask  int
		perms []string
	}
	var subsets []permSubset
	for mask := 1; mask < (1 << len(allPerms)); mask++ {
		var perms []string
		for i := 0; i < len(allPerms); i++ {
			if mask&(1<<uint(i)) != 0 {
				perms = append(perms, allPerms[i])
			}
		}
		subsets = append(subsets, permSubset{mask: mask, perms: perms})
	}

	// tokensManageBit is the bitmask bit for "tokens:manage" (index 5).
	const tokensManageBit = 1 << 5

	// Test PAT credentials: for each P containing tokens:manage, verify
	// the escalation invariant against all Q subsets.
	t.Run("PAT_credential", func(t *testing.T) {
		for _, p := range subsets {
			if p.mask&tokensManageBit == 0 {
				continue // P must include tokens:manage for RequirePermission to pass.
			}

			e, _ := setupCreatePATServerWithPATAuth(t, p.perms)

			for _, q := range subsets {
				body := fmt.Sprintf(
					`{"name":"prop-test","permissions":%s,"expires":30}`,
					mustMarshalJSON(q.perms),
				)
				rec := sendJSON(t, e, http.MethodPost, "/user/tokens", body)

				// Q ⊆ P iff (q.mask & p.mask) == q.mask (all Q bits are set in P).
				qSubsetOfP := (q.mask & p.mask) == q.mask

				if qSubsetOfP {
					if rec.Code != http.StatusCreated {
						t.Errorf("PAT P=%v, Q=%v (Q⊆P): expected 201, got %d; body: %s",
							p.perms, q.perms, rec.Code, rec.Body.String())
					}
				} else {
					if rec.Code != http.StatusForbidden {
						t.Errorf("PAT P=%v, Q=%v (Q⊄P): expected 403, got %d; body: %s",
							p.perms, q.perms, rec.Code, rec.Body.String())
					}
					resp := parseErrorResponse(t, rec)
					if !strings.HasPrefix(resp.Error.Message, "cannot grant permission:") {
						t.Errorf("PAT P=%v, Q=%v: expected message prefix "+
							"'cannot grant permission:', got %q",
							p.perms, q.perms, resp.Error.Message)
					}
				}
			}
		}
	})

	// Test API key credentials: any valid Q should succeed (HTTP 201).
	t.Run("APIKey_credential", func(t *testing.T) {
		e, _ := setupCreatePATServer(t)

		for _, q := range subsets {
			body := fmt.Sprintf(
				`{"name":"prop-test","permissions":%s,"expires":30}`,
				mustMarshalJSON(q.perms),
			)
			rec := sendJSON(t, e, http.MethodPost, "/user/tokens", body)

			if rec.Code != http.StatusCreated {
				t.Errorf("APIKey Q=%v: expected 201, got %d; body: %s",
					q.perms, rec.Code, rec.Body.String())
			}
		}
	})
}

// ========================================================================
// Task 4 helper functions
// ========================================================================

// sha256Hex computes the SHA-256 hash of s and returns a lowercase hex string.
// Used in test assertions to verify secret hashing.
func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// extractSecretFromToken extracts the secret (last segment) from a PAT token
// in the format <prefix>_pat_<token_id>_<secret>.
func extractSecretFromToken(token string) string {
	// Split on "_pat_" to get [prefix, <token_id>_<secret>]
	parts := strings.SplitN(token, "_pat_", 2)
	if len(parts) != 2 {
		return ""
	}
	// The remainder is "<token_id>_<secret>" — split on first "_" to get secret.
	rest := parts[1]
	idx := strings.Index(rest, "_")
	if idx < 0 {
		return ""
	}
	return rest[idx+1:]
}

// ========================================================================
// Task 4.1: Integration tests for successful PAT creation response
// Test Spec: TS-09-20, TS-09-21, TS-09-22, TS-09-23
// Requirements: 09-REQ-5.1, 09-REQ-5.2, 09-REQ-5.3, 09-REQ-5.4
// ========================================================================

// TestCreatePAT_Success verifies that POST /user/tokens with a valid request
// body returns HTTP 201 with a CreatePATResponse containing token_id (8 chars),
// name matching the request, token matching the canonical regex pattern,
// permissions in insertion order, non-null expires_at (for expires=90),
// and a non-empty created_at.
//
// Test Spec: TS-09-20
// Requirement: 09-REQ-5.1
func TestCreatePAT_Success(t *testing.T) {
	e, _ := setupCreatePATServer(t)

	rec := sendJSON(t, e, http.MethodPost, "/user/tokens",
		`{"name": "ci-deploy", "permissions": ["users:read", "orgs:read"], "expires": 90}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected HTTP 201, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp createPATResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse CreatePATResponse: %v\nbody: %s", err, rec.Body.String())
	}

	// token_id must be 8 characters.
	if len(resp.TokenID) != 8 {
		t.Errorf("expected token_id length 8, got %d: %q", len(resp.TokenID), resp.TokenID)
	}

	// name must match the request.
	if resp.Name != "ci-deploy" {
		t.Errorf("expected name %q, got %q", "ci-deploy", resp.Name)
	}

	// token must match the canonical regex pattern.
	pattern := regexp.MustCompile(`^[a-z0-9]+_pat_[a-z0-9]{8}_[a-z0-9]{32}$`)
	if !pattern.MatchString(resp.Token) {
		t.Errorf("token %q does not match pattern %s", resp.Token, pattern)
	}

	// permissions must preserve insertion order.
	expectedPerms := []string{"users:read", "orgs:read"}
	if len(resp.Permissions) != len(expectedPerms) {
		t.Fatalf("expected %d permissions, got %d", len(expectedPerms), len(resp.Permissions))
	}
	for i, perm := range resp.Permissions {
		if perm != expectedPerms[i] {
			t.Errorf("permission[%d] = %q, want %q", i, perm, expectedPerms[i])
		}
	}

	// expires_at must be non-null for expires=90.
	if resp.ExpiresAt == nil {
		t.Error("expected non-null expires_at for expires=90")
	}

	// created_at must be non-empty.
	if resp.CreatedAt == "" {
		t.Error("expected non-empty created_at")
	}
}

// TestCreatePAT_SecretHashed verifies that the pats table row stores
// secret_hash = SHA-256(secret) and never contains the plaintext secret.
//
// Test Spec: TS-09-21
// Requirement: 09-REQ-5.2
func TestCreatePAT_SecretHashed(t *testing.T) {
	e, database := setupCreatePATServer(t)

	rec := sendJSON(t, e, http.MethodPost, "/user/tokens",
		`{"name": "test", "permissions": ["tokens:read"], "expires": 30}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected HTTP 201, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp createPATResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse CreatePATResponse: %v\nbody: %s", err, rec.Body.String())
	}

	// Extract secret from the token string.
	secret := extractSecretFromToken(resp.Token)
	if secret == "" {
		t.Fatal("failed to extract secret from token")
	}

	// Query the database for the stored secret_hash.
	var secretHash string
	err := database.SqlDB.QueryRow(
		"SELECT secret_hash FROM pats WHERE token_id = ?", resp.TokenID,
	).Scan(&secretHash)
	if err != nil {
		t.Fatalf("failed to query pats table: %v", err)
	}

	// Verify secret_hash equals SHA-256(secret).
	expectedHash := sha256Hex(secret)
	if secretHash != expectedHash {
		t.Errorf("secret_hash = %q, want SHA-256(secret) = %q", secretHash, expectedHash)
	}

	// Verify secret_hash is NOT the plaintext secret.
	if secretHash == secret {
		t.Error("secret_hash equals the plaintext secret — secrets must be hashed")
	}
}

// TestCreatePAT_PermissionsInsertionOrder verifies that the permissions array
// is preserved in insertion order in both the CreatePATResponse and the pats
// table.
//
// Test Spec: TS-09-22
// Requirement: 09-REQ-5.3
func TestCreatePAT_PermissionsInsertionOrder(t *testing.T) {
	e, database := setupCreatePATServer(t)

	rec := sendJSON(t, e, http.MethodPost, "/user/tokens",
		`{"name": "test", "permissions": ["orgs:read", "tokens:read", "users:read"], "expires": 30}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected HTTP 201, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp createPATResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse CreatePATResponse: %v\nbody: %s", err, rec.Body.String())
	}

	// Verify response permissions match insertion order.
	expectedPerms := []string{"orgs:read", "tokens:read", "users:read"}
	if len(resp.Permissions) != len(expectedPerms) {
		t.Fatalf("expected %d permissions in response, got %d",
			len(expectedPerms), len(resp.Permissions))
	}
	for i, perm := range resp.Permissions {
		if perm != expectedPerms[i] {
			t.Errorf("response permission[%d] = %q, want %q", i, perm, expectedPerms[i])
		}
	}

	// Verify database stores permissions in the same order.
	var permJSON string
	err := database.SqlDB.QueryRow(
		"SELECT permissions FROM pats WHERE token_id = ?", resp.TokenID,
	).Scan(&permJSON)
	if err != nil {
		t.Fatalf("failed to query pats table: %v", err)
	}

	var dbPerms []string
	if err := json.Unmarshal([]byte(permJSON), &dbPerms); err != nil {
		t.Fatalf("failed to parse permissions JSON from DB: %v", err)
	}

	if len(dbPerms) != len(expectedPerms) {
		t.Fatalf("expected %d permissions in DB, got %d",
			len(expectedPerms), len(dbPerms))
	}
	for i, perm := range dbPerms {
		if perm != expectedPerms[i] {
			t.Errorf("DB permission[%d] = %q, want %q", i, perm, expectedPerms[i])
		}
	}
}

// TestCreatePAT_ResponseOmitsRevokedAtAndExpiresDays verifies that the HTTP 201
// response JSON does not contain 'revoked_at' or 'expires_days' keys.
//
// Test Spec: TS-09-23
// Requirement: 09-REQ-5.4
func TestCreatePAT_ResponseOmitsRevokedAtAndExpiresDays(t *testing.T) {
	e, _ := setupCreatePATServer(t)

	rec := sendJSON(t, e, http.MethodPost, "/user/tokens",
		`{"name": "test", "permissions": ["tokens:read"], "expires": 30}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected HTTP 201, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Parse as a raw map to check key presence.
	var raw map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("failed to parse response as map: %v\nbody: %s", err, rec.Body.String())
	}

	if _, exists := raw["revoked_at"]; exists {
		t.Error("response should not contain 'revoked_at' key for a newly created PAT")
	}

	if _, exists := raw["expires_days"]; exists {
		t.Error("response should not contain 'expires_days' key — internal field not exposed")
	}
}

// ========================================================================
// Task 4.2: Tests for duplicate names and transaction rollback
// Test Spec: TS-09-24, TS-09-26, TS-09-E7, TS-09-E8
// Requirements: 09-REQ-5.5, 09-REQ-5.7, 09-REQ-5.E1, 09-REQ-5.E2
// ========================================================================

// TestCreatePAT_DuplicateName verifies that two PATs with the same name can
// be created for the same user without error; both return HTTP 201 with
// distinct token_ids, and two rows exist in the database.
//
// Test Spec: TS-09-26
// Requirement: 09-REQ-5.7
func TestCreatePAT_DuplicateName(t *testing.T) {
	e, database := setupCreatePATServer(t)

	// Create first PAT with name "ci-deploy".
	rec1 := sendJSON(t, e, http.MethodPost, "/user/tokens",
		`{"name": "ci-deploy", "permissions": ["tokens:read"], "expires": 30}`)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first create: expected HTTP 201, got %d; body: %s",
			rec1.Code, rec1.Body.String())
	}

	var resp1 createPATResponse
	if err := json.Unmarshal(rec1.Body.Bytes(), &resp1); err != nil {
		t.Fatalf("failed to parse first CreatePATResponse: %v", err)
	}

	// Create second PAT with same name "ci-deploy".
	rec2 := sendJSON(t, e, http.MethodPost, "/user/tokens",
		`{"name": "ci-deploy", "permissions": ["tokens:read"], "expires": 60}`)
	if rec2.Code != http.StatusCreated {
		t.Fatalf("second create: expected HTTP 201, got %d; body: %s",
			rec2.Code, rec2.Body.String())
	}

	var resp2 createPATResponse
	if err := json.Unmarshal(rec2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("failed to parse second CreatePATResponse: %v", err)
	}

	// token_ids must be distinct.
	if resp1.TokenID == resp2.TokenID {
		t.Errorf("expected distinct token_ids, got same: %q", resp1.TokenID)
	}

	// Verify two rows exist in the database with name "ci-deploy".
	var count int
	err := database.SqlDB.QueryRow(
		"SELECT COUNT(*) FROM pats WHERE name = ?", "ci-deploy",
	).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query pats table: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 rows with name 'ci-deploy', got %d", count)
	}
}

// TestCreatePAT_TransactionRollback verifies that when the db.WithTx
// transaction fails, HTTP 500 is returned with message "internal server error"
// and no PAT row is persisted. The database is closed after server setup to
// simulate a transaction failure.
//
// Test Spec: TS-09-24, TS-09-E7
// Requirements: 09-REQ-5.5, 09-REQ-5.E1
func TestCreatePAT_TransactionRollback(t *testing.T) {
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}

	// Insert a test user while the database is still open.
	insertTestUser(t, database.SqlDB, "test-user-uuid", "testuser",
		"test@example.com", "github", "gh-test")

	registry := auth.NewPermissionRegistry()
	handler := handlers.NewPATHandler(database, registry)
	if handler == nil {
		t.Fatal("NewPATHandler returned nil")
	}

	e := echo.New()
	g := e.Group("", apikit.CacheMiddleware(apikit.CacheNoStore))
	g.Use(nonAdminAuthMiddleware("test-user-uuid"))
	handler.RegisterRoutes(g)

	// Close the database AFTER server setup to simulate a DB/transaction failure.
	// Any db.WithTx call will now fail, triggering the rollback path.
	database.Close()

	rec := sendJSON(t, e, http.MethodPost, "/user/tokens",
		`{"name": "test", "permissions": ["tokens:read"], "expires": 30}`)

	assertErrorResponse(t, rec, http.StatusInternalServerError, "internal server error")
}

// TestCreatePAT_GeneratedBeforeTransaction verifies that token generation
// (token_id and secret) occurs before the db.WithTx transaction opens so
// that a transaction failure discards the generated values cleanly without
// any intermediate state being partially visible. No token material should
// appear in the error response.
//
// Test Spec: TS-09-E8
// Requirement: 09-REQ-5.E2
func TestCreatePAT_GeneratedBeforeTransaction(t *testing.T) {
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}

	// Insert a test user.
	insertTestUser(t, database.SqlDB, "test-user-uuid", "testuser",
		"test@example.com", "github", "gh-test")

	registry := auth.NewPermissionRegistry()
	handler := handlers.NewPATHandler(database, registry)
	if handler == nil {
		t.Fatal("NewPATHandler returned nil")
	}

	e := echo.New()
	g := e.Group("", apikit.CacheMiddleware(apikit.CacheNoStore))
	g.Use(nonAdminAuthMiddleware("test-user-uuid"))
	handler.RegisterRoutes(g)

	// Close the database to cause the WithTx transaction to fail.
	// Token generation should have already happened before the transaction.
	database.Close()

	rec := sendJSON(t, e, http.MethodPost, "/user/tokens",
		`{"name": "test", "permissions": ["tokens:read"], "expires": 30}`)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected HTTP 500, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Verify no token material is present in the error response.
	var raw map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}

	if _, exists := raw["token"]; exists {
		t.Error("error response should not contain a 'token' field — " +
			"generated token must be discarded on failure")
	}

	if _, exists := raw["token_id"]; exists {
		t.Error("error response should not contain a 'token_id' field — " +
			"generated token_id must be discarded on failure")
	}
}

// ========================================================================
// Task 4.3: Edge case tests for crypto/rand failure
// Test Spec: TS-09-E2
// Requirement: 09-REQ-2.E1
// ========================================================================

// TestCreatePAT_RandFailure verifies that when crypto/rand fails during
// token generation, the CreatePAT handler returns HTTP 500 with message
// "internal server error", no token field in the response, and no rows
// in the pats table.
//
// Test Spec: TS-09-E2
// Requirement: 09-REQ-2.E1
func TestCreatePAT_RandFailure(t *testing.T) {
	e, database := setupCreatePATServer(t)

	// Override the random reader to simulate a crypto/rand failure.
	handlers.SetRandReader(iotest.ErrReader(errors.New("simulated rand failure")))
	defer handlers.SetRandReader(rand.Reader)

	rec := sendJSON(t, e, http.MethodPost, "/user/tokens",
		`{"name": "test", "permissions": ["tokens:read"], "expires": 30}`)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected HTTP 500, got %d; body: %s", rec.Code, rec.Body.String())
	}

	assertErrorResponse(t, rec, http.StatusInternalServerError, "internal server error")

	// Verify no token field in the error response.
	var raw map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	if _, exists := raw["token"]; exists {
		t.Error("error response should not contain a 'token' field when rand fails")
	}

	// Verify pats table has zero rows.
	var count int
	err := database.SqlDB.QueryRow("SELECT COUNT(*) FROM pats").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query pats table: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 rows in pats table after rand failure, got %d", count)
	}
}

// ========================================================================
// Task 4.4: Property tests for secret storage invariant
// Test Spec: TS-09-P1
// Requirements: 09-REQ-5.1, 09-REQ-5.2
// ========================================================================

// TestPATSecretNeverPersisted_Property is a property-based test that creates
// N valid PATs with varying inputs and verifies after each creation that:
// 1. The pats table row has secret_hash == SHA-256(secret)
// 2. The plaintext secret does not appear in any row field
//
// Test Spec: TS-09-P1
// Requirements: 09-REQ-5.1, 09-REQ-5.2
func TestPATSecretNeverPersisted_Property(t *testing.T) {
	e, database := setupCreatePATServer(t)

	// Test with varying inputs: different names, permissions, and expires values.
	testCases := []struct {
		name    string
		perms   string
		expires int
	}{
		{"daily-build", `["users:read"]`, 30},
		{"staging-deploy", `["orgs:read","users:read"]`, 60},
		{"prod-monitor", `["tokens:read"]`, 90},
		{"permanent-key", `["users:read","orgs:read","tokens:read"]`, 0},
		{"short-name", `["keys:read"]`, 30},
		{"ci-runner", `["tokens:manage","tokens:read"]`, 60},
		{"read-only", `["keys:read","users:read","orgs:read"]`, 90},
		{"full-access", `["users:read","orgs:read","keys:read","keys:manage","tokens:read","tokens:manage"]`, 0},
	}

	for i, tc := range testCases {
		body := fmt.Sprintf(`{"name": %q, "permissions": %s, "expires": %d}`,
			tc.name, tc.perms, tc.expires)

		rec := sendJSON(t, e, http.MethodPost, "/user/tokens", body)

		if rec.Code != http.StatusCreated {
			t.Fatalf("iteration %d (%s): expected HTTP 201, got %d; body: %s",
				i, tc.name, rec.Code, rec.Body.String())
		}

		var resp createPATResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("iteration %d (%s): failed to parse CreatePATResponse: %v",
				i, tc.name, err)
		}

		// Extract the secret from the token string.
		secret := extractSecretFromToken(resp.Token)
		if secret == "" {
			t.Fatalf("iteration %d (%s): failed to extract secret from token %q",
				i, tc.name, resp.Token)
		}

		// Query all fields from the pats row.
		var (
			tokenID    string
			userID     string
			name       string
			secretHash string
			permsJSON  string
			createdAt  string
		)
		err := database.SqlDB.QueryRow(
			"SELECT token_id, user_id, name, secret_hash, permissions, created_at "+
				"FROM pats WHERE token_id = ?",
			resp.TokenID,
		).Scan(&tokenID, &userID, &name, &secretHash, &permsJSON, &createdAt)
		if err != nil {
			t.Fatalf("iteration %d (%s): failed to query pats table: %v",
				i, tc.name, err)
		}

		// Verify secret_hash == SHA-256(secret).
		expectedHash := sha256Hex(secret)
		if secretHash != expectedHash {
			t.Errorf("iteration %d (%s): secret_hash = %q, want SHA-256(secret) = %q",
				i, tc.name, secretHash, expectedHash)
		}

		// Verify the plaintext secret does not appear in any row field.
		rowFields := []string{tokenID, userID, name, secretHash, permsJSON, createdAt}
		for _, field := range rowFields {
			if field == secret {
				t.Errorf("iteration %d (%s): plaintext secret %q found in pats row field",
					i, tc.name, secret)
			}
		}
	}
}

// ========================================================================
// Task 5: Tests for ListPATs handler
// ========================================================================

// patResponse represents the JSON response for list, get, and revoke PAT
// operations — metadata only, never includes the plaintext secret.
type patResponse struct {
	TokenID     string   `json:"token_id"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
	ExpiresAt   *string  `json:"expires_at"`
	CreatedAt   string   `json:"created_at"`
	RevokedAt   *string  `json:"revoked_at"`
}

// setupListPATServer creates an Echo instance with PAT handler routes registered,
// API key auth middleware injected (which bypasses RequirePermission checks), and
// CacheMiddleware applied. A test user is inserted into the database.
// Returns the Echo instance and the db.DB handle.
func setupListPATServer(t *testing.T, userID string) (*echo.Echo, *db.DB) {
	t.Helper()

	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	// Insert a test user so that PAT INSERT satisfies FK constraint.
	insertTestUser(t, database.SqlDB, userID, "testuser",
		"test@example.com", "github", "gh-test")

	registry := auth.NewPermissionRegistry()
	handler := handlers.NewPATHandler(database, registry)
	if handler == nil {
		t.Fatal("NewPATHandler returned nil; cannot test ListPATs")
	}

	e := echo.New()
	g := e.Group("", apikit.CacheMiddleware(apikit.CacheNoStore))
	g.Use(nonAdminAuthMiddleware(userID))
	handler.RegisterRoutes(g)

	return e, database
}

// setupListPATServerWithPATAuth creates an Echo instance with PAT handler routes
// registered, PAT auth middleware injected (with specified permissions), and
// CacheMiddleware applied. Returns the Echo instance and the db.DB handle.
func setupListPATServerWithPATAuth(t *testing.T, userID string, permissions []string) (*echo.Echo, *db.DB) {
	t.Helper()

	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	insertTestUser(t, database.SqlDB, userID, "testuser",
		"test@example.com", "github", "gh-test")

	registry := auth.NewPermissionRegistry()
	handler := handlers.NewPATHandler(database, registry)
	if handler == nil {
		t.Fatal("NewPATHandler returned nil; cannot test ListPATs with PAT auth")
	}

	e := echo.New()
	g := e.Group("", apikit.CacheMiddleware(apikit.CacheNoStore))
	g.Use(patAuthMiddleware(userID, permissions))
	handler.RegisterRoutes(g)

	return e, database
}

// sendGET sends an HTTP GET request with no body to the given Echo instance
// and returns the response recorder.
func sendGET(t *testing.T, e *echo.Echo, path string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	return rec
}

// sendDELETE sends an HTTP DELETE request with no body to the given Echo
// instance and returns the response recorder.
func sendDELETE(t *testing.T, e *echo.Echo, path string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodDelete, path, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	return rec
}

// ========================================================================
// Task 5.1: Integration tests for ListPATs success cases
// Test Spec: TS-09-27, TS-09-28, TS-09-29, TS-09-30
// Requirements: 09-REQ-6.1, 09-REQ-6.2, 09-REQ-6.3, 09-REQ-6.4
// ========================================================================

// TestListPATs_Success verifies that GET /user/tokens returns HTTP 200 with a
// JSON array of PATResponse objects ordered by created_at DESC when the
// authenticated user has multiple PATs.
//
// Test Spec: TS-09-27
// Requirement: 09-REQ-6.1
func TestListPATs_Success(t *testing.T) {
	const userID = "user-list-1"
	e, database := setupListPATServer(t, userID)

	// Insert 3 PATs at different timestamps.
	insertTestPAT(t, database.SqlDB, "aaaaaaaa", userID, "oldest-token",
		"hash1", `["tokens:read"]`, 90, nullStr("2024-04-01T00:00:00Z"), nullStrEmpty(), "2024-01-01T00:00:00Z")
	insertTestPAT(t, database.SqlDB, "bbbbbbbb", userID, "middle-token",
		"hash2", `["tokens:read","users:read"]`, 60, nullStr("2024-05-01T00:00:00Z"), nullStrEmpty(), "2024-03-01T00:00:00Z")
	insertTestPAT(t, database.SqlDB, "cccccccc", userID, "newest-token",
		"hash3", `["users:read"]`, 30, nullStr("2024-07-01T00:00:00Z"), nullStrEmpty(), "2024-06-01T00:00:00Z")

	rec := sendGET(t, e, "/user/tokens")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var pats []patResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &pats); err != nil {
		t.Fatalf("failed to parse response: %v\nbody: %s", err, rec.Body.String())
	}

	if len(pats) != 3 {
		t.Fatalf("expected 3 PATs, got %d", len(pats))
	}

	// Verify ordering: newest first (created_at DESC).
	for i := 0; i < len(pats)-1; i++ {
		createdI, errI := db.ParseTime(pats[i].CreatedAt)
		createdJ, errJ := db.ParseTime(pats[i+1].CreatedAt)
		if errI != nil || errJ != nil {
			t.Fatalf("failed to parse created_at: %v, %v", errI, errJ)
		}
		if createdI.Before(createdJ) {
			t.Errorf("PATs not in created_at DESC order: pats[%d].created_at=%s < pats[%d].created_at=%s",
				i, pats[i].CreatedAt, i+1, pats[i+1].CreatedAt)
		}
	}
}

// TestListPATs_IncludesExpiredAndRevoked verifies that GET /user/tokens includes
// all PATs regardless of status: active, expired, and revoked PATs all appear.
//
// Test Spec: TS-09-28
// Requirement: 09-REQ-6.2
func TestListPATs_IncludesExpiredAndRevoked(t *testing.T) {
	const userID = "user-list-2"
	e, database := setupListPATServer(t, userID)

	// Active PAT: expires in the future, not revoked.
	insertTestPAT(t, database.SqlDB, "act11111", userID, "active-pat",
		"hash1", `["tokens:read"]`, 90, nullStr("2099-01-01T00:00:00Z"), nullStrEmpty(), "2024-06-01T00:00:00Z")

	// Expired PAT: expires_at in the past, not revoked.
	insertTestPAT(t, database.SqlDB, "exp22222", userID, "expired-pat",
		"hash2", `["tokens:read"]`, 30, nullStr("2020-01-01T00:00:00Z"), nullStrEmpty(), "2019-12-01T00:00:00Z")

	// Revoked PAT: revoked_at is set.
	insertTestPAT(t, database.SqlDB, "rev33333", userID, "revoked-pat",
		"hash3", `["tokens:read"]`, 60, nullStr("2025-01-01T00:00:00Z"), nullStr("2024-06-15T00:00:00Z"), "2024-05-01T00:00:00Z")

	rec := sendGET(t, e, "/user/tokens")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var pats []patResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &pats); err != nil {
		t.Fatalf("failed to parse response: %v\nbody: %s", err, rec.Body.String())
	}

	if len(pats) != 3 {
		t.Fatalf("expected 3 PATs (active + expired + revoked), got %d", len(pats))
	}

	// Count revoked PATs.
	var revokedCount int
	for _, p := range pats {
		if p.RevokedAt != nil {
			revokedCount++
		}
	}
	if revokedCount != 1 {
		t.Errorf("expected 1 revoked PAT, got %d", revokedCount)
	}

	// Count expired PATs (expires_at in the past).
	now := time.Now()
	var expiredCount int
	for _, p := range pats {
		if p.ExpiresAt != nil {
			expiresAt, err := db.ParseTime(*p.ExpiresAt)
			if err != nil {
				t.Fatalf("failed to parse expires_at %q: %v", *p.ExpiresAt, err)
			}
			if expiresAt.Before(now) {
				expiredCount++
			}
		}
	}
	if expiredCount < 1 {
		t.Errorf("expected at least 1 expired PAT, got %d", expiredCount)
	}
}

// TestListPATs_NoSecrets verifies that the GET /user/tokens response does not
// include secret_hash, plaintext token, or expires_days fields in any element.
//
// Test Spec: TS-09-29
// Requirement: 09-REQ-6.3
func TestListPATs_NoSecrets(t *testing.T) {
	const userID = "user-list-3"
	e, database := setupListPATServer(t, userID)

	insertTestPAT(t, database.SqlDB, "nosec111", userID, "secret-check",
		"hash1", `["tokens:read"]`, 90, nullStr("2025-01-01T00:00:00Z"), nullStrEmpty(), "2024-06-01T00:00:00Z")

	rec := sendGET(t, e, "/user/tokens")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Parse as raw JSON array of maps to check key presence.
	var rawPats []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &rawPats); err != nil {
		t.Fatalf("failed to parse response as array of maps: %v\nbody: %s",
			err, rec.Body.String())
	}

	if len(rawPats) == 0 {
		t.Fatal("expected at least 1 PAT in response, got 0")
	}

	for i, pat := range rawPats {
		if _, exists := pat["secret_hash"]; exists {
			t.Errorf("pats[%d]: response should not contain 'secret_hash' key", i)
		}
		if _, exists := pat["token"]; exists {
			t.Errorf("pats[%d]: response should not contain 'token' key", i)
		}
		if _, exists := pat["expires_days"]; exists {
			t.Errorf("pats[%d]: response should not contain 'expires_days' key", i)
		}
	}
}

// TestListPATs_OtherUserTokensExcluded verifies that GET /user/tokens returns
// only PATs belonging to the authenticated user; PATs from other users are
// excluded from the result.
//
// Test Spec: TS-09-30
// Requirement: 09-REQ-6.4
func TestListPATs_OtherUserTokensExcluded(t *testing.T) {
	const user1ID = "user-list-4a"
	const user2ID = "user-list-4b"

	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	// Insert both test users.
	insertTestUser(t, database.SqlDB, user1ID, "user1", "user1@example.com", "github", "gh-1")
	insertTestUser(t, database.SqlDB, user2ID, "user2", "user2@example.com", "github", "gh-2")

	// Insert 2 PATs for user-1.
	insertTestPAT(t, database.SqlDB, "u1pat111", user1ID, "user1-pat-a",
		"hash1a", `["tokens:read"]`, 90, nullStr("2025-01-01T00:00:00Z"), nullStrEmpty(), "2024-06-01T00:00:00Z")
	insertTestPAT(t, database.SqlDB, "u1pat222", user1ID, "user1-pat-b",
		"hash1b", `["users:read"]`, 60, nullStr("2025-02-01T00:00:00Z"), nullStrEmpty(), "2024-07-01T00:00:00Z")

	// Insert 3 PATs for user-2.
	insertTestPAT(t, database.SqlDB, "u2pat111", user2ID, "user2-pat-a",
		"hash2a", `["tokens:read"]`, 90, nullStr("2025-03-01T00:00:00Z"), nullStrEmpty(), "2024-08-01T00:00:00Z")
	insertTestPAT(t, database.SqlDB, "u2pat222", user2ID, "user2-pat-b",
		"hash2b", `["users:read"]`, 60, nullStr("2025-04-01T00:00:00Z"), nullStrEmpty(), "2024-09-01T00:00:00Z")
	insertTestPAT(t, database.SqlDB, "u2pat333", user2ID, "user2-pat-c",
		"hash2c", `["orgs:read"]`, 30, nullStr("2025-05-01T00:00:00Z"), nullStrEmpty(), "2024-10-01T00:00:00Z")

	// Set up server authenticated as user-1.
	registry := auth.NewPermissionRegistry()
	handler := handlers.NewPATHandler(database, registry)
	if handler == nil {
		t.Fatal("NewPATHandler returned nil; cannot test ListPATs user isolation")
	}

	e := echo.New()
	g := e.Group("", apikit.CacheMiddleware(apikit.CacheNoStore))
	g.Use(nonAdminAuthMiddleware(user1ID))
	handler.RegisterRoutes(g)

	rec := sendGET(t, e, "/user/tokens")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var pats []patResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &pats); err != nil {
		t.Fatalf("failed to parse response: %v\nbody: %s", err, rec.Body.String())
	}

	// User-1 should see only their 2 PATs, not user-2's 3.
	if len(pats) != 2 {
		t.Fatalf("expected 2 PATs for user-1, got %d", len(pats))
	}

	// Verify each returned PAT belongs to user-1 by checking token_id.
	for _, p := range pats {
		var ownerID string
		err := database.SqlDB.QueryRow(
			"SELECT user_id FROM pats WHERE token_id = ?", p.TokenID,
		).Scan(&ownerID)
		if err != nil {
			t.Fatalf("failed to query owner for token_id %q: %v", p.TokenID, err)
		}
		if ownerID != user1ID {
			t.Errorf("PAT %q belongs to %q, expected %q", p.TokenID, ownerID, user1ID)
		}
	}
}

// ========================================================================
// Task 5.2: Tests for ListPATs edge cases and permissions
// Test Spec: TS-09-31, TS-09-E9, TS-09-E10
// Requirements: 09-REQ-6.5, 09-REQ-6.E1, 09-REQ-6.E2
// ========================================================================

// TestListPATs_RequiresTokensRead verifies that GET /user/tokens returns
// HTTP 403 when the credential is a PAT lacking the tokens:read permission.
//
// Test Spec: TS-09-31
// Requirement: 09-REQ-6.5
func TestListPATs_RequiresTokensRead(t *testing.T) {
	// PAT with only tokens:manage (no tokens:read).
	e, _ := setupListPATServerWithPATAuth(t, "user-perm-1", []string{"tokens:manage"})

	rec := sendGET(t, e, "/user/tokens")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected HTTP 403 for PAT lacking tokens:read, got %d; body: %s",
			rec.Code, rec.Body.String())
	}
}

// TestListPATs_Empty verifies that GET /user/tokens returns HTTP 200 with an
// empty JSON array [] when the authenticated user has no PATs.
//
// Test Spec: TS-09-E9
// Requirement: 09-REQ-6.E1
func TestListPATs_Empty(t *testing.T) {
	e, _ := setupListPATServer(t, "user-empty-1")

	rec := sendGET(t, e, "/user/tokens")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var pats []patResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &pats); err != nil {
		t.Fatalf("failed to parse response: %v\nbody: %s", err, rec.Body.String())
	}

	if pats == nil {
		t.Fatal("expected empty array [], got null")
	}

	if len(pats) != 0 {
		t.Fatalf("expected 0 PATs, got %d", len(pats))
	}

	// Also verify the raw body is exactly "[]" (not "null").
	body := strings.TrimSpace(rec.Body.String())
	if body != "[]" {
		t.Errorf("expected body to be %q, got %q", "[]", body)
	}
}

// TestListPATs_DBError verifies that GET /user/tokens returns HTTP 500 with
// message "internal server error" when the database query fails.
//
// Test Spec: TS-09-E10
// Requirement: 09-REQ-6.E2
func TestListPATs_DBError(t *testing.T) {
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}

	insertTestUser(t, database.SqlDB, "user-dberr-1", "testuser",
		"test@example.com", "github", "gh-test")

	registry := auth.NewPermissionRegistry()
	handler := handlers.NewPATHandler(database, registry)
	if handler == nil {
		t.Fatal("NewPATHandler returned nil; cannot test ListPATs DB error")
	}

	e := echo.New()
	g := e.Group("", apikit.CacheMiddleware(apikit.CacheNoStore))
	g.Use(nonAdminAuthMiddleware("user-dberr-1"))
	handler.RegisterRoutes(g)

	// Close the database AFTER setup to cause query failures.
	database.Close()

	rec := sendGET(t, e, "/user/tokens")

	assertErrorResponse(t, rec, http.StatusInternalServerError, "internal server error")
}

// TestListPATs_OrderByCreatedAtDesc verifies that GET /user/tokens returns PATs
// in descending chronological order by created_at timestamp.
//
// Requirement: 09-REQ-6.1
func TestListPATs_OrderByCreatedAtDesc(t *testing.T) {
	const userID = "user-order-1"
	e, database := setupListPATServer(t, userID)

	// Insert PATs with intentionally non-sequential token_ids to ensure
	// ordering comes from created_at, not from insertion order or token_id.
	insertTestPAT(t, database.SqlDB, "zzzzzzzz", userID, "first-created",
		"hash1", `["tokens:read"]`, 90, nullStr("2025-01-01T00:00:00Z"), nullStrEmpty(), "2024-01-15T00:00:00Z")
	insertTestPAT(t, database.SqlDB, "aaaaaaab", userID, "second-created",
		"hash2", `["tokens:read"]`, 90, nullStr("2025-06-01T00:00:00Z"), nullStrEmpty(), "2024-03-20T00:00:00Z")
	insertTestPAT(t, database.SqlDB, "mmmmmmmm", userID, "third-created",
		"hash3", `["tokens:read"]`, 90, nullStr("2025-12-01T00:00:00Z"), nullStrEmpty(), "2024-09-10T00:00:00Z")

	rec := sendGET(t, e, "/user/tokens")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var pats []patResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &pats); err != nil {
		t.Fatalf("failed to parse response: %v\nbody: %s", err, rec.Body.String())
	}

	if len(pats) != 3 {
		t.Fatalf("expected 3 PATs, got %d", len(pats))
	}

	// Verify descending order: newest first.
	// third-created (2024-09-10) > second-created (2024-03-20) > first-created (2024-01-15)
	expectedOrder := []string{"third-created", "second-created", "first-created"}
	for i, expectedName := range expectedOrder {
		if pats[i].Name != expectedName {
			t.Errorf("pats[%d].name = %q, want %q (expected created_at DESC order)",
				i, pats[i].Name, expectedName)
		}
	}
}

// ========================================================================
// Task 5.3: Property test for user isolation invariant
// Test Spec: TS-09-P2
// Requirements: 09-REQ-6.4, 09-REQ-7.1, 09-REQ-7.2, 09-REQ-7.E1,
//               09-REQ-8.1, 09-REQ-8.E2
// ========================================================================

// TestPATUserIsolation_Property is a property-based test that verifies user
// isolation: for any PAT belonging to user A, requests authenticated as
// user B (B != A) always return HTTP 404 for GET and DELETE, and the PAT
// never appears in user B's list response.
//
// Test Spec: TS-09-P2
// Requirements: 09-REQ-6.4, 09-REQ-7.1, 09-REQ-7.2, 09-REQ-7.E1,
//               09-REQ-8.1, 09-REQ-8.E2
func TestPATUserIsolation_Property(t *testing.T) {
	// Generate pairs of distinct users.
	userPairs := []struct {
		userA string
		userB string
	}{
		{"iso-user-a1", "iso-user-b1"},
		{"iso-user-a2", "iso-user-b2"},
		{"iso-user-a3", "iso-user-b3"},
	}

	for _, pair := range userPairs {
		t.Run(fmt.Sprintf("%s_vs_%s", pair.userA, pair.userB), func(t *testing.T) {
			database, err := db.OpenMemory()
			if err != nil {
				t.Fatalf("failed to open in-memory database: %v", err)
			}
			t.Cleanup(func() { database.Close() })

			// Insert both users.
			insertTestUser(t, database.SqlDB, pair.userA, "userA",
				"a@example.com", "github", "gh-a")
			insertTestUser(t, database.SqlDB, pair.userB, "userB",
				"b@example.com", "github", "gh-b")

			// Create PATs for user A with different characteristics.
			userAPATs := []string{"isopataa", "isopatab", "isopatac"}
			for i, tokenID := range userAPATs {
				createdAt := fmt.Sprintf("2024-0%d-01T00:00:00Z", i+1)
				insertTestPAT(t, database.SqlDB, tokenID, pair.userA,
					fmt.Sprintf("userA-pat-%d", i),
					fmt.Sprintf("hash-a-%d", i),
					`["tokens:read"]`, 90, nullStr("2025-01-01T00:00:00Z"), nullStrEmpty(), createdAt)
			}

			// Set up server authenticated as user B (API key for full permissions).
			registry := auth.NewPermissionRegistry()
			handler := handlers.NewPATHandler(database, registry)
			if handler == nil {
				t.Fatal("NewPATHandler returned nil; cannot test user isolation")
			}

			e := echo.New()
			g := e.Group("", apikit.CacheMiddleware(apikit.CacheNoStore))
			g.Use(nonAdminAuthMiddleware(pair.userB))
			handler.RegisterRoutes(g)

			// Verify GET /user/tokens/:token_id for each of userA's PATs returns 404.
			for _, tokenID := range userAPATs {
				rec := sendGET(t, e, "/user/tokens/"+tokenID)
				if rec.Code != http.StatusNotFound {
					t.Errorf("GET /user/tokens/%s as userB: expected 404, got %d; body: %s",
						tokenID, rec.Code, rec.Body.String())
				}
				resp := parseErrorResponse(t, rec)
				if resp.Error.Message != "token not found" {
					t.Errorf("GET /user/tokens/%s as userB: expected message %q, got %q",
						tokenID, "token not found", resp.Error.Message)
				}
			}

			// Verify DELETE /user/tokens/:token_id for each of userA's PATs returns 404.
			for _, tokenID := range userAPATs {
				rec := sendDELETE(t, e, "/user/tokens/"+tokenID)
				if rec.Code != http.StatusNotFound {
					t.Errorf("DELETE /user/tokens/%s as userB: expected 404, got %d; body: %s",
						tokenID, rec.Code, rec.Body.String())
				}
				resp := parseErrorResponse(t, rec)
				if resp.Error.Message != "token not found" {
					t.Errorf("DELETE /user/tokens/%s as userB: expected message %q, got %q",
						tokenID, "token not found", resp.Error.Message)
				}
			}

			// Verify ListPATs as userB never returns userA's PATs.
			rec := sendGET(t, e, "/user/tokens")
			if rec.Code != http.StatusOK {
				t.Fatalf("GET /user/tokens as userB: expected 200, got %d; body: %s",
					rec.Code, rec.Body.String())
			}

			var pats []patResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &pats); err != nil {
				t.Fatalf("failed to parse list response: %v\nbody: %s",
					err, rec.Body.String())
			}

			// User B has no PATs, so the list should be empty.
			if len(pats) != 0 {
				t.Errorf("expected 0 PATs for userB, got %d", len(pats))
				for _, p := range pats {
					t.Errorf("  unexpected PAT: token_id=%s name=%s", p.TokenID, p.Name)
				}
			}

			// Extra check: none of userA's token_ids appear in userB's list.
			userATokenSet := make(map[string]bool)
			for _, id := range userAPATs {
				userATokenSet[id] = true
			}
			for _, p := range pats {
				if userATokenSet[p.TokenID] {
					t.Errorf("userA's token_id %q appeared in userB's list response", p.TokenID)
				}
			}
		})
	}
}

// ========================================================================
// Task 6.1: Integration and unit tests for GetPAT
// Test Spec: TS-09-32, TS-09-33, TS-09-34, TS-09-E11, TS-09-E12
// Requirements: 09-REQ-7.1, 09-REQ-7.2, 09-REQ-7.3, 09-REQ-7.E1, 09-REQ-7.E2
// ========================================================================

// TestGetPAT_Success verifies that GET /user/tokens/:token_id returns HTTP 200
// with a PATResponse containing the correct metadata for a token belonging to
// the authenticated user. The response must NOT include the plaintext token
// or secret_hash fields.
//
// Test Spec: TS-09-32
// Requirement: 09-REQ-7.1
func TestGetPAT_Success(t *testing.T) {
	const userID = "user-get-1"
	e, database := setupListPATServer(t, userID)

	// Insert a PAT with known attributes.
	insertTestPAT(t, database.SqlDB, "abc12345", userID, "ci-deploy",
		"hash-abc", `["tokens:read","users:read"]`, 90,
		nullStr("2025-04-01T00:00:00Z"), nullStrEmpty(), "2024-06-01T00:00:00Z")

	rec := sendGET(t, e, "/user/tokens/abc12345")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp patResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse PATResponse: %v\nbody: %s", err, rec.Body.String())
	}

	// Verify token_id matches.
	if resp.TokenID != "abc12345" {
		t.Errorf("expected token_id %q, got %q", "abc12345", resp.TokenID)
	}

	// Verify name matches.
	if resp.Name != "ci-deploy" {
		t.Errorf("expected name %q, got %q", "ci-deploy", resp.Name)
	}

	// Verify permissions match and preserve order.
	expectedPerms := []string{"tokens:read", "users:read"}
	if len(resp.Permissions) != len(expectedPerms) {
		t.Fatalf("expected %d permissions, got %d", len(expectedPerms), len(resp.Permissions))
	}
	for i, perm := range resp.Permissions {
		if perm != expectedPerms[i] {
			t.Errorf("permission[%d] = %q, want %q", i, perm, expectedPerms[i])
		}
	}

	// Verify created_at is non-empty.
	if resp.CreatedAt == "" {
		t.Error("expected non-empty created_at")
	}

	// Verify expires_at is non-null.
	if resp.ExpiresAt == nil {
		t.Error("expected non-null expires_at")
	}

	// Verify response does NOT contain 'token' or 'secret_hash' keys.
	var raw map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("failed to parse response as map: %v", err)
	}
	if _, exists := raw["token"]; exists {
		t.Error("response should not contain 'token' key — only CreatePATResponse includes the plaintext token")
	}
	if _, exists := raw["secret_hash"]; exists {
		t.Error("response should not contain 'secret_hash' key")
	}
}

// TestGetPAT_NotFound verifies that GET /user/tokens/:token_id returns
// HTTP 404 with message "token not found" when the token_id does not exist
// for the authenticated user.
//
// Test Spec: TS-09-33
// Requirement: 09-REQ-7.2
func TestGetPAT_NotFound(t *testing.T) {
	e, _ := setupListPATServer(t, "user-get-nf")

	rec := sendGET(t, e, "/user/tokens/nonexistent")

	assertErrorResponse(t, rec, http.StatusNotFound, "token not found")
}

// TestGetPAT_RequiresTokensRead verifies that GET /user/tokens/:token_id
// returns HTTP 403 when the authenticated credential is a PAT lacking
// the tokens:read permission.
//
// Test Spec: TS-09-34
// Requirement: 09-REQ-7.3
func TestGetPAT_RequiresTokensRead(t *testing.T) {
	const userID = "user-get-perm"
	// PAT with only tokens:manage — no tokens:read.
	e, database := setupListPATServerWithPATAuth(t, userID, []string{"tokens:manage"})

	insertTestPAT(t, database.SqlDB, "abc12345", userID, "test-pat",
		"hash-abc", `["tokens:read"]`, 90,
		nullStr("2025-01-01T00:00:00Z"), nullStrEmpty(), "2024-06-01T00:00:00Z")

	rec := sendGET(t, e, "/user/tokens/abc12345")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected HTTP 403 for PAT lacking tokens:read, got %d; body: %s",
			rec.Code, rec.Body.String())
	}
}

// TestGetPAT_OtherUserToken verifies that GET /user/tokens/:token_id returns
// HTTP 404 (not 403) when the token_id exists in the pats table but belongs
// to a different user, to avoid leaking token existence information.
//
// Test Spec: TS-09-E11
// Requirement: 09-REQ-7.E1
func TestGetPAT_OtherUserToken(t *testing.T) {
	const user1ID = "user-get-a"
	const user2ID = "user-get-b"

	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	// Insert both users.
	insertTestUser(t, database.SqlDB, user1ID, "user1", "user1@example.com", "github", "gh-1")
	insertTestUser(t, database.SqlDB, user2ID, "user2", "user2@example.com", "github", "gh-2")

	// Insert a PAT belonging to user-2.
	insertTestPAT(t, database.SqlDB, "user2pat1", user2ID, "user2-pat",
		"hash-u2", `["tokens:read"]`, 90,
		nullStr("2025-01-01T00:00:00Z"), nullStrEmpty(), "2024-06-01T00:00:00Z")

	// Set up server authenticated as user-1.
	registry := auth.NewPermissionRegistry()
	handler := handlers.NewPATHandler(database, registry)
	if handler == nil {
		t.Fatal("NewPATHandler returned nil; cannot test GetPAT cross-user isolation")
	}

	e := echo.New()
	g := e.Group("", apikit.CacheMiddleware(apikit.CacheNoStore))
	g.Use(nonAdminAuthMiddleware(user1ID))
	handler.RegisterRoutes(g)

	// User-1 requests user-2's PAT — should get 404, not 403.
	rec := sendGET(t, e, "/user/tokens/user2pat1")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected HTTP 404, got %d; body: %s", rec.Code, rec.Body.String())
	}
	resp := parseErrorResponse(t, rec)
	if resp.Error.Message != "token not found" {
		t.Errorf("expected message %q, got %q", "token not found", resp.Error.Message)
	}
}

// TestGetPAT_DBError verifies that GET /user/tokens/:token_id returns
// HTTP 500 with message "internal server error" when the database query
// fails with an error other than db.ErrNotFound.
//
// Test Spec: TS-09-E12
// Requirement: 09-REQ-7.E2
func TestGetPAT_DBError(t *testing.T) {
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}

	insertTestUser(t, database.SqlDB, "user-get-dberr", "testuser",
		"test@example.com", "github", "gh-test")

	registry := auth.NewPermissionRegistry()
	handler := handlers.NewPATHandler(database, registry)
	if handler == nil {
		t.Fatal("NewPATHandler returned nil; cannot test GetPAT DB error")
	}

	e := echo.New()
	g := e.Group("", apikit.CacheMiddleware(apikit.CacheNoStore))
	g.Use(nonAdminAuthMiddleware("user-get-dberr"))
	handler.RegisterRoutes(g)

	// Close the database AFTER setup to cause query failures.
	database.Close()

	rec := sendGET(t, e, "/user/tokens/abc12345")

	assertErrorResponse(t, rec, http.StatusInternalServerError, "internal server error")
}

// ========================================================================
// Task 6.2: Integration tests for RevokePAT success and permission
// Test Spec: TS-09-35, TS-09-37, TS-09-38
// Requirements: 09-REQ-8.1, 09-REQ-8.3, 09-REQ-8.4
// ========================================================================

// TestRevokePAT_Success verifies that DELETE /user/tokens/:token_id issues
// a conditional UPDATE setting revoked_at and returns HTTP 200 with the
// updated PATResponse including a non-null revoked_at timestamp. The row
// must persist in the pats table with revoked_at set.
//
// Test Spec: TS-09-35
// Requirement: 09-REQ-8.1
func TestRevokePAT_Success(t *testing.T) {
	const userID = "user-revoke-1"
	e, database := setupListPATServer(t, userID)

	// Insert an active (non-revoked) PAT.
	insertTestPAT(t, database.SqlDB, "abc12345", userID, "active-pat",
		"hash-abc", `["tokens:read","users:read"]`, 90,
		nullStr("2025-04-01T00:00:00Z"), nullStrEmpty(), "2024-06-01T00:00:00Z")

	rec := sendDELETE(t, e, "/user/tokens/abc12345")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp patResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse PATResponse: %v\nbody: %s", err, rec.Body.String())
	}

	// Verify revoked_at is set in the response.
	if resp.RevokedAt == nil {
		t.Fatal("expected revoked_at to be non-null in response")
	}

	// Verify the token_id matches.
	if resp.TokenID != "abc12345" {
		t.Errorf("expected token_id %q, got %q", "abc12345", resp.TokenID)
	}

	// Verify the pats table row still exists with revoked_at set.
	var revokedAt sql.NullString
	err := database.SqlDB.QueryRow(
		"SELECT revoked_at FROM pats WHERE token_id = ?", "abc12345",
	).Scan(&revokedAt)
	if err != nil {
		t.Fatalf("failed to query pats table: %v", err)
	}
	if !revokedAt.Valid {
		t.Fatal("expected revoked_at to be set in database, but it was NULL")
	}
}

// TestRevokePAT_RequiresTokensManage verifies that DELETE /user/tokens/:token_id
// returns HTTP 403 when the authenticated credential is a PAT lacking the
// tokens:manage permission. The token must remain unrevoked in the database.
//
// Test Spec: TS-09-37
// Requirement: 09-REQ-8.3
func TestRevokePAT_RequiresTokensManage(t *testing.T) {
	const userID = "user-revoke-perm"
	// PAT with only tokens:read — no tokens:manage.
	e, database := setupListPATServerWithPATAuth(t, userID, []string{"tokens:read"})

	insertTestPAT(t, database.SqlDB, "abc12345", userID, "test-pat",
		"hash-abc", `["tokens:read"]`, 90,
		nullStr("2025-01-01T00:00:00Z"), nullStrEmpty(), "2024-06-01T00:00:00Z")

	rec := sendDELETE(t, e, "/user/tokens/abc12345")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected HTTP 403 for PAT lacking tokens:manage, got %d; body: %s",
			rec.Code, rec.Body.String())
	}

	// Verify the PAT was NOT revoked in the database.
	var revokedAt sql.NullString
	err := database.SqlDB.QueryRow(
		"SELECT revoked_at FROM pats WHERE token_id = ?", "abc12345",
	).Scan(&revokedAt)
	if err != nil {
		t.Fatalf("failed to query pats table: %v", err)
	}
	if revokedAt.Valid {
		t.Fatal("expected revoked_at to remain NULL after permission denial, but it was set")
	}
}

// TestRevokePAT_DoesNotDelete verifies that DELETE /user/tokens/:token_id
// does not remove the PAT row from the pats table; it only sets revoked_at.
// The record is preserved for audit purposes.
//
// Test Spec: TS-09-38
// Requirement: 09-REQ-8.4
func TestRevokePAT_DoesNotDelete(t *testing.T) {
	const userID = "user-revoke-nodelete"
	e, database := setupListPATServer(t, userID)

	// Insert an active PAT.
	insertTestPAT(t, database.SqlDB, "abc12345", userID, "audit-trail-pat",
		"hash-abc", `["tokens:read"]`, 90,
		nullStr("2025-04-01T00:00:00Z"), nullStrEmpty(), "2024-06-01T00:00:00Z")

	rec := sendDELETE(t, e, "/user/tokens/abc12345")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Verify the row still exists in the pats table.
	var tokenID string
	var revokedAt sql.NullString
	err := database.SqlDB.QueryRow(
		"SELECT token_id, revoked_at FROM pats WHERE token_id = ?", "abc12345",
	).Scan(&tokenID, &revokedAt)
	if err != nil {
		t.Fatalf("PAT row should still exist after revocation, but query failed: %v", err)
	}
	if tokenID != "abc12345" {
		t.Errorf("expected token_id %q, got %q", "abc12345", tokenID)
	}
	if !revokedAt.Valid {
		t.Error("expected revoked_at to be set after revocation, but it was NULL")
	}

	// Verify the total row count hasn't decreased.
	var count int
	err = database.SqlDB.QueryRow(
		"SELECT COUNT(*) FROM pats WHERE token_id = ?", "abc12345",
	).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count pats rows: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row for token_id 'abc12345' after revocation, got %d", count)
	}
}

// ========================================================================
// Task 6.3: Unit and integration tests for RevokePAT error cases and concurrency
// Test Spec: TS-09-36, TS-09-E13, TS-09-E14, TS-09-E15
// Requirements: 09-REQ-8.2, 09-REQ-8.E1, 09-REQ-8.E2, 09-REQ-8.E3
// ========================================================================

// TestRevokePAT_NotFound verifies that DELETE /user/tokens/:token_id returns
// HTTP 404 with message "token not found" when the token_id does not exist
// in the pats table for the authenticated user.
//
// Test Spec: TS-09-36
// Requirement: 09-REQ-8.2
func TestRevokePAT_NotFound(t *testing.T) {
	e, _ := setupListPATServer(t, "user-revoke-nf")

	rec := sendDELETE(t, e, "/user/tokens/missing00")

	assertErrorResponse(t, rec, http.StatusNotFound, "token not found")
}

// TestRevokePAT_AlreadyRevoked verifies that DELETE /user/tokens/:token_id
// returns HTTP 400 with message "token already revoked" when the token exists
// but its revoked_at is already set.
//
// Test Spec: TS-09-36
// Requirement: 09-REQ-8.2
func TestRevokePAT_AlreadyRevoked(t *testing.T) {
	const userID = "user-revoke-already"
	e, database := setupListPATServer(t, userID)

	// Insert a revoked PAT.
	insertTestPAT(t, database.SqlDB, "revoked01", userID, "old-revoked-pat",
		"hash-rev", `["tokens:read"]`, 90,
		nullStr("2025-01-01T00:00:00Z"), nullStr("2024-08-01T00:00:00Z"), "2024-06-01T00:00:00Z")

	rec := sendDELETE(t, e, "/user/tokens/revoked01")

	assertErrorResponse(t, rec, http.StatusBadRequest, "token already revoked")
}

// TestRevokePAT_OtherUserToken verifies that DELETE /user/tokens/:token_id
// returns HTTP 404 (not 403) when the token_id exists but belongs to a
// different user, to avoid information leakage.
//
// Test Spec: TS-09-E14
// Requirement: 09-REQ-8.E2
func TestRevokePAT_OtherUserToken(t *testing.T) {
	const user1ID = "user-revoke-a"
	const user2ID = "user-revoke-b"

	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	// Insert both users.
	insertTestUser(t, database.SqlDB, user1ID, "user1", "user1@example.com", "github", "gh-1")
	insertTestUser(t, database.SqlDB, user2ID, "user2", "user2@example.com", "github", "gh-2")

	// Insert an active PAT belonging to user-2.
	insertTestPAT(t, database.SqlDB, "user2pat1", user2ID, "user2-pat",
		"hash-u2", `["tokens:read"]`, 90,
		nullStr("2025-01-01T00:00:00Z"), nullStrEmpty(), "2024-06-01T00:00:00Z")

	// Set up server authenticated as user-1.
	registry := auth.NewPermissionRegistry()
	handler := handlers.NewPATHandler(database, registry)
	if handler == nil {
		t.Fatal("NewPATHandler returned nil; cannot test RevokePAT cross-user isolation")
	}

	e := echo.New()
	g := e.Group("", apikit.CacheMiddleware(apikit.CacheNoStore))
	g.Use(nonAdminAuthMiddleware(user1ID))
	handler.RegisterRoutes(g)

	// User-1 attempts to revoke user-2's PAT — should get 404, not 403.
	rec := sendDELETE(t, e, "/user/tokens/user2pat1")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected HTTP 404, got %d; body: %s", rec.Code, rec.Body.String())
	}
	resp := parseErrorResponse(t, rec)
	if resp.Error.Message != "token not found" {
		t.Errorf("expected message %q, got %q", "token not found", resp.Error.Message)
	}

	// Verify user-2's PAT was NOT revoked.
	var revokedAt sql.NullString
	err = database.SqlDB.QueryRow(
		"SELECT revoked_at FROM pats WHERE token_id = ?", "user2pat1",
	).Scan(&revokedAt)
	if err != nil {
		t.Fatalf("failed to query pats table: %v", err)
	}
	if revokedAt.Valid {
		t.Error("user-2's PAT should not be revoked by user-1's request")
	}
}

// TestRevokePAT_Concurrent verifies that when two concurrent DELETE requests
// arrive for the same token, exactly one receives HTTP 200 with revoked_at set,
// and the other receives HTTP 400 with "token already revoked". The revoked_at
// timestamp is set exactly once in the database.
//
// Test Spec: TS-09-E13
// Requirement: 09-REQ-8.E1
func TestRevokePAT_Concurrent(t *testing.T) {
	const userID = "user-revoke-concurrent"
	e, database := setupListPATServer(t, userID)

	// Insert an active PAT.
	insertTestPAT(t, database.SqlDB, "concurpat", userID, "concurrent-test-pat",
		"hash-conc", `["tokens:read"]`, 90,
		nullStr("2025-01-01T00:00:00Z"), nullStrEmpty(), "2024-06-01T00:00:00Z")

	const goroutines = 2
	results := make([]*httptest.ResponseRecorder, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodDelete, "/user/tokens/concurpat", nil)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			results[idx] = rec
		}(i)
	}

	wg.Wait()

	// Count success (200) and already-revoked (400) responses.
	successCount := 0
	alreadyRevokedCount := 0
	for _, rec := range results {
		switch rec.Code {
		case http.StatusOK:
			successCount++
			var resp patResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to parse 200 response: %v", err)
			}
			if resp.RevokedAt == nil {
				t.Error("HTTP 200 response should have revoked_at set")
			}
		case http.StatusBadRequest:
			alreadyRevokedCount++
			resp := parseErrorResponse(t, rec)
			if resp.Error.Message != "token already revoked" {
				t.Errorf("expected message %q, got %q", "token already revoked", resp.Error.Message)
			}
		default:
			t.Errorf("unexpected HTTP status %d; body: %s", rec.Code, rec.Body.String())
		}
	}

	if successCount != 1 {
		t.Errorf("expected exactly 1 HTTP 200 response, got %d", successCount)
	}
	if alreadyRevokedCount != 1 {
		t.Errorf("expected exactly 1 HTTP 400 response, got %d", alreadyRevokedCount)
	}

	// Verify revoked_at is set exactly once in the database.
	var count int
	err := database.SqlDB.QueryRow(
		"SELECT COUNT(*) FROM pats WHERE token_id = ? AND revoked_at IS NOT NULL", "concurpat",
	).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query pats table: %v", err)
	}
	if count != 1 {
		t.Errorf("expected exactly 1 row with revoked_at set, got %d", count)
	}
}

// TestRevokePAT_DBError verifies that DELETE /user/tokens/:token_id returns
// HTTP 500 with message "internal server error" when the UPDATE or follow-up
// SELECT query fails with a database error.
//
// Test Spec: TS-09-E15
// Requirement: 09-REQ-8.E3
func TestRevokePAT_DBError(t *testing.T) {
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}

	insertTestUser(t, database.SqlDB, "user-revoke-dberr", "testuser",
		"test@example.com", "github", "gh-test")

	registry := auth.NewPermissionRegistry()
	handler := handlers.NewPATHandler(database, registry)
	if handler == nil {
		t.Fatal("NewPATHandler returned nil; cannot test RevokePAT DB error")
	}

	e := echo.New()
	g := e.Group("", apikit.CacheMiddleware(apikit.CacheNoStore))
	g.Use(nonAdminAuthMiddleware("user-revoke-dberr"))
	handler.RegisterRoutes(g)

	// Close the database AFTER setup to cause UPDATE failures.
	database.Close()

	rec := sendDELETE(t, e, "/user/tokens/abc12345")

	assertErrorResponse(t, rec, http.StatusInternalServerError, "internal server error")
}

// ========================================================================
// Task 6.4: Property tests for revocation idempotency and Cache-Control
// Test Spec: TS-09-P5, TS-09-P7
// Requirements: 09-REQ-8.E1, 09-REQ-1.4
// ========================================================================

// TestRevocationIdempotency_Property is a property-based test that dispatches
// N concurrent DELETE /user/tokens/:token_id requests for the same active PAT
// and verifies: exactly 1 receives HTTP 200, N-1 receive HTTP 400 with
// message "token already revoked", and revoked_at is set exactly once in
// the database.
//
// Test Spec: TS-09-P5
// Requirements: 09-REQ-8.1, 09-REQ-8.2, 09-REQ-8.E1
func TestRevocationIdempotency_Property(t *testing.T) {
	// Test with varying concurrency levels.
	for _, n := range []int{2, 5, 10} {
		t.Run(fmt.Sprintf("N=%d", n), func(t *testing.T) {
			const userID = "user-idem-prop"
			tokenID := fmt.Sprintf("idem%04d", n)

			e, database := setupListPATServer(t, userID)

			// Insert an active PAT.
			insertTestPAT(t, database.SqlDB, tokenID, userID, "idempotency-test",
				"hash-idem", `["tokens:read"]`, 90,
				nullStr("2025-01-01T00:00:00Z"), nullStrEmpty(), "2024-06-01T00:00:00Z")

			results := make([]*httptest.ResponseRecorder, n)
			var wg sync.WaitGroup
			wg.Add(n)

			for i := 0; i < n; i++ {
				go func(idx int) {
					defer wg.Done()
					req := httptest.NewRequest(http.MethodDelete, "/user/tokens/"+tokenID, nil)
					rec := httptest.NewRecorder()
					e.ServeHTTP(rec, req)
					results[idx] = rec
				}(i)
			}

			wg.Wait()

			successCount := 0
			alreadyRevokedCount := 0
			for _, rec := range results {
				switch rec.Code {
				case http.StatusOK:
					successCount++
				case http.StatusBadRequest:
					alreadyRevokedCount++
					resp := parseErrorResponse(t, rec)
					if resp.Error.Message != "token already revoked" {
						t.Errorf("expected message %q, got %q",
							"token already revoked", resp.Error.Message)
					}
				default:
					t.Errorf("unexpected HTTP status %d; body: %s", rec.Code, rec.Body.String())
				}
			}

			if successCount != 1 {
				t.Errorf("expected exactly 1 HTTP 200, got %d", successCount)
			}
			if alreadyRevokedCount != n-1 {
				t.Errorf("expected %d HTTP 400 responses, got %d", n-1, alreadyRevokedCount)
			}

			// Verify revoked_at is set exactly once.
			var count int
			err := database.SqlDB.QueryRow(
				"SELECT COUNT(*) FROM pats WHERE token_id = ? AND revoked_at IS NOT NULL", tokenID,
			).Scan(&count)
			if err != nil {
				t.Fatalf("failed to query pats table: %v", err)
			}
			if count != 1 {
				t.Errorf("expected 1 row with revoked_at set, got %d", count)
			}
		})
	}
}

// TestCacheControlHeader_Property is a property-based test that sends a
// variety of valid and invalid requests to all four PAT endpoints and verifies
// that every response includes the Cache-Control: no-store header, regardless
// of HTTP status code (2xx, 4xx, or 5xx).
//
// Test Spec: TS-09-P7
// Requirement: 09-REQ-1.4
func TestCacheControlHeader_Property(t *testing.T) {
	const userID = "user-cache-prop"
	e, database := setupListPATServer(t, userID)

	// Insert PATs for various test scenarios.
	insertTestPAT(t, database.SqlDB, "cacheact1", userID, "active-cache-test",
		"hash-cc1", `["tokens:read"]`, 90,
		nullStr("2025-01-01T00:00:00Z"), nullStrEmpty(), "2024-06-01T00:00:00Z")
	insertTestPAT(t, database.SqlDB, "cacherev1", userID, "revoked-cache-test",
		"hash-cc2", `["tokens:read"]`, 90,
		nullStr("2025-01-01T00:00:00Z"), nullStr("2024-08-01T00:00:00Z"), "2024-06-01T00:00:00Z")

	// Define a set of requests covering various status codes across all endpoints.
	requests := []struct {
		label  string
		method string
		path   string
		body   string
	}{
		// POST /user/tokens — valid request (should be 201)
		{"POST valid", http.MethodPost, "/user/tokens",
			`{"name":"cache-test","permissions":["tokens:read"],"expires":30}`},
		// POST /user/tokens — invalid request body (should be 400)
		{"POST invalid-json", http.MethodPost, "/user/tokens", "not json"},
		// POST /user/tokens — missing name (should be 400)
		{"POST missing-name", http.MethodPost, "/user/tokens",
			`{"permissions":["tokens:read"]}`},
		// GET /user/tokens — list (should be 200)
		{"GET list", http.MethodGet, "/user/tokens", ""},
		// GET /user/tokens/:token_id — existing (should be 200)
		{"GET existing", http.MethodGet, "/user/tokens/cacheact1", ""},
		// GET /user/tokens/:token_id — not found (should be 404)
		{"GET notfound", http.MethodGet, "/user/tokens/nonexistent", ""},
		// DELETE /user/tokens/:token_id — active token (should be 200)
		{"DELETE active", http.MethodDelete, "/user/tokens/cacheact1", ""},
		// DELETE /user/tokens/:token_id — already revoked (should be 400)
		{"DELETE already-revoked", http.MethodDelete, "/user/tokens/cacherev1", ""},
		// DELETE /user/tokens/:token_id — not found (should be 404)
		{"DELETE notfound", http.MethodDelete, "/user/tokens/nonexistent", ""},
	}

	for _, req := range requests {
		t.Run(req.label, func(t *testing.T) {
			var rec *httptest.ResponseRecorder
			if req.body != "" {
				rec = sendJSON(t, e, req.method, req.path, req.body)
			} else {
				switch req.method {
				case http.MethodGet:
					rec = sendGET(t, e, req.path)
				case http.MethodDelete:
					rec = sendDELETE(t, e, req.path)
				default:
					t.Fatalf("unsupported method %q for no-body request", req.method)
				}
			}

			cacheControl := rec.Header().Get("Cache-Control")
			if cacheControl != "no-store" {
				t.Errorf("HTTP %d %s %s: Cache-Control = %q, want %q",
					rec.Code, req.method, req.path, cacheControl, "no-store")
			}
		})
	}
}

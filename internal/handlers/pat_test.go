package handlers_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
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

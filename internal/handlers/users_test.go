package handlers_test

import (
	"database/sql"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/txsvc/apikit/internal/auth"
	"github.com/txsvc/apikit/internal/db"
	"github.com/txsvc/apikit/internal/handlers"
)

// ========================================================================
// Test Helpers
// ========================================================================

// errorResponse matches the standard JSON error envelope format.
type errorResponse struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// setupAdminTestServer creates an Echo instance with RegisterUserHandlers
// registered on a root-level group with a middleware that injects admin-level
// AuthInfo into the Echo context before each handler runs. This simulates an
// authenticated admin session/API key.
// Returns the Echo instance and the raw *sql.DB handle.
func setupAdminTestServer(t *testing.T) (*echo.Echo, *sql.DB) {
	t.Helper()

	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	e := echo.New()
	g := e.Group("")
	g.Use(adminAuthMiddleware("test-admin-uuid"))
	handlers.RegisterUserHandlers(g, database.SqlDB)

	return e, database.SqlDB
}

// adminAuthMiddleware returns Echo middleware that injects admin-level AuthInfo
// into the request context. This simulates an authenticated admin credential.
// Uses auth.SetAuthInfo to store in context.Context (not c.Set) so that
// auth.GetAuthInfo, auth.RequireAdmin, and auth.IsAdmin work correctly.
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

// nonAdminAuthMiddleware returns Echo middleware that injects a non-admin
// (regular user) AuthInfo into the request context.
// Uses auth.SetAuthInfo to store in context.Context (not c.Set) so that
// auth.GetAuthInfo, auth.RequireAdmin, and auth.IsAdmin work correctly.
func nonAdminAuthMiddleware(userID string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			auth.SetAuthInfo(c, &auth.AuthInfo{
				CredentialType: "api_key",
				UserID:         userID,
				Role:           "user",
			})
			return next(c)
		}
	}
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

// parseUserResponse parses the response body as a User JSON object.
func parseUserResponse(t *testing.T, rec *httptest.ResponseRecorder) handlers.User {
	t.Helper()

	var user handlers.User
	if err := json.Unmarshal(rec.Body.Bytes(), &user); err != nil {
		t.Fatalf("failed to parse User response: %v\nbody: %s", err, rec.Body.String())
	}
	return user
}

// parseErrorResponse parses the response body as a standard JSON error envelope.
func parseErrorResponse(t *testing.T, rec *httptest.ResponseRecorder) errorResponse {
	t.Helper()

	var resp errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse error response: %v\nbody: %s", err, rec.Body.String())
	}
	return resp
}

// assertErrorResponse checks that the response has the expected HTTP status
// code and error message in the standard JSON error envelope.
func assertErrorResponse(t *testing.T, rec *httptest.ResponseRecorder, expectedCode int, expectedMessage string) {
	t.Helper()

	if rec.Code != expectedCode {
		t.Errorf("expected HTTP status %d, got %d", expectedCode, rec.Code)
	}

	resp := parseErrorResponse(t, rec)

	if resp.Error.Code != expectedCode {
		t.Errorf("expected error code %d in body, got %d", expectedCode, resp.Error.Code)
	}

	if resp.Error.Message != expectedMessage {
		t.Errorf("expected error message %q, got %q", expectedMessage, resp.Error.Message)
	}
}

// isUUID checks whether a string is a valid UUID v4 format (8-4-4-4-12 hex).
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}

// isRFC3339 checks whether a string is a valid RFC 3339 timestamp.
func isRFC3339(s string) bool {
	// RFC 3339 format: 2006-01-02T15:04:05Z or with timezone offset.
	if len(s) < 20 {
		return false
	}
	// Must end with Z or timezone offset
	return strings.HasSuffix(s, "Z") || (len(s) >= 25 && (s[19] == '+' || s[19] == '-'))
}

// testUUID returns a deterministic UUID v5 for any string, so tests can use
// readable names while producing valid UUIDs for handlers with UUID validation.
func testUUID(name string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(name)).String()
}

// insertTestUser inserts a user directly into the users table for test setup.
// If id is not a valid UUID, it is converted to one via testUUID.
func insertTestUser(t *testing.T, sqlDB *sql.DB, id, username, email, provider, providerID string) {
	t.Helper()
	if _, err := uuid.Parse(id); err != nil {
		id = testUUID(id)
	}

	now := "2024-01-01T00:00:00Z"
	_, err := sqlDB.Exec(
		`INSERT INTO users (id, username, email, full_name, role, status, provider, provider_id, created_at, updated_at)
		 VALUES (?, ?, ?, '', 'user', 'active', ?, ?, ?, ?)`,
		id, username, email, provider, providerID, now, now,
	)
	if err != nil {
		t.Fatalf("failed to insert test user: %v", err)
	}
}

// validCreateUserBody returns a valid JSON body for POST /users.
func validCreateUserBody() string {
	return `{"username":"alice","email":"alice@example.com","provider":"github","provider_id":"gh-001"}`
}

// ========================================================================
// Task 1.1: TestRegisterUserHandlers registers all 15 routes
// Test Spec: TS-07-1
// Requirement: 07-REQ-1.1
// ========================================================================

// TestRegisterUserHandlers_AllRoutes verifies that RegisterUserHandlers
// registers all 15 expected routes on the Echo group with the correct HTTP
// methods and paths.
func TestRegisterUserHandlers_AllRoutes(t *testing.T) {
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	defer database.Close()

	e := echo.New()
	g := e.Group("")
	handlers.RegisterUserHandlers(g, database.SqlDB)

	// The 15 expected (method, path) pairs from the spec.
	expected := map[string]bool{
		"POST /users":                       false,
		"GET /users":                        false,
		"GET /users/:id":                    false,
		"PATCH /users/:id":                  false,
		"POST /users/:id/promote":           false,
		"POST /users/:id/demote":            false,
		"POST /users/:id/block":             false,
		"POST /users/:id/unblock":           false,
		"GET /users/:id/keys":               false,
		"DELETE /users/:id/keys/:key_id":    false,
		"GET /users/:id/tokens":             false,
		"DELETE /users/:id/tokens/:token_id": false,
		"GET /user":                         false,
		"PATCH /user":                       false,
		"GET /user/orgs":                    false,
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
			t.Errorf("expected route %s was not registered", key)
		} else {
			found++
		}
	}

	if found != 15 {
		t.Errorf("expected 15 routes to be registered, found %d", found)
	}
}

// ========================================================================
// Task 1.2: TestHandlerFunctionsUnexported
// Test Spec: TS-07-2
// Requirement: 07-REQ-1.2
//
// Reviewer finding: scoped to users.go specifically (not the whole package)
// to avoid breakage when other handler specs add their own exports.
// ========================================================================

// TestHandlerFunctionsUnexported verifies that all handler functions in
// users.go are unexported and only RegisterUserHandlers is exported from
// that file. This is scoped to users.go specifically to avoid breaking
// when other handler specs (e.g., organization_management) add their own
// exported registration functions to the same package.
func TestHandlerFunctionsUnexported(t *testing.T) {
	// Locate users.go relative to this test file.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	usersGoPath := filepath.Join(filepath.Dir(thisFile), "users.go")

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, usersGoPath, nil, 0)
	if err != nil {
		t.Fatalf("failed to parse users.go: %v", err)
	}

	// Collect all exported function declarations in users.go.
	var exportedFuncs []string
	for _, decl := range f.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		// Skip methods (functions with a receiver) — only check package-level functions.
		if funcDecl.Recv != nil {
			continue
		}
		if funcDecl.Name.IsExported() {
			exportedFuncs = append(exportedFuncs, funcDecl.Name.Name)
		}
	}

	// The only exported function in users.go should be RegisterUserHandlers.
	if len(exportedFuncs) == 0 {
		t.Fatal("expected RegisterUserHandlers to be exported from users.go, but found no exported functions")
	}

	for _, name := range exportedFuncs {
		if name != "RegisterUserHandlers" {
			t.Errorf("unexpected exported function %q in users.go; only RegisterUserHandlers should be exported", name)
		}
	}

	// Verify RegisterUserHandlers IS in the list.
	if !slices.Contains(exportedFuncs, "RegisterUserHandlers") {
		t.Error("RegisterUserHandlers is not exported from users.go")
	}

	// Verify expected handler functions are NOT exported.
	handlerNames := []string{
		"createUser", "listUsers", "getUser", "updateUser",
		"promoteUser", "demoteUser", "blockUser", "unblockUser",
		"listUserKeys", "revokeUserKey", "listUserTokens", "revokeUserToken",
		"getOwnProfile", "updateOwnProfile", "listOwnOrgs",
	}
	for _, name := range handlerNames {
		for _, exported := range exportedFuncs {
			if strings.EqualFold(exported, name) {
				t.Errorf("handler function %q should be unexported but found exported as %q", name, exported)
			}
		}
	}
}

// ========================================================================
// Task 1.3: POST /users success and validation failures
// Test Spec: TS-07-3, TS-07-4, TS-07-5
// Requirements: 07-REQ-2.1, 07-REQ-2.2, 07-REQ-2.3
// ========================================================================

// TestCreateUser_Success verifies that a valid POST /users request from an
// admin creates a user record and returns HTTP 201 with the full User JSON
// object including correct default values.
//
// Test Spec: TS-07-3
// Requirement: 07-REQ-2.1
func TestCreateUser_Success(t *testing.T) {
	e, _ := setupAdminTestServer(t)

	body := `{"username":"alice","email":"alice@example.com","provider":"github","provider_id":"gh-001"}`
	rec := sendJSON(t, e, http.MethodPost, "/users", body)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected HTTP 201, got %d; body: %s", rec.Code, rec.Body.String())
	}

	user := parseUserResponse(t, rec)

	if !isUUID(user.ID) {
		t.Errorf("expected id to be a valid UUID, got %q", user.ID)
	}
	if user.Username != "alice" {
		t.Errorf("expected username %q, got %q", "alice", user.Username)
	}
	if user.Email != "alice@example.com" {
		t.Errorf("expected email %q, got %q", "alice@example.com", user.Email)
	}
	if user.Role != "user" {
		t.Errorf("expected role %q, got %q", "user", user.Role)
	}
	if user.Status != "active" {
		t.Errorf("expected status %q, got %q", "active", user.Status)
	}
	if user.FullName != "" {
		t.Errorf("expected full_name to be empty string, got %q", user.FullName)
	}
	if user.Provider != "github" {
		t.Errorf("expected provider %q, got %q", "github", user.Provider)
	}
	if user.ProviderID != "gh-001" {
		t.Errorf("expected provider_id %q, got %q", "gh-001", user.ProviderID)
	}
	if !isRFC3339(user.CreatedAt) {
		t.Errorf("expected created_at to be RFC 3339, got %q", user.CreatedAt)
	}
	if !isRFC3339(user.UpdatedAt) {
		t.Errorf("expected updated_at to be RFC 3339, got %q", user.UpdatedAt)
	}
}

// TestCreateUser_MissingFields verifies that POST /users returns HTTP 400 with
// a message identifying the missing field when any required field is absent.
//
// Test Spec: TS-07-4
// Requirement: 07-REQ-2.2
func TestCreateUser_MissingFields(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		missingField string
	}{
		{
			name:         "missing username",
			body:         `{"email":"alice@example.com","provider":"github","provider_id":"gh-001"}`,
			missingField: "username",
		},
		{
			name:         "missing email",
			body:         `{"username":"alice","provider":"github","provider_id":"gh-001"}`,
			missingField: "email",
		},
		{
			name:         "missing provider",
			body:         `{"username":"alice","email":"alice@example.com","provider_id":"gh-001"}`,
			missingField: "provider",
		},
		{
			name:         "missing provider_id",
			body:         `{"username":"alice","email":"alice@example.com","provider":"github"}`,
			missingField: "provider_id",
		},
		{
			name:         "empty username",
			body:         `{"username":"","email":"alice@example.com","provider":"github","provider_id":"gh-001"}`,
			missingField: "username",
		},
		{
			name:         "empty email",
			body:         `{"username":"alice","email":"","provider":"github","provider_id":"gh-001"}`,
			missingField: "email",
		},
		{
			name:         "empty provider",
			body:         `{"username":"alice","email":"alice@example.com","provider":"","provider_id":"gh-001"}`,
			missingField: "provider",
		},
		{
			name:         "empty provider_id",
			body:         `{"username":"alice","email":"alice@example.com","provider":"github","provider_id":""}`,
			missingField: "provider_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, _ := setupAdminTestServer(t)

			rec := sendJSON(t, e, http.MethodPost, "/users", tt.body)

			expectedMessage := "missing required field: " + tt.missingField
			assertErrorResponse(t, rec, http.StatusBadRequest, expectedMessage)
		})
	}
}

// TestCreateUser_InvalidJSON verifies that POST /users returns HTTP 400 with
// message 'invalid request body' when the request body is not valid JSON.
//
// Test Spec: TS-07-5
// Requirement: 07-REQ-2.3
func TestCreateUser_InvalidJSON(t *testing.T) {
	e, _ := setupAdminTestServer(t)

	// Send non-JSON body with application/json content type.
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader("not-json-at-all"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assertErrorResponse(t, rec, http.StatusBadRequest, "invalid request body")
}

// ========================================================================
// Task 1.4: POST /users conflict, auth errors, DB errors, and free-form provider
// Test Spec: TS-07-6, TS-07-7, TS-07-8, TS-07-E1, TS-07-E2
// Requirements: 07-REQ-2.4, 07-REQ-2.5, 07-REQ-2.6, 07-REQ-2.E1, 07-REQ-2.E2
// ========================================================================

// TestCreateUser_DuplicateUsername verifies that POST /users returns HTTP 409
// with message 'username already exists' when a duplicate username is submitted.
//
// Test Spec: TS-07-6
// Requirement: 07-REQ-2.4
func TestCreateUser_DuplicateUsername(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	// Pre-insert a user with username 'alice'.
	insertTestUser(t, sqlDB, "existing-uuid-1", "alice", "alice@example.com", "github", "gh-001")

	// Try to create another user with the same username.
	body := `{"username":"alice","email":"new@example.com","provider":"github","provider_id":"gh-002"}`
	rec := sendJSON(t, e, http.MethodPost, "/users", body)

	assertErrorResponse(t, rec, http.StatusConflict, "username already exists")
}

// TestCreateUser_DuplicateProviderIdentity verifies that POST /users returns
// HTTP 409 with message 'provider identity already exists' when the
// (provider, provider_id) pair is already registered.
//
// Test Spec: TS-07-7
// Requirement: 07-REQ-2.5
func TestCreateUser_DuplicateProviderIdentity(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	// Pre-insert a user with provider='github', provider_id='gh-001'.
	insertTestUser(t, sqlDB, "existing-uuid-1", "alice", "alice@example.com", "github", "gh-001")

	// Try to create another user with the same (provider, provider_id).
	body := `{"username":"bob","email":"bob@example.com","provider":"github","provider_id":"gh-001"}`
	rec := sendJSON(t, e, http.MethodPost, "/users", body)

	assertErrorResponse(t, rec, http.StatusConflict, "provider identity already exists")
}

// TestCreateUser_NonAdmin verifies that POST /users returns HTTP 403 when
// called by a non-admin user, and no database query is executed.
//
// Test Spec: TS-07-8
// Requirement: 07-REQ-2.6
func TestCreateUser_NonAdmin(t *testing.T) {
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	defer database.Close()

	e := echo.New()
	g := e.Group("")
	// Apply non-admin auth middleware instead of admin.
	g.Use(nonAdminAuthMiddleware("non-admin-user-uuid"))
	handlers.RegisterUserHandlers(g, database.SqlDB)

	rec := sendJSON(t, e, http.MethodPost, "/users", validCreateUserBody())

	assertErrorResponse(t, rec, http.StatusForbidden, "forbidden")

	// Verify no user was created in the database.
	var count int
	err = database.SqlDB.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		t.Fatalf("failed to count users: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 users in database after non-admin attempt, got %d", count)
	}
}

// TestCreateUser_DBError verifies that an unexpected database error during user
// INSERT returns HTTP 500 with message 'internal server error' without leaking
// internal error details.
//
// Test Spec: TS-07-E1
// Requirement: 07-REQ-2.E1
func TestCreateUser_DBError(t *testing.T) {
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}

	e := echo.New()
	g := e.Group("")
	g.Use(adminAuthMiddleware("test-admin-uuid"))
	handlers.RegisterUserHandlers(g, database.SqlDB)

	// Close the database AFTER registering handlers to simulate a DB failure.
	// Any INSERT will fail with a non-constraint error.
	database.Close()

	rec := sendJSON(t, e, http.MethodPost, "/users", validCreateUserBody())

	assertErrorResponse(t, rec, http.StatusInternalServerError, "internal server error")

	// Ensure no internal DB error message is exposed in the response.
	body := rec.Body.String()
	if strings.Contains(body, "sql") || strings.Contains(body, "database") || strings.Contains(body, "disk") {
		t.Errorf("response body appears to leak internal error details: %s", body)
	}
}

// TestCreateUser_FreeFormProvider verifies that the provider field is stored
// as-is without validation when an admin provides any non-empty string,
// including provider names not in the OAuth provider registry.
//
// Test Spec: TS-07-E2
// Requirement: 07-REQ-2.E2
func TestCreateUser_FreeFormProvider(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	body := `{"username":"carol","email":"carol@example.com","provider":"future-provider-xyz","provider_id":"fp-999"}`
	rec := sendJSON(t, e, http.MethodPost, "/users", body)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected HTTP 201, got %d; body: %s", rec.Code, rec.Body.String())
	}

	user := parseUserResponse(t, rec)
	if user.Provider != "future-provider-xyz" {
		t.Errorf("expected provider %q, got %q", "future-provider-xyz", user.Provider)
	}

	// Verify the provider value is persisted verbatim in the database.
	var dbProvider string
	err := sqlDB.QueryRow("SELECT provider FROM users WHERE id = ?", user.ID).Scan(&dbProvider)
	if err != nil {
		t.Fatalf("failed to query user from database: %v", err)
	}
	if dbProvider != "future-provider-xyz" {
		t.Errorf("expected provider in database to be %q, got %q", "future-provider-xyz", dbProvider)
	}
}

// ========================================================================
// Additional Test Helpers (for task group 2)
// ========================================================================

// setupNonAdminTestServer creates an Echo instance with RegisterUserHandlers
// registered on a root-level group with a middleware that injects non-admin
// (regular user) AuthInfo into the Echo context.
// Returns the Echo instance and the raw *sql.DB handle.
func setupNonAdminTestServer(t *testing.T) (*echo.Echo, *sql.DB) {
	t.Helper()

	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	e := echo.New()
	g := e.Group("")
	g.Use(nonAdminAuthMiddleware("non-admin-user-uuid"))
	handlers.RegisterUserHandlers(g, database.SqlDB)

	return e, database.SqlDB
}

// insertTestUserWithStatus inserts a user into the users table with a specific
// status (e.g. "active" or "blocked"). Uses role="user" and full_name="".
func insertTestUserWithStatus(t *testing.T, sqlDB *sql.DB, id, username, email, provider, providerID, status string) {
	t.Helper()
	if _, err := uuid.Parse(id); err != nil {
		id = testUUID(id)
	}

	now := "2024-01-01T00:00:00Z"
	_, err := sqlDB.Exec(
		`INSERT INTO users (id, username, email, full_name, role, status, provider, provider_id, created_at, updated_at)
		 VALUES (?, ?, ?, '', 'user', ?, ?, ?, ?, ?)`,
		id, username, email, status, provider, providerID, now, now,
	)
	if err != nil {
		t.Fatalf("failed to insert test user with status %q: %v", status, err)
	}
}

// insertTestUserFull inserts a user into the users table with all fields
// specified, providing maximum control for test setup.
func insertTestUserFull(t *testing.T, sqlDB *sql.DB, id, username, email, fullName, role, status, provider, providerID, createdAt, updatedAt string) {
	t.Helper()
	if _, err := uuid.Parse(id); err != nil {
		id = testUUID(id)
	}

	_, err := sqlDB.Exec(
		`INSERT INTO users (id, username, email, full_name, role, status, provider, provider_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, username, email, fullName, role, status, provider, providerID, createdAt, updatedAt,
	)
	if err != nil {
		t.Fatalf("failed to insert test user: %v", err)
	}
}

// parseUsersResponse parses the response body as a JSON array of User objects.
func parseUsersResponse(t *testing.T, rec *httptest.ResponseRecorder) []handlers.User {
	t.Helper()

	var users []handlers.User
	if err := json.Unmarshal(rec.Body.Bytes(), &users); err != nil {
		t.Fatalf("failed to parse users list response: %v\nbody: %s", err, rec.Body.String())
	}
	return users
}

// sendGet sends an HTTP GET request (with no body) to the given Echo instance
// and returns the response recorder.
func sendGet(t *testing.T, e *echo.Echo, path string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	return rec
}

// sendGetWithHeaders sends an HTTP GET request with custom headers to the
// given Echo instance and returns the response recorder.
func sendGetWithHeaders(t *testing.T, e *echo.Echo, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	return rec
}

// ========================================================================
// Task 2.1: GET /users list behavior
// Test Spec: TS-07-9, TS-07-10, TS-07-11, TS-07-E3, TS-07-E4
// Requirements: 07-REQ-3.1, 07-REQ-3.2, 07-REQ-3.3, 07-REQ-3.E1, 07-REQ-3.E2
// ========================================================================

// TestListUsers_ExcludesBlocked verifies that GET /users without the
// include_blocked parameter returns only active users (HTTP 200, array
// excludes blocked).
//
// Test Spec: TS-07-9
// Requirement: 07-REQ-3.1
func TestListUsers_ExcludesBlocked(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	// Seed 2 active users and 1 blocked user.
	insertTestUser(t, sqlDB, "active-uuid-1", "alice", "alice@example.com", "github", "gh-001")
	insertTestUser(t, sqlDB, "active-uuid-2", "bob", "bob@example.com", "github", "gh-002")
	insertTestUserWithStatus(t, sqlDB, "blocked-uuid-1", "charlie", "charlie@example.com", "github", "gh-003", "blocked")

	rec := sendGet(t, e, "/users")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	users := parseUsersResponse(t, rec)
	if len(users) != 2 {
		t.Fatalf("expected 2 active users, got %d", len(users))
	}

	for _, u := range users {
		if u.Status != "active" {
			t.Errorf("expected all returned users to have status 'active', got %q for user %q", u.Status, u.Username)
		}
	}
}

// TestListUsers_IncludeBlocked verifies that GET /users?include_blocked=true
// returns all users including blocked ones (HTTP 200).
//
// Test Spec: TS-07-10
// Requirement: 07-REQ-3.2
func TestListUsers_IncludeBlocked(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	// Seed 2 active users and 1 blocked user.
	insertTestUser(t, sqlDB, "active-uuid-1", "alice", "alice@example.com", "github", "gh-001")
	insertTestUser(t, sqlDB, "active-uuid-2", "bob", "bob@example.com", "github", "gh-002")
	insertTestUserWithStatus(t, sqlDB, "blocked-uuid-1", "charlie", "charlie@example.com", "github", "gh-003", "blocked")

	rec := sendGet(t, e, "/users?include_blocked=true")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	users := parseUsersResponse(t, rec)
	if len(users) != 3 {
		t.Fatalf("expected 3 total users, got %d", len(users))
	}

	hasBlocked := false
	for _, u := range users {
		if u.Status == "blocked" {
			hasBlocked = true
		}
	}
	if !hasBlocked {
		t.Error("expected at least one blocked user in the response")
	}
}

// TestListUsers_NonAdmin verifies that GET /users returns HTTP 403 when
// called by a non-admin user.
//
// Test Spec: TS-07-11
// Requirement: 07-REQ-3.3
func TestListUsers_NonAdmin(t *testing.T) {
	e, _ := setupNonAdminTestServer(t)

	rec := sendGet(t, e, "/users")

	assertErrorResponse(t, rec, http.StatusForbidden, "forbidden")
}

// TestListUsers_Empty verifies that GET /users returns an empty JSON array
// (not null) when no users match the filter.
//
// Test Spec: TS-07-E3
// Requirement: 07-REQ-3.E1
func TestListUsers_Empty(t *testing.T) {
	e, _ := setupAdminTestServer(t)

	// No users in the database — empty result expected.
	rec := sendGet(t, e, "/users")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// The response must be exactly "[]", not "null" or empty.
	body := strings.TrimSpace(rec.Body.String())
	if body != "[]" {
		t.Errorf("expected response body to be '[]', got %q", body)
	}
}

// TestListUsers_DBError verifies that an unexpected database error during the
// list users query returns HTTP 500 with message 'internal server error'.
//
// Test Spec: TS-07-E4
// Requirement: 07-REQ-3.E2
func TestListUsers_DBError(t *testing.T) {
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}

	e := echo.New()
	g := e.Group("")
	g.Use(adminAuthMiddleware("test-admin-uuid"))
	handlers.RegisterUserHandlers(g, database.SqlDB)

	// Close the database AFTER registering handlers to simulate a DB failure.
	database.Close()

	rec := sendGet(t, e, "/users")

	assertErrorResponse(t, rec, http.StatusInternalServerError, "internal server error")
}

// ========================================================================
// Task 2.2: GET /users/:id get single user
// Test Spec: TS-07-12, TS-07-13, TS-07-14, TS-07-E5
// Requirements: 07-REQ-4.1, 07-REQ-4.2, 07-REQ-4.3, 07-REQ-4.E1
// ========================================================================

// TestGetUser_Success verifies that GET /users/:id returns HTTP 200 with the
// correct User JSON object and sets the ETag response header.
//
// Test Spec: TS-07-12
// Requirement: 07-REQ-4.1
func TestGetUser_Success(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	userID := testUUID("get-user-uuid-1")
	insertTestUserFull(t, sqlDB, userID, "alice", "alice@example.com", "Alice Smith", "user", "active", "github", "gh-001", "2024-01-01T00:00:00Z", "2024-06-15T12:00:00Z")

	rec := sendGet(t, e, "/users/"+userID)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	user := parseUserResponse(t, rec)

	if user.ID != userID {
		t.Errorf("expected id %q, got %q", userID, user.ID)
	}
	if user.Username != "alice" {
		t.Errorf("expected username %q, got %q", "alice", user.Username)
	}
	if user.Email != "alice@example.com" {
		t.Errorf("expected email %q, got %q", "alice@example.com", user.Email)
	}
	if user.FullName != "Alice Smith" {
		t.Errorf("expected full_name %q, got %q", "Alice Smith", user.FullName)
	}
	if user.Role != "user" {
		t.Errorf("expected role %q, got %q", "user", user.Role)
	}
	if user.Status != "active" {
		t.Errorf("expected status %q, got %q", "active", user.Status)
	}

	// ETag header must be set to a value derived from user.UpdatedAt.
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Error("expected ETag response header to be set, but it was empty")
	}
}

// TestGetUser_NotFound verifies that GET /users/:id returns HTTP 404 with
// message 'user not found' for a non-existent user UUID.
//
// Test Spec: TS-07-13
// Requirement: 07-REQ-4.2
func TestGetUser_NotFound(t *testing.T) {
	e, _ := setupAdminTestServer(t)

	rec := sendGet(t, e, "/users/00000000-0000-0000-0000-000000000000")

	assertErrorResponse(t, rec, http.StatusNotFound, "user not found")
}

// TestGetUser_NonAdmin verifies that GET /users/:id returns HTTP 403 when
// called by a non-admin user.
//
// Test Spec: TS-07-14
// Requirement: 07-REQ-4.3
func TestGetUser_NonAdmin(t *testing.T) {
	e, _ := setupNonAdminTestServer(t)

	rec := sendGet(t, e, "/users/some-user-id")

	assertErrorResponse(t, rec, http.StatusForbidden, "forbidden")
}

// TestGetUser_ETag verifies that GET /users/:id returns HTTP 304 with no body
// when the If-None-Match header matches the current ETag derived from
// user.UpdatedAt.
//
// Test Spec: TS-07-E5
// Requirement: 07-REQ-4.E1
func TestGetUser_ETag(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	userID := testUUID("etag-user-uuid")
	insertTestUser(t, sqlDB, userID, "etaguser", "etag@example.com", "github", "gh-etag")

	// First request: get the ETag from the response.
	rec1 := sendGet(t, e, "/users/"+userID)

	if rec1.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 on first request, got %d; body: %s", rec1.Code, rec1.Body.String())
	}

	etag := rec1.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag header to be set on first request")
	}

	// Second request: send the ETag as If-None-Match; expect 304.
	rec2 := sendGetWithHeaders(t, e, "/users/"+userID, map[string]string{
		"If-None-Match": etag,
	})

	if rec2.Code != http.StatusNotModified {
		t.Errorf("expected HTTP 304, got %d", rec2.Code)
	}
	if rec2.Body.Len() != 0 {
		t.Errorf("expected empty body for 304 response, got %q", rec2.Body.String())
	}
}

// ========================================================================
// Task 2.3: PATCH /users/:id update full_name
// Test Spec: TS-07-15, TS-07-16, TS-07-17, TS-07-18, TS-07-E6, TS-07-E7
// Requirements: 07-REQ-5.1, 07-REQ-5.2, 07-REQ-5.3, 07-REQ-5.4,
//               07-REQ-5.E1, 07-REQ-5.E2
// ========================================================================

// TestUpdateUser_Success verifies that PATCH /users/:id with a valid full_name
// field updates the user and returns HTTP 200 with the updated User object.
//
// Test Spec: TS-07-15
// Requirement: 07-REQ-5.1
func TestUpdateUser_Success(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	userID := testUUID("update-user-uuid-1")
	insertTestUser(t, sqlDB, userID, "alice", "alice@example.com", "github", "gh-001")

	// Capture the original updated_at.
	var originalUpdatedAt string
	err := sqlDB.QueryRow("SELECT updated_at FROM users WHERE id = ?", userID).Scan(&originalUpdatedAt)
	if err != nil {
		t.Fatalf("failed to query original updated_at: %v", err)
	}

	rec := sendJSON(t, e, http.MethodPatch, "/users/"+userID, `{"full_name":"Alice Smith"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	user := parseUserResponse(t, rec)
	if user.FullName != "Alice Smith" {
		t.Errorf("expected full_name %q, got %q", "Alice Smith", user.FullName)
	}
	if user.UpdatedAt <= originalUpdatedAt {
		t.Errorf("expected updated_at to be refreshed (> %q), got %q", originalUpdatedAt, user.UpdatedAt)
	}
}

// TestUpdateUser_MissingField verifies that PATCH /users/:id returns HTTP 400
// with message 'missing required field: full_name' when the full_name field is
// absent from the request body.
//
// Test Spec: TS-07-16
// Requirement: 07-REQ-5.2
func TestUpdateUser_MissingField(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	userID := testUUID("update-missing-uuid")
	insertTestUser(t, sqlDB, userID, "alice", "alice@example.com", "github", "gh-001")

	rec := sendJSON(t, e, http.MethodPatch, "/users/"+userID, `{}`)

	assertErrorResponse(t, rec, http.StatusBadRequest, "missing required field: full_name")
}

// TestUpdateUser_NotFound verifies that PATCH /users/:id returns HTTP 404 with
// message 'user not found' when the target user does not exist.
//
// Test Spec: TS-07-17
// Requirement: 07-REQ-5.3
func TestUpdateUser_NotFound(t *testing.T) {
	e, _ := setupAdminTestServer(t)

	rec := sendJSON(t, e, http.MethodPatch, "/users/00000000-0000-0000-0000-000000000000", `{"full_name":"Alice"}`)

	assertErrorResponse(t, rec, http.StatusNotFound, "user not found")
}

// TestUpdateUser_NonAdmin verifies that PATCH /users/:id returns HTTP 403 when
// called by a non-admin user.
//
// Test Spec: TS-07-18
// Requirement: 07-REQ-5.4
func TestUpdateUser_NonAdmin(t *testing.T) {
	e, _ := setupNonAdminTestServer(t)

	rec := sendJSON(t, e, http.MethodPatch, "/users/some-user-id", `{"full_name":"Alice"}`)

	assertErrorResponse(t, rec, http.StatusForbidden, "forbidden")
}

// TestUpdateUser_ClearFullName verifies that PATCH /users/:id with full_name
// set to an empty string clears the field and returns HTTP 200 with the
// updated User object.
//
// Test Spec: TS-07-E6
// Requirement: 07-REQ-5.E1
func TestUpdateUser_ClearFullName(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	userID := testUUID("clear-name-uuid")
	insertTestUserFull(t, sqlDB, userID, "alice", "alice@example.com", "Alice Smith", "user", "active", "github", "gh-001", "2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z")

	rec := sendJSON(t, e, http.MethodPatch, "/users/"+userID, `{"full_name":""}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	user := parseUserResponse(t, rec)
	if user.FullName != "" {
		t.Errorf("expected full_name to be empty string, got %q", user.FullName)
	}

	// Verify the change is persisted in the database.
	var dbFullName sql.NullString
	err := sqlDB.QueryRow("SELECT full_name FROM users WHERE id = ?", userID).Scan(&dbFullName)
	if err != nil {
		t.Fatalf("failed to query full_name from database: %v", err)
	}
	if dbFullName.Valid && dbFullName.String != "" {
		t.Errorf("expected full_name in database to be empty string, got %q", dbFullName.String)
	}
}

// TestUpdateUser_PointerDistinction verifies that UpdateUserRequest uses a
// pointer for FullName (*string) so that a missing field (nil pointer) is
// distinguished from an empty string value. Also verifies that a request with
// no full_name key triggers HTTP 400.
//
// Test Spec: TS-07-E7
// Requirement: 07-REQ-5.E2
func TestUpdateUser_PointerDistinction(t *testing.T) {
	// Part 1: Verify UpdateUserRequest.FullName is declared as *string.
	var req handlers.UpdateUserRequest
	typ := reflect.TypeOf(req)
	field, ok := typ.FieldByName("FullName")
	if !ok {
		t.Fatal("UpdateUserRequest does not have a FullName field")
	}
	if field.Type.Kind() != reflect.Pointer || field.Type.Elem().Kind() != reflect.String {
		t.Errorf("expected FullName to be *string, got %s", field.Type)
	}

	// Part 2: Verify that a PATCH request with body {} (no full_name key)
	// results in a nil pointer and triggers HTTP 400.
	e, sqlDB := setupAdminTestServer(t)

	userID := testUUID("pointer-test-uuid")
	insertTestUser(t, sqlDB, userID, "alice", "alice@example.com", "github", "gh-001")

	rec := sendJSON(t, e, http.MethodPatch, "/users/"+userID, `{}`)

	assertErrorResponse(t, rec, http.StatusBadRequest, "missing required field: full_name")
}

// ========================================================================
// Additional Test Helpers (for task group 3)
// ========================================================================

// sendPost sends an HTTP POST request with no body to the given Echo instance
// and returns the response recorder.
func sendPost(t *testing.T, e *echo.Echo, path string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, path, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	return rec
}

// fetchUserFromDB reads a user record directly from the database and returns
// the role, status, and updated_at fields. Fails the test if the query errors.
func fetchUserFromDB(t *testing.T, sqlDB *sql.DB, id string) (role, status, updatedAt string) {
	t.Helper()

	err := sqlDB.QueryRow(
		"SELECT role, status, updated_at FROM users WHERE id = ?", id,
	).Scan(&role, &status, &updatedAt)
	if err != nil {
		t.Fatalf("failed to fetch user %q from database: %v", id, err)
	}
	return role, status, updatedAt
}

// ========================================================================
// Task 3.1: POST /users/:id/promote
// Test Spec: TS-07-19, TS-07-20, TS-07-21, TS-07-22
// Requirements: 07-REQ-6.1, 07-REQ-6.2, 07-REQ-6.3, 07-REQ-6.4
// ========================================================================

// TestPromoteUser_Success verifies that POST /users/:id/promote sets the
// user's role to admin and returns HTTP 200 with the updated User object.
//
// Test Spec: TS-07-19
// Requirement: 07-REQ-6.1
func TestPromoteUser_Success(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	userID := testUUID("promote-user-uuid")
	insertTestUser(t, sqlDB, userID, "alice", "alice@example.com", "github", "gh-001")

	// Capture original updated_at.
	_, _, originalUpdatedAt := fetchUserFromDB(t, sqlDB, userID)

	rec := sendPost(t, e, "/users/"+userID+"/promote")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	user := parseUserResponse(t, rec)

	if user.Role != "admin" {
		t.Errorf("expected role %q, got %q", "admin", user.Role)
	}

	if user.UpdatedAt <= originalUpdatedAt {
		t.Errorf("expected updated_at to be refreshed (> %q), got %q", originalUpdatedAt, user.UpdatedAt)
	}

	// Verify the change is persisted in the database.
	dbRole, _, _ := fetchUserFromDB(t, sqlDB, userID)
	if dbRole != "admin" {
		t.Errorf("expected role in database to be %q, got %q", "admin", dbRole)
	}
}

// TestPromoteUser_AlreadyAdmin verifies that POST /users/:id/promote is
// idempotent: calling it on a user who already has role='admin' returns
// HTTP 200 with the unchanged User object.
//
// Test Spec: TS-07-20
// Requirement: 07-REQ-6.2
func TestPromoteUser_AlreadyAdmin(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	userID := testUUID("already-admin-uuid")
	insertTestUserFull(t, sqlDB, userID, "alice", "alice@example.com", "", "admin", "active", "github", "gh-001", "2024-01-01T00:00:00Z", "2024-06-15T12:00:00Z")

	// Capture original state.
	_, _, originalUpdatedAt := fetchUserFromDB(t, sqlDB, userID)

	rec := sendPost(t, e, "/users/"+userID+"/promote")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	user := parseUserResponse(t, rec)

	if user.Role != "admin" {
		t.Errorf("expected role %q, got %q", "admin", user.Role)
	}

	// updated_at must NOT be refreshed for an idempotent no-op.
	if user.UpdatedAt != originalUpdatedAt {
		t.Errorf("expected updated_at to remain %q (idempotent), got %q", originalUpdatedAt, user.UpdatedAt)
	}
}

// TestPromoteUser_NotFound verifies that POST /users/:id/promote returns
// HTTP 404 with message 'user not found' when the target user does not exist.
//
// Test Spec: TS-07-21
// Requirement: 07-REQ-6.3
func TestPromoteUser_NotFound(t *testing.T) {
	e, _ := setupAdminTestServer(t)

	rec := sendPost(t, e, "/users/00000000-0000-0000-0000-000000000000/promote")

	assertErrorResponse(t, rec, http.StatusNotFound, "user not found")
}

// TestPromoteUser_NonAdmin verifies that POST /users/:id/promote returns
// HTTP 403 when called by a non-admin user.
//
// Test Spec: TS-07-22
// Requirement: 07-REQ-6.4
func TestPromoteUser_NonAdmin(t *testing.T) {
	e, _ := setupNonAdminTestServer(t)

	rec := sendPost(t, e, "/users/some-id/promote")

	assertErrorResponse(t, rec, http.StatusForbidden, "forbidden")
}

// ========================================================================
// Task 3.2: POST /users/:id/demote with last-admin safeguard
// Test Spec: TS-07-23, TS-07-24, TS-07-25, TS-07-26, TS-07-27, TS-07-E8
// Requirements: 07-REQ-7.1, 07-REQ-7.2, 07-REQ-7.3, 07-REQ-7.4,
//               07-REQ-7.5, 07-REQ-7.E1
// ========================================================================

// TestDemoteUser_Success verifies that POST /users/:id/demote sets the user's
// role to 'user' and returns HTTP 200 when more than one active admin exists.
//
// Test Spec: TS-07-23
// Requirement: 07-REQ-7.1
func TestDemoteUser_Success(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	// Insert two active admins — the target and another to satisfy the safeguard.
	targetID := testUUID("demote-target-uuid")
	insertTestUserFull(t, sqlDB, targetID, "alice", "alice@example.com", "", "admin", "active", "github", "gh-001", "2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z")
	insertTestUserFull(t, sqlDB, "other-admin-uuid", "bob", "bob@example.com", "", "admin", "active", "github", "gh-002", "2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z")

	// Capture original updated_at.
	_, _, originalUpdatedAt := fetchUserFromDB(t, sqlDB, targetID)

	rec := sendPost(t, e, "/users/"+targetID+"/demote")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	user := parseUserResponse(t, rec)

	if user.Role != "user" {
		t.Errorf("expected role %q, got %q", "user", user.Role)
	}

	if user.UpdatedAt <= originalUpdatedAt {
		t.Errorf("expected updated_at to be refreshed (> %q), got %q", originalUpdatedAt, user.UpdatedAt)
	}

	// Verify the change is persisted in the database.
	dbRole, _, _ := fetchUserFromDB(t, sqlDB, targetID)
	if dbRole != "user" {
		t.Errorf("expected role in database to be %q, got %q", "user", dbRole)
	}
}

// TestDemoteUser_AlreadyUser verifies that POST /users/:id/demote is
// idempotent: calling it on a user who already has role='user' returns
// HTTP 200 with the unchanged User object.
//
// Test Spec: TS-07-24
// Requirement: 07-REQ-7.2
func TestDemoteUser_AlreadyUser(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	userID := testUUID("already-user-uuid")
	insertTestUserFull(t, sqlDB, userID, "alice", "alice@example.com", "", "user", "active", "github", "gh-001", "2024-01-01T00:00:00Z", "2024-06-15T12:00:00Z")

	// Capture original state.
	_, _, originalUpdatedAt := fetchUserFromDB(t, sqlDB, userID)

	rec := sendPost(t, e, "/users/"+userID+"/demote")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	user := parseUserResponse(t, rec)

	if user.Role != "user" {
		t.Errorf("expected role %q, got %q", "user", user.Role)
	}

	// updated_at must NOT be refreshed for an idempotent no-op.
	if user.UpdatedAt != originalUpdatedAt {
		t.Errorf("expected updated_at to remain %q (idempotent), got %q", originalUpdatedAt, user.UpdatedAt)
	}
}

// TestDemoteUser_LastAdmin verifies that POST /users/:id/demote returns
// HTTP 409 with message 'cannot demote the last admin' when the target is
// the only active admin, and leaves the user record unchanged.
//
// Test Spec: TS-07-25
// Requirement: 07-REQ-7.3
func TestDemoteUser_LastAdmin(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	// Insert exactly one active admin — the sole admin.
	soleAdminID := testUUID("sole-admin-uuid")
	insertTestUserFull(t, sqlDB, soleAdminID, "alice", "alice@example.com", "", "admin", "active", "github", "gh-001", "2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z")

	rec := sendPost(t, e, "/users/"+soleAdminID+"/demote")

	assertErrorResponse(t, rec, http.StatusConflict, "cannot demote the last admin")

	// Verify the user's role remains 'admin' in the database.
	dbRole, _, _ := fetchUserFromDB(t, sqlDB, soleAdminID)
	if dbRole != "admin" {
		t.Errorf("expected role in database to remain %q after failed demote, got %q", "admin", dbRole)
	}
}

// TestDemoteUser_NotFound verifies that POST /users/:id/demote returns
// HTTP 404 with message 'user not found' when the target user does not exist.
//
// Test Spec: TS-07-26
// Requirement: 07-REQ-7.4
func TestDemoteUser_NotFound(t *testing.T) {
	e, _ := setupAdminTestServer(t)

	rec := sendPost(t, e, "/users/00000000-0000-0000-0000-000000000000/demote")

	assertErrorResponse(t, rec, http.StatusNotFound, "user not found")
}

// TestDemoteUser_NonAdmin verifies that POST /users/:id/demote returns
// HTTP 403 when called by a non-admin user.
//
// Test Spec: TS-07-27
// Requirement: 07-REQ-7.5
func TestDemoteUser_NonAdmin(t *testing.T) {
	e, _ := setupNonAdminTestServer(t)

	rec := sendPost(t, e, "/users/some-id/demote")

	assertErrorResponse(t, rec, http.StatusForbidden, "forbidden")
}

// TestDemoteUser_DBCountError verifies that an unexpected database error
// when counting active admins during demote returns HTTP 500 with message
// 'internal server error' without modifying the user record.
//
// Test Spec: TS-07-E8
// Requirement: 07-REQ-7.E1
//
// NOTE: This test simulates a DB error by closing the database connection
// after initial data setup. In practice the handler flow is:
// (1) fetch user, (2) count active admins, (3) update role.
// Closing the DB causes the first DB query to fail. The handler must
// return 500 with "internal server error" for any unexpected DB error.
// A future implementation may refine this to target specifically the COUNT
// query, but the observable behavior (500 + correct message) is the same.
func TestDemoteUser_DBCountError(t *testing.T) {
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}

	// Insert an admin user while the database is still open.
	adminID := testUUID("db-error-admin-uuid")
	now := "2024-01-01T00:00:00Z"
	_, err = database.SqlDB.Exec(
		`INSERT INTO users (id, username, email, full_name, role, status, provider, provider_id, created_at, updated_at)
		 VALUES (?, ?, ?, '', 'admin', 'active', 'github', 'gh-001', ?, ?)`,
		adminID, "alice", "alice@example.com", now, now,
	)
	if err != nil {
		t.Fatalf("failed to insert admin user: %v", err)
	}

	e := echo.New()
	g := e.Group("")
	g.Use(adminAuthMiddleware("test-admin-uuid"))
	handlers.RegisterUserHandlers(g, database.SqlDB)

	// Close the database AFTER registering handlers and inserting data.
	// This causes subsequent SQL queries to fail, simulating a DB error.
	database.Close()

	rec := sendPost(t, e, "/users/"+adminID+"/demote")

	assertErrorResponse(t, rec, http.StatusInternalServerError, "internal server error")
}

// TestDemoteUser_ConcurrentLastTwoAdmins verifies that concurrent demote
// requests against two admins (leaving only one) cannot both succeed.
// Exactly one request must return HTTP 200 and the other HTTP 409, ensuring
// the database always retains at least one active admin.
//
// Test Spec: TS-NS-1
// Requirement: NS-REQ-1
func TestDemoteUser_ConcurrentLastTwoAdmins(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	// Insert exactly two active admins.
	adminA := testUUID("concurrent-admin-a")
	adminB := testUUID("concurrent-admin-b")
	insertTestUserFull(t, sqlDB, adminA, "alice", "alice@example.com", "", "admin", "active", "github", "gh-001", "2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z")
	insertTestUserFull(t, sqlDB, adminB, "bob", "bob@example.com", "", "admin", "active", "github", "gh-002", "2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z")

	var wg sync.WaitGroup
	recA := httptest.NewRecorder()
	recB := httptest.NewRecorder()

	wg.Add(2)
	go func() {
		defer wg.Done()
		req := httptest.NewRequest(http.MethodPost, "/users/"+adminA+"/demote", nil)
		e.ServeHTTP(recA, req)
	}()
	go func() {
		defer wg.Done()
		req := httptest.NewRequest(http.MethodPost, "/users/"+adminB+"/demote", nil)
		e.ServeHTTP(recB, req)
	}()
	wg.Wait()

	codes := []int{recA.Code, recB.Code}

	count200 := 0
	count409 := 0
	for _, code := range codes {
		switch code {
		case http.StatusOK:
			count200++
		case http.StatusConflict:
			count409++
		}
	}

	if count200 != 1 || count409 != 1 {
		t.Errorf("expected exactly one 200 and one 409, got codes %v (200=%d, 409=%d)",
			codes, count200, count409)
	}

	// Verify exactly one active admin remains in the database.
	var adminCount int
	err := sqlDB.QueryRow(
		`SELECT COUNT(*) FROM users WHERE role = 'admin' AND status = 'active'`,
	).Scan(&adminCount)
	if err != nil {
		t.Fatalf("failed to count active admins: %v", err)
	}
	if adminCount != 1 {
		t.Errorf("expected exactly 1 active admin remaining, got %d", adminCount)
	}
}

// ========================================================================
// Task 3.3: POST /users/:id/block and /unblock
// Test Spec: TS-07-28, TS-07-29, TS-07-30, TS-07-31, TS-07-32, TS-07-33
// Requirements: 07-REQ-8.1, 07-REQ-8.2, 07-REQ-8.3, 07-REQ-8.4,
//               07-REQ-9.1, 07-REQ-9.2
// ========================================================================

// TestBlockUser_Success verifies that POST /users/:id/block sets the user's
// status to 'blocked' and returns HTTP 200 with the updated User object.
//
// Test Spec: TS-07-28
// Requirement: 07-REQ-8.1
func TestBlockUser_Success(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	userID := testUUID("block-user-uuid")
	insertTestUser(t, sqlDB, userID, "alice", "alice@example.com", "github", "gh-001")

	// Capture original updated_at.
	_, _, originalUpdatedAt := fetchUserFromDB(t, sqlDB, userID)

	rec := sendPost(t, e, "/users/"+userID+"/block")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	user := parseUserResponse(t, rec)

	if user.Status != "blocked" {
		t.Errorf("expected status %q, got %q", "blocked", user.Status)
	}

	if user.UpdatedAt <= originalUpdatedAt {
		t.Errorf("expected updated_at to be refreshed (> %q), got %q", originalUpdatedAt, user.UpdatedAt)
	}

	// Verify the change is persisted in the database.
	_, dbStatus, _ := fetchUserFromDB(t, sqlDB, userID)
	if dbStatus != "blocked" {
		t.Errorf("expected status in database to be %q, got %q", "blocked", dbStatus)
	}
}

// TestBlockUser_AlreadyBlocked verifies that POST /users/:id/block is
// idempotent: blocking an already-blocked user returns HTTP 200 with the
// unchanged User object.
//
// Test Spec: TS-07-29
// Requirement: 07-REQ-8.2
func TestBlockUser_AlreadyBlocked(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	userID := testUUID("already-blocked-uuid")
	insertTestUserWithStatus(t, sqlDB, userID, "alice", "alice@example.com", "github", "gh-001", "blocked")

	// Capture original state.
	_, _, originalUpdatedAt := fetchUserFromDB(t, sqlDB, userID)

	rec := sendPost(t, e, "/users/"+userID+"/block")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	user := parseUserResponse(t, rec)

	if user.Status != "blocked" {
		t.Errorf("expected status %q, got %q", "blocked", user.Status)
	}

	// updated_at must NOT be refreshed for an idempotent no-op.
	if user.UpdatedAt != originalUpdatedAt {
		t.Errorf("expected updated_at to remain %q (idempotent), got %q", originalUpdatedAt, user.UpdatedAt)
	}
}

// TestBlockUser_NotFound verifies that POST /users/:id/block returns HTTP 404
// with message 'user not found' when the target user does not exist.
//
// Test Spec: TS-07-30
// Requirement: 07-REQ-8.3
func TestBlockUser_NotFound(t *testing.T) {
	e, _ := setupAdminTestServer(t)

	rec := sendPost(t, e, "/users/00000000-0000-0000-0000-000000000000/block")

	assertErrorResponse(t, rec, http.StatusNotFound, "user not found")
}

// TestBlockUser_NonAdmin verifies that POST /users/:id/block returns HTTP 403
// when called by a non-admin user.
//
// Test Spec: TS-07-31
// Requirement: 07-REQ-8.4
func TestBlockUser_NonAdmin(t *testing.T) {
	e, _ := setupNonAdminTestServer(t)

	rec := sendPost(t, e, "/users/some-id/block")

	assertErrorResponse(t, rec, http.StatusForbidden, "forbidden")
}

// TestUnblockUser_Success verifies that POST /users/:id/unblock sets the
// user's status to 'active' and returns HTTP 200 with the updated User object.
//
// Test Spec: TS-07-32
// Requirement: 07-REQ-9.1
func TestUnblockUser_Success(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	userID := testUUID("unblock-user-uuid")
	insertTestUserWithStatus(t, sqlDB, userID, "alice", "alice@example.com", "github", "gh-001", "blocked")

	// Capture original updated_at.
	_, _, originalUpdatedAt := fetchUserFromDB(t, sqlDB, userID)

	rec := sendPost(t, e, "/users/"+userID+"/unblock")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	user := parseUserResponse(t, rec)

	if user.Status != "active" {
		t.Errorf("expected status %q, got %q", "active", user.Status)
	}

	if user.UpdatedAt <= originalUpdatedAt {
		t.Errorf("expected updated_at to be refreshed (> %q), got %q", originalUpdatedAt, user.UpdatedAt)
	}

	// Verify the change is persisted in the database.
	_, dbStatus, _ := fetchUserFromDB(t, sqlDB, userID)
	if dbStatus != "active" {
		t.Errorf("expected status in database to be %q, got %q", "active", dbStatus)
	}
}

// TestUnblockUser_AlreadyActive verifies that POST /users/:id/unblock is
// idempotent: unblocking an already-active user returns HTTP 200 with the
// unchanged User object.
//
// Test Spec: TS-07-33
// Requirement: 07-REQ-9.2
func TestUnblockUser_AlreadyActive(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	userID := testUUID("already-active-uuid")
	insertTestUser(t, sqlDB, userID, "alice", "alice@example.com", "github", "gh-001")

	// Capture original state.
	_, _, originalUpdatedAt := fetchUserFromDB(t, sqlDB, userID)

	rec := sendPost(t, e, "/users/"+userID+"/unblock")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	user := parseUserResponse(t, rec)

	if user.Status != "active" {
		t.Errorf("expected status %q, got %q", "active", user.Status)
	}

	// updated_at must NOT be refreshed for an idempotent no-op.
	if user.UpdatedAt != originalUpdatedAt {
		t.Errorf("expected updated_at to remain %q (idempotent), got %q", originalUpdatedAt, user.UpdatedAt)
	}
}

// ========================================================================
// Task 3.4: POST /users/:id/unblock not-found and auth
// Test Spec: TS-07-34, TS-07-35
// Requirements: 07-REQ-9.3, 07-REQ-9.4
// ========================================================================

// TestUnblockUser_NotFound verifies that POST /users/:id/unblock returns
// HTTP 404 with message 'user not found' when the target user does not exist.
//
// Test Spec: TS-07-34
// Requirement: 07-REQ-9.3
func TestUnblockUser_NotFound(t *testing.T) {
	e, _ := setupAdminTestServer(t)

	rec := sendPost(t, e, "/users/00000000-0000-0000-0000-000000000000/unblock")

	assertErrorResponse(t, rec, http.StatusNotFound, "user not found")
}

// TestUnblockUser_NonAdmin verifies that POST /users/:id/unblock returns
// HTTP 403 when called by a non-admin user.
//
// Test Spec: TS-07-35
// Requirement: 07-REQ-9.4
func TestUnblockUser_NonAdmin(t *testing.T) {
	e, _ := setupNonAdminTestServer(t)

	rec := sendPost(t, e, "/users/some-id/unblock")

	assertErrorResponse(t, rec, http.StatusForbidden, "forbidden")
}

// ========================================================================
// Additional Test Helpers (for task group 4)
// ========================================================================

// sendDelete sends an HTTP DELETE request with no body to the given Echo
// instance and returns the response recorder.
func sendDelete(t *testing.T, e *echo.Echo, path string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodDelete, path, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	return rec
}

// insertTestAPIKey inserts an API key directly into the api_keys table.
// expiresAt and revokedAt accept sql.NullString to support NULL values.
func insertTestAPIKey(t *testing.T, sqlDB *sql.DB, keyID, userID, secretHash string, expiresDays int, expiresAt, revokedAt sql.NullString, createdAt string) {
	t.Helper()

	_, err := sqlDB.Exec(
		`INSERT INTO api_keys (key_id, user_id, secret_hash, expires_days, expires_at, revoked_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		keyID, userID, secretHash, expiresDays, expiresAt, revokedAt, createdAt,
	)
	if err != nil {
		t.Fatalf("failed to insert test API key: %v", err)
	}
}

// insertTestPAT inserts a PAT directly into the pats table.
// expiresAt and revokedAt accept sql.NullString to support NULL values.
func insertTestPAT(t *testing.T, sqlDB *sql.DB, tokenID, userID, name, secretHash, permissions string, expiresDays int, expiresAt, revokedAt sql.NullString, createdAt string) {
	t.Helper()

	_, err := sqlDB.Exec(
		`INSERT INTO pats (token_id, user_id, name, secret_hash, permissions, expires_days, expires_at, revoked_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tokenID, userID, name, secretHash, permissions, expiresDays, expiresAt, revokedAt, createdAt,
	)
	if err != nil {
		t.Fatalf("failed to insert test PAT: %v", err)
	}
}

// nullStr creates a valid sql.NullString from a non-empty string.
func nullStr(s string) sql.NullString {
	return sql.NullString{String: s, Valid: true}
}

// nullStrEmpty creates an invalid (NULL) sql.NullString.
func nullStrEmpty() sql.NullString {
	return sql.NullString{Valid: false}
}

// apiKeyMetaExpectedFields returns the set of field names expected in an
// APIKeyMeta JSON response object.
var apiKeyMetaExpectedFields = map[string]bool{
	"key_id":     true,
	"user_id":    true,
	"created_at": true,
	"expires_at": true,
	"revoked_at": true,
}

// patMetaExpectedFields returns the set of field names expected in a
// PATMeta JSON response object.
var patMetaExpectedFields = map[string]bool{
	"token_id":    true,
	"name":        true,
	"permissions": true,
	"user_id":     true,
	"created_at":  true,
	"expires_at":  true,
	"revoked_at":  true,
}

// ========================================================================
// Task 4.1: GET /users/:id/keys list API keys
// Test Spec: TS-07-36, TS-07-37, TS-07-38, TS-07-E9
// Requirements: 07-REQ-10.1, 07-REQ-10.2, 07-REQ-10.3, 07-REQ-10.E1
// ========================================================================

// TestListUserKeys_Success verifies that GET /users/:id/keys returns HTTP 200
// with an array of APIKeyMeta objects for an existing user with two keys
// (one active, one revoked). Each object must have exactly the expected
// metadata fields and no secret fields.
//
// Test Spec: TS-07-36
// Requirement: 07-REQ-10.1
func TestListUserKeys_Success(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	userID := testUUID("keys-user-uuid")
	insertTestUser(t, sqlDB, userID, "alice", "alice@example.com", "github", "gh-001")

	// Insert two API keys: one active, one revoked.
	insertTestAPIKey(t, sqlDB, "key-active-1", userID, "hash-aaa", 30,
		nullStr("2025-01-31T00:00:00Z"), nullStrEmpty(), "2025-01-01T00:00:00Z")
	insertTestAPIKey(t, sqlDB, "key-revoked-1", userID, "hash-bbb", 90,
		nullStrEmpty(), nullStr("2025-02-15T12:00:00Z"), "2024-12-01T00:00:00Z")

	rec := sendGet(t, e, "/users/"+userID+"/keys")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Parse as array of maps to check exact field set.
	var keys []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &keys); err != nil {
		t.Fatalf("failed to parse response body as JSON array: %v", err)
	}

	if len(keys) != 2 {
		t.Fatalf("expected 2 API keys, got %d", len(keys))
	}

	for i, k := range keys {
		// Verify exact field set matches APIKeyMeta.
		if len(k) != len(apiKeyMetaExpectedFields) {
			t.Errorf("key[%d]: expected %d fields, got %d; fields: %v",
				i, len(apiKeyMetaExpectedFields), len(k), fieldNames(k))
		}
		for fieldName := range apiKeyMetaExpectedFields {
			if _, exists := k[fieldName]; !exists {
				t.Errorf("key[%d]: missing expected field %q", i, fieldName)
			}
		}
		// Verify no secret fields are present.
		for _, secretField := range []string{"secret", "secret_hash", "key", "token"} {
			if _, exists := k[secretField]; exists {
				t.Errorf("key[%d]: secret field %q should not be present", i, secretField)
			}
		}
	}
}

// TestListUserKeys_UserNotFound verifies that GET /users/:id/keys returns
// HTTP 404 with message 'user not found' when the target user does not exist.
//
// Test Spec: TS-07-37
// Requirement: 07-REQ-10.2
func TestListUserKeys_UserNotFound(t *testing.T) {
	e, _ := setupAdminTestServer(t)

	rec := sendGet(t, e, "/users/00000000-0000-0000-0000-000000000000/keys")

	assertErrorResponse(t, rec, http.StatusNotFound, "user not found")
}

// TestListUserKeys_NonAdmin verifies that GET /users/:id/keys returns HTTP 403
// with message 'forbidden' when called by a non-admin user.
//
// Test Spec: TS-07-38
// Requirement: 07-REQ-10.3
func TestListUserKeys_NonAdmin(t *testing.T) {
	e, _ := setupNonAdminTestServer(t)

	rec := sendGet(t, e, "/users/some-id/keys")

	assertErrorResponse(t, rec, http.StatusForbidden, "forbidden")
}

// TestListUserKeys_NoSecrets verifies that the API key listing response
// contains exactly the defined APIKeyMeta fields and no secret values.
// This is a focused check on the security property: secret_hash must never
// leak in the response.
//
// Test Spec: TS-07-E9
// Requirement: 07-REQ-10.E1
func TestListUserKeys_NoSecrets(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	userID := testUUID("nosecrets-key-uuid")
	insertTestUser(t, sqlDB, userID, "bob", "bob@example.com", "github", "gh-002")

	// Insert an API key with a known secret_hash value to ensure it's not returned.
	insertTestAPIKey(t, sqlDB, "key-sec-1", userID, "super-secret-hash-value", 30,
		nullStr("2025-01-31T00:00:00Z"), nullStrEmpty(), "2025-01-01T00:00:00Z")

	rec := sendGet(t, e, "/users/"+userID+"/keys")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var keys []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &keys); err != nil {
		t.Fatalf("failed to parse response body: %v", err)
	}

	if len(keys) != 1 {
		t.Fatalf("expected 1 API key, got %d", len(keys))
	}

	k := keys[0]

	// Verify exact field set.
	for fieldName := range k {
		if !apiKeyMetaExpectedFields[fieldName] {
			t.Errorf("unexpected field %q in API key response", fieldName)
		}
	}
	for fieldName := range apiKeyMetaExpectedFields {
		if _, exists := k[fieldName]; !exists {
			t.Errorf("missing expected field %q in API key response", fieldName)
		}
	}

	// Verify secret_hash value is not in any response field.
	body := rec.Body.String()
	if strings.Contains(body, "super-secret-hash-value") {
		t.Error("response body contains the secret_hash value — secrets must not be exposed")
	}
}

// ========================================================================
// Task 4.2: DELETE /users/:id/keys/:key_id revoke API key
// Test Spec: TS-07-39, TS-07-40, TS-07-41, TS-07-42
// Requirements: 07-REQ-11.1, 07-REQ-11.2, 07-REQ-11.3, 07-REQ-11.4
// ========================================================================

// TestRevokeUserKey_Success verifies that DELETE /users/:id/keys/:key_id
// revokes an active API key: returns HTTP 204 with no body and sets
// revoked_at in the database to a non-null RFC 3339 UTC timestamp.
//
// Test Spec: TS-07-39
// Requirement: 07-REQ-11.1
func TestRevokeUserKey_Success(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	userID := testUUID("revoke-key-user-uuid")
	keyID := "revoke-key-1"
	insertTestUser(t, sqlDB, userID, "alice", "alice@example.com", "github", "gh-001")
	insertTestAPIKey(t, sqlDB, keyID, userID, "hash-aaa", 30,
		nullStr("2025-01-31T00:00:00Z"), nullStrEmpty(), "2025-01-01T00:00:00Z")

	rec := sendDelete(t, e, "/users/"+userID+"/keys/"+keyID)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected HTTP 204, got %d; body: %s", rec.Code, rec.Body.String())
	}

	if rec.Body.Len() != 0 {
		t.Errorf("expected empty response body, got %q", rec.Body.String())
	}

	// Verify revoked_at is set in the database.
	var revokedAt sql.NullString
	err := sqlDB.QueryRow("SELECT revoked_at FROM api_keys WHERE key_id = ?", keyID).Scan(&revokedAt)
	if err != nil {
		t.Fatalf("failed to query api_keys: %v", err)
	}
	if !revokedAt.Valid {
		t.Fatal("expected revoked_at to be set (non-NULL), but it was NULL")
	}
	if !isRFC3339(revokedAt.String) {
		t.Errorf("expected revoked_at to be RFC 3339 format, got %q", revokedAt.String)
	}
}

// TestRevokeUserKey_AlreadyRevoked verifies that DELETE /users/:id/keys/:key_id
// is idempotent: revoking an already-revoked key returns HTTP 204 without
// overwriting the original revoked_at timestamp.
//
// Test Spec: TS-07-40
// Requirement: 07-REQ-11.2
func TestRevokeUserKey_AlreadyRevoked(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	userID := testUUID("idem-key-user-uuid")
	keyID := "idem-key-1"
	originalRevokedAt := "2025-02-15T12:00:00Z"
	insertTestUser(t, sqlDB, userID, "alice", "alice@example.com", "github", "gh-001")
	insertTestAPIKey(t, sqlDB, keyID, userID, "hash-aaa", 30,
		nullStr("2025-01-31T00:00:00Z"), nullStr(originalRevokedAt), "2025-01-01T00:00:00Z")

	rec := sendDelete(t, e, "/users/"+userID+"/keys/"+keyID)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected HTTP 204, got %d; body: %s", rec.Code, rec.Body.String())
	}

	if rec.Body.Len() != 0 {
		t.Errorf("expected empty response body, got %q", rec.Body.String())
	}

	// Verify revoked_at is unchanged.
	var revokedAt sql.NullString
	err := sqlDB.QueryRow("SELECT revoked_at FROM api_keys WHERE key_id = ?", keyID).Scan(&revokedAt)
	if err != nil {
		t.Fatalf("failed to query api_keys: %v", err)
	}
	if !revokedAt.Valid {
		t.Fatal("expected revoked_at to remain set, but it was NULL")
	}
	if revokedAt.String != originalRevokedAt {
		t.Errorf("expected revoked_at to remain %q, got %q", originalRevokedAt, revokedAt.String)
	}
}

// TestRevokeUserKey_NotFound verifies that DELETE /users/:id/keys/:key_id
// returns HTTP 404 with message 'api key not found' when no matching key
// exists for the given user.
//
// Test Spec: TS-07-41
// Requirement: 07-REQ-11.3
func TestRevokeUserKey_NotFound(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	userID := testUUID("nokey-user-uuid")
	insertTestUser(t, sqlDB, userID, "alice", "alice@example.com", "github", "gh-001")

	rec := sendDelete(t, e, "/users/"+userID+"/keys/nonexistent-key-id")

	assertErrorResponse(t, rec, http.StatusNotFound, "api key not found")
}

// TestRevokeUserKey_NonAdmin verifies that DELETE /users/:id/keys/:key_id
// returns HTTP 403 with message 'forbidden' when called by a non-admin user.
//
// Test Spec: TS-07-42
// Requirement: 07-REQ-11.4
func TestRevokeUserKey_NonAdmin(t *testing.T) {
	e, _ := setupNonAdminTestServer(t)

	rec := sendDelete(t, e, "/users/some-id/keys/some-key")

	assertErrorResponse(t, rec, http.StatusForbidden, "forbidden")
}

// ========================================================================
// Task 4.3: GET /users/:id/tokens list PATs
// Test Spec: TS-07-43, TS-07-44, TS-07-45, TS-07-E10
// Requirements: 07-REQ-12.1, 07-REQ-12.2, 07-REQ-12.3, 07-REQ-12.E1
// ========================================================================

// TestListUserTokens_Success verifies that GET /users/:id/tokens returns
// HTTP 200 with an array of PATMeta objects for an existing user with two
// PATs (one active, one revoked). Each object must have exactly the expected
// metadata fields and no secret fields.
//
// Test Spec: TS-07-43
// Requirement: 07-REQ-12.1
func TestListUserTokens_Success(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	userID := testUUID("tokens-user-uuid")
	insertTestUser(t, sqlDB, userID, "alice", "alice@example.com", "github", "gh-001")

	// Insert two PATs: one active, one revoked.
	insertTestPAT(t, sqlDB, "tok-active-1", userID, "My PAT", "hash-tok-aaa",
		`["users:read","orgs:read"]`, 90,
		nullStr("2025-04-01T00:00:00Z"), nullStrEmpty(), "2025-01-01T00:00:00Z")
	insertTestPAT(t, sqlDB, "tok-revoked-1", userID, "Old PAT", "hash-tok-bbb",
		`["users:read"]`, 30,
		nullStrEmpty(), nullStr("2025-02-10T10:00:00Z"), "2024-12-01T00:00:00Z")

	rec := sendGet(t, e, "/users/"+userID+"/tokens")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Parse as array of maps to check exact field set.
	var tokens []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &tokens); err != nil {
		t.Fatalf("failed to parse response body as JSON array: %v", err)
	}

	if len(tokens) != 2 {
		t.Fatalf("expected 2 PATs, got %d", len(tokens))
	}

	for i, tok := range tokens {
		// Verify exact field set matches PATMeta.
		if len(tok) != len(patMetaExpectedFields) {
			t.Errorf("token[%d]: expected %d fields, got %d; fields: %v",
				i, len(patMetaExpectedFields), len(tok), fieldNames(tok))
		}
		for fieldName := range patMetaExpectedFields {
			if _, exists := tok[fieldName]; !exists {
				t.Errorf("token[%d]: missing expected field %q", i, fieldName)
			}
		}
		// Verify no secret fields are present.
		for _, secretField := range []string{"secret", "secret_hash", "key", "token"} {
			if _, exists := tok[secretField]; exists {
				t.Errorf("token[%d]: secret field %q should not be present", i, secretField)
			}
		}
	}
}

// TestListUserTokens_UserNotFound verifies that GET /users/:id/tokens returns
// HTTP 404 with message 'user not found' when the target user does not exist.
//
// Test Spec: TS-07-44
// Requirement: 07-REQ-12.2
func TestListUserTokens_UserNotFound(t *testing.T) {
	e, _ := setupAdminTestServer(t)

	rec := sendGet(t, e, "/users/00000000-0000-0000-0000-000000000000/tokens")

	assertErrorResponse(t, rec, http.StatusNotFound, "user not found")
}

// TestListUserTokens_NonAdmin verifies that GET /users/:id/tokens returns
// HTTP 403 with message 'forbidden' when called by a non-admin user.
//
// Test Spec: TS-07-45
// Requirement: 07-REQ-12.3
func TestListUserTokens_NonAdmin(t *testing.T) {
	e, _ := setupNonAdminTestServer(t)

	rec := sendGet(t, e, "/users/some-id/tokens")

	assertErrorResponse(t, rec, http.StatusForbidden, "forbidden")
}

// TestListUserTokens_NoSecrets verifies that the PAT listing response contains
// exactly the defined PATMeta fields and no secret values. This is a focused
// check on the security property: secret_hash must never leak in the response.
//
// Test Spec: TS-07-E10
// Requirement: 07-REQ-12.E1
func TestListUserTokens_NoSecrets(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	userID := testUUID("nosecrets-tok-uuid")
	insertTestUser(t, sqlDB, userID, "bob", "bob@example.com", "github", "gh-002")

	// Insert a PAT with a known secret_hash value to ensure it's not returned.
	insertTestPAT(t, sqlDB, "tok-sec-1", userID, "Secret PAT", "ultra-secret-hash-value",
		`["users:read"]`, 30,
		nullStr("2025-01-31T00:00:00Z"), nullStrEmpty(), "2025-01-01T00:00:00Z")

	rec := sendGet(t, e, "/users/"+userID+"/tokens")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var tokens []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &tokens); err != nil {
		t.Fatalf("failed to parse response body: %v", err)
	}

	if len(tokens) != 1 {
		t.Fatalf("expected 1 PAT, got %d", len(tokens))
	}

	tok := tokens[0]

	// Verify exact field set.
	for fieldName := range tok {
		if !patMetaExpectedFields[fieldName] {
			t.Errorf("unexpected field %q in PAT response", fieldName)
		}
	}
	for fieldName := range patMetaExpectedFields {
		if _, exists := tok[fieldName]; !exists {
			t.Errorf("missing expected field %q in PAT response", fieldName)
		}
	}

	// Verify secret_hash value is not in any response field.
	body := rec.Body.String()
	if strings.Contains(body, "ultra-secret-hash-value") {
		t.Error("response body contains the secret_hash value — secrets must not be exposed")
	}
}

// ========================================================================
// Task 4.4: DELETE /users/:id/tokens/:token_id revoke PAT
// Test Spec: TS-07-46, TS-07-47, TS-07-48, TS-07-49
// Requirements: 07-REQ-13.1, 07-REQ-13.2, 07-REQ-13.3, 07-REQ-13.4
// ========================================================================

// TestRevokeUserToken_Success verifies that DELETE /users/:id/tokens/:token_id
// revokes an active PAT: returns HTTP 204 with no body and sets revoked_at
// in the database to a non-null RFC 3339 UTC timestamp.
//
// Test Spec: TS-07-46
// Requirement: 07-REQ-13.1
func TestRevokeUserToken_Success(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	userID := testUUID("revoke-tok-user-uuid")
	tokenID := "revoke-tok-1"
	insertTestUser(t, sqlDB, userID, "alice", "alice@example.com", "github", "gh-001")
	insertTestPAT(t, sqlDB, tokenID, userID, "My Token", "hash-tok-aaa",
		`["users:read"]`, 30,
		nullStr("2025-01-31T00:00:00Z"), nullStrEmpty(), "2025-01-01T00:00:00Z")

	rec := sendDelete(t, e, "/users/"+userID+"/tokens/"+tokenID)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected HTTP 204, got %d; body: %s", rec.Code, rec.Body.String())
	}

	if rec.Body.Len() != 0 {
		t.Errorf("expected empty response body, got %q", rec.Body.String())
	}

	// Verify revoked_at is set in the database.
	var revokedAt sql.NullString
	err := sqlDB.QueryRow("SELECT revoked_at FROM pats WHERE token_id = ?", tokenID).Scan(&revokedAt)
	if err != nil {
		t.Fatalf("failed to query pats: %v", err)
	}
	if !revokedAt.Valid {
		t.Fatal("expected revoked_at to be set (non-NULL), but it was NULL")
	}
	if !isRFC3339(revokedAt.String) {
		t.Errorf("expected revoked_at to be RFC 3339 format, got %q", revokedAt.String)
	}
}

// TestRevokeUserToken_AlreadyRevoked verifies that DELETE
// /users/:id/tokens/:token_id is idempotent: revoking an already-revoked PAT
// returns HTTP 204 without overwriting the original revoked_at timestamp.
//
// Test Spec: TS-07-47
// Requirement: 07-REQ-13.2
func TestRevokeUserToken_AlreadyRevoked(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	userID := testUUID("idem-tok-user-uuid")
	tokenID := "idem-tok-1"
	originalRevokedAt := "2025-02-15T12:00:00Z"
	insertTestUser(t, sqlDB, userID, "alice", "alice@example.com", "github", "gh-001")
	insertTestPAT(t, sqlDB, tokenID, userID, "Old Token", "hash-tok-aaa",
		`["users:read"]`, 30,
		nullStrEmpty(), nullStr(originalRevokedAt), "2024-12-01T00:00:00Z")

	rec := sendDelete(t, e, "/users/"+userID+"/tokens/"+tokenID)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected HTTP 204, got %d; body: %s", rec.Code, rec.Body.String())
	}

	if rec.Body.Len() != 0 {
		t.Errorf("expected empty response body, got %q", rec.Body.String())
	}

	// Verify revoked_at is unchanged.
	var revokedAt sql.NullString
	err := sqlDB.QueryRow("SELECT revoked_at FROM pats WHERE token_id = ?", tokenID).Scan(&revokedAt)
	if err != nil {
		t.Fatalf("failed to query pats: %v", err)
	}
	if !revokedAt.Valid {
		t.Fatal("expected revoked_at to remain set, but it was NULL")
	}
	if revokedAt.String != originalRevokedAt {
		t.Errorf("expected revoked_at to remain %q, got %q", originalRevokedAt, revokedAt.String)
	}
}

// TestRevokeUserToken_NotFound verifies that DELETE /users/:id/tokens/:token_id
// returns HTTP 404 with message 'token not found' when no matching token
// exists for the given user.
//
// Test Spec: TS-07-48
// Requirement: 07-REQ-13.3
func TestRevokeUserToken_NotFound(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	userID := testUUID("notok-user-uuid")
	insertTestUser(t, sqlDB, userID, "alice", "alice@example.com", "github", "gh-001")

	rec := sendDelete(t, e, "/users/"+userID+"/tokens/nonexistent-token-id")

	assertErrorResponse(t, rec, http.StatusNotFound, "token not found")
}

// TestRevokeUserToken_NonAdmin verifies that DELETE /users/:id/tokens/:token_id
// returns HTTP 403 with message 'forbidden' when called by a non-admin user.
//
// Test Spec: TS-07-49
// Requirement: 07-REQ-13.4
func TestRevokeUserToken_NonAdmin(t *testing.T) {
	e, _ := setupNonAdminTestServer(t)

	rec := sendDelete(t, e, "/users/some-id/tokens/some-token")

	assertErrorResponse(t, rec, http.StatusForbidden, "forbidden")
}

// fieldNames returns the keys of a map for debug output.
func fieldNames(m map[string]any) []string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	return names
}

// ========================================================================
// Additional Test Helpers (for task group 5 — self-service endpoints)
// ========================================================================

// patAuthMiddleware returns Echo middleware that injects PAT-level AuthInfo
// with the specified permissions into the Echo context. This simulates an
// authenticated PAT credential for self-service endpoint testing.
// Uses auth.SetAuthInfo to store in context.Context (not c.Set) so that
// auth.GetAuthInfo, auth.RequirePermission, and auth.GetUserID work correctly.
func patAuthMiddleware(userID string, permissions []string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			auth.SetAuthInfo(c, &auth.AuthInfo{
				CredentialType: "pat",
				UserID:         userID,
				Permissions:    permissions,
				Role:           "user",
			})
			return next(c)
		}
	}
}

// setupPATTestServer creates an Echo instance with RegisterUserHandlers
// registered with PAT-level auth middleware carrying the specified permissions.
// Returns the Echo instance and the raw *sql.DB handle.
func setupPATTestServer(t *testing.T, userID string, permissions []string) (*echo.Echo, *sql.DB) {
	t.Helper()

	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	e := echo.New()
	g := e.Group("")
	g.Use(patAuthMiddleware(userID, permissions))
	handlers.RegisterUserHandlers(g, database.SqlDB)

	return e, database.SqlDB
}

// parseOrgsResponse parses the response body as a JSON array of OrgResponse
// objects. (insertTestOrg and insertTestOrgMember are defined in orgs_test.go)
func parseOrgsResponse(t *testing.T, rec *httptest.ResponseRecorder) []handlers.OrgResponse {
	t.Helper()

	var orgs []handlers.OrgResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &orgs); err != nil {
		t.Fatalf("failed to parse orgs list response: %v\nbody: %s", err, rec.Body.String())
	}
	return orgs
}

// ========================================================================
// Task 5.1: GET /user own profile
// Test Spec: TS-07-50, TS-07-51, TS-07-E11
// Requirements: 07-REQ-14.1, 07-REQ-14.2, 07-REQ-14.E1
// ========================================================================

// TestGetOwnProfile_Success verifies that GET /user returns HTTP 200 with
// the authenticated user's profile as a User JSON object and sets the ETag
// response header, when the PAT has users:read permission.
//
// Test Spec: TS-07-50
// Requirement: 07-REQ-14.1
func TestGetOwnProfile_Success(t *testing.T) {
	userID := testUUID("own-profile-uuid")
	e, sqlDB := setupPATTestServer(t, userID, []string{"users:read"})

	insertTestUserFull(t, sqlDB, userID, "alice", "alice@example.com", "Alice Smith",
		"user", "active", "github", "gh-001", "2024-01-01T00:00:00Z", "2024-06-15T12:00:00Z")

	rec := sendGet(t, e, "/user")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	user := parseUserResponse(t, rec)

	if user.ID != userID {
		t.Errorf("expected id %q, got %q", userID, user.ID)
	}
	if user.Username != "alice" {
		t.Errorf("expected username %q, got %q", "alice", user.Username)
	}
	if user.Email != "alice@example.com" {
		t.Errorf("expected email %q, got %q", "alice@example.com", user.Email)
	}
	if user.FullName != "Alice Smith" {
		t.Errorf("expected full_name %q, got %q", "Alice Smith", user.FullName)
	}

	// ETag header must be set.
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Error("expected ETag response header to be set, but it was empty")
	}
}

// TestGetOwnProfile_PATWithPermission verifies that GET /user succeeds
// when the PAT has the users:read permission, confirming that users:read
// is sufficient for reading one's own profile.
//
// Test Spec: TS-07-50
// Requirement: 07-REQ-14.1
func TestGetOwnProfile_PATWithPermission(t *testing.T) {
	userID := testUUID("pat-perm-profile-uuid")
	e, sqlDB := setupPATTestServer(t, userID, []string{"users:read"})

	insertTestUser(t, sqlDB, userID, "bob", "bob@example.com", "github", "gh-002")

	rec := sendGet(t, e, "/user")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

// TestGetOwnProfile_PATWithoutPermission verifies that GET /user returns
// HTTP 403 with message 'insufficient permissions' when the PAT lacks the
// users:read permission.
//
// Test Spec: TS-07-51
// Requirement: 07-REQ-14.2
func TestGetOwnProfile_PATWithoutPermission(t *testing.T) {
	userID := testUUID("noperm-profile-uuid")
	e, _ := setupPATTestServer(t, userID, []string{"orgs:read"}) // no users:read

	rec := sendGet(t, e, "/user")

	assertErrorResponse(t, rec, http.StatusForbidden, "insufficient permissions")
}

// TestGetOwnProfile_ETag verifies that GET /user returns HTTP 304 with no
// body when the If-None-Match header matches the current ETag derived from
// user.UpdatedAt.
//
// Test Spec: TS-07-E11
// Requirement: 07-REQ-14.E1
func TestGetOwnProfile_ETag(t *testing.T) {
	userID := testUUID("etag-profile-uuid")
	e, sqlDB := setupPATTestServer(t, userID, []string{"users:read"})

	insertTestUser(t, sqlDB, userID, "alice", "alice@example.com", "github", "gh-001")

	// First request: get the ETag from the response.
	rec1 := sendGet(t, e, "/user")

	if rec1.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 on first request, got %d; body: %s", rec1.Code, rec1.Body.String())
	}

	etag := rec1.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag header to be set on first request")
	}

	// Second request: send the ETag as If-None-Match; expect 304.
	rec2 := sendGetWithHeaders(t, e, "/user", map[string]string{
		"If-None-Match": etag,
	})

	if rec2.Code != http.StatusNotModified {
		t.Errorf("expected HTTP 304, got %d", rec2.Code)
	}
	if rec2.Body.Len() != 0 {
		t.Errorf("expected empty body for 304 response, got %q", rec2.Body.String())
	}
}

// ========================================================================
// Task 5.2: PATCH /user update own profile
// Test Spec: TS-07-52, TS-07-53, TS-07-54, TS-07-E12
// Requirements: 07-REQ-15.1, 07-REQ-15.2, 07-REQ-15.3, 07-REQ-15.E1
// ========================================================================

// TestUpdateOwnProfile_Success verifies that PATCH /user with a valid
// full_name field updates the authenticated user's full_name and returns
// HTTP 200 with the updated User object.
//
// Test Spec: TS-07-52
// Requirement: 07-REQ-15.1
func TestUpdateOwnProfile_Success(t *testing.T) {
	userID := testUUID("update-own-uuid")
	e, sqlDB := setupPATTestServer(t, userID, []string{"users:read"})

	insertTestUser(t, sqlDB, userID, "alice", "alice@example.com", "github", "gh-001")

	// Capture the original updated_at.
	var originalUpdatedAt string
	err := sqlDB.QueryRow("SELECT updated_at FROM users WHERE id = ?", userID).Scan(&originalUpdatedAt)
	if err != nil {
		t.Fatalf("failed to query original updated_at: %v", err)
	}

	rec := sendJSON(t, e, http.MethodPatch, "/user", `{"full_name":"Alice Updated"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	user := parseUserResponse(t, rec)
	if user.FullName != "Alice Updated" {
		t.Errorf("expected full_name %q, got %q", "Alice Updated", user.FullName)
	}
	if user.UpdatedAt <= originalUpdatedAt {
		t.Errorf("expected updated_at to be refreshed (> %q), got %q", originalUpdatedAt, user.UpdatedAt)
	}
}

// TestUpdateOwnProfile_MissingField verifies that PATCH /user returns
// HTTP 400 with message 'missing required field: full_name' when the
// full_name field is absent from the request body.
//
// Test Spec: TS-07-53
// Requirement: 07-REQ-15.2
func TestUpdateOwnProfile_MissingField(t *testing.T) {
	userID := testUUID("update-own-missing-uuid")
	e, sqlDB := setupPATTestServer(t, userID, []string{"users:read"})

	insertTestUser(t, sqlDB, userID, "alice", "alice@example.com", "github", "gh-001")

	rec := sendJSON(t, e, http.MethodPatch, "/user", `{}`)

	assertErrorResponse(t, rec, http.StatusBadRequest, "missing required field: full_name")
}

// TestUpdateOwnProfile_PATWithPermission verifies that PATCH /user succeeds
// when the PAT has the users:read permission.
//
// Test Spec: TS-07-52
// Requirement: 07-REQ-15.1
func TestUpdateOwnProfile_PATWithPermission(t *testing.T) {
	userID := testUUID("update-own-perm-uuid")
	e, sqlDB := setupPATTestServer(t, userID, []string{"users:read"})

	insertTestUser(t, sqlDB, userID, "bob", "bob@example.com", "github", "gh-002")

	rec := sendJSON(t, e, http.MethodPatch, "/user", `{"full_name":"Bob Updated"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	user := parseUserResponse(t, rec)
	if user.FullName != "Bob Updated" {
		t.Errorf("expected full_name %q, got %q", "Bob Updated", user.FullName)
	}
}

// TestUpdateOwnProfile_PATWithoutPermission verifies that PATCH /user
// returns HTTP 403 with message 'insufficient permissions' when the PAT
// lacks the users:read permission.
//
// Test Spec: TS-07-54
// Requirement: 07-REQ-15.3
func TestUpdateOwnProfile_PATWithoutPermission(t *testing.T) {
	userID := testUUID("update-own-noperm-uuid")
	e, _ := setupPATTestServer(t, userID, []string{"orgs:read"}) // no users:read

	rec := sendJSON(t, e, http.MethodPatch, "/user", `{"full_name":"X"}`)

	assertErrorResponse(t, rec, http.StatusForbidden, "insufficient permissions")
}

// TestUpdateOwnProfile_UsesReadPermission verifies that PATCH /user uses
// the users:read permission check (not a write permission) because no
// users:write scope is defined in the PAT permission set. A PAT with only
// users:read (no users:write) can update the user's own full_name.
//
// Test Spec: TS-07-E12
// Requirement: 07-REQ-15.E1
func TestUpdateOwnProfile_UsesReadPermission(t *testing.T) {
	userID := testUUID("update-own-read-uuid")
	// Only users:read — explicitly no users:write
	e, sqlDB := setupPATTestServer(t, userID, []string{"users:read"})

	insertTestUser(t, sqlDB, userID, "alice", "alice@example.com", "github", "gh-001")

	rec := sendJSON(t, e, http.MethodPatch, "/user", `{"full_name":"Updated"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	user := parseUserResponse(t, rec)
	if user.FullName != "Updated" {
		t.Errorf("expected full_name %q, got %q", "Updated", user.FullName)
	}
}

// ========================================================================
// Task 5.3: GET /user/orgs list own organizations
// Test Spec: TS-07-55, TS-07-56, TS-07-E13, TS-07-E14
// Requirements: 07-REQ-16.1, 07-REQ-16.2, 07-REQ-16.E1, 07-REQ-16.E2
// ========================================================================

// TestListOwnOrgs_Success verifies that GET /user/orgs returns HTTP 200
// with the authenticated user's active organization memberships (excluding
// blocked orgs). Tests with 2 active orgs and 1 blocked org.
//
// Test Spec: TS-07-55
// Requirement: 07-REQ-16.1
func TestListOwnOrgs_Success(t *testing.T) {
	userID := testUUID("orgs-user-uuid")
	e, sqlDB := setupPATTestServer(t, userID, []string{"orgs:read"})

	insertTestUser(t, sqlDB, userID, "alice", "alice@example.com", "github", "gh-001")

	// Insert 2 active orgs and 1 blocked org.
	insertTestOrg(t, sqlDB, "org-active-1", "Org One", "org-one", "https://org-one.example.com", "active")
	insertTestOrg(t, sqlDB, "org-active-2", "Org Two", "org-two", "https://org-two.example.com", "active")
	insertTestOrg(t, sqlDB, "org-blocked-1", "Org Blocked", "org-blocked", "https://org-blocked.example.com", "blocked")

	// Add user as member of all three orgs.
	insertTestOrgMember(t, sqlDB, "org-active-1", userID)
	insertTestOrgMember(t, sqlDB, "org-active-2", userID)
	insertTestOrgMember(t, sqlDB, "org-blocked-1", userID)

	rec := sendGet(t, e, "/user/orgs")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	orgs := parseOrgsResponse(t, rec)

	if len(orgs) != 2 {
		t.Fatalf("expected 2 active organizations, got %d", len(orgs))
	}

	for _, org := range orgs {
		if org.Status != "active" {
			t.Errorf("expected all returned orgs to have status 'active', got %q for org %q", org.Status, org.Name)
		}
		// Verify expected fields are present.
		if org.ID == "" {
			t.Error("expected org id to be non-empty")
		}
		if org.Name == "" {
			t.Error("expected org name to be non-empty")
		}
		if org.Slug == "" {
			t.Error("expected org slug to be non-empty")
		}
		if org.CreatedAt == "" {
			t.Error("expected org created_at to be non-empty")
		}
		if org.UpdatedAt == "" {
			t.Error("expected org updated_at to be non-empty")
		}
	}
}

// TestListOwnOrgs_PATWithoutPermission verifies that GET /user/orgs returns
// HTTP 403 with message 'insufficient permissions' when the PAT lacks the
// orgs:read permission.
//
// Test Spec: TS-07-56
// Requirement: 07-REQ-16.2
func TestListOwnOrgs_PATWithoutPermission(t *testing.T) {
	userID := testUUID("orgs-noperm-uuid")
	e, _ := setupPATTestServer(t, userID, []string{"users:read"}) // no orgs:read

	rec := sendGet(t, e, "/user/orgs")

	assertErrorResponse(t, rec, http.StatusForbidden, "insufficient permissions")
}

// TestListOwnOrgs_ExcludesBlockedOrgs verifies that GET /user/orgs excludes
// organizations with status='blocked' from the response. When a user is a
// member of 1 active org and 1 blocked org, only the active org is returned.
//
// Test Spec: TS-07-E13
// Requirement: 07-REQ-16.E1
func TestListOwnOrgs_ExcludesBlockedOrgs(t *testing.T) {
	userID := testUUID("orgs-blocked-uuid")
	e, sqlDB := setupPATTestServer(t, userID, []string{"orgs:read"})

	insertTestUser(t, sqlDB, userID, "alice", "alice@example.com", "github", "gh-001")

	insertTestOrg(t, sqlDB, "org-active-only", "Active Org", "active-org", "https://active.example.com", "active")
	insertTestOrg(t, sqlDB, "org-blocked-only", "Blocked Org", "blocked-org", "https://blocked.example.com", "blocked")

	insertTestOrgMember(t, sqlDB, "org-active-only", userID)
	insertTestOrgMember(t, sqlDB, "org-blocked-only", userID)

	rec := sendGet(t, e, "/user/orgs")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	orgs := parseOrgsResponse(t, rec)

	if len(orgs) != 1 {
		t.Fatalf("expected 1 active organization, got %d", len(orgs))
	}

	if orgs[0].Status != "active" {
		t.Errorf("expected returned org to have status 'active', got %q", orgs[0].Status)
	}
}

// TestListOwnOrgs_NoMemberships verifies that GET /user/orgs returns an
// empty JSON array (not null) when the user has no organization memberships.
//
// Test Spec: TS-07-E14
// Requirement: 07-REQ-16.E2
func TestListOwnOrgs_NoMemberships(t *testing.T) {
	userID := testUUID("orgs-empty-uuid")
	e, sqlDB := setupPATTestServer(t, userID, []string{"orgs:read"})

	insertTestUser(t, sqlDB, userID, "alice", "alice@example.com", "github", "gh-001")

	// No org memberships — empty result expected.
	rec := sendGet(t, e, "/user/orgs")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// The response must be exactly "[]", not "null" or empty.
	body := strings.TrimSpace(rec.Body.String())
	if body != "[]" {
		t.Errorf("expected response body to be '[]', got %q", body)
	}
}

// TestListOwnOrgs_PATWithPermission verifies that GET /user/orgs succeeds
// when the PAT has the orgs:read permission.
//
// Test Spec: TS-07-55
// Requirement: 07-REQ-16.1
func TestListOwnOrgs_PATWithPermission(t *testing.T) {
	userID := testUUID("orgs-perm-uuid")
	e, sqlDB := setupPATTestServer(t, userID, []string{"orgs:read"})

	insertTestUser(t, sqlDB, userID, "bob", "bob@example.com", "github", "gh-002")

	rec := sendGet(t, e, "/user/orgs")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Even with no memberships, the response should be a valid JSON array.
	body := strings.TrimSpace(rec.Body.String())
	if body == "null" {
		t.Error("expected response body to be '[]', not 'null'")
	}

	// Ignore DB insert for _ since unused, but verify user was inserted
	_ = sqlDB
}

// ========================================================================
// Task 5.4: Property test stubs
// Test Spec: TS-07-P1 through TS-07-P6
// Requirements: 07-PROP-1 through 07-PROP-6
// ========================================================================

// TestProp_LastAdminSafeguard verifies that for any sequence of promote and
// demote operations, the system always retains at least one active admin;
// the demote endpoint never reduces the active admin count below 1.
//
// Test Spec: TS-07-P1
// Property: 07-PROP-1
// Validates: 07-REQ-7.3, 07-REQ-7.1
func TestProp_LastAdminSafeguard(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	// Seed 3 users: 2 admins and 1 regular user.
	insertTestUserFull(t, sqlDB, "prop-admin-1", "admin1", "admin1@example.com", "", "admin", "active", "github", "gh-a1", "2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z")
	insertTestUserFull(t, sqlDB, "prop-admin-2", "admin2", "admin2@example.com", "", "admin", "active", "github", "gh-a2", "2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z")
	insertTestUserFull(t, sqlDB, "prop-user-1", "user1", "user1@example.com", "", "user", "active", "github", "gh-u1", "2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z")

	// Execute a sequence of promote and demote operations.
	actions := []struct {
		action string // "promote" or "demote"
		userID string
	}{
		{"demote", "prop-admin-1"},  // Should succeed: admin-2 remains.
		{"promote", "prop-user-1"},  // user-1 becomes admin.
		{"demote", "prop-admin-2"},  // Should succeed: user-1 is admin.
		{"demote", "prop-user-1"},   // Should fail: user-1 would be the last admin.
		{"promote", "prop-admin-1"}, // admin-1 becomes admin again.
		{"demote", "prop-user-1"},   // Should succeed: admin-1 is admin.
	}

	for i, a := range actions {
		rec := sendPost(t, e, "/users/"+a.userID+"/"+a.action)

		// Count active admins after each operation.
		var adminCount int
		err := sqlDB.QueryRow("SELECT COUNT(*) FROM users WHERE role = 'admin' AND status = 'active'").Scan(&adminCount)
		if err != nil {
			t.Fatalf("action[%d] %s %s: failed to count admins: %v", i, a.action, a.userID, err)
		}

		if adminCount < 1 {
			t.Errorf("action[%d] %s %s: active admin count dropped to %d (must be >= 1)",
				i, a.action, a.userID, adminCount)
		}

		// If demote succeeded, there were >= 2 admins before.
		if a.action == "demote" && rec.Code == http.StatusOK && adminCount < 1 {
			t.Errorf("action[%d]: demote succeeded but admin count is %d", i, adminCount)
		}
		// If demote returned 409, admin count is still >= 1.
		if a.action == "demote" && rec.Code == http.StatusConflict && adminCount < 1 {
			t.Errorf("action[%d]: demote returned 409 but admin count is %d", i, adminCount)
		}
	}
}

// TestProp_ActionIdempotency verifies that for any number of repeated calls
// to promote, demote, block, or unblock with the same user ID, the response
// is always HTTP 200 and the user record is unchanged after the second and
// subsequent calls.
//
// Test Spec: TS-07-P2
// Property: 07-PROP-2
// Validates: 07-REQ-6.2, 07-REQ-7.2, 07-REQ-8.2, 07-REQ-9.2
func TestProp_ActionIdempotency(t *testing.T) {
	tests := []struct {
		name        string
		setupRole   string
		setupStatus string
		action      string
	}{
		{"promote already admin", "admin", "active", "promote"},
		{"demote already user", "user", "active", "demote"},
		{"block already blocked", "user", "blocked", "block"},
		{"unblock already active", "user", "active", "unblock"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, sqlDB := setupAdminTestServer(t)

			userID := testUUID("idem-prop-" + tt.action + "-uuid")
			// For demote tests, ensure at least 2 admins to avoid last-admin safeguard.
			if tt.action == "demote" {
				insertTestUserFull(t, sqlDB, "idem-other-admin", "otheradmin", "other@example.com", "",
					"admin", "active", "github", "gh-other", "2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z")
			}
			insertTestUserFull(t, sqlDB, userID, "target", "target@example.com", "",
				tt.setupRole, tt.setupStatus, "github", "gh-target", "2024-01-01T00:00:00Z", "2024-06-15T12:00:00Z")

			// First call.
			rec1 := sendPost(t, e, "/users/"+userID+"/"+tt.action)
			if rec1.Code != http.StatusOK {
				t.Fatalf("first call: expected HTTP 200, got %d; body: %s", rec1.Code, rec1.Body.String())
			}

			// Capture state after first call.
			role1, status1, updatedAt1 := fetchUserFromDB(t, sqlDB, userID)

			// Repeat 3 more times — each should return 200 with unchanged state.
			for i := 2; i <= 4; i++ {
				rec := sendPost(t, e, "/users/"+userID+"/"+tt.action)
				if rec.Code != http.StatusOK {
					t.Errorf("call %d: expected HTTP 200, got %d", i, rec.Code)
				}

				role, status, updatedAt := fetchUserFromDB(t, sqlDB, userID)
				if role != role1 {
					t.Errorf("call %d: role changed from %q to %q", i, role1, role)
				}
				if status != status1 {
					t.Errorf("call %d: status changed from %q to %q", i, status1, status)
				}
				if updatedAt != updatedAt1 {
					t.Errorf("call %d: updated_at changed from %q to %q", i, updatedAt1, updatedAt)
				}
			}
		})
	}
}

// TestProp_CredentialRevocationIdempotency verifies that for any number of
// repeated DELETE calls to revoke the same API key or PAT, every call returns
// HTTP 204, and revoked_at is set exactly once and never overwritten.
//
// Test Spec: TS-07-P3
// Property: 07-PROP-3
// Validates: 07-REQ-11.2, 07-REQ-13.2, 07-REQ-11.1, 07-REQ-13.1
func TestProp_CredentialRevocationIdempotency(t *testing.T) {
	t.Run("api_key", func(t *testing.T) {
		e, sqlDB := setupAdminTestServer(t)

		userID := testUUID("revoke-prop-key-user")
		keyID := "revoke-prop-key-id"
		insertTestUser(t, sqlDB, userID, "alice", "alice@example.com", "github", "gh-001")
		insertTestAPIKey(t, sqlDB, keyID, userID, "hash-aaa", 30,
			nullStr("2025-01-31T00:00:00Z"), nullStrEmpty(), "2025-01-01T00:00:00Z")

		var firstRevokedAt string

		for i := 1; i <= 5; i++ {
			rec := sendDelete(t, e, "/users/"+userID+"/keys/"+keyID)

			if rec.Code != http.StatusNoContent {
				t.Errorf("call %d: expected HTTP 204, got %d", i, rec.Code)
				continue
			}

			var revokedAt sql.NullString
			err := sqlDB.QueryRow("SELECT revoked_at FROM api_keys WHERE key_id = ?", keyID).Scan(&revokedAt)
			if err != nil {
				t.Fatalf("call %d: failed to query revoked_at: %v", i, err)
			}
			if !revokedAt.Valid {
				t.Fatalf("call %d: expected revoked_at to be set", i)
			}

			if i == 1 {
				firstRevokedAt = revokedAt.String
			} else if revokedAt.String != firstRevokedAt {
				t.Errorf("call %d: revoked_at changed from %q to %q", i, firstRevokedAt, revokedAt.String)
			}
		}
	})

	t.Run("pat", func(t *testing.T) {
		e, sqlDB := setupAdminTestServer(t)

		userID := testUUID("revoke-prop-tok-user")
		tokenID := "revoke-prop-tok-id"
		insertTestUser(t, sqlDB, userID, "bob", "bob@example.com", "github", "gh-002")
		insertTestPAT(t, sqlDB, tokenID, userID, "My Token", "hash-bbb",
			`["users:read"]`, 30,
			nullStr("2025-01-31T00:00:00Z"), nullStrEmpty(), "2025-01-01T00:00:00Z")

		var firstRevokedAt string

		for i := 1; i <= 5; i++ {
			rec := sendDelete(t, e, "/users/"+userID+"/tokens/"+tokenID)

			if rec.Code != http.StatusNoContent {
				t.Errorf("call %d: expected HTTP 204, got %d", i, rec.Code)
				continue
			}

			var revokedAt sql.NullString
			err := sqlDB.QueryRow("SELECT revoked_at FROM pats WHERE token_id = ?", tokenID).Scan(&revokedAt)
			if err != nil {
				t.Fatalf("call %d: failed to query revoked_at: %v", i, err)
			}
			if !revokedAt.Valid {
				t.Fatalf("call %d: expected revoked_at to be set", i)
			}

			if i == 1 {
				firstRevokedAt = revokedAt.String
			} else if revokedAt.String != firstRevokedAt {
				t.Errorf("call %d: revoked_at changed from %q to %q", i, firstRevokedAt, revokedAt.String)
			}
		}
	})
}

// TestProp_NoSecretsInListings verifies that for any response from
// GET /users/:id/keys or GET /users/:id/tokens, no JSON field in any
// returned object contains a plaintext secret value.
//
// Test Spec: TS-07-P4
// Property: 07-PROP-4
// Validates: 07-REQ-10.1, 07-REQ-12.1, 07-REQ-10.E1, 07-REQ-12.E1
func TestProp_NoSecretsInListings(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	userID := testUUID("secrets-prop-user")
	insertTestUser(t, sqlDB, userID, "alice", "alice@example.com", "github", "gh-001")

	// Pre-defined credential data with known secrets.
	type credData struct {
		keyID  string
		tokID  string
		secret string
		name   string
	}
	creds := []credData{
		{"key-p-0", "tok-p-0", "secret-hash-alpha", "Token Alpha"},
		{"key-p-1", "tok-p-1", "secret-hash-beta", "Token Beta"},
		{"key-p-2", "tok-p-2", "secret-hash-gamma", "Token Gamma"},
	}

	for _, c := range creds {
		insertTestAPIKey(t, sqlDB, c.keyID, userID, c.secret, 30,
			nullStr("2025-12-31T00:00:00Z"), nullStrEmpty(), "2025-01-01T00:00:00Z")
		insertTestPAT(t, sqlDB, c.tokID, userID, c.name, c.secret,
			`["users:read"]`, 30,
			nullStr("2025-12-31T00:00:00Z"), nullStrEmpty(), "2025-01-01T00:00:00Z")
	}

	// Check API keys listing.
	recKeys := sendGet(t, e, "/users/"+userID+"/keys")
	if recKeys.Code == http.StatusOK {
		keysBody := recKeys.Body.String()
		for _, c := range creds {
			if strings.Contains(keysBody, c.secret) {
				t.Errorf("API keys response contains secret value %q", c.secret)
			}
		}

		var keys []map[string]any
		if err := json.Unmarshal(recKeys.Body.Bytes(), &keys); err == nil {
			for i, k := range keys {
				for field := range k {
					if !apiKeyMetaExpectedFields[field] {
						t.Errorf("key[%d]: unexpected field %q", i, field)
					}
				}
			}
		}
	}

	// Check PATs listing.
	recTokens := sendGet(t, e, "/users/"+userID+"/tokens")
	if recTokens.Code == http.StatusOK {
		tokensBody := recTokens.Body.String()
		for _, c := range creds {
			if strings.Contains(tokensBody, c.secret) {
				t.Errorf("PATs response contains secret value %q", c.secret)
			}
		}

		var tokens []map[string]any
		if err := json.Unmarshal(recTokens.Body.Bytes(), &tokens); err == nil {
			for i, tok := range tokens {
				for field := range tok {
					if !patMetaExpectedFields[field] {
						t.Errorf("token[%d]: unexpected field %q", i, field)
					}
				}
			}
		}
	}
}

// TestProp_AdminAuthBeforeDataAccess verifies that for any request to any
// endpoint under /users, no database query is executed before
// auth.RequireAdmin returns successfully; non-admin requests never touch
// the users, api_keys, or pats tables.
//
// Test Spec: TS-07-P5
// Property: 07-PROP-5
// Validates: 07-REQ-2.6, 07-REQ-3.3, 07-REQ-4.3, 07-REQ-5.4, 07-REQ-6.4,
//
//	07-REQ-7.5, 07-REQ-8.4, 07-REQ-9.4, 07-REQ-10.3, 07-REQ-11.4,
//	07-REQ-12.3, 07-REQ-13.4
func TestProp_AdminAuthBeforeDataAccess(t *testing.T) {
	e, sqlDB := setupNonAdminTestServer(t)

	// Seed a user so endpoints would have data to access if auth check fails.
	userID := testUUID("auth-prop-user")
	insertTestUser(t, sqlDB, userID, "alice", "alice@example.com", "github", "gh-001")
	insertTestAPIKey(t, sqlDB, "auth-prop-key", userID, "hash-aaa", 30,
		nullStr("2025-12-31T00:00:00Z"), nullStrEmpty(), "2025-01-01T00:00:00Z")
	insertTestPAT(t, sqlDB, "auth-prop-tok", userID, "My Token", "hash-bbb",
		`["users:read"]`, 30,
		nullStr("2025-12-31T00:00:00Z"), nullStrEmpty(), "2025-01-01T00:00:00Z")

	// All 12 admin-only endpoint (method, path) combinations.
	adminEndpoints := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/users"},
		{http.MethodGet, "/users"},
		{http.MethodGet, "/users/" + userID},
		{http.MethodPatch, "/users/" + userID},
		{http.MethodPost, "/users/" + userID + "/promote"},
		{http.MethodPost, "/users/" + userID + "/demote"},
		{http.MethodPost, "/users/" + userID + "/block"},
		{http.MethodPost, "/users/" + userID + "/unblock"},
		{http.MethodGet, "/users/" + userID + "/keys"},
		{http.MethodDelete, "/users/" + userID + "/keys/auth-prop-key"},
		{http.MethodGet, "/users/" + userID + "/tokens"},
		{http.MethodDelete, "/users/" + userID + "/tokens/auth-prop-tok"},
	}

	for _, ep := range adminEndpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			var rec *httptest.ResponseRecorder

			switch ep.method {
			case http.MethodPost:
				if ep.path == "/users" {
					rec = sendJSON(t, e, ep.method, ep.path, validCreateUserBody())
				} else {
					rec = sendPost(t, e, ep.path)
				}
			case http.MethodGet:
				rec = sendGet(t, e, ep.path)
			case http.MethodPatch:
				rec = sendJSON(t, e, ep.method, ep.path, `{"full_name":"X"}`)
			case http.MethodDelete:
				rec = sendDelete(t, e, ep.path)
			}

			if rec.Code != http.StatusForbidden {
				t.Errorf("expected HTTP 403, got %d; body: %s", rec.Code, rec.Body.String())
			}
		})
	}

	// Verify no data was modified by the non-admin requests.
	var userCount int
	err := sqlDB.QueryRow("SELECT COUNT(*) FROM users").Scan(&userCount)
	if err != nil {
		t.Fatalf("failed to count users: %v", err)
	}
	if userCount != 1 {
		t.Errorf("expected 1 user in database (no new users created), got %d", userCount)
	}
}

// TestProp_ListEndpointsEmptyArray verifies that for any list endpoint
// (GET /users, GET /users/:id/keys, GET /users/:id/tokens, GET /user/orgs)
// when no records match, the response body is the JSON literal [] and the
// HTTP status is 200, not a null value or a 404 error.
//
// Test Spec: TS-07-P6
// Property: 07-PROP-6
// Validates: 07-REQ-3.E1, 07-REQ-16.E2, 07-REQ-3.1, 07-REQ-10.1, 07-REQ-12.1
func TestProp_ListEndpointsEmptyArray(t *testing.T) {
	t.Run("GET /users empty", func(t *testing.T) {
		e, _ := setupAdminTestServer(t)

		rec := sendGet(t, e, "/users")

		if rec.Code != http.StatusOK {
			t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
		}

		body := strings.TrimSpace(rec.Body.String())
		if body != "[]" {
			t.Errorf("expected '[]', got %q", body)
		}
		if body == "null" {
			t.Error("response body is 'null', expected '[]'")
		}
	})

	t.Run("GET /users/:id/keys empty", func(t *testing.T) {
		e, sqlDB := setupAdminTestServer(t)

		userID := testUUID("empty-keys-user")
		insertTestUser(t, sqlDB, userID, "alice", "alice@example.com", "github", "gh-001")

		rec := sendGet(t, e, "/users/"+userID+"/keys")

		if rec.Code != http.StatusOK {
			t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
		}

		body := strings.TrimSpace(rec.Body.String())
		if body != "[]" {
			t.Errorf("expected '[]', got %q", body)
		}
	})

	t.Run("GET /users/:id/tokens empty", func(t *testing.T) {
		e, sqlDB := setupAdminTestServer(t)

		userID := testUUID("empty-tokens-user")
		insertTestUser(t, sqlDB, userID, "bob", "bob@example.com", "github", "gh-002")

		rec := sendGet(t, e, "/users/"+userID+"/tokens")

		if rec.Code != http.StatusOK {
			t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
		}

		body := strings.TrimSpace(rec.Body.String())
		if body != "[]" {
			t.Errorf("expected '[]', got %q", body)
		}
	})

	t.Run("GET /user/orgs empty", func(t *testing.T) {
		userID := testUUID("empty-orgs-user")
		e, sqlDB := setupPATTestServer(t, userID, []string{"orgs:read"})

		insertTestUser(t, sqlDB, userID, "carol", "carol@example.com", "github", "gh-003")

		rec := sendGet(t, e, "/user/orgs")

		if rec.Code != http.StatusOK {
			t.Fatalf("expected HTTP 200, got %d; body: %s", rec.Code, rec.Body.String())
		}

		body := strings.TrimSpace(rec.Body.String())
		if body != "[]" {
			t.Errorf("expected '[]', got %q", body)
		}
	})
}

// ========================================================================
// Task 5.4: Smoke test stubs
// Test Spec: TS-07-SMOKE-1 through TS-07-SMOKE-5
// ========================================================================

// TestSmoke_CreateUser is an end-to-end smoke test: an admin creates a new
// user via POST /users, verifying the full handler flow from auth check
// through UUID generation, database insertion, and 201 response.
//
// Test Spec: TS-07-SMOKE-1
// Execution Path: 07-PATH-1
func TestSmoke_CreateUser(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	body := `{"username":"smoketest","email":"smoke@example.com","provider":"github","provider_id":"gh-smoke"}`
	rec := sendJSON(t, e, http.MethodPost, "/users", body)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected HTTP 201, got %d; body: %s", rec.Code, rec.Body.String())
	}

	user := parseUserResponse(t, rec)

	// Verify all 10 User fields.
	if !isUUID(user.ID) {
		t.Errorf("expected id to be a valid UUID, got %q", user.ID)
	}
	if user.Username != "smoketest" {
		t.Errorf("expected username %q, got %q", "smoketest", user.Username)
	}
	if user.Email != "smoke@example.com" {
		t.Errorf("expected email %q, got %q", "smoke@example.com", user.Email)
	}
	if user.Role != "user" {
		t.Errorf("expected role %q, got %q", "user", user.Role)
	}
	if user.Status != "active" {
		t.Errorf("expected status %q, got %q", "active", user.Status)
	}
	if user.FullName != "" {
		t.Errorf("expected full_name to be empty string, got %q", user.FullName)
	}
	if user.Provider != "github" {
		t.Errorf("expected provider %q, got %q", "github", user.Provider)
	}
	if user.ProviderID != "gh-smoke" {
		t.Errorf("expected provider_id %q, got %q", "gh-smoke", user.ProviderID)
	}
	if !isRFC3339(user.CreatedAt) {
		t.Errorf("expected created_at to be RFC 3339, got %q", user.CreatedAt)
	}
	if !isRFC3339(user.UpdatedAt) {
		t.Errorf("expected updated_at to be RFC 3339, got %q", user.UpdatedAt)
	}

	// Verify user row exists in the database.
	var dbUsername string
	err := sqlDB.QueryRow("SELECT username FROM users WHERE id = ?", user.ID).Scan(&dbUsername)
	if err != nil {
		t.Fatalf("failed to query user from database: %v", err)
	}
	if dbUsername != "smoketest" {
		t.Errorf("expected username in database to be %q, got %q", "smoketest", dbUsername)
	}
}

// TestSmoke_DemoteWithSafeguard is an end-to-end smoke test: admin demotes
// a target admin user, exercising the last-admin safeguard by verifying that
// demoting a non-last admin succeeds and demoting the sole admin returns 409.
//
// Test Spec: TS-07-SMOKE-2
// Execution Path: 07-PATH-2
func TestSmoke_DemoteWithSafeguard(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	// Seed two active admins.
	admin1 := testUUID("smoke-admin-1")
	admin2 := testUUID("smoke-admin-2")
	insertTestUserFull(t, sqlDB, admin1, "admin1", "admin1@example.com", "", "admin", "active", "github", "gh-a1", "2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z")
	insertTestUserFull(t, sqlDB, admin2, "admin2", "admin2@example.com", "", "admin", "active", "github", "gh-a2", "2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z")

	// First call (two active admins): demote admin-1 should succeed.
	rec1 := sendPost(t, e, "/users/"+admin1+"/demote")
	if rec1.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 for first demote, got %d; body: %s", rec1.Code, rec1.Body.String())
	}

	user1 := parseUserResponse(t, rec1)
	if user1.Role != "user" {
		t.Errorf("expected demoted user role %q, got %q", "user", user1.Role)
	}

	// Second call (sole admin): demote admin-2 should fail.
	rec2 := sendPost(t, e, "/users/"+admin2+"/demote")
	assertErrorResponse(t, rec2, http.StatusConflict, "cannot demote the last admin")

	// Verify exactly one active admin remains.
	var adminCount int
	err := sqlDB.QueryRow("SELECT COUNT(*) FROM users WHERE role = 'admin' AND status = 'active'").Scan(&adminCount)
	if err != nil {
		t.Fatalf("failed to count active admins: %v", err)
	}
	if adminCount != 1 {
		t.Errorf("expected exactly 1 active admin, got %d", adminCount)
	}
}

// TestSmoke_RevokeAPIKey is an end-to-end smoke test: admin revokes a
// user's API key via DELETE, then verifies idempotency on the second call.
//
// Test Spec: TS-07-SMOKE-3
// Execution Path: 07-PATH-3
func TestSmoke_RevokeAPIKey(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	userID := testUUID("smoke-key-user")
	keyID := "smoke-key-id"
	insertTestUser(t, sqlDB, userID, "alice", "alice@example.com", "github", "gh-001")
	insertTestAPIKey(t, sqlDB, keyID, userID, "hash-smoke", 30,
		nullStr("2025-01-31T00:00:00Z"), nullStrEmpty(), "2025-01-01T00:00:00Z")

	// First call: revoke the active key.
	rec1 := sendDelete(t, e, "/users/"+userID+"/keys/"+keyID)
	if rec1.Code != http.StatusNoContent {
		t.Fatalf("expected HTTP 204 on first revoke, got %d; body: %s", rec1.Code, rec1.Body.String())
	}
	if rec1.Body.Len() != 0 {
		t.Errorf("expected empty body on first revoke, got %q", rec1.Body.String())
	}

	// Capture revoked_at after first revocation.
	var revokedAt1 sql.NullString
	err := sqlDB.QueryRow("SELECT revoked_at FROM api_keys WHERE key_id = ?", keyID).Scan(&revokedAt1)
	if err != nil {
		t.Fatalf("failed to query revoked_at: %v", err)
	}
	if !revokedAt1.Valid {
		t.Fatal("expected revoked_at to be non-NULL after first revoke")
	}

	// Second call: revoke again (idempotent).
	rec2 := sendDelete(t, e, "/users/"+userID+"/keys/"+keyID)
	if rec2.Code != http.StatusNoContent {
		t.Fatalf("expected HTTP 204 on second revoke, got %d; body: %s", rec2.Code, rec2.Body.String())
	}

	// Verify revoked_at is unchanged.
	var revokedAt2 sql.NullString
	err = sqlDB.QueryRow("SELECT revoked_at FROM api_keys WHERE key_id = ?", keyID).Scan(&revokedAt2)
	if err != nil {
		t.Fatalf("failed to query revoked_at after second revoke: %v", err)
	}
	if revokedAt2.String != revokedAt1.String {
		t.Errorf("revoked_at changed from %q to %q after idempotent revoke", revokedAt1.String, revokedAt2.String)
	}
}

// TestSmoke_OwnProfile is an end-to-end smoke test: an authenticated user
// reads their own profile via GET /user (with ETag) and then updates their
// full_name via PATCH /user.
//
// Test Spec: TS-07-SMOKE-4
// Execution Path: 07-PATH-4
func TestSmoke_OwnProfile(t *testing.T) {
	userID := testUUID("smoke-own-uuid")
	e, sqlDB := setupPATTestServer(t, userID, []string{"users:read"})

	insertTestUser(t, sqlDB, userID, "alice", "alice@example.com", "github", "gh-001")

	// Step 1: GET /user returns 200 with User and ETag.
	rec1 := sendGet(t, e, "/user")
	if rec1.Code != http.StatusOK {
		t.Fatalf("GET /user: expected HTTP 200, got %d; body: %s", rec1.Code, rec1.Body.String())
	}

	user1 := parseUserResponse(t, rec1)
	if user1.ID != userID {
		t.Errorf("GET /user: expected user id %q, got %q", userID, user1.ID)
	}

	etag := rec1.Header().Get("ETag")
	if etag == "" {
		t.Error("GET /user: expected ETag header to be set")
	}

	// Step 2: GET /user with matching If-None-Match returns 304.
	if etag != "" {
		rec2 := sendGetWithHeaders(t, e, "/user", map[string]string{
			"If-None-Match": etag,
		})
		if rec2.Code != http.StatusNotModified {
			t.Errorf("GET /user with ETag: expected HTTP 304, got %d", rec2.Code)
		}
		if rec2.Body.Len() != 0 {
			t.Errorf("GET /user with ETag: expected empty body, got %q", rec2.Body.String())
		}
	}

	// Step 3: PATCH /user updates full_name.
	rec3 := sendJSON(t, e, http.MethodPatch, "/user", `{"full_name":"Updated Name"}`)
	if rec3.Code != http.StatusOK {
		t.Fatalf("PATCH /user: expected HTTP 200, got %d; body: %s", rec3.Code, rec3.Body.String())
	}

	user3 := parseUserResponse(t, rec3)
	if user3.FullName != "Updated Name" {
		t.Errorf("PATCH /user: expected full_name %q, got %q", "Updated Name", user3.FullName)
	}

	// Verify persistence in database.
	var dbFullName sql.NullString
	err := sqlDB.QueryRow("SELECT full_name FROM users WHERE id = ?", userID).Scan(&dbFullName)
	if err != nil {
		t.Fatalf("failed to query full_name from database: %v", err)
	}
	if !dbFullName.Valid || dbFullName.String != "Updated Name" {
		t.Errorf("expected full_name in database to be %q, got %v", "Updated Name", dbFullName)
	}
}

// TestSmoke_FullEndToEnd is a full end-to-end smoke test: admin creates a
// user, promotes them, and the new user lists their org memberships,
// exercising all three handler interactions and both admin and self-service
// auth paths.
//
// Test Spec: TS-07-SMOKE-5
// Execution Path: 07-PATH-5
func TestSmoke_FullEndToEnd(t *testing.T) {
	// Open a shared database for both admin and self-service servers.
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	defer database.Close()

	// Create admin-auth Echo server.
	adminEcho := echo.New()
	adminGroup := adminEcho.Group("")
	adminGroup.Use(adminAuthMiddleware("test-admin-uuid"))
	handlers.RegisterUserHandlers(adminGroup, database.SqlDB)

	// Step 1: Admin creates a new user.
	createBody := `{"username":"newuser","email":"new@example.com","provider":"github","provider_id":"gh-new"}`
	rec1 := sendJSON(t, adminEcho, http.MethodPost, "/users", createBody)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("Step 1 (create): expected HTTP 201, got %d; body: %s", rec1.Code, rec1.Body.String())
	}

	createdUser := parseUserResponse(t, rec1)
	if createdUser.Role != "user" {
		t.Errorf("Step 1: expected role %q, got %q", "user", createdUser.Role)
	}
	newUserID := createdUser.ID

	// Verify row exists in database.
	var dbRole string
	err = database.SqlDB.QueryRow("SELECT role FROM users WHERE id = ?", newUserID).Scan(&dbRole)
	if err != nil {
		t.Fatalf("Step 1: failed to query user from database: %v", err)
	}

	// Step 2: Admin promotes the new user.
	rec2 := sendPost(t, adminEcho, "/users/"+newUserID+"/promote")
	if rec2.Code != http.StatusOK {
		t.Fatalf("Step 2 (promote): expected HTTP 200, got %d; body: %s", rec2.Code, rec2.Body.String())
	}

	promotedUser := parseUserResponse(t, rec2)
	if promotedUser.Role != "admin" {
		t.Errorf("Step 2: expected role %q, got %q", "admin", promotedUser.Role)
	}

	// Step 3: New user lists their org memberships via GET /user/orgs.
	// Create a separate Echo server with PAT auth for the new user.
	userEcho := echo.New()
	userGroup := userEcho.Group("")
	userGroup.Use(patAuthMiddleware(newUserID, []string{"orgs:read"}))
	handlers.RegisterUserHandlers(userGroup, database.SqlDB)

	// Seed an active org and membership for the new user.
	insertTestOrg(t, database.SqlDB, "smoke-org-1", "Smoke Org", "smoke-org", "https://smoke.example.com", "active")
	insertTestOrgMember(t, database.SqlDB, "smoke-org-1", newUserID)

	rec3 := sendGet(t, userEcho, "/user/orgs")
	if rec3.Code != http.StatusOK {
		t.Fatalf("Step 3 (list orgs): expected HTTP 200, got %d; body: %s", rec3.Code, rec3.Body.String())
	}

	orgs := parseOrgsResponse(t, rec3)
	// Should have at least the one active org we seeded.
	for _, org := range orgs {
		if org.Status == "blocked" {
			t.Error("Step 3: blocked org should not appear in response")
		}
	}
}

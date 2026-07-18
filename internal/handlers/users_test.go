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
	"testing"

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
func adminAuthMiddleware(userID string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("auth_info", &auth.AuthInfo{
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
func nonAdminAuthMiddleware(userID string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("auth_info", &auth.AuthInfo{
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

// insertTestUser inserts a user directly into the users table for test setup.
func insertTestUser(t *testing.T, sqlDB *sql.DB, id, username, email, provider, providerID string) {
	t.Helper()

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

	userID := "get-user-uuid-1"
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

	userID := "etag-user-uuid"
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

	userID := "update-user-uuid-1"
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

	userID := "update-missing-uuid"
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

	userID := "clear-name-uuid"
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

	userID := "pointer-test-uuid"
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

	userID := "promote-user-uuid"
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

	userID := "already-admin-uuid"
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
	targetID := "demote-target-uuid"
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

	userID := "already-user-uuid"
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
	soleAdminID := "sole-admin-uuid"
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
	adminID := "db-error-admin-uuid"
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

	userID := "block-user-uuid"
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

	userID := "already-blocked-uuid"
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

	userID := "unblock-user-uuid"
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

	userID := "already-active-uuid"
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

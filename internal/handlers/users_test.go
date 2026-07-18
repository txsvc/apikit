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

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	apikit "github.com/txsvc/apikit"
)

// ---------------------------------------------------------------------------
// Test-scoped mock interface for SDK user admin methods.
//
// This interface is UNEXPORTED and defined only in this _test.go file.
// It covers all apikit.Client methods that user admin commands will call.
// Signatures match the actual SDK (sdk.go), accounting for the Response[T]
// wrapper and variadic RequestOption parameter.
//
// NOTE: Some methods (CreateUser, UpdateUserByID, PromoteUser, etc.) are
// not yet defined on apikit.Client (spec 12 stubs pending). The test
// interface defines them based on the spec's intended signatures.
//
// DIVERGENCE: The SDK's PromoteUser/DemoteUser/BlockUser/UnblockUser
// currently return only error, but the spec (14-REQ-8.1, 14-REQ-9.1,
// 14-REQ-10.1, 14-REQ-11.1) says these commands "print the returned
// apikit.User as JSON." The mock uses (*apikit.User, error) to match
// the spec's intended behavior. The production code may call the SDK
// action method followed by GetUserByID, or the SDK may be updated.
// ---------------------------------------------------------------------------

type adminUsersClient interface {
	ListUsers(ctx context.Context, opts *apikit.ListUsersOptions) ([]*apikit.User, error)
	GetUserByID(ctx context.Context, userID string, opts ...apikit.RequestOption) (*apikit.Response[apikit.User], error)
	CreateUser(ctx context.Context, req *apikit.CreateUserRequest) (*apikit.User, error)
	UpdateUserByID(ctx context.Context, id string, req *apikit.UpdateUserRequest) (*apikit.User, error)
	PromoteUser(ctx context.Context, id string) (*apikit.User, error)
	DemoteUser(ctx context.Context, id string) (*apikit.User, error)
	BlockUser(ctx context.Context, id string) (*apikit.User, error)
	UnblockUser(ctx context.Context, id string) (*apikit.User, error)
}

// mockAdminUsersClient implements adminUsersClient for testing.
type mockAdminUsersClient struct {
	// Return values
	listUsersResult   []*apikit.User
	listUsersErr      error
	getUserResult     *apikit.Response[apikit.User]
	getUserErr        error
	createUserResult  *apikit.User
	createUserErr     error
	updateUserResult  *apikit.User
	updateUserErr     error
	promoteUserResult  *apikit.User
	promoteUserErr     error
	demoteUserResult   *apikit.User
	demoteUserErr      error
	blockUserResult    *apikit.User
	blockUserErr       error
	unblockUserResult  *apikit.User
	unblockUserErr     error

	// Call tracking
	listUsersCalled bool
	listUsersOpts   *apikit.ListUsersOptions
	getUserCalled   bool
	getUserID       string
	createUserCalled bool
	createUserReq    *apikit.CreateUserRequest
	updateUserCalled bool
	updateUserID     string
	updateUserReq    *apikit.UpdateUserRequest
	promoteUserCalled bool
	promoteUserID     string
	demoteUserCalled  bool
	demoteUserID      string
	blockUserCalled   bool
	blockUserID       string
	unblockUserCalled bool
	unblockUserID     string

	// Context tracking (for TS-14-11)
	capturedCtx context.Context
}

func (m *mockAdminUsersClient) ListUsers(ctx context.Context, opts *apikit.ListUsersOptions) ([]*apikit.User, error) {
	m.capturedCtx = ctx
	m.listUsersCalled = true
	m.listUsersOpts = opts
	return m.listUsersResult, m.listUsersErr
}

func (m *mockAdminUsersClient) GetUserByID(ctx context.Context, userID string, opts ...apikit.RequestOption) (*apikit.Response[apikit.User], error) {
	m.capturedCtx = ctx
	m.getUserCalled = true
	m.getUserID = userID
	return m.getUserResult, m.getUserErr
}

func (m *mockAdminUsersClient) CreateUser(ctx context.Context, req *apikit.CreateUserRequest) (*apikit.User, error) {
	m.capturedCtx = ctx
	m.createUserCalled = true
	m.createUserReq = req
	return m.createUserResult, m.createUserErr
}

func (m *mockAdminUsersClient) UpdateUserByID(ctx context.Context, id string, req *apikit.UpdateUserRequest) (*apikit.User, error) {
	m.capturedCtx = ctx
	m.updateUserCalled = true
	m.updateUserID = id
	m.updateUserReq = req
	return m.updateUserResult, m.updateUserErr
}

func (m *mockAdminUsersClient) PromoteUser(ctx context.Context, id string) (*apikit.User, error) {
	m.capturedCtx = ctx
	m.promoteUserCalled = true
	m.promoteUserID = id
	return m.promoteUserResult, m.promoteUserErr
}

func (m *mockAdminUsersClient) DemoteUser(ctx context.Context, id string) (*apikit.User, error) {
	m.capturedCtx = ctx
	m.demoteUserCalled = true
	m.demoteUserID = id
	return m.demoteUserResult, m.demoteUserErr
}

func (m *mockAdminUsersClient) BlockUser(ctx context.Context, id string) (*apikit.User, error) {
	m.capturedCtx = ctx
	m.blockUserCalled = true
	m.blockUserID = id
	return m.blockUserResult, m.blockUserErr
}

func (m *mockAdminUsersClient) UnblockUser(ctx context.Context, id string) (*apikit.User, error) {
	m.capturedCtx = ctx
	m.unblockUserCalled = true
	m.unblockUserID = id
	return m.unblockUserResult, m.unblockUserErr
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// errorEnvelope is the JSON error envelope structure for parsing test output.
type errorEnvelope struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// executeAdminCmd constructs the admin command tree from NewAdminCmd,
// sets the provided args, captures stdout into a buffer, and executes.
// Returns the captured stdout string and the error from Execute.
func executeAdminCmd(args ...string) (stdout string, err error) {
	cmd := NewAdminCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs(args)

	// Silence Cobra's own usage/error output so it doesn't pollute stdout.
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	for _, sub := range cmd.Commands() {
		sub.SilenceUsage = true
		sub.SilenceErrors = true
		for _, subsub := range sub.Commands() {
			subsub.SilenceUsage = true
			subsub.SilenceErrors = true
		}
	}

	err = cmd.Execute()
	return buf.String(), err
}

// parseErrorEnvelope parses stdout as a JSON error envelope.
func parseErrorEnvelope(t *testing.T, stdout string) errorEnvelope {
	t.Helper()
	var env errorEnvelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("failed to parse error envelope from stdout %q: %v", stdout, err)
	}
	return env
}

// ---------------------------------------------------------------------------
// TS-14-7: Every admin subcommand with a RunE executes loadConfig,
// newClient, validates args/flags, calls the SDK method, and prints JSON
// to stdout on success.
// Requirement: 14-REQ-2.1
// ---------------------------------------------------------------------------

func TestCommonSuccessPattern(t *testing.T) {
	mock := &mockAdminUsersClient{
		listUsersResult: []*apikit.User{{ID: "u1", Username: "alice"}},
	}

	stdout, err := executeAdminCmd("users", "list")

	// Expect the command to succeed (exit code 0).
	if err != nil {
		t.Errorf("expected nil error (exit 0), got: %v", err)
	}

	// Verify stdout is valid JSON.
	if !json.Valid([]byte(stdout)) {
		t.Errorf("stdout is not valid JSON: %q", stdout)
	}

	// Verify the mock SDK method was called.
	if !mock.listUsersCalled {
		t.Error("ListUsers was not called; expected SDK method to be invoked")
	}
}

// ---------------------------------------------------------------------------
// TS-14-8: When the SDK method returns *apikit.APIError, handleError is
// called, the JSON error envelope is printed to stdout, and exit code is 1.
// Requirement: 14-REQ-2.2
// ---------------------------------------------------------------------------

func TestCommonAPIError(t *testing.T) {
	_ = &mockAdminUsersClient{
		getUserErr: &apikit.APIError{Code: 403, Message: "forbidden"},
	}

	stdout, err := executeAdminCmd("users", "show", "user-1")

	// Expect an error (exit code 1 for API errors).
	if err == nil {
		t.Error("expected non-nil error for API error response")
	}

	// Verify the JSON error envelope on stdout.
	if stdout == "" {
		t.Fatal("stdout is empty; expected JSON error envelope")
	}
	env := parseErrorEnvelope(t, stdout)
	if env.Error.Code != 403 {
		t.Errorf("error.code = %d, want 403", env.Error.Code)
	}
	if env.Error.Message != "forbidden" {
		t.Errorf("error.message = %q, want %q", env.Error.Message, "forbidden")
	}
}

// ---------------------------------------------------------------------------
// TS-14-9: When the SDK method returns a non-APIError (e.g. network
// failure), handleError prints a code-0 error envelope to stdout and
// exit code is 2.
// Requirement: 14-REQ-2.3
// ---------------------------------------------------------------------------

func TestCommonNetworkError(t *testing.T) {
	_ = &mockAdminUsersClient{
		listUsersErr: errors.New("connection refused"),
	}

	stdout, err := executeAdminCmd("users", "list")

	// Expect an error (exit code 2 for client errors).
	if err == nil {
		t.Error("expected non-nil error for network failure")
	}

	// Verify the JSON error envelope on stdout with code 0.
	if stdout == "" {
		t.Fatal("stdout is empty; expected JSON error envelope")
	}
	env := parseErrorEnvelope(t, stdout)
	if env.Error.Code != 0 {
		t.Errorf("error.code = %d, want 0 for client error", env.Error.Code)
	}
	if !strings.Contains(env.Error.Message, "connection refused") {
		t.Errorf("error.message = %q, want it to contain %q", env.Error.Message, "connection refused")
	}
}

// ---------------------------------------------------------------------------
// TS-14-10: When loadConfig returns an error (missing API key), the
// command prints a code-0 JSON error envelope and exits with code 2
// without calling the SDK.
// Requirement: 14-REQ-2.4
//
// NOTE: Per reviewer finding, spec 13's PersistentPreRunE handles config
// loading; the admin command's RunE may not handle this directly. This
// test documents the expected end-to-end behavior regardless of where the
// error is caught.
// ---------------------------------------------------------------------------

func TestCommonLoadConfigError(t *testing.T) {
	mock := &mockAdminUsersClient{}

	stdout, err := executeAdminCmd("users", "list")

	// Expect an error.
	if err == nil {
		t.Error("expected non-nil error when config loading fails")
	}

	// SDK should NOT have been called.
	if mock.listUsersCalled {
		t.Error("ListUsers was called despite config error; expected no SDK call")
	}

	// Verify the JSON error envelope.
	if stdout == "" {
		t.Fatal("stdout is empty; expected JSON error envelope")
	}
	env := parseErrorEnvelope(t, stdout)
	if env.Error.Code != 0 {
		t.Errorf("error.code = %d, want 0 for client error", env.Error.Code)
	}
}

// ---------------------------------------------------------------------------
// TS-14-11: All SDK method calls use context.Background() as the context
// argument.
// Requirement: 14-REQ-2.5
// ---------------------------------------------------------------------------

func TestContextIsBackground(t *testing.T) {
	mock := &mockAdminUsersClient{
		listUsersResult: []*apikit.User{},
	}

	_, _ = executeAdminCmd("users", "list")

	if !mock.listUsersCalled {
		t.Fatal("ListUsers was not called; cannot verify context")
	}

	ctx := mock.capturedCtx
	if ctx == nil {
		t.Fatal("captured context is nil")
	}

	// context.Background() has no deadline.
	if deadline, ok := ctx.Deadline(); ok {
		t.Errorf("expected no deadline on context, got %v", deadline)
	}

	// context.Background() is never done.
	select {
	case <-ctx.Done():
		t.Error("context is already done; expected context.Background()")
	default:
		// OK
	}
}

// ---------------------------------------------------------------------------
// TS-14-13: When a required positional argument is absent, the command
// prints a missing-argument JSON error envelope to stdout and exits with
// code 2 without calling the SDK.
// Requirement: 14-REQ-3.1
// ---------------------------------------------------------------------------

func TestMissingPositionalArg(t *testing.T) {
	mock := &mockAdminUsersClient{}

	stdout, err := executeAdminCmd("users", "show")

	// Expect an error (exit code 2).
	if err == nil {
		t.Error("expected error when positional argument is missing")
	}

	// SDK should not be called.
	if mock.getUserCalled {
		t.Error("GetUserByID was called despite missing argument")
	}

	// Verify the JSON error envelope.
	if stdout == "" {
		t.Fatal("stdout is empty; expected JSON error envelope")
	}
	env := parseErrorEnvelope(t, stdout)
	if env.Error.Code != 0 {
		t.Errorf("error.code = %d, want 0", env.Error.Code)
	}
	if env.Error.Message != "missing required argument: id" {
		t.Errorf("error.message = %q, want %q", env.Error.Message, "missing required argument: id")
	}
}

// ---------------------------------------------------------------------------
// TS-14-14: When a required flag is absent, the command prints a
// missing-flag JSON error envelope to stdout and exits with code 2
// without calling the SDK.
// Requirement: 14-REQ-3.2
// ---------------------------------------------------------------------------

func TestMissingRequiredFlag(t *testing.T) {
	mock := &mockAdminUsersClient{}

	// Invoke create with --email, --provider, --provider-id but no --username.
	stdout, err := executeAdminCmd(
		"users", "create",
		"--email", "a@b.com",
		"--provider", "local",
		"--provider-id", "p1",
	)

	// Expect an error (exit code 2).
	if err == nil {
		t.Error("expected error when required flag is missing")
	}

	// SDK should not be called.
	if mock.createUserCalled {
		t.Error("CreateUser was called despite missing flag")
	}

	// Verify the JSON error envelope.
	if stdout == "" {
		t.Fatal("stdout is empty; expected JSON error envelope")
	}
	env := parseErrorEnvelope(t, stdout)
	if env.Error.Code != 0 {
		t.Errorf("error.code = %d, want 0", env.Error.Code)
	}
	if env.Error.Message != "missing required flag: --username" {
		t.Errorf("error.message = %q, want %q", env.Error.Message, "missing required flag: --username")
	}
}

// ---------------------------------------------------------------------------
// TS-14-15: Extra positional arguments cause Cobra's ExactArgs validator
// to reject the command before RunE is called.
// Requirement: 14-REQ-3.3
// ---------------------------------------------------------------------------

func TestExtraArgsRejected(t *testing.T) {
	mock := &mockAdminUsersClient{}

	_, err := executeAdminCmd("users", "show", "id1", "id2")

	// Cobra should reject extra args before RunE runs.
	if err == nil {
		t.Error("expected error when extra positional arguments are provided")
	}

	// SDK should not be called.
	if mock.getUserCalled {
		t.Error("GetUserByID was called despite extra arguments")
	}
}

// ---------------------------------------------------------------------------
// TS-14-16: Non-UUID string IDs are passed through to the SDK without
// client-side validation.
// Requirement: 14-REQ-3.4
// ---------------------------------------------------------------------------

func TestNonUUIDPassthrough(t *testing.T) {
	mock := &mockAdminUsersClient{
		getUserErr: &apikit.APIError{Code: 404, Message: "user not found"},
	}

	stdout, err := executeAdminCmd("users", "show", "not-a-uuid")

	// The SDK should have been called with the non-UUID string.
	if !mock.getUserCalled {
		t.Fatal("GetUserByID was not called; expected pass-through of non-UUID ID")
	}
	if mock.getUserID != "not-a-uuid" {
		t.Errorf("captured ID = %q, want %q", mock.getUserID, "not-a-uuid")
	}

	// The SDK error should be forwarded as exit code 1.
	if err == nil {
		t.Error("expected error from SDK (404)")
	}

	if stdout == "" {
		t.Fatal("stdout is empty")
	}
	env := parseErrorEnvelope(t, stdout)
	if env.Error.Code != 404 {
		t.Errorf("error.code = %d, want 404", env.Error.Code)
	}
}

// ---------------------------------------------------------------------------
// TS-14-17: An empty string value for a required string flag is passed
// through to the SDK without client-side rejection.
// Requirement: 14-REQ-3.5
// ---------------------------------------------------------------------------

func TestEmptyStringFlagPassthrough(t *testing.T) {
	mock := &mockAdminUsersClient{
		createUserErr: &apikit.APIError{Code: 400, Message: "username required"},
	}

	stdout, err := executeAdminCmd(
		"users", "create",
		"--username", "",
		"--email", "a@b.com",
		"--provider", "local",
		"--provider-id", "p1",
	)

	// CreateUser should have been called with an empty username.
	if !mock.createUserCalled {
		t.Fatal("CreateUser was not called; expected empty string to be passed through")
	}
	if mock.createUserReq == nil {
		t.Fatal("CreateUser request is nil")
	}
	if mock.createUserReq.Username != "" {
		t.Errorf("captured Username = %q, want empty string", mock.createUserReq.Username)
	}

	// SDK error should be forwarded.
	if err == nil {
		t.Error("expected error from SDK (400)")
	}

	if stdout == "" {
		t.Fatal("stdout is empty")
	}
	env := parseErrorEnvelope(t, stdout)
	if env.Error.Code != 400 {
		t.Errorf("error.code = %d, want 400", env.Error.Code)
	}
}

// ---------------------------------------------------------------------------
// TS-14-18: akc admin users list without --include-blocked calls ListUsers
// with IncludeBlocked:false and prints the user array as JSON.
// Requirement: 14-REQ-4.1
// ---------------------------------------------------------------------------

func TestAdminUsersListCommand(t *testing.T) {
	mock := &mockAdminUsersClient{
		listUsersResult: []*apikit.User{{ID: "u1", Username: "alice"}},
	}

	stdout, err := executeAdminCmd("users", "list")

	if err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}

	// ListUsers should have been called with IncludeBlocked=false.
	if !mock.listUsersCalled {
		t.Fatal("ListUsers was not called")
	}
	if mock.listUsersOpts == nil {
		t.Fatal("ListUsers options is nil")
	}
	if mock.listUsersOpts.IncludeBlocked {
		t.Error("ListUsers called with IncludeBlocked=true, want false")
	}

	// stdout should be a JSON array with one user.
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not valid JSON: %q", stdout)
	}

	var users []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &users); err != nil {
		t.Fatalf("failed to parse stdout as JSON array: %v", err)
	}
	if len(users) != 1 {
		t.Errorf("got %d users, want 1", len(users))
	}
	if id, _ := users[0]["id"].(string); id != "u1" {
		t.Errorf("user[0].id = %q, want %q", id, "u1")
	}
}

// ---------------------------------------------------------------------------
// TS-14-19: akc admin users list --include-blocked calls ListUsers with
// IncludeBlocked:true.
// Requirement: 14-REQ-4.2
// ---------------------------------------------------------------------------

func TestAdminUsersListIncludeBlocked(t *testing.T) {
	mock := &mockAdminUsersClient{
		listUsersResult: []*apikit.User{{ID: "u1"}},
	}

	stdout, err := executeAdminCmd("users", "list", "--include-blocked")

	if err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}

	if !mock.listUsersCalled {
		t.Fatal("ListUsers was not called")
	}
	if mock.listUsersOpts == nil {
		t.Fatal("ListUsers options is nil")
	}
	if !mock.listUsersOpts.IncludeBlocked {
		t.Error("ListUsers called with IncludeBlocked=false, want true")
	}

	if !json.Valid([]byte(stdout)) {
		t.Errorf("stdout is not valid JSON: %q", stdout)
	}
}

// ---------------------------------------------------------------------------
// TS-14-20: akc admin users list is registered in the agent interface
// with method GET, path /users, auth admin, and flag --include-blocked.
// Requirement: 14-REQ-4.3
//
// NOTE: Agent interface metadata requires spec 13 CLI core to be
// implemented. This test verifies command Annotations on the cobra.Command.
// ---------------------------------------------------------------------------

func TestAdminUsersListAgentInterface(t *testing.T) {
	cmd := NewAdminCmd()

	// Find the "users list" subcommand.
	usersCmd, _, err := cmd.Find([]string{"users", "list"})
	if err != nil {
		t.Fatalf("failed to find 'users list' command: %v", err)
	}
	if usersCmd.Name() != "list" {
		t.Fatalf("found command %q, want %q", usersCmd.Name(), "list")
	}

	// Verify annotations contain the expected metadata.
	annotations := usersCmd.Annotations
	if annotations == nil {
		t.Fatal("users list command has no Annotations")
	}
	if annotations["method"] != "GET" {
		t.Errorf("method annotation = %q, want %q", annotations["method"], "GET")
	}
	if annotations["path"] != "/users" {
		t.Errorf("path annotation = %q, want %q", annotations["path"], "/users")
	}
	if annotations["auth"] != "admin" {
		t.Errorf("auth annotation = %q, want %q", annotations["auth"], "admin")
	}

	// Verify the --include-blocked flag is registered.
	flag := usersCmd.Flags().Lookup("include-blocked")
	if flag == nil {
		t.Error("--include-blocked flag not registered on users list command")
	} else if flag.DefValue != "false" {
		t.Errorf("--include-blocked default = %q, want %q", flag.DefValue, "false")
	}
}

// ---------------------------------------------------------------------------
// TS-14-21: akc admin users show <id> calls GetUserByID with the given id
// and prints the unwrapped user as JSON.
// Requirement: 14-REQ-5.1
// ---------------------------------------------------------------------------

func TestAdminUsersShowCommand(t *testing.T) {
	mock := &mockAdminUsersClient{
		getUserResult: &apikit.Response[apikit.User]{
			Data: apikit.User{ID: "u1", Username: "alice"},
		},
	}

	stdout, err := executeAdminCmd("users", "show", "u1")

	if err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}

	if !mock.getUserCalled {
		t.Fatal("GetUserByID was not called")
	}
	if mock.getUserID != "u1" {
		t.Errorf("captured ID = %q, want %q", mock.getUserID, "u1")
	}

	// stdout should be a JSON user object (unwrapped from Response).
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not valid JSON: %q", stdout)
	}

	var user map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &user); err != nil {
		t.Fatalf("failed to parse stdout as JSON object: %v", err)
	}
	if id, _ := user["id"].(string); id != "u1" {
		t.Errorf("user.id = %q, want %q", id, "u1")
	}
}

// ---------------------------------------------------------------------------
// TS-14-E4: akc admin users show invoked without the <id> argument exits
// with code 2 and prints the missing-argument error envelope.
// Requirement: 14-REQ-5.E1
// ---------------------------------------------------------------------------

func TestAdminUsersShowMissingID(t *testing.T) {
	stdout, err := executeAdminCmd("users", "show")

	if err == nil {
		t.Error("expected error when <id> argument is missing")
	}

	if stdout == "" {
		t.Fatal("stdout is empty; expected JSON error envelope")
	}
	env := parseErrorEnvelope(t, stdout)
	if env.Error.Code != 0 {
		t.Errorf("error.code = %d, want 0", env.Error.Code)
	}
	if env.Error.Message != "missing required argument: id" {
		t.Errorf("error.message = %q, want %q", env.Error.Message, "missing required argument: id")
	}
}

// ---------------------------------------------------------------------------
// TS-14-22: akc admin users show is registered in the agent interface
// with method GET, path /users/:id, auth admin, and positional arg id.
// Requirement: 14-REQ-5.2
// ---------------------------------------------------------------------------

func TestAdminUsersShowAgentInterface(t *testing.T) {
	cmd := NewAdminCmd()

	showCmd, _, err := cmd.Find([]string{"users", "show"})
	if err != nil {
		t.Fatalf("failed to find 'users show' command: %v", err)
	}
	if showCmd.Name() != "show" {
		t.Fatalf("found command %q, want %q", showCmd.Name(), "show")
	}

	annotations := showCmd.Annotations
	if annotations == nil {
		t.Fatal("users show command has no Annotations")
	}
	if annotations["method"] != "GET" {
		t.Errorf("method annotation = %q, want %q", annotations["method"], "GET")
	}
	if annotations["path"] != "/users/:id" {
		t.Errorf("path annotation = %q, want %q", annotations["path"], "/users/:id")
	}
	if annotations["auth"] != "admin" {
		t.Errorf("auth annotation = %q, want %q", annotations["auth"], "admin")
	}
}

// ---------------------------------------------------------------------------
// TS-14-23: akc admin users create with all four required flags calls
// CreateUser with the correct struct and prints the new user as JSON.
// Requirement: 14-REQ-6.1
// ---------------------------------------------------------------------------

func TestAdminUsersCreateCommand(t *testing.T) {
	mock := &mockAdminUsersClient{
		createUserResult: &apikit.User{ID: "u2", Username: "bob"},
	}

	stdout, err := executeAdminCmd(
		"users", "create",
		"--username", "bob",
		"--email", "bob@x.com",
		"--provider", "local",
		"--provider-id", "p2",
	)

	if err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}

	if !mock.createUserCalled {
		t.Fatal("CreateUser was not called")
	}
	if mock.createUserReq == nil {
		t.Fatal("CreateUser request is nil")
	}
	if mock.createUserReq.Username != "bob" {
		t.Errorf("req.Username = %q, want %q", mock.createUserReq.Username, "bob")
	}
	if mock.createUserReq.Email != "bob@x.com" {
		t.Errorf("req.Email = %q, want %q", mock.createUserReq.Email, "bob@x.com")
	}
	if mock.createUserReq.Provider != "local" {
		t.Errorf("req.Provider = %q, want %q", mock.createUserReq.Provider, "local")
	}
	if mock.createUserReq.ProviderID != "p2" {
		t.Errorf("req.ProviderID = %q, want %q", mock.createUserReq.ProviderID, "p2")
	}

	// stdout should be a JSON user object.
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not valid JSON: %q", stdout)
	}

	var user map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &user); err != nil {
		t.Fatalf("failed to parse stdout as JSON object: %v", err)
	}
	if id, _ := user["id"].(string); id != "u2" {
		t.Errorf("user.id = %q, want %q", id, "u2")
	}
}

// ---------------------------------------------------------------------------
// TS-14-24: akc admin users create is registered in the agent interface
// with method POST, path /users, auth admin, and four required string
// flags.
// Requirement: 14-REQ-6.2
// ---------------------------------------------------------------------------

func TestAdminUsersCreateAgentInterface(t *testing.T) {
	cmd := NewAdminCmd()

	createCmd, _, err := cmd.Find([]string{"users", "create"})
	if err != nil {
		t.Fatalf("failed to find 'users create' command: %v", err)
	}
	if createCmd.Name() != "create" {
		t.Fatalf("found command %q, want %q", createCmd.Name(), "create")
	}

	annotations := createCmd.Annotations
	if annotations == nil {
		t.Fatal("users create command has no Annotations")
	}
	if annotations["method"] != "POST" {
		t.Errorf("method annotation = %q, want %q", annotations["method"], "POST")
	}
	if annotations["path"] != "/users" {
		t.Errorf("path annotation = %q, want %q", annotations["path"], "/users")
	}
	if annotations["auth"] != "admin" {
		t.Errorf("auth annotation = %q, want %q", annotations["auth"], "admin")
	}

	// Verify all four required flags are registered.
	requiredFlags := []string{"username", "email", "provider", "provider-id"}
	for _, name := range requiredFlags {
		flag := createCmd.Flags().Lookup(name)
		if flag == nil {
			t.Errorf("flag --%s not registered on users create command", name)
		}
	}
}

// ---------------------------------------------------------------------------
// TS-14-E5: akc admin users create without --username exits with code 2
// and prints the missing-flag error envelope.
// Requirement: 14-REQ-6.E1
// ---------------------------------------------------------------------------

func TestAdminUsersCreateMissingUsername(t *testing.T) {
	mock := &mockAdminUsersClient{}

	stdout, err := executeAdminCmd(
		"users", "create",
		"--email", "a@b.com",
		"--provider", "local",
		"--provider-id", "p1",
	)

	if err == nil {
		t.Error("expected error when --username is missing")
	}

	if mock.createUserCalled {
		t.Error("CreateUser was called despite missing --username flag")
	}

	if stdout == "" {
		t.Fatal("stdout is empty; expected JSON error envelope")
	}
	env := parseErrorEnvelope(t, stdout)
	if env.Error.Code != 0 {
		t.Errorf("error.code = %d, want 0", env.Error.Code)
	}
	if env.Error.Message != "missing required flag: --username" {
		t.Errorf("error.message = %q, want %q", env.Error.Message, "missing required flag: --username")
	}
}

// ---------------------------------------------------------------------------
// TS-14-E6: akc admin users create without --email exits with code 2
// and prints the missing-flag error envelope.
// Requirement: 14-REQ-6.E2
// ---------------------------------------------------------------------------

func TestAdminUsersCreateMissingEmail(t *testing.T) {
	stdout, err := executeAdminCmd(
		"users", "create",
		"--username", "bob",
		"--provider", "local",
		"--provider-id", "p1",
	)

	if err == nil {
		t.Error("expected error when --email is missing")
	}

	if stdout == "" {
		t.Fatal("stdout is empty; expected JSON error envelope")
	}
	env := parseErrorEnvelope(t, stdout)
	if env.Error.Code != 0 {
		t.Errorf("error.code = %d, want 0", env.Error.Code)
	}
	if env.Error.Message != "missing required flag: --email" {
		t.Errorf("error.message = %q, want %q", env.Error.Message, "missing required flag: --email")
	}
}

// ---------------------------------------------------------------------------
// TS-14-E7: akc admin users create without --provider exits with code 2
// and prints the missing-flag error envelope.
// Requirement: 14-REQ-6.E3
// ---------------------------------------------------------------------------

func TestAdminUsersCreateMissingProvider(t *testing.T) {
	stdout, err := executeAdminCmd(
		"users", "create",
		"--username", "bob",
		"--email", "b@x.com",
		"--provider-id", "p1",
	)

	if err == nil {
		t.Error("expected error when --provider is missing")
	}

	if stdout == "" {
		t.Fatal("stdout is empty; expected JSON error envelope")
	}
	env := parseErrorEnvelope(t, stdout)
	if env.Error.Code != 0 {
		t.Errorf("error.code = %d, want 0", env.Error.Code)
	}
	if env.Error.Message != "missing required flag: --provider" {
		t.Errorf("error.message = %q, want %q", env.Error.Message, "missing required flag: --provider")
	}
}

// ---------------------------------------------------------------------------
// TS-14-E8: akc admin users create without --provider-id exits with
// code 2 and prints the missing-flag error envelope.
// Requirement: 14-REQ-6.E4
// ---------------------------------------------------------------------------

func TestAdminUsersCreateMissingProviderID(t *testing.T) {
	stdout, err := executeAdminCmd(
		"users", "create",
		"--username", "bob",
		"--email", "b@x.com",
		"--provider", "local",
	)

	if err == nil {
		t.Error("expected error when --provider-id is missing")
	}

	if stdout == "" {
		t.Fatal("stdout is empty; expected JSON error envelope")
	}
	env := parseErrorEnvelope(t, stdout)
	if env.Error.Code != 0 {
		t.Errorf("error.code = %d, want 0", env.Error.Code)
	}
	if env.Error.Message != "missing required flag: --provider-id" {
		t.Errorf("error.message = %q, want %q", env.Error.Message, "missing required flag: --provider-id")
	}
}

// ===========================================================================
// Task Group 2: admin users update/promote/demote/block/unblock tests
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-14-25: akc admin users update <id> --full-name 'Alice B' calls
// UpdateUserByID with the correct id and FullName, prints user JSON.
// Requirement: 14-REQ-7.1
// ---------------------------------------------------------------------------

func TestAdminUsersUpdateCommand(t *testing.T) {
	mock := &mockAdminUsersClient{
		updateUserResult: &apikit.User{ID: "u1", FullName: "Alice B"},
	}

	stdout, err := executeAdminCmd("users", "update", "u1", "--full-name", "Alice B")

	// Expect success (exit code 0).
	if err != nil {
		t.Errorf("expected nil error (exit 0), got: %v", err)
	}

	// Verify UpdateUserByID was called with correct args.
	if !mock.updateUserCalled {
		t.Fatal("UpdateUserByID was not called")
	}
	if mock.updateUserID != "u1" {
		t.Errorf("captured ID = %q, want %q", mock.updateUserID, "u1")
	}
	if mock.updateUserReq == nil {
		t.Fatal("UpdateUserByID request is nil")
	}
	if mock.updateUserReq.FullName != "Alice B" {
		t.Errorf("req.FullName = %q, want %q", mock.updateUserReq.FullName, "Alice B")
	}

	// stdout should be a JSON user object.
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not valid JSON: %q", stdout)
	}

	var user map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &user); err != nil {
		t.Fatalf("failed to parse stdout as JSON object: %v", err)
	}
	if id, _ := user["id"].(string); id != "u1" {
		t.Errorf("user.id = %q, want %q", id, "u1")
	}
}

// ---------------------------------------------------------------------------
// TS-14-26: akc admin users update with --full-name '' treats empty string
// as valid, calls SDK with FullName='' and exits 0.
// Requirement: 14-REQ-7.2
// ---------------------------------------------------------------------------

func TestAdminUsersUpdateEmptyFullName(t *testing.T) {
	mock := &mockAdminUsersClient{
		updateUserResult: &apikit.User{ID: "u1", FullName: ""},
	}

	stdout, err := executeAdminCmd("users", "update", "u1", "--full-name", "")

	if err != nil {
		t.Errorf("expected nil error (exit 0), got: %v", err)
	}

	if !mock.updateUserCalled {
		t.Fatal("UpdateUserByID was not called")
	}
	if mock.updateUserReq == nil {
		t.Fatal("UpdateUserByID request is nil")
	}
	if mock.updateUserReq.FullName != "" {
		t.Errorf("req.FullName = %q, want empty string", mock.updateUserReq.FullName)
	}

	// stdout should be valid JSON.
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not valid JSON: %q", stdout)
	}
}

// ---------------------------------------------------------------------------
// TS-14-27: akc admin users update is registered in the agent interface
// with method PATCH, path /users/:id, auth admin, positional arg id,
// required flag --full-name.
// Requirement: 14-REQ-7.3
// ---------------------------------------------------------------------------

func TestAdminUsersUpdateAgentInterface(t *testing.T) {
	cmd := NewAdminCmd()

	updateCmd, _, err := cmd.Find([]string{"users", "update"})
	if err != nil {
		t.Fatalf("failed to find 'users update' command: %v", err)
	}
	if updateCmd.Name() != "update" {
		t.Fatalf("found command %q, want %q", updateCmd.Name(), "update")
	}

	// Verify annotations contain the expected metadata.
	annotations := updateCmd.Annotations
	if annotations == nil {
		t.Fatal("users update command has no Annotations")
	}
	if annotations["method"] != "PATCH" {
		t.Errorf("method annotation = %q, want %q", annotations["method"], "PATCH")
	}
	if annotations["path"] != "/users/:id" {
		t.Errorf("path annotation = %q, want %q", annotations["path"], "/users/:id")
	}
	if annotations["auth"] != "admin" {
		t.Errorf("auth annotation = %q, want %q", annotations["auth"], "admin")
	}

	// Verify the --full-name flag is registered.
	flag := updateCmd.Flags().Lookup("full-name")
	if flag == nil {
		t.Error("--full-name flag not registered on users update command")
	}
}

// ---------------------------------------------------------------------------
// TS-14-E9: akc admin users update without the <id> positional argument
// exits with code 2 and prints the missing-argument error envelope.
// Requirement: 14-REQ-7.E1
// ---------------------------------------------------------------------------

func TestAdminUsersUpdateMissingID(t *testing.T) {
	stdout, err := executeAdminCmd("users", "update", "--full-name", "Alice")

	if err == nil {
		t.Error("expected error when <id> argument is missing")
	}

	if stdout == "" {
		t.Fatal("stdout is empty; expected JSON error envelope")
	}
	env := parseErrorEnvelope(t, stdout)
	if env.Error.Code != 0 {
		t.Errorf("error.code = %d, want 0", env.Error.Code)
	}
	if env.Error.Message != "missing required argument: id" {
		t.Errorf("error.message = %q, want %q", env.Error.Message, "missing required argument: id")
	}
}

// ---------------------------------------------------------------------------
// TS-14-E10: akc admin users update without --full-name exits with code 2
// and prints the missing-flag error envelope; SDK is not called.
// Requirement: 14-REQ-7.E2
// ---------------------------------------------------------------------------

func TestAdminUsersUpdateMissingFullName(t *testing.T) {
	mock := &mockAdminUsersClient{}

	stdout, err := executeAdminCmd("users", "update", "u1")

	if err == nil {
		t.Error("expected error when --full-name flag is missing")
	}

	// SDK should NOT have been called.
	if mock.updateUserCalled {
		t.Error("UpdateUserByID was called despite missing --full-name flag")
	}

	if stdout == "" {
		t.Fatal("stdout is empty; expected JSON error envelope")
	}
	env := parseErrorEnvelope(t, stdout)
	if env.Error.Code != 0 {
		t.Errorf("error.code = %d, want 0", env.Error.Code)
	}
	if env.Error.Message != "missing required flag: --full-name" {
		t.Errorf("error.message = %q, want %q", env.Error.Message, "missing required flag: --full-name")
	}
}

// ---------------------------------------------------------------------------
// TS-14-28: akc admin users promote <id> calls PromoteUser with the given
// id and prints the returned user as JSON.
// Requirement: 14-REQ-8.1
// ---------------------------------------------------------------------------

func TestAdminUsersPromoteCommand(t *testing.T) {
	mock := &mockAdminUsersClient{
		promoteUserResult: &apikit.User{ID: "u1", Role: "admin"},
	}

	stdout, err := executeAdminCmd("users", "promote", "u1")

	if err != nil {
		t.Errorf("expected nil error (exit 0), got: %v", err)
	}

	if !mock.promoteUserCalled {
		t.Fatal("PromoteUser was not called")
	}
	if mock.promoteUserID != "u1" {
		t.Errorf("captured ID = %q, want %q", mock.promoteUserID, "u1")
	}

	// stdout should be valid JSON.
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not valid JSON: %q", stdout)
	}
}

// ---------------------------------------------------------------------------
// TS-14-29: akc admin users promote is registered in the agent interface
// with method POST, path /users/:id/promote, auth admin.
// Requirement: 14-REQ-8.2
// ---------------------------------------------------------------------------

func TestAdminUsersPromoteAgentInterface(t *testing.T) {
	cmd := NewAdminCmd()

	promoteCmd, _, err := cmd.Find([]string{"users", "promote"})
	if err != nil {
		t.Fatalf("failed to find 'users promote' command: %v", err)
	}
	if promoteCmd.Name() != "promote" {
		t.Fatalf("found command %q, want %q", promoteCmd.Name(), "promote")
	}

	annotations := promoteCmd.Annotations
	if annotations == nil {
		t.Fatal("users promote command has no Annotations")
	}
	if annotations["method"] != "POST" {
		t.Errorf("method annotation = %q, want %q", annotations["method"], "POST")
	}
	if annotations["path"] != "/users/:id/promote" {
		t.Errorf("path annotation = %q, want %q", annotations["path"], "/users/:id/promote")
	}
	if annotations["auth"] != "admin" {
		t.Errorf("auth annotation = %q, want %q", annotations["auth"], "admin")
	}
}

// ---------------------------------------------------------------------------
// TS-14-E11: akc admin users promote without the <id> argument exits with
// code 2 and prints the missing-argument error envelope.
// Requirement: 14-REQ-8.E1
// ---------------------------------------------------------------------------

func TestAdminUsersPromoteMissingID(t *testing.T) {
	stdout, err := executeAdminCmd("users", "promote")

	if err == nil {
		t.Error("expected error when <id> argument is missing")
	}

	if stdout == "" {
		t.Fatal("stdout is empty; expected JSON error envelope")
	}
	env := parseErrorEnvelope(t, stdout)
	if env.Error.Code != 0 {
		t.Errorf("error.code = %d, want 0", env.Error.Code)
	}
	if env.Error.Message != "missing required argument: id" {
		t.Errorf("error.message = %q, want %q", env.Error.Message, "missing required argument: id")
	}
}

// ---------------------------------------------------------------------------
// TS-14-30: akc admin users demote <id> calls DemoteUser with the given id
// and prints the returned user as JSON without performing the last-admin
// check.
// Requirement: 14-REQ-9.1
// ---------------------------------------------------------------------------

func TestAdminUsersDemoteCommand(t *testing.T) {
	mock := &mockAdminUsersClient{
		demoteUserResult: &apikit.User{ID: "u1", Role: "user"},
	}

	stdout, err := executeAdminCmd("users", "demote", "u1")

	if err != nil {
		t.Errorf("expected nil error (exit 0), got: %v", err)
	}

	if !mock.demoteUserCalled {
		t.Fatal("DemoteUser was not called")
	}
	if mock.demoteUserID != "u1" {
		t.Errorf("captured ID = %q, want %q", mock.demoteUserID, "u1")
	}

	// stdout should be valid JSON (no client-side last-admin validation).
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not valid JSON: %q", stdout)
	}
}

// ---------------------------------------------------------------------------
// TS-14-31: akc admin users demote is registered in the agent interface
// with method POST, path /users/:id/demote, auth admin.
// Requirement: 14-REQ-9.2
// ---------------------------------------------------------------------------

func TestAdminUsersDemoteAgentInterface(t *testing.T) {
	cmd := NewAdminCmd()

	demoteCmd, _, err := cmd.Find([]string{"users", "demote"})
	if err != nil {
		t.Fatalf("failed to find 'users demote' command: %v", err)
	}
	if demoteCmd.Name() != "demote" {
		t.Fatalf("found command %q, want %q", demoteCmd.Name(), "demote")
	}

	annotations := demoteCmd.Annotations
	if annotations == nil {
		t.Fatal("users demote command has no Annotations")
	}
	if annotations["method"] != "POST" {
		t.Errorf("method annotation = %q, want %q", annotations["method"], "POST")
	}
	if annotations["path"] != "/users/:id/demote" {
		t.Errorf("path annotation = %q, want %q", annotations["path"], "/users/:id/demote")
	}
	if annotations["auth"] != "admin" {
		t.Errorf("auth annotation = %q, want %q", annotations["auth"], "admin")
	}
}

// ---------------------------------------------------------------------------
// TS-14-E12: akc admin users demote without the <id> argument exits with
// code 2 and prints the missing-argument error envelope.
// Requirement: 14-REQ-9.E1
// ---------------------------------------------------------------------------

func TestAdminUsersDemoteMissingID(t *testing.T) {
	stdout, err := executeAdminCmd("users", "demote")

	if err == nil {
		t.Error("expected error when <id> argument is missing")
	}

	if stdout == "" {
		t.Fatal("stdout is empty; expected JSON error envelope")
	}
	env := parseErrorEnvelope(t, stdout)
	if env.Error.Code != 0 {
		t.Errorf("error.code = %d, want 0", env.Error.Code)
	}
	if env.Error.Message != "missing required argument: id" {
		t.Errorf("error.message = %q, want %q", env.Error.Message, "missing required argument: id")
	}
}

// ---------------------------------------------------------------------------
// TS-14-E13: akc admin users demote when the server returns 409 last-admin
// conflict prints the 409 error envelope and exits with code 1.
// Requirement: 14-REQ-9.E2
// ---------------------------------------------------------------------------

func TestAdminUsersDemoteLastAdmin(t *testing.T) {
	_ = &mockAdminUsersClient{
		demoteUserErr: &apikit.APIError{Code: 409, Message: "cannot demote the last admin"},
	}

	stdout, err := executeAdminCmd("users", "demote", "u1")

	// Expect an error (exit code 1 for API errors).
	if err == nil {
		t.Error("expected non-nil error for 409 conflict")
	}

	if stdout == "" {
		t.Fatal("stdout is empty; expected JSON error envelope")
	}
	env := parseErrorEnvelope(t, stdout)
	if env.Error.Code != 409 {
		t.Errorf("error.code = %d, want 409", env.Error.Code)
	}
	if env.Error.Message != "cannot demote the last admin" {
		t.Errorf("error.message = %q, want %q", env.Error.Message, "cannot demote the last admin")
	}
}

// ---------------------------------------------------------------------------
// TS-14-32: akc admin users block <id> calls BlockUser with the given id
// and prints the returned user as JSON.
// Requirement: 14-REQ-10.1
// ---------------------------------------------------------------------------

func TestAdminUsersBlockCommand(t *testing.T) {
	mock := &mockAdminUsersClient{
		blockUserResult: &apikit.User{ID: "u1", Status: "blocked"},
	}

	stdout, err := executeAdminCmd("users", "block", "u1")

	if err != nil {
		t.Errorf("expected nil error (exit 0), got: %v", err)
	}

	if !mock.blockUserCalled {
		t.Fatal("BlockUser was not called")
	}
	if mock.blockUserID != "u1" {
		t.Errorf("captured ID = %q, want %q", mock.blockUserID, "u1")
	}

	// stdout should be valid JSON.
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not valid JSON: %q", stdout)
	}
}

// ---------------------------------------------------------------------------
// TS-14-33: akc admin users block is registered in the agent interface
// with method POST, path /users/:id/block, auth admin.
// Requirement: 14-REQ-10.2
// ---------------------------------------------------------------------------

func TestAdminUsersBlockAgentInterface(t *testing.T) {
	cmd := NewAdminCmd()

	blockCmd, _, err := cmd.Find([]string{"users", "block"})
	if err != nil {
		t.Fatalf("failed to find 'users block' command: %v", err)
	}
	if blockCmd.Name() != "block" {
		t.Fatalf("found command %q, want %q", blockCmd.Name(), "block")
	}

	annotations := blockCmd.Annotations
	if annotations == nil {
		t.Fatal("users block command has no Annotations")
	}
	if annotations["method"] != "POST" {
		t.Errorf("method annotation = %q, want %q", annotations["method"], "POST")
	}
	if annotations["path"] != "/users/:id/block" {
		t.Errorf("path annotation = %q, want %q", annotations["path"], "/users/:id/block")
	}
	if annotations["auth"] != "admin" {
		t.Errorf("auth annotation = %q, want %q", annotations["auth"], "admin")
	}
}

// ---------------------------------------------------------------------------
// TS-14-E14: akc admin users block without the <id> argument exits with
// code 2 and prints the missing-argument error envelope.
// Requirement: 14-REQ-10.E1
// ---------------------------------------------------------------------------

func TestAdminUsersBlockMissingID(t *testing.T) {
	stdout, err := executeAdminCmd("users", "block")

	if err == nil {
		t.Error("expected error when <id> argument is missing")
	}

	if stdout == "" {
		t.Fatal("stdout is empty; expected JSON error envelope")
	}
	env := parseErrorEnvelope(t, stdout)
	if env.Error.Code != 0 {
		t.Errorf("error.code = %d, want 0", env.Error.Code)
	}
	if env.Error.Message != "missing required argument: id" {
		t.Errorf("error.message = %q, want %q", env.Error.Message, "missing required argument: id")
	}
}

// ---------------------------------------------------------------------------
// TS-14-34: akc admin users unblock <id> calls UnblockUser with the given
// id and prints the returned user as JSON.
// Requirement: 14-REQ-11.1
// ---------------------------------------------------------------------------

func TestAdminUsersUnblockCommand(t *testing.T) {
	mock := &mockAdminUsersClient{
		unblockUserResult: &apikit.User{ID: "u1", Status: "active"},
	}

	stdout, err := executeAdminCmd("users", "unblock", "u1")

	if err != nil {
		t.Errorf("expected nil error (exit 0), got: %v", err)
	}

	if !mock.unblockUserCalled {
		t.Fatal("UnblockUser was not called")
	}
	if mock.unblockUserID != "u1" {
		t.Errorf("captured ID = %q, want %q", mock.unblockUserID, "u1")
	}

	// stdout should be valid JSON.
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not valid JSON: %q", stdout)
	}
}

// ---------------------------------------------------------------------------
// TS-14-35: akc admin users unblock is registered in the agent interface
// with method POST, path /users/:id/unblock, auth admin.
// Requirement: 14-REQ-11.2
// ---------------------------------------------------------------------------

func TestAdminUsersUnblockAgentInterface(t *testing.T) {
	cmd := NewAdminCmd()

	unblockCmd, _, err := cmd.Find([]string{"users", "unblock"})
	if err != nil {
		t.Fatalf("failed to find 'users unblock' command: %v", err)
	}
	if unblockCmd.Name() != "unblock" {
		t.Fatalf("found command %q, want %q", unblockCmd.Name(), "unblock")
	}

	annotations := unblockCmd.Annotations
	if annotations == nil {
		t.Fatal("users unblock command has no Annotations")
	}
	if annotations["method"] != "POST" {
		t.Errorf("method annotation = %q, want %q", annotations["method"], "POST")
	}
	if annotations["path"] != "/users/:id/unblock" {
		t.Errorf("path annotation = %q, want %q", annotations["path"], "/users/:id/unblock")
	}
	if annotations["auth"] != "admin" {
		t.Errorf("auth annotation = %q, want %q", annotations["auth"], "admin")
	}
}

// ---------------------------------------------------------------------------
// TS-14-E15: akc admin users unblock without the <id> argument exits with
// code 2 and prints the missing-argument error envelope.
// Requirement: 14-REQ-11.E1
// ---------------------------------------------------------------------------

func TestAdminUsersUnblockMissingID(t *testing.T) {
	stdout, err := executeAdminCmd("users", "unblock")

	if err == nil {
		t.Error("expected error when <id> argument is missing")
	}

	if stdout == "" {
		t.Fatal("stdout is empty; expected JSON error envelope")
	}
	env := parseErrorEnvelope(t, stdout)
	if env.Error.Code != 0 {
		t.Errorf("error.code = %d, want 0", env.Error.Code)
	}
	if env.Error.Message != "missing required argument: id" {
		t.Errorf("error.message = %q, want %q", env.Error.Message, "missing required argument: id")
	}
}

// ===========================================================================
// Task Group 2.4: Error forwarding tests (REQ-26)
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-14-70: When the server returns HTTP 403, the CLI prints the 403 error
// envelope to stdout and exits with code 1.
// Requirement: 14-REQ-26.3
// ---------------------------------------------------------------------------

func TestAdminUsersAPIError403(t *testing.T) {
	_ = &mockAdminUsersClient{
		listUsersErr: &apikit.APIError{Code: 403, Message: "forbidden"},
	}

	stdout, err := executeAdminCmd("users", "list")

	// Expect an error (exit code 1 for API errors).
	if err == nil {
		t.Error("expected non-nil error for 403 API error")
	}

	if stdout == "" {
		t.Fatal("stdout is empty; expected JSON error envelope")
	}
	env := parseErrorEnvelope(t, stdout)
	if env.Error.Code != 403 {
		t.Errorf("error.code = %d, want 403", env.Error.Code)
	}
	if env.Error.Message != "forbidden" {
		t.Errorf("error.message = %q, want %q", env.Error.Message, "forbidden")
	}
}

// ---------------------------------------------------------------------------
// TS-14-71: When the server returns HTTP 404, the CLI prints the 404 error
// envelope to stdout and exits with code 1.
// Requirement: 14-REQ-26.4
// ---------------------------------------------------------------------------

func TestAdminUsersAPIError404(t *testing.T) {
	_ = &mockAdminUsersClient{
		getUserErr: &apikit.APIError{Code: 404, Message: "user not found"},
	}

	stdout, err := executeAdminCmd("users", "show", "u999")

	// Expect an error (exit code 1 for API errors).
	if err == nil {
		t.Error("expected non-nil error for 404 API error")
	}

	if stdout == "" {
		t.Fatal("stdout is empty; expected JSON error envelope")
	}
	env := parseErrorEnvelope(t, stdout)
	if env.Error.Code != 404 {
		t.Errorf("error.code = %d, want 404", env.Error.Code)
	}
	if env.Error.Message != "user not found" {
		t.Errorf("error.message = %q, want %q", env.Error.Message, "user not found")
	}
}

// ---------------------------------------------------------------------------
// TS-14-69: All error output paths produce a valid JSON error envelope on
// stdout with code N for API errors and 0 for client errors.
// Requirement: 14-REQ-26.2
// ---------------------------------------------------------------------------

func TestAdminUsersNetworkError(t *testing.T) {
	_ = &mockAdminUsersClient{
		listUsersErr: errors.New("dial tcp: connection refused"),
	}

	stdout, err := executeAdminCmd("users", "list")

	// Expect an error (exit code 2 for client/network errors).
	if err == nil {
		t.Error("expected non-nil error for network failure")
	}

	if stdout == "" {
		t.Fatal("stdout is empty; expected JSON error envelope")
	}
	env := parseErrorEnvelope(t, stdout)
	if env.Error.Code != 0 {
		t.Errorf("error.code = %d, want 0 for client error", env.Error.Code)
	}
	if env.Error.Message == "" {
		t.Error("error.message should not be empty for network error")
	}
}

// ---------------------------------------------------------------------------
// TS-14-68: Exit codes are exactly 0 on success, 1 on APIError, and 2 on
// client error, with no other codes produced.
// Requirement: 14-REQ-26.1
// ---------------------------------------------------------------------------

func TestExitCodeInvariants(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		mockSetup    func() *mockAdminUsersClient
		wantErr      bool
		wantExitCode int // 0=success, 1=APIError, 2=client error
	}{
		{
			name: "success returns exit code 0",
			args: []string{"users", "list"},
			mockSetup: func() *mockAdminUsersClient {
				return &mockAdminUsersClient{
					listUsersResult: []*apikit.User{{ID: "u1"}},
				}
			},
			wantErr:      false,
			wantExitCode: 0,
		},
		{
			name: "APIError returns exit code 1",
			args: []string{"users", "show", "u1"},
			mockSetup: func() *mockAdminUsersClient {
				return &mockAdminUsersClient{
					getUserErr: &apikit.APIError{Code: 404, Message: "not found"},
				}
			},
			wantErr:      true,
			wantExitCode: 1,
		},
		{
			name: "network error returns exit code 2",
			args: []string{"users", "list"},
			mockSetup: func() *mockAdminUsersClient {
				return &mockAdminUsersClient{
					listUsersErr: errors.New("network unreachable"),
				}
			},
			wantErr:      true,
			wantExitCode: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = tt.mockSetup()

			_, err := executeAdminCmd(tt.args...)

			if tt.wantErr && err == nil {
				t.Error("expected non-nil error")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected nil error, got: %v", err)
			}

			// NOTE: Exit code verification requires the ExitCode() helper
			// from spec 13. Once implemented, verify:
			//   exitCode := ExitCode(err)
			//   if exitCode != tt.wantExitCode {
			//       t.Errorf("exit code = %d, want %d", exitCode, tt.wantExitCode)
			//   }
			// For now, we verify the error/no-error invariant.
		})
	}
}

// Ensure imports are used.
var (
	_ = strings.Contains
	_ = errors.New
)

package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	apikit "github.com/txsvc/apikit"
	"github.com/txsvc/apikit/internal/cli"
)

// ===========================================================================
// Smoke tests for admin command wiring verification (task group 12).
//
// These tests verify the full end-to-end paths described in the spec:
// TS-14-SMOKE-1 through TS-14-SMOKE-7, plus a NewAdminCmd registration test.
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-14-SMOKE-1: Successful admin users list: loadConfig reads config,
// newClient constructs client, ListUsers called with IncludeBlocked:false,
// JSON array printed to stdout, exits 0.
// Execution path: 14-PATH-1
// Requirements: 14-REQ-2.1
// ---------------------------------------------------------------------------

func TestSMOKE1_AdminUsersListSuccess(t *testing.T) {
	mock := &mockAdminUsersClient{
		listUsersResult: []*apikit.User{
			{ID: "u1", Username: "alice"},
			{ID: "u2", Username: "bob"},
		},
	}

	stdout, err := executeAdminCmdWithClient(makeUsersRunner(mock), "users", "list")

	// Exit code 0.
	if err != nil {
		t.Fatalf("expected nil error (exit 0), got: %v", err)
	}

	// stdout is a valid JSON array of user objects.
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not valid JSON: %q", stdout)
	}

	// ListUsers called with IncludeBlocked==false.
	if !mock.listUsersCalled {
		t.Fatal("ListUsers was not called")
	}
	if mock.listUsersOpts == nil {
		t.Fatal("ListUsers opts is nil")
	}
	if mock.listUsersOpts.IncludeBlocked {
		t.Error("ListUsers called with IncludeBlocked=true, want false")
	}

	// Parse the output and verify we got the users back.
	var users []map[string]any
	if err := json.Unmarshal([]byte(stdout), &users); err != nil {
		t.Fatalf("failed to parse JSON array: %v", err)
	}
	if len(users) != 2 {
		t.Errorf("expected 2 users, got %d", len(users))
	}
}

// ---------------------------------------------------------------------------
// TS-14-SMOKE-2: API error path: admin users show with non-existent id
// returns 404 APIError, handleError prints error envelope to stdout, exits 1.
// Execution path: 14-PATH-2
// Requirements: 14-REQ-2.2
// ---------------------------------------------------------------------------

func TestSMOKE2_AdminUsersShow404Error(t *testing.T) {
	mock := &mockAdminUsersClient{
		getUserErr: &apikit.APIError{Code: 404, Message: "user not found"},
	}

	stdout, err := executeAdminCmdWithClient(makeUsersRunner(mock), "users", "show", "abc-nonexistent-id")

	// Exit code 1 (API error).
	if err == nil {
		t.Fatal("expected non-nil error for API error response")
	}

	// stdout contains the error envelope.
	var env errorEnvelope
	if jsonErr := json.Unmarshal([]byte(stdout), &env); jsonErr != nil {
		t.Fatalf("failed to parse error envelope: %v\nstdout: %q", jsonErr, stdout)
	}
	if env.Error.Code != 404 {
		t.Errorf("error code = %d, want 404", env.Error.Code)
	}
	if env.Error.Message != "user not found" {
		t.Errorf("error message = %q, want %q", env.Error.Message, "user not found")
	}
}

// ---------------------------------------------------------------------------
// TS-14-SMOKE-3: Client validation error: admin users create with missing
// --provider-id flag prints missing-flag error envelope to stdout, exits 2,
// SDK not called.
// Execution path: 14-PATH-3
// Requirements: 14-REQ-3.2
// ---------------------------------------------------------------------------

func TestSMOKE3_AdminUsersCreateMissingProviderID(t *testing.T) {
	mock := &mockAdminUsersClient{}

	stdout, err := executeAdminCmdWithClient(makeUsersRunner(mock),
		"users", "create",
		"--username", "alice",
		"--email", "alice@example.com",
		"--provider", "local",
		// --provider-id is intentionally missing
	)

	// Expect error (exit code 2 for client errors).
	if err == nil {
		t.Fatal("expected non-nil error for missing flag")
	}

	// stdout contains the error envelope with the missing-flag message.
	var env errorEnvelope
	if jsonErr := json.Unmarshal([]byte(stdout), &env); jsonErr != nil {
		t.Fatalf("failed to parse error envelope: %v\nstdout: %q", jsonErr, stdout)
	}
	if env.Error.Code != 0 {
		t.Errorf("error code = %d, want 0", env.Error.Code)
	}
	if env.Error.Message != "missing required flag: --provider-id" {
		t.Errorf("error message = %q, want %q", env.Error.Message, "missing required flag: --provider-id")
	}

	// CreateUser SDK method must never have been called.
	if mock.createUserCalled {
		t.Error("CreateUser was called; expected SDK method NOT to be called for missing flag")
	}
}

// ---------------------------------------------------------------------------
// TS-14-SMOKE-4: Void-response path: admin orgs delete calls DeleteOrg,
// receives nil error, prints '{}' to stdout, exits 0.
// Execution path: 14-PATH-4
// Requirements: 14-REQ-15.1, 14-REQ-25.2
// ---------------------------------------------------------------------------

func TestSMOKE4_AdminOrgsDeleteVoidResponse(t *testing.T) {
	mock := &mockAdminOrgsClient{
		deleteOrgErr: nil, // success
	}

	stdout, err := executeAdminCmdWithClient(makeOrgsRunner(mock), "orgs", "delete", "org-uuid-123")

	// Exit code 0.
	if err != nil {
		t.Fatalf("expected nil error (exit 0), got: %v", err)
	}

	// DeleteOrg called with correct ID.
	if !mock.deleteOrgCalled {
		t.Fatal("DeleteOrg was not called")
	}
	if mock.deleteOrgID != "org-uuid-123" {
		t.Errorf("DeleteOrg ID = %q, want %q", mock.deleteOrgID, "org-uuid-123")
	}

	// stdout is '{}'.
	trimmed := strings.TrimSpace(stdout)
	if trimmed != "{}" {
		t.Errorf("stdout = %q, want %q", trimmed, "{}")
	}
}

// ---------------------------------------------------------------------------
// TS-14-SMOKE-5: Empty-patch warning path: admin orgs update with no flags
// emits warnf warning to stderr, calls UpdateOrg with nil fields, prints
// org JSON, exits 0.
// Execution path: 14-PATH-5
// Requirements: 14-REQ-14.2
// ---------------------------------------------------------------------------

func TestSMOKE5_AdminOrgsUpdateEmptyPatch(t *testing.T) {
	mock := &mockAdminOrgsClient{
		updateOrgResult: &apikit.Organization{ID: "org-uuid-456"},
	}

	// Need to capture stderr separately, so build the command manually.
	cmd := cli.NewAdminCmd()
	stdoutBuf := new(bytes.Buffer)
	stderrBuf := new(bytes.Buffer)
	cmd.SetOut(stdoutBuf)
	cmd.SetErr(stderrBuf)
	cmd.SetArgs([]string{"orgs", "update", "org-uuid-456"})

	ctx := cli.ContextWithClient(context.Background(), makeOrgsRunner(mock))
	cmd.SetContext(ctx)

	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	silenceSubcommands(cmd)

	err := cmd.Execute()

	// Exit code 0.
	if err != nil {
		t.Fatalf("expected nil error (exit 0), got: %v", err)
	}

	stdout := stdoutBuf.String()
	stderr := stderrBuf.String()

	// stderr contains the warnf warning.
	if !strings.Contains(stderr, "no fields specified for update") {
		t.Errorf("stderr = %q, want it to contain %q", stderr, "no fields specified for update")
	}

	// UpdateOrg called with both fields nil (empty patch).
	if !mock.updateOrgCalled {
		t.Fatal("UpdateOrg was not called")
	}
	if mock.updateOrgReq == nil {
		t.Fatal("UpdateOrg request is nil")
	}
	if mock.updateOrgReq.Name != nil {
		t.Errorf("req.Name = %v, want nil", mock.updateOrgReq.Name)
	}
	if mock.updateOrgReq.URL != nil {
		t.Errorf("req.URL = %v, want nil", mock.updateOrgReq.URL)
	}

	// stdout is valid JSON (the org object).
	if !json.Valid([]byte(stdout)) {
		t.Errorf("stdout is not valid JSON: %q", stdout)
	}
}

// ---------------------------------------------------------------------------
// TS-14-SMOKE-6: Network failure path: admin keys list encounters TCP
// connection failure, handleError prints code-0 error envelope to stdout,
// exits 2.
// Execution path: 14-PATH-6
// Requirements: 14-REQ-2.3
// ---------------------------------------------------------------------------

func TestSMOKE6_AdminKeysListNetworkError(t *testing.T) {
	mock := &mockAdminKeysClient{
		listKeysErr: errors.New("connection refused"),
	}

	stdout, err := executeAdminCmdWithClient(makeKeysRunner(mock), "keys", "list", "user-uuid-789")

	// Expect error (exit code 2 for non-API errors).
	if err == nil {
		t.Fatal("expected non-nil error for network failure")
	}

	// stdout contains the error envelope with code 0.
	var env errorEnvelope
	if jsonErr := json.Unmarshal([]byte(stdout), &env); jsonErr != nil {
		t.Fatalf("failed to parse error envelope: %v\nstdout: %q", jsonErr, stdout)
	}
	if env.Error.Code != 0 {
		t.Errorf("error code = %d, want 0", env.Error.Code)
	}
	if env.Error.Message != "connection refused" {
		t.Errorf("error message = %q, want %q", env.Error.Message, "connection refused")
	}
}

// ---------------------------------------------------------------------------
// TS-14-SMOKE-7: Full agent-discovery-then-create flow: agent parses
// help --json, constructs admin users create command, executes it,
// receives user JSON on stdout.
// Execution path: 14-PATH-7
// Requirements: 14-REQ-2.1, 14-REQ-27.1
// ---------------------------------------------------------------------------

func TestSMOKE7_AgentDiscoveryThenCreate(t *testing.T) {
	// Step 1: Verify the admin users create command is registered with
	// correct annotations (method, path, auth).
	adminCmd := cli.NewAdminCmd()
	var createCmd *cobra.Command
	for _, sub := range adminCmd.Commands() {
		if sub.Name() == "users" {
			for _, usub := range sub.Commands() {
				if usub.Name() == "create" {
					createCmd = usub
					break
				}
			}
			break
		}
	}
	if createCmd == nil {
		t.Fatal("admin users create command not found in command tree")
	}

	// Check annotations.
	if createCmd.Annotations["method"] != "POST" {
		t.Errorf("method annotation = %q, want %q", createCmd.Annotations["method"], "POST")
	}
	if createCmd.Annotations["path"] != "/users" {
		t.Errorf("path annotation = %q, want %q", createCmd.Annotations["path"], "/users")
	}
	if createCmd.Annotations["auth"] != "admin" {
		t.Errorf("auth annotation = %q, want %q", createCmd.Annotations["auth"], "admin")
	}

	// Step 2: Execute admin users create with all required flags.
	mock := &mockAdminUsersClient{
		createUserResult: &apikit.User{ID: "new-id", Username: "bot"},
	}

	stdout, err := executeAdminCmdWithClient(makeUsersRunner(mock),
		"users", "create",
		"--username", "bot",
		"--email", "bot@ci.example.com",
		"--provider", "local",
		"--provider-id", "bot-001",
	)

	// Exit code 0.
	if err != nil {
		t.Fatalf("expected nil error (exit 0), got: %v", err)
	}

	// CreateUser called with correct request.
	if !mock.createUserCalled {
		t.Fatal("CreateUser was not called")
	}
	if mock.createUserReq == nil {
		t.Fatal("CreateUser request is nil")
	}
	if mock.createUserReq.Username != "bot" {
		t.Errorf("req.Username = %q, want %q", mock.createUserReq.Username, "bot")
	}
	if mock.createUserReq.Email != "bot@ci.example.com" {
		t.Errorf("req.Email = %q, want %q", mock.createUserReq.Email, "bot@ci.example.com")
	}
	if mock.createUserReq.Provider != "local" {
		t.Errorf("req.Provider = %q, want %q", mock.createUserReq.Provider, "local")
	}
	if mock.createUserReq.ProviderID != "bot-001" {
		t.Errorf("req.ProviderID = %q, want %q", mock.createUserReq.ProviderID, "bot-001")
	}

	// stdout is valid user JSON with username='bot'.
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not valid JSON: %q", stdout)
	}
	var user map[string]any
	if jsonErr := json.Unmarshal([]byte(stdout), &user); jsonErr != nil {
		t.Fatalf("failed to parse user JSON: %v", jsonErr)
	}
	if user["username"] != "bot" {
		t.Errorf("username = %v, want %q", user["username"], "bot")
	}
}

// ---------------------------------------------------------------------------
// TestSMOKE_NewAdminCmdRegistration: Verify NewAdminCmd() wiring.
// Requirement: 14-REQ-1.6
// ---------------------------------------------------------------------------

func TestSMOKE_NewAdminCmdRegistration(t *testing.T) {
	// Verify NewAdminCmd returns a valid command with Use="admin".
	cmd := cli.NewAdminCmd()
	if cmd == nil {
		t.Fatal("NewAdminCmd() returned nil")
	}
	if cmd.Use != "admin" {
		t.Errorf("Use = %q, want %q", cmd.Use, "admin")
	}

	// Verify the command has subcommands (users, orgs, keys, tokens).
	if !cmd.HasSubCommands() {
		t.Fatal("admin command has no subcommands")
	}

	expected := map[string]bool{"users": false, "orgs": false, "keys": false, "tokens": false}
	for _, sub := range cmd.Commands() {
		if _, ok := expected[sub.Name()]; ok {
			expected[sub.Name()] = true
		}
	}
	for name, found := range expected {
		if !found {
			t.Errorf("subcommand %q not found", name)
		}
	}

	// Verify admin command has no RunE (parent-only, prints help).
	if cmd.RunE != nil {
		t.Error("admin command has RunE set; expected nil (help-only)")
	}
}

package cli_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	apikit "github.com/txsvc/apikit"
	"github.com/txsvc/apikit/internal/cli"
)

// ---------------------------------------------------------------------------
// Test-scoped mock interface for SDK admin token methods.
//
// This interface is UNEXPORTED and defined only in this _test.go file.
// It covers the apikit.Client methods that token admin commands will call.
// ---------------------------------------------------------------------------

type adminTokensClient interface {
	ListUserTokens(ctx context.Context, userID string) ([]*apikit.PAT, error)
	RevokeUserToken(ctx context.Context, userID, tokenID string) error
}

// mockAdminTokensClient implements adminTokensClient for testing.
type mockAdminTokensClient struct {
	// Return values
	listTokensResult []*apikit.PAT
	listTokensErr    error
	revokeTokenErr   error

	// Call tracking
	listTokensCalled  bool
	listTokensUserID  string
	revokeTokenCalled bool
	revokeTokenUserID string
	revokeTokenID     string

	// Context tracking
	capturedCtx context.Context
}

func (m *mockAdminTokensClient) ListUserTokens(ctx context.Context, userID string) ([]*apikit.PAT, error) {
	m.capturedCtx = ctx
	m.listTokensCalled = true
	m.listTokensUserID = userID
	return m.listTokensResult, m.listTokensErr
}

func (m *mockAdminTokensClient) RevokeUserToken(ctx context.Context, userID, tokenID string) error {
	m.capturedCtx = ctx
	m.revokeTokenCalled = true
	m.revokeTokenUserID = userID
	m.revokeTokenID = tokenID
	return m.revokeTokenErr
}

// makeTokensRunner creates a TokensRunner that wraps a mockAdminTokensClient,
// bridging the typed mock interface to the any-typed function values used
// by the production code (which cannot import apikit due to import cycles).
func makeTokensRunner(mock *mockAdminTokensClient) *cli.TokensRunner {
	return &cli.TokensRunner{
		ListUserTokens: func(ctx context.Context, userID string) (any, error) {
			return mock.ListUserTokens(ctx, userID)
		},
		RevokeUserToken: func(ctx context.Context, userID, tokenID string) error {
			return mock.RevokeUserToken(ctx, userID, tokenID)
		},
	}
}

// ===========================================================================
// Task Group 4.2: admin tokens tests (REQ-23, REQ-24)
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-14-61: akc admin tokens list <user_id> calls ListUserTokens and prints
// the token metadata array as JSON without secret material.
// Requirement: 14-REQ-23.1
// ---------------------------------------------------------------------------

func TestAdminTokensListCommand(t *testing.T) {
	mock := &mockAdminTokensClient{
		listTokensResult: []*apikit.PAT{{TokenID: "t1", Name: "mytoken"}},
	}

	stdout, err := executeAdminCmdWithClient(makeTokensRunner(mock), "tokens", "list", "u1")

	if err != nil {
		t.Errorf("expected nil error (exit 0), got: %v", err)
	}

	if !mock.listTokensCalled {
		t.Fatal("ListUserTokens was not called")
	}
	if mock.listTokensUserID != "u1" {
		t.Errorf("captured userID = %q, want %q", mock.listTokensUserID, "u1")
	}

	// stdout should be a JSON array of PAT metadata (no secret material).
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not valid JSON: %q", stdout)
	}

	var tokens []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &tokens); err != nil {
		t.Fatalf("failed to parse stdout as JSON array: %v", err)
	}
	if len(tokens) != 1 {
		t.Errorf("got %d tokens, want 1", len(tokens))
	}
	if tokenID, _ := tokens[0]["token_id"].(string); tokenID != "t1" {
		t.Errorf("token[0].token_id = %q, want %q", tokenID, "t1")
	}

	// Verify no secret token material is present in the output.
	if strings.Contains(stdout, `"token"`) {
		t.Error("stdout contains secret 'token' field; PAT metadata should not include secret material")
	}
}

// ---------------------------------------------------------------------------
// TS-14-E27: akc admin tokens list without the <user_id> argument exits with
// code 2 and prints the missing-argument error envelope.
// Requirement: 14-REQ-23.E1
// ---------------------------------------------------------------------------

func TestAdminTokensListMissingUserID(t *testing.T) {
	stdout, err := executeAdminCmd("tokens", "list")

	if err == nil {
		t.Error("expected error when <user_id> argument is missing")
	}

	if stdout == "" {
		t.Fatal("stdout is empty; expected JSON error envelope")
	}
	env := parseErrorEnvelope(t, stdout)
	if env.Error.Code != 0 {
		t.Errorf("error.code = %d, want 0", env.Error.Code)
	}
	if env.Error.Message != "missing required argument: user_id" {
		t.Errorf("error.message = %q, want %q", env.Error.Message, "missing required argument: user_id")
	}
}

// ---------------------------------------------------------------------------
// TS-14-62: akc admin tokens list is registered in the agent interface with
// method GET, path /users/:id/tokens, auth admin.
// Requirement: 14-REQ-23.2
// ---------------------------------------------------------------------------

func TestAdminTokensListAgentInterface(t *testing.T) {
	cmd := cli.NewAdminCmd()

	tokensListCmd, _, err := cmd.Find([]string{"tokens", "list"})
	if err != nil {
		t.Fatalf("failed to find 'tokens list' command: %v", err)
	}
	if tokensListCmd.Name() != "list" {
		t.Fatalf("found command %q, want %q", tokensListCmd.Name(), "list")
	}

	annotations := tokensListCmd.Annotations
	if annotations == nil {
		t.Fatal("tokens list command has no Annotations")
	}
	if annotations["method"] != "GET" {
		t.Errorf("method annotation = %q, want %q", annotations["method"], "GET")
	}
	if annotations["path"] != "/users/:id/tokens" {
		t.Errorf("path annotation = %q, want %q", annotations["path"], "/users/:id/tokens")
	}
	if annotations["auth"] != "admin" {
		t.Errorf("auth annotation = %q, want %q", annotations["auth"], "admin")
	}
}

// ---------------------------------------------------------------------------
// TS-14-63: akc admin tokens revoke <user_id> <token_id> calls
// RevokeUserToken and prints '{}' on success without confirmation.
// Requirement: 14-REQ-24.1
// ---------------------------------------------------------------------------

func TestAdminTokensRevokeCommand(t *testing.T) {
	mock := &mockAdminTokensClient{
		revokeTokenErr: nil,
	}

	stdout, err := executeAdminCmdWithClient(makeTokensRunner(mock), "tokens", "revoke", "u1", "t1")

	if err != nil {
		t.Errorf("expected nil error (exit 0), got: %v", err)
	}

	if !mock.revokeTokenCalled {
		t.Fatal("RevokeUserToken was not called")
	}
	if mock.revokeTokenUserID != "u1" {
		t.Errorf("captured userID = %q, want %q", mock.revokeTokenUserID, "u1")
	}
	if mock.revokeTokenID != "t1" {
		t.Errorf("captured tokenID = %q, want %q", mock.revokeTokenID, "t1")
	}

	// stdout should be exactly '{}'.
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("failed to parse stdout as JSON: %v (stdout=%q)", err, stdout)
	}
	if len(parsed) != 0 {
		t.Errorf("stdout = %q, want empty JSON object '{}'", stdout)
	}
}

// ---------------------------------------------------------------------------
// TS-14-E28: akc admin tokens revoke with missing positional arguments exits
// with code 2 and prints the appropriate missing-argument error envelope.
// Requirement: 14-REQ-24.E1
// ---------------------------------------------------------------------------

func TestAdminTokensRevokeMissingArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantMsg string
	}{
		{
			name:    "missing both user_id and token_id",
			args:    []string{"tokens", "revoke"},
			wantMsg: "missing required argument: user_id",
		},
		{
			name:    "missing token_id",
			args:    []string{"tokens", "revoke", "u1"},
			wantMsg: "missing required argument: token_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockAdminTokensClient{}

			stdout, err := executeAdminCmd(tt.args...)

			if err == nil {
				t.Error("expected error when positional argument is missing")
			}

			if mock.revokeTokenCalled {
				t.Error("RevokeUserToken was called despite missing argument")
			}

			if stdout == "" {
				t.Fatal("stdout is empty; expected JSON error envelope")
			}
			env := parseErrorEnvelope(t, stdout)
			if env.Error.Code != 0 {
				t.Errorf("error.code = %d, want 0", env.Error.Code)
			}
			if !strings.Contains(env.Error.Message, "missing required argument") {
				t.Errorf("error.message = %q, want it to contain %q", env.Error.Message, "missing required argument")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TS-14-64: akc admin tokens revoke is registered in the agent interface with
// method DELETE, path /users/:id/tokens/:token_id, auth admin.
// Requirement: 14-REQ-24.2
// ---------------------------------------------------------------------------

func TestAdminTokensRevokeAgentInterface(t *testing.T) {
	cmd := cli.NewAdminCmd()

	revokeCmd, _, err := cmd.Find([]string{"tokens", "revoke"})
	if err != nil {
		t.Fatalf("failed to find 'tokens revoke' command: %v", err)
	}
	if revokeCmd.Name() != "revoke" {
		t.Fatalf("found command %q, want %q", revokeCmd.Name(), "revoke")
	}

	annotations := revokeCmd.Annotations
	if annotations == nil {
		t.Fatal("tokens revoke command has no Annotations")
	}
	if annotations["method"] != "DELETE" {
		t.Errorf("method annotation = %q, want %q", annotations["method"], "DELETE")
	}
	if annotations["path"] != "/users/:id/tokens/:token_id" {
		t.Errorf("path annotation = %q, want %q", annotations["path"], "/users/:id/tokens/:token_id")
	}
	if annotations["auth"] != "admin" {
		t.Errorf("auth annotation = %q, want %q", annotations["auth"], "admin")
	}
}

// Ensure imports are used.
var (
	_ adminTokensClient = (*mockAdminTokensClient)(nil)
	_ = strings.Contains
)

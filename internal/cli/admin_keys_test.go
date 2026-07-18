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
// Test-scoped mock interface for SDK admin key methods.
//
// This interface is UNEXPORTED and defined only in this _test.go file.
// It covers the apikit.Client methods that key admin commands will call.
// ---------------------------------------------------------------------------

type adminKeysClient interface {
	ListUserKeys(ctx context.Context, userID string) ([]*apikit.APIKeyMeta, error)
	RevokeUserKey(ctx context.Context, userID, keyID string) error
}

// mockAdminKeysClient implements adminKeysClient for testing.
type mockAdminKeysClient struct {
	// Return values
	listKeysResult []*apikit.APIKeyMeta
	listKeysErr    error
	revokeKeyErr   error

	// Call tracking
	listKeysCalled  bool
	listKeysUserID  string
	revokeKeyCalled bool
	revokeKeyUserID string
	revokeKeyKeyID  string

	// Context tracking
	capturedCtx context.Context
}

func (m *mockAdminKeysClient) ListUserKeys(ctx context.Context, userID string) ([]*apikit.APIKeyMeta, error) {
	m.capturedCtx = ctx
	m.listKeysCalled = true
	m.listKeysUserID = userID
	return m.listKeysResult, m.listKeysErr
}

func (m *mockAdminKeysClient) RevokeUserKey(ctx context.Context, userID, keyID string) error {
	m.capturedCtx = ctx
	m.revokeKeyCalled = true
	m.revokeKeyUserID = userID
	m.revokeKeyKeyID = keyID
	return m.revokeKeyErr
}

// makeKeysRunner creates a KeysRunner that wraps a mockAdminKeysClient,
// bridging the typed mock interface to the any-typed function values used
// by the production code (which cannot import apikit due to import cycles).
func makeKeysRunner(mock *mockAdminKeysClient) *cli.KeysRunner {
	return &cli.KeysRunner{
		ListUserKeys: func(ctx context.Context, userID string) (any, error) {
			return mock.ListUserKeys(ctx, userID)
		},
		RevokeUserKey: func(ctx context.Context, userID, keyID string) error {
			return mock.RevokeUserKey(ctx, userID, keyID)
		},
	}
}

// ===========================================================================
// Task Group 4.2: admin keys tests (REQ-21, REQ-22)
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-14-57: akc admin keys list <user_id> calls ListUserKeys and prints the
// key metadata array as JSON without secret key material.
// Requirement: 14-REQ-21.1
// ---------------------------------------------------------------------------

func TestAdminKeysListCommand(t *testing.T) {
	mock := &mockAdminKeysClient{
		listKeysResult: []*apikit.APIKeyMeta{{KeyID: "k1"}},
	}

	stdout, err := executeAdminCmdWithClient(makeKeysRunner(mock), "keys", "list", "u1")

	if err != nil {
		t.Errorf("expected nil error (exit 0), got: %v", err)
	}

	if !mock.listKeysCalled {
		t.Fatal("ListUserKeys was not called")
	}
	if mock.listKeysUserID != "u1" {
		t.Errorf("captured userID = %q, want %q", mock.listKeysUserID, "u1")
	}

	// stdout should be a JSON array of APIKeyMeta (no secret material).
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not valid JSON: %q", stdout)
	}

	var keys []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &keys); err != nil {
		t.Fatalf("failed to parse stdout as JSON array: %v", err)
	}
	if len(keys) != 1 {
		t.Errorf("got %d keys, want 1", len(keys))
	}
	if keyID, _ := keys[0]["key_id"].(string); keyID != "k1" {
		t.Errorf("key[0].key_id = %q, want %q", keyID, "k1")
	}

	// Verify no secret key material is present in the output.
	if strings.Contains(stdout, `"key"`) {
		t.Error("stdout contains secret 'key' field; APIKeyMeta should not include secret material")
	}
}

// ---------------------------------------------------------------------------
// TS-14-E25: akc admin keys list without the <user_id> argument exits with
// code 2 and prints the missing-argument error envelope.
// Requirement: 14-REQ-21.E1
// ---------------------------------------------------------------------------

func TestAdminKeysListMissingUserID(t *testing.T) {
	stdout, err := executeAdminCmd("keys", "list")

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
// TS-14-58: akc admin keys list is registered in the agent interface with
// method GET, path /users/:id/keys, auth admin.
// Requirement: 14-REQ-21.2
// ---------------------------------------------------------------------------

func TestAdminKeysListAgentInterface(t *testing.T) {
	cmd := cli.NewAdminCmd()

	keysListCmd, _, err := cmd.Find([]string{"keys", "list"})
	if err != nil {
		t.Fatalf("failed to find 'keys list' command: %v", err)
	}
	if keysListCmd.Name() != "list" {
		t.Fatalf("found command %q, want %q", keysListCmd.Name(), "list")
	}

	annotations := keysListCmd.Annotations
	if annotations == nil {
		t.Fatal("keys list command has no Annotations")
	}
	if annotations["method"] != "GET" {
		t.Errorf("method annotation = %q, want %q", annotations["method"], "GET")
	}
	if annotations["path"] != "/users/:id/keys" {
		t.Errorf("path annotation = %q, want %q", annotations["path"], "/users/:id/keys")
	}
	if annotations["auth"] != "admin" {
		t.Errorf("auth annotation = %q, want %q", annotations["auth"], "admin")
	}
}

// ---------------------------------------------------------------------------
// TS-14-59: akc admin keys revoke <user_id> <key_id> calls RevokeUserKey
// and prints '{}' on success without a confirmation prompt.
// Requirement: 14-REQ-22.1
// ---------------------------------------------------------------------------

func TestAdminKeysRevokeCommand(t *testing.T) {
	mock := &mockAdminKeysClient{
		revokeKeyErr: nil,
	}

	stdout, err := executeAdminCmdWithClient(makeKeysRunner(mock), "keys", "revoke", "u1", "k1")

	if err != nil {
		t.Errorf("expected nil error (exit 0), got: %v", err)
	}

	if !mock.revokeKeyCalled {
		t.Fatal("RevokeUserKey was not called")
	}
	if mock.revokeKeyUserID != "u1" {
		t.Errorf("captured userID = %q, want %q", mock.revokeKeyUserID, "u1")
	}
	if mock.revokeKeyKeyID != "k1" {
		t.Errorf("captured keyID = %q, want %q", mock.revokeKeyKeyID, "k1")
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
// TS-14-E26: akc admin keys revoke with missing positional arguments exits
// with code 2 and prints the appropriate missing-argument error envelope.
// Requirement: 14-REQ-22.E1
// ---------------------------------------------------------------------------

func TestAdminKeysRevokeMissingArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantMsg string
	}{
		{
			name:    "missing both user_id and key_id",
			args:    []string{"keys", "revoke"},
			wantMsg: "missing required argument: user_id",
		},
		{
			name:    "missing key_id",
			args:    []string{"keys", "revoke", "u1"},
			wantMsg: "missing required argument: key_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockAdminKeysClient{}

			stdout, err := executeAdminCmd(tt.args...)

			if err == nil {
				t.Error("expected error when positional argument is missing")
			}

			if mock.revokeKeyCalled {
				t.Error("RevokeUserKey was called despite missing argument")
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
// TS-14-60: akc admin keys revoke is registered in the agent interface with
// method DELETE, path /users/:id/keys/:key_id, auth admin.
// Requirement: 14-REQ-22.2
// ---------------------------------------------------------------------------

func TestAdminKeysRevokeAgentInterface(t *testing.T) {
	cmd := cli.NewAdminCmd()

	revokeCmd, _, err := cmd.Find([]string{"keys", "revoke"})
	if err != nil {
		t.Fatalf("failed to find 'keys revoke' command: %v", err)
	}
	if revokeCmd.Name() != "revoke" {
		t.Fatalf("found command %q, want %q", revokeCmd.Name(), "revoke")
	}

	annotations := revokeCmd.Annotations
	if annotations == nil {
		t.Fatal("keys revoke command has no Annotations")
	}
	if annotations["method"] != "DELETE" {
		t.Errorf("method annotation = %q, want %q", annotations["method"], "DELETE")
	}
	if annotations["path"] != "/users/:id/keys/:key_id" {
		t.Errorf("path annotation = %q, want %q", annotations["path"], "/users/:id/keys/:key_id")
	}
	if annotations["auth"] != "admin" {
		t.Errorf("auth annotation = %q, want %q", annotations["auth"], "admin")
	}
}

// Ensure imports are used.
var (
	_ adminKeysClient = (*mockAdminKeysClient)(nil)
	_ = strings.Contains
)

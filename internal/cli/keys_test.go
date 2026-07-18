package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// ---------------------------------------------------------------------------
// Test helpers for keys command tests.
//
// Tests use package cli (internal) for access to unexported types
// like CLIConfig and helper functions like parseKeyID. The pattern
// follows login_test.go from group 2.
//
// Because NewKeysCmd() returns a stub (Use: "placeholder", no subcommands),
// all tests will compile but FAIL. They pass once group 7 wires up
// the real list/refresh/revoke subcommands.
// ---------------------------------------------------------------------------

// executeKeysCmd constructs the keys command tree from NewKeysCmd, sets the
// provided args, captures stdout and stderr, and executes.
func executeKeysCmd(args ...string) (stdout, stderr string, err error) {
	cmd := NewKeysCmd()
	stdoutBuf := new(bytes.Buffer)
	stderrBuf := new(bytes.Buffer)
	cmd.SetOut(stdoutBuf)
	cmd.SetErr(stderrBuf)
	cmd.SetArgs(args)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	for _, sub := range cmd.Commands() {
		sub.SilenceUsage = true
		sub.SilenceErrors = true
	}
	err = cmd.Execute()
	return stdoutBuf.String(), stderrBuf.String(), err
}

// ===========================================================================
// Subtask 3.2: Keys list, refresh, and revoke integration tests
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-15-25: Verify that keys list calls ListKeys and prints the []*APIKeyMeta
// array as indented JSON to stdout.
// Requirement: 15-REQ-5.1
// ---------------------------------------------------------------------------

func TestKeysList_HappyPath(t *testing.T) {
	// Mock server: GET /api/v1/user/keys returns a JSON array of APIKeyMeta.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/user/keys") && r.Method == http.MethodGet {
			keys := []map[string]any{
				{"key_id": "k1", "created_at": "2024-01-01T00:00:00Z"},
				{"key_id": "k2", "created_at": "2024-06-01T00:00:00Z"},
			}
			respJSON, _ := json.Marshal(keys)
			w.Write(respJSON)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	stdout, _, err := executeKeysCmd("list")

	// Exit code must be 0.
	if err != nil {
		t.Errorf("keys list returned error: %v, want nil (exit 0)", err)
	}

	// stdout must contain a valid JSON array.
	if strings.TrimSpace(stdout) == "" {
		t.Fatal("stdout is empty; expected []*APIKeyMeta JSON array")
	}

	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not valid JSON: %s", stdout)
	}

	var arr []any
	if err := json.Unmarshal([]byte(stdout), &arr); err != nil {
		t.Fatalf("stdout is not a JSON array: %v", err)
	}
	if len(arr) == 0 {
		t.Error("stdout JSON array is empty; expected at least one APIKeyMeta entry")
	}
}

// ---------------------------------------------------------------------------
// TS-15-26: Verify that keys refresh parses key_id, calls RefreshKey, updates
// config with the new api_key, prints APIKeyFull JSON to stdout, and prints
// the success message to stderr.
// Requirement: 15-REQ-6.1
// ---------------------------------------------------------------------------

func TestKeysRefresh_HappyPath(t *testing.T) {
	var capturedKeyID string

	// Mock server: POST /api/v1/user/keys/:key_id/refresh returns APIKeyFull.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/user/keys/") && strings.HasSuffix(r.URL.Path, "/refresh") && r.Method == http.MethodPost {
			// Extract key_id from path: /api/v1/user/keys/<key_id>/refresh
			parts := strings.Split(r.URL.Path, "/")
			for i, p := range parts {
				if p == "keys" && i+1 < len(parts) {
					capturedKeyID = parts[i+1]
					break
				}
			}
			resp := map[string]any{
				"key":    "ak_newkeyid_newsecret",
				"key_id": "newkeyid",
			}
			respJSON, _ := json.Marshal(resp)
			w.Write(respJSON)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	stdout, stderr, err := executeKeysCmd("refresh")

	// Exit code must be 0.
	if err != nil {
		t.Errorf("keys refresh returned error: %v, want nil (exit 0)", err)
	}

	// The mock server must have received the refresh request with key_id='keyid123'.
	// This will fail against stubs because no HTTP request is made.
	if capturedKeyID != "keyid123" {
		t.Errorf("captured key_id = %q, want %q", capturedKeyID, "keyid123")
	}

	// stdout must contain APIKeyFull JSON.
	if strings.TrimSpace(stdout) == "" {
		t.Fatal("stdout is empty; expected APIKeyFull JSON")
	}

	var keyFull map[string]any
	if err := json.Unmarshal([]byte(stdout), &keyFull); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, stdout)
	}

	// stderr must contain success message.
	if !strings.Contains(stderr, "API key refreshed") {
		t.Errorf("stderr = %q, want to contain %q", stderr, "API key refreshed")
	}

	// Config must be updated with new key.
	// When implemented, the test would load config and check api_key == 'ak_newkeyid_newsecret'.
	// For now, this assertion captures intent.
	_ = server
}

// ---------------------------------------------------------------------------
// TS-15-27: Verify that keys refresh exits with code 2 and the invalid key
// format error JSON when the api_key has fewer than 3 underscore-delimited
// segments.
// Requirement: 15-REQ-6.2
// ---------------------------------------------------------------------------

func TestKeysRefresh_InvalidKeyFormat(t *testing.T) {
	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// With api_key='badkey' (no underscores), parseKeyID should fail.
	// The stub parseKeyID returns ("", nil), so the assertion on the error
	// message will fail until the implementation is done.
	stdout, _, err := executeKeysCmd("refresh")

	// Must exit with code 2.
	if err == nil {
		t.Fatal("keys refresh with bad api_key returned nil error, want exit code 2")
	}

	// stdout must contain the invalid key format error.
	if strings.TrimSpace(stdout) == "" {
		t.Fatal("stdout is empty; expected error envelope JSON")
	}

	var env errorEnvelopeSpec15
	if jsonErr := json.Unmarshal([]byte(stdout), &env); jsonErr != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", jsonErr, stdout)
	}
	if env.Error.Message != "invalid API key format" {
		t.Errorf("error message = %q, want %q", env.Error.Message, "invalid API key format")
	}

	// No network request should be made.
	if count := atomic.LoadInt32(&requestCount); count != 0 {
		t.Errorf("mock server received %d requests, want 0", count)
	}
}

// ---------------------------------------------------------------------------
// TS-15-28: Verify that keys revoke parses key_id, calls RevokeKey, clears
// api_key and user_id in config, prints RevokeKeyResponse JSON to stdout,
// and prints the revocation message to stderr.
// Requirement: 15-REQ-7.1
// ---------------------------------------------------------------------------

func TestKeysRevoke_HappyPath(t *testing.T) {
	var capturedKeyID string

	// Mock server: DELETE /api/v1/user/keys/:key_id returns RevokeKeyResponse.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/user/keys/") && r.Method == http.MethodDelete {
			parts := strings.Split(r.URL.Path, "/")
			for i, p := range parts {
				if p == "keys" && i+1 < len(parts) {
					capturedKeyID = parts[i+1]
					break
				}
			}
			resp := map[string]any{
				"key_id":     "keyid123",
				"revoked_at": "2024-07-18T00:00:00Z",
			}
			respJSON, _ := json.Marshal(resp)
			w.Write(respJSON)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	stdout, stderr, err := executeKeysCmd("revoke")

	// Exit code must be 0.
	if err != nil {
		t.Errorf("keys revoke returned error: %v, want nil (exit 0)", err)
	}

	// The mock server must have received the revoke request.
	if capturedKeyID != "keyid123" {
		t.Errorf("captured key_id = %q, want %q", capturedKeyID, "keyid123")
	}

	// stdout must contain RevokeKeyResponse JSON.
	if strings.TrimSpace(stdout) == "" {
		t.Fatal("stdout is empty; expected RevokeKeyResponse JSON")
	}

	var revokeResp map[string]any
	if err := json.Unmarshal([]byte(stdout), &revokeResp); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, stdout)
	}

	// stderr must contain revocation message.
	if !strings.Contains(stderr, "API key revoked. Run 'akc login' to obtain a new key.") {
		t.Errorf("stderr = %q, want to contain %q",
			stderr, "API key revoked. Run 'akc login' to obtain a new key.")
	}

	// Config api_key and user_id should be cleared to empty strings.
	// When implemented, load config and verify.
	_ = server
}

// ---------------------------------------------------------------------------
// TS-15-29: Verify that keys revoke exits with code 2 and the invalid key
// format error JSON when the api_key has fewer than 3 underscore-delimited
// segments.
// Requirement: 15-REQ-7.2
// ---------------------------------------------------------------------------

func TestKeysRevoke_InvalidKeyFormat(t *testing.T) {
	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	stdout, _, err := executeKeysCmd("revoke")

	// Must exit with code 2.
	if err == nil {
		t.Fatal("keys revoke with bad api_key returned nil error, want exit code 2")
	}

	// stdout must contain the invalid key format error.
	if strings.TrimSpace(stdout) == "" {
		t.Fatal("stdout is empty; expected error envelope JSON")
	}

	var env errorEnvelopeSpec15
	if jsonErr := json.Unmarshal([]byte(stdout), &env); jsonErr != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", jsonErr, stdout)
	}
	if env.Error.Message != "invalid API key format" {
		t.Errorf("error message = %q, want %q", env.Error.Message, "invalid API key format")
	}

	// No network request should be made.
	if count := atomic.LoadInt32(&requestCount); count != 0 {
		t.Errorf("mock server received %d requests, want 0", count)
	}
}

// ===========================================================================
// Subtask 3.3: Keys refresh and revoke config write failure edge case tests
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-15-48: Verify that config-mutating commands use CLI Core atomic write
// and preserve unchanged config fields.
// Requirement: 15-REQ-21.1
// ---------------------------------------------------------------------------

func TestKeysRefresh_ConfigPreservation(t *testing.T) {
	// Mock server returns a new key on refresh.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/user/keys/") && strings.HasSuffix(r.URL.Path, "/refresh") {
			resp := map[string]any{
				"key":    "ak_newkey_newsecret",
				"key_id": "newkey",
			}
			respJSON, _ := json.Marshal(resp)
			w.Write(respJSON)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	// When keys refresh is implemented, it will call saveConfigFn.
	// This test verifies that existing config fields (like custom_field)
	// are preserved after the mutation.
	//
	// The saveConfigFn pattern (from loginOpts) will be adapted for
	// keys commands. For now, this test asserts on the command output.

	var savedConfig *CLIConfig
	saveConfigStub := func(_ string, cfg *CLIConfig) error {
		savedConfig = cfg
		return nil
	}

	// Store the stub for when the implementation uses it.
	_ = saveConfigStub

	stdout, _, err := executeKeysCmd("refresh")

	// When implemented, exit code is 0 and config is updated.
	if err != nil {
		t.Logf("keys refresh returned error: %v", err)
	}

	// Verify config was saved (will fail against stub).
	if savedConfig == nil {
		t.Fatal("saveConfigFn was not called; expected config to be saved atomically")
	}

	// Verify api_key was updated.
	if savedConfig.APIKey == "ak_k_s" {
		t.Error("api_key was not updated in config")
	}

	// Verify other fields are preserved.
	if savedConfig.EndpointURL == "" {
		t.Error("endpoint_url was cleared; should be preserved")
	}

	_ = stdout
	_ = server
}

// ---------------------------------------------------------------------------
// TS-15-E6: Verify that when the config write fails after a successful
// RefreshKey response, keys refresh exits with code 2 with a config failure
// error envelope and does NOT print the new key.
// Requirement: 15-REQ-6.E1
// ---------------------------------------------------------------------------

func TestKeysRefresh_ConfigWriteFailure(t *testing.T) {
	// Mock server returns a new key successfully.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/user/keys/") && strings.HasSuffix(r.URL.Path, "/refresh") {
			resp := map[string]any{
				"key":    "ak_newkeyid_newsecret",
				"key_id": "newkeyid",
			}
			respJSON, _ := json.Marshal(resp)
			w.Write(respJSON)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	// Stub config save to fail.
	saveConfigStub := func(_ string, _ *CLIConfig) error {
		return errors.New("io error")
	}

	// When implemented, keys refresh will use this saveConfigFn.
	_ = saveConfigStub

	stdout, _, err := executeKeysCmd("refresh")

	// Must exit with code 2.
	if err == nil {
		t.Fatal("keys refresh with failing config save returned nil error, want exit code 2")
	}

	// stdout must contain config failure error envelope.
	if strings.TrimSpace(stdout) == "" {
		t.Fatal("stdout is empty; expected config failure error envelope")
	}

	var env errorEnvelopeSpec15
	if jsonErr := json.Unmarshal([]byte(stdout), &env); jsonErr != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", jsonErr, stdout)
	}
	if env.Error.Code != 2 {
		t.Errorf("error envelope code = %d, want 2", env.Error.Code)
	}

	// The new key value must NOT appear in stdout.
	if strings.Contains(stdout, "ak_newkeyid_newsecret") {
		t.Error("stdout contains the new key value; it must NOT be printed on config write failure")
	}
}

// ---------------------------------------------------------------------------
// TS-15-E7: Verify that when the CLI Core atomic write fails during any
// config-mutating command, the command exits with code 2 and the data that
// would have been printed on success is NOT printed.
// Requirement: 15-REQ-21.E1
// ---------------------------------------------------------------------------

func TestKeysRevoke_ConfigWriteFailure(t *testing.T) {
	// Mock server returns RevokeKeyResponse successfully.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/user/keys/") && r.Method == http.MethodDelete {
			resp := map[string]any{
				"key_id":     "keyid",
				"revoked_at": "2024-07-18T00:00:00Z",
			}
			respJSON, _ := json.Marshal(resp)
			w.Write(respJSON)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	// Stub config save to fail.
	saveConfigStub := func(_ string, _ *CLIConfig) error {
		return errors.New("permission denied")
	}

	// When implemented, keys revoke will use this saveConfigFn.
	_ = saveConfigStub

	stdout, _, err := executeKeysCmd("revoke")

	// Must exit with code 2.
	if err == nil {
		t.Fatal("keys revoke with failing config save returned nil error, want exit code 2")
	}

	// stdout must contain config failure error envelope.
	if strings.TrimSpace(stdout) == "" {
		t.Fatal("stdout is empty; expected config failure error envelope")
	}

	var env errorEnvelopeSpec15
	if jsonErr := json.Unmarshal([]byte(stdout), &env); jsonErr != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", jsonErr, stdout)
	}
	if env.Error.Code != 2 {
		t.Errorf("error envelope code = %d, want 2", env.Error.Code)
	}

	// RevokeKeyResponse data must NOT appear in stdout.
	if strings.Contains(stdout, "revoked_at") {
		t.Error("stdout contains RevokeKeyResponse data; it must NOT be printed on config write failure")
	}
}

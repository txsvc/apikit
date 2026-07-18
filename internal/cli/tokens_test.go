package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// ---------------------------------------------------------------------------
// Test helpers for tokens command tests.
//
// Tests use package cli (internal) for access to unexported types.
// Because NewTokensCmd() returns a stub (Use: "placeholder", no subcommands),
// all tests will compile but FAIL. They pass once group 8 wires up the
// real list/create/show/revoke subcommands.
// ---------------------------------------------------------------------------

// executeTokensCmd constructs the tokens command tree from NewTokensCmd, sets
// the provided args, captures stdout and stderr, and executes.
func executeTokensCmd(args ...string) (stdout, stderr string, err error) {
	cmd := NewTokensCmd()
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
// Subtask 3.4: Tokens list, create (happy path and validation) tests
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-15-30: Verify that tokens list calls ListTokens and prints the []*PAT
// array as indented JSON to stdout.
// Requirement: 15-REQ-8.1
// ---------------------------------------------------------------------------

func TestTokensList_HappyPath(t *testing.T) {
	// Mock server: GET /api/v1/user/tokens returns a JSON array of PATs.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/user/tokens") && r.Method == http.MethodGet {
			pats := []map[string]any{
				{
					"token_id":    "tok-1",
					"name":        "ci-bot",
					"permissions": []string{"users:read"},
					"created_at":  "2024-01-01T00:00:00Z",
				},
				{
					"token_id":    "tok-2",
					"name":        "deploy",
					"permissions": []string{"orgs:read", "users:read"},
					"created_at":  "2024-06-01T00:00:00Z",
				},
			}
			respJSON, _ := json.Marshal(pats)
			w.Write(respJSON)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	stdout, _, err := executeTokensCmd("list")

	// Exit code must be 0.
	if err != nil {
		t.Errorf("tokens list returned error: %v, want nil (exit 0)", err)
	}

	// stdout must contain a valid JSON array.
	if strings.TrimSpace(stdout) == "" {
		t.Fatal("stdout is empty; expected []*PAT JSON array")
	}

	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not valid JSON: %s", stdout)
	}

	var arr []any
	if err := json.Unmarshal([]byte(stdout), &arr); err != nil {
		t.Fatalf("stdout is not a JSON array: %v", err)
	}
	if len(arr) == 0 {
		t.Error("stdout JSON array is empty; expected at least one PAT entry")
	}
}

// ---------------------------------------------------------------------------
// TS-15-31: Verify that tokens create parses permissions, validates expires,
// calls CreateToken, prints PATFull JSON to stdout, and prints the warning
// to stderr.
// Requirement: 15-REQ-9.1
// ---------------------------------------------------------------------------

func TestTokensCreate_HappyPath(t *testing.T) {
	var capturedBody map[string]any

	// Mock server: POST /api/v1/user/tokens captures request body and returns PATFull.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/user/tokens") && r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &capturedBody)

			resp := map[string]any{
				"token":       "pat_plaintext_value",
				"token_id":    "tok-new",
				"name":        "ci-bot",
				"permissions": []string{"users:read", "orgs:read"},
			}
			respJSON, _ := json.Marshal(resp)
			w.Write(respJSON)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	stdout, stderr, err := executeTokensCmd(
		"create",
		"--name", "ci-bot",
		"--permissions", "users:read,orgs:read",
		"--expires", "30",
	)

	// Exit code must be 0.
	if err != nil {
		t.Errorf("tokens create returned error: %v, want nil (exit 0)", err)
	}

	// Mock server must have received the POST with correct body.
	if capturedBody == nil {
		t.Fatal("mock server did not receive POST request body")
	}
	if capturedBody["name"] != "ci-bot" {
		t.Errorf("captured name = %v, want %q", capturedBody["name"], "ci-bot")
	}

	// permissions must be an array of two strings.
	perms, ok := capturedBody["permissions"].([]any)
	if !ok {
		t.Fatalf("captured permissions is not an array: %T", capturedBody["permissions"])
	}
	expectedPerms := []string{"users:read", "orgs:read"}
	if len(perms) != len(expectedPerms) {
		t.Errorf("captured permissions length = %d, want %d", len(perms), len(expectedPerms))
	}
	for i, p := range expectedPerms {
		if i < len(perms) && perms[i] != p {
			t.Errorf("captured permissions[%d] = %v, want %q", i, perms[i], p)
		}
	}

	// expires must be 30.
	expiresVal, _ := capturedBody["expires"].(float64)
	if int(expiresVal) != 30 {
		t.Errorf("captured expires = %v, want 30", capturedBody["expires"])
	}

	// stdout must contain PATFull JSON.
	if strings.TrimSpace(stdout) == "" {
		t.Fatal("stdout is empty; expected PATFull JSON")
	}

	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not valid JSON: %s", stdout)
	}

	// stderr must contain the save-token warning.
	if !strings.Contains(stderr, "Save the token value") {
		t.Errorf("stderr = %q, want to contain %q", stderr, "Save the token value")
	}
}

// ---------------------------------------------------------------------------
// TS-15-32: Verify that tokens create exits with code 2 and the empty
// permissions error JSON when --permissions resolves to an empty slice.
// Requirement: 15-REQ-9.2
// ---------------------------------------------------------------------------

func TestTokensCreate_EmptyPermissions(t *testing.T) {
	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	stdout, _, err := executeTokensCmd(
		"create",
		"--name", "ci-bot",
		"--permissions", "",
		"--expires", "90",
	)

	// Must exit with code 2.
	if err == nil {
		t.Fatal("tokens create with empty permissions returned nil error, want exit code 2")
	}

	// stdout must contain the empty permissions error.
	if strings.TrimSpace(stdout) == "" {
		t.Fatal("stdout is empty; expected error envelope JSON")
	}

	var env errorEnvelopeSpec15
	if jsonErr := json.Unmarshal([]byte(stdout), &env); jsonErr != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", jsonErr, stdout)
	}
	if env.Error.Message != "--permissions must not be empty" {
		t.Errorf("error message = %q, want %q", env.Error.Message, "--permissions must not be empty")
	}

	// No network request should be made.
	if count := atomic.LoadInt32(&requestCount); count != 0 {
		t.Errorf("mock server received %d requests, want 0", count)
	}
}

// ---------------------------------------------------------------------------
// TS-15-33: Verify that tokens create exits with code 2 and the expires
// validation error JSON when --expires is an invalid value.
// Requirement: 15-REQ-9.3
// ---------------------------------------------------------------------------

func TestTokensCreate_InvalidExpires(t *testing.T) {
	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	stdout, _, err := executeTokensCmd(
		"create",
		"--name", "ci-bot",
		"--permissions", "users:read",
		"--expires", "15",
	)

	// Must exit with code 2.
	if err == nil {
		t.Fatal("tokens create with invalid expires returned nil error, want exit code 2")
	}

	// stdout must contain the expires validation error.
	if strings.TrimSpace(stdout) == "" {
		t.Fatal("stdout is empty; expected error envelope JSON")
	}

	var env errorEnvelopeSpec15
	if jsonErr := json.Unmarshal([]byte(stdout), &env); jsonErr != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", jsonErr, stdout)
	}
	if env.Error.Message != "--expires must be 0, 30, 60, or 90" {
		t.Errorf("error message = %q, want %q",
			env.Error.Message, "--expires must be 0, 30, 60, or 90")
	}

	// No network request should be made.
	if count := atomic.LoadInt32(&requestCount); count != 0 {
		t.Errorf("mock server received %d requests, want 0", count)
	}
}

// ===========================================================================
// Subtask 3.5: Tokens show and revoke integration tests
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-15-34: Verify that tokens show calls GetToken with the positional
// token_id argument and prints response.Data as indented JSON to stdout.
// Requirement: 15-REQ-10.1
// ---------------------------------------------------------------------------

func TestTokensShow_HappyPath(t *testing.T) {
	var capturedPath string

	// Mock server: GET /api/v1/user/tokens/{id} returns Response[PAT].
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/user/tokens/") && r.Method == http.MethodGet {
			resp := map[string]any{
				"token_id":    "tok-abc123",
				"name":        "ci-bot",
				"permissions": []string{"users:read"},
				"created_at":  "2024-01-01T00:00:00Z",
			}
			respJSON, _ := json.Marshal(resp)
			w.Write(respJSON)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	stdout, _, err := executeTokensCmd("show", "tok-abc123")

	// Exit code must be 0.
	if err != nil {
		t.Errorf("tokens show returned error: %v, want nil (exit 0)", err)
	}

	// The token_id must appear in the request path.
	if !strings.Contains(capturedPath, "tok-abc123") {
		t.Errorf("request path = %q, want to contain %q", capturedPath, "tok-abc123")
	}

	// stdout must contain PAT JSON.
	if strings.TrimSpace(stdout) == "" {
		t.Fatal("stdout is empty; expected PAT JSON")
	}

	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not valid JSON: %s", stdout)
	}

	var pat map[string]any
	if err := json.Unmarshal([]byte(stdout), &pat); err != nil {
		t.Fatalf("failed to parse stdout as JSON: %v", err)
	}
	if pat["token_id"] != "tok-abc123" {
		t.Errorf("stdout token_id = %v, want %q", pat["token_id"], "tok-abc123")
	}
}

// ---------------------------------------------------------------------------
// TS-15-35: Verify that tokens revoke calls RevokeToken with the positional
// token_id, prints {} to stdout on success (HTTP 204), and prints the
// revocation message to stderr.
// Requirement: 15-REQ-11.1
// Validates: 15-PROP-9
// ---------------------------------------------------------------------------

func TestTokensRevoke_HappyPath(t *testing.T) {
	var capturedPath string

	// Mock server: DELETE /api/v1/user/tokens/{id} returns HTTP 204 (no body).
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		if strings.Contains(r.URL.Path, "/user/tokens/") && r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	stdout, stderr, err := executeTokensCmd("revoke", "tok-abc123")

	// Exit code must be 0.
	if err != nil {
		t.Errorf("tokens revoke returned error: %v, want nil (exit 0)", err)
	}

	// The token_id must appear in the request path.
	if !strings.Contains(capturedPath, "tok-abc123") {
		t.Errorf("request path = %q, want to contain %q", capturedPath, "tok-abc123")
	}

	// stdout must contain exactly '{}' (trimmed whitespace).
	trimmedStdout := strings.TrimSpace(stdout)
	if trimmedStdout != "{}" {
		t.Errorf("stdout = %q, want %q", trimmedStdout, "{}")
	}

	// stderr must contain revocation message with the token_id.
	if !strings.Contains(stderr, "Token tok-abc123 revoked") {
		t.Errorf("stderr = %q, want to contain %q", stderr, "Token tok-abc123 revoked")
	}
}

// ---------------------------------------------------------------------------
// Property test TS-15-P9: tokens revoke always emits valid JSON '{}' to
// stdout regardless of the token_id value.
// Validates: 15-PROP-9
// ---------------------------------------------------------------------------

func TestTokensRevoke_Property_AlwaysEmitsEmptyJSON(t *testing.T) {
	tokenIDs := []string{
		"tok-abc123",
		"tok-with-dashes",
		"12345",
		"uuid-aaaa-bbbb-cccc-dddd",
	}

	for _, tokenID := range tokenIDs {
		t.Run(tokenID, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodDelete {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				http.NotFound(w, r)
			}))
			defer server.Close()

			stdout, stderr, err := executeTokensCmd("revoke", tokenID)

			if err != nil {
				t.Errorf("tokens revoke %q returned error: %v", tokenID, err)
			}

			trimmedStdout := strings.TrimSpace(stdout)
			if trimmedStdout != "{}" {
				t.Errorf("stdout for token %q = %q, want %q", tokenID, trimmedStdout, "{}")
			}

			expectedMsg := "Token " + tokenID + " revoked"
			if !strings.Contains(stderr, expectedMsg) {
				t.Errorf("stderr = %q, want to contain %q", stderr, expectedMsg)
			}
		})
	}
}

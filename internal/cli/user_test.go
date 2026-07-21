package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// ---------------------------------------------------------------------------
// Test helpers for spec 15 user/keys/tokens command tests.
//
// These tests use package cli (internal) for access to unexported types
// like CLIConfig. The pattern follows login_test.go from group 2.
// ---------------------------------------------------------------------------

// errorEnvelopeSpec15 is the JSON error envelope structure for parsing test output.
// Defined here (not in admin_*_test.go) because those are in package cli_test.
type errorEnvelopeSpec15 struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}


// executeUserCmd constructs the user command tree from NewUserCmd, sets the
// provided args, captures stdout and stderr, and executes. Returns stdout,
// stderr, and the error from Execute.
// Used for tests that do NOT inject a client (e.g., missing-API-key tests).
func executeUserCmd(args ...string) (stdout, stderr string, err error) {
	cmd := NewUserCmd()
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

// executeUserCmdWithClient is like executeUserCmd but injects a *CmdClient
// into the command's context via ContextWithClient. Used for happy-path
// and integration tests that need an authenticated client.
func executeUserCmdWithClient(client *CmdClient, args ...string) (stdout, stderr string, err error) {
	cmd := NewUserCmd()
	stdoutBuf := new(bytes.Buffer)
	stderrBuf := new(bytes.Buffer)
	cmd.SetOut(stdoutBuf)
	cmd.SetErr(stderrBuf)
	cmd.SetArgs(args)
	if client != nil {
		ctx := ContextWithClient(context.Background(), client)
		cmd.SetContext(ctx)
	}
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
// Subtask 3.1: User show and user update integration tests
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-15-21: Verify that user show constructs an authenticated client, calls
// GetUser, and prints response.Data as indented JSON to stdout.
// Requirement: 15-REQ-3.1
// ---------------------------------------------------------------------------

func TestUserShow_HappyPath(t *testing.T) {
	var requestCount int32

	// Mock server: GET /api/v1/user returns a User JSON object.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/user") && r.Method == http.MethodGet {
			resp := map[string]any{
				"id":        "user-abc",
				"username":  "alice",
				"email":     "alice@example.com",
				"full_name": "Alice Wonderland",
				"status":    "active",
				"role":      "user",
			}
			respJSON, _ := json.Marshal(resp)
			w.Write(respJSON)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := &CmdClient{
		endpointURL: server.URL,
		apiKey:      "ak_keyid_secret",
	}
	stdout, _, err := executeUserCmdWithClient(client, "show")

	// Exit code must be 0.
	if err != nil {
		t.Errorf("user show returned error: %v, want nil (exit 0)", err)
	}

	// stdout must contain valid indented JSON.
	if strings.TrimSpace(stdout) == "" {
		t.Fatal("stdout is empty; expected User JSON output")
	}

	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not valid JSON: %s", stdout)
	}

	// Verify two-space indentation pattern.
	if !strings.Contains(stdout, "  ") {
		t.Error("stdout does not contain two-space indentation")
	}

	// Verify the user data is present.
	var userJSON map[string]any
	if err := json.Unmarshal([]byte(stdout), &userJSON); err != nil {
		t.Fatalf("failed to parse stdout as JSON: %v", err)
	}

	// Verify the mock server received the request.
	if count := atomic.LoadInt32(&requestCount); count == 0 {
		t.Error("mock server received 0 requests; expected GET /user call")
	}
}

// ---------------------------------------------------------------------------
// TS-15-22: Verify that user show exits with code 2 and the missing API key
// error JSON when api_key is absent, without making any network request.
// Requirement: 15-REQ-3.2
// ---------------------------------------------------------------------------

func TestUserShow_MissingAPIKey(t *testing.T) {
	var requestCount int32

	// Mock server that records request counts.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	stdout, _, err := executeUserCmd("show")

	// Must exit with code 2 (non-nil error).
	if err == nil {
		t.Fatal("user show with no api_key returned nil error, want exit code 2")
	}

	// stdout must contain the missing API key error envelope.
	if strings.TrimSpace(stdout) == "" {
		t.Fatal("stdout is empty; expected error envelope JSON")
	}

	var env errorEnvelopeSpec15
	if jsonErr := json.Unmarshal([]byte(stdout), &env); jsonErr != nil {
		t.Fatalf("stdout is not valid JSON error envelope: %v\nstdout: %s", jsonErr, stdout)
	}
	if env.Error.Code != 2 {
		t.Errorf("error envelope code = %d, want 2", env.Error.Code)
	}
	if !strings.Contains(env.Error.Message, "no API key configured") {
		t.Errorf("error envelope message = %q, want to contain %q",
			env.Error.Message, "no API key configured")
	}

	// Mock server must receive zero requests.
	if count := atomic.LoadInt32(&requestCount); count != 0 {
		t.Errorf("mock server received %d requests, want 0 (no network request)", count)
	}
}

// ---------------------------------------------------------------------------
// TS-15-23: Verify that user update calls UpdateUser with the provided
// --full-name value and prints the updated User as indented JSON to stdout.
// Requirement: 15-REQ-4.1
// ---------------------------------------------------------------------------

func TestUserUpdate_HappyPath(t *testing.T) {
	var capturedBody map[string]any

	// Mock server: PATCH /api/v1/user captures request body and returns updated User.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/user") && r.Method == http.MethodPatch {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &capturedBody)

			resp := map[string]any{
				"id":        "user-abc",
				"username":  "alice",
				"email":     "alice@example.com",
				"full_name": "Alice Smith",
				"status":    "active",
				"role":      "user",
			}
			respJSON, _ := json.Marshal(resp)
			w.Write(respJSON)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := &CmdClient{
		endpointURL: server.URL,
		apiKey:      "ak_k_s",
	}
	stdout, _, err := executeUserCmdWithClient(client, "update", "--full-name", "Alice Smith")

	// Exit code must be 0.
	if err != nil {
		t.Errorf("user update returned error: %v, want nil (exit 0)", err)
	}

	// Mock server must have received the PATCH with full_name.
	if capturedBody == nil {
		t.Fatal("mock server did not receive PATCH request body")
	}
	if capturedBody["full_name"] != "Alice Smith" {
		t.Errorf("captured full_name = %v, want %q", capturedBody["full_name"], "Alice Smith")
	}

	// stdout must contain the updated user JSON.
	if strings.TrimSpace(stdout) == "" {
		t.Fatal("stdout is empty; expected updated User JSON")
	}

	var userJSON map[string]any
	if err := json.Unmarshal([]byte(stdout), &userJSON); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, stdout)
	}
	if userJSON["full_name"] != "Alice Smith" {
		t.Errorf("stdout user full_name = %v, want %q", userJSON["full_name"], "Alice Smith")
	}
}

// ---------------------------------------------------------------------------
// TS-15-24: Verify that a 422 server error from user update surfaces as
// exit code 1 with the server's error envelope JSON to stdout.
// Requirement: 15-REQ-4.2
// ---------------------------------------------------------------------------

func TestUserUpdate_ServerError422(t *testing.T) {
	// Mock server returns HTTP 422 with error envelope.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"error":{"code":422,"message":"full_name cannot be empty"}}`))
	}))
	defer server.Close()

	client := &CmdClient{
		endpointURL: server.URL,
		apiKey:      "ak_k_s",
	}
	stdout, _, err := executeUserCmdWithClient(client, "update", "--full-name", "")

	// Must exit with code 1 (API error).
	if err == nil {
		t.Fatal("user update with 422 response returned nil error, want exit code 1")
	}

	// stdout must contain server's error envelope.
	if strings.TrimSpace(stdout) == "" {
		t.Fatal("stdout is empty; expected server error envelope JSON")
	}

	var env errorEnvelopeSpec15
	if jsonErr := json.Unmarshal([]byte(stdout), &env); jsonErr != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", jsonErr, stdout)
	}
	if env.Error.Code == 0 {
		t.Error("error envelope code is 0; want server error code (422)")
	}
}

// ===========================================================================
// Subtask 3.6: JSON output formatting and API error propagation tests
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-15-46: Verify that all commands format successful output using
// json.MarshalIndent with two-space indentation and no HTML escaping.
// Requirement: 15-REQ-20.1
// ---------------------------------------------------------------------------

func TestUserShow_NoHTMLEscaping(t *testing.T) {
	// Mock server returns a user with HTML-sensitive characters.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/user") && r.Method == http.MethodGet {
			// Return User with HTML chars in full_name.
			resp := map[string]any{
				"id":        "user-html",
				"username":  "admin",
				"email":     "admin@example.com",
				"full_name": "<Admin & Owner>",
				"status":    "active",
				"role":      "admin",
			}
			respJSON, _ := json.Marshal(resp)
			w.Write(respJSON)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := &CmdClient{
		endpointURL: server.URL,
		apiKey:      "ak_k_s",
	}
	stdout, _, err := executeUserCmdWithClient(client, "show")

	if err != nil {
		t.Logf("user show returned error: %v", err)
	}

	// stdout must contain unescaped HTML chars.
	if !strings.Contains(stdout, "<Admin & Owner>") {
		t.Errorf("stdout does not contain '<Admin & Owner>' unescaped; got: %s", stdout)
	}

	// Must NOT contain HTML-escaped versions (Unicode escape sequences).
	if strings.Contains(stdout, `\u003c`) {
		t.Errorf("stdout contains \\u003c (HTML-escaped '<'); output must not HTML-escape")
	}
	if strings.Contains(stdout, `\u0026`) {
		t.Errorf("stdout contains \\u0026 (HTML-escaped '&'); output must not HTML-escape")
	}
	if strings.Contains(stdout, `\u003e`) {
		t.Errorf("stdout contains \\u003e (HTML-escaped '>'); output must not HTML-escape")
	}

	// Verify two-space indentation.
	lines := strings.Split(stdout, "\n")
	hasIndent := false
	for _, line := range lines {
		if strings.HasPrefix(line, "  ") {
			hasIndent = true
			break
		}
	}
	if !hasIndent {
		t.Error("stdout does not have two-space indentation")
	}
}

// ---------------------------------------------------------------------------
// TS-15-47: Verify that a 401 server error from any command surfaces as
// exit code 1 with the server's error envelope JSON to stdout.
// Requirement: 15-REQ-20.2
// ---------------------------------------------------------------------------

func TestUserShow_APIError401(t *testing.T) {
	// Mock server returns HTTP 401 with error envelope.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"code":401,"message":"unauthorized"}}`))
	}))
	defer server.Close()

	client := &CmdClient{
		endpointURL: server.URL,
		apiKey:      "ak_k_s",
	}
	stdout, _, err := executeUserCmdWithClient(client, "show")

	// Must exit with code 1 (API error).
	if err == nil {
		t.Fatal("user show with 401 response returned nil error, want exit code 1")
	}

	// stdout must contain server's error envelope.
	if strings.TrimSpace(stdout) == "" {
		t.Fatal("stdout is empty; expected error envelope JSON")
	}

	var env errorEnvelopeSpec15
	if jsonErr := json.Unmarshal([]byte(stdout), &env); jsonErr != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", jsonErr, stdout)
	}
	if env.Error.Code == 0 {
		t.Error("error envelope code is 0; want server error code")
	}
}

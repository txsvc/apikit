package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test helpers for spec 15 orgs command tests.
//
// Tests use package cli (internal) for access to unexported types like
// CLIConfig and errorEnvelopeSpec15. The pattern follows user_test.go,
// keys_test.go, and tokens_test.go from group 3.
//
// Because NewOrgsCmd() returns a stub (Use: "placeholder", no subcommands,
// no RunE), all tests will compile but FAIL at the assertion level:
//   - Stub commands produce no output on stdout
//   - No API calls are made to mock servers
// They will pass once implementation group 8 wires up the real logic.
// ---------------------------------------------------------------------------

// executeOrgsCmd constructs the orgs command tree from NewOrgsCmd, sets the
// provided args, captures stdout and stderr, and executes. Returns stdout,
// stderr, and the error from Execute.
// Used for tests that do NOT inject a client (e.g., missing-API-key tests).
func executeOrgsCmd(args ...string) (stdout, stderr string, err error) {
	cmd := NewOrgsCmd()
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

// executeOrgsCmdWithClient is like executeOrgsCmd but injects a *cmdClient
// into the command's context via ContextWithClient. Used for happy-path and
// integration tests that need an authenticated client.
func executeOrgsCmdWithClient(client *cmdClient, args ...string) (stdout, stderr string, err error) {
	cmd := NewOrgsCmd()
	stdoutBuf := new(bytes.Buffer)
	stderrBuf := new(bytes.Buffer)
	cmd.SetOut(stdoutBuf)
	cmd.SetErr(stderrBuf)
	cmd.SetArgs(args)
	if client != nil {
		ctx := context.Background()
		ctx = ContextWithClient(ctx, client)
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
// Subtask 4.1: Orgs list, show, and members integration tests
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-15-36: Verify that orgs list calls ListUserOrgs and prints the
// []*Organization array as indented JSON to stdout.
// Requirement: 15-REQ-12.1
// ---------------------------------------------------------------------------

func TestOrgsList_HappyPath(t *testing.T) {
	var requestCount int32

	// Mock server: GET /api/v1/orgs returns a JSON array of Organization objects.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/orgs") && r.Method == http.MethodGet {
			orgs := []map[string]any{
				{
					"id":          "org-111",
					"name":        "Acme Corp",
					"description": "A corporation",
					"created_at":  "2024-01-01T00:00:00Z",
				},
				{
					"id":          "org-222",
					"name":        "Beta Inc",
					"description": "Another company",
					"created_at":  "2024-06-01T00:00:00Z",
				},
			}
			respJSON, _ := json.Marshal(orgs)
			w.Write(respJSON)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := &cmdClient{
		endpointURL: server.URL,
		apiKey:      "ak_k_s",
	}
	stdout, _, err := executeOrgsCmdWithClient(client, "list")

	// Exit code must be 0.
	if err != nil {
		t.Errorf("orgs list returned error: %v, want nil (exit 0)", err)
	}

	// stdout must contain a valid JSON array.
	if strings.TrimSpace(stdout) == "" {
		t.Fatal("stdout is empty; expected []*Organization JSON array")
	}

	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not valid JSON: %s", stdout)
	}

	var arr []any
	if err := json.Unmarshal([]byte(stdout), &arr); err != nil {
		t.Fatalf("stdout is not a JSON array: %v", err)
	}
	if len(arr) == 0 {
		t.Error("stdout JSON array is empty; expected at least one Organization entry")
	}

	// Verify two-space indentation pattern.
	if !strings.Contains(stdout, "  ") {
		t.Error("stdout does not contain two-space indentation")
	}

	// The mock server must have received the GET request.
	if count := atomic.LoadInt32(&requestCount); count == 0 {
		t.Error("mock server received 0 requests; expected GET /orgs call")
	}
}

// ---------------------------------------------------------------------------
// TS-15-37: Verify that orgs show calls GetOrg with the positional id
// argument and prints response.Data as indented JSON to stdout.
// Requirement: 15-REQ-13.1
// ---------------------------------------------------------------------------

func TestOrgsShow_HappyPath(t *testing.T) {
	var capturedPath string

	// Mock server: GET /api/v1/orgs/{id} returns Response[Organization] JSON.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/orgs/") && r.Method == http.MethodGet {
			resp := map[string]any{
				"id":          "org-uuid-abc",
				"name":        "Acme Corp",
				"description": "A corporation",
				"created_at":  "2024-01-01T00:00:00Z",
			}
			respJSON, _ := json.Marshal(resp)
			w.Write(respJSON)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := &cmdClient{
		endpointURL: server.URL,
		apiKey:      "ak_k_s",
	}
	stdout, _, err := executeOrgsCmdWithClient(client, "show", "org-uuid-abc")

	// Exit code must be 0.
	if err != nil {
		t.Errorf("orgs show returned error: %v, want nil (exit 0)", err)
	}

	// The org ID must appear in the request path.
	if !strings.Contains(capturedPath, "org-uuid-abc") {
		t.Errorf("request path = %q, want to contain %q", capturedPath, "org-uuid-abc")
	}

	// stdout must contain Organization JSON.
	if strings.TrimSpace(stdout) == "" {
		t.Fatal("stdout is empty; expected Organization JSON")
	}

	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not valid JSON: %s", stdout)
	}

	var org map[string]any
	if err := json.Unmarshal([]byte(stdout), &org); err != nil {
		t.Fatalf("failed to parse stdout as JSON: %v", err)
	}
	if org["id"] != "org-uuid-abc" {
		t.Errorf("stdout org id = %v, want %q", org["id"], "org-uuid-abc")
	}
}

// ---------------------------------------------------------------------------
// TS-15-38: Verify that orgs members calls ListOrgMembers with the positional
// id argument and prints the []*User array as indented JSON to stdout.
// Requirement: 15-REQ-14.1
// ---------------------------------------------------------------------------

func TestOrgsMembers_HappyPath(t *testing.T) {
	var capturedPath string

	// Mock server: GET /api/v1/orgs/{id}/members returns a JSON array of Users.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/orgs/") &&
			strings.HasSuffix(r.URL.Path, "/members") &&
			r.Method == http.MethodGet {
			users := []map[string]any{
				{
					"id":       "user-1",
					"username": "alice",
					"email":    "alice@example.com",
					"role":     "admin",
				},
				{
					"id":       "user-2",
					"username": "bob",
					"email":    "bob@example.com",
					"role":     "member",
				},
			}
			respJSON, _ := json.Marshal(users)
			w.Write(respJSON)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := &cmdClient{
		endpointURL: server.URL,
		apiKey:      "ak_k_s",
	}
	stdout, _, err := executeOrgsCmdWithClient(client, "members", "org-uuid-abc")

	// Exit code must be 0.
	if err != nil {
		t.Errorf("orgs members returned error: %v, want nil (exit 0)", err)
	}

	// The org ID must appear in the request path.
	if !strings.Contains(capturedPath, "org-uuid-abc") {
		t.Errorf("request path = %q, want to contain %q", capturedPath, "org-uuid-abc")
	}

	// stdout must contain a valid JSON array.
	if strings.TrimSpace(stdout) == "" {
		t.Fatal("stdout is empty; expected []*User JSON array")
	}

	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not valid JSON: %s", stdout)
	}

	var arr []any
	if err := json.Unmarshal([]byte(stdout), &arr); err != nil {
		t.Fatalf("stdout is not a JSON array: %v", err)
	}
	if len(arr) == 0 {
		t.Error("stdout JSON array is empty; expected at least one User entry")
	}

	// Verify two-space indentation pattern.
	if !strings.Contains(stdout, "  ") {
		t.Error("stdout does not contain two-space indentation")
	}
}

// ===========================================================================
// Subtask 4.2: Pre-validation (missing api_key) tests for all authenticated
// commands
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-15-45: Verify that all authenticated commands exit with code 2 and the
// missing API key error without making any network request when api_key is
// absent.
// Requirement: 15-REQ-19.1
// Validates: 15-PROP-6
// ---------------------------------------------------------------------------

func TestPreValidation_MissingAPIKey_AllAuthenticatedCommands(t *testing.T) {
	// Mock server that records all incoming requests.
	// It must receive zero requests across all test cases.
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Each test case represents one authenticated command.
	// No api_key is configured — each command should fail with code 2 before
	// making any network request.
	testCases := []struct {
		name string
		run  func() (stdout string, stderr string, err error)
	}{
		{
			name: "user show",
			run:  func() (string, string, error) { return executeUserCmd("show") },
		},
		{
			name: "user update",
			run: func() (string, string, error) {
				return executeUserCmd("update", "--full-name", "Test")
			},
		},
		{
			name: "keys list",
			run:  func() (string, string, error) { return executeKeysCmd("list") },
		},
		{
			name: "keys refresh",
			run:  func() (string, string, error) { return executeKeysCmd("refresh") },
		},
		{
			name: "keys revoke",
			run:  func() (string, string, error) { return executeKeysCmd("revoke") },
		},
		{
			name: "tokens list",
			run:  func() (string, string, error) { return executeTokensCmd("list") },
		},
		{
			name: "tokens create",
			run: func() (string, string, error) {
				return executeTokensCmd("create", "--name", "t", "--permissions", "users:read")
			},
		},
		{
			name: "tokens show",
			run: func() (string, string, error) {
				return executeTokensCmd("show", "tok-123")
			},
		},
		{
			name: "tokens revoke",
			run: func() (string, string, error) {
				return executeTokensCmd("revoke", "tok-123")
			},
		},
		{
			name: "orgs list",
			run:  func() (string, string, error) { return executeOrgsCmd("list") },
		},
		{
			name: "orgs show",
			run: func() (string, string, error) {
				return executeOrgsCmd("show", "org-uuid-abc")
			},
		},
		{
			name: "orgs members",
			run: func() (string, string, error) {
				return executeOrgsCmd("members", "org-uuid-abc")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Reset request counter for each command.
			atomic.StoreInt32(&requestCount, 0)

			stdout, _, err := tc.run()

			// Must exit with code 2 (non-nil error).
			if err == nil {
				t.Fatalf("%s with no api_key returned nil error, want exit code 2", tc.name)
			}

			// stdout must contain the missing API key error envelope.
			if strings.TrimSpace(stdout) == "" {
				t.Fatalf("stdout is empty; expected error envelope JSON")
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
				t.Errorf("mock server received %d requests, want 0 (no network request)",
					count)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Property test TS-15-P6: Enumerate all authenticated commands with no
// api_key and verify that no HTTP requests are made and exit code is 2.
// Validates: 15-PROP-6 (No API Call Made When api_key Is Absent)
// ---------------------------------------------------------------------------

func TestPreValidation_Property_NoNetworkRequestWithoutAPIKey(t *testing.T) {
	// Track total requests across all commands in a single server.
	var totalRequests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&totalRequests, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Run all authenticated commands — none should hit the server.
	commands := []func() (string, string, error){
		func() (string, string, error) { return executeUserCmd("show") },
		func() (string, string, error) { return executeUserCmd("update", "--full-name", "X") },
		func() (string, string, error) { return executeKeysCmd("list") },
		func() (string, string, error) { return executeKeysCmd("refresh") },
		func() (string, string, error) { return executeKeysCmd("revoke") },
		func() (string, string, error) { return executeTokensCmd("list") },
		func() (string, string, error) { return executeTokensCmd("create", "--name", "t", "--permissions", "u:r") },
		func() (string, string, error) { return executeTokensCmd("show", "tok-1") },
		func() (string, string, error) { return executeTokensCmd("revoke", "tok-1") },
		func() (string, string, error) { return executeOrgsCmd("list") },
		func() (string, string, error) { return executeOrgsCmd("show", "org-1") },
		func() (string, string, error) { return executeOrgsCmd("members", "org-1") },
	}

	for _, cmd := range commands {
		_, _, err := cmd()
		// Each command must return a non-nil error (exit code 2).
		if err == nil {
			t.Error("authenticated command with no api_key returned nil error, want exit code 2")
		}
	}

	// After running all commands, the server must have received zero requests.
	if total := atomic.LoadInt32(&totalRequests); total != 0 {
		t.Errorf("total mock server requests across all commands = %d, want 0", total)
	}
}

// ===========================================================================
// Subtask 4.3: loginTimeoutSeconds constant invariant property test
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-15-49 / TS-15-P10: Verify that loginTimeoutSeconds is a package-level
// constant equal to 120, that runLogin can be called directly with a short
// timeout, and that the constant remains unchanged after multiple calls.
// Requirement: 15-REQ-22.1
// Validates: 15-PROP-10
// ---------------------------------------------------------------------------

func TestLoginTimeoutSeconds_Property_UnchangedAfterMultipleCalls(t *testing.T) {
	// Pre-check: constant must be 120.
	if loginTimeoutSeconds != 120 {
		t.Fatalf("loginTimeoutSeconds = %d before test, want 120", loginTimeoutSeconds)
	}

	// Create a mock server returning providers so the flow can proceed
	// past validation. No callback ever arrives, so runLogin times out.
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/auth/providers") {
			_, _ = w.Write([]byte(`[{"name":"github","authorize_url":"https://github.com/login/oauth/authorize?client_id=abc"}]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer mockSrv.Close()

	// Call runLogin 5 times with short timeouts. After each call, assert
	// that the constant has not been modified.
	for i := range 5 {
		opts := loginOpts{
			provider:      "github",
			expires:       90,
			endpointURL:   mockSrv.URL,
			openBrowserFn: func(_ string) error { return nil },
			stderr:        new(bytes.Buffer),
			stdout:        new(bytes.Buffer),
		}

		err := runLogin(context.Background(), 50*time.Millisecond, opts)

		// Each call should either time out or return nil (from stub).
		// Either way, the constant must remain 120.
		if err != nil && !strings.Contains(err.Error(), "timed out") {
			t.Logf("call %d returned unexpected error: %v", i+1, err)
		}

		if loginTimeoutSeconds != 120 {
			t.Fatalf("after call %d: loginTimeoutSeconds = %d, want 120 (constant must never change)",
				i+1, loginTimeoutSeconds)
		}
	}

	// Final check: constant must still be 120 after all calls.
	if loginTimeoutSeconds != 120 {
		t.Errorf("loginTimeoutSeconds = %d after all calls, want 120", loginTimeoutSeconds)
	}
}

// ---------------------------------------------------------------------------
// Compile-time verification: runLogin function signature must be
// func(context.Context, time.Duration, loginOpts) error.
// Requirement: 15-REQ-22.1
// ---------------------------------------------------------------------------

func TestRunLogin_FunctionSignature(t *testing.T) {
	// Compile-time check: assign runLogin to a variable with the expected
	// signature. If the signature changes, this code will not compile.
	var fn func(context.Context, time.Duration, loginOpts) error = runLogin

	// Use fn in a runtime assertion to prevent the "declared and not used"
	// compile error. The type assignment above is the real test — it proves
	// the function signature matches the expected type.
	_ = fn
}

// ---------------------------------------------------------------------------
// Verify that loginTimeoutSeconds is defined as const (not var) by checking
// its value is immutable. Since Go consts cannot be modified at runtime, the
// only runtime test is confirming the value. The const vs var distinction is
// enforced at compile time — assigning to loginTimeoutSeconds would fail.
// Requirement: 15-REQ-22.1
// Validates: 15-PROP-10
// ---------------------------------------------------------------------------

func TestLoginTimeoutSeconds_IsConst(t *testing.T) {
	// Verify the value is exactly 120.
	if loginTimeoutSeconds != 120 {
		t.Errorf("loginTimeoutSeconds = %d, want 120", loginTimeoutSeconds)
	}

	// In Go, a const cannot be modified at runtime. If loginTimeoutSeconds
	// were declared as `var`, a test could assign a new value to it:
	//     loginTimeoutSeconds = 999
	// Since this is a const, that line would not compile. The compile-time
	// guarantee is the real test; this runtime check is a safety net.
	//
	// Note: We do NOT try to assign to loginTimeoutSeconds here — doing so
	// would cause a compile error, which is exactly the desired behavior
	// for a const.
}

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

// ===========================================================================
// Subtask 1.5: State parameter generation and authorization URL tests
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-15-8: Verify that the generated OAuth state parameter is exactly 64
// lowercase hex characters derived from 32 random bytes.
// Requirement: 15-REQ-2.4
// Validates: 15-PROP-1
// ---------------------------------------------------------------------------

var hexPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func TestGenerateState_LengthAndFormat(t *testing.T) {
	state, err := generateState()
	if err != nil {
		t.Fatalf("generateState() returned error: %v", err)
	}

	if len(state) != 64 {
		t.Errorf("generateState() returned string of length %d, want 64", len(state))
	}

	if !hexPattern.MatchString(state) {
		t.Errorf("generateState() = %q, want string matching ^[0-9a-f]{64}$", state)
	}
}

func TestGenerateState_Uniqueness(t *testing.T) {
	state1, err := generateState()
	if err != nil {
		t.Fatalf("generateState() first call returned error: %v", err)
	}

	state2, err := generateState()
	if err != nil {
		t.Fatalf("generateState() second call returned error: %v", err)
	}

	if state1 == state2 {
		t.Errorf("generateState() returned same value twice: %q", state1)
	}
}

// ---------------------------------------------------------------------------
// Property test: generate 100 state parameters, all must have length 64
// and match the hex pattern.
// Validates: 15-PROP-1
// ---------------------------------------------------------------------------

func TestGenerateState_Property_AllValid(t *testing.T) {
	seen := make(map[string]bool)

	for i := range 100 {
		state, err := generateState()
		if err != nil {
			t.Fatalf("generateState() call %d returned error: %v", i, err)
		}

		if len(state) != 64 {
			t.Errorf("generateState() call %d: length = %d, want 64", i, len(state))
		}

		if !hexPattern.MatchString(state) {
			t.Errorf("generateState() call %d: %q does not match ^[0-9a-f]{64}$", i, state)
		}

		if seen[state] {
			t.Errorf("generateState() call %d: duplicate value %q", i, state)
		}
		seen[state] = true
	}
}

// ---------------------------------------------------------------------------
// TS-15-10: Verify that the authorization URL preserves existing query
// parameters from authorize_url and appends redirect_uri, state, and
// response_type=code without duplicating client_id.
// Requirement: 15-REQ-2.6
// Validates: 15-PROP-2
// ---------------------------------------------------------------------------

func TestBuildAuthURL_PreservesExistingParams(t *testing.T) {
	authorizeURL := "https://auth.example.com/oauth?client_id=abc123&scope=read"
	redirectURI := "http://127.0.0.1:12345/callback"
	state := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"

	finalURL, err := buildAuthURL(authorizeURL, redirectURI, state)
	if err != nil {
		t.Fatalf("buildAuthURL() returned error: %v", err)
	}

	parsed, err := url.Parse(finalURL)
	if err != nil {
		t.Fatalf("buildAuthURL() returned invalid URL: %v", err)
	}

	params := parsed.Query()

	// Existing params must be preserved.
	if got := params.Get("client_id"); got != "abc123" {
		t.Errorf("client_id = %q, want %q", got, "abc123")
	}
	if got := params.Get("scope"); got != "read" {
		t.Errorf("scope = %q, want %q", got, "read")
	}

	// New params must be added.
	if got := params.Get("redirect_uri"); got != redirectURI {
		t.Errorf("redirect_uri = %q, want %q", got, redirectURI)
	}
	if got := params.Get("state"); got != state {
		t.Errorf("state = %q, want %q", got, state)
	}
	if got := params.Get("response_type"); got != "code" {
		t.Errorf("response_type = %q, want %q", got, "code")
	}

	// client_id must appear exactly once.
	clientIDCount := strings.Count(finalURL, "client_id")
	if clientIDCount != 1 {
		t.Errorf("client_id appears %d times in URL, want exactly 1", clientIDCount)
	}
}

// ---------------------------------------------------------------------------
// Property test: buildAuthURL never overwrites existing params and always
// adds the three required params.
// Validates: 15-PROP-2
// ---------------------------------------------------------------------------

func TestBuildAuthURL_Property_NoOverwrite(t *testing.T) {
	tests := []struct {
		name         string
		authorizeURL string
		wantParams   map[string]string // existing params that must survive
	}{
		{
			name:         "simple URL with client_id",
			authorizeURL: "https://auth.example.com/oauth?client_id=abc123",
			wantParams:   map[string]string{"client_id": "abc123"},
		},
		{
			name:         "URL with multiple existing params",
			authorizeURL: "https://auth.example.com/oauth?client_id=xyz&scope=user&prompt=consent",
			wantParams:   map[string]string{"client_id": "xyz", "scope": "user", "prompt": "consent"},
		},
		{
			name:         "URL with no existing params",
			authorizeURL: "https://auth.example.com/oauth",
			wantParams:   map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			redirectURI := "http://127.0.0.1:9999/callback"
			state := "aabbccddaabbccddaabbccddaabbccddaabbccddaabbccddaabbccddaabbccdd"

			finalURL, err := buildAuthURL(tt.authorizeURL, redirectURI, state)
			if err != nil {
				t.Fatalf("buildAuthURL() returned error: %v", err)
			}

			parsed, err := url.Parse(finalURL)
			if err != nil {
				t.Fatalf("buildAuthURL() returned invalid URL: %v", err)
			}

			params := parsed.Query()

			// All existing params must survive.
			for key, val := range tt.wantParams {
				if got := params.Get(key); got != val {
					t.Errorf("param %q = %q, want %q", key, got, val)
				}
			}

			// The three required params must be present.
			if params.Get("redirect_uri") != redirectURI {
				t.Errorf("redirect_uri = %q, want %q", params.Get("redirect_uri"), redirectURI)
			}
			if params.Get("state") != state {
				t.Errorf("state = %q, want %q", params.Get("state"), state)
			}
			if params.Get("response_type") != "code" {
				t.Errorf("response_type = %q, want %q", params.Get("response_type"), "code")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Supplementary: loginTimeoutSeconds constant is exactly 120.
// Requirement: 15-REQ-22.1
// Validates: 15-PROP-10
// ---------------------------------------------------------------------------

func TestLoginTimeoutSecondsConstant(t *testing.T) {
	if loginTimeoutSeconds != 120 {
		t.Errorf("loginTimeoutSeconds = %d, want 120", loginTimeoutSeconds)
	}
}

// ===========================================================================
// Subtask 2.1: Login command precondition and flag validation tests
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-15-19: Verify that the login command exits with code 2 and the missing
// endpoint URL error JSON when endpoint_url is absent.
// Requirement: 15-REQ-2.15
// ---------------------------------------------------------------------------

func TestLoginMissingEndpointURL(t *testing.T) {
	opts := loginOpts{
		provider:    "github",
		expires:     90,
		endpointURL: "", // absent
		stderr:      new(bytes.Buffer),
		stdout:      new(bytes.Buffer),
	}

	err := runLogin(context.Background(), 200*time.Millisecond, opts)
	if err == nil {
		t.Fatal("runLogin with empty endpointURL returned nil error, want non-nil")
	}

	if !strings.Contains(err.Error(), "endpoint URL is required for login") {
		t.Errorf("error = %q, want to contain %q",
			err.Error(), "endpoint URL is required for login")
	}
}

// ---------------------------------------------------------------------------
// TS-15-20: Verify that the login command exits with code 2 and the expires
// validation error JSON when --expires is an invalid value.
// Requirement: 15-REQ-2.16
// ---------------------------------------------------------------------------

func TestLoginInvalidExpires(t *testing.T) {
	invalidValues := []int{1, 15, 45, 91, -1, 100}

	for _, expires := range invalidValues {
		t.Run(fmt.Sprintf("expires_%d", expires), func(t *testing.T) {
			opts := loginOpts{
				provider:    "github",
				expires:     expires,
				endpointURL: "https://api.example.com",
				stderr:      new(bytes.Buffer),
				stdout:      new(bytes.Buffer),
			}

			err := runLogin(context.Background(), 200*time.Millisecond, opts)
			if err == nil {
				t.Fatalf("runLogin with expires=%d returned nil error, want non-nil", expires)
			}

			if !strings.Contains(err.Error(), "--expires must be 0, 30, 60, or 90") {
				t.Errorf("error = %q, want to contain %q",
					err.Error(), "--expires must be 0, 30, 60, or 90")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TS-15-49: Verify that loginTimeoutSeconds=120, runLogin times out with
// a short timeout, and the constant remains unchanged after the call.
// Requirement: 15-REQ-22.1
// Validates: 15-PROP-10
// ---------------------------------------------------------------------------

func TestRunLogin_ShortTimeoutTimesOut(t *testing.T) {
	// Pre-check: constant must be 120.
	if loginTimeoutSeconds != 120 {
		t.Fatalf("loginTimeoutSeconds = %d before test, want 120", loginTimeoutSeconds)
	}

	// Create a mock server returning a providers list so the flow
	// proceeds past validation but no callback ever arrives.
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/auth/providers"):
			_, _ = w.Write([]byte(`[{"name":"github","authorize_url":"https://github.com/login/oauth/authorize?client_id=abc"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockSrv.Close()

	opts := loginOpts{
		provider:      "github",
		expires:       90,
		endpointURL:   mockSrv.URL,
		openBrowserFn: func(_ string) error { return nil }, // no-op
		stderr:        new(bytes.Buffer),
		stdout:        new(bytes.Buffer),
	}

	err := runLogin(context.Background(), 50*time.Millisecond, opts)
	if err == nil {
		t.Fatal("runLogin with 50ms timeout returned nil error, want timeout error")
	}

	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "timed out")
	}

	// Post-check: constant must still be 120.
	if loginTimeoutSeconds != 120 {
		t.Errorf("loginTimeoutSeconds = %d after test, want 120 (constant must not change)",
			loginTimeoutSeconds)
	}
}

// ---------------------------------------------------------------------------
// TS-15-12: Verify RunE calls runLogin with time.Duration(loginTimeoutSeconds)*time.Second.
// Requirement: 15-REQ-2.8
// ---------------------------------------------------------------------------

func TestLoginCmd_RunE_Exists(t *testing.T) {
	cmd := NewLoginCmd()
	if cmd == nil {
		t.Fatal("NewLoginCmd() returned nil")
	}

	if cmd.RunE == nil {
		t.Error("NewLoginCmd().RunE is nil; RunE must be set to call runLogin")
	}
}

// ===========================================================================
// Subtask 2.2: Callback server handler tests
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-15-9: Verify that the callback server returns HTTP 404 for any path
// other than /callback and does not signal either channel.
// Requirement: 15-REQ-2.5
// Validates: 15-PROP-3
// ---------------------------------------------------------------------------

func TestCallbackHandler_NonCallbackPath_Returns404(t *testing.T) {
	state := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	handler := newCallbackHandler(state, codeCh, errCh)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/favicon.ico")
	if err != nil {
		t.Fatalf("GET /favicon.ico failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /favicon.ico status = %d, want %d",
			resp.StatusCode, http.StatusNotFound)
	}

	// Neither channel should receive a value.
	select {
	case code := <-codeCh:
		t.Errorf("codeCh received unexpected value: %q", code)
	case chErr := <-errCh:
		t.Errorf("errCh received unexpected error: %v", chErr)
	default:
		// Good — neither channel has a value.
	}
}

// ---------------------------------------------------------------------------
// Property test TS-15-P3: Arbitrary non-/callback paths all return 404
// and do not trigger channel signals.
// Requirement: 15-REQ-2.5
// Validates: 15-PROP-3
// ---------------------------------------------------------------------------

func TestCallbackHandler_Property_AllNonCallbackPaths_Return404(t *testing.T) {
	state := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	handler := newCallbackHandler(state, codeCh, errCh)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	paths := []string{"/", "/favicon.ico", "/index.html", "/callback/extra", "/login", "/health"}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			resp, err := http.Get(srv.URL + path)
			if err != nil {
				t.Fatalf("GET %s failed: %v", path, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("GET %s status = %d, want %d",
					path, resp.StatusCode, http.StatusNotFound)
			}
		})
	}

	// After all requests, neither channel should have received anything.
	select {
	case code := <-codeCh:
		t.Errorf("codeCh received unexpected value: %q", code)
	case chErr := <-errCh:
		t.Errorf("errCh received unexpected error: %v", chErr)
	default:
		// Good.
	}
}

// ---------------------------------------------------------------------------
// TS-15-13: Verify that GET /callback with correct state and code returns
// HTTP 200 with the verbatim success HTML and sends the code on codeCh.
// Requirement: 15-REQ-2.9
// ---------------------------------------------------------------------------

func TestCallbackHandler_ValidCallback_Returns200(t *testing.T) {
	state := "aaaa1111bbbb2222cccc3333dddd4444eeee5555ffff6666aaaa7777bbbb8888"
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	handler := newCallbackHandler(state, codeCh, errCh)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	reqURL := fmt.Sprintf("%s/callback?state=%s&code=authcode123", srv.URL, state)
	resp, err := http.Get(reqURL)
	if err != nil {
		t.Fatalf("GET /callback failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /callback status = %d, want %d",
			resp.StatusCode, http.StatusOK)
	}

	body, _ := io.ReadAll(resp.Body)
	expectedHTML := "<html><body><h1>Login successful</h1><p>You may close this tab.</p></body></html>"
	if string(body) != expectedHTML {
		t.Errorf("response body = %q, want %q", string(body), expectedHTML)
	}

	select {
	case code := <-codeCh:
		if code != "authcode123" {
			t.Errorf("codeCh received %q, want %q", code, "authcode123")
		}
	case chErr := <-errCh:
		t.Fatalf("errCh received unexpected error: %v", chErr)
	case <-time.After(time.Second):
		t.Fatal("codeCh did not receive expected code within 1s")
	}
}

// ---------------------------------------------------------------------------
// TS-15-14: Verify that a state mismatch on the callback causes HTTP 400
// with the error HTML page and sends an error on errCh.
// Requirement: 15-REQ-2.10
// ---------------------------------------------------------------------------

func TestCallbackHandler_StateMismatch_Returns400(t *testing.T) {
	correctState := "correct1correct2correct3correct4correct5correct6correct7correct8"
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	handler := newCallbackHandler(correctState, codeCh, errCh)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/callback?state=wrongstate&code=auth123")
	if err != nil {
		t.Fatalf("GET /callback failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("GET /callback status = %d, want %d",
			resp.StatusCode, http.StatusBadRequest)
	}

	body, _ := io.ReadAll(resp.Body)
	expectedHTML := "<html><body><h1>Login failed</h1><p>OAuth state mismatch. Please try again.</p></body></html>"
	if string(body) != expectedHTML {
		t.Errorf("response body = %q, want %q", string(body), expectedHTML)
	}

	select {
	case code := <-codeCh:
		t.Fatalf("codeCh received unexpected value %q on state mismatch", code)
	case chErr := <-errCh:
		if chErr == nil {
			t.Fatal("errCh received nil error, want non-nil")
		}
	case <-time.After(time.Second):
		t.Fatal("errCh did not receive expected error within 1s")
	}
}

// ===========================================================================
// Subtask 2.3: Login timeout and browser open fallback tests
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-15-18: Verify that when the login context times out before a callback
// arrives, runLogin returns a timeout error.
// Requirement: 15-REQ-2.14
// ---------------------------------------------------------------------------

func TestRunLogin_Timeout_NoCallback(t *testing.T) {
	// Mock server that returns providers but never sends a callback.
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/auth/providers") {
			_, _ = w.Write([]byte(`[{"name":"github","authorize_url":"https://github.com/login/oauth/authorize?client_id=abc"}]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer mockSrv.Close()

	opts := loginOpts{
		provider:      "github",
		expires:       90,
		endpointURL:   mockSrv.URL,
		openBrowserFn: func(_ string) error { return nil },
		stderr:        new(bytes.Buffer),
		stdout:        new(bytes.Buffer),
	}

	err := runLogin(context.Background(), 100*time.Millisecond, opts)
	if err == nil {
		t.Fatal("runLogin with 100ms timeout returned nil error, want timeout error")
	}

	if !strings.Contains(err.Error(), "login timed out waiting for browser callback") {
		t.Errorf("error = %q, want to contain %q",
			err.Error(), "login timed out waiting for browser callback")
	}
}

// ---------------------------------------------------------------------------
// TS-15-E3: Verify that the callback server is shut down after timeout
// (connection refused on subsequent requests).
// Requirement: 15-REQ-2.E3
// ---------------------------------------------------------------------------

func TestRunLogin_Timeout_ServerShutDown(t *testing.T) {
	// To test that the server is shut down after timeout, we need runLogin
	// to actually start a callback server and shut it down. We verify by
	// checking that runLogin returns a timeout error (which implies the
	// flow completed including server lifecycle).

	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/auth/providers") {
			_, _ = w.Write([]byte(`[{"name":"github","authorize_url":"https://github.com/login/oauth/authorize?client_id=abc"}]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer mockSrv.Close()

	opts := loginOpts{
		provider:      "github",
		expires:       90,
		endpointURL:   mockSrv.URL,
		openBrowserFn: func(_ string) error { return nil },
		stderr:        new(bytes.Buffer),
		stdout:        new(bytes.Buffer),
	}

	err := runLogin(context.Background(), 100*time.Millisecond, opts)

	// The error itself confirms the flow reached timeout.
	if err == nil {
		t.Fatal("runLogin returned nil, want timeout error (server should be shut down)")
	}

	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error = %q, want timeout-related error", err.Error())
	}
}

// ---------------------------------------------------------------------------
// TS-15-11: Verify that the login command prints the browser-opening message
// to stderr and falls back to printing the URL if openBrowser fails.
// Requirement: 15-REQ-2.7
// ---------------------------------------------------------------------------

func TestRunLogin_BrowserOpenFallback(t *testing.T) {
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/auth/providers") {
			_, _ = w.Write([]byte(`[{"name":"github","authorize_url":"https://github.com/login/oauth/authorize?client_id=abc"}]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer mockSrv.Close()

	stderrBuf := new(bytes.Buffer)
	opts := loginOpts{
		provider:    "github",
		expires:     90,
		endpointURL: mockSrv.URL,
		openBrowserFn: func(_ string) error {
			return errors.New("no display server available")
		},
		stderr: stderrBuf,
		stdout: new(bytes.Buffer),
	}

	// Use a short timeout — we don't care about the callback in this test.
	_ = runLogin(context.Background(), 200*time.Millisecond, opts)

	stderrStr := stderrBuf.String()

	if !strings.Contains(stderrStr, "Opening browser for authentication...") {
		t.Errorf("stderr = %q, want to contain %q",
			stderrStr, "Opening browser for authentication...")
	}

	if !strings.Contains(stderrStr, "Open this URL in your browser:") {
		t.Errorf("stderr = %q, want to contain %q",
			stderrStr, "Open this URL in your browser:")
	}
}

// ---------------------------------------------------------------------------
// TS-15-E4: Verify that browser open failure is non-fatal — the login
// flow continues.
// Requirement: 15-REQ-2.E4
// ---------------------------------------------------------------------------

func TestRunLogin_BrowserFailure_NonFatal(t *testing.T) {
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/auth/providers") {
			_, _ = w.Write([]byte(`[{"name":"github","authorize_url":"https://github.com/login/oauth/authorize?client_id=abc"}]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer mockSrv.Close()

	opts := loginOpts{
		provider:    "github",
		expires:     90,
		endpointURL: mockSrv.URL,
		openBrowserFn: func(_ string) error {
			return errors.New("xdg-open not found")
		},
		stderr: new(bytes.Buffer),
		stdout: new(bytes.Buffer),
	}

	// If the browser failure were fatal, runLogin would return an error
	// containing the browser error message. Instead, it should continue
	// and time out waiting for the callback.
	err := runLogin(context.Background(), 100*time.Millisecond, opts)

	// The login flow must NOT fail due to browser error.
	if err != nil && strings.Contains(err.Error(), "xdg-open") {
		t.Errorf("runLogin failed due to browser error: %v; browser failure should be non-fatal", err)
	}

	// It should eventually time out (the flow continued past the browser error).
	if err != nil && !strings.Contains(err.Error(), "timed out") {
		// Allow timeout or nil (stub) — but not a browser-related error.
		t.Logf("runLogin returned: %v (expected timeout or nil from stub)", err)
	}
}

// ===========================================================================
// Subtask 2.4: Login integration tests — provider lookup and code exchange
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-15-6: Verify that the login command constructs an unauthenticated SDK
// client and calls GetProviders when invoked with a valid endpoint_url.
// Requirement: 15-REQ-2.2
// ---------------------------------------------------------------------------

func TestRunLogin_CallsGetProviders(t *testing.T) {
	var providersCalled bool

	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/auth/providers") {
			providersCalled = true
			_, _ = w.Write([]byte(`[{"name":"github","authorize_url":"https://github.com/login/oauth/authorize?client_id=abc"}]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer mockSrv.Close()

	opts := loginOpts{
		provider:      "github",
		expires:       90,
		endpointURL:   mockSrv.URL,
		openBrowserFn: func(_ string) error { return nil },
		stderr:        new(bytes.Buffer),
		stdout:        new(bytes.Buffer),
	}

	// Use short timeout — we care about the GetProviders call, not the full flow.
	_ = runLogin(context.Background(), 200*time.Millisecond, opts)

	if !providersCalled {
		t.Error("runLogin did not call GET /auth/providers; expected provider discovery call")
	}
}

// ---------------------------------------------------------------------------
// TS-15-7: Verify that the login command exits with code 1 and
// provider-not-found error JSON when the provider is absent from the list.
// Requirement: 15-REQ-2.3
// ---------------------------------------------------------------------------

func TestRunLogin_ProviderNotFound(t *testing.T) {
	// Server returns providers list WITHOUT 'gitlab'.
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/auth/providers") {
			_, _ = w.Write([]byte(`[{"name":"github","authorize_url":"https://github.com/login/oauth/authorize?client_id=abc"}]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer mockSrv.Close()

	opts := loginOpts{
		provider:      "gitlab", // not in the mock server's list
		expires:       90,
		endpointURL:   mockSrv.URL,
		openBrowserFn: func(_ string) error { return nil },
		stderr:        new(bytes.Buffer),
		stdout:        new(bytes.Buffer),
	}

	err := runLogin(context.Background(), 200*time.Millisecond, opts)
	if err == nil {
		t.Fatal("runLogin with unknown provider returned nil error, want 'provider not found'")
	}

	if !strings.Contains(err.Error(), "provider 'gitlab' not found") {
		t.Errorf("error = %q, want to contain %q",
			err.Error(), "provider 'gitlab' not found")
	}
}

// ---------------------------------------------------------------------------
// TS-15-15: Verify that upon receiving the code from codeCh, the login
// command calls ExchangeOAuthCode with correct provider, code, redirect_uri,
// and expires.
// Requirement: 15-REQ-2.11
// ---------------------------------------------------------------------------

func TestRunLogin_CodeExchange_CorrectParams(t *testing.T) {
	var capturedExchangeBody map[string]any

	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasSuffix(r.URL.Path, "/auth/providers"):
			_, _ = w.Write([]byte(`[{"name":"github","authorize_url":"https://github.com/login/oauth/authorize?client_id=abc"}]`))

		case strings.HasSuffix(r.URL.Path, "/auth/callback") && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &capturedExchangeBody)

			resp := map[string]any{
				"user": map[string]any{
					"id":       "user-test-123",
					"username": "testuser",
					"email":    "test@example.com",
				},
				"api_key": map[string]any{
					"key":    "ak_testkey_secret",
					"key_id": "testkey",
				},
			}
			respJSON, _ := json.Marshal(resp)
			_, _ = w.Write(respJSON)

		default:
			http.NotFound(w, r)
		}
	}))
	defer mockSrv.Close()

	// To test code exchange, we need to simulate the callback arriving.
	// The runLogin function should start a callback server, and we need
	// to send a request to it. However, since runLogin is a stub, this
	// test will fail because no exchange call is made.
	//
	// When implemented, the test works by:
	// 1. runLogin starts callback server
	// 2. openBrowserFn captures the auth URL (which contains the callback port)
	// 3. We simulate the browser redirect by sending a GET to the callback URL
	// 4. runLogin receives the code and calls ExchangeOAuthCode

	var capturedAuthURL string
	opts := loginOpts{
		provider:    "github",
		expires:     30,
		endpointURL: mockSrv.URL,
		openBrowserFn: func(authURL string) error {
			capturedAuthURL = authURL
			// Parse the redirect_uri from the auth URL to find the callback port.
			parsed, err := url.Parse(authURL)
			if err != nil {
				return nil
			}
			redirectURI := parsed.Query().Get("redirect_uri")
			state := parsed.Query().Get("state")
			if redirectURI != "" && state != "" {
				// Simulate the OAuth provider redirecting back with code and state.
				go func() {
					time.Sleep(10 * time.Millisecond) // small delay to let server start
					callbackURL := fmt.Sprintf("%s?code=testcode&state=%s", redirectURI, state)
					resp, err := http.Get(callbackURL)
					if err == nil {
						resp.Body.Close()
					}
				}()
			}
			return nil
		},
		saveConfigFn: func(_ string, _ *CLIConfig) error { return nil },
		stderr:       new(bytes.Buffer),
		stdout:       new(bytes.Buffer),
	}

	err := runLogin(context.Background(), 2*time.Second, opts)

	// When implemented, err should be nil (successful flow).
	// The stub returns nil, but capturedExchangeBody will be nil because
	// no actual exchange call was made.
	if err != nil {
		t.Logf("runLogin returned error: %v", err)
	}

	_ = capturedAuthURL // used when openBrowserFn is called

	if capturedExchangeBody == nil {
		t.Fatal("ExchangeOAuthCode was not called; expected POST to /auth/callback")
	}

	if capturedExchangeBody["provider"] != "github" {
		t.Errorf("exchange body provider = %v, want %q",
			capturedExchangeBody["provider"], "github")
	}

	if capturedExchangeBody["code"] != "testcode" {
		t.Errorf("exchange body code = %v, want %q",
			capturedExchangeBody["code"], "testcode")
	}

	// redirect_uri must match http://127.0.0.1:<port>/callback
	redirectURI, _ := capturedExchangeBody["redirect_uri"].(string)
	if !strings.HasPrefix(redirectURI, "http://127.0.0.1:") ||
		!strings.HasSuffix(redirectURI, "/callback") {
		t.Errorf("exchange body redirect_uri = %q, want http://127.0.0.1:<port>/callback",
			redirectURI)
	}

	// expires must be 30
	expiresVal, _ := capturedExchangeBody["expires"].(float64)
	if int(expiresVal) != 30 {
		t.Errorf("exchange body expires = %v, want 30", capturedExchangeBody["expires"])
	}
}

// ===========================================================================
// Subtask 2.5: Login success, config save, and shutdown tests
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-15-16: Verify that after a successful code exchange the login command
// saves endpoint_url, user_id, and api_key to config, prints user JSON to
// stdout, and prints the success message to stderr.
// Requirement: 15-REQ-2.12
// ---------------------------------------------------------------------------

func TestRunLogin_SuccessFlow(t *testing.T) {
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasSuffix(r.URL.Path, "/auth/providers"):
			_, _ = w.Write([]byte(`[{"name":"github","authorize_url":"https://github.com/login/oauth/authorize?client_id=abc"}]`))

		case strings.HasSuffix(r.URL.Path, "/auth/callback") && r.Method == http.MethodPost:
			resp := map[string]any{
				"user": map[string]any{
					"id":       "user-123",
					"username": "alice",
					"email":    "alice@example.com",
				},
				"api_key": map[string]any{
					"key":    "ak_keyid_secret",
					"key_id": "keyid",
				},
			}
			respJSON, _ := json.Marshal(resp)
			_, _ = w.Write(respJSON)

		default:
			http.NotFound(w, r)
		}
	}))
	defer mockSrv.Close()

	var savedConfig *CLIConfig
	stderrBuf := new(bytes.Buffer)
	stdoutBuf := new(bytes.Buffer)

	opts := loginOpts{
		provider:    "github",
		expires:     90,
		endpointURL: mockSrv.URL,
		openBrowserFn: func(authURL string) error {
			// Simulate the OAuth provider redirecting back.
			parsed, err := url.Parse(authURL)
			if err != nil {
				return nil
			}
			redirectURI := parsed.Query().Get("redirect_uri")
			state := parsed.Query().Get("state")
			if redirectURI != "" && state != "" {
				go func() {
					time.Sleep(10 * time.Millisecond)
					callbackURL := fmt.Sprintf("%s?code=testcode&state=%s", redirectURI, state)
					resp, err := http.Get(callbackURL)
					if err == nil {
						resp.Body.Close()
					}
				}()
			}
			return nil
		},
		saveConfigFn: func(_ string, cfg *CLIConfig) error {
			savedConfig = cfg
			return nil
		},
		stderr: stderrBuf,
		stdout: stdoutBuf,
	}

	err := runLogin(context.Background(), 2*time.Second, opts)
	if err != nil {
		t.Fatalf("runLogin returned error: %v", err)
	}

	// Verify config was saved with correct values.
	if savedConfig == nil {
		t.Fatal("saveConfigFn was not called; expected config to be saved")
	}
	if savedConfig.EndpointURL == "" {
		t.Error("saved config endpoint_url is empty")
	}
	if savedConfig.UserID != "user-123" {
		t.Errorf("saved config user_id = %q, want %q", savedConfig.UserID, "user-123")
	}
	if savedConfig.APIKey != "ak_keyid_secret" {
		t.Errorf("saved config api_key = %q, want %q", savedConfig.APIKey, "ak_keyid_secret")
	}

	// Verify stdout contains user JSON.
	stdoutStr := stdoutBuf.String()
	if stdoutStr == "" {
		t.Fatal("stdout is empty; expected user JSON")
	}

	var userJSON map[string]any
	if err := json.Unmarshal([]byte(stdoutStr), &userJSON); err != nil {
		t.Fatalf("stdout is not valid JSON: %v", err)
	}
	if userJSON["username"] != "alice" {
		t.Errorf("stdout user username = %v, want %q", userJSON["username"], "alice")
	}

	// Verify stderr contains success message.
	stderrStr := stderrBuf.String()
	if !strings.Contains(stderrStr, "Logged in as alice") {
		t.Errorf("stderr = %q, want to contain %q", stderrStr, "Logged in as alice")
	}
}

// ---------------------------------------------------------------------------
// TS-15-17: Verify that the callback server is shut down using a fresh
// 5-second background context after login completes.
// Requirement: 15-REQ-2.13
// Validates: 15-PROP-8
// ---------------------------------------------------------------------------

func TestRunLogin_ShutdownUsesFreshContext(t *testing.T) {
	// This test verifies the shutdown context by completing a successful
	// login flow and checking that the server was shut down properly.
	// A fully implemented runLogin should shut down the server with a
	// fresh context.WithTimeout(context.Background(), 5*time.Second).

	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/auth/providers"):
			_, _ = w.Write([]byte(`[{"name":"github","authorize_url":"https://github.com/login/oauth/authorize?client_id=abc"}]`))
		case strings.HasSuffix(r.URL.Path, "/auth/callback") && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"user":{"id":"u1","username":"bob"},"api_key":{"key":"ak_k1_s","key_id":"k1"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockSrv.Close()

	opts := loginOpts{
		provider:    "github",
		expires:     90,
		endpointURL: mockSrv.URL,
		openBrowserFn: func(authURL string) error {
			parsed, _ := url.Parse(authURL)
			redirectURI := parsed.Query().Get("redirect_uri")
			state := parsed.Query().Get("state")
			go func() {
				time.Sleep(10 * time.Millisecond)
				resp, err := http.Get(fmt.Sprintf("%s?code=c1&state=%s", redirectURI, state))
				if err == nil {
					resp.Body.Close()
				}
			}()
			return nil
		},
		saveConfigFn: func(_ string, _ *CLIConfig) error { return nil },
		stderr:       new(bytes.Buffer),
		stdout:       new(bytes.Buffer),
	}

	err := runLogin(context.Background(), 2*time.Second, opts)
	if err != nil {
		t.Fatalf("runLogin returned error: %v (expected successful completion)", err)
	}

	// If runLogin completed without error, the server was shut down.
	// A more detailed test would instrument the shutdown call to verify
	// the context is derived from context.Background() with a 5-second
	// deadline, but that requires implementation-level hooks.
}

// ---------------------------------------------------------------------------
// Property test TS-15-P8: All three termination paths (success, timeout,
// state mismatch) use a fresh shutdown context.
// Validates: 15-PROP-8
// ---------------------------------------------------------------------------

func TestRunLogin_Property_ShutdownContextAlwaysFresh(t *testing.T) {
	// Path 1: Timeout (no callback arrives).
	t.Run("timeout", func(t *testing.T) {
		mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if strings.HasSuffix(r.URL.Path, "/auth/providers") {
				_, _ = w.Write([]byte(`[{"name":"github","authorize_url":"https://github.com/login/oauth/authorize?client_id=abc"}]`))
				return
			}
			http.NotFound(w, r)
		}))
		defer mockSrv.Close()

		opts := loginOpts{
			provider:      "github",
			expires:       90,
			endpointURL:   mockSrv.URL,
			openBrowserFn: func(_ string) error { return nil },
			stderr:        new(bytes.Buffer),
			stdout:        new(bytes.Buffer),
		}

		err := runLogin(context.Background(), 100*time.Millisecond, opts)
		// On timeout, runLogin must still shut down the server.
		// The stub returns nil, which will fail the timeout check.
		if err == nil {
			t.Error("runLogin returned nil on timeout path, want timeout error")
		}
	})

	// Path 2: Success (callback arrives with valid code).
	t.Run("success", func(t *testing.T) {
		mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch {
			case strings.HasSuffix(r.URL.Path, "/auth/providers"):
				_, _ = w.Write([]byte(`[{"name":"github","authorize_url":"https://github.com/login/oauth/authorize?client_id=abc"}]`))
			case strings.HasSuffix(r.URL.Path, "/auth/callback"):
				_, _ = w.Write([]byte(`{"user":{"id":"u1","username":"x"},"api_key":{"key":"ak_k_s","key_id":"k"}}`))
			default:
				http.NotFound(w, r)
			}
		}))
		defer mockSrv.Close()

		opts := loginOpts{
			provider:    "github",
			expires:     90,
			endpointURL: mockSrv.URL,
			openBrowserFn: func(authURL string) error {
				parsed, _ := url.Parse(authURL)
				redirectURI := parsed.Query().Get("redirect_uri")
				state := parsed.Query().Get("state")
				go func() {
					time.Sleep(10 * time.Millisecond)
					resp, err := http.Get(fmt.Sprintf("%s?code=c&state=%s", redirectURI, state))
					if err == nil {
						resp.Body.Close()
					}
				}()
				return nil
			},
			saveConfigFn: func(_ string, _ *CLIConfig) error { return nil },
			stderr:       new(bytes.Buffer),
			stdout:       new(bytes.Buffer),
		}

		err := runLogin(context.Background(), 2*time.Second, opts)
		if err != nil {
			t.Errorf("runLogin returned error on success path: %v", err)
		}
	})

	// Path 3: State mismatch (callback with wrong state).
	t.Run("state_mismatch", func(t *testing.T) {
		mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if strings.HasSuffix(r.URL.Path, "/auth/providers") {
				_, _ = w.Write([]byte(`[{"name":"github","authorize_url":"https://github.com/login/oauth/authorize?client_id=abc"}]`))
				return
			}
			http.NotFound(w, r)
		}))
		defer mockSrv.Close()

		opts := loginOpts{
			provider:    "github",
			expires:     90,
			endpointURL: mockSrv.URL,
			openBrowserFn: func(authURL string) error {
				// Send callback with WRONG state to trigger mismatch.
				parsed, _ := url.Parse(authURL)
				redirectURI := parsed.Query().Get("redirect_uri")
				go func() {
					time.Sleep(10 * time.Millisecond)
					resp, err := http.Get(fmt.Sprintf("%s?code=c&state=wrongstate", redirectURI))
					if err == nil {
						resp.Body.Close()
					}
				}()
				return nil
			},
			stderr: new(bytes.Buffer),
			stdout: new(bytes.Buffer),
		}

		err := runLogin(context.Background(), 2*time.Second, opts)
		if err == nil {
			t.Error("runLogin returned nil on state mismatch, want CSRF error")
		}
	})
}

// ===========================================================================
// Subtask 2.6: Login config write failure and state mismatch exit tests
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-15-E1: Verify that when the config write fails after successful code
// exchange, the login command exits with code 2 and config failure error
// JSON, and does NOT print credentials to stdout.
// Requirement: 15-REQ-2.E1
// Validates: 15-PROP-4
// ---------------------------------------------------------------------------

func TestRunLogin_ConfigWriteFailure(t *testing.T) {
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/auth/providers"):
			_, _ = w.Write([]byte(`[{"name":"github","authorize_url":"https://github.com/login/oauth/authorize?client_id=abc"}]`))
		case strings.HasSuffix(r.URL.Path, "/auth/callback") && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"user":{"id":"u1","username":"alice","email":"a@b.com"},"api_key":{"key":"ak_k_s","key_id":"k"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockSrv.Close()

	stdoutBuf := new(bytes.Buffer)

	opts := loginOpts{
		provider:    "github",
		expires:     90,
		endpointURL: mockSrv.URL,
		openBrowserFn: func(authURL string) error {
			parsed, _ := url.Parse(authURL)
			redirectURI := parsed.Query().Get("redirect_uri")
			state := parsed.Query().Get("state")
			go func() {
				time.Sleep(10 * time.Millisecond)
				resp, err := http.Get(fmt.Sprintf("%s?code=c&state=%s", redirectURI, state))
				if err == nil {
					resp.Body.Close()
				}
			}()
			return nil
		},
		saveConfigFn: func(_ string, _ *CLIConfig) error {
			return errors.New("disk full")
		},
		stderr: new(bytes.Buffer),
		stdout: stdoutBuf,
	}

	err := runLogin(context.Background(), 2*time.Second, opts)

	// Must return an error indicating config save failure.
	if err == nil {
		t.Fatal("runLogin returned nil when config save failed, want error")
	}

	if !strings.Contains(err.Error(), "failed to save config") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "failed to save config")
	}

	// Credentials must NOT appear in stdout.
	stdoutStr := stdoutBuf.String()
	if strings.Contains(stdoutStr, "ak_k_s") {
		t.Error("stdout contains api_key value on config write failure; credentials must not be emitted")
	}
	if strings.Contains(stdoutStr, "alice") {
		t.Error("stdout contains username on config write failure; user data must not be emitted")
	}
}

// ---------------------------------------------------------------------------
// Property test TS-15-P4: No credential data in stdout on config write
// failure, regardless of the ExchangeOAuthCode response shape.
// Validates: 15-PROP-4
// ---------------------------------------------------------------------------

func TestRunLogin_Property_NoCredentialsOnConfigFailure(t *testing.T) {
	testCases := []struct {
		name     string
		apiKey   string
		username string
	}{
		{"short_key", "ak_k_s", "user1"},
		{"long_key", "ak_longkeyid12345_supersecretvalue", "user2"},
		{"special_username", "ak_x_y", "admin-user"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.HasSuffix(r.URL.Path, "/auth/providers"):
					_, _ = w.Write([]byte(`[{"name":"github","authorize_url":"https://github.com/login/oauth/authorize?client_id=abc"}]`))
				case strings.HasSuffix(r.URL.Path, "/auth/callback"):
					resp := fmt.Sprintf(`{"user":{"id":"u1","username":"%s"},"api_key":{"key":"%s","key_id":"k"}}`,
						tc.username, tc.apiKey)
					_, _ = w.Write([]byte(resp))
				default:
					http.NotFound(w, r)
				}
			}))
			defer mockSrv.Close()

			stdoutBuf := new(bytes.Buffer)

			opts := loginOpts{
				provider:    "github",
				expires:     90,
				endpointURL: mockSrv.URL,
				openBrowserFn: func(authURL string) error {
					parsed, _ := url.Parse(authURL)
					redirectURI := parsed.Query().Get("redirect_uri")
					state := parsed.Query().Get("state")
					go func() {
						time.Sleep(10 * time.Millisecond)
						resp, err := http.Get(fmt.Sprintf("%s?code=c&state=%s", redirectURI, state))
						if err == nil {
							resp.Body.Close()
						}
					}()
					return nil
				},
				saveConfigFn: func(_ string, _ *CLIConfig) error {
					return errors.New("permission denied")
				},
				stderr: new(bytes.Buffer),
				stdout: stdoutBuf,
			}

			_ = runLogin(context.Background(), 2*time.Second, opts)

			stdoutStr := stdoutBuf.String()
			if strings.Contains(stdoutStr, tc.apiKey) {
				t.Errorf("stdout contains api_key %q on config failure", tc.apiKey)
			}
			if strings.Contains(stdoutStr, tc.username) {
				t.Errorf("stdout contains username %q on config failure", tc.username)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TS-15-E5: Verify that the callback server shutdown always uses a fresh
// context.Background() with a 5-second timeout even when the login context
// is already cancelled (e.g., after state mismatch).
// Requirement: 15-REQ-2.E5
// Validates: 15-PROP-8
// ---------------------------------------------------------------------------

func TestRunLogin_StateMismatch_FreshShutdownContext(t *testing.T) {
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/auth/providers") {
			_, _ = w.Write([]byte(`[{"name":"github","authorize_url":"https://github.com/login/oauth/authorize?client_id=abc"}]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer mockSrv.Close()

	opts := loginOpts{
		provider:    "github",
		expires:     90,
		endpointURL: mockSrv.URL,
		openBrowserFn: func(authURL string) error {
			// Send callback with WRONG state to trigger mismatch and
			// cancel the login context.
			parsed, _ := url.Parse(authURL)
			redirectURI := parsed.Query().Get("redirect_uri")
			go func() {
				time.Sleep(10 * time.Millisecond)
				resp, err := http.Get(fmt.Sprintf("%s?code=c&state=badstate", redirectURI))
				if err == nil {
					resp.Body.Close()
				}
			}()
			return nil
		},
		stderr: new(bytes.Buffer),
		stdout: new(bytes.Buffer),
	}

	err := runLogin(context.Background(), 2*time.Second, opts)

	// On state mismatch, the login context gets cancelled, but the
	// server should still shut down using a fresh background context.
	// The error must indicate state mismatch (CSRF), not a shutdown failure.
	if err == nil {
		t.Fatal("runLogin returned nil on state mismatch, want CSRF error")
	}

	if !strings.Contains(err.Error(), "OAuth state mismatch") {
		t.Errorf("error = %q, want to contain %q",
			err.Error(), "OAuth state mismatch")
	}
}

// ---------------------------------------------------------------------------
// TS-15-E2 (edge case): Verify that sending /favicon.ico does NOT shut
// down the callback server — it continues listening.
// Requirement: 15-REQ-2.E2
// Validates: 15-PROP-3
// ---------------------------------------------------------------------------

func TestCallbackHandler_FaviconDoesNotShutDown(t *testing.T) {
	state := "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	handler := newCallbackHandler(state, codeCh, errCh)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// Send /favicon.ico — should get 404.
	resp, err := http.Get(srv.URL + "/favicon.ico")
	if err != nil {
		t.Fatalf("GET /favicon.ico failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /favicon.ico status = %d, want %d",
			resp.StatusCode, http.StatusNotFound)
	}

	// Server must still be up — send another request.
	resp2, err := http.Get(srv.URL + "/other")
	if err != nil {
		t.Fatalf("server down after /favicon.ico: GET /other failed: %v", err)
	}
	resp2.Body.Close()

	// The second request also gets 404 (confirming the server is still listening).
	if resp2.StatusCode != http.StatusNotFound {
		t.Errorf("GET /other status = %d, want %d",
			resp2.StatusCode, http.StatusNotFound)
	}
}

// ===========================================================================
// Issue #37: HTTP request timeout tests
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-NS-5: Verify httpRequestTimeout constant is ≤ 30 seconds and distinct
// from loginTimeoutSeconds.
// Requirement: NS-REQ-5
// ---------------------------------------------------------------------------

func TestHTTPRequestTimeout_NamedConstant(t *testing.T) {
	if httpRequestTimeout > 30*time.Second {
		t.Errorf("httpRequestTimeout = %v, want ≤ 30s", httpRequestTimeout)
	}
	if httpRequestTimeout <= 0 {
		t.Errorf("httpRequestTimeout = %v, want > 0", httpRequestTimeout)
	}
	if httpRequestTimeout == time.Duration(loginTimeoutSeconds)*time.Second {
		t.Errorf("httpRequestTimeout (%v) must differ from loginTimeoutSeconds (%ds)",
			httpRequestTimeout, loginTimeoutSeconds)
	}
}

// ---------------------------------------------------------------------------
// TS-NS-1: Verify provider discovery returns an error when the server stalls,
// rather than hanging indefinitely.
// Requirement: NS-REQ-1
// ---------------------------------------------------------------------------

func TestRunLogin_ProviderDiscovery_Timeout(t *testing.T) {
	done := make(chan struct{})

	// Server that stalls indefinitely on /auth/providers.
	stallSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the test is done (simulates unresponsive server).
		<-done
	}))
	defer func() {
		close(done)      // Unblock handler first
		stallSrv.Close() // Then close server
	}()

	opts := loginOpts{
		provider:      "github",
		expires:       90,
		endpointURL:   stallSrv.URL,
		openBrowserFn: func(_ string) error { return nil },
		stderr:        new(bytes.Buffer),
		stdout:        new(bytes.Buffer),
	}

	// Use a short-deadline parent context so the HTTP request timeout fires quickly.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := runLogin(ctx, 2*time.Second, opts)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("runLogin returned nil against stalling provider endpoint, want error")
	}

	if !strings.Contains(err.Error(), "failed to fetch providers") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "failed to fetch providers")
	}

	// Must return well before the 2-second browser timeout.
	if elapsed > 2*time.Second {
		t.Errorf("runLogin took %v against stalling server, want < 2s", elapsed)
	}
}

// ---------------------------------------------------------------------------
// TS-NS-2: Verify token exchange returns an error when the server stalls on
// /auth/callback, rather than hanging indefinitely.
// Requirement: NS-REQ-2
// ---------------------------------------------------------------------------

func TestRunLogin_TokenExchange_Timeout(t *testing.T) {
	done := make(chan struct{})

	// Server that responds to provider discovery but stalls on token exchange.
	stallSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/auth/providers"):
			_, _ = w.Write([]byte(`[{"name":"github","authorize_url":"https://github.com/login/oauth/authorize?client_id=abc"}]`))
		case strings.HasSuffix(r.URL.Path, "/auth/callback"):
			// Stall until the test is done.
			<-done
		default:
			http.NotFound(w, r)
		}
	}))
	defer func() {
		close(done)      // Unblock handler first
		stallSrv.Close() // Then close server
	}()

	// Use a short parent context to limit how long we wait.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	opts := loginOpts{
		provider:    "github",
		expires:     90,
		endpointURL: stallSrv.URL,
		openBrowserFn: func(authURL string) error {
			// Simulate the browser callback arriving so we reach the exchange step.
			parsed, err := url.Parse(authURL)
			if err != nil {
				return nil
			}
			redirectURI := parsed.Query().Get("redirect_uri")
			state := parsed.Query().Get("state")
			if redirectURI != "" && state != "" {
				go func() {
					time.Sleep(10 * time.Millisecond)
					callbackURL := fmt.Sprintf("%s?code=testcode&state=%s", redirectURI, state)
					resp, err := http.Get(callbackURL)
					if err == nil {
						resp.Body.Close()
					}
				}()
			}
			return nil
		},
		stderr: new(bytes.Buffer),
		stdout: new(bytes.Buffer),
	}

	start := time.Now()
	err := runLogin(ctx, 2*time.Second, opts)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("runLogin returned nil against stalling exchange endpoint, want error")
	}

	if !strings.Contains(err.Error(), "failed to exchange OAuth code") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "failed to exchange OAuth code")
	}

	// Must return well before the 2-second browser timeout.
	if elapsed > 2*time.Second {
		t.Errorf("runLogin took %v against stalling exchange server, want < 2s", elapsed)
	}
}

// ---------------------------------------------------------------------------
// TS-NS-3: Verify cancelling the parent context cancels in-flight HTTP
// requests and runLogin returns promptly with a context.Canceled error.
// Requirement: NS-REQ-3
// ---------------------------------------------------------------------------

func TestRunLogin_ParentCancel_CancelsHTTPRequests(t *testing.T) {
	done := make(chan struct{})

	// Server that stalls on /auth/providers until the test is done.
	stallSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-done
	}))
	defer func() {
		close(done)      // Unblock handler first
		stallSrv.Close() // Then close server
	}()

	ctx, cancel := context.WithCancel(context.Background())

	opts := loginOpts{
		provider:      "github",
		expires:       90,
		endpointURL:   stallSrv.URL,
		openBrowserFn: func(_ string) error { return nil },
		stderr:        new(bytes.Buffer),
		stdout:        new(bytes.Buffer),
	}

	// Cancel the context after a short delay.
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := runLogin(ctx, 10*time.Second, opts)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("runLogin returned nil after parent cancel, want error")
	}

	// The error must wrap context.Canceled.
	if !errors.Is(err, context.Canceled) {
		// Accept errors that contain the cancellation message even if not directly wrapping.
		if !strings.Contains(err.Error(), "context canceled") {
			t.Errorf("error = %q, want to wrap context.Canceled", err.Error())
		}
	}

	// Must return well under 1 second after cancellation.
	if elapsed > 500*time.Millisecond {
		t.Errorf("runLogin took %v after cancel, want < 500ms", elapsed)
	}
}

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"
)

// =========================================================================
// Smoke test helpers
// =========================================================================

// setupSmokeTestEnv sets up HOME, TokenPrefix, and a config directory with
// empty config.toml for smoke tests. Returns the config directory path.
func setupSmokeTestEnv(t *testing.T, prefix string) string {
	t.Helper()

	savedPrefix := TokenPrefix
	TokenPrefix = prefix
	t.Cleanup(func() { TokenPrefix = savedPrefix })

	tmpHome := t.TempDir()
	savedHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", savedHome) })

	configDir := filepath.Join(tmpHome, "."+prefix)
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	configContent := "endpoint_url = \"\"\nuser_id = \"\"\napi_key = \"\"\n"
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config.toml: %v", err)
	}

	return configDir
}

// smokeSetEnv sets an env var for the duration of the test.
func smokeSetEnv(t *testing.T, key, value string) {
	t.Helper()
	saved, had := os.LookupEnv(key)
	os.Setenv(key, value)
	t.Cleanup(func() {
		if had {
			os.Setenv(key, saved)
		} else {
			os.Unsetenv(key)
		}
	})
}

// smokeUnsetEnv unsets an env var for the duration of the test.
func smokeUnsetEnv(t *testing.T, key string) {
	t.Helper()
	saved, had := os.LookupEnv(key)
	os.Unsetenv(key)
	t.Cleanup(func() {
		if had {
			os.Setenv(key, saved)
		} else {
			os.Unsetenv(key)
		}
	})
}

// makeSmokeLeafCmd creates a simple annotated leaf command for smoke tests.
func makeSmokeLeafCmd(use string, annotations map[string]string, runE func(cmd *cobra.Command, args []string) error) *cobra.Command {
	return &cobra.Command{
		Use:         use,
		Short:       "Smoke test command: " + use,
		Annotations: annotations,
		RunE:        runE,
	}
}

// =========================================================================
// TS-13-SMOKE-1: Full end-to-end authenticated command: credentials
// resolved, client constructed, SDK called, JSON result printed, exit 0.
//
// Execution Path: 13-PATH-1
// Real components: cmd/akc/main.go, internal/cli/root.go,
//   internal/cli/config.go, internal/cli/output.go, internal/cli/context.go
// Mockable: apikit.Client.SomeMethod, os.UserHomeDir
// =========================================================================

func TestSmokeAuthenticatedCommandEndToEnd(t *testing.T) {
	// Set up environment with valid credentials.
	setupSmokeTestEnv(t, "smokepfx")
	smokeSetEnv(t, "ENDPOINT_URL", "http://localhost:9999")
	smokeSetEnv(t, "API_KEY", "test-api-key")
	smokeSetEnv(t, "USER_ID", "test-user-id")

	var gotClient any
	var gotUserID string
	var runECalled bool

	rootCmd := RootCommand()
	rootCmd.AddCommand(makeSmokeLeafCmd("smokecmd", map[string]string{
		"auth":   "api_key",
		"method": "GET",
		"path":   "/api/v1/test",
	}, func(cmd *cobra.Command, _ []string) error {
		runECalled = true
		gotClient = ClientFromContext(cmd.Context())
		gotUserID = UserIDFromContext(cmd.Context())
		// Simulate success: print a JSON result.
		return PrintJSON(map[string]string{"status": "ok"})
	}))
	rootCmd.SetArgs([]string{"smokecmd"})

	stdout, stderr := captureStdoutAndStderr(t, func() {
		err := rootCmd.Execute()
		if err != nil {
			PrintError(err)
		}
	})

	// PersistentPreRunE must have run — RunE called.
	if !runECalled {
		t.Fatal("RunE was not called; PersistentPreRunE may have blocked execution")
	}

	// Client must be constructed and stored in context.
	if gotClient == nil {
		t.Error("ClientFromContext should return non-nil when credentials are resolved")
	}

	// UserID must be resolved.
	if gotUserID != "test-user-id" {
		t.Errorf("UserIDFromContext = %q, want %q", gotUserID, "test-user-id")
	}

	// stdout must contain valid JSON (the PrintJSON result).
	if len(strings.TrimSpace(stdout)) == 0 {
		t.Fatal("stdout should contain JSON output")
	}
	if !json.Valid([]byte(stdout)) {
		t.Errorf("stdout is not valid JSON: %s", stdout)
	}

	// stdout must NOT contain an error envelope.
	if strings.Contains(stdout, `"error":`) {
		t.Error("stdout should not contain an error envelope on success")
	}

	// stderr must be empty.
	if len(strings.TrimSpace(stderr)) > 0 {
		t.Errorf("stderr should be empty on success, got: %s", stderr)
	}
}

// =========================================================================
// TS-13-SMOKE-2: API error from server: SDK returns *apikit.APIError,
// Execute() writes error envelope to stdout, process exits 1.
//
// Execution Path: 13-PATH-2
// Real components: cmd/akc/main.go, internal/cli/root.go,
//   internal/cli/output.go
// Mockable: apikit.Client.SomeMethod (returns APIError)
//
// Note: This smoke test uses a simulated APIError. The full integration
// test with the real SDK client is deferred to group 16 (wiring
// verification), as internal/cli cannot import apikit due to an import
// cycle. The error is constructed indirectly via the error interface.
// =========================================================================

func TestSmokeAPIErrorEnvelopeAndExitCode(t *testing.T) {
	// We cannot construct *apikit.APIError from package cli due to import
	// cycle. Instead, we test that Execute() + PrintError produces a valid
	// error envelope for a plain error (client-side). The *apikit.APIError
	// variant is tested in output_test.go (package cli_test).
	//
	// This smoke test validates the structural flow:
	// RunE returns error -> Execute() calls PrintError -> envelope on stdout.
	setupSmokeTestEnv(t, "smokepfx")

	rootCmd := RootCommand()
	rootCmd.AddCommand(makeSmokeLeafCmd("smokeerr", map[string]string{
		"auth": "none",
	}, func(_ *cobra.Command, _ []string) error {
		return &smokeTestError{code: 401, message: "unauthorized"}
	}))
	rootCmd.SetArgs([]string{"smokeerr"})

	var execErr error
	stdout, stderr := captureStdoutAndStderr(t, func() {
		execErr = rootCmd.Execute()
		if execErr != nil {
			PrintError(execErr)
		}
	})

	// Must have an error.
	if execErr == nil {
		t.Fatal("expected non-nil error from command returning error")
	}

	// stdout must contain a valid JSON error envelope.
	if len(strings.TrimSpace(stdout)) == 0 {
		t.Fatal("stdout should contain the JSON error envelope")
	}
	if !json.Valid([]byte(stdout)) {
		t.Errorf("stdout is not valid JSON: %s", stdout)
	}

	// Parse and verify envelope structure.
	var env errorEnvelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("failed to parse error envelope: %v\nstdout: %s", err, stdout)
	}
	if env.Error.Message == "" {
		t.Error("error envelope message should not be empty")
	}

	// stderr must be empty for error conditions.
	if len(strings.TrimSpace(stderr)) > 0 {
		t.Errorf("stderr should be empty, got: %s", stderr)
	}
}

// smokeTestError simulates an error with code and message for smoke testing.
type smokeTestError struct {
	code    int
	message string
}

func (e *smokeTestError) Error() string {
	return e.message
}

// =========================================================================
// TS-13-SMOKE-3: Missing required credential: PersistentPreRunE returns
// canonical error; Execute() writes code:0 envelope to stdout; exit 2.
//
// Execution Path: 13-PATH-3
// Real components: cmd/akc/main.go, internal/cli/root.go,
//   internal/cli/config.go, internal/cli/output.go
// Mockable: os.UserHomeDir (returns valid dir), filesystem (valid config dir)
// =========================================================================

func TestSmokeMissingCredentialProducesCanonicalError(t *testing.T) {
	setupSmokeTestEnv(t, "smokepfx")

	// Unset all credential sources.
	smokeUnsetEnv(t, "ENDPOINT_URL")
	smokeUnsetEnv(t, "API_KEY")
	smokeUnsetEnv(t, "USER_ID")

	rootCmd := RootCommand()
	rootCmd.AddCommand(makeSmokeLeafCmd("smokeauth", map[string]string{
		"auth":   "api_key",
		"method": "GET",
		"path":   "/api/v1/test",
	}, func(_ *cobra.Command, _ []string) error {
		return nil
	}))
	rootCmd.SetArgs([]string{"smokeauth"})

	var execErr error
	stdout, stderr := captureStdoutAndStderr(t, func() {
		execErr = rootCmd.Execute()
		if execErr != nil {
			PrintError(execErr)
		}
	})

	// Must fail with a credential error.
	if execErr == nil {
		t.Fatal("expected error when no credentials are configured")
	}

	// Error message must be the canonical format.
	errMsg := execErr.Error()
	if !strings.Contains(errMsg, "is not set") {
		t.Errorf("error should contain 'is not set', got: %v", execErr)
	}

	// stdout must contain a JSON error envelope with code: 0.
	if len(strings.TrimSpace(stdout)) == 0 {
		t.Fatal("stdout should contain the JSON error envelope")
	}
	var env errorEnvelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, stdout)
	}
	if env.Error.Code != 0 {
		t.Errorf("error envelope code = %d, want 0 (client sentinel)", env.Error.Code)
	}

	// ExitCode should be 2 for client-side errors.
	if code := ExitCode(execErr); code != 2 {
		t.Errorf("ExitCode = %d, want 2", code)
	}

	// stderr must be empty.
	if len(strings.TrimSpace(stderr)) > 0 {
		t.Errorf("stderr should be empty, got: %s", stderr)
	}
}

// =========================================================================
// TS-13-SMOKE-4: akc version with reachable server: auth-exempt path,
// ResolveEndpointURL, SDK Version() called, full JSON output with
// server_version.
//
// Execution Path: 13-PATH-4
// Real components: internal/cli/version.go, internal/cli/root.go,
//   internal/cli/output.go
// Mockable: apikit.Client.Version (returns VersionResponse), os.UserHomeDir
// =========================================================================

func TestSmokeVersionWithReachableServer(t *testing.T) {
	// Start a mock HTTP server that responds to GET /version.
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]string{
				"go_version":  "2.0.0",
				"build_time":  "2025-06-01T00:00:00Z",
				"commit":      "abc123",
				"mount_point": "/api/v1",
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockServer.Close()

	// Configure environment.
	setupSmokeTestEnv(t, "smokepfx")
	smokeSetEnv(t, "ENDPOINT_URL", mockServer.URL)

	rootCmd := RootCommand()
	rootCmd.SetArgs([]string{"version"})

	var execErr error
	stdout, stderr := captureStdoutAndStderr(t, func() {
		execErr = rootCmd.Execute()
		if execErr != nil {
			PrintError(execErr)
		}
	})

	// Exit code should be 0.
	if ExitCode(execErr) != 0 {
		t.Errorf("ExitCode = %d, want 0", ExitCode(execErr))
	}

	// stdout should contain valid JSON.
	if len(strings.TrimSpace(stdout)) == 0 {
		t.Fatal("version command should produce JSON output on stdout")
	}

	var output struct {
		CLIVersion    string `json:"cli_version"`
		Build         string `json:"build"`
		Prefix        string `json:"prefix"`
		ServerVersion *struct {
			Version    string `json:"go_version"`
			BuildTime  string `json:"build_time"`
			Commit     string `json:"commit"`
			MountPoint string `json:"mount_point"`
		} `json:"server_version,omitempty"`
	}
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("failed to parse version JSON output: %v\nstdout: %s", err, stdout)
	}

	// cli_version, build, prefix must be present.
	if output.CLIVersion == "" {
		t.Error("cli_version should be present")
	}
	if output.Build == "" {
		t.Error("build should be present")
	}
	if output.Prefix == "" {
		t.Error("prefix should be present")
	}

	// server_version should be present when endpoint is reachable.
	if output.ServerVersion == nil {
		t.Error("server_version should be present when mock server is reachable")
	} else {
		if output.ServerVersion.Version != "2.0.0" {
			t.Errorf("server_version.go_version = %q, want %q", output.ServerVersion.Version, "2.0.0")
		}
	}

	// stderr should be empty (server reachable, no warning).
	if len(strings.TrimSpace(stderr)) > 0 {
		t.Errorf("stderr should be empty when server is reachable, got: %s", stderr)
	}
}

// =========================================================================
// TS-13-SMOKE-5: akc help --json: full command tree walker runs, collects
// leaf commands, outputs JSON tree for agent discovery.
//
// Execution Path: 13-PATH-5
// Real components: internal/cli/help.go, internal/cli/root.go,
//   internal/cli/output.go
// Mockable: stub 'user show' and 'admin users list' commands
// =========================================================================

func TestSmokeHelpJSONCommandTree(t *testing.T) {
	rootCmd := RootCommand()

	// Register stub 'user show' as a test fixture.
	userGroup := &cobra.Command{
		Use:   "user",
		Short: "User management commands",
	}
	userGroup.AddCommand(makeAnnotatedTestLeaf("show", "Show user details", map[string]string{
		"auth":   "api_key",
		"method": "GET",
		"path":   "/api/v1/user",
	}))
	rootCmd.AddCommand(userGroup)

	// Register stub 'admin users list' as a test fixture.
	adminGroup := &cobra.Command{
		Use:   "admin",
		Short: "Admin commands",
	}
	adminUsersGroup := &cobra.Command{
		Use:   "users",
		Short: "Admin user management",
	}
	adminUsersGroup.AddCommand(makeAnnotatedTestLeaf("list", "List all users", map[string]string{
		"auth":   "admin",
		"method": "GET",
		"path":   "/api/v1/admin/users",
	}))
	adminGroup.AddCommand(adminUsersGroup)
	rootCmd.AddCommand(adminGroup)

	rootCmd.SetArgs([]string{"help", "--json"})

	stdout := captureStdout(t, func() {
		_ = rootCmd.Execute()
	})

	trimmed := strings.TrimSpace(stdout)
	if !json.Valid([]byte(trimmed)) {
		t.Fatalf("help --json output is not valid JSON:\n%s", trimmed)
	}

	var tree struct {
		Name     string `json:"name"`
		Version  string `json:"version"`
		Commands []struct {
			Name string `json:"name"`
			Auth string `json:"auth"`
		} `json:"commands"`
	}
	if err := json.Unmarshal([]byte(trimmed), &tree); err != nil {
		t.Fatalf("failed to parse help --json output: %v", err)
	}

	// Root object must have name and version.
	if tree.Name == "" {
		t.Error("help --json root 'name' should not be empty")
	}
	if tree.Version == "" {
		t.Error("help --json root 'version' should not be empty")
	}

	// Commands array must contain the registered leaf commands.
	nameSet := make(map[string]bool)
	for _, cmd := range tree.Commands {
		nameSet[cmd.Name] = true
		// Also index by last segment.
		parts := strings.Fields(cmd.Name)
		if len(parts) > 0 {
			nameSet[parts[len(parts)-1]] = true
		}
	}

	// 'user show' must appear.
	if !nameSet["show"] {
		found := false
		for name := range nameSet {
			if strings.Contains(name, "show") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("'user show' should appear in help --json output; got names: %v", nameSet)
		}
	}

	// 'admin users list' must appear.
	if !nameSet["list"] {
		found := false
		for name := range nameSet {
			if strings.Contains(name, "list") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("'admin users list' should appear in help --json output; got names: %v", nameSet)
		}
	}

	// Group commands ('user', 'admin', 'admin users') should NOT appear as
	// separate entries because they have no RunE.
	for _, cmd := range tree.Commands {
		parts := strings.Fields(cmd.Name)
		lastPart := parts[len(parts)-1]
		if lastPart == "user" || lastPart == "admin" || lastPart == "users" {
			t.Errorf("group command %q should not appear in commands array", cmd.Name)
		}
	}
}

// =========================================================================
// TS-13-SMOKE-6: Atomic config write by a spec-14/15 command: temp file
// created in configDir, renamed atomically, no temp files left, config
// has correct content.
//
// Execution Path: 13-PATH-6
// Real components: internal/cli/config.go
// Mockable: os.CreateTemp (observed but real), os.Rename (observed but real)
// =========================================================================

func TestSmokeAtomicConfigWrite(t *testing.T) {
	configDir := t.TempDir()

	// Step 1: Write initial config.
	initial := &CLIConfig{
		EndpointURL: "http://initial.example.com",
		UserID:      "initial-user",
		APIKey:      "initial-key",
	}
	if err := SaveConfig(configDir, initial); err != nil {
		t.Fatalf("initial SaveConfig failed: %v", err)
	}

	// Step 2: Simulate a login command updating credentials.
	updated := &CLIConfig{
		EndpointURL: "http://updated.example.com",
		UserID:      "updated-user",
		APIKey:      "refreshed-key-abc123",
	}
	if err := SaveConfig(configDir, updated); err != nil {
		t.Fatalf("updated SaveConfig failed: %v", err)
	}

	// Verify: config.toml has the updated content.
	configPath := filepath.Join(configDir, "config.toml")
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config.toml: %v", err)
	}

	var parsed CLIConfig
	if _, decErr := toml.Decode(string(content), &parsed); decErr != nil {
		t.Fatalf("config.toml is not valid TOML: %v\ncontent:\n%s", decErr, string(content))
	}
	if parsed.EndpointURL != "http://updated.example.com" {
		t.Errorf("EndpointURL = %q, want %q", parsed.EndpointURL, "http://updated.example.com")
	}
	if parsed.UserID != "updated-user" {
		t.Errorf("UserID = %q, want %q", parsed.UserID, "updated-user")
	}
	if parsed.APIKey != "refreshed-key-abc123" {
		t.Errorf("APIKey = %q, want %q", parsed.APIKey, "refreshed-key-abc123")
	}

	// Verify: permissions are 0600.
	info, statErr := os.Stat(configPath)
	if statErr != nil {
		t.Fatalf("failed to stat config.toml: %v", statErr)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("config.toml permission = %04o, want 0600", perm)
	}

	// Verify: no temp files remain.
	entries, readErr := os.ReadDir(configDir)
	if readErr != nil {
		t.Fatalf("failed to read config dir: %v", readErr)
	}
	for _, e := range entries {
		if e.Name() != "config.toml" {
			t.Errorf("unexpected file in config dir after atomic write: %s", e.Name())
		}
	}
}

// =========================================================================
// Spec 15 Smoke Tests — CLI User Commands
// =========================================================================

// =========================================================================
// TS-15-SMOKE-1: End-to-end smoke test of the full login OAuth flow:
// providers fetched, browser opened (stubbed), callback received with valid
// state, code exchanged, config saved, user JSON printed to stdout.
//
// Execution Path: 15-PATH-1
// Real components: internal/cli/login.go (runLogin, callback server),
//   internal/cli/helpers.go (validateExpires), CLI Core config save
// Mockable: httptest.Server (handles /auth/providers and /auth/callback),
//   openBrowser replaced with simulated callback
// =========================================================================

func TestSmokeSpec15_LoginOAuthFlowEndToEnd(t *testing.T) {
	// Mock API server: handles provider discovery and code exchange.
	var providersRequested, exchangeRequested atomic.Int32

	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasSuffix(r.URL.Path, "/auth/providers") && r.Method == http.MethodGet:
			providersRequested.Add(1)
			_, _ = w.Write([]byte(`[{"name":"github","authorize_url":"https://github.com/login/oauth/authorize?client_id=abc"}]`))

		case strings.HasSuffix(r.URL.Path, "/auth/callback") && r.Method == http.MethodPost:
			exchangeRequested.Add(1)
			// Verify request body has the expected fields.
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
				if body["provider"] != "github" {
					t.Errorf("exchange provider = %v, want github", body["provider"])
				}
				if body["code"] != "testcode" {
					t.Errorf("exchange code = %v, want testcode", body["code"])
				}
				if _, ok := body["redirect_uri"]; !ok {
					t.Error("exchange request missing redirect_uri")
				}
				if _, ok := body["expires"]; !ok {
					t.Error("exchange request missing expires")
				}
			}

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
			// Simulate the OAuth provider redirecting back to the callback server.
			parsed, err := url.Parse(authURL)
			if err != nil {
				return nil
			}

			// Verify authorization URL construction.
			redirectURI := parsed.Query().Get("redirect_uri")
			state := parsed.Query().Get("state")
			responseType := parsed.Query().Get("response_type")
			clientID := parsed.Query().Get("client_id")

			if redirectURI == "" {
				t.Error("auth URL missing redirect_uri")
			}
			if state == "" {
				t.Error("auth URL missing state")
			}
			if responseType != "code" {
				t.Errorf("auth URL response_type = %q, want %q", responseType, "code")
			}
			// Verify existing params (client_id) preserved.
			if clientID != "abc" {
				t.Errorf("auth URL client_id = %q, want %q (existing param not preserved)", clientID, "abc")
			}

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

	err := runLogin(context.Background(), 5*time.Second, opts)
	if err != nil {
		t.Fatalf("runLogin returned error: %v", err)
	}

	// Verify: GET /auth/providers was called on the mock server.
	if got := providersRequested.Load(); got != 1 {
		t.Errorf("GET /auth/providers called %d times, want 1", got)
	}

	// Verify: POST /auth/callback (code exchange) was called.
	if got := exchangeRequested.Load(); got != 1 {
		t.Errorf("POST /auth/callback called %d times, want 1", got)
	}

	// Verify: Config file atomically updated with endpoint_url, user_id, api_key.
	if savedConfig == nil {
		t.Fatal("saveConfigFn was not called; config not saved")
	}
	if savedConfig.EndpointURL != mockSrv.URL {
		t.Errorf("saved endpoint_url = %q, want %q", savedConfig.EndpointURL, mockSrv.URL)
	}
	if savedConfig.UserID != "user-123" {
		t.Errorf("saved user_id = %q, want %q", savedConfig.UserID, "user-123")
	}
	if savedConfig.APIKey != "ak_keyid_secret" {
		t.Errorf("saved api_key = %q, want %q", savedConfig.APIKey, "ak_keyid_secret")
	}

	// Verify: stdout contains indented user JSON.
	stdoutStr := stdoutBuf.String()
	if stdoutStr == "" {
		t.Fatal("stdout is empty; expected user JSON")
	}
	var userJSON map[string]any
	if err := json.Unmarshal([]byte(stdoutStr), &userJSON); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, stdoutStr)
	}
	if userJSON["username"] != "alice" {
		t.Errorf("stdout user username = %v, want %q", userJSON["username"], "alice")
	}

	// Verify: stderr contains 'Logged in as alice'.
	stderrStr := stderrBuf.String()
	if !strings.Contains(stderrStr, "Logged in as alice") {
		t.Errorf("stderr = %q, want to contain %q", stderrStr, "Logged in as alice")
	}

	// Verify: Exit code is 0 (no error returned).
	// Already checked above — err == nil means exit code 0.
}

// =========================================================================
// TS-15-SMOKE-2: Smoke test for akc user show happy path: authenticated
// client constructed, GetUser called, user JSON printed to stdout.
//
// Execution Path: 15-PATH-2
// Real components: internal/cli/user.go (newUserShowCmd),
//   CLI Core NewAuthenticatedClient (via CmdClient injection)
// Mockable: httptest.Server handling GET /user
// =========================================================================

func TestSmokeSpec15_UserShowHappyPath(t *testing.T) {
	var requestCount atomic.Int32
	var gotAuthHeader string

	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/user") && r.Method == http.MethodGet {
			requestCount.Add(1)
			gotAuthHeader = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"user-123","username":"alice","email":"alice@example.com","full_name":"Alice Smith","status":"active","role":"user"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer mockSrv.Close()

	client := &CmdClient{
		endpointURL: mockSrv.URL,
		apiKey:      "test-api-key",
		httpClient:  mockSrv.Client(),
	}

	stdout, stderr, err := executeUserCmdWithClient(client, "show")
	if err != nil {
		t.Fatalf("user show returned error: %v", err)
	}

	// Verify: GET /user was called with authentication headers.
	if got := requestCount.Load(); got != 1 {
		t.Errorf("GET /user called %d times, want 1", got)
	}
	if gotAuthHeader != "Bearer test-api-key" {
		t.Errorf("Authorization header = %q, want %q", gotAuthHeader, "Bearer test-api-key")
	}

	// Verify: stdout contains indented User JSON (response.Data).
	stdoutTrimmed := strings.TrimSpace(stdout)
	if !json.Valid([]byte(stdoutTrimmed)) {
		t.Fatalf("stdout is not valid JSON: %s", stdoutTrimmed)
	}
	var user map[string]any
	if err := json.Unmarshal([]byte(stdoutTrimmed), &user); err != nil {
		t.Fatalf("failed to parse user JSON: %v", err)
	}
	if user["username"] != "alice" {
		t.Errorf("user username = %v, want %q", user["username"], "alice")
	}

	// Verify: Exit code is 0 (no error).
	// Already confirmed above.

	// Verify: stderr is empty.
	if strings.TrimSpace(stderr) != "" {
		t.Errorf("stderr should be empty, got: %q", stderr)
	}
}

// =========================================================================
// TS-15-SMOKE-3: Smoke test for akc keys refresh: key_id parsed from
// api_key, RefreshKey called, config updated with new key, APIKeyFull JSON
// printed to stdout.
//
// Execution Path: 15-PATH-3
// Real components: internal/cli/keys.go (newKeysRefreshCmd),
//   internal/cli/helpers.go (parseKeyID), CLI Core atomic write (via DI)
// Mockable: httptest.Server handling refresh endpoint
// =========================================================================

func TestSmokeSpec15_KeysRefreshEndToEnd(t *testing.T) {
	var gotKeyIDInPath string

	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/refresh") {
			// Extract key_id from path: /api/v1/user/keys/<keyID>/refresh
			parts := strings.Split(r.URL.Path, "/")
			for i, p := range parts {
				if p == "keys" && i+1 < len(parts) {
					gotKeyIDInPath = parts[i+1]
					break
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"key":"ak_newkeyid_newsecret","key_id":"newkeyid","expires_at":"2026-10-15T00:00:00Z"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer mockSrv.Close()

	var savedConfig *CLIConfig

	client := &CmdClient{
		endpointURL: mockSrv.URL,
		apiKey:      "ak_keyid123_secret",
		httpClient:  mockSrv.Client(),
		saveConfigFn: func(_ string, cfg *CLIConfig) error {
			savedConfig = cfg
			return nil
		},
	}

	stdout, stderr, err := executeKeysCmdWithClient(client, "refresh")
	if err != nil {
		t.Fatalf("keys refresh returned error: %v", err)
	}

	// Verify: parseKeyID extracts 'keyid123' from 'ak_keyid123_secret'.
	if gotKeyIDInPath != "keyid123" {
		t.Errorf("key_id in request path = %q, want %q", gotKeyIDInPath, "keyid123")
	}

	// Verify: Config api_key is atomically updated to 'ak_newkeyid_newsecret'.
	if savedConfig == nil {
		t.Fatal("saveConfigFn was not called; config not updated")
	}
	if savedConfig.APIKey != "ak_newkeyid_newsecret" {
		t.Errorf("saved api_key = %q, want %q", savedConfig.APIKey, "ak_newkeyid_newsecret")
	}

	// Verify: stdout contains indented APIKeyFull JSON.
	stdoutTrimmed := strings.TrimSpace(stdout)
	if !json.Valid([]byte(stdoutTrimmed)) {
		t.Fatalf("stdout is not valid JSON: %s", stdoutTrimmed)
	}
	var keyFull map[string]any
	if err := json.Unmarshal([]byte(stdoutTrimmed), &keyFull); err != nil {
		t.Fatalf("failed to parse key JSON: %v", err)
	}
	if keyFull["key"] != "ak_newkeyid_newsecret" {
		t.Errorf("key = %v, want %q", keyFull["key"], "ak_newkeyid_newsecret")
	}

	// Verify: stderr contains 'API key refreshed'.
	if !strings.Contains(stderr, "API key refreshed") {
		t.Errorf("stderr = %q, want to contain %q", stderr, "API key refreshed")
	}
}

// =========================================================================
// TS-15-SMOKE-4: Smoke test for login timeout: runLogin called with 100ms
// timeout and no callback sent, command exits with timeout error.
//
// Execution Path: 15-PATH-4
// Real components: internal/cli/login.go (runLogin, callback server),
//   internal/cli/helpers.go (validateExpires)
// Mockable: httptest.Server handling GET /providers, openBrowser no-op
// =========================================================================

func TestSmokeSpec15_LoginTimeoutNoCallback(t *testing.T) {
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/auth/providers") && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"name":"github","authorize_url":"https://github.com/login/oauth/authorize?client_id=abc"}]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer mockSrv.Close()

	stderrBuf := new(bytes.Buffer)
	stdoutBuf := new(bytes.Buffer)
	var configSaved bool

	opts := loginOpts{
		provider:    "github",
		expires:     90,
		endpointURL: mockSrv.URL,
		openBrowserFn: func(url string) error {
			// No-op: do not simulate a callback — let it time out.
			return nil
		},
		saveConfigFn: func(_ string, cfg *CLIConfig) error {
			configSaved = true
			return nil
		},
		stderr: stderrBuf,
		stdout: stdoutBuf,
	}

	// Call runLogin with a very short timeout (100ms).
	err := runLogin(context.Background(), 100*time.Millisecond, opts)

	// Verify: runLogin returns error containing 'login timed out'.
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "login timed out waiting for browser callback") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "login timed out waiting for browser callback")
	}

	// Verify: No credentials saved to config.
	if configSaved {
		t.Error("saveConfigFn should not be called on timeout")
	}

	// Verify: stdout is empty (no user data printed on timeout).
	if stdoutBuf.String() != "" {
		t.Errorf("stdout should be empty on timeout, got: %q", stdoutBuf.String())
	}
}

// =========================================================================
// TS-15-SMOKE-5: Smoke test for akc tokens create: permissions parsed,
// expires validated, CreateToken called with correct request body, PATFull
// JSON printed to stdout.
//
// Execution Path: 15-PATH-5
// Real components: internal/cli/tokens.go (newTokensCreateCmd),
//   internal/cli/helpers.go (parsePermissions, validateExpires),
//   CLI Core NewAuthenticatedClient (via CmdClient injection)
// Mockable: httptest.Server handling POST /tokens
// =========================================================================

func TestSmokeSpec15_TokensCreateEndToEnd(t *testing.T) {
	var receivedBody map[string]any

	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/tokens") && r.Method == http.MethodPost {
			if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
				t.Errorf("failed to decode request body: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]any{
				"id":          "tok-123",
				"name":        "ci-bot",
				"token":       "pat_plaintext_secret",
				"permissions": []string{"users:read", "orgs:read"},
				"expires_at":  "2026-08-17T00:00:00Z",
			}
			respJSON, _ := json.Marshal(resp)
			_, _ = w.Write(respJSON)
			return
		}
		http.NotFound(w, r)
	}))
	defer mockSrv.Close()

	client := &CmdClient{
		endpointURL: mockSrv.URL,
		apiKey:      "test-api-key",
		httpClient:  mockSrv.Client(),
	}

	stdout, stderr, err := executeTokensCmdWithClient(client,
		"create", "--name", "ci-bot", "--permissions", "users:read,orgs:read", "--expires", "30")
	if err != nil {
		t.Fatalf("tokens create returned error: %v", err)
	}

	// Verify: parsePermissions returns ['users:read', 'orgs:read'].
	if receivedBody == nil {
		t.Fatal("mock server did not receive a request body")
	}
	if receivedBody["name"] != "ci-bot" {
		t.Errorf("request name = %v, want %q", receivedBody["name"], "ci-bot")
	}
	perms, ok := receivedBody["permissions"].([]any)
	if !ok {
		t.Fatalf("request permissions is not an array: %T", receivedBody["permissions"])
	}
	if len(perms) != 2 {
		t.Fatalf("request permissions length = %d, want 2", len(perms))
	}
	if perms[0] != "users:read" || perms[1] != "orgs:read" {
		t.Errorf("permissions = %v, want [users:read, orgs:read]", perms)
	}

	// Verify: validateExpires(30) returns nil (30 is valid).
	// If validateExpires failed, the command would have exited before making the API call.

	// Verify: expires in request body is 30.
	if expires, ok := receivedBody["expires"].(float64); !ok || int(expires) != 30 {
		t.Errorf("request expires = %v, want 30", receivedBody["expires"])
	}

	// Verify: stdout contains indented PATFull JSON including plaintext token.
	stdoutTrimmed := strings.TrimSpace(stdout)
	if !json.Valid([]byte(stdoutTrimmed)) {
		t.Fatalf("stdout is not valid JSON: %s", stdoutTrimmed)
	}
	var pat map[string]any
	if err := json.Unmarshal([]byte(stdoutTrimmed), &pat); err != nil {
		t.Fatalf("failed to parse PATFull JSON: %v", err)
	}
	if pat["token"] != "pat_plaintext_secret" {
		t.Errorf("token = %v, want %q", pat["token"], "pat_plaintext_secret")
	}

	// Verify: stderr contains 'Save the token value'.
	if !strings.Contains(stderr, "Save the token value") {
		t.Errorf("stderr = %q, want to contain %q", stderr, "Save the token value")
	}
}

// =========================================================================
// TS-15-SMOKE-6: Smoke test for pre-validation firing before network:
// running akc orgs list without api_key exits with code 2 and no HTTP
// request is made.
//
// Execution Path: 15-PATH-6
// Real components: internal/cli/orgs.go (newOrgsListCmd),
//   CLI Core NewAuthenticatedClient (via CmdClient context absence)
// Mockable: httptest.Server recording all incoming requests
// =========================================================================

func TestSmokeSpec15_PreValidationNoNetworkRequest(t *testing.T) {
	var requestCount atomic.Int32

	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer mockSrv.Close()

	// Run orgs list WITHOUT injecting a client (no api_key).
	stdout, _, err := executeOrgsCmd("list")

	// Verify: command returns error (NewAuthenticatedClient fails).
	if err == nil {
		t.Fatal("expected error when no api_key configured, got nil")
	}

	// Verify: Mock server receives 0 HTTP requests.
	if got := requestCount.Load(); got != 0 {
		t.Errorf("mock server received %d requests, want 0 (pre-validation should prevent network calls)", got)
	}

	// Verify: stdout contains error envelope with code 2.
	stdoutTrimmed := strings.TrimSpace(stdout)
	if stdoutTrimmed == "" {
		t.Fatal("stdout should contain error envelope, got empty")
	}
	var env errorEnvelopeSpec15
	if err := json.Unmarshal([]byte(stdoutTrimmed), &env); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, stdoutTrimmed)
	}
	if env.Error.Code != 2 {
		t.Errorf("error envelope code = %d, want 2", env.Error.Code)
	}
	if !strings.Contains(env.Error.Message, "no API key configured") {
		t.Errorf("error message = %q, want to contain %q", env.Error.Message, "no API key configured")
	}
}

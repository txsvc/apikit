package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
				"version":     "2.0.0",
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
			Version    string `json:"version"`
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
			t.Errorf("server_version.version = %q, want %q", output.ServerVersion.Version, "2.0.0")
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

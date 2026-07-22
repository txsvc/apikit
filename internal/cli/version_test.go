package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// serverVersionJSON is a test helper struct for parsing the server_version
// sub-object from the version command JSON output.
type serverVersionJSON struct {
	Version    string `json:"go_version"`
	BuildTime  string `json:"build_time"`
	Commit     string `json:"commit"`
	MountPoint string `json:"mount_point"`
}

// versionOutputJSON is a test helper struct for parsing the full version
// command JSON output, with a concrete struct type for server_version
// (allowing field-level assertions).
type versionOutputJSON struct {
	CLIVersion    string             `json:"cli_version"`
	Build         string             `json:"build"`
	Prefix        string             `json:"prefix"`
	ServerVersion *serverVersionJSON `json:"server_version,omitempty"`
}

// setupVersionTestEnv configures HOME, TokenPrefix, and a config directory
// with an empty config.toml for version command tests. Returns a cleanup
// function that restores the original values.
func setupVersionTestEnv(t *testing.T) {
	t.Helper()

	savedPrefix := TokenPrefix
	TokenPrefix = "testpfx"
	t.Cleanup(func() { TokenPrefix = savedPrefix })

	tmpHome := t.TempDir()
	savedHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", savedHome) })

	configDir := filepath.Join(tmpHome, ".testpfx")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	configContent := "endpoint_url = \"\"\nuser_id = \"\"\napi_key = \"\"\n"
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config.toml: %v", err)
	}
}

// saveAndSetEnv saves the current value of an env var, sets it to the new
// value, and registers a cleanup to restore it. If newValue is empty, the
// var is unset instead.
func saveAndSetEnv(t *testing.T, key, newValue string) {
	t.Helper()
	saved, had := os.LookupEnv(key)
	if newValue == "" {
		os.Unsetenv(key)
	} else {
		os.Setenv(key, newValue)
	}
	t.Cleanup(func() {
		if had {
			os.Setenv(key, saved)
		} else {
			os.Unsetenv(key)
		}
	})
}

// unsetEnv unsets an env var and registers a cleanup to restore its original value.
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	saveAndSetEnv(t, key, "")
}

// =========================================================================
// TS-13-42: The version command has 'auth': 'none' and 'composite': 'true'
// annotations, skipping PersistentPreRunE client creation.
// Requirement: 13-REQ-10.1
// =========================================================================

func TestVersionCmdAnnotations(t *testing.T) {
	rootCmd := RootCommand()

	// Walk the command tree to find the version subcommand.
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "version" {
			if cmd.Annotations == nil {
				t.Fatal("version command should have Annotations map")
			}
			if auth := cmd.Annotations["auth"]; auth != "none" {
				t.Errorf("version command Annotations['auth'] = %q, want %q", auth, "none")
			}
			if composite := cmd.Annotations["composite"]; composite != "true" {
				t.Errorf("version command Annotations['composite'] = %q, want %q", composite, "true")
			}
			return
		}
	}
	t.Fatal("version command not found under root command")
}

// =========================================================================
// TS-13-43: VersionOutput struct has correct fields and JSON tags;
// ServerVersion uses omitempty so it is omitted (not null) when nil.
// Requirement: 13-REQ-10.2
// =========================================================================

func TestVersionOutputStructJSONTags(t *testing.T) {
	out := VersionOutput{
		CLIVersion:    "1.0",
		Build:         "abc",
		Prefix:        "ak",
		ServerVersion: nil,
	}

	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("json.Marshal(VersionOutput) failed: %v", err)
	}

	jsonStr := string(data)

	// Must contain cli_version, build, prefix keys.
	if !strings.Contains(jsonStr, `"cli_version"`) {
		t.Error("JSON output should contain 'cli_version' key")
	}
	if !strings.Contains(jsonStr, `"build"`) {
		t.Error("JSON output should contain 'build' key")
	}
	if !strings.Contains(jsonStr, `"prefix"`) {
		t.Error("JSON output should contain 'prefix' key")
	}

	// server_version must be omitted entirely (not present as null)
	// when ServerVersion is nil, via the omitempty tag.
	if strings.Contains(jsonStr, "server_version") {
		t.Error("JSON output should NOT contain 'server_version' when ServerVersion is nil (omitempty)")
	}

	// Verify field values round-trip correctly.
	var parsed versionOutputJSON
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if parsed.CLIVersion != "1.0" {
		t.Errorf("CLIVersion = %q, want %q", parsed.CLIVersion, "1.0")
	}
	if parsed.Build != "abc" {
		t.Errorf("Build = %q, want %q", parsed.Build, "abc")
	}
	if parsed.Prefix != "ak" {
		t.Errorf("Prefix = %q, want %q", parsed.Prefix, "ak")
	}
	if parsed.ServerVersion != nil {
		t.Error("ServerVersion should be nil after round-trip")
	}
}

// =========================================================================
// TS-13-44: Version command RunE constructs an apikit.Client and calls
// client.Version(ctx) when ResolveEndpointURL returns non-empty URL.
// Requirement: 13-REQ-10.3
// =========================================================================

func TestVersionCmdWithMockServer(t *testing.T) {
	// Start a mock server that responds to GET /version with VersionResponse.
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]string{
				"go_version":  "2.0.0",
				"build_time":  "2025-06-01T00:00:00Z",
				"commit":      "def456",
				"mount_point": "/api/v1",
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockServer.Close()

	// Configure ENDPOINT_URL to point to the mock server.
	saveAndSetEnv(t, "ENDPOINT_URL", mockServer.URL)
	setupVersionTestEnv(t)

	rootCmd := RootCommand()
	rootCmd.SetArgs([]string{"version"})

	stdout, _ := captureStdoutAndStderr(t, func() {
		err := rootCmd.Execute()
		if err != nil {
			PrintError(err)
		}
	})

	// stdout should contain valid JSON output from the version command.
	if len(strings.TrimSpace(stdout)) == 0 {
		t.Fatal("version command should produce JSON output on stdout")
	}

	var output versionOutputJSON
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("failed to parse version JSON output: %v\nstdout: %s", err, stdout)
	}

	// server_version should be present when endpoint is configured and reachable.
	if output.ServerVersion == nil {
		t.Fatal("server_version should be present when endpoint is configured and reachable")
	}

	// Verify individual server_version fields.
	if output.ServerVersion.Version == "" {
		t.Error("server_version.version should not be empty")
	}
	if output.ServerVersion.BuildTime == "" {
		t.Error("server_version.build_time should not be empty")
	}
	if output.ServerVersion.Commit == "" {
		t.Error("server_version.commit should not be empty")
	}
	if output.ServerVersion.MountPoint == "" {
		t.Error("server_version.mount_point should not be empty")
	}
}

// =========================================================================
// TS-13-45: Version command omits server_version and prints no warning
// when ResolveEndpointURL returns empty string.
// Requirement: 13-REQ-10.4
// =========================================================================

func TestVersionCmdNoEndpointConfigured(t *testing.T) {
	// Ensure ENDPOINT_URL is not set anywhere.
	unsetEnv(t, "ENDPOINT_URL")
	setupVersionTestEnv(t)

	rootCmd := RootCommand()
	rootCmd.SetArgs([]string{"version"})

	stdout, stderr := captureStdoutAndStderr(t, func() {
		err := rootCmd.Execute()
		if err != nil {
			PrintError(err)
		}
	})

	// stdout should contain valid JSON.
	if len(strings.TrimSpace(stdout)) == 0 {
		t.Fatal("version command should produce JSON output on stdout")
	}

	var output versionOutputJSON
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("failed to parse version JSON output: %v\nstdout: %s", err, stdout)
	}

	// cli_version, build, prefix must be present.
	if output.CLIVersion == "" {
		t.Error("cli_version should be present in version output")
	}

	// server_version must NOT be present (no endpoint configured).
	if output.ServerVersion != nil {
		t.Error("server_version should not be present when no endpoint is configured")
	}

	// stderr must be empty (no warning for unconfigured endpoint — this is normal).
	if len(strings.TrimSpace(stderr)) > 0 {
		t.Errorf("stderr should be empty when no endpoint is configured, got: %s", stderr)
	}
}

// =========================================================================
// TS-13-46: Version command omits server_version and prints warning to
// stderr when endpoint is configured but server is unreachable.
// Requirement: 13-REQ-10.5
// =========================================================================

func TestVersionCmdUnreachableEndpoint(t *testing.T) {
	// Set ENDPOINT_URL to a non-listening address.
	saveAndSetEnv(t, "ENDPOINT_URL", "http://127.0.0.1:19999")
	setupVersionTestEnv(t)

	rootCmd := RootCommand()
	rootCmd.SetArgs([]string{"version"})

	var execErr error
	stdout, stderr := captureStdoutAndStderr(t, func() {
		execErr = rootCmd.Execute()
		if execErr != nil {
			PrintError(execErr)
		}
	})

	// Exit code should be 0 — version command does not fail on server errors.
	if ExitCode(execErr) != 0 {
		t.Errorf("ExitCode should be 0 for version with unreachable server, got %d", ExitCode(execErr))
	}

	// stdout should contain valid JSON.
	if len(strings.TrimSpace(stdout)) == 0 {
		t.Fatal("version command should produce JSON output on stdout")
	}

	var output versionOutputJSON
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("failed to parse version JSON output: %v\nstdout: %s", err, stdout)
	}

	// server_version must NOT be present.
	if output.ServerVersion != nil {
		t.Error("server_version should not be present when endpoint is unreachable")
	}

	// stderr should contain the specific warning message.
	if !strings.Contains(stderr, "warning: could not reach server:") {
		t.Errorf("stderr should contain 'warning: could not reach server:', got: %q", stderr)
	}
}

// =========================================================================
// TS-13-E13: Version command's context has a deadline/timeout;
// client.Version(ctx) call terminates within the deadline when server hangs.
// Requirement: 13-REQ-10.E1
// =========================================================================

func TestVersionCmdContextDeadline(t *testing.T) {
	// Start a mock server that never responds (blocks until request context
	// is cancelled or test cleanup fires).
	hangDone := make(chan struct{})
	hangServer := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-hangDone:
		case <-r.Context().Done():
		}
	}))
	defer hangServer.Close()
	defer close(hangDone) // runs before hangServer.Close() (LIFO)

	// Set ENDPOINT_URL to the hanging server.
	saveAndSetEnv(t, "ENDPOINT_URL", hangServer.URL)
	setupVersionTestEnv(t)

	rootCmd := RootCommand()
	rootCmd.SetArgs([]string{"version"})

	start := time.Now()
	var execErr error
	stdout, stderr := captureStdoutAndStderr(t, func() {
		execErr = rootCmd.Execute()
		if execErr != nil {
			PrintError(execErr)
		}
	})
	elapsed := time.Since(start)

	// Command must return within a reasonable timeout (10 seconds).
	// The implementation should use a context deadline of ~5 seconds.
	if elapsed > 10*time.Second {
		t.Errorf("version command took %v, should complete within 10 seconds (context deadline)", elapsed)
	}

	// Exit code should be 0.
	if ExitCode(execErr) != 0 {
		t.Errorf("ExitCode should be 0 when server hangs beyond deadline, got %d", ExitCode(execErr))
	}

	// stdout should contain valid JSON.
	if len(strings.TrimSpace(stdout)) == 0 {
		t.Fatal("version command should produce JSON output on stdout")
	}

	var output versionOutputJSON
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("failed to parse version JSON output: %v\nstdout: %s", err, stdout)
	}

	// server_version must NOT be present (timed out).
	if output.ServerVersion != nil {
		t.Error("server_version should not be present when server hangs beyond deadline")
	}

	// stderr should contain a warning about the timeout/unreachable server.
	if !strings.Contains(stderr, "warning") {
		t.Errorf("stderr should contain a warning when server hangs beyond deadline, got: %q", stderr)
	}
}

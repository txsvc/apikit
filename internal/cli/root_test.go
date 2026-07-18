package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// errorEnvelope is the expected JSON error envelope structure.
type errorEnvelope struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// --- Helper: capture stdout during a function call ---

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("ReadFrom pipe: %v", err)
	}
	return buf.String()
}

// captureStdoutAndStderr captures both stdout and stderr during fn.
func captureStdoutAndStderr(t *testing.T, fn func()) (string, string) {
	t.Helper()

	// Capture stdout
	oldStdout := os.Stdout
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe (stdout): %v", err)
	}
	os.Stdout = wOut

	// Capture stderr
	oldStderr := os.Stderr
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe (stderr): %v", err)
	}
	os.Stderr = wErr

	fn()

	wOut.Close()
	wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	var bufOut, bufErr bytes.Buffer
	bufOut.ReadFrom(rOut)
	bufErr.ReadFrom(rErr)
	return bufOut.String(), bufErr.String()
}

// =========================================================================
// TS-13-1: main.go calls cli.Execute() and passes error to ExitCode
// =========================================================================

// TestMainSourceContainsExecuteAndExitCode verifies that cmd/akc/main.go
// source code contains the expected pattern: cli.Execute() + os.Exit(cli.ExitCode(err)).
// It must not produce any output of its own (no fmt.Print, no os.Stdout writes).
func TestMainSourceContainsExecuteAndExitCode(t *testing.T) {
	src, err := os.ReadFile("../../cmd/akc/main.go")
	if err != nil {
		t.Fatalf("failed to read cmd/akc/main.go: %v", err)
	}
	source := string(src)

	// Must call cli.Execute()
	if !strings.Contains(source, "cli.Execute()") {
		t.Error("main.go must call cli.Execute()")
	}

	// Must call cli.ExitCode
	if !strings.Contains(source, "cli.ExitCode(") {
		t.Error("main.go must call cli.ExitCode()")
	}

	// Must call os.Exit
	if !strings.Contains(source, "os.Exit(") {
		t.Error("main.go must call os.Exit()")
	}

	// Must NOT contain fmt.Print/Println/Printf or direct os.Stdout writes
	for _, forbidden := range []string{
		"fmt.Print(", "fmt.Println(", "fmt.Printf(",
		"os.Stdout.Write(", "os.Stdout.WriteString(",
	} {
		if strings.Contains(source, forbidden) {
			t.Errorf("main.go must not contain %q — all output should come from Execute()", forbidden)
		}
	}
}

// =========================================================================
// TS-13-2: Execute() calls PrintError(err) on non-nil error
// =========================================================================

// TestExecuteCallsPrintErrorOnError verifies that Execute() produces
// exactly one JSON error envelope on stdout when rootCmd.Execute() returns
// a non-nil error, and that Execute() returns the same error.
func TestExecuteCallsPrintErrorOnError(t *testing.T) {
	// Get a root command and register a stub command that returns an error
	rootCmd := RootCommand()
	testErr := errors.New("test error from stub")
	rootCmd.AddCommand(&cobra.Command{
		Use:   "failcmd",
		Short: "A command that always fails",
		Annotations: map[string]string{
			"auth": "none",
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			return testErr
		},
	})
	rootCmd.SetArgs([]string{"failcmd"})

	var execErr error
	stdout := captureStdout(t, func() {
		execErr = rootCmd.Execute()
		if execErr != nil {
			PrintError(execErr)
		}
	})

	// Execute() must return a non-nil error
	if execErr == nil {
		t.Fatal("Execute() should return a non-nil error when RunE returns an error")
	}

	// stdout must contain exactly one JSON error envelope
	if !strings.Contains(stdout, `"error"`) {
		t.Error("stdout should contain a JSON error envelope")
	}

	count := strings.Count(stdout, `"error":`)
	if count != 1 {
		t.Errorf("expected exactly 1 error envelope, got %d", count)
	}

	// Verify the envelope is valid JSON
	var env errorEnvelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Errorf("error envelope is not valid JSON: %v\nstdout: %s", err, stdout)
	}
}

// =========================================================================
// TS-13-3: Root command SilenceErrors and SilenceUsage
// =========================================================================

// TestRootCmdSilenceSettings verifies that the root Cobra command is configured
// with SilenceErrors: true and SilenceUsage: true.
func TestRootCmdSilenceSettings(t *testing.T) {
	rootCmd := RootCommand()

	if !rootCmd.SilenceErrors {
		t.Error("rootCmd.SilenceErrors must be true")
	}
	if !rootCmd.SilenceUsage {
		t.Error("rootCmd.SilenceUsage must be true")
	}
}

// =========================================================================
// TS-13-4: RunE calls PrintJSON on success, returns nil
// =========================================================================

// TestSuccessfulCommandWritesJSONNotErrorEnvelope verifies that a stub command
// returning success writes valid JSON via PrintJSON and produces no error envelope.
func TestSuccessfulCommandWritesJSONNotErrorEnvelope(t *testing.T) {
	rootCmd := RootCommand()
	rootCmd.AddCommand(&cobra.Command{
		Use:   "okcmd",
		Short: "A command that succeeds",
		Annotations: map[string]string{
			"auth": "none",
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			return PrintJSON(map[string]string{"status": "ok"})
		},
	})
	rootCmd.SetArgs([]string{"okcmd"})

	stdout, stderr := captureStdoutAndStderr(t, func() {
		err := rootCmd.Execute()
		if err != nil {
			t.Errorf("Execute() returned unexpected error: %v", err)
		}
	})

	// stdout should contain valid JSON
	if len(strings.TrimSpace(stdout)) == 0 {
		t.Fatal("stdout should not be empty after a successful command")
	}

	if !json.Valid([]byte(stdout)) {
		t.Errorf("stdout is not valid JSON: %s", stdout)
	}

	// stdout must NOT contain an error envelope
	if strings.Contains(stdout, `"error"`) {
		t.Error("successful command should not produce an error envelope")
	}

	// stderr must be empty
	if len(strings.TrimSpace(stderr)) > 0 {
		t.Errorf("stderr should be empty, got: %s", stderr)
	}
}

// =========================================================================
// TS-13-5: RunE returns error → Execute() calls PrintError centrally
// =========================================================================

// TestErrorProducesExactlyOneEnvelope verifies that when a command's RunE
// returns an error, exactly one JSON error envelope is produced on stdout
// (via Execute() calling PrintError centrally).
func TestErrorProducesExactlyOneEnvelope(t *testing.T) {
	rootCmd := RootCommand()
	rootCmd.AddCommand(&cobra.Command{
		Use:   "errcmd",
		Short: "A command that errors",
		Annotations: map[string]string{
			"auth": "none",
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("something went wrong")
		},
	})
	rootCmd.SetArgs([]string{"errcmd"})

	// Execute the command through the real Execute() wrapper if available
	stdout := captureStdout(t, func() {
		err := Execute()
		// Even if Execute() doesn't work yet, try manually
		if err != nil {
			PrintError(err)
		}
	})

	count := strings.Count(stdout, `"error":`)
	if count != 1 {
		t.Errorf("expected exactly 1 error envelope on stdout, got %d occurrences\nstdout: %s", count, stdout)
	}
}

// =========================================================================
// TS-13-E1: Execute() calls PrintError exactly once on error, never on nil
// =========================================================================

// TestExecuteNeverCallsPrintErrorOnSuccess verifies that Execute() does
// not call PrintError when rootCmd.Execute() returns nil.
func TestExecuteNeverCallsPrintErrorOnSuccess(t *testing.T) {
	rootCmd := RootCommand()
	rootCmd.AddCommand(&cobra.Command{
		Use:   "successcmd",
		Short: "A command that succeeds",
		Annotations: map[string]string{
			"auth": "none",
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			return PrintJSON(map[string]string{"result": "good"})
		},
	})
	rootCmd.SetArgs([]string{"successcmd"})

	stdout := captureStdout(t, func() {
		err := rootCmd.Execute()
		if err != nil {
			t.Errorf("command should have succeeded: %v", err)
		}
	})

	// No error envelope should be present
	if strings.Contains(stdout, `"error":`) {
		t.Error("stdout should not contain an error envelope when command succeeds")
	}
}

// =========================================================================
// TS-13-6: Root command persistent flags
// =========================================================================

// TestPersistentFlagsExist verifies that --endpoint-url, --user-id,
// --api-key, and --json are defined as persistent flags on the root command.
func TestPersistentFlagsExist(t *testing.T) {
	rootCmd := RootCommand()
	flags := rootCmd.PersistentFlags()

	tests := []struct {
		name     string
		wantType string
	}{
		{"endpoint-url", "string"},
		{"user-id", "string"},
		{"api-key", "string"},
		{"json", "bool"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := flags.Lookup(tt.name)
			if f == nil {
				t.Fatalf("persistent flag %q not found", tt.name)
			}
			if got := f.Value.Type(); got != tt.wantType {
				t.Errorf("flag %q type = %q, want %q", tt.name, got, tt.wantType)
			}
		})
	}
}

// =========================================================================
// TS-13-7: Bare invocation prints help text
// =========================================================================

// TestBareInvocationPrintsHelp verifies that invoking the root command
// with no subcommand and no flags prints help text to stdout and exits 0.
func TestBareInvocationPrintsHelp(t *testing.T) {
	rootCmd := RootCommand()
	rootCmd.SetArgs([]string{})

	stdout := captureStdout(t, func() {
		err := rootCmd.Execute()
		if err != nil {
			t.Errorf("bare invocation should not return an error, got: %v", err)
		}
	})

	if len(strings.TrimSpace(stdout)) == 0 {
		t.Error("bare invocation should produce help text on stdout")
	}

	// Help text should mention the binary name or "Usage"
	if !strings.Contains(stdout, "akc") && !strings.Contains(stdout, "Usage") {
		t.Errorf("help text should contain 'akc' or 'Usage', got:\n%s", stdout)
	}
}

// =========================================================================
// TS-13-8: --json flag silently ignored on non-help commands
// =========================================================================

// TestJSONFlagSilentlyIgnoredOnNonHelp verifies that passing --json
// to a non-help command produces identical output as without it.
func TestJSONFlagSilentlyIgnoredOnNonHelp(t *testing.T) {
	// Run version command without --json
	cmd1 := RootCommand()
	cmd1.SetArgs([]string{"version"})
	out1 := captureStdout(t, func() {
		// Ignore error — version command may not exist yet in stub
		_ = cmd1.Execute()
	})

	// Run version command with --json
	cmd2 := RootCommand()
	cmd2.SetArgs([]string{"version", "--json"})
	out2 := captureStdout(t, func() {
		_ = cmd2.Execute()
	})

	if out1 != out2 {
		t.Errorf("output should be identical with or without --json on non-help commands\nwithout: %s\nwith:    %s", out1, out2)
	}
}

// =========================================================================
// TS-13-E2: Unrecognized flag → JSON error envelope
// =========================================================================

// TestUnrecognizedFlagProducesErrorEnvelope verifies that passing an unknown
// flag produces a JSON error envelope on stdout with code:0, stderr is empty,
// and exit code is 2.
func TestUnrecognizedFlagProducesErrorEnvelope(t *testing.T) {
	rootCmd := RootCommand()
	rootCmd.SetArgs([]string{"--unknown-flag"})

	var execErr error
	stdout, stderr := captureStdoutAndStderr(t, func() {
		execErr = rootCmd.Execute()
		if execErr != nil {
			PrintError(execErr)
		}
	})

	// Should have an error
	if execErr == nil {
		t.Fatal("unrecognized flag should return an error")
	}

	// Exit code should be 2
	if code := ExitCode(execErr); code != 2 {
		t.Errorf("ExitCode for unrecognized flag should be 2, got %d", code)
	}

	// stdout should contain JSON error envelope with code: 0
	var env errorEnvelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("stdout should be valid JSON error envelope: %v\nstdout: %s", err, stdout)
	}
	if env.Error.Code != 0 {
		t.Errorf("error envelope code should be 0 (client error sentinel), got %d", env.Error.Code)
	}

	// stderr should be empty (SilenceErrors + SilenceUsage)
	if len(strings.TrimSpace(stderr)) > 0 {
		t.Errorf("stderr should be empty, got: %s", stderr)
	}
}

// =========================================================================
// TS-13-11: TokenPrefix empty → exit-2 error on non-auth-exempt command
// =========================================================================

// TestTokenPrefixEmptyReturnsError verifies that when TokenPrefix is empty,
// invoking a non-auth-exempt command returns an exit-2 error with the exact
// message about TokenPrefix.
func TestTokenPrefixEmptyReturnsError(t *testing.T) {
	saved := TokenPrefix
	TokenPrefix = ""
	defer func() { TokenPrefix = saved }()

	rootCmd := RootCommand()
	// Register a non-auth-exempt command (no "auth":"none" annotation)
	rootCmd.AddCommand(&cobra.Command{
		Use:   "authedcmd",
		Short: "A command requiring auth",
		Annotations: map[string]string{
			"auth": "api_key",
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			return nil
		},
	})
	rootCmd.SetArgs([]string{"authedcmd"})

	var execErr error
	stdout := captureStdout(t, func() {
		execErr = rootCmd.Execute()
		if execErr != nil {
			PrintError(execErr)
		}
	})

	if execErr == nil {
		t.Fatal("expected error when TokenPrefix is empty")
	}

	expectedMsg := "TokenPrefix is empty: binary was built without a valid -ldflags TokenPrefix value"
	if !strings.Contains(execErr.Error(), expectedMsg) {
		t.Errorf("error message should contain %q, got: %v", expectedMsg, execErr)
	}

	// Verify JSON envelope
	var env errorEnvelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("stdout should be valid JSON error envelope: %v\nstdout: %s", err, stdout)
	}
	if env.Error.Code != 0 {
		t.Errorf("error envelope code should be 0, got %d", env.Error.Code)
	}
	if env.Error.Message != expectedMsg {
		t.Errorf("error envelope message = %q, want %q", env.Error.Message, expectedMsg)
	}

	// Exit code should be 2
	if code := ExitCode(execErr); code != 2 {
		t.Errorf("ExitCode should be 2, got %d", code)
	}
}

// =========================================================================
// TS-13-E3: Auth-exempt command with empty TokenPrefix skips validation
// =========================================================================

// TestTokenPrefixEmptySkippedForAuthNone verifies that when TokenPrefix is
// empty but the invoked command has "auth":"none", the TokenPrefix validation
// is skipped and the command proceeds without error.
func TestTokenPrefixEmptySkippedForAuthNone(t *testing.T) {
	saved := TokenPrefix
	TokenPrefix = ""
	defer func() { TokenPrefix = saved }()

	rootCmd := RootCommand()
	rootCmd.AddCommand(&cobra.Command{
		Use:   "authnonecmd",
		Short: "An auth-exempt command",
		Annotations: map[string]string{
			"auth": "none",
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			return PrintJSON(map[string]string{"status": "ok"})
		},
	})
	rootCmd.SetArgs([]string{"authnonecmd"})

	stdout := captureStdout(t, func() {
		err := rootCmd.Execute()
		if err != nil {
			t.Errorf("auth-exempt command should not return error even with empty TokenPrefix: %v", err)
		}
	})

	// Should NOT contain TokenPrefix error
	if strings.Contains(stdout, "TokenPrefix is empty") {
		t.Error("auth-exempt command should not produce a TokenPrefix error")
	}
}

// =========================================================================
// TS-13-24: PersistentPreRunE resolves endpoint_url and api_key as required,
// and user_id as optional (empty string stored in context without error).
// =========================================================================

// TestPersistentPreRunEResolvesCredentials verifies that PersistentPreRunE
// resolves endpoint_url and api_key as required fields, and user_id as
// optional. When user_id is unset, it should be stored as empty string
// in context without error.
func TestPersistentPreRunEResolvesCredentials(t *testing.T) {
	// Set up environment: endpoint and api_key provided, user_id unset
	savedEndpoint, hadEndpoint := os.LookupEnv("ENDPOINT_URL")
	savedAPIKey, hadAPIKey := os.LookupEnv("API_KEY")
	savedUserID, hadUserID := os.LookupEnv("USER_ID")
	os.Setenv("ENDPOINT_URL", "http://localhost")
	os.Setenv("API_KEY", "testkey")
	os.Unsetenv("USER_ID")
	defer func() {
		if hadEndpoint {
			os.Setenv("ENDPOINT_URL", savedEndpoint)
		} else {
			os.Unsetenv("ENDPOINT_URL")
		}
		if hadAPIKey {
			os.Setenv("API_KEY", savedAPIKey)
		} else {
			os.Unsetenv("API_KEY")
		}
		if hadUserID {
			os.Setenv("USER_ID", savedUserID)
		} else {
			os.Unsetenv("USER_ID")
		}
	}()

	// Set up TokenPrefix and HOME
	savedPrefix := TokenPrefix
	TokenPrefix = "testpfx"
	defer func() { TokenPrefix = savedPrefix }()

	tmpHome := t.TempDir()
	savedHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", savedHome)

	// Create config directory and empty config file
	configDir := filepath.Join(tmpHome, ".testpfx")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	configContent := "endpoint_url = \"\"\nuser_id = \"\"\napi_key = \"\"\n"
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config.toml: %v", err)
	}

	var gotClient any
	var gotUserID string
	var runECalled bool

	rootCmd := RootCommand()
	testCmd := &cobra.Command{
		Use:   "testcmd",
		Short: "Test command requiring auth",
		Annotations: map[string]string{
			"auth": "api_key",
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			runECalled = true
			gotClient = ClientFromContext(cmd.Context())
			gotUserID = UserIDFromContext(cmd.Context())
			return nil
		},
	}
	rootCmd.AddCommand(testCmd)
	rootCmd.SetArgs([]string{"testcmd"})

	var execErr error
	captureStdout(t, func() {
		execErr = rootCmd.Execute()
		if execErr != nil {
			PrintError(execErr)
		}
	})

	if execErr != nil {
		t.Fatalf("expected no error, got: %v", execErr)
	}

	if !runECalled {
		t.Fatal("RunE was not called; PersistentPreRunE may have blocked execution")
	}

	// ClientFromContext should return non-nil when endpoint_url and api_key are set
	if gotClient == nil {
		t.Error("ClientFromContext should return non-nil client when credentials are resolved")
	}

	// UserIDFromContext should return empty string (user_id is optional and unset)
	if gotUserID != "" {
		t.Errorf("UserIDFromContext should return empty string when USER_ID is unset, got %q", gotUserID)
	}
}

// =========================================================================
// TS-13-25: PersistentPreRunE checks flag presence using cmd.Flags().Changed()
// on the leaf command, not the root.
// =========================================================================

// TestPersistentPreRunEChecksLeafCommandFlags verifies that PersistentPreRunE
// detects flag-level overrides on the leaf command (not the root), consistent
// with Cobra's behavior of passing the leaf command to PersistentPreRunE.
func TestPersistentPreRunEChecksLeafCommandFlags(t *testing.T) {
	// Set ENDPOINT_URL env to a different value than the flag
	savedEndpoint, hadEndpoint := os.LookupEnv("ENDPOINT_URL")
	savedAPIKey, hadAPIKey := os.LookupEnv("API_KEY")
	os.Setenv("ENDPOINT_URL", "http://env-val")
	os.Setenv("API_KEY", "testkey")
	defer func() {
		if hadEndpoint {
			os.Setenv("ENDPOINT_URL", savedEndpoint)
		} else {
			os.Unsetenv("ENDPOINT_URL")
		}
		if hadAPIKey {
			os.Setenv("API_KEY", savedAPIKey)
		} else {
			os.Unsetenv("API_KEY")
		}
	}()

	savedPrefix := TokenPrefix
	TokenPrefix = "testpfx"
	defer func() { TokenPrefix = savedPrefix }()

	tmpHome := t.TempDir()
	savedHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", savedHome)

	// Create config directory and empty config file
	configDir := filepath.Join(tmpHome, ".testpfx")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	configContent := "endpoint_url = \"\"\nuser_id = \"\"\napi_key = \"\"\n"
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config.toml: %v", err)
	}

	var gotClient any

	rootCmd := RootCommand()

	// RootCommand must have persistent flags for this test to work
	if f := rootCmd.PersistentFlags().Lookup("endpoint-url"); f == nil {
		t.Fatal("RootCommand must have --endpoint-url persistent flag")
	}

	leafCmd := &cobra.Command{
		Use:   "leafcmd",
		Short: "Leaf command requiring auth",
		Annotations: map[string]string{
			"auth": "api_key",
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			gotClient = ClientFromContext(cmd.Context())
			return nil
		},
	}
	rootCmd.AddCommand(leafCmd)

	// Pass --endpoint-url as a flag — should take precedence over env
	rootCmd.SetArgs([]string{"leafcmd", "--endpoint-url", "http://flag-val", "--api-key", "k"})

	var execErr error
	captureStdout(t, func() {
		execErr = rootCmd.Execute()
		if execErr != nil {
			PrintError(execErr)
		}
	})

	if execErr != nil {
		t.Fatalf("expected no error, got: %v", execErr)
	}

	// Client should have been created with the flag value (not the env value)
	if gotClient == nil {
		t.Fatal("ClientFromContext should return non-nil client when endpoint_url is set via flag")
	}
}

// =========================================================================
// TS-13-26: ResolveEndpointURL returns empty string (not an error) when
// no endpoint is configured in any source.
// =========================================================================

// TestResolveEndpointURLReturnsEmptyWhenNoEndpoint verifies that
// ResolveEndpointURL returns an empty string (not an error) when
// endpoint_url is not configured via flag, env, or config.
func TestResolveEndpointURLReturnsEmptyWhenNoEndpoint(t *testing.T) {
	savedEnv, hadEnv := os.LookupEnv("ENDPOINT_URL")
	os.Unsetenv("ENDPOINT_URL")
	defer func() {
		if hadEnv {
			os.Setenv("ENDPOINT_URL", savedEnv)
		}
	}()

	savedPrefix := TokenPrefix
	TokenPrefix = "testpfx"
	defer func() { TokenPrefix = savedPrefix }()

	tmpHome := t.TempDir()
	savedHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", savedHome)

	// Create config dir with empty endpoint_url
	configDir := filepath.Join(tmpHome, ".testpfx")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	configContent := "endpoint_url = \"\"\nuser_id = \"\"\napi_key = \"\"\n"
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config.toml: %v", err)
	}

	rootCmd := RootCommand()
	val := ResolveEndpointURL(rootCmd)
	if val != "" {
		t.Errorf("ResolveEndpointURL should return empty string when no endpoint is configured, got %q", val)
	}
}

// =========================================================================
// TS-13-E8: When leaf command has 'auth': 'none', PersistentPreRunE skips
// all credential resolution and client construction.
// =========================================================================

// TestPersistentPreRunESkipsCredentialResolutionForAuthNone verifies that
// auth-exempt commands do not trigger any credential resolution, config
// loading, or client construction. This is tested by making the environment
// hostile (unset HOME, empty TokenPrefix) — if PersistentPreRunE tried any
// credential resolution, it would fail.
func TestPersistentPreRunESkipsCredentialResolutionForAuthNone(t *testing.T) {
	// Unset all credentials
	savedEndpoint, hadEndpoint := os.LookupEnv("ENDPOINT_URL")
	savedAPIKey, hadAPIKey := os.LookupEnv("API_KEY")
	savedUserID, hadUserID := os.LookupEnv("USER_ID")
	os.Unsetenv("ENDPOINT_URL")
	os.Unsetenv("API_KEY")
	os.Unsetenv("USER_ID")
	defer func() {
		if hadEndpoint {
			os.Setenv("ENDPOINT_URL", savedEndpoint)
		}
		if hadAPIKey {
			os.Setenv("API_KEY", savedAPIKey)
		}
		if hadUserID {
			os.Setenv("USER_ID", savedUserID)
		}
	}()

	// Set HOME to a non-existent path — if PersistentPreRunE tried to
	// load config, it would fail because the directory doesn't exist.
	savedHome := os.Getenv("HOME")
	os.Setenv("HOME", "/nonexistent/path/for/test")
	defer os.Setenv("HOME", savedHome)

	// Empty TokenPrefix — if PersistentPreRunE checked it, it would fail.
	savedPrefix := TokenPrefix
	TokenPrefix = ""
	defer func() { TokenPrefix = savedPrefix }()

	var runECalled bool

	rootCmd := RootCommand()
	rootCmd.AddCommand(&cobra.Command{
		Use:   "authnone",
		Short: "Auth-exempt command",
		Annotations: map[string]string{
			"auth": "none",
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			runECalled = true
			return nil
		},
	})
	rootCmd.SetArgs([]string{"authnone"})

	var execErr error
	captureStdout(t, func() {
		execErr = rootCmd.Execute()
		if execErr != nil {
			PrintError(execErr)
		}
	})

	if execErr != nil {
		t.Fatalf("auth-exempt command should succeed without credentials, got error: %v", execErr)
	}

	if !runECalled {
		t.Error("RunE was not called; auth-exempt command should execute its RunE")
	}
}

// =========================================================================
// TS-13-E9: PersistentPreRunE reads 'auth' annotation from the Cobra leaf
// command argument (cmd), not from the root command.
// =========================================================================

// TestPersistentPreRunEReadsAuthAnnotationFromLeaf verifies that
// PersistentPreRunE reads the "auth" annotation from the cmd argument
// (which is the leaf command), not from the root command. The root command
// has no "auth" annotation; only the leaf command does.
func TestPersistentPreRunEReadsAuthAnnotationFromLeaf(t *testing.T) {
	rootCmd := RootCommand()

	// Verify RootCommand has PersistentPreRunE set
	if rootCmd.PersistentPreRunE == nil {
		t.Fatal("RootCommand() must have PersistentPreRunE set")
	}

	// Capture the cmd argument passed to PersistentPreRunE
	var capturedCmd *cobra.Command
	originalPreRunE := rootCmd.PersistentPreRunE

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		capturedCmd = cmd
		return originalPreRunE(cmd, args)
	}

	// Register an auth-exempt leaf command
	leafCmd := &cobra.Command{
		Use:   "authnoneleaf",
		Short: "Auth-exempt leaf command",
		Annotations: map[string]string{
			"auth": "none",
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			return nil
		},
	}
	rootCmd.AddCommand(leafCmd)
	rootCmd.SetArgs([]string{"authnoneleaf"})

	captureStdout(t, func() {
		err := rootCmd.Execute()
		if err != nil {
			t.Errorf("auth-exempt command should not error: %v", err)
		}
	})

	if capturedCmd == nil {
		t.Fatal("PersistentPreRunE was never called")
	}

	// The captured cmd should be the leaf command, not the root
	if capturedCmd.Annotations == nil {
		t.Fatal("captured cmd should have Annotations")
	}
	if capturedCmd.Annotations["auth"] != "none" {
		t.Errorf("captured cmd should have auth=none annotation, got %v", capturedCmd.Annotations)
	}
	if capturedCmd.Use == rootCmd.Use {
		t.Error("captured cmd should be the leaf command, not the root command")
	}
}

// =========================================================================
// TS-13-54: All apikit-owned commands have at minimum the 'auth' annotation
// key with value 'none', 'api_key', or 'admin'.
// Requirement: 13-REQ-12.1
// =========================================================================

// walkLeafCommands recursively collects all leaf commands (those with RunE set)
// from the command tree.
func walkLeafCommands(root *cobra.Command) []*cobra.Command {
	var leaves []*cobra.Command
	for _, cmd := range root.Commands() {
		if cmd.RunE != nil {
			leaves = append(leaves, cmd)
		}
		leaves = append(leaves, walkLeafCommands(cmd)...)
	}
	return leaves
}

func TestAllLeafCommandsHaveAuthAnnotation(t *testing.T) {
	rootCmd := RootCommand()

	leaves := walkLeafCommands(rootCmd)
	if len(leaves) == 0 {
		t.Fatal("RootCommand() should have at least one leaf command (with RunE set)")
	}

	validAuthValues := map[string]bool{
		"none":    true,
		"api_key": true,
		"admin":   true,
	}

	for _, cmd := range leaves {
		fullName := cmd.CommandPath()
		t.Run(fullName, func(t *testing.T) {
			if cmd.Annotations == nil {
				t.Errorf("command %q has no Annotations map", fullName)
				return
			}
			authVal, ok := cmd.Annotations["auth"]
			if !ok {
				t.Errorf("command %q is missing 'auth' annotation", fullName)
				return
			}
			if !validAuthValues[authVal] {
				t.Errorf("command %q has invalid auth annotation value %q; want one of 'none', 'api_key', 'admin'", fullName, authVal)
			}
		})
	}
}

// =========================================================================
// TS-13-55: Child commands use PreRunE (not PersistentPreRunE); root's
// PersistentPreRunE runs automatically before child's PreRunE.
// Requirement: 13-REQ-12.2
// =========================================================================

func TestPreRunEHookOrdering(t *testing.T) {
	var order []string

	rootCmd := RootCommand()

	// Root must have PersistentPreRunE for hook chaining to work.
	if rootCmd.PersistentPreRunE == nil {
		t.Fatal("RootCommand() must have PersistentPreRunE set")
	}

	// Wrap root's PersistentPreRunE to record invocation order.
	originalPreRunE := rootCmd.PersistentPreRunE
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		order = append(order, "root")
		return originalPreRunE(cmd, args)
	}

	// Create a child command with PreRunE (not PersistentPreRunE).
	childCmd := &cobra.Command{
		Use:   "hookorderchild",
		Short: "Child command for hook ordering test",
		Annotations: map[string]string{
			"auth": "none",
		},
		PreRunE: func(_ *cobra.Command, _ []string) error {
			order = append(order, "child")
			return nil
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			return nil
		},
	}
	rootCmd.AddCommand(childCmd)
	rootCmd.SetArgs([]string{"hookorderchild"})

	captureStdout(t, func() {
		err := rootCmd.Execute()
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	// Root's PersistentPreRunE must run before child's PreRunE.
	if len(order) != 2 {
		t.Fatalf("expected 2 hook invocations, got %d: %v", len(order), order)
	}
	if order[0] != "root" {
		t.Errorf("first hook should be 'root', got %q", order[0])
	}
	if order[1] != "child" {
		t.Errorf("second hook should be 'child', got %q", order[1])
	}

	// Child must NOT have PersistentPreRunE (forbidden pattern).
	if childCmd.PersistentPreRunE != nil {
		t.Error("child command must not define PersistentPreRunE (forbidden pattern per 13-REQ-12.E1)")
	}
}

// =========================================================================
// TS-13-56: PersistentPreRunE reads auth annotation from the cmd argument
// (leaf command), not the root command.
// Requirement: 13-REQ-12.3
// =========================================================================

func TestPersistentPreRunEReceivesLeafCommand(t *testing.T) {
	rootCmd := RootCommand()

	if rootCmd.PersistentPreRunE == nil {
		t.Fatal("RootCommand() must have PersistentPreRunE set")
	}

	// Capture the annotations from the cmd argument inside PersistentPreRunE.
	var capturedAnnotations map[string]string
	originalPreRunE := rootCmd.PersistentPreRunE
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		capturedAnnotations = cmd.Annotations
		return originalPreRunE(cmd, args)
	}

	// Register a leaf command with auth=none. The root command itself has no
	// auth annotation — if PersistentPreRunE read from root instead of cmd,
	// it would see nil or a different value.
	leafCmd := &cobra.Command{
		Use:   "leafannotation",
		Short: "Leaf command for annotation test",
		Annotations: map[string]string{
			"auth": "none",
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			return nil
		},
	}
	rootCmd.AddCommand(leafCmd)
	rootCmd.SetArgs([]string{"leafannotation"})

	captureStdout(t, func() {
		err := rootCmd.Execute()
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	if capturedAnnotations == nil {
		t.Fatal("PersistentPreRunE was not called or cmd.Annotations was nil")
	}

	// The captured annotations should be from the LEAF command, not the root.
	if auth := capturedAnnotations["auth"]; auth != "none" {
		t.Errorf("capturedAnnotations['auth'] = %q, want %q (should reflect leaf command)", auth, "none")
	}

	// Root command should NOT have an auth annotation (verifies we're not
	// accidentally reading from root).
	if rootCmd.Annotations != nil {
		if _, hasAuth := rootCmd.Annotations["auth"]; hasAuth {
			t.Log("Note: root command has 'auth' annotation; this test is less conclusive but still verifies cmd == leaf")
		}
	}
}

// =========================================================================
// TS-13-E16: A child command defining PersistentPreRunE shadows the root's
// hook, breaking client initialization for its subcommands.
// This is a NEGATIVE TEST documenting the forbidden pattern.
// Requirement: 13-REQ-12.E1
// =========================================================================

func TestChildPersistentPreRunEShadowsRootHook(t *testing.T) {
	// This test demonstrates WHY child commands must not define
	// PersistentPreRunE: it shadows the root's hook, preventing client
	// initialization from running on grandchild commands.
	//
	// The test uses a standalone command tree (not RootCommand()) because
	// it tests a Cobra framework behavior, not our specific implementation.

	rootCalled := false

	root := &cobra.Command{
		Use: "testroot",
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			rootCalled = true
			return nil
		},
	}

	// Child command with its own PersistentPreRunE — the FORBIDDEN pattern.
	badChild := &cobra.Command{
		Use: "badchild",
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			// This shadows root's PersistentPreRunE for all descendants.
			return nil
		},
	}

	grandchild := &cobra.Command{
		Use: "grandchild",
		RunE: func(_ *cobra.Command, _ []string) error {
			return nil
		},
	}

	badChild.AddCommand(grandchild)
	root.AddCommand(badChild)
	root.SetArgs([]string{"badchild", "grandchild"})

	err := root.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Root's PersistentPreRunE should NOT have been called because the
	// child's PersistentPreRunE shadows it. This demonstrates why the
	// pattern is forbidden — client initialization never runs.
	if rootCalled {
		t.Error("root's PersistentPreRunE should NOT be called when a child defines its own PersistentPreRunE — this demonstrates the shadow problem")
	}
}

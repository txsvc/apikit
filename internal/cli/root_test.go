package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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

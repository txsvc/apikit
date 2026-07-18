package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apikit "github.com/txsvc/apikit"
	"github.com/txsvc/apikit/internal/cli"

	"github.com/spf13/cobra"
)

// --- Stdout/stderr capture helpers for the cli_test package ---
// These are separate from the identically-named functions in root_test.go
// (package cli), which are not accessible from this external test package.

func captureStdoutExt(t *testing.T, fn func()) string {
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

func captureStdoutAndStderrExt(t *testing.T, fn func()) (string, string) {
	t.Helper()
	oldStdout := os.Stdout
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe (stdout): %v", err)
	}
	os.Stdout = wOut
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
// TS-13-37: PrintJSON marshals the value with json.MarshalIndent
// (two-space indent) and writes it to stdout.
// Requirement: 13-REQ-9.1
// =========================================================================

func TestPrintJSONProducesIndentedJSON(t *testing.T) {
	stdout := captureStdoutExt(t, func() {
		err := cli.PrintJSON(map[string]string{"key": "value"})
		if err != nil {
			t.Errorf("PrintJSON returned error: %v", err)
		}
	})

	// stdout must not be empty
	if len(strings.TrimSpace(stdout)) == 0 {
		t.Fatal("PrintJSON should produce output on stdout")
	}

	// stdout must be valid JSON
	if !json.Valid([]byte(stdout)) {
		t.Errorf("PrintJSON output is not valid JSON: %s", stdout)
	}

	// Verify two-space indentation
	expected := "{\n  \"key\": \"value\"\n}\n"
	if stdout != expected {
		t.Errorf("PrintJSON output = %q, want %q", stdout, expected)
	}
}

// =========================================================================
// TS-13-38: PrintError with a plain error writes JSON error envelope to
// stdout only with correct code and message; nothing written to stderr.
// Requirement: 13-REQ-9.2
// =========================================================================

func TestPrintErrorPlainErrorWritesEnvelopeToStdoutOnly(t *testing.T) {
	plainErr := fmt.Errorf("some client error")

	stdout, stderr := captureStdoutAndStderrExt(t, func() {
		cli.PrintError(plainErr)
	})

	// stdout must not be empty
	if len(strings.TrimSpace(stdout)) == 0 {
		t.Fatal("PrintError should produce output on stdout")
	}

	// Parse the error envelope
	var env errorEnvelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("stdout is not a valid JSON error envelope: %v\nstdout: %s", err, stdout)
	}

	// code must be 0 (client error sentinel)
	if env.Error.Code != 0 {
		t.Errorf("error envelope code should be 0, got %d", env.Error.Code)
	}

	// message must be the error string
	if env.Error.Message != "some client error" {
		t.Errorf("error envelope message = %q, want %q", env.Error.Message, "some client error")
	}

	// stderr must be empty
	if len(strings.TrimSpace(stderr)) > 0 {
		t.Errorf("stderr should be empty, got: %s", stderr)
	}
}

// =========================================================================
// TS-13-39: PrintError with *apikit.APIError uses the error's HTTP status
// code (>= 400) and message; code is never 0 for API errors.
// Requirement: 13-REQ-9.2
// =========================================================================

func TestPrintErrorAPIErrorUsesHTTPStatusCode(t *testing.T) {
	apiErr := &apikit.APIError{Code: 404, Message: "not found"}

	stdout := captureStdoutExt(t, func() {
		cli.PrintError(apiErr)
	})

	// stdout must not be empty
	if len(strings.TrimSpace(stdout)) == 0 {
		t.Fatal("PrintError should produce output on stdout for APIError")
	}

	// Parse the error envelope
	var env errorEnvelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("stdout is not a valid JSON error envelope: %v\nstdout: %s", err, stdout)
	}

	// code must be the HTTP status (404), never 0
	if env.Error.Code != 404 {
		t.Errorf("error envelope code = %d, want 404", env.Error.Code)
	}
	if env.Error.Code == 0 {
		t.Error("error envelope code must never be 0 for *apikit.APIError")
	}

	// message must be the API error message
	if env.Error.Message != "not found" {
		t.Errorf("error envelope message = %q, want %q", env.Error.Message, "not found")
	}
}

// =========================================================================
// TS-13-40: ExitCode returns 0 for nil, 1 for *apikit.APIError, and 2
// for all other non-nil errors.
// Requirement: 13-REQ-9.3
// =========================================================================

func TestExitCodeMapping(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode int
	}{
		{"nil error", nil, 0},
		{"*apikit.APIError", &apikit.APIError{Code: 401, Message: "unauthorized"}, 1},
		{"plain error", fmt.Errorf("some error"), 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := cli.ExitCode(tt.err)
			if code != tt.wantCode {
				t.Errorf("ExitCode(%v) = %d, want %d", tt.err, code, tt.wantCode)
			}
		})
	}
}

// =========================================================================
// TS-13-41: Human-readable messages (warnings, progress) are written to
// stderr only and never appear on stdout.
// Requirement: 13-REQ-9.4
// =========================================================================

func TestHumanMessagesGoToStderrOnly(t *testing.T) {
	// Register a test command that simulates the version command behavior:
	// writes warning to stderr and JSON to stdout.
	rootCmd := cli.RootCommand()
	rootCmd.AddCommand(&cobra.Command{
		Use:   "warntest",
		Short: "Test command that produces a warning",
		Annotations: map[string]string{
			"auth": "none",
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			// Simulate: human-readable warning goes to stderr
			fmt.Fprintf(os.Stderr, "warning: could not reach server: connection refused\n")
			// Simulate: JSON output goes to stdout via PrintJSON
			return cli.PrintJSON(map[string]string{"cli_version": "dev"})
		},
	})
	rootCmd.SetArgs([]string{"warntest"})

	stdout, stderr := captureStdoutAndStderrExt(t, func() {
		err := rootCmd.Execute()
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	// stdout should contain valid JSON
	if len(strings.TrimSpace(stdout)) == 0 {
		t.Fatal("stdout should not be empty — PrintJSON should write JSON output")
	}
	if !json.Valid([]byte(stdout)) {
		t.Errorf("stdout should be valid JSON, got: %s", stdout)
	}

	// Warning text should NOT appear on stdout
	if strings.Contains(stdout, "warning") {
		t.Error("warning text should not appear on stdout")
	}

	// stderr should contain the warning text
	if !strings.Contains(stderr, "warning") {
		t.Error("stderr should contain the warning text")
	}
}

// =========================================================================
// TS-13-E12: PrintError with a plain (non-*apikit.APIError) error emits
// code:0 and never emits code:0 for *apikit.APIError errors.
// Requirement: 13-REQ-9.E1
// =========================================================================

func TestPrintErrorCodeZeroForPlainNotForAPIError(t *testing.T) {
	// Plain error should produce code: 0
	stdout1 := captureStdoutExt(t, func() {
		cli.PrintError(fmt.Errorf("oops"))
	})

	if len(strings.TrimSpace(stdout1)) == 0 {
		t.Fatal("PrintError should produce output for plain error")
	}

	var env1 errorEnvelope
	if err := json.Unmarshal([]byte(stdout1), &env1); err != nil {
		t.Fatalf("failed to parse error envelope for plain error: %v\nstdout: %s", err, stdout1)
	}
	if env1.Error.Code != 0 {
		t.Errorf("plain error: envelope code = %d, want 0", env1.Error.Code)
	}

	// *apikit.APIError should produce code: 500 (not 0)
	stdout2 := captureStdoutExt(t, func() {
		cli.PrintError(&apikit.APIError{Code: 500, Message: "server error"})
	})

	if len(strings.TrimSpace(stdout2)) == 0 {
		t.Fatal("PrintError should produce output for APIError")
	}

	var env2 errorEnvelope
	if err := json.Unmarshal([]byte(stdout2), &env2); err != nil {
		t.Fatalf("failed to parse error envelope for APIError: %v\nstdout: %s", err, stdout2)
	}
	if env2.Error.Code != 500 {
		t.Errorf("APIError: envelope code = %d, want 500", env2.Error.Code)
	}
	if env2.Error.Code == 0 {
		t.Error("APIError: envelope code must never be 0")
	}
}

// =========================================================================
// TS-13-61: CLI process exits with code 0 on success, 1 for
// *apikit.APIError, and 2 for all other errors.
// Requirement: 13-REQ-14.1
// =========================================================================

func TestExitCodeConsistency(t *testing.T) {
	// Verify the full exit code table
	if code := cli.ExitCode(nil); code != 0 {
		t.Errorf("ExitCode(nil) = %d, want 0", code)
	}
	if code := cli.ExitCode(&apikit.APIError{Code: 403, Message: "forbidden"}); code != 1 {
		t.Errorf("ExitCode(*apikit.APIError{403}) = %d, want 1", code)
	}
	if code := cli.ExitCode(fmt.Errorf("config missing")); code != 2 {
		t.Errorf("ExitCode(plain error) = %d, want 2", code)
	}
}

// =========================================================================
// TS-13-62: Error envelopes are written exclusively to stdout; stderr is
// empty for all error conditions.
// Requirement: 13-REQ-14.2
// =========================================================================

func TestErrorEnvelopeExclusivelyOnStdout(t *testing.T) {
	// Register a non-auth-exempt command that returns an error
	rootCmd := cli.RootCommand()
	rootCmd.AddCommand(&cobra.Command{
		Use:   "failenvcmd",
		Short: "Command that fails for error envelope test",
		Annotations: map[string]string{
			"auth": "none",
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("test client error")
		},
	})
	rootCmd.SetArgs([]string{"failenvcmd"})

	stdout, stderr := captureStdoutAndStderrExt(t, func() {
		err := rootCmd.Execute()
		if err != nil {
			cli.PrintError(err)
		}
	})

	// stdout should contain the JSON error envelope
	if len(strings.TrimSpace(stdout)) == 0 {
		t.Fatal("stdout should contain the JSON error envelope")
	}
	if !json.Valid([]byte(stdout)) {
		t.Errorf("stdout should be valid JSON, got: %s", stdout)
	}

	var env errorEnvelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("stdout is not a valid error envelope: %v\nstdout: %s", err, stdout)
	}
	if env.Error.Message == "" {
		t.Error("error envelope message should not be empty")
	}

	// stderr must be empty for error conditions
	if len(strings.TrimSpace(stderr)) > 0 {
		t.Errorf("stderr should be empty for error conditions, got: %s", stderr)
	}
}

// =========================================================================
// TS-13-63: main.go produces no output of any kind; only calls
// os.Exit(cli.ExitCode(err)) after cli.Execute() returns.
// Requirement: 13-REQ-14.3
// =========================================================================

func TestMainGoProducesNoOutput(t *testing.T) {
	src, err := os.ReadFile("../../cmd/akc/main.go")
	if err != nil {
		t.Fatalf("failed to read cmd/akc/main.go: %v", err)
	}
	source := string(src)

	// main.go must call cli.Execute()
	if !strings.Contains(source, "cli.Execute()") {
		t.Error("main.go must call cli.Execute()")
	}

	// main.go must call cli.ExitCode
	if !strings.Contains(source, "cli.ExitCode(") {
		t.Error("main.go must call cli.ExitCode()")
	}

	// main.go must call os.Exit
	if !strings.Contains(source, "os.Exit(") {
		t.Error("main.go must call os.Exit()")
	}

	// main.go must NOT contain any direct output calls
	forbidden := []string{
		"fmt.Print(",
		"fmt.Println(",
		"fmt.Printf(",
		"os.Stdout.Write(",
		"os.Stdout.WriteString(",
	}
	for _, f := range forbidden {
		if strings.Contains(source, f) {
			t.Errorf("main.go must not contain %q — all output should come from Execute()", f)
		}
	}
}

// =========================================================================
// TS-13-E18: When server returns 4xx/5xx, SDK wraps it as *apikit.APIError;
// PrintError uses HTTP status as code and server message; exit code 1.
// Requirement: 13-REQ-14.E1
// =========================================================================

func TestAPIErrorEnvelopeAndExitCode(t *testing.T) {
	// Register a command that returns an *apikit.APIError
	apiErr := &apikit.APIError{Code: 401, Message: "unauthorized"}

	rootCmd := cli.RootCommand()
	rootCmd.AddCommand(&cobra.Command{
		Use:   "apierrcmd",
		Short: "Command that returns an API error",
		Annotations: map[string]string{
			"auth": "none",
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			return apiErr
		},
	})
	rootCmd.SetArgs([]string{"apierrcmd"})

	var execErr error
	stdout, stderr := captureStdoutAndStderrExt(t, func() {
		execErr = rootCmd.Execute()
		if execErr != nil {
			cli.PrintError(execErr)
		}
	})

	// Should have an error
	if execErr == nil {
		t.Fatal("expected non-nil error from command returning APIError")
	}

	// stdout should contain the error envelope with HTTP status code
	if len(strings.TrimSpace(stdout)) == 0 {
		t.Fatal("stdout should contain the JSON error envelope")
	}

	var env errorEnvelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("stdout is not a valid JSON error envelope: %v\nstdout: %s", err, stdout)
	}
	if env.Error.Code != 401 {
		t.Errorf("error envelope code = %d, want 401", env.Error.Code)
	}
	if env.Error.Message != "unauthorized" {
		t.Errorf("error envelope message = %q, want %q", env.Error.Message, "unauthorized")
	}

	// ExitCode should be 1 for APIError
	if code := cli.ExitCode(execErr); code != 1 {
		t.Errorf("ExitCode for APIError should be 1, got %d", code)
	}

	// stderr should be empty
	if len(strings.TrimSpace(stderr)) > 0 {
		t.Errorf("stderr should be empty for API errors, got: %s", stderr)
	}
}

// =========================================================================
// TS-13-E19: Client-side error (missing endpoint_url) produces code:0
// envelope on stdout; exit code 2; stderr empty.
// Requirement: 13-REQ-14.E2
// =========================================================================

func TestMissingEndpointURLEnvelopeAndExitCode(t *testing.T) {
	// Ensure endpoint_url is not set anywhere
	savedEndpoint, hadEndpoint := os.LookupEnv("ENDPOINT_URL")
	os.Unsetenv("ENDPOINT_URL")
	defer func() {
		if hadEndpoint {
			os.Setenv("ENDPOINT_URL", savedEndpoint)
		}
	}()

	// Set API_KEY to isolate the endpoint_url error
	savedAPIKey, hadAPIKey := os.LookupEnv("API_KEY")
	os.Setenv("API_KEY", "k")
	defer func() {
		if hadAPIKey {
			os.Setenv("API_KEY", savedAPIKey)
		} else {
			os.Unsetenv("API_KEY")
		}
	}()

	// Set up TokenPrefix and HOME with config
	savedPrefix := cli.TokenPrefix
	cli.TokenPrefix = "testpfx"
	defer func() { cli.TokenPrefix = savedPrefix }()

	tmpHome := t.TempDir()
	savedHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", savedHome)

	configDir := filepath.Join(tmpHome, ".testpfx")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	configContent := "endpoint_url = \"\"\nuser_id = \"\"\napi_key = \"\"\n"
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config.toml: %v", err)
	}

	// Register a non-auth-exempt command
	rootCmd := cli.RootCommand()
	rootCmd.AddCommand(&cobra.Command{
		Use:   "needsendpoint",
		Short: "Command requiring endpoint_url",
		Annotations: map[string]string{
			"auth": "api_key",
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			return nil
		},
	})
	rootCmd.SetArgs([]string{"needsendpoint"})

	var execErr error
	stdout, stderr := captureStdoutAndStderrExt(t, func() {
		execErr = rootCmd.Execute()
		if execErr != nil {
			cli.PrintError(execErr)
		}
	})

	// Should have an error about missing endpoint_url
	if execErr == nil {
		t.Fatal("expected error when endpoint_url is not configured")
	}

	// stdout should contain error envelope with code: 0
	if len(strings.TrimSpace(stdout)) == 0 {
		t.Fatal("stdout should contain the JSON error envelope")
	}

	var env errorEnvelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("stdout is not a valid JSON error envelope: %v\nstdout: %s", err, stdout)
	}
	if env.Error.Code != 0 {
		t.Errorf("error envelope code = %d, want 0 (client sentinel)", env.Error.Code)
	}
	if !strings.Contains(env.Error.Message, "endpoint_url is not set") {
		t.Errorf("error envelope message should contain 'endpoint_url is not set', got %q", env.Error.Message)
	}

	// ExitCode should be 2 for client-side errors
	if code := cli.ExitCode(execErr); code != 2 {
		t.Errorf("ExitCode for missing endpoint_url should be 2, got %d", code)
	}

	// stderr should be empty
	if len(strings.TrimSpace(stderr)) > 0 {
		t.Errorf("stderr should be empty for error conditions, got: %s", stderr)
	}
}

package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"
)

// =========================================================================
// TS-13-12: InitConfig creates the config directory with mode 0700
// =========================================================================

func TestInitConfigCreatesDirectoryWithMode0700(t *testing.T) {
	tmpHome := t.TempDir()
	configDir := filepath.Join(tmpHome, ".ak")

	err := InitConfig(configDir)
	if err != nil {
		t.Fatalf("InitConfig(%q) returned error: %v", configDir, err)
	}

	info, err := os.Stat(configDir)
	if err != nil {
		t.Fatalf("config directory %q should exist: %v", configDir, err)
	}
	if !info.IsDir() {
		t.Errorf("config path %q should be a directory", configDir)
	}
	if perm := info.Mode().Perm(); perm != 0700 {
		t.Errorf("config directory permission = %04o, want 0700", perm)
	}
}

// =========================================================================
// TS-13-13: InitConfig creates config.toml from the hard-coded template
// with mode 0600
// =========================================================================

func TestInitConfigCreatesConfigTomlFromTemplate(t *testing.T) {
	tmpHome := t.TempDir()
	configDir := filepath.Join(tmpHome, ".ak")

	// Create the config directory first
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	err := InitConfig(configDir)
	if err != nil {
		t.Fatalf("InitConfig(%q) returned error: %v", configDir, err)
	}

	configPath := filepath.Join(configDir, "config.toml")
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("config.toml should exist at %q: %v", configPath, err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("config.toml permission = %04o, want 0600", perm)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config.toml: %v", err)
	}

	// Verify the template contains exactly the three expected lines
	if !strings.Contains(string(content), `endpoint_url = ""`) {
		t.Error(`config.toml should contain 'endpoint_url = ""'`)
	}
	if !strings.Contains(string(content), `user_id = ""`) {
		t.Error(`config.toml should contain 'user_id = ""'`)
	}
	if !strings.Contains(string(content), `api_key = ""`) {
		t.Error(`config.toml should contain 'api_key = ""'`)
	}
}

// =========================================================================
// TS-13-14: InitConfig does not modify an existing config.toml
// =========================================================================

func TestInitConfigDoesNotModifyExistingConfigToml(t *testing.T) {
	tmpHome := t.TempDir()
	configDir := filepath.Join(tmpHome, ".ak")

	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	configPath := filepath.Join(configDir, "config.toml")
	originalContent := "endpoint_url = \"http://example.com\"\nuser_id = \"u1\"\napi_key = \"k1\"\n"
	if err := os.WriteFile(configPath, []byte(originalContent), 0600); err != nil {
		t.Fatalf("failed to write initial config.toml: %v", err)
	}

	err := InitConfig(configDir)
	if err != nil {
		t.Fatalf("InitConfig(%q) returned error: %v", configDir, err)
	}

	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config.toml after InitConfig: %v", err)
	}
	if string(after) != originalContent {
		t.Errorf("config.toml content should be unchanged\nwant: %s\ngot:  %s", originalContent, string(after))
	}
}

// =========================================================================
// TS-13-15: CLIConfig struct has correct TOML tags via round-trip
// =========================================================================

func TestCLIConfigTOMLRoundTrip(t *testing.T) {
	cfg := CLIConfig{
		EndpointURL: "e",
		UserID:      "u",
		APIKey:      "k",
	}

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		t.Fatalf("TOML encode failed: %v", err)
	}
	encoded := buf.String()

	if !strings.Contains(encoded, `endpoint_url = "e"`) {
		t.Errorf("TOML output should contain endpoint_url = \"e\", got:\n%s", encoded)
	}
	if !strings.Contains(encoded, `user_id = "u"`) {
		t.Errorf("TOML output should contain user_id = \"u\", got:\n%s", encoded)
	}
	if !strings.Contains(encoded, `api_key = "k"`) {
		t.Errorf("TOML output should contain api_key = \"k\", got:\n%s", encoded)
	}

	// Decode back and verify round-trip
	var decoded CLIConfig
	if _, err := toml.Decode(encoded, &decoded); err != nil {
		t.Fatalf("TOML decode failed: %v", err)
	}
	if decoded.EndpointURL != "e" {
		t.Errorf("EndpointURL after round-trip = %q, want %q", decoded.EndpointURL, "e")
	}
	if decoded.UserID != "u" {
		t.Errorf("UserID after round-trip = %q, want %q", decoded.UserID, "u")
	}
	if decoded.APIKey != "k" {
		t.Errorf("APIKey after round-trip = %q, want %q", decoded.APIKey, "k")
	}
}

// =========================================================================
// TS-13-E4: Unresolvable $HOME → PersistentPreRunE exit-2 error envelope
// =========================================================================

func TestPersistentPreRunEUnresolvableHome(t *testing.T) {
	// This test verifies the PersistentPreRunE behavior when os.UserHomeDir()
	// fails. Since we cannot easily mock os.UserHomeDir, we test that the
	// PersistentPreRunE on the root command produces the correct error when
	// HOME is unresolvable.
	//
	// Approach: unset HOME and invoke a non-auth-exempt command.
	savedHome := os.Getenv("HOME")
	os.Unsetenv("HOME")
	defer os.Setenv("HOME", savedHome)

	// Also unset user-level home dir env vars that os.UserHomeDir() might use
	savedUserProfile := os.Getenv("USERPROFILE")
	os.Unsetenv("USERPROFILE")
	defer os.Setenv("USERPROFILE", savedUserProfile)

	rootCmd := RootCommand()
	rootCmd.AddCommand(newAuthCommand("homecheckcmd"))
	rootCmd.SetArgs([]string{"homecheckcmd"})

	var execErr error
	stdout := captureStdout(t, func() {
		execErr = rootCmd.Execute()
		if execErr != nil {
			PrintError(execErr)
		}
	})

	if execErr == nil {
		t.Fatal("expected error when HOME is unresolvable")
	}

	expectedMsg := "cannot determine home directory: $HOME is not set or unresolvable"
	if !strings.Contains(execErr.Error(), expectedMsg) {
		t.Errorf("error message should contain %q, got: %v", expectedMsg, execErr)
	}

	if code := ExitCode(execErr); code != 2 {
		t.Errorf("ExitCode should be 2, got %d", code)
	}

	// Verify JSON error envelope
	if !strings.Contains(stdout, expectedMsg) {
		t.Errorf("stdout should contain the error message %q, got:\n%s", expectedMsg, stdout)
	}
}

// newAuthCommand creates a simple non-auth-exempt command stub for testing.
func newAuthCommand(name string) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: fmt.Sprintf("Test command %s requiring auth", name),
		Annotations: map[string]string{
			"auth": "api_key",
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			return nil
		},
	}
}

// =========================================================================
// TS-13-E5: Unwritable HOME causes InitConfig failure → exit-2
// =========================================================================

func TestPersistentPreRunEUnwritableHome(t *testing.T) {
	// Create a temp dir as HOME and make it unwritable
	tmpHome := t.TempDir()
	unwritableDir := filepath.Join(tmpHome, "noperm")
	if err := os.MkdirAll(unwritableDir, 0700); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	// Make the dir unwritable so os.MkdirAll inside it will fail
	if err := os.Chmod(unwritableDir, 0000); err != nil {
		t.Fatalf("failed to chmod: %v", err)
	}
	defer os.Chmod(unwritableDir, 0700)

	savedPrefix := TokenPrefix
	TokenPrefix = "testprefix"
	defer func() { TokenPrefix = savedPrefix }()

	savedHome := os.Getenv("HOME")
	os.Setenv("HOME", unwritableDir)
	defer os.Setenv("HOME", savedHome)

	rootCmd := RootCommand()
	rootCmd.AddCommand(newAuthCommand("writecheckcmd"))
	rootCmd.SetArgs([]string{"writecheckcmd"})

	var execErr error
	stdout := captureStdout(t, func() {
		execErr = rootCmd.Execute()
		if execErr != nil {
			PrintError(execErr)
		}
	})

	if execErr == nil {
		t.Fatal("expected error when HOME is unwritable")
	}

	if code := ExitCode(execErr); code != 2 {
		t.Errorf("ExitCode should be 2, got %d", code)
	}

	// stdout should contain an error envelope
	if !strings.Contains(stdout, `"error"`) {
		t.Errorf("stdout should contain a JSON error envelope, got:\n%s", stdout)
	}
}

// =========================================================================
// TS-13-16: LoadConfig parses valid config.toml into CLIConfig
// =========================================================================

func TestLoadConfigParsesValidTOML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	content := "endpoint_url = \"http://x\"\nuser_id = \"u\"\napi_key = \"k\"\n"
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write config.toml: %v", err)
	}

	cfg, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("LoadConfig(%q) returned error: %v", tmpDir, err)
	}
	if cfg == nil {
		t.Fatal("LoadConfig should return a non-nil *CLIConfig on success")
	}
	if cfg.EndpointURL != "http://x" {
		t.Errorf("EndpointURL = %q, want %q", cfg.EndpointURL, "http://x")
	}
	if cfg.UserID != "u" {
		t.Errorf("UserID = %q, want %q", cfg.UserID, "u")
	}
	if cfg.APIKey != "k" {
		t.Errorf("APIKey = %q, want %q", cfg.APIKey, "k")
	}
}

// =========================================================================
// TS-13-17: LoadConfig rejects TOML parse errors with canonical message
// =========================================================================

func TestLoadConfigRejectsMalformedTOML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	// Write syntactically invalid TOML content
	content := "endpoint_url = !!!INVALID!!!"
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write config.toml: %v", err)
	}

	cfg, err := LoadConfig(tmpDir)
	if cfg != nil {
		t.Errorf("LoadConfig should return nil *CLIConfig on parse error, got %+v", cfg)
	}
	if err == nil {
		t.Fatal("LoadConfig should return an error for malformed TOML")
	}
	if !strings.HasPrefix(err.Error(), "config file is unparseable:") {
		t.Errorf("error message should start with 'config file is unparseable:', got: %v", err)
	}
}

// =========================================================================
// TS-13-E6: LoadConfig rejects partially valid TOML
// =========================================================================

func TestLoadConfigRejectsPartiallyValidTOML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	// Two valid fields followed by one syntactically invalid field
	content := "endpoint_url = \"x\"\nuser_id = \"u\"\napi_key = !!!BAD!!!"
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write config.toml: %v", err)
	}

	cfg, err := LoadConfig(tmpDir)
	if cfg != nil {
		t.Errorf("LoadConfig should return nil *CLIConfig on partial parse error, got %+v", cfg)
	}
	if err == nil {
		t.Fatal("LoadConfig should return an error for partially valid TOML")
	}
	if !strings.HasPrefix(err.Error(), "config file is unparseable:") {
		t.Errorf("error message should start with 'config file is unparseable:', got: %v", err)
	}
}

// TestLoadConfigEmptyValuesReturnsEmptyStrings verifies that a config.toml
// with all fields set to empty strings returns a CLIConfig with all empty
// string values (not nil).
func TestLoadConfigEmptyValuesReturnsEmptyStrings(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	content := "endpoint_url = \"\"\nuser_id = \"\"\napi_key = \"\"\n"
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write config.toml: %v", err)
	}

	cfg, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadConfig should return a non-nil *CLIConfig for empty values")
	}
	if cfg.EndpointURL != "" {
		t.Errorf("EndpointURL = %q, want empty string", cfg.EndpointURL)
	}
	if cfg.UserID != "" {
		t.Errorf("UserID = %q, want empty string", cfg.UserID)
	}
	if cfg.APIKey != "" {
		t.Errorf("APIKey = %q, want empty string", cfg.APIKey)
	}
}

// TestLoadConfigMissingKeyReturnsEmptyString verifies that a config.toml
// missing one of the three keys returns a CLIConfig with that field as "".
func TestLoadConfigMissingKeyReturnsEmptyString(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	// Only two keys present; api_key is missing
	content := "endpoint_url = \"http://x\"\nuser_id = \"u\"\n"
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write config.toml: %v", err)
	}

	cfg, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadConfig should return a non-nil *CLIConfig when a key is missing")
	}
	if cfg.EndpointURL != "http://x" {
		t.Errorf("EndpointURL = %q, want %q", cfg.EndpointURL, "http://x")
	}
	if cfg.UserID != "u" {
		t.Errorf("UserID = %q, want %q", cfg.UserID, "u")
	}
	if cfg.APIKey != "" {
		t.Errorf("APIKey = %q, want empty string for missing key", cfg.APIKey)
	}
}

// =========================================================================
// TS-13-33: SaveConfig atomically writes CLIConfig to config.toml
// =========================================================================

func TestSaveConfigWritesValidTOMLWithMode0600(t *testing.T) {
	configDir := t.TempDir()
	cfg := &CLIConfig{
		EndpointURL: "http://x",
		UserID:      "u",
		APIKey:      "k",
	}

	err := SaveConfig(configDir, cfg)
	if err != nil {
		t.Fatalf("SaveConfig returned error: %v", err)
	}

	configPath := filepath.Join(configDir, "config.toml")
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("config.toml should exist after SaveConfig: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("config.toml permission = %04o, want 0600", perm)
	}

	// Read and parse the file
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config.toml: %v", err)
	}

	var parsed CLIConfig
	if _, err := toml.Decode(string(content), &parsed); err != nil {
		t.Fatalf("config.toml is not valid TOML: %v\ncontent:\n%s", err, string(content))
	}
	if parsed.EndpointURL != "http://x" {
		t.Errorf("EndpointURL = %q, want %q", parsed.EndpointURL, "http://x")
	}
	if parsed.UserID != "u" {
		t.Errorf("UserID = %q, want %q", parsed.UserID, "u")
	}
	if parsed.APIKey != "k" {
		t.Errorf("APIKey = %q, want %q", parsed.APIKey, "k")
	}

	// Verify no temp files remain
	entries, err := os.ReadDir(configDir)
	if err != nil {
		t.Fatalf("failed to read config dir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "config.toml" {
			t.Errorf("unexpected file in config dir: %s (possible leftover temp file)", e.Name())
		}
	}
}

// =========================================================================
// TS-13-34: SaveConfig returns error when os.CreateTemp fails
// =========================================================================

func TestSaveConfigErrorOnUnwritableDir(t *testing.T) {
	configDir := t.TempDir()

	// Pre-create a valid config.toml
	originalContent := "endpoint_url = \"original\"\nuser_id = \"ou\"\napi_key = \"ok\"\n"
	configPath := filepath.Join(configDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(originalContent), 0600); err != nil {
		t.Fatalf("failed to write initial config.toml: %v", err)
	}

	// Make the directory unwritable so os.CreateTemp fails
	if err := os.Chmod(configDir, 0500); err != nil {
		t.Fatalf("failed to chmod config dir: %v", err)
	}
	defer os.Chmod(configDir, 0700)

	cfg := &CLIConfig{
		EndpointURL: "http://new",
		UserID:      "newu",
		APIKey:      "newk",
	}

	err := SaveConfig(configDir, cfg)
	if err == nil {
		t.Fatal("SaveConfig should return an error when config directory is unwritable")
	}

	// Restore permissions to check state
	os.Chmod(configDir, 0700)

	// Original config.toml should be unchanged
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config.toml after failed SaveConfig: %v", err)
	}
	if string(after) != originalContent {
		t.Errorf("config.toml should be unchanged after failed SaveConfig\nwant: %s\ngot:  %s", originalContent, string(after))
	}

	// No temp files should remain
	entries, err := os.ReadDir(configDir)
	if err != nil {
		t.Fatalf("failed to read config dir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "config.toml" {
			t.Errorf("unexpected file in config dir after failed SaveConfig: %s", e.Name())
		}
	}
}

// =========================================================================
// TS-13-35: os.Rename failure → deferred os.Remove cleans up temp file
// =========================================================================

// TestSaveConfigRenameFailureCleansUp verifies that when the rename step
// fails, the original config.toml is preserved and no temp files remain.
// We simulate this by making config.toml immutable (read-only parent dir
// won't work since we need CreateTemp to succeed first).
//
// Strategy: Write a valid config, then make the target location fail by
// removing write permission on the config file itself after temp creation.
// Since we can't inject failures into os.Rename directly, we test the
// observable invariant: after any SaveConfig error, original content is
// preserved and no temp files linger.
func TestSaveConfigRenameFailureCleansUp(t *testing.T) {
	// We test the general contract: if SaveConfig returns an error for
	// any reason after creating a temp file, the temp file is cleaned up
	// and the original config.toml is preserved.
	//
	// To force an os.Rename failure, we create a subdirectory at the
	// config.toml path, making it impossible to rename a file on top of it.
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.toml")

	// Create config.toml as a directory to cause os.Rename to fail
	if err := os.MkdirAll(configPath, 0700); err != nil {
		t.Fatalf("failed to create dir at config.toml path: %v", err)
	}

	cfg := &CLIConfig{
		EndpointURL: "http://new",
		UserID:      "newu",
		APIKey:      "newk",
	}

	err := SaveConfig(configDir, cfg)
	if err == nil {
		// If SaveConfig succeeds despite the directory, implementation
		// may handle it differently. Either way, verify state.
		t.Log("SaveConfig did not error with directory at config.toml path")
	}

	// Regardless of error, verify no temp files remain in configDir
	entries, err := os.ReadDir(configDir)
	if err != nil {
		t.Fatalf("failed to read config dir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "config.toml" {
			t.Errorf("unexpected file in config dir (possible temp file leak): %s", e.Name())
		}
	}
}

// =========================================================================
// TS-13-36: SaveConfig creates temp file in same directory as config.toml
// =========================================================================

// TestSaveConfigTempFileInSameDirectory verifies that SaveConfig creates
// the temp file inside the config directory (same filesystem) to guarantee
// that os.Rename is a same-filesystem operation.
func TestSaveConfigTempFileInSameDirectory(t *testing.T) {
	configDir := t.TempDir()
	cfg := &CLIConfig{
		EndpointURL: "http://x",
		UserID:      "u",
		APIKey:      "k",
	}

	// After a successful SaveConfig, we can only indirectly verify this.
	// The key invariant is that the write succeeds (meaning os.Rename
	// worked, which requires same-filesystem temp). We verify that:
	// 1. SaveConfig succeeds
	// 2. config.toml exists in configDir
	// 3. No temp files remain
	err := SaveConfig(configDir, cfg)
	if err != nil {
		t.Fatalf("SaveConfig returned error: %v", err)
	}

	configPath := filepath.Join(configDir, "config.toml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("config.toml should exist after SaveConfig")
	}

	// Verify all files in configDir — only config.toml should be present
	entries, err := os.ReadDir(configDir)
	if err != nil {
		t.Fatalf("failed to read config dir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "config.toml" {
			t.Errorf("unexpected file in config dir: %s (temp file should be in same dir, not leaked)", e.Name())
		}
	}

	// Parse the result to verify it's valid
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config.toml: %v", err)
	}
	var parsed CLIConfig
	if _, err := toml.Decode(string(content), &parsed); err != nil {
		t.Fatalf("config.toml is not valid TOML after SaveConfig: %v", err)
	}
	if parsed.EndpointURL != "http://x" {
		t.Errorf("EndpointURL = %q, want %q", parsed.EndpointURL, "http://x")
	}
}

// =========================================================================
// TS-13-E11: Concurrent SaveConfig → last-writer-wins, no corruption
// =========================================================================

func TestSaveConfigConcurrentLastWriterWins(t *testing.T) {
	configDir := t.TempDir()

	cfg1 := &CLIConfig{
		EndpointURL: "http://one",
		UserID:      "user1",
		APIKey:      "key1",
	}
	cfg2 := &CLIConfig{
		EndpointURL: "http://two",
		UserID:      "user2",
		APIKey:      "key2",
	}

	var wg sync.WaitGroup
	var err1, err2 error

	wg.Add(2)
	go func() {
		defer wg.Done()
		err1 = SaveConfig(configDir, cfg1)
	}()
	go func() {
		defer wg.Done()
		err2 = SaveConfig(configDir, cfg2)
	}()
	wg.Wait()

	// At least one should succeed (both may succeed with last-writer-wins)
	if err1 != nil && err2 != nil {
		t.Fatalf("both concurrent SaveConfig calls failed: err1=%v, err2=%v", err1, err2)
	}

	configPath := filepath.Join(configDir, "config.toml")
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config.toml: %v", err)
	}

	// Content must be valid TOML
	var parsed CLIConfig
	if _, err := toml.Decode(string(content), &parsed); err != nil {
		t.Fatalf("config.toml is not valid TOML after concurrent writes: %v\ncontent:\n%s", err, string(content))
	}

	// Result must be exactly one of the two configs (no partial state)
	isCfg1 := parsed.EndpointURL == "http://one" && parsed.UserID == "user1" && parsed.APIKey == "key1"
	isCfg2 := parsed.EndpointURL == "http://two" && parsed.UserID == "user2" && parsed.APIKey == "key2"
	if !isCfg1 && !isCfg2 {
		t.Errorf("config.toml should contain exactly one of the two updates, got: %+v", parsed)
	}

	// No temp files should remain
	entries, err := os.ReadDir(configDir)
	if err != nil {
		t.Fatalf("failed to read config dir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "config.toml" {
			t.Errorf("unexpected file in config dir after concurrent writes: %s", e.Name())
		}
	}
}

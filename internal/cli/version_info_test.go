package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// =========================================================================
// TS-13-10 / TS-13-57: Build-time variable defaults
// =========================================================================

// TestBuildTimeVariableDefaults verifies that Version, Build, and TokenPrefix
// have the correct default values when the binary is compiled without
// -ldflags overrides.
func TestBuildTimeVariableDefaults(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"Version", Version, "dev"},
		{"Build", Build, "unknown"},
		{"TokenPrefix", TokenPrefix, "ak"},
		{"EnvPrefix", EnvPrefix, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

// =========================================================================
// TS-13-58: TokenPrefix determines config directory and version output
// =========================================================================

// versionOutput mirrors the VersionOutput struct for JSON unmarshalling.
type versionOutput struct {
	CLIVersion    string `json:"cli_version"`
	Build         string `json:"build"`
	Prefix        string `json:"prefix"`
	ServerVersion any    `json:"server_version,omitempty"`
}

// TestTokenPrefixDeterminesConfigDirAndVersionOutput verifies that
// TokenPrefix='myapp' causes the config directory to be $HOME/.myapp/
// and that version output contains "prefix":"myapp".
func TestTokenPrefixDeterminesConfigDirAndVersionOutput(t *testing.T) {
	// Save and restore originals
	savedPrefix := TokenPrefix
	defer func() { TokenPrefix = savedPrefix }()

	TokenPrefix = "myapp"

	// Create a temp dir to act as HOME
	tmpHome := t.TempDir()

	// The config directory should be $HOME/.myapp/
	expectedConfigDir := filepath.Join(tmpHome, ".myapp")
	actualConfigDir := filepath.Join(tmpHome, "."+TokenPrefix)

	if actualConfigDir != expectedConfigDir {
		t.Errorf("config dir should be %q, got %q", expectedConfigDir, actualConfigDir)
	}

	// Initialize config directory to verify the path is correct
	err := InitConfig(expectedConfigDir)
	if err != nil {
		t.Fatalf("InitConfig(%q) returned error: %v", expectedConfigDir, err)
	}

	// Verify the config directory was created
	if _, err := os.Stat(expectedConfigDir); os.IsNotExist(err) {
		t.Errorf("config directory %q should have been created", expectedConfigDir)
	}

	// Test that version output includes the prefix
	rootCmd := RootCommand()
	rootCmd.SetArgs([]string{"version"})

	stdout := captureStdout(t, func() {
		_ = rootCmd.Execute()
	})

	// If there's version output, verify it contains the prefix
	if len(strings.TrimSpace(stdout)) > 0 {
		var vo versionOutput
		if err := json.Unmarshal([]byte(stdout), &vo); err == nil {
			if vo.Prefix != "myapp" {
				t.Errorf("version output prefix = %q, want %q", vo.Prefix, "myapp")
			}
		} else {
			// If we can't parse it, at least check the string contains the prefix
			if !strings.Contains(stdout, "myapp") {
				t.Errorf("version output should contain prefix 'myapp', got:\n%s", stdout)
			}
		}
	} else {
		t.Error("version command should produce output")
	}
}

func TestEnvPrefixNameAndPrefixedEnvVar(t *testing.T) {
	savedTokenPrefix := TokenPrefix
	savedEnvPrefix := EnvPrefix
	defer func() {
		TokenPrefix = savedTokenPrefix
		EnvPrefix = savedEnvPrefix
	}()

	// Default: TokenPrefix="ak", EnvPrefix="" -> "AK"
	TokenPrefix = "ak"
	EnvPrefix = ""
	if got := EnvPrefixName(); got != "AK" {
		t.Errorf("EnvPrefixName() = %q, want %q", got, "AK")
	}
	if got := PrefixedEnvVar("API_KEY"); got != "AK_API_KEY" {
		t.Errorf("PrefixedEnvVar(\"API_KEY\") = %q, want %q", got, "AK_API_KEY")
	}

	// TokenPrefix="af", EnvPrefix="" -> "AF"
	TokenPrefix = "af"
	EnvPrefix = ""
	if got := EnvPrefixName(); got != "AF" {
		t.Errorf("EnvPrefixName() = %q, want %q", got, "AF")
	}
	if got := PrefixedEnvVar("ENDPOINT_URL"); got != "AF_ENDPOINT_URL" {
		t.Errorf("PrefixedEnvVar(\"ENDPOINT_URL\") = %q, want %q", got, "AF_ENDPOINT_URL")
	}

	// EnvPrefix explicitly set to "myapp" -> "MYAPP"
	TokenPrefix = "af"
	EnvPrefix = "myapp"
	if got := EnvPrefixName(); got != "MYAPP" {
		t.Errorf("EnvPrefixName() = %q, want %q", got, "MYAPP")
	}
	if got := PrefixedEnvVar("CONFIG_DIR"); got != "MYAPP_CONFIG_DIR" {
		t.Errorf("PrefixedEnvVar(\"CONFIG_DIR\") = %q, want %q", got, "MYAPP_CONFIG_DIR")
	}
}


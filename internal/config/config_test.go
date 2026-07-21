package config_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/txsvc/apikit/internal/config"
)

// ========================================================================
// Helpers
// ========================================================================

// mustUnsetenv unsets an environment variable and restores it on cleanup.
func mustUnsetenv(t *testing.T, key string) {
	t.Helper()
	if val, ok := os.LookupEnv(key); ok {
		t.Cleanup(func() { os.Setenv(key, val) })
	}
	os.Unsetenv(key)
}

// clearXDGVars ensures XDG_CONFIG_HOME and XDG_DATA_HOME are not set.
func clearXDGVars(t *testing.T) {
	t.Helper()
	mustUnsetenv(t, "XDG_CONFIG_HOME")
	mustUnsetenv(t, "XDG_DATA_HOME")
}

// loadWithTOML writes content to config.toml in a temp dir and calls Load.
// XDG vars are cleared so loading uses the current directory.
func loadWithTOML(t *testing.T, content string) (*config.Config, error) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}
	clearXDGVars(t)
	t.Chdir(dir)
	return config.Load()
}

// snapshotDir returns a sorted list of all relative paths under dir.
func snapshotDir(t *testing.T, dir string) []string {
	t.Helper()
	var entries []string
	err := filepath.WalkDir(dir, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		if rel != "." {
			entries = append(entries, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("snapshotDir: %v", err)
	}
	slices.Sort(entries)
	return entries
}

// ========================================================================
// Task 1.1: LoadConfig defaults and missing file behavior
// (TS-01-9, TS-01-11, TS-01-17)
// ========================================================================

// TestConfig_DefaultsNoFile verifies that LoadConfig returns *Config with all
// documented defaults and nil error when config.toml is absent.
// Covers TS-01-9, TS-01-11 (Requirements: 01-REQ-3.1, 01-REQ-3.3).
func TestConfig_DefaultsNoFile(t *testing.T) {
	clearXDGVars(t)
	dir := t.TempDir()
	t.Chdir(dir)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil Config")
	}

	if cfg.Server.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Server.Port)
	}
	if cfg.Server.Bind != "0.0.0.0" {
		t.Errorf("Bind = %q, want %q", cfg.Server.Bind, "0.0.0.0")
	}
	if cfg.Server.ExternalURL != "" {
		t.Errorf("ExternalURL = %q, want %q", cfg.Server.ExternalURL, "")
	}
	if cfg.Server.MountPoint != "/api/v1" {
		t.Errorf("MountPoint = %q, want %q", cfg.Server.MountPoint, "/api/v1")
	}
	if cfg.Server.MaxBodyBytes() != 1048576 {
		t.Errorf("MaxBodyBytes() = %d, want 1048576", cfg.Server.MaxBodyBytes())
	}
	if cfg.Database.Path != "./data/apikit.db" {
		t.Errorf("Database.Path = %q, want %q", cfg.Database.Path, "./data/apikit.db")
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("Logging.Level = %q, want %q", cfg.Logging.Level, "info")
	}
}

// TestConfig_NoFilesystemSideEffects verifies that LoadConfig creates no files
// or directories beyond reading the config file.
// Covers TS-01-17 (Requirement: 01-REQ-3.9).
func TestConfig_NoFilesystemSideEffects(t *testing.T) {
	dir := t.TempDir()
	xdgData := t.TempDir()

	clearXDGVars(t)
	t.Setenv("XDG_DATA_HOME", xdgData)
	t.Chdir(dir)

	before := snapshotDir(t, dir)
	beforeXDG := snapshotDir(t, xdgData)

	_, _ = config.Load()

	after := snapshotDir(t, dir)
	afterXDG := snapshotDir(t, xdgData)

	if !slices.Equal(before, after) {
		t.Errorf("working dir modified: before=%v, after=%v", before, after)
	}
	if !slices.Equal(beforeXDG, afterXDG) {
		t.Errorf("XDG data dir modified: before=%v, after=%v", beforeXDG, afterXDG)
	}
}

// ========================================================================
// Task 1.2: Validation — malformed TOML, port, bind, external_url
// (TS-01-10, TS-01-12, TS-01-13, TS-01-14)
// ========================================================================

// TestConfig_MalformedTOML verifies that LoadConfig returns (nil, error) with
// a descriptive parse error when config.toml contains invalid TOML syntax.
// Covers TS-01-10 (Requirement: 01-REQ-3.2).
func TestConfig_MalformedTOML(t *testing.T) {
	cfg, err := loadWithTOML(t, "[[invalid")
	if cfg != nil {
		t.Error("expected nil config for malformed TOML")
	}
	if err == nil {
		t.Fatal("expected non-nil error for malformed TOML")
	}
	if len(err.Error()) == 0 {
		t.Error("expected descriptive error message")
	}
}

// TestConfig_PortValidation verifies that LoadConfig accepts port 0–65535 and
// rejects values outside that range with a descriptive error.
// Covers TS-01-12, TS-01-E5 (Requirements: 01-REQ-3.4, 01-REQ-3.E3).
func TestConfig_PortValidation(t *testing.T) {
	validPorts := []int{0, 8080, 65535}
	for _, port := range validPorts {
		t.Run(fmt.Sprintf("valid_%d", port), func(t *testing.T) {
			cfg, err := loadWithTOML(t, fmt.Sprintf("[server]\nport = %d\n", port))
			if err != nil {
				t.Fatalf("unexpected error for port %d: %v", port, err)
			}
			if cfg == nil {
				t.Fatal("expected non-nil config")
			}
			if cfg.Server.Port != port {
				t.Errorf("Port = %d, want %d", cfg.Server.Port, port)
			}
		})
	}

	invalidPorts := []int{65536, -1, 99999}
	for _, port := range invalidPorts {
		t.Run(fmt.Sprintf("invalid_%d", port), func(t *testing.T) {
			cfg, err := loadWithTOML(t, fmt.Sprintf("[server]\nport = %d\n", port))
			if cfg != nil {
				t.Error("expected nil config for invalid port")
			}
			if err == nil {
				t.Fatal("expected error for invalid port")
			}
			// TS-01-E5: error should reference the invalid port value or range
			errMsg := err.Error()
			if port == 99999 {
				hasRef := strings.Contains(errMsg, "99999") ||
					strings.Contains(errMsg, "port") ||
					strings.Contains(errMsg, "range")
				if !hasRef {
					t.Errorf("error should reference the invalid port: %s", errMsg)
				}
			}
		})
	}
}

// TestConfig_BindValidation verifies that LoadConfig accepts any non-empty bind
// string as-is and replaces empty or absent values with "0.0.0.0".
// Covers TS-01-13 (Requirement: 01-REQ-3.5).
func TestConfig_BindValidation(t *testing.T) {
	t.Run("non_empty_valid_ip", func(t *testing.T) {
		cfg, err := loadWithTOML(t, "[server]\nbind = \"192.168.1.1\"\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg == nil {
			t.Fatal("expected non-nil config")
		}
		if cfg.Server.Bind != "192.168.1.1" {
			t.Errorf("Bind = %q, want %q", cfg.Server.Bind, "192.168.1.1")
		}
	})

	t.Run("non_empty_invalid_string", func(t *testing.T) {
		cfg, err := loadWithTOML(t, "[server]\nbind = \"not-an-ip\"\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg == nil {
			t.Fatal("expected non-nil config")
		}
		if cfg.Server.Bind != "not-an-ip" {
			t.Errorf("Bind = %q, want %q", cfg.Server.Bind, "not-an-ip")
		}
	})

	t.Run("empty_string", func(t *testing.T) {
		cfg, err := loadWithTOML(t, "[server]\nbind = \"\"\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg == nil {
			t.Fatal("expected non-nil config")
		}
		if cfg.Server.Bind != "0.0.0.0" {
			t.Errorf("Bind = %q, want %q (default for empty)", cfg.Server.Bind, "0.0.0.0")
		}
	})

	t.Run("absent", func(t *testing.T) {
		cfg, err := loadWithTOML(t, "[server]\nport = 8080\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg == nil {
			t.Fatal("expected non-nil config")
		}
		if cfg.Server.Bind != "0.0.0.0" {
			t.Errorf("Bind = %q, want %q (default for absent)", cfg.Server.Bind, "0.0.0.0")
		}
	})
}

// TestConfig_ExternalURLValidation verifies that LoadConfig stores external_url
// as-is without validation and stores empty string when absent.
// Covers TS-01-14 (Requirement: 01-REQ-3.6).
func TestConfig_ExternalURLValidation(t *testing.T) {
	tests := []struct {
		name string
		toml string
		want string
	}{
		{"valid_url", "[server]\nexternal_url = \"https://example.com\"\n", "https://example.com"},
		{"invalid_url", "[server]\nexternal_url = \"not-a-url\"\n", "not-a-url"},
		{"empty_string", "[server]\nexternal_url = \"\"\n", ""},
		{"absent", "[server]\nport = 8080\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := loadWithTOML(t, tt.toml)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg == nil {
				t.Fatal("expected non-nil config")
			}
			if cfg.Server.ExternalURL != tt.want {
				t.Errorf("ExternalURL = %q, want %q", cfg.Server.ExternalURL, tt.want)
			}
		})
	}
}

// ========================================================================
// Task 1.3: Validation — log level and max_body_size
// (TS-01-15, TS-01-16, TS-01-E3, TS-01-E4, TS-01-E5, TS-01-E6)
// ========================================================================

// TestConfig_LogLevelValidation verifies that LoadConfig accepts the seven
// canonical log levels (case-insensitive) and rejects invalid values.
// Note: "WARNING" is intentionally invalid per spec (01-REQ-3.7) even though
// logrus.ParseLevel accepts it. The implementation must use a custom allowlist.
// Covers TS-01-15, TS-01-E6 (Requirements: 01-REQ-3.7, 01-REQ-3.E4).
func TestConfig_LogLevelValidation(t *testing.T) {
	validLevels := []string{"trace", "debug", "info", "warn", "error", "fatal", "panic", "INFO", "DEBUG"}
	for _, level := range validLevels {
		t.Run("valid_"+level, func(t *testing.T) {
			cfg, err := loadWithTOML(t, fmt.Sprintf("[logging]\nlevel = %q\n", level))
			if err != nil {
				t.Fatalf("unexpected error for level %q: %v", level, err)
			}
			if cfg == nil {
				t.Fatalf("expected non-nil config for level %q", level)
			}
		})
	}

	// Invalid levels — "WARNING" is explicitly excluded per 01-REQ-3.7
	// despite logrus.ParseLevel accepting it as an alias for "warn".
	invalidLevels := []string{"verbose", "WARNING", "silent"}
	for _, level := range invalidLevels {
		t.Run("invalid_"+level, func(t *testing.T) {
			cfg, err := loadWithTOML(t, fmt.Sprintf("[logging]\nlevel = %q\n", level))
			if cfg != nil {
				t.Errorf("expected nil config for invalid level %q", level)
			}
			if err == nil {
				t.Fatalf("expected error for invalid level %q", level)
			}
			// TS-01-E6: error should mention the invalid level or list valid options
			if len(err.Error()) == 0 {
				t.Error("expected descriptive error message")
			}
		})
	}
}

// TestConfig_MaxBodySizeValidation verifies max_body_size format validation:
// valid suffixes (KB, MB, GB) accepted case-insensitively, empty treated as
// default 1MB, invalid values rejected with descriptive error.
// Covers TS-01-16, TS-01-E3, TS-01-E4 (Requirements: 01-REQ-3.8, 01-REQ-3.E1, 01-REQ-3.E2).
func TestConfig_MaxBodySizeValidation(t *testing.T) {
	// Valid values with expected byte counts
	validCases := []struct {
		input string
		bytes int64
	}{
		{"512KB", 524288},
		{"1MB", 1048576},
		{"2GB", 2147483648},
		{"512kb", 524288},  // case-insensitive
		{"1mb", 1048576},   // case-insensitive
	}
	for _, tc := range validCases {
		t.Run("valid_"+tc.input, func(t *testing.T) {
			cfg, err := loadWithTOML(t, fmt.Sprintf("[server]\nmax_body_size = %q\n", tc.input))
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.input, err)
			}
			if cfg == nil {
				t.Fatal("expected non-nil config")
			}
			if cfg.Server.MaxBodyBytes() != tc.bytes {
				t.Errorf("MaxBodyBytes() = %d, want %d for input %q",
					cfg.Server.MaxBodyBytes(), tc.bytes, tc.input)
			}
		})
	}

	// Empty string: treated as absent, defaults to 1MB
	t.Run("empty_string_defaults_to_1MB", func(t *testing.T) {
		cfg, err := loadWithTOML(t, "[server]\nmax_body_size = \"\"\n")
		if err != nil {
			t.Fatalf("unexpected error for empty string: %v", err)
		}
		if cfg == nil {
			t.Fatal("expected non-nil config")
		}
		if cfg.Server.MaxBodyBytes() != 1048576 {
			t.Errorf("MaxBodyBytes() = %d, want 1048576 (default for empty)", cfg.Server.MaxBodyBytes())
		}
	})

	// Invalid values (TS-01-E3: 0MB, -1MB; TS-01-E4: 1TB, "1 MB")
	invalidCases := []struct {
		input string
		desc  string
	}{
		{"1 MB", "space between number and suffix"},
		{"1048576", "plain integer without suffix"},
		{"1TB", "unsupported suffix"},
		{"0MB", "zero size"},
		{"-1MB", "negative size"},
	}
	for _, tc := range invalidCases {
		t.Run("invalid_"+tc.input, func(t *testing.T) {
			cfg, err := loadWithTOML(t, fmt.Sprintf("[server]\nmax_body_size = %q\n", tc.input))
			if cfg != nil {
				t.Errorf("expected nil config for %q (%s)", tc.input, tc.desc)
			}
			if err == nil {
				t.Fatalf("expected error for %q (%s)", tc.input, tc.desc)
			}
			if len(err.Error()) == 0 {
				t.Errorf("expected descriptive error message for %q", tc.input)
			}
		})
	}
}

// ========================================================================
// Task 1.4: XDG base directory support
// (TS-01-18, TS-01-19, TS-01-20, TS-01-E7)
// ========================================================================

// TestConfig_XDGConfigHome_FilePresent verifies that when XDG_CONFIG_HOME is
// set and config.toml exists there, LoadConfig reads from the XDG path
// exclusively and does NOT fall back to ./config.toml.
// Covers TS-01-18 (Requirement: 01-REQ-4.1).
func TestConfig_XDGConfigHome_FilePresent(t *testing.T) {
	// Set up XDG config dir with config.toml (port=9090)
	xdgDir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(xdgDir, "config.toml"),
		[]byte("[server]\nport = 9090\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	// Set up cwd with a DIFFERENT config.toml (port=7070)
	cwdDir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(cwdDir, "config.toml"),
		[]byte("[server]\nport = 7070\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	mustUnsetenv(t, "XDG_DATA_HOME")
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	t.Chdir(cwdDir)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	// Must use XDG path (port=9090), NOT cwd (port=7070)
	if cfg.Server.Port != 9090 {
		t.Errorf("Port = %d, want 9090 (from XDG path, not 7070 from cwd)", cfg.Server.Port)
	}
}

// TestConfig_XDGConfigHome_FileAbsent verifies that when XDG_CONFIG_HOME is set
// but $XDG_CONFIG_HOME/config.toml does not exist, LoadConfig applies
// all defaults and does NOT fall back to ./config.toml.
// Covers TS-01-E7 (Requirement: 01-REQ-4.E1).
func TestConfig_XDGConfigHome_FileAbsent(t *testing.T) {
	// XDG config dir exists but has no config.toml
	xdgDir := t.TempDir()

	// cwd has a config.toml with port=9999
	cwdDir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(cwdDir, "config.toml"),
		[]byte("[server]\nport = 9999\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	mustUnsetenv(t, "XDG_DATA_HOME")
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	t.Chdir(cwdDir)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	// Must use defaults (port=8080), NOT cwd config (port=9999)
	if cfg.Server.Port != 8080 {
		t.Errorf("Port = %d, want 8080 (default, not 9999 from cwd)", cfg.Server.Port)
	}
}

// TestConfig_XDGDataHome_DatabasePath verifies that when XDG_DATA_HOME is set,
// Config.Database.Path resolves to $XDG_DATA_HOME/apikit.db and no
// subdirectory is created.
// Covers TS-01-19 (Requirement: 01-REQ-4.2).
func TestConfig_XDGDataHome_DatabasePath(t *testing.T) {
	dataDir := t.TempDir()
	clearXDGVars(t)
	t.Setenv("XDG_DATA_HOME", dataDir)

	dir := t.TempDir()
	t.Chdir(dir)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}

	want := filepath.Join(dataDir, "apikit.db")
	if cfg.Database.Path != want {
		t.Errorf("Database.Path = %q, want %q", cfg.Database.Path, want)
	}
}

// TestConfig_XDGDataHome_BareFilename verifies that when XDG_DATA_HOME is set
// and database.path is a bare filename, the path is combined with XDG_DATA_HOME.
func TestConfig_XDGDataHome_BareFilename(t *testing.T) {
	dataDir := t.TempDir()
	cfgDir := t.TempDir()
	clearXDGVars(t)
	t.Setenv("XDG_DATA_HOME", dataDir)
	t.Chdir(cfgDir)

	if err := os.WriteFile(
		filepath.Join(cfgDir, "config.toml"),
		[]byte("[database]\npath = \"myapp.db\"\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := filepath.Join(dataDir, "myapp.db")
	if cfg.Database.Path != want {
		t.Errorf("Database.Path = %q, want %q", cfg.Database.Path, want)
	}
}

// TestConfig_XDGDataHome_RelativePath verifies that when database.path has a
// directory component, it is used as-is even when XDG_DATA_HOME is set.
func TestConfig_XDGDataHome_RelativePath(t *testing.T) {
	dataDir := t.TempDir()
	cfgDir := t.TempDir()
	clearXDGVars(t)
	t.Setenv("XDG_DATA_HOME", dataDir)
	t.Chdir(cfgDir)

	if err := os.WriteFile(
		filepath.Join(cfgDir, "config.toml"),
		[]byte("[database]\npath = \"./myapp.db\"\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Database.Path != "./myapp.db" {
		t.Errorf("Database.Path = %q, want %q", cfg.Database.Path, "./myapp.db")
	}
}

// TestConfig_BareFilename_NoXDG verifies that a bare filename is used as-is
// when XDG_DATA_HOME is not set.
func TestConfig_BareFilename_NoXDG(t *testing.T) {
	cfgDir := t.TempDir()
	clearXDGVars(t)
	t.Chdir(cfgDir)

	if err := os.WriteFile(
		filepath.Join(cfgDir, "config.toml"),
		[]byte("[database]\npath = \"myapp.db\"\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Database.Path != "myapp.db" {
		t.Errorf("Database.Path = %q, want %q", cfg.Database.Path, "myapp.db")
	}
}

// TestConfig_NoXDG_CwdDefaults verifies that when neither XDG_CONFIG_HOME nor
// XDG_DATA_HOME is set, LoadConfig uses ./config.toml and ./data/apikit.db.
// Covers TS-01-20 (Requirement: 01-REQ-4.3).
func TestConfig_NoXDG_CwdDefaults(t *testing.T) {
	clearXDGVars(t)
	dir := t.TempDir()
	t.Chdir(dir)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.Database.Path != "./data/apikit.db" {
		t.Errorf("Database.Path = %q, want %q", cfg.Database.Path, "./data/apikit.db")
	}
}

// ========================================================================
// Environment variable expansion
// ========================================================================

// TestConfig_EnvExpansion_DollarVar verifies that $VAR syntax in a string
// field is expanded from the environment before TOML parsing.
func TestConfig_EnvExpansion_DollarVar(t *testing.T) {
	t.Setenv("TEST_EXTERNAL_URL", "https://expanded.example.com")
	cfg, err := loadWithTOML(t, `[server]
external_url = "$TEST_EXTERNAL_URL"
`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Server.ExternalURL != "https://expanded.example.com" {
		t.Errorf("ExternalURL = %q, want %q", cfg.Server.ExternalURL, "https://expanded.example.com")
	}
}

// TestConfig_EnvExpansion_BracedVar verifies that ${VAR} syntax works for
// secret fields like client_secret.
func TestConfig_EnvExpansion_BracedVar(t *testing.T) {
	t.Setenv("TEST_GH_ID", "gh-id-123")
	t.Setenv("TEST_GH_SECRET", "gh-secret-456")
	cfg, err := loadWithTOML(t, `
[[oauth.providers]]
name = "github"
client_id = "${TEST_GH_ID}"
client_secret = "${TEST_GH_SECRET}"
`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.OAuth.Providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(cfg.OAuth.Providers))
	}
	p := cfg.OAuth.Providers[0]
	if p.ClientID != "gh-id-123" {
		t.Errorf("ClientID = %q, want %q", p.ClientID, "gh-id-123")
	}
	if p.ClientSecret != "gh-secret-456" {
		t.Errorf("ClientSecret = %q, want %q", p.ClientSecret, "gh-secret-456")
	}
}

// TestConfig_EnvExpansion_UndefinedVar verifies that a reference to an
// undefined env var expands to an empty string, which triggers a validation
// error for required fields.
func TestConfig_EnvExpansion_UndefinedVar(t *testing.T) {
	mustUnsetenv(t, "DEFINITELY_NOT_SET_12345")
	t.Setenv("TEST_GH_ID", "gh-id-123")
	_, err := loadWithTOML(t, `
[[oauth.providers]]
name = "github"
client_id = "${TEST_GH_ID}"
client_secret = "${DEFINITELY_NOT_SET_12345}"
`)
	if err == nil {
		t.Fatal("expected validation error for empty client_secret from undefined env var")
	}
	if !strings.Contains(err.Error(), "client_secret") {
		t.Errorf("error should mention client_secret: %v", err)
	}
}

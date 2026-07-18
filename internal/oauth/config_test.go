package oauth_test

import (
	"os"
	"path/filepath"
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

// ========================================================================
// TS-06-13: TOML with [[oauth.providers]] correctly unmarshals
// (Requirement: 06-REQ-4.1)
// ========================================================================

// TestConfig_OAuthProviderUnmarshal verifies that a [[oauth.providers]]
// TOML entry correctly unmarshals into Config.OAuth.Providers including
// optional URL overrides.
func TestConfig_OAuthProviderUnmarshal(t *testing.T) {
	tomlContent := `
[[oauth.providers]]
name = "github"
client_id = "cid"
client_secret = "csec"
authorize_url = "https://custom.example.com/auth"
token_url = "https://custom.example.com/token"
userinfo_url = "https://custom.example.com/user"
`
	cfg, err := loadWithTOML(t, tomlContent)
	if err != nil {
		t.Fatalf("LoadConfig error = %v, want nil", err)
	}
	if cfg == nil {
		t.Fatal("LoadConfig returned nil Config")
	}

	if len(cfg.OAuth.Providers) != 1 {
		t.Fatalf("len(OAuth.Providers) = %d, want 1", len(cfg.OAuth.Providers))
	}

	p := cfg.OAuth.Providers[0]
	if p.Name != "github" {
		t.Errorf("Name = %q, want %q", p.Name, "github")
	}
	if p.ClientID != "cid" {
		t.Errorf("ClientID = %q, want %q", p.ClientID, "cid")
	}
	if p.ClientSecret != "csec" {
		t.Errorf("ClientSecret = %q, want %q", p.ClientSecret, "csec")
	}
	if p.AuthorizeURL != "https://custom.example.com/auth" {
		t.Errorf("AuthorizeURL = %q, want %q", p.AuthorizeURL, "https://custom.example.com/auth")
	}
	if p.TokenURL != "https://custom.example.com/token" {
		t.Errorf("TokenURL = %q, want %q", p.TokenURL, "https://custom.example.com/token")
	}
	if p.UserinfoURL != "https://custom.example.com/user" {
		t.Errorf("UserinfoURL = %q, want %q", p.UserinfoURL, "https://custom.example.com/user")
	}
}

// ========================================================================
// TS-06-14: Missing client_id returns (nil, error)
// (Requirement: 06-REQ-4.2)
// ========================================================================

// TestConfig_OAuthProviderMissingClientID verifies that LoadConfig returns
// (nil, error) when a provider entry is missing client_id.
func TestConfig_OAuthProviderMissingClientID(t *testing.T) {
	tomlContent := `
[[oauth.providers]]
name = "github"
client_secret = "csec"
`
	cfg, err := loadWithTOML(t, tomlContent)
	if cfg != nil {
		t.Error("expected nil config for missing client_id")
	}
	if err == nil {
		t.Fatal("expected non-nil error for missing client_id")
	}
	errMsg := err.Error()
	if !containsStr(errMsg, "client_id") {
		t.Errorf("error message %q does not mention 'client_id'", errMsg)
	}
}

// TestConfig_OAuthProviderMissingName verifies that LoadConfig returns
// (nil, error) when a provider entry is missing name.
func TestConfig_OAuthProviderMissingName(t *testing.T) {
	tomlContent := `
[[oauth.providers]]
client_id = "cid"
client_secret = "csec"
`
	cfg, err := loadWithTOML(t, tomlContent)
	if cfg != nil {
		t.Error("expected nil config for missing name")
	}
	if err == nil {
		t.Fatal("expected non-nil error for missing name")
	}
	errMsg := err.Error()
	if !containsStr(errMsg, "name") {
		t.Errorf("error message %q does not mention 'name'", errMsg)
	}
}

// TestConfig_OAuthProviderMissingClientSecret verifies that LoadConfig returns
// (nil, error) when a provider entry is missing client_secret.
func TestConfig_OAuthProviderMissingClientSecret(t *testing.T) {
	tomlContent := `
[[oauth.providers]]
name = "github"
client_id = "cid"
`
	cfg, err := loadWithTOML(t, tomlContent)
	if cfg != nil {
		t.Error("expected nil config for missing client_secret")
	}
	if err == nil {
		t.Fatal("expected non-nil error for missing client_secret")
	}
	errMsg := err.Error()
	if !containsStr(errMsg, "client_secret") {
		t.Errorf("error message %q does not mention 'client_secret'", errMsg)
	}
}

// ========================================================================
// TS-06-15: Duplicate provider names returns (nil, error)
// (Requirement: 06-REQ-4.3)
// ========================================================================

// TestConfig_OAuthProviderDuplicateNames verifies that LoadConfig returns
// (nil, error) when two [[oauth.providers]] entries share the same name.
func TestConfig_OAuthProviderDuplicateNames(t *testing.T) {
	tomlContent := `
[[oauth.providers]]
name = "github"
client_id = "cid1"
client_secret = "csec1"

[[oauth.providers]]
name = "github"
client_id = "cid2"
client_secret = "csec2"
`
	cfg, err := loadWithTOML(t, tomlContent)
	if cfg != nil {
		t.Error("expected nil config for duplicate provider names")
	}
	if err == nil {
		t.Fatal("expected non-nil error for duplicate provider names")
	}
	errMsg := err.Error()
	if !containsStr(errMsg, "duplicate") && !containsStr(errMsg, "github") {
		t.Errorf("error message %q does not indicate duplicate or mention 'github'", errMsg)
	}
}

// ========================================================================
// TS-06-17: Empty/absent [[oauth.providers]] is valid
// (Requirement: 06-REQ-4.5)
// ========================================================================

// TestConfig_OAuthProvidersEmpty verifies that LoadConfig accepts empty or
// absent [[oauth.providers]] array as valid.
func TestConfig_OAuthProvidersEmpty(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		// Config with no oauth section at all
		cfg, err := loadWithTOML(t, "[server]\nport = 8080\n")
		if err != nil {
			t.Fatalf("LoadConfig error = %v, want nil", err)
		}
		if cfg == nil {
			t.Fatal("LoadConfig returned nil Config")
		}
		if len(cfg.OAuth.Providers) != 0 {
			t.Errorf("len(OAuth.Providers) = %d, want 0", len(cfg.OAuth.Providers))
		}
	})

	t.Run("empty_section", func(t *testing.T) {
		// Config with [oauth] section but no [[oauth.providers]] entries
		cfg, err := loadWithTOML(t, "[oauth]\n")
		if err != nil {
			t.Fatalf("LoadConfig error = %v, want nil", err)
		}
		if cfg == nil {
			t.Fatal("LoadConfig returned nil Config")
		}
		if len(cfg.OAuth.Providers) != 0 {
			t.Errorf("len(OAuth.Providers) = %d, want 0", len(cfg.OAuth.Providers))
		}
	})
}

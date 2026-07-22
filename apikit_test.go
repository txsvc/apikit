package apikit_test

import (
	"os"
	"os/exec"
	"testing"

	"github.com/txsvc/apikit"
	"github.com/txsvc/apikit/internal/config"
)

// ========================================================================
// Task 1.5: Build-time variables and config re-export
// (TS-01-67, TS-01-68, TS-01-69, TS-01-70)
// ========================================================================

// TestConfig_BuildVarDefaults verifies that Version, Commit, BuildTime, and
// TokenPrefix are declared with correct default values when built without
// -ldflags.
// Covers TS-01-67 (Requirement: 01-REQ-20.1).
func TestConfig_BuildVarDefaults(t *testing.T) {
	if apikit.Version != "dev" {
		t.Errorf("Version = %q, want %q", apikit.Version, "dev")
	}
	if apikit.Commit != "dev" {
		t.Errorf("Commit = %q, want %q", apikit.Commit, "dev")
	}
	if apikit.BuildTime != "" {
		t.Errorf("BuildTime = %q, want %q", apikit.BuildTime, "")
	}
	if apikit.TokenPrefix != "ak" {
		t.Errorf("TokenPrefix = %q, want %q", apikit.TokenPrefix, "ak")
	}
}

// TestConfig_MakeBuild verifies that make build completes successfully and
// produces the bin/apikit binary. This test depends on the Makefile existing
// (created in task group 10).
// Covers TS-01-68 (Requirement: 01-REQ-20.2).
func TestConfig_MakeBuild(t *testing.T) {
	cmd := exec.Command("make", "build")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make build failed: %v\nOutput: %s", err, out)
	}

	if _, err := os.Stat("bin/apikit"); os.IsNotExist(err) {
		t.Fatal("bin/apikit not found after make build")
	}
}

// TestConfig_TypeAlias verifies that apikit.Config is a type alias for
// internal/config.Config, so *apikit.Config and *config.Config are the same
// type requiring no conversion.
// Covers TS-01-69 (Requirement: 01-REQ-21.1).
func TestConfig_TypeAlias(t *testing.T) {
	// Compile-time verification: if Config is a type alias, this assignment
	// compiles without any type assertion or conversion. If it were a
	// distinct named type, this would be a compile error.
	var internalCfg *config.Config = &config.Config{}
	var apikitCfg *apikit.Config = internalCfg
	_ = apikitCfg

	// Also verify the reverse direction
	var apikitCfg2 *apikit.Config = &apikit.Config{}
	var internalCfg2 *config.Config = apikitCfg2
	_ = internalCfg2
}

// TestConfig_LoadReExport verifies that apikit.LoadConfig() produces identical
// results to internal/config.Load().
// Covers TS-01-70 (Requirement: 01-REQ-21.2).
func TestConfig_LoadReExport(t *testing.T) {
	// Ensure clean environment
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME"} {
		if val, ok := os.LookupEnv(key); ok {
			t.Cleanup(func() { os.Setenv(key, val) })
		}
		os.Unsetenv(key)
	}

	dir := t.TempDir()
	t.Chdir(dir)

	cfg1, err1 := apikit.LoadConfig()
	cfg2, err2 := config.Load()

	// Both should return the same error status
	if (err1 == nil) != (err2 == nil) {
		t.Fatalf("error mismatch: apikit err=%v, config err=%v", err1, err2)
	}
	if err1 != nil {
		return
	}
	if cfg1 == nil || cfg2 == nil {
		t.Fatal("expected non-nil configs from both functions")
	}

	// Verify field-by-field equivalence
	if cfg1.Server.Port != cfg2.Server.Port {
		t.Errorf("Port: apikit=%d, config=%d", cfg1.Server.Port, cfg2.Server.Port)
	}
	if cfg1.Server.Bind != cfg2.Server.Bind {
		t.Errorf("Bind: apikit=%q, config=%q", cfg1.Server.Bind, cfg2.Server.Bind)
	}
	if cfg1.Server.ExternalURL != cfg2.Server.ExternalURL {
		t.Errorf("ExternalURL: apikit=%q, config=%q", cfg1.Server.ExternalURL, cfg2.Server.ExternalURL)
	}
	if cfg1.Server.MountPoint != cfg2.Server.MountPoint {
		t.Errorf("MountPoint: apikit=%q, config=%q", cfg1.Server.MountPoint, cfg2.Server.MountPoint)
	}
	if cfg1.Server.MaxBodyBytes() != cfg2.Server.MaxBodyBytes() {
		t.Errorf("MaxBodyBytes: apikit=%d, config=%d", cfg1.Server.MaxBodyBytes(), cfg2.Server.MaxBodyBytes())
	}
	if cfg1.Database.Path != cfg2.Database.Path {
		t.Errorf("Database.Path: apikit=%q, config=%q", cfg1.Database.Path, cfg2.Database.Path)
	}
	if cfg1.Logging.Level != cfg2.Logging.Level {
		t.Errorf("Logging.Level: apikit=%q, config=%q", cfg1.Logging.Level, cfg2.Logging.Level)
	}

	// Additionally verify defaults are applied (since no config file exists)
	if cfg1.Server.Port != 8080 {
		t.Errorf("LoadConfig: Port = %d, want 8080 (default)", cfg1.Server.Port)
	}
}

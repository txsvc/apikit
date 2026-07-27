package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/txsvc/apikit/internal/config"
)

// ========================================================================
// Spec 05 Task 1.1: WorkspaceConfig struct and path resolution
// (TS-05-1, TS-05-2, TS-05-3, TS-05-4, 05-REQ-1.E1–E4)
// ========================================================================

// getWorkspaceField extracts the Workspace field from a *Config using
// reflection, returning (Path, Workers, ok). This allows tests to compile
// and run even before the WorkspaceConfig struct is added to Config.
func getWorkspaceField(t *testing.T, cfg *config.Config) (path string, workers int64, ok bool) {
	t.Helper()
	v := reflect.ValueOf(cfg).Elem()
	ws := v.FieldByName("Workspace")
	if !ws.IsValid() {
		t.Fatal("Config has no Workspace field")
		return "", 0, false
	}

	pathVal := ws.FieldByName("Path")
	if !pathVal.IsValid() {
		t.Fatal("WorkspaceConfig has no Path field")
		return "", 0, false
	}

	workersVal := ws.FieldByName("Workers")
	if !workersVal.IsValid() {
		t.Fatal("WorkspaceConfig has no Workers field")
		return "", 0, false
	}

	return pathVal.String(), workersVal.Int(), true
}

// TS-05-1: WorkspaceConfig struct exists with Path and Workers fields and is
// embedded in Config as Workspace with correct TOML tags.
// Requirement: 05-REQ-1.1
func TestWorkspaceConfig_StructFields(t *testing.T) {
	cfgType := reflect.TypeOf(config.Config{})

	// Config must have a Workspace field of type WorkspaceConfig.
	wsField, ok := cfgType.FieldByName("Workspace")
	if !ok {
		t.Fatal("Config struct missing Workspace field")
	}
	if wsField.Type.Name() != "WorkspaceConfig" {
		t.Errorf("Workspace field type = %q, want %q", wsField.Type.Name(), "WorkspaceConfig")
	}
	if tag := wsField.Tag.Get("toml"); tag != "workspace" {
		t.Errorf("Workspace toml tag = %q, want %q", tag, "workspace")
	}

	// WorkspaceConfig must have Path (string, toml:"path").
	wsType := wsField.Type
	pathField, ok := wsType.FieldByName("Path")
	if !ok {
		t.Fatal("WorkspaceConfig missing Path field")
	}
	if pathField.Type.Kind() != reflect.String {
		t.Errorf("Path type = %v, want string", pathField.Type.Kind())
	}
	if tag := pathField.Tag.Get("toml"); tag != "path" {
		t.Errorf("Path toml tag = %q, want %q", tag, "path")
	}

	// WorkspaceConfig must have Workers (int, toml:"workers").
	workersField, ok := wsType.FieldByName("Workers")
	if !ok {
		t.Fatal("WorkspaceConfig missing Workers field")
	}
	if workersField.Type.Kind() != reflect.Int {
		t.Errorf("Workers type = %v, want int", workersField.Type.Kind())
	}
	if tag := workersField.Tag.Get("toml"); tag != "workers" {
		t.Errorf("Workers toml tag = %q, want %q", tag, "workers")
	}
}

// TS-05-2: Load() resolves Workspace.Path via resolveDataPath and defaults
// Workers to 4 when configured value is 0.
// Requirement: 05-REQ-1.2
func TestWorkspaceConfig_LoadDefaultsWorkersTo4WhenZero(t *testing.T) {
	cfg, err := loadWithTOML(t, "[workspace]\npath = \"./workspace\"\nworkers = 0\n")
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	path, workers, ok := getWorkspaceField(t, cfg)
	if !ok {
		return
	}
	if path != "./workspace" {
		t.Errorf("Workspace.Path = %q, want %q", path, "./workspace")
	}
	if workers != 4 {
		t.Errorf("Workspace.Workers = %d, want 4 (default for zero)", workers)
	}
}

// TS-05-3: Load() sets Workspace.Path to './workspace' and Workers to 4 when
// config.toml specifies those exact values.
// Requirement: 05-REQ-1.3
func TestWorkspaceConfig_LoadExplicitValues(t *testing.T) {
	cfg, err := loadWithTOML(t, "[workspace]\npath = \"./workspace\"\nworkers = 4\n")
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	path, workers, ok := getWorkspaceField(t, cfg)
	if !ok {
		return
	}
	if path != "./workspace" {
		t.Errorf("Workspace.Path = %q, want %q", path, "./workspace")
	}
	if workers != 4 {
		t.Errorf("Workspace.Workers = %d, want 4", workers)
	}
}

// TS-05-4: Load() applies empty-path resolution rules and defaults Workers to 4
// when the [workspace] section is entirely omitted from config.toml.
// Requirement: 05-REQ-1.4
func TestWorkspaceConfig_LoadOmittedSection(t *testing.T) {
	clearXDGVars(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("# no [workspace] section\n"), 0o644); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}
	t.Chdir(dir)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	path, workers, ok := getWorkspaceField(t, cfg)
	if !ok {
		return
	}
	if workers != 4 {
		t.Errorf("Workspace.Workers = %d, want 4 (default for omitted)", workers)
	}
	if path != "./data/workspaces" {
		t.Errorf("Workspace.Path = %q, want %q", path, "./data/workspaces")
	}
}

// ========================================================================
// Edge Cases
// ========================================================================

// 05-REQ-1.E1: When XDG_DATA_HOME is set and workspace path is a bare name
// (no directory component), the path resolves to $XDG_DATA_HOME/<bare-name>.
func TestWorkspaceConfig_XDGDataHome_BareName(t *testing.T) {
	xdgDir := t.TempDir()
	cfgDir := t.TempDir()
	clearXDGVars(t)
	t.Setenv("XDG_DATA_HOME", xdgDir)
	t.Chdir(cfgDir)

	if err := os.WriteFile(
		filepath.Join(cfgDir, "config.toml"),
		[]byte("[workspace]\npath = \"workspaces\"\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	path, _, ok := getWorkspaceField(t, cfg)
	if !ok {
		return
	}
	want := filepath.Join(xdgDir, "workspaces")
	if path != want {
		t.Errorf("Workspace.Path = %q, want %q", path, want)
	}
}

// 05-REQ-1.E2: When workspace path is empty and XDG_DATA_HOME is not set,
// the path defaults to ./data/workspaces.
func TestWorkspaceConfig_EmptyPath_NoXDG(t *testing.T) {
	clearXDGVars(t)
	dir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(dir, "config.toml"),
		[]byte("[workspace]\npath = \"\"\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	path, _, ok := getWorkspaceField(t, cfg)
	if !ok {
		return
	}
	if path != "./data/workspaces" {
		t.Errorf("Workspace.Path = %q, want %q", path, "./data/workspaces")
	}
}

// 05-REQ-1.E3: When workspace path is empty and XDG_DATA_HOME is set,
// the path defaults to $XDG_DATA_HOME/workspaces.
func TestWorkspaceConfig_EmptyPath_WithXDG(t *testing.T) {
	xdgDir := t.TempDir()
	cfgDir := t.TempDir()
	clearXDGVars(t)
	t.Setenv("XDG_DATA_HOME", xdgDir)
	t.Chdir(cfgDir)

	if err := os.WriteFile(
		filepath.Join(cfgDir, "config.toml"),
		[]byte("[workspace]\npath = \"\"\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	path, _, ok := getWorkspaceField(t, cfg)
	if !ok {
		return
	}
	want := filepath.Join(xdgDir, "workspaces")
	if path != want {
		t.Errorf("Workspace.Path = %q, want %q", path, want)
	}
}

// 05-REQ-1.E4: When workers is set to 0 in config.toml, Load() overrides
// Workers to 4. (Also validates 05-PROP-5: Workers is always >= 1 after Load.)
func TestWorkspaceConfig_WorkersZeroOverriddenTo4(t *testing.T) {
	cfg, err := loadWithTOML(t, "[workspace]\nworkers = 0\n")
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	_, workers, ok := getWorkspaceField(t, cfg)
	if !ok {
		return
	}
	if workers != 4 {
		t.Errorf("Workspace.Workers = %d, want 4 (default for zero)", workers)
	}
	if workers < 1 {
		t.Errorf("Workspace.Workers = %d, violates invariant Workers >= 1 (05-PROP-5)", workers)
	}
}

// 05-REQ-1.3 (path with directory component): When path has a directory
// component, it is used as-is even when XDG_DATA_HOME is set.
func TestWorkspaceConfig_DirectoryPath_IgnoresXDG(t *testing.T) {
	xdgDir := t.TempDir()
	cfgDir := t.TempDir()
	clearXDGVars(t)
	t.Setenv("XDG_DATA_HOME", xdgDir)
	t.Chdir(cfgDir)

	if err := os.WriteFile(
		filepath.Join(cfgDir, "config.toml"),
		[]byte("[workspace]\npath = \"./workspace\"\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	path, _, ok := getWorkspaceField(t, cfg)
	if !ok {
		return
	}
	// Path with directory component should be used as-is, not combined with XDG_DATA_HOME.
	if path != "./workspace" {
		t.Errorf("Workspace.Path = %q, want %q (directory component path used as-is)", path, "./workspace")
	}
}

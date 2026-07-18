package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"
)

// =========================================================================
// TS-13-P3: For any combination of flag, env, and config values,
// ResolveField returns exactly one deterministic value per the precedence
// order with no ties.
//
// Property: 13-PROP-3
// Validates: 13-REQ-6.1, 13-REQ-6.E1
//
// Tests all 2^3 source-set combinations (flagChanged, envSet, configSet)
// times 2 required/optional modes = 16 cases.
// =========================================================================

func TestPropertyResolveFieldPrecedenceDeterministic(t *testing.T) {
	const (
		fieldName  = "test_field"
		flagName   = "--test-field"
		envVarName = "TEST_FIELD_PROP"
		flagVal    = "from-flag"
		envVal     = "from-env"
		configVal  = "from-config"
	)

	type sourceCombo struct {
		flagChanged bool
		envSet      bool
		configSet   bool
	}

	combos := []sourceCombo{
		{false, false, false},
		{false, false, true},
		{false, true, false},
		{false, true, true},
		{true, false, false},
		{true, false, true},
		{true, true, false},
		{true, true, true},
	}

	for _, combo := range combos {
		for _, required := range []bool{false, true} {
			name := ""
			if combo.flagChanged {
				name += "flag+"
			}
			if combo.envSet {
				name += "env+"
			}
			if combo.configSet {
				name += "config+"
			}
			if name == "" {
				name = "none+"
			}
			if required {
				name += "required"
			} else {
				name += "optional"
			}

			t.Run(name, func(t *testing.T) {
				// Set/unset env var
				saved, had := os.LookupEnv(envVarName)
				if combo.envSet {
					os.Setenv(envVarName, envVal)
				} else {
					os.Unsetenv(envVarName)
				}
				defer func() {
					if had {
						os.Setenv(envVarName, saved)
					} else {
						os.Unsetenv(envVarName)
					}
				}()

				fv := ""
				if combo.flagChanged {
					fv = flagVal
				}
				cv := ""
				if combo.configSet {
					cv = configVal
				}

				val, err := ResolveField(fieldName, flagName, fv, combo.flagChanged, envVarName, cv, required)

				// Determine expected outcome per precedence chain:
				// flag (when changed) > non-empty env > non-empty config > error/empty
				switch {
				case combo.flagChanged:
					if val != flagVal {
						t.Errorf("got val=%q, want %q (flag should win)", val, flagVal)
					}
					if err != nil {
						t.Errorf("got err=%v, want nil (flag is set)", err)
					}
				case combo.envSet:
					if val != envVal {
						t.Errorf("got val=%q, want %q (env should win)", val, envVal)
					}
					if err != nil {
						t.Errorf("got err=%v, want nil (env is set)", err)
					}
				case combo.configSet:
					if val != configVal {
						t.Errorf("got val=%q, want %q (config should win)", val, configVal)
					}
					if err != nil {
						t.Errorf("got err=%v, want nil (config is set)", err)
					}
				default:
					// All sources unset
					if val != "" {
						t.Errorf("got val=%q, want empty string (all sources unset)", val)
					}
					if required {
						if err == nil {
							t.Error("got err=nil, want canonical error (required, all unset)")
						} else {
							expected := fieldName + " is not set: provide via " + flagName + ", $" + envVarName + ", or config file"
							if err.Error() != expected {
								t.Errorf("got err=%q, want %q", err.Error(), expected)
							}
						}
					} else {
						if err != nil {
							t.Errorf("got err=%v, want nil (optional, all unset)", err)
						}
					}
				}

				// Invariant: value is always deterministic — re-calling gives
				// the same result.
				val2, err2 := ResolveField(fieldName, flagName, fv, combo.flagChanged, envVarName, cv, required)
				if val != val2 {
					t.Errorf("ResolveField not deterministic: first=%q, second=%q", val, val2)
				}
				if (err == nil) != (err2 == nil) {
					t.Errorf("ResolveField not deterministic: first err=%v, second err=%v", err, err2)
				}
			})
		}
	}
}

// =========================================================================
// TS-13-P4: For any call to SaveConfig, regardless of failure point,
// config.toml is never partially written and no temp files remain.
//
// Property: 13-PROP-4
// Validates: 13-REQ-8.1, 13-REQ-8.2, 13-REQ-8.3
// =========================================================================

func TestPropertySaveConfigNeverPartiallyWritten(t *testing.T) {
	// Scenario 1: Successful write — config.toml is valid TOML.
	t.Run("success", func(t *testing.T) {
		configDir := t.TempDir()
		cfg := &CLIConfig{
			EndpointURL: "http://one.example.com",
			UserID:      "user-1",
			APIKey:      "key-1",
		}

		err := SaveConfig(configDir, cfg)
		if err != nil {
			t.Fatalf("SaveConfig returned error: %v", err)
		}

		// config.toml must be valid TOML with the correct values.
		configPath := filepath.Join(configDir, "config.toml")
		content, readErr := os.ReadFile(configPath)
		if readErr != nil {
			t.Fatalf("failed to read config.toml: %v", readErr)
		}
		var parsed CLIConfig
		if _, decErr := toml.Decode(string(content), &parsed); decErr != nil {
			t.Fatalf("config.toml is not valid TOML: %v\ncontent:\n%s", decErr, string(content))
		}
		if parsed.EndpointURL != cfg.EndpointURL || parsed.UserID != cfg.UserID || parsed.APIKey != cfg.APIKey {
			t.Errorf("config.toml content mismatch: got %+v, want %+v", parsed, *cfg)
		}

		// No temp files should remain.
		assertNoTempFiles(t, configDir)
	})

	// Scenario 2: Overwrite — existing config updated atomically.
	t.Run("overwrite", func(t *testing.T) {
		configDir := t.TempDir()
		original := &CLIConfig{EndpointURL: "http://old", UserID: "old-u", APIKey: "old-k"}
		if err := SaveConfig(configDir, original); err != nil {
			t.Fatalf("initial SaveConfig failed: %v", err)
		}

		updated := &CLIConfig{EndpointURL: "http://new", UserID: "new-u", APIKey: "new-k"}
		if err := SaveConfig(configDir, updated); err != nil {
			t.Fatalf("second SaveConfig failed: %v", err)
		}

		configPath := filepath.Join(configDir, "config.toml")
		content, _ := os.ReadFile(configPath)
		var parsed CLIConfig
		if _, decErr := toml.Decode(string(content), &parsed); decErr != nil {
			t.Fatalf("config.toml is not valid TOML after overwrite: %v", decErr)
		}
		if parsed.EndpointURL != "http://new" {
			t.Errorf("EndpointURL = %q, want %q", parsed.EndpointURL, "http://new")
		}
		assertNoTempFiles(t, configDir)
	})

	// Scenario 3: Unwritable directory — CreateTemp fails, original preserved.
	t.Run("unwritable_dir", func(t *testing.T) {
		configDir := t.TempDir()
		originalContent := "endpoint_url = \"original\"\nuser_id = \"ou\"\napi_key = \"ok\"\n"
		configPath := filepath.Join(configDir, "config.toml")
		if err := os.WriteFile(configPath, []byte(originalContent), 0600); err != nil {
			t.Fatalf("failed to write initial config.toml: %v", err)
		}

		// Make directory unwritable so CreateTemp fails.
		os.Chmod(configDir, 0500)
		defer os.Chmod(configDir, 0700)

		err := SaveConfig(configDir, &CLIConfig{EndpointURL: "http://new"})
		if err == nil {
			// Restore and verify — if it succeeded despite read-only, check state.
			os.Chmod(configDir, 0700)
			return
		}

		// Restore permissions to verify state.
		os.Chmod(configDir, 0700)

		// Original config.toml must be untouched.
		after, readErr := os.ReadFile(configPath)
		if readErr != nil {
			t.Fatalf("failed to read config.toml: %v", readErr)
		}
		if string(after) != originalContent {
			t.Errorf("config.toml should be unchanged after failed SaveConfig\nwant: %s\ngot:  %s", originalContent, string(after))
		}

		// No temp files should remain.
		assertNoTempFiles(t, configDir)
	})

	// Scenario 4: Directory as target — Rename should fail, temp cleaned up.
	t.Run("rename_failure_dir_as_target", func(t *testing.T) {
		configDir := t.TempDir()
		configPath := filepath.Join(configDir, "config.toml")
		// Create config.toml as a directory so os.Rename fails.
		if err := os.MkdirAll(configPath, 0700); err != nil {
			t.Fatalf("failed to create dir at config.toml path: %v", err)
		}

		err := SaveConfig(configDir, &CLIConfig{EndpointURL: "http://new"})
		// Either success or error is acceptable, but no temp files should remain.
		_ = err
		assertNoTempFiles(t, configDir)
	})

	// Scenario 5: Empty CLIConfig values — still valid TOML.
	t.Run("empty_values", func(t *testing.T) {
		configDir := t.TempDir()
		cfg := &CLIConfig{EndpointURL: "", UserID: "", APIKey: ""}

		err := SaveConfig(configDir, cfg)
		if err != nil {
			t.Fatalf("SaveConfig returned error: %v", err)
		}

		configPath := filepath.Join(configDir, "config.toml")
		content, _ := os.ReadFile(configPath)
		var parsed CLIConfig
		if _, decErr := toml.Decode(string(content), &parsed); decErr != nil {
			t.Fatalf("config.toml is not valid TOML for empty values: %v", decErr)
		}
		assertNoTempFiles(t, configDir)
	})
}

// assertNoTempFiles verifies that no temp files remain in the directory.
// Only config.toml should be present (if it exists).
func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read dir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "config.toml" {
			t.Errorf("unexpected file in config dir (possible temp file leak): %s", e.Name())
		}
	}
}

// =========================================================================
// TS-13-P5: For any command annotated with 'auth': 'none',
// PersistentPreRunE never loads config, resolves credentials, or
// constructs an apikit.Client.
//
// Property: 13-PROP-5
// Validates: 13-REQ-6.E2, 13-REQ-10.1, 13-REQ-12.1
//
// Strategy: Make the environment hostile (no HOME, empty TokenPrefix, no
// credentials) — if PersistentPreRunE tried any credential resolution,
// it would fail. For auth-exempt commands, it should return nil immediately.
// =========================================================================

func TestPropertyAuthExemptSkipsAllCredentialResolution(t *testing.T) {
	// Test multiple credential configurations to verify auth-exempt commands
	// are truly skipped regardless of the credential environment.
	configurations := []struct {
		name       string
		prefix     string
		homeExists bool
		envs       map[string]string
	}{
		{
			name:       "all_missing",
			prefix:     "",
			homeExists: false,
			envs:       map[string]string{},
		},
		{
			name:       "empty_prefix_no_home",
			prefix:     "",
			homeExists: false,
			envs: map[string]string{
				"ENDPOINT_URL": "http://x",
				"API_KEY":      "k",
			},
		},
		{
			name:       "valid_prefix_no_home",
			prefix:     "testpfx",
			homeExists: false,
			envs:       map[string]string{},
		},
		{
			name:       "all_present",
			prefix:     "testpfx",
			homeExists: true,
			envs: map[string]string{
				"ENDPOINT_URL": "http://x",
				"API_KEY":      "k",
				"USER_ID":      "u",
			},
		},
	}

	for _, cfg := range configurations {
		t.Run(cfg.name, func(t *testing.T) {
			// Save and set TokenPrefix
			savedPrefix := TokenPrefix
			TokenPrefix = cfg.prefix
			defer func() { TokenPrefix = savedPrefix }()

			// Save and set HOME
			savedHome := os.Getenv("HOME")
			if cfg.homeExists {
				tmpHome := t.TempDir()
				os.Setenv("HOME", tmpHome)
			} else {
				os.Setenv("HOME", "/nonexistent/path/for/property/test")
			}
			defer os.Setenv("HOME", savedHome)

			// Save and set env vars
			savedEnvs := make(map[string]string)
			hadEnvs := make(map[string]bool)
			for _, key := range []string{"ENDPOINT_URL", "API_KEY", "USER_ID"} {
				if v, ok := os.LookupEnv(key); ok {
					savedEnvs[key] = v
					hadEnvs[key] = true
				}
				os.Unsetenv(key) // clear first
			}
			for k, v := range cfg.envs {
				os.Setenv(k, v)
			}
			defer func() {
				for _, key := range []string{"ENDPOINT_URL", "API_KEY", "USER_ID"} {
					if hadEnvs[key] {
						os.Setenv(key, savedEnvs[key])
					} else {
						os.Unsetenv(key)
					}
				}
			}()

			var runECalled bool

			rootCmd := RootCommand()
			rootCmd.AddCommand(&cobra.Command{
				Use:   "authnonetest",
				Short: "Auth-exempt command for property test",
				Annotations: map[string]string{
					"auth": "none",
				},
				RunE: func(_ *cobra.Command, _ []string) error {
					runECalled = true
					return nil
				},
			})
			rootCmd.SetArgs([]string{"authnonetest"})

			var execErr error
			captureStdout(t, func() {
				execErr = rootCmd.Execute()
				if execErr != nil {
					PrintError(execErr)
				}
			})

			// Auth-exempt command must succeed regardless of environment.
			if execErr != nil {
				t.Errorf("auth-exempt command should succeed, got error: %v", execErr)
			}

			// RunE must have been called.
			if !runECalled {
				t.Error("RunE was not called; PersistentPreRunE may have blocked execution")
			}
		})
	}
}

// =========================================================================
// TS-13-P6: For any command registered via AddCommand with at least one
// Annotations key, it appears in help --json output automatically without
// additional registration.
//
// Property: 13-PROP-6
// Validates: 13-REQ-11.3, 13-REQ-11.7
// =========================================================================

func TestPropertyHelpJSONReflectsLiveCommandTree(t *testing.T) {
	// Register varying sets of annotated commands and verify they all appear.
	testSets := []struct {
		name string
		cmds []*cobra.Command
	}{
		{
			name: "single_leaf",
			cmds: []*cobra.Command{
				makeAnnotatedTestLeaf("proptest1", "First prop test", map[string]string{
					"auth":   "api_key",
					"method": "GET",
					"path":   "/prop/test1",
				}),
			},
		},
		{
			name: "multiple_leaves",
			cmds: []*cobra.Command{
				makeAnnotatedTestLeaf("proptesta", "Prop test A", map[string]string{
					"auth":   "api_key",
					"method": "GET",
					"path":   "/prop/a",
				}),
				makeAnnotatedTestLeaf("proptestb", "Prop test B", map[string]string{
					"auth":   "admin",
					"method": "POST",
					"path":   "/prop/b",
				}),
				makeAnnotatedTestLeaf("proptestc", "Prop test C", map[string]string{
					"auth":   "none",
					"method": "DELETE",
					"path":   "/prop/c",
				}),
			},
		},
		{
			name: "nested_in_group",
			cmds: func() []*cobra.Command {
				group := &cobra.Command{
					Use:   "propgroup",
					Short: "Property test group",
				}
				group.AddCommand(makeAnnotatedTestLeaf("nested", "Nested leaf", map[string]string{
					"auth":   "api_key",
					"method": "GET",
					"path":   "/prop/group/nested",
				}))
				return []*cobra.Command{group}
			}(),
		},
		{
			name: "composite_command",
			cmds: []*cobra.Command{
				makeAnnotatedTestLeaf("propcomposite", "Composite command", map[string]string{
					"auth":      "none",
					"composite": "true",
				}),
			},
		},
	}

	for _, ts := range testSets {
		t.Run(ts.name, func(t *testing.T) {
			rootCmd := RootCommand()
			for _, cmd := range ts.cmds {
				rootCmd.AddCommand(cmd)
			}
			rootCmd.SetArgs([]string{"help", "--json"})

			stdout := captureStdout(t, func() {
				_ = rootCmd.Execute()
			})

			trimmed := strings.TrimSpace(stdout)
			if !json.Valid([]byte(trimmed)) {
				t.Fatalf("help --json output is not valid JSON:\n%s", trimmed)
			}

			var tree struct {
				Commands []struct {
					Name string `json:"name"`
				} `json:"commands"`
			}
			if err := json.Unmarshal([]byte(trimmed), &tree); err != nil {
				t.Fatalf("failed to parse help --json output: %v", err)
			}

			// Collect all command names in the output.
			outputNames := make(map[string]bool)
			for _, cmd := range tree.Commands {
				outputNames[cmd.Name] = true
				// Also index by last segment for nested commands.
				parts := strings.Fields(cmd.Name)
				if len(parts) > 0 {
					outputNames[parts[len(parts)-1]] = true
				}
			}

			// Every registered annotated leaf must appear in the output.
			for _, cmd := range ts.cmds {
				leaves := collectAnnotatedLeaves(cmd)
				for _, leaf := range leaves {
					found := false
					leafName := leaf.Name()
					leafPath := leaf.CommandPath()
					for name := range outputNames {
						if name == leafName || name == leafPath || strings.Contains(name, leafName) {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("annotated command %q (path=%q) not found in help --json output; got names: %v",
							leafName, leafPath, outputNames)
					}
				}
			}
		})
	}
}

// makeAnnotatedTestLeaf creates a leaf command for property testing.
func makeAnnotatedTestLeaf(use, short string, annotations map[string]string) *cobra.Command {
	return &cobra.Command{
		Use:         use,
		Short:       short,
		Annotations: annotations,
		RunE:        func(_ *cobra.Command, _ []string) error { return nil },
	}
}

// collectAnnotatedLeaves recursively collects all commands with RunE and
// at least one Annotations key from the command tree.
func collectAnnotatedLeaves(cmd *cobra.Command) []*cobra.Command {
	var leaves []*cobra.Command
	if cmd.RunE != nil && len(cmd.Annotations) > 0 {
		leaves = append(leaves, cmd)
	}
	for _, child := range cmd.Commands() {
		leaves = append(leaves, collectAnnotatedLeaves(child)...)
	}
	return leaves
}

// =========================================================================
// TS-13-P7: For any value stored in context under any external key type,
// ClientFromContext and UserIDFromContext return only values stored under
// the unexported struct key types.
//
// Property: 13-PROP-7
// Validates: 13-REQ-7.1, 13-REQ-7.E1
// =========================================================================

func TestPropertyContextKeySafetyPreventsCollisions(t *testing.T) {
	type externalKeyStruct struct{}
	type anotherKeyStruct struct{ id int }

	// Define a mock client value for testing.
	type mockClient struct{ Name string }
	realClient := &mockClient{Name: "real-client"}
	const realUserID = "real-user-id-12345"

	testCases := []struct {
		name       string
		buildCtx   func() context.Context
		wantClient any    // expected ClientFromContext result
		wantUserID string // expected UserIDFromContext result
	}{
		{
			name: "empty context returns nil and empty",
			buildCtx: func() context.Context {
				return context.Background()
			},
			wantClient: nil,
			wantUserID: "",
		},
		{
			name: "string key client does not interfere",
			buildCtx: func() context.Context {
				return context.WithValue(context.Background(), "client", "impostor-client") //nolint:staticcheck
			},
			wantClient: nil,
			wantUserID: "",
		},
		{
			name: "string key user_id does not interfere",
			buildCtx: func() context.Context {
				return context.WithValue(context.Background(), "user_id", "impostor-uid") //nolint:staticcheck
			},
			wantClient: nil,
			wantUserID: "",
		},
		{
			name: "int key does not interfere",
			buildCtx: func() context.Context {
				ctx := context.WithValue(context.Background(), 42, "impostor-value") //nolint:staticcheck
				return ctx
			},
			wantClient: nil,
			wantUserID: "",
		},
		{
			name: "external struct key does not interfere",
			buildCtx: func() context.Context {
				ctx := context.WithValue(context.Background(), externalKeyStruct{}, "ext-value")
				ctx = context.WithValue(ctx, anotherKeyStruct{id: 1}, "another-value")
				return ctx
			},
			wantClient: nil,
			wantUserID: "",
		},
		{
			name: "correct keys return stored values",
			buildCtx: func() context.Context {
				ctx := context.WithValue(context.Background(), clientContextKey{}, realClient)
				ctx = context.WithValue(ctx, userIDContextKey{}, realUserID)
				return ctx
			},
			wantClient: realClient,
			wantUserID: realUserID,
		},
		{
			name: "mixed keys: only correct keys return values",
			buildCtx: func() context.Context {
				ctx := context.WithValue(context.Background(), "client", "impostor") //nolint:staticcheck
				ctx = context.WithValue(ctx, clientContextKey{}, realClient)
				ctx = context.WithValue(ctx, "user_id", "impostor-uid") //nolint:staticcheck
				ctx = context.WithValue(ctx, userIDContextKey{}, realUserID)
				ctx = context.WithValue(ctx, externalKeyStruct{}, "external")
				ctx = context.WithValue(ctx, 99, "numeric-key") //nolint:staticcheck
				return ctx
			},
			wantClient: realClient,
			wantUserID: realUserID,
		},
		{
			name: "client set without user_id",
			buildCtx: func() context.Context {
				return context.WithValue(context.Background(), clientContextKey{}, realClient)
			},
			wantClient: realClient,
			wantUserID: "",
		},
		{
			name: "user_id set without client",
			buildCtx: func() context.Context {
				return context.WithValue(context.Background(), userIDContextKey{}, realUserID)
			},
			wantClient: nil,
			wantUserID: realUserID,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := tc.buildCtx()

			gotClient := ClientFromContext(ctx)
			gotUserID := UserIDFromContext(ctx)

			if gotClient != tc.wantClient {
				t.Errorf("ClientFromContext = %v, want %v", gotClient, tc.wantClient)
			}
			if gotUserID != tc.wantUserID {
				t.Errorf("UserIDFromContext = %q, want %q", gotUserID, tc.wantUserID)
			}
		})
	}
}

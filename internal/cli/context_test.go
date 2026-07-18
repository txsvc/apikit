package cli

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"unsafe"

	"github.com/spf13/cobra"
)

// =========================================================================
// TS-13-27: context.go defines unexported zero-size struct types
// clientContextKey and userIDContextKey as context keys.
// Requirement: 13-REQ-7.1
// =========================================================================

func TestContextKeyTypesAreZeroSize(t *testing.T) {
	// Verify both types are zero-size structs (struct{})
	if size := unsafe.Sizeof(clientContextKey{}); size != 0 {
		t.Errorf("clientContextKey should be zero-size, got %d bytes", size)
	}
	if size := unsafe.Sizeof(userIDContextKey{}); size != 0 {
		t.Errorf("userIDContextKey should be zero-size, got %d bytes", size)
	}

	// Verify both types are unexported (names start with lowercase)
	clientKeyType := reflect.TypeFor[clientContextKey]()
	if name := clientKeyType.Name(); len(name) == 0 || (name[0] >= 'A' && name[0] <= 'Z') {
		t.Errorf("clientContextKey should be unexported, got type name %q", name)
	}
	userIDKeyType := reflect.TypeFor[userIDContextKey]()
	if name := userIDKeyType.Name(); len(name) == 0 || (name[0] >= 'A' && name[0] <= 'Z') {
		t.Errorf("userIDContextKey should be unexported, got type name %q", name)
	}

	// Verify both are struct types
	if clientKeyType.Kind() != reflect.Struct {
		t.Errorf("clientContextKey should be a struct, got %v", clientKeyType.Kind())
	}
	if userIDKeyType.Kind() != reflect.Struct {
		t.Errorf("userIDContextKey should be a struct, got %v", userIDKeyType.Kind())
	}

	// Note: The fact that this test compiles (accessing unexported types from
	// within the package) while no external package can construct these types
	// proves the context key safety property (13-REQ-7.E1).
}

// =========================================================================
// TS-13-28: ClientFromContext returns nil when called with a plain context
// that has no stored client.
// Requirement: 13-REQ-7.2
// =========================================================================

func TestClientFromContextReturnsNilOnPlainContext(t *testing.T) {
	client := ClientFromContext(context.Background())
	if client != nil {
		t.Errorf("ClientFromContext(context.Background()) should return nil, got %v", client)
	}
}

// =========================================================================
// TS-13-29: ClientFromContext returns the stored *apikit.Client when one
// has been stored via context.WithValue using clientContextKey{}.
// Requirement: 13-REQ-7.2
// =========================================================================

func TestClientFromContextReturnsStoredClient(t *testing.T) {
	// Use a stand-in value since internal/cli cannot import apikit due to
	// import cycle. In production, this would be *apikit.Client.
	type mockClient struct{ Name string }
	mc := &mockClient{Name: "test-client"}
	ctx := context.WithValue(context.Background(), clientContextKey{}, mc)

	result := ClientFromContext(ctx)
	if result == nil {
		t.Fatal("ClientFromContext should return non-nil when a client is stored")
	}
	if result != mc {
		t.Errorf("ClientFromContext should return the exact same client that was stored, got %v", result)
	}
}

// =========================================================================
// TS-13-30: UserIDFromContext returns empty string when called with a
// plain context.
// Requirement: 13-REQ-7.3
// =========================================================================

func TestUserIDFromContextReturnsEmptyOnPlainContext(t *testing.T) {
	uid := UserIDFromContext(context.Background())
	if uid != "" {
		t.Errorf("UserIDFromContext(context.Background()) should return empty string, got %q", uid)
	}
}

// =========================================================================
// TS-13-31: UserIDFromContext returns the stored user_id string when set
// in context.
// Requirement: 13-REQ-7.3
// =========================================================================

func TestUserIDFromContextReturnsStoredString(t *testing.T) {
	ctx := context.WithValue(context.Background(), userIDContextKey{}, "user-123")
	uid := UserIDFromContext(ctx)
	if uid != "user-123" {
		t.Errorf("UserIDFromContext should return %q, got %q", "user-123", uid)
	}
}

// =========================================================================
// TS-13-32: PersistentPreRunE stores *apikit.Client and user_id in context
// via context.WithValue; user_id is not passed to apikit.NewClient.
// Requirement: 13-REQ-7.4
// =========================================================================

func TestPersistentPreRunEStoresClientAndUserIDInContext(t *testing.T) {
	// Set up all credentials via env
	envVars := map[string]string{
		"ENDPOINT_URL": "http://server",
		"API_KEY":      "k",
		"USER_ID":      "u123",
	}
	savedEnv := make(map[string]string)
	hadEnv := make(map[string]bool)
	for k := range envVars {
		if v, ok := os.LookupEnv(k); ok {
			savedEnv[k] = v
			hadEnv[k] = true
		}
		os.Setenv(k, envVars[k])
	}
	defer func() {
		for k := range envVars {
			if hadEnv[k] {
				os.Setenv(k, savedEnv[k])
			} else {
				os.Unsetenv(k)
			}
		}
	}()

	// Set TokenPrefix and HOME
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

	var capturedCtx context.Context

	rootCmd := RootCommand()
	rootCmd.AddCommand(&cobra.Command{
		Use:   "ctxcmd",
		Short: "Test command for context verification",
		Annotations: map[string]string{
			"auth": "api_key",
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			capturedCtx = cmd.Context()
			return nil
		},
	})
	rootCmd.SetArgs([]string{"ctxcmd"})

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

	if capturedCtx == nil {
		t.Fatal("RunE was not called; capturedCtx is nil")
	}

	// ClientFromContext should return non-nil when credentials are resolved
	if client := ClientFromContext(capturedCtx); client == nil {
		t.Error("ClientFromContext should return non-nil client when credentials are resolved")
	}

	// UserIDFromContext should return the resolved user_id
	if uid := UserIDFromContext(capturedCtx); uid != "u123" {
		t.Errorf("UserIDFromContext should return %q, got %q", "u123", uid)
	}
}

// =========================================================================
// TS-13-E10: Consuming project string key "client" does not interfere
// with ClientFromContext.
// Requirement: 13-REQ-7.E1
// =========================================================================

func TestClientFromContextIgnoresConsumingProjectStringKey(t *testing.T) {
	// Store a value under the string key "client" — this simulates a
	// consuming project using its own context key.
	ctx := context.WithValue(context.Background(), "client", "external-client-value") //nolint:staticcheck

	// ClientFromContext should return nil, not the string-keyed value,
	// because the unexported struct key type prevents any collision.
	result := ClientFromContext(ctx)
	if result != nil {
		t.Errorf("ClientFromContext should return nil when only string key 'client' is set, got %v", result)
	}

	// Also verify that when BOTH keys are present, ClientFromContext
	// returns the correct value (the one under clientContextKey{}).
	type mockClient struct{ Name string }
	mc := &mockClient{Name: "real-client"}
	ctx = context.WithValue(ctx, clientContextKey{}, mc)

	result = ClientFromContext(ctx)
	if result != mc {
		t.Errorf("ClientFromContext should return the real client stored under clientContextKey{}, got %v", result)
	}
}

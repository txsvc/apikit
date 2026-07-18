package cli

import (
	"github.com/spf13/cobra"
)

// CLIConfig represents the three-field TOML config file.
// Stub — will be implemented in task group 10.
type CLIConfig struct {
	EndpointURL string `toml:"endpoint_url"`
	UserID      string `toml:"user_id"`
	APIKey      string `toml:"api_key"`
}

// InitConfig creates the config directory and initial config.toml if needed.
// Stub — will be implemented in task group 10.
func InitConfig(_ string) error {
	return nil
}

// LoadConfig reads and parses config.toml into a CLIConfig struct.
// Stub — will be implemented in task group 10.
func LoadConfig(_ string) (*CLIConfig, error) {
	return nil, nil
}

// SaveConfig atomically writes an updated CLIConfig to config.toml.
// Stub — will be implemented in task group 10.
func SaveConfig(_ string, _ *CLIConfig) error {
	return nil
}

// ResolveField resolves a single credential field using the four-level
// precedence chain: CLI flag (when flagChanged) > non-empty env var >
// non-empty config value > error (required) or ("", nil) (optional).
// Stub — will be implemented in task group 10.
func ResolveField(_, _, _ string, _ bool, _, _ string, _ bool) (string, error) {
	return "", nil
}

// ResolveEndpointURL is a package-level helper for auth-exempt commands
// that need to optionally resolve endpoint_url without going through
// PersistentPreRunE. It reads the persistent --endpoint-url flag from
// cmd.Root().PersistentFlags(), loads config.toml, and calls ResolveField
// with required: false.
// Stub — will be implemented in task group 10.
func ResolveEndpointURL(_ *cobra.Command) string {
	return ""
}

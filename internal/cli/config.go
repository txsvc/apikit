package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"
)

// CLIConfig represents the three-field TOML config file.
type CLIConfig struct {
	EndpointURL string `toml:"endpoint_url"`
	UserID      string `toml:"user_id"`
	APIKey      string `toml:"api_key"`
}

// configTemplate is the hard-coded template for initial config.toml,
// ensuring all three keys appear in the file regardless of TOML encoder behavior.
const configTemplate = `endpoint_url = ""
user_id = ""
api_key = ""
`

// InitConfig creates the config directory and initial config.toml if they
// do not exist. The directory is created with mode 0700 and the file with
// mode 0600.
func InitConfig(configDir string) error {
	// Create config directory with mode 0700.
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return err
	}

	configPath := filepath.Join(configDir, "config.toml")

	// Only create config.toml if it does not already exist.
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := os.WriteFile(configPath, []byte(configTemplate), 0600); err != nil {
			return err
		}
	}

	return nil
}

// LoadConfig reads and parses config.toml into a CLIConfig struct.
// Returns (nil, error) with a descriptive message if the file is unparseable.
func LoadConfig(configDir string) (*CLIConfig, error) {
	configPath := filepath.Join(configDir, "config.toml")

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var cfg CLIConfig
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return nil, fmt.Errorf("config file is unparseable: %s", err.Error())
	}

	return &cfg, nil
}

// SaveConfig atomically writes an updated CLIConfig to config.toml.
// It creates a temp file in the same directory, writes the TOML content,
// sets permissions to 0600, and renames it into place.
// If any step fails, the original config.toml is untouched.
func SaveConfig(configDir string, cfg *CLIConfig) error {
	configPath := filepath.Join(configDir, "config.toml")

	// Create temp file in the same directory for atomic rename.
	tmpFile, err := os.CreateTemp(configDir, "config-*.toml.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())

	// Encode the config as TOML.
	if err := toml.NewEncoder(tmpFile).Encode(cfg); err != nil {
		tmpFile.Close()
		return err
	}

	// Set permissions before closing.
	if err := tmpFile.Chmod(0600); err != nil {
		tmpFile.Close()
		return err
	}

	if err := tmpFile.Close(); err != nil {
		return err
	}

	// Atomic rename into place.
	return os.Rename(tmpFile.Name(), configPath)
}

// ResolveField resolves a single credential field using the four-level
// precedence chain:
//  1. CLI flag (when flagChanged is true and flagValue is non-empty)
//  2. Non-empty environment variable (looked up via envVarName)
//  3. Non-empty config value
//  4. Error (required) or ("", nil) (optional)
//
// Parameters:
//   - fieldName: credential field name (e.g., "endpoint_url")
//   - flagName: CLI flag name (e.g., "--endpoint-url")
//   - flagValue: the value of the CLI flag
//   - flagChanged: whether the CLI flag was explicitly set
//   - envVarName: the environment variable name (e.g., "ENDPOINT_URL")
//   - configValue: the value from config.toml
//   - required: whether the field is required
func ResolveField(fieldName, flagName, flagValue string, flagChanged bool, envVarName, configValue string, required bool) (string, error) {
	// 1. Flag takes highest precedence when explicitly changed.
	if flagChanged && flagValue != "" {
		return flagValue, nil
	}

	// 2. Environment variable.
	if envVal := os.Getenv(envVarName); envVal != "" {
		return envVal, nil
	}

	// 3. Config value (empty string treated as unset).
	if configValue != "" {
		return configValue, nil
	}

	// 4. All sources exhausted.
	if required {
		return "", fmt.Errorf("%s is not set: provide via %s, $%s, or config file",
			fieldName, flagName, envVarName)
	}

	return "", nil
}

// ResolveEndpointURL is a package-level helper for auth-exempt commands
// that need to optionally resolve endpoint_url without going through
// PersistentPreRunE. It reads the persistent --endpoint-url flag from
// cmd.Root().PersistentFlags(), checks the environment, loads config,
// and calls ResolveField with required: false.
func ResolveEndpointURL(cmd *cobra.Command) string {
	flags := cmd.Root().PersistentFlags()

	flagValue, _ := flags.GetString("endpoint-url")
	flagChanged := flags.Changed("endpoint-url")

	// Try to load config for the config value.
	var configValue string
	if TokenPrefix != "" {
		home, err := os.UserHomeDir()
		if err == nil && home != "" {
			configDir := filepath.Join(home, "."+TokenPrefix)
			if cfg, err := LoadConfig(configDir); err == nil && cfg != nil {
				configValue = cfg.EndpointURL
			}
		}
	}

	// Resolve with required: false — returns ("", nil) when all sources are empty.
	val, _ := ResolveField("endpoint_url", "--endpoint-url", flagValue, flagChanged, "ENDPOINT_URL", configValue, false)
	return val
}

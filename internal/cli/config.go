package cli

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

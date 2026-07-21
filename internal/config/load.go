package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// validLogLevels is the custom allowlist of accepted log level strings.
// Per 01-REQ-3.7 this is strictly the 7 canonical values — "warning" is
// excluded even though logrus.ParseLevel accepts it as an alias for "warn".
var validLogLevels = map[string]bool{
	"trace": true,
	"debug": true,
	"info":  true,
	"warn":  true,
	"error": true,
	"fatal": true,
	"panic": true,
}

// bodyRe matches a positive integer followed immediately by KB, MB, or GB (case-insensitive).
var bodyRe = regexp.MustCompile(`^([1-9][0-9]*)(KB|MB|GB)$`)

// Load reads config.toml, applies defaults, validates fields, and returns
// a populated *Config. The config file path is resolved as follows:
//
//   - If XDG_CONFIG_HOME is set, exclusively use $XDG_CONFIG_HOME/config.toml.
//   - Otherwise, use ./config.toml in the current working directory.
//
// When the resolved config file is absent, all defaults are applied and
// (*Config, nil) is returned. When the file exists but contains invalid
// TOML, (nil, error) is returned.
//
// Load performs no filesystem side effects beyond reading the config file.
func Load() (*Config, error) {
	cfg := applyDefaults()

	// Resolve config file path.
	cfgPath := resolveConfigPath()

	// Attempt to read the config file.
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			// File absent: return defaults.
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	// Expand $VAR and ${VAR} references before parsing so that secrets
	// (e.g. OAuth client_secret) can be injected from the environment.
	expanded := os.ExpandEnv(string(data))

	// Parse TOML into config.
	if _, err := toml.Decode(expanded, cfg); err != nil {
		return nil, fmt.Errorf("parsing config.toml: %w", err)
	}

	// Apply defaults for fields that remain at zero/empty after parsing.
	applyFieldDefaults(cfg)

	// Validate all fields.
	if err := validate(cfg); err != nil {
		return nil, err
	}

	// Validate OAuth provider entries.
	if err := validateOAuthProviders(cfg.OAuth.Providers); err != nil {
		return nil, err
	}

	// Parse and store max_body_size.
	bytes, err := parseBodySize(cfg.Server.MaxBodySize)
	if err != nil {
		return nil, err
	}
	cfg.Server.maxBodyBytes = bytes

	// Resolve database path.
	cfg.Database.Path = resolveDataPath(cfg.Database.Path)

	return cfg, nil
}

// applyDefaults returns a *Config with all fields set to their documented
// default values.
func applyDefaults() *Config {
	cfg := &Config{}
	cfg.Server.Port = 8080
	cfg.Server.Bind = "0.0.0.0"
	cfg.Server.ExternalURL = ""
	cfg.Server.MountPoint = "/api/v1"
	cfg.Server.MaxBodySize = "1MB"
	cfg.Server.maxBodyBytes = 1048576 // 1MB
	cfg.Database.Path = resolveDataPath("")
	cfg.Logging.Level = "info"
	return cfg
}

// applyFieldDefaults fills in defaults for fields that are empty/zero after
// TOML parsing. This handles the case where a config file exists but omits
// certain keys.
func applyFieldDefaults(cfg *Config) {
	if cfg.Server.Port == 0 {
		// Port 0 is valid (ephemeral), but TOML returns 0 for absent int.
		// We cannot distinguish absent from explicit 0 here. However, when
		// port=0 is explicitly set in TOML, the parsed value is 0, and we
		// leave it as 0 (valid). When absent from TOML, the zero value of
		// int is 0, but we override with the default 8080 only if it was
		// NOT explicitly present in the TOML. Since toml.Decode pre-fills
		// the struct from applyDefaults() which sets Port=8080, an absent
		// port field keeps 8080. An explicit port=0 sets it to 0. This is
		// correct because we decode into the defaults struct.
	}
	if cfg.Server.Bind == "" {
		cfg.Server.Bind = "0.0.0.0"
	}
	if cfg.Server.MountPoint == "" {
		cfg.Server.MountPoint = "/api/v1"
	}
	if cfg.Server.MaxBodySize == "" {
		cfg.Server.MaxBodySize = "1MB"
	}
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
}

// validate checks all config fields for validity. Returns nil on success
// or a descriptive error.
func validate(cfg *Config) error {
	if err := validatePort(cfg.Server.Port); err != nil {
		return err
	}
	if err := validateLogLevel(cfg.Logging.Level); err != nil {
		return err
	}
	return nil
}

// validatePort checks that port is in the range 0–65535.
func validatePort(port int) error {
	if port < 0 || port > 65535 {
		return fmt.Errorf("invalid port %d: must be in range 0-65535", port)
	}
	return nil
}

// validateLogLevel checks that level is one of the 7 canonical log levels
// (case-insensitive). "warning" is NOT accepted per spec 01-REQ-3.7.
func validateLogLevel(level string) error {
	if validLogLevels[strings.ToLower(level)] {
		return nil
	}
	return fmt.Errorf(
		"invalid log level %q: must be one of trace, debug, info, warn, error, fatal, panic",
		level,
	)
}

// parseBodySize parses a size string like "1MB" into bytes.
// Accepted format: <positive-integer><KB|MB|GB> (case-insensitive, no spaces).
// An empty string is treated as absent and returns the default 1MB (1048576).
// Returns an error for invalid formats including zero, negative, unsupported
// suffixes, or spaces.
func parseBodySize(s string) (int64, error) {
	if s == "" {
		return 1048576, nil // default: 1MB
	}

	upper := strings.ToUpper(s)
	match := bodyRe.FindStringSubmatch(upper)
	if match == nil {
		return 0, fmt.Errorf("invalid max_body_size %q: must be a positive integer followed by KB, MB, or GB (e.g. '1MB', '512KB')", s)
	}

	n, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid max_body_size %q: %w", s, err)
	}

	var multiplier int64
	switch match[2] {
	case "KB":
		multiplier = 1024
	case "MB":
		multiplier = 1024 * 1024
	case "GB":
		multiplier = 1024 * 1024 * 1024
	}

	return n * multiplier, nil
}

// ConfigDir returns the directory where config.toml is located (or would be
// located), for resolving adjacent files like admin_token.
func ConfigDir() string {
	return filepath.Dir(resolveConfigPath())
}

// ConfigPath returns the resolved path to config.toml.
func ConfigPath() string {
	return resolveConfigPath()
}

// resolveConfigPath returns the path to config.toml, respecting XDG_CONFIG_HOME.
func resolveConfigPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "config.toml")
	}
	return "config.toml"
}

// resolveDataPath returns the database path.
//
// Resolution order:
//  1. If dbPath contains a directory component (e.g. "./name.db",
//     "/abs/path/name.db"), it is used as-is.
//  2. If dbPath is a bare filename (e.g. "name.db") and XDG_DATA_HOME
//     is set, the filename is placed under $XDG_DATA_HOME.
//  3. If dbPath is empty and XDG_DATA_HOME is set: $XDG_DATA_HOME/apikit.db.
//  4. If dbPath is empty and XDG_DATA_HOME is unset: ./data/apikit.db.
func resolveDataPath(dbPath string) string {
	xdg := os.Getenv("XDG_DATA_HOME")
	if dbPath != "" {
		if filepath.Base(dbPath) == dbPath && xdg != "" {
			return filepath.Join(xdg, dbPath)
		}
		return dbPath
	}
	if xdg != "" {
		return filepath.Join(xdg, "apikit.db")
	}
	return "./data/apikit.db"
}

// validateOAuthProviders validates the [[oauth.providers]] entries.
// Returns an error if any entry is missing required fields or if
// duplicate provider names are found.
func validateOAuthProviders(providers []ProviderConfig) error {
	seen := make(map[string]bool)
	for i, p := range providers {
		if p.Name == "" {
			return fmt.Errorf("oauth.providers[%d]: name is required", i)
		}
		if p.ClientID == "" {
			return fmt.Errorf("oauth.providers[%d] (%s): client_id is required", i, p.Name)
		}
		if p.ClientSecret == "" {
			return fmt.Errorf("oauth.providers[%d] (%s): client_secret is required", i, p.Name)
		}
		if seen[p.Name] {
			return fmt.Errorf("oauth.providers: duplicate provider name %q", p.Name)
		}
		seen[p.Name] = true
	}
	return nil
}

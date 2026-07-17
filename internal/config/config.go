// Package config implements configuration loading for apikit.
package config

// Config holds all server configuration.
type Config struct {
	Server   ServerConfig   `toml:"server"`
	Database DatabaseConfig `toml:"database"`
	Logging  LoggingConfig  `toml:"logging"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Port        int    `toml:"port"`
	Bind        string `toml:"bind"`
	ExternalURL string `toml:"external_url"`
	MountPoint  string `toml:"mount_point"`
	MaxBodySize string `toml:"max_body_size"`

	maxBodyBytes int64 // parsed during Load
}

// MaxBodyBytes returns the parsed body size limit in bytes.
func (s *ServerConfig) MaxBodyBytes() int64 {
	return s.maxBodyBytes
}

// DatabaseConfig holds database settings.
type DatabaseConfig struct {
	Path string `toml:"path"`
}

// LoggingConfig holds logging settings.
type LoggingConfig struct {
	Level string `toml:"level"`
}


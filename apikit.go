// Package apikit provides the foundational HTTP server infrastructure.
package apikit

import (
	"github.com/labstack/echo/v4"
	"github.com/txsvc/apikit/internal/config"
	"github.com/txsvc/apikit/internal/db"
	"github.com/txsvc/apikit/internal/keys"
	"github.com/txsvc/apikit/internal/oauth"
)

// Build-time configurable variables, overridable via -ldflags.
var (
	// Version holds the semantic version string.
	Version = "dev"
	// Build holds the short git commit SHA.
	Build = "dev"
	// TokenPrefix is the token namespace prefix.
	TokenPrefix = "ak"
)

// Config is a type alias for the internal config type.
// This allows consumers to use *apikit.Config without importing internal/config.
type Config = config.Config

// Provider is a type alias for the internal oauth.Provider interface.
// Consuming projects can implement custom providers without importing internal/oauth.
type Provider = oauth.Provider

// UserInfo is a type alias for the internal oauth.UserInfo struct.
// Consuming projects can use *apikit.UserInfo without importing internal/oauth.
type UserInfo = oauth.UserInfo

// APIKeyResult is a type alias for the internal keys.APIKeyResult struct.
// Consumers can use *apikit.APIKeyResult without importing internal/keys.
type APIKeyResult = keys.APIKeyResult

// LoadConfig loads the server configuration from config.toml,
// respecting XDG base directory conventions.
func LoadConfig() (*Config, error) {
	return config.Load()
}

// GenerateAPIKey creates a new API key for the given user, revoking any
// existing active keys. It delegates to keys.GenerateAPIKey.
func GenerateAPIKey(tx db.Executor, userID string, expiresDays int, logger echo.Logger) (*APIKeyResult, error) {
	return keys.GenerateAPIKey(tx, userID, expiresDays, logger)
}

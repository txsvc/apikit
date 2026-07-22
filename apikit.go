// Package apikit provides the foundational HTTP server infrastructure.
package apikit

import (
	"context"
	"fmt"

	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"

	"github.com/txsvc/apikit/internal/apiutil"
	"github.com/txsvc/apikit/internal/authctx"
	"github.com/txsvc/apikit/internal/bootstrap"
	"github.com/txsvc/apikit/internal/config"
	"github.com/txsvc/apikit/internal/db"
	"github.com/txsvc/apikit/internal/keys"
	"github.com/txsvc/apikit/internal/oauth"
)

func init() {
	apiutil.TokenPrefix = TokenPrefix
}

// Build-time configurable variables, overridable via -ldflags.
var (
	// Version holds the semantic version string.
	Version = "dev"
	// Commit holds the short git commit SHA.
	Commit = "dev"
	// BuildTime holds the UTC build timestamp (e.g. "2025-06-01T00:00:00Z").
	BuildTime = ""
	// TokenPrefix is the token namespace prefix.
	TokenPrefix = "ak"
)

// Config is a type alias for the internal config type.
// This allows consumers to use *apikit.Config without importing internal/config.
type Config = config.Config

// DB is a type alias for the internal db type.
// Consumers can use *apikit.DB without importing internal/db.
type DB = db.DB

// Provider is a type alias for the internal oauth.Provider interface.
// Consuming projects can implement custom providers without importing internal/oauth.
type Provider = oauth.Provider

// UserInfo is a type alias for the internal oauth.UserInfo struct.
// Consuming projects can use *apikit.UserInfo without importing internal/oauth.
type UserInfo = oauth.UserInfo

// APIKeyResult is a type alias for the internal keys.APIKeyResult struct.
// Consumers can use *apikit.APIKeyResult without importing internal/keys.
type APIKeyResult = keys.APIKeyResult

// AuthInfo is a type alias for the internal authctx.AuthInfo struct.
// Consumers can use *apikit.AuthInfo to inspect the authenticated credential
// without importing internal/authctx.
type AuthInfo = authctx.AuthInfo

// GetAuthInfo retrieves the AuthInfo struct from the Echo request context.
// Returns nil if no AuthInfo has been injected (e.g. no auth middleware ran).
func GetAuthInfo(c echo.Context) *AuthInfo {
	return authctx.GetAuthInfo(c)
}

// SetAuthInfo stores the AuthInfo in the request's context.Context, making it
// available via GetAuthInfo. Test code can call this to inject auth state
// without running the full middleware stack.
func SetAuthInfo(c echo.Context, info *AuthInfo) {
	authctx.SetAuthInfo(c, info)
}

// GetUserID returns the authenticated user's UUID string from the context
// AuthInfo, or an empty string if AuthInfo is nil or UserID is empty.
func GetUserID(c echo.Context) string {
	return authctx.GetUserID(c)
}

// Permission defines a custom PAT permission scope. Register custom
// permissions by passing them to MountHandlers:
//
//	server.MountHandlers(database,
//	    apikit.Permission{Resource: "workspaces", Action: "read"},
//	    apikit.Permission{Resource: "workspaces", Action: "create"},
//	)
type Permission struct {
	Resource string
	Action   string
}

// ErrBootstrapComplete is returned by Bootstrap when the bootstrap sequence
// generates an admin token (first boot or token rotation). The caller must
// exit the process cleanly without starting the HTTP server — the operator
// needs to save the token and delete the file before the next start.
var ErrBootstrapComplete = bootstrap.ErrTokenGenerated

// BootstrapOptions configures the admin bootstrap sequence.
type BootstrapOptions struct {
	// AdminEmail is the designated admin email (--admin-email flag).
	// Required on first boot; silently ignored on subsequent boots.
	AdminEmail string
	// ResetToken triggers admin token rotation when true (--reset-admin-token flag).
	ResetToken bool
}

// LoadConfig loads the server configuration from config.toml,
// respecting XDG base directory conventions.
func LoadConfig() (*Config, error) {
	return config.Load()
}

// ConfigDir returns the directory containing config.toml,
// used for resolving adjacent files like admin_token.
func ConfigDir() string {
	return config.ConfigDir()
}

// ConfigPath returns the resolved path to config.toml.
func ConfigPath() string {
	return config.ConfigPath()
}

// OpenDatabase opens and initializes a SQLite database at the given path.
// The schema is applied automatically. The caller must call Close when done.
func OpenDatabase(path string) (*DB, error) {
	return db.Open(path)
}

// Bootstrap runs the admin bootstrap sequence when applicable.
// It detects whether bootstrap is needed (admin email provided, reset flag,
// or server already bootstrapped) and executes the appropriate sequence.
// Returns nil without action when no bootstrap is needed.
//
// Returns ErrBootstrapComplete when a token is generated (first boot or
// token rotation). The caller must exit the process cleanly without
// starting the HTTP server. Returns nil on a successful subsequent boot
// (server should proceed to start). Any other non-nil error is fatal.
//
// Must be called after the database is opened and before the HTTP server
// begins accepting requests.
func Bootstrap(ctx context.Context, database *DB, opts BootstrapOptions) error {
	var bootstrapped bool
	if err := database.SqlDB.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM admin_config WHERE key = 'admin_token_hash')",
	).Scan(&bootstrapped); err != nil {
		return fmt.Errorf("checking bootstrap state: %w", err)
	}

	if opts.AdminEmail == "" && !opts.ResetToken && !bootstrapped {
		return nil
	}

	return bootstrap.Run(ctx, bootstrap.BootstrapParams{
		DB:          database.SqlDB,
		AdminEmail:  opts.AdminEmail,
		ResetToken:  opts.ResetToken,
		ConfigDir:   ConfigDir(),
		TokenPrefix: TokenPrefix,
		Logger:      logrus.StandardLogger(),
	})
}

// GenerateAPIKey creates a new API key for the given user, revoking any
// existing active keys. It delegates to keys.GenerateAPIKey.
func GenerateAPIKey(tx db.Executor, userID string, expiresDays int, logger echo.Logger) (*APIKeyResult, error) {
	return keys.GenerateAPIKey(tx, userID, expiresDays, logger)
}

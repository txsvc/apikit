// Package bootstrap implements the admin bootstrap sequence for apikit.
//
// Run() detects whether this is a first boot (zero users) or subsequent boot,
// generates and stores admin tokens, and enforces the file-presence guard.
// ShouldAutoPromote() checks whether a user should receive the admin role.
package bootstrap

import (
	"context"
	"database/sql"
	"io"

	crypto_rand "crypto/rand"

	"github.com/sirupsen/logrus"
)

// randReader is the source of randomness for token generation.
// Tests override this via SetRandReader in export_test.go.
var randReader io.Reader = crypto_rand.Reader

// BootstrapParams bundles all inputs for the admin bootstrap sequence.
type BootstrapParams struct {
	// DB is the opened SQLite database connection.
	DB *sql.DB
	// AdminEmail is the value of the --admin-email flag.
	AdminEmail string
	// ResetToken triggers token rotation when true.
	ResetToken bool
	// ConfigDir is the directory containing config.toml,
	// used to resolve the admin_token file path via filepath.Join.
	ConfigDir string
	// TokenPrefix is the build-time configurable prefix for tokens.
	TokenPrefix string
	// Logger is the structured logger instance.
	Logger *logrus.Logger
}

// Run executes the full admin bootstrap sequence.
// It returns a non-nil error on any failure; it never calls os.Exit or log.Fatal.
func Run(_ context.Context, _ BootstrapParams) error {
	return nil
}

// ShouldAutoPromote checks whether a newly created user should
// receive the admin role based on the designated admin email.
func ShouldAutoPromote(_ context.Context, _ *sql.DB, _ string) (bool, error) {
	return false, nil
}

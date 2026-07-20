// Package bootstrap implements the admin bootstrap sequence for apikit.
//
// Run() detects whether this is a first boot (zero users) or subsequent boot,
// generates and stores admin tokens, and enforces the file-presence guard.
// Concurrent calls to Run are not supported; the caller must ensure
// single-instance startup so that only one server instance runs the
// bootstrap sequence at a time, preventing concurrent writes to admin_config.
//
// ShouldAutoPromote() checks whether a user should receive the admin role
// based on the designated admin email stored in admin_config.
package bootstrap

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	crypto_rand "crypto/rand"

	"github.com/sirupsen/logrus"
	"github.com/txsvc/apikit/internal/db"
)

// randReader is the source of randomness for token generation.
// Tests override this via SetRandReader in export_test.go.
var randReader io.Reader = crypto_rand.Reader

// BootstrapParams bundles all inputs for the admin bootstrap sequence.
//
// Cross-spec integration: DB should be the *sql.DB from db.Open()
// (02_database_layer) with schema already applied (users and admin_config
// tables must exist). ConfigDir should be the directory returned by
// LoadConfig() (01_server_core). TokenPrefix should be populated from
// apikit.TokenPrefix.
type BootstrapParams struct {
	// DB is the opened SQLite database connection from db.Open()
	// (02_database_layer). The users and admin_config tables must
	// exist before Run is called.
	DB *sql.DB
	// AdminEmail is the value of the --admin-email flag.
	// Required on first boot; silently ignored on subsequent boots.
	AdminEmail string
	// ResetToken triggers token rotation when true (--reset-admin-token).
	ResetToken bool
	// ConfigDir is the directory containing config.toml,
	// used to resolve the admin_token file path via filepath.Join.
	// Typically derived from LoadConfig() in 01_server_core.
	ConfigDir string
	// TokenPrefix is the build-time configurable prefix for tokens
	// (apikit.TokenPrefix, default "ak").
	TokenPrefix string
	// Logger is the structured logger instance.
	Logger *logrus.Logger
}

// generateToken creates a new admin token in the format
// <prefix>_admin_<64 lowercase hex chars> using 32 cryptographically
// random bytes from crypto/rand.
func generateToken(prefix string) (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(randReader, b); err != nil {
		return "", fmt.Errorf("generating random bytes: %w", err)
	}
	return prefix + "_admin_" + hex.EncodeToString(b), nil
}

// hashToken computes the SHA-256 hash of the full token string and returns
// a 64-character lowercase hex string.
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// compareTokenHashes performs a timing-safe comparison of two hex-encoded
// hash strings using crypto/subtle.ConstantTimeCompare.
func compareTokenHashes(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// tokenFilePath returns the path to the admin_token file within the given
// config directory. Path resolution (XDG_CONFIG_HOME vs CWD) is handled by
// the caller, which populates ConfigDir in BootstrapParams accordingly.
func tokenFilePath(configDir string) string {
	return filepath.Join(configDir, "admin_token")
}

// writeTokenFile writes the plaintext token to the given path with mode 0600.
// The token is written with no trailing newline.
func writeTokenFile(path, token string) error {
	if err := os.WriteFile(path, []byte(token), 0o600); err != nil {
		return fmt.Errorf("writing admin_token file: %w", err)
	}
	return nil
}

// Run executes the full admin bootstrap sequence. It must be called after
// LoadConfig completes and after the database is opened with schema applied,
// but before the HTTP server begins accepting requests.
//
// The caller (server binary main) must treat a non-nil return value as fatal
// and not start the HTTP server. Run never terminates the process internally;
// all errors are returned to the caller so that main() controls process
// termination.
func Run(_ context.Context, params BootstrapParams) error {
	// Token rotation takes priority over first-boot/subsequent-boot logic.
	if params.ResetToken {
		return runTokenRotation(params)
	}

	// Determine boot type: if an admin token hash already exists,
	// bootstrap has run before — treat as subsequent boot regardless
	// of user count.
	var hashExists bool
	if err := params.DB.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM admin_config WHERE key = 'admin_token_hash')",
	).Scan(&hashExists); err != nil {
		return fmt.Errorf("checking admin_token_hash: %w", err)
	}

	if hashExists {
		return runSubsequentBoot(params)
	}
	return runFirstBoot(params)
}

// runFirstBoot handles the first boot sequence: validates admin email,
// generates and stores the admin token, and writes the token file.
func runFirstBoot(params BootstrapParams) error {
	if params.AdminEmail == "" {
		return fmt.Errorf("first boot: --admin-email is required")
	}

	// Store admin email.
	if _, err := params.DB.Exec(
		"INSERT OR REPLACE INTO admin_config (key, value) VALUES (?, ?)",
		"admin_email", params.AdminEmail,
	); err != nil {
		return fmt.Errorf("storing admin_email: %w", err)
	}

	// Generate token.
	token, err := generateToken(params.TokenPrefix)
	if err != nil {
		return err
	}

	// Compute and store the token hash.
	hash := hashToken(token)
	if _, err := params.DB.Exec(
		"INSERT OR REPLACE INTO admin_config (key, value) VALUES (?, ?)",
		"admin_token_hash", hash,
	); err != nil {
		return fmt.Errorf("storing admin_token_hash: %w", err)
	}

	// Write the token file.
	tokenPath := tokenFilePath(params.ConfigDir)
	if err := writeTokenFile(tokenPath, token); err != nil {
		return err
	}

	// Log the token file path at warn level.
	absPath, err := filepath.Abs(tokenPath)
	if err != nil {
		absPath = tokenPath
	}
	params.Logger.Warnf("Admin token written to %s — save the token securely and delete the file", absPath)

	return nil
}

// runSubsequentBoot handles the subsequent boot sequence: checks the
// file-presence guard, validates the ADMIN_TOKEN environment variable,
// and compares hashes.
func runSubsequentBoot(params BootstrapParams) error {
	tokenPath := tokenFilePath(params.ConfigDir)

	// File-presence guard: refuse to start if admin_token file exists.
	if _, err := os.Stat(tokenPath); err == nil {
		absPath, _ := filepath.Abs(tokenPath)
		return fmt.Errorf("admin_token file exists at %s: save the token securely and delete the file before restarting", absPath)
	}

	// AdminEmail is silently ignored on subsequent boots (no logging, no DB write).

	// Validate the ADMIN_TOKEN environment variable.
	envToken := os.Getenv("ADMIN_TOKEN")
	if envToken == "" {
		return fmt.Errorf("ADMIN_TOKEN environment variable is required")
	}

	// Read the stored hash from admin_config.
	var storedHash string
	err := params.DB.QueryRow(
		"SELECT value FROM admin_config WHERE key = ?", "admin_token_hash",
	).Scan(&storedHash)
	if err == sql.ErrNoRows {
		return fmt.Errorf("no admin token hash found in database; run with --reset-admin-token to generate a new token")
	}
	if err != nil {
		return fmt.Errorf("reading admin_token_hash: %w", err)
	}

	// Compare hashes using constant-time comparison.
	candidateHash := hashToken(envToken)
	if !compareTokenHashes(candidateHash, storedHash) {
		return fmt.Errorf("ADMIN_TOKEN does not match the stored admin token")
	}

	return nil
}

// runTokenRotation generates a new admin token, stores its hash, and writes
// the new token file. It skips the file-presence guard and ADMIN_TOKEN check.
func runTokenRotation(params BootstrapParams) error {
	// Generate a new token.
	token, err := generateToken(params.TokenPrefix)
	if err != nil {
		return err
	}

	// Compute and store the new token hash.
	hash := hashToken(token)
	if _, err := params.DB.Exec(
		"INSERT OR REPLACE INTO admin_config (key, value) VALUES (?, ?)",
		"admin_token_hash", hash,
	); err != nil {
		return fmt.Errorf("storing admin_token_hash: %w", err)
	}

	// Write the new token file.
	tokenPath := tokenFilePath(params.ConfigDir)
	if err := writeTokenFile(tokenPath, token); err != nil {
		return err
	}

	// Log the token file path at warn level.
	absPath, err := filepath.Abs(tokenPath)
	if err != nil {
		absPath = tokenPath
	}
	params.Logger.Warnf("Admin token written to %s — save the token securely and delete the file", absPath)

	return nil
}

// ShouldAutoPromote checks whether a newly created user should receive the
// admin role based on the designated admin email stored in admin_config.
// It returns (true, nil) if the email matches, (false, nil) if it does not
// match or no admin_email is configured, and (false, error) on database errors.
//
// This function is intended for new user creation only (first OAuth login).
// Callers (the OAuth callback handler) must not invoke it on updates to
// existing users. When it returns true, the caller should grant the admin
// role to the new user and log the auto-promotion event at info level.
//
// Expected call site in the OAuth callback handler:
//
//	promote, err := bootstrap.ShouldAutoPromote(ctx, sqlDB, userEmail)
//	if err != nil { return err }
//	if promote {
//	    // set role = "admin" on the new user record
//	    logger.Infof("auto-promoted user %s to admin", userEmail)
//	}
func ShouldAutoPromote(ctx context.Context, q db.Executor, email string) (bool, error) {
	var storedEmail string
	err := q.QueryRowContext(ctx,
		"SELECT value FROM admin_config WHERE key = ?", "admin_email",
	).Scan(&storedEmail)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("querying admin_email: %w", err)
	}

	return storedEmail == email, nil
}

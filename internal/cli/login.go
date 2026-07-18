package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/spf13/cobra"
)

// loginTimeoutSeconds is the maximum number of seconds the login command
// waits for the browser OAuth callback before timing out.
const loginTimeoutSeconds = 120

// loginOpts holds configuration for the login OAuth flow.
// Fields with Fn suffix are dependency injection points for testing.
type loginOpts struct {
	provider      string
	expires       int
	endpointURL   string
	configPath    string
	openBrowserFn func(string) error
	saveConfigFn  func(string, *CLIConfig) error
	stderr        io.Writer
	stdout        io.Writer
}

// NewLoginCmd returns the Cobra command for `akc login`.
// Stub — will be implemented in task group 6.
func NewLoginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "placeholder",
	}
	return cmd
}

// runLogin implements the OAuth login flow with a configurable timeout.
// The Cobra RunE function calls this with time.Duration(loginTimeoutSeconds)*time.Second.
// Tests call this directly with a short timeout to exercise timeout behavior.
// Stub — will be implemented in task group 6.
func runLogin(_ context.Context, _ time.Duration, _ loginOpts) error {
	return nil
}

// newCallbackHandler returns an HTTP handler for the OAuth callback server.
// The handler handles GET /callback by validating the state parameter and
// sending the code on codeCh (success) or an error on errCh (state mismatch).
// All other paths receive HTTP 404.
// Stub — will be implemented in task group 6.
func newCallbackHandler(_ string, _ chan string, _ chan error) http.Handler {
	return http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		// Stub: does nothing — tests will fail with wrong status codes and missing channel signals.
	})
}

// generateState generates a cryptographically random 64-character hex
// string for the OAuth state parameter using crypto/rand.
func generateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// buildAuthURL constructs the OAuth authorization URL by parsing the
// provider's authorize_url, preserving existing query parameters, and
// appending redirect_uri, state, and response_type=code.
// Does NOT add client_id — it is already in the authorize_url.
func buildAuthURL(authorizeURL, redirectURI, state string) (string, error) {
	u, err := url.Parse(authorizeURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("redirect_uri", redirectURI)
	q.Set("state", state)
	q.Set("response_type", "code")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

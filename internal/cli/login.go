package cli

import (
	"github.com/spf13/cobra"
)

// loginTimeoutSeconds is the maximum number of seconds the login command
// waits for the browser OAuth callback before timing out.
const loginTimeoutSeconds = 120

// NewLoginCmd returns the Cobra command for `akc login`.
// Stub — will be implemented in task group 6.
func NewLoginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "placeholder",
	}
	return cmd
}

// generateState generates a cryptographically random 64-character hex
// string for the OAuth state parameter using crypto/rand.
// Stub — will be implemented in task group 6.
func generateState() (string, error) {
	return "", nil
}

// buildAuthURL constructs the OAuth authorization URL by parsing the
// provider's authorize_url, preserving existing query parameters, and
// appending redirect_uri, state, and response_type=code.
// Stub — will be implemented in task group 6.
func buildAuthURL(_, _, _ string) (string, error) {
	return "", nil
}

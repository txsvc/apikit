package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Shared infrastructure for authenticated non-admin commands.
//
// These commands cannot import the root apikit package (which imports
// internal/cli), so they make raw HTTP calls — the same approach used by
// the login command (login.go). The admin commands use a Runner DI pattern
// (runners.go); the non-admin commands use cmdClient, which wraps an
// HTTP client with endpoint URL and API key.
// ---------------------------------------------------------------------------

// cmdClient holds configuration for authenticated non-admin commands.
// Commands retrieve it from the Cobra context via ClientFromContext.
// In production, PersistentPreRunE injects it; in tests, the test
// helper injects it directly.
type cmdClient struct {
	endpointURL  string
	apiKey       string
	httpClient   *http.Client
	saveConfigFn func(string, *CLIConfig) error
	configPath   string
}

// cmdError is a pre-validation or client-side error with a fixed code.
// Satisfies the codedError interface (admin.go) for consistent error
// envelope rendering.
type cmdError struct {
	code    int
	message string
}

func (e *cmdError) Error() string        { return e.message }
func (e *cmdError) ErrorCode() int       { return e.code }
func (e *cmdError) ErrorMessage() string { return e.message }

// newAuthenticatedCmdClient retrieves a *cmdClient from the command's
// context. If no client is injected (nil context value), it returns
// the "no API key configured" error — matching the spec's
// NewAuthenticatedClient pre-validation behavior.
func newAuthenticatedCmdClient(cmd *cobra.Command) (*cmdClient, error) {
	raw := ClientFromContext(cmd.Context())
	if c, ok := raw.(*cmdClient); ok && c != nil {
		return c, nil
	}

	// No client injected. In production, PersistentPreRunE on the root
	// command injects the client after resolving config/env/flags.
	// Without injection, we report the missing-key error.
	return nil, &cmdError{
		code:    2,
		message: "no API key configured — run 'akc login' first",
	}
}

// cmdPrintJSON writes v as indented JSON to cmd's stdout.
// Uses json.NewEncoder with HTML escaping disabled per 15-REQ-20.1.
func cmdPrintJSON(cmd *cobra.Command, v any) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// cmdHandleError writes a JSON error envelope to stdout and returns
// the original error. For coded errors (satisfying codedError), the
// envelope uses the error's code. For other errors, code is 2.
func cmdHandleError(cmd *cobra.Command, err error) error {
	code := 2
	msg := err.Error()

	var ce codedError
	if errors.As(err, &ce) {
		code = ce.ErrorCode()
		msg = ce.ErrorMessage()
	}

	envelope := map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": msg,
		},
	}
	_ = cmdPrintJSON(cmd, envelope)
	return &printedError{err}
}

// doRequest performs an authenticated HTTP request and returns the decoded
// response body. On 4xx/5xx responses, it decodes the error envelope and
// returns a *cmdError. The caller prints the result or error envelope.
func (c *cmdClient) doRequest(ctx context.Context, method, path string, body any) (any, error) {
	fullURL := strings.TrimRight(c.endpointURL, "/") + "/api/v1" + path

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, &cmdError{code: 2, message: fmt.Sprintf("failed to marshal request: %v", err)}
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, &cmdError{code: 2, message: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	httpClient := c.httpClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, &cmdError{code: 2, message: err.Error()}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &cmdError{code: 2, message: err.Error()}
	}

	if resp.StatusCode >= 400 {
		// Try to decode the server's error envelope.
		var errEnv struct {
			Error struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(respBody, &errEnv) == nil && errEnv.Error.Code != 0 {
			return nil, &cmdError{code: errEnv.Error.Code, message: errEnv.Error.Message}
		}
		return nil, &cmdError{code: resp.StatusCode, message: http.StatusText(resp.StatusCode)}
	}

	// Decode response body into any (map[string]any for objects,
	// []any for arrays).
	var result any
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &result); err != nil {
			return nil, &cmdError{code: 2, message: fmt.Sprintf("failed to decode response: %v", err)}
		}
	}

	return result, nil
}

// ---------------------------------------------------------------------------
// User commands
// ---------------------------------------------------------------------------

// NewUserCmd returns the Cobra parent command for `akc user`.
// It registers show and update subcommands.
func NewUserCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user",
		Short: "Manage your user profile",
	}

	cmd.AddCommand(
		newUserShowCmd(),
		newUserUpdateCmd(),
	)

	return cmd
}

// newUserShowCmd returns the `akc user show` subcommand.
// No flags or positional arguments. Calls GET /user and prints
// the User JSON to stdout.
func newUserShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show your user profile",
		Long:  "Retrieve and display the authenticated user's profile information.",
		Args:  cobra.NoArgs,
		Annotations: map[string]string{
			"auth":   "api_key",
			"method": "GET",
			"path":   "/user",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newAuthenticatedCmdClient(cmd)
			if err != nil {
				return cmdHandleError(cmd, err)
			}

			result, err := client.doRequest(cmd.Context(), http.MethodGet, "/user", nil)
			if err != nil {
				return cmdHandleError(cmd, err)
			}

			return cmdPrintJSON(cmd, result)
		},
	}
}

// newUserUpdateCmd returns the `akc user update` subcommand.
// Requires --full-name flag. Calls PATCH /user and prints the
// updated User JSON to stdout. No client-side validation of the
// flag value — server validates.
func newUserUpdateCmd() *cobra.Command {
	var fullName string

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update your user profile",
		Long:  "Update the authenticated user's display name.",
		Args:  cobra.NoArgs,
		Annotations: map[string]string{
			"auth":   "api_key",
			"method": "PATCH",
			"path":   "/user",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newAuthenticatedCmdClient(cmd)
			if err != nil {
				return cmdHandleError(cmd, err)
			}

			body := map[string]string{"full_name": fullName}
			result, err := client.doRequest(cmd.Context(), http.MethodPatch, "/user", body)
			if err != nil {
				return cmdHandleError(cmd, err)
			}

			return cmdPrintJSON(cmd, result)
		},
	}

	cmd.Flags().StringVar(&fullName, "full-name", "", "Full display name")
	_ = cmd.MarkFlagRequired("full-name")

	return cmd
}

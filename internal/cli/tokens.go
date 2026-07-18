package cli

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

// NewTokensCmd returns the Cobra parent command for `akc tokens`.
// It registers list, create, show, and revoke subcommands.
func NewTokensCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tokens",
		Short: "Manage your Personal Access Tokens",
	}

	cmd.AddCommand(
		newTokensListCmd(),
		newTokensCreateCmd(),
		newTokensShowCmd(),
		newTokensRevokeCmd(),
	)

	return cmd
}

// newTokensListCmd returns the `akc tokens list` subcommand.
// No flags or arguments. Calls GET /user/tokens and prints the
// []*PAT JSON array to stdout.
func newTokensListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List your Personal Access Tokens",
		Long:  "List all Personal Access Tokens associated with the authenticated user.",
		Args:  cobra.NoArgs,
		Annotations: map[string]string{
			"auth":   "api_key",
			"method": "GET",
			"path":   "/user/tokens",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newAuthenticatedCmdClient(cmd)
			if err != nil {
				return cmdHandleError(cmd, err)
			}

			result, err := client.doRequest(cmd.Context(), http.MethodGet, "/user/tokens", nil)
			if err != nil {
				return cmdHandleError(cmd, err)
			}

			return cmdPrintJSON(cmd, result)
		},
	}
}

// newTokensCreateCmd returns the `akc tokens create` subcommand.
// Requires --name and --permissions flags. Optional --expires (default 90).
// Validates permissions (non-empty) and expires (0, 30, 60, 90) before
// making the API call. Prints PATFull JSON to stdout and a save-token
// warning to stderr.
func newTokensCreateCmd() *cobra.Command {
	var (
		name        string
		permissions string
		expires     int
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new Personal Access Token",
		Long:  "Create a new Personal Access Token with the specified name, permissions, and expiry.",
		Args:  cobra.NoArgs,
		Annotations: map[string]string{
			"auth":   "api_key",
			"method": "POST",
			"path":   "/user/tokens",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validate permissions before client check — validation errors
			// take priority over missing-api-key errors.
			perms, err := parsePermissions(permissions)
			if err != nil {
				return cmdHandleError(cmd, &cmdError{code: 2, message: err.Error()})
			}

			// Validate expires.
			if err := validateExpires(expires); err != nil {
				return cmdHandleError(cmd, &cmdError{code: 2, message: err.Error()})
			}

			client, err := newAuthenticatedCmdClient(cmd)
			if err != nil {
				return cmdHandleError(cmd, err)
			}

			body := map[string]any{
				"name":        name,
				"permissions": perms,
				"expires":     expires,
			}
			result, err := client.doRequest(cmd.Context(), http.MethodPost, "/user/tokens", body)
			if err != nil {
				return cmdHandleError(cmd, err)
			}

			if err := cmdPrintJSON(cmd, result); err != nil {
				return err
			}

			fmt.Fprintln(cmd.ErrOrStderr(), "Token created. Save the token value — it cannot be retrieved later.")

			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Token name")
	_ = cmd.MarkFlagRequired("name")

	cmd.Flags().StringVar(&permissions, "permissions", "", "Comma-separated permissions (e.g., users:read,orgs:read)")
	_ = cmd.MarkFlagRequired("permissions")

	cmd.Flags().IntVar(&expires, "expires", 90, "Token expiry in days (0, 30, 60, or 90)")

	return cmd
}

// newTokensShowCmd returns the `akc tokens show` subcommand.
// Takes exactly one positional argument: token_id.
// Calls GET /user/tokens/{id} and prints PAT JSON to stdout.
func newTokensShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <token_id>",
		Short: "Show a Personal Access Token",
		Long:  "Retrieve and display metadata for a specific Personal Access Token.",
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"auth":   "api_key",
			"method": "GET",
			"path":   "/user/tokens/:token_id",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newAuthenticatedCmdClient(cmd)
			if err != nil {
				return cmdHandleError(cmd, err)
			}

			tokenID := args[0]
			result, err := client.doRequest(cmd.Context(), http.MethodGet, "/user/tokens/"+tokenID, nil)
			if err != nil {
				return cmdHandleError(cmd, err)
			}

			return cmdPrintJSON(cmd, result)
		},
	}
}

// newTokensRevokeCmd returns the `akc tokens revoke` subcommand.
// Takes exactly one positional argument: token_id.
// Calls DELETE /user/tokens/{id}. On success (HTTP 204, empty body),
// prints {} to stdout and "Token <token_id> revoked" to stderr.
func newTokensRevokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <token_id>",
		Short: "Revoke a Personal Access Token",
		Long:  "Invalidate a specific Personal Access Token immediately.",
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"auth":   "api_key",
			"method": "DELETE",
			"path":   "/user/tokens/:token_id",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newAuthenticatedCmdClient(cmd)
			if err != nil {
				return cmdHandleError(cmd, err)
			}

			tokenID := args[0]
			_, err = client.doRequest(cmd.Context(), http.MethodDelete, "/user/tokens/"+tokenID, nil)
			if err != nil {
				return cmdHandleError(cmd, err)
			}

			// RevokeToken returns no body (HTTP 204). Print {} to stdout.
			emptyObj := map[string]any{}
			if err := cmdPrintJSON(cmd, emptyObj); err != nil {
				return err
			}

			fmt.Fprintf(cmd.ErrOrStderr(), "Token %s revoked\n", tokenID)

			return nil
		},
	}
}

package cli

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

// NewTokensCmd returns the Cobra parent command for `akc tokens`.
// It registers list, create, show, revoke, replace, add, and remove subcommands.
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
		newTokensReplaceCmd(),
		newTokensAddCmd(),
		newTokensRemoveCmd(),
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
			client, err := NewAuthenticatedCmdClient(cmd)
			if err != nil {
				return CmdHandleError(cmd, err)
			}

			result, err := client.DoRequest(cmd.Context(), http.MethodGet, "/user/tokens", nil)
			if err != nil {
				return CmdHandleError(cmd, err)
			}

			return CmdPrintJSON(cmd, result)
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
				return CmdHandleError(cmd, &CmdError{code: 2, message: err.Error()})
			}

			// Validate expires.
			if err := validateExpires(expires); err != nil {
				return CmdHandleError(cmd, &CmdError{code: 2, message: err.Error()})
			}

			client, err := NewAuthenticatedCmdClient(cmd)
			if err != nil {
				return CmdHandleError(cmd, err)
			}

			body := map[string]any{
				"name":        name,
				"permissions": perms,
				"expires":     expires,
			}
			result, err := client.DoRequest(cmd.Context(), http.MethodPost, "/user/tokens", body)
			if err != nil {
				return CmdHandleError(cmd, err)
			}

			if err := CmdPrintJSON(cmd, result); err != nil {
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

// newTokensReplaceCmd returns the `akc tokens replace` subcommand.
// Takes exactly one positional argument: token_id.
// Requires --permissions flag. Parses with parsePermissions, sends a PUT
// request to /user/tokens/<token_id>/permissions, prints PATResponse JSON
// to stdout. If revoked_at is non-null in the response, prints a
// revocation warning to stderr.
func newTokensReplaceCmd() *cobra.Command {
	var permissions string

	cmd := &cobra.Command{
		Use:   "replace <token_id>",
		Short: "Replace all permissions on a Personal Access Token",
		Long:  "Replace the entire permissions set on a PAT with the specified permissions.",
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"auth":   "api_key",
			"method": "PUT",
			"path":   "/user/tokens/:token_id/permissions",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			perms, err := parsePermissions(permissions)
			if err != nil {
				return CmdHandleError(cmd, &CmdError{code: 2, message: err.Error()})
			}

			client, err := NewAuthenticatedCmdClient(cmd)
			if err != nil {
				return CmdHandleError(cmd, err)
			}

			tokenID := args[0]
			body := map[string]any{
				"permissions": perms,
			}
			result, err := client.DoRequest(cmd.Context(), http.MethodPut, "/user/tokens/"+tokenID+"/permissions", body)
			if err != nil {
				return CmdHandleError(cmd, err)
			}

			if err := CmdPrintJSON(cmd, result); err != nil {
				return err
			}

			if isAutoRevoked(result) {
				fmt.Fprintf(cmd.ErrOrStderr(), "Token %s has been revoked (no permissions remaining)\n", tokenID)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&permissions, "permissions", "", "Comma-separated permissions (e.g., users:read,orgs:read)")
	_ = cmd.MarkFlagRequired("permissions")

	return cmd
}

// newTokensAddCmd returns the `akc tokens add` subcommand.
// Takes exactly one positional argument: token_id.
// Requires --permissions flag. Parses with parsePermissions, sends a PATCH
// request to /user/tokens/<token_id>/permissions, prints PATResponse JSON
// to stdout. Never prints a revocation warning (adding permissions cannot
// produce an empty set).
func newTokensAddCmd() *cobra.Command {
	var permissions string

	cmd := &cobra.Command{
		Use:   "add <token_id>",
		Short: "Add permissions to a Personal Access Token",
		Long:  "Add one or more permissions to an existing PAT without replacing the current set.",
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"auth":   "api_key",
			"method": "PATCH",
			"path":   "/user/tokens/:token_id/permissions",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			perms, err := parsePermissions(permissions)
			if err != nil {
				return CmdHandleError(cmd, &CmdError{code: 2, message: err.Error()})
			}

			client, err := NewAuthenticatedCmdClient(cmd)
			if err != nil {
				return CmdHandleError(cmd, err)
			}

			tokenID := args[0]
			body := map[string]any{
				"permissions": perms,
			}
			result, err := client.DoRequest(cmd.Context(), http.MethodPatch, "/user/tokens/"+tokenID+"/permissions", body)
			if err != nil {
				return CmdHandleError(cmd, err)
			}

			return CmdPrintJSON(cmd, result)
		},
	}

	cmd.Flags().StringVar(&permissions, "permissions", "", "Comma-separated permissions (e.g., orgs:read)")
	_ = cmd.MarkFlagRequired("permissions")

	return cmd
}

// newTokensRemoveCmd returns the `akc tokens remove` subcommand.
// Takes exactly one positional argument: token_id.
// Requires --permissions flag. Parses with parsePermissions, sends a DELETE
// request to /user/tokens/<token_id>/permissions, prints PATResponse JSON
// to stdout. If revoked_at is non-null in the response, prints a
// revocation warning to stderr.
func newTokensRemoveCmd() *cobra.Command {
	var permissions string

	cmd := &cobra.Command{
		Use:   "remove <token_id>",
		Short: "Remove permissions from a Personal Access Token",
		Long:  "Remove one or more permissions from an existing PAT without revoking it entirely.",
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"auth":   "api_key",
			"method": "DELETE",
			"path":   "/user/tokens/:token_id/permissions",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			perms, err := parsePermissions(permissions)
			if err != nil {
				return CmdHandleError(cmd, &CmdError{code: 2, message: err.Error()})
			}

			client, err := NewAuthenticatedCmdClient(cmd)
			if err != nil {
				return CmdHandleError(cmd, err)
			}

			tokenID := args[0]
			body := map[string]any{
				"permissions": perms,
			}
			result, err := client.DoRequest(cmd.Context(), http.MethodDelete, "/user/tokens/"+tokenID+"/permissions", body)
			if err != nil {
				return CmdHandleError(cmd, err)
			}

			if err := CmdPrintJSON(cmd, result); err != nil {
				return err
			}

			if isAutoRevoked(result) {
				fmt.Fprintf(cmd.ErrOrStderr(), "Token %s has been revoked (no permissions remaining)\n", tokenID)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&permissions, "permissions", "", "Comma-separated permissions (e.g., users:read)")
	_ = cmd.MarkFlagRequired("permissions")

	return cmd
}

// isAutoRevoked checks whether a DoRequest result (expected to be a
// map[string]any PATResponse) contains a non-null revoked_at field,
// indicating the token was auto-revoked due to empty permissions.
func isAutoRevoked(result any) bool {
	m, ok := result.(map[string]any)
	if !ok {
		return false
	}
	v, exists := m["revoked_at"]
	return exists && v != nil
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
			client, err := NewAuthenticatedCmdClient(cmd)
			if err != nil {
				return CmdHandleError(cmd, err)
			}

			tokenID := args[0]
			result, err := client.DoRequest(cmd.Context(), http.MethodGet, "/user/tokens/"+tokenID, nil)
			if err != nil {
				return CmdHandleError(cmd, err)
			}

			return CmdPrintJSON(cmd, result)
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
			client, err := NewAuthenticatedCmdClient(cmd)
			if err != nil {
				return CmdHandleError(cmd, err)
			}

			tokenID := args[0]
			_, err = client.DoRequest(cmd.Context(), http.MethodDelete, "/user/tokens/"+tokenID, nil)
			if err != nil {
				return CmdHandleError(cmd, err)
			}

			// RevokeToken returns no body (HTTP 204). Print {} to stdout.
			emptyObj := map[string]any{}
			if err := CmdPrintJSON(cmd, emptyObj); err != nil {
				return err
			}

			fmt.Fprintf(cmd.ErrOrStderr(), "Token %s revoked\n", tokenID)

			return nil
		},
	}
}

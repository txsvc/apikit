package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

// newAdminTokensCmd returns the admin tokens command group with subcommands:
// list and revoke.
// The parent command has no RunE — invoking it directly prints help.
func newAdminTokensCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tokens",
		Short: "Manage user personal access tokens (admin)",
	}

	// list subcommand — requires exactly one positional arg (user selector).
	listCmd := &cobra.Command{
		Use:   "list <user_id|username|email>",
		Short: "List a user's personal access tokens",
		Long:  "List all personal access tokens for a user identified by UUID, username, or email address.",
		Args:  adminCheckMissingArg("user_id"),
		Annotations: map[string]string{
			"method": "GET",
			"path":   "/users/:id/tokens",
			"auth":   "admin",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			raw := ClientFromContext(cmd.Context())
			if raw == nil {
				return adminHandleError(cmd, fmt.Errorf("configuration not loaded: missing endpoint URL or API key"))
			}
			runner, ok := raw.(*TokensRunner)
			if !ok {
				return adminHandleError(cmd, fmt.Errorf("invalid client configuration"))
			}

			userID := args[0]
			result, err := runner.ListUserTokens(context.Background(), userID)
			if err != nil {
				return adminHandleError(cmd, err)
			}
			if err := adminPrintJSON(cmd, result); err != nil {
				return err
			}
			return nil
		},
	}

	// revoke subcommand — requires exactly two positional args (user selector, token ID).
	revokeCmd := &cobra.Command{
		Use:   "revoke <user_id|username|email> <token_id>",
		Short: "Revoke a user's personal access token",
		Long:  "Revoke a personal access token for a user identified by UUID, username, or email address.",
		Args:  adminCheckTwoArgs("user_id", "token_id"),
		Annotations: map[string]string{
			"method": "DELETE",
			"path":   "/users/:id/tokens/:token_id",
			"auth":   "admin",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			raw := ClientFromContext(cmd.Context())
			if raw == nil {
				return adminHandleError(cmd, fmt.Errorf("configuration not loaded: missing endpoint URL or API key"))
			}
			runner, ok := raw.(*TokensRunner)
			if !ok {
				return adminHandleError(cmd, fmt.Errorf("invalid client configuration"))
			}

			userID := args[0]
			tokenID := args[1]
			if err := runner.RevokeUserToken(context.Background(), userID, tokenID); err != nil {
				return adminHandleError(cmd, err)
			}
			if err := adminPrintJSON(cmd, struct{}{}); err != nil {
				return err
			}
			return nil
		},
	}

	cmd.AddCommand(
		listCmd,
		revokeCmd,
	)

	return cmd
}

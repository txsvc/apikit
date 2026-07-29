package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

// newAdminKeysCmd returns the admin keys command group with subcommands:
// list and revoke.
// The parent command has no RunE — invoking it directly prints help.
func newAdminKeysCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keys",
		Short: "Manage user API keys (admin)",
	}

	// list subcommand — requires exactly one positional arg (user selector).
	listCmd := &cobra.Command{
		Use:   "list <user_id|username|email>",
		Short: "List a user's API keys",
		Long:  "List all API keys for a user identified by UUID, username, or email address.",
		Args:  adminCheckMissingArg("user_id"),
		Annotations: map[string]string{
			"method": "GET",
			"path":   "/users/:id/keys",
			"auth":   "admin",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			raw := ClientFromContext(cmd.Context())
			if raw == nil {
				return adminHandleError(cmd, fmt.Errorf("configuration not loaded: missing endpoint URL or API key"))
			}
			runner, ok := raw.(*KeysRunner)
			if !ok {
				return adminHandleError(cmd, fmt.Errorf("invalid client configuration"))
			}

			userID := args[0]
			result, err := runner.ListUserKeys(context.Background(), userID)
			if err != nil {
				return adminHandleError(cmd, err)
			}
			if err := adminPrintJSON(cmd, result); err != nil {
				return err
			}
			return nil
		},
	}

	// revoke subcommand — requires exactly two positional args (user selector, key ID).
	revokeCmd := &cobra.Command{
		Use:   "revoke <user_id|username|email> <key_id>",
		Short: "Revoke a user's API key",
		Long:  "Revoke an API key for a user identified by UUID, username, or email address.",
		Args:  adminCheckTwoArgs("user_id", "key_id"),
		Annotations: map[string]string{
			"method": "DELETE",
			"path":   "/users/:id/keys/:key_id",
			"auth":   "admin",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			raw := ClientFromContext(cmd.Context())
			if raw == nil {
				return adminHandleError(cmd, fmt.Errorf("configuration not loaded: missing endpoint URL or API key"))
			}
			runner, ok := raw.(*KeysRunner)
			if !ok {
				return adminHandleError(cmd, fmt.Errorf("invalid client configuration"))
			}

			userID := args[0]
			keyID := args[1]
			if err := runner.RevokeUserKey(context.Background(), userID, keyID); err != nil {
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

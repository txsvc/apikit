package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

// newAdminUsersCmd returns the admin users command group with subcommands:
// list, show, create, update, promote, demote, block, unblock.
// The parent command has no RunE — invoking it directly prints help.
func newAdminUsersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "users",
		Short: "Manage users (admin)",
	}

	// list subcommand — no positional args, optional --include-blocked flag.
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all users",
		Args:  cobra.NoArgs,
		Annotations: map[string]string{
			"method": "GET",
			"path":   "/users",
			"auth":   "admin",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			raw := ClientFromContext(cmd.Context())
			if raw == nil {
				return adminHandleError(cmd, fmt.Errorf("configuration not loaded: missing endpoint URL or API key"))
			}
			runner, ok := raw.(*UsersRunner)
			if !ok {
				return adminHandleError(cmd, fmt.Errorf("invalid client configuration"))
			}

			includeBlocked, _ := cmd.Flags().GetBool("include-blocked")
			result, err := runner.ListUsers(context.Background(), includeBlocked)
			if err != nil {
				return adminHandleError(cmd, err)
			}
			if err := adminPrintJSON(cmd, result); err != nil {
				return err
			}
			return nil
		},
	}
	listCmd.Flags().Bool("include-blocked", false, "Include blocked users in the response")

	// show subcommand — requires exactly one positional arg (user ID).
	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Show a user by ID",
		Args:  adminCheckMissingArg("id"),
		Annotations: map[string]string{
			"method": "GET",
			"path":   "/users/:id",
			"auth":   "admin",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			raw := ClientFromContext(cmd.Context())
			if raw == nil {
				return adminHandleError(cmd, fmt.Errorf("configuration not loaded: missing endpoint URL or API key"))
			}
			runner, ok := raw.(*UsersRunner)
			if !ok {
				return adminHandleError(cmd, fmt.Errorf("invalid client configuration"))
			}

			id := args[0]
			result, err := runner.GetUserByID(context.Background(), id)
			if err != nil {
				return adminHandleError(cmd, err)
			}
			if err := adminPrintJSON(cmd, result); err != nil {
				return err
			}
			return nil
		},
	}

	// create subcommand — no positional args; four required flags.
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new user",
		Args:  cobra.NoArgs,
		Annotations: map[string]string{
			"method": "POST",
			"path":   "/users",
			"auth":   "admin",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validate required flags in order: username, email, provider, provider-id.
			username, err := adminCheckRequiredFlag(cmd, "username")
			if err != nil {
				return err
			}
			email, err := adminCheckRequiredFlag(cmd, "email")
			if err != nil {
				return err
			}
			provider, err := adminCheckRequiredFlag(cmd, "provider")
			if err != nil {
				return err
			}
			providerID, err := adminCheckRequiredFlag(cmd, "provider-id")
			if err != nil {
				return err
			}

			raw := ClientFromContext(cmd.Context())
			if raw == nil {
				return adminHandleError(cmd, fmt.Errorf("configuration not loaded: missing endpoint URL or API key"))
			}
			runner, ok := raw.(*UsersRunner)
			if !ok {
				return adminHandleError(cmd, fmt.Errorf("invalid client configuration"))
			}

			result, err := runner.CreateUser(context.Background(), username, email, provider, providerID)
			if err != nil {
				return adminHandleError(cmd, err)
			}
			if err := adminPrintJSON(cmd, result); err != nil {
				return err
			}
			return nil
		},
	}
	createCmd.Flags().String("username", "", "Username for the new user")
	createCmd.Flags().String("email", "", "Email address for the new user")
	createCmd.Flags().String("provider", "", "OAuth provider name")
	createCmd.Flags().String("provider-id", "", "Provider-specific user ID")

	// update subcommand — requires exactly one positional arg (user ID); --full-name flag.
	updateCmd := &cobra.Command{
		Use:   "update",
		Short: "Update a user by ID",
		Args:  adminCheckMissingArg("id"),
		Annotations: map[string]string{
			"method": "PATCH",
			"path":   "/users/:id",
			"auth":   "admin",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}
	updateCmd.Flags().String("full-name", "", "Full name of the user")

	// promote subcommand — requires exactly one positional arg (user ID).
	promoteCmd := &cobra.Command{
		Use:   "promote",
		Short: "Grant admin role to a user",
		Args:  adminCheckMissingArg("id"),
		Annotations: map[string]string{
			"method": "POST",
			"path":   "/users/:id/promote",
			"auth":   "admin",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}

	// demote subcommand — requires exactly one positional arg (user ID).
	demoteCmd := &cobra.Command{
		Use:   "demote",
		Short: "Revoke admin role from a user",
		Args:  adminCheckMissingArg("id"),
		Annotations: map[string]string{
			"method": "POST",
			"path":   "/users/:id/demote",
			"auth":   "admin",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}

	// block subcommand — requires exactly one positional arg (user ID).
	blockCmd := &cobra.Command{
		Use:   "block",
		Short: "Block a user",
		Args:  adminCheckMissingArg("id"),
		Annotations: map[string]string{
			"method": "POST",
			"path":   "/users/:id/block",
			"auth":   "admin",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}

	// unblock subcommand — requires exactly one positional arg (user ID).
	unblockCmd := &cobra.Command{
		Use:   "unblock",
		Short: "Unblock a user",
		Args:  adminCheckMissingArg("id"),
		Annotations: map[string]string{
			"method": "POST",
			"path":   "/users/:id/unblock",
			"auth":   "admin",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}

	cmd.AddCommand(
		listCmd,
		showCmd,
		createCmd,
		updateCmd,
		promoteCmd,
		demoteCmd,
		blockCmd,
		unblockCmd,
	)

	return cmd
}

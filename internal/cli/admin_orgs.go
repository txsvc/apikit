package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

// newAdminOrgsCmd returns the admin orgs command group with subcommands:
// list, create, update, delete, block, unblock, and members sub-group.
// The parent command has no RunE — invoking it directly prints help.
func newAdminOrgsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "orgs",
		Short: "Manage organizations (admin)",
	}

	// list subcommand — no positional args, optional --include-blocked flag.
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all organizations",
		Args:  cobra.NoArgs,
		Annotations: map[string]string{
			"method": "GET",
			"path":   "/orgs",
			"auth":   "admin",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			raw := ClientFromContext(cmd.Context())
			if raw == nil {
				return adminHandleError(cmd, fmt.Errorf("configuration not loaded: missing endpoint URL or API key"))
			}
			runner, ok := raw.(*OrgsRunner)
			if !ok {
				return adminHandleError(cmd, fmt.Errorf("invalid client configuration"))
			}

			includeBlocked, _ := cmd.Flags().GetBool("include-blocked")
			result, err := runner.ListOrgs(context.Background(), includeBlocked)
			if err != nil {
				return adminHandleError(cmd, err)
			}
			if err := adminPrintJSON(cmd, result); err != nil {
				return err
			}
			return nil
		},
	}
	listCmd.Flags().Bool("include-blocked", false, "Include blocked organizations in the response")

	// create subcommand — no positional args; --name and --slug required, --url optional.
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new organization",
		Args:  cobra.NoArgs,
		Annotations: map[string]string{
			"method": "POST",
			"path":   "/orgs",
			"auth":   "admin",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validate required flags in order: name, slug.
			name, err := adminCheckRequiredFlag(cmd, "name")
			if err != nil {
				return err
			}
			slug, err := adminCheckRequiredFlag(cmd, "slug")
			if err != nil {
				return err
			}

			// URL is optional — only set pointer when explicitly provided.
			var urlPtr *string
			if cmd.Flags().Changed("url") {
				urlStr, _ := cmd.Flags().GetString("url")
				urlPtr = &urlStr
			}

			raw := ClientFromContext(cmd.Context())
			if raw == nil {
				return adminHandleError(cmd, fmt.Errorf("configuration not loaded: missing endpoint URL or API key"))
			}
			runner, ok := raw.(*OrgsRunner)
			if !ok {
				return adminHandleError(cmd, fmt.Errorf("invalid client configuration"))
			}

			result, err := runner.CreateOrg(context.Background(), name, slug, urlPtr)
			if err != nil {
				return adminHandleError(cmd, err)
			}
			if err := adminPrintJSON(cmd, result); err != nil {
				return err
			}
			return nil
		},
	}
	createCmd.Flags().String("name", "", "Organization name")
	createCmd.Flags().String("slug", "", "Organization slug")
	createCmd.Flags().String("url", "", "Organization URL")

	// update subcommand — requires exactly one positional arg (org ID); --name and --url optional.
	updateCmd := &cobra.Command{
		Use:   "update",
		Short: "Update an organization by ID",
		Args:  adminCheckMissingArg("id"),
		Annotations: map[string]string{
			"method": "PATCH",
			"path":   "/orgs/:id",
			"auth":   "admin",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]

			// Build update request: set pointer fields only when flags are explicitly provided.
			var namePtr *string
			if cmd.Flags().Changed("name") {
				nameStr, _ := cmd.Flags().GetString("name")
				namePtr = &nameStr
			}
			var urlPtr *string
			if cmd.Flags().Changed("url") {
				urlStr, _ := cmd.Flags().GetString("url")
				urlPtr = &urlStr
			}

			// Warn on empty patch (neither flag provided).
			if namePtr == nil && urlPtr == nil {
				adminWarnf(cmd, "no fields specified for update")
			}

			raw := ClientFromContext(cmd.Context())
			if raw == nil {
				return adminHandleError(cmd, fmt.Errorf("configuration not loaded: missing endpoint URL or API key"))
			}
			runner, ok := raw.(*OrgsRunner)
			if !ok {
				return adminHandleError(cmd, fmt.Errorf("invalid client configuration"))
			}

			result, err := runner.UpdateOrg(context.Background(), id, namePtr, urlPtr)
			if err != nil {
				return adminHandleError(cmd, err)
			}
			if err := adminPrintJSON(cmd, result); err != nil {
				return err
			}
			return nil
		},
	}
	updateCmd.Flags().String("name", "", "Organization name")
	updateCmd.Flags().String("url", "", "Organization URL")

	// delete subcommand — requires exactly one positional arg (org ID).
	deleteCmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete an organization by ID",
		Args:  adminCheckMissingArg("id"),
		Annotations: map[string]string{
			"method": "DELETE",
			"path":   "/orgs/:id",
			"auth":   "admin",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}

	// block subcommand — requires exactly one positional arg (org ID).
	blockCmd := &cobra.Command{
		Use:   "block",
		Short: "Block an organization",
		Args:  adminCheckMissingArg("id"),
		Annotations: map[string]string{
			"method": "POST",
			"path":   "/orgs/:id/block",
			"auth":   "admin",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}

	// unblock subcommand — requires exactly one positional arg (org ID).
	unblockCmd := &cobra.Command{
		Use:   "unblock",
		Short: "Unblock an organization",
		Args:  adminCheckMissingArg("id"),
		Annotations: map[string]string{
			"method": "POST",
			"path":   "/orgs/:id/unblock",
			"auth":   "admin",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}

	cmd.AddCommand(
		listCmd,
		createCmd,
		updateCmd,
		deleteCmd,
		blockCmd,
		unblockCmd,
		newAdminOrgsMembersCmd(),
	)

	return cmd
}

// newAdminOrgsMembersCmd returns the admin orgs members sub-group with
// subcommands: list, add, remove.
// The parent command has no RunE — invoking it directly prints help.
func newAdminOrgsMembersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "members",
		Short: "Manage organization members (admin)",
	}

	// list subcommand — requires exactly one positional arg (org ID).
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List members of an organization",
		Args:  adminCheckMissingArg("id"),
		Annotations: map[string]string{
			"method": "GET",
			"path":   "/orgs/:id/members",
			"auth":   "admin",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}

	// add subcommand — requires exactly two positional args (org ID, user ID).
	addCmd := &cobra.Command{
		Use:   "add",
		Short: "Add a member to an organization",
		Args:  cobra.ExactArgs(2),
		Annotations: map[string]string{
			"method": "PUT",
			"path":   "/orgs/:id/members/:user_id",
			"auth":   "admin",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}

	// remove subcommand — requires exactly two positional args (org ID, user ID).
	removeCmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a member from an organization",
		Args:  cobra.ExactArgs(2),
		Annotations: map[string]string{
			"method": "DELETE",
			"path":   "/orgs/:id/members/:user_id",
			"auth":   "admin",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}

	cmd.AddCommand(
		listCmd,
		addCmd,
		removeCmd,
	)

	return cmd
}

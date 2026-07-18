package cli

import (
	"net/http"

	"github.com/spf13/cobra"
)

// NewOrgsCmd returns the Cobra parent command for `akc orgs`.
// It registers list, show, and members subcommands.
func NewOrgsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "orgs",
		Short: "Browse your organizations",
	}

	cmd.AddCommand(
		newOrgsListCmd(),
		newOrgsShowCmd(),
		newOrgsMembersCmd(),
	)

	return cmd
}

// newOrgsListCmd returns the `akc orgs list` subcommand.
// No flags or arguments. Calls GET /orgs and prints the
// []*Organization JSON array to stdout.
func newOrgsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List your organizations",
		Long:  "List all organizations the authenticated user belongs to.",
		Args:  cobra.NoArgs,
		Annotations: map[string]string{
			"auth":   "api_key",
			"method": "GET",
			"path":   "/orgs",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newAuthenticatedCmdClient(cmd)
			if err != nil {
				return cmdHandleError(cmd, err)
			}

			result, err := client.doRequest(cmd.Context(), http.MethodGet, "/orgs", nil)
			if err != nil {
				return cmdHandleError(cmd, err)
			}

			return cmdPrintJSON(cmd, result)
		},
	}
}

// newOrgsShowCmd returns the `akc orgs show` subcommand.
// Takes exactly one positional argument: org id (UUID).
// Calls GET /orgs/{id} and prints Organization JSON to stdout.
func newOrgsShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show organization details",
		Long:  "Retrieve and display details for a specific organization.",
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"auth":   "api_key",
			"method": "GET",
			"path":   "/orgs/:id",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newAuthenticatedCmdClient(cmd)
			if err != nil {
				return cmdHandleError(cmd, err)
			}

			id := args[0]
			result, err := client.doRequest(cmd.Context(), http.MethodGet, "/orgs/"+id, nil)
			if err != nil {
				return cmdHandleError(cmd, err)
			}

			return cmdPrintJSON(cmd, result)
		},
	}
}

// newOrgsMembersCmd returns the `akc orgs members` subcommand.
// Takes exactly one positional argument: org id (UUID).
// Calls GET /orgs/{id}/members and prints []*User JSON array to stdout.
func newOrgsMembersCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "members <id>",
		Short: "List organization members",
		Long:  "List all members of a specific organization.",
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"auth":   "api_key",
			"method": "GET",
			"path":   "/orgs/:id/members",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newAuthenticatedCmdClient(cmd)
			if err != nil {
				return cmdHandleError(cmd, err)
			}

			id := args[0]
			result, err := client.doRequest(cmd.Context(), http.MethodGet, "/orgs/"+id+"/members", nil)
			if err != nil {
				return cmdHandleError(cmd, err)
			}

			return cmdPrintJSON(cmd, result)
		},
	}
}

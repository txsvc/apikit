package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/spf13/cobra"
	"github.com/txsvc/apikit"
)

func main() {
	rootCmd := apikit.RootCommand()
	rootCmd.Use = "mycli"
	rootCmd.Short = "My custom CLI built on apikit"

	// Register all built-in commands.
	rootCmd.AddCommand(
		apikit.LoginCmd(),
		apikit.UserCmd(),
		apikit.KeysCmd(),
		apikit.TokensCmd(),
		apikit.OrgsCmd(),
		apikit.AdminCmd(),
	)

	// Add custom commands that use apikit's CLI client for API calls.
	rootCmd.AddCommand(widgetCmd())

	err := apikit.CLIExecute()
	if err != nil {
		apikit.CLIPrintError(err)
	}
	os.Exit(apikit.CLIExitCode(err))
}

// widgetCmd returns the parent command for "mycli widget" with CRUD
// subcommands. This demonstrates building custom API-backed commands
// using apikit's CLIClient, CLIPrintResult, and CLIHandleError.
func widgetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "widget",
		Short:         "Manage widgets",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	cmd.AddCommand(
		widgetListCmd(),
		widgetCreateCmd(),
		widgetGetCmd(),
		widgetDeleteCmd(),
	)

	return cmd
}

// widgetListCmd demonstrates a simple GET command.
func widgetListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all widgets",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Retrieve the authenticated client from context. apikit's
			// PersistentPreRunE has already resolved credentials from
			// flags, environment variables, and the config file.
			client, err := apikit.CLIClientFromCmd(cmd)
			if err != nil {
				return apikit.CLIHandleError(cmd, err)
			}

			// Make an authenticated GET request to the custom endpoint.
			result, err := client.DoRequest(cmd.Context(), http.MethodGet, "/widgets", nil)
			if err != nil {
				return apikit.CLIHandleError(cmd, err)
			}

			// Print the result as indented JSON to stdout.
			return apikit.CLIPrintResult(cmd, result)
		},
	}
}

// widgetCreateCmd demonstrates a POST command with flags.
func widgetCreateCmd() *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a widget",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return apikit.CLIHandleError(cmd,
					apikit.NewCLIError(2, "--name flag is required"))
			}

			client, err := apikit.CLIClientFromCmd(cmd)
			if err != nil {
				return apikit.CLIHandleError(cmd, err)
			}

			body := map[string]string{"name": name}
			result, err := client.DoRequest(cmd.Context(), http.MethodPost, "/widgets", body)
			if err != nil {
				return apikit.CLIHandleError(cmd, err)
			}

			return apikit.CLIPrintResult(cmd, result)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Widget name (required)")

	return cmd
}

// widgetGetCmd demonstrates a GET command with a positional argument.
func widgetGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Get widget details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := apikit.CLIClientFromCmd(cmd)
			if err != nil {
				return apikit.CLIHandleError(cmd, err)
			}

			result, err := client.DoRequest(cmd.Context(), http.MethodGet, "/widgets/"+args[0], nil)
			if err != nil {
				return apikit.CLIHandleError(cmd, err)
			}

			return apikit.CLIPrintResult(cmd, result)
		},
	}
}

// widgetDeleteCmd demonstrates a destructive command with a --confirm flag.
func widgetDeleteCmd() *cobra.Command {
	var confirm bool

	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a widget",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !confirm {
				return apikit.CLIHandleError(cmd,
					apikit.NewCLIError(2, "--confirm flag is required to delete a widget"))
			}

			client, err := apikit.CLIClientFromCmd(cmd)
			if err != nil {
				return apikit.CLIHandleError(cmd, err)
			}

			_, err = client.DoRequest(cmd.Context(), http.MethodDelete, "/widgets/"+args[0], nil)
			if err != nil {
				return apikit.CLIHandleError(cmd, err)
			}

			fmt.Fprintf(cmd.ErrOrStderr(), "Widget '%s' deleted.\n", args[0])
			return nil
		},
	}

	cmd.Flags().BoolVar(&confirm, "confirm", false, "Confirm deletion")

	return cmd
}

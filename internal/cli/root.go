package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// rootCmd is the package-level root Cobra command. RootCommand() always
// builds a fresh instance and stores it here so that Execute() can run it.
// Tests call RootCommand(), configure it (e.g., add subcommands, set args),
// and then either call rootCmd.Execute() directly or call Execute().
var rootCmd *cobra.Command

// RootCommand constructs and returns the root Cobra command for the CLI.
// The returned command has SilenceErrors and SilenceUsage set to true,
// persistent flags for --endpoint-url, --user-id, --api-key, and --json,
// and a PersistentPreRunE that handles auth annotation checks and
// credential resolution.
//
// Each call returns a fresh command tree and updates the package-level
// rootCmd so that Execute() always operates on the latest instance.
func RootCommand() *cobra.Command {
	rootCmd = &cobra.Command{
		Use:           "akc",
		Short:         "apikit client CLI",
		Long:          "akc is the CLI client for the apikit API server.",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	// Register persistent flags available to all subcommands.
	rootCmd.PersistentFlags().String("endpoint-url", "", "Server endpoint URL")
	rootCmd.PersistentFlags().String("user-id", "", "Authenticated user UUID")
	rootCmd.PersistentFlags().String("api-key", "", "API key")
	rootCmd.PersistentFlags().Bool("json", false, "Output in JSON format (for help commands)")

	// PersistentPreRunE is called by Cobra with the leaf command as cmd.
	// Only the root command defines PersistentPreRunE; child commands
	// use PreRunE only (13-REQ-12.E1).
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		// Check auth annotation on the leaf command (cmd), not on root.
		if cmd.Annotations != nil && cmd.Annotations["auth"] == "none" {
			return nil
		}

		// Validate TokenPrefix is non-empty (13-REQ-3.2).
		if TokenPrefix == "" {
			return fmt.Errorf("TokenPrefix is empty: binary was built without a valid -ldflags TokenPrefix value")
		}

		// Resolve $HOME for config directory.
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return fmt.Errorf("cannot determine home directory: $HOME is not set or unresolvable")
		}

		// Config directory path: $HOME/.<TokenPrefix>/
		configDir := home + "/." + TokenPrefix

		// Initialize config directory and config.toml if they don't exist.
		if err := InitConfig(configDir); err != nil {
			return err
		}

		// Load config.toml.
		cfg, err := LoadConfig(configDir)
		if err != nil {
			return err
		}

		// Resolve credentials using four-level precedence chain.
		flags := cmd.Root().PersistentFlags()

		// endpoint_url — required
		epFlag, _ := flags.GetString("endpoint-url")
		epChanged := flags.Changed("endpoint-url")
		var epConfig string
		if cfg != nil {
			epConfig = cfg.EndpointURL
		}
		endpointURL, err := ResolveField("endpoint_url", "--endpoint-url", epFlag, epChanged, "ENDPOINT_URL", epConfig, true)
		if err != nil {
			return err
		}

		// api_key — required
		akFlag, _ := flags.GetString("api-key")
		akChanged := flags.Changed("api-key")
		var akConfig string
		if cfg != nil {
			akConfig = cfg.APIKey
		}
		apiKey, err := ResolveField("api_key", "--api-key", akFlag, akChanged, "API_KEY", akConfig, true)
		if err != nil {
			return err
		}

		// user_id — optional (required: false)
		uidFlag, _ := flags.GetString("user-id")
		uidChanged := flags.Changed("user-id")
		var uidConfig string
		if cfg != nil {
			uidConfig = cfg.UserID
		}
		userID, _ := ResolveField("user_id", "--user-id", uidFlag, uidChanged, "USER_ID", uidConfig, false)

		// Construct the API client. We use the newClient function to avoid
		// importing the root apikit package (import cycle). The client is
		// stored as any in context.
		client := newAPIClient(endpointURL, apiKey)

		// Store client and user_id in context.
		ctx := cmd.Context()
		ctx = ContextWithClient(ctx, client)
		ctx = context.WithValue(ctx, userIDContextKey{}, userID)
		cmd.SetContext(ctx)

		return nil
	}

	return rootCmd
}

// newAPIClient creates an API client without importing apikit (avoiding cycle).
// It uses a lightweight client struct that wraps the endpoint and API key.
// In production, PersistentPreRunE will use the real apikit.NewClient.
//
// This is a package-level variable so it can be overridden for testing.
var newAPIClient = defaultNewAPIClient

// cliClient is a lightweight API client for use within internal/cli.
// It avoids importing the root apikit package (which would create a cycle).
type cliClient struct {
	EndpointURL string
	APIKey      string
}

func defaultNewAPIClient(endpointURL, apiKey string) any {
	return &cliClient{
		EndpointURL: endpointURL,
		APIKey:      apiKey,
	}
}

// Execute wraps rootCmd.Execute() with centralized error handling.
// It returns the error from rootCmd.Execute() so the caller can
// call PrintError and ExitCode as needed.
//
// Note: The caller (or test harness) is responsible for calling
// PrintError(err) when err is non-nil. This avoids double-printing
// in test scenarios where the caller also handles the error.
//
// If RootCommand() has not been called yet, Execute() initializes it.
func Execute() error {
	if rootCmd == nil {
		RootCommand()
	}
	return rootCmd.Execute()
}


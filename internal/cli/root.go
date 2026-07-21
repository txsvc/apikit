package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
	}

	// Register persistent flags available to all subcommands.
	rootCmd.PersistentFlags().String("endpoint-url", "", "Server endpoint URL")
	rootCmd.PersistentFlags().String("user-id", "", "Authenticated user UUID")
	rootCmd.PersistentFlags().String("api-key", "", "API key")
	rootCmd.PersistentFlags().Bool("json", false, "Output in JSON format (for help commands)")

	// Register subcommands.
	rootCmd.AddCommand(newVersionCmd())

	// Register custom help command and SetHelpFunc for --json support.
	registerHelpCommand(rootCmd)

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
		configDir := filepath.Join(home, "."+TokenPrefix)

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
		// Use cmd.Flags() (the leaf command's merged flag set) for flag
		// detection, consistent with Cobra passing the leaf command to
		// PersistentPreRunE (13-REQ-6.4).

		// endpoint_url — required
		epFlag, _ := cmd.Flags().GetString("endpoint-url")
		epChanged := cmd.Flags().Changed("endpoint-url")
		var epConfig string
		if cfg != nil {
			epConfig = cfg.EndpointURL
		}
		endpointURL, err := ResolveField("endpoint_url", "--endpoint-url", epFlag, epChanged, "ENDPOINT_URL", epConfig, true)
		if err != nil {
			return err
		}

		// api_key — required
		akFlag, _ := cmd.Flags().GetString("api-key")
		akChanged := cmd.Flags().Changed("api-key")
		var akConfig string
		if cfg != nil {
			akConfig = cfg.APIKey
		}
		apiKey, err := ResolveField("api_key", "--api-key", akFlag, akChanged, "API_KEY", akConfig, true)
		if err != nil {
			return err
		}

		// user_id — optional (required: false)
		uidFlag, _ := cmd.Flags().GetString("user-id")
		uidChanged := cmd.Flags().Changed("user-id")
		var uidConfig string
		if cfg != nil {
			uidConfig = cfg.UserID
		}
		userID, _ := ResolveField("user_id", "--user-id", uidFlag, uidChanged, "USER_ID", uidConfig, false)

		// Construct the API client.
		client := newAPIClient(endpointURL, apiKey)

		// For admin commands, wrap the client in the appropriate Runner
		// so admin command RunE functions can type-assert it.
		if isAdminCommand(cmd) {
			client = buildAdminRunner(client, cmd)
		}

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

func defaultNewAPIClient(endpointURL, apiKey string) any {
	return &CmdClient{
		endpointURL: endpointURL,
		apiKey:      apiKey,
	}
}

// isAdminCommand returns true if cmd is in the "admin" subtree.
func isAdminCommand(cmd *cobra.Command) bool {
	for p := cmd.Parent(); p != nil; p = p.Parent() {
		if p.Name() == "admin" {
			return true
		}
	}
	return false
}

// adminCommandGroup returns the admin subgroup name (users, orgs, keys, tokens)
// for a leaf command inside the admin tree.
func adminCommandGroup(cmd *cobra.Command) string {
	for p := cmd; p != nil; p = p.Parent() {
		if pp := p.Parent(); pp != nil && pp.Name() == "admin" {
			return p.Name()
		}
	}
	return ""
}

// buildAdminRunner wraps a *CmdClient in the Runner type expected by the
// admin command group that cmd belongs to.
func buildAdminRunner(raw any, cmd *cobra.Command) any {
	c, ok := raw.(*CmdClient)
	if !ok {
		return raw
	}
	switch adminCommandGroup(cmd) {
	case "users":
		return buildUsersRunner(c)
	case "orgs":
		return buildOrgsRunner(c)
	case "keys":
		return buildKeysRunner(c)
	case "tokens":
		return buildTokensRunner(c)
	default:
		return raw
	}
}

func buildUsersRunner(c *CmdClient) *UsersRunner {
	return &UsersRunner{
		ListUsers: func(ctx context.Context, includeBlocked bool) (any, error) {
			path := "/users"
			if includeBlocked {
				path += "?include_blocked=true"
			}
			return c.DoRequest(ctx, "GET", path, nil)
		},
		GetUserByID: func(ctx context.Context, id string) (any, error) {
			return c.DoRequest(ctx, "GET", "/users/"+id, nil)
		},
		CreateUser: func(ctx context.Context, username, email, provider, providerID string) (any, error) {
			body := map[string]string{
				"username": username, "email": email,
				"provider": provider, "provider_id": providerID,
			}
			return c.DoRequest(ctx, "POST", "/users", body)
		},
		UpdateUserByID: func(ctx context.Context, id string, fullName string) (any, error) {
			return c.DoRequest(ctx, "PATCH", "/users/"+id, map[string]string{"full_name": fullName})
		},
		PromoteUser: func(ctx context.Context, id string) (any, error) {
			return c.DoRequest(ctx, "POST", "/users/"+id+"/promote", nil)
		},
		DemoteUser: func(ctx context.Context, id string) (any, error) {
			return c.DoRequest(ctx, "POST", "/users/"+id+"/demote", nil)
		},
		BlockUser: func(ctx context.Context, id string) (any, error) {
			return c.DoRequest(ctx, "POST", "/users/"+id+"/block", nil)
		},
		UnblockUser: func(ctx context.Context, id string) (any, error) {
			return c.DoRequest(ctx, "POST", "/users/"+id+"/unblock", nil)
		},
	}
}

func buildOrgsRunner(c *CmdClient) *OrgsRunner {
	return &OrgsRunner{
		ListOrgs: func(ctx context.Context, includeBlocked bool) (any, error) {
			path := "/orgs"
			if includeBlocked {
				path += "?include_blocked=true"
			}
			return c.DoRequest(ctx, "GET", path, nil)
		},
		CreateOrg: func(ctx context.Context, name, slug string, url *string) (any, error) {
			body := map[string]any{"name": name, "slug": slug}
			if url != nil {
				body["url"] = *url
			}
			return c.DoRequest(ctx, "POST", "/orgs", body)
		},
		UpdateOrg: func(ctx context.Context, id string, name *string, url *string) (any, error) {
			body := map[string]any{}
			if name != nil {
				body["name"] = *name
			}
			if url != nil {
				body["url"] = *url
			}
			return c.DoRequest(ctx, "PATCH", "/orgs/"+id, body)
		},
		DeleteOrg: func(ctx context.Context, id string) error {
			_, err := c.DoRequest(ctx, "DELETE", "/orgs/"+id, nil)
			return err
		},
		BlockOrg: func(ctx context.Context, id string) (any, error) {
			return c.DoRequest(ctx, "POST", "/orgs/"+id+"/block", nil)
		},
		UnblockOrg: func(ctx context.Context, id string) (any, error) {
			return c.DoRequest(ctx, "POST", "/orgs/"+id+"/unblock", nil)
		},
		ListOrgMembers: func(ctx context.Context, orgID string) (any, error) {
			return c.DoRequest(ctx, "GET", "/orgs/"+orgID+"/members", nil)
		},
		AddOrgMember: func(ctx context.Context, orgID, userID string) error {
			_, err := c.DoRequest(ctx, "PUT", "/orgs/"+orgID+"/members/"+userID, nil)
			return err
		},
		RemoveOrgMember: func(ctx context.Context, orgID, userID string) error {
			_, err := c.DoRequest(ctx, "DELETE", "/orgs/"+orgID+"/members/"+userID, nil)
			return err
		},
	}
}

func buildKeysRunner(c *CmdClient) *KeysRunner {
	return &KeysRunner{
		ListUserKeys: func(ctx context.Context, userID string) (any, error) {
			return c.DoRequest(ctx, "GET", "/users/"+userID+"/keys", nil)
		},
		RevokeUserKey: func(ctx context.Context, userID, keyID string) error {
			_, err := c.DoRequest(ctx, "DELETE", "/users/"+userID+"/keys/"+keyID, nil)
			return err
		},
	}
}

func buildTokensRunner(c *CmdClient) *TokensRunner {
	return &TokensRunner{
		ListUserTokens: func(ctx context.Context, userID string) (any, error) {
			return c.DoRequest(ctx, "GET", "/users/"+userID+"/tokens", nil)
		},
		RevokeUserToken: func(ctx context.Context, userID, tokenID string) error {
			_, err := c.DoRequest(ctx, "DELETE", "/users/"+userID+"/tokens/"+tokenID, nil)
			return err
		},
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


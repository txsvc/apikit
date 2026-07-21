package cli

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

// NewKeysCmd returns the Cobra parent command for `akc keys`.
// It registers list, refresh, and revoke subcommands.
func NewKeysCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keys",
		Short: "Manage your API keys",
	}

	cmd.AddCommand(
		newKeysListCmd(),
		newKeysRefreshCmd(),
		newKeysRevokeCmd(),
	)

	return cmd
}

// newKeysListCmd returns the `akc keys list` subcommand.
// No flags or arguments. Calls GET /user/keys and prints the
// []*APIKeyMeta JSON array to stdout.
func newKeysListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List your API keys",
		Long:  "List all API keys associated with the authenticated user.",
		Args:  cobra.NoArgs,
		Annotations: map[string]string{
			"auth":   "api_key",
			"method": "GET",
			"path":   "/user/keys",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := NewAuthenticatedCmdClient(cmd)
			if err != nil {
				return CmdHandleError(cmd, err)
			}

			result, err := client.DoRequest(cmd.Context(), http.MethodGet, "/user/keys", nil)
			if err != nil {
				return CmdHandleError(cmd, err)
			}

			return CmdPrintJSON(cmd, result)
		},
	}
}

// newKeysRefreshCmd returns the `akc keys refresh` subcommand.
// No flags or arguments. Parses key_id from the current api_key,
// calls POST /user/keys/:keyID/refresh, updates config with the
// new key, and prints the APIKeyFull JSON to stdout.
func newKeysRefreshCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "refresh",
		Short: "Refresh your API key",
		Long:  "Rotate the current API key — generates a new secret while keeping the same key_id. The new key is automatically saved to the config file.",
		Args:  cobra.NoArgs,
		Annotations: map[string]string{
			"auth":   "api_key",
			"method": "POST",
			"path":   "/user/keys/:key_id/refresh",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := NewAuthenticatedCmdClient(cmd)
			if err != nil {
				return CmdHandleError(cmd, err)
			}

			// Parse key_id from the configured api_key.
			keyID, err := parseKeyID(client.apiKey)
			if err != nil {
				return CmdHandleError(cmd, &CmdError{code: 2, message: err.Error()})
			}

			// Call the refresh endpoint.
			result, err := client.DoRequest(cmd.Context(), http.MethodPost, "/user/keys/"+keyID+"/refresh", nil)
			if err != nil {
				return CmdHandleError(cmd, err)
			}

			// Extract the new key from the response for config update.
			resultMap, ok := result.(map[string]any)
			if !ok {
				return CmdHandleError(cmd, &CmdError{code: 2, message: "unexpected response format"})
			}

			newKey, _ := resultMap["key"].(string)
			if newKey == "" {
				return CmdHandleError(cmd, &CmdError{code: 2, message: "response missing key field"})
			}

			// Update config with the new key via atomic write.
			saveFn := client.saveConfigFn
			if saveFn == nil {
				saveFn = SaveConfig
			}
			cfg := &CLIConfig{
				EndpointURL: client.endpointURL,
				APIKey:      newKey,
				UserID:      UserIDFromContext(cmd.Context()),
			}
			if err := saveFn(client.configPath, cfg); err != nil {
				// Config write failure — do NOT print the new key.
				return CmdHandleError(cmd, &CmdError{
					code:    2,
					message: fmt.Sprintf("failed to save config: %v", err),
				})
			}

			// Success: print response JSON and status message.
			if err := CmdPrintJSON(cmd, result); err != nil {
				return err
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "API key refreshed")

			return nil
		},
	}
}

// newKeysRevokeCmd returns the `akc keys revoke` subcommand.
// No flags or arguments. Parses key_id from the current api_key,
// calls DELETE /user/keys/:keyID, clears api_key and user_id in
// config, and prints the RevokeKeyResponse JSON to stdout.
func newKeysRevokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke",
		Short: "Revoke your API key",
		Long:  "Invalidate the current API key immediately and clear it from the config file. Run 'akc login' afterwards to obtain a new key.",
		Args:  cobra.NoArgs,
		Annotations: map[string]string{
			"auth":   "api_key",
			"method": "DELETE",
			"path":   "/user/keys/:key_id",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := NewAuthenticatedCmdClient(cmd)
			if err != nil {
				return CmdHandleError(cmd, err)
			}

			// Parse key_id from the configured api_key.
			keyID, err := parseKeyID(client.apiKey)
			if err != nil {
				return CmdHandleError(cmd, &CmdError{code: 2, message: err.Error()})
			}

			// Call the revoke endpoint.
			result, err := client.DoRequest(cmd.Context(), http.MethodDelete, "/user/keys/"+keyID, nil)
			if err != nil {
				return CmdHandleError(cmd, err)
			}

			// Clear api_key and user_id in config via atomic write.
			saveFn := client.saveConfigFn
			if saveFn == nil {
				saveFn = SaveConfig
			}
			cfg := &CLIConfig{
				EndpointURL: client.endpointURL,
				APIKey:      "",
				UserID:      "",
			}
			if err := saveFn(client.configPath, cfg); err != nil {
				// Config write failure — do NOT print the response data.
				return CmdHandleError(cmd, &CmdError{
					code:    2,
					message: fmt.Sprintf("failed to save config: %v", err),
				})
			}

			// Success: print response JSON and revocation message.
			if err := CmdPrintJSON(cmd, result); err != nil {
				return err
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "API key revoked. Run 'akc login' to obtain a new key.")

			return nil
		},
	}
}

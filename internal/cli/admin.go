// Package cli implements the akc CLI command tree.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

// NewAdminCmd returns the root *cobra.Command for the admin command tree.
// It registers users, orgs, keys, and tokens subcommand groups as children.
// The admin command itself has no RunE — invoking it directly prints help.
func NewAdminCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Administrative commands for managing users, orgs, keys, and tokens",
	}

	cmd.AddCommand(
		newAdminUsersCmd(),
		newAdminOrgsCmd(),
		newAdminKeysCmd(),
		newAdminTokensCmd(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// Shared helpers for admin commands.
// These are unexported to satisfy TestOnlyNewAdminCmdExported.
// They avoid importing the root apikit package to prevent import cycles.
// ---------------------------------------------------------------------------

// adminPrintJSON serializes a value as indented JSON and writes it to
// the command's stdout (cmd.OutOrStdout()). Uses two-space indentation
// per REQ-25.1.
func adminPrintJSON(cmd *cobra.Command, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("json marshal: %w", err)
	}
	_, writeErr := fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return writeErr
}

// codedError is an interface matched by any error that exposes
// ErrorCode() and ErrorMessage() methods. apikit.APIError satisfies
// this interface, allowing admin commands to detect API errors without
// importing the root apikit package (which would create an import cycle).
type codedError interface {
	ErrorCode() int
	ErrorMessage() string
}

// adminHandleError writes a JSON error envelope to stdout and returns
// the original error. For API errors (satisfying codedError), the
// envelope uses the HTTP status code. For other errors, code is 0.
func adminHandleError(cmd *cobra.Command, err error) error {
	code := 0
	msg := err.Error()

	var ce codedError
	if errors.As(err, &ce) {
		code = ce.ErrorCode()
		msg = ce.ErrorMessage()
	}

	envelope := map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": msg,
		},
	}
	_ = adminPrintJSON(cmd, envelope)
	return &printedError{err}
}

// adminCheckMissingArg returns a Cobra PositionalArgs validator that:
// - On missing arg (0 args): writes a JSON error envelope to stdout and
//   returns an error. The envelope message is "missing required argument: <name>".
// - On correct arg count (1 arg): passes validation.
// - On extra args (>1 args): returns a Cobra-style error (no JSON envelope).
func adminCheckMissingArg(argName string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < 1 {
			err := fmt.Errorf("missing required argument: %s", argName)
			_ = adminHandleError(cmd, err)
			return err
		}
		if len(args) > 1 {
			return fmt.Errorf("accepts 1 arg(s), received %d", len(args))
		}
		return nil
	}
}

// adminCheckRequiredFlag checks whether a flag was explicitly provided on the
// command line. If not, it writes a JSON error envelope to stdout and returns
// an error. The flag's value (including empty string) is returned on success.
func adminCheckRequiredFlag(cmd *cobra.Command, flagName string) (string, error) {
	if !cmd.Flags().Changed(flagName) {
		err := fmt.Errorf("missing required flag: --%s", flagName)
		_ = adminHandleError(cmd, err)
		return "", err
	}
	val, _ := cmd.Flags().GetString(flagName)
	return val, nil
}

// adminCheckTwoArgs returns a Cobra PositionalArgs validator for commands
// that require exactly two positional arguments. On missing first arg, writes
// a JSON error envelope with "missing required argument: <arg1Name>". On
// missing second arg, writes "missing required argument: <arg2Name>".
func adminCheckTwoArgs(arg1Name, arg2Name string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < 1 {
			err := fmt.Errorf("missing required argument: %s", arg1Name)
			_ = adminHandleError(cmd, err)
			return err
		}
		if len(args) < 2 {
			err := fmt.Errorf("missing required argument: %s", arg2Name)
			_ = adminHandleError(cmd, err)
			return err
		}
		if len(args) > 2 {
			return fmt.Errorf("accepts 2 arg(s), received %d", len(args))
		}
		return nil
	}
}

// adminWarnf writes a warning message to stderr via cmd.ErrOrStderr().
func adminWarnf(cmd *cobra.Command, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s\n", msg)
}

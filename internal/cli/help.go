package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// =========================================================================
// Agent interface: help --json command tree walker (13-REQ-11)
//
// The help --json output provides a machine-readable description of all
// available CLI commands, enabling LLMs and autonomous agents to discover
// and invoke commands without prior knowledge of the CLI's structure.
// =========================================================================

// commandTreeOutput is the root JSON object for help --json output.
type commandTreeOutput struct {
	Name     string         `json:"name"`
	Version  string         `json:"version"`
	Commands []commandEntry `json:"commands"`
}

// commandEntry describes a single leaf command in the help --json output.
type commandEntry struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Method      *string          `json:"method"`
	Path        *string          `json:"path"`
	Args        []argDescriptor  `json:"args"`
	Flags       []flagDescriptor `json:"flags"`
	Auth        string           `json:"auth"`
	Composite   bool             `json:"composite,omitempty"`
}

// argDescriptor describes a positional argument in the help --json output.
type argDescriptor struct {
	Name string `json:"name"`
}

// flagDescriptor describes a flag in the help --json output.
// The Default field is omitted when the value equals the type's zero value
// (empty string, 0, false, empty array) — per 13-REQ-11.4.
type flagDescriptor struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Default     any    `json:"default,omitempty"`
	Description string `json:"description"`
}

// walkCommands recursively traverses the Cobra command tree and collects
// leaf commands: those with RunE set and at least one key in their
// Annotations map. Group/parent commands without RunE are excluded.
// Commands with both RunE and child subcommands are included as leaves
// (13-REQ-11.E2). Cobra built-ins without annotations are excluded
// (13-REQ-11.6).
func walkCommands(cmd *cobra.Command) []commandEntry {
	var entries []commandEntry

	// Check if this command is a leaf: has RunE and at least one annotation.
	if cmd.RunE != nil && len(cmd.Annotations) > 0 {
		entries = append(entries, buildCommandEntry(cmd))
	}

	// Recurse into children regardless of whether the parent is a leaf,
	// so that dual-mode commands (RunE + children) have both themselves
	// and their children collected.
	for _, child := range cmd.Commands() {
		entries = append(entries, walkCommands(child)...)
	}

	return entries
}

// buildCommandEntry constructs a commandEntry from a Cobra command.
func buildCommandEntry(cmd *cobra.Command) commandEntry {
	entry := commandEntry{
		Name:        cmd.CommandPath(),
		Description: cmd.Short,
		Auth:        cmd.Annotations["auth"],
	}

	// Method and path: string pointers so they can be null for composite commands.
	if method, ok := cmd.Annotations["method"]; ok {
		entry.Method = &method
	}
	if path, ok := cmd.Annotations["path"]; ok {
		entry.Path = &path
	}

	// Composite flag from annotations.
	if cmd.Annotations["composite"] == "true" {
		entry.Composite = true
	}

	// Extract positional args from the Use string.
	// Cobra's Use is "command <arg1> <arg2>" — extract tokens after the first word.
	entry.Args = extractArgs(cmd.Use)

	// Extract flag descriptors.
	entry.Flags = extractFlags(cmd)

	return entry
}

// extractArgs parses positional argument names from a Cobra Use string.
// The Use string format is "command <arg1> [optional]" — tokens after the
// first word that look like argument placeholders are collected.
func extractArgs(use string) []argDescriptor {
	var args []argDescriptor
	parts := strings.Fields(use)
	if len(parts) <= 1 {
		return args
	}
	for _, part := range parts[1:] {
		// Strip angle brackets and square brackets to get the arg name.
		name := strings.TrimLeft(part, "<[")
		name = strings.TrimRight(name, ">]")
		if name != "" {
			args = append(args, argDescriptor{Name: name})
		}
	}
	return args
}

// extractFlags collects flag descriptors from a command's local and
// inherited persistent flags, excluding hidden flags and the built-in
// --help flag (which Cobra adds automatically).
func extractFlags(cmd *cobra.Command) []flagDescriptor {
	var flags []flagDescriptor

	// Collect local flags.
	cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" {
			return
		}
		flags = append(flags, buildFlagDescriptor(f))
	})

	// Collect inherited persistent flags (from parent commands).
	cmd.InheritedFlags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" || f.Name == "json" {
			return
		}
		flags = append(flags, buildFlagDescriptor(f))
	})

	return flags
}

// buildFlagDescriptor creates a flagDescriptor from a pflag.Flag.
// The default value is omitted when it equals the type's zero value
// (empty string for "string", "0" for "int"/"count", "false" for "bool",
// "[]" for slice types) — per 13-REQ-11.4.
func buildFlagDescriptor(f *pflag.Flag) flagDescriptor {
	fd := flagDescriptor{
		Name:        "--" + f.Name,
		Type:        mapFlagType(f.Value.Type()),
		Required:    isFlagRequired(f),
		Description: f.Usage,
	}

	// Include default only when it's not the type's zero value.
	defVal := f.DefValue
	switch fd.Type {
	case "string":
		if defVal != "" {
			fd.Default = defVal
		}
	case "int":
		if defVal != "" && defVal != "0" {
			if v, err := strconv.Atoi(defVal); err == nil {
				fd.Default = v
			}
		}
	case "bool":
		if defVal == "true" {
			fd.Default = true
		}
		// "false" is zero → omitted.
	case "stringSlice":
		if defVal != "" && defVal != "[]" {
			fd.Default = defVal
		}
	default:
		if defVal != "" {
			fd.Default = defVal
		}
	}

	return fd
}

// mapFlagType normalizes pflag type names to the spec's vocabulary.
func mapFlagType(pflagType string) string {
	switch pflagType {
	case "string":
		return "string"
	case "int", "int32", "int64":
		return "int"
	case "bool":
		return "bool"
	case "stringSlice":
		return "stringSlice"
	default:
		return pflagType
	}
}

// isFlagRequired checks if a flag has the Cobra required annotation set.
func isFlagRequired(f *pflag.Flag) bool {
	if f.Annotations == nil {
		return false
	}
	ann, ok := f.Annotations[cobra.BashCompOneRequiredFlag]
	return ok && len(ann) > 0 && ann[0] == "true"
}

// buildCommandTree builds the full help --json output for the root command.
func buildCommandTree(root *cobra.Command) commandTreeOutput {
	return commandTreeOutput{
		Name:     root.Name(),
		Version:  Version,
		Commands: walkCommands(root),
	}
}

// =========================================================================
// Custom help subcommand and SetHelpFunc (13-REQ-11.1, 13-REQ-11.5)
// =========================================================================

// registerHelpCommand sets up the custom help subcommand and SetHelpFunc
// on the root command. This replaces Cobra's default help behavior:
//
//   - The custom help subcommand outputs the full JSON command tree when
//     --json is set; otherwise, it delegates to Cobra's default help.
//   - SetHelpFunc on the root outputs single-command JSON when --json
//     is set on per-command help (e.g., `akc version --help --json`);
//     otherwise, it falls back to Cobra's default help text.
func registerHelpCommand(root *cobra.Command) {
	// Capture Cobra's default help function before we override it.
	// HelpFunc() returns a default closure (using help templates) even
	// before InitDefaultHelpCmd is called, so no explicit initialization
	// is needed.
	defaultHelpFunc := root.HelpFunc()

	// Custom help subcommand that replaces Cobra's default "help" command.
	helpCmd := &cobra.Command{
		Use:   "help [command]",
		Short: "Help about any command",
		Long: `Help provides help for any command in the application.
Simply type ` + root.Name() + ` help [path to command] for full details.`,
		// Mark as auth-exempt so PersistentPreRunE skips credential resolution.
		Annotations: map[string]string{
			"auth": "none",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonFlag := isJSONFlagSet(cmd)
			if jsonFlag {
				// --json is set: output the full command tree, ignoring
				// positional arguments (13-REQ-11.E1).
				tree := buildCommandTree(cmd.Root())
				return PrintJSON(tree)
			}

			// --json not set: delegate to Cobra's default help behavior.
			// Find the target command from positional args.
			targetCmd, _, err := cmd.Root().Find(args)
			if err != nil || targetCmd == nil {
				targetCmd = cmd.Root()
			}
			defaultHelpFunc(targetCmd, args)
			return nil
		},
	}

	// Register our custom help command both as a child (AddCommand) and as
	// the designated help command (SetHelpCommand). Setting both ensures:
	// - AddCommand: the command is immediately available in the tree
	// - SetHelpCommand: Cobra's InitDefaultHelpCmd() (called during Execute)
	//   will see c.helpCommand != nil, skip creating a default, and just
	//   re-add our command (RemoveCommand + AddCommand — a harmless no-op
	//   since it's already registered)
	root.AddCommand(helpCmd)
	root.SetHelpCommand(helpCmd)

	// Override the root's help function for per-command --help requests.
	// This fires when a user invokes e.g. `akc version --help --json`.
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		jsonFlag := isJSONFlagSet(cmd)
		if jsonFlag {
			// --json is set: output a single command entry.
			if cmd.RunE != nil && len(cmd.Annotations) > 0 {
				if err := PrintJSON(buildCommandEntry(cmd)); err != nil {
					fmt.Fprintln(cmd.OutOrStdout(), err)
				}
			} else {
				// For non-leaf commands (e.g., root or groups), output the full tree.
				tree := buildCommandTree(cmd.Root())
				if err := PrintJSON(tree); err != nil {
					fmt.Fprintln(cmd.OutOrStdout(), err)
				}
			}
			return
		}

		// --json not set: fall back to Cobra's default help.
		defaultHelpFunc(cmd, args)
	})
}

// isJSONFlagSet checks whether the persistent --json flag was set on the
// root command. It first tries the merged Flags() set (which reflects
// parsed values), then falls back to the root's PersistentFlags().
func isJSONFlagSet(cmd *cobra.Command) bool {
	// Try the command's own merged flags first (covers both local and inherited).
	if f := cmd.Flags().Lookup("json"); f != nil && f.Changed {
		return true
	}
	// Fall back to root's persistent flags (covers the case where
	// flag parsing happened on a different command in the tree).
	if f := cmd.Root().PersistentFlags().Lookup("json"); f != nil && f.Changed {
		return true
	}
	return false
}

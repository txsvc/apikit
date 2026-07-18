// Package main provides the entry point for the akc CLI binary.
package main

import (
	"os"

	"github.com/txsvc/apikit/internal/cli"
)

func main() {
	// Build the root command and register all command groups explicitly.
	// No init() functions are used for registration (15-REQ-1.1).
	rootCmd := cli.RootCommand()
	rootCmd.AddCommand(
		cli.NewLoginCmd(),
		cli.NewUserCmd(),
		cli.NewKeysCmd(),
		cli.NewTokensCmd(),
		cli.NewOrgsCmd(),
	)

	err := cli.Execute()
	if err != nil {
		cli.PrintError(err)
	}
	os.Exit(cli.ExitCode(err))
}

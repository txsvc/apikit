// Package main provides the entry point for the akc CLI binary.
package main

import (
	"os"

	"github.com/txsvc/apikit/internal/cli"
)

func main() {
	err := cli.Execute()
	if err != nil {
		cli.PrintError(err)
	}
	os.Exit(cli.ExitCode(err))
}

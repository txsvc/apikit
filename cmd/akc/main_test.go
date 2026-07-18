package main

import (
	"os"
	"strings"
	"testing"
)

// TestMainGoSourceHasNoDirectOutput verifies that main.go contains only
// cli.Execute() and os.Exit(cli.ExitCode(err)) calls, and does not produce
// any output of its own (no fmt.Print, no os.Stdout writes).
func TestMainGoSourceHasNoDirectOutput(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("failed to read main.go: %v", err)
	}
	source := string(src)

	// Must call cli.Execute()
	if !strings.Contains(source, "cli.Execute()") {
		t.Error("main.go must call cli.Execute()")
	}

	// Must call os.Exit with cli.ExitCode
	if !strings.Contains(source, "os.Exit(") {
		t.Error("main.go must call os.Exit()")
	}
	if !strings.Contains(source, "cli.ExitCode(") {
		t.Error("main.go must call cli.ExitCode()")
	}

	// Must NOT contain any direct output calls
	forbidden := []string{
		"fmt.Print(",
		"fmt.Println(",
		"fmt.Printf(",
		"fmt.Fprint(",
		"fmt.Fprintln(",
		"fmt.Fprintf(",
		"os.Stdout.Write(",
		"os.Stdout.WriteString(",
		"os.Stderr.Write(",
		"os.Stderr.WriteString(",
		"log.Print(",
		"log.Println(",
		"log.Printf(",
		"log.Fatal(",
	}

	for _, f := range forbidden {
		if strings.Contains(source, f) {
			t.Errorf("main.go must not contain %q — all output should come from Execute()", f)
		}
	}
}

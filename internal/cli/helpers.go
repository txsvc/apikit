package cli

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// parseKeyID extracts the key_id from a full API key string formatted
// as <prefix>_<key_id>_<secret>. Returns the penultimate segment when
// split on underscore. Handles prefixes with or without embedded
// underscores by always taking parts[len(parts)-2].
func parseKeyID(apiKey string) (string, error) {
	parts := strings.Split(apiKey, "_")
	if len(parts) < 3 {
		return "", fmt.Errorf("invalid API key format")
	}
	return parts[len(parts)-2], nil
}

// validateExpires validates that an --expires integer is one of the
// allowed values: 0, 30, 60, or 90.
func validateExpires(v int) error {
	switch v {
	case 0, 30, 60, 90:
		return nil
	default:
		return fmt.Errorf("--expires must be 0, 30, 60, or 90")
	}
}

// parsePermissions splits a comma-separated permissions string, trims
// whitespace from each entry, discards empty entries, and returns the
// resulting []string. Returns an error if the resulting slice is empty.
// Individual entry format (e.g., resource_type:action) is not validated.
func parsePermissions(s string) ([]string, error) {
	raw := strings.Split(s, ",")
	var result []string
	for _, entry := range raw {
		trimmed := strings.TrimSpace(entry)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("--permissions must not be empty")
	}
	return result, nil
}

// execRunner is the function signature for creating exec.Cmd objects.
// Matches the signature of exec.Command for dependency injection.
type execRunner func(name string, arg ...string) *exec.Cmd

// openBrowser opens a URL in the user's default browser using platform-
// appropriate commands. It delegates to openBrowserWith using the real
// runtime.GOOS and exec.Command.
func openBrowser(url string) error {
	return openBrowserWith(runtime.GOOS, exec.Command, url)
}

// openBrowserWith opens a URL using the given exec runner and platform
// string. This is the testable version of openBrowser.
func openBrowserWith(goos string, execFn execRunner, url string) error {
	var cmd *exec.Cmd
	switch goos {
	case "darwin":
		cmd = execFn("open", url)
	case "linux":
		cmd = execFn("xdg-open", url)
	default:
		return fmt.Errorf("unsupported platform: %s", goos)
	}
	return cmd.Run()
}

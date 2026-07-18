package cli

import (
	"os/exec"
)

// parseKeyID extracts the key_id from a full API key string formatted
// as <prefix>_<key_id>_<secret>. Returns the penultimate segment when
// split on underscore.
// Stub — will be implemented in task group 5.
func parseKeyID(_ string) (string, error) {
	return "", nil
}

// validateExpires validates that an --expires integer is one of the
// allowed values: 0, 30, 60, or 90.
// Stub — will be implemented in task group 5.
func validateExpires(_ int) error {
	return nil
}

// parsePermissions splits a comma-separated permissions string, trims
// whitespace from each entry, discards empty entries, and returns the
// resulting []string. Returns an error if the resulting slice is empty.
// Stub — will be implemented in task group 5.
func parsePermissions(_ string) ([]string, error) {
	return nil, nil
}

// execRunner is the function signature for creating exec.Cmd objects.
// Matches the signature of exec.Command for dependency injection.
type execRunner func(name string, arg ...string) *exec.Cmd

// openBrowser opens a URL in the user's default browser using platform-
// appropriate commands. It delegates to openBrowserWith using the real
// runtime.GOOS and exec.Command.
// Stub — will be implemented in task group 5.
func openBrowser(_ string) error {
	return nil
}

// openBrowserWith opens a URL using the given exec runner and platform
// string. This is the testable version of openBrowser.
// Stub — will be implemented in task group 5.
func openBrowserWith(_ string, _ execRunner, _ string) error {
	return nil
}

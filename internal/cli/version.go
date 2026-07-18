package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// VersionOutput defines the JSON output for "akc version".
//
// ServerVersion is typed as `any` rather than `*apikit.VersionResponse`
// because `internal/cli` cannot import the root `apikit` package (the root
// package already imports `internal/cli` in cli.go, which would create a
// cycle). The actual value stored at runtime will be a *versionResponse
// (a local mirror of apikit.VersionResponse); json.Marshal handles both
// identically.
type VersionOutput struct {
	CLIVersion    string `json:"cli_version"`
	Build         string `json:"build"`
	Prefix        string `json:"prefix"`
	ServerVersion any    `json:"server_version,omitempty"`
}

// versionResponse mirrors apikit.VersionResponse for use within internal/cli,
// avoiding the import cycle. The JSON tags match exactly so marshal output
// is identical to the SDK type.
type versionResponse struct {
	Version    string `json:"version"`
	BuildTime  string `json:"build_time"`
	Commit     string `json:"commit"`
	MountPoint string `json:"mount_point"`
}

// versionTimeout is the context deadline for the server version fetch.
// Exported as a variable so tests can verify the command respects it.
var versionTimeout = 5 * time.Second

// newVersionCmd creates the "akc version" command.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show CLI and server version information",
		Annotations: map[string]string{
			"auth":      "none",
			"composite": "true",
		},
		RunE: versionRunE,
	}
}

// versionRunE is the RunE implementation for the version command.
func versionRunE(cmd *cobra.Command, _ []string) error {
	output := VersionOutput{
		CLIVersion: Version,
		Build:      Build,
		Prefix:     TokenPrefix,
	}

	// Optionally resolve endpoint_url (required: false).
	endpointURL := ResolveEndpointURL(cmd)
	if endpointURL != "" {
		sv, err := fetchServerVersion(cmd.Context(), endpointURL)
		if err != nil {
			// Server unreachable or timed out — omit server_version, warn on stderr.
			Warnf("warning: could not reach server: %v\n", err)
		} else {
			output.ServerVersion = sv
		}
	}
	// No endpoint configured → silently omit server_version (no warning).

	return PrintJSON(output)
}

// fetchServerVersion makes a direct HTTP GET to <baseURL>/version with a
// bounded context. It returns a *versionResponse on success or an error
// when the server is unreachable or the response is unparseable.
//
// This function bypasses apikit.Client to avoid the import cycle between
// internal/cli and the root apikit package. The HTTP call is equivalent to
// apikit.Client.Version(ctx) which calls GET /version (probe endpoint,
// bypasses mount point).
func fetchServerVersion(parent context.Context, baseURL string) (*versionResponse, error) {
	ctx, cancel := context.WithTimeout(parent, versionTimeout)
	defer cancel()

	url := strings.TrimRight(baseURL, "/") + "/version"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned HTTP %d", resp.StatusCode)
	}

	var result versionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &result, nil
}

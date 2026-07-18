package cli

// VersionOutput defines the JSON output for "akc version".
//
// ServerVersion is typed as `any` rather than `*apikit.VersionResponse`
// because `internal/cli` cannot import the root `apikit` package (the root
// package already imports `internal/cli` in cli.go, which would create a
// cycle). The actual value stored at runtime will be *apikit.VersionResponse
// (or a locally-defined mirror struct); json.Marshal handles both identically.
//
// Stub — will be fully implemented in task group 13.
type VersionOutput struct {
	CLIVersion    string `json:"cli_version"`
	Build         string `json:"build"`
	Prefix        string `json:"prefix"`
	ServerVersion any    `json:"server_version,omitempty"`
}

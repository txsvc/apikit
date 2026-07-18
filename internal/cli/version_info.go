package cli

// Build-time variables, injectable via:
//
//	go build -ldflags "-X github.com/txsvc/apikit/internal/cli.Version=v1.0.0"
var (
	// Version holds the semantic version string. Default: "dev".
	Version = "dev"
	// Build holds the build timestamp or commit hash. Default: "unknown".
	Build = "unknown"
	// TokenPrefix determines the config directory name ($HOME/.<prefix>/).
	// Default: "ak".
	TokenPrefix = "ak"
)

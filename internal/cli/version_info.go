package cli

// Build-time variables, injectable via -ldflags:
//
//	go build -ldflags "\
//	  -X github.com/txsvc/apikit/internal/cli.Version=v1.0.0 \
//	  -X github.com/txsvc/apikit/internal/cli.Build=abc1234 \
//	  -X github.com/txsvc/apikit/internal/cli.TokenPrefix=myapp"
//
// A binary built without any -ldflags override has valid non-empty defaults.
var (
	// Version holds the semantic version string. Default: "dev".
	Version = "dev"
	// Build holds the build timestamp or commit hash. Default: "unknown".
	Build = "unknown"
	// TokenPrefix determines the config directory name ($HOME/.<prefix>/)
	// and the prefix displayed in "akc version" output. Must be coordinated
	// with the server build to ensure token format alignment.
	// Default: "ak".
	TokenPrefix = "ak"
)

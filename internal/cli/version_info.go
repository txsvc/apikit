package cli

import (
	"fmt"
	"strings"
)

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
	// EnvPrefix determines the environment variable prefix (e.g. AF_API_KEY).
	// When empty, it defaults to strings.ToUpper(TokenPrefix).
	EnvPrefix = ""
)

// EnvPrefixName returns the resolved environment variable prefix in uppercase.
// It uses EnvPrefix if set, otherwise TokenPrefix, defaulting to "AK".
func EnvPrefixName() string {
	if EnvPrefix != "" {
		return strings.ToUpper(EnvPrefix)
	}
	if TokenPrefix != "" {
		return strings.ToUpper(TokenPrefix)
	}
	return "AK"
}

// PrefixedEnvVar returns the environment variable name with the active prefix,
// e.g. PrefixedEnvVar("API_KEY") -> "AK_API_KEY" or "AF_API_KEY".
func PrefixedEnvVar(canonicalName string) string {
	return fmt.Sprintf("%s_%s", EnvPrefixName(), canonicalName)
}


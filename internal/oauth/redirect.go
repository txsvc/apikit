package oauth

import (
	"fmt"
	"net/url"
)

// ValidateRedirectURI checks whether the given redirect URI is allowed.
// It accepts:
//   - Any http://localhost:<port>/* URI (HTTPS on localhost is rejected)
//   - Any URI whose scheme and host match the externalURL (when non-empty)
//
// Returns nil if the URI is allowed, or a non-nil error if rejected.
func ValidateRedirectURI(redirectURI, externalURL string) error {
	parsed, err := url.Parse(redirectURI)
	if err != nil {
		return fmt.Errorf("redirect_uri is not allowed")
	}

	// Check localhost: must be http (not https), host must be localhost
	if parsed.Hostname() == "localhost" {
		if parsed.Scheme == "http" {
			return nil
		}
		return fmt.Errorf("redirect_uri is not allowed")
	}

	// Check against external_url if configured
	if externalURL != "" {
		ext, err := url.Parse(externalURL)
		if err == nil && parsed.Scheme == ext.Scheme && parsed.Host == ext.Host {
			return nil
		}
	}

	return fmt.Errorf("redirect_uri is not allowed")
}

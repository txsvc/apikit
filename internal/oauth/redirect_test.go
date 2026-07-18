package oauth_test

import (
	"testing"

	"github.com/txsvc/apikit/internal/oauth"
)

// ========================================================================
// TS-06-28: Localhost URIs with valid ports are accepted
// (Requirement: 06-REQ-7.1)
// ========================================================================

// TestValidateRedirectURI_LocalhostPorts verifies that the redirect URI
// validator accepts http://localhost:<port>/path for various valid port
// numbers.
func TestValidateRedirectURI_LocalhostPorts(t *testing.T) {
	uris := []string{
		"http://localhost:9000/cb",
		"http://localhost:1/cb",
		"http://localhost:65535/cb",
		"http://localhost:54321/callback/foo",
	}

	for _, uri := range uris {
		t.Run(uri, func(t *testing.T) {
			err := oauth.ValidateRedirectURI(uri, "")
			if err != nil {
				t.Errorf("ValidateRedirectURI(%q, \"\") = %v, want nil", uri, err)
			}
		})
	}
}

// ========================================================================
// TS-06-29: URIs matching external_url scheme+host are accepted
// (Requirement: 06-REQ-7.2)
// ========================================================================

// TestValidateRedirectURI_ExternalURLMatch verifies that the redirect URI
// validator accepts URIs whose scheme and host match external_url,
// including extended paths.
func TestValidateRedirectURI_ExternalURLMatch(t *testing.T) {
	externalURL := "https://api.example.com"
	uris := []string{
		"https://api.example.com/callback",
		"https://api.example.com/auth/callback/extra",
	}

	for _, uri := range uris {
		t.Run(uri, func(t *testing.T) {
			err := oauth.ValidateRedirectURI(uri, externalURL)
			if err != nil {
				t.Errorf("ValidateRedirectURI(%q, %q) = %v, want nil", uri, externalURL, err)
			}
		})
	}
}

// ========================================================================
// TS-06-30: When external_url is empty, only localhost URIs are accepted
// (Requirement: 06-REQ-7.3)
// ========================================================================

// TestValidateRedirectURI_EmptyExternalURLOnlyLocalhost verifies that when
// external_url is empty, only localhost URIs are accepted and non-localhost
// URIs are rejected.
func TestValidateRedirectURI_EmptyExternalURLOnlyLocalhost(t *testing.T) {
	t.Run("localhost_accepted", func(t *testing.T) {
		err := oauth.ValidateRedirectURI("http://localhost:9000/cb", "")
		if err != nil {
			t.Errorf("ValidateRedirectURI(localhost, \"\") = %v, want nil", err)
		}
	})

	t.Run("non_localhost_rejected", func(t *testing.T) {
		err := oauth.ValidateRedirectURI("https://example.com/cb", "")
		if err == nil {
			t.Error("ValidateRedirectURI(non-localhost, \"\") = nil, want error")
		}
	})
}

// ========================================================================
// TS-06-E10: HTTPS on localhost is rejected
// (Requirement: 06-REQ-7.E1)
// ========================================================================

// TestValidateRedirectURI_HTTPSLocalhostRejected verifies that the
// redirect URI validator rejects https://localhost URIs (only http://
// localhost is accepted).
func TestValidateRedirectURI_HTTPSLocalhostRejected(t *testing.T) {
	err := oauth.ValidateRedirectURI("https://localhost:3000/callback", "")
	if err == nil {
		t.Error("ValidateRedirectURI(https://localhost:3000/callback, \"\") = nil, want error")
	}
}

// ========================================================================
// TS-06-E11: Non-matching external_url scheme/host is rejected
// (Requirement: 06-REQ-7.E2)
// ========================================================================

// TestValidateRedirectURI_NonMatchingExternalURLRejected verifies that the
// redirect URI validator rejects a non-localhost URI when external_url is
// configured but the scheme or host does not match.
func TestValidateRedirectURI_NonMatchingExternalURLRejected(t *testing.T) {
	externalURL := "https://api.example.com"

	t.Run("different_host", func(t *testing.T) {
		err := oauth.ValidateRedirectURI("https://evil.example.com/callback", externalURL)
		if err == nil {
			t.Error("ValidateRedirectURI(evil.example.com) = nil, want error")
		}
	})

	t.Run("different_scheme", func(t *testing.T) {
		err := oauth.ValidateRedirectURI("http://api.example.com/callback", externalURL)
		if err == nil {
			t.Error("ValidateRedirectURI(http instead of https) = nil, want error")
		}
	})
}

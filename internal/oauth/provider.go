// Package oauth implements the OAuth provider registry and handlers.
package oauth

import "context"

// Provider defines the interface for an OAuth identity provider.
// Any type implementing all four methods satisfies this interface.
type Provider interface {
	// Name returns the provider's identifier string (e.g. "github").
	Name() string

	// AuthorizeURL constructs the full OAuth authorization URL with
	// state and redirect URI parameters.
	AuthorizeURL(state, redirectURI string) string

	// Exchange exchanges an authorization code for an access token.
	Exchange(ctx context.Context, code, redirectURI string) (string, error)

	// UserInfo retrieves user identity information using an access token.
	UserInfo(ctx context.Context, accessToken string) (*UserInfo, error)
}

// UserInfo holds user identity information returned by an OAuth provider.
type UserInfo struct {
	Username   string
	Email      string
	ProviderID string
}

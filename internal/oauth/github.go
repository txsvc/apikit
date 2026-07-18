package oauth

import (
	"context"
	"net/http"
	"net/url"
)

const (
	defaultGitHubAuthorizeURL = "https://github.com/login/oauth/authorize"
	defaultGitHubTokenURL     = "https://github.com/login/oauth/access_token"
	defaultGitHubUserinfoURL  = "https://api.github.com/user"
)

// GitHubProvider implements the Provider interface for GitHub OAuth.
type GitHubProvider struct {
	clientID     string
	clientSecret string
	authorizeURL string
	tokenURL     string
	userinfoURL  string
	httpClient   *http.Client
}

// NewGitHubProvider constructs a GitHubProvider with the given credentials
// and optional URL overrides. When override URLs are empty, the built-in
// GitHub defaults are used. The shared *http.Client is injected and must
// not be nil.
func NewGitHubProvider(clientID, clientSecret, authorizeURL, tokenURL, userinfoURL string, client *http.Client) *GitHubProvider {
	if authorizeURL == "" {
		authorizeURL = defaultGitHubAuthorizeURL
	}
	if tokenURL == "" {
		tokenURL = defaultGitHubTokenURL
	}
	if userinfoURL == "" {
		userinfoURL = defaultGitHubUserinfoURL
	}
	return &GitHubProvider{
		clientID:     clientID,
		clientSecret: clientSecret,
		authorizeURL: authorizeURL,
		tokenURL:     tokenURL,
		userinfoURL:  userinfoURL,
		httpClient:   client,
	}
}

// HTTPClient returns the provider's internal HTTP client reference.
// This is used for testing to verify constructor injection.
func (g *GitHubProvider) HTTPClient() *http.Client {
	return g.httpClient
}

// Name returns "github".
func (g *GitHubProvider) Name() string {
	return "github"
}

// AuthorizeURL constructs the full OAuth authorization URL with
// client_id, scope=user:email, state, and redirect_uri as query parameters.
func (g *GitHubProvider) AuthorizeURL(state, redirectURI string) string {
	u, _ := url.Parse(g.authorizeURL)
	q := u.Query()
	q.Set("client_id", g.clientID)
	q.Set("scope", "user:email")
	q.Set("state", state)
	q.Set("redirect_uri", redirectURI)
	u.RawQuery = q.Encode()
	return u.String()
}

// Exchange exchanges an authorization code for an access token.
// TODO: implement in a later task group.
func (g *GitHubProvider) Exchange(_ context.Context, _, _ string) (string, error) {
	return "", nil
}

// UserInfo retrieves user identity information using an access token.
// TODO: implement in a later task group.
func (g *GitHubProvider) UserInfo(_ context.Context, _ string) (*UserInfo, error) {
	return nil, nil
}

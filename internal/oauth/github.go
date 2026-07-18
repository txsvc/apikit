package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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

// Exchange exchanges an authorization code for an access token by POSTing
// to the token endpoint with form-encoded credentials.
func (g *GitHubProvider) Exchange(ctx context.Context, code, redirectURI string) (string, error) {
	form := url.Values{
		"client_id":     {g.clientID},
		"client_secret": {g.clientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURI},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("exchange: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("exchange: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("exchange: token endpoint returned HTTP %d", resp.StatusCode)
	}

	var result struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("exchange: %w", err)
	}

	if result.Error != "" {
		return "", fmt.Errorf("exchange: %s", result.Error)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("exchange: access token not returned")
	}

	return result.AccessToken, nil
}

// UserInfo retrieves user identity information from the provider's user
// info endpoint using the given access token.
func (g *GitHubProvider) UserInfo(ctx context.Context, accessToken string) (*UserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.userinfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("userinfo: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("userinfo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("userinfo: endpoint returned HTTP %d", resp.StatusCode)
	}

	var result struct {
		Login string `json:"login"`
		Email string `json:"email"`
		ID    int64  `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("userinfo: %w", err)
	}

	return &UserInfo{
		Username:   result.Login,
		Email:      result.Email,
		ProviderID: strconv.FormatInt(result.ID, 10),
	}, nil
}

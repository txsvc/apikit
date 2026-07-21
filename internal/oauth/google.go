package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	defaultGoogleAuthorizeURL = "https://accounts.google.com/o/oauth2/v2/auth"
	defaultGoogleTokenURL     = "https://oauth2.googleapis.com/token"
	defaultGoogleUserinfoURL  = "https://www.googleapis.com/oauth2/v3/userinfo"
)

// GoogleProvider implements the Provider interface for Google OAuth.
type GoogleProvider struct {
	clientID     string
	clientSecret string
	authorizeURL string
	tokenURL     string
	userinfoURL  string
	httpClient   *http.Client
}

// NewGoogleProvider constructs a GoogleProvider with the given credentials
// and optional URL overrides. When override URLs are empty, the built-in
// Google defaults are used. The shared *http.Client is injected and must
// not be nil.
func NewGoogleProvider(clientID, clientSecret, authorizeURL, tokenURL, userinfoURL string, client *http.Client) *GoogleProvider {
	if authorizeURL == "" {
		authorizeURL = defaultGoogleAuthorizeURL
	}
	if tokenURL == "" {
		tokenURL = defaultGoogleTokenURL
	}
	if userinfoURL == "" {
		userinfoURL = defaultGoogleUserinfoURL
	}
	return &GoogleProvider{
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
func (g *GoogleProvider) HTTPClient() *http.Client {
	return g.httpClient
}

// Name returns "google".
func (g *GoogleProvider) Name() string {
	return "google"
}

// AuthorizeURL constructs the full OAuth authorization URL with
// client_id, response_type=code, scope=openid email profile, state,
// and redirect_uri as query parameters.
func (g *GoogleProvider) AuthorizeURL(state, redirectURI string) string {
	u, _ := url.Parse(g.authorizeURL)
	q := u.Query()
	q.Set("client_id", g.clientID)
	q.Set("response_type", "code")
	q.Set("scope", "openid email profile")
	q.Set("state", state)
	q.Set("redirect_uri", redirectURI)
	u.RawQuery = q.Encode()
	return u.String()
}

// Exchange exchanges an authorization code for an access token by POSTing
// to the token endpoint with form-encoded credentials.
func (g *GoogleProvider) Exchange(ctx context.Context, code, redirectURI string) (string, error) {
	form := url.Values{
		"client_id":     {g.clientID},
		"client_secret": {g.clientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
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

// UserInfo retrieves user identity information from the Google userinfo
// endpoint using the given access token.
func (g *GoogleProvider) UserInfo(ctx context.Context, accessToken string) (*UserInfo, error) {
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
		Sub           string `json:"sub"`
		Name          string `json:"name"`
		GivenName     string `json:"given_name"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("userinfo: %w", err)
	}

	if result.Email == "" {
		return nil, fmt.Errorf("userinfo: email not returned")
	}
	if !result.EmailVerified {
		return nil, fmt.Errorf("userinfo: email not verified")
	}

	username := result.Name
	if username == "" {
		username = result.GivenName
	}

	return &UserInfo{
		Username:   username,
		Email:      result.Email,
		ProviderID: result.Sub,
	}, nil
}

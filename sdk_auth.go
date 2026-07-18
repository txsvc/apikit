package apikit

import "context"

// ---------------------------------------------------------------------------
// Auth endpoints
// ---------------------------------------------------------------------------

// GetProviders calls GET /auth/providers to discover available OAuth providers.
func (c *Client) GetProviders(ctx context.Context) ([]*OAuthProvider, error) {
	return doList[OAuthProvider](c, ctx, c.apiURL("/auth/providers"))
}

// ExchangeOAuthCode calls POST /auth/callback to exchange an OAuth code.
func (c *Client) ExchangeOAuthCode(ctx context.Context, req *AuthCallbackRequest) (*AuthCallbackResponse, error) {
	resp, err := doJSON[AuthCallbackResponse](c, ctx, "POST", c.apiURL("/auth/callback"), req)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

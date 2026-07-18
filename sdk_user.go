package apikit

import "context"

// ---------------------------------------------------------------------------
// Authenticated user endpoints
// ---------------------------------------------------------------------------

// GetUser calls GET /user to fetch the authenticated user.
func (c *Client) GetUser(ctx context.Context, opts ...RequestOption) (*Response[User], error) {
	return doJSON[User](c, ctx, "GET", c.apiURL("/user"), nil, opts...)
}

// UpdateUser calls PATCH /user to update the authenticated user.
func (c *Client) UpdateUser(ctx context.Context, req *UpdateUserRequest) (*User, error) {
	resp, err := doJSON[User](c, ctx, "PATCH", c.apiURL("/user"), req)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// ListKeys calls GET /user/keys to list the authenticated user's API keys.
func (c *Client) ListKeys(ctx context.Context) ([]*APIKeyMeta, error) {
	return doList[APIKeyMeta](c, ctx, c.apiURL("/user/keys"))
}

// RefreshKey calls POST /user/keys/:keyID/refresh to refresh an API key.
func (c *Client) RefreshKey(ctx context.Context, keyID string) (*APIKeyFull, error) {
	resp, err := doJSON[APIKeyFull](c, ctx, "POST", c.apiURL("/user/keys/"+keyID+"/refresh"), nil)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// RevokeKey calls DELETE /user/keys/:keyID to revoke an API key.
// Returns *RevokeKeyResponse on HTTP 200 success.
func (c *Client) RevokeKey(ctx context.Context, keyID string) (*RevokeKeyResponse, error) {
	resp, err := doJSON[RevokeKeyResponse](c, ctx, "DELETE", c.apiURL("/user/keys/"+keyID), nil)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// ListTokens calls GET /user/tokens to list the authenticated user's PATs.
func (c *Client) ListTokens(ctx context.Context) ([]*PAT, error) {
	return doList[PAT](c, ctx, c.apiURL("/user/tokens"))
}

// CreateToken calls POST /user/tokens to create a new personal access token.
func (c *Client) CreateToken(ctx context.Context, req *CreateTokenRequest) (*PATFull, error) {
	resp, err := doJSON[PATFull](c, ctx, "POST", c.apiURL("/user/tokens"), req)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// GetToken calls GET /user/tokens/:tokenID to fetch a PAT by ID.
func (c *Client) GetToken(ctx context.Context, tokenID string, opts ...RequestOption) (*Response[PAT], error) {
	return doJSON[PAT](c, ctx, "GET", c.apiURL("/user/tokens/"+tokenID), nil, opts...)
}

// RevokeToken calls DELETE /user/tokens/:tokenID to revoke a PAT.
func (c *Client) RevokeToken(ctx context.Context, tokenID string) error {
	return c.doEmpty(ctx, "DELETE", c.apiURL("/user/tokens/"+tokenID), nil)
}

// ListUserOrgs calls GET /user/orgs to list the authenticated user's organizations.
func (c *Client) ListUserOrgs(ctx context.Context) ([]*Organization, error) {
	return doList[Organization](c, ctx, c.apiURL("/user/orgs"))
}

package apikit

import (
	"context"
	"net/url"
)

// ---------------------------------------------------------------------------
// Admin user endpoints
// ---------------------------------------------------------------------------

// GetUserByID calls GET /users/:id to fetch a user by ID.
func (c *Client) GetUserByID(ctx context.Context, userID string, opts ...RequestOption) (*Response[User], error) {
	return doJSON[User](c, ctx, "GET", c.apiURL("/users/"+userID), nil, opts...)
}

// ListUsers calls GET /users to list all users.
// Query parameters are constructed using url.Values for forward-compatible extension.
func (c *Client) ListUsers(ctx context.Context, opts *ListUsersOptions) ([]*User, error) {
	path := "/users"
	if opts != nil && opts.IncludeBlocked {
		v := url.Values{}
		v.Set("include_blocked", "true")
		path += "?" + v.Encode()
	}
	return doList[User](c, ctx, c.apiURL(path))
}

// CreateUser calls POST /users to create a new user (admin).
func (c *Client) CreateUser(ctx context.Context, req *CreateUserRequest) (*User, error) {
	resp, err := doJSON[User](c, ctx, "POST", c.apiURL("/users"), req)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// UpdateUserByID calls PATCH /users/:id to update a user by ID (admin).
func (c *Client) UpdateUserByID(ctx context.Context, userID string, req *UpdateUserRequest) (*User, error) {
	resp, err := doJSON[User](c, ctx, "PATCH", c.apiURL("/users/"+userID), req)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// PromoteUser calls POST /users/:id/promote to promote a user to admin (admin).
func (c *Client) PromoteUser(ctx context.Context, userID string) (*User, error) {
	resp, err := doJSON[User](c, ctx, "POST", c.apiURL("/users/"+userID+"/promote"), nil)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// DemoteUser calls POST /users/:id/demote to demote an admin to user (admin).
func (c *Client) DemoteUser(ctx context.Context, userID string) (*User, error) {
	resp, err := doJSON[User](c, ctx, "POST", c.apiURL("/users/"+userID+"/demote"), nil)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// BlockUser calls POST /users/:id/block to block a user (admin).
func (c *Client) BlockUser(ctx context.Context, userID string) (*User, error) {
	resp, err := doJSON[User](c, ctx, "POST", c.apiURL("/users/"+userID+"/block"), nil)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// UnblockUser calls POST /users/:id/unblock to unblock a user (admin).
func (c *Client) UnblockUser(ctx context.Context, userID string) (*User, error) {
	resp, err := doJSON[User](c, ctx, "POST", c.apiURL("/users/"+userID+"/unblock"), nil)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// ListUserKeys calls GET /users/:userID/keys to list a user's API keys (admin).
func (c *Client) ListUserKeys(ctx context.Context, userID string) ([]*APIKeyMeta, error) {
	return doList[APIKeyMeta](c, ctx, c.apiURL("/users/"+userID+"/keys"))
}

// RevokeUserKey calls DELETE /users/:userID/keys/:keyID to revoke a user's
// API key (admin).
func (c *Client) RevokeUserKey(ctx context.Context, userID, keyID string) error {
	return c.doEmpty(ctx, "DELETE", c.apiURL("/users/"+userID+"/keys/"+keyID), nil)
}

// ListUserTokens calls GET /users/:userID/tokens to list a user's PATs (admin).
func (c *Client) ListUserTokens(ctx context.Context, userID string) ([]*PAT, error) {
	return doList[PAT](c, ctx, c.apiURL("/users/"+userID+"/tokens"))
}

// RevokeUserToken calls DELETE /users/:userID/tokens/:tokenID to revoke a
// user's PAT (admin).
func (c *Client) RevokeUserToken(ctx context.Context, userID, tokenID string) error {
	return c.doEmpty(ctx, "DELETE", c.apiURL("/users/"+userID+"/tokens/"+tokenID), nil)
}

// ---------------------------------------------------------------------------
// Admin organization endpoints
// ---------------------------------------------------------------------------

// CreateOrg calls POST /orgs to create a new organization (admin).
func (c *Client) CreateOrg(ctx context.Context, req *CreateOrgRequest) (*Organization, error) {
	resp, err := doJSON[Organization](c, ctx, "POST", c.apiURL("/orgs"), req)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// ListOrgs calls GET /orgs to list all organizations.
// Query parameters are constructed using url.Values for forward-compatible extension.
func (c *Client) ListOrgs(ctx context.Context, opts *ListOrgsOptions) ([]*Organization, error) {
	path := "/orgs"
	if opts != nil && opts.IncludeBlocked {
		v := url.Values{}
		v.Set("include_blocked", "true")
		path += "?" + v.Encode()
	}
	return doList[Organization](c, ctx, c.apiURL(path))
}

// GetOrg calls GET /orgs/:id to fetch an organization by ID.
func (c *Client) GetOrg(ctx context.Context, orgID string, opts ...RequestOption) (*Response[Organization], error) {
	return doJSON[Organization](c, ctx, "GET", c.apiURL("/orgs/"+orgID), nil, opts...)
}

// UpdateOrg calls PATCH /orgs/:id to update an organization (admin).
func (c *Client) UpdateOrg(ctx context.Context, orgID string, req *UpdateOrgRequest) (*Organization, error) {
	resp, err := doJSON[Organization](c, ctx, "PATCH", c.apiURL("/orgs/"+orgID), req)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// DeleteOrg calls DELETE /orgs/:id to delete an organization (admin).
func (c *Client) DeleteOrg(ctx context.Context, orgID string) error {
	return c.doEmpty(ctx, "DELETE", c.apiURL("/orgs/"+orgID), nil)
}

// BlockOrg calls POST /orgs/:id/block to block an organization (admin).
func (c *Client) BlockOrg(ctx context.Context, orgID string) (*Organization, error) {
	resp, err := doJSON[Organization](c, ctx, "POST", c.apiURL("/orgs/"+orgID+"/block"), nil)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// UnblockOrg calls POST /orgs/:id/unblock to unblock an organization (admin).
func (c *Client) UnblockOrg(ctx context.Context, orgID string) (*Organization, error) {
	resp, err := doJSON[Organization](c, ctx, "POST", c.apiURL("/orgs/"+orgID+"/unblock"), nil)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// ListOrgMembers calls GET /orgs/:id/members to list organization members.
// Returns []*User directly -- OrgMember is not a distinct Go type.
func (c *Client) ListOrgMembers(ctx context.Context, orgID string) ([]*User, error) {
	return doList[User](c, ctx, c.apiURL("/orgs/"+orgID+"/members"))
}

// AddOrgMember calls PUT /orgs/:orgID/members/:userID to add a member.
func (c *Client) AddOrgMember(ctx context.Context, orgID, userID string) error {
	return c.doEmpty(ctx, "PUT", c.apiURL("/orgs/"+orgID+"/members/"+userID), nil)
}

// RemoveOrgMember calls DELETE /orgs/:orgID/members/:userID to remove a member.
func (c *Client) RemoveOrgMember(ctx context.Context, orgID, userID string) error {
	return c.doEmpty(ctx, "DELETE", c.apiURL("/orgs/"+orgID+"/members/"+userID), nil)
}

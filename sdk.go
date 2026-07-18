package apikit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ClientOption is a function that configures a Client at construction time.
type ClientOption func(*Client)

// RequestOption is a function that configures per-request behavior.
type RequestOption func(*http.Request)

// Client is the SDK's entry point for making API requests.
// Safe for concurrent use after construction.
type Client struct {
	baseURL    string
	httpClient *http.Client
	mountPoint string
	apiKey     string
	requestID  string
}

// NewClient creates a new Client with the given baseURL and options.
// Returns a non-nil *Client even for empty baseURL.
func NewClient(baseURL string, opts ...ClientOption) *Client {
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: http.DefaultClient,
		mountPoint: "/api/v1",
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// WithAPIKey sets the Bearer token for the Authorization header.
func WithAPIKey(key string) ClientOption {
	return func(c *Client) {
		c.apiKey = key
	}
}

// WithHTTPClient overrides the default http.Client.
func WithHTTPClient(client *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = client
	}
}

// WithRequestID sets a static X-Request-ID header on every request.
func WithRequestID(id string) ClientOption {
	return func(c *Client) {
		c.requestID = id
	}
}

// WithMountPoint overrides the default mount point (/api/v1).
// Normalizes the path: adds leading slash if missing, strips trailing slash.
// Empty string normalizes to "/".
func WithMountPoint(path string) ClientOption {
	return func(c *Client) {
		if path == "" {
			c.mountPoint = "/"
			return
		}
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		path = strings.TrimRight(path, "/")
		if path == "" {
			path = "/"
		}
		c.mountPoint = path
	}
}

// WithIfNoneMatch adds an If-None-Match header to a single request.
func WithIfNoneMatch(etag string) RequestOption {
	return func(req *http.Request) {
		req.Header.Set("If-None-Match", etag)
	}
}

// ---------------------------------------------------------------------------
// Internal request execution pipeline
// ---------------------------------------------------------------------------

// do is the internal method that handles all HTTP request construction,
// header setting, execution, and response dispatching. All endpoint methods
// delegate to do for consistent cross-cutting concerns.
//
// The path parameter is the full URL path (e.g., "/api/v1/user" or "/healthz").
// Endpoint methods construct this by combining c.mountPoint + relative path
// for API endpoints, or using the bare path for health probes.
//
// The body parameter is marshaled to JSON if non-nil. The result parameter
// receives the decoded JSON response body on 2xx responses (if non-nil).
//
// Returns (statusCode, responseHeaders, error). On error (4xx/5xx, 304,
// network, marshal, decode), statusCode is 0 and headers are nil.
func (c *Client) do(ctx context.Context, method, path string, body, result interface{}, opts ...RequestOption) (int, http.Header, error) {
	// Marshal request body if present.
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, nil, fmt.Errorf("encoding request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	// Construct full URL.
	reqURL := c.baseURL + path

	// Create request with caller-supplied context.
	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return 0, nil, fmt.Errorf("creating request: %w", err)
	}

	// Set standard headers.
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	if c.requestID != "" {
		req.Header.Set("X-Request-ID", c.requestID)
	}

	// Apply per-request options (e.g., WithIfNoneMatch).
	for _, opt := range opts {
		opt(req)
	}

	// Execute the request.
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	// 304 Not Modified — return sentinel error, nil response.
	if resp.StatusCode == http.StatusNotModified {
		return 0, nil, ErrNotModified
	}

	// 4xx/5xx — decode error envelope or fall back to status text.
	if resp.StatusCode >= 400 {
		return 0, nil, decodeErrorResponse(resp)
	}

	// 204 No Content — no body to decode.
	if resp.StatusCode == http.StatusNoContent {
		return resp.StatusCode, resp.Header, nil
	}

	// 200/201 — decode JSON response body into result.
	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return 0, nil, fmt.Errorf("decoding response body: %w", err)
		}
	}

	return resp.StatusCode, resp.Header, nil
}

// decodeErrorResponse attempts to decode a JSON error envelope from a 4xx/5xx
// response. If the body is not valid JSON or does not contain the expected
// envelope structure, it returns an *APIError with the HTTP status text.
func decodeErrorResponse(resp *http.Response) *APIError {
	var envelope struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return &APIError{
			Code:    resp.StatusCode,
			Message: http.StatusText(resp.StatusCode),
		}
	}
	if envelope.Error.Code != 0 && envelope.Error.Message != "" {
		return &APIError{
			Code:    envelope.Error.Code,
			Message: envelope.Error.Message,
		}
	}
	return &APIError{
		Code:    resp.StatusCode,
		Message: http.StatusText(resp.StatusCode),
	}
}

// ---------------------------------------------------------------------------
// Health and meta endpoints (bypass mount point)
// ---------------------------------------------------------------------------

// Healthz calls GET /healthz (liveness probe).
// Health probes bypass the mount point.
func (c *Client) Healthz(ctx context.Context, opts ...RequestOption) (*Response[HealthResponse], error) {
	var result HealthResponse
	status, header, err := c.do(ctx, "GET", "/healthz", nil, &result, opts...)
	if err != nil {
		return nil, err
	}
	return &Response[HealthResponse]{Data: result, StatusCode: status, Header: header}, nil
}

// Readyz calls GET /readyz (readiness probe).
// Health probes bypass the mount point.
func (c *Client) Readyz(ctx context.Context, opts ...RequestOption) (*Response[HealthResponse], error) {
	var result HealthResponse
	status, header, err := c.do(ctx, "GET", "/readyz", nil, &result, opts...)
	if err != nil {
		return nil, err
	}
	return &Response[HealthResponse]{Data: result, StatusCode: status, Header: header}, nil
}

// Version calls GET /version.
// Health/meta probes bypass the mount point.
func (c *Client) Version(ctx context.Context, opts ...RequestOption) (*Response[VersionResponse], error) {
	var result VersionResponse
	status, header, err := c.do(ctx, "GET", "/version", nil, &result, opts...)
	if err != nil {
		return nil, err
	}
	return &Response[VersionResponse]{Data: result, StatusCode: status, Header: header}, nil
}

// ---------------------------------------------------------------------------
// Authenticated user endpoints
// ---------------------------------------------------------------------------

// GetUser calls GET /user to fetch the authenticated user.
func (c *Client) GetUser(ctx context.Context, opts ...RequestOption) (*Response[User], error) {
	var result User
	status, header, err := c.do(ctx, "GET", c.mountPoint+"/user", nil, &result, opts...)
	if err != nil {
		return nil, err
	}
	return &Response[User]{Data: result, StatusCode: status, Header: header}, nil
}

// UpdateUser calls PATCH /user to update the authenticated user.
func (c *Client) UpdateUser(ctx context.Context, req *UpdateUserRequest) (*Response[User], error) {
	var result User
	status, header, err := c.do(ctx, "PATCH", c.mountPoint+"/user", req, &result)
	if err != nil {
		return nil, err
	}
	return &Response[User]{Data: result, StatusCode: status, Header: header}, nil
}

// ListKeys calls GET /user/keys to list the authenticated user's API keys.
func (c *Client) ListKeys(ctx context.Context) ([]*APIKeyMeta, error) {
	var result []*APIKeyMeta
	_, _, err := c.do(ctx, "GET", c.mountPoint+"/user/keys", nil, &result)
	if err != nil {
		return nil, err
	}
	if result == nil {
		result = []*APIKeyMeta{}
	}
	return result, nil
}

// RevokeKey calls DELETE /user/keys/:keyID to revoke an API key.
func (c *Client) RevokeKey(ctx context.Context, keyID string) (*Response[RevokeKeyResponse], error) {
	var result RevokeKeyResponse
	status, header, err := c.do(ctx, "DELETE", c.mountPoint+"/user/keys/"+keyID, nil, &result)
	if err != nil {
		return nil, err
	}
	return &Response[RevokeKeyResponse]{Data: result, StatusCode: status, Header: header}, nil
}

// RefreshKey calls POST /user/keys/:keyID/refresh to refresh an API key.
func (c *Client) RefreshKey(ctx context.Context, keyID string) (*Response[APIKeyFull], error) {
	var result APIKeyFull
	status, header, err := c.do(ctx, "POST", c.mountPoint+"/user/keys/"+keyID+"/refresh", nil, &result)
	if err != nil {
		return nil, err
	}
	return &Response[APIKeyFull]{Data: result, StatusCode: status, Header: header}, nil
}

// ListTokens calls GET /user/tokens to list the authenticated user's PATs.
func (c *Client) ListTokens(ctx context.Context) ([]*PAT, error) {
	var result []*PAT
	_, _, err := c.do(ctx, "GET", c.mountPoint+"/user/tokens", nil, &result)
	if err != nil {
		return nil, err
	}
	if result == nil {
		result = []*PAT{}
	}
	return result, nil
}

// CreateToken calls POST /user/tokens to create a new personal access token.
func (c *Client) CreateToken(ctx context.Context, req *CreateTokenRequest) (*Response[PATFull], error) {
	var result PATFull
	status, header, err := c.do(ctx, "POST", c.mountPoint+"/user/tokens", req, &result)
	if err != nil {
		return nil, err
	}
	return &Response[PATFull]{Data: result, StatusCode: status, Header: header}, nil
}

// GetToken calls GET /user/tokens/:tokenID to fetch a PAT by ID.
func (c *Client) GetToken(ctx context.Context, tokenID string, opts ...RequestOption) (*Response[PAT], error) {
	var result PAT
	status, header, err := c.do(ctx, "GET", c.mountPoint+"/user/tokens/"+tokenID, nil, &result, opts...)
	if err != nil {
		return nil, err
	}
	return &Response[PAT]{Data: result, StatusCode: status, Header: header}, nil
}

// RevokeToken calls DELETE /user/tokens/:tokenID to revoke a PAT.
func (c *Client) RevokeToken(ctx context.Context, tokenID string) error {
	_, _, err := c.do(ctx, "DELETE", c.mountPoint+"/user/tokens/"+tokenID, nil, nil)
	return err
}

// ---------------------------------------------------------------------------
// Auth endpoints
// ---------------------------------------------------------------------------

// GetProviders calls GET /auth/providers to discover available OAuth providers.
func (c *Client) GetProviders(ctx context.Context) ([]*OAuthProvider, error) {
	var result []*OAuthProvider
	_, _, err := c.do(ctx, "GET", c.mountPoint+"/auth/providers", nil, &result)
	if err != nil {
		return nil, err
	}
	if result == nil {
		result = []*OAuthProvider{}
	}
	return result, nil
}

// ExchangeOAuthCode calls POST /auth/callback to exchange an OAuth code.
func (c *Client) ExchangeOAuthCode(ctx context.Context, req *AuthCallbackRequest) (*Response[AuthCallbackResponse], error) {
	var result AuthCallbackResponse
	status, header, err := c.do(ctx, "POST", c.mountPoint+"/auth/callback", req, &result)
	if err != nil {
		return nil, err
	}
	return &Response[AuthCallbackResponse]{Data: result, StatusCode: status, Header: header}, nil
}

// ---------------------------------------------------------------------------
// Admin user endpoints
// ---------------------------------------------------------------------------

// GetUserByID calls GET /users/:id to fetch a user by ID.
func (c *Client) GetUserByID(ctx context.Context, userID string, opts ...RequestOption) (*Response[User], error) {
	var result User
	status, header, err := c.do(ctx, "GET", c.mountPoint+"/users/"+userID, nil, &result, opts...)
	if err != nil {
		return nil, err
	}
	return &Response[User]{Data: result, StatusCode: status, Header: header}, nil
}

// ListUsers calls GET /users to list all users.
func (c *Client) ListUsers(ctx context.Context, opts *ListUsersOptions) ([]*User, error) {
	path := c.mountPoint + "/users"
	var result []*User
	_, _, err := c.do(ctx, "GET", path, nil, &result)
	if err != nil {
		return nil, err
	}
	if result == nil {
		result = []*User{}
	}
	return result, nil
}

// CreateUser calls POST /users to create a new user (admin).
func (c *Client) CreateUser(ctx context.Context, req *CreateUserRequest) (*Response[User], error) {
	var result User
	status, header, err := c.do(ctx, "POST", c.mountPoint+"/users", req, &result)
	if err != nil {
		return nil, err
	}
	return &Response[User]{Data: result, StatusCode: status, Header: header}, nil
}

// UpdateUserByID calls PATCH /users/:id to update a user by ID (admin).
func (c *Client) UpdateUserByID(ctx context.Context, userID string, req *UpdateUserRequest) (*Response[User], error) {
	var result User
	status, header, err := c.do(ctx, "PATCH", c.mountPoint+"/users/"+userID, req, &result)
	if err != nil {
		return nil, err
	}
	return &Response[User]{Data: result, StatusCode: status, Header: header}, nil
}

// BlockUser calls POST /users/:id/block to block a user (admin).
func (c *Client) BlockUser(ctx context.Context, userID string) error {
	_, _, err := c.do(ctx, "POST", c.mountPoint+"/users/"+userID+"/block", nil, nil)
	return err
}

// UnblockUser calls POST /users/:id/unblock to unblock a user (admin).
func (c *Client) UnblockUser(ctx context.Context, userID string) error {
	_, _, err := c.do(ctx, "POST", c.mountPoint+"/users/"+userID+"/unblock", nil, nil)
	return err
}

// PromoteUser calls POST /users/:id/promote to promote a user to admin (admin).
func (c *Client) PromoteUser(ctx context.Context, userID string) error {
	_, _, err := c.do(ctx, "POST", c.mountPoint+"/users/"+userID+"/promote", nil, nil)
	return err
}

// DemoteUser calls POST /users/:id/demote to demote an admin to user (admin).
func (c *Client) DemoteUser(ctx context.Context, userID string) error {
	_, _, err := c.do(ctx, "POST", c.mountPoint+"/users/"+userID+"/demote", nil, nil)
	return err
}

// RevokeUserKey calls DELETE /users/:userID/keys/:keyID to revoke a user's
// API key (admin).
func (c *Client) RevokeUserKey(ctx context.Context, userID, keyID string) error {
	_, _, err := c.do(ctx, "DELETE", c.mountPoint+"/users/"+userID+"/keys/"+keyID, nil, nil)
	return err
}

// ---------------------------------------------------------------------------
// Admin organization endpoints
// ---------------------------------------------------------------------------

// ListOrgs calls GET /orgs to list all organizations.
func (c *Client) ListOrgs(ctx context.Context, opts *ListOrgsOptions) ([]*Organization, error) {
	path := c.mountPoint + "/orgs"
	var result []*Organization
	_, _, err := c.do(ctx, "GET", path, nil, &result)
	if err != nil {
		return nil, err
	}
	if result == nil {
		result = []*Organization{}
	}
	return result, nil
}

// GetOrg calls GET /orgs/:id to fetch an organization by ID.
func (c *Client) GetOrg(ctx context.Context, orgID string, opts ...RequestOption) (*Response[Organization], error) {
	var result Organization
	status, header, err := c.do(ctx, "GET", c.mountPoint+"/orgs/"+orgID, nil, &result, opts...)
	if err != nil {
		return nil, err
	}
	return &Response[Organization]{Data: result, StatusCode: status, Header: header}, nil
}

// CreateOrg calls POST /orgs to create a new organization (admin).
func (c *Client) CreateOrg(ctx context.Context, req *CreateOrgRequest) (*Response[Organization], error) {
	var result Organization
	status, header, err := c.do(ctx, "POST", c.mountPoint+"/orgs", req, &result)
	if err != nil {
		return nil, err
	}
	return &Response[Organization]{Data: result, StatusCode: status, Header: header}, nil
}

// UpdateOrg calls PATCH /orgs/:id to update an organization (admin).
func (c *Client) UpdateOrg(ctx context.Context, orgID string, req *UpdateOrgRequest) (*Response[Organization], error) {
	var result Organization
	status, header, err := c.do(ctx, "PATCH", c.mountPoint+"/orgs/"+orgID, req, &result)
	if err != nil {
		return nil, err
	}
	return &Response[Organization]{Data: result, StatusCode: status, Header: header}, nil
}

// DeleteOrg calls DELETE /orgs/:id to delete an organization (admin).
func (c *Client) DeleteOrg(ctx context.Context, orgID string) error {
	_, _, err := c.do(ctx, "DELETE", c.mountPoint+"/orgs/"+orgID, nil, nil)
	return err
}

// BlockOrg calls POST /orgs/:id/block to block an organization (admin).
func (c *Client) BlockOrg(ctx context.Context, orgID string) error {
	_, _, err := c.do(ctx, "POST", c.mountPoint+"/orgs/"+orgID+"/block", nil, nil)
	return err
}

// UnblockOrg calls POST /orgs/:id/unblock to unblock an organization (admin).
func (c *Client) UnblockOrg(ctx context.Context, orgID string) error {
	_, _, err := c.do(ctx, "POST", c.mountPoint+"/orgs/"+orgID+"/unblock", nil, nil)
	return err
}

// ListOrgMembers calls GET /orgs/:id/members to list organization members.
// Returns []*User directly -- OrgMember is not a distinct Go type.
func (c *Client) ListOrgMembers(ctx context.Context, orgID string) ([]*User, error) {
	var result []*User
	_, _, err := c.do(ctx, "GET", c.mountPoint+"/orgs/"+orgID+"/members", nil, &result)
	if err != nil {
		return nil, err
	}
	if result == nil {
		result = []*User{}
	}
	return result, nil
}

// AddOrgMember calls PUT /orgs/:orgID/members/:userID to add a member.
func (c *Client) AddOrgMember(ctx context.Context, orgID, userID string) error {
	_, _, err := c.do(ctx, "PUT", c.mountPoint+"/orgs/"+orgID+"/members/"+userID, nil, nil)
	return err
}

// RemoveOrgMember calls DELETE /orgs/:orgID/members/:userID to remove a member.
func (c *Client) RemoveOrgMember(ctx context.Context, orgID, userID string) error {
	_, _, err := c.do(ctx, "DELETE", c.mountPoint+"/orgs/"+orgID+"/members/"+userID, nil, nil)
	return err
}

package apikit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ClientOption is a function that configures a Client at construction time.
type ClientOption func(*Client)

// RequestOption is a function that configures per-request behavior.
type RequestOption func(*http.Request)

// Client is the SDK's entry point for making API requests.
// Safe for concurrent use after construction — all fields are set once
// in NewClient and never modified thereafter.
type Client struct {
	baseURL    string
	httpClient *http.Client
	mountPoint string
	apiKey     string
	requestID  string
}

// NewClient creates a new Client with the given baseURL and options.
// Returns a non-nil *Client even for empty baseURL.
// Trailing slash is stripped from baseURL at construction time.
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
		// Collapse multiple leading slashes to exactly one.
		for strings.HasPrefix(path, "//") {
			path = path[1:]
		}
		path = strings.TrimRight(path, "/")
		if path == "" {
			path = "/"
		}
		c.mountPoint = path
	}
}

// WithIfNoneMatch adds an If-None-Match header to a single request
// for conditional GET support.
func WithIfNoneMatch(etag string) RequestOption {
	return func(req *http.Request) {
		req.Header.Set("If-None-Match", etag)
	}
}

// ---------------------------------------------------------------------------
// URL construction helpers
// ---------------------------------------------------------------------------

// apiURL constructs an API endpoint URL as baseURL + mountPoint + path.
// Used for all API and auth endpoints. Path parameters are interpolated
// as-is without url.PathEscape — callers are responsible for providing
// URL-safe values.
func (c *Client) apiURL(path string) string {
	return c.baseURL + c.mountPoint + path
}

// probeURL constructs a health probe endpoint URL as baseURL + path,
// bypassing the mount point. Used for /healthz, /readyz, and /version.
func (c *Client) probeURL(path string) string {
	return c.baseURL + path
}

// ---------------------------------------------------------------------------
// Internal request execution pipeline
// ---------------------------------------------------------------------------

// do is the internal method that handles all HTTP request construction,
// header setting, execution, and response dispatching. All endpoint methods
// delegate to do for consistent cross-cutting concerns.
//
// The fullURL parameter is the complete URL (constructed via apiURL or probeURL).
// The body parameter is marshaled to JSON if non-nil. The result parameter
// receives the decoded JSON response body on 2xx responses (if non-nil).
//
// Returns (statusCode, responseHeaders, error). On error (4xx/5xx, 304,
// network, marshal, decode), statusCode is 0 and headers are nil.
func (c *Client) do(ctx context.Context, method, fullURL string, body, result any, opts ...RequestOption) (int, http.Header, error) {
	// Marshal request body if present.
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, nil, fmt.Errorf("encoding request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	// Create request with caller-supplied context.
	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
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
	var envelope errorEnvelope
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
// Generic typed wrappers for the do method
// ---------------------------------------------------------------------------

// doJSON executes a request and decodes the JSON response into a typed
// Response[T]. Used for single-resource endpoints that return a JSON object
// and wrap it with response metadata (status code, headers for ETag access).
func doJSON[T any](c *Client, ctx context.Context, method, fullURL string, body any, opts ...RequestOption) (*Response[T], error) {
	var result T
	status, header, err := c.do(ctx, method, fullURL, body, &result, opts...)
	if err != nil {
		return nil, err
	}
	return &Response[T]{Data: result, StatusCode: status, Header: header}, nil
}

// doList executes a request and decodes the JSON response from a bare JSON
// array directly into a []*T slice. Returns a non-nil empty slice when the
// server returns []. Used for all list endpoints.
func doList[T any](c *Client, ctx context.Context, fullURL string, opts ...RequestOption) ([]*T, error) {
	var result []*T
	_, _, err := c.do(ctx, "GET", fullURL, nil, &result, opts...)
	if err != nil {
		return nil, err
	}
	if result == nil {
		result = []*T{}
	}
	return result, nil
}

// doEmpty executes a request and expects no response body (HTTP 204).
// Used for delete and other void endpoints.
func (c *Client) doEmpty(ctx context.Context, method, fullURL string, body any, opts ...RequestOption) error {
	_, _, err := c.do(ctx, method, fullURL, body, nil, opts...)
	return err
}

// ---------------------------------------------------------------------------
// Health and meta endpoints (bypass mount point)
// ---------------------------------------------------------------------------

// Healthz calls GET /healthz (liveness probe).
// Health probes bypass the mount point.
func (c *Client) Healthz(ctx context.Context) (*HealthResponse, error) {
	var result HealthResponse
	_, _, err := c.do(ctx, "GET", c.probeURL("/healthz"), nil, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// Readyz calls GET /readyz (readiness probe).
// Health probes bypass the mount point.
func (c *Client) Readyz(ctx context.Context) (*HealthResponse, error) {
	var result HealthResponse
	_, _, err := c.do(ctx, "GET", c.probeURL("/readyz"), nil, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// Version calls GET /version.
// Health/meta probes bypass the mount point.
func (c *Client) Version(ctx context.Context) (*VersionResponse, error) {
	var result VersionResponse
	_, _, err := c.do(ctx, "GET", c.probeURL("/version"), nil, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

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

// CreateOrg calls POST /orgs to create a new organization (admin).
func (c *Client) CreateOrg(ctx context.Context, req *CreateOrgRequest) (*Organization, error) {
	resp, err := doJSON[Organization](c, ctx, "POST", c.apiURL("/orgs"), req)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
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
// Returns []*User directly — OrgMember is not a distinct Go type.
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

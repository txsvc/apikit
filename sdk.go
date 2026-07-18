package apikit

import (
	"context"
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

// Healthz calls GET /healthz (liveness probe).
// Health probes bypass the mount point.
func (c *Client) Healthz(ctx context.Context, opts ...RequestOption) (*Response[HealthResponse], error) {
	// Stub — will be implemented in a later task group.
	return nil, nil
}

// GetUser calls GET /user to fetch the authenticated user.
func (c *Client) GetUser(ctx context.Context, opts ...RequestOption) (*Response[User], error) {
	// Stub — will be implemented in a later task group.
	return nil, nil
}

// ListUsers calls GET /users to list all users.
func (c *Client) ListUsers(ctx context.Context, opts *ListUsersOptions) ([]*User, error) {
	// Stub — will be implemented in a later task group.
	return nil, nil
}

// ListOrgs calls GET /orgs to list all organizations.
func (c *Client) ListOrgs(ctx context.Context, opts *ListOrgsOptions) ([]*Organization, error) {
	// Stub — will be implemented in a later task group.
	return nil, nil
}

// ListOrgMembers calls GET /orgs/:id/members to list organization members.
// Returns []*User directly — OrgMember is not a distinct Go type.
func (c *Client) ListOrgMembers(ctx context.Context, orgID string) ([]*User, error) {
	// Stub — will be implemented in a later task group.
	return nil, nil
}

// GetUserByID calls GET /users/:id to fetch a user by ID.
func (c *Client) GetUserByID(ctx context.Context, userID string, opts ...RequestOption) (*Response[User], error) {
	// Stub — will be implemented in a later task group.
	return nil, nil
}

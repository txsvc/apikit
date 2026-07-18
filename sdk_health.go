package apikit

import "context"

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

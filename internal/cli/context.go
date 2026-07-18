package cli

import "context"

// Context key types -- unexported zero-size struct types guarantee collision
// safety with consuming-project code.  No external package can construct
// clientContextKey{} or userIDContextKey{} (13-REQ-7.1, 13-REQ-7.E1).
type clientContextKey struct{}
type userIDContextKey struct{}

// ContextWithClient stores an API client in the context.
// The client is stored as any to avoid import cycles between
// internal/cli and the root apikit package.
func ContextWithClient(ctx context.Context, client any) context.Context {
	return context.WithValue(ctx, clientContextKey{}, client)
}

// ClientFromContext retrieves the API client from a context.
// Returns nil if no client was stored (e.g., auth-exempt commands).
func ClientFromContext(ctx context.Context) any {
	return ctx.Value(clientContextKey{})
}

// UserIDFromContext retrieves the resolved user_id string from a context.
// Returns "" if no user_id was stored or if user_id was not configured.
func UserIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(userIDContextKey{}).(string)
	return v
}

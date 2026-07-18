package cli

import "context"

// UsersRunner holds function values for admin user operations.
// The function signatures use primitive types and any to avoid importing
// the root apikit package (which would create an import cycle since
// cli.go in the root package imports internal/cli).
//
// In tests, the functions wrap mock SDK clients. In production, they
// will wrap *apikit.Client methods (wired by PersistentPreRunE in spec 13).
type UsersRunner struct {
	ListUsers   func(ctx context.Context, includeBlocked bool) (any, error)
	GetUserByID func(ctx context.Context, id string) (any, error)
	CreateUser  func(ctx context.Context, username, email, provider, providerID string) (any, error)
}

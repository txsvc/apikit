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
	ListUsers      func(ctx context.Context, includeBlocked bool) (any, error)
	GetUserByID    func(ctx context.Context, id string) (any, error)
	CreateUser     func(ctx context.Context, username, email, provider, providerID string) (any, error)
	UpdateUserByID func(ctx context.Context, id string, fullName string) (any, error)
	PromoteUser    func(ctx context.Context, id string) (any, error)
	DemoteUser     func(ctx context.Context, id string) (any, error)
	BlockUser      func(ctx context.Context, id string) (any, error)
	UnblockUser    func(ctx context.Context, id string) (any, error)
}

// OrgsRunner holds function values for admin org operations.
// Same DI pattern as UsersRunner — primitive types and any to avoid
// importing the root apikit package.
type OrgsRunner struct {
	ListOrgs        func(ctx context.Context, includeBlocked bool) (any, error)
	CreateOrg       func(ctx context.Context, name, slug string, url *string) (any, error)
	UpdateOrg       func(ctx context.Context, id string, name *string, url *string) (any, error)
	DeleteOrg       func(ctx context.Context, id string) error
	BlockOrg        func(ctx context.Context, id string) (any, error)
	UnblockOrg      func(ctx context.Context, id string) (any, error)
	ListOrgMembers  func(ctx context.Context, orgID string) (any, error)
	AddOrgMember    func(ctx context.Context, orgID, userID string) error
	RemoveOrgMember func(ctx context.Context, orgID, userID string) error
}

// KeysRunner holds function values for admin API key operations.
// Same DI pattern as UsersRunner — primitive types and any to avoid
// importing the root apikit package.
type KeysRunner struct {
	ListUserKeys  func(ctx context.Context, userID string) (any, error)
	RevokeUserKey func(ctx context.Context, userID, keyID string) error
}

// TokensRunner holds function values for admin token operations.
// Same DI pattern as UsersRunner — primitive types and any to avoid
// importing the root apikit package.
type TokensRunner struct {
	ListUserTokens  func(ctx context.Context, userID string) (any, error)
	RevokeUserToken func(ctx context.Context, userID, tokenID string) error
}

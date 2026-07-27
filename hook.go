package apikit

import (
	"context"
	"database/sql"
)

// AfterUserCreateFunc is a callback invoked after a new user row is inserted
// into the database, within the same transaction. It receives the active
// transaction, the new user's ID, username, and email. If it returns a
// non-nil error, the enclosing transaction is rolled back, undoing both the
// user INSERT and any side effects the hook performed.
//
// This hook is used by consuming projects (e.g. hub) to implement
// post-user-creation logic such as personal organization creation.
type AfterUserCreateFunc func(ctx context.Context, tx *sql.Tx, userID, username, email string) error

// OnAfterUserCreate registers an AfterUserCreateFunc hook on the server.
// The hook will be called after a new user is created via either the OAuth
// callback (handleCallback) or the admin user creation handler (createUser).
//
// Only one hook may be registered at a time. Calling OnAfterUserCreate a
// second time replaces the previously registered hook.
//
// The hook must be registered before MountHandlers is called to ensure it
// is wired into all user creation paths. Registering after MountHandlers
// stores the hook but behavior is undefined.
func (s *Server) OnAfterUserCreate(fn AfterUserCreateFunc) {
	s.afterUserCreateHook = fn
}

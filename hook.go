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

// BeforeOrgDeleteFunc is a callback invoked before an organization row is deleted.
// It receives the context and the ID of the organization to be deleted.
// If it returns a non-nil error, the deletion is aborted (vetoed) and the error
// is returned to the client without modifying the database.
type BeforeOrgDeleteFunc func(ctx context.Context, orgID string) error

// AfterOrgDeleteFunc is a callback invoked after an organization row is deleted,
// within the same database transaction. It receives the context, the active
// transaction, and the ID of the deleted organization. If it returns a non-nil
// error, the transaction is rolled back, undoing the deletion.
type AfterOrgDeleteFunc func(ctx context.Context, tx *sql.Tx, orgID string) error

// BeforeUserDeleteFunc is a callback invoked before a user row is deleted.
// It receives the context and the ID of the user to be deleted.
// If it returns a non-nil error, the deletion is aborted (vetoed) and the error
// is returned to the client without modifying the database.
type BeforeUserDeleteFunc func(ctx context.Context, userID string) error

// AfterUserDeleteFunc is a callback invoked after a user row is deleted,
// within the same database transaction. It receives the context, the active
// transaction, and the ID of the deleted user. If it returns a non-nil error,
// the transaction is rolled back, undoing the deletion.
type AfterUserDeleteFunc func(ctx context.Context, tx *sql.Tx, userID string) error

// OnBeforeOrgDelete registers a BeforeOrgDeleteFunc hook on the server.
// Calling OnBeforeOrgDelete replaces any previously registered hook.
func (s *Server) OnBeforeOrgDelete(fn BeforeOrgDeleteFunc) {
	s.beforeOrgDeleteHook = fn
}

// OnAfterOrgDelete registers an AfterOrgDeleteFunc hook on the server.
// Calling OnAfterOrgDelete replaces any previously registered hook.
func (s *Server) OnAfterOrgDelete(fn AfterOrgDeleteFunc) {
	s.afterOrgDeleteHook = fn
}

// OnBeforeUserDelete registers a BeforeUserDeleteFunc hook on the server.
// Calling OnBeforeUserDelete replaces any previously registered hook.
func (s *Server) OnBeforeUserDelete(fn BeforeUserDeleteFunc) {
	s.beforeUserDeleteHook = fn
}

// OnAfterUserDelete registers an AfterUserDeleteFunc hook on the server.
// Calling OnAfterUserDelete replaces any previously registered hook.
func (s *Server) OnAfterUserDelete(fn AfterUserDeleteFunc) {
	s.afterUserDeleteHook = fn
}

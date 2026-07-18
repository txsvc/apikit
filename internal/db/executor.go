package db

import (
	"context"
	"database/sql"
)

// Executor is an interface satisfied by both *sql.DB and *sql.Tx,
// allowing functions to work with or without an explicit transaction.
type Executor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

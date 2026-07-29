package handlers

import "database/sql"

// resolveUserID resolves a user selector string (UUID, email, or username)
// to a canonical user UUID by querying the users table using a three-step
// detection heuristic:
//  1. If the selector parses as a UUID, query by the id column.
//  2. If the selector contains '@', query by the email column.
//  3. Otherwise, query by the username column (fallback).
//
// Returns (uuid, nil) on success, or ("", sql.ErrNoRows) when no matching
// row exists, or ("", err) for any other database error.
func resolveUserID(sqlDB *sql.DB, selector string) (string, error) {
	// Stub — implementation in task group 5.
	return "", nil
}

// resolveOrgID resolves an org selector string (UUID or slug) to a canonical
// org UUID by querying the orgs table using a two-step detection heuristic:
//  1. If the selector parses as a UUID, query by the id column.
//  2. Otherwise (including selectors containing '@'), query by the slug column.
//
// Returns (uuid, nil) on success, or ("", sql.ErrNoRows) when no matching
// row exists, or ("", err) for any other database error.
func resolveOrgID(sqlDB *sql.DB, selector string) (string, error) {
	// Stub — implementation in task group 5.
	return "", nil
}

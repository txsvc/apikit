package handlers

import (
	"database/sql"
	"strings"

	"github.com/google/uuid"
)

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
	var query string
	if _, err := uuid.Parse(selector); err == nil {
		query = "SELECT id FROM users WHERE id = ?"
	} else if strings.Contains(selector, "@") {
		query = "SELECT id FROM users WHERE email = ?"
	} else {
		query = "SELECT id FROM users WHERE username = ?"
	}

	var id string
	if err := sqlDB.QueryRow(query, selector).Scan(&id); err != nil {
		return "", err
	}
	return id, nil
}

// resolveOrgID resolves an org selector string (UUID or slug) to a canonical
// org UUID by querying the orgs table using a two-step detection heuristic:
//  1. If the selector parses as a UUID, query by the id column.
//  2. Otherwise (including selectors containing '@'), query by the slug column.
//
// Returns (uuid, nil) on success, or ("", sql.ErrNoRows) when no matching
// row exists, or ("", err) for any other database error.
func resolveOrgID(sqlDB *sql.DB, selector string) (string, error) {
	var query string
	if _, err := uuid.Parse(selector); err == nil {
		query = "SELECT id FROM orgs WHERE id = ?"
	} else {
		query = "SELECT id FROM orgs WHERE slug = ?"
	}

	var id string
	if err := sqlDB.QueryRow(query, selector).Scan(&id); err != nil {
		return "", err
	}
	return id, nil
}

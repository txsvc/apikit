package handlers

import "database/sql"

// ValidateSlug exposes validateSlug for external tests.
var ValidateSlug = validateSlug

// IsOrgMember exposes isOrgMember for external tests.
var IsOrgMember = func(db *sql.DB, orgID, userID string) (bool, error) {
	return isOrgMember(db, orgID, userID)
}

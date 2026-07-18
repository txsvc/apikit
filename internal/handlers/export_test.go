package handlers

import "database/sql"

// ValidateSlug exposes validateSlug for external tests.
var ValidateSlug = validateSlug

// IsOrgMember exposes isOrgMember for external tests.
var IsOrgMember = func(db *sql.DB, orgID, userID string) (bool, error) {
	return isOrgMember(db, orgID, userID)
}

// GenerateTokenID exposes generateTokenID for external tests.
var GenerateTokenID = generateTokenID

// GenerateSecret exposes generateSecret for external tests.
var GenerateSecret = generateSecret

// HashSecret exposes hashSecret for external tests.
var HashSecret = hashSecret

// TokenAlphabet exposes tokenAlphabet for external tests.
const TokenAlphabet = tokenAlphabet

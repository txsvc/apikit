package auth

import (
	"net/http"
	"testing"
)

// TestMiddleware_RevokedAPIKey_WrongSecret verifies that a revoked API key
// presented with the wrong secret yields the generic "invalid credentials"
// response rather than "credential revoked". Revocation and expiry state
// must never be disclosed to a caller who does not hold the secret, since
// key_ids are visible in listings and logs.
func TestMiddleware_RevokedAPIKey_WrongSecret(t *testing.T) {
	database := openTestDB(t)

	insertUser(t, database, "uid1", "user", "active")
	insertAPIKey(t, database, "revokedkey", "uid1", hexSHA256("mysecret"), "", "2025-01-01T00:00:00Z")

	rec := runMiddlewareWithDB(t, database, map[string]string{
		"Authorization": "Bearer ak_revokedkey_wrongsecret",
	})
	assertErrorResponseDB(t, rec, http.StatusUnauthorized, "invalid credentials")
}

// TestMiddleware_ExpiredAPIKey_WrongSecret verifies that an expired API key
// presented with the wrong secret yields "invalid credentials", not
// "credential expired".
func TestMiddleware_ExpiredAPIKey_WrongSecret(t *testing.T) {
	database := openTestDB(t)

	insertUser(t, database, "uid1", "user", "active")
	insertAPIKey(t, database, "expiredkey", "uid1", hexSHA256("mysecret"), "2000-01-01T00:00:00Z", "")

	rec := runMiddlewareWithDB(t, database, map[string]string{
		"Authorization": "Bearer ak_expiredkey_wrongsecret",
	})
	assertErrorResponseDB(t, rec, http.StatusUnauthorized, "invalid credentials")
}

// TestMiddleware_RevokedPAT_WrongSecret verifies that a revoked PAT presented
// with the wrong secret yields "invalid credentials", not "credential revoked".
func TestMiddleware_RevokedPAT_WrongSecret(t *testing.T) {
	database := openTestDB(t)

	insertUser(t, database, "uid1", "user", "active")
	insertPATRow(t, database, "revokedtok", "uid1", hexSHA256("mysecret"),
		`["keys:read"]`, "", "2025-01-01T00:00:00Z")

	rec := runMiddlewareWithDB(t, database, map[string]string{
		"Authorization": "Bearer ak_pat_revokedtok_wrongsecret",
	})
	assertErrorResponseDB(t, rec, http.StatusUnauthorized, "invalid credentials")
}

// TestMiddleware_ExpiredPAT_WrongSecret verifies that an expired PAT presented
// with the wrong secret yields "invalid credentials", not "credential expired".
func TestMiddleware_ExpiredPAT_WrongSecret(t *testing.T) {
	database := openTestDB(t)

	insertUser(t, database, "uid1", "user", "active")
	insertPATRow(t, database, "expiredtok", "uid1", hexSHA256("mysecret"),
		`["keys:read"]`, "2000-01-01T00:00:00Z", "")

	rec := runMiddlewareWithDB(t, database, map[string]string{
		"Authorization": "Bearer ak_pat_expiredtok_wrongsecret",
	})
	assertErrorResponseDB(t, rec, http.StatusUnauthorized, "invalid credentials")
}

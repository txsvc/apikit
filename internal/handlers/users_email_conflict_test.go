package handlers_test

import (
	"net/http"
	"testing"
)

// TestCreateUser_DuplicateEmail verifies that POST /users returns HTTP 409
// "email already exists" when the email is already registered. The unique
// index on users.email (idx_users_email) used to surface as a 500.
func TestCreateUser_DuplicateEmail(t *testing.T) {
	e, sqlDB := setupAdminTestServer(t)

	insertTestUser(t, sqlDB, "existing-uuid-email", "alice", "alice@example.com", "github", "gh-001")

	body := `{"username":"alice2","email":"alice@example.com","provider":"google","provider_id":"g-002"}`
	rec := sendJSON(t, e, http.MethodPost, "/users", body)

	assertErrorResponse(t, rec, http.StatusConflict, "email already exists")
}

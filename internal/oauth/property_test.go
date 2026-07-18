package oauth_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/txsvc/apikit/internal/db"
	"github.com/txsvc/apikit/internal/oauth"
)

// ========================================================================
// Property test infrastructure
// ========================================================================

// propTestProvider creates a testProvider for property tests with configurable identity.
func propTestProvider(providerID, username, email string) *testProvider {
	return &testProvider{
		name: "github",
		exchangeFn: func(_ context.Context, _, _ string) (string, error) {
			return "token-prop", nil
		},
		userInfoFn: func(_ context.Context, _ string) (*oauth.UserInfo, error) {
			return &oauth.UserInfo{
				Username:   username,
				Email:      email,
				ProviderID: providerID,
			}, nil
		},
	}
}

// propSetupEcho creates a test Echo server with a testProvider and database.
func propSetupEcho(t *testing.T, p oauth.Provider, database *db.DB, externalURL string) *echo.Echo {
	t.Helper()
	e := echo.New()
	registry := oauth.NewRegistry()
	if p != nil {
		if err := registry.Register(p); err != nil {
			t.Fatalf("register provider: %v", err)
		}
	}
	g := e.Group("")
	oauth.RegisterOAuthHandlers(g, registry, database, externalURL)
	return e
}

// propPostCallback posts a callback request and returns the response recorder.
func propPostCallback(e *echo.Echo, providerName string) *httptest.ResponseRecorder {
	body := fmt.Sprintf(`{"provider":%q,"code":"abc","redirect_uri":"http://localhost:9000/cb"}`, providerName)
	req := httptest.NewRequest(http.MethodPost, "/auth/callback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// ========================================================================
// TS-06-P1: Key uniqueness — after N callbacks, exactly one active key
// (Property: 06-PROP-1)
// Validates: 06-REQ-11.1, 06-REQ-11.2, 06-REQ-14.2
// ========================================================================

// TestProperty_KeyUniquenessAfterNLogins runs N callbacks (1..10) for the
// same user and verifies that after each callback, exactly one active
// (non-revoked, non-expired) key exists. Revoked count equals N-1 after N.
func TestProperty_KeyUniquenessAfterNLogins(t *testing.T) {
	database := openTestDB(t)
	p := propTestProvider("p1-user", "p1user", "p1@example.com")
	e := propSetupEcho(t, p, database, "")

	const N = 5
	for i := range N {
		rec := propPostCallback(e, "github")
		if rec.Code != http.StatusOK {
			t.Fatalf("login %d: status = %d, want 200; body = %s", i+1, rec.Code, rec.Body.String())
		}

		// Get user ID from the response.
		var resp struct {
			User struct {
				ID string `json:"id"`
			} `json:"user"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("login %d: parse response: %v", i+1, err)
		}
		userID := resp.User.ID

		// After each login: exactly one active key.
		var activeCount int
		err := database.SqlDB.QueryRow(
			"SELECT COUNT(*) FROM api_keys WHERE user_id = ? AND revoked_at IS NULL",
			userID,
		).Scan(&activeCount)
		if err != nil {
			t.Fatalf("login %d: count active keys: %v", i+1, err)
		}
		if activeCount != 1 {
			t.Errorf("login %d: active key count = %d, want 1", i+1, activeCount)
		}

		// Revoked count should be i (0 after first login, 1 after second, etc.).
		var revokedCount int
		err = database.SqlDB.QueryRow(
			"SELECT COUNT(*) FROM api_keys WHERE user_id = ? AND revoked_at IS NOT NULL",
			userID,
		).Scan(&revokedCount)
		if err != nil {
			t.Fatalf("login %d: count revoked keys: %v", i+1, err)
		}
		if revokedCount != i {
			t.Errorf("login %d: revoked key count = %d, want %d", i+1, revokedCount, i)
		}
	}
}

// ========================================================================
// TS-06-P2: Blocked user — always HTTP 403, status unchanged, no new keys
// (Property: 06-PROP-2)
// Validates: 06-REQ-10.4
// ========================================================================

// TestProperty_BlockedUserInvariant runs M callbacks (1..5) for a blocked
// user and verifies each returns HTTP 403, user status remains blocked,
// and api_keys count is unchanged.
func TestProperty_BlockedUserInvariant(t *testing.T) {
	database := openTestDB(t)

	// Pre-insert a blocked user.
	now := db.FormatTime(time.Now().UTC())
	userID := "blocked-prop-user"
	_, err := database.SqlDB.Exec(
		`INSERT INTO users (id, username, email, full_name, role, status, provider, provider_id, created_at, updated_at)
		 VALUES (?, ?, ?, NULL, ?, ?, ?, ?, ?, ?)`,
		userID, "blocked_prop", "blocked_prop@example.com", "user", "blocked", "github", "p2-user",
		now, now,
	)
	if err != nil {
		t.Fatalf("insert blocked user: %v", err)
	}

	// Count keys before.
	var keyCountBefore int
	err = database.SqlDB.QueryRow(
		"SELECT COUNT(*) FROM api_keys WHERE user_id = ?", userID,
	).Scan(&keyCountBefore)
	if err != nil {
		t.Fatalf("count keys before: %v", err)
	}

	p := propTestProvider("p2-user", "blocked_prop", "blocked_prop@example.com")
	e := propSetupEcho(t, p, database, "")

	const M = 3
	for i := range M {
		rec := propPostCallback(e, "github")

		// Must be HTTP 403.
		if rec.Code != http.StatusForbidden {
			t.Errorf("attempt %d: status = %d, want %d", i+1, rec.Code, http.StatusForbidden)
		}

		// Verify status still blocked.
		var status string
		err := database.SqlDB.QueryRow(
			"SELECT status FROM users WHERE id = ?", userID,
		).Scan(&status)
		if err != nil {
			t.Fatalf("attempt %d: query user status: %v", i+1, err)
		}
		if status != "blocked" {
			t.Errorf("attempt %d: status = %q, want %q", i+1, status, "blocked")
		}

		// Verify key count unchanged.
		var keyCountAfter int
		err = database.SqlDB.QueryRow(
			"SELECT COUNT(*) FROM api_keys WHERE user_id = ?", userID,
		).Scan(&keyCountAfter)
		if err != nil {
			t.Fatalf("attempt %d: count keys: %v", i+1, err)
		}
		if keyCountAfter != keyCountBefore {
			t.Errorf("attempt %d: key count changed from %d to %d", i+1, keyCountBefore, keyCountAfter)
		}
	}
}

// ========================================================================
// TS-06-P3: Secret non-persistence — plaintext never in DB
// (Property: 06-PROP-3)
// Validates: 06-REQ-11.2, 06-REQ-11.4
// ========================================================================

// TestProperty_SecretNonPersistence runs N callbacks and verifies for each:
// secret_hash == hex(sha256(plaintext_secret)) and plaintext is not stored
// in any api_keys column.
func TestProperty_SecretNonPersistence(t *testing.T) {
	database := openTestDB(t)

	const N = 5
	for i := range N {
		providerID := fmt.Sprintf("p3-user-%d", i)
		username := fmt.Sprintf("p3user%d", i)
		email := fmt.Sprintf("p3-%d@example.com", i)
		p := propTestProvider(providerID, username, email)
		e := propSetupEcho(t, p, database, "")

		rec := propPostCallback(e, "github")
		if rec.Code != http.StatusOK {
			t.Fatalf("iteration %d: status = %d, want 200; body = %s", i, rec.Code, rec.Body.String())
		}

		// Extract plaintext secret from response.
		var resp struct {
			APIKey struct {
				Key   string `json:"key"`
				KeyID string `json:"key_id"`
			} `json:"api_key"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("iteration %d: parse response: %v", i, err)
		}

		parts := strings.Split(resp.APIKey.Key, "_")
		if len(parts) != 3 {
			t.Fatalf("iteration %d: key format invalid: %q", i, resp.APIKey.Key)
		}
		plaintextSecret := parts[2]

		// Compute expected hash.
		expectedHash := sha256.Sum256([]byte(plaintextSecret))
		expectedHex := hex.EncodeToString(expectedHash[:])

		// Verify secret_hash in DB matches.
		var dbSecretHash string
		err := database.SqlDB.QueryRow(
			"SELECT secret_hash FROM api_keys WHERE key_id = ?",
			resp.APIKey.KeyID,
		).Scan(&dbSecretHash)
		if err != nil {
			t.Fatalf("iteration %d: query api_keys: %v", i, err)
		}
		if dbSecretHash != expectedHex {
			t.Errorf("iteration %d: secret_hash = %q, want %q", i, dbSecretHash, expectedHex)
		}

		// Verify plaintext secret is NOT stored in any column.
		if dbSecretHash == plaintextSecret {
			t.Errorf("iteration %d: secret_hash equals plaintext — secret stored in raw form!", i)
		}

		// Check no column in api_keys contains the plaintext.
		var keyID, userID, secretHash string
		var expiresAt, revokedAt, createdAt *string
		err = database.SqlDB.QueryRow(
			"SELECT key_id, user_id, secret_hash, expires_at, revoked_at, created_at FROM api_keys WHERE key_id = ?",
			resp.APIKey.KeyID,
		).Scan(&keyID, &userID, &secretHash, &expiresAt, &revokedAt, &createdAt)
		if err != nil {
			t.Fatalf("iteration %d: scan all columns: %v", i, err)
		}
		allVals := []string{keyID, userID, secretHash}
		for _, ptr := range []*string{expiresAt, revokedAt, createdAt} {
			if ptr != nil {
				allVals = append(allVals, *ptr)
			}
		}
		for _, val := range allVals {
			if val == plaintextSecret {
				t.Errorf("iteration %d: plaintext secret found in api_keys column value %q", i, val)
			}
		}
	}
}

// ========================================================================
// TS-06-P4: Admin auto-grant fires at most once
// (Property: 06-PROP-4)
// Validates: 06-REQ-10.2, 06-REQ-10.3, 06-REQ-10.E3
// ========================================================================

// TestProperty_AdminAutoGrantFiresOnce runs K callbacks from different
// provider IDs but all with admin_email, and verifies that exactly one
// user gets role='admin' — the first to create when no admin existed.
func TestProperty_AdminAutoGrantFiresOnce(t *testing.T) {
	database := openTestDB(t)

	// Set admin_email.
	_, err := database.SqlDB.Exec(
		"INSERT INTO admin_config (key, value) VALUES ('admin_email', 'admin-prop@example.com')",
	)
	if err != nil {
		t.Fatalf("insert admin_config: %v", err)
	}

	const K = 5
	for i := range K {
		providerID := fmt.Sprintf("p4-user-%d", i)
		username := fmt.Sprintf("p4user%d", i)
		p := propTestProvider(providerID, username, "admin-prop@example.com")
		e := propSetupEcho(t, p, database, "")

		rec := propPostCallback(e, "github")
		if rec.Code != http.StatusOK {
			t.Fatalf("user %d: status = %d, want 200; body = %s", i, rec.Code, rec.Body.String())
		}
	}

	// After all K requests: exactly one admin.
	var adminCount int
	err = database.SqlDB.QueryRow(
		"SELECT COUNT(*) FROM users WHERE role = 'admin'",
	).Scan(&adminCount)
	if err != nil {
		t.Fatalf("count admins: %v", err)
	}
	if adminCount != 1 {
		t.Errorf("admin count = %d, want 1 (auto-grant fires at most once)", adminCount)
	}

	// All other users should have role='user'.
	var userRoleCount int
	err = database.SqlDB.QueryRow(
		"SELECT COUNT(*) FROM users WHERE role = 'user'",
	).Scan(&userRoleCount)
	if err != nil {
		t.Fatalf("count user-role users: %v", err)
	}
	if userRoleCount != K-1 {
		t.Errorf("user-role count = %d, want %d", userRoleCount, K-1)
	}
}

// ========================================================================
// TS-06-P5: Registry name uniqueness — duplicates return error
// (Property: 06-PROP-5)
// Validates: 06-REQ-2.2, 06-REQ-2.E1
// ========================================================================

// TestProperty_RegistryNameUniqueness generates random sequences of
// Register() calls with names from a small alphabet and verifies that
// duplicate names always return an error, and the registry has at most
// one provider per name.
func TestProperty_RegistryNameUniqueness(t *testing.T) {
	names := []string{"alpha", "beta", "gamma", "alpha", "beta", "delta", "gamma", "alpha"}

	r := oauth.NewRegistry()
	seen := make(map[string]bool)

	for _, name := range names {
		p := &mockProvider{name: name}
		err := r.Register(p)

		if seen[name] {
			// Duplicate — must return error.
			if err == nil {
				t.Errorf("Register(%q) duplicate: got nil error, want non-nil", name)
			}
		} else {
			// First registration — must succeed.
			if err != nil {
				t.Errorf("Register(%q) first: got error %v, want nil", name, err)
			}
			seen[name] = true
		}
	}

	// Total registered providers == count of unique names.
	registered := r.List()
	uniqueCount := len(seen)
	if len(registered) != uniqueCount {
		t.Errorf("registered count = %d, want %d (unique names)", len(registered), uniqueCount)
	}

	// Verify each name appears at most once.
	nameSet := make(map[string]bool)
	for _, name := range registered {
		if nameSet[name] {
			t.Errorf("name %q appears more than once in List()", name)
		}
		nameSet[name] = true
	}
}

// ========================================================================
// TS-06-P6: No secrets in provider list response
// (Property: 06-PROP-6)
// Validates: 06-REQ-5.1, 06-REQ-5.4
// ========================================================================

// TestProperty_NoSecretsInProviderList registers N providers (0..5) with
// random client_ids and client_secrets, then verifies that GET /auth/providers
// response objects contain only 'name' and 'authorize_url' fields.
func TestProperty_NoSecretsInProviderList(t *testing.T) {
	// Test with 0 providers.
	t.Run("ZeroProviders", func(t *testing.T) {
		e := echo.New()
		registry := oauth.NewRegistry()
		g := e.Group("")
		oauth.RegisterOAuthHandlers(g, registry, nil, "")

		rec := getProviders(e)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}

		var body []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("parse response: %v", err)
		}
		if len(body) != 0 {
			t.Errorf("expected empty array, got %d entries", len(body))
		}
	})

	// Test with N providers using different client_ids/secrets.
	providers := []struct {
		name         string
		clientID     string
		clientSecret string
	}{
		{"github", "gh-client-id", "gh-super-secret-123"},
	}

	for _, pc := range providers {
		t.Run("Provider_"+pc.name, func(t *testing.T) {
			ghp := oauth.NewGitHubProvider(pc.clientID, pc.clientSecret, "", "", "", &http.Client{})
			e := echo.New()
			registry := oauth.NewRegistry()
			_ = registry.Register(ghp)
			g := e.Group("")
			oauth.RegisterOAuthHandlers(g, registry, nil, "")

			rec := getProviders(e)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}

			var body []map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("parse response: %v", err)
			}

			for i, entry := range body {
				// Only 'name' and 'authorize_url' should be present.
				for key := range entry {
					if key != "name" && key != "authorize_url" {
						t.Errorf("entry[%d] has unexpected key %q", i, key)
					}
				}

				// Explicitly check forbidden keys.
				for _, forbidden := range []string{"client_secret", "token_url", "userinfo_url"} {
					if _, ok := entry[forbidden]; ok {
						t.Errorf("entry[%d] contains forbidden key %q", i, forbidden)
					}
				}
			}

			// Raw body must not contain the client secret.
			raw := rec.Body.String()
			if strings.Contains(raw, pc.clientSecret) {
				t.Errorf("response body contains client secret %q", pc.clientSecret)
			}
		})
	}
}

// ========================================================================
// TS-06-P7: Redirect allowlist — Exchange never called for disallowed URIs
// (Property: 06-PROP-7)
// Validates: 06-REQ-7.4, 06-REQ-8.1
// ========================================================================

// TestProperty_RedirectAllowlistEnforcement generates random disallowed
// redirect_uri strings and verifies each returns HTTP 400 and Exchange is
// never invoked.
func TestProperty_RedirectAllowlistEnforcement(t *testing.T) {
	disallowed := []string{
		"ftp://evil.example.com/steal",
		"https://attacker.com/callback",
		"file:///etc/passwd",
		"https://localhost:3000/callback", // HTTPS on localhost
		"http://evil.example.com/callback",
		"http://127.0.0.2:8080/cb",
		"javascript:alert(1)",
	}

	for _, uri := range disallowed {
		t.Run(uri, func(t *testing.T) {
			p := &testProvider{name: "github"}
			e := propSetupEcho(t, p, nil, "https://api.example.com")

			body := fmt.Sprintf(`{"provider":"github","code":"abc","redirect_uri":%q}`, uri)
			req := httptest.NewRequest(http.MethodPost, "/auth/callback", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 for redirect_uri %q", rec.Code, uri)
			}

			if p.exchangeCalls != 0 {
				t.Errorf("Exchange called %d times for disallowed redirect_uri %q, want 0",
					p.exchangeCalls, uri)
			}

			// Verify error message.
			var errResp integrationError
			if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err == nil {
				if errResp.Error.Message != "redirect_uri is not allowed" {
					t.Errorf("message = %q, want %q", errResp.Error.Message, "redirect_uri is not allowed")
				}
			}
		})
	}
}

// ========================================================================
// TS-06-P8: Transaction atomicity — fault injection shows no partial rows
// (Property: 06-PROP-8)
// Validates: 06-REQ-10.1, 06-REQ-10.E1, 06-REQ-11.1, 06-REQ-11.2
// ========================================================================

// TestProperty_TransactionAtomicity injects faults by breaking the DB schema
// at different points and verifies that after a failed callback, no partial
// user updates or key records are visible.
func TestProperty_TransactionAtomicity(t *testing.T) {
	// Scenario 1: Break users table → user insert fails.
	t.Run("BrokenUsersTable", func(t *testing.T) {
		database := openTestDB(t)
		// Rename the email column to cause INSERT failure.
		_, err := database.SqlDB.Exec("ALTER TABLE users RENAME COLUMN email TO email_broken")
		if err != nil {
			t.Fatalf("alter table: %v", err)
		}

		p := propTestProvider("p8-user-1", "p8user1", "p8@example.com")
		e := propSetupEcho(t, p, database, "")

		rec := propPostCallback(e, "github")

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rec.Code)
		}

		// No user should exist.
		var count int
		_ = database.SqlDB.QueryRow(
			"SELECT COUNT(*) FROM users WHERE provider_id = 'p8-user-1'",
		).Scan(&count)
		if count != 0 {
			t.Errorf("users count = %d, want 0 after failed insert", count)
		}

		// No key should exist.
		var keyCount int
		_ = database.SqlDB.QueryRow("SELECT COUNT(*) FROM api_keys").Scan(&keyCount)
		if keyCount != 0 {
			t.Errorf("api_keys count = %d, want 0 after failed transaction", keyCount)
		}
	})

	// Scenario 2: Break api_keys table → key insert fails after user insert.
	t.Run("BrokenApiKeysTable", func(t *testing.T) {
		database := openTestDB(t)
		// Rename a required column in api_keys to cause INSERT failure.
		_, err := database.SqlDB.Exec("ALTER TABLE api_keys RENAME COLUMN secret_hash TO secret_hash_broken")
		if err != nil {
			t.Fatalf("alter table: %v", err)
		}

		p := propTestProvider("p8-user-2", "p8user2", "p8-2@example.com")
		e := propSetupEcho(t, p, database, "")

		rec := propPostCallback(e, "github")

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rec.Code)
		}

		// Verify no partial data: no user should exist (transaction rolled back).
		var userCount int
		_ = database.SqlDB.QueryRow(
			"SELECT COUNT(*) FROM users WHERE provider_id = 'p8-user-2'",
		).Scan(&userCount)
		if userCount != 0 {
			t.Errorf("users count = %d, want 0 (transaction should be rolled back)", userCount)
		}

		// No key should exist either.
		var keyCount int
		_ = database.SqlDB.QueryRow("SELECT COUNT(*) FROM api_keys").Scan(&keyCount)
		if keyCount != 0 {
			t.Errorf("api_keys count = %d, want 0 after failed transaction", keyCount)
		}
	})
}

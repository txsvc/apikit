package apikit_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/txsvc/apikit"
	"github.com/txsvc/apikit/internal/auth"
)

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

type errEnvelope struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// setupValidationTestDB sets up a test database with pre-populated users, api keys,
// pats, and admin tokens for testing ValidateCredential and NewAuthMiddleware.
func setupValidationTestDB(t *testing.T) (*apikit.DB, string, string, string, string, string) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "val_test.db")
	database, err := apikit.OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	// Insert active user
	activeUserID := "uid-active"
	_, err = database.SqlDB.Exec(
		`INSERT INTO users (id, username, email, role, status, provider, provider_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		activeUserID, "activeuser", "active@example.com", "user", "active",
		"github", "gh-active", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert active user: %v", err)
	}

	// Insert blocked user
	blockedUserID := "uid-blocked"
	_, err = database.SqlDB.Exec(
		`INSERT INTO users (id, username, email, role, status, provider, provider_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		blockedUserID, "blockeduser", "blocked@example.com", "user", "blocked",
		"github", "gh-blocked", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert blocked user: %v", err)
	}

	// Admin token
	adminHex := strings.Repeat("ef", 32)
	adminToken := "ak_admin_" + adminHex
	_, err = database.SqlDB.Exec(
		`INSERT INTO admin_config (key, value) VALUES ('admin_token_hash', ?)`,
		sha256Hex(adminToken),
	)
	if err != nil {
		t.Fatalf("insert admin token hash: %v", err)
	}

	// Active API key for active user
	apiKeySecret := "validsecret123"
	apiKeyToken := "ak_keyvalid_" + apiKeySecret
	_, err = database.SqlDB.Exec(
		`INSERT INTO api_keys (key_id, user_id, secret_hash, expires_days, expires_at, revoked_at, created_at)
		 VALUES (?, ?, ?, 30, ?, NULL, ?)`,
		"keyvalid", activeUserID, sha256Hex(apiKeySecret),
		time.Now().UTC().Add(30*24*time.Hour).Format(time.RFC3339),
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("insert valid api key: %v", err)
	}

	// API key for blocked user
	blockedKeyToken := "ak_keyblocked_" + apiKeySecret
	_, err = database.SqlDB.Exec(
		`INSERT INTO api_keys (key_id, user_id, secret_hash, expires_days, expires_at, revoked_at, created_at)
		 VALUES (?, ?, ?, 30, ?, NULL, ?)`,
		"keyblocked", blockedUserID, sha256Hex(apiKeySecret),
		time.Now().UTC().Add(30*24*time.Hour).Format(time.RFC3339),
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("insert blocked api key: %v", err)
	}

	// Active PAT for active user
	patSecret := "patsecret123"
	patToken := "ak_pat_patvalid_" + patSecret
	_, err = database.SqlDB.Exec(
		`INSERT INTO pats (token_id, user_id, name, secret_hash, permissions, expires_days, expires_at, revoked_at, created_at)
		 VALUES (?, ?, 'testpat', ?, '["workspaces:read"]', 30, ?, NULL, ?)`,
		"patvalid", activeUserID, sha256Hex(patSecret),
		time.Now().UTC().Add(30*24*time.Hour).Format(time.RFC3339),
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("insert valid pat: %v", err)
	}

	// PAT for blocked user
	blockedPatToken := "ak_pat_patblocked_" + patSecret
	_, err = database.SqlDB.Exec(
		`INSERT INTO pats (token_id, user_id, name, secret_hash, permissions, expires_days, expires_at, revoked_at, created_at)
		 VALUES (?, ?, 'blockedpat', ?, '["workspaces:read"]', 30, ?, NULL, ?)`,
		"patblocked", blockedUserID, sha256Hex(patSecret),
		time.Now().UTC().Add(30*24*time.Hour).Format(time.RFC3339),
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("insert blocked pat: %v", err)
	}

	return database, adminToken, apiKeyToken, blockedKeyToken, patToken, blockedPatToken
}

// TestValidateCredential_AC1_AdminToken tests validating an admin token outside Echo.
func TestValidateCredential_AC1_AdminToken(t *testing.T) {
	database, adminToken, _, _, _, _ := setupValidationTestDB(t)
	ctx := context.Background()

	// Without "Bearer " prefix
	info, err := apikit.ValidateCredential(ctx, database, adminToken)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if info.CredentialType != "admin_token" {
		t.Errorf("CredentialType = %q, want %q", info.CredentialType, "admin_token")
	}
	if info.Role != "admin" {
		t.Errorf("Role = %q, want %q", info.Role, "admin")
	}

	// With "Bearer " prefix
	infoBearer, err := apikit.ValidateCredential(ctx, database, "Bearer "+adminToken)
	if err != nil {
		t.Fatalf("expected success with Bearer prefix, got error: %v", err)
	}
	if !reflect.DeepEqual(info, infoBearer) {
		t.Errorf("expected AuthInfo with Bearer prefix to match without Bearer prefix")
	}

	// Invalid hex suffix length
	_, err = apikit.ValidateCredential(ctx, database, "ak_admin_short")
	if !errors.Is(err, apikit.ErrInvalidCredentials) {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}

	// Wrong admin token hash
	_, err = apikit.ValidateCredential(ctx, database, "ak_admin_"+strings.Repeat("00", 32))
	if !errors.Is(err, apikit.ErrInvalidCredentials) {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}
}

// TestValidateCredential_AC1_APIKey tests validating API keys outside Echo.
func TestValidateCredential_AC1_APIKey(t *testing.T) {
	database, _, apiKeyToken, _, _, _ := setupValidationTestDB(t)
	ctx := context.Background()

	// Success without Bearer
	info, err := apikit.ValidateCredential(ctx, database, apiKeyToken)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if info.CredentialType != "api_key" {
		t.Errorf("CredentialType = %q, want 'api_key'", info.CredentialType)
	}
	if info.UserID != "uid-active" {
		t.Errorf("UserID = %q, want 'uid-active'", info.UserID)
	}
	if info.KeyID != "keyvalid" {
		t.Errorf("KeyID = %q, want 'keyvalid'", info.KeyID)
	}

	// Success with Bearer
	infoBearer, err := apikit.ValidateCredential(ctx, database, "Bearer "+apiKeyToken)
	if err != nil {
		t.Fatalf("expected success with Bearer prefix, got %v", err)
	}
	if !reflect.DeepEqual(info, infoBearer) {
		t.Errorf("AuthInfo mismatch between Bearer and non-Bearer calls")
	}

	// Unknown key ID
	_, err = apikit.ValidateCredential(ctx, database, "ak_nonexistent_secret")
	if !errors.Is(err, apikit.ErrInvalidCredentials) {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}

	// Wrong secret
	_, err = apikit.ValidateCredential(ctx, database, "ak_keyvalid_wrongsecret")
	if !errors.Is(err, apikit.ErrInvalidCredentials) {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}
}

// TestValidateCredential_AC2_BlockedUser verifies that tokens belonging to
// users with status = 'blocked' return ErrUserBlocked for both API keys and PATs.
func TestValidateCredential_AC2_BlockedUser(t *testing.T) {
	database, _, _, blockedKeyToken, _, blockedPatToken := setupValidationTestDB(t)
	ctx := context.Background()

	// API key belonging to blocked user
	info, err := apikit.ValidateCredential(ctx, database, blockedKeyToken)
	if info != nil {
		t.Errorf("expected nil AuthInfo for blocked user API key, got %+v", info)
	}
	if !errors.Is(err, apikit.ErrUserBlocked) {
		t.Errorf("expected ErrUserBlocked for blocked API key, got %v", err)
	}
	var ae *apikit.AuthError
	if errors.As(err, &ae) {
		if ae.Code != http.StatusForbidden {
			t.Errorf("expected code 403, got %d", ae.Code)
		}
		if ae.Message != "user is blocked" {
			t.Errorf("expected message 'user is blocked', got %q", ae.Message)
		}
	} else {
		t.Fatal("expected err to be *apikit.AuthError")
	}

	// PAT belonging to blocked user
	infoPat, errPat := apikit.ValidateCredential(ctx, database, blockedPatToken)
	if infoPat != nil {
		t.Errorf("expected nil AuthInfo for blocked user PAT, got %+v", infoPat)
	}
	if !errors.Is(errPat, apikit.ErrUserBlocked) {
		t.Errorf("expected ErrUserBlocked for blocked PAT, got %v", errPat)
	}
}

// TestValidateCredential_AC1_RevokedAndExpired verifies revoked and expired credential errors.
func TestValidateCredential_AC1_RevokedAndExpired(t *testing.T) {
	database, _, _, _, _, _ := setupValidationTestDB(t)
	ctx := context.Background()

	// Insert revoked API key
	revokedSecret := "revokedsec"
	_, err := database.SqlDB.Exec(
		`INSERT INTO api_keys (key_id, user_id, secret_hash, expires_days, expires_at, revoked_at, created_at)
		 VALUES ('keyrevoked', 'uid-active', ?, 30, NULL, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		sha256Hex(revokedSecret),
	)
	if err != nil {
		t.Fatalf("insert revoked api key: %v", err)
	}

	_, err = apikit.ValidateCredential(ctx, database, "ak_keyrevoked_"+revokedSecret)
	if !errors.Is(err, apikit.ErrCredentialRevoked) {
		t.Errorf("expected ErrCredentialRevoked, got %v", err)
	}

	// Insert expired API key
	expiredSecret := "expiredsec"
	_, err = database.SqlDB.Exec(
		`INSERT INTO api_keys (key_id, user_id, secret_hash, expires_days, expires_at, revoked_at, created_at)
		 VALUES ('keyexpired', 'uid-active', ?, 30, '2020-01-01T00:00:00Z', NULL, '2020-01-01T00:00:00Z')`,
		sha256Hex(expiredSecret),
	)
	if err != nil {
		t.Fatalf("insert expired api key: %v", err)
	}

	_, err = apikit.ValidateCredential(ctx, database, "ak_keyexpired_"+expiredSecret)
	if !errors.Is(err, apikit.ErrCredentialExpired) {
		t.Errorf("expected ErrCredentialExpired, got %v", err)
	}
}

// TestValidateCredential_AC1_PAT tests validating PAT outside Echo.
func TestValidateCredential_AC1_PAT(t *testing.T) {
	database, _, _, _, patToken, _ := setupValidationTestDB(t)
	ctx := context.Background()

	info, err := apikit.ValidateCredential(ctx, database, patToken)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if info.CredentialType != "pat" {
		t.Errorf("CredentialType = %q, want 'pat'", info.CredentialType)
	}
	if info.TokenID != "patvalid" {
		t.Errorf("TokenID = %q, want 'patvalid'", info.TokenID)
	}
	if len(info.Permissions) != 1 || info.Permissions[0] != "workspaces:read" {
		t.Errorf("Permissions = %v, want ['workspaces:read']", info.Permissions)
	}

	// Unknown PAT
	_, err = apikit.ValidateCredential(ctx, database, "ak_pat_nonexistent_secret")
	if !errors.Is(err, apikit.ErrInvalidCredentials) {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}
}

// TestValidateCredential_AC1_UnrecognizedFormat tests unrecognized token formats.
func TestValidateCredential_AC1_UnrecognizedFormat(t *testing.T) {
	database, _, _, _, _, _ := setupValidationTestDB(t)
	ctx := context.Background()

	tests := []string{
		"",
		"Bearer ",
		"random_token_string",
		"ak_nounderscore",
		"ak_pat_nounderscore",
	}

	for _, token := range tests {
		_, err := apikit.ValidateCredential(ctx, database, token)
		if !errors.Is(err, apikit.ErrUnrecognizedToken) {
			t.Errorf("token %q: expected ErrUnrecognizedToken, got %v", token, err)
		}
	}
}

// TestValidateCredential_AC3_MiddlewareParity exercises both NewAuthMiddleware
// and ValidateCredential against identical tokens and DB states, confirming identical outcomes.
func TestValidateCredential_AC3_MiddlewareParity(t *testing.T) {
	database, adminToken, apiKeyToken, blockedKeyToken, patToken, blockedPatToken := setupValidationTestDB(t)

	// Add revoked and expired tokens
	revSecret := "revsec"
	_, err := database.SqlDB.Exec(
		`INSERT INTO api_keys (key_id, user_id, secret_hash, expires_days, expires_at, revoked_at, created_at)
		 VALUES ('keyrev2', 'uid-active', ?, 30, NULL, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		sha256Hex(revSecret),
	)
	if err != nil {
		t.Fatal(err)
	}
	revKeyToken := "ak_keyrev2_" + revSecret

	expSecret := "expsec"
	_, err = database.SqlDB.Exec(
		`INSERT INTO api_keys (key_id, user_id, secret_hash, expires_days, expires_at, revoked_at, created_at)
		 VALUES ('keyexp2', 'uid-active', ?, 30, '2020-01-01T00:00:00Z', NULL, '2020-01-01T00:00:00Z')`,
		sha256Hex(expSecret),
	)
	if err != nil {
		t.Fatal(err)
	}
	expKeyToken := "ak_keyexp2_" + expSecret

	cases := []struct {
		name       string
		token      string
		expectCode int
		expectErr  error
	}{
		{name: "admin_valid", token: adminToken, expectCode: http.StatusOK, expectErr: nil},
		{name: "admin_wrong_hash", token: "ak_admin_" + strings.Repeat("00", 32), expectCode: http.StatusUnauthorized, expectErr: apikit.ErrInvalidCredentials},
		{name: "api_key_valid", token: apiKeyToken, expectCode: http.StatusOK, expectErr: nil},
		{name: "api_key_wrong_secret", token: "ak_keyvalid_wrong", expectCode: http.StatusUnauthorized, expectErr: apikit.ErrInvalidCredentials},
		{name: "api_key_blocked", token: blockedKeyToken, expectCode: http.StatusForbidden, expectErr: apikit.ErrUserBlocked},
		{name: "api_key_revoked", token: revKeyToken, expectCode: http.StatusUnauthorized, expectErr: apikit.ErrCredentialRevoked},
		{name: "api_key_expired", token: expKeyToken, expectCode: http.StatusUnauthorized, expectErr: apikit.ErrCredentialExpired},
		{name: "pat_valid", token: patToken, expectCode: http.StatusOK, expectErr: nil},
		{name: "pat_blocked", token: blockedPatToken, expectCode: http.StatusForbidden, expectErr: apikit.ErrUserBlocked},
		{name: "unrecognized", token: "unknown_format", expectCode: http.StatusUnauthorized, expectErr: apikit.ErrUnrecognizedToken},
	}

	registry := auth.NewPermissionRegistry()
	mw := auth.NewAuthMiddleware(database, registry)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 1. Direct ValidateCredential call
			valInfo, valErr := apikit.ValidateCredential(context.Background(), database, tc.token)

			// 2. Middleware call through Echo
			e := echo.New()
			var mwInjectedInfo *apikit.AuthInfo
			handler := func(c echo.Context) error {
				mwInjectedInfo = apikit.GetAuthInfo(c)
				return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
			}
			e.GET("/test", handler, mw)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Authorization", "Bearer "+tc.token)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			// Compare HTTP status
			if rec.Code != tc.expectCode {
				t.Fatalf("middleware code = %d, want %d (body: %s)", rec.Code, tc.expectCode, rec.Body.String())
			}

			if tc.expectErr != nil {
				if !errors.Is(valErr, tc.expectErr) {
					t.Fatalf("ValidateCredential error = %v, want %v", valErr, tc.expectErr)
				}
				var resp errEnvelope
				if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
					t.Fatalf("failed to unmarshal middleware error: %v", err)
				}
				var ae *apikit.AuthError
				if errors.As(valErr, &ae) {
					if resp.Error.Message != ae.Message {
						t.Errorf("error message mismatch: middleware %q, ValidateCredential %q", resp.Error.Message, ae.Message)
					}
					if resp.Error.Code != ae.Code {
						t.Errorf("error code mismatch: middleware %d, ValidateCredential %d", resp.Error.Code, ae.Code)
					}
				}
			} else {
				if valErr != nil {
					t.Fatalf("ValidateCredential returned unexpected error: %v", valErr)
				}
				if !reflect.DeepEqual(valInfo, mwInjectedInfo) {
					t.Errorf("AuthInfo mismatch:\nValidateCredential: %+v\nMiddleware: %+v", valInfo, mwInjectedInfo)
				}
			}
		})
	}
}

// TestValidateCredential_ContextCancellation verifies that canceling the context
// passed to ValidateCredential causes it to abort and return an error.
func TestValidateCredential_ContextCancellation(t *testing.T) {
	database, adminToken, _, _, _, _ := setupValidationTestDB(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := apikit.ValidateCredential(ctx, database, adminToken)
	if err == nil {
		t.Fatal("expected error on canceled context, got nil")
	}
}

// TestAuthError_TypeAliasAndSentinels verifies that AuthError and all error
// sentinels are accessible and function with errors.Is.
func TestAuthError_TypeAliasAndSentinels(t *testing.T) {
	sentinels := []struct {
		err     *apikit.AuthError
		code    int
		message string
	}{
		{apikit.ErrUnrecognizedToken, http.StatusUnauthorized, "unrecognized token format"},
		{apikit.ErrInvalidCredentials, http.StatusUnauthorized, "invalid credentials"},
		{apikit.ErrCredentialRevoked, http.StatusUnauthorized, "credential revoked"},
		{apikit.ErrCredentialExpired, http.StatusUnauthorized, "credential expired"},
		{apikit.ErrUserBlocked, http.StatusForbidden, "user is blocked"},
		{apikit.ErrInternalServer, http.StatusInternalServerError, "internal server error"},
	}

	for _, s := range sentinels {
		if s.err.Code != s.code {
			t.Errorf("sentinel code = %d, want %d", s.err.Code, s.code)
		}
		if s.err.Message != s.message {
			t.Errorf("sentinel message = %q, want %q", s.err.Message, s.message)
		}
		if s.err.Error() != s.message {
			t.Errorf("sentinel Error() = %q, want %q", s.err.Error(), s.message)
		}
		if !errors.Is(s.err, s.err) {
			t.Errorf("errors.Is failed for sentinel %v", s.err)
		}
	}

	// Verify AuthError type alias compiles and works
	var ae *apikit.AuthError = &apikit.AuthError{
		Code:    http.StatusTeapot,
		Message: "teapot",
	}
	if ae.Error() != "teapot" {
		t.Errorf("ae.Error() = %q, want 'teapot'", ae.Error())
	}
}


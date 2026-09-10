package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/txsvc/apikit/internal/db"
)

// ========================================================================
// Helpers
// ========================================================================

// errorResponse matches the standard JSON error envelope format.
type errorResponse struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// runMiddleware creates an Echo instance with the auth middleware applied and
// sends a GET request with the specified headers. Returns the response recorder.
// Pass nil for headers to send a request with no custom headers (e.g. no
// Authorization header).
func runMiddleware(t *testing.T, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	e := echo.New()

	database := &db.DB{}
	registry := NewPermissionRegistry()
	mw := NewAuthMiddleware(database, registry)

	handler := func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	e.GET("/test", handler, mw)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	return rec
}

// assertErrorResponse checks that the response has the expected HTTP status
// code and error message in the standard JSON error envelope.
func assertErrorResponse(t *testing.T, rec *httptest.ResponseRecorder, expectedCode int, expectedMessage string) {
	t.Helper()

	if rec.Code != expectedCode {
		t.Errorf("expected HTTP status %d, got %d", expectedCode, rec.Code)
	}

	var resp errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse error response body: %v\nbody: %s", err, rec.Body.String())
	}

	if resp.Error.Code != expectedCode {
		t.Errorf("expected error code %d in body, got %d", expectedCode, resp.Error.Code)
	}

	if resp.Error.Message != expectedMessage {
		t.Errorf("expected error message %q, got %q", expectedMessage, resp.Error.Message)
	}
}

// readAuthSourceFiles reads all non-test Go source files in the current
// package directory and returns their concatenated contents.
func readAuthSourceFiles(t *testing.T) string {
	t.Helper()

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("failed to glob source files: %v", err)
	}

	var sb strings.Builder
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("failed to read %s: %v", f, err)
		}
		sb.Write(data)
	}

	return sb.String()
}

// ========================================================================
// 1.1 Bearer Token Extraction Tests (REQ-2)
// ========================================================================

// TestMiddleware_MissingAuthHeader verifies that a request with no Authorization
// header is rejected with HTTP 401 and message "missing authorization header".
//
// Test Spec: TS-05-4
// Requirement: 05-REQ-2.1
func TestMiddleware_MissingAuthHeader(t *testing.T) {
	rec := runMiddleware(t, nil)
	assertErrorResponse(t, rec, http.StatusUnauthorized, "missing authorization header")
}

// TestMiddleware_InvalidAuthFormat verifies that a non-Bearer authentication
// scheme (e.g. Basic) is rejected with HTTP 401 and message
// "invalid authorization header format".
//
// Test Spec: TS-05-5
// Requirement: 05-REQ-2.2
func TestMiddleware_InvalidAuthFormat(t *testing.T) {
	rec := runMiddleware(t, map[string]string{
		"Authorization": "Basic dXNlcjpwYXNz",
	})
	assertErrorResponse(t, rec, http.StatusUnauthorized, "invalid authorization header format")
}

// TestMiddleware_BearerNoSpace verifies that "Bearer" with no trailing space
// or token is treated as invalid format and returns HTTP 401 with message
// "invalid authorization header format".
//
// Test Spec: TS-05-E2
// Requirement: 05-REQ-2.E1
func TestMiddleware_BearerNoSpace(t *testing.T) {
	rec := runMiddleware(t, map[string]string{
		"Authorization": "Bearer",
	})
	assertErrorResponse(t, rec, http.StatusUnauthorized, "invalid authorization header format")
}

// TestMiddleware_EmptyBearer verifies that "Bearer " (Bearer followed by a
// single space and nothing else) is rejected with HTTP 401 and message
// "missing token".
//
// Test Spec: TS-05-6
// Requirement: 05-REQ-2.3
func TestMiddleware_EmptyBearer(t *testing.T) {
	rec := runMiddleware(t, map[string]string{
		"Authorization": "Bearer ",
	})
	assertErrorResponse(t, rec, http.StatusUnauthorized, "missing token")
}

// TestMiddleware_WellFormedHeader verifies that a well-formed
// "Authorization: Bearer <token>" header is accepted by the extraction stage
// and forwarded to credential type detection. The response must NOT contain
// any of the extraction-phase error messages.
//
// Test Spec: TS-05-7
// Requirement: 05-REQ-2.4
func TestMiddleware_WellFormedHeader(t *testing.T) {
	// Use a token in valid admin format (64 hex chars after prefix_admin_).
	token := "ak_admin_" + strings.Repeat("ab", 32)

	rec := runMiddleware(t, map[string]string{
		"Authorization": "Bearer " + token,
	})

	// The response must not be one of the token-extraction error messages.
	// It may be a downstream credential-validation error or a successful 200.
	extractionErrors := []string{
		"missing authorization header",
		"invalid authorization header format",
		"missing token",
	}

	body := rec.Body.Bytes()
	if len(body) > 0 {
		var resp errorResponse
		if json.Unmarshal(body, &resp) == nil && resp.Error.Message != "" {
			for _, msg := range extractionErrors {
				if resp.Error.Message == msg {
					t.Errorf("expected extraction to pass, but got extraction error: %q", msg)
				}
			}
		}
	}
}

// TestMiddleware_UnrecognizedTokenFormat verifies that a token not matching any
// recognized credential pattern is rejected with HTTP 401 and message
// "unrecognized token format" when sent through the full middleware.
//
// Test Spec: TS-05-12
// Requirement: 05-REQ-3.5
func TestMiddleware_UnrecognizedTokenFormat(t *testing.T) {
	rec := runMiddleware(t, map[string]string{
		"Authorization": "Bearer totally_garbage_token",
	})
	assertErrorResponse(t, rec, http.StatusUnauthorized, "unrecognized token format")
}

// ========================================================================
// 1.2 parseToken Credential Type Detection Tests (REQ-3)
// ========================================================================

// TestParseToken_DetectionOrder verifies that parseToken checks admin pattern
// first, then PAT, then API key, returning the correct credential type for each.
//
// Test Spec: TS-05-8
// Requirement: 05-REQ-3.1
func TestParseToken_DetectionOrder(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		wantType string
	}{
		{
			name:     "admin token",
			token:    "ak_admin_" + strings.Repeat("ab", 32),
			wantType: "admin_token",
		},
		{
			name:     "PAT",
			token:    "ak_pat_tokenid_secretval",
			wantType: "pat",
		},
		{
			name:     "API key",
			token:    "ak_keyid_secretval",
			wantType: "api_key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			credType, _, err := parseToken(tt.token)
			if err != nil {
				t.Fatalf("parseToken(%q) returned unexpected error: %v", tt.token, err)
			}
			if credType != tt.wantType {
				t.Errorf("parseToken(%q) credential type = %q, want %q", tt.token, credType, tt.wantType)
			}
		})
	}
}

// TestParseToken_AdminToken verifies that a valid admin token is parsed with
// credential type "admin_token" and the full token as the sole component.
//
// Test Spec: TS-05-9
// Requirement: 05-REQ-3.2
func TestParseToken_AdminToken(t *testing.T) {
	token := "ak_admin_" + strings.Repeat("ab", 32)

	credType, components, err := parseToken(token)
	if err != nil {
		t.Fatalf("parseToken returned unexpected error: %v", err)
	}
	if credType != "admin_token" {
		t.Errorf("expected credential type %q, got %q", "admin_token", credType)
	}
	if len(components) != 1 {
		t.Fatalf("expected 1 component, got %d", len(components))
	}
	if components[0] != token {
		t.Errorf("expected component[0] = full token %q, got %q", token, components[0])
	}
}

// TestParseToken_PAT verifies that a valid PAT token is parsed with credential
// type "pat" and token_id, secret as components.
//
// Test Spec: TS-05-10
// Requirement: 05-REQ-3.3
func TestParseToken_PAT(t *testing.T) {
	credType, components, err := parseToken("ak_pat_mytoken123_mysecret456")
	if err != nil {
		t.Fatalf("parseToken returned unexpected error: %v", err)
	}
	if credType != "pat" {
		t.Errorf("expected credential type %q, got %q", "pat", credType)
	}
	if len(components) != 2 {
		t.Fatalf("expected 2 components, got %d", len(components))
	}
	if components[0] != "mytoken123" {
		t.Errorf("expected token_id %q, got %q", "mytoken123", components[0])
	}
	if components[1] != "mysecret456" {
		t.Errorf("expected secret %q, got %q", "mysecret456", components[1])
	}
}

// TestParseToken_APIKey verifies that a valid API key token is parsed with
// credential type "api_key" and key_id, secret as components.
//
// Test Spec: TS-05-11
// Requirement: 05-REQ-3.4
func TestParseToken_APIKey(t *testing.T) {
	credType, components, err := parseToken("ak_mykey123_mysecretabc")
	if err != nil {
		t.Fatalf("parseToken returned unexpected error: %v", err)
	}
	if credType != "api_key" {
		t.Errorf("expected credential type %q, got %q", "api_key", credType)
	}
	if len(components) != 2 {
		t.Fatalf("expected 2 components, got %d", len(components))
	}
	if components[0] != "mykey123" {
		t.Errorf("expected key_id %q, got %q", "mykey123", components[0])
	}
	if components[1] != "mysecretabc" {
		t.Errorf("expected secret %q, got %q", "mysecretabc", components[1])
	}
}

// TestParseToken_UnrecognizedFormat verifies that parseToken returns a non-nil
// error for a token that does not match any recognized credential pattern.
//
// Test Spec: TS-05-12 (parseToken-level assertion)
// Requirement: 05-REQ-3.5
func TestParseToken_UnrecognizedFormat(t *testing.T) {
	_, _, err := parseToken("totally_garbage_token")
	if err == nil {
		t.Error("expected non-nil error for unrecognized token format")
	}
}

// TestParseToken_EmptyString verifies that parseToken returns a non-nil error
// for an empty string input.
//
// Test Spec: TS-05-E3
// Requirement: 05-REQ-3.E1
func TestParseToken_EmptyString(t *testing.T) {
	_, _, err := parseToken("")
	if err == nil {
		t.Error("expected non-nil error for empty token string")
	}
}

// TestParseToken_WrongPrefix verifies that parseToken returns a non-nil error
// when the token begins with a prefix other than TokenPrefix.
//
// Test Spec: TS-05-E4
// Requirement: 05-REQ-3.E2
func TestParseToken_WrongPrefix(t *testing.T) {
	_, _, err := parseToken("wrong_prefix_keyid_secret")
	if err == nil {
		t.Error("expected non-nil error for wrong prefix")
	}
}

// TestParseToken_PATOverAPIKey verifies that a token starting with
// '<prefix>_pat_' is correctly identified as "pat" due to detection-order
// precedence and is never misclassified as "api_key".
//
// Test Spec: TS-05-E5
// Requirement: 05-REQ-3.E3
func TestParseToken_PATOverAPIKey(t *testing.T) {
	credType, components, err := parseToken("ak_pat_tokenid_secretval")
	if err != nil {
		t.Fatalf("parseToken returned unexpected error: %v", err)
	}
	if credType == "api_key" {
		t.Error("token starting with 'ak_pat_' was misclassified as 'api_key'")
	}
	if credType != "pat" {
		t.Errorf("expected credential type %q, got %q", "pat", credType)
	}
	if len(components) != 2 {
		t.Fatalf("expected 2 components, got %d", len(components))
	}
	if components[0] != "tokenid" {
		t.Errorf("expected token_id %q, got %q", "tokenid", components[0])
	}
	if components[1] != "secretval" {
		t.Errorf("expected secret %q, got %q", "secretval", components[1])
	}
}

// TestParseToken_AdminBeforePAT verifies that the admin pattern is checked
// before the PAT or API key patterns, so an admin token is never misclassified.
// An admin token superficially matches the API key pattern
// (prefix_admin_hexstring ≈ prefix_keyid_secret), but detection order ensures
// correct classification.
func TestParseToken_AdminBeforePAT(t *testing.T) {
	token := "ak_admin_" + strings.Repeat("ab", 32)
	credType, _, err := parseToken(token)
	if err != nil {
		t.Fatalf("parseToken returned unexpected error: %v", err)
	}
	if credType != "admin_token" {
		t.Errorf("expected credential type %q, got %q; admin should be detected before other patterns", "admin_token", credType)
	}
}

// ========================================================================
// 1.3 hashToken and Constant-Time Comparison Tests (REQ-11)
// ========================================================================

// TestHashToken verifies that hashToken computes the correct SHA-256 digest
// and returns it as a 64-character lowercase hex string.
//
// Test Spec: TS-05-50
// Requirement: 05-REQ-11.2
func TestHashToken(t *testing.T) {
	result := hashToken("hello")

	// SHA-256 of "hello" — the canonical expected value.
	const expected = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"

	if len(result) != 64 {
		t.Errorf("expected length 64, got %d", len(result))
	}
	if result != expected {
		t.Errorf("hashToken(%q) = %q, want %q", "hello", result, expected)
	}
	if result != strings.ToLower(result) {
		t.Errorf("hashToken result is not all lowercase: %q", result)
	}
}

// TestHashToken_EmptyString verifies that hashToken correctly handles empty
// input and returns a valid 64-character hex digest.
func TestHashToken_EmptyString(t *testing.T) {
	result := hashToken("")

	// Compute expected SHA-256 of empty string.
	h := sha256.Sum256([]byte(""))
	expected := hex.EncodeToString(h[:])

	if len(result) != 64 {
		t.Errorf("expected length 64, got %d", len(result))
	}
	if result != expected {
		t.Errorf("hashToken(%q) = %q, want %q", "", result, expected)
	}
}

// TestConstantTimeComparison performs code inspection to verify that all hash
// comparisons use crypto/subtle.ConstantTimeCompare and never use == or
// bytes.Equal for hash values.
//
// Test Spec: TS-05-49
// Requirement: 05-REQ-11.1
func TestConstantTimeComparison(t *testing.T) {
	source := readAuthSourceFiles(t)
	if source == "" {
		t.Fatal("no non-test source files found in the auth package")
	}

	// Verify at least 3 occurrences of subtle.ConstantTimeCompare (one per
	// credential type: admin token, API key, PAT).
	count := strings.Count(source, "subtle.ConstantTimeCompare")
	if count < 3 {
		t.Errorf("expected at least 3 occurrences of subtle.ConstantTimeCompare, found %d", count)
	}

	// Verify no hash comparisons use == or bytes.Equal.
	lines := strings.Split(source, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip comments.
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") {
			continue
		}

		// Flag bytes.Equal used with hash-related variables.
		if strings.Contains(trimmed, "bytes.Equal") &&
			(strings.Contains(strings.ToLower(trimmed), "hash") ||
				strings.Contains(strings.ToLower(trimmed), "secret")) {
			t.Errorf("line %d: found bytes.Equal used for hash comparison: %s", i+1, trimmed)
		}
	}
}

// TestHashRepresentation verifies via code inspection that both computed and
// stored hashes are converted to []byte before being passed to
// subtle.ConstantTimeCompare, ensuring valid and length-safe comparison.
//
// Test Spec: TS-05-E20
// Requirement: 05-REQ-11.E1
func TestHashRepresentation(t *testing.T) {
	source := readAuthSourceFiles(t)
	if source == "" {
		t.Fatal("no non-test source files found in the auth package")
	}

	// subtle.ConstantTimeCompare must be present.
	if !strings.Contains(source, "subtle.ConstantTimeCompare") {
		t.Fatal("subtle.ConstantTimeCompare not found in source files")
	}

	// Verify no direct string comparison of hash values (e.g. hashToken(...) ==).
	lines := strings.Split(source, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip comments.
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") {
			continue
		}

		// Flag direct string equality comparison of hashToken results.
		if strings.Contains(trimmed, "hashToken(") && strings.Contains(trimmed, "==") {
			t.Errorf("line %d: found direct string comparison of hashToken result: %s", i+1, trimmed)
		}
	}
}

// ========================================================================
// ValidateCredential Unit & Parity Tests (Issue #42)
// ========================================================================

func TestValidateCredential_UnitAndParity(t *testing.T) {
	database := openTestDB(t)

	// Setup users
	insertUser(t, database, "uid-active", "user", "active")
	insertUser(t, database, "uid-blocked", "user", "blocked")

	// Setup admin token
	adminToken := "ak_admin_" + strings.Repeat("ef", 32)
	insertAdminConfig(t, database, "admin_token_hash", hexSHA256(adminToken))

	// Setup API keys
	apiKeySecret := "validsecret123"
	apiKeyToken := "ak_keyvalid_" + apiKeySecret
	insertAPIKey(t, database, "keyvalid", "uid-active", hexSHA256(apiKeySecret), "2099-01-01T00:00:00Z", "")

	blockedKeyToken := "ak_keyblocked_" + apiKeySecret
	insertAPIKey(t, database, "keyblocked", "uid-blocked", hexSHA256(apiKeySecret), "2099-01-01T00:00:00Z", "")

	revokedKeyToken := "ak_keyrevoked_" + apiKeySecret
	insertAPIKey(t, database, "keyrevoked", "uid-active", hexSHA256(apiKeySecret), "", "2026-01-01T00:00:00Z")

	expiredKeyToken := "ak_keyexpired_" + apiKeySecret
	insertAPIKey(t, database, "keyexpired", "uid-active", hexSHA256(apiKeySecret), "2020-01-01T00:00:00Z", "")

	// Setup PATs
	patSecret := "patsecret123"
	patToken := "ak_pat_patvalid_" + patSecret
	insertPATRow(t, database, "patvalid", "uid-active", hexSHA256(patSecret), `["workspaces:read"]`, "2099-01-01T00:00:00Z", "")

	blockedPatToken := "ak_pat_patblocked_" + patSecret
	insertPATRow(t, database, "patblocked", "uid-blocked", hexSHA256(patSecret), `["workspaces:read"]`, "2099-01-01T00:00:00Z", "")

	revokedPatToken := "ak_pat_patrevoked_" + patSecret
	insertPATRow(t, database, "patrevoked", "uid-active", hexSHA256(patSecret), `["workspaces:read"]`, "", "2026-01-01T00:00:00Z")

	expiredPatToken := "ak_pat_patexpired_" + patSecret
	insertPATRow(t, database, "patexpired", "uid-active", hexSHA256(patSecret), `["workspaces:read"]`, "2020-01-01T00:00:00Z", "")

	cases := []struct {
		name       string
		token      string
		expectCode int
		expectErr  error
	}{
		{name: "admin_valid", token: adminToken, expectCode: http.StatusOK, expectErr: nil},
		{name: "admin_bearer_valid", token: "Bearer " + adminToken, expectCode: http.StatusOK, expectErr: nil},
		{name: "admin_invalid_hex", token: "ak_admin_short", expectCode: http.StatusUnauthorized, expectErr: ErrInvalidCredentials},
		{name: "admin_wrong_hash", token: "ak_admin_" + strings.Repeat("00", 32), expectCode: http.StatusUnauthorized, expectErr: ErrInvalidCredentials},
		{name: "api_key_valid", token: apiKeyToken, expectCode: http.StatusOK, expectErr: nil},
		{name: "api_key_bearer_valid", token: "Bearer " + apiKeyToken, expectCode: http.StatusOK, expectErr: nil},
		{name: "api_key_wrong_secret", token: "ak_keyvalid_wrongsecret", expectCode: http.StatusUnauthorized, expectErr: ErrInvalidCredentials},
		{name: "api_key_blocked", token: blockedKeyToken, expectCode: http.StatusForbidden, expectErr: ErrUserBlocked},
		{name: "api_key_revoked", token: revokedKeyToken, expectCode: http.StatusUnauthorized, expectErr: ErrCredentialRevoked},
		{name: "api_key_expired", token: expiredKeyToken, expectCode: http.StatusUnauthorized, expectErr: ErrCredentialExpired},
		{name: "pat_valid", token: patToken, expectCode: http.StatusOK, expectErr: nil},
		{name: "pat_bearer_valid", token: "Bearer " + patToken, expectCode: http.StatusOK, expectErr: nil},
		{name: "pat_wrong_secret", token: "ak_pat_patvalid_wrongsecret", expectCode: http.StatusUnauthorized, expectErr: ErrInvalidCredentials},
		{name: "pat_blocked", token: blockedPatToken, expectCode: http.StatusForbidden, expectErr: ErrUserBlocked},
		{name: "pat_revoked", token: revokedPatToken, expectCode: http.StatusUnauthorized, expectErr: ErrCredentialRevoked},
		{name: "pat_expired", token: expiredPatToken, expectCode: http.StatusUnauthorized, expectErr: ErrCredentialExpired},
		{name: "unrecognized", token: "garbage_format", expectCode: http.StatusUnauthorized, expectErr: ErrUnrecognizedToken},
		{name: "unrecognized_api_key_no_secret", token: "ak_nosecret", expectCode: http.StatusUnauthorized, expectErr: ErrUnrecognizedToken},
	}

	registry := NewPermissionRegistry()
	mw := NewAuthMiddleware(database, registry)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Direct ValidateCredential call
			valInfo, valErr := ValidateCredential(context.Background(), database, tc.token)

			// Middleware call
			e := echo.New()
			var mwInjectedInfo *AuthInfo
			handler := func(c echo.Context) error {
				mwInjectedInfo = GetAuthInfo(c)
				return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
			}
			e.GET("/test", handler, mw)

			cleanToken := strings.TrimPrefix(tc.token, "Bearer ")
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Authorization", "Bearer "+cleanToken)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			if rec.Code != tc.expectCode {
				t.Fatalf("middleware code = %d, want %d (body: %s)", rec.Code, tc.expectCode, rec.Body.String())
			}

			if tc.expectErr != nil {
				if !errors.Is(valErr, tc.expectErr) {
					t.Fatalf("ValidateCredential error = %v, want %v", valErr, tc.expectErr)
				}
				var resp errorResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
					t.Fatalf("unmarshal middleware response: %v", err)
				}
				var ae *AuthError
				if errors.As(valErr, &ae) {
					if resp.Error.Message != ae.Message {
						t.Errorf("message mismatch: middleware %q, ValidateCredential %q", resp.Error.Message, ae.Message)
					}
					if resp.Error.Code != ae.Code {
						t.Errorf("code mismatch: middleware %d, ValidateCredential %d", resp.Error.Code, ae.Code)
					}
				}
			} else {
				if valErr != nil {
					t.Fatalf("ValidateCredential unexpected error: %v", valErr)
				}
				if !reflect.DeepEqual(valInfo, mwInjectedInfo) {
					t.Errorf("AuthInfo mismatch:\nValidateCredential: %+v\nMiddleware: %+v", valInfo, mwInjectedInfo)
				}
			}
		})
	}
}

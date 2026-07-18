package oauth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/txsvc/apikit/internal/oauth"
)

// setupCallbackEcho creates an Echo instance with OAuth handlers mounted,
// using a mock "github" provider. Suitable for testing request validation.
func setupCallbackEcho(t *testing.T) *echo.Echo {
	t.Helper()

	e := echo.New()
	registry := oauth.NewRegistry()

	mock := &mockProvider{name: "github"}
	if err := registry.Register(mock); err != nil {
		t.Fatalf("failed to register mock provider: %v", err)
	}

	g := e.Group("")
	oauth.RegisterOAuthHandlers(g, registry, nil, "")
	return e
}

// callbackErrorResponse represents the JSON error envelope returned by the
// callback handler.
type callbackErrorResponse struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// parseCallbackError decodes the error response body.
func parseCallbackError(t *testing.T, body string) callbackErrorResponse {
	t.Helper()
	var resp callbackErrorResponse
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&resp); err != nil {
		t.Fatalf("failed to decode error response: %v (body: %s)", err, body)
	}
	return resp
}

// ========================================================================
// TS-06-22: Valid request body passes validation
// (Requirement: 06-REQ-6.1)
// ========================================================================

// TestCallback_ValidBody verifies that a POST request with provider, code,
// redirect_uri all non-empty and expires a valid value proceeds past
// validation without returning HTTP 400.
func TestCallback_ValidBody(t *testing.T) {
	e := setupCallbackEcho(t)

	body := `{"provider":"github","code":"abc","redirect_uri":"http://localhost:9000/cb","expires":30}`
	req := httptest.NewRequest(http.MethodPost, "/auth/callback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// The request should not be rejected with a validation error.
	if rec.Code == http.StatusBadRequest {
		resp := parseCallbackError(t, rec.Body.String())
		validationMsgs := []string{
			"provider is required",
			"code is required",
			"redirect_uri is required",
			"expires must be 0, 30, 60, or 90",
		}
		for _, msg := range validationMsgs {
			if resp.Error.Message == msg {
				t.Fatalf("valid request rejected with validation error: %q", msg)
			}
		}
	}
}

// ========================================================================
// TS-06-23: Missing provider returns HTTP 400
// (Requirement: 06-REQ-6.2)
// ========================================================================

// TestCallback_MissingProvider verifies that a missing provider field
// returns HTTP 400 with message "provider is required".
func TestCallback_MissingProvider(t *testing.T) {
	e := setupCallbackEcho(t)

	body := `{"code":"abc","redirect_uri":"http://localhost:9000/cb"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/callback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	resp := parseCallbackError(t, rec.Body.String())
	if resp.Error.Code != 400 {
		t.Errorf("error.code = %d, want 400", resp.Error.Code)
	}
	if resp.Error.Message != "provider is required" {
		t.Errorf("error.message = %q, want %q", resp.Error.Message, "provider is required")
	}
}

// ========================================================================
// TS-06-24: Missing code returns HTTP 400
// (Requirement: 06-REQ-6.3)
// ========================================================================

// TestCallback_MissingCode verifies that a missing code field returns
// HTTP 400 with message "code is required".
func TestCallback_MissingCode(t *testing.T) {
	e := setupCallbackEcho(t)

	body := `{"provider":"github","redirect_uri":"http://localhost:9000/cb"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/callback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	resp := parseCallbackError(t, rec.Body.String())
	if resp.Error.Message != "code is required" {
		t.Errorf("error.message = %q, want %q", resp.Error.Message, "code is required")
	}
}

// ========================================================================
// TS-06-25: Missing redirect_uri returns HTTP 400
// (Requirement: 06-REQ-6.4)
// ========================================================================

// TestCallback_MissingRedirectURI verifies that a missing redirect_uri
// field returns HTTP 400 with message "redirect_uri is required".
func TestCallback_MissingRedirectURI(t *testing.T) {
	e := setupCallbackEcho(t)

	body := `{"provider":"github","code":"abc"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/callback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	resp := parseCallbackError(t, rec.Body.String())
	if resp.Error.Message != "redirect_uri is required" {
		t.Errorf("error.message = %q, want %q", resp.Error.Message, "redirect_uri is required")
	}
}

// ========================================================================
// TS-06-26: Invalid expires returns HTTP 400
// (Requirement: 06-REQ-6.5)
// ========================================================================

// TestCallback_InvalidExpires verifies that an expires value not in
// {0, 30, 60, 90} returns HTTP 400 with the appropriate message.
func TestCallback_InvalidExpires(t *testing.T) {
	e := setupCallbackEcho(t)

	body := `{"provider":"github","code":"abc","redirect_uri":"http://localhost:9000/cb","expires":45}`
	req := httptest.NewRequest(http.MethodPost, "/auth/callback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	resp := parseCallbackError(t, rec.Body.String())
	if resp.Error.Code != 400 {
		t.Errorf("error.code = %d, want 400", resp.Error.Code)
	}
	if resp.Error.Message != "expires must be 0, 30, 60, or 90" {
		t.Errorf("error.message = %q, want %q", resp.Error.Message, "expires must be 0, 30, 60, or 90")
	}
}

// ========================================================================
// TS-06-27: Absent expires defaults to 90
// (Requirement: 06-REQ-6.6)
// ========================================================================

// TestCallback_DefaultExpires90 verifies that when the expires field is
// absent from the request body, the handler defaults to 90 days. When the
// full callback flow is implemented, the resulting api_key.expires_at
// should be approximately 90 days from now.
func TestCallback_DefaultExpires90(t *testing.T) {
	e := setupCallbackEcho(t)

	// Request body omits the expires field entirely.
	body := `{"provider":"github","code":"abc","redirect_uri":"http://localhost:9000/cb"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/callback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// Must not be rejected with an expires validation error.
	if rec.Code == http.StatusBadRequest {
		resp := parseCallbackError(t, rec.Body.String())
		if resp.Error.Message == "expires must be 0, 30, 60, or 90" {
			t.Fatal("absent expires should default to 90, not trigger validation error")
		}
	}

	// When full callback flow is implemented (HTTP 200 with api_key),
	// verify the expires_at is approximately 90 days from now.
	if rec.Code == http.StatusOK {
		var respBody map[string]any
		if err := json.NewDecoder(strings.NewReader(rec.Body.String())).Decode(&respBody); err != nil || respBody == nil {
			t.Fatal("expected non-null JSON object in response body")
		}

		apiKey, ok := respBody["api_key"].(map[string]any)
		if !ok {
			t.Fatal("expected api_key object in response body")
		}

		expiresAtStr, ok := apiKey["expires_at"].(string)
		if !ok {
			t.Fatal("expected expires_at string in api_key")
		}

		expiresAt, err := time.Parse(time.RFC3339, expiresAtStr)
		if err != nil {
			t.Fatalf("invalid expires_at format: %v", err)
		}

		diff := time.Until(expiresAt)
		lower := 89 * 24 * time.Hour
		upper := 91 * 24 * time.Hour
		if diff < lower || diff > upper {
			t.Errorf("expires_at should be ~90 days from now, got difference of %v", diff)
		}
	}
}

// ========================================================================
// TS-06-E9: Non-JSON request body returns HTTP 400
// (Requirement: 06-REQ-6.E1)
// ========================================================================

// TestCallback_InvalidJSON verifies that a non-JSON request body returns
// HTTP 400 with an error response.
func TestCallback_InvalidJSON(t *testing.T) {
	e := setupCallbackEcho(t)

	req := httptest.NewRequest(http.MethodPost, "/auth/callback", strings.NewReader("not-json-{{"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	resp := parseCallbackError(t, rec.Body.String())
	if resp.Error.Code != 400 {
		t.Errorf("error.code = %d, want 400", resp.Error.Code)
	}
}

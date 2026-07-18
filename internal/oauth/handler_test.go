package oauth_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/txsvc/apikit/internal/oauth"
)

// ========================================================================
// TS-06-48: RegisterOAuthHandlers mounts routes with correct headers
// (Requirement: 06-REQ-13.1)
// ========================================================================

// TestHandler_RegisterMountsRoutes verifies that RegisterOAuthHandlers
// mounts GET /auth/providers with Cache-Control: public, max-age=300
// and POST /auth/callback returns 400 (validation error) not 404.
func TestHandler_RegisterMountsRoutes(t *testing.T) {
	e := echo.New()
	registry := oauth.NewRegistry()

	// Register a mock provider so the registry is non-empty.
	mock := &mockProvider{name: "github"}
	if err := registry.Register(mock); err != nil {
		t.Fatalf("failed to register mock provider: %v", err)
	}

	g := e.Group("")
	oauth.RegisterOAuthHandlers(g, registry, nil, "")

	// --- GET /auth/providers ---
	t.Run("GET_providers_200_with_cache_control", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/auth/providers", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("GET /auth/providers status = %d, want %d", rec.Code, http.StatusOK)
		}

		cc := rec.Header().Get("Cache-Control")
		if cc != "public, max-age=300" {
			t.Errorf("Cache-Control = %q, want %q", cc, "public, max-age=300")
		}
	})

	// --- POST /auth/callback ---
	t.Run("POST_callback_returns_400_not_404", func(t *testing.T) {
		// Send an empty JSON body to trigger a validation error.
		req := httptest.NewRequest(http.MethodPost, "/auth/callback", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed {
			t.Errorf("POST /auth/callback status = %d, want 400 (route should exist)", rec.Code)
		}
		if rec.Code != http.StatusBadRequest {
			t.Errorf("POST /auth/callback status = %d, want %d (validation error)", rec.Code, http.StatusBadRequest)
		}
	})
}

// ========================================================================
// TS-06-49: Shared http.Client with 30s timeout injected into providers
// (Requirement: 06-REQ-13.2)
// ========================================================================

// TestHandler_SharedHTTPClient verifies that BuildRegistryFromConfig creates
// providers that share the injected *http.Client, and verifies the client
// has the expected 30-second timeout and DefaultTransport.
func TestHandler_SharedHTTPClient(t *testing.T) {
	// Create a shared client matching the spec requirements.
	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: http.DefaultTransport,
	}

	// Build a registry with a GitHub provider using the shared client.
	configs := []oauth.ProviderConfig{
		{
			Name:         "github",
			ClientID:     "test-id",
			ClientSecret: "test-secret",
		},
	}

	registry, err := oauth.BuildRegistryFromConfig(configs, client)
	if err != nil {
		t.Fatalf("BuildRegistryFromConfig() error = %v", err)
	}

	// Verify the client properties.
	if client.Timeout != 30*time.Second {
		t.Errorf("client.Timeout = %v, want %v", client.Timeout, 30*time.Second)
	}
	if client.Transport != http.DefaultTransport {
		t.Errorf("client.Transport = %v, want http.DefaultTransport", client.Transport)
	}

	// Verify the provider uses the shared client.
	provider := registry.Get("github")
	if provider == nil {
		t.Fatal("registry.Get(\"github\") returned nil")
	}

	ghProvider, ok := provider.(*oauth.GitHubProvider)
	if !ok {
		t.Fatal("provider is not *GitHubProvider")
	}

	if ghProvider.HTTPClient() != client {
		t.Errorf("provider HTTPClient = %p, want %p (shared client)", ghProvider.HTTPClient(), client)
	}
}

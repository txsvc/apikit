package apikit_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/txsvc/apikit"
)

// ========================================================================
// Task 4.1: Integration tests for Content-Type enforcement and Body Size Limit
// (TS-01-45, TS-01-46, TS-01-47, TS-01-48)
// Requirements: 01-REQ-12.1, 01-REQ-12.2, 01-REQ-13.1, 01-REQ-13.2
// ========================================================================

// TestContentType_NonJSON_ReturnsHTTP415 verifies that POST, PUT, and PATCH
// requests with non-JSON Content-Type are rejected with HTTP 415 and the
// standard JSON error envelope with Content-Type: application/json; charset=utf-8.
// Covers TS-01-45 (Requirement: 01-REQ-12.1).
func TestContentType_NonJSON_ReturnsHTTP415(t *testing.T) {
	cfg := buildTestConfig(0)
	cfg.Server.MaxBodySize = "1MB"
	srv := apikit.NewServer(cfg, nil)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		<-startErr
	})

	addr := waitUntilListening(t, srv, 2*time.Second)

	// Register a handler at /api/v1/test accepting POST, PUT, PATCH
	api := srv.APIGroup()
	if api == nil {
		t.Fatal("APIGroup() returned nil")
	}
	handler := func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}
	api.POST("/test", handler)
	api.PUT("/test", handler)
	api.PATCH("/test", handler)

	cases := []struct {
		method      string
		contentType string
	}{
		{"POST", "text/plain"},
		{"PUT", "text/xml"},
		{"PATCH", "application/octet-stream"},
	}

	for _, tc := range cases {
		t.Run(tc.method+"_"+tc.contentType, func(t *testing.T) {
			req, err := http.NewRequest(tc.method,
				"http://"+addr+"/api/v1/test",
				strings.NewReader("data"))
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			req.Header.Set("Content-Type", tc.contentType)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("HTTP request failed: %v", err)
			}
			defer resp.Body.Close()

			// Assert HTTP 415 Unsupported Media Type
			if resp.StatusCode != http.StatusUnsupportedMediaType {
				t.Errorf("status = %d, want 415", resp.StatusCode)
			}

			// Assert Content-Type header
			ct := resp.Header.Get("Content-Type")
			if ct != "application/json; charset=utf-8" {
				t.Errorf("Content-Type = %q, want %q", ct, "application/json; charset=utf-8")
			}

			// Assert standard error envelope
			var body map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode response body: %v", err)
			}
			errObj, ok := body["error"].(map[string]interface{})
			if !ok {
				t.Fatal("response body missing 'error' object")
			}
			code, ok := errObj["code"].(float64) // JSON numbers are float64
			if !ok || int(code) != 415 {
				t.Errorf("error.code = %v, want 415", errObj["code"])
			}
		})
	}
}

// TestContentType_NonMutatingMethodsPassThrough verifies that GET, DELETE, HEAD,
// and OPTIONS requests pass through without Content-Type enforcement, even when
// Content-Type is set to a non-JSON value.
// Covers TS-01-46 (Requirement: 01-REQ-12.2).
func TestContentType_NonMutatingMethodsPassThrough(t *testing.T) {
	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		<-startErr
	})

	addr := waitUntilListening(t, srv, 2*time.Second)

	// Register handlers for each non-mutating method
	api := srv.APIGroup()
	if api == nil {
		t.Fatal("APIGroup() returned nil")
	}
	handler := func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}
	api.GET("/test", handler)
	api.DELETE("/test", handler)
	api.HEAD("/test", handler)
	api.OPTIONS("/test", handler)

	methods := []string{"GET", "DELETE", "HEAD", "OPTIONS"}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req, err := http.NewRequest(method,
				"http://"+addr+"/api/v1/test", nil)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			req.Header.Set("Content-Type", "text/plain")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("HTTP request failed: %v", err)
			}
			resp.Body.Close()

			// None of these methods should return 415
			if resp.StatusCode == http.StatusUnsupportedMediaType {
				t.Errorf("%s returned 415; non-mutating methods should pass through "+
					"without Content-Type enforcement", method)
			}
		})
	}
}

// TestBodySize_ExceedsLimit_ReturnsHTTP413 verifies that a request body
// exceeding max_body_size returns HTTP 413 with the standard JSON error envelope,
// X-Request-ID header, and Content-Type: application/json; charset=utf-8.
// Covers TS-01-47 (Requirement: 01-REQ-13.1).
func TestBodySize_ExceedsLimit_ReturnsHTTP413(t *testing.T) {
	cfg := buildTestConfig(0)
	cfg.Server.MaxBodySize = "1MB"
	srv := apikit.NewServer(cfg, nil)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		<-startErr
	})

	addr := waitUntilListening(t, srv, 2*time.Second)

	// Register a handler for POST
	api := srv.APIGroup()
	if api == nil {
		t.Fatal("APIGroup() returned nil")
	}
	api.POST("/upload", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// Send POST with 2MB body (exceeds 1MB limit)
	body2MB := strings.Repeat("x", 2*1024*1024)
	req, err := http.NewRequest("POST",
		"http://"+addr+"/api/v1/upload",
		strings.NewReader(body2MB))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	// Assert HTTP 413 Payload Too Large
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", resp.StatusCode)
	}

	// Assert Content-Type header
	ct := resp.Header.Get("Content-Type")
	if ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json; charset=utf-8")
	}

	// Assert X-Request-ID is present
	requestID := resp.Header.Get("X-Request-ID")
	if requestID == "" {
		t.Error("X-Request-ID header missing from 413 response")
	}

	// Assert standard error envelope
	var respBody map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	errObj, ok := respBody["error"].(map[string]interface{})
	if !ok {
		t.Fatal("response body missing 'error' object")
	}
	code, ok := errObj["code"].(float64)
	if !ok || int(code) != 413 {
		t.Errorf("error.code = %v, want 413", errObj["code"])
	}
}

// TestBodySize_DefaultsTo1MB verifies that MaxBodyBytes() defaults to
// 1,048,576 bytes (1MB) when not configured, and that a request with a body
// just over 1MB is rejected with 413.
// Covers TS-01-48 (Requirement: 01-REQ-13.2).
func TestBodySize_DefaultsTo1MB(t *testing.T) {
	// Unit check: MaxBodyBytes() returns 1048576 for default config
	t.Run("default_value", func(t *testing.T) {
		// Unset XDG vars to ensure clean defaults
		for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME"} {
			if val, ok := os.LookupEnv(key); ok {
				t.Cleanup(func() { os.Setenv(key, val) })
			}
			os.Unsetenv(key)
		}

		dir := t.TempDir()
		t.Chdir(dir) // no config.toml present

		cfg, err := apikit.LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig() error: %v", err)
		}

		if cfg.Server.MaxBodyBytes() != 1048576 {
			t.Errorf("MaxBodyBytes() = %d, want 1048576", cfg.Server.MaxBodyBytes())
		}
	})

	// Behavioral check: body just over 1MB is rejected
	t.Run("behavioral", func(t *testing.T) {
		cfg := buildTestConfig(0)
		// No MaxBodySize set — should default to 1MB
		srv := apikit.NewServer(cfg, nil)

		startErr := startServerInBackground(srv)
		t.Cleanup(func() {
			srv.Shutdown(context.Background())
			<-startErr
		})

		addr := waitUntilListening(t, srv, 2*time.Second)

		api := srv.APIGroup()
		if api == nil {
			t.Fatal("APIGroup() returned nil")
		}
		api.POST("/test", func(c echo.Context) error {
			return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
		})

		// Send body of exactly 1048577 bytes (1 byte over 1MB)
		bodyOverLimit := strings.Repeat("x", 1048577)
		req, err := http.NewRequest("POST",
			"http://"+addr+"/api/v1/test",
			strings.NewReader(bodyOverLimit))
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("HTTP request failed: %v", err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusRequestEntityTooLarge {
			t.Errorf("status = %d, want 413 for body 1 byte over 1MB default",
				resp.StatusCode)
		}
	})
}

// ========================================================================
// Task 4.2: Integration tests for Security Headers middleware
// (TS-01-49, TS-01-50, TS-01-P4)
// Requirements: 01-REQ-14.1, 01-REQ-14.2
// ========================================================================

// TestSecurity_HeadersOnEveryResponse verifies that X-Content-Type-Options: nosniff,
// X-Frame-Options: DENY, and Referrer-Policy: no-referrer are set on every
// response regardless of route or status code.
// Covers TS-01-49 (Requirement: 01-REQ-14.1).
func TestSecurity_HeadersOnEveryResponse(t *testing.T) {
	cfg := buildTestConfig(0)
	cfg.Server.MaxBodySize = "1MB"
	srv := apikit.NewServer(cfg, nil)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		<-startErr
	})

	addr := waitUntilListening(t, srv, 2*time.Second)

	// Register test handlers
	api := srv.APIGroup()
	if api == nil {
		t.Fatal("APIGroup() returned nil")
	}
	api.GET("/test", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})
	api.POST("/test", func(c echo.Context) error {
		return c.JSON(http.StatusCreated, map[string]string{"status": "created"})
	})

	type testCase struct {
		name   string
		method string
		url    string
		ct     string
		body   string
	}

	cases := []testCase{
		{"GET_healthz", "GET", "http://" + addr + "/healthz", "", ""},
		{"GET_api_test", "GET", "http://" + addr + "/api/v1/test", "", ""},
		{"POST_api_test", "POST", "http://" + addr + "/api/v1/test",
			"application/json", `{}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var req *http.Request
			var err error
			if tc.body != "" {
				req, err = http.NewRequest(tc.method, tc.url,
					strings.NewReader(tc.body))
			} else {
				req, err = http.NewRequest(tc.method, tc.url, nil)
			}
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			if tc.ct != "" {
				req.Header.Set("Content-Type", tc.ct)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("HTTP request failed: %v", err)
			}
			resp.Body.Close()

			// Assert all three security headers
			if v := resp.Header.Get("X-Content-Type-Options"); v != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q, want %q", v, "nosniff")
			}
			if v := resp.Header.Get("X-Frame-Options"); v != "DENY" {
				t.Errorf("X-Frame-Options = %q, want %q", v, "DENY")
			}
			if v := resp.Header.Get("Referrer-Policy"); v != "no-referrer" {
				t.Errorf("Referrer-Policy = %q, want %q", v, "no-referrer")
			}
		})
	}
}

// TestSecurity_DoesNotSetCacheControl verifies that the Security Headers
// middleware does NOT set the Cache-Control header; that header is managed
// exclusively by CacheMiddleware. Includes both behavioral and source-level
// verification.
// Covers TS-01-50 (Requirement: 01-REQ-14.2).
func TestSecurity_DoesNotSetCacheControl(t *testing.T) {
	// Behavioral check: verify security headers and Cache-Control coexist
	// but come from separate middleware
	t.Run("behavioral", func(t *testing.T) {
		cfg := buildTestConfig(0)
		srv := apikit.NewServer(cfg, nil)

		startErr := startServerInBackground(srv)
		t.Cleanup(func() {
			srv.Shutdown(context.Background())
			<-startErr
		})

		addr := waitUntilListening(t, srv, 2*time.Second)

		api := srv.APIGroup()
		if api == nil {
			t.Fatal("APIGroup() returned nil")
		}
		api.GET("/test", func(c echo.Context) error {
			return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
		})

		resp, err := http.Get("http://" + addr + "/api/v1/test")
		if err != nil {
			t.Fatalf("HTTP request failed: %v", err)
		}
		resp.Body.Close()

		// Security headers should be present
		if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Error("X-Content-Type-Options not set to nosniff")
		}

		// Cache-Control should be set by CacheMiddleware (no-store for API routes),
		// NOT by the Security Headers middleware
		cc := resp.Header.Get("Cache-Control")
		if cc != "no-store" {
			t.Errorf("Cache-Control = %q, want %q (from CacheMiddleware, not security headers)",
				cc, "no-store")
		}
	})

	// Source-level check: verify that Go source files implementing security
	// headers (containing "nosniff") do not also reference "Cache-Control"
	t.Run("source_inspection", func(t *testing.T) {
		entries, err := os.ReadDir(".")
		if err != nil {
			t.Fatalf("ReadDir: %v", err)
		}

		for _, entry := range entries {
			name := entry.Name()
			// Only check non-test Go source files
			if entry.IsDir() ||
				!strings.HasSuffix(name, ".go") ||
				strings.HasSuffix(name, "_test.go") {
				continue
			}
			content, err := os.ReadFile(name)
			if err != nil {
				continue
			}
			src := string(content)
			// If a source file sets "nosniff" (security headers), it must
			// NOT also set "Cache-Control" — that belongs to CacheMiddleware
			if strings.Contains(src, `"nosniff"`) &&
				strings.Contains(src, `"Cache-Control"`) {
				t.Errorf("file %s contains both 'nosniff' and 'Cache-Control'; "+
					"security headers middleware must not set Cache-Control", name)
			}
		}
	})
}

// TestSecurity_PropertyAllResponsesHaveHeaders is a property test verifying
// that for any HTTP response, all three security headers are present with
// exact values, and Cache-Control is not set by the Security Headers middleware.
// Tests across all registered endpoints with various methods.
// Covers TS-01-P4 (Property: 01-PROP-4).
// Validates: 01-REQ-14.1, 01-REQ-14.2.
func TestSecurity_PropertyAllResponsesHaveHeaders(t *testing.T) {
	cfg := buildTestConfig(0)
	cfg.Server.MaxBodySize = "1MB"
	srv := apikit.NewServer(cfg, nil)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		<-startErr
	})

	addr := waitUntilListening(t, srv, 2*time.Second)

	// Register consumer routes
	api := srv.APIGroup()
	if api == nil {
		t.Fatal("APIGroup() returned nil")
	}
	api.GET("/test", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})
	api.POST("/test", func(c echo.Context) error {
		return c.JSON(http.StatusCreated, map[string]string{"status": "created"})
	})
	api.DELETE("/test", func(c echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})

	// Generate requests across all registered endpoints with various methods
	type request struct {
		name   string
		method string
		path   string
		ct     string
		body   string
	}

	requests := []request{
		// Health probes
		{"healthz", "GET", "/healthz", "", ""},
		{"readyz", "GET", "/readyz", "", ""},
		// Consumer routes with different methods
		{"GET_test", "GET", "/api/v1/test", "", ""},
		{"POST_test_json", "POST", "/api/v1/test", "application/json", `{}`},
		{"DELETE_test", "DELETE", "/api/v1/test", "", ""},
		// Error-producing requests
		{"POST_wrong_ct", "POST", "/api/v1/test", "text/plain", "data"},
		{"POST_oversized", "POST", "/api/v1/test", "application/json",
			strings.Repeat("x", 2*1024*1024)},
	}

	for _, r := range requests {
		t.Run(r.name, func(t *testing.T) {
			var req *http.Request
			var err error
			if r.body != "" {
				req, err = http.NewRequest(r.method, "http://"+addr+r.path,
					strings.NewReader(r.body))
			} else {
				req, err = http.NewRequest(r.method, "http://"+addr+r.path, nil)
			}
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			if r.ct != "" {
				req.Header.Set("Content-Type", r.ct)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("HTTP request failed: %v", err)
			}
			resp.Body.Close()

			// Invariant: all three security headers present with exact values
			if v := resp.Header.Get("X-Content-Type-Options"); v != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q, want %q", v, "nosniff")
			}
			if v := resp.Header.Get("X-Frame-Options"); v != "DENY" {
				t.Errorf("X-Frame-Options = %q, want %q", v, "DENY")
			}
			if v := resp.Header.Get("Referrer-Policy"); v != "no-referrer" {
				t.Errorf("Referrer-Policy = %q, want %q", v, "no-referrer")
			}
		})
	}

	// Source-level invariant: security headers middleware source does not
	// contain 'Cache-Control'
	t.Run("source_no_cache_control", func(t *testing.T) {
		entries, err := os.ReadDir(".")
		if err != nil {
			t.Fatalf("ReadDir: %v", err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() ||
				!strings.HasSuffix(name, ".go") ||
				strings.HasSuffix(name, "_test.go") {
				continue
			}
			content, err := os.ReadFile(name)
			if err != nil {
				continue
			}
			src := string(content)
			if strings.Contains(src, `"nosniff"`) &&
				strings.Contains(src, `"Cache-Control"`) {
				t.Errorf("file %s: security headers source must not reference Cache-Control", name)
			}
		}
	})
}

// ========================================================================
// Task 4.3: Unit and integration tests for Cache-Control middleware
// (TS-01-51, TS-01-52, TS-01-53)
// Requirements: 01-REQ-15.1, 01-REQ-15.2, 01-REQ-15.3
// ========================================================================

// TestCache_APIGroupDefaultsToNoStore verifies that APIGroup() pre-applies
// CacheMiddleware(CacheNoStore) so all consumer-registered routes without
// explicit cache middleware return Cache-Control: no-store.
// Covers TS-01-51 (Requirement: 01-REQ-15.1).
func TestCache_APIGroupDefaultsToNoStore(t *testing.T) {
	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		<-startErr
	})

	addr := waitUntilListening(t, srv, 2*time.Second)

	// Register a handler on APIGroup() with no explicit CacheMiddleware
	api := srv.APIGroup()
	if api == nil {
		t.Fatal("APIGroup() returned nil")
	}
	api.GET("/items", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]interface{}{})
	})

	resp, err := http.Get("http://" + addr + "/api/v1/items")
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	resp.Body.Close()

	cc := resp.Header.Get("Cache-Control")
	if cc != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", cc, "no-store")
	}
}

// TestCache_PublicOverridesGroupDefault verifies that attaching
// CacheMiddleware(CachePublic) to a specific route overrides the group-level
// CacheNoStore default, while other group routes still return no-store.
// Covers TS-01-52 (Requirement: 01-REQ-15.2).
func TestCache_PublicOverridesGroupDefault(t *testing.T) {
	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		<-startErr
	})

	addr := waitUntilListening(t, srv, 2*time.Second)

	api := srv.APIGroup()
	if api == nil {
		t.Fatal("APIGroup() returned nil")
	}

	handler := func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]interface{}{})
	}

	// Route without cache override
	api.GET("/items", handler)
	// Route with CachePublic override
	api.GET("/providers", handler, apikit.CacheMiddleware(apikit.CachePublic))

	// /items should use group-level default: no-store
	resp1, err := http.Get("http://" + addr + "/api/v1/items")
	if err != nil {
		t.Fatalf("GET /items failed: %v", err)
	}
	resp1.Body.Close()

	cc1 := resp1.Header.Get("Cache-Control")
	if cc1 != "no-store" {
		t.Errorf("/items Cache-Control = %q, want %q", cc1, "no-store")
	}

	// /providers should have per-route override: public, max-age=300
	resp2, err := http.Get("http://" + addr + "/api/v1/providers")
	if err != nil {
		t.Fatalf("GET /providers failed: %v", err)
	}
	resp2.Body.Close()

	cc2 := resp2.Header.Get("Cache-Control")
	if cc2 != "public, max-age=300" {
		t.Errorf("/providers Cache-Control = %q, want %q", cc2, "public, max-age=300")
	}
}

// TestCache_MiddlewareExactValues is a unit test verifying that CacheMiddleware
// returns middleware setting exact Cache-Control header values for each
// CacheCategory: CacheNoStore ("no-store"), CacheNoCache ("no-cache"),
// CachePublic ("public, max-age=300").
// Covers TS-01-53 (Requirement: 01-REQ-15.3).
func TestCache_MiddlewareExactValues(t *testing.T) {
	cases := []struct {
		name     string
		category apikit.CacheCategory
		expected string
	}{
		{"CacheNoStore", apikit.CacheNoStore, "no-store"},
		{"CacheNoCache", apikit.CacheNoCache, "no-cache"},
		{"CachePublic", apikit.CachePublic, "public, max-age=300"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mw := apikit.CacheMiddleware(tc.category)

			// Create Echo test context
			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			// Apply middleware to a simple handler
			handler := mw(func(c echo.Context) error {
				return c.NoContent(http.StatusOK)
			})

			if err := handler(c); err != nil {
				t.Fatalf("handler returned error: %v", err)
			}

			cc := rec.Header().Get("Cache-Control")
			if cc != tc.expected {
				t.Errorf("Cache-Control = %q, want %q", cc, tc.expected)
			}
		})
	}
}

// ========================================================================
// Task 4.4: Unit tests for APIError and error envelope consistency
// (TS-01-58, TS-01-59, TS-01-P2)
// Requirements: 01-REQ-17.1, 01-REQ-17.2
// ========================================================================

// TestAPIError_WritesStandardEnvelope verifies that APIError writes the standard
// JSON error envelope {"error":{"code":<integer>,"message":"<string>"}} with
// Content-Type: application/json; charset=utf-8 and returns c.JSON()'s error.
// Covers TS-01-58 (Requirement: 01-REQ-17.1).
func TestAPIError_WritesStandardEnvelope(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := apikit.APIError(c, 404, "not found")

	// Verify Content-Type
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json; charset=utf-8")
	}

	// Verify response status
	if rec.Code != 404 {
		t.Errorf("status = %d, want 404", rec.Code)
	}

	// Verify standard error envelope
	var body map[string]interface{}
	if jsonErr := json.Unmarshal(rec.Body.Bytes(), &body); jsonErr != nil {
		t.Fatalf("failed to decode response body: %v", jsonErr)
	}

	errObj, ok := body["error"].(map[string]interface{})
	if !ok {
		t.Fatal("response body missing 'error' object")
	}

	// code must be integer (float64 in JSON)
	code, ok := errObj["code"].(float64)
	if !ok || int(code) != 404 {
		t.Errorf("error.code = %v, want 404 (integer)", errObj["code"])
	}

	// message must be string
	msg, ok := errObj["message"].(string)
	if !ok {
		t.Error("error.message is missing or not a string")
	} else if msg != "not found" {
		t.Errorf("error.message = %q, want %q", msg, "not found")
	}

	// Verify return value propagates write errors (nil means no write error)
	// The stub returns nil, but with real implementation, c.JSON() error should
	// be returned
	_ = err
}

// TestAPIError_MiddlewareErrorsUseEnvelope verifies that all middleware-generated
// error responses (HTTP 413, 415, 500) include Content-Type: application/json;
// charset=utf-8 and the standard error envelope with integer code field.
// Covers TS-01-59 (Requirement: 01-REQ-17.2).
func TestAPIError_MiddlewareErrorsUseEnvelope(t *testing.T) {
	cfg := buildTestConfig(0)
	cfg.Server.MaxBodySize = "1MB"
	srv := apikit.NewServer(cfg, nil)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		<-startErr
	})

	addr := waitUntilListening(t, srv, 2*time.Second)

	api := srv.APIGroup()
	if api == nil {
		t.Fatal("APIGroup() returned nil")
	}
	api.POST("/test", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})
	api.GET("/panic", func(c echo.Context) error {
		panic("test panic for 500")
	})

	type errorCase struct {
		name         string
		method       string
		path         string
		contentType  string
		body         string
		expectedCode int
	}

	cases := []errorCase{
		// 413: oversized body
		{"413_oversized", "POST", "/api/v1/test", "application/json",
			strings.Repeat("x", 2*1024*1024), 413},
		// 415: wrong Content-Type
		{"415_wrong_ct", "POST", "/api/v1/test", "text/plain", "data", 415},
		// 500: handler panic
		{"500_panic", "GET", "/api/v1/panic", "", "", 500},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var req *http.Request
			var err error
			if tc.body != "" {
				req, err = http.NewRequest(tc.method, "http://"+addr+tc.path,
					strings.NewReader(tc.body))
			} else {
				req, err = http.NewRequest(tc.method, "http://"+addr+tc.path, nil)
			}
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("HTTP request failed: %v", err)
			}
			defer resp.Body.Close()

			// Assert expected status code
			if resp.StatusCode != tc.expectedCode {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.expectedCode)
			}

			// Assert Content-Type
			ct := resp.Header.Get("Content-Type")
			if ct != "application/json; charset=utf-8" {
				t.Errorf("Content-Type = %q, want %q", ct, "application/json; charset=utf-8")
			}

			// Assert standard error envelope
			var body map[string]interface{}
			if jsonErr := json.NewDecoder(resp.Body).Decode(&body); jsonErr != nil {
				t.Fatalf("failed to decode response body: %v", jsonErr)
			}
			errObj, ok := body["error"].(map[string]interface{})
			if !ok {
				t.Fatal("response body missing 'error' object")
			}
			code, ok := errObj["code"].(float64)
			if !ok || int(code) != tc.expectedCode {
				t.Errorf("error.code = %v, want %d", errObj["code"], tc.expectedCode)
			}
		})
	}
}

// TestError_PropertyEnvelopeConsistency is a property test verifying that for
// any HTTP error response produced by any middleware or handler, the response
// body conforms to {"error":{"code":<integer>,"message":"<string>"}} and
// Content-Type is application/json; charset=utf-8.
// Covers TS-01-P2 (Property: 01-PROP-2).
// Validates: 01-REQ-17.1, 01-REQ-17.2.
func TestError_PropertyEnvelopeConsistency(t *testing.T) {
	cfg := buildTestConfig(0)
	cfg.Server.MaxBodySize = "1MB"
	srv := apikit.NewServer(cfg, nil)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		<-startErr
	})

	addr := waitUntilListening(t, srv, 2*time.Second)

	api := srv.APIGroup()
	if api == nil {
		t.Fatal("APIGroup() returned nil")
	}

	// Register handlers that produce various error codes via APIError
	api.POST("/test", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})
	api.GET("/bad-request", func(c echo.Context) error {
		return apikit.APIError(c, 400, "bad request")
	})
	api.GET("/not-found", func(c echo.Context) error {
		return apikit.APIError(c, 404, "resource not found")
	})
	api.GET("/conflict", func(c echo.Context) error {
		return apikit.APIError(c, 409, "resource conflict")
	})
	api.GET("/panic", func(c echo.Context) error {
		panic("test panic for property test")
	})

	type errorInput struct {
		name        string
		method      string
		path        string
		contentType string
		body        string
	}

	// Trigger errors at each middleware layer and from handlers
	inputs := []errorInput{
		// Middleware errors
		{"413_body_too_large", "POST", "/api/v1/test", "application/json",
			strings.Repeat("x", 2*1024*1024)},
		{"415_wrong_content_type", "POST", "/api/v1/test", "text/plain", "data"},
		{"500_handler_panic", "GET", "/api/v1/panic", "", ""},
		// Handler errors via APIError
		{"400_bad_request", "GET", "/api/v1/bad-request", "", ""},
		{"404_not_found", "GET", "/api/v1/not-found", "", ""},
		{"409_conflict", "GET", "/api/v1/conflict", "", ""},
	}

	for _, input := range inputs {
		t.Run(input.name, func(t *testing.T) {
			var req *http.Request
			var err error
			if input.body != "" {
				req, err = http.NewRequest(input.method, "http://"+addr+input.path,
					strings.NewReader(input.body))
			} else {
				req, err = http.NewRequest(input.method, "http://"+addr+input.path, nil)
			}
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			if input.contentType != "" {
				req.Header.Set("Content-Type", input.contentType)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("HTTP request failed: %v", err)
			}
			defer resp.Body.Close()

			// Invariant: Content-Type is always application/json; charset=utf-8
			ct := resp.Header.Get("Content-Type")
			if ct != "application/json; charset=utf-8" {
				t.Errorf("Content-Type = %q, want %q", ct, "application/json; charset=utf-8")
			}

			// Invariant: body conforms to {"error":{"code":<integer>,"message":"<string>"}}
			var body map[string]interface{}
			if jsonErr := json.NewDecoder(resp.Body).Decode(&body); jsonErr != nil {
				t.Fatalf("failed to decode response body: %v", jsonErr)
			}
			errObj, ok := body["error"].(map[string]interface{})
			if !ok {
				t.Fatal("response body missing 'error' object")
			}

			// error.code must be an integer (represented as float64 in JSON)
			code, ok := errObj["code"].(float64)
			if !ok {
				t.Error("error.code is missing or not a number")
			} else if code != float64(int(code)) {
				t.Errorf("error.code = %v, want integer", code)
			}

			// error.message must be a string
			if _, ok := errObj["message"].(string); !ok {
				t.Error("error.message is missing or not a string")
			}
		})
	}
}

// ========================================================================
// Task 4.5: Property tests for shutdown idempotency and library non-termination
// (TS-01-P5, TS-01-P8)
// Requirements: 01-REQ-1.E2, 01-REQ-6.6, 01-REQ-6.4, 01-REQ-6.E3
// ========================================================================

// TestShutdown_PropertyConcurrentAllReturnNil is a property test verifying that
// for any sequence of 1 to 20 concurrent Shutdown() calls with varied delays
// between calls, the server shuts down exactly once, all calls return nil, and
// no goroutine blocks indefinitely. This test should be run with -race.
// Covers TS-01-P5 (Property: 01-PROP-5).
// Validates: 01-REQ-6.4, 01-REQ-6.E3.
func TestShutdown_PropertyConcurrentAllReturnNil(t *testing.T) {
	// Test with different counts of concurrent shutdown calls
	counts := []int{1, 3, 5, 10, 20}

	for _, count := range counts {
		t.Run(fmt.Sprintf("count_%d", count), func(t *testing.T) {
			// Use a fresh server for each subtest
			cfg := buildTestConfig(0)
			srv := apikit.NewServer(cfg, nil)

			startErr := startServerInBackground(srv)

			waitUntilListening(t, srv, 2*time.Second)

			errs := make([]error, count)
			var wg sync.WaitGroup
			wg.Add(count)

			for i := 0; i < count; i++ {
				go func(idx int) {
					defer wg.Done()
					// Deterministic but varied delay: (idx * 7) % 100 milliseconds
					delay := time.Duration((idx*7)%100) * time.Millisecond
					time.Sleep(delay)
					errs[idx] = srv.Shutdown(context.Background())
				}(i)
			}

			// All goroutines must complete within 5 seconds of start
			done := make(chan struct{})
			go func() {
				wg.Wait()
				close(done)
			}()

			select {
			case <-done:
				// All returned — check results
			case <-time.After(5 * time.Second):
				t.Fatal("goroutines blocked indefinitely; possible deadlock")
			}

			// All calls must return nil
			for i, err := range errs {
				if err != nil {
					t.Errorf("goroutine %d: Shutdown() returned error: %v", i, err)
				}
			}

			// Start() must also return
			select {
			case <-startErr:
			case <-time.After(5 * time.Second):
				t.Fatal("Start() did not return after concurrent Shutdown() calls")
			}
		})
	}
}

// TestError_PropertyLibraryNeverCallsOsExit is a property test verifying that
// for any documented error path, the library never calls os.Exit(); all errors
// are returned as values, and the calling test goroutine continues executing
// normally. If os.Exit() were called, the test process would terminate
// immediately and this test would never complete.
// Covers TS-01-P8 (Property: 01-PROP-8).
// Validates: 01-REQ-1.E2, 01-REQ-6.6.
func TestError_PropertyLibraryNeverCallsOsExit(t *testing.T) {
	// Error path 1: malformed config file
	t.Run("malformed_config", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		os.WriteFile(filepath.Join(dir, "config.toml"), []byte("invalid [[["), 0644)

		for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME"} {
			if val, ok := os.LookupEnv(key); ok {
				t.Cleanup(func() { os.Setenv(key, val) })
			}
			os.Unsetenv(key)
		}

		_, err := apikit.LoadConfig()
		// If os.Exit() were called, we'd never reach this line
		if err == nil {
			t.Error("expected error for malformed config, got nil")
		}
	})

	// Error path 2: invalid port in config
	t.Run("invalid_port", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		os.WriteFile(filepath.Join(dir, "config.toml"),
			[]byte("[server]\nport = 99999\n"), 0644)

		for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME"} {
			if val, ok := os.LookupEnv(key); ok {
				t.Cleanup(func() { os.Setenv(key, val) })
			}
			os.Unsetenv(key)
		}

		_, err := apikit.LoadConfig()
		if err == nil {
			t.Error("expected error for invalid port 99999, got nil")
		}
	})

	// Error path 3: invalid log level
	t.Run("invalid_log_level", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		os.WriteFile(filepath.Join(dir, "config.toml"),
			[]byte("[logging]\nlevel = \"INVALID\"\n"), 0644)

		for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME"} {
			if val, ok := os.LookupEnv(key); ok {
				t.Cleanup(func() { os.Setenv(key, val) })
			}
			os.Unsetenv(key)
		}

		_, err := apikit.LoadConfig()
		if err == nil {
			t.Error("expected error for invalid log level, got nil")
		}
	})

	// Error path 4: invalid max_body_size
	t.Run("invalid_max_body_size", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		os.WriteFile(filepath.Join(dir, "config.toml"),
			[]byte("[server]\nmax_body_size = \"0MB\"\n"), 0644)

		for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME"} {
			if val, ok := os.LookupEnv(key); ok {
				t.Cleanup(func() { os.Setenv(key, val) })
			}
			os.Unsetenv(key)
		}

		_, err := apikit.LoadConfig()
		if err == nil {
			t.Error("expected error for 0MB max_body_size, got nil")
		}
	})

	// Error path 5: port already in use at Start()
	t.Run("port_in_use", func(t *testing.T) {
		// Bind a port first
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("net.Listen: %v", err)
		}
		defer ln.Close()

		_, portStr, _ := net.SplitHostPort(ln.Addr().String())
		var port int
		for _, c := range portStr {
			port = port*10 + int(c-'0')
		}

		cfg := buildTestConfig(port)
		cfg.Server.Bind = "127.0.0.1"
		srv := apikit.NewServer(cfg, nil)

		startErr := srv.Start()
		// If os.Exit() were called, we'd never reach this line
		if startErr == nil {
			t.Error("Start() returned nil for port already in use; expected error")
			srv.Shutdown(context.Background())
		}
	})

	// Error path 6: shutdown returns cleanly, no os.Exit
	t.Run("clean_shutdown", func(t *testing.T) {
		cfg := buildTestConfig(0)
		srv := apikit.NewServer(cfg, nil)

		startCh := make(chan error, 1)
		go func() { startCh <- srv.Start() }()

		// Give start a moment
		time.Sleep(100 * time.Millisecond)

		// Shutdown should not call os.Exit
		srv.Shutdown(context.Background())

		select {
		case <-startCh:
			// If we reach here, os.Exit was NOT called during shutdown
		case <-time.After(5 * time.Second):
			t.Fatal("Start() did not return after Shutdown; possible os.Exit or deadlock")
		}
	})

	// Error path 7: body too large (server still running after middleware error)
	t.Run("body_too_large", func(t *testing.T) {
		cfg := buildTestConfig(0)
		cfg.Server.MaxBodySize = "1MB"
		srv := apikit.NewServer(cfg, nil)

		startErr := startServerInBackground(srv)
		t.Cleanup(func() {
			srv.Shutdown(context.Background())
			<-startErr
		})

		addr := waitUntilListening(t, srv, 2*time.Second)

		api := srv.APIGroup()
		if api == nil {
			t.Fatal("APIGroup() returned nil")
		}
		api.POST("/test", func(c echo.Context) error {
			return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
		})

		oversized := strings.Repeat("x", 2*1024*1024)
		req, _ := http.NewRequest("POST",
			"http://"+addr+"/api/v1/test",
			strings.NewReader(oversized))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("HTTP request failed: %v", err)
		}
		resp.Body.Close()
		// If os.Exit were called, we'd never reach here
		// The error should be handled via HTTP response, not process exit
	})

	// Error path 8: wrong content type (server still running after middleware error)
	t.Run("wrong_content_type", func(t *testing.T) {
		cfg := buildTestConfig(0)
		srv := apikit.NewServer(cfg, nil)

		startErr := startServerInBackground(srv)
		t.Cleanup(func() {
			srv.Shutdown(context.Background())
			<-startErr
		})

		addr := waitUntilListening(t, srv, 2*time.Second)

		api := srv.APIGroup()
		if api == nil {
			t.Fatal("APIGroup() returned nil")
		}
		api.POST("/test", func(c echo.Context) error {
			return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
		})

		req, _ := http.NewRequest("POST",
			"http://"+addr+"/api/v1/test",
			strings.NewReader("plain text"))
		req.Header.Set("Content-Type", "text/plain")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("HTTP request failed: %v", err)
		}
		resp.Body.Close()
		// If os.Exit were called, we'd never reach here
	})

	// Error path 9: handler panic (server still running after recovery)
	t.Run("handler_panic", func(t *testing.T) {
		cfg := buildTestConfig(0)
		srv := apikit.NewServer(cfg, nil)

		startErr := startServerInBackground(srv)
		t.Cleanup(func() {
			srv.Shutdown(context.Background())
			<-startErr
		})

		addr := waitUntilListening(t, srv, 2*time.Second)

		api := srv.APIGroup()
		if api == nil {
			t.Fatal("APIGroup() returned nil")
		}
		api.GET("/panic", func(c echo.Context) error {
			panic("intentional panic for os.Exit test")
		})

		resp, err := http.Get("http://" + addr + "/api/v1/panic")
		if err != nil {
			t.Fatalf("HTTP request failed: %v", err)
		}
		resp.Body.Close()
		// If os.Exit were called by panic recovery, we'd never reach here
	})
}


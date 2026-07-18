package apikit_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"

	"github.com/txsvc/apikit"
)

// ========================================================================
// Smoke Tests: TS-01-SMOKE-1 through TS-01-SMOKE-8
//
// These are end-to-end integration tests exercising full execution paths
// with real components (no mocks except for injected HealthChecker).
// Each test maps to a smoke test entry in the test specification.
// ========================================================================

// TestSmoke_ZeroConfigStartup verifies that a server started without
// config.toml binds on defaults and serves health probe requests.
// Execution Path: 01-PATH-1
// Covers: TS-01-SMOKE-1
// Requirements: 01-REQ-1.1, 01-REQ-1.2, 01-REQ-1.3, 01-REQ-1.5
func TestSmoke_ZeroConfigStartup(t *testing.T) {
	// Use a temp dir with no config.toml — LoadConfig returns defaults
	dir := t.TempDir()
	t.Chdir(dir)

	cfg, err := apikit.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() returned error: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadConfig() returned nil config")
	}

	// Override port to 0 for ephemeral binding (avoid conflict with port 8080)
	cfg.Server.Port = 0

	srv := apikit.NewServer(cfg, nil)
	if srv == nil {
		t.Fatal("NewServer() returned nil")
	}

	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		<-startErr
	})

	addr := waitUntilListening(t, srv, 2*time.Second)

	// GET /healthz returns 200 {"status":"ok"}
	resp, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("body = %v, want {\"status\": \"ok\"}", body)
	}
}

// TestSmoke_ReadinessProbeWithHealthChecker verifies the Kubernetes readiness
// probe with an injected HealthChecker function. Returns 200 when healthy,
// 503 when unhealthy.
// Execution Path: 01-PATH-2
// Covers: TS-01-SMOKE-2
// Requirements: 01-REQ-5.2, 01-REQ-5.3
func TestSmoke_ReadinessProbeWithHealthChecker(t *testing.T) {
	cfg := buildTestConfig(0)

	// Controllable health checker simulating database ping
	healthy := true
	checker := func() error {
		if !healthy {
			return errors.New("database unreachable")
		}
		return nil
	}

	srv := apikit.NewServer(cfg, checker)
	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		<-startErr
	})

	addr := waitUntilListening(t, srv, 2*time.Second)

	// Test 1: healthy → 200 {"status":"ready"} with Cache-Control: no-cache
	t.Run("healthy", func(t *testing.T) {
		healthy = true
		resp, err := http.Get("http://" + addr + "/readyz")
		if err != nil {
			t.Fatalf("GET /readyz failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}

		var body map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body["status"] != "ready" {
			t.Errorf("body = %v, want {\"status\": \"ready\"}", body)
		}

		if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("Cache-Control = %q, want %q", cc, "no-cache")
		}
	})

	// Test 2: unhealthy → 503 {"status":"not ready"}
	t.Run("unhealthy", func(t *testing.T) {
		healthy = false
		resp, err := http.Get("http://" + addr + "/readyz")
		if err != nil {
			t.Fatalf("GET /readyz failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503", resp.StatusCode)
		}

		var body map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body["status"] != "not ready" {
			t.Errorf("body = %v, want {\"status\": \"not ready\"}", body)
		}
	})
}

// TestSmoke_OversizedBodyRejection verifies that a POST with a body exceeding
// max_body_size returns 413 with X-Request-ID and standard JSON error envelope.
// Execution Path: 01-PATH-3
// Covers: TS-01-SMOKE-3
// Requirements: 01-REQ-8.2, 01-REQ-13.1, 01-REQ-17.1
func TestSmoke_OversizedBodyRejection(t *testing.T) {
	cfg := buildTestConfig(0)
	cfg.Server.MaxBodySize = "1MB"
	srv := apikit.NewServer(cfg, nil)

	logs := startLogCapture(t, logrus.DebugLevel)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		<-startErr
	})

	addr := waitUntilListening(t, srv, 2*time.Second)

	// Register a test handler under the API group
	api := srv.APIGroup()
	api.POST("/test", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// POST 2MB body to trigger 413
	oversizedBody := strings.Repeat("x", 2*1024*1024)
	req, err := http.NewRequest("POST", "http://"+addr+"/api/v1/test",
		strings.NewReader(oversizedBody))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	// Assert HTTP 413
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", resp.StatusCode)
	}

	// Assert X-Request-ID present with valid UUID v4
	requestID := resp.Header.Get("X-Request-ID")
	if !isValidUUIDv4(requestID) {
		t.Errorf("X-Request-ID = %q, want valid UUID v4", requestID)
	}

	// Assert Content-Type
	ct := resp.Header.Get("Content-Type")
	if ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json; charset=utf-8")
	}

	// Assert standard error envelope
	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	errObj, ok := body["error"].(map[string]interface{})
	if !ok {
		t.Fatal("response body missing 'error' object")
	}
	code, ok := errObj["code"].(float64)
	if !ok || int(code) != 413 {
		t.Errorf("error.code = %v, want 413", errObj["code"])
	}

	// Assert log entry has status 413
	time.Sleep(50 * time.Millisecond)
	logEntry := logs.findByRequestID(requestID)
	if logEntry != nil {
		if logStatus, ok := logEntry["status"].(float64); ok && int(logStatus) != 413 {
			t.Errorf("log status = %d, want 413", int(logStatus))
		}
	}
}

// TestSmoke_GracefulShutdown verifies graceful shutdown via Shutdown(): the
// server stops accepting new connections and Start() returns nil.
// Execution Path: 01-PATH-4
// Covers: TS-01-SMOKE-4
// Requirements: 01-REQ-6.1, 01-REQ-6.5, 01-REQ-6.6
func TestSmoke_GracefulShutdown(t *testing.T) {
	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	logs := startLogCapture(t, logrus.InfoLevel)

	startErr := startServerInBackground(srv)
	addr := waitUntilListening(t, srv, 2*time.Second)

	// Verify server is accepting requests
	resp, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatalf("pre-shutdown GET /healthz failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("pre-shutdown status = %d, want 200", resp.StatusCode)
	}

	// Trigger shutdown
	srv.Shutdown(context.Background())

	// Wait for Start() to return
	select {
	case err := <-startErr:
		if err != nil {
			t.Errorf("Start() returned non-nil error: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Start() did not return within 20 seconds")
	}

	// Verify shutdown was logged at info level with drain timeout
	time.Sleep(50 * time.Millisecond)
	logOutput := ""
	for _, entry := range logs.entries() {
		level, _ := entry["level"].(string)
		msg, _ := entry["msg"].(string)
		if level == "info" && strings.Contains(strings.ToLower(msg), "shutdown") {
			logOutput = msg
			break
		}
	}
	if logOutput == "" {
		t.Error("no info-level shutdown log entry found")
	}
}

// TestSmoke_EphemeralPortIntegration verifies the integration test pattern:
// server binds to port 0, Addr() returns the actual port, requests succeed,
// and Shutdown() cleanly stops the server.
// Execution Path: 01-PATH-5
// Covers: TS-01-SMOKE-5
// Requirements: 01-REQ-7.1, 01-REQ-7.2
func TestSmoke_EphemeralPortIntegration(t *testing.T) {
	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	startErr := startServerInBackground(srv)

	// Addr() returns non-empty string after binding
	addr := waitUntilListening(t, srv, 2*time.Second)
	if addr == "" {
		t.Fatal("Addr() returned empty string")
	}

	// GET /healthz succeeds via Addr()-constructed URL
	resp, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	// Shutdown stops server; Start() returns nil
	srv.Shutdown(context.Background())

	select {
	case err := <-startErr:
		if err != nil {
			t.Errorf("Start() returned non-nil error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start() did not return within 5 seconds")
	}
}

// TestSmoke_PanicRecovery verifies that a panicking handler returns HTTP 500
// via APIError(), the error log includes request_id, panic, and stack_trace
// fields, and the server continues serving subsequent requests.
// Execution Path: 01-PATH-6
// Covers: TS-01-SMOKE-6
// Requirements: 01-REQ-9.1, 01-REQ-9.E1
func TestSmoke_PanicRecovery(t *testing.T) {
	cfg := buildTestConfig(0)
	cfg.Logging.Level = "debug"
	srv := apikit.NewServer(cfg, nil)

	logs := startLogCapture(t, logrus.DebugLevel)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		<-startErr
	})

	addr := waitUntilListening(t, srv, 2*time.Second)

	// Register a panicking handler
	api := srv.APIGroup()
	api.GET("/panic", func(c echo.Context) error {
		panic("test panic value")
	})

	// Trigger panic
	resp, err := http.Get("http://" + addr + "/api/v1/panic")
	if err != nil {
		t.Fatalf("GET /api/v1/panic failed: %v", err)
	}
	defer resp.Body.Close()

	// Assert HTTP 500
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}

	// Assert Content-Type: application/json; charset=utf-8
	ct := resp.Header.Get("Content-Type")
	if ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json; charset=utf-8")
	}

	// Assert standard error envelope
	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["error"]; !ok {
		t.Error("response body missing 'error' key")
	}

	// Assert error log has required fields
	time.Sleep(50 * time.Millisecond)
	errorLogs := logs.findByLevel("error")
	found := false
	for _, entry := range errorLogs {
		if _, hasPanic := entry["panic"]; hasPanic {
			found = true
			if _, ok := entry["request_id"]; !ok {
				t.Error("error log missing 'request_id'")
			}
			if _, ok := entry["stack_trace"]; !ok {
				t.Error("error log missing 'stack_trace'")
			}
			break
		}
	}
	if !found {
		t.Error("no error log with 'panic' field found")
	}

	// Assert log entry has status=500
	requestID := resp.Header.Get("X-Request-ID")
	logEntry := logs.findByRequestID(requestID)
	if logEntry != nil {
		if logStatus, ok := logEntry["status"].(float64); ok && int(logStatus) != 500 {
			t.Errorf("log status = %d, want 500", int(logStatus))
		}
	}

	// Assert server still serving after panic
	resp2, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatalf("server not serving after panic: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("GET /healthz after panic: status = %d, want 200", resp2.StatusCode)
	}
}

// TestSmoke_ETagRoundTrip verifies the ETag / If-None-Match conditional GET
// round-trip: first request returns 200 with ETag; second with matching
// If-None-Match returns 304 with no body.
// Execution Path: 01-PATH-7
// Covers: TS-01-SMOKE-7
// Requirements: 01-REQ-16.1, 01-REQ-16.2, 01-REQ-16.3
func TestSmoke_ETagRoundTrip(t *testing.T) {
	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		<-startErr
	})

	addr := waitUntilListening(t, srv, 2*time.Second)

	// Register a handler that uses SetETag and CheckETag
	updatedAt := time.Date(2026, 7, 17, 14, 30, 0, 0, time.UTC)
	api := srv.APIGroup()
	api.GET("/resource", func(c echo.Context) error {
		if apikit.CheckETag(c, updatedAt) {
			return c.NoContent(http.StatusNotModified)
		}
		apikit.SetETag(c, updatedAt)
		return c.JSON(http.StatusOK, map[string]string{"data": "value"})
	})

	// First GET: should return 200 with ETag header
	resp1, err := http.Get("http://" + addr + "/api/v1/resource")
	if err != nil {
		t.Fatalf("first GET failed: %v", err)
	}
	resp1.Body.Close()

	if resp1.StatusCode != http.StatusOK {
		t.Errorf("first GET: status = %d, want 200", resp1.StatusCode)
	}

	etag := resp1.Header.Get("ETag")
	expectedETag := `W/"2026-07-17T14:30:00Z"`
	if etag != expectedETag {
		t.Errorf("ETag = %q, want %q", etag, expectedETag)
	}

	// Second GET with matching If-None-Match: should return 304
	req2, err := http.NewRequest("GET", "http://"+addr+"/api/v1/resource", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req2.Header.Set("If-None-Match", etag)

	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("second GET failed: %v", err)
	}
	resp2.Body.Close()

	if resp2.StatusCode != http.StatusNotModified {
		t.Errorf("second GET (matching If-None-Match): status = %d, want 304", resp2.StatusCode)
	}

	// Third GET with non-matching If-None-Match: should return 200
	req3, err := http.NewRequest("GET", "http://"+addr+"/api/v1/resource", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req3.Header.Set("If-None-Match", `W/"2025-01-01T00:00:00Z"`)

	resp3, err := http.DefaultClient.Do(req3)
	if err != nil {
		t.Fatalf("third GET failed: %v", err)
	}
	resp3.Body.Close()

	if resp3.StatusCode != http.StatusOK {
		t.Errorf("third GET (non-matching If-None-Match): status = %d, want 200", resp3.StatusCode)
	}
}

// TestSmoke_CachePublicOverride verifies that a route registered with
// CacheMiddleware(CachePublic) returns Cache-Control: public, max-age=300,
// while other routes on the same group return Cache-Control: no-store.
// Execution Path: 01-PATH-8
// Covers: TS-01-SMOKE-8
// Requirements: 01-REQ-15.1, 01-REQ-15.2, 01-REQ-15.3
func TestSmoke_CachePublicOverride(t *testing.T) {
	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		<-startErr
	})

	addr := waitUntilListening(t, srv, 2*time.Second)

	api := srv.APIGroup()

	// Register /providers with CachePublic override
	api.GET("/providers", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"data": "providers"})
	}, apikit.CacheMiddleware(apikit.CachePublic))

	// Register /items WITHOUT explicit cache middleware (inherits group default)
	api.GET("/items", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"data": "items"})
	})

	// GET /providers should have Cache-Control: public, max-age=300
	resp1, err := http.Get("http://" + addr + "/api/v1/providers")
	if err != nil {
		t.Fatalf("GET /providers failed: %v", err)
	}
	resp1.Body.Close()

	cc1 := resp1.Header.Get("Cache-Control")
	if cc1 != "public, max-age=300" {
		t.Errorf("GET /providers Cache-Control = %q, want %q", cc1, "public, max-age=300")
	}

	// GET /items should have Cache-Control: no-store (group default)
	resp2, err := http.Get("http://" + addr + "/api/v1/items")
	if err != nil {
		t.Fatalf("GET /items failed: %v", err)
	}
	resp2.Body.Close()

	cc2 := resp2.Header.Get("Cache-Control")
	if cc2 != "no-store" {
		t.Errorf("GET /items Cache-Control = %q, want %q", cc2, "no-store")
	}

	// Verify security headers on both responses
	for _, resp := range []*http.Response{resp1, resp2} {
		if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Error("missing X-Content-Type-Options: nosniff")
		}
		if resp.Header.Get("X-Frame-Options") != "DENY" {
			t.Error("missing X-Frame-Options: DENY")
		}
		if resp.Header.Get("Referrer-Policy") != "no-referrer" {
			t.Error("missing Referrer-Policy: no-referrer")
		}
	}
}

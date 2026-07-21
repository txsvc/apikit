package apikit_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/txsvc/apikit"
	"github.com/txsvc/apikit/internal/config"
)

// ========================================================================
// Task 5.1: Integration tests for health probe endpoints
// (TS-01-21, TS-01-22, TS-01-23, TS-01-24, TS-01-25, TS-01-E8, TS-01-E9)
// Requirements: 01-REQ-5.1, 01-REQ-5.2, 01-REQ-5.3, 01-REQ-5.4, 01-REQ-5.5,
//               01-REQ-5.E1, 01-REQ-5.E2
// ========================================================================

// TestHealth_HealthzReturns200OK verifies that GET /healthz returns HTTP 200
// with JSON body {"status": "ok"}, Content-Type: application/json; charset=utf-8,
// and Cache-Control: no-cache. No authentication is required.
// Covers TS-01-21 (Requirement: 01-REQ-5.1).
func TestHealth_HealthzReturns200OK(t *testing.T) {
	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		<-startErr
	})

	addr := waitUntilListening(t, srv, 2*time.Second)

	resp, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz failed: %v", err)
	}
	defer resp.Body.Close()

	// Assert HTTP 200
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	// Assert Content-Type
	ct := resp.Header.Get("Content-Type")
	if ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json; charset=utf-8")
	}

	// Assert JSON body
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("body = %v, want {\"status\": \"ok\"}", body)
	}

	// Assert Cache-Control: no-cache
	cc := resp.Header.Get("Cache-Control")
	if cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want %q", cc, "no-cache")
	}
}

// TestHealth_ReadyzWithChecker verifies that GET /readyz calls the injected
// HealthChecker and returns HTTP 200 {"status":"ready"} when it returns nil,
// or HTTP 503 {"status":"not ready"} when it returns an error. Both cases
// include Cache-Control: no-cache.
// Covers TS-01-22 (Requirement: 01-REQ-5.2).
func TestHealth_ReadyzWithChecker(t *testing.T) {
	cfg := buildTestConfig(0)

	// Create a togglable health checker
	var mu sync.Mutex
	var checkErr error
	checker := func() error {
		mu.Lock()
		defer mu.Unlock()
		return checkErr
	}

	srv := apikit.NewServer(cfg, checker)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		<-startErr
	})

	addr := waitUntilListening(t, srv, 2*time.Second)

	// Test 1: checker returns nil — should get 200 {"status":"ready"}
	t.Run("checker_returns_nil", func(t *testing.T) {
		mu.Lock()
		checkErr = nil
		mu.Unlock()

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
			t.Fatalf("failed to decode response body: %v", err)
		}
		if body["status"] != "ready" {
			t.Errorf("body = %v, want {\"status\": \"ready\"}", body)
		}

		cc := resp.Header.Get("Cache-Control")
		if cc != "no-cache" {
			t.Errorf("Cache-Control = %q, want %q", cc, "no-cache")
		}
	})

	// Test 2: checker returns error — should get 503 {"status":"not ready"}
	t.Run("checker_returns_error", func(t *testing.T) {
		mu.Lock()
		checkErr = errors.New("db down")
		mu.Unlock()

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
			t.Fatalf("failed to decode response body: %v", err)
		}
		if body["status"] != "not ready" {
			t.Errorf("body = %v, want {\"status\": \"not ready\"}", body)
		}

		cc := resp.Header.Get("Cache-Control")
		if cc != "no-cache" {
			t.Errorf("Cache-Control = %q, want %q", cc, "no-cache")
		}
	})
}

// TestHealth_ReadyzWithNilChecker verifies that GET /readyz returns HTTP 200
// {"status":"ready"} with Cache-Control: no-cache when no HealthChecker was
// provided (nil).
// Covers TS-01-23 (Requirement: 01-REQ-5.3).
func TestHealth_ReadyzWithNilChecker(t *testing.T) {
	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		<-startErr
	})

	addr := waitUntilListening(t, srv, 2*time.Second)

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
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["status"] != "ready" {
		t.Errorf("body = %v, want {\"status\": \"ready\"}", body)
	}

	cc := resp.Header.Get("Cache-Control")
	if cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want %q", cc, "no-cache")
	}
}

// TestHealth_VersionEndpoint verifies that GET /version returns HTTP 200
// with JSON body containing version (string), build (string), and mount_point
// (string matching config), with Cache-Control: public, max-age=300.
// Covers TS-01-24 (Requirement: 01-REQ-5.4).
func TestHealth_VersionEndpoint(t *testing.T) {
	cfg := buildTestConfig(0)
	// mount_point defaults to /api/v1 in buildTestConfig
	srv := apikit.NewServer(cfg, nil)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		<-startErr
	})

	addr := waitUntilListening(t, srv, 2*time.Second)

	resp, err := http.Get("http://" + addr + "/version")
	if err != nil {
		t.Fatalf("GET /version failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	// Verify version field exists and is a string
	if _, ok := body["version"].(string); !ok {
		t.Error("body missing 'version' string field")
	}

	// Verify build field exists and is a string
	if _, ok := body["build"].(string); !ok {
		t.Error("body missing 'build' string field")
	}

	// Verify mount_point matches config
	mp, ok := body["mount_point"].(string)
	if !ok {
		t.Error("body missing 'mount_point' string field")
	} else if mp != "/api/v1" {
		t.Errorf("mount_point = %q, want %q", mp, "/api/v1")
	}

	// Verify Cache-Control
	cc := resp.Header.Get("Cache-Control")
	if cc != "public, max-age=300" {
		t.Errorf("Cache-Control = %q, want %q", cc, "public, max-age=300")
	}
}

// TestHealth_EndpointsAtServerRoot verifies that /healthz, /readyz, and
// /version are registered at the server root, outside the mount_point, so
// they are reachable regardless of mount point configuration. Also verifies
// they are NOT available under the mount_point path.
// Covers TS-01-25 (Requirement: 01-REQ-5.5).
func TestHealth_EndpointsAtServerRoot(t *testing.T) {
	cfg := &apikit.Config{
		Server: config.ServerConfig{
			Port:       0,
			Bind:       "0.0.0.0",
			MountPoint: "/api/v2",
		},
		Database: config.DatabaseConfig{
			Path: "./data/apikit.db",
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
	}
	srv := apikit.NewServer(cfg, nil)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		<-startErr
	})

	addr := waitUntilListening(t, srv, 2*time.Second)

	// Verify endpoints are reachable at server root (not 404)
	rootPaths := []string{"/healthz", "/readyz", "/version"}
	for _, path := range rootPaths {
		t.Run("root"+path, func(t *testing.T) {
			resp, err := http.Get("http://" + addr + path)
			if err != nil {
				t.Fatalf("GET %s failed: %v", path, err)
			}
			resp.Body.Close()

			if resp.StatusCode == http.StatusNotFound {
				t.Errorf("GET %s returned 404; endpoint should be at server root", path)
			}
		})
	}

	// Verify endpoints are NOT mounted under the mount_point
	mountedPaths := []string{"/api/v2/healthz", "/api/v2/readyz", "/api/v2/version"}
	for _, path := range mountedPaths {
		t.Run("mounted"+path, func(t *testing.T) {
			resp, err := http.Get("http://" + addr + path)
			if err != nil {
				t.Fatalf("GET %s failed: %v", path, err)
			}
			resp.Body.Close()

			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("GET %s returned %d, want 404; "+
					"health endpoints should NOT be under mount_point",
					path, resp.StatusCode)
			}
		})
	}
}

// TestHealth_VersionDefaultDevValues verifies that GET /version returns
// version='dev' and build='dev' when the binary was built without -ldflags
// overrides (the default values of apikit.Version and apikit.Build).
// Covers TS-01-E8 (Requirement: 01-REQ-5.E1).
func TestHealth_VersionDefaultDevValues(t *testing.T) {
	// Verify the package-level defaults are "dev" (no -ldflags in test builds)
	if apikit.Version != "dev" {
		t.Errorf("apikit.Version = %q, want %q (default)", apikit.Version, "dev")
	}
	if apikit.Build != "dev" {
		t.Errorf("apikit.Build = %q, want %q (default)", apikit.Build, "dev")
	}

	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		<-startErr
	})

	addr := waitUntilListening(t, srv, 2*time.Second)

	resp, err := http.Get("http://" + addr + "/version")
	if err != nil {
		t.Fatalf("GET /version failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if body["version"] != "dev" {
		t.Errorf("version = %q, want %q", body["version"], "dev")
	}
	if body["build"] != "dev" {
		t.Errorf("build = %q, want %q", body["build"], "dev")
	}
}

// TestHealth_VersionEmptyStringOverride verifies that /version returns
// empty strings for version and build when those package-level variables
// are overridden to empty string (simulating -ldflags '-X ...= -X ...='),
// without any runtime fallback to "dev".
// Covers TS-01-E9 (Requirement: 01-REQ-5.E2).
func TestHealth_VersionEmptyStringOverride(t *testing.T) {
	// Override package-level vars to empty string (simulating -ldflags override)
	origVersion := apikit.Version
	origBuild := apikit.Build
	apikit.Version = ""
	apikit.Build = ""
	t.Cleanup(func() {
		apikit.Version = origVersion
		apikit.Build = origBuild
	})

	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		<-startErr
	})

	addr := waitUntilListening(t, srv, 2*time.Second)

	resp, err := http.Get("http://" + addr + "/version")
	if err != nil {
		t.Fatalf("GET /version failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	// Verify empty strings, NOT fallback to "dev"
	if body["version"] != "" {
		t.Errorf("version = %q, want empty string (no fallback)", body["version"])
	}
	if body["build"] != "" {
		t.Errorf("build = %q, want empty string (no fallback)", body["build"])
	}
}

// ========================================================================
// Task 5.5 (partial): Property test for health probe log suppression
// (TS-01-P10)
// Property: 01-PROP-10
// Validates: 01-REQ-11.2
// ========================================================================

// TestHealth_PropertyLogSuppression verifies that for any GET request to
// /healthz or /readyz when configured log level is info or above, no log
// entry is emitted; at debug level, a structurally identical entry is emitted
// with all five standard fields including duration as float64.
// Covers TS-01-P10 (Property: 01-PROP-10).
func TestHealth_PropertyLogSuppression(t *testing.T) {
	healthPaths := []string{"/healthz", "/readyz"}

	// Test at debug level with log_health_probes enabled: entries should be present
	t.Run("visible_at_debug", func(t *testing.T) {
		cfg := buildTestConfig(0)
		cfg.Logging.Level = "debug"
		cfg.Logging.LogHealthProbes = true
		srv := apikit.NewServer(cfg, nil)

		logs := startLogCapture(t, logrus.DebugLevel)

		startErr := startServerInBackground(srv)
		t.Cleanup(func() {
			srv.Shutdown(context.Background())
			<-startErr
		})

		addr := waitUntilListening(t, srv, 2*time.Second)

		for _, path := range healthPaths {
			resp, err := http.Get("http://" + addr + path)
			if err != nil {
				t.Fatalf("GET %s failed: %v", path, err)
			}
			resp.Body.Close()
		}

		time.Sleep(50 * time.Millisecond)

		for _, path := range healthPaths {
			entries := logs.findByPath(path)
			if len(entries) == 0 {
				t.Errorf("path %s: no log entry found at debug level", path)
				continue
			}

			entry := entries[0]
			level, _ := entry["level"].(string)
			if level != "debug" {
				t.Errorf("path %s: log level = %q, want %q", path, level, "debug")
			}

			// Verify all five standard fields
			for _, field := range []string{"method", "path", "status", "duration", "request_id"} {
				if _, ok := entry[field]; !ok {
					t.Errorf("path %s: missing log field %q", path, field)
				}
			}

			// duration must be float64
			if _, ok := entry["duration"].(float64); !ok {
				t.Errorf("path %s: 'duration' is not float64", path)
			}
		}
	})

	// Test at info, warn, and error levels: no health probe entries should appear
	suppressedLevels := []struct {
		name     string
		level    string
		logLevel logrus.Level
	}{
		{"suppressed_at_info", "info", logrus.InfoLevel},
		{"suppressed_at_warn", "warn", logrus.WarnLevel},
		{"suppressed_at_error", "error", logrus.ErrorLevel},
	}

	for _, sl := range suppressedLevels {
		t.Run(sl.name, func(t *testing.T) {
			cfg := buildTestConfig(0)
			cfg.Logging.Level = sl.level
			srv := apikit.NewServer(cfg, nil)

			logs := startLogCapture(t, sl.logLevel)

			startErr := startServerInBackground(srv)
			t.Cleanup(func() {
				srv.Shutdown(context.Background())
				<-startErr
			})

			addr := waitUntilListening(t, srv, 2*time.Second)

			for _, path := range healthPaths {
				resp, err := http.Get("http://" + addr + path)
				if err != nil {
					t.Fatalf("GET %s failed: %v", path, err)
				}
				resp.Body.Close()
			}

			time.Sleep(50 * time.Millisecond)

			// At this level, health probe entries (logged at debug) should NOT appear
			for _, path := range healthPaths {
				entries := logs.findByPath(path)
				if len(entries) > 0 {
					t.Errorf("path %s at %s level: found %d log entries, "+
						"want 0 (health probes should be debug-only)",
						path, sl.level, len(entries))
				}
			}
		})
	}
}

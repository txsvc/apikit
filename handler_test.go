package apikit_test

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/txsvc/apikit"
)

// ========================================================================
// Task 5.4: Unit and integration tests for handler registration API
// (TS-01-64, TS-01-65, TS-01-66, TS-01-E15)
// Requirements: 01-REQ-19.1, 01-REQ-19.2, 01-REQ-19.3, 01-REQ-19.E1
// ========================================================================

// TestAPIGroup_ReturnsSameNonNilGroup verifies that APIGroup() always returns
// the same non-nil *echo.Group at the configured mount point and never panics,
// even when called at different lifecycle stages: before Start(), during
// serving, and after Shutdown().
// Covers TS-01-64 (Requirement: 01-REQ-19.1).
func TestAPIGroup_ReturnsSameNonNilGroup(t *testing.T) {
	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	// Phase 1: Before Start()
	g1 := srv.APIGroup()
	if g1 == nil {
		t.Fatal("APIGroup() returned nil before Start()")
	}

	// Phase 2: During serving
	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		<-startErr
	})

	// Give Start() time to bind (works with real implementation)
	time.Sleep(50 * time.Millisecond)

	g2 := srv.APIGroup()
	if g2 == nil {
		t.Fatal("APIGroup() returned nil during serving")
	}

	// Phase 3: After Shutdown()
	srv.Shutdown(context.Background())

	// Wait for Start() to return
	select {
	case <-startErr:
	case <-time.After(5 * time.Second):
		t.Fatal("Start() did not return within 5 seconds")
	}

	g3 := srv.APIGroup()
	if g3 == nil {
		t.Fatal("APIGroup() returned nil after Shutdown()")
	}

	// All calls must return the same object (pre-constructed, cached)
	if g1 != g2 {
		t.Error("APIGroup() before Start() != APIGroup() during serving; want same object")
	}
	if g2 != g3 {
		t.Error("APIGroup() during serving != APIGroup() after Shutdown(); want same object")
	}
}

// TestAPIGroup_InheritsMiddlewareChain verifies that handlers registered via
// APIGroup() automatically inherit the full middleware chain. Tested by
// observing middleware effects:
//   - Content-Type enforcement: POST with text/plain returns 415
//   - Body size limit: POST with oversized body returns 413
//
// Covers TS-01-65 (Requirement: 01-REQ-19.2).
func TestAPIGroup_InheritsMiddlewareChain(t *testing.T) {
	cfg := buildTestConfig(0)
	cfg.Server.MaxBodySize = "1MB"
	srv := apikit.NewServer(cfg, nil)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		<-startErr
	})

	addr := waitUntilListening(t, srv, 2*time.Second)

	// Register a handler on APIGroup
	api := srv.APIGroup()
	if api == nil {
		t.Fatal("APIGroup() returned nil")
	}
	api.POST("/test", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// Test 1: Content-Type enforcement — POST with wrong Content-Type should get 415
	t.Run("content_type_enforcement_415", func(t *testing.T) {
		req, err := http.NewRequest("POST",
			"http://"+addr+"/api/v1/test",
			strings.NewReader("plain text"))
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Content-Type", "text/plain")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("HTTP request failed: %v", err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusUnsupportedMediaType {
			t.Errorf("status = %d, want 415 (content-type enforcement active)", resp.StatusCode)
		}
	})

	// Test 2: Body size limit — POST with oversized body should get 413
	t.Run("body_size_limit_413", func(t *testing.T) {
		oversizedBody := strings.Repeat("x", 2*1024*1024) // 2MB > 1MB limit
		req, err := http.NewRequest("POST",
			"http://"+addr+"/api/v1/test",
			strings.NewReader(oversizedBody))
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
			t.Errorf("status = %d, want 413 (body size limit active)", resp.StatusCode)
		}
	})
}

// TestAPIGroup_ConcurrentSafe verifies that APIGroup() is safe to call from
// multiple goroutines concurrently after NewServer() has returned. All calls
// must return the same non-nil *echo.Group with no data race.
// Run with: go test -race ./... -run TestAPIGroup_ConcurrentSafe
// Covers TS-01-66 (Requirement: 01-REQ-19.3).
func TestAPIGroup_ConcurrentSafe(t *testing.T) {
	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	const numGoroutines = 10
	results := make([]*echo.Group, numGoroutines)
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx] = srv.APIGroup()
		}(i)
	}

	wg.Wait()

	// All results must be non-nil
	for i, g := range results {
		if g == nil {
			t.Errorf("goroutine %d: APIGroup() returned nil", i)
		}
	}

	// All results must be the same object (pointer equality)
	for i := 1; i < numGoroutines; i++ {
		if results[i] != results[0] {
			t.Errorf("goroutine %d: APIGroup() returned different object "+
				"(pointer %p vs %p); want same pre-constructed group",
				i, results[i], results[0])
		}
	}
}

// TestAPIGroup_AfterShutdownNoPanic verifies that APIGroup() returns the
// pre-constructed Echo group without panicking or returning nil after
// Shutdown() has completed. Routes registered at this point may never
// receive traffic, but the call must be safe.
// Covers TS-01-E15 (Requirement: 01-REQ-19.E1).
func TestAPIGroup_AfterShutdownNoPanic(t *testing.T) {
	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	// Start and then shut down
	startErr := startServerInBackground(srv)

	// Give Start() a moment to bind
	time.Sleep(50 * time.Millisecond)

	srv.Shutdown(context.Background())

	// Wait for Start() to return
	select {
	case <-startErr:
	case <-time.After(5 * time.Second):
		t.Fatal("Start() did not return within 5 seconds")
	}

	// Call APIGroup() after shutdown — must not panic
	var g *echo.Group
	var panicked bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
				t.Errorf("APIGroup() panicked after Shutdown(): %v", r)
			}
		}()
		g = srv.APIGroup()
	}()

	if panicked {
		t.Fatal("APIGroup() panicked after Shutdown()")
	}
	if g == nil {
		t.Error("APIGroup() returned nil after Shutdown(); want non-nil *echo.Group")
	}
}

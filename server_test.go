package apikit_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"

	"github.com/txsvc/apikit"
	"github.com/txsvc/apikit/internal/config"
)

// ========================================================================
// Helpers
// ========================================================================

// buildTestConfig creates a *Config with sensible defaults for testing.
// port=0 causes the OS to assign an ephemeral port.
func buildTestConfig(port int) *apikit.Config {
	return &apikit.Config{
		Server: config.ServerConfig{
			Port:       port,
			Bind:       "0.0.0.0",
			MountPoint: "/api/v1",
		},
		Database: config.DatabaseConfig{
			Path: "./data/apikit.db",
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
	}
}

// waitUntilListening polls srv.Addr() until a non-empty address is returned
// or the timeout expires. Returns the address string.
func waitUntilListening(t *testing.T, srv *apikit.Server, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		addr := srv.Addr()
		if addr != "" {
			return addr
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server did not start listening within timeout")
	return ""
}

// startServerInBackground starts srv.Start() in a goroutine and returns
// a channel that receives the Start() return value.
func startServerInBackground(srv *apikit.Server) <-chan error {
	ch := make(chan error, 1)
	go func() { ch <- srv.Start() }()
	return ch
}

// ========================================================================
// Task 2.1: Unit tests for caller usage contract and NewServer behavior
// (TS-01-1, TS-01-3, TS-01-4, TS-01-E1, TS-01-E2)
// ========================================================================

// TestServer_ExportedSymbols verifies that LoadConfig, NewServer, APIGroup, and
// Start are all exported from the root apikit package. This is a compile-time
// test — if any symbol were missing or had the wrong signature, this file
// would not compile.
// Covers TS-01-1 (Requirement: 01-REQ-1.1).
func TestServer_ExportedSymbols(t *testing.T) {
	// Compile-time: verify these symbols exist and have the expected types.
	// The variables are assigned to _ to avoid "unused" errors.
	var loadConfig func() (*apikit.Config, error) = apikit.LoadConfig
	_ = loadConfig

	var newServer func(*apikit.Config, apikit.HealthChecker) *apikit.Server = apikit.NewServer
	_ = newServer

	// Verify method set of *Server
	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	var apiGroup func() = func() { _ = srv.APIGroup() }
	_ = apiGroup

	var start func() error = srv.Start
	_ = start

	var shutdown func(context.Context) error = srv.Shutdown
	_ = shutdown

	var addr func() string = srv.Addr
	_ = addr
}

// TestNewServer_NoFileIO verifies that NewServer does not perform file I/O
// and does not call LoadConfig() internally. It works with a purely in-memory
// Config and returns a non-nil *Server.
// Covers TS-01-3 (Requirement: 01-REQ-1.3).
func TestNewServer_NoFileIO(t *testing.T) {
	// Create config in memory — no config.toml exists
	dir := t.TempDir()
	t.Chdir(dir) // empty dir, no config.toml

	cfg := buildTestConfig(8080)
	srv := apikit.NewServer(cfg, nil)
	if srv == nil {
		t.Fatal("NewServer returned nil *Server")
	}
}

// TestNewServer_PanicsOnNilConfig verifies that NewServer panics with a
// descriptive message when cfg is nil.
// Covers TS-01-4 (Requirement: 01-REQ-1.4).
func TestNewServer_PanicsOnNilConfig(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic when cfg is nil, but no panic occurred")
		}
	}()
	apikit.NewServer(nil, nil)
}

// TestNewServer_PanicMessageDescriptive verifies that the panic value from
// NewServer(nil, nil) is a non-empty string mentioning "cfg" or "nil".
// Covers TS-01-E1 (Requirement: 01-REQ-1.E1).
func TestNewServer_PanicMessageDescriptive(t *testing.T) {
	var recovered interface{}
	func() {
		defer func() { recovered = recover() }()
		apikit.NewServer(nil, nil)
	}()

	if recovered == nil {
		t.Fatal("expected panic, got none")
	}

	msg := fmt.Sprintf("%v", recovered)
	if len(msg) == 0 {
		t.Fatal("panic value is empty")
	}

	mentionsCfg := strings.Contains(strings.ToLower(msg), "cfg")
	mentionsNil := strings.Contains(strings.ToLower(msg), "nil")
	if !mentionsCfg && !mentionsNil {
		t.Errorf("panic message %q does not mention 'cfg' or 'nil'", msg)
	}
}

// TestServer_ErrorsViaReturnValues verifies that library functions signal
// errors via return values and never call os.Exit(). If os.Exit() were
// called, the test process would terminate and this assertion would not run.
// Covers TS-01-E2 (Requirement: 01-REQ-1.E2).
func TestServer_ErrorsViaReturnValues(t *testing.T) {
	// Test 1: Start() with a port already in use should return a non-nil error.
	// Bind a port first so Start() encounters a conflict.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	cfg := buildTestConfig(port)
	cfg.Server.Bind = "127.0.0.1"
	srv := apikit.NewServer(cfg, nil)

	startErr := srv.Start()
	ln.Close() // cleanup the pre-bound listener

	// If we reach this line, os.Exit() was NOT called. Good.
	// Start() should have returned a non-nil error because the port was in use.
	if startErr == nil {
		t.Error("Start() returned nil error for a port already in use; expected non-nil error")
	}
}

// ========================================================================
// Task 2.2: Integration tests for Start() lifecycle and reference binary
// (TS-01-5, TS-01-6, TS-01-7, TS-01-8)
// ========================================================================

// TestStart_BlocksAndReturnsNilOnShutdown verifies that Start() blocks until
// Shutdown() is called and then returns nil on clean graceful shutdown.
// Covers TS-01-5 (Requirement: 01-REQ-1.5).
func TestStart_BlocksAndReturnsNilOnShutdown(t *testing.T) {
	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	startErr := startServerInBackground(srv)

	// Wait for server to be listening
	waitUntilListening(t, srv, 2*time.Second)

	// Trigger shutdown
	if err := srv.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown() returned error: %v", err)
	}

	// Wait for Start() to return
	select {
	case err := <-startErr:
		if err != nil {
			t.Errorf("Start() returned non-nil error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start() did not return within 5 seconds after Shutdown()")
	}
}

// TestServer_MakeBuildCompiles verifies that make build produces the
// bin/apikit binary using the three-step caller usage contract.
// Covers TS-01-6 (Requirement: 01-REQ-2.1).
func TestServer_MakeBuildCompiles(t *testing.T) {
	cmd := exec.Command("make", "build")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make build failed: %v\nOutput: %s", err, out)
	}

	if _, err := os.Stat("bin/apikit"); os.IsNotExist(err) {
		t.Fatal("bin/apikit not found after make build")
	}
}

// TestServer_BinaryHealthzEndpoint verifies that the cmd/apikit binary
// responds to GET /healthz with HTTP 200 and {"status": "ok"}.
// Covers TS-01-7 (Requirement: 01-REQ-2.2).
func TestServer_BinaryHealthzEndpoint(t *testing.T) {
	// Build the binary
	buildCmd := exec.Command("make", "build")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("make build failed: %v\nOutput: %s", err, out)
	}

	// Start the binary (use a temp dir with no config.toml for defaults).
	// Use absolute path because Go 1.19+ resolves relative paths containing
	// a separator against Cmd.Dir, not the process working directory.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	dir := t.TempDir()
	proc := exec.Command(filepath.Join(wd, "bin", "apikit"))
	proc.Dir = dir
	if err := proc.Start(); err != nil {
		t.Fatalf("failed to start bin/apikit: %v", err)
	}
	t.Cleanup(func() {
		proc.Process.Signal(syscall.SIGTERM)
		proc.Wait()
	})

	// Wait for the binary to be ready (poll /healthz)
	var resp *http.Response
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var err error
		resp, err = client.Get("http://localhost:8080/healthz")
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if resp == nil {
		t.Fatal("bin/apikit did not become ready within 5 seconds")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /healthz status = %d, want 200", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("response body = %v, want {\"status\": \"ok\"}", body)
	}
}

// TestServer_CmdApikitCompiles verifies that cmd/apikit compiles as part
// of the Go test suite (go build ./cmd/apikit/).
// Covers TS-01-8 (Requirement: 01-REQ-2.3).
func TestServer_CmdApikitCompiles(t *testing.T) {
	cmd := exec.Command("go", "build", "./cmd/apikit/")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build ./cmd/apikit/ failed: %v\nOutput: %s", err, out)
	}
}

// ========================================================================
// Task 2.3: Integration tests for Port 0 / ephemeral port and Addr()
// (TS-01-32, TS-01-33)
// ========================================================================

// TestAddr_EphemeralPort verifies that configuring port=0 causes the server
// to bind to an OS-assigned ephemeral port and Addr() returns the actual
// host:port string.
// Covers TS-01-32 (Requirement: 01-REQ-7.1).
func TestAddr_EphemeralPort(t *testing.T) {
	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		<-startErr
	})

	// Wait for server to be listening
	addr := waitUntilListening(t, srv, 2*time.Second)

	// Verify the address is in host:port format
	if addr == "" {
		t.Fatal("Addr() returned empty string")
	}

	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("Addr() returned invalid host:port %q: %v", addr, err)
	}
	if host == "" {
		t.Error("host component is empty")
	}

	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		t.Fatalf("port is not an integer: %q", portStr)
	}
	if port <= 0 {
		t.Errorf("port = %d, want > 0", port)
	}
}

// TestAddr_EmptyBeforeAndAfterLifecycle verifies that Addr() returns empty
// string before Start() has bound the listener and after shutdown.
// Covers TS-01-33 (Requirement: 01-REQ-7.2).
func TestAddr_EmptyBeforeAndAfterLifecycle(t *testing.T) {
	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	// Before Start(): Addr() should return empty string
	if addr := srv.Addr(); addr != "" {
		t.Errorf("Addr() before Start() = %q, want empty string", addr)
	}

	// Run a Start→Shutdown lifecycle
	startErr := startServerInBackground(srv)

	// Give Start() a moment to bind (for the real implementation)
	time.Sleep(100 * time.Millisecond)

	// Shut down the server
	srv.Shutdown(context.Background())

	// Wait for Start() to return
	select {
	case <-startErr:
	case <-time.After(5 * time.Second):
		t.Fatal("Start() did not return within 5 seconds")
	}

	// After shutdown: Addr() should return empty string
	if addr := srv.Addr(); addr != "" {
		t.Errorf("Addr() after Shutdown() = %q, want empty string", addr)
	}
}

// ========================================================================
// Task 2.4: Integration tests for graceful shutdown behavior
// (TS-01-26, TS-01-28, TS-01-29, TS-01-30, TS-01-31)
// ========================================================================

// TestShutdown_DrainsOnSIGTERM verifies that the server stops accepting
// connections and drains in-flight requests on SIGTERM within the drain
// timeout, and Start() returns nil.
// Covers TS-01-26 (Requirement: 01-REQ-6.1).
func TestShutdown_DrainsOnSIGTERM(t *testing.T) {
	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	// Prevent SIGTERM from killing the test process
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
	})

	// Wait for server to be listening
	waitUntilListening(t, srv, 2*time.Second)

	// Send SIGTERM to the current process.
	// The server's internal signal handler should catch this and call Shutdown.
	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("FindProcess: %v", err)
	}
	if err := p.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("Signal SIGTERM: %v", err)
	}

	// Wait for Start() to return
	select {
	case err := <-startErr:
		if err != nil {
			t.Errorf("Start() returned non-nil error after SIGTERM: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Start() did not return within 20 seconds after SIGTERM")
	}
}

// TestShutdown_CallerContextDeadlineWins verifies that Shutdown() uses
// context.WithTimeout combining the caller context and drainTimeout,
// with the earlier of the two winning.
// Covers TS-01-28 (Requirement: 01-REQ-6.3).
func TestShutdown_CallerContextDeadlineWins(t *testing.T) {
	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() { <-startErr })

	// Wait for server to be listening
	addr := waitUntilListening(t, srv, 2*time.Second)

	// Register a slow handler that takes 30 seconds
	api := srv.APIGroup()
	if api == nil {
		t.Fatal("APIGroup() returned nil")
	}
	api.GET("/slow", func(c echo.Context) error {
		time.Sleep(30 * time.Second)
		return c.NoContent(http.StatusOK)
	})

	// Start a request that will be in-flight during shutdown
	go func() {
		http.Get("http://" + addr + "/api/v1/slow")
	}()
	time.Sleep(100 * time.Millisecond)

	// Call Shutdown with a 2-second deadline
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	start := time.Now()
	srv.Shutdown(ctx)
	elapsed := time.Since(start)

	// Should complete within ~2 seconds (caller context), not 15 seconds (drainTimeout)
	if elapsed >= 5*time.Second {
		t.Errorf("Shutdown took %v, expected < 5s (caller context should win over 15s drain)", elapsed)
	}
}

// TestShutdown_Idempotent verifies that Shutdown() is idempotent: the first
// call initiates shutdown, subsequent calls return nil immediately.
// Covers TS-01-29 (Requirement: 01-REQ-6.4).
func TestShutdown_Idempotent(t *testing.T) {
	// Test sequential idempotency
	t.Run("sequential", func(t *testing.T) {
		cfg := buildTestConfig(0)
		srv := apikit.NewServer(cfg, nil)
		startErr := startServerInBackground(srv)

		waitUntilListening(t, srv, 2*time.Second)

		err1 := srv.Shutdown(context.Background())
		err2 := srv.Shutdown(context.Background())

		if err1 != nil {
			t.Errorf("first Shutdown() returned error: %v", err1)
		}
		if err2 != nil {
			t.Errorf("second Shutdown() returned error: %v", err2)
		}

		select {
		case <-startErr:
		case <-time.After(5 * time.Second):
			t.Fatal("Start() did not return")
		}
	})

	// Test concurrent idempotency
	t.Run("concurrent", func(t *testing.T) {
		cfg := buildTestConfig(0)
		srv := apikit.NewServer(cfg, nil)
		startErr := startServerInBackground(srv)

		waitUntilListening(t, srv, 2*time.Second)

		const numGoroutines = 5
		errs := make([]error, numGoroutines)
		var wg sync.WaitGroup
		wg.Add(numGoroutines)
		for i := 0; i < numGoroutines; i++ {
			go func(idx int) {
				defer wg.Done()
				errs[idx] = srv.Shutdown(context.Background())
			}(i)
		}
		wg.Wait()

		for i, err := range errs {
			if err != nil {
				t.Errorf("goroutine %d: Shutdown() returned error: %v", i, err)
			}
		}

		select {
		case <-startErr:
		case <-time.After(5 * time.Second):
			t.Fatal("Start() did not return")
		}
	})
}

// TestShutdown_LogsInitiation verifies that shutdown initiation is logged
// at info level with the drainTimeout duration before the drain window begins.
// Covers TS-01-30 (Requirement: 01-REQ-6.5).
func TestShutdown_LogsInitiation(t *testing.T) {
	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	// Capture logrus output
	var buf bytes.Buffer
	origOutput := logrus.StandardLogger().Out
	origLevel := logrus.GetLevel()
	logrus.SetOutput(&buf)
	logrus.SetLevel(logrus.InfoLevel)
	t.Cleanup(func() {
		logrus.SetOutput(origOutput)
		logrus.SetLevel(origLevel)
	})

	startErr := startServerInBackground(srv)
	waitUntilListening(t, srv, 2*time.Second)

	srv.Shutdown(context.Background())

	select {
	case <-startErr:
	case <-time.After(5 * time.Second):
		t.Fatal("Start() did not return")
	}

	// Parse log output for shutdown entry
	logOutput := buf.String()
	lines := strings.Split(strings.TrimSpace(logOutput), "\n")

	found := false
	for _, line := range lines {
		if line == "" {
			continue
		}
		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue // skip non-JSON lines
		}
		level, _ := entry["level"].(string)
		msg, _ := entry["msg"].(string)
		if level == "info" && strings.Contains(strings.ToLower(msg), "shutdown") {
			// Check if the message or fields reference the 15s timeout
			lineStr := strings.ToLower(line)
			if strings.Contains(lineStr, "15") {
				found = true
				break
			}
		}
	}

	if !found {
		t.Errorf("expected info-level log entry mentioning shutdown and 15s timeout; got:\n%s", logOutput)
	}
}

// TestStart_ReturnsAfterShutdownNoOsExit verifies that Start() returns to
// its caller after shutdown completes and os.Exit() is never called.
// If os.Exit() were called, this test's assertion would never run.
// Covers TS-01-31 (Requirement: 01-REQ-6.6).
func TestStart_ReturnsAfterShutdownNoOsExit(t *testing.T) {
	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	startCh := make(chan error, 1)
	go func() { startCh <- srv.Start() }()

	waitUntilListening(t, srv, 2*time.Second)

	srv.Shutdown(context.Background())

	select {
	case err := <-startCh:
		if err != nil {
			t.Errorf("Start() returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start() did not return within 5 seconds — possible os.Exit() or hang")
	}
	// Reaching this point proves os.Exit() was not called.
}

// ========================================================================
// Task 2.5: Integration tests for shutdown edge cases
// (TS-01-E10, TS-01-E11, TS-01-E12, TS-01-E13)
// ========================================================================

// TestShutdown_CancelledContextForcesClose verifies that if the caller's
// context is cancelled before drainTimeout, force-close proceeds immediately.
// Covers TS-01-E10 (Requirement: 01-REQ-6.E1).
func TestShutdown_CancelledContextForcesClose(t *testing.T) {
	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() { <-startErr })

	waitUntilListening(t, srv, 2*time.Second)

	// Create a context that will be cancelled after 1 second
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(1 * time.Second)
		cancel()
	}()

	start := time.Now()
	srv.Shutdown(ctx)
	elapsed := time.Since(start)

	// Should complete within ~1-2 seconds, NOT 15 seconds
	if elapsed >= 5*time.Second {
		t.Errorf("Shutdown took %v after context cancellation, expected < 5s", elapsed)
	}
}

// TestShutdown_DrainTimeoutForcesClose verifies that if in-flight requests
// do not complete within drainTimeout (15s), the server force-closes.
// Covers TS-01-E11 (Requirement: 01-REQ-6.E2).
func TestShutdown_DrainTimeoutForcesClose(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow test in short mode")
	}

	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() { <-startErr })

	addr := waitUntilListening(t, srv, 2*time.Second)

	// Register a handler that sleeps for 30 seconds (exceeds 15s drain timeout)
	api := srv.APIGroup()
	if api == nil {
		t.Fatal("APIGroup() returned nil")
	}
	api.GET("/slow", func(c echo.Context) error {
		time.Sleep(30 * time.Second)
		return c.NoContent(http.StatusOK)
	})

	// Trigger the slow handler
	go func() {
		http.Get("http://" + addr + "/api/v1/slow")
	}()
	time.Sleep(200 * time.Millisecond)

	// Call Shutdown and measure how long it takes
	start := time.Now()
	srv.Shutdown(context.Background())
	elapsed := time.Since(start)

	// Should force-close at ~15 seconds (drainTimeout)
	if elapsed < 15*time.Second {
		t.Errorf("Shutdown completed in %v, expected >= 15s (drain timeout)", elapsed)
	}
	if elapsed >= 20*time.Second {
		t.Errorf("Shutdown took %v, expected < 20s (15s drain + margin)", elapsed)
	}
}

// TestShutdown_ConcurrentCallsNoRace verifies that concurrent Shutdown() calls
// result in server shutting down exactly once with no data race or deadlock.
// Run with: go test -race
// Covers TS-01-E12 (Requirement: 01-REQ-6.E3).
func TestShutdown_ConcurrentCallsNoRace(t *testing.T) {
	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	startErr := startServerInBackground(srv)

	waitUntilListening(t, srv, 2*time.Second)

	const numGoroutines = 5
	errs := make([]error, numGoroutines)
	var wg sync.WaitGroup
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			errs[idx] = srv.Shutdown(context.Background())
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: Shutdown() returned error: %v", i, err)
		}
	}

	select {
	case <-startErr:
	case <-time.After(5 * time.Second):
		t.Fatal("Start() did not return after concurrent Shutdown() calls")
	}
}

// TestStart_SecondCallAfterShutdownReturnsError verifies that calling Start()
// a second time after shutdown returns a non-nil error immediately with a
// message indicating the server has already been shut down.
// Covers TS-01-E13 (Requirement: 01-REQ-6.E4).
func TestStart_SecondCallAfterShutdownReturnsError(t *testing.T) {
	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	// First Start() + Shutdown cycle
	startErr := startServerInBackground(srv)
	waitUntilListening(t, srv, 2*time.Second)

	srv.Shutdown(context.Background())

	// Wait for first Start() to return
	select {
	case <-startErr:
	case <-time.After(5 * time.Second):
		t.Fatal("first Start() did not return after Shutdown()")
	}

	// Second Start() should return error immediately
	start := time.Now()
	err := srv.Start()
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("second Start() returned nil error; expected non-nil error indicating already shut down")
	}

	// Should return immediately (not block)
	if elapsed >= 1*time.Second {
		t.Errorf("second Start() took %v, expected immediate return (< 1s)", elapsed)
	}

	// Error message should indicate the server is already shut down
	errMsg := strings.ToLower(err.Error())
	if !strings.Contains(errMsg, "shut down") && !strings.Contains(errMsg, "already") && !strings.Contains(errMsg, "closed") {
		t.Errorf("error message %q does not indicate server is already shut down", err.Error())
	}
}

// ========================================================================
// MountHandlers custom permissions
// ========================================================================

func TestMountHandlers_CustomPermissions(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := apikit.OpenDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	err = srv.MountHandlers(database,
		apikit.Permission{Resource: "workspaces", Action: "read"},
		apikit.Permission{Resource: "workspaces", Action: "create"},
	)
	if err != nil {
		t.Fatalf("MountHandlers with custom permissions failed: %v", err)
	}
}

func TestMountHandlers_InvalidPermissionReturnsError(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := apikit.OpenDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	err = srv.MountHandlers(database,
		apikit.Permission{Resource: "INVALID", Action: "read"},
	)
	if err == nil {
		t.Fatal("expected error for invalid permission resource, got nil")
	}
	if !strings.Contains(err.Error(), "INVALID") {
		t.Errorf("error %q does not mention the invalid resource", err.Error())
	}
}

func TestMountHandlers_DuplicateBuiltinPermissionReturnsError(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := apikit.OpenDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	err = srv.MountHandlers(database,
		apikit.Permission{Resource: "users", Action: "read"},
	)
	if err == nil {
		t.Fatal("expected error for duplicate built-in permission, got nil")
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Errorf("error %q does not mention duplicate registration", err.Error())
	}
}

func TestMountHandlers_NoPermissions(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := apikit.OpenDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	err = srv.MountHandlers(database)
	if err != nil {
		t.Fatalf("MountHandlers with no custom permissions failed: %v", err)
	}
}

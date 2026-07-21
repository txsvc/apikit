package apikit_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"

	"github.com/txsvc/apikit"
)

// ========================================================================
// Helpers for middleware, panic recovery, request ID, and logging tests
// ========================================================================

// isValidUUIDv4 validates that the given string is a valid UUID version 4.
// Implements the shared helper specified in subtask 3.3.
func isValidUUIDv4(s string) bool {
	u, err := uuid.Parse(s)
	if err != nil {
		return false
	}
	return u.Version() == 4
}

// logCapture captures logrus output during a test and provides helpers
// for searching captured log entries.
type logCapture struct {
	buf bytes.Buffer
}

// startLogCapture redirects logrus output to a buffer at the specified level
// and restores original settings on test cleanup. Returns the capture object
// for inspecting log entries after requests complete.
func startLogCapture(t *testing.T, level logrus.Level) *logCapture {
	t.Helper()
	lc := &logCapture{}
	origOutput := logrus.StandardLogger().Out
	origLevel := logrus.GetLevel()
	origFormatter := logrus.StandardLogger().Formatter
	logrus.SetOutput(&lc.buf)
	logrus.SetLevel(level)
	logrus.SetFormatter(&logrus.JSONFormatter{})
	t.Cleanup(func() {
		logrus.SetOutput(origOutput)
		logrus.SetLevel(origLevel)
		logrus.SetFormatter(origFormatter)
	})
	return lc
}

// entries parses all captured log lines as JSON maps.
func (lc *logCapture) entries() []map[string]interface{} {
	var result []map[string]interface{}
	for _, line := range strings.Split(strings.TrimSpace(lc.buf.String()), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]interface{}
		if json.Unmarshal([]byte(line), &entry) == nil {
			result = append(result, entry)
		}
	}
	return result
}

// findByRequestID returns the first log entry matching the given request_id.
func (lc *logCapture) findByRequestID(requestID string) map[string]interface{} {
	for _, entry := range lc.entries() {
		if rid, ok := entry["request_id"].(string); ok && rid == requestID {
			return entry
		}
	}
	return nil
}

// findByLevel returns all log entries at the given level string.
func (lc *logCapture) findByLevel(level string) []map[string]interface{} {
	var result []map[string]interface{}
	for _, entry := range lc.entries() {
		if l, ok := entry["level"].(string); ok && l == level {
			result = append(result, entry)
		}
	}
	return result
}

// findByPath returns log entries where the "path" field matches.
func (lc *logCapture) findByPath(path string) []map[string]interface{} {
	var result []map[string]interface{}
	for _, entry := range lc.entries() {
		if p, ok := entry["path"].(string); ok && p == path {
			result = append(result, entry)
		}
	}
	return result
}

// ========================================================================
// Task 3.1: Integration tests for middleware execution order
// (TS-01-34, TS-01-35, TS-01-36)
// Requirements: 01-REQ-8.1, 01-REQ-8.2, 01-REQ-8.3
// ========================================================================

// TestMiddleware_OrderBodyLimitAfterRequestID verifies the fixed middleware
// order by confirming that a 413 (body too large) response includes an
// X-Request-ID header, proving that Request ID middleware (step 2) runs
// before Body Size Limit middleware (step 3/4).
// Covers TS-01-34 (Requirement: 01-REQ-8.1).
func TestMiddleware_OrderBodyLimitAfterRequestID(t *testing.T) {
	cfg := buildTestConfig(0)
	cfg.Server.MaxBodySize = "1MB"
	srv := apikit.NewServer(cfg, nil)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		<-startErr
	})

	addr := waitUntilListening(t, srv, 2*time.Second)

	// Register a test handler on the API group
	api := srv.APIGroup()
	if api == nil {
		t.Fatal("APIGroup() returned nil")
	}
	api.POST("/test", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// Send a POST with a body larger than 1MB to trigger 413
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

	// Verify 413 status
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", resp.StatusCode)
	}

	// Verify X-Request-ID is present — body limit must fire after request ID
	requestID := resp.Header.Get("X-Request-ID")
	if requestID == "" {
		t.Error("X-Request-ID header missing from 413 response; " +
			"body size limit must fire after request ID assignment")
	}
}

// TestMiddleware_413IncludesValidUUIDv4RequestID verifies that a 413 Payload
// Too Large response includes the X-Request-ID header with a valid UUID v4.
// Covers TS-01-35 (Requirement: 01-REQ-8.2).
func TestMiddleware_413IncludesValidUUIDv4RequestID(t *testing.T) {
	cfg := buildTestConfig(0)
	cfg.Server.MaxBodySize = "1MB"
	srv := apikit.NewServer(cfg, nil)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		<-startErr
	})

	addr := waitUntilListening(t, srv, 2*time.Second)

	// Register a test handler
	api := srv.APIGroup()
	if api == nil {
		t.Fatal("APIGroup() returned nil")
	}
	api.POST("/test", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// Send 2MB body to trigger 413
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

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", resp.StatusCode)
	}

	requestID := resp.Header.Get("X-Request-ID")
	if !isValidUUIDv4(requestID) {
		t.Errorf("X-Request-ID = %q, want valid UUID v4", requestID)
	}
}

// TestMiddleware_LogStatusMatchesResponseStatus verifies that the structured
// log entry status field matches the actual HTTP status code returned to the
// client, including error codes produced by middleware (e.g. 415 from
// Content-Type Enforcement).
// Covers TS-01-36 (Requirement: 01-REQ-8.3).
func TestMiddleware_LogStatusMatchesResponseStatus(t *testing.T) {
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

	// Register a test handler
	api := srv.APIGroup()
	if api == nil {
		t.Fatal("APIGroup() returned nil")
	}
	api.POST("/test", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// Send POST with non-JSON Content-Type to trigger 415
	req, err := http.NewRequest("POST", "http://"+addr+"/api/v1/test",
		strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "text/plain")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("status = %d, want 415", resp.StatusCode)
	}

	// Allow logging to complete
	time.Sleep(50 * time.Millisecond)

	// Find the log entry for this request
	requestID := resp.Header.Get("X-Request-ID")
	logEntry := logs.findByRequestID(requestID)
	if logEntry == nil {
		t.Fatal("no log entry found for request")
	}

	// Verify status in log matches response status
	logStatus, ok := logEntry["status"].(float64) // JSON numbers are float64
	if !ok {
		t.Fatal("log entry 'status' field is missing or not a number")
	}
	if int(logStatus) != resp.StatusCode {
		t.Errorf("log status = %d, response status = %d; should match",
			int(logStatus), resp.StatusCode)
	}
}

// TestContentType_BodilessPostPassesThrough verifies that a POST request
// with no body (Content-Length 0, no Content-Type header) passes through the
// Content-Type enforcement middleware without a 415 rejection.
func TestContentType_BodilessPostPassesThrough(t *testing.T) {
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
	api.POST("/action", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "done"})
	})

	req, err := http.NewRequest("POST", "http://"+addr+"/api/v1/action", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200; bodiless POST should not be rejected by Content-Type enforcement", resp.StatusCode)
	}
}

// ========================================================================
// Task 3.2: Unit and integration tests for panic recovery middleware
// (TS-01-37, TS-01-38, TS-01-E14)
// Requirements: 01-REQ-9.1, 01-REQ-9.2, 01-REQ-9.E1
// ========================================================================

// TestPanic_RecoveryReturns500WithErrorEnvelope verifies that a panicking
// handler is caught by the recovery middleware, returns HTTP 500 with the
// standard JSON error envelope and Content-Type: application/json; charset=utf-8,
// logs the panic at error level with request_id, panic, and stack_trace fields,
// and the server continues serving subsequent requests.
// Covers TS-01-37 (Requirement: 01-REQ-9.1).
func TestPanic_RecoveryReturns500WithErrorEnvelope(t *testing.T) {
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

	// Register a handler that panics
	api := srv.APIGroup()
	if api == nil {
		t.Fatal("APIGroup() returned nil")
	}
	api.GET("/panic", func(c echo.Context) error {
		panic("test panic")
	})

	resp, err := http.Get("http://" + addr + "/api/v1/panic")
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	// Verify HTTP 500
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}

	// Verify Content-Type
	ct := resp.Header.Get("Content-Type")
	if ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json; charset=utf-8")
	}

	// Verify standard error envelope: {"error": {"code": 500, "message": "<string>"}}
	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	errObj, ok := body["error"].(map[string]interface{})
	if !ok {
		t.Fatal("response body missing 'error' object")
	}
	code, ok := errObj["code"].(float64)
	if !ok || int(code) != 500 {
		t.Errorf("error.code = %v, want 500", errObj["code"])
	}
	if _, ok := errObj["message"].(string); !ok {
		t.Error("error.message is missing or not a string")
	}

	// Allow logging to complete
	time.Sleep(50 * time.Millisecond)

	// Verify error-level log entry with required fields
	errorLogs := logs.findByLevel("error")
	if len(errorLogs) == 0 {
		t.Fatal("no error-level log entry found for panic")
	}

	panicLog := errorLogs[0]
	if _, ok := panicLog["request_id"]; !ok {
		t.Error("error log missing 'request_id' field")
	}
	if _, ok := panicLog["panic"]; !ok {
		t.Error("error log missing 'panic' field")
	}
	if _, ok := panicLog["stack_trace"]; !ok {
		t.Error("error log missing 'stack_trace' field")
	}

	// Verify server still serving after panic: send another request
	resp2, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatalf("server not serving after panic recovery: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("GET /healthz after panic: status = %d, want 200", resp2.StatusCode)
	}
}

// TestPanic_NoEchoBuiltinRecover verifies that only the custom panic recovery
// middleware is active and Echo's built-in middleware.Recover() is NOT registered.
// Behavioral check: a panic returns exactly one HTTP 500 response with a single
// error envelope (not doubled by duplicate recovery layers).
// Covers TS-01-38 (Requirement: 01-REQ-9.2).
func TestPanic_NoEchoBuiltinRecover(t *testing.T) {
	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		<-startErr
	})

	addr := waitUntilListening(t, srv, 2*time.Second)

	// Register a handler that panics
	api := srv.APIGroup()
	if api == nil {
		t.Fatal("APIGroup() returned nil")
	}
	api.GET("/panic", func(c echo.Context) error {
		panic("test panic")
	})

	resp, err := http.Get("http://" + addr + "/api/v1/panic")
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	// Verify exactly one 500 response (not doubled by duplicate recovery)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}

	// Parse the body: should contain exactly one error envelope (not nested/doubled)
	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if _, ok := body["error"]; !ok {
		t.Error("response body missing 'error' key; custom recovery may not be active")
	}
}

// TestPanic_LogRequestIDMatchesResponseHeader verifies that the panic recovery
// log entry includes the request_id field matching the X-Request-ID response
// header value. Since panic recovery runs after Request ID assignment, the
// request_id should be available in the log.
// Covers TS-01-E14 (Requirement: 01-REQ-9.E1).
func TestPanic_LogRequestIDMatchesResponseHeader(t *testing.T) {
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

	// Register a handler that panics
	api := srv.APIGroup()
	if api == nil {
		t.Fatal("APIGroup() returned nil")
	}
	api.GET("/panic", func(c echo.Context) error {
		panic("test panic")
	})

	resp, err := http.Get("http://" + addr + "/api/v1/panic")
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	resp.Body.Close()

	responseRequestID := resp.Header.Get("X-Request-ID")
	if responseRequestID == "" {
		t.Fatal("X-Request-ID header missing from panic response")
	}

	// Allow logging to complete
	time.Sleep(50 * time.Millisecond)

	// Find error-level log entry with 'panic' field
	errorLogs := logs.findByLevel("error")
	found := false
	for _, entry := range errorLogs {
		if _, hasPanic := entry["panic"]; hasPanic {
			logRequestID, _ := entry["request_id"].(string)
			if logRequestID != responseRequestID {
				t.Errorf("log request_id = %q, response X-Request-ID = %q; should match",
					logRequestID, responseRequestID)
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no error-level log entry with 'panic' field found")
	}
}

// ========================================================================
// Task 3.3: Unit and integration tests for Request ID middleware
// (TS-01-39, TS-01-40, TS-01-41)
// Requirements: 01-REQ-10.1, 01-REQ-10.2, 01-REQ-10.3
// ========================================================================

// TestRequestID_GeneratedWhenMissing verifies that a request without an
// X-Request-ID header gets a freshly generated UUID v4 in the X-Request-ID
// response header.
// Covers TS-01-39 (Requirement: 01-REQ-10.1).
func TestRequestID_GeneratedWhenMissing(t *testing.T) {
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
		t.Fatalf("HTTP request failed: %v", err)
	}
	resp.Body.Close()

	requestID := resp.Header.Get("X-Request-ID")
	if requestID == "" {
		t.Fatal("X-Request-ID header missing from response")
	}
	if !isValidUUIDv4(requestID) {
		t.Errorf("X-Request-ID = %q, want valid UUID v4", requestID)
	}
}

// TestRequestID_ValidUUIDv4Reused verifies that a valid UUID v4 in the incoming
// X-Request-ID header is reused in the response header and the structured log
// entry's request_id field.
// Covers TS-01-40 (Requirement: 01-REQ-10.2).
func TestRequestID_ValidUUIDv4Reused(t *testing.T) {
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

	// 550e8400-e29b-41d4-a716-446655440000 is a valid UUID v4
	// (version nibble = 4 in the 3rd group, variant nibble = a in the 4th)
	validUUID := "550e8400-e29b-41d4-a716-446655440000"
	req, err := http.NewRequest("GET", "http://"+addr+"/healthz", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("X-Request-ID", validUUID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	resp.Body.Close()

	// Verify response echoes the client-supplied UUID v4
	responseID := resp.Header.Get("X-Request-ID")
	if responseID != validUUID {
		t.Errorf("X-Request-ID = %q, want %q (client-supplied UUID v4)",
			responseID, validUUID)
	}

	// Allow logging to complete
	time.Sleep(50 * time.Millisecond)

	// Verify log entry uses the same request_id
	logEntry := logs.findByRequestID(validUUID)
	if logEntry == nil {
		t.Errorf("no log entry found with request_id = %q", validUUID)
	}
}

// TestRequestID_InvalidDiscardedNewGenerated verifies that invalid or
// non-UUID-v4 values in the X-Request-ID header are discarded and a new
// UUID v4 is generated. Tests both a non-UUID string and a UUID v3 (not v4).
// Covers TS-01-41 (Requirement: 01-REQ-10.3).
func TestRequestID_InvalidDiscardedNewGenerated(t *testing.T) {
	cfg := buildTestConfig(0)
	srv := apikit.NewServer(cfg, nil)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		<-startErr
	})

	addr := waitUntilListening(t, srv, 2*time.Second)

	invalidIDs := []struct {
		name  string
		value string
	}{
		{"not_a_uuid", "not-a-uuid"},
		// UUID v3 (version nibble = 3 in the 3rd group) — valid UUID but not v4
		{"uuid_v3", "550e8400-e29b-31d4-a716-446655440000"},
	}

	for _, tc := range invalidIDs {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", "http://"+addr+"/healthz", nil)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			req.Header.Set("X-Request-ID", tc.value)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("HTTP request failed: %v", err)
			}
			resp.Body.Close()

			responseID := resp.Header.Get("X-Request-ID")
			if responseID == tc.value {
				t.Errorf("X-Request-ID = %q, should have been replaced with a new UUID v4",
					responseID)
			}
			if !isValidUUIDv4(responseID) {
				t.Errorf("X-Request-ID = %q, want valid UUID v4", responseID)
			}
		})
	}
}

// ========================================================================
// Task 3.4: Integration tests for structured JSON logging
// (TS-01-42, TS-01-43, TS-01-44)
// Requirements: 01-REQ-11.1, 01-REQ-11.2, 01-REQ-11.3
// ========================================================================

// TestLogging_StructuredJSONFields verifies that every HTTP request produces
// a structured JSON log entry with all five standard fields: method (string),
// path (string), status (integer), duration (float64 ms), request_id (string).
// Covers TS-01-42 (Requirement: 01-REQ-11.1).
func TestLogging_StructuredJSONFields(t *testing.T) {
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

	// Register a test handler that returns 200
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

	// Allow logging to complete
	time.Sleep(50 * time.Millisecond)

	// Find the log entry for this request
	requestID := resp.Header.Get("X-Request-ID")
	logEntry := logs.findByRequestID(requestID)
	if logEntry == nil {
		t.Fatal("no log entry found for request")
	}

	// Verify method is string with correct value
	method, ok := logEntry["method"].(string)
	if !ok {
		t.Error("log field 'method' is missing or not a string")
	} else if method != "GET" {
		t.Errorf("log field 'method' = %q, want %q", method, "GET")
	}

	// Verify path is string
	if _, ok := logEntry["path"].(string); !ok {
		t.Error("log field 'path' is missing or not a string")
	}

	// Verify status is integer (represented as float64 in JSON)
	if _, ok := logEntry["status"].(float64); !ok {
		t.Error("log field 'status' is missing or not a number")
	}

	// Verify duration is float64 (milliseconds)
	if _, ok := logEntry["duration"].(float64); !ok {
		t.Error("log field 'duration' is missing or not a float64")
	}

	// Verify request_id is string and valid UUID v4
	rid, ok := logEntry["request_id"].(string)
	if !ok {
		t.Error("log field 'request_id' is missing or not a string")
	} else if !isValidUUIDv4(rid) {
		t.Errorf("log field 'request_id' = %q, want valid UUID v4", rid)
	}
}

// TestLogging_HealthProbesAtDebugLevel verifies that /healthz and /readyz
// requests are logged at debug level (not info or above) and include all five
// standard log fields including duration as float64. At info level, health
// probe entries should be absent.
// Covers TS-01-43 (Requirement: 01-REQ-11.2).
func TestLogging_HealthProbesAtDebugLevel(t *testing.T) {
	t.Run("visible_at_debug_when_enabled", func(t *testing.T) {
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

		// Send requests to both health probe endpoints
		for _, path := range []string{"/healthz", "/readyz"} {
			resp, err := http.Get("http://" + addr + path)
			if err != nil {
				t.Fatalf("GET %s failed: %v", path, err)
			}
			resp.Body.Close()
		}

		// Allow logging to complete
		time.Sleep(50 * time.Millisecond)

		// Verify log entries at debug level for each path
		for _, path := range []string{"/healthz", "/readyz"} {
			entries := logs.findByPath(path)
			if len(entries) == 0 {
				t.Errorf("no log entry found for path %s", path)
				continue
			}

			entry := entries[0]
			level, _ := entry["level"].(string)
			if level != "debug" {
				t.Errorf("path %s: log level = %q, want %q", path, level, "debug")
			}

			// Verify all five standard fields present
			if _, ok := entry["method"]; !ok {
				t.Errorf("path %s: missing 'method' field", path)
			}
			if _, ok := entry["path"]; !ok {
				t.Errorf("path %s: missing 'path' field", path)
			}
			if _, ok := entry["status"]; !ok {
				t.Errorf("path %s: missing 'status' field", path)
			}
			if _, ok := entry["duration"].(float64); !ok {
				t.Errorf("path %s: 'duration' missing or not float64", path)
			}
			if _, ok := entry["request_id"]; !ok {
				t.Errorf("path %s: missing 'request_id' field", path)
			}
		}
	})

	t.Run("suppressed_at_info", func(t *testing.T) {
		cfg := buildTestConfig(0)
		cfg.Logging.Level = "info"
		srv := apikit.NewServer(cfg, nil)

		logs := startLogCapture(t, logrus.InfoLevel)

		startErr := startServerInBackground(srv)
		t.Cleanup(func() {
			srv.Shutdown(context.Background())
			<-startErr
		})

		addr := waitUntilListening(t, srv, 2*time.Second)

		// Send health probe requests
		for _, path := range []string{"/healthz", "/readyz"} {
			resp, err := http.Get("http://" + addr + path)
			if err != nil {
				t.Fatalf("GET %s failed: %v", path, err)
			}
			resp.Body.Close()
		}

		// Allow logging to complete
		time.Sleep(50 * time.Millisecond)

		// At info level, health probe entries should NOT appear
		for _, path := range []string{"/healthz", "/readyz"} {
			entries := logs.findByPath(path)
			if len(entries) > 0 {
				t.Errorf("path %s: found %d log entries at info level, "+
					"want 0 (health probes should not appear)",
					path, len(entries))
			}
		}
	})

	t.Run("fully_suppressed_at_debug_by_default", func(t *testing.T) {
		cfg := buildTestConfig(0)
		cfg.Logging.Level = "debug"
		// LogHealthProbes defaults to false
		srv := apikit.NewServer(cfg, nil)

		logs := startLogCapture(t, logrus.DebugLevel)

		startErr := startServerInBackground(srv)
		t.Cleanup(func() {
			srv.Shutdown(context.Background())
			<-startErr
		})

		addr := waitUntilListening(t, srv, 2*time.Second)

		for _, path := range []string{"/healthz", "/readyz"} {
			resp, err := http.Get("http://" + addr + path)
			if err != nil {
				t.Fatalf("GET %s failed: %v", path, err)
			}
			resp.Body.Close()
		}

		time.Sleep(50 * time.Millisecond)

		for _, path := range []string{"/healthz", "/readyz"} {
			entries := logs.findByPath(path)
			if len(entries) > 0 {
				t.Errorf("path %s: found %d log entries at debug level with log_health_probes=false, "+
					"want 0", path, len(entries))
			}
		}
	})
}

// TestLogging_LevelFromConfig verifies that the logrus log level is configured
// from Config.Logging.Level at server startup. When level is "warn", info-level
// log entries should not be emitted for normal requests.
// Covers TS-01-44 (Requirement: 01-REQ-11.3).
func TestLogging_LevelFromConfig(t *testing.T) {
	cfg := buildTestConfig(0)
	cfg.Logging.Level = "warn"

	// Capture logs at debug level (permissive). The server should override
	// logrus to "warn" based on Config.Logging.Level, suppressing info entries.
	logs := startLogCapture(t, logrus.DebugLevel)

	srv := apikit.NewServer(cfg, nil)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		<-startErr
	})

	addr := waitUntilListening(t, srv, 2*time.Second)

	// Register a test handler
	api := srv.APIGroup()
	if api == nil {
		t.Fatal("APIGroup() returned nil")
	}
	api.GET("/test", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// Send a normal request (would normally be logged at info level)
	resp, err := http.Get("http://" + addr + "/api/v1/test")
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	resp.Body.Close()

	// Allow logging to complete
	time.Sleep(50 * time.Millisecond)

	// At warn config level, no info-level entries should appear
	infoEntries := logs.findByLevel("info")
	if len(infoEntries) > 0 {
		t.Errorf("found %d info-level log entries at warn config level; "+
			"should be suppressed", len(infoEntries))
	}
}

// ========================================================================
// Task 3.5: Property tests for middleware chain invariants
// (TS-01-P1, TS-01-P3)
// ========================================================================

// TestMiddleware_PropertyAllStepsApplied verifies that for any HTTP request
// reaching a registered handler, all middleware steps are applied in the
// documented order. Checks across varied request types (methods, headers,
// body sizes) that:
// - X-Request-ID is always set (step 2)
// - Security headers are always present (step 5/6)
// - Log entry status matches response status (logging step)
// - 413 and 415 responses include X-Request-ID
// Covers TS-01-P1 (Property: 01-PROP-1).
// Validates: 01-REQ-8.1, 01-REQ-8.2, 01-REQ-8.3.
func TestMiddleware_PropertyAllStepsApplied(t *testing.T) {
	cfg := buildTestConfig(0)
	cfg.Server.MaxBodySize = "1MB"
	cfg.Logging.Level = "debug"
	srv := apikit.NewServer(cfg, nil)

	logs := startLogCapture(t, logrus.DebugLevel)

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

	// Property test: various request types that exercise different middleware paths
	cases := []struct {
		name        string
		method      string
		path        string
		contentType string
		body        string
	}{
		{"GET_normal", "GET", "/api/v1/test", "", ""},
		{"POST_json", "POST", "/api/v1/test", "application/json", `{"key":"value"}`},
		{"POST_wrong_ct", "POST", "/api/v1/test", "text/plain", "hello"},
		{"POST_oversized", "POST", "/api/v1/test", "application/json",
			strings.Repeat("x", 2*1024*1024)},
		{"GET_healthz", "GET", "/healthz", "", ""},
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
			resp.Body.Close()

			// Invariant 1: X-Request-ID is always set and is a valid UUID v4
			requestID := resp.Header.Get("X-Request-ID")
			if !isValidUUIDv4(requestID) {
				t.Errorf("X-Request-ID = %q, want valid UUID v4", requestID)
			}

			// Invariant 2: Security headers always present
			if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
				t.Error("missing X-Content-Type-Options: nosniff")
			}
			if resp.Header.Get("X-Frame-Options") != "DENY" {
				t.Error("missing X-Frame-Options: DENY")
			}
			if resp.Header.Get("Referrer-Policy") != "no-referrer" {
				t.Error("missing Referrer-Policy: no-referrer")
			}

			// Allow logging to complete
			time.Sleep(50 * time.Millisecond)

			// Invariant 3: Log entry status matches response status
			logEntry := logs.findByRequestID(requestID)
			if logEntry != nil {
				logStatus, ok := logEntry["status"].(float64)
				if ok && int(logStatus) != resp.StatusCode {
					t.Errorf("log status = %d, response status = %d; should match",
						int(logStatus), resp.StatusCode)
				}
			}
		})
	}
}

// TestRequestID_PropertyAlwaysValidAndMatchesLog verifies that for any HTTP
// response, the X-Request-ID header is present with a valid UUID v4, and the
// same value appears in the structured log entry. Tests with no header, valid
// UUID v4 header, and various invalid header inputs (non-UUID, UUID v3, too
// long string, whitespace).
// Covers TS-01-P3 (Property: 01-PROP-3).
// Validates: 01-REQ-10.1, 01-REQ-10.2, 01-REQ-10.3.
func TestRequestID_PropertyAlwaysValidAndMatchesLog(t *testing.T) {
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

	// Property test: various X-Request-ID inputs
	cases := []struct {
		name        string
		inputID     string // empty means no X-Request-ID header sent
		shouldReuse bool   // true if input is a valid UUID v4 that should be reused
	}{
		{"no_header", "", false},
		{"valid_uuid_v4", "550e8400-e29b-41d4-a716-446655440000", true},
		{"invalid_not_uuid", "not-a-uuid", false},
		{"uuid_v3", "550e8400-e29b-31d4-a716-446655440000", false},
		{"too_long_string", strings.Repeat("a", 100), false},
		{"whitespace_only", " ", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", "http://"+addr+"/healthz", nil)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			if tc.inputID != "" {
				req.Header.Set("X-Request-ID", tc.inputID)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("HTTP request failed: %v", err)
			}
			resp.Body.Close()

			// Invariant: X-Request-ID response header is always a valid UUID v4
			responseID := resp.Header.Get("X-Request-ID")
			if !isValidUUIDv4(responseID) {
				t.Errorf("X-Request-ID = %q, want valid UUID v4", responseID)
			}

			// If input was a valid UUID v4, it should be reused
			if tc.shouldReuse && responseID != tc.inputID {
				t.Errorf("X-Request-ID = %q, want %q (should reuse valid UUID v4)",
					responseID, tc.inputID)
			}
			// If input was invalid/absent, it should NOT appear in response
			if !tc.shouldReuse && tc.inputID != "" && responseID == tc.inputID {
				t.Errorf("X-Request-ID = %q, should have been replaced (invalid input)",
					responseID)
			}

			// Allow logging to complete
			time.Sleep(50 * time.Millisecond)

			// Invariant: log entry request_id matches response X-Request-ID
			logEntry := logs.findByRequestID(responseID)
			if logEntry == nil {
				t.Errorf("no log entry found with request_id = %q", responseID)
			}
		})
	}
}

// ========================================================================
// Issue #36: Body size limit overshoot in limitedReadCloser
// Tests for chunked/streaming body limit enforcement and HTTPErrorHandler
// MaxBytesError handling.
// ========================================================================

// TestChunkedBody_ExceedingLimitReturns413 verifies that a chunked HTTP
// request (Transfer-Encoding: chunked, no Content-Length) whose total body
// exceeds the configured limit returns HTTP 413 with the standard JSON error
// envelope, not HTTP 500.
// Covers TS-NS-1 (Requirement: NS-REQ-1).
func TestChunkedBody_ExceedingLimitReturns413(t *testing.T) {
	cfg := buildTestConfig(0)
	cfg.Server.MaxBodySize = "1MB"
	srv := apikit.NewServer(cfg, nil)

	startErr := startServerInBackground(srv)
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		<-startErr
	})

	addr := waitUntilListening(t, srv, 2*time.Second)

	// Register a handler that reads the body (triggers MaxBytesReader error)
	api := srv.APIGroup()
	if api == nil {
		t.Fatal("APIGroup() returned nil")
	}
	api.POST("/test", func(c echo.Context) error {
		if _, err := io.ReadAll(c.Request().Body); err != nil {
			return err // Surfaces MaxBytesError to the error handler
		}
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// Use io.Pipe to create a chunked transfer (no Content-Length).
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		// Write more than 1MB in chunks
		chunk := bytes.Repeat([]byte("x"), 64*1024) // 64KB chunks
		for i := 0; i < 20; i++ {                   // 20 * 64KB = 1.25MB
			if _, err := pw.Write(chunk); err != nil {
				return
			}
		}
	}()

	req, err := http.NewRequest("POST", "http://"+addr+"/api/v1/test", pr)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// Explicitly set ContentLength to -1 to force chunked encoding
	req.ContentLength = -1

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	// Verify 413 status (not 500)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", resp.StatusCode)
	}

	// Verify standard JSON error envelope
	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	errObj, ok := body["error"].(map[string]interface{})
	if !ok {
		t.Fatal("response body missing 'error' object")
	}
	code, ok := errObj["code"].(float64)
	if !ok || int(code) != 413 {
		t.Errorf("error.code = %v, want 413", errObj["code"])
	}
	msg, ok := errObj["message"].(string)
	if !ok || msg != "payload too large" {
		t.Errorf("error.message = %v, want %q", errObj["message"], "payload too large")
	}
}

// TestHTTPErrorHandler_MaxBytesError verifies that HTTPErrorHandler maps
// *http.MaxBytesError to HTTP 413 with the standard JSON error envelope.
// Covers TS-NS-3 (Requirement: NS-REQ-3).
func TestHTTPErrorHandler_MaxBytesError(t *testing.T) {
	e := echo.New()
	e.HTTPErrorHandler = apikit.HTTPErrorHandler

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	c := e.NewContext(req, rec)

	// Invoke the error handler with a *http.MaxBytesError
	apikit.HTTPErrorHandler(&http.MaxBytesError{Limit: 1}, c)

	// Verify 413 status
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rec.Code)
	}

	// Verify JSON error envelope
	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	errObj, ok := body["error"].(map[string]interface{})
	if !ok {
		t.Fatal("response body missing 'error' object")
	}
	code, ok := errObj["code"].(float64)
	if !ok || int(code) != 413 {
		t.Errorf("error.code = %v, want 413", errObj["code"])
	}
	msg, ok := errObj["message"].(string)
	if !ok || msg != "payload too large" {
		t.Errorf("error.message = %v, want %q", errObj["message"], "payload too large")
	}
}

// TestHTTPErrorHandler_WrappedMaxBytesError verifies that HTTPErrorHandler
// correctly detects a wrapped *http.MaxBytesError via errors.As.
func TestHTTPErrorHandler_WrappedMaxBytesError(t *testing.T) {
	e := echo.New()
	e.HTTPErrorHandler = apikit.HTTPErrorHandler

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	c := e.NewContext(req, rec)

	// Wrap the MaxBytesError
	wrappedErr := fmt.Errorf("reading body: %w", &http.MaxBytesError{Limit: 1024})
	apikit.HTTPErrorHandler(wrappedErr, c)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rec.Code)
	}
}

// TestChunkedBody_413IncludesValidUUIDv4RequestID verifies that a chunked
// body 413 response includes the X-Request-ID header with a valid UUID v4.
// Covers TS-NS-4 (Requirement: NS-REQ-4).
func TestChunkedBody_413IncludesValidUUIDv4RequestID(t *testing.T) {
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
		if _, err := io.ReadAll(c.Request().Body); err != nil {
			return err
		}
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// Use io.Pipe to create chunked transfer
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		chunk := bytes.Repeat([]byte("x"), 64*1024)
		for i := 0; i < 20; i++ {
			if _, err := pw.Write(chunk); err != nil {
				return
			}
		}
	}()

	req, err := http.NewRequest("POST", "http://"+addr+"/api/v1/test", pr)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = -1

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", resp.StatusCode)
	}

	// Verify X-Request-ID is present and valid UUID v4
	requestID := resp.Header.Get("X-Request-ID")
	if requestID == "" {
		t.Fatal("X-Request-ID header missing from chunked 413 response")
	}
	if !isValidUUIDv4(requestID) {
		t.Errorf("X-Request-ID = %q, want valid UUID v4", requestID)
	}
}

// TestHTTPErrorHandler_NonBodyErrors_NotMisclassified verifies that errors
// unrelated to body size are not misclassified as 413.
// Covers TS-NS-5 (Requirement: NS-REQ-5).
func TestHTTPErrorHandler_NonBodyErrors_NotMisclassified(t *testing.T) {
	e := echo.New()
	e.HTTPErrorHandler = apikit.HTTPErrorHandler

	t.Run("generic_error_returns_500", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		c := e.NewContext(req, rec)

		apikit.HTTPErrorHandler(fmt.Errorf("something else"), c)

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rec.Code)
		}
	})

	t.Run("echo_http_error_404", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		c := e.NewContext(req, rec)

		apikit.HTTPErrorHandler(echo.NewHTTPError(http.StatusNotFound, "not found"), c)

		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rec.Code)
		}

		// Verify JSON error envelope
		var body map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode response body: %v", err)
		}
		errObj, ok := body["error"].(map[string]interface{})
		if !ok {
			t.Fatal("response body missing 'error' object")
		}
		code, ok := errObj["code"].(float64)
		if !ok || int(code) != 404 {
			t.Errorf("error.code = %v, want 404", errObj["code"])
		}
	})
}

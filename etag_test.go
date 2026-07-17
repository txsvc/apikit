package apikit_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/txsvc/apikit"
)

// ========================================================================
// Task 5.2: Unit tests for ETag utilities
// (TS-01-54, TS-01-55, TS-01-56, TS-01-57)
// Requirements: 01-REQ-16.1, 01-REQ-16.2, 01-REQ-16.3, 01-REQ-16.4
// ========================================================================

// TestSetETag_SetsWeakETagHeader verifies that SetETag sets the ETag response
// header to W/"<RFC3339-UTC-timestamp>" derived from the updatedAt time.
// Covers TS-01-54 (Requirement: 01-REQ-16.1).
func TestSetETag_SetsWeakETagHeader(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	updatedAt := time.Date(2026, 7, 17, 14, 30, 0, 0, time.UTC)
	apikit.SetETag(c, updatedAt)

	etag := c.Response().Header().Get("ETag")
	expected := `W/"2026-07-17T14:30:00Z"`
	if etag != expected {
		t.Errorf("ETag = %q, want %q", etag, expected)
	}
}

// TestCheckETag_MatchReturnsTrue verifies that CheckETag returns true when
// the If-None-Match request header matches the current ETag derived from
// updatedAt.
// Covers TS-01-55 (Requirement: 01-REQ-16.2).
func TestCheckETag_MatchReturnsTrue(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("If-None-Match", `W/"2026-07-17T14:30:00Z"`)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	updatedAt := time.Date(2026, 7, 17, 14, 30, 0, 0, time.UTC)
	result := apikit.CheckETag(c, updatedAt)

	if !result {
		t.Error("CheckETag returned false, want true (If-None-Match matches)")
	}
}

// TestCheckETag_MismatchReturnsFalse verifies that CheckETag returns false
// when the If-None-Match request header does not match the current ETag.
// Covers TS-01-56 (Requirement: 01-REQ-16.3).
func TestCheckETag_MismatchReturnsFalse(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// Set a different timestamp than updatedAt
	req.Header.Set("If-None-Match", `W/"2026-01-01T00:00:00Z"`)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	updatedAt := time.Date(2026, 7, 17, 14, 30, 0, 0, time.UTC)
	result := apikit.CheckETag(c, updatedAt)

	if result {
		t.Error("CheckETag returned true, want false (If-None-Match does not match)")
	}
}

// TestETag_ZeroTimeNoOp verifies that SetETag and CheckETag are no-ops when
// called with the zero value of time.Time: SetETag sets no ETag header,
// CheckETag returns false without checking If-None-Match.
// Covers TS-01-57 (Requirement: 01-REQ-16.4).
func TestETag_ZeroTimeNoOp(t *testing.T) {
	t.Run("SetETag_no_header", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		apikit.SetETag(c, time.Time{})

		etag := c.Response().Header().Get("ETag")
		if etag != "" {
			t.Errorf("ETag = %q, want empty (no-op for zero time)", etag)
		}
	})

	t.Run("CheckETag_returns_false", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		// Set If-None-Match even though it shouldn't be checked for zero time
		req.Header.Set("If-None-Match", `W/"2026-07-17T14:30:00Z"`)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		result := apikit.CheckETag(c, time.Time{})
		if result {
			t.Error("CheckETag returned true for zero time.Time, want false (no-op)")
		}
	})
}

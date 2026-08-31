package apikit_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/txsvc/apikit"
)

// TestWriteAPIErrorWithType_IncludesErrorType verifies that
// WriteAPIErrorWithType produces a JSON envelope containing the
// error_type field alongside code and message.
func TestWriteAPIErrorWithType_IncludesErrorType(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := apikit.WriteAPIErrorWithType(c, http.StatusBadRequest,
		"rebuild is only supported for carry_patch workspaces",
		"workspace_mode_mismatch")
	if err != nil {
		t.Fatalf("WriteAPIErrorWithType returned error: %v", err)
	}

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want %d", rec.Code, http.StatusBadRequest)
	}

	// Decode as generic map to verify exact field presence.
	var envelope map[string]map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&envelope); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	errObj, ok := envelope["error"]
	if !ok {
		t.Fatal("response missing 'error' key")
	}

	if code, ok := errObj["code"].(float64); !ok || int(code) != 400 {
		t.Errorf("error.code = %v; want 400", errObj["code"])
	}

	if msg, ok := errObj["message"].(string); !ok || msg != "rebuild is only supported for carry_patch workspaces" {
		t.Errorf("error.message = %v; want expected message", errObj["message"])
	}

	if et, ok := errObj["error_type"].(string); !ok || et != "workspace_mode_mismatch" {
		t.Errorf("error.error_type = %v; want 'workspace_mode_mismatch'", errObj["error_type"])
	}
}

// TestWriteAPIErrorWithType_SDKDecoding verifies that the SDK client
// correctly decodes the error_type field from a JSON error envelope
// produced by WriteAPIErrorWithType, and populates APIError.ErrorType.
func TestWriteAPIErrorWithType_SDKDecoding(t *testing.T) {
	// Start a server that returns a JSON error with error_type.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":{"code":409,"message":"a rebuild job is already queued or running for this workspace","error_type":"concurrent_rebuild"}}`))
	}))
	defer server.Close()

	client := apikit.NewClient(server.URL, apikit.WithAPIKey("key"))
	_, err := client.Healthz(context.Background())
	if err == nil {
		t.Fatal("expected error from 409 response")
	}

	var apiErr *apikit.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Code != 409 {
		t.Errorf("apiErr.Code = %d; want 409", apiErr.Code)
	}
	if apiErr.ErrorType != "concurrent_rebuild" {
		t.Errorf("apiErr.ErrorType = %q; want 'concurrent_rebuild'", apiErr.ErrorType)
	}
}

// TestWriteAPIError_SDKDecoding_NoErrorType verifies that the SDK
// client correctly handles error envelopes without error_type, leaving
// APIError.ErrorType as empty string.
func TestWriteAPIError_SDKDecoding_NoErrorType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":404,"message":"workspace not found"}}`))
	}))
	defer server.Close()

	client := apikit.NewClient(server.URL, apikit.WithAPIKey("key"))
	_, err := client.Healthz(context.Background())
	if err == nil {
		t.Fatal("expected error from 404 response")
	}

	var apiErr *apikit.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.ErrorType != "" {
		t.Errorf("apiErr.ErrorType = %q; want empty string", apiErr.ErrorType)
	}
}

// TestWriteAPIError_OmitsErrorType verifies that the original
// WriteAPIError function (without error_type) produces a JSON
// envelope that does NOT contain the error_type field, maintaining
// backward compatibility.
func TestWriteAPIError_OmitsErrorType(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := apikit.WriteAPIError(c, http.StatusNotFound, "workspace not found")
	if err != nil {
		t.Fatalf("WriteAPIError returned error: %v", err)
	}

	// Decode as generic map to verify error_type is absent.
	var envelope map[string]map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&envelope); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	errObj := envelope["error"]
	if _, exists := errObj["error_type"]; exists {
		t.Errorf("error_type should be omitted when not set, but found: %v", errObj["error_type"])
	}

	if code, ok := errObj["code"].(float64); !ok || int(code) != 404 {
		t.Errorf("error.code = %v; want 404", errObj["code"])
	}

	if msg, ok := errObj["message"].(string); !ok || msg != "workspace not found" {
		t.Errorf("error.message = %v; want 'workspace not found'", errObj["message"])
	}
}

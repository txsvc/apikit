package apikit

import (
	"errors"
	"fmt"
	"net/http"
)

// APIError is a typed error struct matching the server's error envelope.
// Callers inspect it via errors.As.
type APIError struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	ErrorType string `json:"error_type,omitempty"`
}

// Error implements the error interface.
// Returns "API error {Code}: {Message}".
func (e *APIError) Error() string {
	return fmt.Sprintf("API error %d: %s", e.Code, e.Message)
}

// ErrorCode returns the HTTP status code from the API error.
// Used by internal/cli to detect API errors via interface matching
// without importing the root apikit package (avoiding import cycles).
func (e *APIError) ErrorCode() int { return e.Code }

// ErrorMessage returns the error message from the API error.
// Used by internal/cli to detect API errors via interface matching
// without importing the root apikit package (avoiding import cycles).
func (e *APIError) ErrorMessage() string { return e.Message }

// ErrorErrorType returns the machine-readable error type from the API error.
// Used by internal/cli to detect API error types via interface matching
// without importing the root apikit package (avoiding import cycles).
func (e *APIError) ErrorErrorType() string { return e.ErrorType }

// ErrNotModified is a sentinel error returned when a conditional GET
// receives HTTP 304 (Not Modified). Callers check with errors.Is.
var ErrNotModified = errors.New("not modified")

// Response wraps a typed API response with HTTP headers, enabling
// ETag access without losing type safety.
type Response[T any] struct {
	Data       T
	StatusCode int
	Header     http.Header
}

// ETag returns the ETag header value from the response.
func (r *Response[T]) ETag() string {
	if r == nil {
		return ""
	}
	return r.Header.Get("ETag")
}

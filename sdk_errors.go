package apikit

import (
	"errors"
	"fmt"
	"net/http"
)

// APIError is a typed error struct matching the server's error envelope.
// Callers inspect it via errors.As.
type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Error implements the error interface.
// Returns "API error {Code}: {Message}".
func (e *APIError) Error() string {
	return fmt.Sprintf("API error %d: %s", e.Code, e.Message)
}

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

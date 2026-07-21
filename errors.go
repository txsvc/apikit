package apikit

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
)

// errorEnvelope is the standard JSON error response structure.
type errorEnvelope struct {
	Error errorDetail `json:"error"`
}

// errorDetail carries the integer code and human-readable message.
type errorDetail struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// WriteAPIError writes a standard JSON error response envelope to the response.
// Format: {"error": {"code": <integer>, "message": "<string>"}}
// Sets Content-Type: application/json; charset=utf-8.
// Returns the error from c.JSON() directly, propagating write errors.
func WriteAPIError(c echo.Context, code int, message string) error {
	// Explicitly set Content-Type with charset before writing the response,
	// since Echo v4.15+ omits charset from c.JSON() by default.
	c.Response().Header().Set("Content-Type", "application/json; charset=utf-8")
	return c.JSON(code, errorEnvelope{
		Error: errorDetail{
			Code:    code,
			Message: message,
		},
	})
}

// HTTPErrorHandler is the custom Echo error handler that ensures all error
// responses use the standard JSON envelope format.
func HTTPErrorHandler(err error, c echo.Context) {
	if c.Response().Committed {
		return
	}

	code := http.StatusInternalServerError
	message := "internal server error"

	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		code = http.StatusRequestEntityTooLarge
		message = "payload too large"
	} else if he, ok := err.(*echo.HTTPError); ok {
		code = he.Code
		if m, ok := he.Message.(string); ok {
			message = m
		} else {
			message = http.StatusText(code)
		}
	}

	_ = WriteAPIError(c, code, message)
}

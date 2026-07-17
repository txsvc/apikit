package apikit

import (
	"github.com/labstack/echo/v4"
)

// APIError writes a standard JSON error response envelope to the response.
// Format: {"error": {"code": <integer>, "message": "<string>"}}
// Sets Content-Type: application/json; charset=utf-8.
// Returns the error from c.JSON() directly, propagating write errors.
func APIError(c echo.Context, code int, message string) error {
	return nil // stub
}

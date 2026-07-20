package apiutil

import (
	"github.com/labstack/echo/v4"
)

type errorEnvelope struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// WriteAPIError writes a standard JSON error response envelope.
func WriteAPIError(c echo.Context, code int, message string) error {
	c.Response().Header().Set("Content-Type", "application/json; charset=utf-8")
	return c.JSON(code, errorEnvelope{
		Error: errorDetail{
			Code:    code,
			Message: message,
		},
	})
}

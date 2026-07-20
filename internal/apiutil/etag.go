package apiutil

import (
	"fmt"
	"time"

	"github.com/labstack/echo/v4"
)

func etagValue(updatedAt time.Time) string {
	return fmt.Sprintf(`W/"%s"`, updatedAt.UTC().Format(time.RFC3339))
}

// SetETag sets a weak ETag response header derived from the updatedAt timestamp.
func SetETag(c echo.Context, updatedAt time.Time) {
	if updatedAt.IsZero() {
		return
	}
	c.Response().Header().Set("ETag", etagValue(updatedAt))
}

// CheckETag compares the If-None-Match request header against the ETag
// derived from updatedAt. Returns true if they match.
func CheckETag(c echo.Context, updatedAt time.Time) bool {
	if updatedAt.IsZero() {
		return false
	}
	return c.Request().Header.Get("If-None-Match") == etagValue(updatedAt)
}

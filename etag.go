package apikit

import (
	"fmt"
	"time"

	"github.com/labstack/echo/v4"
)

// etagValue returns the weak ETag string for the given timestamp.
func etagValue(updatedAt time.Time) string {
	return fmt.Sprintf(`W/"%s"`, FormatUTC(updatedAt))
}

// SetETag sets a weak ETag response header derived from the updatedAt timestamp.
// The ETag format is W/"<RFC3339-UTC-timestamp>".
// If updatedAt is the zero value of time.Time, SetETag is a no-op.
func SetETag(c echo.Context, updatedAt time.Time) {
	if updatedAt.IsZero() {
		return
	}
	c.Response().Header().Set("ETag", etagValue(updatedAt))
}

// CheckETag compares the If-None-Match request header against the ETag
// derived from updatedAt. Returns true if they match (client cache is current),
// false otherwise. If updatedAt is the zero value of time.Time, returns false
// without checking If-None-Match.
func CheckETag(c echo.Context, updatedAt time.Time) bool {
	if updatedAt.IsZero() {
		return false
	}
	inm := c.Request().Header.Get("If-None-Match")
	return inm == etagValue(updatedAt)
}

package apikit

import (
	"time"

	"github.com/labstack/echo/v4"
)

// SetETag sets a weak ETag response header derived from the updatedAt timestamp.
// The ETag format is W/"<RFC3339-UTC-timestamp>".
// If updatedAt is the zero value of time.Time, SetETag is a no-op.
func SetETag(c echo.Context, updatedAt time.Time) {
	// stub: no-op
}

// CheckETag compares the If-None-Match request header against the ETag
// derived from updatedAt. Returns true if they match (client cache is current),
// false otherwise. If updatedAt is the zero value of time.Time, returns false
// without checking If-None-Match.
func CheckETag(c echo.Context, updatedAt time.Time) bool {
	return false // stub
}

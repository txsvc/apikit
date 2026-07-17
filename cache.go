package apikit

import (
	"github.com/labstack/echo/v4"
)

// CacheCategory identifies one of three cache behaviors for HTTP responses.
type CacheCategory int

const (
	// CacheNoStore produces Cache-Control: no-store.
	// Default for all routes under the API mount point.
	CacheNoStore CacheCategory = iota
	// CacheNoCache produces Cache-Control: no-cache.
	// Applied to health probe endpoints.
	CacheNoCache
	// CachePublic produces Cache-Control: public, max-age=300.
	// Applied to the /version endpoint and static discovery routes.
	CachePublic
)

// CacheMiddleware returns Echo middleware that sets the Cache-Control header
// based on the provided CacheCategory.
func CacheMiddleware(cat CacheCategory) echo.MiddlewareFunc {
	var value string
	switch cat {
	case CacheNoStore:
		value = "no-store"
	case CacheNoCache:
		value = "no-cache"
	case CachePublic:
		value = "public, max-age=300"
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Response().Header().Set("Cache-Control", value)
			return next(c)
		}
	}
}

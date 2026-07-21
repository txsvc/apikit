package apikit

import (
	"fmt"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
)

// contextKeyRequestID is the Echo context key for the request ID.
const contextKeyRequestID = "request_id"

// panicRecoveryMiddleware returns Echo middleware that recovers from panics
// in downstream handlers or middleware, logs the panic at error level with
// structured fields (request_id, panic, stack_trace), and returns HTTP 500
// via WriteAPIError(). This replaces Echo's built-in middleware.Recover().
func panicRecoveryMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			defer func() {
				if r := recover(); r != nil {
					// Extract request_id set by the Request ID middleware
					requestID, _ := c.Get(contextKeyRequestID).(string)

					// Log panic at error level with structured fields
					logrus.WithFields(logrus.Fields{
						"request_id":  requestID,
						"panic":       fmt.Sprintf("%v", r),
						"stack_trace": string(debug.Stack()),
					}).Error("panic recovered")

					// Return standard JSON error envelope
					_ = WriteAPIError(c, http.StatusInternalServerError, "internal server error")
				}
			}()
			return next(c)
		}
	}
}

// requestIDMiddleware returns Echo middleware that assigns a UUID v4 request ID
// to every request. If the incoming request has a valid UUID v4 in the
// X-Request-ID header, it is reused; otherwise a new one is generated.
// The request ID is set in the X-Request-ID response header and stored in
// the Echo context for downstream middleware and handlers.
func requestIDMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			var requestID string

			// Check incoming X-Request-ID header
			incoming := c.Request().Header.Get("X-Request-ID")
			if incoming != "" {
				parsed, err := uuid.Parse(incoming)
				if err == nil && parsed.Version() == 4 {
					requestID = incoming
				}
			}

			// Generate a new UUID v4 if incoming was absent or invalid
			if requestID == "" {
				requestID = uuid.New().String()
			}

			// Set in response header and Echo context
			c.Response().Header().Set("X-Request-ID", requestID)
			c.Set(contextKeyRequestID, requestID)

			return next(c)
		}
	}
}

// loggingMiddleware returns Echo middleware that logs structured JSON entries
// for every HTTP request. Health probe paths (/healthz, /readyz) are logged
// at debug level; all other paths are logged at info level.
// Fields: method, path, status, duration (float64 ms), request_id.
func loggingMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()

			// Execute the downstream chain
			err := next(c)

			// Compute duration in milliseconds as float64
			duration := float64(time.Since(start).Nanoseconds()) / 1e6

			// Extract request_id from context
			requestID, _ := c.Get(contextKeyRequestID).(string)

			fields := logrus.Fields{
				"method":     c.Request().Method,
				"path":       c.Request().URL.Path,
				"status":     c.Response().Status,
				"duration":   duration,
				"request_id": requestID,
			}

			// Health probe paths are logged at debug level
			path := c.Request().URL.Path
			if path == "/healthz" || path == "/readyz" {
				logrus.WithFields(fields).Debug("request completed")
			} else {
				logrus.WithFields(fields).Info("request completed")
			}

			return err
		}
	}
}

// bodySizeLimitMiddleware returns Echo middleware that rejects requests with
// a body exceeding maxBytes with HTTP 413 via WriteAPIError().
func bodySizeLimitMiddleware(maxBytes int64) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if c.Request().Body != nil && c.Request().ContentLength > maxBytes {
				return WriteAPIError(c, http.StatusRequestEntityTooLarge, "payload too large")
			}

			if c.Request().Body != nil {
				c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, maxBytes)
			}

			return next(c)
		}
	}
}

// contentTypeEnforcementMiddleware returns Echo middleware that rejects POST,
// PUT, and PATCH requests with a Content-Type other than application/json
// with HTTP 415 via WriteAPIError(). Requests with no body (Content-Length 0
// or missing) are exempt since there is no media type to enforce. GET, DELETE,
// HEAD, and OPTIONS pass through without Content-Type inspection.
func contentTypeEnforcementMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			method := c.Request().Method
			if method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch {
				if c.Request().ContentLength != 0 {
					ct := c.Request().Header.Get("Content-Type")
					if !strings.HasPrefix(ct, "application/json") {
						return WriteAPIError(c, http.StatusUnsupportedMediaType, "unsupported media type")
					}
				}
			}
			return next(c)
		}
	}
}

// securityHeadersMiddleware returns Echo middleware that sets standard HTTP
// security headers on every response:
//   - X-Content-Type-Options: nosniff
//   - X-Frame-Options: DENY
//   - Referrer-Policy: no-referrer
//
// It does NOT set Cache-Control; that is managed exclusively by CacheMiddleware.
func securityHeadersMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			h := c.Response().Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "no-referrer")
			return next(c)
		}
	}
}

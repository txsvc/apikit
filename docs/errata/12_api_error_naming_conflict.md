# Erratum: APIError Naming Conflict (Spec 12)

## Context

Spec 12 (Go SDK) defines `APIError` as a struct type in `sdk_errors.go`:

```go
type APIError struct {
    Code    int
    Message string
}
```

However, the root `apikit` package already had `APIError` defined as a
**function** in `errors.go`:

```go
func APIError(c echo.Context, code int, message string) error
```

Go does not allow a function and a type to share the same name in the same
package. Since spec 12-REQ-15.1 requires all SDK source files to be in the
root `apikit` package, these names collide.

## Resolution

The existing server-side function `APIError` was renamed to `WriteAPIError`
across the entire codebase. This name more accurately describes what the
function does (writes an error response to the Echo context) and avoids
collision with the SDK's `APIError` struct type.

### Files Modified

- `errors.go` — function definition and call in `HTTPErrorHandler`
- `middleware.go` — three call sites in middleware functions
- `internal/auth/auth.go` — seven call sites in auth middleware
- `internal/handlers/users.go` — all handler call sites
- `internal/handlers/orgs.go` — all handler call sites
- `response_middleware_test.go` — test call sites
- `internal/handlers/orgs_test.go` — test call site
- `internal/auth/credentials.go` — comment reference
- `internal/oauth/handler.go` — comment reference

### Impact

All existing tests pass after the rename. The function signature and behavior
are unchanged; only the name differs. External consumers who imported
`apikit.APIError(...)` as a function call would need to update to
`apikit.WriteAPIError(...)`.

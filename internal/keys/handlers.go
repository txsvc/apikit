package keys

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
)

// keyHandlers holds the database handle shared by all API key lifecycle
// handler functions. Methods on this struct are unexported; only
// RegisterKeyHandlers is exported.
type keyHandlers struct {
	db *sql.DB
}

// RegisterKeyHandlers registers the three API key lifecycle endpoints on the
// provided Echo group:
//   - GET /user/keys
//   - POST /user/keys/:key_id/refresh
//   - DELETE /user/keys/:key_id
//
// The group is expected to be mounted at /api/v1 with CacheMiddleware(CacheNoStore)
// already applied by the parent, so all three endpoints inherit Cache-Control: no-store.
func RegisterKeyHandlers(group *echo.Group, database *sql.DB) {
	h := &keyHandlers{db: database}

	group.GET("/user/keys", h.listKeys)
	group.POST("/user/keys/:key_id/refresh", h.refreshKey)
	group.DELETE("/user/keys/:key_id", h.revokeKey)
}

// listKeys handles GET /user/keys — lists all API key metadata for the
// authenticated user.
func (h *keyHandlers) listKeys(c echo.Context) error {
	return errors.New("not implemented")
}

// refreshKey handles POST /user/keys/:key_id/refresh — generates a new secret
// for an existing, non-revoked, non-expired key.
func (h *keyHandlers) refreshKey(c echo.Context) error {
	return c.JSON(http.StatusInternalServerError, map[string]string{"error": "not implemented"})
}

// revokeKey handles DELETE /user/keys/:key_id — permanently revokes a key.
func (h *keyHandlers) revokeKey(c echo.Context) error {
	return c.JSON(http.StatusInternalServerError, map[string]string{"error": "not implemented"})
}

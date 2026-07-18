package oauth

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// providerResponse represents a single entry in the GET /auth/providers
// response. Only name and authorize_url are included; secrets such as
// client_secret, token_url, and userinfo_url are never exposed.
type providerResponse struct {
	Name         string `json:"name"`
	AuthorizeURL string `json:"authorize_url"`
}

// handleProviders returns an Echo handler for GET /auth/providers.
// It calls Registry.List() to get provider names in alphabetical order,
// then for each name calls Registry.Get(name).AuthorizeURL("", "") to
// obtain a base authorization URL with client_id and scope pre-populated
// but without state or redirect_uri.
func handleProviders(registry *Registry) echo.HandlerFunc {
	return func(c echo.Context) error {
		names := registry.List()
		result := make([]providerResponse, 0, len(names))
		for _, name := range names {
			p := registry.Get(name)
			result = append(result, providerResponse{
				Name:         name,
				AuthorizeURL: p.AuthorizeURL("", ""),
			})
		}
		return c.JSON(http.StatusOK, result)
	}
}

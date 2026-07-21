package apikit

import (
	"encoding/json"
	"fmt"
)

// resolveOrgSlugFromJSON decodes a JSON array of organizations and returns
// the UUID of the org matching the given slug. Returns an error if no match
// is found or the matched org has an empty UUID.
func resolveOrgSlugFromJSON(data []byte, slug string) (string, error) {
	var orgs []struct {
		ID   string `json:"id"`
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(data, &orgs); err != nil {
		return "", fmt.Errorf("unexpected response format from organization listing")
	}

	for _, org := range orgs {
		if org.Slug == slug {
			if org.ID == "" {
				return "", fmt.Errorf("organization '%s' has no valid UUID", slug)
			}
			return org.ID, nil
		}
	}

	return "", fmt.Errorf("organization '%s' not found", slug)
}

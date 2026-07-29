package api_test

import (
	"strings"
	"testing"
)

// ========================================================================
// Spec 16 Task 3.2: OpenAPI specification validation tests for flexible
// resource selectors.
// Test Spec: TS-16-22, TS-16-23, TS-16-24
// Requirements: 16-REQ-7.1, 16-REQ-7.2, 16-REQ-7.3
// ========================================================================

// TestOpenAPIUserEndpointIDParam verifies that the {id} parameter for all
// user endpoints describes accepted selector formats (UUID, username, or
// email) and does NOT include format: uuid.
//
// Test Spec: TS-16-22
// Requirement: 16-REQ-7.1
func TestOpenAPIUserEndpointIDParam(t *testing.T) {
	doc := loadSpec(t)

	// User endpoints that have an {id} path parameter.
	userPaths := []string{
		"/users/{id}",
		"/users/{id}/promote",
		"/users/{id}/demote",
		"/users/{id}/block",
		"/users/{id}/unblock",
		"/users/{id}/keys",
		"/users/{id}/tokens",
	}

	for _, path := range userPaths {
		t.Run(path, func(t *testing.T) {
			pi := mustGetPathItem(t, doc, path)

			// Collect all parameters from the path item and its operations.
			// Path-level parameters apply to all operations.
			var foundID bool
			allParams := pi.Parameters

			// Also check operation-level parameters.
			for _, method := range allMethods {
				op := getOperation(pi, method)
				if op == nil {
					continue
				}
				allParams = append(allParams, op.Parameters...)
			}

			for _, param := range allParams {
				if param == nil || param.Name != "id" || param.In != "path" {
					continue
				}
				foundID = true

				// Description must mention UUID, username, and email.
				desc := param.Description
				if !strings.Contains(desc, "UUID") {
					t.Errorf("%s {id} description missing 'UUID'; got: %q", path, desc)
				}
				if !strings.Contains(desc, "username") {
					t.Errorf("%s {id} description missing 'username'; got: %q", path, desc)
				}
				if !strings.Contains(desc, "email") {
					t.Errorf("%s {id} description missing 'email'; got: %q", path, desc)
				}

				// Schema must NOT have format: uuid.
				if param.Schema != nil {
					schema := param.Schema.Schema()
					if schema != nil && schema.Format != "" && strings.EqualFold(schema.Format, "uuid") {
						t.Errorf("%s {id} schema still has format: uuid; want format removed", path)
					}
				}
			}

			if !foundID {
				t.Errorf("%s: no {id} path parameter found", path)
			}
		})
	}
}

// TestOpenAPIOrgEndpointIDParam verifies that the {id} parameter for all
// org endpoints describes accepted selector formats (UUID or slug) and does
// NOT include format: uuid.
//
// Test Spec: TS-16-23
// Requirement: 16-REQ-7.2
func TestOpenAPIOrgEndpointIDParam(t *testing.T) {
	doc := loadSpec(t)

	// Org endpoints that have an {id} path parameter.
	orgPaths := []string{
		"/orgs/{id}",
		"/orgs/{id}/block",
		"/orgs/{id}/unblock",
		"/orgs/{id}/members",
	}

	for _, path := range orgPaths {
		t.Run(path, func(t *testing.T) {
			pi := mustGetPathItem(t, doc, path)

			var foundID bool
			allParams := pi.Parameters

			for _, method := range allMethods {
				op := getOperation(pi, method)
				if op == nil {
					continue
				}
				allParams = append(allParams, op.Parameters...)
			}

			for _, param := range allParams {
				if param == nil || param.Name != "id" || param.In != "path" {
					continue
				}
				foundID = true

				// Description must mention UUID and slug.
				desc := param.Description
				if !strings.Contains(desc, "UUID") {
					t.Errorf("%s {id} description missing 'UUID'; got: %q", path, desc)
				}
				if !strings.Contains(desc, "slug") {
					t.Errorf("%s {id} description missing 'slug'; got: %q", path, desc)
				}

				// Schema must NOT have format: uuid.
				if param.Schema != nil {
					schema := param.Schema.Schema()
					if schema != nil && schema.Format != "" && strings.EqualFold(schema.Format, "uuid") {
						t.Errorf("%s {id} schema still has format: uuid; want format removed", path)
					}
				}
			}

			if !foundID {
				t.Errorf("%s: no {id} path parameter found", path)
			}
		})
	}
}

// TestOpenAPIOrgMemberUserIDParam verifies that the {user_id} parameter in
// org member endpoints describes accepted selector formats (UUID, username,
// or email) and does NOT include format: uuid.
//
// Test Spec: TS-16-24
// Requirement: 16-REQ-7.3
func TestOpenAPIOrgMemberUserIDParam(t *testing.T) {
	doc := loadSpec(t)

	// The org member endpoint path.
	path := "/orgs/{id}/members/{user_id}"
	pi := mustGetPathItem(t, doc, path)

	// Check both PUT and DELETE operations.
	memberMethods := []string{"put", "delete"}

	for _, method := range memberMethods {
		t.Run(strings.ToUpper(method)+" "+path, func(t *testing.T) {
			op := getOperation(pi, method)
			if op == nil {
				t.Fatalf("no %s operation on %s", strings.ToUpper(method), path)
			}

			// Collect path-level and operation-level parameters.
			allParams := append(pi.Parameters, op.Parameters...)

			var foundUserID bool
			for _, param := range allParams {
				if param == nil || param.Name != "user_id" || param.In != "path" {
					continue
				}
				foundUserID = true

				// Description must mention UUID, username, and email.
				desc := param.Description
				if !strings.Contains(desc, "UUID") {
					t.Errorf("%s %s {user_id} description missing 'UUID'; got: %q", strings.ToUpper(method), path, desc)
				}
				if !strings.Contains(desc, "username") {
					t.Errorf("%s %s {user_id} description missing 'username'; got: %q", strings.ToUpper(method), path, desc)
				}
				if !strings.Contains(desc, "email") {
					t.Errorf("%s %s {user_id} description missing 'email'; got: %q", strings.ToUpper(method), path, desc)
				}

				// Schema must NOT have format: uuid.
				if param.Schema != nil {
					schema := param.Schema.Schema()
					if schema != nil && schema.Format != "" && strings.EqualFold(schema.Format, "uuid") {
						t.Errorf("%s %s {user_id} schema still has format: uuid; want format removed", strings.ToUpper(method), path)
					}
				}
			}

			if !foundUserID {
				t.Errorf("%s %s: no {user_id} path parameter found", strings.ToUpper(method), path)
			}
		})
	}
}

// TestOpenAPIKeyTokenParamsUnchanged verifies that removing format: uuid
// from selector parameters does NOT accidentally add selector descriptions
// (username, email, slug) to key_id or token_id parameters — those remain
// simple string identifiers, unaffected by the selector changes.
//
// Edge case: 16-REQ-7.E1
func TestOpenAPIKeyTokenParamsUnchanged(t *testing.T) {
	doc := loadSpec(t)

	// These parameters must NOT gain selector descriptions.
	nonSelectorParams := []struct {
		path      string
		paramName string
	}{
		{"/users/{id}/keys/{key_id}", "key_id"},
		{"/users/{id}/tokens/{token_id}", "token_id"},
	}

	for _, tc := range nonSelectorParams {
		t.Run(tc.path+"/{"+tc.paramName+"}", func(t *testing.T) {
			pi := mustGetPathItem(t, doc, tc.path)

			var allParams = pi.Parameters
			for _, method := range allMethods {
				op := getOperation(pi, method)
				if op == nil {
					continue
				}
				allParams = append(allParams, op.Parameters...)
			}

			var found bool
			for _, param := range allParams {
				if param == nil || param.Name != tc.paramName || param.In != "path" {
					continue
				}
				found = true

				// Description must NOT mention selector alternatives.
				desc := param.Description
				if strings.Contains(desc, "username") {
					t.Errorf("%s {%s} description unexpectedly contains 'username': %q", tc.path, tc.paramName, desc)
				}
				if strings.Contains(desc, "email") {
					t.Errorf("%s {%s} description unexpectedly contains 'email': %q", tc.path, tc.paramName, desc)
				}
				if strings.Contains(desc, "slug") {
					t.Errorf("%s {%s} description unexpectedly contains 'slug': %q", tc.path, tc.paramName, desc)
				}
			}

			if !found {
				t.Errorf("%s: no {%s} path parameter found", tc.path, tc.paramName)
			}
		})
	}
}

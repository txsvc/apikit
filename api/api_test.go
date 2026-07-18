package api_test

import (
	"os"
	"strings"
	"testing"

	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
)

// specPath is the path to the OpenAPI specification file relative to the
// module root. Tests are run via `go test ./api/...` from the module root.
const specPath = "api/openapi.yaml"

// loadSpec reads and parses api/openapi.yaml using libopenapi, returning the
// high-level v3 document model. It calls t.Fatalf on any error (file read,
// parse, or structural validation failure) — never os.Exit or panic.
func loadSpec(t *testing.T) *v3high.Document {
	t.Helper()

	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", specPath, err)
	}

	doc, err := libopenapi.NewDocument(data)
	if err != nil {
		t.Fatalf("failed to parse OpenAPI document: %v", err)
	}

	model, err := doc.BuildV3Model()
	if err != nil {
		t.Fatalf("failed to build OpenAPI v3 model: %v", err)
	}
	if model == nil {
		t.Fatalf("BuildV3Model returned nil model")
	}

	return &model.Model
}

// getOperation retrieves the *Operation for a given HTTP method (lowercase)
// from a PathItem. Returns nil if the method is not defined.
func getOperation(pi *v3high.PathItem, method string) *v3high.Operation {
	switch strings.ToLower(method) {
	case "get":
		return pi.Get
	case "put":
		return pi.Put
	case "post":
		return pi.Post
	case "delete":
		return pi.Delete
	case "patch":
		return pi.Patch
	case "options":
		return pi.Options
	case "head":
		return pi.Head
	case "trace":
		return pi.Trace
	}
	return nil
}

// publicPaths lists the path+method pairs that are public (no bearerAuth,
// no 401 response). POST /auth/callback is public because it is the OAuth
// code exchange endpoint that creates credentials — it cannot require auth.
var publicPaths = map[string]string{
	"/healthz":        "get",
	"/readyz":         "get",
	"/version":        "get",
	"/auth/providers": "get",
	"/auth/callback":  "post",
}

// allMethods is the list of HTTP methods to iterate over on each path item.
var allMethods = []string{"get", "put", "post", "delete", "patch", "options", "head", "trace"}

// hasBearerAuth checks whether a security requirement list contains a
// reference to the "bearerAuth" scheme.
func hasBearerAuth(security []*base.SecurityRequirement) bool {
	for _, req := range security {
		if req == nil || req.Requirements == nil {
			continue
		}
		for name := range req.Requirements.FromOldest() {
			if name == "bearerAuth" {
				return true
			}
		}
	}
	return false
}

// ============================================================================
// TS-03-1, TS-03-72, TS-03-73, TS-03-74 (Task 1.1)
// Validates: 03-REQ-1.1, 03-REQ-16.1, 03-REQ-16.2, 03-REQ-16.3
// ============================================================================

// TestOpenAPISpec verifies that api/openapi.yaml exists, uses OpenAPI 3.1.0
// format with YAML syntax, and is parseable by libopenapi without structural
// errors.
//
// Scope is limited to structural OpenAPI 3.1 parsing correctness only.
// This test does NOT validate individual operations, request/response
// examples, or ETag derivation (03-REQ-16.2).
func TestOpenAPISpec(t *testing.T) {
	doc := loadSpec(t)

	if doc.Info == nil {
		t.Fatalf("parsed spec has nil Info block")
	}

	if doc.Version != "3.1.0" {
		t.Errorf("expected OpenAPI version 3.1.0, got %q", doc.Version)
	}
}

// ============================================================================
// TS-03-2, TS-03-3 (Task 1.2)
// Validates: 03-REQ-1.2, 03-REQ-1.3
// ============================================================================

// TestOpenAPIServers verifies the top-level servers block and health probe
// path-level server overrides.
func TestOpenAPIServers(t *testing.T) {
	doc := loadSpec(t)

	// TS-03-2: Top-level servers block has exactly one entry.
	t.Run("top_level_servers", func(t *testing.T) {
		if len(doc.Servers) != 1 {
			t.Fatalf("expected exactly 1 top-level server, got %d", len(doc.Servers))
		}
		if doc.Servers[0].URL != "/api/v1" {
			t.Errorf("expected server URL '/api/v1', got %q", doc.Servers[0].URL)
		}
		wantDesc := "Default mount point (configurable per deployment)"
		if doc.Servers[0].Description != wantDesc {
			t.Errorf("expected server description %q, got %q", wantDesc, doc.Servers[0].Description)
		}
	})

	// TS-03-3: Path-level servers override for health probes.
	healthPaths := []string{"/healthz", "/readyz", "/version"}
	for _, path := range healthPaths {
		t.Run("path_server_override_"+strings.TrimPrefix(path, "/"), func(t *testing.T) {
			if doc.Paths == nil || doc.Paths.PathItems == nil {
				t.Fatalf("spec has no paths defined")
			}
			pathItem := doc.Paths.PathItems.GetOrZero(path)
			if pathItem == nil {
				t.Fatalf("path %s not found in spec", path)
			}
			if len(pathItem.Servers) != 1 {
				t.Fatalf("expected 1 path-level server on %s, got %d", path, len(pathItem.Servers))
			}
			if pathItem.Servers[0].URL != "/" {
				t.Errorf("expected path-level server URL '/' on %s, got %q", path, pathItem.Servers[0].URL)
			}
		})
	}
}

// ============================================================================
// TS-03-4 (Task 1.3)
// Validates: 03-REQ-1.4
// ============================================================================

// TestOpenAPIRefResolution verifies that all $ref references in
// api/openapi.yaml resolve without errors. The libopenapi parser reports
// unresolved references as structural errors during BuildV3Model, so
// loadSpec (which asserts no errors) covers this requirement.
func TestOpenAPIRefResolution(t *testing.T) {
	_ = loadSpec(t)
}

// ============================================================================
// TS-03-5, TS-03-6, TS-03-7 (Task 1.4)
// Validates: 03-REQ-2.1, 03-REQ-2.2, 03-REQ-2.3
// ============================================================================

// TestOpenAPISecurityScheme verifies that a single bearerAuth HTTP bearer
// security scheme is defined under components/securitySchemes with a
// description covering all three credential types.
func TestOpenAPISecurityScheme(t *testing.T) {
	doc := loadSpec(t)

	if doc.Components == nil {
		t.Fatalf("spec has no components defined")
	}
	if doc.Components.SecuritySchemes == nil {
		t.Fatalf("spec has no securitySchemes defined")
	}

	scheme := doc.Components.SecuritySchemes.GetOrZero("bearerAuth")
	if scheme == nil {
		t.Fatalf("bearerAuth security scheme not found in components/securitySchemes")
	}

	if scheme.Type != "http" {
		t.Errorf("expected bearerAuth type 'http', got %q", scheme.Type)
	}
	if scheme.Scheme != "bearer" {
		t.Errorf("expected bearerAuth scheme 'bearer', got %q", scheme.Scheme)
	}

	desc := scheme.Description
	for _, keyword := range []string{"Admin Token", "API Key", "PAT"} {
		if !strings.Contains(desc, keyword) {
			t.Errorf("bearerAuth description should mention %q, got: %q", keyword, desc)
		}
	}
}

// TestOpenAPIAuthenticatedEndpoints verifies that all non-public endpoints
// include bearerAuth in their security array.
func TestOpenAPIAuthenticatedEndpoints(t *testing.T) {
	doc := loadSpec(t)

	if doc.Paths == nil || doc.Paths.PathItems == nil {
		t.Fatalf("spec has no paths defined")
	}

	for path, pathItem := range doc.Paths.PathItems.FromOldest() {
		for _, method := range allMethods {
			op := getOperation(pathItem, method)
			if op == nil {
				continue
			}

			// Skip public endpoints.
			if m, ok := publicPaths[path]; ok && m == method {
				continue
			}

			if !hasBearerAuth(op.Security) {
				t.Errorf("%s %s: expected bearerAuth in security, but not found",
					strings.ToUpper(method), path)
			}
		}
	}
}

// TestOpenAPIPublicEndpoints verifies that public endpoints (health probes,
// GET /auth/providers, POST /auth/callback) have no bearerAuth security
// requirement and no 401 response defined.
func TestOpenAPIPublicEndpoints(t *testing.T) {
	doc := loadSpec(t)

	if doc.Paths == nil || doc.Paths.PathItems == nil {
		t.Fatalf("spec has no paths defined")
	}

	for path, method := range publicPaths {
		t.Run(strings.ToUpper(method)+"_"+strings.ReplaceAll(strings.TrimPrefix(path, "/"), "/", "_"), func(t *testing.T) {
			pathItem := doc.Paths.PathItems.GetOrZero(path)
			if pathItem == nil {
				t.Fatalf("path %s not found in spec", path)
			}

			op := getOperation(pathItem, method)
			if op == nil {
				t.Fatalf("no %s operation on %s", method, path)
			}

			// Security should be empty or absent (no non-empty requirements).
			for _, req := range op.Security {
				if req != nil && req.Requirements != nil && orderedmap.Len(req.Requirements) > 0 {
					t.Errorf("%s %s: expected no security requirements, but found some",
						strings.ToUpper(method), path)
					break
				}
			}

			// No 401 response should be defined.
			if op.Responses != nil && op.Responses.Codes != nil {
				if op.Responses.Codes.GetOrZero("401") != nil {
					t.Errorf("%s %s: expected no 401 response, but one is defined",
						strings.ToUpper(method), path)
				}
			}
		})
	}
}

// ============================================================================
// TS-03-8, TS-03-9, TS-03-P4 (Task 1.5)
// Validates: 03-REQ-3.1, 03-REQ-3.2
// ============================================================================

// TestOpenAPIPATPermissions verifies that the PAT permissions array item
// schema uses a pattern constraint (not enum) and documents all built-in
// permission strings.
func TestOpenAPIPATPermissions(t *testing.T) {
	doc := loadSpec(t)

	if doc.Components == nil || doc.Components.Schemas == nil {
		t.Fatalf("spec has no components/schemas")
	}

	for _, name := range []string{"PatMetadata", "Pat"} {
		t.Run(name, func(t *testing.T) {
			proxy := doc.Components.Schemas.GetOrZero(name)
			if proxy == nil {
				t.Fatalf("schema %s not found in components/schemas", name)
			}
			schema := proxy.Schema()
			if schema == nil {
				t.Fatalf("could not build schema for %s", name)
			}
			if schema.Properties == nil {
				t.Fatalf("schema %s has no properties", name)
			}

			permProxy := schema.Properties.GetOrZero("permissions")
			if permProxy == nil {
				t.Fatalf("schema %s has no 'permissions' property", name)
			}
			permSchema := permProxy.Schema()
			if permSchema == nil {
				t.Fatalf("could not build permissions schema for %s", name)
			}

			// Permissions should be an array with items.
			if permSchema.Items == nil || !permSchema.Items.IsA() {
				t.Fatalf("schema %s permissions has no items schema", name)
			}
			itemSchema := permSchema.Items.A.Schema()
			if itemSchema == nil {
				t.Fatalf("could not build permissions items schema for %s", name)
			}

			// TS-03-8 / TS-03-P4: Pattern constraint, no enum.
			wantPattern := `^[a-z_]+:[a-z_]+$`
			if itemSchema.Pattern != wantPattern {
				t.Errorf("schema %s permissions items pattern: expected %q, got %q",
					name, wantPattern, itemSchema.Pattern)
			}
			if len(itemSchema.Enum) > 0 {
				t.Errorf("schema %s permissions items should not use enum (found %d values)",
					name, len(itemSchema.Enum))
			}

			// TS-03-9: Description documents all six built-in permissions.
			builtInPerms := []string{
				"users:read", "orgs:read", "keys:read",
				"keys:manage", "tokens:read", "tokens:manage",
			}
			for _, perm := range builtInPerms {
				if !strings.Contains(itemSchema.Description, perm) {
					t.Errorf("schema %s permissions items description should mention %q",
						name, perm)
				}
			}
		})
	}
}

// ============================================================================
// TS-03-10, TS-03-11, TS-03-P6 (Task 1.6)
// Validates: 03-REQ-4.1, 03-REQ-4.2
// ============================================================================

// TestOpenAPIExpiresField verifies that the expires field on
// POST /auth/callback and POST /user/tokens uses enum [0,30,60,90],
// default 90, with 0 documented as permanent.
func TestOpenAPIExpiresField(t *testing.T) {
	doc := loadSpec(t)

	if doc.Paths == nil || doc.Paths.PathItems == nil {
		t.Fatalf("spec has no paths defined")
	}

	for _, path := range []string{"/auth/callback", "/user/tokens"} {
		t.Run(path, func(t *testing.T) {
			pathItem := doc.Paths.PathItems.GetOrZero(path)
			if pathItem == nil {
				t.Fatalf("path %s not found in spec", path)
			}
			if pathItem.Post == nil {
				t.Fatalf("no POST operation on %s", path)
			}
			if pathItem.Post.RequestBody == nil {
				t.Fatalf("POST %s has no request body", path)
			}
			if pathItem.Post.RequestBody.Content == nil {
				t.Fatalf("POST %s request body has no content", path)
			}

			mt := pathItem.Post.RequestBody.Content.GetOrZero("application/json")
			if mt == nil {
				t.Fatalf("POST %s request body has no application/json content", path)
			}
			if mt.Schema == nil {
				t.Fatalf("POST %s application/json has no schema", path)
			}

			bodySchema := mt.Schema.Schema()
			if bodySchema == nil {
				t.Fatalf("could not build request body schema for POST %s", path)
			}
			if bodySchema.Properties == nil {
				t.Fatalf("POST %s request body schema has no properties", path)
			}

			expiresProxy := bodySchema.Properties.GetOrZero("expires")
			if expiresProxy == nil {
				t.Fatalf("POST %s request body has no 'expires' property", path)
			}
			expiresSchema := expiresProxy.Schema()
			if expiresSchema == nil {
				t.Fatalf("could not build expires schema for POST %s", path)
			}

			// Check enum values: [0, 30, 60, 90].
			expectedEnum := []int64{0, 30, 60, 90}
			if len(expiresSchema.Enum) != len(expectedEnum) {
				t.Fatalf("POST %s expires enum: expected %d values, got %d",
					path, len(expectedEnum), len(expiresSchema.Enum))
			}
			for i, node := range expiresSchema.Enum {
				var val int64
				if err := node.Decode(&val); err != nil {
					t.Fatalf("POST %s expires enum[%d]: failed to decode: %v", path, i, err)
				}
				if val != expectedEnum[i] {
					t.Errorf("POST %s expires enum[%d]: expected %d, got %d",
						path, i, expectedEnum[i], val)
				}
			}

			// Check default value: 90.
			if expiresSchema.Default == nil {
				t.Fatalf("POST %s expires has no default value", path)
			}
			var defaultVal int64
			if err := expiresSchema.Default.Decode(&defaultVal); err != nil {
				t.Fatalf("POST %s expires default: failed to decode: %v", path, err)
			}
			if defaultVal != 90 {
				t.Errorf("POST %s expires default: expected 90, got %d", path, defaultVal)
			}

			// Check description mentions 0=permanent/null.
			desc := expiresSchema.Description
			if !strings.Contains(desc, "0") || !strings.Contains(strings.ToLower(desc), "null") {
				t.Errorf("POST %s expires description should mention '0' and 'null' for permanent semantics, got %q",
					path, desc)
			}
		})
	}
}

// TestOpenAPIExpiresAtNullable verifies that expires_at fields in all
// relevant schemas are declared nullable with format date-time.
func TestOpenAPIExpiresAtNullable(t *testing.T) {
	doc := loadSpec(t)

	if doc.Components == nil || doc.Components.Schemas == nil {
		t.Fatalf("spec has no components/schemas")
	}

	for _, name := range []string{"ApiKeyMetadata", "ApiKey", "PatMetadata", "Pat"} {
		t.Run(name, func(t *testing.T) {
			proxy := doc.Components.Schemas.GetOrZero(name)
			if proxy == nil {
				t.Fatalf("schema %s not found in components/schemas", name)
			}
			schema := proxy.Schema()
			if schema == nil {
				t.Fatalf("could not build schema for %s", name)
			}
			if schema.Properties == nil {
				t.Fatalf("schema %s has no properties", name)
			}

			eaProxy := schema.Properties.GetOrZero("expires_at")
			if eaProxy == nil {
				t.Fatalf("schema %s has no 'expires_at' property", name)
			}
			eaSchema := eaProxy.Schema()
			if eaSchema == nil {
				t.Fatalf("could not build expires_at schema for %s", name)
			}

			// In OpenAPI 3.1, nullable is expressed as type: ["string", "null"].
			// Also check the legacy nullable field for 3.0 compat.
			hasNull := false
			for _, typ := range eaSchema.Type {
				if typ == "null" {
					hasNull = true
					break
				}
			}
			if eaSchema.Nullable != nil && *eaSchema.Nullable {
				hasNull = true
			}
			if !hasNull {
				t.Errorf("schema %s expires_at should be nullable (type includes 'null' or nullable: true), got type %v",
					name, eaSchema.Type)
			}

			if eaSchema.Format != "date-time" {
				t.Errorf("schema %s expires_at format: expected 'date-time', got %q",
					name, eaSchema.Format)
			}
		})
	}
}

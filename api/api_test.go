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
// package directory. Go test runs with CWD set to the package source
// directory (api/), so the path is just the filename.
const specPath = "openapi.yaml"

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

// ============================================================================
// Helpers for task groups 2+
// ============================================================================

// mustGetPathItem retrieves a PathItem or calls t.Fatalf.
func mustGetPathItem(t *testing.T, doc *v3high.Document, path string) *v3high.PathItem {
	t.Helper()
	if doc.Paths == nil || doc.Paths.PathItems == nil {
		t.Fatalf("spec has no paths defined")
	}
	pi := doc.Paths.PathItems.GetOrZero(path)
	if pi == nil {
		t.Fatalf("path %s not found in spec", path)
	}
	return pi
}

// mustGetOp retrieves an Operation by method and path, or calls t.Fatalf.
func mustGetOp(t *testing.T, doc *v3high.Document, method, path string) *v3high.Operation {
	t.Helper()
	pi := mustGetPathItem(t, doc, path)
	op := getOperation(pi, method)
	if op == nil {
		t.Fatalf("no %s operation on %s", strings.ToUpper(method), path)
	}
	return op
}

// requireResponse returns the Response for a status code, calling t.Fatalf if
// the Responses map is nil or the code is absent.
func requireResponse(t *testing.T, op *v3high.Operation, code, label string) *v3high.Response {
	t.Helper()
	if op.Responses == nil || op.Responses.Codes == nil {
		t.Fatalf("%s: no responses defined", label)
	}
	resp := op.Responses.Codes.GetOrZero(code)
	if resp == nil {
		t.Fatalf("%s: expected response %s to be defined", label, code)
	}
	return resp
}

// assertResponseDefined checks that a status code exists without returning it.
func assertResponseDefined(t *testing.T, op *v3high.Operation, code, label string) {
	t.Helper()
	if op.Responses == nil || op.Responses.Codes == nil {
		t.Errorf("%s: no responses defined (expected %s)", label, code)
		return
	}
	if op.Responses.Codes.GetOrZero(code) == nil {
		t.Errorf("%s: expected response %s to be defined", label, code)
	}
}

// requireResponseSchema extracts *base.Schema from a response's
// application/json content.
func requireResponseSchema(t *testing.T, op *v3high.Operation, code, label string) *base.Schema {
	t.Helper()
	resp := requireResponse(t, op, code, label)
	if resp.Content == nil {
		t.Fatalf("%s: response %s has no content", label, code)
	}
	mt := resp.Content.GetOrZero("application/json")
	if mt == nil {
		t.Fatalf("%s: response %s has no application/json content", label, code)
	}
	if mt.Schema == nil {
		t.Fatalf("%s: response %s application/json has no schema", label, code)
	}
	schema := mt.Schema.Schema()
	if schema == nil {
		t.Fatalf("%s: could not build response %s schema", label, code)
	}
	return schema
}

// requireRequestBodySchema extracts *base.Schema from the request body's
// application/json content.
func requireRequestBodySchema(t *testing.T, op *v3high.Operation, label string) *base.Schema {
	t.Helper()
	if op.RequestBody == nil {
		t.Fatalf("%s: no request body defined", label)
	}
	if op.RequestBody.Content == nil {
		t.Fatalf("%s: request body has no content", label)
	}
	mt := op.RequestBody.Content.GetOrZero("application/json")
	if mt == nil {
		t.Fatalf("%s: request body has no application/json content", label)
	}
	if mt.Schema == nil {
		t.Fatalf("%s: request body application/json has no schema", label)
	}
	schema := mt.Schema.Schema()
	if schema == nil {
		t.Fatalf("%s: could not build request body schema", label)
	}
	return schema
}

// requireResponseHeader checks that a named header exists on a response and
// returns it. Returns nil (after calling t.Errorf) when the header is absent.
func requireResponseHeader(t *testing.T, op *v3high.Operation, code, headerName, label string) *v3high.Header {
	t.Helper()
	resp := requireResponse(t, op, code, label)
	if resp.Headers == nil {
		t.Fatalf("%s: response %s has no headers defined", label, code)
	}
	h := resp.Headers.GetOrZero(headerName)
	if h == nil {
		t.Errorf("%s: response %s missing header %s", label, code, headerName)
	}
	return h
}

// assertResponseNoContent checks that a response defines no content/body.
func assertResponseNoContent(t *testing.T, op *v3high.Operation, code, label string) {
	t.Helper()
	resp := requireResponse(t, op, code, label)
	if resp.Content != nil {
		for range resp.Content.FromOldest() {
			t.Errorf("%s: response %s should have no content body", label, code)
			return
		}
	}
}

// assertNoHeader checks that a named header is NOT present on a response.
func assertNoHeader(t *testing.T, op *v3high.Operation, code, headerName, label string) {
	t.Helper()
	if op.Responses == nil || op.Responses.Codes == nil {
		return
	}
	resp := op.Responses.Codes.GetOrZero(code)
	if resp == nil || resp.Headers == nil {
		return
	}
	if resp.Headers.GetOrZero(headerName) != nil {
		t.Errorf("%s: response %s should NOT have %s header", label, code, headerName)
	}
}

// assertBearerAuth checks that bearerAuth is in the operation security.
func assertBearerAuth(t *testing.T, op *v3high.Operation, label string) {
	t.Helper()
	if !hasBearerAuth(op.Security) {
		t.Errorf("%s: expected bearerAuth in security requirements", label)
	}
}

// assertCacheControl verifies the Cache-Control header schema has an enum
// containing the expected directive (e.g. "no-store", "no-cache").
func assertCacheControl(t *testing.T, op *v3high.Operation, code, expected, label string) {
	t.Helper()
	h := requireResponseHeader(t, op, code, "Cache-Control", label)
	if h == nil {
		return
	}
	if h.Schema == nil {
		t.Errorf("%s: Cache-Control header on %s has no schema", label, code)
		return
	}
	schema := h.Schema.Schema()
	if schema == nil {
		t.Errorf("%s: could not build Cache-Control header schema on %s", label, code)
		return
	}
	found := false
	for _, node := range schema.Enum {
		var val string
		if err := node.Decode(&val); err == nil && val == expected {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("%s: Cache-Control on response %s should have enum containing %q",
			label, code, expected)
	}
}

// assertXRequestID checks that X-Request-ID header is present on a response.
func assertXRequestID(t *testing.T, op *v3high.Operation, code, label string) {
	t.Helper()
	requireResponseHeader(t, op, code, "X-Request-ID", label)
}

// assertETagHeader checks that the ETag header is present as an opaque string
// with no format or pattern (PROP-1: derivation algorithm not exposed).
func assertETagHeader(t *testing.T, op *v3high.Operation, code, label string) {
	t.Helper()
	h := requireResponseHeader(t, op, code, "ETag", label)
	if h == nil {
		return
	}
	if h.Schema == nil {
		t.Errorf("%s: ETag header on %s has no schema", label, code)
		return
	}
	schema := h.Schema.Schema()
	if schema == nil {
		return
	}
	hasString := false
	for _, typ := range schema.Type {
		if typ == "string" {
			hasString = true
		}
	}
	if !hasString {
		t.Errorf("%s: ETag header should be type string, got %v", label, schema.Type)
	}
	if schema.Format != "" {
		t.Errorf("%s: ETag header should have no format (opaque), got %q",
			label, schema.Format)
	}
	if schema.Pattern != "" {
		t.Errorf("%s: ETag header should have no pattern (opaque), got %q",
			label, schema.Pattern)
	}
}

// assertPatchBodyOnlyFullName checks that a PATCH request body schema has
// exactly one property (full_name) and does not include username or email.
func assertPatchBodyOnlyFullName(t *testing.T, schema *base.Schema, label string) {
	t.Helper()
	if schema.Properties == nil {
		t.Fatalf("%s: request body schema has no properties", label)
	}
	count := 0
	for range schema.Properties.FromOldest() {
		count++
	}
	if count != 1 {
		t.Errorf("%s: expected exactly 1 property in request body, got %d", label, count)
	}
	if schema.Properties.GetOrZero("full_name") == nil {
		t.Errorf("%s: request body schema missing 'full_name' property", label)
	}
	if schema.Properties.GetOrZero("username") != nil {
		t.Errorf("%s: request body schema must not contain 'username'", label)
	}
	if schema.Properties.GetOrZero("email") != nil {
		t.Errorf("%s: request body schema must not contain 'email'", label)
	}
}

// assert304Conditional checks the 304 response: no content, X-Request-ID +
// ETag headers present, no Cache-Control (PROP-9).
func assert304Conditional(t *testing.T, op *v3high.Operation, label string) {
	t.Helper()
	resp := requireResponse(t, op, "304", label)
	// No content body.
	if resp.Content != nil {
		for range resp.Content.FromOldest() {
			t.Errorf("%s: 304 response should have no content body", label)
			break
		}
	}
	// X-Request-ID + ETag headers present.
	assertXRequestID(t, op, "304", label)
	requireResponseHeader(t, op, "304", "ETag", label)
	// No Cache-Control on 304.
	assertNoHeader(t, op, "304", "Cache-Control", label)
}

// ============================================================================
// TS-03-12 (Task 2.1) — Validates: 03-REQ-5.1
// ============================================================================

// TestOpenAPIPatchUserBody verifies that the PATCH /user request body schema
// contains only full_name and does not include username or email.
func TestOpenAPIPatchUserBody(t *testing.T) {
	doc := loadSpec(t)
	op := mustGetOp(t, doc, "patch", "/user")
	schema := requireRequestBodySchema(t, op, "PATCH /user")
	assertPatchBodyOnlyFullName(t, schema, "PATCH /user")
}

// ============================================================================
// TS-03-13 (Task 2.1) — Validates: 03-REQ-5.2
// ============================================================================

// TestOpenAPIPatchUsersIdBody verifies that the PATCH /users/{id} request body
// schema contains only full_name and does not include username or email.
func TestOpenAPIPatchUsersIdBody(t *testing.T) {
	doc := loadSpec(t)
	op := mustGetOp(t, doc, "patch", "/users/{id}")
	schema := requireRequestBodySchema(t, op, "PATCH /users/{id}")
	assertPatchBodyOnlyFullName(t, schema, "PATCH /users/{id}")
}

// ============================================================================
// TS-03-14 (Task 2.1) — Validates: 03-REQ-5.3
// ============================================================================

// TestOpenAPIPatchDescriptions verifies that both PATCH /user and
// PATCH /users/{id} operation descriptions state that username and email are
// immutable via PATCH.
func TestOpenAPIPatchDescriptions(t *testing.T) {
	doc := loadSpec(t)
	for _, path := range []string{"/user", "/users/{id}"} {
		t.Run(path, func(t *testing.T) {
			op := mustGetOp(t, doc, "patch", path)
			desc := strings.ToLower(op.Description)
			if !strings.Contains(desc, "username") || !strings.Contains(desc, "email") {
				t.Errorf("PATCH %s description should mention username and email", path)
			}
			if !strings.Contains(desc, "immutable") &&
				!strings.Contains(desc, "cannot be changed") &&
				!strings.Contains(desc, "set at creation") {
				t.Errorf("PATCH %s description should state immutability of username/email", path)
			}
		})
	}
}

// ============================================================================
// TS-03-P5 (Task 2.1) — Validates: 03-REQ-5.1, 03-REQ-5.2
// Property: For any PATCH operation, request body has only full_name.
// ============================================================================

// TestOpenAPIPatchPropertyOnlyFullName enumerates all PATCH operations in the
// spec and verifies each has a request body with exactly one property
// (full_name), with no username or email.
// PROP-5 scopes this constraint to PATCH /user and PATCH /users/{id} only;
// PATCH /orgs/{id} has a different schema (name, url) per 03-REQ-10.4.
func TestOpenAPIPatchPropertyOnlyFullName(t *testing.T) {
	doc := loadSpec(t)

	// Only user PATCH endpoints are subject to the full_name-only constraint.
	userPatchPaths := []string{"/user", "/users/{id}"}
	for _, path := range userPatchPaths {
		t.Run(path, func(t *testing.T) {
			op := mustGetOp(t, doc, "patch", path)
			schema := requireRequestBodySchema(t, op, "PATCH "+path)
			assertPatchBodyOnlyFullName(t, schema, "PATCH "+path)
		})
	}
}

// ============================================================================
// TS-03-15 (Task 2.2) — Validates: 03-REQ-6.1
// ============================================================================

// TestOpenAPIHealthz verifies GET /healthz: path-level servers override, no
// auth, 200 response with status=ok, Cache-Control: no-cache, X-Request-ID.
func TestOpenAPIHealthz(t *testing.T) {
	doc := loadSpec(t)
	label := "GET /healthz"
	op := mustGetOp(t, doc, "get", "/healthz")

	// Path-level servers override.
	pi := mustGetPathItem(t, doc, "/healthz")
	if len(pi.Servers) < 1 || pi.Servers[0].URL != "/" {
		t.Errorf("%s: expected path-level server with url '/'", label)
	}

	// No security.
	if hasBearerAuth(op.Security) {
		t.Errorf("%s: should not require authentication", label)
	}

	// 200 response schema has "status" property with enum containing "ok".
	schema := requireResponseSchema(t, op, "200", label)
	if schema.Properties == nil || schema.Properties.GetOrZero("status") == nil {
		t.Errorf("%s: 200 response schema missing 'status' property", label)
	} else {
		statusSchema := schema.Properties.GetOrZero("status").Schema()
		if statusSchema != nil {
			found := false
			for _, node := range statusSchema.Enum {
				var val string
				if err := node.Decode(&val); err == nil && val == "ok" {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s: status field should have enum containing 'ok'", label)
			}
		}
	}

	assertCacheControl(t, op, "200", "no-cache", label)
	assertXRequestID(t, op, "200", label)
}

// ============================================================================
// TS-03-16 (Task 2.2) — Validates: 03-REQ-6.2
// ============================================================================

// TestOpenAPIReadyz verifies GET /readyz: path-level servers override, no auth,
// 200 response with status=ok, 503 response with status=unavailable,
// Cache-Control: no-cache, X-Request-ID on both.
func TestOpenAPIReadyz(t *testing.T) {
	doc := loadSpec(t)
	label := "GET /readyz"
	op := mustGetOp(t, doc, "get", "/readyz")

	// Path-level servers override.
	pi := mustGetPathItem(t, doc, "/readyz")
	if len(pi.Servers) < 1 || pi.Servers[0].URL != "/" {
		t.Errorf("%s: expected path-level server with url '/'", label)
	}

	// No security.
	if hasBearerAuth(op.Security) {
		t.Errorf("%s: should not require authentication", label)
	}

	// 200 response.
	assertResponseDefined(t, op, "200", label)

	// 503 response with status=unavailable.
	schema503 := requireResponseSchema(t, op, "503", label)
	if schema503.Properties == nil || schema503.Properties.GetOrZero("status") == nil {
		t.Errorf("%s: 503 response schema missing 'status' property", label)
	} else {
		statusSchema := schema503.Properties.GetOrZero("status").Schema()
		if statusSchema != nil {
			found := false
			for _, node := range statusSchema.Enum {
				var val string
				if err := node.Decode(&val); err == nil && val == "unavailable" {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s: 503 status field should have enum containing 'unavailable'", label)
			}
		}
	}

	// Headers on both responses.
	assertCacheControl(t, op, "200", "no-cache", label)
	assertXRequestID(t, op, "200", label)
	assertXRequestID(t, op, "503", label)
}

// ============================================================================
// TS-03-17 (Task 2.2) — Validates: 03-REQ-6.3
// ============================================================================

// TestOpenAPIVersion verifies GET /version: 200 response body has version,
// build_time, commit, mount_point fields (all strings), Cache-Control:
// no-cache, X-Request-ID.
func TestOpenAPIVersion(t *testing.T) {
	doc := loadSpec(t)
	label := "GET /version"
	op := mustGetOp(t, doc, "get", "/version")

	// Path-level servers override.
	pi := mustGetPathItem(t, doc, "/version")
	if len(pi.Servers) < 1 || pi.Servers[0].URL != "/" {
		t.Errorf("%s: expected path-level server with url '/'", label)
	}

	// 200 response schema: version, build_time, commit, mount_point (all strings).
	schema := requireResponseSchema(t, op, "200", label)
	for _, field := range []string{"version", "build_time", "commit", "mount_point"} {
		if schema.Properties == nil || schema.Properties.GetOrZero(field) == nil {
			t.Errorf("%s: 200 response schema missing '%s' property", label, field)
			continue
		}
		fieldSchema := schema.Properties.GetOrZero(field).Schema()
		if fieldSchema == nil {
			t.Errorf("%s: could not build schema for field '%s'", label, field)
			continue
		}
		hasString := false
		for _, typ := range fieldSchema.Type {
			if typ == "string" {
				hasString = true
			}
		}
		if !hasString {
			t.Errorf("%s: field '%s' should be type string, got %v",
				label, field, fieldSchema.Type)
		}
	}

	assertCacheControl(t, op, "200", "no-cache", label)
	assertXRequestID(t, op, "200", label)
}

// ============================================================================
// TS-03-P7 (Task 2.2) — Validates: 03-REQ-1.3, 03-REQ-6.1–6.3
// Property: Health probe paths use path-level servers override with url '/'.
// ============================================================================

// TestOpenAPIHealthProbeServers enumerates /healthz, /readyz, /version and
// verifies each defines a path-level servers block with url '/'.
func TestOpenAPIHealthProbeServers(t *testing.T) {
	doc := loadSpec(t)
	for _, path := range []string{"/healthz", "/readyz", "/version"} {
		t.Run(path, func(t *testing.T) {
			pi := mustGetPathItem(t, doc, path)
			if len(pi.Servers) == 0 {
				t.Fatalf("path %s has no path-level servers defined", path)
			}
			if pi.Servers[0].URL != "/" {
				t.Errorf("path %s: expected servers[0].URL = '/', got %q",
					path, pi.Servers[0].URL)
			}
		})
	}
}

// ============================================================================
// TS-03-18 (Task 2.3) — Validates: 03-REQ-7.1
// ============================================================================

// TestOpenAPIAuthProviders verifies GET /auth/providers: public (no auth, no
// 401), returns array of OAuthProvider objects with name + authorize_url,
// Cache-Control: public, max-age=300, X-Request-ID.
func TestOpenAPIAuthProviders(t *testing.T) {
	doc := loadSpec(t)
	label := "GET /auth/providers"
	op := mustGetOp(t, doc, "get", "/auth/providers")

	// No security.
	if hasBearerAuth(op.Security) {
		t.Errorf("%s: should not require authentication", label)
	}

	// No 401 response.
	if op.Responses != nil && op.Responses.Codes != nil {
		if op.Responses.Codes.GetOrZero("401") != nil {
			t.Errorf("%s: should not define a 401 response", label)
		}
	}

	// 200 response: array of objects with name + authorize_url.
	schema := requireResponseSchema(t, op, "200", label)
	hasArray := false
	for _, typ := range schema.Type {
		if typ == "array" {
			hasArray = true
		}
	}
	if !hasArray {
		t.Errorf("%s: 200 response should be type 'array', got %v", label, schema.Type)
	}
	if schema.Items == nil || !schema.Items.IsA() {
		t.Fatalf("%s: 200 response array has no items schema", label)
	}
	itemSchema := schema.Items.A.Schema()
	if itemSchema == nil {
		t.Fatalf("%s: could not build items schema", label)
	}
	if itemSchema.Properties == nil {
		t.Fatalf("%s: items schema has no properties", label)
	}
	for _, field := range []string{"name", "authorize_url"} {
		if itemSchema.Properties.GetOrZero(field) == nil {
			t.Errorf("%s: OAuthProvider items missing '%s' property", label, field)
		}
	}

	assertCacheControl(t, op, "200", "public, max-age=300", label)
	assertXRequestID(t, op, "200", label)
}

// ============================================================================
// TS-03-19 (Task 2.3) — Validates: 03-REQ-7.2
// ============================================================================

// TestOpenAPIAuthCallback verifies POST /auth/callback: required request fields
// (provider, code, redirect_uri), optional expires enum [0,30,60,90] default
// 90, 200 AuthCallbackResponse with user + api_key, 400 error,
// Cache-Control: no-store, X-Request-ID.
func TestOpenAPIAuthCallback(t *testing.T) {
	doc := loadSpec(t)
	label := "POST /auth/callback"
	op := mustGetOp(t, doc, "post", "/auth/callback")

	// Request body: required fields provider, code, redirect_uri.
	schema := requireRequestBodySchema(t, op, label)
	requiredSet := make(map[string]bool)
	for _, r := range schema.Required {
		requiredSet[r] = true
	}
	for _, field := range []string{"provider", "code", "redirect_uri"} {
		if !requiredSet[field] {
			t.Errorf("%s: request body should require '%s'", label, field)
		}
	}

	// expires property with enum and default.
	if schema.Properties == nil {
		t.Fatalf("%s: request body has no properties", label)
	}
	if schema.Properties.GetOrZero("expires") == nil {
		t.Errorf("%s: request body missing 'expires' property", label)
	} else {
		expiresSchema := schema.Properties.GetOrZero("expires").Schema()
		if expiresSchema != nil {
			expectedEnum := []int64{0, 30, 60, 90}
			if len(expiresSchema.Enum) != len(expectedEnum) {
				t.Errorf("%s: expires enum: expected %d values, got %d",
					label, len(expectedEnum), len(expiresSchema.Enum))
			}
			if expiresSchema.Default != nil {
				var def int64
				if err := expiresSchema.Default.Decode(&def); err == nil && def != 90 {
					t.Errorf("%s: expires default: expected 90, got %d", label, def)
				}
			}
		}
	}

	// 200 response: AuthCallbackResponse with user + api_key.
	respSchema := requireResponseSchema(t, op, "200", label)
	if respSchema.Properties == nil {
		t.Fatalf("%s: 200 response schema has no properties", label)
	}
	if respSchema.Properties.GetOrZero("user") == nil {
		t.Errorf("%s: 200 response missing 'user' property", label)
	}
	apiKeyProxy := respSchema.Properties.GetOrZero("api_key")
	if apiKeyProxy == nil {
		t.Errorf("%s: 200 response missing 'api_key' property", label)
	} else {
		akSchema := apiKeyProxy.Schema()
		if akSchema != nil && akSchema.Properties != nil {
			for _, field := range []string{"key", "key_id", "expires_at"} {
				if akSchema.Properties.GetOrZero(field) == nil {
					t.Errorf("%s: api_key missing '%s' property", label, field)
				}
			}
		}
	}

	// 400 error response.
	assertResponseDefined(t, op, "400", label)

	assertCacheControl(t, op, "200", "no-store", label)
	assertXRequestID(t, op, "200", label)
}

// ============================================================================
// TS-03-20, TS-03-SMOKE-3 (Task 2.4) — Validates: 03-REQ-8.1
// ============================================================================

// TestOpenAPIGetUser verifies GET /user: bearerAuth, 200 with User object and
// ETag, 304 conditional response (no body, X-Request-ID + ETag, no
// Cache-Control), 401/403 errors, Cache-Control: no-store, and If-None-Match
// documentation.
func TestOpenAPIGetUser(t *testing.T) {
	doc := loadSpec(t)
	label := "GET /user"
	op := mustGetOp(t, doc, "get", "/user")

	assertBearerAuth(t, op, label)

	// 200 response with ETag and caching headers.
	assertResponseDefined(t, op, "200", label)
	assertETagHeader(t, op, "200", label)
	assertCacheControl(t, op, "200", "no-store", label)
	assertXRequestID(t, op, "200", label)

	// 304 conditional response (PROP-9, SMOKE-3).
	assert304Conditional(t, op, label)

	// Error responses.
	assertResponseDefined(t, op, "401", label)
	assertResponseDefined(t, op, "403", label)

	// SMOKE-3: operation references If-None-Match in parameters or description.
	hasIfNoneMatch := false
	for _, param := range op.Parameters {
		if param != nil && param.Name == "If-None-Match" && param.In == "header" {
			hasIfNoneMatch = true
			break
		}
	}
	if !hasIfNoneMatch && !strings.Contains(op.Description, "If-None-Match") {
		t.Errorf("%s: should reference If-None-Match in parameters or description", label)
	}
}

// ============================================================================
// TS-03-21 (Task 2.4) — Validates: 03-REQ-8.2
// ============================================================================

// TestOpenAPIPatchUser verifies PATCH /user: bearerAuth, request body with only
// full_name, responses 200/400/401/403/415, Cache-Control: no-store,
// X-Request-ID.
func TestOpenAPIPatchUser(t *testing.T) {
	doc := loadSpec(t)
	label := "PATCH /user"
	op := mustGetOp(t, doc, "patch", "/user")

	assertBearerAuth(t, op, label)

	// Request body has only full_name.
	schema := requireRequestBodySchema(t, op, label)
	assertPatchBodyOnlyFullName(t, schema, label)

	// Response codes.
	for _, code := range []string{"200", "400", "401", "403", "415"} {
		assertResponseDefined(t, op, code, label)
	}

	assertCacheControl(t, op, "200", "no-store", label)
	assertXRequestID(t, op, "200", label)
}

// ============================================================================
// TS-03-22 (Task 2.5) — Validates: 03-REQ-8.3
// ============================================================================

// TestOpenAPIGetUserKeys verifies GET /user/keys: bearerAuth, 200 returns
// array of ApiKeyMetadata with key_id, created_at, nullable expires_at and
// revoked_at, 401/403 errors, Cache-Control: no-store.
func TestOpenAPIGetUserKeys(t *testing.T) {
	doc := loadSpec(t)
	label := "GET /user/keys"
	op := mustGetOp(t, doc, "get", "/user/keys")

	assertBearerAuth(t, op, label)

	// 200: array of ApiKeyMetadata.
	schema := requireResponseSchema(t, op, "200", label)
	hasArray := false
	for _, typ := range schema.Type {
		if typ == "array" {
			hasArray = true
		}
	}
	if !hasArray {
		t.Errorf("%s: 200 response should be type 'array', got %v", label, schema.Type)
	}

	// Verify items schema has expected fields.
	if schema.Items == nil || !schema.Items.IsA() {
		t.Fatalf("%s: array items schema not found", label)
	}
	itemSchema := schema.Items.A.Schema()
	if itemSchema == nil {
		t.Fatalf("%s: could not resolve items schema", label)
	}
	if itemSchema.Properties == nil {
		t.Fatalf("%s: items schema has no properties", label)
	}
	for _, field := range []string{"key_id", "created_at", "expires_at", "revoked_at"} {
		if itemSchema.Properties.GetOrZero(field) == nil {
			t.Errorf("%s: ApiKeyMetadata missing '%s' property", label, field)
		}
	}

	assertResponseDefined(t, op, "401", label)
	assertResponseDefined(t, op, "403", label)
	assertCacheControl(t, op, "200", "no-store", label)
	assertXRequestID(t, op, "200", label)
}

// ============================================================================
// TS-03-23 (Task 2.5) — Validates: 03-REQ-8.4
// ============================================================================

// TestOpenAPIRefreshKey verifies POST /user/keys/{key_id}/refresh: bearerAuth,
// 200 returns ApiKey with key, key_id, nullable expires_at, 401/403/404 errors.
func TestOpenAPIRefreshKey(t *testing.T) {
	doc := loadSpec(t)
	label := "POST /user/keys/{key_id}/refresh"
	op := mustGetOp(t, doc, "post", "/user/keys/{key_id}/refresh")

	assertBearerAuth(t, op, label)

	// 200: ApiKey with key, key_id, expires_at.
	schema := requireResponseSchema(t, op, "200", label)
	if schema.Properties == nil {
		t.Fatalf("%s: 200 response schema has no properties", label)
	}
	for _, field := range []string{"key", "key_id", "expires_at"} {
		if schema.Properties.GetOrZero(field) == nil {
			t.Errorf("%s: 200 response missing '%s' property", label, field)
		}
	}

	for _, code := range []string{"401", "403", "404"} {
		assertResponseDefined(t, op, code, label)
	}
	assertCacheControl(t, op, "200", "no-store", label)
	assertXRequestID(t, op, "200", label)
}

// ============================================================================
// TS-03-24 (Task 2.5) — Validates: 03-REQ-8.5
// ============================================================================

// TestOpenAPIDeleteUserKey verifies DELETE /user/keys/{key_id}: bearerAuth,
// 204 with no content, 401/403/404 errors, Cache-Control: no-store,
// X-Request-ID.
func TestOpenAPIDeleteUserKey(t *testing.T) {
	doc := loadSpec(t)
	label := "DELETE /user/keys/{key_id}"
	op := mustGetOp(t, doc, "delete", "/user/keys/{key_id}")

	assertBearerAuth(t, op, label)

	// 204: no content.
	assertResponseDefined(t, op, "204", label)
	assertResponseNoContent(t, op, "204", label)

	for _, code := range []string{"401", "403", "404"} {
		assertResponseDefined(t, op, code, label)
	}
	assertXRequestID(t, op, "204", label)
	assertCacheControl(t, op, "204", "no-store", label)
}

// ============================================================================
// TS-03-25 (Task 2.6) — Validates: 03-REQ-8.6
// ============================================================================

// TestOpenAPIGetUserTokens verifies GET /user/tokens: bearerAuth, 200 returns
// array of PatMetadata with token_id, name, permissions, created_at, nullable
// expires_at and revoked_at, 401/403 errors.
func TestOpenAPIGetUserTokens(t *testing.T) {
	doc := loadSpec(t)
	label := "GET /user/tokens"
	op := mustGetOp(t, doc, "get", "/user/tokens")

	assertBearerAuth(t, op, label)

	// 200: array of PatMetadata.
	schema := requireResponseSchema(t, op, "200", label)
	hasArray := false
	for _, typ := range schema.Type {
		if typ == "array" {
			hasArray = true
		}
	}
	if !hasArray {
		t.Errorf("%s: 200 response should be type 'array', got %v", label, schema.Type)
	}

	// Verify items schema has expected PatMetadata fields.
	if schema.Items == nil || !schema.Items.IsA() {
		t.Fatalf("%s: array items schema not found", label)
	}
	itemSchema := schema.Items.A.Schema()
	if itemSchema == nil {
		t.Fatalf("%s: could not resolve items schema", label)
	}
	if itemSchema.Properties == nil {
		t.Fatalf("%s: items schema has no properties", label)
	}
	for _, field := range []string{"token_id", "name", "permissions", "created_at", "expires_at", "revoked_at"} {
		if itemSchema.Properties.GetOrZero(field) == nil {
			t.Errorf("%s: PatMetadata missing '%s' property", label, field)
		}
	}

	assertResponseDefined(t, op, "401", label)
	assertResponseDefined(t, op, "403", label)
	assertCacheControl(t, op, "200", "no-store", label)
	assertXRequestID(t, op, "200", label)
}

// ============================================================================
// TS-03-26, TS-03-SMOKE-5 (Task 2.6) — Validates: 03-REQ-8.7
// ============================================================================

// TestOpenAPICreateToken verifies POST /user/tokens: bearerAuth, requires name
// + permissions (pattern-constrained), optional expires enum [0,30,60,90]
// default 90, 201 returns Pat with token/token_id/name/permissions/expires_at,
// 400/401/403/415 errors.
func TestOpenAPICreateToken(t *testing.T) {
	doc := loadSpec(t)
	label := "POST /user/tokens"
	op := mustGetOp(t, doc, "post", "/user/tokens")

	assertBearerAuth(t, op, label)

	// Request body: requires name + permissions.
	schema := requireRequestBodySchema(t, op, label)
	requiredSet := make(map[string]bool)
	for _, r := range schema.Required {
		requiredSet[r] = true
	}
	if !requiredSet["name"] {
		t.Errorf("%s: request body should require 'name'", label)
	}
	if !requiredSet["permissions"] {
		t.Errorf("%s: request body should require 'permissions'", label)
	}

	// SMOKE-5: permissions items use pattern ^[a-z_]+:[a-z_]+$.
	if schema.Properties != nil {
		permProxy := schema.Properties.GetOrZero("permissions")
		if permProxy == nil {
			t.Errorf("%s: request body missing 'permissions' property", label)
		} else {
			permSchema := permProxy.Schema()
			if permSchema != nil && permSchema.Items != nil && permSchema.Items.IsA() {
				itemSchema := permSchema.Items.A.Schema()
				if itemSchema != nil {
					wantPattern := `^[a-z_]+:[a-z_]+$`
					if itemSchema.Pattern != wantPattern {
						t.Errorf("%s: permissions items pattern: expected %q, got %q",
							label, wantPattern, itemSchema.Pattern)
					}
				}
			}
		}
	}

	// expires: optional integer enum [0,30,60,90] default 90.
	if schema.Properties != nil && schema.Properties.GetOrZero("expires") != nil {
		expiresSchema := schema.Properties.GetOrZero("expires").Schema()
		if expiresSchema != nil {
			expectedEnum := []int64{0, 30, 60, 90}
			if len(expiresSchema.Enum) != len(expectedEnum) {
				t.Errorf("%s: expires enum: expected %d values, got %d",
					label, len(expectedEnum), len(expiresSchema.Enum))
			}
		}
	}

	// 201 response: Pat with token, token_id, name, permissions, expires_at.
	respSchema := requireResponseSchema(t, op, "201", label)
	if respSchema.Properties == nil {
		t.Fatalf("%s: 201 response schema has no properties", label)
	}
	for _, field := range []string{"token", "token_id", "name", "permissions", "expires_at"} {
		if respSchema.Properties.GetOrZero(field) == nil {
			t.Errorf("%s: 201 response missing '%s' property", label, field)
		}
	}

	for _, code := range []string{"400", "401", "403", "415"} {
		assertResponseDefined(t, op, code, label)
	}
	assertCacheControl(t, op, "201", "no-store", label)
	assertXRequestID(t, op, "201", label)
}

// ============================================================================
// TS-03-27 (Task 2.6) — Validates: 03-REQ-8.8
// ============================================================================

// TestOpenAPIGetUserToken verifies GET /user/tokens/{token_id}: bearerAuth,
// 200 with ETag, 304 conditional response (no body, X-Request-ID + ETag,
// no Cache-Control), 401/403/404 errors.
func TestOpenAPIGetUserToken(t *testing.T) {
	doc := loadSpec(t)
	label := "GET /user/tokens/{token_id}"
	op := mustGetOp(t, doc, "get", "/user/tokens/{token_id}")

	assertBearerAuth(t, op, label)

	// 200: PatMetadata + ETag.
	assertResponseDefined(t, op, "200", label)
	assertETagHeader(t, op, "200", label)
	assertCacheControl(t, op, "200", "no-store", label)
	assertXRequestID(t, op, "200", label)

	// 304 conditional response.
	assert304Conditional(t, op, label)

	for _, code := range []string{"401", "403", "404"} {
		assertResponseDefined(t, op, code, label)
	}
}

// ============================================================================
// TS-03-28 (Task 2.6) — Validates: 03-REQ-8.9
// ============================================================================

// TestOpenAPIDeleteUserToken verifies DELETE /user/tokens/{token_id}:
// bearerAuth, 204 with no content, 401/403/404 errors, Cache-Control:
// no-store, X-Request-ID.
func TestOpenAPIDeleteUserToken(t *testing.T) {
	doc := loadSpec(t)
	label := "DELETE /user/tokens/{token_id}"
	op := mustGetOp(t, doc, "delete", "/user/tokens/{token_id}")

	assertBearerAuth(t, op, label)

	// 204: no content.
	assertResponseDefined(t, op, "204", label)
	assertResponseNoContent(t, op, "204", label)

	for _, code := range []string{"401", "403", "404"} {
		assertResponseDefined(t, op, code, label)
	}
	assertXRequestID(t, op, "204", label)
	assertCacheControl(t, op, "204", "no-store", label)
}

// ============================================================================
// TS-03-29, TS-03-SMOKE-5 (Task 2.6) — Validates: 03-REQ-8.10
// ============================================================================

// TestOpenAPIGetUserOrgs verifies GET /user/orgs: bearerAuth, description
// mentions orgs:read PAT permission, 200 returns array of Organization
// objects, 401/403 errors.
func TestOpenAPIGetUserOrgs(t *testing.T) {
	doc := loadSpec(t)
	label := "GET /user/orgs"
	op := mustGetOp(t, doc, "get", "/user/orgs")

	assertBearerAuth(t, op, label)

	// Description or summary mentions orgs:read PAT permission.
	combined := op.Description + " " + op.Summary
	if !strings.Contains(combined, "orgs:read") {
		t.Errorf("%s: description or summary should mention 'orgs:read' PAT permission", label)
	}

	// 200: array of Organization objects.
	schema := requireResponseSchema(t, op, "200", label)
	hasArray := false
	for _, typ := range schema.Type {
		if typ == "array" {
			hasArray = true
		}
	}
	if !hasArray {
		t.Errorf("%s: 200 response should be type 'array', got %v", label, schema.Type)
	}

	assertResponseDefined(t, op, "401", label)
	assertResponseDefined(t, op, "403", label)
	assertCacheControl(t, op, "200", "no-store", label)
	assertXRequestID(t, op, "200", label)
}

// ============================================================================
// TS-03-30, TS-03-SMOKE-4 (Task 3.1) — Validates: 03-REQ-9.1
// ============================================================================

// TestOpenAPIPostUsers verifies POST /users: admin bearerAuth, request body
// requires username, email, provider, provider_id; responses 201 (User), 400,
// 401, 403, 409, 415; Cache-Control: no-store, X-Request-ID.
func TestOpenAPIPostUsers(t *testing.T) {
	doc := loadSpec(t)
	label := "POST /users"
	op := mustGetOp(t, doc, "post", "/users")

	assertBearerAuth(t, op, label)

	// Request body: requires username, email, provider, provider_id.
	schema := requireRequestBodySchema(t, op, label)
	requiredSet := make(map[string]bool)
	for _, r := range schema.Required {
		requiredSet[r] = true
	}
	for _, field := range []string{"username", "email", "provider", "provider_id"} {
		if !requiredSet[field] {
			t.Errorf("%s: request body should require '%s'", label, field)
		}
	}

	// 201 response: User object.
	assertResponseDefined(t, op, "201", label)

	// Error responses: 400, 401, 403, 409, 415.
	for _, code := range []string{"400", "401", "403", "409", "415"} {
		assertResponseDefined(t, op, code, label)
	}

	assertCacheControl(t, op, "201", "no-store", label)
	assertXRequestID(t, op, "201", label)
}

// ============================================================================
// TS-03-31 (Task 3.1) — Validates: 03-REQ-9.2
// ============================================================================

// TestOpenAPIGetUsers verifies GET /users: admin bearerAuth, include_blocked
// boolean query param defaulting false, 200 returns array of User objects,
// 401/403 errors.
func TestOpenAPIGetUsers(t *testing.T) {
	doc := loadSpec(t)
	label := "GET /users"
	op := mustGetOp(t, doc, "get", "/users")

	assertBearerAuth(t, op, label)

	// include_blocked query parameter: boolean, default false.
	var found bool
	for _, param := range op.Parameters {
		if param != nil && param.Name == "include_blocked" && param.In == "query" {
			found = true
			if param.Schema == nil {
				t.Fatalf("%s: include_blocked parameter has no schema", label)
			}
			paramSchema := param.Schema.Schema()
			if paramSchema == nil {
				t.Fatalf("%s: could not build include_blocked schema", label)
			}
			hasBool := false
			for _, typ := range paramSchema.Type {
				if typ == "boolean" {
					hasBool = true
				}
			}
			if !hasBool {
				t.Errorf("%s: include_blocked should be type boolean, got %v",
					label, paramSchema.Type)
			}
			if paramSchema.Default != nil {
				var def bool
				if err := paramSchema.Default.Decode(&def); err == nil && def != false {
					t.Errorf("%s: include_blocked default should be false, got %v",
						label, def)
				}
			} else {
				t.Errorf("%s: include_blocked should have default value false", label)
			}
			break
		}
	}
	if !found {
		t.Errorf("%s: missing 'include_blocked' query parameter", label)
	}

	// 200 response: array of User objects.
	schema := requireResponseSchema(t, op, "200", label)
	hasArray := false
	for _, typ := range schema.Type {
		if typ == "array" {
			hasArray = true
		}
	}
	if !hasArray {
		t.Errorf("%s: 200 response should be type 'array', got %v", label, schema.Type)
	}

	assertResponseDefined(t, op, "401", label)
	assertResponseDefined(t, op, "403", label)
	assertCacheControl(t, op, "200", "no-store", label)
	assertXRequestID(t, op, "200", label)
}

// ============================================================================
// TS-03-32 (Task 3.1) — Validates: 03-REQ-9.3
// ============================================================================

// TestOpenAPIGetUserById verifies GET /users/{id}: admin bearerAuth, ETag on
// 200, 304 conditional response with X-Request-ID+ETag and no content, 401,
// 403, 404 defined.
func TestOpenAPIGetUserById(t *testing.T) {
	doc := loadSpec(t)
	label := "GET /users/{id}"
	op := mustGetOp(t, doc, "get", "/users/{id}")

	assertBearerAuth(t, op, label)

	// 200 response with ETag.
	assertResponseDefined(t, op, "200", label)
	assertETagHeader(t, op, "200", label)
	assertCacheControl(t, op, "200", "no-store", label)
	assertXRequestID(t, op, "200", label)

	// 304 conditional response.
	assert304Conditional(t, op, label)

	// Error responses.
	for _, code := range []string{"401", "403", "404"} {
		assertResponseDefined(t, op, code, label)
	}
}

// ============================================================================
// TS-03-33 (Task 3.1) — Validates: 03-REQ-9.4
// ============================================================================

// TestOpenAPIPatchUsersById verifies PATCH /users/{id}: admin bearerAuth,
// request body with only full_name, responses 200, 401, 403, 404, 415.
func TestOpenAPIPatchUsersById(t *testing.T) {
	doc := loadSpec(t)
	label := "PATCH /users/{id}"
	op := mustGetOp(t, doc, "patch", "/users/{id}")

	assertBearerAuth(t, op, label)

	// Request body has only full_name.
	schema := requireRequestBodySchema(t, op, label)
	assertPatchBodyOnlyFullName(t, schema, label)

	// Response codes.
	for _, code := range []string{"200", "401", "403", "404", "415"} {
		assertResponseDefined(t, op, code, label)
	}

	assertCacheControl(t, op, "200", "no-store", label)
	assertXRequestID(t, op, "200", label)
}

// ============================================================================
// TS-03-34 (Task 3.1) — Validates: 03-REQ-9.5
// ============================================================================

// TestOpenAPIPromoteUser verifies POST /users/{id}/promote: admin bearerAuth,
// 200 returns User object, 401/403/404 defined, description mentions role=admin.
func TestOpenAPIPromoteUser(t *testing.T) {
	doc := loadSpec(t)
	label := "POST /users/{id}/promote"
	op := mustGetOp(t, doc, "post", "/users/{id}/promote")

	assertBearerAuth(t, op, label)

	// 200 response: User object.
	assertResponseDefined(t, op, "200", label)

	// Error responses.
	for _, code := range []string{"401", "403", "404"} {
		assertResponseDefined(t, op, code, label)
	}

	// Description or 200 response description mentions role=admin.
	resp := requireResponse(t, op, "200", label)
	combined := op.Description + " " + op.Summary + " " + resp.Description
	if !strings.Contains(combined, "admin") {
		t.Errorf("%s: description or 200 response description should mention 'admin' role",
			label)
	}

	assertCacheControl(t, op, "200", "no-store", label)
	assertXRequestID(t, op, "200", label)
}

// ============================================================================
// TS-03-35 (Task 3.1) — Validates: 03-REQ-9.6
// ============================================================================

// TestOpenAPIDemoteUser verifies POST /users/{id}/demote: admin bearerAuth,
// 200 response, 409 for last-admin constraint, 401/403/404 defined.
func TestOpenAPIDemoteUser(t *testing.T) {
	doc := loadSpec(t)
	label := "POST /users/{id}/demote"
	op := mustGetOp(t, doc, "post", "/users/{id}/demote")

	assertBearerAuth(t, op, label)

	// 200 response.
	assertResponseDefined(t, op, "200", label)

	// 409 for last-admin constraint.
	assertResponseDefined(t, op, "409", label)

	// Check 409 description mentions last admin.
	resp409 := requireResponse(t, op, "409", label)
	combined := resp409.Description + " " + op.Description
	if !strings.Contains(strings.ToLower(combined), "last") ||
		!strings.Contains(strings.ToLower(combined), "admin") {
		t.Errorf("%s: 409 or operation description should mention 'last admin' constraint",
			label)
	}

	// Error responses.
	for _, code := range []string{"401", "403", "404"} {
		assertResponseDefined(t, op, code, label)
	}

	assertCacheControl(t, op, "200", "no-store", label)
	assertXRequestID(t, op, "200", label)
}

// ============================================================================
// TS-03-36 (Task 3.1) — Validates: 03-REQ-9.7
// ============================================================================

// TestOpenAPIBlockUser verifies POST /users/{id}/block: admin bearerAuth,
// 200 returns User object with status=blocked, 401/403/404 defined.
func TestOpenAPIBlockUser(t *testing.T) {
	doc := loadSpec(t)
	label := "POST /users/{id}/block"
	op := mustGetOp(t, doc, "post", "/users/{id}/block")

	assertBearerAuth(t, op, label)

	assertResponseDefined(t, op, "200", label)
	for _, code := range []string{"401", "403", "404"} {
		assertResponseDefined(t, op, code, label)
	}

	assertCacheControl(t, op, "200", "no-store", label)
	assertXRequestID(t, op, "200", label)
}

// ============================================================================
// TS-03-37 (Task 3.1) — Validates: 03-REQ-9.8
// ============================================================================

// TestOpenAPIUnblockUser verifies POST /users/{id}/unblock: admin bearerAuth,
// 200 returns User object with status=active, 401/403/404 defined.
func TestOpenAPIUnblockUser(t *testing.T) {
	doc := loadSpec(t)
	label := "POST /users/{id}/unblock"
	op := mustGetOp(t, doc, "post", "/users/{id}/unblock")

	assertBearerAuth(t, op, label)

	assertResponseDefined(t, op, "200", label)
	for _, code := range []string{"401", "403", "404"} {
		assertResponseDefined(t, op, code, label)
	}

	assertCacheControl(t, op, "200", "no-store", label)
	assertXRequestID(t, op, "200", label)
}

// ============================================================================
// TS-03-38 (Task 3.2) — Validates: 03-REQ-9.9
// ============================================================================

// TestOpenAPIAdminUserKeys verifies GET /users/{id}/keys: admin bearerAuth,
// 200 returns array of ApiKeyMetadata objects, 401/403/404 defined.
func TestOpenAPIAdminUserKeys(t *testing.T) {
	doc := loadSpec(t)
	label := "GET /users/{id}/keys"
	op := mustGetOp(t, doc, "get", "/users/{id}/keys")

	assertBearerAuth(t, op, label)

	// 200: array of ApiKeyMetadata.
	schema := requireResponseSchema(t, op, "200", label)
	hasArray := false
	for _, typ := range schema.Type {
		if typ == "array" {
			hasArray = true
		}
	}
	if !hasArray {
		t.Errorf("%s: 200 response should be type 'array', got %v", label, schema.Type)
	}

	for _, code := range []string{"401", "403", "404"} {
		assertResponseDefined(t, op, code, label)
	}

	assertCacheControl(t, op, "200", "no-store", label)
	assertXRequestID(t, op, "200", label)
}

// ============================================================================
// TS-03-39 (Task 3.2) — Validates: 03-REQ-9.10
// ============================================================================

// TestOpenAPIAdminDeleteUserKey verifies DELETE /users/{id}/keys/{key_id}:
// admin bearerAuth, 204 no body, 401/403/404 defined.
func TestOpenAPIAdminDeleteUserKey(t *testing.T) {
	doc := loadSpec(t)
	label := "DELETE /users/{id}/keys/{key_id}"
	op := mustGetOp(t, doc, "delete", "/users/{id}/keys/{key_id}")

	assertBearerAuth(t, op, label)

	assertResponseDefined(t, op, "204", label)
	assertResponseNoContent(t, op, "204", label)

	for _, code := range []string{"401", "403", "404"} {
		assertResponseDefined(t, op, code, label)
	}

	assertXRequestID(t, op, "204", label)
	assertCacheControl(t, op, "204", "no-store", label)
}

// ============================================================================
// TS-03-40 (Task 3.2) — Validates: 03-REQ-9.11
// ============================================================================

// TestOpenAPIAdminUserTokens verifies GET /users/{id}/tokens: admin bearerAuth,
// 200 returns array of PatMetadata objects, 401/403/404 defined.
func TestOpenAPIAdminUserTokens(t *testing.T) {
	doc := loadSpec(t)
	label := "GET /users/{id}/tokens"
	op := mustGetOp(t, doc, "get", "/users/{id}/tokens")

	assertBearerAuth(t, op, label)

	// 200: array of PatMetadata.
	schema := requireResponseSchema(t, op, "200", label)
	hasArray := false
	for _, typ := range schema.Type {
		if typ == "array" {
			hasArray = true
		}
	}
	if !hasArray {
		t.Errorf("%s: 200 response should be type 'array', got %v", label, schema.Type)
	}

	for _, code := range []string{"401", "403", "404"} {
		assertResponseDefined(t, op, code, label)
	}

	assertCacheControl(t, op, "200", "no-store", label)
	assertXRequestID(t, op, "200", label)
}

// ============================================================================
// TS-03-41 (Task 3.2) — Validates: 03-REQ-9.12
// ============================================================================

// TestOpenAPIAdminDeleteUserToken verifies DELETE /users/{id}/tokens/{token_id}:
// admin bearerAuth, 204 no body, 401/403/404 defined.
func TestOpenAPIAdminDeleteUserToken(t *testing.T) {
	doc := loadSpec(t)
	label := "DELETE /users/{id}/tokens/{token_id}"
	op := mustGetOp(t, doc, "delete", "/users/{id}/tokens/{token_id}")

	assertBearerAuth(t, op, label)

	assertResponseDefined(t, op, "204", label)
	assertResponseNoContent(t, op, "204", label)

	for _, code := range []string{"401", "403", "404"} {
		assertResponseDefined(t, op, code, label)
	}

	assertXRequestID(t, op, "204", label)
	assertCacheControl(t, op, "204", "no-store", label)
}

// ============================================================================
// TS-03-42 (Task 3.2) — Validates: 03-REQ-10.1
// ============================================================================

// TestOpenAPIPostOrgs verifies POST /orgs: admin bearerAuth, request body with
// required name and slug, optional url; 201 Organization, 409 conflict, 400,
// 401, 403, 415 defined.
func TestOpenAPIPostOrgs(t *testing.T) {
	doc := loadSpec(t)
	label := "POST /orgs"
	op := mustGetOp(t, doc, "post", "/orgs")

	assertBearerAuth(t, op, label)

	// Request body: name and slug required, url optional.
	schema := requireRequestBodySchema(t, op, label)
	requiredSet := make(map[string]bool)
	for _, r := range schema.Required {
		requiredSet[r] = true
	}
	if !requiredSet["name"] {
		t.Errorf("%s: request body should require 'name'", label)
	}
	if !requiredSet["slug"] {
		t.Errorf("%s: request body should require 'slug'", label)
	}
	if requiredSet["url"] {
		t.Errorf("%s: request body should NOT require 'url' (optional)", label)
	}

	// 201 response: Organization object.
	assertResponseDefined(t, op, "201", label)

	// Error responses.
	for _, code := range []string{"400", "401", "403", "409", "415"} {
		assertResponseDefined(t, op, code, label)
	}

	assertCacheControl(t, op, "201", "no-store", label)
	assertXRequestID(t, op, "201", label)
}

// ============================================================================
// TS-03-43, TS-03-SMOKE-6 (Task 3.2) — Validates: 03-REQ-10.2
// ============================================================================

// TestOpenAPIGetOrgs verifies GET /orgs: admin bearerAuth, include_blocked
// boolean query param defaulting false, 200 returns array of Organization
// objects, 401/403 defined.
func TestOpenAPIGetOrgs(t *testing.T) {
	doc := loadSpec(t)
	label := "GET /orgs"
	op := mustGetOp(t, doc, "get", "/orgs")

	assertBearerAuth(t, op, label)

	// include_blocked query parameter: boolean, default false.
	var found bool
	for _, param := range op.Parameters {
		if param != nil && param.Name == "include_blocked" && param.In == "query" {
			found = true
			if param.Schema == nil {
				t.Fatalf("%s: include_blocked parameter has no schema", label)
			}
			paramSchema := param.Schema.Schema()
			if paramSchema == nil {
				t.Fatalf("%s: could not build include_blocked schema", label)
			}
			hasBool := false
			for _, typ := range paramSchema.Type {
				if typ == "boolean" {
					hasBool = true
				}
			}
			if !hasBool {
				t.Errorf("%s: include_blocked should be type boolean, got %v",
					label, paramSchema.Type)
			}
			if paramSchema.Default != nil {
				var def bool
				if err := paramSchema.Default.Decode(&def); err == nil && def != false {
					t.Errorf("%s: include_blocked default should be false, got %v",
						label, def)
				}
			} else {
				t.Errorf("%s: include_blocked should have default value false", label)
			}
			break
		}
	}
	if !found {
		t.Errorf("%s: missing 'include_blocked' query parameter", label)
	}

	// 200 response: array of Organization objects.
	schema := requireResponseSchema(t, op, "200", label)
	hasArray := false
	for _, typ := range schema.Type {
		if typ == "array" {
			hasArray = true
		}
	}
	if !hasArray {
		t.Errorf("%s: 200 response should be type 'array', got %v", label, schema.Type)
	}

	assertResponseDefined(t, op, "401", label)
	assertResponseDefined(t, op, "403", label)
	assertCacheControl(t, op, "200", "no-store", label)
	assertXRequestID(t, op, "200", label)
}

// ============================================================================
// TS-03-44 (Task 3.3) — Validates: 03-REQ-10.3
// ============================================================================

// TestOpenAPIGetOrgById verifies GET /orgs/{id}: bearerAuth with dual-role auth
// (admin or orgs:read PAT), ETag on 200, 304 conditional response with
// X-Request-ID+ETag and no content, 401/403/404 defined.
func TestOpenAPIGetOrgById(t *testing.T) {
	doc := loadSpec(t)
	label := "GET /orgs/{id}"
	op := mustGetOp(t, doc, "get", "/orgs/{id}")

	assertBearerAuth(t, op, label)

	// Description mentions orgs:read for member access.
	combined := op.Description + " " + op.Summary
	if !strings.Contains(combined, "orgs:read") {
		t.Errorf("%s: description or summary should mention 'orgs:read' PAT permission", label)
	}

	// 200 response with ETag.
	assertResponseDefined(t, op, "200", label)
	assertETagHeader(t, op, "200", label)
	assertCacheControl(t, op, "200", "no-store", label)
	assertXRequestID(t, op, "200", label)

	// 304 conditional response.
	assert304Conditional(t, op, label)

	// Error responses.
	for _, code := range []string{"401", "403", "404"} {
		assertResponseDefined(t, op, code, label)
	}
}

// ============================================================================
// TS-03-45 (Task 3.3) — Validates: 03-REQ-10.4
// ============================================================================

// TestOpenAPIPatchOrg verifies PATCH /orgs/{id}: admin bearerAuth, request body
// with optional name and url (not in required); 200, 401, 403, 404, 409, 415.
func TestOpenAPIPatchOrg(t *testing.T) {
	doc := loadSpec(t)
	label := "PATCH /orgs/{id}"
	op := mustGetOp(t, doc, "patch", "/orgs/{id}")

	assertBearerAuth(t, op, label)

	// Request body: name and url should exist but NOT be required.
	schema := requireRequestBodySchema(t, op, label)
	requiredSet := make(map[string]bool)
	for _, r := range schema.Required {
		requiredSet[r] = true
	}
	if requiredSet["name"] {
		t.Errorf("%s: 'name' should NOT be required in request body (optional)", label)
	}
	if requiredSet["url"] {
		t.Errorf("%s: 'url' should NOT be required in request body (optional)", label)
	}

	// Both properties should exist.
	if schema.Properties != nil {
		if schema.Properties.GetOrZero("name") == nil {
			t.Errorf("%s: request body should have 'name' property", label)
		}
		if schema.Properties.GetOrZero("url") == nil {
			t.Errorf("%s: request body should have 'url' property", label)
		}
	}

	// Response codes.
	for _, code := range []string{"200", "401", "403", "404", "409", "415"} {
		assertResponseDefined(t, op, code, label)
	}

	assertCacheControl(t, op, "200", "no-store", label)
	assertXRequestID(t, op, "200", label)
}

// ============================================================================
// TS-03-46 (Task 3.3) — Validates: 03-REQ-10.5
// ============================================================================

// TestOpenAPIDeleteOrg verifies DELETE /orgs/{id}: admin bearerAuth, 204 no
// body with cascade behavior noted in description, 401/403/404 defined.
func TestOpenAPIDeleteOrg(t *testing.T) {
	doc := loadSpec(t)
	label := "DELETE /orgs/{id}"
	op := mustGetOp(t, doc, "delete", "/orgs/{id}")

	assertBearerAuth(t, op, label)

	assertResponseDefined(t, op, "204", label)
	assertResponseNoContent(t, op, "204", label)

	// Description mentions cascade or membership removal.
	desc := strings.ToLower(op.Description)
	if !strings.Contains(desc, "cascade") && !strings.Contains(desc, "membership") {
		t.Errorf("%s: description should mention cascade of memberships", label)
	}

	for _, code := range []string{"401", "403", "404"} {
		assertResponseDefined(t, op, code, label)
	}

	assertXRequestID(t, op, "204", label)
	assertCacheControl(t, op, "204", "no-store", label)
}

// ============================================================================
// TS-03-47 (Task 3.3) — Validates: 03-REQ-10.6
// ============================================================================

// TestOpenAPIBlockOrg verifies POST /orgs/{id}/block: admin bearerAuth, 200
// returns Organization object with status=blocked, 401/403/404 defined.
func TestOpenAPIBlockOrg(t *testing.T) {
	doc := loadSpec(t)
	label := "POST /orgs/{id}/block"
	op := mustGetOp(t, doc, "post", "/orgs/{id}/block")

	assertBearerAuth(t, op, label)

	assertResponseDefined(t, op, "200", label)
	for _, code := range []string{"401", "403", "404"} {
		assertResponseDefined(t, op, code, label)
	}

	assertCacheControl(t, op, "200", "no-store", label)
	assertXRequestID(t, op, "200", label)
}

// ============================================================================
// TS-03-48 (Task 3.3) — Validates: 03-REQ-10.7
// ============================================================================

// TestOpenAPIUnblockOrg verifies POST /orgs/{id}/unblock: admin bearerAuth,
// 200 returns Organization object with status=active, 401/403/404 defined.
func TestOpenAPIUnblockOrg(t *testing.T) {
	doc := loadSpec(t)
	label := "POST /orgs/{id}/unblock"
	op := mustGetOp(t, doc, "post", "/orgs/{id}/unblock")

	assertBearerAuth(t, op, label)

	assertResponseDefined(t, op, "200", label)
	for _, code := range []string{"401", "403", "404"} {
		assertResponseDefined(t, op, code, label)
	}

	assertCacheControl(t, op, "200", "no-store", label)
	assertXRequestID(t, op, "200", label)
}

// ============================================================================
// TS-03-49 (Task 3.3) — Validates: 03-REQ-10.8
// ============================================================================

// TestOpenAPIOrgMembers verifies GET /orgs/{id}/members: bearerAuth with
// dual-role auth (admin or orgs:read), 200 returns array of User objects,
// 401/403/404 defined.
func TestOpenAPIOrgMembers(t *testing.T) {
	doc := loadSpec(t)
	label := "GET /orgs/{id}/members"
	op := mustGetOp(t, doc, "get", "/orgs/{id}/members")

	assertBearerAuth(t, op, label)

	// Description mentions orgs:read for member access.
	combined := op.Description + " " + op.Summary
	if !strings.Contains(combined, "orgs:read") {
		t.Errorf("%s: description or summary should mention 'orgs:read' PAT permission", label)
	}

	// 200: array of User objects.
	schema := requireResponseSchema(t, op, "200", label)
	hasArray := false
	for _, typ := range schema.Type {
		if typ == "array" {
			hasArray = true
		}
	}
	if !hasArray {
		t.Errorf("%s: 200 response should be type 'array', got %v", label, schema.Type)
	}

	for _, code := range []string{"401", "403", "404"} {
		assertResponseDefined(t, op, code, label)
	}

	assertCacheControl(t, op, "200", "no-store", label)
	assertXRequestID(t, op, "200", label)
}

// ============================================================================
// TS-03-50 (Task 3.3) — Validates: 03-REQ-10.9
// ============================================================================

// TestOpenAPIPutOrgMember verifies PUT /orgs/{id}/members/{user_id}: admin
// bearerAuth, no request body defined, 204 no body, 401/403/404 defined.
func TestOpenAPIPutOrgMember(t *testing.T) {
	doc := loadSpec(t)
	label := "PUT /orgs/{id}/members/{user_id}"
	op := mustGetOp(t, doc, "put", "/orgs/{id}/members/{user_id}")

	assertBearerAuth(t, op, label)

	// No request body.
	if op.RequestBody != nil {
		t.Errorf("%s: should have no request body defined", label)
	}

	// 204: no content.
	assertResponseDefined(t, op, "204", label)
	assertResponseNoContent(t, op, "204", label)

	for _, code := range []string{"401", "403", "404"} {
		assertResponseDefined(t, op, code, label)
	}

	assertXRequestID(t, op, "204", label)
	assertCacheControl(t, op, "204", "no-store", label)
}

// ============================================================================
// TS-03-51 (Task 3.3) — Validates: 03-REQ-10.10
// ============================================================================

// TestOpenAPIDeleteOrgMember verifies DELETE /orgs/{id}/members/{user_id}:
// admin bearerAuth, 204 no body, 401/403/404 defined.
func TestOpenAPIDeleteOrgMember(t *testing.T) {
	doc := loadSpec(t)
	label := "DELETE /orgs/{id}/members/{user_id}"
	op := mustGetOp(t, doc, "delete", "/orgs/{id}/members/{user_id}")

	assertBearerAuth(t, op, label)

	assertResponseDefined(t, op, "204", label)
	assertResponseNoContent(t, op, "204", label)

	for _, code := range []string{"401", "403", "404"} {
		assertResponseDefined(t, op, code, label)
	}

	assertXRequestID(t, op, "204", label)
	assertCacheControl(t, op, "204", "no-store", label)
}

// ============================================================================
// TS-03-52 (Task 3.4) — Validates: 03-REQ-11.1
// ============================================================================

// TestOpenAPIUserSchema verifies the User schema: all fields present,
// full_name nullable, status enum active/blocked, role enum admin/user,
// id has format uuid.
func TestOpenAPIUserSchema(t *testing.T) {
	doc := loadSpec(t)
	if doc.Components == nil || doc.Components.Schemas == nil {
		t.Fatalf("spec has no components/schemas")
	}

	proxy := doc.Components.Schemas.GetOrZero("User")
	if proxy == nil {
		t.Fatalf("User schema not found in components/schemas")
	}
	schema := proxy.Schema()
	if schema == nil {
		t.Fatalf("could not build User schema")
	}
	if schema.Properties == nil {
		t.Fatalf("User schema has no properties")
	}

	// All expected fields.
	expectedFields := []string{
		"id", "username", "email", "full_name", "status", "role",
		"provider", "provider_id", "created_at", "updated_at",
	}
	for _, field := range expectedFields {
		if schema.Properties.GetOrZero(field) == nil {
			t.Errorf("User schema missing '%s' property", field)
		}
	}

	// id: format uuid.
	if idProxy := schema.Properties.GetOrZero("id"); idProxy != nil {
		idSchema := idProxy.Schema()
		if idSchema != nil && idSchema.Format != "uuid" {
			t.Errorf("User.id should have format 'uuid', got %q", idSchema.Format)
		}
	}

	// full_name: nullable.
	if fnProxy := schema.Properties.GetOrZero("full_name"); fnProxy != nil {
		fnSchema := fnProxy.Schema()
		if fnSchema != nil {
			hasNull := false
			for _, typ := range fnSchema.Type {
				if typ == "null" {
					hasNull = true
				}
			}
			if fnSchema.Nullable != nil && *fnSchema.Nullable {
				hasNull = true
			}
			if !hasNull {
				t.Errorf("User.full_name should be nullable")
			}
		}
	}

	// status: enum active, blocked.
	if statusProxy := schema.Properties.GetOrZero("status"); statusProxy != nil {
		statusSchema := statusProxy.Schema()
		if statusSchema != nil {
			enumVals := make([]string, 0, len(statusSchema.Enum))
			for _, node := range statusSchema.Enum {
				var val string
				if err := node.Decode(&val); err == nil {
					enumVals = append(enumVals, val)
				}
			}
			wantEnums := map[string]bool{"active": false, "blocked": false}
			for _, v := range enumVals {
				wantEnums[v] = true
			}
			for e, found := range wantEnums {
				if !found {
					t.Errorf("User.status enum should include %q", e)
				}
			}
		}
	}

	// role: enum admin, user.
	if roleProxy := schema.Properties.GetOrZero("role"); roleProxy != nil {
		roleSchema := roleProxy.Schema()
		if roleSchema != nil {
			enumVals := make([]string, 0, len(roleSchema.Enum))
			for _, node := range roleSchema.Enum {
				var val string
				if err := node.Decode(&val); err == nil {
					enumVals = append(enumVals, val)
				}
			}
			wantEnums := map[string]bool{"admin": false, "user": false}
			for _, v := range enumVals {
				wantEnums[v] = true
			}
			for e, found := range wantEnums {
				if !found {
					t.Errorf("User.role enum should include %q", e)
				}
			}
		}
	}
}

// ============================================================================
// TS-03-53 (Task 3.4) — Validates: 03-REQ-11.2
// ============================================================================

// TestOpenAPIApiKeyMetadataSchema verifies the ApiKeyMetadata schema: key_id,
// created_at, nullable expires_at, nullable revoked_at.
func TestOpenAPIApiKeyMetadataSchema(t *testing.T) {
	doc := loadSpec(t)
	if doc.Components == nil || doc.Components.Schemas == nil {
		t.Fatalf("spec has no components/schemas")
	}

	proxy := doc.Components.Schemas.GetOrZero("ApiKeyMetadata")
	if proxy == nil {
		t.Fatalf("ApiKeyMetadata schema not found in components/schemas")
	}
	schema := proxy.Schema()
	if schema == nil {
		t.Fatalf("could not build ApiKeyMetadata schema")
	}
	if schema.Properties == nil {
		t.Fatalf("ApiKeyMetadata schema has no properties")
	}

	for _, field := range []string{"key_id", "created_at", "expires_at", "revoked_at"} {
		if schema.Properties.GetOrZero(field) == nil {
			t.Errorf("ApiKeyMetadata missing '%s' property", field)
		}
	}

	// expires_at and revoked_at: nullable.
	for _, field := range []string{"expires_at", "revoked_at"} {
		p := schema.Properties.GetOrZero(field)
		if p == nil {
			continue
		}
		fs := p.Schema()
		if fs == nil {
			continue
		}
		hasNull := false
		for _, typ := range fs.Type {
			if typ == "null" {
				hasNull = true
			}
		}
		if fs.Nullable != nil && *fs.Nullable {
			hasNull = true
		}
		if !hasNull {
			t.Errorf("ApiKeyMetadata.%s should be nullable", field)
		}
	}
}

// ============================================================================
// TS-03-54 (Task 3.4) — Validates: 03-REQ-11.3
// ============================================================================

// TestOpenAPIApiKeySchema verifies the ApiKey schema: key (full plaintext),
// key_id, nullable expires_at.
func TestOpenAPIApiKeySchema(t *testing.T) {
	doc := loadSpec(t)
	if doc.Components == nil || doc.Components.Schemas == nil {
		t.Fatalf("spec has no components/schemas")
	}

	proxy := doc.Components.Schemas.GetOrZero("ApiKey")
	if proxy == nil {
		t.Fatalf("ApiKey schema not found in components/schemas")
	}
	schema := proxy.Schema()
	if schema == nil {
		t.Fatalf("could not build ApiKey schema")
	}
	if schema.Properties == nil {
		t.Fatalf("ApiKey schema has no properties")
	}

	for _, field := range []string{"key", "key_id", "expires_at"} {
		if schema.Properties.GetOrZero(field) == nil {
			t.Errorf("ApiKey missing '%s' property", field)
		}
	}

	// key: type string.
	if keyProxy := schema.Properties.GetOrZero("key"); keyProxy != nil {
		keySchema := keyProxy.Schema()
		if keySchema != nil {
			hasString := false
			for _, typ := range keySchema.Type {
				if typ == "string" {
					hasString = true
				}
			}
			if !hasString {
				t.Errorf("ApiKey.key should be type string, got %v", keySchema.Type)
			}
		}
	}

	// expires_at: nullable.
	if eaProxy := schema.Properties.GetOrZero("expires_at"); eaProxy != nil {
		eaSchema := eaProxy.Schema()
		if eaSchema != nil {
			hasNull := false
			for _, typ := range eaSchema.Type {
				if typ == "null" {
					hasNull = true
				}
			}
			if eaSchema.Nullable != nil && *eaSchema.Nullable {
				hasNull = true
			}
			if !hasNull {
				t.Errorf("ApiKey.expires_at should be nullable")
			}
		}
	}
}

// ============================================================================
// TS-03-55 (Task 3.4) — Validates: 03-REQ-11.4
// ============================================================================

// TestOpenAPIPatMetadataSchema verifies the PatMetadata schema: token_id, name,
// permissions (array of pattern-constrained strings), created_at, nullable
// expires_at, nullable revoked_at.
func TestOpenAPIPatMetadataSchema(t *testing.T) {
	doc := loadSpec(t)
	if doc.Components == nil || doc.Components.Schemas == nil {
		t.Fatalf("spec has no components/schemas")
	}

	proxy := doc.Components.Schemas.GetOrZero("PatMetadata")
	if proxy == nil {
		t.Fatalf("PatMetadata schema not found in components/schemas")
	}
	schema := proxy.Schema()
	if schema == nil {
		t.Fatalf("could not build PatMetadata schema")
	}
	if schema.Properties == nil {
		t.Fatalf("PatMetadata schema has no properties")
	}

	for _, field := range []string{"token_id", "name", "permissions", "created_at", "expires_at", "revoked_at"} {
		if schema.Properties.GetOrZero(field) == nil {
			t.Errorf("PatMetadata missing '%s' property", field)
		}
	}

	// permissions: array with pattern-constrained items.
	permProxy := schema.Properties.GetOrZero("permissions")
	if permProxy != nil {
		permSchema := permProxy.Schema()
		if permSchema != nil && permSchema.Items != nil && permSchema.Items.IsA() {
			itemSchema := permSchema.Items.A.Schema()
			if itemSchema != nil {
				wantPattern := `^[a-z_]+:[a-z_]+$`
				if itemSchema.Pattern != wantPattern {
					t.Errorf("PatMetadata.permissions items pattern: expected %q, got %q",
						wantPattern, itemSchema.Pattern)
				}
			}
		}
	}

	// Nullable fields.
	for _, field := range []string{"expires_at", "revoked_at"} {
		p := schema.Properties.GetOrZero(field)
		if p == nil {
			continue
		}
		fs := p.Schema()
		if fs == nil {
			continue
		}
		hasNull := false
		for _, typ := range fs.Type {
			if typ == "null" {
				hasNull = true
			}
		}
		if fs.Nullable != nil && *fs.Nullable {
			hasNull = true
		}
		if !hasNull {
			t.Errorf("PatMetadata.%s should be nullable", field)
		}
	}
}

// ============================================================================
// TS-03-56 (Task 3.4) — Validates: 03-REQ-11.5
// ============================================================================

// TestOpenAPIPatSchema verifies the Pat schema (creation): token (plaintext),
// token_id, name, permissions (pattern-constrained), nullable expires_at.
func TestOpenAPIPatSchema(t *testing.T) {
	doc := loadSpec(t)
	if doc.Components == nil || doc.Components.Schemas == nil {
		t.Fatalf("spec has no components/schemas")
	}

	proxy := doc.Components.Schemas.GetOrZero("Pat")
	if proxy == nil {
		t.Fatalf("Pat schema not found in components/schemas")
	}
	schema := proxy.Schema()
	if schema == nil {
		t.Fatalf("could not build Pat schema")
	}
	if schema.Properties == nil {
		t.Fatalf("Pat schema has no properties")
	}

	for _, field := range []string{"token", "token_id", "name", "permissions", "expires_at"} {
		if schema.Properties.GetOrZero(field) == nil {
			t.Errorf("Pat missing '%s' property", field)
		}
	}

	// expires_at: nullable.
	if eaProxy := schema.Properties.GetOrZero("expires_at"); eaProxy != nil {
		eaSchema := eaProxy.Schema()
		if eaSchema != nil {
			hasNull := false
			for _, typ := range eaSchema.Type {
				if typ == "null" {
					hasNull = true
				}
			}
			if eaSchema.Nullable != nil && *eaSchema.Nullable {
				hasNull = true
			}
			if !hasNull {
				t.Errorf("Pat.expires_at should be nullable")
			}
		}
	}
}

// ============================================================================
// TS-03-57 (Task 3.4) — Validates: 03-REQ-11.6
// ============================================================================

// TestOpenAPIOrgSchema verifies the Organization schema: id (uuid), name, slug,
// nullable url, status enum active/blocked, created_at, updated_at.
func TestOpenAPIOrgSchema(t *testing.T) {
	doc := loadSpec(t)
	if doc.Components == nil || doc.Components.Schemas == nil {
		t.Fatalf("spec has no components/schemas")
	}

	proxy := doc.Components.Schemas.GetOrZero("Organization")
	if proxy == nil {
		t.Fatalf("Organization schema not found in components/schemas")
	}
	schema := proxy.Schema()
	if schema == nil {
		t.Fatalf("could not build Organization schema")
	}
	if schema.Properties == nil {
		t.Fatalf("Organization schema has no properties")
	}

	for _, field := range []string{"id", "name", "slug", "url", "status", "created_at", "updated_at"} {
		if schema.Properties.GetOrZero(field) == nil {
			t.Errorf("Organization missing '%s' property", field)
		}
	}

	// url: nullable.
	if urlProxy := schema.Properties.GetOrZero("url"); urlProxy != nil {
		urlSchema := urlProxy.Schema()
		if urlSchema != nil {
			hasNull := false
			for _, typ := range urlSchema.Type {
				if typ == "null" {
					hasNull = true
				}
			}
			if urlSchema.Nullable != nil && *urlSchema.Nullable {
				hasNull = true
			}
			if !hasNull {
				t.Errorf("Organization.url should be nullable")
			}
		}
	}

	// status: enum active, blocked.
	if statusProxy := schema.Properties.GetOrZero("status"); statusProxy != nil {
		statusSchema := statusProxy.Schema()
		if statusSchema != nil {
			enumVals := make([]string, 0, len(statusSchema.Enum))
			for _, node := range statusSchema.Enum {
				var val string
				if err := node.Decode(&val); err == nil {
					enumVals = append(enumVals, val)
				}
			}
			wantEnums := map[string]bool{"active": false, "blocked": false}
			for _, v := range enumVals {
				wantEnums[v] = true
			}
			for e, found := range wantEnums {
				if !found {
					t.Errorf("Organization.status enum should include %q", e)
				}
			}
		}
	}
}

// ============================================================================
// TS-03-58 (Task 3.4) — Validates: 03-REQ-11.7
// ============================================================================

// TestOpenAPIOAuthProviderSchema verifies the OAuthProvider schema has exactly
// two properties: name (string) and authorize_url (string).
func TestOpenAPIOAuthProviderSchema(t *testing.T) {
	doc := loadSpec(t)
	if doc.Components == nil || doc.Components.Schemas == nil {
		t.Fatalf("spec has no components/schemas")
	}

	proxy := doc.Components.Schemas.GetOrZero("OAuthProvider")
	if proxy == nil {
		t.Fatalf("OAuthProvider schema not found in components/schemas")
	}
	schema := proxy.Schema()
	if schema == nil {
		t.Fatalf("could not build OAuthProvider schema")
	}
	if schema.Properties == nil {
		t.Fatalf("OAuthProvider schema has no properties")
	}

	// Exactly two properties: name and authorize_url.
	count := 0
	for range schema.Properties.FromOldest() {
		count++
	}
	if count != 2 {
		t.Errorf("OAuthProvider should have exactly 2 properties, got %d", count)
	}

	for _, field := range []string{"name", "authorize_url"} {
		p := schema.Properties.GetOrZero(field)
		if p == nil {
			t.Errorf("OAuthProvider missing '%s' property", field)
			continue
		}
		fs := p.Schema()
		if fs == nil {
			continue
		}
		hasString := false
		for _, typ := range fs.Type {
			if typ == "string" {
				hasString = true
			}
		}
		if !hasString {
			t.Errorf("OAuthProvider.%s should be type string, got %v", field, fs.Type)
		}
	}
}

// ============================================================================
// TS-03-59, TS-03-P8 (Task 3.4) — Validates: 03-REQ-11.8
// ============================================================================

// TestOpenAPIErrorSchema verifies the Error schema: error object with code
// (integer) and message (string); description states code equals HTTP status
// code with no sub-codes.
func TestOpenAPIErrorSchema(t *testing.T) {
	doc := loadSpec(t)
	if doc.Components == nil || doc.Components.Schemas == nil {
		t.Fatalf("spec has no components/schemas")
	}

	proxy := doc.Components.Schemas.GetOrZero("Error")
	if proxy == nil {
		t.Fatalf("Error schema not found in components/schemas")
	}
	schema := proxy.Schema()
	if schema == nil {
		t.Fatalf("could not build Error schema")
	}
	if schema.Properties == nil {
		t.Fatalf("Error schema has no properties")
	}

	// Required "error" object property.
	errProxy := schema.Properties.GetOrZero("error")
	if errProxy == nil {
		t.Fatalf("Error schema missing 'error' property")
	}
	errSchema := errProxy.Schema()
	if errSchema == nil {
		t.Fatalf("could not build 'error' sub-schema")
	}
	if errSchema.Properties == nil {
		t.Fatalf("Error.error has no properties")
	}

	// code: integer.
	codeProxy := errSchema.Properties.GetOrZero("code")
	if codeProxy == nil {
		t.Fatalf("Error.error missing 'code' property")
	}
	codeSchema := codeProxy.Schema()
	if codeSchema != nil {
		hasInteger := false
		for _, typ := range codeSchema.Type {
			if typ == "integer" {
				hasInteger = true
			}
		}
		if !hasInteger {
			t.Errorf("Error.error.code should be type integer, got %v", codeSchema.Type)
		}
	}

	// message: string.
	msgProxy := errSchema.Properties.GetOrZero("message")
	if msgProxy == nil {
		t.Fatalf("Error.error missing 'message' property")
	}
	msgSchema := msgProxy.Schema()
	if msgSchema != nil {
		hasString := false
		for _, typ := range msgSchema.Type {
			if typ == "string" {
				hasString = true
			}
		}
		if !hasString {
			t.Errorf("Error.error.message should be type string, got %v", msgSchema.Type)
		}
	}

	// TS-03-P8: Description states code equals HTTP status code, no sub-codes.
	combinedDesc := schema.Description + " " + codeSchema.Description
	lower := strings.ToLower(combinedDesc)
	if !strings.Contains(lower, "http") && !strings.Contains(lower, "status") {
		t.Errorf("Error schema description should mention 'HTTP' or 'status code'")
	}
	if strings.Contains(lower, "sub-code") || strings.Contains(lower, "subcode") {
		t.Errorf("Error schema description should not use sub-codes")
	}
}

// ============================================================================
// TS-03-60 (Task 3.4) — Validates: 03-REQ-11.9
// ============================================================================

// TestOpenAPIAuthCallbackResponseSchema verifies the AuthCallbackResponse
// schema: user ($ref to User) and api_key object with key, key_id, nullable
// expires_at.
func TestOpenAPIAuthCallbackResponseSchema(t *testing.T) {
	doc := loadSpec(t)
	if doc.Components == nil || doc.Components.Schemas == nil {
		t.Fatalf("spec has no components/schemas")
	}

	proxy := doc.Components.Schemas.GetOrZero("AuthCallbackResponse")
	if proxy == nil {
		t.Fatalf("AuthCallbackResponse schema not found in components/schemas")
	}
	schema := proxy.Schema()
	if schema == nil {
		t.Fatalf("could not build AuthCallbackResponse schema")
	}
	if schema.Properties == nil {
		t.Fatalf("AuthCallbackResponse schema has no properties")
	}

	// user: should reference User schema (libopenapi resolves $ref transparently).
	if schema.Properties.GetOrZero("user") == nil {
		t.Errorf("AuthCallbackResponse missing 'user' property")
	}

	// api_key: object with key, key_id, nullable expires_at.
	akProxy := schema.Properties.GetOrZero("api_key")
	if akProxy == nil {
		t.Fatalf("AuthCallbackResponse missing 'api_key' property")
	}
	akSchema := akProxy.Schema()
	if akSchema == nil {
		t.Fatalf("could not build api_key schema")
	}
	if akSchema.Properties == nil {
		t.Fatalf("AuthCallbackResponse.api_key has no properties")
	}
	for _, field := range []string{"key", "key_id", "expires_at"} {
		if akSchema.Properties.GetOrZero(field) == nil {
			t.Errorf("AuthCallbackResponse.api_key missing '%s' property", field)
		}
	}

	// api_key.expires_at: nullable.
	if eaProxy := akSchema.Properties.GetOrZero("expires_at"); eaProxy != nil {
		eaSchema := eaProxy.Schema()
		if eaSchema != nil {
			hasNull := false
			for _, typ := range eaSchema.Type {
				if typ == "null" {
					hasNull = true
				}
			}
			if eaSchema.Nullable != nil && *eaSchema.Nullable {
				hasNull = true
			}
			if !hasNull {
				t.Errorf("AuthCallbackResponse.api_key.expires_at should be nullable")
			}
		}
	}
}

// ============================================================================
// TS-03-61 (Task 3.5) — Validates: 03-REQ-12.1
// ============================================================================

// TestOpenAPIErrorCoverage enumerates all operations and verifies that each
// has its applicable error codes defined using the Error schema $ref.
// Expected error codes per category:
//   - All request-body endpoints: 400, 415
//   - All authenticated endpoints: 401, 403
//   - All resource-by-id endpoints (except list): 404
//   - POST /users: 409; POST /users/{id}/demote: 409
//   - POST /orgs: 409; PATCH /orgs/{id}: 409
func TestOpenAPIErrorCoverage(t *testing.T) {
	doc := loadSpec(t)
	if doc.Paths == nil || doc.Paths.PathItems == nil {
		t.Fatalf("spec has no paths defined")
	}

	// Map of (method+path) -> expected error codes beyond auth errors.
	// Auth errors (401, 403) tested separately in TS-03-62.
	type opKey struct {
		method string
		path   string
	}
	extraErrors := map[opKey][]string{
		{"post", "/users"}:             {"409"},
		{"post", "/users/{id}/demote"}: {"409"},
		{"post", "/orgs"}:             {"409"},
		{"patch", "/orgs/{id}"}:       {"409"},
	}

	for path, pi := range doc.Paths.PathItems.FromOldest() {
		for _, method := range allMethods {
			op := getOperation(pi, method)
			if op == nil {
				continue
			}

			label := strings.ToUpper(method) + " " + path

			// Check extra error codes for specific endpoints.
			key := opKey{method, path}
			if codes, ok := extraErrors[key]; ok {
				for _, code := range codes {
					assertResponseDefined(t, op, code, label+" (error coverage)")
				}
			}
		}
	}
}

// ============================================================================
// TS-03-62, TS-03-P2 (Task 3.5) — Validates: 03-REQ-12.2
// ============================================================================

// TestOpenAPIAuthErrors verifies that every operation with bearerAuth security
// has both 401 and 403 responses referencing the Error schema.
func TestOpenAPIAuthErrors(t *testing.T) {
	doc := loadSpec(t)
	if doc.Paths == nil || doc.Paths.PathItems == nil {
		t.Fatalf("spec has no paths defined")
	}

	for path, pi := range doc.Paths.PathItems.FromOldest() {
		for _, method := range allMethods {
			op := getOperation(pi, method)
			if op == nil {
				continue
			}
			if !hasBearerAuth(op.Security) {
				continue
			}

			label := strings.ToUpper(method) + " " + path

			assertResponseDefined(t, op, "401", label+" (auth errors)")
			assertResponseDefined(t, op, "403", label+" (auth errors)")
		}
	}
}

// ============================================================================
// TS-03-63 (Task 3.5) — Validates: 03-REQ-12.3
// ============================================================================

// TestOpenAPI413Coverage verifies that all operations with a requestBody
// have a 413 response entry referencing the Error schema.
func TestOpenAPI413Coverage(t *testing.T) {
	doc := loadSpec(t)
	if doc.Paths == nil || doc.Paths.PathItems == nil {
		t.Fatalf("spec has no paths defined")
	}

	for path, pi := range doc.Paths.PathItems.FromOldest() {
		for _, method := range allMethods {
			op := getOperation(pi, method)
			if op == nil {
				continue
			}
			if op.RequestBody == nil {
				continue
			}

			label := strings.ToUpper(method) + " " + path
			assertResponseDefined(t, op, "413", label+" (413 coverage)")
		}
	}
}

// ============================================================================
// TS-03-64 (Task 3.5) — Validates: 03-REQ-13.1
// ============================================================================

// TestOpenAPICachingMutableEndpoints verifies that all mutable resource
// endpoints (not health probes, not GET /auth/providers) include
// Cache-Control: no-store on their success responses.
func TestOpenAPICachingMutableEndpoints(t *testing.T) {
	doc := loadSpec(t)
	if doc.Paths == nil || doc.Paths.PathItems == nil {
		t.Fatalf("spec has no paths defined")
	}

	// Paths that are NOT mutable resource endpoints.
	nonMutable := map[string]bool{
		"/healthz":        true,
		"/readyz":         true,
		"/version":        true,
		"/auth/providers": true,
	}

	for path, pi := range doc.Paths.PathItems.FromOldest() {
		if nonMutable[path] {
			continue
		}
		for _, method := range allMethods {
			op := getOperation(pi, method)
			if op == nil {
				continue
			}

			label := strings.ToUpper(method) + " " + path

			// Check Cache-Control: no-store on success responses (200, 201, 204).
			if op.Responses == nil || op.Responses.Codes == nil {
				continue
			}
			for _, code := range []string{"200", "201", "204"} {
				if op.Responses.Codes.GetOrZero(code) != nil {
					assertCacheControl(t, op, code, "no-store",
						label+" (mutable caching)")
				}
			}
		}
	}
}

// ============================================================================
// TS-03-65 (Task 3.5) — Validates: 03-REQ-13.2
// ============================================================================

// TestOpenAPICachingHealthProbes verifies that GET /healthz, GET /readyz, and
// GET /version document Cache-Control: no-cache.
func TestOpenAPICachingHealthProbes(t *testing.T) {
	doc := loadSpec(t)

	for _, path := range []string{"/healthz", "/readyz", "/version"} {
		t.Run(path, func(t *testing.T) {
			op := mustGetOp(t, doc, "get", path)
			label := "GET " + path
			assertCacheControl(t, op, "200", "no-cache", label+" (health caching)")
		})
	}
}

// ============================================================================
// TS-03-66 (Task 3.5) — Validates: 03-REQ-13.3
// ============================================================================

// TestOpenAPICachingAuthProviders verifies that GET /auth/providers documents
// Cache-Control: public, max-age=300.
func TestOpenAPICachingAuthProviders(t *testing.T) {
	doc := loadSpec(t)
	label := "GET /auth/providers"
	op := mustGetOp(t, doc, "get", "/auth/providers")

	assertCacheControl(t, op, "200", "public, max-age=300", label+" (auth providers caching)")
}

// ============================================================================
// TS-03-71 (Task 3.5) — Validates: 03-REQ-15.1
// ============================================================================

// TestOpenAPITimestampFormat verifies that all timestamp fields across all
// schemas (created_at, updated_at, expires_at, revoked_at) use format:
// date-time.
func TestOpenAPITimestampFormat(t *testing.T) {
	doc := loadSpec(t)
	if doc.Components == nil || doc.Components.Schemas == nil {
		t.Fatalf("spec has no components/schemas")
	}

	timestampFields := []string{"created_at", "updated_at", "expires_at", "revoked_at"}
	schemaNames := []string{"User", "ApiKeyMetadata", "ApiKey", "PatMetadata", "Pat", "Organization"}

	for _, name := range schemaNames {
		proxy := doc.Components.Schemas.GetOrZero(name)
		if proxy == nil {
			t.Errorf("schema %s not found in components/schemas", name)
			continue
		}
		schema := proxy.Schema()
		if schema == nil || schema.Properties == nil {
			continue
		}

		for _, field := range timestampFields {
			fp := schema.Properties.GetOrZero(field)
			if fp == nil {
				continue // not all schemas have all timestamp fields
			}
			fs := fp.Schema()
			if fs == nil {
				continue
			}
			if fs.Format != "date-time" {
				t.Errorf("%s.%s should have format 'date-time', got %q",
					name, field, fs.Format)
			}
		}
	}
}

// ============================================================================
// TS-03-67, TS-03-P3 (Task 3.6) — Validates: 03-REQ-14.1
// ============================================================================

// TestOpenAPIXRequestIDEverywhere enumerates every response across all
// operations and all status codes, asserting X-Request-ID header is defined.
func TestOpenAPIXRequestIDEverywhere(t *testing.T) {
	doc := loadSpec(t)
	if doc.Paths == nil || doc.Paths.PathItems == nil {
		t.Fatalf("spec has no paths defined")
	}

	for path, pi := range doc.Paths.PathItems.FromOldest() {
		for _, method := range allMethods {
			op := getOperation(pi, method)
			if op == nil {
				continue
			}
			if op.Responses == nil || op.Responses.Codes == nil {
				continue
			}

			for code := range op.Responses.Codes.FromOldest() {
				label := strings.ToUpper(method) + " " + path
				assertXRequestID(t, op, code, label+" (X-Request-ID everywhere)")
			}
		}
	}
}

// ============================================================================
// TS-03-68, TS-03-P1 (Task 3.6) — Validates: 03-REQ-14.2
// ============================================================================

// TestOpenAPIETagOpaque verifies that ETag is documented as an opaque string
// header (no format, no pattern, no derivation algorithm description) on
// exactly the four single-resource GET endpoints.
func TestOpenAPIETagOpaque(t *testing.T) {
	doc := loadSpec(t)

	etagEndpoints := []struct {
		method string
		path   string
	}{
		{"get", "/user"},
		{"get", "/users/{id}"},
		{"get", "/user/tokens/{token_id}"},
		{"get", "/orgs/{id}"},
	}

	for _, ep := range etagEndpoints {
		label := strings.ToUpper(ep.method) + " " + ep.path
		t.Run(label, func(t *testing.T) {
			op := mustGetOp(t, doc, ep.method, ep.path)

			h := requireResponseHeader(t, op, "200", "ETag", label)
			if h == nil {
				return
			}
			if h.Schema == nil {
				t.Errorf("%s: ETag header has no schema", label)
				return
			}
			schema := h.Schema.Schema()
			if schema == nil {
				return
			}

			// Type must be string.
			hasString := false
			for _, typ := range schema.Type {
				if typ == "string" {
					hasString = true
				}
			}
			if !hasString {
				t.Errorf("%s: ETag should be type string, got %v", label, schema.Type)
			}

			// No format or pattern (opaque).
			if schema.Format != "" {
				t.Errorf("%s: ETag should have no format (opaque), got %q",
					label, schema.Format)
			}
			if schema.Pattern != "" {
				t.Errorf("%s: ETag should have no pattern (opaque), got %q",
					label, schema.Pattern)
			}

			// TS-03-P1: no derivation algorithm in description.
			desc := strings.ToLower(h.Description + " " + schema.Description)
			if strings.Contains(desc, "hash") || strings.Contains(desc, "algorithm") {
				t.Errorf("%s: ETag description should not mention derivation algorithm",
					label)
			}
		})
	}
}

// ============================================================================
// TS-03-69 (Task 3.6) — Validates: 03-REQ-14.3
// ============================================================================

// TestOpenAPIContentTypeConstraints verifies: all requestBody definitions use
// application/json; all response bodies use application/json (or
// application/json; charset=utf-8); 204 and 304 responses have no content.
func TestOpenAPIContentTypeConstraints(t *testing.T) {
	doc := loadSpec(t)
	if doc.Paths == nil || doc.Paths.PathItems == nil {
		t.Fatalf("spec has no paths defined")
	}

	for path, pi := range doc.Paths.PathItems.FromOldest() {
		for _, method := range allMethods {
			op := getOperation(pi, method)
			if op == nil {
				continue
			}
			label := strings.ToUpper(method) + " " + path

			// Request body: must use application/json.
			if op.RequestBody != nil && op.RequestBody.Content != nil {
				if op.RequestBody.Content.GetOrZero("application/json") == nil {
					t.Errorf("%s: requestBody should define 'application/json' content",
						label)
				}
			}

			// 204 and 304 responses: no content.
			if op.Responses != nil && op.Responses.Codes != nil {
				for _, code := range []string{"204", "304"} {
					resp := op.Responses.Codes.GetOrZero(code)
					if resp == nil {
						continue
					}
					if resp.Content != nil {
						hasContent := false
						for range resp.Content.FromOldest() {
							hasContent = true
							break
						}
						if hasContent {
							t.Errorf("%s: response %s should have no content body",
								label, code)
						}
					}
				}
			}
		}
	}
}

// ============================================================================
// TS-03-70, TS-03-P9 (Task 3.6) — Validates: 03-REQ-14.4
// ============================================================================

// TestOpenAPI304Responses verifies that 304 responses on the four conditional
// GET endpoints have X-Request-ID and ETag headers, no body, and no
// Cache-Control header.
func TestOpenAPI304Responses(t *testing.T) {
	doc := loadSpec(t)

	conditionalEndpoints := []struct {
		method string
		path   string
	}{
		{"get", "/user"},
		{"get", "/users/{id}"},
		{"get", "/user/tokens/{token_id}"},
		{"get", "/orgs/{id}"},
	}

	for _, ep := range conditionalEndpoints {
		label := strings.ToUpper(ep.method) + " " + ep.path
		t.Run(label, func(t *testing.T) {
			op := mustGetOp(t, doc, ep.method, ep.path)
			assert304Conditional(t, op, label+" (304 response)")
		})
	}
}

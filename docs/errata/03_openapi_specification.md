# Errata: Spec 03 — OpenAPI Specification

## 1. libopenapi BuildV3Model API signature

**Affected:** 03-REQ-16.1, external_apis section

The spec's `external_apis` documents `BuildV3Model` as returning
`(*DocumentModel[v3.Document], []error)`. The actual libopenapi v0.38.7 API
returns `(*DocumentModel[v3high.Document], error)` — a single `error`, not
`[]error`. The type parameter is `v3high.Document` (import path
`github.com/pb33f/libopenapi/datamodel/high/v3`), not `v3.Document`.

**Resolution:** Tests use the correct API signature. Errors are checked as a
single `error` value, not a slice.

## 2. POST /auth/callback is a public endpoint

**Affected:** 03-REQ-2.2, 03-REQ-2.3, TS-03-6, TS-03-7

The requirements list four public endpoints (GET /healthz, GET /readyz,
GET /version, GET /auth/providers) but omit POST /auth/callback. The test
spec TS-03-6 would flag POST /auth/callback as missing bearerAuth. However,
POST /auth/callback is the OAuth code exchange that creates credentials — it
cannot require authentication.

**Resolution:** Tests treat POST /auth/callback as a public endpoint: it is
excluded from the authenticated endpoint sweep (TestOpenAPIAuthenticatedEndpoints)
and included in the public endpoint assertions (TestOpenAPIPublicEndpoints).

## 3. Test specPath relative to package directory, not module root

**Affected:** all tests in api/api_test.go

The original `specPath` constant was `"api/openapi.yaml"` with a comment saying
tests run from the module root. Go tests actually run with CWD set to the
package source directory (`api/`), so `os.ReadFile("api/openapi.yaml")` looks
for `api/api/openapi.yaml` which doesn't exist.

**Resolution:** Changed `specPath` to `"openapi.yaml"` (relative to the package
directory where tests run).

## 4. TestOpenAPIPatchPropertyOnlyFullName swept all PATCH ops incorrectly

**Affected:** PROP-5, 03-REQ-5.1, 03-REQ-5.2, 03-REQ-10.4

The test iterated ALL PATCH operations and asserted that each has only a
`full_name` property. PROP-5 scopes this constraint to `PATCH /user` and
`PATCH /users/{id}` only. `PATCH /orgs/{id}` has `name` and `url` properties
per 03-REQ-10.4.

**Resolution:** Changed the test to only check the two user PATCH paths
(`/user` and `/users/{id}`).

## 5. openapi-validator CLI command path

**Affected:** Makefile check-spec target, 03-REQ-17.1

The spec references `github.com/pb33f/libopenapi-validator/cmd/openapi-validator`
but the actual CLI binary in libopenapi-validator v0.14.0 is at
`github.com/pb33f/libopenapi-validator/cmd/validate`.

**Resolution:** Updated the Makefile `check-spec` target to use the correct
binary path.

## 6. PATCH /user missing 400 response code

**Affected:** 03-REQ-8.2 (TS-03-21)

The test `TestOpenAPIPatchUser` expects a 400 response on `PATCH /user`.
While the original requirement 03-REQ-8.2 doesn't explicitly list 400, the
test (written in group 2) asserts it. Added 400 to PATCH /user in the spec
for consistency with test expectations.

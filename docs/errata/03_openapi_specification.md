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

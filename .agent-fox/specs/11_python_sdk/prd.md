---
spec_id: '11'
spec_name: python_sdk
title: Python Sdk
status: draft
created_at: '2026-07-17T12:23:04.125423+00:00'
updated_at: '2026-07-17T12:32:11.624821+00:00'
owner: ''
source: interactive
schema_version: 1
---
# Python SDK

## Intent

Provide a typed Python client library for the apikit built-in API endpoints.
The Python SDK lives under `packages/sdk-python/` in the apikit repository,
is managed with `uv` (pyproject.toml), and provides the same capabilities as
the Go SDK: typed request/response dataclasses and a client wrapping each
built-in endpoint with a Python method.

## Background

This spec is derived from the master PRD at `docs/PRD.md` in the apikit
repository. All endpoint definitions, schema shapes, status codes, headers,
and behavioral constraints are extracted from that document. The typed
request/response dataclasses match the schemas defined in the OpenAPI
specification at `api/openapi.yaml`.

The Python SDK is the first client-side SDK artifact for apikit. A Go SDK is
planned as a separate spec and will be loosely aligned in capability but is not
a formal dependency of this spec. Both SDKs derive independently from the
OpenAPI specification rather than from each other.

This is a solo project with no designated owner. Responsibility for keeping
the SDK in sync with OpenAPI spec changes and for reviewing package PRs rests
with the project author.

Distribution of the Python SDK to PyPI or any private registry is out of scope
for the first iteration. The SDK is intended for internal/monorepo use only.
No formal versioning scheme is required for this iteration; a version bump
strategy will be defined if/when PyPI publishing is introduced.

## Source Reference

All endpoint definitions, schema shapes, status codes, headers, and behavioral
constraints are extracted from `docs/PRD.md` in the apikit repository. The
typed request/response dataclasses match the schemas defined in the OpenAPI
specification at `api/openapi.yaml`.

The master PRD explicitly lists `/healthz`, `/readyz`, and `/version` as
**public endpoints outside the mount point**, confirming that all three are
resolved against `base_url` directly (no mount-point prefix). This is the
authoritative reference for the URL construction rules documented in this spec.

## Dependencies

| Spec | Relationship |
|------|-------------|
| 03_openapi_specification | Request/response schemas derive from OpenAPI spec |

The Python SDK is a downstream consumer of the OpenAPI specification. All
typed models and method signatures must match the schemas defined in the
OpenAPI spec. If the OpenAPI spec changes, the Python SDK must be updated to
match.

## Goals

- Provide a complete Python client for all built-in apikit API endpoints.
- Use typed dataclasses for all request/response objects matching OpenAPI schemas.
- Support Bearer token authentication via Authorization header.
- Raise typed exceptions (APIError) for API errors.
- Support query parameters (e.g., `include_blocked=true`).
- Support ETag/If-None-Match conditional requests.
- Target Python 3.12+ with full type annotations.
- Use `httpx` for HTTP transport.
- Manage the project with `uv` (pyproject.toml).

## Non-Goals

- OAuth flow implementation (the SDK calls endpoints; it does not orchestrate
  browser-based OAuth flows like the CLI does).
- Admin token generation or rotation (server-side concerns).
- Caching layer or automatic retry logic.
- Async client (synchronous httpx only in first iteration).
- Code generation from the OpenAPI spec (the SDK is hand-authored).
- PyPI or private registry publishing (internal/monorepo use only for the
  first iteration).
- Formal versioning scheme or OpenAPI spec version tracking (to be revisited
  if/when PyPI publishing is introduced).
- Formal dependency on or alignment enforcement with the Go SDK (both SDKs
  derive independently from the OpenAPI spec).
- Pagination support (no apikit endpoints return paginated results in the
  current iteration; this will be revisited if pagination is added to the API).
- Enum enforcement for server-defined string values (e.g., `status`, `role`);
  callers are responsible for type-narrowing these fields as needed.

## Technical Stack

| Component | Technology |
|-----------|-----------|
| Language | Python 3.12+ |
| HTTP client | httpx >=0.27 |
| Type checking | mypy >=1.10 (strict mode) |
| Linting | ruff >=0.4 |
| Testing | pytest + respx >=0.21 |
| Package management | uv |
| Project config | pyproject.toml |

## Package Structure

```
packages/sdk-python/
  pyproject.toml          # uv-managed project config
  src/
    apikit/
      __init__.py         # Exports Client, models, exceptions
      client.py           # Client class with all endpoint methods
      models.py           # Typed dataclasses for request/response objects
      exceptions.py       # APIError and related exceptions
  tests/
    __init__.py
    test_client.py        # Client unit tests (httpx mocked via respx)
    test_models.py        # Model serialization/deserialization tests
    test_exceptions.py    # Exception tests
```

## pyproject.toml Configuration

The `pyproject.toml` must specify the following:

- **Package name / import name:** `apikit` (matching `src/apikit/`). There is
  no naming collision with the Go server because the Go server is not a Python
  package.
- **Python requirement:** `>=3.12`
- **Runtime dependencies:** `httpx>=0.27`
- **Dev/test dependencies:** `respx>=0.21`, `mypy>=1.10`, `ruff>=0.4`, `pytest`
- **No PyPI publishing configuration** (internal/monorepo use only).

Example `pyproject.toml` structure:

```toml
[project]
name = "apikit"
version = "0.1.0"
requires-python = ">=3.12"
dependencies = ["httpx>=0.27"]

[project.optional-dependencies]
dev = ["respx>=0.21", "mypy>=1.10", "ruff>=0.4", "pytest"]

[tool.mypy]
strict = true

[tool.ruff]
# ruff defaults are acceptable; extend as needed
```

## Detailed Requirements

### Client Class

The client is instantiated with a server root URL, an optional mount point,
an optional API key, and an optional timeout:

```python
from apikit import Client

client = Client(
    "https://api.example.com",
    mount_point="/api/v1",
    api_key="ak_abc12345_secret",
    timeout=30.0,
)
```

Constructor parameters:
- `base_url` (str, required): The server's root URL **without** any mount
  point suffix (e.g., `https://api.example.com`). Health probe endpoints
  (`/healthz`, `/readyz`) and the version endpoint (`/version`) are resolved
  directly against this URL. **Trailing slashes are stripped on construction**
  to prevent double-slash URL construction.
- `mount_point` (str, optional, default `"/api/v1"`): The path prefix appended
  to `base_url` when constructing all API endpoint URLs (e.g., the `GET /user`
  request is sent to `https://api.example.com/api/v1/user`). **Trailing slashes
  are stripped on construction.**
- `api_key` (str | None, optional): Bearer token for authentication. When
  provided, it is sent as `Authorization: Bearer <api_key>` on every request.
- `timeout` (float | None, optional, default `30.0`): Timeout in seconds for
  the underlying `httpx.Client`. Applies to each individual request. Pass
  `None` to disable the timeout entirely.

**URL construction rules:**
- `base_url` and `mount_point` are normalized by stripping trailing slashes at
  construction time. For example, `base_url='https://api.example.com/'` is
  stored as `'https://api.example.com'`, and `mount_point='/api/v1/'` is stored
  as `'/api/v1'`.
- Health probes (`/healthz`, `/readyz`) and the version endpoint (`/version`)
  are sent to `base_url` directly (no mount point). This is confirmed by the
  master PRD, which classifies all three as "public, outside mount point"
  endpoints.
- All other endpoints are sent to `base_url + mount_point + path`.

The client uses `httpx.Client` internally for HTTP transport. The client is
usable as a context manager for proper resource cleanup:

```python
with Client("https://api.example.com", api_key=key) as client:
    user = client.get_user()
```

**Context manager behavior:** `__exit__` always closes the underlying
`httpx.Client`, even if an exception occurred inside the `with` block.
Exceptions are never suppressed — `__exit__` returns `None` (falsy), so any
exception propagates normally to the caller.

### Typed Models (Dataclasses)

All request/response types are Python dataclasses with full type annotations.
Nullable fields use `T | None`. Timestamps are `datetime` objects parsed from
RFC 3339 strings.

**Serialization strategy:** The apikit API uses snake_case JSON keys throughout
(e.g., `key_id`, `created_at`, `provider_id`). Python dataclass field names
match the JSON keys directly — no camelCase mapping is required. Each
dataclass exposes a `from_dict(data: dict) -> Self` classmethod that handles:
- Parsing RFC 3339 datetime strings into `datetime` objects.
- Constructing nested dataclass instances from nested dicts.
- Passing all other fields through directly.
- **Silently ignoring any unknown/extra fields** present in the response JSON
  that are not defined on the dataclass. This ensures forward-compatibility
  when the server adds new fields in future versions.

Request dataclasses expose a `to_dict() -> dict` method that serializes fields
to a plain dict suitable for JSON encoding, omitting fields that are `None`
where the field is optional.

**Request models are internal implementation details.** They are not exposed
as part of the public method signatures. All public `Client` methods accept
explicit keyword arguments rather than typed request object parameters. The
request dataclasses are used internally for serialization only.

#### Response Models

Field types for `status` (on `User` and `Organization`) and `role` (on `User`)
are plain `str`. The SDK does not enforce valid enum values — it passes through
whatever the server returns. Documented valid values are noted below for
reference, but the dataclasses use `str` to remain forward-compatible.

- **User**: `id` (str), `username` (str), `email` (str), `full_name`
  (str | None), `status` (str; known values: `"active"`, `"blocked"`),
  `role` (str; known values: `"admin"`, `"user"`), `provider` (str),
  `provider_id` (str), `created_at` (datetime), `updated_at` (datetime)
- **APIKey**: `key_id` (str), `created_at` (datetime), `expires_at`
  (datetime | None), `revoked_at` (datetime | None)
- **APIKeyWithSecret**: `key` (str), `key_id` (str), `expires_at`
  (datetime | None). Note: `created_at` and `revoked_at` are intentionally
  omitted — this model represents the secret-bearing subset returned by key
  creation and refresh operations, matching the OpenAPI spec shape for those
  responses.
- **PAT**: `token_id` (str), `name` (str), `permissions` (list[str]),
  `created_at` (datetime), `expires_at` (datetime | None), `revoked_at`
  (datetime | None)
- **PATWithSecret**: `token` (str), `token_id` (str), `name` (str),
  `permissions` (list[str]), `expires_at` (datetime | None)
- **Organization**: `id` (str), `name` (str), `slug` (str), `url`
  (str | None), `status` (str; known values: `"active"`, `"blocked"`),
  `created_at` (datetime), `updated_at` (datetime)
- **OAuthProvider**: `name` (str), `authorize_url` (str)
- **AuthCallbackResponse**: `user` (User), `api_key` (APIKeyWithSecret)
- **VersionInfo**: `version` (str), `build_time` (str), `commit` (str),
  `mount_point` (str)
- **HealthStatus**: `status` (str; known values: `"ok"`, `"error"`)

> **Note:** `OrgMember` is not a separate type. Organization member listings
> return `User` objects directly. `list_org_members` returns `list[User]`.

#### Request Models (Internal)

These dataclasses are used internally by the client for serialization. They
are not part of the public method signatures.

- **UpdateUserRequest**: `full_name` (str)
- **CreateUserRequest**: `username` (str), `email` (str), `provider` (str),
  `provider_id` (str)
- **CreateTokenRequest**: `name` (str), `permissions` (list[str]), `expires`
  (int, optional, default `90`) — unit is **days**; valid values are `0` (no
  expiry), `30`, `60`, or `90`. Expiry is calculated as exactly 24 h × N from
  the creation timestamp.
- **CreateOrgRequest**: `name` (str), `slug` (str), `url` (str | None,
  optional)
- **UpdateOrgRequest**: `name` (str | None, optional), `url` (str | None,
  optional)
- **AuthCallbackRequest**: `provider` (str), `code` (str), `redirect_uri`
  (str), `expires` (int, optional, default `90`) — unit is **days**; same
  valid values and expiry calculation as `CreateTokenRequest.expires`.

#### Error Model

- **ErrorDetail**: `code` (int), `message` (str)

### Exception Handling

API errors are raised as `APIError` exceptions:

```python
class APIError(Exception):
    def __init__(self, code: int, message: str) -> None:
        super().__init__(message)
        self.code = code        # HTTP status code (e.g., 404, 401, 409)
        self.message = message  # Human-readable error message
```

The `APIError` is raised whenever the server returns an error response (4xx or
5xx). The client attempts to parse the standard error envelope
`{"error": {"code": N, "message": "..."}}`. If the response body is not valid
JSON or does not contain the expected envelope structure, the client raises
`APIError` with the HTTP status code and the raw response text as the `message`
field (or `"Unexpected error"` if the body is empty).

Additional exception types:
- `NotFoundError(APIError)`: Raised on 404 responses.
- `UnauthorizedError(APIError)`: Raised on 401 responses.
- `ForbiddenError(APIError)`: Raised on 403 responses.
- `ConflictError(APIError)`: Raised on 409 responses.
- `NotModifiedError(Exception)`: Raised on 304 responses when using
  conditional requests with ETag/If-None-Match. This exception does **not**
  subclass `APIError` because 304 is not an error condition.

### Endpoint Methods

All methods on the `Client` class accept individual fields as explicit keyword
arguments. Request dataclasses are used internally for serialization and are
not exposed in method signatures.

#### Health/Meta (no auth required)

| Method | HTTP | Path | Base | Returns |
|--------|------|------|------|---------|
| `healthz()` | GET | `/healthz` | `base_url` | `HealthStatus` |
| `readyz()` | GET | `/readyz` | `base_url` | `HealthStatus` |
| `version()` | GET | `/version` | `base_url` | `VersionInfo` |

Health probe and version paths are resolved against `base_url` directly
(without the mount point). The master PRD confirms all three endpoints are
"public, outside mount point" — they live at the server root, not under
`/api/v1` or any other mount-point prefix.

#### Auth (no auth required)

| Method | HTTP | Path | Returns |
|--------|------|------|---------|
| `get_providers()` | GET | `/auth/providers` | `list[OAuthProvider]` |
| `exchange_oauth_code(*, provider, code, redirect_uri, expires=90)` | POST | `/auth/callback` | `AuthCallbackResponse` |

#### Authenticated User (self)

| Method | HTTP | Path | Returns |
|--------|------|------|---------|
| `get_user(*, if_none_match=None)` | GET | `/user` | `User` |
| `update_user(*, full_name)` | PATCH | `/user` | `User` |
| `list_keys()` | GET | `/user/keys` | `list[APIKey]` |
| `refresh_key(key_id)` | POST | `/user/keys/{key_id}/refresh` | `APIKeyWithSecret` |
| `revoke_key(key_id)` | DELETE | `/user/keys/{key_id}` | `None` |
| `list_tokens()` | GET | `/user/tokens` | `list[PAT]` |
| `create_token(*, name, permissions, expires=90)` | POST | `/user/tokens` | `PATWithSecret` |
| `get_token(token_id, *, if_none_match=None)` | GET | `/user/tokens/{token_id}` | `PAT` |
| `revoke_token(token_id)` | DELETE | `/user/tokens/{token_id}` | `None` |
| `list_user_orgs()` | GET | `/user/orgs` | `list[Organization]` |

#### Admin User Management

| Method | HTTP | Path | Returns |
|--------|------|------|---------|
| `list_users(*, include_blocked=False)` | GET | `/users` | `list[User]` |
| `get_user_by_id(user_id, *, if_none_match=None)` | GET | `/users/{id}` | `User` |
| `create_user(*, username, email, provider, provider_id)` | POST | `/users` | `User` |
| `update_user_by_id(user_id, *, full_name)` | PATCH | `/users/{id}` | `User` |
| `promote_user(user_id)` | POST | `/users/{id}/promote` | `User` |
| `demote_user(user_id)` | POST | `/users/{id}/demote` | `User` |
| `block_user(user_id)` | POST | `/users/{id}/block` | `User` |
| `unblock_user(user_id)` | POST | `/users/{id}/unblock` | `User` |
| `list_user_keys(user_id)` | GET | `/users/{id}/keys` | `list[APIKey]` |
| `revoke_user_key(user_id, key_id)` | DELETE | `/users/{id}/keys/{key_id}` | `None` |
| `list_user_tokens(user_id)` | GET | `/users/{id}/tokens` | `list[PAT]` |
| `revoke_user_token(user_id, token_id)` | DELETE | `/users/{id}/tokens/{token_id}` | `None` |

#### Organization Management

| Method | HTTP | Path | Returns |
|--------|------|------|---------|
| `create_org(*, name, slug, url=None)` | POST | `/orgs` | `Organization` |
| `list_orgs(*, include_blocked=False)` | GET | `/orgs` | `list[Organization]` |
| `get_org(org_id, *, if_none_match=None)` | GET | `/orgs/{id}` | `Organization` |
| `update_org(org_id, *, name=None, url=None)` | PATCH | `/orgs/{id}` | `Organization` |
| `delete_org(org_id)` | DELETE | `/orgs/{id}` | `None` |
| `block_org(org_id)` | POST | `/orgs/{id}/block` | `Organization` |
| `unblock_org(org_id)` | POST | `/orgs/{id}/unblock` | `Organization` |
| `list_org_members(org_id)` | GET | `/orgs/{id}/members` | `list[User]` |
| `add_org_member(org_id, user_id)` | PUT | `/orgs/{id}/members/{user_id}` | `None` |
| `remove_org_member(org_id, user_id)` | DELETE | `/orgs/{id}/members/{user_id}` | `None` |

### HTTP Status Codes

The following status codes govern client parsing behavior:

| Status | Meaning | Client behavior |
|--------|---------|----------------|
| 200 | OK | Parse response body into return type |
| 201 | Created | Parse response body into return type (POST endpoints creating resources) |
| 204 | No Content | Return `None` (DELETE and PUT endpoints) |
| 304 | Not Modified | Raise `NotModifiedError` (conditional GET with If-None-Match) |
| 401 | Unauthorized | Raise `UnauthorizedError` |
| 403 | Forbidden | Raise `ForbiddenError` |
| 404 | Not Found | Raise `NotFoundError` |
| 409 | Conflict | Raise `ConflictError` |
| 4xx / 5xx | Error | Raise `APIError` |

**`refresh_key` specifically returns HTTP 200** (not 201), and the client
parses the response body as `APIKeyWithSecret`.

### ETag / If-None-Match Support

Methods that support conditional GET accept an optional `if_none_match`
keyword argument (str). When provided, the client sends an `If-None-Match`
header with the given ETag value. If the server returns 304, the method
raises `NotModifiedError`.

The ETag value from GET responses is available via a `last_etag` property
on the client, updated after each successful GET response:
- If the response includes an `ETag` header, `last_etag` is set to that value.
- If the response does **not** include an `ETag` header, `last_etag` is reset
  to `None`.

> **Thread-safety note:** The `last_etag` and `last_request_id` properties are
> instance-level mutable state. Because the client is synchronous-only (no
> async support in this iteration), concurrent use from multiple threads is not
> supported. Callers sharing a `Client` instance across threads must apply
> their own synchronization or use separate `Client` instances per thread.

### Response Header Access

Each successful response stores the `X-Request-ID` and `ETag` headers (when
present) on the client instance for inspection:

- `client.last_request_id` -> `str | None`
- `client.last_etag` -> `str | None`

Both properties are reset to `None` at the start of each request and then
populated from the response headers if present.

### HTTP Transport Details

- All requests set `Content-Type: application/json` when sending a body.
- All requests set `Accept: application/json`.
- Authentication is sent as `Authorization: Bearer <api_key>` when an API key
  is configured.
- The underlying `httpx.Client` is configured with the `timeout` value
  specified at construction (default `30.0` seconds). Pass `timeout=None` to
  disable the timeout entirely.
- DELETE endpoints returning 204 return `None` from the client method.
- PUT endpoints returning 204 return `None` from the client method.
- POST endpoints returning 201 parse and return the created object.
- POST endpoints returning 200 (e.g., `refresh_key`) parse and return the
  response object.

### Testing Strategy

Tests are written with `pytest` and mock the HTTP transport layer using
`respx`. No live test server is required. No minimum code coverage threshold
is enforced for the first iteration.

Tests must cover:
- Correct URL construction for all endpoint categories (base_url-only paths
  vs. mount-point paths, trailing-slash normalization).
- Correct request headers (`Authorization`, `Content-Type`, `Accept`,
  `If-None-Match`).
- Correct serialization of request bodies (datetime fields, optional fields
  omitted when None).
- Correct deserialization of response bodies into typed dataclasses (including
  datetime parsing and nested objects).
- `from_dict` silently ignores unknown/extra fields in response JSON.
- `APIError` raised on 4xx/5xx responses, including non-JSON bodies and
  empty bodies (fallback to `"Unexpected error"`).
- Subclass exceptions raised for 401 (`UnauthorizedError`), 403
  (`ForbiddenError`), 404 (`NotFoundError`), and 409 (`ConflictError`).
- `NotModifiedError` raised on 304 when `if_none_match` is provided.
- `last_etag` and `last_request_id` populated and reset correctly.
- Context manager protocol: `__exit__` always closes `httpx.Client` and never
  suppresses exceptions.
- Timeout parameter forwarded to `httpx.Client`; `timeout=None` disables it.
- `refresh_key` correctly parses a 200 response (not 201) as `APIKeyWithSecret`.
- `APIKeyWithSecret` does not include `created_at` or `revoked_at` fields.
- All method signatures use keyword arguments (no request object parameters).

## Ownership

This is a solo project with no designated owner. There is no formal review
or approval chain; the project author is responsible for keeping the SDK
aligned with any future changes to the OpenAPI specification (`api/openapi.yaml`)
and for updating this spec when the API contract evolves.

## Acceptance Criteria

1. `packages/sdk-python/pyproject.toml` exists with:
   - Package/import name `apikit`.
   - Python `>=3.12` requirement.
   - `httpx>=0.27` as a runtime dependency.
   - `respx>=0.21`, `mypy>=1.10`, `ruff>=0.4`, and `pytest` as dev/test
     dependencies.
   - No PyPI publishing configuration (internal use only).
2. `Client` class accepts `base_url` (str, required), `mount_point` (str,
   optional, default `"/api/v1"`), `api_key` (str | None, optional), and
   `timeout` (float | None, optional, default `30.0`) parameters.
3. `base_url` and `mount_point` are normalized by stripping trailing slashes
   at construction time to prevent double-slash URL construction.
4. Health probe methods (`healthz`, `readyz`) and `version()` resolve URLs
   against `base_url` directly (no mount point); all other methods resolve
   against `base_url + mount_point`.
5. All endpoint methods listed in this spec exist and return the correct typed
   objects.
6. All public `Client` methods accept individual fields as explicit keyword
   arguments. No method accepts a typed request object as a parameter.
7. All typed dataclasses expose a `from_dict` classmethod and a `to_dict`
   method; field names match the snake_case JSON keys from the API directly.
8. `from_dict` silently ignores unknown/extra fields present in the response
   JSON that are not defined on the dataclass.
9. All typed dataclasses match the OpenAPI schema definitions (field names,
   types, nullability).
10. `status` fields on `User` and `Organization`, and the `role` field on
    `User`, are typed as plain `str` (no enum enforcement). Documented known
    values are `"active"`/`"blocked"` for `status` and `"admin"`/`"user"` for
    `role`.
11. `OrgMember` is not defined as a separate type; `list_org_members` returns
    `list[User]`.
12. `APIKeyWithSecret` contains only `key`, `key_id`, and `expires_at`;
    `created_at` and `revoked_at` are intentionally absent.
13. `refresh_key` sends a POST request and parses a 200 response as
    `APIKeyWithSecret`.
14. `CreateTokenRequest.expires` and `AuthCallbackRequest.expires` are
    documented as **days** (valid values: 0, 30, 60, 90; default 90).
15. `APIError.__init__` accepts `code: int` and `message: str`; both are
    instance attributes.
16. `APIError` is raised on 4xx/5xx responses with the correct `code` and
    `message`. When the response body is not valid JSON or lacks the expected
    error envelope, `APIError` is raised with the HTTP status code and the raw
    response text (or `"Unexpected error"` if the body is empty) as the
    message.
17. Specific exception subclasses exist for 401 (`UnauthorizedError`), 403
    (`ForbiddenError`), 404 (`NotFoundError`), and 409 (`ConflictError`).
18. `NotModifiedError` is raised on 304 when using `if_none_match`; it does
    not subclass `APIError`.
19. Query parameters (`include_blocked`) are correctly passed.
20. ETag/If-None-Match conditional request support works. `last_etag` is set
    from the `ETag` response header when present, and reset to `None` when
    absent.
21. `last_request_id` is set from the `X-Request-ID` response header when
    present, and reset to `None` when absent.
22. `Authorization: Bearer` header is sent on all authenticated requests.
23. The client works as a context manager (`with Client(...) as c:`).
    `__exit__` always closes the underlying `httpx.Client` and never suppresses
    exceptions (returns `None`).
24. The `timeout` value is forwarded to `httpx.Client`; passing `timeout=None`
    disables the timeout.
25. `cd packages/sdk-python && uv run pytest -q` passes all tests.
26. `cd packages/sdk-python && uv run ruff check .` passes with no errors.
27. `cd packages/sdk-python && uv run mypy .` passes with no errors in strict
    mode.

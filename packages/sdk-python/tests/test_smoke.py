"""Property-based invariant tests and smoke tests for end-to-end paths.

Covers property tests TS-11-P1, TS-11-P3, TS-11-P6,
and smoke tests TS-11-SMOKE-1 through TS-11-SMOKE-6.
Requirements: 11-PROP-1, 11-PROP-3, 11-PROP-6,
              11-PATH-1 through 11-PATH-6.
"""

from __future__ import annotations

import json
from datetime import datetime

import httpx
import pytest
import respx

from apikit import (
    APIError,
    AuthCallbackResponse,
    Client,
    ConflictError,
    ForbiddenError,
    HealthStatus,
    NotFoundError,
    NotModifiedError,
    UnauthorizedError,
    User,
)

# ---------------------------------------------------------------------------
# Sample response fixtures
# ---------------------------------------------------------------------------

SAMPLE_USER_DICT: dict[str, object] = {
    "id": "usr_123",
    "username": "testuser",
    "email": "test@example.com",
    "full_name": "Test User",
    "role": "user",
    "status": "active",
    "provider": "github",
    "provider_id": "gh_456",
    "created_at": "2024-01-01T00:00:00Z",
    "updated_at": "2024-01-01T00:00:00Z",
}

SAMPLE_HEALTH_DICT: dict[str, str] = {"status": "ok"}

SAMPLE_VERSION_DICT: dict[str, str] = {
    "version": "1.0.0",
    "build_time": "2024-01-01T00:00:00Z",
    "commit": "abc123",
    "mount_point": "/api/v1",
}

SAMPLE_API_KEY_WITH_SECRET_DICT: dict[str, object] = {
    "key": "ak_secret_value",
    "key_id": "key-1",
    "expires_at": "2025-01-01T00:00:00Z",
}

SAMPLE_ORG_DICT: dict[str, object] = {
    "id": "org-1",
    "name": "Acme",
    "slug": "acme",
    "url": "https://acme.com",
    "status": "active",
    "created_at": "2024-01-01T00:00:00Z",
    "updated_at": "2024-06-01T00:00:00Z",
}

SAMPLE_AUTH_CALLBACK_RESPONSE_DICT: dict[str, object] = {
    "user": SAMPLE_USER_DICT,
    "api_key": SAMPLE_API_KEY_WITH_SECRET_DICT,
}


# ===========================================================================
# TS-11-P1: No double slashes in constructed URLs
#           (11-PROP-1, validates 11-REQ-2.2, 11-REQ-2.E1)
# ===========================================================================


class TestNoDoubleSlashesProperty:
    """TS-11-P1: No double slashes in URL path after scheme."""

    @pytest.mark.parametrize(
        ("base_url", "mount_point"),
        [
            ("https://api.example.com", "/api/v1"),
            ("https://api.example.com/", "/api/v1"),
            ("https://api.example.com///", "/api/v1"),
            ("https://api.example.com", "/api/v1/"),
            ("https://api.example.com/", "/api/v1/"),
            ("https://api.example.com", ""),
            ("https://api.example.com/", ""),
            ("https://api.example.com", "/v2//"),
            ("https://api.example.com///", "/v2//"),
        ],
    )
    def test_no_double_slashes_in_path(
        self, base_url: str, mount_point: str
    ) -> None:
        """After construction, base_url + mount_point + path has no //."""
        c = Client(base_url, mount_point=mount_point)
        for path in [
            "/healthz",
            "/readyz",
            "/version",
            "/user",
            "/users",
            "/orgs",
        ]:
            if path in ("/healthz", "/readyz", "/version"):
                url = c._base_url + path
            else:
                url = c._base_url + c._mount_point + path
            # Strip scheme to check path portion only
            path_part = url.split("://", 1)[-1]
            assert "//" not in path_part, (
                f"Double slash in URL: {url}"
            )


# ===========================================================================
# TS-11-P3: last_etag and last_request_id reset at request start
#           (11-PROP-3, validates 11-REQ-6.1)
# ===========================================================================


class TestHeaderResetProperty:
    """TS-11-P3: Headers reset to None at the start of each request."""

    @respx.mock
    def test_headers_reset_across_multiple_calls(self) -> None:
        """After a call with ETag, a call without ETag resets to None."""
        respx.get("https://api.example.com/api/v1/user").mock(
            side_effect=[
                # Call 1: response with ETag and X-Request-ID
                httpx.Response(
                    200,
                    json=SAMPLE_USER_DICT,
                    headers={
                        "ETag": "etag-1",
                        "X-Request-ID": "req-1",
                    },
                ),
                # Call 2: response without any extra headers
                httpx.Response(200, json=SAMPLE_USER_DICT),
                # Call 3: response with ETag only
                httpx.Response(
                    200,
                    json=SAMPLE_USER_DICT,
                    headers={"ETag": "etag-2"},
                ),
            ]
        )
        c = Client("https://api.example.com", api_key="tok")

        # Call 1: both headers present
        c.get_user()
        assert c.last_etag == "etag-1"
        assert c.last_request_id == "req-1"

        # Call 2: no headers in response -> both reset to None
        c.get_user()
        assert c.last_etag is None
        assert c.last_request_id is None

        # Call 3: only ETag present -> last_etag set, request_id None
        c.get_user()
        assert c.last_etag == "etag-2"
        assert c.last_request_id is None


# ===========================================================================
# TS-11-P6: Exception subclasses are also instances of APIError
#           (11-PROP-6, validates 11-REQ-8.5)
# ===========================================================================


class TestExceptionSubclassProperty:
    """TS-11-P6: Each 4xx exception is both specific class and APIError."""

    @pytest.mark.parametrize(
        ("status_code", "exc_cls"),
        [
            (401, UnauthorizedError),
            (403, ForbiddenError),
            (404, NotFoundError),
            (409, ConflictError),
        ],
    )
    @respx.mock
    def test_subclass_is_also_api_error(
        self, status_code: int, exc_cls: type[APIError]
    ) -> None:
        respx.get("https://api.example.com/api/v1/user").mock(
            return_value=httpx.Response(
                status_code,
                json={
                    "error": {
                        "code": status_code,
                        "message": "err",
                    }
                },
            )
        )
        c = Client("https://api.example.com", api_key="tok")
        raised = None
        try:
            c.get_user()
        except exc_cls as e:
            raised = e
        assert raised is not None
        assert isinstance(raised, exc_cls)
        assert isinstance(raised, APIError)


# ===========================================================================
# TS-11-SMOKE-1: Successful authenticated GET with ETag caching
#                (11-PATH-1)
# ===========================================================================


class TestSmoke1AuthenticatedGetWithETag:
    """SMOKE-1: Full GET /user path with ETag caching."""

    @respx.mock
    def test_authenticated_get_with_etag(self) -> None:
        """End-to-end: Client with trailing slashes, GET /user with ETag."""
        route = respx.get("https://api.example.com/api/v1/user").mock(
            return_value=httpx.Response(
                200,
                json=SAMPLE_USER_DICT,
                headers={
                    "ETag": '"etag-value"',
                    "X-Request-ID": "req-abc",
                },
            )
        )
        c = Client(
            "https://api.example.com/",
            mount_point="/api/v1/",
            api_key="my-key",
        )
        user = c.get_user()

        # URL has no double slashes
        assert route.called
        request_url = str(route.calls.last.request.url)
        assert request_url == "https://api.example.com/api/v1/user"

        # Correct headers sent
        req_headers = route.calls.last.request.headers
        assert req_headers["authorization"] == "Bearer my-key"
        assert req_headers["accept"] == "application/json"

        # Response header capture
        assert c.last_etag == '"etag-value"'
        assert c.last_request_id == "req-abc"

        # Typed deserialization
        assert isinstance(user, User)
        assert isinstance(user.created_at, datetime)
        assert user.username == "testuser"


# ===========================================================================
# TS-11-SMOKE-2: API error response with typed exception
#                (11-PATH-2)
# ===========================================================================


class TestSmoke2ErrorResponse404:
    """SMOKE-2: GET /users/{id} returning 404 raises NotFoundError."""

    @respx.mock
    def test_not_found_error_raised(self) -> None:
        """End-to-end: non-existent user triggers NotFoundError."""
        respx.get(
            "https://api.example.com/api/v1/users/non-existent-id"
        ).mock(
            return_value=httpx.Response(
                404,
                json={
                    "error": {
                        "code": 404,
                        "message": "user not found",
                    }
                },
            )
        )
        c = Client("https://api.example.com", api_key="tok")
        raised = None
        try:
            c.get_user_by_id("non-existent-id")
        except NotFoundError as e:
            raised = e
        assert raised is not None
        assert raised.code == 404
        assert raised.message == "user not found"
        assert isinstance(raised, APIError)


# ===========================================================================
# TS-11-SMOKE-3: Conditional GET raising NotModifiedError
#                (11-PATH-3)
# ===========================================================================


class TestSmoke3ConditionalGetNotModified:
    """SMOKE-3: GET /orgs/{id} with if_none_match raises NotModifiedError."""

    @respx.mock
    def test_304_raises_not_modified(self) -> None:
        """End-to-end: conditional GET on org returns 304."""
        respx.get("https://api.example.com/api/v1/orgs/org-1").mock(
            return_value=httpx.Response(304)
        )
        c = Client("https://api.example.com", api_key="tok")
        raised = None
        try:
            c.get_org("org-1", if_none_match='"prev-etag"')
        except NotModifiedError as e:
            raised = e
        assert raised is not None
        assert not isinstance(raised, APIError)


# ===========================================================================
# TS-11-SMOKE-4: Context manager with exception propagation
#                (11-PATH-4)
# ===========================================================================


class TestSmoke4ContextManagerExceptionPropagation:
    """SMOKE-4: Exception inside with block propagates; httpx closed."""

    def test_value_error_propagates_through_context_manager(self) -> None:
        """End-to-end: ValueError in with block propagates unchanged."""
        inner_http = None
        caught = None
        try:
            with Client(
                "https://api.example.com", api_key="tok"
            ) as client:
                inner_http = client._http_client
                assert isinstance(client, Client)
                raise ValueError("boom")
        except ValueError as e:
            caught = e

        assert caught is not None
        assert str(caught) == "boom"
        # httpx.Client must be closed after __exit__
        assert inner_http is not None
        assert inner_http.is_closed


# ===========================================================================
# TS-11-SMOKE-5: OAuth code exchange end-to-end
#                (11-PATH-5)
# ===========================================================================


class TestSmoke5OAuthCodeExchange:
    """SMOKE-5: POST /auth/callback with body, returns AuthCallbackResponse."""

    @respx.mock
    def test_oauth_exchange_end_to_end(self) -> None:
        """End-to-end: exchange_oauth_code POST and response parsing."""
        route = respx.post(
            "https://api.example.com/api/v1/auth/callback"
        ).mock(
            return_value=httpx.Response(
                200, json=SAMPLE_AUTH_CALLBACK_RESPONSE_DICT
            )
        )
        c = Client("https://api.example.com", api_key="tok")
        result = c.exchange_oauth_code(
            provider="github",
            code="abc",
            redirect_uri="https://app.example.com/cb",
            expires=30,
        )

        # Verify POST was sent to the correct URL
        assert route.called

        # Verify Content-Type header
        req_headers = route.calls.last.request.headers
        assert "application/json" in req_headers.get("content-type", "")

        # Verify request body
        body = json.loads(route.calls.last.request.content)
        assert body["provider"] == "github"
        assert body["code"] == "abc"
        assert body["redirect_uri"] == "https://app.example.com/cb"
        assert body["expires"] == 30

        # Verify response deserialization
        assert isinstance(result, AuthCallbackResponse)
        assert isinstance(result.user, User)


# ===========================================================================
# TS-11-SMOKE-6: Health probe URL resolution with trailing slashes
#                (11-PATH-6)
# ===========================================================================


class TestSmoke6HealthProbeURL:
    """SMOKE-6: Health probe resolves without mount_point or double slashes."""

    @respx.mock
    def test_health_url_with_trailing_slashes(self) -> None:
        """End-to-end: Client with trailing slashes, healthz() URL correct."""
        route = respx.get("https://api.example.com/healthz").mock(
            return_value=httpx.Response(200, json={"status": "ok"})
        )
        c = Client(
            "https://api.example.com/", mount_point="/api/v1/"
        )
        result = c.healthz()

        # URL is exactly base_url/healthz with no mount_point
        assert route.called
        assert (
            str(route.calls.last.request.url)
            == "https://api.example.com/healthz"
        )

        # Return type
        assert isinstance(result, HealthStatus)
        assert result.status == "ok"

        # Internal state: trailing slashes stripped
        assert c._base_url == "https://api.example.com"
        assert c._mount_point == "/api/v1"

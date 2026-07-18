"""Tests for Client class: construction, context manager, headers, and conditional GET.

Covers test specs TS-11-5 through TS-11-20 and edge cases TS-11-E2, TS-11-E3, TS-11-E4.
"""

from __future__ import annotations

import httpx
import pytest
import respx

from apikit import Client
from apikit.exceptions import APIError, NotModifiedError

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


# ===========================================================================
# TS-11-5: Client constructor accepts required/optional parameters
# ===========================================================================


class TestClientConstructor:
    """TS-11-5: Verify the Client constructor parameters and defaults."""

    def test_create_with_base_url_only(self) -> None:
        """Client can be created with just base_url; no exception raised."""
        c = Client("https://api.example.com")
        assert c is not None

    def test_create_with_all_params(self) -> None:
        """Client accepts mount_point, api_key, and timeout."""
        c = Client(
            "https://api.example.com",
            mount_point="/v2",
            api_key="tok",
            timeout=5.0,
        )
        assert c is not None

    def test_default_mount_point(self) -> None:
        """Default mount_point is '/api/v1'."""
        c = Client("https://api.example.com")
        assert c._mount_point == "/api/v1"

    def test_default_timeout(self) -> None:
        """Default timeout is 30.0."""
        c = Client("https://api.example.com")
        assert c._timeout == 30.0


# ===========================================================================
# TS-11-6: Trailing slashes stripped from base_url and mount_point
# ===========================================================================


class TestURLNormalization:
    """TS-11-6: Verify trailing slash stripping at construction time."""

    def test_trailing_slash_stripped_from_base_url(self) -> None:
        c = Client("https://api.example.com/", mount_point="/api/v1")
        assert c._base_url == "https://api.example.com"

    def test_trailing_slash_stripped_from_mount_point(self) -> None:
        c = Client("https://api.example.com", mount_point="/api/v1/")
        assert c._mount_point == "/api/v1"

    def test_both_trailing_slashes_stripped(self) -> None:
        c = Client("https://api.example.com/", mount_point="/api/v1/")
        assert c._base_url == "https://api.example.com"
        assert c._mount_point == "/api/v1"


# ===========================================================================
# TS-11-E2: Multiple trailing slashes stripped
# ===========================================================================


class TestMultipleTrailingSlashes:
    """TS-11-E2 (11-REQ-2.E1): Multiple trailing slashes stripped."""

    def test_multiple_trailing_slashes_base_url(self) -> None:
        c = Client("https://api.example.com///")
        assert c._base_url == "https://api.example.com"

    def test_multiple_trailing_slashes_mount_point(self) -> None:
        c = Client("https://api.example.com", mount_point="/api/v1///")
        assert c._mount_point == "/api/v1"


# ===========================================================================
# TS-11-E3: Empty string mount_point
# ===========================================================================


class TestEmptyMountPoint:
    """TS-11-E3 (11-REQ-2.E2): Empty string mount_point stored as ''."""

    def test_empty_mount_point_stored(self) -> None:
        c = Client("https://api.example.com", mount_point="")
        assert c._mount_point == ""

    @respx.mock
    def test_empty_mount_point_url_construction(self) -> None:
        """With empty mount_point, non-health endpoints use base_url + path."""
        route = respx.get("https://api.example.com/user").mock(
            return_value=httpx.Response(200, json=SAMPLE_USER_DICT)
        )
        c = Client("https://api.example.com", mount_point="", api_key="tok")
        c.get_user()
        assert route.called


# ===========================================================================
# TS-11-7: URL routing for health vs. mounted endpoints
# ===========================================================================


class TestURLRouting:
    """TS-11-7: healthz/readyz/version use base_url only; others use mount_point."""

    @respx.mock
    def test_healthz_url(self) -> None:
        route = respx.get("https://api.example.com/healthz").mock(
            return_value=httpx.Response(200, json=SAMPLE_HEALTH_DICT)
        )
        c = Client("https://api.example.com", mount_point="/api/v1")
        c.healthz()
        assert route.called

    @respx.mock
    def test_readyz_url(self) -> None:
        route = respx.get("https://api.example.com/readyz").mock(
            return_value=httpx.Response(200, json=SAMPLE_HEALTH_DICT)
        )
        c = Client("https://api.example.com", mount_point="/api/v1")
        c.readyz()
        assert route.called

    @respx.mock
    def test_version_url(self) -> None:
        route = respx.get("https://api.example.com/version").mock(
            return_value=httpx.Response(200, json=SAMPLE_VERSION_DICT)
        )
        c = Client("https://api.example.com", mount_point="/api/v1")
        c.version()
        assert route.called

    @respx.mock
    def test_get_user_uses_mount_point(self) -> None:
        route = respx.get("https://api.example.com/api/v1/user").mock(
            return_value=httpx.Response(200, json=SAMPLE_USER_DICT)
        )
        c = Client("https://api.example.com", mount_point="/api/v1", api_key="tok")
        c.get_user()
        assert route.called


# ===========================================================================
# TS-11-8: Timeout forwarding to httpx.Client
# ===========================================================================


class TestTimeoutForwarding:
    """TS-11-8: Verify timeout is forwarded to the underlying httpx.Client."""

    def test_custom_timeout(self) -> None:
        c = Client("https://api.example.com", timeout=5.0)
        # httpx stores timeout as httpx.Timeout; check the pool timeout value
        timeout = c._http_client.timeout
        if isinstance(timeout, httpx.Timeout):
            assert timeout.connect == 5.0
        else:
            assert timeout == 5.0

    def test_none_timeout_disables(self) -> None:
        c = Client("https://api.example.com", timeout=None)
        timeout = c._http_client.timeout
        # httpx.Timeout(None) means no timeout
        if isinstance(timeout, httpx.Timeout):
            assert timeout.connect is None
        else:
            assert timeout is None


# ===========================================================================
# TS-11-9: Context manager protocol (__enter__ returns self, __exit__ closes)
# ===========================================================================


class TestContextManager:
    """TS-11-9: Verify the context manager protocol."""

    def test_enter_returns_self(self) -> None:
        with Client("https://api.example.com") as c:
            assert c is not None
            assert isinstance(c, Client)

    def test_exit_closes_http_client(self) -> None:
        with Client("https://api.example.com") as c:
            inner_http = c._http_client
        assert inner_http.is_closed


# ===========================================================================
# TS-11-10: Exceptions propagate through __exit__
# ===========================================================================


class TestContextManagerExceptionPropagation:
    """TS-11-10: Verify exceptions inside the with block propagate."""

    def test_exception_propagates(self) -> None:
        caught_exception = None
        inner_http = None
        try:
            with Client("https://api.example.com") as c:
                inner_http = c._http_client
                raise ValueError("test error")
        except ValueError as e:
            caught_exception = e
        assert caught_exception is not None
        assert str(caught_exception) == "test error"
        assert inner_http is not None
        assert inner_http.is_closed

    def test_runtime_error_propagates(self) -> None:
        """RuntimeError also propagates (verifying __exit__ returns falsy)."""
        with pytest.raises(RuntimeError, match="boom"):
            with Client("https://api.example.com") as _c:
                raise RuntimeError("boom")


# ===========================================================================
# TS-11-E4: Close exception propagation
# ===========================================================================


class TestCloseExceptionPropagation:
    """TS-11-E4 (11-REQ-3.E1): Exception from close() propagates."""

    def test_close_exception_propagates(self) -> None:
        """If httpx.Client.close() raises, the exception propagates."""
        raised = None
        try:
            c = Client("https://api.example.com")
            # Patch the instance's close method directly (not at class level)
            # to ensure we intercept the actual close call.
            c._http_client.close = lambda: (_ for _ in ()).throw(  # type: ignore[method-assign]
                RuntimeError("close failed")
            )
            with c:
                pass  # no exception inside block
        except RuntimeError as e:
            raised = e
        assert raised is not None
        assert str(raised) == "close failed"

    def test_close_exception_via_mock(self) -> None:
        """Alternative: use unittest.mock to patch the instance close method."""
        from unittest.mock import MagicMock

        raised = None
        try:
            c = Client("https://api.example.com")
            mock_close = MagicMock(side_effect=RuntimeError("close failed"))
            c._http_client.close = mock_close  # type: ignore[method-assign]
            with c:
                pass
        except RuntimeError as e:
            raised = e
        assert raised is not None
        assert str(raised) == "close failed"


# ===========================================================================
# TS-11-11: Authorization header with api_key
# ===========================================================================


class TestAuthorizationHeader:
    """TS-11-11: When api_key is set, Authorization: Bearer is sent."""

    @respx.mock
    def test_bearer_token_sent(self) -> None:
        route = respx.get("https://api.example.com/api/v1/user").mock(
            return_value=httpx.Response(200, json=SAMPLE_USER_DICT)
        )
        c = Client("https://api.example.com", api_key="ak_secret")
        c.get_user()
        assert route.calls.last is not None
        assert (
            route.calls.last.request.headers["authorization"] == "Bearer ak_secret"
        )


# ===========================================================================
# TS-11-12: No Authorization header when api_key is None
# ===========================================================================


class TestNoAuthorizationHeader:
    """TS-11-12: When api_key is None, no Authorization header is sent."""

    @respx.mock
    def test_no_auth_header(self) -> None:
        route = respx.get("https://api.example.com/healthz").mock(
            return_value=httpx.Response(200, json=SAMPLE_HEALTH_DICT)
        )
        c = Client("https://api.example.com", api_key=None)
        c.healthz()
        assert route.calls.last is not None
        assert "authorization" not in route.calls.last.request.headers


# ===========================================================================
# TS-11-13: Accept: application/json on all requests
# ===========================================================================


class TestAcceptHeader:
    """TS-11-13: Every outgoing request includes Accept: application/json."""

    @respx.mock
    def test_accept_header_present(self) -> None:
        route = respx.get("https://api.example.com/healthz").mock(
            return_value=httpx.Response(200, json=SAMPLE_HEALTH_DICT)
        )
        c = Client("https://api.example.com")
        c.healthz()
        assert route.calls.last is not None
        assert route.calls.last.request.headers["accept"] == "application/json"


# ===========================================================================
# TS-11-14: Content-Type: application/json on POST/PATCH requests
# ===========================================================================


class TestContentTypeHeader:
    """TS-11-14: POST and PATCH requests include Content-Type: application/json."""

    @respx.mock
    def test_patch_content_type(self) -> None:
        route = respx.patch("https://api.example.com/api/v1/user").mock(
            return_value=httpx.Response(200, json=SAMPLE_USER_DICT)
        )
        c = Client("https://api.example.com", api_key="tok")
        c.update_user(full_name="Test")
        assert route.calls.last is not None
        content_type = route.calls.last.request.headers["content-type"]
        assert "application/json" in content_type


# ===========================================================================
# TS-11-15: last_etag and last_request_id reset and populated
# ===========================================================================


class TestResponseHeaderCapture:
    """TS-11-15: last_etag/last_request_id reset at start, populated from response."""

    @respx.mock
    def test_headers_captured_then_reset(self) -> None:
        """First call sets headers; second call (no headers) resets to None."""
        respx.get("https://api.example.com/api/v1/user").mock(
            side_effect=[
                httpx.Response(
                    200,
                    json=SAMPLE_USER_DICT,
                    headers={"ETag": "etag-1", "X-Request-ID": "req-1"},
                ),
                httpx.Response(200, json=SAMPLE_USER_DICT),
            ]
        )
        c = Client("https://api.example.com", api_key="tok")

        # First call: headers present
        c.get_user()
        assert c.last_etag == "etag-1"
        assert c.last_request_id == "req-1"

        # Second call: headers absent
        c.get_user()
        assert c.last_etag is None
        assert c.last_request_id is None


# ===========================================================================
# TS-11-16: last_etag is None when response has no ETag header
# ===========================================================================


class TestNoETagHeader:
    """TS-11-16: last_etag is None when response lacks ETag."""

    @respx.mock
    def test_last_etag_none_without_etag_header(self) -> None:
        respx.get("https://api.example.com/healthz").mock(
            return_value=httpx.Response(200, json=SAMPLE_HEALTH_DICT)
        )
        c = Client("https://api.example.com")
        c.healthz()
        assert c.last_etag is None


# ===========================================================================
# TS-11-17: last_request_id is None when response has no X-Request-ID header
# ===========================================================================


class TestNoRequestIDHeader:
    """TS-11-17: last_request_id is None when response lacks X-Request-ID."""

    @respx.mock
    def test_last_request_id_none_without_header(self) -> None:
        respx.get("https://api.example.com/healthz").mock(
            return_value=httpx.Response(200, json=SAMPLE_HEALTH_DICT)
        )
        c = Client("https://api.example.com")
        c.healthz()
        assert c.last_request_id is None


# ===========================================================================
# TS-11-18: If-None-Match header sent when if_none_match is provided
# ===========================================================================


class TestIfNoneMatchHeaderSent:
    """TS-11-18: If-None-Match header is sent with the provided ETag value."""

    @respx.mock
    def test_if_none_match_header_present(self) -> None:
        route = respx.get("https://api.example.com/api/v1/user").mock(
            return_value=httpx.Response(200, json=SAMPLE_USER_DICT)
        )
        c = Client("https://api.example.com", api_key="tok")
        c.get_user(if_none_match='"abc123"')
        assert route.calls.last is not None
        assert route.calls.last.request.headers["if-none-match"] == '"abc123"'


# ===========================================================================
# TS-11-19: NotModifiedError raised on HTTP 304
# ===========================================================================


class TestNotModifiedError:
    """TS-11-19: NotModifiedError raised on 304; not an instance of APIError."""

    @respx.mock
    def test_304_raises_not_modified_error(self) -> None:
        respx.get("https://api.example.com/api/v1/user").mock(
            return_value=httpx.Response(304)
        )
        c = Client("https://api.example.com", api_key="tok")
        raised = None
        try:
            c.get_user(if_none_match='"etag"')
        except NotModifiedError as e:
            raised = e
        assert raised is not None
        assert not isinstance(raised, APIError)


# ===========================================================================
# TS-11-20: If-None-Match header omitted when if_none_match=None
# ===========================================================================


class TestIfNoneMatchOmitted:
    """TS-11-20: If-None-Match header absent when if_none_match is None."""

    @respx.mock
    def test_no_if_none_match_header(self) -> None:
        route = respx.get("https://api.example.com/api/v1/user").mock(
            return_value=httpx.Response(200, json=SAMPLE_USER_DICT)
        )
        c = Client("https://api.example.com", api_key="tok")
        c.get_user(if_none_match=None)
        assert route.calls.last is not None
        assert "if-none-match" not in route.calls.last.request.headers

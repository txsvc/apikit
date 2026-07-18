"""Tests for exception types and error handling.

Covers test specs TS-11-21 through TS-11-26 and edge case TS-11-E5.
Requirements: 11-REQ-8.1 through 11-REQ-8.6, 11-REQ-8.E1.
"""

from __future__ import annotations

import httpx
import pytest
import respx

from apikit import (
    APIError,
    Client,
    ConflictError,
    ForbiddenError,
    NotFoundError,
    NotModifiedError,
    UnauthorizedError,
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


# ===========================================================================
# TS-11-21: APIError defines __init__ with code and message (11-REQ-8.1)
# ===========================================================================


class TestAPIErrorInit:
    """TS-11-21: APIError(code, message) stores both and is catchable as Exception."""

    def test_code_attribute(self) -> None:
        e = APIError(code=404, message="not found")
        assert e.code == 404

    def test_message_attribute(self) -> None:
        e = APIError(code=404, message="not found")
        assert e.message == "not found"

    def test_is_exception(self) -> None:
        e = APIError(code=404, message="not found")
        assert isinstance(e, Exception)

    def test_str_is_message(self) -> None:
        e = APIError(code=404, message="not found")
        assert str(e) == "not found"


# ===========================================================================
# TS-11-22: 4xx with valid JSON error envelope raises APIError (11-REQ-8.2)
# ===========================================================================


class TestAPIErrorFromEnvelope:
    """TS-11-22: Valid JSON error envelope parsed into APIError code/message."""

    @respx.mock
    def test_400_with_json_envelope(self) -> None:
        respx.get("https://api.example.com/api/v1/user").mock(
            return_value=httpx.Response(
                400,
                json={"error": {"code": 400, "message": "bad request"}},
            )
        )
        c = Client("https://api.example.com", api_key="tok")
        raised = None
        try:
            c.get_user()
        except APIError as e:
            raised = e
        assert raised is not None
        assert raised.code == 400
        assert raised.message == "bad request"


# ===========================================================================
# TS-11-23: Non-JSON body raises APIError with raw text (11-REQ-8.3)
# ===========================================================================


class TestAPIErrorNonJSON:
    """TS-11-23: Non-JSON body -> APIError with HTTP status code and raw text."""

    @respx.mock
    def test_500_with_plain_text(self) -> None:
        respx.get("https://api.example.com/api/v1/user").mock(
            return_value=httpx.Response(500, text="Internal Server Error")
        )
        c = Client("https://api.example.com", api_key="tok")
        raised = None
        try:
            c.get_user()
        except APIError as e:
            raised = e
        assert raised is not None
        assert raised.code == 500
        assert raised.message == "Internal Server Error"


# ===========================================================================
# TS-11-24: Empty body raises APIError with "Unexpected error" (11-REQ-8.4)
# ===========================================================================


class TestAPIErrorEmptyBody:
    """TS-11-24: Empty body -> APIError with code and message='Unexpected error'."""

    @respx.mock
    def test_503_with_empty_body(self) -> None:
        respx.get("https://api.example.com/api/v1/user").mock(
            return_value=httpx.Response(503, content=b"")
        )
        c = Client("https://api.example.com", api_key="tok")
        raised = None
        try:
            c.get_user()
        except APIError as e:
            raised = e
        assert raised is not None
        assert raised.code == 503
        assert raised.message == "Unexpected error"


# ===========================================================================
# TS-11-25: Typed exception subclasses for 401/403/404/409 (11-REQ-8.5)
# ===========================================================================


class TestExceptionSubclasses:
    """TS-11-25: Status-specific subclasses raised and catchable as APIError."""

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
    def test_specific_exception_raised(
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
        assert isinstance(raised, APIError)
        assert raised.code == status_code

    def test_unauthorized_is_api_error(self) -> None:
        assert issubclass(UnauthorizedError, APIError)

    def test_forbidden_is_api_error(self) -> None:
        assert issubclass(ForbiddenError, APIError)

    def test_not_found_is_api_error(self) -> None:
        assert issubclass(NotFoundError, APIError)

    def test_conflict_is_api_error(self) -> None:
        assert issubclass(ConflictError, APIError)


# ===========================================================================
# TS-11-26: NotModifiedError is Exception but not APIError (11-REQ-8.6)
# ===========================================================================


class TestNotModifiedErrorHierarchy:
    """TS-11-26: NotModifiedError is a direct subclass of Exception, not APIError."""

    def test_is_subclass_of_exception(self) -> None:
        assert issubclass(NotModifiedError, Exception)

    def test_is_not_subclass_of_api_error(self) -> None:
        assert not issubclass(NotModifiedError, APIError)

    def test_instance_is_not_api_error(self) -> None:
        e = NotModifiedError()
        assert not isinstance(e, APIError)


# ===========================================================================
# TS-11-E5: 5xx with JSON body missing code/message sub-fields (11-REQ-8.E1)
# ===========================================================================


class TestAPIErrorMalformedEnvelope:
    """TS-11-E5: JSON with error key but missing code/message falls back."""

    @respx.mock
    def test_missing_code_field(self) -> None:
        """error key present but no code sub-field -> fallback to raw text."""
        body = {"error": {"message": "oops"}}
        respx.get("https://api.example.com/api/v1/user").mock(
            return_value=httpx.Response(502, json=body)
        )
        c = Client("https://api.example.com", api_key="tok")
        raised = None
        try:
            c.get_user()
        except APIError as e:
            raised = e
        assert raised is not None
        assert raised.code == 502

    @respx.mock
    def test_missing_message_field(self) -> None:
        """error key present but no message sub-field -> fallback to raw text."""
        body = {"error": {"code": 500}}
        respx.get("https://api.example.com/api/v1/user").mock(
            return_value=httpx.Response(500, json=body)
        )
        c = Client("https://api.example.com", api_key="tok")
        raised = None
        try:
            c.get_user()
        except APIError as e:
            raised = e
        assert raised is not None
        assert raised.code == 500

    @respx.mock
    def test_no_key_error_propagates(self) -> None:
        """No KeyError or AttributeError escapes; APIError is raised."""
        body = {"error": {}}
        respx.get("https://api.example.com/api/v1/user").mock(
            return_value=httpx.Response(500, json=body)
        )
        c = Client("https://api.example.com", api_key="tok")
        with pytest.raises(APIError):
            c.get_user()

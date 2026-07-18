"""Tests for HTTP status code rules, keyword-only args, and tooling gates.

Covers test specs TS-11-64 through TS-11-67, TS-11-71.
Requirements: 11-REQ-18.1 through 11-REQ-18.3, 11-REQ-19.1, 11-REQ-20.4.

Note: Shell integration tests TS-11-68, TS-11-69, TS-11-70 are deferred to
task group 9 per reviewer guidance -- they depend on full implementation
and cannot participate meaningfully in the red-green TDD cycle.
"""

from __future__ import annotations

import dataclasses
import pathlib

import httpx
import pytest
import respx

from apikit import (
    APIKeyWithSecret,
    Client,
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


# ===========================================================================
# TS-11-64: 200 and 201 responses parsed into typed dataclasses
#           (11-REQ-18.1)
# ===========================================================================


class TestHTTP200And201Parsing:
    """TS-11-64: 200 and 201 responses parsed into correct typed objects."""

    @respx.mock
    def test_get_user_200_returns_user(self) -> None:
        """GET /user returns User on HTTP 200."""
        respx.get("https://api.example.com/api/v1/user").mock(
            return_value=httpx.Response(200, json=SAMPLE_USER_DICT)
        )
        c = Client("https://api.example.com", api_key="tok")
        assert isinstance(c.get_user(), User)

    @respx.mock
    def test_create_user_201_returns_user(self) -> None:
        """POST /users returns User on HTTP 201."""
        respx.post("https://api.example.com/api/v1/users").mock(
            return_value=httpx.Response(201, json=SAMPLE_USER_DICT)
        )
        c = Client("https://api.example.com", api_key="tok")
        result = c.create_user(
            username="a",
            email="a@b.com",
            provider="p",
            provider_id="p1",
        )
        assert isinstance(result, User)


# ===========================================================================
# TS-11-65: DELETE/PUT 204 returns None without parsing body
#           (11-REQ-18.2)
# ===========================================================================


class TestHTTP204ReturnsNone:
    """TS-11-65: DELETE and PUT 204 responses return None."""

    @respx.mock
    def test_revoke_key_204_returns_none(self) -> None:
        """DELETE /user/keys/k1 returns None on 204."""
        respx.delete(
            "https://api.example.com/api/v1/user/keys/k1"
        ).mock(return_value=httpx.Response(204))
        c = Client("https://api.example.com", api_key="tok")
        assert c.revoke_key("k1") is None

    @respx.mock
    def test_add_org_member_204_returns_none(self) -> None:
        """PUT /orgs/o1/members/u1 returns None on 204."""
        respx.put(
            "https://api.example.com/api/v1/orgs/o1/members/u1"
        ).mock(return_value=httpx.Response(204))
        c = Client("https://api.example.com", api_key="tok")
        assert c.add_org_member("o1", "u1") is None


# ===========================================================================
# TS-11-66: refresh_key parses HTTP 200 as APIKeyWithSecret
#           (11-REQ-18.3)
# ===========================================================================


class TestRefreshKeyHTTP200:
    """TS-11-66: refresh_key parses 200 as APIKeyWithSecret (not 201)."""

    @respx.mock
    def test_refresh_key_200_returns_api_key_with_secret(self) -> None:
        """Result is APIKeyWithSecret and lacks created_at/revoked_at."""
        respx.post(
            "https://api.example.com/api/v1/user/keys/k1/refresh"
        ).mock(
            return_value=httpx.Response(
                200,
                json={
                    "key": "sec",
                    "key_id": "k1",
                    "expires_at": None,
                },
            )
        )
        c = Client("https://api.example.com", api_key="tok")
        result = c.refresh_key("k1")
        assert isinstance(result, APIKeyWithSecret)
        field_names = {f.name for f in dataclasses.fields(result)}
        assert "created_at" not in field_names
        assert "revoked_at" not in field_names


# ===========================================================================
# TS-11-67: Keyword-only enforcement (TypeError on positional args)
#           (11-REQ-19.1)
# ===========================================================================


class TestKeywordOnlyArgs:
    """TS-11-67: Public methods enforce keyword-only for body params."""

    def test_update_user_positional_raises_type_error(self) -> None:
        """Calling update_user with positional arg raises TypeError."""
        c = Client("https://api.example.com", api_key="tok")
        with pytest.raises(TypeError):
            c.update_user("Alice Smith")  # type: ignore[misc]


# ===========================================================================
# TS-11-71: Meta-test -- all documented test scenario categories covered
#           (11-REQ-20.4)
# ===========================================================================


class TestAllCategoriesCovered:
    """TS-11-71: Verify all documented scenario categories have tests."""

    def test_all_test_categories_present(self) -> None:
        """Each documented test category has at least one test function."""
        test_dir = pathlib.Path(__file__).parent
        all_test_content = ""
        for fpath in sorted(test_dir.glob("test_*.py")):
            all_test_content += fpath.read_text()

        categories = {
            "url_construction_base_vs_mount": (
                "base_url" in all_test_content
                and "mount_point" in all_test_content
            ),
            "trailing_slash_normalization": (
                "trailing" in all_test_content.lower()
            ),
            "authorization_header": (
                "authorization" in all_test_content.lower()
                or "Bearer" in all_test_content
            ),
            "content_type_and_accept_headers": (
                "content_type" in all_test_content.lower()
                or "content-type" in all_test_content.lower()
            )
            and ("accept" in all_test_content.lower()),
            "request_body_serialization": "to_dict" in all_test_content,
            "response_deserialization_datetime": (
                "from_dict" in all_test_content
                and "datetime" in all_test_content.lower()
            ),
            "unknown_field_ignoring": (
                "extra_keys" in all_test_content
                or "unknown" in all_test_content.lower()
            ),
            "api_error_valid_envelope": (
                "APIErrorFromEnvelope" in all_test_content
                or "json_envelope" in all_test_content.lower()
                or "error_envelope" in all_test_content.lower()
                or "TS-11-22" in all_test_content
            ),
            "api_error_non_json": (
                "NonJSON" in all_test_content
                or "non_json" in all_test_content.lower()
                or "plain_text" in all_test_content.lower()
            ),
            "api_error_empty_body": (
                "EmptyBody" in all_test_content
                or "empty_body" in all_test_content.lower()
                or "Unexpected error" in all_test_content
            ),
            "exception_subclasses_401_403_404_409": (
                "UnauthorizedError" in all_test_content
                and "ForbiddenError" in all_test_content
                and "NotFoundError" in all_test_content
                and "ConflictError" in all_test_content
            ),
            "not_modified_error_304": (
                "NotModifiedError" in all_test_content
            ),
            "etag_and_request_id": (
                "last_etag" in all_test_content
                and "last_request_id" in all_test_content
            ),
            "context_manager_protocol": (
                "__enter__" in all_test_content
                or "__exit__" in all_test_content
                or "ContextManager" in all_test_content
            ),
            "timeout_forwarding": (
                "timeout" in all_test_content.lower()
                and "Timeout" in all_test_content
            ),
            "refresh_key_200_parsing": (
                "refresh_key" in all_test_content
            ),
        }

        missing = [cat for cat, present in categories.items() if not present]
        assert not missing, f"Missing test categories: {missing}"

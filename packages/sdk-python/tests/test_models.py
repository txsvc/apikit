"""Tests for response and request model dataclasses.

Covers test specs TS-11-27 through TS-11-37, edge cases TS-11-E6, TS-11-E7,
and property tests TS-11-P4, TS-11-P5.

Requirements: 11-REQ-9.x, 11-REQ-10.x, 11-REQ-11.1, 11-REQ-12.x.
"""

from __future__ import annotations

import dataclasses
from datetime import datetime
from typing import Any

import httpx
import pytest
import respx

from apikit import (
    PAT,
    APIKey,
    APIKeyWithSecret,
    AuthCallbackResponse,
    Client,
    HealthStatus,
    OAuthProvider,
    Organization,
    PATWithSecret,
    User,
    VersionInfo,
)

# ---------------------------------------------------------------------------
# Sample data fixtures
# ---------------------------------------------------------------------------

SAMPLE_USER_DICT: dict[str, object] = {
    "id": "u1",
    "username": "alice",
    "email": "a@b.com",
    "full_name": None,
    "status": "active",
    "role": "user",
    "provider": "github",
    "provider_id": "gh-1",
    "created_at": "2024-01-15T12:00:00Z",
    "updated_at": "2024-06-01T00:00:00Z",
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

SAMPLE_APIKEY_DICT: dict[str, object] = {
    "key_id": "key-1",
    "created_at": "2024-01-01T00:00:00Z",
    "expires_at": "2025-01-01T00:00:00Z",
    "revoked_at": None,
}

SAMPLE_APIKEY_WITH_SECRET_DICT: dict[str, object] = {
    "key": "ak_secret_value",
    "key_id": "key-1",
    "expires_at": "2025-01-01T00:00:00Z",
}

SAMPLE_PAT_DICT: dict[str, object] = {
    "token_id": "tok-1",
    "name": "my-token",
    "permissions": ["users:read", "orgs:write"],
    "created_at": "2024-01-01T00:00:00Z",
    "expires_at": "2025-01-01T00:00:00Z",
    "revoked_at": None,
}

SAMPLE_PAT_WITH_SECRET_DICT: dict[str, object] = {
    "token": "pat_secret_value",
    "token_id": "tok-1",
    "name": "my-token",
    "permissions": ["users:read"],
    "expires_at": "2025-01-01T00:00:00Z",
}

SAMPLE_OAUTH_PROVIDER_DICT: dict[str, str] = {
    "name": "github",
    "authorize_url": "https://github.com/login/oauth/authorize",
}

SAMPLE_AUTH_CALLBACK_RESPONSE_DICT: dict[str, object] = {
    "user": SAMPLE_USER_DICT,
    "api_key": SAMPLE_APIKEY_WITH_SECRET_DICT,
}

SAMPLE_VERSION_DICT: dict[str, str] = {
    "version": "1.0.0",
    "build_time": "2024-01-01T00:00:00Z",
    "commit": "abc123",
    "mount_point": "/api/v1",
}

SAMPLE_HEALTH_DICT: dict[str, str] = {"status": "ok"}


# ===========================================================================
# TS-11-27: All 10 response dataclasses importable and are dataclasses
#           (11-REQ-9.1)
# ===========================================================================


class TestResponseDataclassesExist:
    """TS-11-27: All 10 response dataclasses are importable and are dataclasses."""

    @pytest.mark.parametrize(
        "cls",
        [
            User,
            APIKey,
            APIKeyWithSecret,
            PAT,
            PATWithSecret,
            Organization,
            OAuthProvider,
            AuthCallbackResponse,
            VersionInfo,
            HealthStatus,
        ],
    )
    def test_is_dataclass(self, cls: type) -> None:
        assert dataclasses.is_dataclass(cls), f"{cls.__name__} is not a dataclass"


# ===========================================================================
# TS-11-28: snake_case field names matching JSON keys (11-REQ-9.2)
# ===========================================================================


class TestFieldNameConventions:
    """TS-11-28: Dataclass field names use snake_case matching JSON keys."""

    def test_user_has_created_at(self) -> None:
        fields = {f.name for f in dataclasses.fields(User)}
        assert "created_at" in fields

    def test_user_has_provider_id(self) -> None:
        fields = {f.name for f in dataclasses.fields(User)}
        assert "provider_id" in fields

    def test_apikey_has_key_id(self) -> None:
        fields = {f.name for f in dataclasses.fields(APIKey)}
        assert "key_id" in fields

    def test_organization_has_created_at(self) -> None:
        fields = {f.name for f in dataclasses.fields(Organization)}
        assert "created_at" in fields

    def test_pat_has_token_id(self) -> None:
        fields = {f.name for f in dataclasses.fields(PAT)}
        assert "token_id" in fields


# ===========================================================================
# TS-11-29: status and role typed as plain str (11-REQ-9.3)
# ===========================================================================


class TestPlainStrTypes:
    """TS-11-29: status on User/Organization and role on User are plain str."""

    def test_user_status_is_str(self) -> None:
        hints = {f.name: f.type for f in dataclasses.fields(User)}
        assert hints["status"] is str or hints["status"] == "str"

    def test_user_role_is_str(self) -> None:
        hints = {f.name: f.type for f in dataclasses.fields(User)}
        assert hints["role"] is str or hints["role"] == "str"

    def test_org_status_is_str(self) -> None:
        hints = {f.name: f.type for f in dataclasses.fields(Organization)}
        assert hints["status"] is str or hints["status"] == "str"

    def test_arbitrary_status_accepted(self) -> None:
        """Arbitrary string values are accepted without validation errors."""
        u = User.from_dict(
            {**SAMPLE_USER_DICT, "status": "custom_value", "role": "superuser"}
        )
        assert u.status == "custom_value"
        assert u.role == "superuser"


# ===========================================================================
# TS-11-30: APIKeyWithSecret has exactly {key, key_id, expires_at}
#           (11-REQ-9.4)
# ===========================================================================


class TestAPIKeyWithSecretFields:
    """TS-11-30: APIKeyWithSecret has exactly three fields."""

    def test_exactly_three_fields(self) -> None:
        fields = {f.name for f in dataclasses.fields(APIKeyWithSecret)}
        assert fields == {"key", "key_id", "expires_at"}

    def test_created_at_absent(self) -> None:
        fields = {f.name for f in dataclasses.fields(APIKeyWithSecret)}
        assert "created_at" not in fields

    def test_revoked_at_absent(self) -> None:
        fields = {f.name for f in dataclasses.fields(APIKeyWithSecret)}
        assert "revoked_at" not in fields


# ===========================================================================
# TS-11-31: AuthCallbackResponse has user: User and api_key: APIKeyWithSecret
#           (11-REQ-9.5)
# ===========================================================================


class TestAuthCallbackResponseFields:
    """TS-11-31: AuthCallbackResponse has user and api_key fields."""

    def test_has_user_field(self) -> None:
        fields = {f.name: f for f in dataclasses.fields(AuthCallbackResponse)}
        assert "user" in fields

    def test_has_api_key_field(self) -> None:
        fields = {f.name: f for f in dataclasses.fields(AuthCallbackResponse)}
        assert "api_key" in fields

    def test_user_type(self) -> None:
        fields = {f.name: f for f in dataclasses.fields(AuthCallbackResponse)}
        assert fields["user"].type is User or fields["user"].type == "User"

    def test_api_key_type(self) -> None:
        fields = {f.name: f for f in dataclasses.fields(AuthCallbackResponse)}
        field_type = fields["api_key"].type
        assert (
            field_type is APIKeyWithSecret or field_type == "APIKeyWithSecret"
        )


# ===========================================================================
# TS-11-32: list_org_members returns list[User]; no OrgMember type
#           (11-REQ-9.6)
# ===========================================================================


class TestListOrgMembersReturn:
    """TS-11-32: list_org_members returns list[User]; OrgMember not defined."""

    @respx.mock
    def test_returns_list_of_users(self) -> None:
        respx.get("https://api.example.com/api/v1/orgs/org-1/members").mock(
            return_value=httpx.Response(200, json=[SAMPLE_USER_DICT])
        )
        c = Client("https://api.example.com", api_key="tok")
        members = c.list_org_members("org-1")
        assert isinstance(members, list)
        assert all(isinstance(m, User) for m in members)

    def test_no_org_member_type(self) -> None:
        """Importing OrgMember from apikit must raise ImportError."""
        with pytest.raises(ImportError):
            from apikit import OrgMember  # type: ignore[attr-defined]  # noqa: F401


# ===========================================================================
# TS-11-33: from_dict parses RFC 3339 datetime strings (11-REQ-10.1)
# ===========================================================================


class TestFromDictDatetimeParsing:
    """TS-11-33: from_dict converts RFC 3339 strings to datetime objects."""

    def test_created_at_is_datetime(self) -> None:
        u = User.from_dict(SAMPLE_USER_DICT)
        assert isinstance(u.created_at, datetime)

    def test_updated_at_is_datetime(self) -> None:
        u = User.from_dict(SAMPLE_USER_DICT)
        assert isinstance(u.updated_at, datetime)

    def test_id_passthrough(self) -> None:
        u = User.from_dict(SAMPLE_USER_DICT)
        assert u.id == "u1"

    def test_username_passthrough(self) -> None:
        u = User.from_dict(SAMPLE_USER_DICT)
        assert u.username == "alice"


# ===========================================================================
# TS-11-34: from_dict silently ignores unknown/extra keys (11-REQ-10.2)
# ===========================================================================


class TestFromDictExtraKeys:
    """TS-11-34: Extra keys in from_dict input are silently ignored."""

    def test_extra_keys_ignored(self) -> None:
        result = HealthStatus.from_dict(
            {
                "status": "ok",
                "unknown_future_field": "some_value",
                "another_new_field": 42,
            }
        )
        assert result.status == "ok"


# ===========================================================================
# TS-11-P4: from_dict with extra keys never raises for any response dataclass
#           (11-PROP-4)
# ===========================================================================


# Build a mapping of (class, sample_dict) so the parametrized test covers
# every response dataclass.
_DATACLASS_SAMPLES: list[tuple[type, dict[str, object]]] = [
    (User, SAMPLE_USER_DICT),
    (APIKey, SAMPLE_APIKEY_DICT),
    (APIKeyWithSecret, SAMPLE_APIKEY_WITH_SECRET_DICT),
    (PAT, SAMPLE_PAT_DICT),
    (PATWithSecret, SAMPLE_PAT_WITH_SECRET_DICT),
    (Organization, SAMPLE_ORG_DICT),
    (OAuthProvider, dict(SAMPLE_OAUTH_PROVIDER_DICT)),
    (AuthCallbackResponse, SAMPLE_AUTH_CALLBACK_RESPONSE_DICT),
    (VersionInfo, dict(SAMPLE_VERSION_DICT)),
    (HealthStatus, dict(SAMPLE_HEALTH_DICT)),
]


class TestFromDictExtraKeysProperty:
    """TS-11-P4: Parametrized test -- from_dict with extra keys never raises."""

    @pytest.mark.parametrize(
        ("cls", "sample"),
        _DATACLASS_SAMPLES,
        ids=[c.__name__ for c, _ in _DATACLASS_SAMPLES],
    )
    def test_extra_keys_never_raise(
        self, cls: Any, sample: dict[str, object]
    ) -> None:
        augmented = {
            **sample,
            "_extra_unknown_1": "surprise",
            "_extra_unknown_2": 999,
        }
        instance = cls.from_dict(augmented)
        assert instance is not None


# ===========================================================================
# TS-11-E6: from_dict raises ValueError on malformed datetime (11-REQ-10.E1)
# ===========================================================================


class TestFromDictMalformedDatetime:
    """TS-11-E6: Malformed datetime string raises ValueError."""

    def test_malformed_created_at(self) -> None:
        raised = None
        try:
            User.from_dict(
                {
                    "id": "u1",
                    "username": "alice",
                    "email": "a@b.com",
                    "full_name": None,
                    "status": "active",
                    "role": "user",
                    "provider": "github",
                    "provider_id": "gh-1",
                    "created_at": "not-a-date",
                    "updated_at": "2024-01-01T00:00:00Z",
                }
            )
        except ValueError as e:
            raised = e
        assert raised is not None


# ===========================================================================
# TS-11-E7: from_dict raises KeyError/TypeError on missing required field
#           (11-REQ-10.E2)
# ===========================================================================


class TestFromDictMissingRequiredField:
    """TS-11-E7: Missing required fields raise KeyError or TypeError."""

    def test_missing_required_fields(self) -> None:
        raised = None
        try:
            User.from_dict({"username": "alice"})
        except (KeyError, TypeError) as e:
            raised = e
        assert raised is not None


# ===========================================================================
# TS-11-35: to_dict omits optional None fields (11-REQ-11.1)
# ===========================================================================


class TestToDictOmitsNone:
    """TS-11-35: to_dict returns dict without None-valued optional fields."""

    def test_update_org_omits_none_name(self) -> None:
        from apikit.models import UpdateOrgRequest

        result = UpdateOrgRequest(name=None, url="https://example.com").to_dict()
        assert "name" not in result
        assert result.get("url") == "https://example.com"
        assert None not in result.values()

    def test_update_org_includes_both_when_set(self) -> None:
        from apikit.models import UpdateOrgRequest

        result = UpdateOrgRequest(name="New", url="https://example.com").to_dict()
        assert result["name"] == "New"
        assert result["url"] == "https://example.com"

    def test_update_org_all_none(self) -> None:
        from apikit.models import UpdateOrgRequest

        result = UpdateOrgRequest(name=None, url=None).to_dict()
        assert result == {}


# ===========================================================================
# TS-11-36: All 6 internal request dataclasses defined (11-REQ-12.1)
# ===========================================================================


class TestInternalRequestDataclasses:
    """TS-11-36: All 6 internal request dataclasses importable as dataclasses."""

    def test_all_importable(self) -> None:
        from apikit.models import (
            AuthCallbackRequest,
            CreateOrgRequest,
            CreateTokenRequest,
            CreateUserRequest,
            UpdateOrgRequest,
            UpdateUserRequest,
        )

        for cls in [
            UpdateUserRequest,
            CreateUserRequest,
            CreateTokenRequest,
            CreateOrgRequest,
            UpdateOrgRequest,
            AuthCallbackRequest,
        ]:
            assert dataclasses.is_dataclass(cls), (
                f"{cls.__name__} is not a dataclass"
            )


# ===========================================================================
# TS-11-37: CreateTokenRequest and AuthCallbackRequest expires default 90
#           (11-REQ-12.2)
# ===========================================================================


class TestExpiresFieldDefault:
    """TS-11-37: expires field typed int with default 90."""

    def test_create_token_request_default(self) -> None:
        from apikit.models import CreateTokenRequest

        fields = {f.name: f for f in dataclasses.fields(CreateTokenRequest)}
        assert "expires" in fields
        assert fields["expires"].default == 90

    def test_auth_callback_request_default(self) -> None:
        from apikit.models import AuthCallbackRequest

        fields = {f.name: f for f in dataclasses.fields(AuthCallbackRequest)}
        assert "expires" in fields
        assert fields["expires"].default == 90

    def test_create_token_instance_default(self) -> None:
        from apikit.models import CreateTokenRequest

        instance = CreateTokenRequest(
            name="test", permissions=["users:read"]
        )
        assert instance.expires == 90

    def test_auth_callback_instance_default(self) -> None:
        from apikit.models import AuthCallbackRequest

        instance = AuthCallbackRequest(
            provider="github",
            code="abc",
            redirect_uri="https://app.example.com/cb",
        )
        assert instance.expires == 90


# ===========================================================================
# TS-11-P5: to_dict for all request dataclasses with optional None never
#           yields None values (11-PROP-5)
# ===========================================================================


class TestToDictNoneProperty:
    """TS-11-P5: Parametrized test -- to_dict() never includes None values."""

    def test_update_user_request(self) -> None:
        from apikit.models import UpdateUserRequest

        result = UpdateUserRequest(full_name="Alice").to_dict()
        assert None not in result.values()

    def test_create_org_request_with_none_url(self) -> None:
        from apikit.models import CreateOrgRequest

        result = CreateOrgRequest(
            name="Acme", slug="acme", url=None
        ).to_dict()
        assert "url" not in result
        assert None not in result.values()

    def test_update_org_request_all_none(self) -> None:
        from apikit.models import UpdateOrgRequest

        result = UpdateOrgRequest(name=None, url=None).to_dict()
        assert None not in result.values()
        assert result == {}

    def test_create_user_request_no_optional(self) -> None:
        from apikit.models import CreateUserRequest

        result = CreateUserRequest(
            username="alice",
            email="a@b.com",
            provider="github",
            provider_id="gh-1",
        ).to_dict()
        assert None not in result.values()

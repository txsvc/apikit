"""Tests for endpoint methods: health/meta, auth, and authenticated-user (self).

Covers test specs TS-11-38 through TS-11-50 and TS-11-P2.
Requirements: 11-REQ-13.1, 11-REQ-14.1, 11-REQ-14.2,
              11-REQ-15.1 through 11-REQ-15.10.
"""

from __future__ import annotations

import json

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
    NotModifiedError,
    OAuthProvider,
    Organization,
    PATWithSecret,
    User,
    VersionInfo,
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
    "version": "1.0",
    "build_time": "2024-01-01T00:00:00Z",
    "commit": "abc123",
    "mount_point": "/api/v1",
}

SAMPLE_OAUTH_PROVIDER_DICT: dict[str, str] = {
    "name": "github",
    "authorize_url": "https://github.com/login/oauth/authorize",
}

SAMPLE_API_KEY_DICT: dict[str, object] = {
    "key_id": "key-1",
    "created_at": "2024-01-01T00:00:00Z",
    "expires_at": "2025-01-01T00:00:00Z",
    "revoked_at": None,
}

SAMPLE_API_KEY_WITH_SECRET_DICT: dict[str, object] = {
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
# TS-11-38: healthz(), readyz(), version() hit base_url and return typed objects
#           (11-REQ-13.1)
# ===========================================================================


class TestHealthMetaEndpoints:
    """TS-11-38: healthz, readyz, version return correct typed objects."""

    @respx.mock
    def test_healthz_returns_health_status(self) -> None:
        """healthz() sends GET to base_url/healthz, returns HealthStatus."""
        respx.get("https://api.example.com/healthz").mock(
            return_value=httpx.Response(200, json={"status": "ok"})
        )
        c = Client("https://api.example.com", mount_point="/api/v1")
        h = c.healthz()
        assert isinstance(h, HealthStatus)
        assert h.status == "ok"

    @respx.mock
    def test_readyz_returns_health_status(self) -> None:
        """readyz() sends GET to base_url/readyz, returns HealthStatus."""
        respx.get("https://api.example.com/readyz").mock(
            return_value=httpx.Response(200, json={"status": "ok"})
        )
        c = Client("https://api.example.com", mount_point="/api/v1")
        r = c.readyz()
        assert isinstance(r, HealthStatus)
        assert r.status == "ok"

    @respx.mock
    def test_version_returns_version_info(self) -> None:
        """version() sends GET to base_url/version, returns VersionInfo."""
        respx.get("https://api.example.com/version").mock(
            return_value=httpx.Response(200, json=SAMPLE_VERSION_DICT)
        )
        c = Client("https://api.example.com", mount_point="/api/v1")
        v = c.version()
        assert isinstance(v, VersionInfo)
        assert v.version == "1.0"

    @respx.mock
    def test_all_health_endpoints_hit_base_url_only(self) -> None:
        """Health/meta URLs hit base_url directly, without mount_point."""
        h_route = respx.get("https://api.example.com/healthz").mock(
            return_value=httpx.Response(200, json=SAMPLE_HEALTH_DICT)
        )
        r_route = respx.get("https://api.example.com/readyz").mock(
            return_value=httpx.Response(200, json=SAMPLE_HEALTH_DICT)
        )
        v_route = respx.get("https://api.example.com/version").mock(
            return_value=httpx.Response(200, json=SAMPLE_VERSION_DICT)
        )
        c = Client("https://api.example.com", mount_point="/api/v1")
        c.healthz()
        c.readyz()
        c.version()
        assert h_route.called
        assert r_route.called
        assert v_route.called


# ===========================================================================
# TS-11-P2: Health/version URLs never contain mount_point string
#           (11-PROP-2)
# ===========================================================================


class TestHealthURLNeverContainsMountPoint:
    """TS-11-P2: Parametrized over mount_point values."""

    @pytest.mark.parametrize(
        "mount_point",
        ["/api/v1", "/v2", "/custom/prefix", "/api/v1/"],
    )
    @respx.mock
    def test_healthz_url_excludes_mount_point(
        self, mount_point: str
    ) -> None:
        """healthz URL never includes the mount_point path."""
        route = respx.get("https://api.example.com/healthz").mock(
            return_value=httpx.Response(200, json=SAMPLE_HEALTH_DICT)
        )
        c = Client("https://api.example.com", mount_point=mount_point)
        c.healthz()
        assert route.called
        mp_stripped = mount_point.rstrip("/")
        request_path = str(route.calls.last.request.url).replace(
            "https://api.example.com", ""
        )
        assert mp_stripped not in request_path

    @pytest.mark.parametrize(
        "mount_point",
        ["/api/v1", "/v2", "/custom/prefix"],
    )
    @respx.mock
    def test_version_url_excludes_mount_point(
        self, mount_point: str
    ) -> None:
        """version URL never includes the mount_point path."""
        route = respx.get("https://api.example.com/version").mock(
            return_value=httpx.Response(200, json=SAMPLE_VERSION_DICT)
        )
        c = Client("https://api.example.com", mount_point=mount_point)
        c.version()
        assert route.called
        mp_stripped = mount_point.rstrip("/")
        request_path = str(route.calls.last.request.url).replace(
            "https://api.example.com", ""
        )
        assert mp_stripped not in request_path


# ===========================================================================
# TS-11-39: get_providers() returns list[OAuthProvider]
#           (11-REQ-14.1)
# ===========================================================================


class TestGetProviders:
    """TS-11-39: get_providers sends GET to mount_point+/auth/providers."""

    @respx.mock
    def test_get_providers_returns_list_of_oauth_provider(self) -> None:
        respx.get("https://api.example.com/api/v1/auth/providers").mock(
            return_value=httpx.Response(
                200, json=[SAMPLE_OAUTH_PROVIDER_DICT]
            )
        )
        c = Client("https://api.example.com", api_key="tok")
        providers = c.get_providers()
        assert isinstance(providers, list)
        assert len(providers) == 1
        assert isinstance(providers[0], OAuthProvider)
        assert providers[0].name == "github"


# ===========================================================================
# TS-11-40: exchange_oauth_code() POST with body, returns AuthCallbackResponse
#           (11-REQ-14.2)
# ===========================================================================


class TestExchangeOAuthCode:
    """TS-11-40: exchange_oauth_code sends POST to /auth/callback."""

    @respx.mock
    def test_sends_post_and_returns_auth_callback_response(self) -> None:
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
        assert isinstance(result, AuthCallbackResponse)
        assert route.calls.last is not None
        body = json.loads(route.calls.last.request.content)
        assert body["provider"] == "github"
        assert body["code"] == "abc"
        assert body["redirect_uri"] == "https://app.example.com/cb"
        assert body["expires"] == 30


# ===========================================================================
# TS-11-41: get_user() returns User on 200; raises NotModifiedError on 304
#           (11-REQ-15.1)
# ===========================================================================


class TestGetUserEndpoint:
    """TS-11-41: get_user returns User on 200, NotModifiedError on 304."""

    @respx.mock
    def test_get_user_returns_user_on_200(self) -> None:
        respx.get("https://api.example.com/api/v1/user").mock(
            return_value=httpx.Response(200, json=SAMPLE_USER_DICT)
        )
        c = Client("https://api.example.com", api_key="tok")
        u = c.get_user()
        assert isinstance(u, User)

    @respx.mock
    def test_get_user_raises_not_modified_on_304(self) -> None:
        respx.get("https://api.example.com/api/v1/user").mock(
            side_effect=[
                httpx.Response(200, json=SAMPLE_USER_DICT),
                httpx.Response(304),
            ]
        )
        c = Client("https://api.example.com", api_key="tok")
        u = c.get_user()
        assert isinstance(u, User)
        with pytest.raises(NotModifiedError):
            c.get_user(if_none_match="etag")


# ===========================================================================
# TS-11-42: update_user() sends PATCH with body, returns User
#           (11-REQ-15.2)
# ===========================================================================


class TestUpdateUserEndpoint:
    """TS-11-42: update_user sends PATCH to /user with body."""

    @respx.mock
    def test_update_user_sends_patch_and_returns_user(self) -> None:
        route = respx.patch("https://api.example.com/api/v1/user").mock(
            return_value=httpx.Response(200, json=SAMPLE_USER_DICT)
        )
        c = Client("https://api.example.com", api_key="tok")
        result = c.update_user(full_name="Alice Smith")
        assert isinstance(result, User)
        assert route.calls.last is not None
        body = json.loads(route.calls.last.request.content)
        assert body["full_name"] == "Alice Smith"


# ===========================================================================
# TS-11-43: list_keys() returns list[APIKey]
#           (11-REQ-15.3)
# ===========================================================================


class TestListKeysEndpoint:
    """TS-11-43: list_keys sends GET to /user/keys."""

    @respx.mock
    def test_list_keys_returns_list_of_api_key(self) -> None:
        respx.get("https://api.example.com/api/v1/user/keys").mock(
            return_value=httpx.Response(200, json=[SAMPLE_API_KEY_DICT])
        )
        c = Client("https://api.example.com", api_key="tok")
        keys = c.list_keys()
        assert isinstance(keys, list)
        assert all(isinstance(k, APIKey) for k in keys)


# ===========================================================================
# TS-11-44: refresh_key() POST to /user/keys/{key_id}/refresh, HTTP 200
#           (11-REQ-15.4)
# ===========================================================================


class TestRefreshKeyEndpoint:
    """TS-11-44: refresh_key sends POST and parses HTTP 200."""

    @respx.mock
    def test_refresh_key_returns_api_key_with_secret(self) -> None:
        route = respx.post(
            "https://api.example.com/api/v1/user/keys/key-abc/refresh"
        ).mock(
            return_value=httpx.Response(
                200, json=SAMPLE_API_KEY_WITH_SECRET_DICT
            )
        )
        c = Client("https://api.example.com", api_key="tok")
        result = c.refresh_key("key-abc")
        assert isinstance(result, APIKeyWithSecret)
        assert route.called


# ===========================================================================
# TS-11-45: revoke_key() DELETE returns None on 204
#           (11-REQ-15.5)
# ===========================================================================


class TestRevokeKeyEndpoint:
    """TS-11-45: revoke_key sends DELETE to /user/keys/{key_id}."""

    @respx.mock
    def test_revoke_key_returns_none_on_204(self) -> None:
        respx.delete(
            "https://api.example.com/api/v1/user/keys/key-abc"
        ).mock(return_value=httpx.Response(204))
        c = Client("https://api.example.com", api_key="tok")
        c.revoke_key("key-abc")  # returns None on 204


# ===========================================================================
# TS-11-46: list_tokens() returns list[PAT]
#           (11-REQ-15.6)
# ===========================================================================


class TestListTokensEndpoint:
    """TS-11-46: list_tokens sends GET to /user/tokens."""

    @respx.mock
    def test_list_tokens_returns_list_of_pat(self) -> None:
        respx.get("https://api.example.com/api/v1/user/tokens").mock(
            return_value=httpx.Response(200, json=[SAMPLE_PAT_DICT])
        )
        c = Client("https://api.example.com", api_key="tok")
        tokens = c.list_tokens()
        assert isinstance(tokens, list)
        assert all(isinstance(t, PAT) for t in tokens)


# ===========================================================================
# TS-11-47: create_token() POST with body, returns PATWithSecret on 201
#           (11-REQ-15.7)
# ===========================================================================


class TestCreateTokenEndpoint:
    """TS-11-47: create_token sends POST to /user/tokens."""

    @respx.mock
    def test_create_token_returns_pat_with_secret(self) -> None:
        route = respx.post(
            "https://api.example.com/api/v1/user/tokens"
        ).mock(
            return_value=httpx.Response(
                201, json=SAMPLE_PAT_WITH_SECRET_DICT
            )
        )
        c = Client("https://api.example.com", api_key="tok")
        result = c.create_token(
            name="my-token", permissions=["read"], expires=30
        )
        assert isinstance(result, PATWithSecret)
        assert route.calls.last is not None
        body = json.loads(route.calls.last.request.content)
        assert body["name"] == "my-token"
        assert body["permissions"] == ["read"]
        assert body["expires"] == 30


# ===========================================================================
# TS-11-48: get_token() returns PAT on 200; raises NotModifiedError on 304
#           (11-REQ-15.8)
# ===========================================================================


class TestGetTokenEndpoint:
    """TS-11-48: get_token returns PAT on 200, NotModifiedError on 304."""

    @respx.mock
    def test_get_token_returns_pat_on_200(self) -> None:
        respx.get(
            "https://api.example.com/api/v1/user/tokens/tok-1"
        ).mock(
            return_value=httpx.Response(200, json=SAMPLE_PAT_DICT)
        )
        c = Client("https://api.example.com", api_key="tok")
        t = c.get_token("tok-1")
        assert isinstance(t, PAT)

    @respx.mock
    def test_get_token_raises_not_modified_on_304(self) -> None:
        respx.get(
            "https://api.example.com/api/v1/user/tokens/tok-1"
        ).mock(
            side_effect=[
                httpx.Response(200, json=SAMPLE_PAT_DICT),
                httpx.Response(304),
            ]
        )
        c = Client("https://api.example.com", api_key="tok")
        t = c.get_token("tok-1")
        assert isinstance(t, PAT)
        with pytest.raises(NotModifiedError):
            c.get_token("tok-1", if_none_match="etag")


# ===========================================================================
# TS-11-49: revoke_token() DELETE returns None on 204
#           (11-REQ-15.9)
# ===========================================================================


class TestRevokeTokenEndpoint:
    """TS-11-49: revoke_token sends DELETE to /user/tokens/{token_id}."""

    @respx.mock
    def test_revoke_token_returns_none_on_204(self) -> None:
        respx.delete(
            "https://api.example.com/api/v1/user/tokens/tok-1"
        ).mock(return_value=httpx.Response(204))
        c = Client("https://api.example.com", api_key="tok")
        c.revoke_token("tok-1")  # returns None on 204


# ===========================================================================
# TS-11-50: list_user_orgs() returns list[Organization]
#           (11-REQ-15.10)
# ===========================================================================


class TestListUserOrgsEndpoint:
    """TS-11-50: list_user_orgs sends GET to /user/orgs."""

    @respx.mock
    def test_list_user_orgs_returns_list_of_organization(self) -> None:
        respx.get("https://api.example.com/api/v1/user/orgs").mock(
            return_value=httpx.Response(200, json=[SAMPLE_ORG_DICT])
        )
        c = Client("https://api.example.com", api_key="tok")
        orgs = c.list_user_orgs()
        assert isinstance(orgs, list)
        assert all(isinstance(o, Organization) for o in orgs)

"""Tests for admin user management and organization endpoint methods.

Covers test specs TS-11-51 through TS-11-63.
Requirements: 11-REQ-16.1 through 11-REQ-16.7,
              11-REQ-17.1 through 11-REQ-17.6.
"""

from __future__ import annotations

import json

import httpx
import respx

from apikit import (
    PAT,
    APIKey,
    Client,
    NotModifiedError,
    Organization,
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

SAMPLE_API_KEY_DICT: dict[str, object] = {
    "key_id": "key-1",
    "created_at": "2024-01-01T00:00:00Z",
    "expires_at": "2025-01-01T00:00:00Z",
    "revoked_at": None,
}

SAMPLE_PAT_DICT: dict[str, object] = {
    "token_id": "tok-1",
    "name": "my-token",
    "permissions": ["users:read", "orgs:write"],
    "created_at": "2024-01-01T00:00:00Z",
    "expires_at": "2025-01-01T00:00:00Z",
    "revoked_at": None,
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


# ===========================================================================
# TS-11-51: list_users() sends GET to /users and appends include_blocked=true
#           query param only when True (11-REQ-16.1)
# ===========================================================================


class TestListUsers:
    """TS-11-51: list_users sends GET and handles include_blocked param."""

    @respx.mock
    def test_list_users_without_include_blocked(self) -> None:
        """list_users() sends GET to /users without include_blocked param."""
        route = respx.get("https://api.example.com/api/v1/users").mock(
            return_value=httpx.Response(
                200, json=[SAMPLE_USER_DICT]
            )
        )
        c = Client("https://api.example.com", api_key="tok")
        result = c.list_users()
        assert isinstance(result, list)
        assert all(isinstance(u, User) for u in result)
        assert route.called
        assert "include_blocked" not in str(
            route.calls.last.request.url
        )

    @respx.mock
    def test_list_users_with_include_blocked_true(self) -> None:
        """list_users(include_blocked=True) appends include_blocked=true."""
        route = respx.get(
            url__regex=r".*include_blocked=true.*"
        ).mock(
            return_value=httpx.Response(
                200, json=[SAMPLE_USER_DICT]
            )
        )
        c = Client("https://api.example.com", api_key="tok")
        result = c.list_users(include_blocked=True)
        assert isinstance(result, list)
        assert all(isinstance(u, User) for u in result)
        assert route.called
        assert "include_blocked=true" in str(
            route.calls.last.request.url
        )


# ===========================================================================
# TS-11-52: get_user_by_id() GET /users/{id}, returns User on 200,
#           raises NotModifiedError on 304 (11-REQ-16.2)
# ===========================================================================


class TestGetUserById:
    """TS-11-52: get_user_by_id returns User or raises NotModifiedError."""

    @respx.mock
    def test_get_user_by_id_returns_user_on_200(self) -> None:
        """GET /users/user-1 returns User on 200."""
        respx.get(
            "https://api.example.com/api/v1/users/user-1"
        ).mock(
            return_value=httpx.Response(200, json=SAMPLE_USER_DICT)
        )
        c = Client("https://api.example.com", api_key="tok")
        u = c.get_user_by_id("user-1")
        assert isinstance(u, User)

    @respx.mock
    def test_get_user_by_id_raises_not_modified_on_304(self) -> None:
        """GET /users/user-1 with if_none_match raises NotModifiedError."""
        respx.get(
            "https://api.example.com/api/v1/users/user-1"
        ).mock(
            side_effect=[
                httpx.Response(200, json=SAMPLE_USER_DICT),
                httpx.Response(304),
            ]
        )
        c = Client("https://api.example.com", api_key="tok")
        u = c.get_user_by_id("user-1")
        assert isinstance(u, User)
        raised = None
        try:
            c.get_user_by_id("user-1", if_none_match="etag")
        except NotModifiedError as e:
            raised = e
        assert raised is not None


# ===========================================================================
# TS-11-53: create_user() POST to /users with correct body, returns User
#           on 201 (11-REQ-16.3)
# ===========================================================================


class TestCreateUser:
    """TS-11-53: create_user sends POST with body and returns User on 201."""

    @respx.mock
    def test_create_user_post_body_and_returns_user(self) -> None:
        """POST /users with serialized body returns User on HTTP 201."""
        route = respx.post(
            "https://api.example.com/api/v1/users"
        ).mock(
            return_value=httpx.Response(201, json=SAMPLE_USER_DICT)
        )
        c = Client("https://api.example.com", api_key="tok")
        result = c.create_user(
            username="alice",
            email="a@b.com",
            provider="github",
            provider_id="gh-1",
        )
        assert isinstance(result, User)
        body = json.loads(route.calls.last.request.content)
        assert body == {
            "username": "alice",
            "email": "a@b.com",
            "provider": "github",
            "provider_id": "gh-1",
        }


# ===========================================================================
# TS-11-54: update_user_by_id() PATCH /users/{id} with body, returns User
#           (11-REQ-16.4)
# ===========================================================================


class TestUpdateUserById:
    """TS-11-54: update_user_by_id sends PATCH with full_name body."""

    @respx.mock
    def test_update_user_by_id_patch_and_returns_user(self) -> None:
        """PATCH /users/user-1 with body {full_name: 'Bob'} returns User."""
        route = respx.patch(
            "https://api.example.com/api/v1/users/user-1"
        ).mock(
            return_value=httpx.Response(200, json=SAMPLE_USER_DICT)
        )
        c = Client("https://api.example.com", api_key="tok")
        result = c.update_user_by_id("user-1", full_name="Bob")
        assert isinstance(result, User)
        body = json.loads(route.calls.last.request.content)
        assert body["full_name"] == "Bob"


# ===========================================================================
# TS-11-55: promote_user, demote_user, block_user, unblock_user each POST
#           to correct path and return User on 200 (11-REQ-16.5)
# ===========================================================================


class TestUserStatusChangeMethods:
    """TS-11-55: promote, demote, block, unblock user actions."""

    @respx.mock
    def test_promote_user_sends_post_and_returns_user(self) -> None:
        """POST /users/u1/promote returns User on 200."""
        route = respx.post(
            "https://api.example.com/api/v1/users/u1/promote"
        ).mock(
            return_value=httpx.Response(200, json=SAMPLE_USER_DICT)
        )
        c = Client("https://api.example.com", api_key="tok")
        result = c.promote_user("u1")
        assert isinstance(result, User)
        assert route.called

    @respx.mock
    def test_demote_user_sends_post_and_returns_user(self) -> None:
        """POST /users/u1/demote returns User on 200."""
        route = respx.post(
            "https://api.example.com/api/v1/users/u1/demote"
        ).mock(
            return_value=httpx.Response(200, json=SAMPLE_USER_DICT)
        )
        c = Client("https://api.example.com", api_key="tok")
        result = c.demote_user("u1")
        assert isinstance(result, User)
        assert route.called

    @respx.mock
    def test_block_user_sends_post_and_returns_user(self) -> None:
        """POST /users/u1/block returns User on 200."""
        route = respx.post(
            "https://api.example.com/api/v1/users/u1/block"
        ).mock(
            return_value=httpx.Response(200, json=SAMPLE_USER_DICT)
        )
        c = Client("https://api.example.com", api_key="tok")
        result = c.block_user("u1")
        assert isinstance(result, User)
        assert route.called

    @respx.mock
    def test_unblock_user_sends_post_and_returns_user(self) -> None:
        """POST /users/u1/unblock returns User on 200."""
        route = respx.post(
            "https://api.example.com/api/v1/users/u1/unblock"
        ).mock(
            return_value=httpx.Response(200, json=SAMPLE_USER_DICT)
        )
        c = Client("https://api.example.com", api_key="tok")
        result = c.unblock_user("u1")
        assert isinstance(result, User)
        assert route.called


# ===========================================================================
# TS-11-56: list_user_keys() and revoke_user_key() correct types
#           (11-REQ-16.6)
# ===========================================================================


class TestUserKeyManagement:
    """TS-11-56: list_user_keys returns list[APIKey]; revoke returns None."""

    @respx.mock
    def test_list_user_keys_returns_api_key_list(self) -> None:
        """GET /users/u1/keys returns list of APIKey on 200."""
        respx.get(
            "https://api.example.com/api/v1/users/u1/keys"
        ).mock(
            return_value=httpx.Response(
                200, json=[SAMPLE_API_KEY_DICT]
            )
        )
        c = Client("https://api.example.com", api_key="tok")
        keys = c.list_user_keys("u1")
        assert all(isinstance(k, APIKey) for k in keys)

    @respx.mock
    def test_revoke_user_key_returns_none_on_204(self) -> None:
        """DELETE /users/u1/keys/key-1 returns None on 204."""
        respx.delete(
            "https://api.example.com/api/v1/users/u1/keys/key-1"
        ).mock(return_value=httpx.Response(204))
        c = Client("https://api.example.com", api_key="tok")
        result = c.revoke_user_key("u1", "key-1")
        assert result is None


# ===========================================================================
# TS-11-57: list_user_tokens() and revoke_user_token() correct types
#           (11-REQ-16.7)
# ===========================================================================


class TestUserTokenManagement:
    """TS-11-57: list_user_tokens returns list[PAT]; revoke returns None."""

    @respx.mock
    def test_list_user_tokens_returns_pat_list(self) -> None:
        """GET /users/u1/tokens returns list of PAT on 200."""
        respx.get(
            "https://api.example.com/api/v1/users/u1/tokens"
        ).mock(
            return_value=httpx.Response(
                200, json=[SAMPLE_PAT_DICT]
            )
        )
        c = Client("https://api.example.com", api_key="tok")
        tokens = c.list_user_tokens("u1")
        assert all(isinstance(t, PAT) for t in tokens)

    @respx.mock
    def test_revoke_user_token_returns_none_on_204(self) -> None:
        """DELETE /users/u1/tokens/tok-1 returns None on 204."""
        respx.delete(
            "https://api.example.com/api/v1/users/u1/tokens/tok-1"
        ).mock(return_value=httpx.Response(204))
        c = Client("https://api.example.com", api_key="tok")
        result = c.revoke_user_token("u1", "tok-1")
        assert result is None


# ===========================================================================
# TS-11-58: create_org() POST /orgs with body, returns Organization on 201
#           (11-REQ-17.1)
# ===========================================================================


class TestCreateOrg:
    """TS-11-58: create_org sends POST with body and returns Organization."""

    @respx.mock
    def test_create_org_post_body_and_returns_organization(self) -> None:
        """POST /orgs with body returns Organization on HTTP 201."""
        route = respx.post(
            "https://api.example.com/api/v1/orgs"
        ).mock(
            return_value=httpx.Response(201, json=SAMPLE_ORG_DICT)
        )
        c = Client("https://api.example.com", api_key="tok")
        result = c.create_org(
            name="Acme", slug="acme", url="https://acme.com"
        )
        assert isinstance(result, Organization)
        body = json.loads(route.calls.last.request.content)
        assert body["name"] == "Acme" and body["slug"] == "acme"


# ===========================================================================
# TS-11-59: list_orgs() GET /orgs and include_blocked=true only when True
#           (11-REQ-17.2)
# ===========================================================================


class TestListOrgs:
    """TS-11-59: list_orgs sends GET and handles include_blocked param."""

    @respx.mock
    def test_list_orgs_without_include_blocked(self) -> None:
        """list_orgs() sends GET to /orgs without include_blocked."""
        route = respx.get(
            "https://api.example.com/api/v1/orgs"
        ).mock(
            return_value=httpx.Response(
                200, json=[SAMPLE_ORG_DICT]
            )
        )
        c = Client("https://api.example.com", api_key="tok")
        c.list_orgs()
        assert "include_blocked" not in str(
            route.calls.last.request.url
        )

    @respx.mock
    def test_list_orgs_with_include_blocked_true(self) -> None:
        """list_orgs(include_blocked=True) appends include_blocked=true."""
        route = respx.get(
            url__regex=r".*include_blocked=true.*"
        ).mock(
            return_value=httpx.Response(
                200, json=[SAMPLE_ORG_DICT]
            )
        )
        c = Client("https://api.example.com", api_key="tok")
        c.list_orgs(include_blocked=True)
        assert "include_blocked=true" in str(
            route.calls.last.request.url
        )


# ===========================================================================
# TS-11-60: get_org() GET /orgs/{id} and raises NotModifiedError on 304
#           (11-REQ-17.3)
# ===========================================================================


class TestGetOrg:
    """TS-11-60: get_org returns Organization or raises NotModifiedError."""

    @respx.mock
    def test_get_org_returns_organization_on_200(self) -> None:
        """GET /orgs/org-1 returns Organization on 200."""
        respx.get(
            "https://api.example.com/api/v1/orgs/org-1"
        ).mock(
            return_value=httpx.Response(200, json=SAMPLE_ORG_DICT)
        )
        c = Client("https://api.example.com", api_key="tok")
        o = c.get_org("org-1")
        assert isinstance(o, Organization)

    @respx.mock
    def test_get_org_raises_not_modified_on_304(self) -> None:
        """GET /orgs/org-1 with if_none_match raises NotModifiedError."""
        respx.get(
            "https://api.example.com/api/v1/orgs/org-1"
        ).mock(
            side_effect=[
                httpx.Response(200, json=SAMPLE_ORG_DICT),
                httpx.Response(304),
            ]
        )
        c = Client("https://api.example.com", api_key="tok")
        o = c.get_org("org-1")
        assert isinstance(o, Organization)
        raised = None
        try:
            c.get_org("org-1", if_none_match="etag")
        except NotModifiedError as e:
            raised = e
        assert raised is not None


# ===========================================================================
# TS-11-61: update_org() PATCH with only non-None fields and returns
#           Organization (11-REQ-17.4)
# ===========================================================================


class TestUpdateOrg:
    """TS-11-61: update_org PATCH with only non-None fields in body."""

    @respx.mock
    def test_update_org_omits_none_fields(self) -> None:
        """PATCH /orgs/org-1 body has only non-None fields."""
        route = respx.patch(
            "https://api.example.com/api/v1/orgs/org-1"
        ).mock(
            return_value=httpx.Response(200, json=SAMPLE_ORG_DICT)
        )
        c = Client("https://api.example.com", api_key="tok")
        result = c.update_org("org-1", name="NewName", url=None)
        assert isinstance(result, Organization)
        body = json.loads(route.calls.last.request.content)
        assert "name" in body and body["name"] == "NewName"
        assert "url" not in body


# ===========================================================================
# TS-11-62: delete_org(), block_org(), unblock_org() correct requests
#           (11-REQ-17.5)
# ===========================================================================


class TestDeleteBlockUnblockOrg:
    """TS-11-62: delete/block/unblock org correct requests and types."""

    @respx.mock
    def test_delete_org_returns_none_on_204(self) -> None:
        """DELETE /orgs/org-1 returns None on 204."""
        respx.delete(
            "https://api.example.com/api/v1/orgs/org-1"
        ).mock(return_value=httpx.Response(204))
        c = Client("https://api.example.com", api_key="tok")
        assert c.delete_org("org-1") is None

    @respx.mock
    def test_block_org_returns_organization_on_200(self) -> None:
        """POST /orgs/org-1/block returns Organization on 200."""
        respx.post(
            "https://api.example.com/api/v1/orgs/org-1/block"
        ).mock(
            return_value=httpx.Response(200, json=SAMPLE_ORG_DICT)
        )
        c = Client("https://api.example.com", api_key="tok")
        assert isinstance(c.block_org("org-1"), Organization)

    @respx.mock
    def test_unblock_org_returns_organization_on_200(self) -> None:
        """POST /orgs/org-1/unblock returns Organization on 200."""
        respx.post(
            "https://api.example.com/api/v1/orgs/org-1/unblock"
        ).mock(
            return_value=httpx.Response(200, json=SAMPLE_ORG_DICT)
        )
        c = Client("https://api.example.com", api_key="tok")
        assert isinstance(c.unblock_org("org-1"), Organization)


# ===========================================================================
# TS-11-63: list_org_members(), add_org_member(), remove_org_member()
#           correct requests and types (11-REQ-17.6)
# ===========================================================================


class TestOrgMemberManagement:
    """TS-11-63: org member listing, adding, and removing."""

    @respx.mock
    def test_list_org_members_returns_user_list(self) -> None:
        """GET /orgs/org-1/members returns list of User on 200."""
        respx.get(
            "https://api.example.com/api/v1/orgs/org-1/members"
        ).mock(
            return_value=httpx.Response(
                200, json=[SAMPLE_USER_DICT]
            )
        )
        c = Client("https://api.example.com", api_key="tok")
        members = c.list_org_members("org-1")
        assert all(isinstance(m, User) for m in members)

    @respx.mock
    def test_add_org_member_returns_none_on_204(self) -> None:
        """PUT /orgs/org-1/members/u-1 returns None on 204."""
        respx.put(
            "https://api.example.com/api/v1/orgs/org-1/members/u-1"
        ).mock(return_value=httpx.Response(204))
        c = Client("https://api.example.com", api_key="tok")
        assert c.add_org_member("org-1", "u-1") is None

    @respx.mock
    def test_remove_org_member_returns_none_on_204(self) -> None:
        """DELETE /orgs/org-1/members/u-1 returns None on 204."""
        respx.delete(
            "https://api.example.com/api/v1/orgs/org-1/members/u-1"
        ).mock(return_value=httpx.Response(204))
        c = Client("https://api.example.com", api_key="tok")
        assert c.remove_org_member("org-1", "u-1") is None

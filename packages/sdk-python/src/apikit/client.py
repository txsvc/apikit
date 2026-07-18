"""Client class for the apikit SDK.

Provides a typed, synchronous Python client for the apikit built-in API
endpoints. Uses httpx as the HTTP transport layer.
"""

from __future__ import annotations

from types import TracebackType
from typing import Any

import httpx

from apikit.exceptions import (
    APIError,
    ConflictError,
    ForbiddenError,
    NotFoundError,
    NotModifiedError,
    UnauthorizedError,
)
from apikit.models import (
    PAT,
    APIKey,
    APIKeyWithSecret,
    AuthCallbackResponse,
    HealthStatus,
    OAuthProvider,
    Organization,
    PATWithSecret,
    User,
    VersionInfo,
)

# Map HTTP status codes to specific exception subclasses
_STATUS_TO_EXCEPTION: dict[int, type[APIError]] = {
    401: UnauthorizedError,
    403: ForbiddenError,
    404: NotFoundError,
    409: ConflictError,
}


class Client:
    """SDK client for the apikit API.

    Args:
        base_url: The server root URL. Trailing slashes are stripped.
        mount_point: Path prefix for non-health endpoints
            (default ``/api/v1``). Trailing slashes are stripped.
        api_key: Optional Bearer token for authentication.
        timeout: Request timeout in seconds (default 30.0).
            Pass ``None`` to disable the timeout.
    """

    def __init__(
        self,
        base_url: str,
        mount_point: str = "/api/v1",
        api_key: str | None = None,
        timeout: float | None = 30.0,
    ) -> None:
        self._base_url = base_url.rstrip("/")
        self._mount_point = mount_point.rstrip("/")
        self._api_key = api_key
        self._timeout = timeout

        self._http_client = httpx.Client(timeout=timeout)

        # Response header state, reset at the start of each request
        self.last_etag: str | None = None
        self.last_request_id: str | None = None

    # ------------------------------------------------------------------
    # Context manager protocol
    # ------------------------------------------------------------------

    def __enter__(self) -> Client:
        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc_val: BaseException | None,
        exc_tb: TracebackType | None,
    ) -> None:
        self._http_client.close()

    # ------------------------------------------------------------------
    # Internal helpers
    # ------------------------------------------------------------------

    def _headers(self) -> dict[str, str]:
        """Build the common request headers."""
        headers: dict[str, str] = {
            "Accept": "application/json",
        }
        if self._api_key is not None:
            headers["Authorization"] = f"Bearer {self._api_key}"
        return headers

    def _url(self, path: str, *, use_mount: bool = True) -> str:
        """Construct the full URL for a given path.

        Args:
            path: The endpoint path (e.g. ``/user``).
            use_mount: If True, prefix with ``mount_point``.
        """
        if use_mount:
            return f"{self._base_url}{self._mount_point}{path}"
        return f"{self._base_url}{path}"

    def _raise_for_status(self, response: httpx.Response) -> None:
        """Check response status and raise appropriate exceptions.

        - 304 raises NotModifiedError
        - 4xx/5xx raises APIError or a status-specific subclass
        """
        if response.status_code == 304:
            raise NotModifiedError()

        if response.status_code >= 400:
            code = response.status_code
            message = "Unexpected error"

            text = response.text
            if text:
                try:
                    body = response.json()
                    if (
                        isinstance(body, dict)
                        and "error" in body
                        and isinstance(body["error"], dict)
                    ):
                        error_obj = body["error"]
                        if (
                            "code" in error_obj
                            and "message" in error_obj
                        ):
                            code = error_obj["code"]
                            message = error_obj["message"]
                        else:
                            # Malformed envelope: fallback to raw text
                            message = text
                    else:
                        message = text
                except Exception:
                    message = text
            exc_cls = _STATUS_TO_EXCEPTION.get(
                response.status_code, APIError
            )
            raise exc_cls(code=code, message=message)

    def _capture_headers(self, response: httpx.Response) -> None:
        """Capture ETag and X-Request-ID from the response headers."""
        self.last_etag = response.headers.get("ETag")
        self.last_request_id = response.headers.get("X-Request-ID")

    def _request(
        self,
        method: str,
        path: str,
        *,
        use_mount: bool = True,
        json: Any = None,
        params: dict[str, Any] | None = None,
        extra_headers: dict[str, str] | None = None,
    ) -> httpx.Response:
        """Send an HTTP request and handle status/header capture.

        Resets ``last_etag`` and ``last_request_id`` at the start of
        each request. On success (2xx), captures response headers.
        On error, raises the appropriate exception.
        """
        # Reset header state at start of each request (11-PROP-3)
        self.last_etag = None
        self.last_request_id = None

        headers = self._headers()
        if extra_headers:
            headers.update(extra_headers)

        url = self._url(path, use_mount=use_mount)
        response = self._http_client.request(
            method,
            url,
            headers=headers,
            json=json,
            params=params,
        )

        self._raise_for_status(response)
        self._capture_headers(response)
        return response

    # ------------------------------------------------------------------
    # Health / meta endpoints (no mount_point)
    # ------------------------------------------------------------------

    def healthz(self) -> HealthStatus:
        """GET /healthz - Health probe."""
        resp = self._request("GET", "/healthz", use_mount=False)
        return HealthStatus.from_dict(resp.json())

    def readyz(self) -> HealthStatus:
        """GET /readyz - Readiness probe."""
        resp = self._request("GET", "/readyz", use_mount=False)
        return HealthStatus.from_dict(resp.json())

    def version(self) -> VersionInfo:
        """GET /version - Server version information."""
        resp = self._request("GET", "/version", use_mount=False)
        return VersionInfo.from_dict(resp.json())

    # ------------------------------------------------------------------
    # Auth endpoints
    # ------------------------------------------------------------------

    def get_providers(self) -> list[OAuthProvider]:
        """GET /auth/providers - List OAuth providers."""
        resp = self._request("GET", "/auth/providers")
        return [OAuthProvider.from_dict(p) for p in resp.json()]

    def exchange_oauth_code(
        self,
        *,
        provider: str,
        code: str,
        redirect_uri: str,
        expires: int = 90,
    ) -> AuthCallbackResponse:
        """POST /auth/callback - Exchange OAuth code for credentials."""
        from apikit.models import AuthCallbackRequest

        body = AuthCallbackRequest(
            provider=provider,
            code=code,
            redirect_uri=redirect_uri,
            expires=expires,
        ).to_dict()
        resp = self._request("POST", "/auth/callback", json=body)
        return AuthCallbackResponse.from_dict(resp.json())

    # ------------------------------------------------------------------
    # Authenticated user (self) endpoints
    # ------------------------------------------------------------------

    def get_user(
        self,
        *,
        if_none_match: str | None = None,
    ) -> User:
        """GET /user - Get the authenticated user's profile."""
        extra: dict[str, str] | None = None
        if if_none_match is not None:
            extra = {"If-None-Match": if_none_match}
        resp = self._request("GET", "/user", extra_headers=extra)
        return User.from_dict(resp.json())

    def update_user(self, *, full_name: str) -> User:
        """PATCH /user - Update the authenticated user's profile."""
        from apikit.models import UpdateUserRequest

        body = UpdateUserRequest(full_name=full_name).to_dict()
        resp = self._request("PATCH", "/user", json=body)
        return User.from_dict(resp.json())

    def list_keys(self) -> list[APIKey]:
        """GET /user/keys - List the authenticated user's API keys."""
        resp = self._request("GET", "/user/keys")
        return [APIKey.from_dict(k) for k in resp.json()]

    def refresh_key(self, key_id: str) -> APIKeyWithSecret:
        """POST /user/keys/{key_id}/refresh - Refresh an API key."""
        resp = self._request(
            "POST", f"/user/keys/{key_id}/refresh"
        )
        return APIKeyWithSecret.from_dict(resp.json())

    def revoke_key(self, key_id: str) -> None:
        """DELETE /user/keys/{key_id} - Revoke an API key."""
        self._request("DELETE", f"/user/keys/{key_id}")

    def list_tokens(self) -> list[PAT]:
        """GET /user/tokens - List the authenticated user's PATs."""
        resp = self._request("GET", "/user/tokens")
        return [PAT.from_dict(t) for t in resp.json()]

    def create_token(
        self,
        *,
        name: str,
        permissions: list[str],
        expires: int = 90,
    ) -> PATWithSecret:
        """POST /user/tokens - Create a personal access token."""
        from apikit.models import CreateTokenRequest

        body = CreateTokenRequest(
            name=name,
            permissions=permissions,
            expires=expires,
        ).to_dict()
        resp = self._request("POST", "/user/tokens", json=body)
        return PATWithSecret.from_dict(resp.json())

    def get_token(
        self,
        token_id: str,
        *,
        if_none_match: str | None = None,
    ) -> PAT:
        """GET /user/tokens/{token_id} - Get a specific PAT."""
        extra: dict[str, str] | None = None
        if if_none_match is not None:
            extra = {"If-None-Match": if_none_match}
        resp = self._request(
            "GET", f"/user/tokens/{token_id}", extra_headers=extra
        )
        return PAT.from_dict(resp.json())

    def revoke_token(self, token_id: str) -> None:
        """DELETE /user/tokens/{token_id} - Revoke a PAT."""
        self._request("DELETE", f"/user/tokens/{token_id}")

    def list_user_orgs(self) -> list[Organization]:
        """GET /user/orgs - List orgs the authenticated user belongs to."""
        resp = self._request("GET", "/user/orgs")
        return [Organization.from_dict(o) for o in resp.json()]

    # ------------------------------------------------------------------
    # Admin user management endpoints (stubs for group 8)
    # ------------------------------------------------------------------

    def list_users(
        self,
        *,
        include_blocked: bool = False,
    ) -> list[User]:
        """GET /users - List all users (admin)."""
        raise NotImplementedError

    def create_user(
        self,
        *,
        username: str,
        email: str,
        provider: str,
        provider_id: str,
    ) -> User:
        """POST /users - Create a new user (admin)."""
        raise NotImplementedError

    def get_user_by_id(
        self,
        user_id: str,
        *,
        if_none_match: str | None = None,
    ) -> User:
        """GET /users/{user_id} - Get a user by ID (admin)."""
        raise NotImplementedError

    def update_user_by_id(
        self,
        user_id: str,
        *,
        full_name: str,
    ) -> User:
        """PATCH /users/{user_id} - Update a user by ID (admin)."""
        raise NotImplementedError

    def block_user(self, user_id: str) -> User:
        """POST /users/{user_id}/block - Block a user (admin)."""
        raise NotImplementedError

    def unblock_user(self, user_id: str) -> User:
        """POST /users/{user_id}/unblock - Unblock a user (admin)."""
        raise NotImplementedError

    def list_user_keys(self, user_id: str) -> list[APIKey]:
        """GET /users/{user_id}/keys - List a user's keys (admin)."""
        raise NotImplementedError

    def revoke_user_key(
        self, user_id: str, key_id: str
    ) -> None:
        """DELETE /users/{user_id}/keys/{key_id} - Revoke key (admin)."""
        raise NotImplementedError

    def list_user_tokens(self, user_id: str) -> list[PAT]:
        """GET /users/{user_id}/tokens - List user's PATs (admin)."""
        raise NotImplementedError

    def revoke_user_token(
        self, user_id: str, token_id: str
    ) -> None:
        """DELETE /users/{user_id}/tokens/{token_id} (admin)."""
        raise NotImplementedError

    # ------------------------------------------------------------------
    # Organization management endpoints (stubs for group 8)
    # ------------------------------------------------------------------

    def list_orgs(
        self,
        *,
        include_blocked: bool = False,
    ) -> list[Organization]:
        """GET /orgs - List organizations."""
        raise NotImplementedError

    def create_org(
        self,
        *,
        name: str,
        slug: str,
        url: str | None = None,
    ) -> Organization:
        """POST /orgs - Create an organization."""
        raise NotImplementedError

    def get_org(
        self,
        org_id: str,
        *,
        if_none_match: str | None = None,
    ) -> Organization:
        """GET /orgs/{org_id} - Get an organization."""
        raise NotImplementedError

    def update_org(
        self,
        org_id: str,
        *,
        name: str | None = None,
        url: str | None = None,
    ) -> Organization:
        """PATCH /orgs/{org_id} - Update an organization."""
        raise NotImplementedError

    def list_org_members(self, org_id: str) -> list[User]:
        """GET /orgs/{org_id}/members - List organization members."""
        resp = self._request("GET", f"/orgs/{org_id}/members")
        return [User.from_dict(u) for u in resp.json()]

    def add_org_member(
        self, org_id: str, user_id: str
    ) -> None:
        """PUT /orgs/{org_id}/members/{user_id} - Add a member."""
        raise NotImplementedError

    def remove_org_member(
        self, org_id: str, user_id: str
    ) -> None:
        """DELETE /orgs/{org_id}/members/{user_id} - Remove a member."""
        raise NotImplementedError

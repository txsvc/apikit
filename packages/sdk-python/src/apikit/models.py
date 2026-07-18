"""Response and request model dataclasses for the apikit SDK.

Response dataclasses define the typed shapes returned by API endpoints.
Request dataclasses are internal types used for serializing request bodies.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime
from typing import Any

# ---------------------------------------------------------------------------
# Helper: parse RFC 3339 datetime strings
# ---------------------------------------------------------------------------


def _parse_datetime(value: str) -> datetime:
    """Parse an RFC 3339 datetime string into a datetime object.

    Raises ValueError if the string cannot be parsed.
    """
    # Handle the common 'Z' suffix
    s = value.replace("Z", "+00:00")
    try:
        return datetime.fromisoformat(s)
    except (ValueError, TypeError) as exc:
        raise ValueError(
            f"Cannot parse datetime: {value!r}"
        ) from exc


def _parse_optional_datetime(value: object) -> datetime | None:
    """Parse a datetime string or return None if the value is None."""
    if value is None:
        return None
    if isinstance(value, str):
        return _parse_datetime(value)
    raise ValueError(f"Expected str or None, got {type(value).__name__}")


# ---------------------------------------------------------------------------
# Response models
# ---------------------------------------------------------------------------


@dataclass
class User:
    """User response model."""

    id: str
    username: str
    email: str
    full_name: str | None
    status: str
    role: str
    provider: str
    provider_id: str
    created_at: datetime
    updated_at: datetime

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> User:
        """Construct a User from a plain dict.

        Parses datetime strings and silently ignores unknown fields.
        Raises KeyError if a required field is missing.
        Raises ValueError if a datetime string is malformed.
        """
        raise NotImplementedError

    def to_dict(self) -> dict[str, Any]:
        """Serialize to a plain dict."""
        raise NotImplementedError


@dataclass
class APIKey:
    """API key response model (metadata only, no secret)."""

    key_id: str
    created_at: datetime
    expires_at: datetime | None
    revoked_at: datetime | None

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> APIKey:
        """Construct an APIKey from a plain dict."""
        raise NotImplementedError

    def to_dict(self) -> dict[str, Any]:
        """Serialize to a plain dict."""
        raise NotImplementedError


@dataclass
class APIKeyWithSecret:
    """API key with secret response model.

    Returned by key creation and refresh operations.
    Contains only key, key_id, and expires_at (no created_at or revoked_at).
    """

    key: str
    key_id: str
    expires_at: datetime | None

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> APIKeyWithSecret:
        """Construct an APIKeyWithSecret from a plain dict."""
        raise NotImplementedError

    def to_dict(self) -> dict[str, Any]:
        """Serialize to a plain dict."""
        raise NotImplementedError


@dataclass
class PAT:
    """Personal access token response model (metadata only, no secret)."""

    token_id: str
    name: str
    permissions: list[str]
    created_at: datetime
    expires_at: datetime | None
    revoked_at: datetime | None

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> PAT:
        """Construct a PAT from a plain dict."""
        raise NotImplementedError

    def to_dict(self) -> dict[str, Any]:
        """Serialize to a plain dict."""
        raise NotImplementedError


@dataclass
class PATWithSecret:
    """PAT with secret response model.

    Returned by token creation operations.
    """

    token: str
    token_id: str
    name: str
    permissions: list[str]
    expires_at: datetime | None

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> PATWithSecret:
        """Construct a PATWithSecret from a plain dict."""
        raise NotImplementedError

    def to_dict(self) -> dict[str, Any]:
        """Serialize to a plain dict."""
        raise NotImplementedError


@dataclass
class Organization:
    """Organization response model."""

    id: str
    name: str
    slug: str
    url: str | None
    status: str
    created_at: datetime
    updated_at: datetime

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> Organization:
        """Construct an Organization from a plain dict."""
        raise NotImplementedError

    def to_dict(self) -> dict[str, Any]:
        """Serialize to a plain dict."""
        raise NotImplementedError


@dataclass
class OAuthProvider:
    """OAuth provider response model."""

    name: str
    authorize_url: str

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> OAuthProvider:
        """Construct an OAuthProvider from a plain dict."""
        raise NotImplementedError

    def to_dict(self) -> dict[str, Any]:
        """Serialize to a plain dict."""
        raise NotImplementedError


@dataclass
class AuthCallbackResponse:
    """Auth callback response model.

    Contains a nested User and APIKeyWithSecret.
    """

    user: User
    api_key: APIKeyWithSecret

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> AuthCallbackResponse:
        """Construct an AuthCallbackResponse from a plain dict."""
        raise NotImplementedError

    def to_dict(self) -> dict[str, Any]:
        """Serialize to a plain dict."""
        raise NotImplementedError


@dataclass
class VersionInfo:
    """Version info response model."""

    version: str
    build_time: str
    commit: str
    mount_point: str

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> VersionInfo:
        """Construct a VersionInfo from a plain dict."""
        raise NotImplementedError

    def to_dict(self) -> dict[str, Any]:
        """Serialize to a plain dict."""
        raise NotImplementedError


@dataclass
class HealthStatus:
    """Health status response model."""

    status: str

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> HealthStatus:
        """Construct a HealthStatus from a plain dict."""
        raise NotImplementedError

    def to_dict(self) -> dict[str, Any]:
        """Serialize to a plain dict."""
        raise NotImplementedError


# ---------------------------------------------------------------------------
# Internal request models
# ---------------------------------------------------------------------------


@dataclass
class UpdateUserRequest:
    """Internal request model for updating a user profile."""

    full_name: str

    def to_dict(self) -> dict[str, Any]:
        """Serialize to a plain dict, omitting None optional fields."""
        raise NotImplementedError


@dataclass
class CreateUserRequest:
    """Internal request model for creating a user."""

    username: str
    email: str
    provider: str
    provider_id: str

    def to_dict(self) -> dict[str, Any]:
        """Serialize to a plain dict, omitting None optional fields."""
        raise NotImplementedError


@dataclass
class CreateTokenRequest:
    """Internal request model for creating a personal access token.

    The ``expires`` field is in **days** with valid values 0, 30, 60, or 90.
    Default is 90 days.
    """

    name: str
    permissions: list[str]
    expires: int = 90

    def to_dict(self) -> dict[str, Any]:
        """Serialize to a plain dict, omitting None optional fields."""
        raise NotImplementedError


@dataclass
class CreateOrgRequest:
    """Internal request model for creating an organization."""

    name: str
    slug: str
    url: str | None = None

    def to_dict(self) -> dict[str, Any]:
        """Serialize to a plain dict, omitting None optional fields."""
        raise NotImplementedError


@dataclass
class UpdateOrgRequest:
    """Internal request model for updating an organization."""

    name: str | None = None
    url: str | None = None

    def to_dict(self) -> dict[str, Any]:
        """Serialize to a plain dict, omitting None optional fields."""
        raise NotImplementedError


@dataclass
class AuthCallbackRequest:
    """Internal request model for the OAuth code exchange endpoint.

    The ``expires`` field is in **days** with valid values 0, 30, 60, or 90.
    Default is 90 days.
    """

    provider: str
    code: str
    redirect_uri: str
    expires: int = 90

    def to_dict(self) -> dict[str, Any]:
        """Serialize to a plain dict, omitting None optional fields."""
        raise NotImplementedError

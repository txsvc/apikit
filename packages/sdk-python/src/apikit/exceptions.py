"""Exception types for the apikit SDK.

Stub definitions for test collection. Full implementation in later task groups.
"""


class APIError(Exception):
    """Base exception for API errors. Stub - not yet implemented."""


class UnauthorizedError(APIError):
    """Raised on HTTP 401 responses. Stub - not yet implemented."""


class ForbiddenError(APIError):
    """Raised on HTTP 403 responses. Stub - not yet implemented."""


class NotFoundError(APIError):
    """Raised on HTTP 404 responses. Stub - not yet implemented."""


class ConflictError(APIError):
    """Raised on HTTP 409 responses. Stub - not yet implemented."""


class NotModifiedError(Exception):
    """Raised on HTTP 304 responses. Stub - not yet implemented."""

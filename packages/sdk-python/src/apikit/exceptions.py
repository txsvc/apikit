"""Exception types for the apikit SDK."""


class APIError(Exception):
    """Base exception for API errors.

    Raised when the server returns a 4xx or 5xx HTTP response.
    Carries the HTTP status code and a human-readable error message.
    """

    def __init__(self, code: int, message: str) -> None:
        self.code = code
        self.message = message
        super().__init__(message)


class UnauthorizedError(APIError):
    """Raised on HTTP 401 responses."""


class ForbiddenError(APIError):
    """Raised on HTTP 403 responses."""


class NotFoundError(APIError):
    """Raised on HTTP 404 responses."""


class ConflictError(APIError):
    """Raised on HTTP 409 responses."""


class NotModifiedError(Exception):
    """Raised on HTTP 304 responses.

    This is a direct subclass of Exception, not of APIError,
    because a 304 is not an error condition.
    """

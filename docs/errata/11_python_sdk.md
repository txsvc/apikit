# Errata: Spec 11 — Python SDK

## PATWithSecret missing `created_at` field

**Affected:** 11-REQ-9.1 (PATWithSecret dataclass definition)

The SDK spec defines `PATWithSecret` with fields `token`, `token_id`, `name`,
`permissions`, and `expires_at`. However, the upstream server's
`CreatePATResponse` (spec 09, 09-REQ-4) returns an additional `created_at`
field in the response body.

**Decision:** Follow the SDK spec as written. The `from_dict` method silently
ignores unknown fields (per 11-PROP-4), so the `created_at` value from the
server will be discarded without error. Callers who need the creation timestamp
should use the `PAT` type returned by `get_token()` or `list_tokens()`.

## Self-revocation status code mismatch (revoke_key / revoke_token)

**Affected:** 11-REQ-15.5, 11-REQ-15.9

The SDK spec says `revoke_key` and `revoke_token` return `None` on HTTP 204.
However, upstream specs define self-revocation endpoints as returning HTTP 200
with a JSON body containing the revoked resource metadata:

- Spec 10, 10-REQ-7.1: `DELETE /user/keys/:key_id` returns 200 with
  `{"key_id": "...", "revoked_at": "..."}`
- Spec 09, 09-REQ-8.1: `DELETE /user/tokens/:token_id` returns 200 with
  a PATResponse body

**Decision:** Follow the SDK spec as written for now. Tests mock HTTP 204.
The implementation will handle both 200 and 204 gracefully (200 response body
is simply discarded). A future spec revision should reconcile the status codes.

# Errata: Security Review Hardening (project-wide)

A code review of the authentication, OAuth, credential, and organization code
paths identified the divergences below between the specifications and what the
implementation must do to be safe. Each section names the affected spec, what
the spec (or the earlier implementation) said, and what the code does now.

## 1. Secret verified before revocation/expiry state (spec 05)

**Spec says (05-REQ-5.2 – 5.4, 05-REQ-6.2 – 6.4):** check `revoked_at`, then
`expires_at`, then compare the secret hash.

**Problem:** `key_id` and `token_id` are not secret (they appear in listings,
admin endpoints, and logs). Reporting "credential revoked" / "credential
expired" before verifying the secret let anyone holding an identifier probe
whether the credential was still live.

**Implementation:** `validateAPIKey` and `validatePAT` compare the secret hash
first and return the generic 401 "invalid credentials" on mismatch. The
revocation and expiry checks (and their distinct messages) run only after the
secret has been verified. The externally observable behavior for callers who
hold the secret is unchanged.

## 2. `keys:read` / `keys:manage` enforced on self-service key endpoints (spec 10)

**Spec / OpenAPI say:** `GET /user/keys` accepts a PAT with `keys:read`;
`DELETE /user/keys/:key_id` accepts a PAT with `keys:manage`.

**Problem:** the handlers never called `RequirePermission`, so any PAT (for
example one scoped to `users:read` only) could list and revoke its owner's API
key. The earlier `docs/authentication.md` even documented this gap as
"no specific permission is enforced".

**Implementation:** `listKeys` requires `keys:read` and `revokeKey` requires
`keys:manage` via `auth.RequirePermission`; PATs lacking the permission get
403 "insufficient permissions". Admin tokens and API keys are unaffected.
`POST /user/keys/:key_id/refresh` continues to reject every PAT with 401
"API key authentication required" as specified. `internal/keys` now imports
`internal/auth` directly; the import cycle recorded in
`10_etag_computation_and_import_cycle.md` no longer exists because
`internal/auth` depends on `internal/apiutil` rather than the root package.

## 3. `orgs:read` enforced on member-accessible org endpoints (spec 08)

**OpenAPI says:** `GET /orgs/{id}` and `GET /orgs/{id}/members` are
"accessible to admin or member with orgs:read PAT permission".

**Problem:** the handlers only checked admin-or-member; a PAT without
`orgs:read` could read organizations and member lists (including member
emails) as long as its owner was a member.

**Implementation:** both handlers call `auth.RequirePermission(c, "orgs",
"read")` before any database access. A PAT lacking the permission receives
403 "insufficient permissions"; the existing admin-or-member check is
unchanged for API keys and admin tokens.

## 4. Unbiased key material in `internal/keys` (spec 10)

**Earlier implementation:** `randAlphanumeric` mapped each random byte with
`byte % 62`, which selects the first eight charset characters with probability
5/256 instead of 4/256.

**Implementation:** rejection sampling — bytes >= 252 are discarded and
replaced by further reads. Bytes are still read in bulk, so the deterministic
test readers that feed byte values below 252 produce the same keys as before.
The PAT and OAuth generators already used rejection sampling.

## 5. OAuth callback identity validation and username handling (spec 06)

**Spec says (06-REQ-10.3, 06-REQ-11):** upsert the user by
`(provider, provider_id)`, and on re-login overwrite `username` and `email`
with the provider values. Erratum 06 item 7 recorded that a username
collision on re-login "falls through" to HTTP 500.

**Problems:**

- Neither the handler nor the providers rejected an empty provider user id
  (GitHub `id` of 0, missing Google `sub`, or a misbehaving custom
  `userinfo_url`). Every such login would have matched the same
  `(provider, "")` row — an account-takeover path.
- `users.username` is globally UNIQUE, but provider display names are neither
  unique nor stable, and Google's `name` is user-controlled. A collision made a
  new signup fail with 409 forever and turned an existing user's provider-side
  rename into a permanent 500 on login. Anyone could pre-empt another user's
  name by setting their own display name to it.
- An empty provider username was stored as `""`, which only one account can
  ever hold.

**Implementation:**

- `handleCallback` returns 502 "provider returned empty user id" when
  `ProviderID` is empty. `GitHubProvider.UserInfo` rejects a missing/zero `id`
  and `GoogleProvider.UserInfo` rejects a missing `sub`.
- An empty provider username becomes `<provider>-<provider_id>`.
- New user: on a `users.username` UNIQUE violation the INSERT is retried within
  the same transaction with `<name>-2`, `<name>-3`, ... (at most ten attempts).
  Other constraint violations (for example the unique email index) still map to
  409 "user already exists".
- Existing user: if updating the username would collide with another account,
  the existing username is kept and only `email` and `updated_at` are
  refreshed. The login succeeds.
- `AuthorizeURL` on both built-in providers no longer dereferences a nil
  `*url.URL` when the configured `authorize_url` is malformed; the raw string
  is returned instead.
- `Server.MountHandlers` builds the provider registry with an `http.Client`
  carrying a 15-second timeout instead of `http.DefaultClient` (no timeout).

## 6. Duplicate email on admin user creation (spec 07)

**Spec says (07-REQ-2.4, 07-REQ-2.5):** 409 for username and
`(provider, provider_id)` conflicts; any other database error is 500.

**Problem:** spec 16 added a unique index on `users.email`; a duplicate email
on `POST /users` therefore surfaced as 500 "internal server error".

**Implementation:** the handler maps `UNIQUE constraint failed: users.email`
to 409 "email already exists".

## 7. Foreign-key pragma via DSN (spec 02)

**Spec says (02):** execute `PRAGMA foreign_keys = ON` after opening.

**Problem:** the pragma is per-connection. With `MaxOpenConns(1)` the pool
still discards and reopens a connection after a driver error, and a reopened
connection would silently stop enforcing `REFERENCES` and
`ON DELETE CASCADE`.

**Implementation:** `db.Open` and `db.OpenMemory` append
`?_pragma=foreign_keys(1)` to the DSN so every connection the pool opens
enforces foreign keys. The explicit PRAGMA in `initDB` is kept.

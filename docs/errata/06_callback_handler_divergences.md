# Errata: Spec 06 OAuth Callback Handler Divergences

## 1. api_keys schema: key_id is PK, no id column

**Spec says (06-REQ-11.2, task 8.4):** `INSERT api_keys (id=uuid.New(), ...)`

**Actual DDL (spec 02):** `key_id TEXT NOT NULL PRIMARY KEY` — there is no `id`
column on `api_keys`.

**Implementation:** Uses `key_id` as the primary key. No separate UUID `id` is
generated for api_keys rows. The INSERT is:

```sql
INSERT INTO api_keys (key_id, user_id, secret_hash, expires_days, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?)
```

## 2. api_keys.expires_days column required

**Spec says (06-REQ-11.2):** Insert `key_id`, `secret_hash`, `expires_at`, and
`user_id` — omits `expires_days`.

**Actual DDL (spec 02):** `expires_days INTEGER NOT NULL` — the column has a
NOT NULL constraint.

**Implementation:** Includes `expires_days` in the INSERT, set to the `expires`
value from the request (0, 30, 60, or 90).

## 3. user.full_name has no data source

**Spec says (06-REQ-12.1):** Response includes `user.full_name` as a field.

**Problem:** The `UserInfo` struct only has `Username`, `Email`, and
`ProviderID`. The GitHub provider extracts `login`, `email`, and `id` — not the
`name` field. There is no data source for `full_name` in the OAuth flow.

**Implementation:** `full_name` is returned as JSON `null` for new users
(inserted as SQL NULL). For returning users, the existing DB value is returned
(which will be NULL unless set by another mechanism). The response type uses
`*string` to serialize as JSON null.

## 4. NowUTC() returns string, not time.Time

**Spec says (06-REQ-11.2):** `NowUTC() + (expires * 24h)` — implies time
arithmetic.

**Problem:** `apikit.NowUTC()` returns a `string`, not `time.Time`. String
addition with a duration is not valid Go.

**Implementation:** Uses `time.Now().UTC()` for the arithmetic, then
`db.FormatTime()` for serialization to the DB format. The `ComputeExpiresAt`
helper in `callback.go` handles this correctly.

## 5. Admin auto-promote: inline logic vs ShouldAutoPromote

**Spec 04** defines `ShouldAutoPromote(ctx, sqlDB, email) (bool, error)` for
the OAuth callback to call. **Spec 06** inlines the admin check logic and
explicitly states no dependency on spec 04.

**Implementation:** Inlines the admin check within the `db.WithTx` transaction:
queries `admin_config` for `admin_email`, checks if email matches, and verifies
no admin exists yet. This avoids a cross-package dependency and keeps the entire
user creation flow within a single transaction. The inline logic matches
`ShouldAutoPromote`'s behavior but adds the "no existing admin" check per
06-REQ-10.2.

## 6. TokenPrefix circular import

**Problem:** `internal/oauth` cannot import root `apikit` package (circular
import) to access `apikit.TokenPrefix`.

**Implementation:** Defines `defaultTokenPrefix = "ak"` as a local constant in
`handler.go`. This matches the default value of `apikit.TokenPrefix`. If the
token prefix needs to be configurable at the handler level, it can be refactored
to accept it as a parameter in a future task group.

## 7. username UNIQUE constraint collision on re-login

**Spec says (06-REQ-10.3):** Update `username` on re-login.

**Problem:** The `users.username` column has a UNIQUE constraint. If the
provider returns a username already taken by a different user, the UPDATE fails
with a constraint violation.

**Implementation:** The generic DB error handling catches this via
`db.WrapError` and returns HTTP 500 with `"internal server error"` (per
06-ERR-11). No special handling for this edge case — it falls through to the
existing error path.

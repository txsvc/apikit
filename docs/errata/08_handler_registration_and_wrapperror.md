# Errata: Spec 08 — Handler Registration and db.WrapError Usage

## 1. RegisterOrgHandlers Not Called from Production Server Setup

**Spec assumption (08-REQ-1.1, task 14.4):** RegisterOrgHandlers is called
from production server setup code.

**Actual behavior:** apikit is a library, not a standalone server. The
reference binary (`cmd/apikit/main.go`) creates an Echo server via
`apikit.NewServer()` but does not register any domain handlers.
RegisterOrgHandlers, RegisterUserHandlers, and RegisterPATHandlers are all
library functions that consuming projects call after obtaining the APIGroup.

This is consistent with the PRD's description: "A consuming project imports
apikit as a library... registers its own Echo handlers to extend the API
surface." No production registration call is needed or expected.

## 2. db.WrapError Not Used in Org Handlers

**Spec assumption (08-ERR-16):** Handlers wrap SQL errors via db.WrapError
before returning APIError.

**Actual behavior:** Org handlers inspect raw SQLite error strings directly
(e.g., `strings.Contains(errStr, "UNIQUE constraint failed: orgs.name")`)
to distinguish between name and slug constraint violations. This is necessary
because `db.WrapError` maps all UNIQUE/PRIMARY KEY violations to a single
`db.ErrConflict` sentinel, losing the column identity needed to produce
the spec-required distinct error messages ("organization name already exists"
vs "organization slug already exists").

All non-UNIQUE database errors are still returned as HTTP 500 with
"internal server error" without leaking SQL details, matching the spec's
intent.

## 3. NowUTC() vs db.FormatTime(time.Now().UTC())

**Spec assumption:** Handlers call `NowUTC()` from Server Core (spec 01)
for timestamps.

**Actual behavior:** Handlers use `db.FormatTime(time.Now().UTC())` which
truncates to whole-second precision and formats as RFC 3339 UTC with Z
suffix (`"2006-01-02T15:04:05Z"`). This is functionally equivalent to
`apikit.NowUTC()` (which produces the same format) and additionally ensures
sub-second truncation consistency with SQLite storage.

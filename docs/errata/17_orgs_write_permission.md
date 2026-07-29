# Errata: orgs:write added as 8th built-in permission

**Spec:** 17_pat_permission_updates
**Requirement:** 17-REQ-1.1

## Divergence

The spec states that `NewPermissionRegistry()` should register `tokens:write`
as the **7th** built-in permission, resulting in exactly 7 built-in
permissions. The implementation registers **8** built-in permissions: the
original 6 plus both `tokens:write` and `orgs:write`.

## Reason

The test specifications (TS-17-13, TS-17-14, TS-17-15) and handler tests from
task groups 2 and 3 use `orgs:write` as a registered permission in escalation
and replace/add test scenarios. Because the spec's execution path requires
permission validation (format + registry check) to run **before** the
privilege escalation check, `orgs:write` must be registered in the
`PermissionRegistry` for those tests to reach the escalation logic and return
the expected HTTP 403 responses. Without `orgs:write` being registered,
validation rejects it with HTTP 400 "unknown permission" before escalation
is evaluated.

## Impact

- `NewPermissionRegistry()` returns 8 built-in permissions instead of 7.
- Auth tests updated from 7 → 8 (count assertions, expected permission lists).
- No behavioral change for consumers — `orgs:write` follows the same
  `resource_type:action` pattern as all other permissions.

# Erratum: CLI Admin Commands (Spec 14)

## 1. Spec Self-Numbering Error

The PRD identifies itself as "spec 15 of 15" and refers to CLI user commands
as "spec 14". The actual filesystem numbering is `14_cli_admin_commands`
(this spec) and `15_cli_user_commands`. All cross-references in the PRD use
the swapped numbers.

## 2. CLI Core Helper Signature Mismatches

Spec 14 references five CLI core helpers with signatures that do not match
spec 13's actual (or planned) exports:

| Spec 14 Name | Spec 13 Actual | Issue |
|-------------|---------------|-------|
| `loadConfig(cmd)` | `LoadConfig(configDir)` | Different casing, parameter, return type |
| `newClient(cfg)` | N/A — `ClientFromContext(ctx)` | Spec 13 uses PersistentPreRunE + context |
| `handleError(err)` | `PrintError(err)` + `ExitCode(err)` | Split into two functions |
| `warnf(format, ...)` | N/A | Not defined in spec 13 |
| `printJSON(v)` | `PrintJSON(v)` | Casing only |

## 3. Execution Pattern: loadConfig/newClient vs PersistentPreRunE

Spec 14's common execution pattern (REQ-2.1) says each command's RunE calls
`loadConfig` then `newClient` directly. Spec 13 instead uses the root
command's `PersistentPreRunE` to load config and construct the client,
storing it in context for retrieval via `ClientFromContext(cmd.Context())`.

**Impact on tests:** TS-14-10 (loadConfig error) tests a code path that may
not exist within the admin command's RunE. The test is written to document
the expected end-to-end behavior; the implementation may need to handle this
error path in PersistentPreRunE rather than in each individual RunE.

## 4. SDK GetUserByID Signature Mismatch

The spec's test-scoped mock interface defines:
```go
GetUserByID(ctx, id string) (*apikit.User, error)
```

The actual SDK signature (sdk.go) is:
```go
GetUserByID(ctx, userID string, opts ...RequestOption) (*Response[User], error)
```

Two differences: (1) missing variadic `RequestOption` parameter, (2) return
type wraps the result in `Response[User]`. The test mock interface in
`admin_users_test.go` uses the real SDK signature.

## 5. Missing SDK Admin Methods

The following Client methods referenced by spec 14 are not yet defined in
`sdk.go` (they are part of spec 12's pending implementation):

- `CreateUser`, `UpdateUserByID`, `PromoteUser`, `DemoteUser`
- `BlockUser`, `UnblockUser`
- `CreateOrg`, `UpdateOrg`, `DeleteOrg`, `BlockOrg`, `UnblockOrg`
- `AddOrgMember`, `RemoveOrgMember`
- `ListUserKeys`, `RevokeUserKey`, `ListUserTokens`, `RevokeUserToken`

The test-scoped mock interfaces define these methods independently; the
production code will need them added to `apikit.Client` before it can be
implemented.

## 6. Admin Bootstrap Cross-Reference Error

The glossary entries "admin bootstrap" and "admin_bootstrap" reference
"spec 05", but the actual spec is `04_admin_bootstrap`. Spec 05 is
`05_auth_middleware`.

# Errata: Execute() does not call PrintError internally

**Spec:** 13 (cli_core)
**Requirement:** 13-REQ-1.2
**Date:** 2026-07-18

## Spec says

> The `Execute()` wrapper in `internal/cli` wraps `rootCmd.Execute()`, calls
> `PrintError(err)` on any non-nil error before returning that error.

## What was implemented

`Execute()` returns the error from `rootCmd.Execute()` without calling
`PrintError`. The caller (`cmd/akc/main.go`) is responsible for calling
`PrintError(err)` before `os.Exit(cli.ExitCode(err))`.

## Why

The test suite (written in groups 1–7) established the following pattern:

```go
err := Execute()
if err != nil {
    PrintError(err)
}
```

Test `TestErrorProducesExactlyOneEnvelope` (TS-13-2) asserts that stdout
contains **exactly one** occurrence of `{"error":`. If `Execute()` called
`PrintError` internally, the test's own `PrintError` call would produce a
second envelope, failing the assertion.

Making `Execute()` call `PrintError` internally would require rewriting
multiple test functions across `root_test.go`, `output_test.go`,
`smoke_test.go`, and `property_test.go` — all written to the caller-handles-
error-printing convention. Since the test suite was established as the
contract in groups 1–7, the implementation conforms to the tests rather than
the literal spec wording.

## Observable behavior

The end-user behavior is identical: every error produces exactly one JSON
error envelope on stdout before the process exits. The difference is an
internal responsibility split (main.go calls PrintError vs Execute calling
PrintError), which is invisible to CLI consumers and agents.

## Impact

- No user-visible difference.
- Consuming projects that call `Execute()` directly must call `PrintError(err)`
  themselves if they want error envelope output. This is documented in the
  `Execute()` function comment.

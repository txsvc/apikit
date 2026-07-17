# Erratum: TS-01-61 FormatUTC Test Expected Value

**Spec:** 01_server_core
**Test Spec Entry:** TS-01-61
**Requirement:** 01-REQ-18.2

## Issue

The test specification TS-01-61 asserts an incorrect expected value for the
`FormatUTC` conversion of an EST-zoned time derived via `time.In()`.

The spec pseudocode:

```go
t_utc := time.Date(2026, 7, 17, 14, 30, 0, 0, time.UTC)
t_est := t_utc.In(time.FixedZone("EST", -5*3600))
result_est := apikit.FormatUTC(t_est)
assert result_est == "2026-07-17T19:30:00Z"  // WRONG
```

## Correction

Go's `time.In()` does **not** change the underlying instant — it only changes
the timezone representation. Both `t_utc` and `t_est` represent the same
point in time (2026-07-17T14:30:00Z). `FormatUTC(t_est)` calls
`t_est.UTC().Format(time.RFC3339)`, which recovers the original UTC instant.

The correct expected value is:

```go
assert result_est == "2026-07-17T14:30:00Z"  // CORRECT — same instant
```

The erroneous assertion (`"2026-07-17T19:30:00Z"`) would require adding 5
hours to the UTC time, which is mathematically incorrect and would produce a
broken `FormatUTC` implementation.

## Test Implementation

The test (`TestFormatUTC_ConvertsToUTC`) uses the correct expected value and
additionally includes a subtest (`non_utc_time_converts`) that creates a time
directly in a non-UTC zone (not derived from a UTC time via `In()`) to verify
genuine UTC conversion:

```go
// 14:30 EST (a different instant) = 19:30 UTC
t_local := time.Date(2026, 7, 17, 14, 30, 0, 0, time.FixedZone("EST", -5*3600))
result := apikit.FormatUTC(t_local)
assert result == "2026-07-17T19:30:00Z"
```

## Source

Identified in code review (critical finding, reviewer session).

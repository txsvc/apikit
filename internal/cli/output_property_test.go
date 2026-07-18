package cli_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	apikit "github.com/txsvc/apikit"
	"github.com/txsvc/apikit/internal/cli"
)

// =========================================================================
// TS-13-P1: For any error condition, PrintError writes exactly one valid
// JSON error envelope to stdout before Execute() returns, with nothing on
// stderr.
//
// Property: 13-PROP-1
// Validates: 13-REQ-9.2, 13-REQ-14.2, 13-REQ-1.2
// =========================================================================

func TestPropertyPrintErrorAlwaysWritesExactlyOneValidEnvelope(t *testing.T) {
	testCases := []struct {
		name string
		err  error
	}{
		// Plain errors
		{"plain error short", fmt.Errorf("oops")},
		{"plain error long message", fmt.Errorf("long: %s", strings.Repeat("x", 500))},
		{"plain error empty message", fmt.Errorf("")},
		{"plain error with special chars", fmt.Errorf(`error with "quotes" and \backslash`)},

		// *apikit.APIError values with various status codes
		{"APIError 400 bad request", &apikit.APIError{Code: 400, Message: "bad request"}},
		{"APIError 401 unauthorized", &apikit.APIError{Code: 401, Message: "unauthorized"}},
		{"APIError 403 forbidden", &apikit.APIError{Code: 403, Message: "forbidden"}},
		{"APIError 404 not found", &apikit.APIError{Code: 404, Message: "not found"}},
		{"APIError 409 conflict", &apikit.APIError{Code: 409, Message: "conflict"}},
		{"APIError 422 unprocessable", &apikit.APIError{Code: 422, Message: "unprocessable entity"}},
		{"APIError 429 rate limited", &apikit.APIError{Code: 429, Message: "rate limited"}},
		{"APIError 500 internal", &apikit.APIError{Code: 500, Message: "internal server error"}},
		{"APIError 502 bad gateway", &apikit.APIError{Code: 502, Message: "bad gateway"}},
		{"APIError 503 unavailable", &apikit.APIError{Code: 503, Message: "service unavailable"}},
		{"APIError empty message", &apikit.APIError{Code: 500, Message: ""}},

		// Wrapped errors
		{"wrapped plain error", fmt.Errorf("outer: %w", fmt.Errorf("inner error"))},
		{"wrapped APIError", fmt.Errorf("wrapper: %w", &apikit.APIError{Code: 401, Message: "unauthorized"})},
		{"double wrapped APIError", fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", &apikit.APIError{Code: 403, Message: "forbidden"}))},
		{"wrapped plain in plain", fmt.Errorf("a: %w", fmt.Errorf("b: %w", fmt.Errorf("c")))},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr := captureStdoutAndStderrExt(t, func() {
				cli.PrintError(tc.err)
			})

			// stdout must not be empty — PrintError must always produce output.
			if len(strings.TrimSpace(stdout)) == 0 {
				t.Fatal("PrintError must produce output on stdout")
			}

			// stdout must be valid JSON.
			if !json.Valid([]byte(stdout)) {
				t.Errorf("stdout is not valid JSON: %s", stdout)
			}

			// Exactly one error envelope on stdout.
			if count := strings.Count(stdout, `"error":`); count != 1 {
				t.Errorf("expected exactly 1 error envelope on stdout, got %d occurrences\nstdout: %s", count, stdout)
			}

			// Must parse as the canonical error envelope structure.
			var env errorEnvelope
			if err := json.Unmarshal([]byte(stdout), &env); err != nil {
				t.Fatalf("failed to parse error envelope: %v\nstdout: %s", err, stdout)
			}

			// stderr must be empty for all error conditions.
			if len(strings.TrimSpace(stderr)) > 0 {
				t.Errorf("stderr must be empty, got: %s", stderr)
			}
		})
	}
}

// =========================================================================
// TS-13-P2: For any error value, ExitCode returns exactly 0, 1, or 2
// consistently with the error type table.
//
// Property: 13-PROP-2
// Validates: 13-REQ-9.3, 13-REQ-14.1
// =========================================================================

func TestPropertyExitCodeConsistentWithErrorTypeTable(t *testing.T) {
	testCases := []struct {
		name     string
		err      error
		wantCode int
	}{
		// nil -> 0
		{"nil error", nil, 0},

		// *apikit.APIError -> 1
		{"APIError 400", &apikit.APIError{Code: 400, Message: "bad request"}, 1},
		{"APIError 401", &apikit.APIError{Code: 401, Message: "unauthorized"}, 1},
		{"APIError 403", &apikit.APIError{Code: 403, Message: "forbidden"}, 1},
		{"APIError 404", &apikit.APIError{Code: 404, Message: "not found"}, 1},
		{"APIError 500", &apikit.APIError{Code: 500, Message: "internal"}, 1},
		{"APIError 502", &apikit.APIError{Code: 502, Message: "bad gateway"}, 1},
		{"APIError 503", &apikit.APIError{Code: 503, Message: "unavailable"}, 1},

		// Wrapped *apikit.APIError -> 1 (errors.As traverses the chain)
		{"wrapped APIError", fmt.Errorf("wrapped: %w", &apikit.APIError{Code: 401, Message: "unauthorized"}), 1},
		{"double wrapped APIError", fmt.Errorf("a: %w", fmt.Errorf("b: %w", &apikit.APIError{Code: 403, Message: "forbidden"})), 1},

		// plain errors -> 2
		{"plain error", fmt.Errorf("something went wrong"), 2},
		{"empty message error", fmt.Errorf(""), 2},
		{"long message error", fmt.Errorf("long: %s", strings.Repeat("x", 1000)), 2},

		// Wrapped plain errors -> 2
		{"wrapped plain error", fmt.Errorf("outer: %w", fmt.Errorf("inner")), 2},
		{"double wrapped plain", fmt.Errorf("a: %w", fmt.Errorf("b: %w", fmt.Errorf("c"))), 2},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			code := cli.ExitCode(tc.err)

			// Must be exactly the expected code.
			if code != tc.wantCode {
				t.Errorf("ExitCode(%v) = %d, want %d", tc.err, code, tc.wantCode)
			}

			// Invariant: ExitCode never returns anything other than 0, 1, or 2.
			if code != 0 && code != 1 && code != 2 {
				t.Errorf("ExitCode returned %d; only 0, 1, or 2 are valid exit codes", code)
			}
		})
	}
}

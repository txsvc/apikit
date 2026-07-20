package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// apiErrorer is an interface that matches the ErrorCode/ErrorMessage methods
// on *apikit.APIError, allowing internal/cli to detect API errors without
// importing the root apikit package (avoiding import cycle).
type apiErrorer interface {
	error
	ErrorCode() int
	ErrorMessage() string
}

// asAPIError checks if err implements apiErrorer, traversing the error chain.
// This is equivalent to errors.As but uses interface matching rather than
// concrete type assertion, avoiding the import cycle.
func asAPIError(err error, target *apiErrorer) bool {
	for err != nil {
		if ae, ok := err.(apiErrorer); ok {
			*target = ae
			return true
		}
		// Unwrap: support both Unwrap() error and Unwrap() []error
		switch x := err.(type) {
		case interface{ Unwrap() error }:
			err = x.Unwrap()
		case interface{ Unwrap() []error }:
			for _, e := range x.Unwrap() {
				if asAPIError(e, target) {
					return true
				}
			}
			return false
		default:
			return false
		}
	}
	return false
}

// ExitCode maps an error to an integer exit code:
//   - nil -> 0
//   - error implementing apiErrorer (i.e., *apikit.APIError) -> 1
//   - all other non-nil errors -> 2
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var ae apiErrorer
	if asAPIError(err, &ae) {
		return 1
	}
	return 2
}

// PrintError writes a JSON error envelope to stdout.
// For errors implementing apiErrorer (*apikit.APIError), the envelope
// uses the HTTP status code and server message.
// For all other errors, the envelope uses code: 0 (client sentinel)
// and err.Error() as the message.
// Nothing is written to stderr (13-REQ-9.2).
// printedError wraps an error that has already been printed as a JSON
// envelope by cmdHandleError. PrintError checks for this to avoid
// double-printing.
type printedError struct{ err error }

func (e *printedError) Error() string { return e.err.Error() }
func (e *printedError) Unwrap() error { return e.err }

func PrintError(err error) {
	if err == nil {
		return
	}
	var pe *printedError
	if errors.As(err, &pe) {
		return
	}

	code := 0
	message := err.Error()

	var ae apiErrorer
	if asAPIError(err, &ae) {
		code = ae.ErrorCode()
		message = ae.ErrorMessage()
	}

	envelope := struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{}
	envelope.Error.Code = code
	envelope.Error.Message = message

	data, _ := json.MarshalIndent(envelope, "", "  ")
	fmt.Fprintln(os.Stdout, string(data))
}

// PrintJSON marshals a value to indented JSON (two-space indent) and
// writes it to stdout followed by a newline.
func PrintJSON(v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, string(data))
	return nil
}

// Warnf writes a human-readable warning to stderr.
// Human-readable messages (warnings, progress, informational) are written
// to stderr only; they never appear on stdout (13-REQ-9.4).
func Warnf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
}

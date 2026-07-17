package apikit

import "time"

// NowUTC returns the current time formatted as an RFC 3339 UTC string
// with Z suffix (e.g. "2026-07-17T14:30:00Z").
func NowUTC() string {
	return "" // stub
}

// FormatUTC formats a time.Time as RFC 3339 UTC with Z suffix,
// converting to UTC if necessary.
func FormatUTC(t time.Time) string {
	return "" // stub
}

// ParseUTC parses any RFC 3339 timestamp (including offset variants)
// and normalizes the result to UTC. Returns (time.Time{}, error) for
// invalid or non-RFC-3339 input.
func ParseUTC(s string) (time.Time, error) {
	return time.Time{}, nil // stub: returns zero time with nil error
}

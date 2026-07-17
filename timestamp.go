package apikit

import "time"

// NowUTC returns the current time formatted as an RFC 3339 UTC string
// with Z suffix (e.g. "2026-07-17T14:30:00Z").
func NowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// FormatUTC formats a time.Time as RFC 3339 UTC with Z suffix,
// converting to UTC if necessary.
func FormatUTC(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// ParseUTC parses any RFC 3339 timestamp (including offset variants)
// and normalizes the result to UTC. Returns (time.Time{}, error) for
// invalid or non-RFC-3339 input.
func ParseUTC(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

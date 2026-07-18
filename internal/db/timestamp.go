package db

import "time"

// TimeFormat is the canonical timestamp format for all database timestamp strings.
const TimeFormat = "2006-01-02T15:04:05Z"

// FormatTime truncates t to whole-second precision, converts to UTC,
// and formats using TimeFormat.
func FormatTime(t time.Time) string {
	return t.Truncate(time.Second).UTC().Format(TimeFormat)
}

// ParseTime parses the input string using TimeFormat and returns the
// resulting time.Time in UTC.
func ParseTime(s string) (time.Time, error) {
	return time.Parse(TimeFormat, s)
}

package apikit_test

import (
	"strings"
	"testing"
	"time"

	"github.com/txsvc/apikit"
)

// ========================================================================
// Task 5.3: Unit tests for timestamp utilities
// (TS-01-60, TS-01-61, TS-01-62, TS-01-63)
// Requirements: 01-REQ-18.1, 01-REQ-18.2, 01-REQ-18.3, 01-REQ-18.4
// ========================================================================

// TestNowUTC_ReturnsRFC3339WithZ verifies that NowUTC() returns the current
// time as a valid RFC 3339 UTC string ending with Z.
// Covers TS-01-60 (Requirement: 01-REQ-18.1).
func TestNowUTC_ReturnsRFC3339WithZ(t *testing.T) {
	result := apikit.NowUTC()

	if !strings.HasSuffix(result, "Z") {
		t.Errorf("NowUTC() = %q, want string ending with 'Z'", result)
	}

	_, err := time.Parse(time.RFC3339, result)
	if err != nil {
		t.Errorf("NowUTC() = %q, not parseable as RFC 3339: %v", result, err)
	}
}

// TestFormatUTC_ConvertsToUTC verifies that FormatUTC formats a time.Time as
// RFC 3339 UTC with Z suffix, converting non-UTC times correctly.
//
// NOTE: TS-01-61 in the spec asserts FormatUTC(t_utc.In(EST)) == "2026-07-17T19:30:00Z"
// which is incorrect. time.In() preserves the instant; only the zone label changes.
// FormatUTC(t.In(EST)) should return the same UTC instant ("2026-07-17T14:30:00Z").
// See docs/errata/01_format_utc_test_assertion.md for the correction.
//
// Covers TS-01-61 (Requirement: 01-REQ-18.2).
func TestFormatUTC_ConvertsToUTC(t *testing.T) {
	t.Run("utc_time", func(t *testing.T) {
		tUTC := time.Date(2026, 7, 17, 14, 30, 0, 0, time.UTC)
		result := apikit.FormatUTC(tUTC)

		expected := "2026-07-17T14:30:00Z"
		if result != expected {
			t.Errorf("FormatUTC(UTC time) = %q, want %q", result, expected)
		}
	})

	t.Run("utc_time_via_In_preserves_instant", func(t *testing.T) {
		// time.In() only changes the zone label, not the instant.
		// FormatUTC should recover the original UTC instant.
		tUTC := time.Date(2026, 7, 17, 14, 30, 0, 0, time.UTC)
		tEST := tUTC.In(time.FixedZone("EST", -5*3600))

		result := apikit.FormatUTC(tEST)

		if !strings.HasSuffix(result, "Z") {
			t.Errorf("FormatUTC(EST-zoned time) = %q, want string ending with 'Z'", result)
		}

		// The instant is preserved: FormatUTC should return the same UTC value
		expected := "2026-07-17T14:30:00Z"
		if result != expected {
			t.Errorf("FormatUTC(t.In(EST)) = %q, want %q (same instant)", result, expected)
		}
	})

	t.Run("non_utc_time_converts", func(t *testing.T) {
		// Create a time directly in a non-UTC zone (different instant than same
		// numbers in UTC). 14:30 EST = 19:30 UTC.
		tLocal := time.Date(2026, 7, 17, 14, 30, 0, 0, time.FixedZone("EST", -5*3600))

		result := apikit.FormatUTC(tLocal)

		if !strings.HasSuffix(result, "Z") {
			t.Errorf("FormatUTC(local time) = %q, want string ending with 'Z'", result)
		}

		expected := "2026-07-17T19:30:00Z"
		if result != expected {
			t.Errorf("FormatUTC(14:30 EST) = %q, want %q (19:30 UTC)", result, expected)
		}
	})
}

// TestParseUTC_NormalizesToUTC verifies that ParseUTC parses valid RFC 3339
// strings including those with timezone offsets and normalizes the result
// to UTC.
// Covers TS-01-62 (Requirement: 01-REQ-18.3).
func TestParseUTC_NormalizesToUTC(t *testing.T) {
	t.Run("utc_z_suffix", func(t *testing.T) {
		result, err := apikit.ParseUTC("2026-07-17T14:30:00Z")
		if err != nil {
			t.Fatalf("ParseUTC returned error: %v", err)
		}

		if result.Location() != time.UTC {
			t.Errorf("result.Location() = %v, want time.UTC", result.Location())
		}

		expected := time.Date(2026, 7, 17, 14, 30, 0, 0, time.UTC)
		if !result.Equal(expected) {
			t.Errorf("ParseUTC('...Z') = %v, want %v", result, expected)
		}
	})

	t.Run("positive_offset_normalized", func(t *testing.T) {
		// 14:30+05:00 = 09:30 UTC
		result, err := apikit.ParseUTC("2026-07-17T14:30:00+05:00")
		if err != nil {
			t.Fatalf("ParseUTC returned error: %v", err)
		}

		if result.Location() != time.UTC {
			t.Errorf("result.Location() = %v, want time.UTC", result.Location())
		}

		expected := time.Date(2026, 7, 17, 9, 30, 0, 0, time.UTC)
		if !result.Equal(expected) {
			t.Errorf("ParseUTC('+05:00') = %v, want %v", result, expected)
		}
	})

	t.Run("negative_offset_normalized", func(t *testing.T) {
		// 14:30-05:00 = 19:30 UTC
		result, err := apikit.ParseUTC("2026-07-17T14:30:00-05:00")
		if err != nil {
			t.Fatalf("ParseUTC returned error: %v", err)
		}

		if result.Location() != time.UTC {
			t.Errorf("result.Location() = %v, want time.UTC", result.Location())
		}

		expected := time.Date(2026, 7, 17, 19, 30, 0, 0, time.UTC)
		if !result.Equal(expected) {
			t.Errorf("ParseUTC('-05:00') = %v, want %v", result, expected)
		}
	})
}

// TestParseUTC_InvalidInput verifies that ParseUTC returns (time.Time{}, error)
// for invalid or non-RFC-3339 input strings.
// Covers TS-01-63 (Requirement: 01-REQ-18.4).
func TestParseUTC_InvalidInput(t *testing.T) {
	invalidInputs := []struct {
		name  string
		input string
	}{
		{"not_a_timestamp", "not-a-timestamp"},
		{"date_with_slashes", "2026/07/17"},
		{"empty_string", ""},
		{"invalid_month_13", "2026-13-01T00:00:00Z"},
	}

	for _, tc := range invalidInputs {
		t.Run(tc.name, func(t *testing.T) {
			result, err := apikit.ParseUTC(tc.input)

			if err == nil {
				t.Errorf("ParseUTC(%q) returned nil error, want non-nil", tc.input)
			}

			if result != (time.Time{}) {
				t.Errorf("ParseUTC(%q) returned %v, want time.Time{} (zero value)",
					tc.input, result)
			}
		})
	}
}

// ========================================================================
// Task 5.5 (partial): Property test for UTC timestamp normalization
// (TS-01-P7)
// Property: 01-PROP-7
// Validates: 01-REQ-18.2, 01-REQ-18.3
// ========================================================================

// TestTimestamp_PropertyFormatParseRoundTrip verifies that for any time.Time
// value in any timezone, FormatUTC(t) ends with 'Z' and ParseUTC(FormatUTC(t))
// round-trips correctly. For any valid RFC 3339 string with offset,
// FormatUTC(ParseUTC(s)) round-trips correctly.
// Covers TS-01-P7 (Property: 01-PROP-7).
func TestTimestamp_PropertyFormatParseRoundTrip(t *testing.T) {
	// Generate time values across various timezones
	zones := []*time.Location{
		time.UTC,
		time.FixedZone("EST", -5*3600),
		time.FixedZone("+05:00", 5*3600),
		time.FixedZone("-08:00", -8*3600),
		time.FixedZone("+05:30", 5*3600+30*60), // India (half-hour offset)
		time.FixedZone("+09:45", 9*3600+45*60), // Chatham Islands (quarter-hour)
	}

	// Property 1: FormatUTC(t) ends with 'Z' and round-trips via ParseUTC
	t.Run("format_then_parse_round_trip", func(t *testing.T) {
		for _, zone := range zones {
			zoneName := zone.String()
			t.Run(zoneName, func(t *testing.T) {
				// Create a time in this zone
				original := time.Date(2026, 7, 17, 14, 30, 0, 0, zone)

				formatted := apikit.FormatUTC(original)

				// Invariant: FormatUTC output ends with 'Z'
				if !strings.HasSuffix(formatted, "Z") {
					t.Errorf("FormatUTC(%v in %s) = %q, want string ending with 'Z'",
						original, zoneName, formatted)
				}

				// Invariant: ParseUTC(FormatUTC(t)) equals t.UTC()
				parsed, err := apikit.ParseUTC(formatted)
				if err != nil {
					t.Fatalf("ParseUTC(%q) returned error: %v", formatted, err)
				}

				if !parsed.Equal(original.UTC()) {
					t.Errorf("round-trip: ParseUTC(FormatUTC(%v)) = %v, want %v",
						original, parsed, original.UTC())
				}
			})
		}
	})

	// Property 2: FormatUTC(ParseUTC(s)) round-trips for valid RFC 3339 input
	t.Run("parse_then_format_round_trip", func(t *testing.T) {
		validInputs := []struct {
			name        string
			input       string
			expectedUTC string
		}{
			{"utc_z", "2026-07-17T14:30:00Z", "2026-07-17T14:30:00Z"},
			{"positive_offset", "2026-07-17T14:30:00+05:00", "2026-07-17T09:30:00Z"},
			{"negative_offset", "2026-07-17T14:30:00-08:00", "2026-07-17T22:30:00Z"},
			{"zero_offset", "2026-07-17T14:30:00+00:00", "2026-07-17T14:30:00Z"},
		}

		for _, tc := range validInputs {
			t.Run(tc.name, func(t *testing.T) {
				parsed, err := apikit.ParseUTC(tc.input)
				if err != nil {
					t.Fatalf("ParseUTC(%q) returned error: %v", tc.input, err)
				}

				formatted := apikit.FormatUTC(parsed)

				if formatted != tc.expectedUTC {
					t.Errorf("FormatUTC(ParseUTC(%q)) = %q, want %q",
						tc.input, formatted, tc.expectedUTC)
				}
			})
		}
	})

	// Property 3: FormatUTC result is always parseable as RFC 3339
	t.Run("format_always_valid_rfc3339", func(t *testing.T) {
		times := []time.Time{
			time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),           // New Year midnight UTC
			time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC),       // End of year UTC
			time.Date(2026, 6, 15, 12, 0, 0, 0,                     // Noon in an offset zone
				time.FixedZone("+05:30", 5*3600+30*60)),
		}

		for _, tm := range times {
			formatted := apikit.FormatUTC(tm)
			_, err := time.Parse(time.RFC3339, formatted)
			if err != nil {
				t.Errorf("FormatUTC(%v) = %q, not valid RFC 3339: %v",
					tm, formatted, err)
			}
		}
	})
}

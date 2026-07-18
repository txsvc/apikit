package cli

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// ===========================================================================
// Subtask 1.1: parseKeyID unit tests
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-15-39: parseKeyID correctly extracts the penultimate segment from
// various valid API key formats.
// Requirement: 15-REQ-15.1
// ---------------------------------------------------------------------------

func TestParseKeyID_ValidFormats(t *testing.T) {
	tests := []struct {
		name   string
		apiKey string
		want   string
	}{
		{
			name:   "standard 3-part key",
			apiKey: "ak_abc12345_secret",
			want:   "abc12345",
		},
		{
			name:   "different prefix",
			apiKey: "myapp_abc12345_secret",
			want:   "abc12345",
		},
		{
			name:   "prefix with embedded underscore",
			apiKey: "my_app_abc12345_secret",
			want:   "abc12345",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keyID, err := parseKeyID(tt.apiKey)
			if err != nil {
				t.Errorf("parseKeyID(%q) returned unexpected error: %v", tt.apiKey, err)
			}
			if keyID != tt.want {
				t.Errorf("parseKeyID(%q) = %q, want %q", tt.apiKey, keyID, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TS-15-40: parseKeyID returns an error when the api_key string has fewer
// than 3 underscore-delimited segments.
// Requirement: 15-REQ-15.2
// ---------------------------------------------------------------------------

func TestParseKeyID_InvalidFormats(t *testing.T) {
	tests := []struct {
		name   string
		apiKey string
	}{
		{
			name:   "no underscores",
			apiKey: "badkey",
		},
		{
			name:   "only two segments",
			apiKey: "onlytwo_segments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keyID, err := parseKeyID(tt.apiKey)
			if err == nil {
				t.Errorf("parseKeyID(%q) returned nil error, want non-nil", tt.apiKey)
			}
			if keyID != "" {
				t.Errorf("parseKeyID(%q) = %q, want empty string", tt.apiKey, keyID)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Property test: parseKeyID always returns penultimate segment for keys
// with >=3 segments, and errors for <3 segments.
// Validates: 15-PROP-5
// ---------------------------------------------------------------------------

func TestParseKeyID_Property_PenultimateSegment(t *testing.T) {
	// Generate api_key strings with varying underscore-prefix structures.
	cases := []struct {
		name     string
		parts    []string
		wantID   string
		wantErr  bool
	}{
		{
			name:    "0 underscores (1 segment)",
			parts:   []string{"nosep"},
			wantErr: true,
		},
		{
			name:    "1 underscore (2 segments)",
			parts:   []string{"prefix", "secret"},
			wantErr: true,
		},
		{
			name:   "2 underscores (3 segments)",
			parts:  []string{"ak", "keyid123", "secret"},
			wantID: "keyid123",
		},
		{
			name:   "3 underscores (4 segments)",
			parts:  []string{"my", "app", "keyid456", "secret"},
			wantID: "keyid456",
		},
		{
			name:   "4 underscores (5 segments)",
			parts:  []string{"a", "b", "c", "keyid789", "secret"},
			wantID: "keyid789",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			apiKey := strings.Join(tt.parts, "_")
			keyID, err := parseKeyID(apiKey)

			if tt.wantErr {
				if err == nil {
					t.Errorf("parseKeyID(%q) returned nil error, want non-nil", apiKey)
				}
				if keyID != "" {
					t.Errorf("parseKeyID(%q) = %q, want empty string on error", apiKey, keyID)
				}
			} else {
				if err != nil {
					t.Errorf("parseKeyID(%q) returned unexpected error: %v", apiKey, err)
				}
				if keyID != tt.wantID {
					t.Errorf("parseKeyID(%q) = %q, want %q", apiKey, keyID, tt.wantID)
				}
			}
		})
	}
}

// ===========================================================================
// Subtask 1.2: validateExpires unit tests
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-15-41: validateExpires returns nil for {0, 30, 60, 90} and a non-nil
// error for all other integers.
// Requirement: 15-REQ-16.1
// Validates: 15-PROP-7
// ---------------------------------------------------------------------------

func TestValidateExpires_ValidValues(t *testing.T) {
	valid := []int{0, 30, 60, 90}
	for _, v := range valid {
		t.Run(fmt.Sprintf("valid_%d", v), func(t *testing.T) {
			err := validateExpires(v)
			if err != nil {
				t.Errorf("validateExpires(%d) = %v, want nil", v, err)
			}
		})
	}
}

func TestValidateExpires_InvalidValues(t *testing.T) {
	invalid := []int{1, 15, 45, 91, -1, 100}
	for _, v := range invalid {
		t.Run(fmt.Sprintf("invalid_%d", v), func(t *testing.T) {
			err := validateExpires(v)
			if err == nil {
				t.Errorf("validateExpires(%d) = nil, want non-nil error", v)
			}
			if err != nil && !strings.Contains(err.Error(), "--expires must be 0, 30, 60, or 90") {
				t.Errorf("validateExpires(%d) error = %q, want message containing %q",
					v, err.Error(), "--expires must be 0, 30, 60, or 90")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Property test: validateExpires accepts exactly {0, 30, 60, 90}.
// Validates: 15-PROP-7
// ---------------------------------------------------------------------------

func TestValidateExpires_Property_ExactSet(t *testing.T) {
	validSet := map[int]bool{0: true, 30: true, 60: true, 90: true}

	// Test a range of integers including boundary values.
	for v := -10; v <= 100; v++ {
		err := validateExpires(v)
		if validSet[v] {
			if err != nil {
				t.Errorf("validateExpires(%d) = %v, want nil (valid value)", v, err)
			}
		} else {
			if err == nil {
				t.Errorf("validateExpires(%d) = nil, want non-nil error (invalid value)", v)
			}
		}
	}

	// Also check extreme values.
	for _, v := range []int{1000, -1000} {
		err := validateExpires(v)
		if err == nil {
			t.Errorf("validateExpires(%d) = nil, want non-nil error", v)
		}
	}
}

// ===========================================================================
// Subtask 1.3: parsePermissions unit tests
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-15-42: parsePermissions correctly splits comma-separated input, trims
// whitespace, and returns a non-empty slice.
// Requirement: 15-REQ-17.1
// ---------------------------------------------------------------------------

func TestParsePermissions_ValidInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "two permissions",
			input: "users:read,orgs:read",
			want:  []string{"users:read", "orgs:read"},
		},
		{
			name:  "two permissions with whitespace",
			input: "users:read, orgs:read",
			want:  []string{"users:read", "orgs:read"},
		},
		{
			name:  "single permission",
			input: "users:read",
			want:  []string{"users:read"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			perms, err := parsePermissions(tt.input)
			if err != nil {
				t.Errorf("parsePermissions(%q) returned error: %v", tt.input, err)
			}
			if len(perms) != len(tt.want) {
				t.Fatalf("parsePermissions(%q) returned %d items, want %d",
					tt.input, len(perms), len(tt.want))
			}
			for i, p := range perms {
				if p != tt.want[i] {
					t.Errorf("parsePermissions(%q)[%d] = %q, want %q",
						tt.input, i, p, tt.want[i])
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TS-15-43: parsePermissions returns an error when input is empty or
// contains only whitespace and commas.
// Requirement: 15-REQ-17.2
// ---------------------------------------------------------------------------

func TestParsePermissions_EmptyInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "empty string",
			input: "",
		},
		{
			name:  "whitespace and commas",
			input: "  , ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			perms, err := parsePermissions(tt.input)
			if err == nil {
				t.Errorf("parsePermissions(%q) returned nil error, want non-nil", tt.input)
			}
			if perms != nil {
				t.Errorf("parsePermissions(%q) returned %v, want nil", tt.input, perms)
			}
			if err != nil && err.Error() != "--permissions must not be empty" {
				t.Errorf("parsePermissions(%q) error = %q, want %q",
					tt.input, err.Error(), "--permissions must not be empty")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Supplementary: parsePermissions does not validate individual entry format.
// Requirement: 15-REQ-17.1
// ---------------------------------------------------------------------------

func TestParsePermissions_NoFormatValidation(t *testing.T) {
	// An entry like 'notacolon' should pass through without format validation.
	perms, err := parsePermissions("notacolon")
	if err != nil {
		t.Errorf("parsePermissions(\"notacolon\") returned error: %v", err)
	}
	if len(perms) != 1 || perms[0] != "notacolon" {
		t.Errorf("parsePermissions(\"notacolon\") = %v, want [\"notacolon\"]", perms)
	}
}

// ===========================================================================
// Subtask 1.4: openBrowser unit tests
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-15-44: openBrowser executes 'open' on darwin and 'xdg-open' on linux.
// Requirement: 15-REQ-18.1
// ---------------------------------------------------------------------------

func TestOpenBrowser_Darwin(t *testing.T) {
	var capturedName string
	var capturedArgs []string

	mockExec := func(name string, arg ...string) *exec.Cmd {
		capturedName = name
		capturedArgs = arg
		// Return a command that succeeds.
		return exec.Command("true")
	}

	err := openBrowserWith("darwin", mockExec, "https://auth.example.com")
	if err != nil {
		t.Errorf("openBrowserWith(darwin) returned error: %v", err)
	}
	if capturedName != "open" {
		t.Errorf("exec command = %q, want %q", capturedName, "open")
	}
	if len(capturedArgs) != 1 || capturedArgs[0] != "https://auth.example.com" {
		t.Errorf("exec args = %v, want [\"https://auth.example.com\"]", capturedArgs)
	}
}

func TestOpenBrowser_Linux(t *testing.T) {
	var capturedName string
	var capturedArgs []string

	mockExec := func(name string, arg ...string) *exec.Cmd {
		capturedName = name
		capturedArgs = arg
		return exec.Command("true")
	}

	err := openBrowserWith("linux", mockExec, "https://auth.example.com")
	if err != nil {
		t.Errorf("openBrowserWith(linux) returned error: %v", err)
	}
	if capturedName != "xdg-open" {
		t.Errorf("exec command = %q, want %q", capturedName, "xdg-open")
	}
	if len(capturedArgs) != 1 || capturedArgs[0] != "https://auth.example.com" {
		t.Errorf("exec args = %v, want [\"https://auth.example.com\"]", capturedArgs)
	}
}

func TestOpenBrowser_FailingCommand(t *testing.T) {
	mockExec := func(name string, arg ...string) *exec.Cmd {
		return exec.Command("false") // command that exits with code 1
	}

	err := openBrowserWith("darwin", mockExec, "https://auth.example.com")
	if err == nil {
		t.Error("openBrowserWith with failing command returned nil error, want non-nil")
	}
}

func TestOpenBrowser_UnsupportedPlatform(t *testing.T) {
	mockExec := func(name string, arg ...string) *exec.Cmd {
		return exec.Command("true")
	}

	err := openBrowserWith("windows", mockExec, "https://auth.example.com")
	if err == nil {
		t.Error("openBrowserWith(windows) returned nil error, want non-nil for unsupported platform")
	}
}

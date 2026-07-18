package cli

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// ===========================================================================
// Subtask 1.5: State parameter generation and authorization URL tests
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-15-8: Verify that the generated OAuth state parameter is exactly 64
// lowercase hex characters derived from 32 random bytes.
// Requirement: 15-REQ-2.4
// Validates: 15-PROP-1
// ---------------------------------------------------------------------------

var hexPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func TestGenerateState_LengthAndFormat(t *testing.T) {
	state, err := generateState()
	if err != nil {
		t.Fatalf("generateState() returned error: %v", err)
	}

	if len(state) != 64 {
		t.Errorf("generateState() returned string of length %d, want 64", len(state))
	}

	if !hexPattern.MatchString(state) {
		t.Errorf("generateState() = %q, want string matching ^[0-9a-f]{64}$", state)
	}
}

func TestGenerateState_Uniqueness(t *testing.T) {
	state1, err := generateState()
	if err != nil {
		t.Fatalf("generateState() first call returned error: %v", err)
	}

	state2, err := generateState()
	if err != nil {
		t.Fatalf("generateState() second call returned error: %v", err)
	}

	if state1 == state2 {
		t.Errorf("generateState() returned same value twice: %q", state1)
	}
}

// ---------------------------------------------------------------------------
// Property test: generate 100 state parameters, all must have length 64
// and match the hex pattern.
// Validates: 15-PROP-1
// ---------------------------------------------------------------------------

func TestGenerateState_Property_AllValid(t *testing.T) {
	seen := make(map[string]bool)

	for i := range 100 {
		state, err := generateState()
		if err != nil {
			t.Fatalf("generateState() call %d returned error: %v", i, err)
		}

		if len(state) != 64 {
			t.Errorf("generateState() call %d: length = %d, want 64", i, len(state))
		}

		if !hexPattern.MatchString(state) {
			t.Errorf("generateState() call %d: %q does not match ^[0-9a-f]{64}$", i, state)
		}

		if seen[state] {
			t.Errorf("generateState() call %d: duplicate value %q", i, state)
		}
		seen[state] = true
	}
}

// ---------------------------------------------------------------------------
// TS-15-10: Verify that the authorization URL preserves existing query
// parameters from authorize_url and appends redirect_uri, state, and
// response_type=code without duplicating client_id.
// Requirement: 15-REQ-2.6
// Validates: 15-PROP-2
// ---------------------------------------------------------------------------

func TestBuildAuthURL_PreservesExistingParams(t *testing.T) {
	authorizeURL := "https://auth.example.com/oauth?client_id=abc123&scope=read"
	redirectURI := "http://127.0.0.1:12345/callback"
	state := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"

	finalURL, err := buildAuthURL(authorizeURL, redirectURI, state)
	if err != nil {
		t.Fatalf("buildAuthURL() returned error: %v", err)
	}

	parsed, err := url.Parse(finalURL)
	if err != nil {
		t.Fatalf("buildAuthURL() returned invalid URL: %v", err)
	}

	params := parsed.Query()

	// Existing params must be preserved.
	if got := params.Get("client_id"); got != "abc123" {
		t.Errorf("client_id = %q, want %q", got, "abc123")
	}
	if got := params.Get("scope"); got != "read" {
		t.Errorf("scope = %q, want %q", got, "read")
	}

	// New params must be added.
	if got := params.Get("redirect_uri"); got != redirectURI {
		t.Errorf("redirect_uri = %q, want %q", got, redirectURI)
	}
	if got := params.Get("state"); got != state {
		t.Errorf("state = %q, want %q", got, state)
	}
	if got := params.Get("response_type"); got != "code" {
		t.Errorf("response_type = %q, want %q", got, "code")
	}

	// client_id must appear exactly once.
	clientIDCount := strings.Count(finalURL, "client_id")
	if clientIDCount != 1 {
		t.Errorf("client_id appears %d times in URL, want exactly 1", clientIDCount)
	}
}

// ---------------------------------------------------------------------------
// Property test: buildAuthURL never overwrites existing params and always
// adds the three required params.
// Validates: 15-PROP-2
// ---------------------------------------------------------------------------

func TestBuildAuthURL_Property_NoOverwrite(t *testing.T) {
	tests := []struct {
		name         string
		authorizeURL string
		wantParams   map[string]string // existing params that must survive
	}{
		{
			name:         "simple URL with client_id",
			authorizeURL: "https://auth.example.com/oauth?client_id=abc123",
			wantParams:   map[string]string{"client_id": "abc123"},
		},
		{
			name:         "URL with multiple existing params",
			authorizeURL: "https://auth.example.com/oauth?client_id=xyz&scope=user&prompt=consent",
			wantParams:   map[string]string{"client_id": "xyz", "scope": "user", "prompt": "consent"},
		},
		{
			name:         "URL with no existing params",
			authorizeURL: "https://auth.example.com/oauth",
			wantParams:   map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			redirectURI := "http://127.0.0.1:9999/callback"
			state := "aabbccddaabbccddaabbccddaabbccddaabbccddaabbccddaabbccddaabbccdd"

			finalURL, err := buildAuthURL(tt.authorizeURL, redirectURI, state)
			if err != nil {
				t.Fatalf("buildAuthURL() returned error: %v", err)
			}

			parsed, err := url.Parse(finalURL)
			if err != nil {
				t.Fatalf("buildAuthURL() returned invalid URL: %v", err)
			}

			params := parsed.Query()

			// All existing params must survive.
			for key, val := range tt.wantParams {
				if got := params.Get(key); got != val {
					t.Errorf("param %q = %q, want %q", key, got, val)
				}
			}

			// The three required params must be present.
			if params.Get("redirect_uri") != redirectURI {
				t.Errorf("redirect_uri = %q, want %q", params.Get("redirect_uri"), redirectURI)
			}
			if params.Get("state") != state {
				t.Errorf("state = %q, want %q", params.Get("state"), state)
			}
			if params.Get("response_type") != "code" {
				t.Errorf("response_type = %q, want %q", params.Get("response_type"), "code")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Supplementary: loginTimeoutSeconds constant is exactly 120.
// Requirement: 15-REQ-22.1
// Validates: 15-PROP-10
// ---------------------------------------------------------------------------

func TestLoginTimeoutSecondsConstant(t *testing.T) {
	if loginTimeoutSeconds != 120 {
		t.Errorf("loginTimeoutSeconds = %d, want 120", loginTimeoutSeconds)
	}
}

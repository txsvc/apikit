package apikit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Task 1.1: NewClient defaults and baseURL normalization
// Test Specs: TS-12-1, TS-12-2, TS-12-E1, TS-12-E2
// Requirements: 12-REQ-1.1, 12-REQ-1.2, 12-REQ-1.E1, 12-REQ-1.E2
// ---------------------------------------------------------------------------

// TestNewClientDefaults verifies that NewClient returns a non-nil *Client
// with all expected default field values when called with only a baseURL.
func TestNewClientDefaults(t *testing.T) {
	client := NewClient("https://api.example.com")
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
	if client.httpClient != http.DefaultClient {
		t.Errorf("httpClient = %v, want http.DefaultClient", client.httpClient)
	}
	if client.mountPoint != "/api/v1" {
		t.Errorf("mountPoint = %q, want %q", client.mountPoint, "/api/v1")
	}
	if client.apiKey != "" {
		t.Errorf("apiKey = %q, want empty string", client.apiKey)
	}
	if client.requestID != "" {
		t.Errorf("requestID = %q, want empty string", client.requestID)
	}
}

// TestNewClientBaseURLNormalization verifies that a trailing slash on the
// baseURL is stripped at construction time.
func TestNewClientBaseURLNormalization(t *testing.T) {
	client := NewClient("https://api.example.com/")
	if client.baseURL != "https://api.example.com" {
		t.Errorf("baseURL = %q, want %q", client.baseURL, "https://api.example.com")
	}
}

// TestNewClientEmptyBaseURL verifies that an empty baseURL does not panic
// and returns a non-nil *Client. A subsequent Healthz call should return
// a plain error (not *APIError).
func TestNewClientEmptyBaseURL(t *testing.T) {
	client := NewClient("")
	if client == nil {
		t.Fatal("NewClient(\"\") returned nil; expected non-nil *Client")
	}

	resp, err := client.Healthz(context.Background())
	if err == nil {
		t.Fatal("Healthz with empty baseURL should return an error")
	}
	if resp != nil {
		t.Errorf("Healthz response should be nil on error, got %+v", resp)
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Errorf("expected plain error, not *APIError; got %v", apiErr)
	}
}

// TestNewClientBaseURLTrailingSlashPreventsDoubleSlash verifies that
// constructing a Client with a trailing-slash baseURL and then making
// a request produces a correct URL path without double slashes.
func TestNewClientBaseURLTrailingSlashPreventsDoubleSlash(t *testing.T) {
	var capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"u1","username":"alice","email":"a@b.com","full_name":"","status":"active","role":"user","provider":"github","provider_id":"gh1","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}`))
	}))
	defer server.Close()

	baseURLWithSlash := server.URL + "/"
	client := NewClient(baseURLWithSlash, WithAPIKey("key"))

	if client.baseURL != server.URL {
		t.Errorf("baseURL = %q, want %q (no trailing slash)", client.baseURL, server.URL)
	}

	_, err := client.GetUser(context.Background())
	if err != nil {
		t.Fatalf("GetUser returned error: %v", err)
	}
	if strings.HasPrefix(capturedPath, "//") {
		t.Errorf("request path has double slash: %q", capturedPath)
	}
}

// ---------------------------------------------------------------------------
// Task 1.2: ClientOption functions
// Test Specs: TS-12-4, TS-12-5, TS-12-6
// Requirements: 12-REQ-1.4, 12-REQ-1.5, 12-REQ-1.6
// ---------------------------------------------------------------------------

// TestWithAPIKey verifies that WithAPIKey sets the Client's apiKey field.
func TestWithAPIKey(t *testing.T) {
	client := NewClient("https://api.example.com", WithAPIKey("my-secret-key"))
	if client.apiKey != "my-secret-key" {
		t.Errorf("apiKey = %q, want %q", client.apiKey, "my-secret-key")
	}
}

// TestWithHTTPClient verifies that WithHTTPClient sets the Client's httpClient
// to the provided *http.Client (pointer equality), overriding http.DefaultClient.
func TestWithHTTPClient(t *testing.T) {
	customHTTP := &http.Client{Timeout: 5 * time.Second}
	client := NewClient("https://api.example.com", WithHTTPClient(customHTTP))
	if client.httpClient != customHTTP {
		t.Errorf("httpClient = %p, want %p (pointer equality)", client.httpClient, customHTTP)
	}
}

// TestWithRequestID verifies that WithRequestID sets the Client's requestID field.
func TestWithRequestID(t *testing.T) {
	client := NewClient("https://api.example.com", WithRequestID("req-abc-123"))
	if client.requestID != "req-abc-123" {
		t.Errorf("requestID = %q, want %q", client.requestID, "req-abc-123")
	}
}

// ---------------------------------------------------------------------------
// Task 1.3: WithMountPoint normalization
// Test Specs: TS-12-7, TS-12-8, TS-12-9
// Requirements: 12-REQ-1.7, 12-REQ-1.8, 12-REQ-1.9
// ---------------------------------------------------------------------------

// TestWithMountPointLeadingSlash verifies that a path without a leading slash
// gets one prepended.
func TestWithMountPointLeadingSlash(t *testing.T) {
	client := NewClient("https://api.example.com", WithMountPoint("api/v1"))
	if client.mountPoint != "/api/v1" {
		t.Errorf("mountPoint = %q, want %q", client.mountPoint, "/api/v1")
	}
}

// TestWithMountPointTrailingSlash verifies that a trailing slash is stripped.
func TestWithMountPointTrailingSlash(t *testing.T) {
	client := NewClient("https://api.example.com", WithMountPoint("/api/v1/"))
	if client.mountPoint != "/api/v1" {
		t.Errorf("mountPoint = %q, want %q", client.mountPoint, "/api/v1")
	}
}

// TestWithMountPointEmpty verifies that an empty string normalizes to "/".
func TestWithMountPointEmpty(t *testing.T) {
	client := NewClient("https://api.example.com", WithMountPoint(""))
	if client.mountPoint != "/" {
		t.Errorf("mountPoint = %q, want %q", client.mountPoint, "/")
	}
}

// TestWithMountPointBothSlashes verifies normalization when both leading and
// trailing slashes are present: "/api/v2/" → "/api/v2".
func TestWithMountPointBothSlashes(t *testing.T) {
	client := NewClient("https://api.example.com", WithMountPoint("/api/v2/"))
	if client.mountPoint != "/api/v2" {
		t.Errorf("mountPoint = %q, want %q", client.mountPoint, "/api/v2")
	}
}

// TestWithMountPointIdempotent verifies that applying WithMountPoint twice
// with the same value produces the same result (12-PROP-4).
func TestWithMountPointIdempotent(t *testing.T) {
	client1 := NewClient("https://api.example.com", WithMountPoint("api/v2"))
	client2 := NewClient("https://api.example.com", WithMountPoint(client1.mountPoint))
	if client1.mountPoint != client2.mountPoint {
		t.Errorf("not idempotent: first=%q, second=%q", client1.mountPoint, client2.mountPoint)
	}
}

// ---------------------------------------------------------------------------
// Task 1.4: Canonical shared types in sdk_types.go
// Test Specs: TS-12-10, TS-12-11, TS-12-12, TS-12-13, TS-12-14, TS-12-15
// Requirements: 12-REQ-2.1, 12-REQ-2.2, 12-REQ-2.3, 12-REQ-2.4, 12-REQ-2.5, 12-REQ-2.6
// ---------------------------------------------------------------------------

// TestTypeDefinitionsUnique verifies that all canonical domain types compile.
// If any type were declared more than once, the package would fail to build.
// This test serves as a compile-time assertion that each type is declared
// exactly once (TS-12-10).
func TestTypeDefinitionsUnique(t *testing.T) {
	// Each type is referenced below to ensure it exists and compiles.
	// A 'type X redeclared' error at build time would fail this test.
	_ = User{}
	_ = APIKeyMeta{}
	_ = APIKeyFull{}
	_ = PAT{}
	_ = PATFull{}
	_ = Organization{}
	_ = OAuthProvider{}
	_ = AuthCallbackRequest{}
	_ = AuthCallbackResponse{}
	_ = CreateUserRequest{}
	_ = UpdateUserRequest{}
	_ = CreateTokenRequest{}
	_ = CreateOrgRequest{}
	_ = UpdateOrgRequest{}
	_ = HealthResponse{}
	_ = VersionResponse{}
	_ = RevokeKeyResponse{}
	_ = ListUsersOptions{}
	_ = ListOrgsOptions{}
}

// TestJSONTagsSnakeCase verifies that User struct JSON tags produce
// snake_case field names matching the OpenAPI schema (TS-12-11).
func TestJSONTagsSnakeCase(t *testing.T) {
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	u := User{
		ID:         "u1",
		Username:   "alice",
		Email:      "a@b.com",
		FullName:   "Alice Smith",
		Status:     "active",
		Role:       "user",
		Provider:   "github",
		ProviderID: "gh1",
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	data, err := json.Marshal(u)
	if err != nil {
		t.Fatalf("json.Marshal(User) failed: %v", err)
	}

	result := string(data)
	expectedKeys := []string{
		`"id"`, `"username"`, `"email"`, `"full_name"`, `"status"`,
		`"role"`, `"provider"`, `"provider_id"`, `"created_at"`, `"updated_at"`,
	}
	for _, key := range expectedKeys {
		if !strings.Contains(result, key) {
			t.Errorf("JSON output missing key %s; got: %s", key, result)
		}
	}
}

// TestNullableTimestampDeserialization verifies that nullable *time.Time fields
// deserialize null JSON values as nil and non-null as valid *time.Time (TS-12-12).
func TestNullableTimestampDeserialization(t *testing.T) {
	jsonStr := `{"key_id":"k1","created_at":"2024-01-01T00:00:00Z","expires_at":null,"revoked_at":null}`
	var m APIKeyMeta
	if err := json.Unmarshal([]byte(jsonStr), &m); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if m.ExpiresAt != nil {
		t.Errorf("ExpiresAt should be nil for null JSON, got %v", m.ExpiresAt)
	}
	if m.RevokedAt != nil {
		t.Errorf("RevokedAt should be nil for null JSON, got %v", m.RevokedAt)
	}
	if m.CreatedAt.Year() != 2024 {
		t.Errorf("CreatedAt.Year() = %d, want 2024", m.CreatedAt.Year())
	}
}

// TestNullableTimestampNonNull verifies that non-null timestamp values
// deserialize to valid *time.Time values.
func TestNullableTimestampNonNull(t *testing.T) {
	jsonStr := `{"key_id":"k1","created_at":"2024-01-01T00:00:00Z","expires_at":"2025-06-01T12:00:00Z","revoked_at":null}`
	var m APIKeyMeta
	if err := json.Unmarshal([]byte(jsonStr), &m); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if m.ExpiresAt == nil {
		t.Fatal("ExpiresAt should be non-nil for non-null JSON value")
	}
	if m.ExpiresAt.Year() != 2025 {
		t.Errorf("ExpiresAt.Year() = %d, want 2025", m.ExpiresAt.Year())
	}
}

// TestUpdateUserRequestAlwaysIncludesFullName verifies that FullName has
// no omitempty tag, so the JSON body always includes "full_name" even
// when the value is an empty string (TS-12-13).
func TestUpdateUserRequestAlwaysIncludesFullName(t *testing.T) {
	req := UpdateUserRequest{FullName: ""}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("json.Marshal(UpdateUserRequest) failed: %v", err)
	}
	result := string(data)
	if !strings.Contains(result, `"full_name"`) {
		t.Errorf("JSON output missing \"full_name\" key; got: %s", result)
	}
	if !strings.Contains(result, `"full_name":""`) {
		t.Errorf("JSON output should contain \"full_name\":\"\" but got: %s", result)
	}
}

// TestOmitemptyOptionalFields verifies that optional request fields with
// omitempty are absent from the JSON body when nil (TS-12-14).
func TestOmitemptyOptionalFields(t *testing.T) {
	t.Run("AuthCallbackRequest_omits_expires", func(t *testing.T) {
		req := AuthCallbackRequest{
			Provider:    "github",
			Code:        "abc",
			RedirectURI: "http://localhost/cb",
		}
		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}
		if strings.Contains(string(data), `"expires"`) {
			t.Errorf("expected 'expires' to be omitted when nil; got: %s", data)
		}
	})

	t.Run("CreateOrgRequest_omits_url", func(t *testing.T) {
		req := CreateOrgRequest{Name: "Acme", Slug: "acme"}
		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}
		if strings.Contains(string(data), `"url"`) {
			t.Errorf("expected 'url' to be omitted when nil; got: %s", data)
		}
	})

	t.Run("UpdateOrgRequest_omits_name_and_url", func(t *testing.T) {
		req := UpdateOrgRequest{}
		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}
		result := string(data)
		if strings.Contains(result, `"name"`) {
			t.Errorf("expected 'name' to be omitted when nil; got: %s", result)
		}
		if strings.Contains(result, `"url"`) {
			t.Errorf("expected 'url' to be omitted when nil; got: %s", result)
		}
	})

	t.Run("CreateTokenRequest_omits_expires", func(t *testing.T) {
		req := CreateTokenRequest{Name: "mytoken", Permissions: []string{"read"}}
		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}
		if strings.Contains(string(data), `"expires"`) {
			t.Errorf("expected 'expires' to be omitted when nil; got: %s", data)
		}
	})
}

// TestListOrgMembersReturnsUserSlice verifies that ListOrgMembers returns
// []*User directly and that there is no OrgMember type (TS-12-15).
func TestListOrgMembersReturnsUserSlice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"u1","username":"alice","email":"a@b.com","full_name":"Alice","status":"active","role":"user","provider":"github","provider_id":"gh1","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}]`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	members, err := client.ListOrgMembers(context.Background(), "org-id-1")
	if err != nil {
		t.Fatalf("ListOrgMembers returned error: %v", err)
	}
	if members == nil {
		t.Fatal("ListOrgMembers returned nil slice")
	}
	if len(members) != 1 {
		t.Fatalf("len(members) = %d, want 1", len(members))
	}
	if members[0].ID != "u1" {
		t.Errorf("members[0].ID = %q, want %q", members[0].ID, "u1")
	}

	// Compile-time assertion: members is of type []*User.
	// If OrgMember existed and was returned instead, this would fail.
	var _ []*User = members
}

// ---------------------------------------------------------------------------
// Task 1.5: APIError and ErrNotModified
// Test Specs: TS-12-40, TS-12-41, TS-12-43, TS-12-E12
// Requirements: 12-REQ-6.1, 12-REQ-6.2, 12-REQ-6.4, 12-REQ-6.E3
// ---------------------------------------------------------------------------

// TestAPIErrorFormat verifies that APIError.Error() returns the string
// in the format "API error {Code}: {Message}" (TS-12-40).
func TestAPIErrorFormat(t *testing.T) {
	tests := []struct {
		code    int
		message string
		want    string
	}{
		{404, "User not found", "API error 404: User not found"},
		{500, "Internal Server Error", "API error 500: Internal Server Error"},
		{422, "Validation failed", "API error 422: Validation failed"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("code_%d", tt.code), func(t *testing.T) {
			apiErr := &APIError{Code: tt.code, Message: tt.message}
			got := apiErr.Error()
			if got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestAPIErrorFields verifies that APIError.Code and APIError.Message
// fields are accessible and contain the correct values (TS-12-41).
func TestAPIErrorFields(t *testing.T) {
	apiErr := &APIError{Code: 422, Message: "Validation failed"}
	if apiErr.Code != 422 {
		t.Errorf("Code = %d, want 422", apiErr.Code)
	}
	if apiErr.Message != "Validation failed" {
		t.Errorf("Message = %q, want %q", apiErr.Message, "Validation failed")
	}
}

// TestErrNotModifiedSentinel verifies that ErrNotModified is a package-level
// sentinel error value and that errors.Is works correctly (TS-12-43).
func TestErrNotModifiedSentinel(t *testing.T) {
	if ErrNotModified == nil {
		t.Fatal("ErrNotModified is nil")
	}
	if !errors.Is(ErrNotModified, ErrNotModified) {
		t.Error("errors.Is(ErrNotModified, ErrNotModified) returned false")
	}
	if ErrNotModified.Error() != "not modified" {
		t.Errorf("ErrNotModified.Error() = %q, want %q", ErrNotModified.Error(), "not modified")
	}
}

// TestErrorsAsIncompatibleType verifies that errors.As returns false for
// incompatible target types but true for *APIError (TS-12-E12).
func TestErrorsAsIncompatibleType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":{"code":404,"message":"not found"}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	_, err := client.GetUser(context.Background())
	if err == nil {
		t.Fatal("expected error from 404 response")
	}

	// errors.As with unrelated type returns false
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		t.Error("errors.As with *os.PathError should return false")
	}

	// errors.As with *APIError returns true
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Error("errors.As with *APIError should return true")
	}
}

// ---------------------------------------------------------------------------
// Task 1.6: Concurrent safety, options structs, redirect, package placement
// Test Specs: TS-12-3, TS-12-86, TS-12-87, TS-12-91, TS-12-92, TS-12-93
// Requirements: 12-REQ-1.3, 12-REQ-12.3, 12-REQ-12.4, 12-REQ-14.2,
//               12-REQ-15.1, 12-REQ-15.2
// ---------------------------------------------------------------------------

// TestClientConcurrentSafety verifies that Client is safe for concurrent use
// by multiple goroutines (TS-12-3). Run with: go test -race ./...
func TestClientConcurrentSafety(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = client.Healthz(context.Background())
		}()
	}
	wg.Wait()
	// If we reach here without a data race, the test passes.
	// The -race flag will detect races at runtime.
}

// TestListUsersOptionsCompiles verifies that ListUsersOptions is a struct
// with an IncludeBlocked bool field and is accepted by ListUsers (TS-12-86).
func TestListUsersOptionsCompiles(t *testing.T) {
	opts := &ListUsersOptions{IncludeBlocked: true}
	if !opts.IncludeBlocked {
		t.Error("IncludeBlocked should be true")
	}

	// Compile-time check: ListUsers accepts *ListUsersOptions
	client := NewClient("https://api.example.com")
	_, _ = client.ListUsers(context.Background(), opts)
}

// TestListOrgsOptionsCompiles verifies that ListOrgsOptions is a struct
// with an IncludeBlocked bool field and is accepted by ListOrgs (TS-12-87).
func TestListOrgsOptionsCompiles(t *testing.T) {
	opts := &ListOrgsOptions{IncludeBlocked: true}
	if !opts.IncludeBlocked {
		t.Error("IncludeBlocked should be true")
	}

	// Compile-time check: ListOrgs accepts *ListOrgsOptions
	client := NewClient("https://api.example.com")
	_, _ = client.ListOrgs(context.Background(), opts)
}

// TestWithHTTPClientCustomRedirect verifies that a caller can inject a
// configured *http.Client with a custom CheckRedirect policy and the SDK
// does not override it (TS-12-91).
func TestWithHTTPClientCustomRedirect(t *testing.T) {
	customHTTP := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	client := NewClient("https://example.com", WithHTTPClient(customHTTP))
	if client.httpClient != customHTTP {
		t.Error("httpClient pointer does not match the injected custom *http.Client")
	}
	if client.httpClient.CheckRedirect == nil {
		t.Error("custom CheckRedirect was overridden to nil")
	}
}

// TestPackagePlacement verifies that this test file (and by extension all
// SDK source files) are in package apikit. If they were in a sub-package
// or internal/, this test would not compile here (TS-12-92).
func TestPackagePlacement(t *testing.T) {
	// This test compiling proves all SDK types are in package apikit.
	// If any were in internal/ or a sub-package, this would fail to compile.
	_ = NewClient("https://example.com")
	_ = &APIError{}
	_ = ErrNotModified
	_ = &Response[User]{}
	_ = User{}
	_ = Organization{}
}

// TestGoVersionRequirement verifies that generic syntax (Response[T]) compiles,
// which requires Go 1.18+ (TS-12-93). The go.mod go directive is checked
// separately.
func TestGoVersionRequirement(t *testing.T) {
	// If this compiles, Go generics are supported (Go 1.18+).
	var r *Response[User]
	_ = r

	var r2 *Response[HealthResponse]
	_ = r2

	// Verify go.mod has go directive >= 1.18.
	// The go.mod currently has go 1.25 which satisfies this.
	// This is a compile-time assertion; if go < 1.18, generics would fail.
}

// ---------------------------------------------------------------------------
// Common test data constants
// ---------------------------------------------------------------------------

const testUserJSON = `{"id":"u1","username":"alice","email":"a@b.com","full_name":"Alice","status":"active","role":"user","provider":"github","provider_id":"gh1","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}`

const testHealthJSON = `{"status":"ok"}`

const testVersionJSON = `{"version":"1.0","build_time":"2024-01-01","commit":"abc123","mount_point":"/api/v1"}`

// ---------------------------------------------------------------------------
// Task 2.1: do method headers (Accept, Content-Type, Authorization)
// Test Specs: TS-12-16, TS-12-17, TS-12-18, TS-12-20
// Requirements: 12-REQ-3.1, 12-REQ-3.2, 12-REQ-3.3, 12-REQ-3.5
// ---------------------------------------------------------------------------

// TestAcceptHeaderAlwaysSet verifies that do sets Accept: application/json
// on every outgoing request (TS-12-16).
func TestAcceptHeaderAlwaysSet(t *testing.T) {
	var capturedHeader http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(testHealthJSON))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	_, _ = client.Healthz(context.Background())

	got := capturedHeader.Get("Accept")
	if got != "application/json" {
		t.Errorf("Accept header = %q, want %q", got, "application/json")
	}
}

// TestContentTypeSetOnBodyRequests verifies that do sets Content-Type:
// application/json on requests that have a non-nil body (TS-12-17).
func TestContentTypeSetOnBodyRequests(t *testing.T) {
	var capturedCT string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCT = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(testUserJSON))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	_, _ = client.UpdateUser(context.Background(), &UpdateUserRequest{FullName: "Alice"})

	if capturedCT != "application/json" {
		t.Errorf("Content-Type header = %q, want %q", capturedCT, "application/json")
	}
}

// TestAuthorizationHeaderWithAPIKey verifies that do sets Authorization:
// Bearer <apiKey> when apiKey is non-empty (TS-12-18).
func TestAuthorizationHeaderWithAPIKey(t *testing.T) {
	var capturedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(testHealthJSON))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("my-token"))
	_, _ = client.Healthz(context.Background())

	if capturedAuth != "Bearer my-token" {
		t.Errorf("Authorization header = %q, want %q", capturedAuth, "Bearer my-token")
	}
}

// TestAuthorizationHeaderAbsentWithoutAPIKey verifies that do does not set
// any Authorization header when apiKey is empty (TS-12-20).
func TestAuthorizationHeaderAbsentWithoutAPIKey(t *testing.T) {
	var capturedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(testHealthJSON))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	_, _ = client.Healthz(context.Background())

	if capturedAuth != "" {
		t.Errorf("Authorization header = %q, want empty string", capturedAuth)
	}
}

// ---------------------------------------------------------------------------
// Task 2.2: do method headers (X-Request-ID) and context propagation
// Test Specs: TS-12-19, TS-12-21, TS-12-22
// Requirements: 12-REQ-3.4, 12-REQ-3.6, 12-REQ-3.7
// ---------------------------------------------------------------------------

// TestRequestIDHeaderSet verifies that do sets X-Request-ID when requestID
// is configured (TS-12-19).
func TestRequestIDHeaderSet(t *testing.T) {
	var capturedRID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRID = r.Header.Get("X-Request-ID")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(testHealthJSON))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithRequestID("req-xyz"))
	_, _ = client.Healthz(context.Background())

	if capturedRID != "req-xyz" {
		t.Errorf("X-Request-ID header = %q, want %q", capturedRID, "req-xyz")
	}
}

// TestRequestIDHeaderAbsent verifies that do does not set X-Request-ID
// when requestID is empty (TS-12-21).
func TestRequestIDHeaderAbsent(t *testing.T) {
	var capturedRID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRID = r.Header.Get("X-Request-ID")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(testHealthJSON))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	_, _ = client.Healthz(context.Background())

	if capturedRID != "" {
		t.Errorf("X-Request-ID header = %q, want empty string", capturedRID)
	}
}

// TestContextCancellation verifies that do passes the caller-supplied context
// to the request, enabling cancellation (TS-12-22).
func TestContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(testHealthJSON))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := client.Healthz(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Task 2.3: do method response status handling (200, 204, 304)
// Test Specs: TS-12-23, TS-12-24, TS-12-25, TS-12-28
// Requirements: 12-REQ-3.8, 12-REQ-3.9, 12-REQ-3.10, 12-REQ-3.13
// ---------------------------------------------------------------------------

// TestDoDecodes200Response verifies that do decodes the JSON response body
// into the typed response struct on HTTP 200 (TS-12-23).
func TestDoDecodes200Response(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(testUserJSON))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	resp, err := client.GetUser(context.Background())
	if err != nil {
		t.Fatalf("GetUser returned error: %v", err)
	}
	if resp == nil {
		t.Fatal("GetUser returned nil response")
	}
	if resp.Data.ID != "u1" {
		t.Errorf("resp.Data.ID = %q, want %q", resp.Data.ID, "u1")
	}
	if resp.Data.Username != "alice" {
		t.Errorf("resp.Data.Username = %q, want %q", resp.Data.Username, "alice")
	}
}

// TestDoHandles204Response verifies that do returns nil error on HTTP 204
// No Content (TS-12-24).
func TestDoHandles204Response(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	err := client.RevokeToken(context.Background(), "tok-1")
	if err != nil {
		t.Fatalf("RevokeToken returned error: %v", err)
	}
}

// TestDoHandles304Response verifies that do returns nil *Response[T] and
// ErrNotModified when the server responds with HTTP 304 (TS-12-25).
func TestDoHandles304Response(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(304)
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	resp, err := client.GetUser(context.Background(), WithIfNoneMatch(`"etag-abc"`))
	if resp != nil {
		t.Errorf("expected nil response on 304, got %+v", resp)
	}
	if !errors.Is(err, ErrNotModified) {
		t.Errorf("expected ErrNotModified, got %v", err)
	}
}

// TestPerRequestOptionApplied verifies that do applies per-request
// RequestOption functions to the request before execution (TS-12-28).
func TestPerRequestOptionApplied(t *testing.T) {
	var capturedINM string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedINM = r.Header.Get("If-None-Match")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(testUserJSON))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	_, _ = client.GetUser(context.Background(), WithIfNoneMatch(`"v1"`))

	if capturedINM != `"v1"` {
		t.Errorf("If-None-Match header = %q, want %q", capturedINM, `"v1"`)
	}
}

// ---------------------------------------------------------------------------
// Task 2.4: do method error handling (4xx/5xx JSON and non-JSON bodies)
// Test Specs: TS-12-26, TS-12-27, TS-12-42
// Requirements: 12-REQ-3.11, 12-REQ-3.12, 12-REQ-6.3
// ---------------------------------------------------------------------------

// TestDoDecodes4xxJSONEnvelope verifies that do decodes the nested JSON error
// envelope on 4xx responses and returns *APIError (TS-12-26, TS-12-42).
func TestDoDecodes4xxJSONEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":{"code":404,"message":"User not found"}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	resp, err := client.GetUser(context.Background())
	if resp != nil {
		t.Errorf("expected nil response on 404, got %+v", resp)
	}
	if err == nil {
		t.Fatal("expected error from 404 response")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Code != 404 {
		t.Errorf("apiErr.Code = %d, want 404", apiErr.Code)
	}
	if apiErr.Message != "User not found" {
		t.Errorf("apiErr.Message = %q, want %q", apiErr.Message, "User not found")
	}
	if err.Error() != "API error 404: User not found" {
		t.Errorf("err.Error() = %q, want %q", err.Error(), "API error 404: User not found")
	}
}

// TestDoHandlesNonJSONErrorBody verifies that do returns *APIError with Code
// from HTTP status and Message from status text when the error body is
// non-JSON (TS-12-27).
func TestDoHandlesNonJSONErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502)
		_, _ = w.Write([]byte("<html>Bad Gateway</html>"))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	resp, err := client.GetUser(context.Background())
	if resp != nil {
		t.Errorf("expected nil response on 502, got %+v", resp)
	}
	if err == nil {
		t.Fatal("expected error from 502 response")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Code != 502 {
		t.Errorf("apiErr.Code = %d, want 502", apiErr.Code)
	}
	if apiErr.Message != "Bad Gateway" {
		t.Errorf("apiErr.Message = %q, want %q", apiErr.Message, "Bad Gateway")
	}
}

// TestDoHandles5xxJSONEnvelope verifies that do correctly decodes 5xx
// responses with JSON error envelopes (TS-12-42 variant).
func TestDoHandles5xxJSONEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"error":{"code":500,"message":"Internal Server Error"}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	_, err := client.GetUser(context.Background())
	if err == nil {
		t.Fatal("expected error from 500 response")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Code != 500 {
		t.Errorf("apiErr.Code = %d, want 500", apiErr.Code)
	}
	if apiErr.Message != "Internal Server Error" {
		t.Errorf("apiErr.Message = %q, want %q", apiErr.Message, "Internal Server Error")
	}
}

// ---------------------------------------------------------------------------
// Task 2.5: do method plain error scenarios (network, marshal, JSON decode)
// Test Specs: TS-12-44, TS-12-E3, TS-12-E4, TS-12-E5, TS-12-E6, TS-12-E7
// Requirements: 12-REQ-3.E1, 12-REQ-3.E2, 12-REQ-3.E3, 12-REQ-3.E4,
//               12-REQ-3.E5, 12-REQ-6.5
// ---------------------------------------------------------------------------

// TestDoReturnsPlainErrorOnNetworkFailure verifies that do returns a plain
// error wrapping the network error when the server is unreachable
// (TS-12-44, TS-12-E5).
func TestDoReturnsPlainErrorOnNetworkFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	serverURL := server.URL
	server.Close() // close immediately to force connection refused

	client := NewClient(serverURL, WithAPIKey("key"))
	_, err := client.GetUser(context.Background())
	if err == nil {
		t.Fatal("expected error from closed server")
	}
	if errors.Unwrap(err) == nil {
		t.Error("error should wrap underlying network error; errors.Unwrap returned nil")
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Errorf("expected plain error, not *APIError; got %v", apiErr)
	}
}

// TestDoReturnsPlainErrorOnEmpty200Body verifies that do returns a plain
// error wrapping the JSON decode error when the server returns 200 with an
// empty body (TS-12-E3).
func TestDoReturnsPlainErrorOnEmpty200Body(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		// write nothing — empty body
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	resp, err := client.GetUser(context.Background())
	if resp != nil {
		t.Errorf("expected nil response on decode failure, got %+v", resp)
	}
	if err == nil {
		t.Fatal("expected error from empty 200 body")
	}
	if errors.Unwrap(err) == nil {
		t.Error("error should wrap underlying JSON decode error; errors.Unwrap returned nil")
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Errorf("expected plain error, not *APIError; got %v", apiErr)
	}
}

// TestDoReturnsPlainErrorOnNonJSON200Body verifies that do returns a plain
// error when the server returns 200 with a non-JSON body (TS-12-E3 variant).
func TestDoReturnsPlainErrorOnNonJSON200Body(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	resp, err := client.GetUser(context.Background())
	if resp != nil {
		t.Errorf("expected nil response on decode failure, got %+v", resp)
	}
	if err == nil {
		t.Fatal("expected error from non-JSON 200 body")
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Errorf("expected plain error, not *APIError; got %v", apiErr)
	}
}

// TestDoReturnsPlainErrorOnMarshalFailure verifies that do returns a plain
// error wrapping the JSON marshal error before any HTTP request is made when
// the request body encoding fails (TS-12-E4).
func TestDoReturnsPlainErrorOnMarshalFailure(t *testing.T) {
	var requestMade bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestMade = true
		w.WriteHeader(200)
	}))
	defer server.Close()

	client := NewClient(server.URL)

	// A channel value causes json.Marshal to fail.
	badBody := make(chan int)
	_, _, err := client.do(context.Background(), "POST", "/test", badBody, nil)
	if err == nil {
		t.Fatal("expected error from marshal failure")
	}
	if requestMade {
		t.Error("HTTP request should not have been made on marshal failure")
	}
	if errors.Unwrap(err) == nil {
		t.Error("error should wrap underlying marshal error; errors.Unwrap returned nil")
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Errorf("expected plain error, not *APIError; got %v", apiErr)
	}
}

// TestContextCancelledWrapsUnderlyingError verifies that do wraps
// context.Canceled as a plain error, not *APIError (TS-12-E6).
func TestContextCancelledWrapsUnderlyingError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(testUserJSON))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := client.GetUser(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled in error chain, got %v", err)
	}
	if errors.Unwrap(err) == nil {
		t.Error("error should wrap underlying cause; errors.Unwrap returned nil")
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Errorf("expected plain error, not *APIError; got %v", apiErr)
	}
}

// TestEmptyBaseURLReturnsPlainError verifies that an empty baseURL causes
// a plain error (not *APIError) from the HTTP transport layer, and the SDK
// does not panic (TS-12-E7).
func TestEmptyBaseURLReturnsPlainError(t *testing.T) {
	client := NewClient("")
	_, err := client.ListOrgs(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error with empty baseURL")
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Errorf("expected plain error, not *APIError; got %v", apiErr)
	}
}

// ---------------------------------------------------------------------------
// Task 2.6: URL construction and mount point routing
// Test Specs: TS-12-29, TS-12-30, TS-12-31, TS-12-32, TS-12-33, TS-12-E8
// Requirements: 12-REQ-4.1, 12-REQ-4.2, 12-REQ-4.3, 12-REQ-4.4,
//               12-REQ-4.5, 12-REQ-4.E1
// ---------------------------------------------------------------------------

// TestAPIEndpointURLConstruction verifies that API endpoint URLs are
// constructed as baseURL + mountPoint + path (TS-12-29).
func TestAPIEndpointURLConstruction(t *testing.T) {
	var capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(testUserJSON))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	_, _ = client.GetUser(context.Background())

	if capturedPath != "/api/v1/user" {
		t.Errorf("request path = %q, want %q", capturedPath, "/api/v1/user")
	}
}

// TestHealthProbeURLBypassesMountPoint verifies that health probe endpoint
// URLs are constructed as baseURL + path without the mountPoint (TS-12-30).
func TestHealthProbeURLBypassesMountPoint(t *testing.T) {
	var capturedPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPaths = append(capturedPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		// Return appropriate JSON for each endpoint
		switch r.URL.Path {
		case "/version":
			_, _ = w.Write([]byte(testVersionJSON))
		default:
			_, _ = w.Write([]byte(testHealthJSON))
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, WithMountPoint("/api/v1"))
	_, _ = client.Healthz(context.Background())
	_, _ = client.Readyz(context.Background())
	_, _ = client.Version(context.Background())

	if len(capturedPaths) != 3 {
		t.Fatalf("expected 3 requests, got %d", len(capturedPaths))
	}
	if capturedPaths[0] != "/healthz" {
		t.Errorf("Healthz path = %q, want %q", capturedPaths[0], "/healthz")
	}
	if capturedPaths[1] != "/readyz" {
		t.Errorf("Readyz path = %q, want %q", capturedPaths[1], "/readyz")
	}
	if capturedPaths[2] != "/version" {
		t.Errorf("Version path = %q, want %q", capturedPaths[2], "/version")
	}
}

// TestAuthEndpointUseMountPoint verifies that auth endpoint URLs include
// the mountPoint prefix (TS-12-31).
func TestAuthEndpointUseMountPoint(t *testing.T) {
	var capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	_, _ = client.GetProviders(context.Background())

	if capturedPath != "/api/v1/auth/providers" {
		t.Errorf("request path = %q, want %q", capturedPath, "/api/v1/auth/providers")
	}
}

// TestCustomMountPointUsed verifies that WithMountPoint overrides the default
// mount point in URL construction (TS-12-32).
func TestCustomMountPointUsed(t *testing.T) {
	var capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(testUserJSON))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"), WithMountPoint("/v2"))
	_, _ = client.GetUser(context.Background())

	if capturedPath != "/v2/user" {
		t.Errorf("request path = %q, want %q", capturedPath, "/v2/user")
	}
}

// TestPathParameterVerbatim verifies that path parameters are interpolated
// into URL paths as-is without URL-encoding (TS-12-33).
func TestPathParameterVerbatim(t *testing.T) {
	var capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"key":"sk_live_xxx","key_id":"key-uuid-123","expires_at":null}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	_, _ = client.RefreshKey(context.Background(), "key-uuid-123")

	if capturedPath != "/api/v1/user/keys/key-uuid-123/refresh" {
		t.Errorf("request path = %q, want %q", capturedPath, "/api/v1/user/keys/key-uuid-123/refresh")
	}
}

// TestNoDoubleSlashesInURL verifies that trailing slash stripping in NewClient
// ensures no double slashes appear in constructed URLs (TS-12-E8).
func TestNoDoubleSlashesInURL(t *testing.T) {
	var capturedURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(testUserJSON))
	}))
	defer server.Close()

	client := NewClient(server.URL+"/", WithAPIKey("key"))
	_, _ = client.GetUser(context.Background())

	if strings.Contains(capturedURL, "//api") {
		t.Errorf("URL contains double slash: %q", capturedURL)
	}
	if capturedURL != "/api/v1/user" {
		t.Errorf("request URL = %q, want %q", capturedURL, "/api/v1/user")
	}
}

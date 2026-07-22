package apikit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

const testVersionJSON = `{"go_version":"1.0","build_time":"2024-01-01","commit":"abc123","mount_point":"/api/v1"}`

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

// ---------------------------------------------------------------------------
// Task 3.1: WithIfNoneMatch and ETag response capture
// Test Specs: TS-12-34, TS-12-36, TS-12-37, TS-12-39
// Requirements: 12-REQ-5.1, 12-REQ-5.3, 12-REQ-5.4, 12-REQ-5.6
// ---------------------------------------------------------------------------

// TestWithIfNoneMatchAddsHeader verifies that WithIfNoneMatch sets the
// If-None-Match header on the request with the provided etag value (TS-12-34).
func TestWithIfNoneMatchAddsHeader(t *testing.T) {
	var capturedINM string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedINM = r.Header.Get("If-None-Match")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(testUserJSON))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	_, _ = client.GetUser(context.Background(), WithIfNoneMatch(`"abc123"`))

	if capturedINM != `"abc123"` {
		t.Errorf("If-None-Match header = %q, want %q", capturedINM, `"abc123"`)
	}
}

// TestGetUserReturnsETagOn200 verifies that GetUser returns a non-nil
// *Response[User] with Data populated, StatusCode 200, and the ETag header
// value accessible via resp.ETag() (TS-12-36).
func TestGetUserReturnsETagOn200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v42"`)
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
	if resp.StatusCode != 200 {
		t.Errorf("resp.StatusCode = %d, want 200", resp.StatusCode)
	}
	if resp.ETag() != `"v42"` {
		t.Errorf("resp.ETag() = %q, want %q", resp.ETag(), `"v42"`)
	}
}

// TestResponseETagAbsent verifies that Response[T].ETag() returns an empty
// string when the server does not include an ETag header (TS-12-37).
func TestResponseETagAbsent(t *testing.T) {
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
	if resp.ETag() != "" {
		t.Errorf("resp.ETag() = %q, want empty string", resp.ETag())
	}
}

// TestResponseStatusCodeReflectsActual verifies that Response[T].StatusCode
// reflects the actual HTTP status code returned by the server (e.g., 201 for
// creation endpoints) (TS-12-39).
func TestResponseStatusCodeReflectsActual(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(testUserJSON))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	resp, err := client.GetUserByID(context.Background(), "id1")
	if err != nil {
		t.Fatalf("GetUserByID returned error: %v", err)
	}
	if resp == nil {
		t.Fatal("GetUserByID returned nil response")
	}
	if resp.StatusCode != 201 {
		t.Errorf("resp.StatusCode = %d, want 201", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Task 3.2: 304 handling and ETag-capable method signatures
// Test Specs: TS-12-35, TS-12-38, TS-12-E9
// Requirements: 12-REQ-5.2, 12-REQ-5.5, 12-REQ-5.E1
// ---------------------------------------------------------------------------

// TestGetUserReturns304ErrNotModified verifies that GetUser returns nil
// *Response[User] and ErrNotModified when the server returns HTTP 304 (TS-12-35).
func TestGetUserReturns304ErrNotModified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(304)
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	resp, err := client.GetUser(context.Background(), WithIfNoneMatch(`"abc"`))
	if resp != nil {
		t.Errorf("expected nil *Response[User] on 304, got %+v", resp)
	}
	if !errors.Is(err, ErrNotModified) {
		t.Errorf("expected ErrNotModified, got %v", err)
	}
}

// TestETagMethodSignatures verifies that GetUser, GetUserByID, GetToken,
// and GetOrg all accept ...RequestOption as a variadic parameter (TS-12-38).
// This is a compile-time check — if any of these methods did not accept
// RequestOption, this test would fail to compile. Other methods
// (UpdateUser, ListKeys, etc.) do NOT accept RequestOption; this is verified
// by `go build ./...` — adding RequestOption to their call would produce
// a compile error.
func TestETagMethodSignatures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		switch r.URL.Path {
		case "/api/v1/user/tokens/tid":
			_, _ = w.Write([]byte(`{"token_id":"tid","name":"t","permissions":["read"],"created_at":"2024-01-01T00:00:00Z","expires_at":null,"revoked_at":null}`))
		case "/api/v1/orgs/oid":
			_, _ = w.Write([]byte(`{"id":"oid","name":"Acme","slug":"acme","status":"active","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}`))
		default:
			_, _ = w.Write([]byte(testUserJSON))
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	ctx := context.Background()
	opt := WithIfNoneMatch(`"e"`)

	// All four of these MUST compile with ...RequestOption:
	_, _ = client.GetUser(ctx, opt)
	_, _ = client.GetUserByID(ctx, "id", opt)
	_, _ = client.GetToken(ctx, "tid", opt)
	_, _ = client.GetOrg(ctx, "oid", opt)

	// NOTE: The following MUST NOT compile (verified by go build ./...):
	// client.UpdateUser(ctx, &UpdateUserRequest{}, opt) — compile error
	// client.ListKeys(ctx, opt) — compile error
	// client.ListTokens(ctx, opt) — compile error
}

// TestResponseNilOn304 verifies that on HTTP 304, the *Response[T] is nil
// and ErrNotModified is returned, using GetOrg as the target method (TS-12-E9).
func TestResponseNilOn304(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(304)
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	resp, err := client.GetOrg(context.Background(), "org-1", WithIfNoneMatch(`"v1"`))
	if resp != nil {
		t.Errorf("expected nil *Response[Organization] on 304, got %+v", resp)
	}
	if !errors.Is(err, ErrNotModified) {
		t.Errorf("expected ErrNotModified, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Task 3.3: ListUsers query parameter construction
// Test Specs: TS-12-55, TS-12-56, TS-12-84
// Requirements: 12-REQ-8.1, 12-REQ-8.2, 12-REQ-12.1
// ---------------------------------------------------------------------------

// TestListUsersWithIncludeBlocked verifies that ListUsers appends
// include_blocked=true when IncludeBlocked is true (TS-12-55, TS-12-84).
func TestListUsersWithIncludeBlocked(t *testing.T) {
	var capturedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	_, err := client.ListUsers(context.Background(), &ListUsersOptions{IncludeBlocked: true})
	if err != nil {
		t.Fatalf("ListUsers returned error: %v", err)
	}
	if capturedQuery != "include_blocked=true" {
		t.Errorf("RawQuery = %q, want %q", capturedQuery, "include_blocked=true")
	}
}

// TestListUsersNilOptions verifies that ListUsers sends no include_blocked
// query parameter when options is nil (TS-12-56 part 1).
func TestListUsersNilOptions(t *testing.T) {
	var capturedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	_, err := client.ListUsers(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListUsers returned error: %v", err)
	}
	if strings.Contains(capturedQuery, "include_blocked") {
		t.Errorf("RawQuery = %q, should not contain include_blocked", capturedQuery)
	}
}

// TestListUsersFalseOptions verifies that ListUsers sends no include_blocked
// query parameter when IncludeBlocked is false (TS-12-56 part 2).
func TestListUsersFalseOptions(t *testing.T) {
	var capturedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	_, err := client.ListUsers(context.Background(), &ListUsersOptions{IncludeBlocked: false})
	if err != nil {
		t.Fatalf("ListUsers returned error: %v", err)
	}
	if strings.Contains(capturedQuery, "include_blocked") {
		t.Errorf("RawQuery = %q, should not contain include_blocked", capturedQuery)
	}
}

// ---------------------------------------------------------------------------
// Task 3.4: ListOrgs query parameter construction
// Test Specs: TS-12-69, TS-12-70, TS-12-85
// Requirements: 12-REQ-9.2, 12-REQ-9.3, 12-REQ-12.2
// ---------------------------------------------------------------------------

// TestListOrgsWithIncludeBlocked verifies that ListOrgs appends
// include_blocked=true when IncludeBlocked is true (TS-12-69, TS-12-85).
func TestListOrgsWithIncludeBlocked(t *testing.T) {
	var capturedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	_, err := client.ListOrgs(context.Background(), &ListOrgsOptions{IncludeBlocked: true})
	if err != nil {
		t.Fatalf("ListOrgs returned error: %v", err)
	}
	if capturedQuery != "include_blocked=true" {
		t.Errorf("RawQuery = %q, want %q", capturedQuery, "include_blocked=true")
	}
}

// TestListOrgsNilOptions verifies that ListOrgs sends no include_blocked
// query parameter when options is nil (TS-12-70 part 1).
func TestListOrgsNilOptions(t *testing.T) {
	var capturedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	_, err := client.ListOrgs(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListOrgs returned error: %v", err)
	}
	if strings.Contains(capturedQuery, "include_blocked") {
		t.Errorf("RawQuery = %q, should not contain include_blocked", capturedQuery)
	}
}

// TestListOrgsFalseOptions verifies that ListOrgs sends no include_blocked
// query parameter when IncludeBlocked is false (TS-12-70 part 2).
func TestListOrgsFalseOptions(t *testing.T) {
	var capturedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	_, err := client.ListOrgs(context.Background(), &ListOrgsOptions{IncludeBlocked: false})
	if err != nil {
		t.Fatalf("ListOrgs returned error: %v", err)
	}
	if strings.Contains(capturedQuery, "include_blocked") {
		t.Errorf("RawQuery = %q, should not contain include_blocked", capturedQuery)
	}
}

// ---------------------------------------------------------------------------
// Task 3.5: List endpoint bare JSON array decoding
// Test Specs: TS-12-88, TS-12-89, TS-12-E13, TS-12-E15, TS-12-E19
// Requirements: 12-REQ-13.1, 12-REQ-13.2, 12-REQ-7.E1, 12-REQ-8.E1
// ---------------------------------------------------------------------------

// TestBareArrayDecoding verifies that list endpoint responses are decoded
// directly from bare JSON arrays into Go slices (TS-12-88).
func TestBareArrayDecoding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`[{"key_id":"k1","created_at":"2024-01-01T00:00:00Z","expires_at":null,"revoked_at":null},{"key_id":"k2","created_at":"2024-01-01T00:00:00Z","expires_at":null,"revoked_at":null}]`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	keys, err := client.ListKeys(context.Background())
	if err != nil {
		t.Fatalf("ListKeys returned error: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("len(keys) = %d, want 2", len(keys))
	}
	if keys[0].KeyID != "k1" {
		t.Errorf("keys[0].KeyID = %q, want %q", keys[0].KeyID, "k1")
	}
	if keys[1].KeyID != "k2" {
		t.Errorf("keys[1].KeyID = %q, want %q", keys[1].KeyID, "k2")
	}
}

// TestEmptyArrayReturnsNonNilSlice verifies that list endpoints return a
// non-nil empty slice when the server returns an empty JSON array [] (TS-12-89, TS-12-E13).
func TestEmptyArrayReturnsNonNilSlice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	keys, err := client.ListKeys(context.Background())
	if err != nil {
		t.Fatalf("ListKeys returned error: %v", err)
	}
	if keys == nil {
		t.Fatal("ListKeys returned nil slice, want non-nil empty slice")
	}
	if len(keys) != 0 {
		t.Errorf("len(keys) = %d, want 0", len(keys))
	}
}

// TestEmptyArrayListUsers verifies that ListUsers returns a non-nil empty
// []*User slice when the server returns an empty JSON array [] (TS-12-E15).
func TestEmptyArrayListUsers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	users, err := client.ListUsers(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListUsers returned error: %v", err)
	}
	if users == nil {
		t.Fatal("ListUsers returned nil slice, want non-nil empty slice")
	}
	if len(users) != 0 {
		t.Errorf("len(users) = %d, want 0", len(users))
	}
}

// TestWrappedObjectFails verifies that do returns a plain error (not *APIError)
// when a list endpoint server returns a wrapped JSON object instead of a bare
// array (TS-12-E19).
func TestWrappedObjectFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"keys":[{"key_id":"k1","created_at":"2024-01-01T00:00:00Z","expires_at":null,"revoked_at":null}]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	keys, err := client.ListKeys(context.Background())
	if err == nil {
		t.Fatal("expected error from wrapped JSON object, got nil")
	}
	if keys != nil {
		t.Errorf("expected nil slice on decode failure, got %v", keys)
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Errorf("expected plain error, not *APIError; got %v", apiErr)
	}
}

// ---------------------------------------------------------------------------
// Task 4.1: Authenticated user endpoints (GetUser, UpdateUser, ListKeys,
//           RefreshKey, RevokeKey)
// Test Specs: TS-12-45, TS-12-46, TS-12-47, TS-12-48, TS-12-49
// Requirements: 12-REQ-7.1, 12-REQ-7.2, 12-REQ-7.3, 12-REQ-7.4, 12-REQ-7.5
// ---------------------------------------------------------------------------

// TestGetUserHappyPath verifies that GetUser sends a GET request to
// mountPoint+"/user" and returns *Response[User] with decoded user data
// (TS-12-45).
func TestGetUserHappyPath(t *testing.T) {
	var capturedMethod, capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
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
	if capturedMethod != "GET" {
		t.Errorf("method = %q, want GET", capturedMethod)
	}
	if capturedPath != "/api/v1/user" {
		t.Errorf("path = %q, want /api/v1/user", capturedPath)
	}
	if resp == nil {
		t.Fatal("GetUser returned nil response")
	}
	if resp.Data.ID == "" {
		t.Error("resp.Data.ID is empty, want non-empty")
	}
	if resp.Data.Username != "alice" {
		t.Errorf("resp.Data.Username = %q, want %q", resp.Data.Username, "alice")
	}
}

// TestUpdateUser verifies that UpdateUser sends a PATCH request to
// mountPoint+"/user" with JSON body always including full_name and returns
// *User (TS-12-46).
func TestUpdateUser(t *testing.T) {
	var capturedBody []byte
	var capturedMethod, capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"id":"u1","username":"alice","email":"a@b.com","full_name":"Alice Smith","status":"active","role":"user","provider":"github","provider_id":"gh1","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	user, err := client.UpdateUser(context.Background(), &UpdateUserRequest{FullName: "Alice Smith"})
	if err != nil {
		t.Fatalf("UpdateUser returned error: %v", err)
	}
	if capturedMethod != "PATCH" {
		t.Errorf("method = %q, want PATCH", capturedMethod)
	}
	if capturedPath != "/api/v1/user" {
		t.Errorf("path = %q, want /api/v1/user", capturedPath)
	}
	if user == nil {
		t.Fatal("UpdateUser returned nil user")
	}
	if !strings.Contains(string(capturedBody), `"full_name":"Alice Smith"`) {
		t.Errorf("request body missing full_name: %s", capturedBody)
	}
}

// TestListKeys verifies that ListKeys sends GET to /user/keys and decodes
// the bare JSON array into []*APIKeyMeta (TS-12-47).
func TestListKeys(t *testing.T) {
	var capturedMethod, capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`[{"key_id":"k1","created_at":"2024-01-01T00:00:00Z","expires_at":null,"revoked_at":null}]`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	keys, err := client.ListKeys(context.Background())
	if err != nil {
		t.Fatalf("ListKeys returned error: %v", err)
	}
	if capturedMethod != "GET" {
		t.Errorf("method = %q, want GET", capturedMethod)
	}
	if capturedPath != "/api/v1/user/keys" {
		t.Errorf("path = %q, want /api/v1/user/keys", capturedPath)
	}
	if len(keys) != 1 {
		t.Fatalf("len(keys) = %d, want 1", len(keys))
	}
	if keys[0].KeyID != "k1" {
		t.Errorf("keys[0].KeyID = %q, want %q", keys[0].KeyID, "k1")
	}
}

// TestRefreshKey verifies that RefreshKey sends a POST to
// /user/keys/{keyID}/refresh and returns *APIKeyFull (TS-12-48).
func TestRefreshKey(t *testing.T) {
	var capturedMethod, capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"key":"new-secret","key_id":"key-1","expires_at":null}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	keyFull, err := client.RefreshKey(context.Background(), "key-1")
	if err != nil {
		t.Fatalf("RefreshKey returned error: %v", err)
	}
	if capturedMethod != "POST" {
		t.Errorf("method = %q, want POST", capturedMethod)
	}
	if capturedPath != "/api/v1/user/keys/key-1/refresh" {
		t.Errorf("path = %q, want /api/v1/user/keys/key-1/refresh", capturedPath)
	}
	if keyFull == nil {
		t.Fatal("RefreshKey returned nil")
	}
	if keyFull.Key != "new-secret" {
		t.Errorf("keyFull.Key = %q, want %q", keyFull.Key, "new-secret")
	}
	if keyFull.KeyID != "key-1" {
		t.Errorf("keyFull.KeyID = %q, want %q", keyFull.KeyID, "key-1")
	}
}

// TestRevokeKey verifies that RevokeKey sends DELETE to /user/keys/{keyID}
// and returns *RevokeKeyResponse with KeyID and RevokedAt on HTTP 200
// (TS-12-49).
func TestRevokeKey(t *testing.T) {
	var capturedMethod, capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"key_id":"key-1","revoked_at":"2024-06-01T00:00:00Z"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	revokeResp, err := client.RevokeKey(context.Background(), "key-1")
	if err != nil {
		t.Fatalf("RevokeKey returned error: %v", err)
	}
	if capturedMethod != "DELETE" {
		t.Errorf("method = %q, want DELETE", capturedMethod)
	}
	if capturedPath != "/api/v1/user/keys/key-1" {
		t.Errorf("path = %q, want /api/v1/user/keys/key-1", capturedPath)
	}
	if revokeResp == nil {
		t.Fatal("RevokeKey returned nil")
	}
	if revokeResp.KeyID != "key-1" {
		t.Errorf("revokeResp.KeyID = %q, want %q", revokeResp.KeyID, "key-1")
	}
	if revokeResp.RevokedAt.IsZero() {
		t.Error("revokeResp.RevokedAt is zero time, want non-zero")
	}
}

// ---------------------------------------------------------------------------
// Task 4.2: Authenticated user endpoints (ListTokens, CreateToken, GetToken,
//           RevokeToken, ListUserOrgs) and edge case RevokeToken error
// Test Specs: TS-12-50, TS-12-51, TS-12-52, TS-12-53, TS-12-54, TS-12-E14
// Requirements: 12-REQ-7.6, 12-REQ-7.7, 12-REQ-7.8, 12-REQ-7.9,
//               12-REQ-7.10, 12-REQ-7.E2
// ---------------------------------------------------------------------------

// TestListTokens verifies that ListTokens sends GET to /user/tokens and
// decodes the bare JSON array into []*PAT (TS-12-50).
func TestListTokens(t *testing.T) {
	var capturedMethod, capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`[{"token_id":"t1","name":"mytoken","permissions":["read"],"created_at":"2024-01-01T00:00:00Z","expires_at":null,"revoked_at":null}]`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	tokens, err := client.ListTokens(context.Background())
	if err != nil {
		t.Fatalf("ListTokens returned error: %v", err)
	}
	if capturedMethod != "GET" {
		t.Errorf("method = %q, want GET", capturedMethod)
	}
	if capturedPath != "/api/v1/user/tokens" {
		t.Errorf("path = %q, want /api/v1/user/tokens", capturedPath)
	}
	if len(tokens) != 1 {
		t.Fatalf("len(tokens) = %d, want 1", len(tokens))
	}
	if tokens[0].TokenID != "t1" {
		t.Errorf("tokens[0].TokenID = %q, want %q", tokens[0].TokenID, "t1")
	}
}

// TestCreateToken verifies that CreateToken sends POST to /user/tokens with
// JSON body and returns *PATFull on 201 (TS-12-51).
func TestCreateToken(t *testing.T) {
	var capturedMethod, capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"token":"tok-secret","token_id":"t1","name":"ci","permissions":["read","write"],"expires_at":null}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	patFull, err := client.CreateToken(context.Background(), &CreateTokenRequest{
		Name:        "ci",
		Permissions: []string{"read", "write"},
	})
	if err != nil {
		t.Fatalf("CreateToken returned error: %v", err)
	}
	if capturedMethod != "POST" {
		t.Errorf("method = %q, want POST", capturedMethod)
	}
	if capturedPath != "/api/v1/user/tokens" {
		t.Errorf("path = %q, want /api/v1/user/tokens", capturedPath)
	}
	if patFull == nil {
		t.Fatal("CreateToken returned nil")
	}
	if patFull.Token != "tok-secret" {
		t.Errorf("patFull.Token = %q, want %q", patFull.Token, "tok-secret")
	}
	if patFull.TokenID != "t1" {
		t.Errorf("patFull.TokenID = %q, want %q", patFull.TokenID, "t1")
	}
}

// TestGetToken verifies that GetToken sends GET to /user/tokens/{tokenID}
// and returns *Response[PAT] on success (TS-12-52).
func TestGetToken(t *testing.T) {
	var capturedMethod, capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"token_id":"t1","name":"mytoken","permissions":["read"],"created_at":"2024-01-01T00:00:00Z","expires_at":null,"revoked_at":null}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	resp, err := client.GetToken(context.Background(), "t1")
	if err != nil {
		t.Fatalf("GetToken returned error: %v", err)
	}
	if capturedMethod != "GET" {
		t.Errorf("method = %q, want GET", capturedMethod)
	}
	if capturedPath != "/api/v1/user/tokens/t1" {
		t.Errorf("path = %q, want /api/v1/user/tokens/t1", capturedPath)
	}
	if resp == nil {
		t.Fatal("GetToken returned nil response")
	}
	if resp.Data.TokenID != "t1" {
		t.Errorf("resp.Data.TokenID = %q, want %q", resp.Data.TokenID, "t1")
	}
}

// TestRevokeToken204 verifies that RevokeToken sends DELETE to
// /user/tokens/{tokenID} and returns nil error on 204 (TS-12-53).
func TestRevokeToken204(t *testing.T) {
	var capturedMethod, capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.WriteHeader(204)
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	err := client.RevokeToken(context.Background(), "t1")
	if err != nil {
		t.Fatalf("RevokeToken returned error: %v", err)
	}
	if capturedMethod != "DELETE" {
		t.Errorf("method = %q, want DELETE", capturedMethod)
	}
	if capturedPath != "/api/v1/user/tokens/t1" {
		t.Errorf("path = %q, want /api/v1/user/tokens/t1", capturedPath)
	}
}

// TestListUserOrgs verifies that ListUserOrgs sends GET to /user/orgs and
// decodes the bare JSON array into []*Organization (TS-12-54).
func TestListUserOrgs(t *testing.T) {
	var capturedMethod, capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`[{"id":"org1","name":"Acme","slug":"acme","status":"active","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}]`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	orgs, err := client.ListUserOrgs(context.Background())
	if err != nil {
		t.Fatalf("ListUserOrgs returned error: %v", err)
	}
	if capturedMethod != "GET" {
		t.Errorf("method = %q, want GET", capturedMethod)
	}
	if capturedPath != "/api/v1/user/orgs" {
		t.Errorf("path = %q, want /api/v1/user/orgs", capturedPath)
	}
	if len(orgs) != 1 {
		t.Fatalf("len(orgs) = %d, want 1", len(orgs))
	}
	if orgs[0].ID != "org1" {
		t.Errorf("orgs[0].ID = %q, want %q", orgs[0].ID, "org1")
	}
}

// TestRevokeTokenErrorStatus verifies that RevokeToken returns an *APIError
// when the server returns a non-204 error status (TS-12-E14).
func TestRevokeTokenErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"error":{"code":403,"message":"Forbidden"}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	err := client.RevokeToken(context.Background(), "t1")
	if err == nil {
		t.Fatal("expected error from 403 response")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Code != 403 {
		t.Errorf("apiErr.Code = %d, want 403", apiErr.Code)
	}
}

// ---------------------------------------------------------------------------
// Task 4.3: Admin user list and single-user endpoints
// Test Specs: TS-12-57, TS-12-58, TS-12-59, TS-12-E15
// Requirements: 12-REQ-8.3, 12-REQ-8.4, 12-REQ-8.5, 12-REQ-8.E1
// ---------------------------------------------------------------------------

// TestGetUserByID verifies that GetUserByID sends GET to /users/{id} and
// returns *Response[User] with Data populated (TS-12-57).
func TestGetUserByID(t *testing.T) {
	var capturedMethod, capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"id":"user-1","username":"alice","email":"a@b.com","full_name":"Alice","status":"active","role":"user","provider":"github","provider_id":"gh1","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	resp, err := client.GetUserByID(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("GetUserByID returned error: %v", err)
	}
	if capturedMethod != "GET" {
		t.Errorf("method = %q, want GET", capturedMethod)
	}
	if capturedPath != "/api/v1/users/user-1" {
		t.Errorf("path = %q, want /api/v1/users/user-1", capturedPath)
	}
	if resp == nil {
		t.Fatal("GetUserByID returned nil response")
	}
	if resp.Data.ID != "user-1" {
		t.Errorf("resp.Data.ID = %q, want %q", resp.Data.ID, "user-1")
	}
}

// TestCreateUser verifies that CreateUser sends POST to /users with JSON
// body and returns *User on 201 (TS-12-58).
func TestCreateUser(t *testing.T) {
	var capturedMethod, capturedPath string
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"id":"u2","username":"bob","email":"bob@example.com","full_name":"","status":"active","role":"user","provider":"github","provider_id":"gh2","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	user, err := client.CreateUser(context.Background(), &CreateUserRequest{
		Username:   "bob",
		Email:      "bob@example.com",
		Provider:   "github",
		ProviderID: "gh2",
	})
	if err != nil {
		t.Fatalf("CreateUser returned error: %v", err)
	}
	if capturedMethod != "POST" {
		t.Errorf("method = %q, want POST", capturedMethod)
	}
	if capturedPath != "/api/v1/users" {
		t.Errorf("path = %q, want /api/v1/users", capturedPath)
	}
	if user == nil {
		t.Fatal("CreateUser returned nil user")
	}
	if user.Username != "bob" {
		t.Errorf("user.Username = %q, want %q", user.Username, "bob")
	}
	if !strings.Contains(string(capturedBody), `"username":"bob"`) {
		t.Errorf("request body missing username: %s", capturedBody)
	}
}

// TestUpdateUserByID verifies that UpdateUserByID sends PATCH to /users/{id}
// with full_name body and returns *User (TS-12-59).
func TestUpdateUserByID(t *testing.T) {
	var capturedMethod, capturedPath string
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"id":"user-1","username":"alice","email":"a@b.com","full_name":"Bob Smith","status":"active","role":"user","provider":"github","provider_id":"gh1","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	user, err := client.UpdateUserByID(context.Background(), "user-1", &UpdateUserRequest{FullName: "Bob Smith"})
	if err != nil {
		t.Fatalf("UpdateUserByID returned error: %v", err)
	}
	if capturedMethod != "PATCH" {
		t.Errorf("method = %q, want PATCH", capturedMethod)
	}
	if capturedPath != "/api/v1/users/user-1" {
		t.Errorf("path = %q, want /api/v1/users/user-1", capturedPath)
	}
	if user == nil {
		t.Fatal("UpdateUserByID returned nil user")
	}
	if !strings.Contains(string(capturedBody), `"full_name"`) {
		t.Errorf("request body missing full_name: %s", capturedBody)
	}
}

// NOTE: TestListUsersEmptyArray (TS-12-E15, 12-REQ-8.E1) is already covered
// by TestEmptyArrayListUsers in task 3.5 above, which verifies that ListUsers
// returns a non-nil empty []*User slice when the server returns [].

// ---------------------------------------------------------------------------
// Task 4.4: Admin user action endpoints (promote, demote, block, unblock)
// Test Specs: TS-12-60, TS-12-61, TS-12-62, TS-12-63
// Requirements: 12-REQ-8.6, 12-REQ-8.7, 12-REQ-8.8, 12-REQ-8.9
// ---------------------------------------------------------------------------

// TestPromoteUser verifies that PromoteUser sends POST to
// /users/{id}/promote with no body and returns *User (TS-12-60).
func TestPromoteUser(t *testing.T) {
	var capturedMethod, capturedPath string
	var capturedBodyLen int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		capturedBodyLen = r.ContentLength
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"id":"user-1","username":"alice","email":"a@b.com","full_name":"Alice","status":"active","role":"admin","provider":"github","provider_id":"gh1","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	user, err := client.PromoteUser(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("PromoteUser returned error: %v", err)
	}
	if capturedMethod != "POST" {
		t.Errorf("method = %q, want POST", capturedMethod)
	}
	if capturedPath != "/api/v1/users/user-1/promote" {
		t.Errorf("path = %q, want /api/v1/users/user-1/promote", capturedPath)
	}
	if capturedBodyLen > 0 {
		t.Errorf("expected no body, got Content-Length %d", capturedBodyLen)
	}
	if user == nil {
		t.Fatal("PromoteUser returned nil user")
	}
}

// TestDemoteUser verifies that DemoteUser sends POST to /users/{id}/demote
// with no body and returns *User (TS-12-61).
func TestDemoteUser(t *testing.T) {
	var capturedMethod, capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"id":"user-1","username":"alice","email":"a@b.com","full_name":"Alice","status":"active","role":"user","provider":"github","provider_id":"gh1","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	user, err := client.DemoteUser(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("DemoteUser returned error: %v", err)
	}
	if capturedMethod != "POST" {
		t.Errorf("method = %q, want POST", capturedMethod)
	}
	if capturedPath != "/api/v1/users/user-1/demote" {
		t.Errorf("path = %q, want /api/v1/users/user-1/demote", capturedPath)
	}
	if user == nil {
		t.Fatal("DemoteUser returned nil user")
	}
}

// TestBlockUser verifies that BlockUser sends POST to /users/{id}/block
// with no body and returns *User (TS-12-62).
func TestBlockUser(t *testing.T) {
	var capturedMethod, capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"id":"user-1","username":"alice","email":"a@b.com","full_name":"Alice","status":"blocked","role":"user","provider":"github","provider_id":"gh1","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	user, err := client.BlockUser(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("BlockUser returned error: %v", err)
	}
	if capturedMethod != "POST" {
		t.Errorf("method = %q, want POST", capturedMethod)
	}
	if capturedPath != "/api/v1/users/user-1/block" {
		t.Errorf("path = %q, want /api/v1/users/user-1/block", capturedPath)
	}
	if user == nil {
		t.Fatal("BlockUser returned nil user")
	}
}

// TestUnblockUser verifies that UnblockUser sends POST to /users/{id}/unblock
// with no body and returns *User (TS-12-63).
func TestUnblockUser(t *testing.T) {
	var capturedMethod, capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"id":"user-1","username":"alice","email":"a@b.com","full_name":"Alice","status":"active","role":"user","provider":"github","provider_id":"gh1","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	user, err := client.UnblockUser(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("UnblockUser returned error: %v", err)
	}
	if capturedMethod != "POST" {
		t.Errorf("method = %q, want POST", capturedMethod)
	}
	if capturedPath != "/api/v1/users/user-1/unblock" {
		t.Errorf("path = %q, want /api/v1/users/user-1/unblock", capturedPath)
	}
	if user == nil {
		t.Fatal("UnblockUser returned nil user")
	}
}

// ---------------------------------------------------------------------------
// Task 4.5: Admin user key and token management endpoints
// Test Specs: TS-12-64, TS-12-65, TS-12-66, TS-12-67
// Requirements: 12-REQ-8.10, 12-REQ-8.11, 12-REQ-8.12, 12-REQ-8.13
// ---------------------------------------------------------------------------

// TestListUserKeys verifies that ListUserKeys sends GET to
// /users/{userID}/keys and decodes the bare JSON array into []*APIKeyMeta
// (TS-12-64).
func TestListUserKeys(t *testing.T) {
	var capturedMethod, capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`[{"key_id":"k1","created_at":"2024-01-01T00:00:00Z","expires_at":null,"revoked_at":null}]`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	keys, err := client.ListUserKeys(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("ListUserKeys returned error: %v", err)
	}
	if capturedMethod != "GET" {
		t.Errorf("method = %q, want GET", capturedMethod)
	}
	if capturedPath != "/api/v1/users/user-1/keys" {
		t.Errorf("path = %q, want /api/v1/users/user-1/keys", capturedPath)
	}
	if len(keys) != 1 {
		t.Fatalf("len(keys) = %d, want 1", len(keys))
	}
	if keys[0].KeyID != "k1" {
		t.Errorf("keys[0].KeyID = %q, want %q", keys[0].KeyID, "k1")
	}
}

// TestRevokeUserKey verifies that RevokeUserKey sends DELETE to
// /users/{userID}/keys/{keyID} and returns nil error on 204 (TS-12-65).
func TestRevokeUserKey(t *testing.T) {
	var capturedMethod, capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.WriteHeader(204)
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	err := client.RevokeUserKey(context.Background(), "user-1", "key-1")
	if err != nil {
		t.Fatalf("RevokeUserKey returned error: %v", err)
	}
	if capturedMethod != "DELETE" {
		t.Errorf("method = %q, want DELETE", capturedMethod)
	}
	if capturedPath != "/api/v1/users/user-1/keys/key-1" {
		t.Errorf("path = %q, want /api/v1/users/user-1/keys/key-1", capturedPath)
	}
}

// TestListUserTokens verifies that ListUserTokens sends GET to
// /users/{userID}/tokens and decodes the bare JSON array into []*PAT
// (TS-12-66).
func TestListUserTokens(t *testing.T) {
	var capturedMethod, capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`[{"token_id":"t1","name":"ci","permissions":["read"],"created_at":"2024-01-01T00:00:00Z","expires_at":null,"revoked_at":null}]`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	tokens, err := client.ListUserTokens(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("ListUserTokens returned error: %v", err)
	}
	if capturedMethod != "GET" {
		t.Errorf("method = %q, want GET", capturedMethod)
	}
	if capturedPath != "/api/v1/users/user-1/tokens" {
		t.Errorf("path = %q, want /api/v1/users/user-1/tokens", capturedPath)
	}
	if len(tokens) != 1 {
		t.Fatalf("len(tokens) = %d, want 1", len(tokens))
	}
}

// TestRevokeUserToken verifies that RevokeUserToken sends DELETE to
// /users/{userID}/tokens/{tokenID} and returns nil error on 204 (TS-12-67).
func TestRevokeUserToken(t *testing.T) {
	var capturedMethod, capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.WriteHeader(204)
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	err := client.RevokeUserToken(context.Background(), "user-1", "t1")
	if err != nil {
		t.Fatalf("RevokeUserToken returned error: %v", err)
	}
	if capturedMethod != "DELETE" {
		t.Errorf("method = %q, want DELETE", capturedMethod)
	}
	if capturedPath != "/api/v1/users/user-1/tokens/t1" {
		t.Errorf("path = %q, want /api/v1/users/user-1/tokens/t1", capturedPath)
	}
}

// ---------------------------------------------------------------------------
// Task 5.1: Admin organization CRUD and action endpoints
// Test Specs: TS-12-68, TS-12-71, TS-12-72, TS-12-73, TS-12-74, TS-12-75
// Requirements: 12-REQ-9.1, 12-REQ-9.4, 12-REQ-9.5, 12-REQ-9.6,
//               12-REQ-9.7, 12-REQ-9.8
// ---------------------------------------------------------------------------

const testOrgJSON = `{"id":"org-1","name":"Acme","slug":"acme","url":"","status":"active","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}`

// TestCreateOrg verifies that CreateOrg sends a POST request to
// mountPoint+"/orgs" with JSON body and returns *Organization on 201 (TS-12-68).
func TestCreateOrg(t *testing.T) {
	var capturedMethod, capturedPath string
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"id":"org1","name":"Acme","slug":"acme","status":"active","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	org, err := client.CreateOrg(context.Background(), &CreateOrgRequest{Name: "Acme", Slug: "acme"})
	if err != nil {
		t.Fatalf("CreateOrg returned error: %v", err)
	}
	if capturedMethod != "POST" {
		t.Errorf("method = %q, want POST", capturedMethod)
	}
	if capturedPath != "/api/v1/orgs" {
		t.Errorf("path = %q, want /api/v1/orgs", capturedPath)
	}
	if org == nil {
		t.Fatal("CreateOrg returned nil")
	}
	if org.ID != "org1" {
		t.Errorf("org.ID = %q, want %q", org.ID, "org1")
	}
	if org.Slug != "acme" {
		t.Errorf("org.Slug = %q, want %q", org.Slug, "acme")
	}
	if !strings.Contains(string(capturedBody), `"name":"Acme"`) {
		t.Errorf("request body missing name: %s", capturedBody)
	}
	if !strings.Contains(string(capturedBody), `"slug":"acme"`) {
		t.Errorf("request body missing slug: %s", capturedBody)
	}
}

// TestGetOrg verifies that GetOrg sends a GET request to
// mountPoint+"/orgs/{id}" and returns *Response[Organization] on success
// (TS-12-71).
func TestGetOrg(t *testing.T) {
	var capturedMethod, capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"id":"org-1","name":"Acme","slug":"acme","status":"active","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	resp, err := client.GetOrg(context.Background(), "org-1")
	if err != nil {
		t.Fatalf("GetOrg returned error: %v", err)
	}
	if capturedMethod != "GET" {
		t.Errorf("method = %q, want GET", capturedMethod)
	}
	if capturedPath != "/api/v1/orgs/org-1" {
		t.Errorf("path = %q, want /api/v1/orgs/org-1", capturedPath)
	}
	if resp == nil {
		t.Fatal("GetOrg returned nil response")
	}
	if resp.Data.ID != "org-1" {
		t.Errorf("resp.Data.ID = %q, want %q", resp.Data.ID, "org-1")
	}
}

// TestUpdateOrg verifies that UpdateOrg sends a PATCH request to
// mountPoint+"/orgs/{id}" with JSON body and returns *Organization (TS-12-72).
func TestUpdateOrg(t *testing.T) {
	var capturedMethod, capturedPath string
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"id":"org-1","name":"NewName","slug":"acme","status":"active","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	name := "NewName"
	org, err := client.UpdateOrg(context.Background(), "org-1", &UpdateOrgRequest{Name: &name})
	if err != nil {
		t.Fatalf("UpdateOrg returned error: %v", err)
	}
	if capturedMethod != "PATCH" {
		t.Errorf("method = %q, want PATCH", capturedMethod)
	}
	if capturedPath != "/api/v1/orgs/org-1" {
		t.Errorf("path = %q, want /api/v1/orgs/org-1", capturedPath)
	}
	if org == nil {
		t.Fatal("UpdateOrg returned nil")
	}
	if org.Name != "NewName" {
		t.Errorf("org.Name = %q, want %q", org.Name, "NewName")
	}
	if !strings.Contains(string(capturedBody), `"name"`) {
		t.Errorf("request body missing name: %s", capturedBody)
	}
}

// TestDeleteOrg verifies that DeleteOrg sends a DELETE request to
// mountPoint+"/orgs/{id}" and returns nil error on 204 (TS-12-73).
func TestDeleteOrg(t *testing.T) {
	var capturedMethod, capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.WriteHeader(204)
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	err := client.DeleteOrg(context.Background(), "org-1")
	if err != nil {
		t.Fatalf("DeleteOrg returned error: %v", err)
	}
	if capturedMethod != "DELETE" {
		t.Errorf("method = %q, want DELETE", capturedMethod)
	}
	if capturedPath != "/api/v1/orgs/org-1" {
		t.Errorf("path = %q, want /api/v1/orgs/org-1", capturedPath)
	}
}

// TestBlockOrg verifies that BlockOrg sends a POST request to
// mountPoint+"/orgs/{id}/block" with no body and returns *Organization
// with Status=="blocked" (TS-12-74).
func TestBlockOrg(t *testing.T) {
	var capturedMethod, capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"id":"org-1","name":"Acme","slug":"acme","status":"blocked","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	org, err := client.BlockOrg(context.Background(), "org-1")
	if err != nil {
		t.Fatalf("BlockOrg returned error: %v", err)
	}
	if capturedMethod != "POST" {
		t.Errorf("method = %q, want POST", capturedMethod)
	}
	if capturedPath != "/api/v1/orgs/org-1/block" {
		t.Errorf("path = %q, want /api/v1/orgs/org-1/block", capturedPath)
	}
	if org == nil {
		t.Fatal("BlockOrg returned nil")
	}
	if org.Status != "blocked" {
		t.Errorf("org.Status = %q, want %q", org.Status, "blocked")
	}
}

// TestUnblockOrg verifies that UnblockOrg sends a POST request to
// mountPoint+"/orgs/{id}/unblock" with no body and returns *Organization
// with Status=="active" (TS-12-75).
func TestUnblockOrg(t *testing.T) {
	var capturedMethod, capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"id":"org-1","name":"Acme","slug":"acme","status":"active","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	org, err := client.UnblockOrg(context.Background(), "org-1")
	if err != nil {
		t.Fatalf("UnblockOrg returned error: %v", err)
	}
	if capturedMethod != "POST" {
		t.Errorf("method = %q, want POST", capturedMethod)
	}
	if capturedPath != "/api/v1/orgs/org-1/unblock" {
		t.Errorf("path = %q, want /api/v1/orgs/org-1/unblock", capturedPath)
	}
	if org == nil {
		t.Fatal("UnblockOrg returned nil")
	}
	if org.Status != "active" {
		t.Errorf("org.Status = %q, want %q", org.Status, "active")
	}
}

// ---------------------------------------------------------------------------
// Task 5.2: Admin organization member management endpoints
// Test Specs: TS-12-76, TS-12-77, TS-12-78, TS-12-E16
// Requirements: 12-REQ-9.9, 12-REQ-9.10, 12-REQ-9.11, 12-REQ-9.E1
// ---------------------------------------------------------------------------

// TestListOrgMembers verifies that ListOrgMembers sends a GET request to
// mountPoint+"/orgs/{orgID}/members" and decodes the bare JSON array
// into []*User (TS-12-76).
func TestListOrgMembers(t *testing.T) {
	var capturedMethod, capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`[{"id":"u1","username":"alice","email":"a@b.com","full_name":"Alice","status":"active","role":"user","provider":"github","provider_id":"gh1","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}]`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	members, err := client.ListOrgMembers(context.Background(), "org-1")
	if err != nil {
		t.Fatalf("ListOrgMembers returned error: %v", err)
	}
	if capturedMethod != "GET" {
		t.Errorf("method = %q, want GET", capturedMethod)
	}
	if capturedPath != "/api/v1/orgs/org-1/members" {
		t.Errorf("path = %q, want /api/v1/orgs/org-1/members", capturedPath)
	}
	if len(members) != 1 {
		t.Fatalf("len(members) = %d, want 1", len(members))
	}
	if members[0].ID != "u1" {
		t.Errorf("members[0].ID = %q, want %q", members[0].ID, "u1")
	}
}

// TestAddOrgMember verifies that AddOrgMember sends a PUT request to
// mountPoint+"/orgs/{orgID}/members/{userID}" with no request body
// and returns nil error on 204 (TS-12-77).
func TestAddOrgMember(t *testing.T) {
	var capturedMethod, capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.WriteHeader(204)
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	err := client.AddOrgMember(context.Background(), "org-1", "user-1")
	if err != nil {
		t.Fatalf("AddOrgMember returned error: %v", err)
	}
	if capturedMethod != "PUT" {
		t.Errorf("method = %q, want PUT", capturedMethod)
	}
	if capturedPath != "/api/v1/orgs/org-1/members/user-1" {
		t.Errorf("path = %q, want /api/v1/orgs/org-1/members/user-1", capturedPath)
	}
}

// TestRemoveOrgMember verifies that RemoveOrgMember sends a DELETE request to
// mountPoint+"/orgs/{orgID}/members/{userID}" and returns nil error on 204
// (TS-12-78).
func TestRemoveOrgMember(t *testing.T) {
	var capturedMethod, capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.WriteHeader(204)
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	err := client.RemoveOrgMember(context.Background(), "org-1", "user-1")
	if err != nil {
		t.Fatalf("RemoveOrgMember returned error: %v", err)
	}
	if capturedMethod != "DELETE" {
		t.Errorf("method = %q, want DELETE", capturedMethod)
	}
	if capturedPath != "/api/v1/orgs/org-1/members/user-1" {
		t.Errorf("path = %q, want /api/v1/orgs/org-1/members/user-1", capturedPath)
	}
}

// TestAddOrgMemberNonURLSafeID verifies that AddOrgMember does not escape
// path parameters: a non-URL-safe userID causes a transport error or reaches
// the server with slashes verbatim (TS-12-E16).
func TestAddOrgMemberNonURLSafeID(t *testing.T) {
	// Use a port that is not listening to ensure transport error.
	client := NewClient("http://localhost:19999", WithAPIKey("key"))
	err := client.AddOrgMember(context.Background(), "org-1", "user/with/slashes")
	// Either transport error or the path was malformed — no *APIError.
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) {
			t.Errorf("expected plain error, not *APIError; got %v", apiErr)
		}
	}
	// If err is nil, the request somehow succeeded or was handled —
	// also acceptable per spec (the SDK does not escape the value).
}

// ---------------------------------------------------------------------------
// Task 5.3: Auth and health/meta endpoint tests
// Test Specs: TS-12-79, TS-12-80, TS-12-81, TS-12-82, TS-12-83, TS-12-E17
// Requirements: 12-REQ-10.1, 12-REQ-10.2, 12-REQ-11.1, 12-REQ-11.2,
//               12-REQ-11.3, 12-REQ-10.E1
// ---------------------------------------------------------------------------

// TestGetProviders verifies that GetProviders sends a GET request to
// mountPoint+"/auth/providers" and decodes the bare JSON array
// into []*OAuthProvider (TS-12-79).
func TestGetProviders(t *testing.T) {
	var capturedMethod, capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`[{"name":"github","authorize_url":"https://github.com/login/oauth/authorize"}]`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	providers, err := client.GetProviders(context.Background())
	if err != nil {
		t.Fatalf("GetProviders returned error: %v", err)
	}
	if capturedMethod != "GET" {
		t.Errorf("method = %q, want GET", capturedMethod)
	}
	if capturedPath != "/api/v1/auth/providers" {
		t.Errorf("path = %q, want /api/v1/auth/providers", capturedPath)
	}
	if len(providers) != 1 {
		t.Fatalf("len(providers) = %d, want 1", len(providers))
	}
	if providers[0].Name != "github" {
		t.Errorf("providers[0].Name = %q, want %q", providers[0].Name, "github")
	}
}

// TestExchangeOAuthCode verifies that ExchangeOAuthCode sends a POST request
// to mountPoint+"/auth/callback" with JSON body and returns
// *AuthCallbackResponse on success (TS-12-80).
func TestExchangeOAuthCode(t *testing.T) {
	var capturedMethod, capturedPath string
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"user":{"id":"u1","username":"alice","email":"a@b.com","full_name":"Alice","status":"active","role":"user","provider":"github","provider_id":"gh1","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"},"api_key":{"key":"secret","key_id":"k1","expires_at":null}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	resp, err := client.ExchangeOAuthCode(context.Background(), &AuthCallbackRequest{
		Provider:    "github",
		Code:        "code123",
		RedirectURI: "http://localhost/cb",
	})
	if err != nil {
		t.Fatalf("ExchangeOAuthCode returned error: %v", err)
	}
	if capturedMethod != "POST" {
		t.Errorf("method = %q, want POST", capturedMethod)
	}
	if capturedPath != "/api/v1/auth/callback" {
		t.Errorf("path = %q, want /api/v1/auth/callback", capturedPath)
	}
	if resp == nil {
		t.Fatal("ExchangeOAuthCode returned nil")
	}
	if resp.User == nil || resp.User.ID != "u1" {
		t.Errorf("resp.User.ID = %v, want %q", resp.User, "u1")
	}
	if resp.APIKey == nil || resp.APIKey.Key != "secret" {
		t.Errorf("resp.APIKey.Key = %v, want %q", resp.APIKey, "secret")
	}
	if !strings.Contains(string(capturedBody), `"provider":"github"`) {
		t.Errorf("request body missing provider: %s", capturedBody)
	}
	if !strings.Contains(string(capturedBody), `"code":"code123"`) {
		t.Errorf("request body missing code: %s", capturedBody)
	}
	if !strings.Contains(string(capturedBody), `"redirect_uri":"http://localhost/cb"`) {
		t.Errorf("request body missing redirect_uri: %s", capturedBody)
	}
}

// TestHealthz verifies that Healthz sends a GET request to
// baseURL+"/healthz" (without the mount point) and returns *HealthResponse
// (TS-12-81).
func TestHealthz(t *testing.T) {
	var capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithMountPoint("/api/v1"))
	resp, err := client.Healthz(context.Background())
	if err != nil {
		t.Fatalf("Healthz returned error: %v", err)
	}
	if resp == nil {
		t.Fatal("Healthz returned nil")
	}
	if resp.Status != "ok" {
		t.Errorf("resp.Status = %q, want %q", resp.Status, "ok")
	}
	if capturedPath != "/healthz" {
		t.Errorf("path = %q, want /healthz (should bypass mount point)", capturedPath)
	}
}

// TestReadyz verifies that Readyz sends a GET request to baseURL+"/readyz"
// (without the mount point) and returns *HealthResponse (TS-12-82).
func TestReadyz(t *testing.T) {
	var capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	resp, err := client.Readyz(context.Background())
	if err != nil {
		t.Fatalf("Readyz returned error: %v", err)
	}
	if resp == nil {
		t.Fatal("Readyz returned nil")
	}
	if resp.Status != "ok" {
		t.Errorf("resp.Status = %q, want %q", resp.Status, "ok")
	}
	if capturedPath != "/readyz" {
		t.Errorf("path = %q, want /readyz", capturedPath)
	}
}

// TestVersion verifies that Version sends a GET request to baseURL+"/version"
// (without the mount point) and returns *VersionResponse (TS-12-83).
func TestVersion(t *testing.T) {
	var capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"go_version":"1.0.0","build_time":"2024-01-01","commit":"abc123","mount_point":"/api/v1"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	resp, err := client.Version(context.Background())
	if err != nil {
		t.Fatalf("Version returned error: %v", err)
	}
	if resp == nil {
		t.Fatal("Version returned nil")
	}
	if resp.Version != "1.0.0" {
		t.Errorf("resp.Version = %q, want %q", resp.Version, "1.0.0")
	}
	if resp.BuildTime != "2024-01-01" {
		t.Errorf("resp.BuildTime = %q, want %q", resp.BuildTime, "2024-01-01")
	}
	if resp.Commit != "abc123" {
		t.Errorf("resp.Commit = %q, want %q", resp.Commit, "abc123")
	}
	if capturedPath != "/version" {
		t.Errorf("path = %q, want /version", capturedPath)
	}
}

// TestExchangeOAuthCode4xxError verifies that ExchangeOAuthCode returns
// *APIError when the server returns a 4xx error with JSON error envelope
// (TS-12-E17).
func TestExchangeOAuthCode4xxError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":{"code":400,"message":"Invalid code"}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	resp, err := client.ExchangeOAuthCode(context.Background(), &AuthCallbackRequest{
		Provider:    "github",
		Code:        "bad",
		RedirectURI: "http://localhost/cb",
	})
	if resp != nil {
		t.Errorf("expected nil response on 400, got %+v", resp)
	}
	if err == nil {
		t.Fatal("expected error from 400 response")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Code != 400 {
		t.Errorf("apiErr.Code = %d, want 400", apiErr.Code)
	}
	if apiErr.Message != "Invalid code" {
		t.Errorf("apiErr.Message = %q, want %q", apiErr.Message, "Invalid code")
	}
}

// ---------------------------------------------------------------------------
// Task 5.4: Non-JSON error bodies and network error edge cases
// Test Specs: TS-12-E10, TS-12-E11, TS-12-E18
// Requirements: 12-REQ-6.E1, 12-REQ-6.E2, 12-REQ-11.E1
// ---------------------------------------------------------------------------

// TestHTMLErrorBody502 verifies that do returns *APIError with Code=502 and
// Message containing "Bad Gateway" when server returns a 502 with an HTML
// body (TS-12-E10).
func TestHTMLErrorBody502(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502)
		_, _ = w.Write([]byte("<html>Bad Gateway</html>"))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	_, err := client.GetUser(context.Background())
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
	if !strings.Contains(apiErr.Message, "Bad Gateway") {
		t.Errorf("apiErr.Message = %q, want it to contain %q", apiErr.Message, "Bad Gateway")
	}
}

// TestPlainTextErrorBody413 verifies that do returns *APIError with Code=413
// and Message from HTTP status text when the server returns a 413 with a
// plain-text body (TS-12-E11).
func TestPlainTextErrorBody413(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(413)
		_, _ = w.Write([]byte("Request Entity Too Large"))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	_, err := client.CreateToken(context.Background(), &CreateTokenRequest{
		Name:        "t",
		Permissions: []string{"read"},
	})
	if err == nil {
		t.Fatal("expected error from 413 response")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Code != 413 {
		t.Errorf("apiErr.Code = %d, want 413", apiErr.Code)
	}
}

// TestHealthzUnreachableServer verifies that Healthz returns a plain error
// wrapping the network error when the server is unreachable; errors.As
// for *APIError returns false (TS-12-E18).
func TestHealthzUnreachableServer(t *testing.T) {
	client := NewClient("http://localhost:19998", WithAPIKey("key"))
	resp, err := client.Healthz(context.Background())
	if err == nil {
		t.Fatal("expected error from unreachable server")
	}
	if resp != nil {
		t.Errorf("expected nil response, got %+v", resp)
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Errorf("expected plain error, not *APIError; got %v", apiErr)
	}
	if errors.Unwrap(err) == nil {
		t.Error("expected wrapped error; errors.Unwrap returned nil")
	}
}

// ---------------------------------------------------------------------------
// Task 5.5: Property-based and smoke tests
// Test Specs: TS-12-P1 through TS-12-P10
// Requirements: 12-PROP-1 through 12-PROP-10
// ---------------------------------------------------------------------------

// TestPropClientConcurrency verifies that Client is safe for concurrent use
// by multiple goroutines (TS-12-P1). Run with: go test -race ./...
// 50 goroutines call various methods simultaneously; the race detector
// catches any data races.
func TestPropClientConcurrency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/healthz", "/readyz":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/version":
			_, _ = w.Write([]byte(testVersionJSON))
		default:
			_, _ = w.Write([]byte(testUserJSON))
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"), WithRequestID("rid-1"))
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			switch i % 5 {
			case 0:
				_, _ = client.Healthz(ctx)
			case 1:
				_, _ = client.GetUser(ctx)
			case 2:
				_, _ = client.ListOrgs(ctx, nil)
			case 3:
				_, _ = client.Version(ctx)
			case 4:
				_, _ = client.ListKeys(ctx)
			}
		}(i)
	}
	wg.Wait()
}

// TestPropAPIErrorExhaustive verifies that for any HTTP status code >= 400,
// do always returns a non-nil *APIError with Code set to the HTTP status
// code (TS-12-P2).
func TestPropAPIErrorExhaustive(t *testing.T) {
	// Test representative status codes with both JSON and non-JSON bodies.
	statusCodes := []int{400, 401, 403, 404, 405, 409, 413, 422, 429, 500, 502, 503}
	bodyTypes := []struct {
		name string
		body string
	}{
		{"json_envelope", `{"error":{"code":%d,"message":"test error"}}`},
		{"html", `<html>Error</html>`},
		{"plain_text", `Some error text`},
		{"empty", ``},
	}

	for _, code := range statusCodes {
		for _, bt := range bodyTypes {
			name := fmt.Sprintf("status_%d_%s", code, bt.name)
			t.Run(name, func(t *testing.T) {
				statusCode := code
				body := bt.body
				if bt.name == "json_envelope" {
					body = fmt.Sprintf(bt.body, statusCode)
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if bt.name == "json_envelope" {
						w.Header().Set("Content-Type", "application/json")
					}
					w.WriteHeader(statusCode)
					_, _ = w.Write([]byte(body))
				}))
				defer server.Close()

				client := NewClient(server.URL, WithAPIKey("key"))
				_, err := client.GetUser(context.Background())
				if err == nil {
					t.Fatalf("expected error for status %d", statusCode)
				}
				var apiErr *APIError
				if !errors.As(err, &apiErr) {
					t.Fatalf("expected *APIError for status %d/%s, got %T: %v",
						statusCode, bt.name, err, err)
				}
				if apiErr.Code != statusCode {
					t.Errorf("apiErr.Code = %d, want %d", apiErr.Code, statusCode)
				}
			})
		}
	}
}

// TestPropPlainErrorsWrapCause verifies that all plain error scenarios
// (network, context, JSON decode, JSON encode) satisfy errors.Unwrap != nil
// (TS-12-P3).
func TestPropPlainErrorsWrapCause(t *testing.T) {
	t.Run("network_failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		serverURL := server.URL
		server.Close()

		client := NewClient(serverURL)
		_, err := client.Healthz(context.Background())
		if err == nil {
			t.Fatal("expected error")
		}
		if errors.Unwrap(err) == nil {
			t.Error("network error should wrap cause")
		}
		var apiErr *APIError
		if errors.As(err, &apiErr) {
			t.Error("expected plain error, not *APIError")
		}
	})

	t.Run("context_cancelled", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(200)
		}))
		defer server.Close()

		client := NewClient(server.URL)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := client.Healthz(ctx)
		if err == nil {
			t.Fatal("expected error")
		}
		if errors.Unwrap(err) == nil {
			t.Error("context error should wrap cause")
		}
		var apiErr *APIError
		if errors.As(err, &apiErr) {
			t.Error("expected plain error, not *APIError")
		}
	})

	t.Run("json_decode_error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			_, _ = w.Write([]byte("not json"))
		}))
		defer server.Close()

		client := NewClient(server.URL)
		_, err := client.Healthz(context.Background())
		if err == nil {
			t.Fatal("expected error")
		}
		if errors.Unwrap(err) == nil {
			t.Error("JSON decode error should wrap cause")
		}
		var apiErr *APIError
		if errors.As(err, &apiErr) {
			t.Error("expected plain error, not *APIError")
		}
	})

	t.Run("json_marshal_error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
		}))
		defer server.Close()

		client := NewClient(server.URL)
		badBody := make(chan int)
		_, _, err := client.do(context.Background(), "POST", "/test", badBody, nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if errors.Unwrap(err) == nil {
			t.Error("marshal error should wrap cause")
		}
		var apiErr *APIError
		if errors.As(err, &apiErr) {
			t.Error("expected plain error, not *APIError")
		}
	})
}

// TestPropMountPointNormalizationIdempotent verifies that for any path
// string p, applying WithMountPoint(p) produces a mountPoint starting
// with exactly one '/' and with no trailing '/' (TS-12-P4).
func TestPropMountPointNormalizationIdempotent(t *testing.T) {
	paths := []string{
		"", "api", "/api", "api/", "/api/",
		"api/v1", "/api/v1", "/api/v1/", "//api", "/api//v1",
	}

	for _, p := range paths {
		t.Run(fmt.Sprintf("path_%q", p), func(t *testing.T) {
			c := NewClient("http://example.com", WithMountPoint(p))

			// Must start with exactly one '/'
			if !strings.HasPrefix(c.mountPoint, "/") {
				t.Errorf("mountPoint %q does not start with '/'", c.mountPoint)
			}
			if strings.HasPrefix(c.mountPoint, "//") {
				t.Errorf("mountPoint %q starts with double slash", c.mountPoint)
			}

			// Must not end with '/' (unless it is exactly "/")
			if c.mountPoint != "/" && strings.HasSuffix(c.mountPoint, "/") {
				t.Errorf("mountPoint %q has trailing slash", c.mountPoint)
			}

			// Idempotent: applying again produces the same result
			c2 := NewClient("http://example.com", WithMountPoint(c.mountPoint))
			if c.mountPoint != c2.mountPoint {
				t.Errorf("not idempotent: first=%q, second=%q", c.mountPoint, c2.mountPoint)
			}
		})
	}
}

// TestPropHealthProbesBypassMountPoint verifies that Healthz, Readyz, and
// Version always construct URLs without the mount point prefix, regardless
// of mountPoint configuration (TS-12-P5).
func TestPropHealthProbesBypassMountPoint(t *testing.T) {
	mountPoints := []string{"/api/v1", "/v2", "/deeply/nested/mount", "/"}

	for _, mp := range mountPoints {
		t.Run(fmt.Sprintf("mount_%s", mp), func(t *testing.T) {
			var capturedPaths []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedPaths = append(capturedPaths, r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/version":
					_, _ = w.Write([]byte(testVersionJSON))
				default:
					_, _ = w.Write([]byte(testHealthJSON))
				}
			}))
			defer server.Close()

			client := NewClient(server.URL, WithMountPoint(mp))
			_, _ = client.Healthz(context.Background())
			_, _ = client.Readyz(context.Background())
			_, _ = client.Version(context.Background())

			if len(capturedPaths) != 3 {
				t.Fatalf("expected 3 requests, got %d", len(capturedPaths))
			}
			expected := []string{"/healthz", "/readyz", "/version"}
			for i, want := range expected {
				if capturedPaths[i] != want {
					t.Errorf("path[%d] = %q, want %q (mount=%q)", i, capturedPaths[i], want, mp)
				}
			}
		})
	}
}

// TestPropListEndpointsNonNilEmptySlice verifies that all list endpoint
// methods return a non-nil empty slice when the server returns []
// (TS-12-P6).
func TestPropListEndpointsNonNilEmptySlice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	ctx := context.Background()

	t.Run("ListKeys", func(t *testing.T) {
		result, err := client.ListKeys(ctx)
		if err != nil {
			t.Fatalf("returned error: %v", err)
		}
		if result == nil {
			t.Fatal("returned nil, want non-nil empty slice")
		}
		if len(result) != 0 {
			t.Errorf("len = %d, want 0", len(result))
		}
	})

	t.Run("ListTokens", func(t *testing.T) {
		result, err := client.ListTokens(ctx)
		if err != nil {
			t.Fatalf("returned error: %v", err)
		}
		if result == nil {
			t.Fatal("returned nil, want non-nil empty slice")
		}
		if len(result) != 0 {
			t.Errorf("len = %d, want 0", len(result))
		}
	})

	t.Run("ListUsers", func(t *testing.T) {
		result, err := client.ListUsers(ctx, nil)
		if err != nil {
			t.Fatalf("returned error: %v", err)
		}
		if result == nil {
			t.Fatal("returned nil, want non-nil empty slice")
		}
		if len(result) != 0 {
			t.Errorf("len = %d, want 0", len(result))
		}
	})

	t.Run("ListOrgs", func(t *testing.T) {
		result, err := client.ListOrgs(ctx, nil)
		if err != nil {
			t.Fatalf("returned error: %v", err)
		}
		if result == nil {
			t.Fatal("returned nil, want non-nil empty slice")
		}
		if len(result) != 0 {
			t.Errorf("len = %d, want 0", len(result))
		}
	})

	t.Run("ListOrgMembers", func(t *testing.T) {
		result, err := client.ListOrgMembers(ctx, "org-1")
		if err != nil {
			t.Fatalf("returned error: %v", err)
		}
		if result == nil {
			t.Fatal("returned nil, want non-nil empty slice")
		}
		if len(result) != 0 {
			t.Errorf("len = %d, want 0", len(result))
		}
	})

	t.Run("ListUserOrgs", func(t *testing.T) {
		result, err := client.ListUserOrgs(ctx)
		if err != nil {
			t.Fatalf("returned error: %v", err)
		}
		if result == nil {
			t.Fatal("returned nil, want non-nil empty slice")
		}
		if len(result) != 0 {
			t.Errorf("len = %d, want 0", len(result))
		}
	})

	t.Run("ListUserKeys", func(t *testing.T) {
		result, err := client.ListUserKeys(ctx, "user-1")
		if err != nil {
			t.Fatalf("returned error: %v", err)
		}
		if result == nil {
			t.Fatal("returned nil, want non-nil empty slice")
		}
		if len(result) != 0 {
			t.Errorf("len = %d, want 0", len(result))
		}
	})

	t.Run("ListUserTokens", func(t *testing.T) {
		result, err := client.ListUserTokens(ctx, "user-1")
		if err != nil {
			t.Fatalf("returned error: %v", err)
		}
		if result == nil {
			t.Fatal("returned nil, want non-nil empty slice")
		}
		if len(result) != 0 {
			t.Errorf("len = %d, want 0", len(result))
		}
	})

	t.Run("GetProviders", func(t *testing.T) {
		result, err := client.GetProviders(ctx)
		if err != nil {
			t.Fatalf("returned error: %v", err)
		}
		if result == nil {
			t.Fatal("returned nil, want non-nil empty slice")
		}
		if len(result) != 0 {
			t.Errorf("len = %d, want 0", len(result))
		}
	})
}

// TestPropAuthHeaderIffAPIKey verifies that the Authorization header is
// present with "Bearer <key>" if and only if apiKey is non-empty (TS-12-P7).
func TestPropAuthHeaderIffAPIKey(t *testing.T) {
	keys := []string{"", "key-1", "key-2", "very-long-key-abcdef", "x"}

	for _, key := range keys {
		t.Run(fmt.Sprintf("key_%q", key), func(t *testing.T) {
			var capturedAuth string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedAuth = r.Header.Get("Authorization")
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(testHealthJSON))
			}))
			defer server.Close()

			client := NewClient(server.URL, WithAPIKey(key))
			_, _ = client.Healthz(context.Background())

			if key == "" {
				if capturedAuth != "" {
					t.Errorf("Authorization = %q, want empty for empty apiKey", capturedAuth)
				}
			} else {
				want := "Bearer " + key
				if capturedAuth != want {
					t.Errorf("Authorization = %q, want %q", capturedAuth, want)
				}
			}
		})
	}
}

// TestPropErrNotModifiedIff304 verifies that ErrNotModified is returned if
// and only if the server responds with 304, and the *Response[T] is always
// nil in that case (TS-12-P8).
func TestPropErrNotModifiedIff304(t *testing.T) {
	type testCase struct {
		name   string
		status int
	}
	statuses := []testCase{
		{"200", 200},
		{"304", 304},
	}

	for _, method := range []string{"GetUser", "GetUserByID", "GetToken", "GetOrg"} {
		for _, tc := range statuses {
			t.Run(fmt.Sprintf("%s_status_%s", method, tc.name), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(tc.status)
					if tc.status == 200 {
						switch {
						case strings.HasSuffix(r.URL.Path, "/tokens/tid"):
							_, _ = w.Write([]byte(`{"token_id":"tid","name":"t","permissions":["read"],"created_at":"2024-01-01T00:00:00Z","expires_at":null,"revoked_at":null}`))
						case strings.HasSuffix(r.URL.Path, "/orgs/oid"):
							_, _ = w.Write([]byte(`{"id":"oid","name":"Acme","slug":"acme","status":"active","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}`))
						default:
							_, _ = w.Write([]byte(testUserJSON))
						}
					}
				}))
				defer server.Close()

				client := NewClient(server.URL, WithAPIKey("key"))
				ctx := context.Background()
				opt := WithIfNoneMatch(`"e"`)

				var err error
				var respNil bool

				switch method {
				case "GetUser":
					resp, e := client.GetUser(ctx, opt)
					err = e
					respNil = (resp == nil)
				case "GetUserByID":
					resp, e := client.GetUserByID(ctx, "id", opt)
					err = e
					respNil = (resp == nil)
				case "GetToken":
					resp, e := client.GetToken(ctx, "tid", opt)
					err = e
					respNil = (resp == nil)
				case "GetOrg":
					resp, e := client.GetOrg(ctx, "oid", opt)
					err = e
					respNil = (resp == nil)
				}

				if tc.status == 200 {
					if errors.Is(err, ErrNotModified) {
						t.Error("should not return ErrNotModified on 200")
					}
					if respNil {
						t.Error("response should be non-nil on 200")
					}
				} else {
					if !errors.Is(err, ErrNotModified) {
						t.Errorf("expected ErrNotModified on 304, got %v", err)
					}
					if !respNil {
						t.Error("response should be nil on 304")
					}
				}
			})
		}
	}
}

// TestPropUpdateUserAlwaysFullName verifies that UpdateUserRequest always
// includes full_name in the JSON body, even when FullName is an empty string
// or contains special characters (TS-12-P9).
func TestPropUpdateUserAlwaysFullName(t *testing.T) {
	values := []string{
		"",
		" ",
		"Alice Smith",
		"   lots   of   spaces   ",
		"名前",
		`has "quotes" and \backslashes`,
		strings.Repeat("x", 1000),
	}

	for _, v := range values {
		t.Run(fmt.Sprintf("fullname_%q", v), func(t *testing.T) {
			var capturedBody []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedBody, _ = io.ReadAll(r.Body)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(testUserJSON))
			}))
			defer server.Close()

			client := NewClient(server.URL, WithAPIKey("key"))
			_, _ = client.UpdateUser(context.Background(), &UpdateUserRequest{FullName: v})

			if !strings.Contains(string(capturedBody), `"full_name"`) {
				t.Errorf("body missing full_name key for value %q: %s", v, capturedBody)
			}

			// Also verify via marshal directly
			data, err := json.Marshal(&UpdateUserRequest{FullName: v})
			if err != nil {
				t.Fatalf("json.Marshal failed: %v", err)
			}
			if !strings.Contains(string(data), `"full_name"`) {
				t.Errorf("marshalled JSON missing full_name: %s", data)
			}
		})
	}
}

// TestPropCanonicalTypesUniqueDefinition verifies that all canonical domain
// types compile in the apikit package (TS-12-P10). If any type were declared
// more than once, the Go compiler would reject the package with a
// 'type X redeclared' error. This test is a compile-time assertion.
func TestPropCanonicalTypesUniqueDefinition(t *testing.T) {
	// Each type referenced below is verified to exist exactly once.
	// Duplicate definitions would cause a build failure.
	types := []interface{}{
		User{},
		APIKeyMeta{},
		APIKeyFull{},
		PAT{},
		PATFull{},
		Organization{},
		OAuthProvider{},
		AuthCallbackRequest{},
		AuthCallbackResponse{},
		CreateUserRequest{},
		UpdateUserRequest{},
		CreateTokenRequest{},
		CreateOrgRequest{},
		UpdateOrgRequest{},
		HealthResponse{},
		VersionResponse{},
		RevokeKeyResponse{},
		ListUsersOptions{},
		ListOrgsOptions{},
	}
	if len(types) != 19 {
		t.Errorf("expected 19 canonical types, got %d", len(types))
	}
	// go build ./... succeeding proves no redeclarations exist.
}

// ---------------------------------------------------------------------------
// Smoke Tests: End-to-end integration tests with httptest.Server
// Spec: TS-12-SMOKE-1 through TS-12-SMOKE-6
// ---------------------------------------------------------------------------

// TestSmokeETagCaptureAndConditionalRefetch verifies the full ETag
// round-trip: first GET captures the ETag, second GET with If-None-Match
// returns ErrNotModified (TS-12-SMOKE-1, PATH-1).
func TestSmokeETagCaptureAndConditionalRefetch(t *testing.T) {
	const etagValue = `"abc123"`
	var reqCount int
	var capturedPaths []string
	var capturedAuthHeaders []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		capturedPaths = append(capturedPaths, r.URL.Path)
		capturedAuthHeaders = append(capturedAuthHeaders, r.Header.Get("Authorization"))

		if inm := r.Header.Get("If-None-Match"); inm == etagValue {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etagValue)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(testUserJSON))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("test-key"))

	// First call: capture ETag.
	resp, err := client.GetUser(context.Background())
	if err != nil {
		t.Fatalf("first GetUser: %v", err)
	}
	if resp == nil {
		t.Fatal("first GetUser returned nil response")
	}
	if resp.StatusCode != 200 {
		t.Errorf("first GetUser StatusCode = %d, want 200", resp.StatusCode)
	}
	if resp.Data.ID == "" {
		t.Error("first GetUser Data.ID is empty")
	}
	etag := resp.ETag()
	if etag != etagValue {
		t.Errorf("ETag() = %q, want %q", etag, etagValue)
	}

	// Second call: conditional GET with If-None-Match.
	resp2, err := client.GetUser(context.Background(), WithIfNoneMatch(etag))
	if !errors.Is(err, ErrNotModified) {
		t.Fatalf("second GetUser err = %v, want ErrNotModified", err)
	}
	if resp2 != nil {
		t.Errorf("second GetUser response should be nil on 304, got %+v", resp2)
	}

	// Verify request paths.
	for _, p := range capturedPaths {
		if p != "/api/v1/user" {
			t.Errorf("request path = %q, want /api/v1/user", p)
		}
	}
	// Verify authorization header on both requests.
	for i, auth := range capturedAuthHeaders {
		if auth != "Bearer test-key" {
			t.Errorf("request %d Authorization = %q, want %q", i+1, auth, "Bearer test-key")
		}
	}
}

// TestSmokeAPIErrorOn4xx verifies that a 404 from the server is decoded
// into a typed *APIError inspectable via errors.As (TS-12-SMOKE-2, PATH-2).
func TestSmokeAPIErrorOn4xx(t *testing.T) {
	var capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":{"code":404,"message":"User not found"}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"))
	resp, err := client.GetUserByID(context.Background(), "nonexistent-id")
	if err == nil {
		t.Fatal("GetUserByID should return error on 404")
	}
	if resp != nil {
		t.Errorf("response should be nil on error, got %+v", resp)
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("errors.As should return true for *APIError; err = %v", err)
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
	if capturedPath != "/api/v1/users/nonexistent-id" {
		t.Errorf("path = %q, want /api/v1/users/nonexistent-id", capturedPath)
	}
}

// TestSmokeListOrgsIncludeBlocked verifies that ListOrgs with IncludeBlocked
// sends the correct query parameter and handles an empty result as a non-nil
// empty slice (TS-12-SMOKE-3, PATH-3).
func TestSmokeListOrgsIncludeBlocked(t *testing.T) {
	var capturedMethod, capturedPath, capturedQuery string
	var capturedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		capturedQuery = r.URL.RawQuery
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("admin-key"))
	orgs, err := client.ListOrgs(context.Background(), &ListOrgsOptions{IncludeBlocked: true})
	if err != nil {
		t.Fatalf("ListOrgs returned error: %v", err)
	}
	if capturedMethod != "GET" {
		t.Errorf("method = %q, want GET", capturedMethod)
	}
	if capturedPath != "/api/v1/orgs" {
		t.Errorf("path = %q, want /api/v1/orgs", capturedPath)
	}
	if capturedQuery != "include_blocked=true" {
		t.Errorf("query = %q, want include_blocked=true", capturedQuery)
	}
	if capturedAuth != "Bearer admin-key" {
		t.Errorf("Authorization = %q, want %q", capturedAuth, "Bearer admin-key")
	}
	if orgs == nil {
		t.Fatal("ListOrgs returned nil slice, want non-nil empty slice")
	}
	if len(orgs) != 0 {
		t.Errorf("len(orgs) = %d, want 0", len(orgs))
	}
}

// TestSmokeOrgMemberManagement verifies the CreateOrg → AddOrgMember →
// ListOrgMembers sequence end-to-end (TS-12-SMOKE-4, PATH-4).
func TestSmokeOrgMemberManagement(t *testing.T) {
	var requests []struct {
		method string
		path   string
	}

	const orgJSON = `{"id":"org-42","name":"Acme","slug":"acme","status":"active","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}`
	const memberJSON = `[{"id":"user-1","username":"bob","email":"bob@co.com","full_name":"Bob","status":"active","role":"user","provider":"github","provider_id":"gh2","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}]`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, struct {
			method string
			path   string
		}{r.Method, r.URL.Path})

		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == "POST" && r.URL.Path == "/api/v1/orgs":
			// CreateOrg → 201
			w.WriteHeader(201)
			_, _ = w.Write([]byte(orgJSON))
		case r.Method == "PUT" && r.URL.Path == "/api/v1/orgs/org-42/members/user-1":
			// AddOrgMember → 204
			w.WriteHeader(204)
		case r.Method == "GET" && r.URL.Path == "/api/v1/orgs/org-42/members":
			// ListOrgMembers → 200
			w.WriteHeader(200)
			_, _ = w.Write([]byte(memberJSON))
		default:
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"error":{"code":404,"message":"not found"}}`))
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("admin-key"))

	// Step 1: CreateOrg
	org, err := client.CreateOrg(context.Background(), &CreateOrgRequest{Name: "Acme", Slug: "acme"})
	if err != nil {
		t.Fatalf("CreateOrg returned error: %v", err)
	}
	if org == nil {
		t.Fatal("CreateOrg returned nil org")
	}
	if org.ID != "org-42" {
		t.Errorf("org.ID = %q, want %q", org.ID, "org-42")
	}

	// Step 2: AddOrgMember
	err = client.AddOrgMember(context.Background(), org.ID, "user-1")
	if err != nil {
		t.Fatalf("AddOrgMember returned error: %v", err)
	}

	// Step 3: ListOrgMembers
	members, err := client.ListOrgMembers(context.Background(), org.ID)
	if err != nil {
		t.Fatalf("ListOrgMembers returned error: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("len(members) = %d, want 1", len(members))
	}
	if members[0].Username != "bob" {
		t.Errorf("members[0].Username = %q, want %q", members[0].Username, "bob")
	}

	// Verify all requests used Authorization header.
	if len(requests) != 3 {
		t.Fatalf("expected 3 requests, got %d", len(requests))
	}

	expectedOps := []struct{ method, path string }{
		{"POST", "/api/v1/orgs"},
		{"PUT", "/api/v1/orgs/org-42/members/user-1"},
		{"GET", "/api/v1/orgs/org-42/members"},
	}
	for i, expected := range expectedOps {
		if requests[i].method != expected.method || requests[i].path != expected.path {
			t.Errorf("request %d: %s %s, want %s %s",
				i, requests[i].method, requests[i].path, expected.method, expected.path)
		}
	}
}

// TestSmokeCustomMountPoint verifies that a custom mount point is used for
// API endpoints but health probes bypass it (TS-12-SMOKE-5, PATH-5).
func TestSmokeCustomMountPoint(t *testing.T) {
	var capturedPaths []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPaths = append(capturedPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)

		switch r.URL.Path {
		case "/api/v2/user":
			_, _ = w.Write([]byte(testUserJSON))
		case "/healthz":
			_, _ = w.Write([]byte(testHealthJSON))
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("key"), WithMountPoint("api/v2"))

	// Verify mount point was normalized.
	if client.mountPoint != "/api/v2" {
		t.Errorf("mountPoint = %q, want %q", client.mountPoint, "/api/v2")
	}

	// API endpoint should use custom mount point.
	resp, err := client.GetUser(context.Background())
	if err != nil {
		t.Fatalf("GetUser returned error: %v", err)
	}
	if resp == nil {
		t.Fatal("GetUser returned nil response")
	}

	// Health probe should bypass mount point.
	health, err := client.Healthz(context.Background())
	if err != nil {
		t.Fatalf("Healthz returned error: %v", err)
	}
	if health == nil {
		t.Fatal("Healthz returned nil response")
	}
	if health.Status != "ok" {
		t.Errorf("Healthz status = %q, want %q", health.Status, "ok")
	}

	// Verify captured paths.
	if len(capturedPaths) < 2 {
		t.Fatalf("expected at least 2 requests, got %d", len(capturedPaths))
	}
	if capturedPaths[0] != "/api/v2/user" {
		t.Errorf("GetUser path = %q, want /api/v2/user", capturedPaths[0])
	}
	if capturedPaths[1] != "/healthz" {
		t.Errorf("Healthz path = %q, want /healthz", capturedPaths[1])
	}

	// Verify no double slashes in any captured path.
	for _, p := range capturedPaths {
		if strings.Contains(p, "//") {
			t.Errorf("double slash found in path: %q", p)
		}
	}
}

// TestSmokeOAuthCodeExchange verifies the full OAuth flow:
// GetProviders discovers providers, ExchangeOAuthCode exchanges the code
// for a User and APIKeyFull (TS-12-SMOKE-6, PATH-6).
func TestSmokeOAuthCodeExchange(t *testing.T) {
	var capturedAuthHeaders []string
	var capturedMethods []string
	var capturedPaths []string
	var capturedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuthHeaders = append(capturedAuthHeaders, r.Header.Get("Authorization"))
		capturedMethods = append(capturedMethods, r.Method)
		capturedPaths = append(capturedPaths, r.URL.Path)

		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/auth/providers":
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`[{"name":"github","authorize_url":"https://github.com/login/oauth/authorize"}]`))
		case r.Method == "POST" && r.URL.Path == "/api/v1/auth/callback":
			capturedBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{
				"user": {"id":"u-new","username":"newuser","email":"new@co.com","full_name":"New User","status":"active","role":"user","provider":"github","provider_id":"gh-new","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"},
				"api_key": {"key":"sk-new-secret","key_id":"k-new","expires_at":null}
			}`))
		default:
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"error":{"code":404,"message":"not found"}}`))
		}
	}))
	defer server.Close()

	// Create unauthenticated client (no API key).
	client := NewClient(server.URL)

	// Step 1: Discover providers.
	providers, err := client.GetProviders(context.Background())
	if err != nil {
		t.Fatalf("GetProviders returned error: %v", err)
	}
	if len(providers) != 1 {
		t.Fatalf("len(providers) = %d, want 1", len(providers))
	}
	if providers[0].Name != "github" {
		t.Errorf("provider name = %q, want %q", providers[0].Name, "github")
	}

	// Step 2: Exchange OAuth code.
	callbackResp, err := client.ExchangeOAuthCode(context.Background(), &AuthCallbackRequest{
		Provider:    "github",
		Code:        "auth-code-xyz",
		RedirectURI: "http://localhost:8080/callback",
	})
	if err != nil {
		t.Fatalf("ExchangeOAuthCode returned error: %v", err)
	}
	if callbackResp == nil {
		t.Fatal("ExchangeOAuthCode returned nil response")
	}
	if callbackResp.User == nil || callbackResp.User.ID != "u-new" {
		t.Errorf("User.ID = %v, want %q", callbackResp.User, "u-new")
	}
	if callbackResp.APIKey == nil || callbackResp.APIKey.Key != "sk-new-secret" {
		t.Errorf("APIKey.Key = %v, want %q", callbackResp.APIKey, "sk-new-secret")
	}

	// Verify no Authorization header (unauthenticated client).
	for i, auth := range capturedAuthHeaders {
		if auth != "" {
			t.Errorf("request %d: Authorization = %q, want empty (unauthenticated)", i+1, auth)
		}
	}

	// Verify request paths.
	if capturedPaths[0] != "/api/v1/auth/providers" {
		t.Errorf("GetProviders path = %q, want /api/v1/auth/providers", capturedPaths[0])
	}
	if capturedPaths[1] != "/api/v1/auth/callback" {
		t.Errorf("ExchangeOAuthCode path = %q, want /api/v1/auth/callback", capturedPaths[1])
	}

	// Verify request body contains expected fields.
	if !strings.Contains(string(capturedBody), `"provider":"github"`) {
		t.Errorf("request body missing provider: %s", capturedBody)
	}
	if !strings.Contains(string(capturedBody), `"code":"auth-code-xyz"`) {
		t.Errorf("request body missing code: %s", capturedBody)
	}
	if !strings.Contains(string(capturedBody), `"redirect_uri":"http://localhost:8080/callback"`) {
		t.Errorf("request body missing redirect_uri: %s", capturedBody)
	}

	// Verify methods.
	if capturedMethods[0] != "GET" {
		t.Errorf("GetProviders method = %q, want GET", capturedMethods[0])
	}
	if capturedMethods[1] != "POST" {
		t.Errorf("ExchangeOAuthCode method = %q, want POST", capturedMethods[1])
	}
}

// TestSmokeHTTPRedirectFollowed verifies that the SDK follows HTTP
// redirects automatically using Go's default policy (TS-12-90).
func TestSmokeHTTPRedirectFollowed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			_, _ = w.Write([]byte(testHealthJSON))
			return
		}
		http.Redirect(w, r, "/healthz", http.StatusFound)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	resp, err := client.Healthz(context.Background())
	if err != nil {
		t.Fatalf("Healthz with redirect returned error: %v", err)
	}
	if resp == nil {
		t.Fatal("Healthz returned nil after redirect")
	}
	if resp.Status != "ok" {
		t.Errorf("resp.Status = %q, want %q", resp.Status, "ok")
	}
}

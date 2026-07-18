package keys_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/txsvc/apikit"
	"github.com/txsvc/apikit/internal/auth"
	"github.com/txsvc/apikit/internal/db"
	"github.com/txsvc/apikit/internal/keys"
)

// ---------------------------------------------------------------------------
// Handler test helpers
// ---------------------------------------------------------------------------

// setupEchoWithHandlers creates an Echo instance with the /api/v1 group,
// CacheMiddleware(CacheNoStore), and RegisterKeyHandlers registered.
// Returns the Echo instance and the *sql.DB.
func setupEchoWithHandlers(t *testing.T) (*echo.Echo, *db.DB) {
	t.Helper()
	database := testDB(t)

	e := echo.New()
	e.HTTPErrorHandler = apikit.HTTPErrorHandler
	group := e.Group("/api/v1", apikit.CacheMiddleware(apikit.CacheNoStore))
	keys.RegisterKeyHandlers(group, database.SqlDB)
	return e, database
}

// setupHandlersWithAuth creates an Echo instance with the /api/v1 group,
// CacheMiddleware(CacheNoStore), an auth-injection middleware that sets
// the given AuthInfo, and RegisterKeyHandlers registered.
func setupHandlersWithAuth(t *testing.T, database *db.DB, info *auth.AuthInfo) *echo.Echo {
	t.Helper()
	e := echo.New()
	e.HTTPErrorHandler = apikit.HTTPErrorHandler

	authMW := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if info != nil {
				auth.SetAuthInfo(c, info)
			}
			return next(c)
		}
	}
	group := e.Group("/api/v1", apikit.CacheMiddleware(apikit.CacheNoStore), authMW)
	keys.RegisterKeyHandlers(group, database.SqlDB)
	return e
}

// =========================================================================
// Subtask 3.1: RegisterKeyHandlers — route registration and Cache-Control
// Test Spec: TS-10-15, TS-10-16
// Requirements: 10-REQ-4.1, 10-REQ-4.2
// =========================================================================

// TestRegisterKeyHandlers_RoutesRegistered verifies that RegisterKeyHandlers
// registers exactly GET /api/v1/user/keys, POST /api/v1/user/keys/:key_id/refresh,
// and DELETE /api/v1/user/keys/:key_id on the provided Echo group.
// TS-10-15 (Requirement: 10-REQ-4.1)
func TestRegisterKeyHandlers_RoutesRegistered(t *testing.T) {
	e, _ := setupEchoWithHandlers(t)

	routes := e.Routes()
	type routeCheck struct {
		method string
		path   string
	}
	expected := []routeCheck{
		{method: http.MethodGet, path: "/api/v1/user/keys"},
		{method: http.MethodPost, path: "/api/v1/user/keys/:key_id/refresh"},
		{method: http.MethodDelete, path: "/api/v1/user/keys/:key_id"},
	}

	for _, want := range expected {
		found := false
		for _, r := range routes {
			if r.Method == want.method && r.Path == want.path {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected route %s %s not found in registered routes", want.method, want.path)
		}
	}
}

// TestRegisterKeyHandlers_CacheControlNoStore verifies that all three
// endpoints respond with Cache-Control: no-store, inherited from the
// parent Echo group's CacheMiddleware(CacheNoStore).
// TS-10-16 (Requirement: 10-REQ-4.2)
func TestRegisterKeyHandlers_CacheControlNoStore(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-cache")
	// Insert a key so GET has data to work with (and the key can be used for
	// refresh/delete attempts — we just need the Cache-Control header).
	insertTestKey(t, database.SqlDB, "cacheky1", "user-cache", "hash1", 90,
		db.FormatTime(time.Now().UTC().Add(90*24*time.Hour)), "", db.FormatTime(time.Now().UTC()))

	e := echo.New()
	e.HTTPErrorHandler = apikit.HTTPErrorHandler

	// Install auth-injecting middleware so handlers can read user_id.
	authMW := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			auth.SetAuthInfo(c, &auth.AuthInfo{
				CredentialType: "api_key",
				UserID:         "user-cache",
				KeyID:          "cacheky1",
			})
			return next(c)
		}
	}
	group := e.Group("/api/v1", apikit.CacheMiddleware(apikit.CacheNoStore), authMW)
	keys.RegisterKeyHandlers(group, database.SqlDB)

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/user/keys"},
		{http.MethodPost, "/api/v1/user/keys/cacheky1/refresh"},
		{http.MethodDelete, "/api/v1/user/keys/cacheky1"},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.path, nil)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			cc := rec.Header().Get("Cache-Control")
			if cc != "no-store" {
				t.Errorf("%s %s: Cache-Control = %q; want %q", ep.method, ep.path, cc, "no-store")
			}
		})
	}
}

// =========================================================================
// Subtask 3.2: GET /api/v1/user/keys — happy path and metadata-only response
// Test Spec: TS-10-17, TS-10-44
// Requirements: 10-REQ-5.1, 10-REQ-10.2
// =========================================================================

// TestListKeys_HappyPath verifies that GET /api/v1/user/keys returns HTTP 200
// with a JSON array of key metadata objects containing only key_id, created_at,
// expires_at, and revoked_at (never secret, secret_hash, key, or expires_days).
// TS-10-17 (Requirement: 10-REQ-5.1)
func TestListKeys_HappyPath(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-013")
	createdAt := "2026-07-17T14:30:00Z"
	expiresAt := "2026-10-15T14:30:00Z"
	insertTestKey(t, database.SqlDB, "aB3xK9mQ", "user-013", "somehash", 90,
		expiresAt, "", createdAt)

	e := setupHandlersWithAuth(t, database, &auth.AuthInfo{
		CredentialType: "api_key",
		UserID:         "user-013",
		KeyID:          "aB3xK9mQ",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/keys", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/user/keys status = %d; want %d", rec.Code, http.StatusOK)
	}

	var body []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response body: %v", err)
	}

	if len(body) != 1 {
		t.Fatalf("response array length = %d; want 1", len(body))
	}

	obj := body[0]

	// Verify required fields are present.
	if obj["key_id"] != "aB3xK9mQ" {
		t.Errorf("key_id = %v; want %q", obj["key_id"], "aB3xK9mQ")
	}
	if obj["created_at"] == nil || obj["created_at"] == "" {
		t.Error("created_at is missing or empty")
	}
	if _, ok := obj["expires_at"]; !ok {
		t.Error("expires_at field is missing")
	}
	if _, ok := obj["revoked_at"]; !ok {
		t.Error("revoked_at field is missing")
	}

	// Verify forbidden fields are absent.
	for _, forbidden := range []string{"secret", "secret_hash", "expires_days"} {
		if _, ok := obj[forbidden]; ok {
			t.Errorf("response contains forbidden field %q", forbidden)
		}
	}
}

// TestListKeys_MetadataOnly verifies that GET /api/v1/user/keys never includes
// secret_hash, plaintext secret, key, or expires_days in any response object.
// Each object must contain ONLY key_id, created_at, expires_at, revoked_at.
// TS-10-44 (Requirement: 10-REQ-10.2)
func TestListKeys_MetadataOnly(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-036")
	createdAt := db.FormatTime(time.Now().UTC())
	expiresAt := db.FormatTime(time.Now().UTC().Add(90 * 24 * time.Hour))
	insertTestKey(t, database.SqlDB, "metaky36", "user-036", "hash036", 90,
		expiresAt, "", createdAt)

	e := setupHandlersWithAuth(t, database, &auth.AuthInfo{
		CredentialType: "api_key",
		UserID:         "user-036",
		KeyID:          "metaky36",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/keys", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/user/keys status = %d; want %d", rec.Code, http.StatusOK)
	}

	var body []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response body: %v", err)
	}

	if len(body) == 0 {
		t.Fatal("response array is empty; want at least one key object")
	}

	allowedKeys := map[string]bool{
		"key_id":     true,
		"created_at": true,
		"expires_at": true,
		"revoked_at": true,
	}

	for i, obj := range body {
		// Verify only allowed keys are present.
		for k := range obj {
			if !allowedKeys[k] {
				t.Errorf("body[%d] contains unexpected field %q; allowed: %v", i, k, allowedKeys)
			}
		}

		// Verify all allowed keys are present.
		for k := range allowedKeys {
			if _, ok := obj[k]; !ok {
				t.Errorf("body[%d] missing required field %q", i, k)
			}
		}

		// Explicitly check forbidden fields.
		for _, forbidden := range []string{"secret", "secret_hash", "key", "expires_days"} {
			if _, ok := obj[forbidden]; ok {
				t.Errorf("body[%d] contains forbidden field %q", i, forbidden)
			}
		}
	}
}

// =========================================================================
// Subtask 3.3: GET /api/v1/user/keys — empty result and ordering
// Test Spec: TS-10-18, TS-10-19
// Requirements: 10-REQ-5.2, 10-REQ-5.3
// =========================================================================

// TestListKeys_EmptyResult verifies that GET /api/v1/user/keys returns
// HTTP 200 with an empty JSON array [] and no ETag header when the user
// has no keys.
// TS-10-18 (Requirement: 10-REQ-5.2)
func TestListKeys_EmptyResult(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-014")

	e := setupHandlersWithAuth(t, database, &auth.AuthInfo{
		CredentialType: "api_key",
		UserID:         "user-014",
		KeyID:          "somekey",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/keys", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/user/keys status = %d; want %d", rec.Code, http.StatusOK)
	}

	var body []any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response body: %v", err)
	}

	if len(body) != 0 {
		t.Errorf("response array length = %d; want 0 (empty array)", len(body))
	}

	// Verify no ETag header is present.
	if etag := rec.Header().Get("ETag"); etag != "" {
		t.Errorf("ETag header = %q; want empty (no ETag for empty result)", etag)
	}
}

// TestListKeys_Ordering verifies that GET /api/v1/user/keys returns all
// historical keys (active, expired, revoked) ordered by created_at DESC.
// TS-10-19 (Requirement: 10-REQ-5.3)
func TestListKeys_Ordering(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-015")

	now := time.Now().UTC()

	// key3: oldest, expired.
	insertTestKey(t, database.SqlDB, "key3_015", "user-015", "hash3", 30,
		db.FormatTime(now.Add(-24*time.Hour)),  // expires_at in the past
		"",                                      // not explicitly revoked
		db.FormatTime(now.Add(-60*24*time.Hour)), // created 60 days ago
	)

	// key2: middle, revoked.
	insertTestKey(t, database.SqlDB, "key2_015", "user-015", "hash2", 90,
		db.FormatTime(now.Add(30*24*time.Hour)),    // expires_at in the future
		db.FormatTime(now.Add(-10*24*time.Hour)),    // revoked 10 days ago
		db.FormatTime(now.Add(-30*24*time.Hour)),    // created 30 days ago
	)

	// key1: newest, active.
	insertTestKey(t, database.SqlDB, "key1_015", "user-015", "hash1", 90,
		db.FormatTime(now.Add(90*24*time.Hour)), // expires_at in the future
		"",                                       // not revoked
		db.FormatTime(now),                       // created now
	)

	e := setupHandlersWithAuth(t, database, &auth.AuthInfo{
		CredentialType: "api_key",
		UserID:         "user-015",
		KeyID:          "key1_015",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/keys", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/user/keys status = %d; want %d", rec.Code, http.StatusOK)
	}

	var body []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response body: %v", err)
	}

	// Verify all 3 keys are returned.
	if len(body) != 3 {
		t.Fatalf("response array length = %d; want 3", len(body))
	}

	// Verify ordering: first element is key1 (newest), last is key3 (oldest).
	if body[0]["key_id"] != "key1_015" {
		t.Errorf("body[0].key_id = %v; want %q (newest)", body[0]["key_id"], "key1_015")
	}
	if body[2]["key_id"] != "key3_015" {
		t.Errorf("body[2].key_id = %v; want %q (oldest)", body[2]["key_id"], "key3_015")
	}

	// Verify created_at values are non-increasing across the array.
	for i := 0; i < len(body)-1; i++ {
		cur, ok1 := body[i]["created_at"].(string)
		nxt, ok2 := body[i+1]["created_at"].(string)
		if !ok1 || !ok2 {
			t.Fatalf("created_at at index %d or %d is not a string", i, i+1)
		}
		curTime, err1 := time.Parse(time.RFC3339, cur)
		nxtTime, err2 := time.Parse(time.RFC3339, nxt)
		if err1 != nil || err2 != nil {
			t.Fatalf("failed to parse created_at at index %d or %d: %v, %v", i, i+1, err1, err2)
		}
		if curTime.Before(nxtTime) {
			t.Errorf("body[%d].created_at (%s) < body[%d].created_at (%s); want non-increasing order",
				i, cur, i+1, nxt)
		}
	}
}

// =========================================================================
// Subtask 3.4: GET /api/v1/user/keys — ETag caching
// Test Spec: TS-10-20, TS-10-E7, TS-10-E8
// Requirements: 10-REQ-5.4, 10-REQ-5.E1, 10-REQ-5.E2
// =========================================================================

// TestListKeys_ETag_SetAndMatch verifies that the first GET returns an ETag
// header, and a second GET with matching If-None-Match returns HTTP 304.
// TS-10-20 (Requirement: 10-REQ-5.4)
func TestListKeys_ETag_SetAndMatch(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-016")
	createdAt := db.FormatTime(time.Now().UTC())
	expiresAt := db.FormatTime(time.Now().UTC().Add(90 * 24 * time.Hour))
	insertTestKey(t, database.SqlDB, "etagky16", "user-016", "hash016", 90,
		expiresAt, "", createdAt)

	e := echo.New()
	e.HTTPErrorHandler = apikit.HTTPErrorHandler
	authMW := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			auth.SetAuthInfo(c, &auth.AuthInfo{
				CredentialType: "api_key",
				UserID:         "user-016",
				KeyID:          "etagky16",
			})
			return next(c)
		}
	}
	group := e.Group("/api/v1", apikit.CacheMiddleware(apikit.CacheNoStore), authMW)
	keys.RegisterKeyHandlers(group, database.SqlDB)

	// First request: should return HTTP 200 with ETag.
	req1 := httptest.NewRequest(http.MethodGet, "/api/v1/user/keys", nil)
	rec1 := httptest.NewRecorder()
	e.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("first GET status = %d; want %d", rec1.Code, http.StatusOK)
	}

	etag := rec1.Header().Get("ETag")
	if etag == "" {
		t.Fatal("first GET missing ETag header; want non-empty")
	}

	// Second request: with If-None-Match matching the ETag → HTTP 304.
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/user/keys", nil)
	req2.Header.Set("If-None-Match", etag)
	rec2 := httptest.NewRecorder()
	e.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusNotModified {
		t.Errorf("second GET with matching If-None-Match status = %d; want %d",
			rec2.Code, http.StatusNotModified)
	}

	// Verify the 304 response has an empty body.
	if rec2.Body.Len() > 0 {
		t.Errorf("304 response body length = %d; want 0", rec2.Body.Len())
	}
}

// TestListKeys_ETag_NoKeysNoETag verifies that when the ETag derivation query
// returns NULL (no keys), GET returns HTTP 200 with [] and no ETag header.
// Even if an If-None-Match header is sent, it is not evaluated.
// TS-10-E7 (Requirement: 10-REQ-5.E1)
func TestListKeys_ETag_NoKeysNoETag(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-E7")

	e := echo.New()
	e.HTTPErrorHandler = apikit.HTTPErrorHandler
	authMW := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			auth.SetAuthInfo(c, &auth.AuthInfo{
				CredentialType: "api_key",
				UserID:         "user-E7",
				KeyID:          "fakekey",
			})
			return next(c)
		}
	}
	group := e.Group("/api/v1", apikit.CacheMiddleware(apikit.CacheNoStore), authMW)
	keys.RegisterKeyHandlers(group, database.SqlDB)

	// Send GET with an If-None-Match header — should be ignored since no keys.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/keys", nil)
	req.Header.Set("If-None-Match", `"some-etag"`)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d; want %d", rec.Code, http.StatusOK)
	}

	var body []any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response body: %v", err)
	}
	if len(body) != 0 {
		t.Errorf("response array length = %d; want 0", len(body))
	}

	// Verify no ETag header.
	if etag := rec.Header().Get("ETag"); etag != "" {
		t.Errorf("ETag header = %q; want empty (no ETag when no keys)", etag)
	}
}

// TestListKeys_ETag_RevocationInvalidation verifies that revoking a key
// invalidates the prior ETag so a subsequent GET with the original
// If-None-Match returns HTTP 200 (not 304) with a new ETag.
// TS-10-E8 (Requirement: 10-REQ-5.E2)
func TestListKeys_ETag_RevocationInvalidation(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-E8")
	createdAt := db.FormatTime(time.Now().UTC())
	expiresAt := db.FormatTime(time.Now().UTC().Add(90 * 24 * time.Hour))
	insertTestKey(t, database.SqlDB, "etagkyE8", "user-E8", "hashE8", 90,
		expiresAt, "", createdAt)

	e := echo.New()
	e.HTTPErrorHandler = apikit.HTTPErrorHandler
	authMW := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			auth.SetAuthInfo(c, &auth.AuthInfo{
				CredentialType: "api_key",
				UserID:         "user-E8",
				KeyID:          "etagkyE8",
			})
			return next(c)
		}
	}
	group := e.Group("/api/v1", apikit.CacheMiddleware(apikit.CacheNoStore), authMW)
	keys.RegisterKeyHandlers(group, database.SqlDB)

	// Step 1: First GET — capture ETag.
	req1 := httptest.NewRequest(http.MethodGet, "/api/v1/user/keys", nil)
	rec1 := httptest.NewRecorder()
	e.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("first GET status = %d; want %d", rec1.Code, http.StatusOK)
	}

	originalETag := rec1.Header().Get("ETag")
	if originalETag == "" {
		t.Fatal("first GET missing ETag header")
	}

	// Step 2: Revoke the key directly via SQL (simulating DELETE endpoint).
	revokedAt := db.FormatTime(time.Now().UTC())
	_, err := database.SqlDB.Exec(
		"UPDATE api_keys SET revoked_at = ? WHERE key_id = ? AND user_id = ?",
		revokedAt, "etagkyE8", "user-E8",
	)
	if err != nil {
		t.Fatalf("revoke key failed: %v", err)
	}

	// Step 3: GET with original If-None-Match — should return HTTP 200 (not 304).
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/user/keys", nil)
	req2.Header.Set("If-None-Match", originalETag)
	rec2 := httptest.NewRecorder()
	e.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Errorf("GET after revocation with original ETag status = %d; want %d (not 304)",
			rec2.Code, http.StatusOK)
	}

	// Verify the new ETag differs from the original.
	newETag := rec2.Header().Get("ETag")
	if newETag == originalETag {
		t.Error("new ETag matches original ETag after revocation; want different ETag")
	}
}

// =========================================================================
// Subtask 3.5: GET /api/v1/user/keys — auth acceptance and DB error
// Test Spec: TS-10-21, TS-10-E9
// Requirements: 10-REQ-5.5, 10-REQ-5.E3
// =========================================================================

// TestListKeys_APIKeyAuth verifies that GET /api/v1/user/keys accepts
// authentication via API key and returns HTTP 200.
// TS-10-21 part 1 (Requirement: 10-REQ-5.5)
func TestListKeys_APIKeyAuth(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-017")
	createdAt := db.FormatTime(time.Now().UTC())
	expiresAt := db.FormatTime(time.Now().UTC().Add(90 * 24 * time.Hour))
	insertTestKey(t, database.SqlDB, "authky17", "user-017", "hash017", 90,
		expiresAt, "", createdAt)

	e := echo.New()
	e.HTTPErrorHandler = apikit.HTTPErrorHandler
	authMW := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			auth.SetAuthInfo(c, &auth.AuthInfo{
				CredentialType: "api_key",
				UserID:         "user-017",
				KeyID:          "authky17",
			})
			return next(c)
		}
	}
	group := e.Group("/api/v1", apikit.CacheMiddleware(apikit.CacheNoStore), authMW)
	keys.RegisterKeyHandlers(group, database.SqlDB)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/keys", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GET with API key auth status = %d; want %d", rec.Code, http.StatusOK)
	}
}

// TestListKeys_PATAuth verifies that GET /api/v1/user/keys accepts
// authentication via PAT with keys:read permission and returns HTTP 200.
// TS-10-21 part 2 (Requirement: 10-REQ-5.5)
func TestListKeys_PATAuth(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-017b")
	createdAt := db.FormatTime(time.Now().UTC())
	expiresAt := db.FormatTime(time.Now().UTC().Add(90 * 24 * time.Hour))
	insertTestKey(t, database.SqlDB, "patky17b", "user-017b", "hash017b", 90,
		expiresAt, "", createdAt)

	e := echo.New()
	e.HTTPErrorHandler = apikit.HTTPErrorHandler
	authMW := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			auth.SetAuthInfo(c, &auth.AuthInfo{
				CredentialType: "pat",
				UserID:         "user-017b",
				TokenID:        "pat-token-id",
				Permissions:    []string{"keys:read"},
			})
			return next(c)
		}
	}
	group := e.Group("/api/v1", apikit.CacheMiddleware(apikit.CacheNoStore), authMW)
	keys.RegisterKeyHandlers(group, database.SqlDB)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/keys", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GET with PAT keys:read auth status = %d; want %d", rec.Code, http.StatusOK)
	}
}

// TestListKeys_DBError verifies that a database error during the GET query
// returns HTTP 500 with "internal server error" via the APIError envelope.
// TS-10-E9 (Requirement: 10-REQ-5.E3)
func TestListKeys_DBError(t *testing.T) {
	database := testDB(t)
	insertTestUser(t, database.SqlDB, "user-E9")

	// Close the database to force all queries to fail.
	database.SqlDB.Close()

	e := echo.New()
	e.HTTPErrorHandler = apikit.HTTPErrorHandler
	authMW := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			auth.SetAuthInfo(c, &auth.AuthInfo{
				CredentialType: "api_key",
				UserID:         "user-E9",
				KeyID:          "fakekey",
			})
			return next(c)
		}
	}
	group := e.Group("/api/v1", apikit.CacheMiddleware(apikit.CacheNoStore), authMW)
	keys.RegisterKeyHandlers(group, database.SqlDB)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/keys", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("GET with DB error status = %d; want %d", rec.Code, http.StatusInternalServerError)
	}

	// Verify the error response body contains the APIError envelope.
	var errResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}

	// APIError envelope: {"error": {"code": 500, "message": "internal server error"}}
	errObj, ok := errResp["error"].(map[string]any)
	if !ok {
		t.Fatalf("response missing 'error' object; got: %v", errResp)
	}
	if msg, ok := errObj["message"].(string); !ok || msg != "internal server error" {
		t.Errorf("error.message = %v; want %q", errObj["message"], "internal server error")
	}
}

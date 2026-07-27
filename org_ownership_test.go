package apikit_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/txsvc/apikit"
)

// ========================================================================
// Spec 04 Task 2.2: Organization struct OwnerID field tests
// Test Spec: TS-04-13
// Requirements: 04-REQ-4.2
// ========================================================================

// TestPersonalOrg_OrganizationStructOwnerIDField verifies that the
// apikit.Organization struct has an OwnerID field of type *string with
// JSON tag "owner_id,omitempty". The field must serialize into JSON when
// set and be omitted from JSON when nil.
//
// Test Spec: TS-04-13
// Requirement: 04-REQ-4.2
func TestPersonalOrg_OrganizationStructOwnerIDField(t *testing.T) {
	// Use reflection to check the Organization struct has an OwnerID field.
	orgType := reflect.TypeFor[apikit.Organization]()

	field, ok := orgType.FieldByName("OwnerID")
	if !ok {
		t.Fatal("Organization struct does not have an OwnerID field; expected *string with JSON tag 'owner_id,omitempty'")
	}

	// Verify the field type is *string.
	expectedType := reflect.TypeFor[*string]()
	if field.Type != expectedType {
		t.Errorf("OwnerID field type = %v; want %v (*string)", field.Type, expectedType)
	}

	// Verify the JSON struct tag is "owner_id,omitempty".
	jsonTag := field.Tag.Get("json")
	if jsonTag != "owner_id,omitempty" {
		t.Errorf("OwnerID JSON tag = %q; want %q", jsonTag, "owner_id,omitempty")
	}
}

// TestPersonalOrg_OrganizationJSON_OwnerIDPresent verifies that when OwnerID
// is set (non-nil), marshalling the Organization struct to JSON includes
// the "owner_id" field with the expected value.
//
// Test Spec: TS-04-13 (JSON marshal with OwnerID set)
// Requirement: 04-REQ-4.2
func TestPersonalOrg_OrganizationJSON_OwnerIDPresent(t *testing.T) {
	// Use reflection to set OwnerID since it may not exist yet.
	orgType := reflect.TypeFor[apikit.Organization]()
	if _, ok := orgType.FieldByName("OwnerID"); !ok {
		t.Fatal("Organization struct does not have OwnerID field; cannot test JSON serialization")
	}

	// Create an Organization instance and set OwnerID via reflection.
	org := apikit.Organization{
		ID:     "test-id",
		Name:   "Test Org",
		Slug:   "test-org",
		Status: "active",
	}

	orgVal := reflect.ValueOf(&org).Elem()
	ownerField := orgVal.FieldByName("OwnerID")
	if !ownerField.IsValid() || !ownerField.CanSet() {
		t.Fatal("cannot set OwnerID field via reflection")
	}

	ownerID := "user-123"
	ownerField.Set(reflect.ValueOf(&ownerID))

	jsonBytes, err := json.Marshal(org)
	if err != nil {
		t.Fatalf("json.Marshal error = %v; want nil", err)
	}

	// Parse the JSON and check for owner_id.
	var raw map[string]any
	if err := json.Unmarshal(jsonBytes, &raw); err != nil {
		t.Fatalf("json.Unmarshal error = %v; want nil", err)
	}

	val, exists := raw["owner_id"]
	if !exists {
		t.Fatal("JSON output does not contain 'owner_id' key; expected 'owner_id' when OwnerID is set")
	}
	if val != "user-123" {
		t.Errorf("owner_id value = %v; want %q", val, "user-123")
	}
}

// TestPersonalOrg_OrganizationJSON_OwnerIDOmitted verifies that when OwnerID
// is nil (zero value for *string), marshalling the Organization struct to
// JSON omits the "owner_id" field entirely (omitempty semantics).
//
// Test Spec: TS-04-13 (JSON marshal with OwnerID nil)
// Requirement: 04-REQ-4.2
func TestPersonalOrg_OrganizationJSON_OwnerIDOmitted(t *testing.T) {
	// Use reflection to verify the field exists.
	orgType := reflect.TypeFor[apikit.Organization]()
	if _, ok := orgType.FieldByName("OwnerID"); !ok {
		t.Fatal("Organization struct does not have OwnerID field; cannot test JSON omission")
	}

	// Create an Organization with nil OwnerID (default zero value).
	org := apikit.Organization{
		ID:     "test-id-2",
		Name:   "Admin Org",
		Slug:   "admin-org",
		Status: "active",
	}

	jsonBytes, err := json.Marshal(org)
	if err != nil {
		t.Fatalf("json.Marshal error = %v; want nil", err)
	}

	// Parse the JSON and verify owner_id is NOT present.
	var raw map[string]any
	if err := json.Unmarshal(jsonBytes, &raw); err != nil {
		t.Fatalf("json.Unmarshal error = %v; want nil", err)
	}

	if _, exists := raw["owner_id"]; exists {
		t.Error("JSON output contains 'owner_id' key when OwnerID is nil; expected omission due to omitempty")
	}
}

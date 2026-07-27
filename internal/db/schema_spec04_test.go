package db

import (
	"testing"
)

// ========================================================================
// Spec 04 Task 2.1: Organization ownership — owner_id column DDL tests
// Test Spec: TS-04-12, TS-04-16
// Requirements: 04-REQ-4.1, 04-REQ-4.5
// ========================================================================

// TestPersonalOrg_OrgsTableOwnerIDColumn verifies that the orgs table DDL
// includes an owner_id column of type TEXT that is nullable (notnull=0).
// Existing org rows without an owner have owner_id = NULL.
//
// Test Spec: TS-04-12
// Requirement: 04-REQ-4.1
func TestPersonalOrg_OrgsTableOwnerIDColumn(t *testing.T) {
	database, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	defer database.Close()

	// Query column metadata via PRAGMA table_info.
	rows, err := database.SqlDB.Query("PRAGMA table_info('orgs')")
	if err != nil {
		t.Fatalf("PRAGMA table_info('orgs') error = %v", err)
	}
	defer rows.Close()

	type columnInfo struct {
		CID       int
		Name      string
		Type      string
		NotNull   int
		DfltValue *string
		PK        int
	}

	var found bool
	for rows.Next() {
		var col columnInfo
		if err := rows.Scan(&col.CID, &col.Name, &col.Type, &col.NotNull, &col.DfltValue, &col.PK); err != nil {
			t.Fatalf("failed to scan column info: %v", err)
		}

		if col.Name == "owner_id" {
			found = true

			// owner_id must be TEXT type.
			if col.Type != "TEXT" {
				t.Errorf("owner_id column type = %q; want %q", col.Type, "TEXT")
			}

			// owner_id must be nullable (notnull = 0).
			if col.NotNull != 0 {
				t.Errorf("owner_id column notnull = %d; want 0 (nullable)", col.NotNull)
			}

			// owner_id must NOT be a primary key.
			if col.PK != 0 {
				t.Errorf("owner_id column pk = %d; want 0 (not a primary key)", col.PK)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows iteration error: %v", err)
	}

	if !found {
		t.Fatal("owner_id column not found in orgs table; expected TEXT nullable column")
	}

	// Verify that existing org rows without an owner have owner_id = NULL.
	// Insert an org without owner_id and confirm it reads back as NULL.
	_, err = database.SqlDB.Exec(
		`INSERT INTO orgs (id, name, slug, url, status, created_at, updated_at)
		 VALUES ('org-no-owner', 'No Owner Org', 'no-owner', '', 'active', '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`,
	)
	if err != nil {
		t.Fatalf("failed to insert org without owner_id: %v", err)
	}

	var ownerID *string
	err = database.SqlDB.QueryRow("SELECT owner_id FROM orgs WHERE id = 'org-no-owner'").Scan(&ownerID)
	if err != nil {
		t.Fatalf("failed to query owner_id: %v", err)
	}
	if ownerID != nil {
		t.Errorf("owner_id for org without owner = %v; want nil (NULL)", *ownerID)
	}
}

// TestPersonalOrg_OrgsTableNoForeignKeyOnOwnerID verifies that the owner_id
// column in the orgs table has no foreign key constraint. The column is treated
// as a soft reference to users.id at the application level only.
//
// Test Spec: TS-04-16
// Requirement: 04-REQ-4.5
func TestPersonalOrg_OrgsTableNoForeignKeyOnOwnerID(t *testing.T) {
	database, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory error = %v; want nil", err)
	}
	defer database.Close()

	// First verify the owner_id column exists — if it doesn't,
	// there's nothing meaningful to check about its FK constraints.
	var colExists bool
	colRows, err := database.SqlDB.Query("PRAGMA table_info('orgs')")
	if err != nil {
		t.Fatalf("PRAGMA table_info('orgs') error = %v", err)
	}
	for colRows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dflt *string
		if err := colRows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			t.Fatalf("failed to scan column info: %v", err)
		}
		if name == "owner_id" {
			colExists = true
		}
	}
	colRows.Close()

	if !colExists {
		t.Fatal("owner_id column not found in orgs table; cannot verify FK constraints")
	}

	// Query foreign key constraints on the orgs table.
	rows, err := database.SqlDB.Query("PRAGMA foreign_key_list('orgs')")
	if err != nil {
		t.Fatalf("PRAGMA foreign_key_list('orgs') error = %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id, seq int
		var table, from, to, onUpdate, onDelete, match string
		if err := rows.Scan(&id, &seq, &table, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			t.Fatalf("failed to scan FK info: %v", err)
		}

		if from == "owner_id" {
			t.Errorf("found foreign key constraint on owner_id referencing %s(%s); want no FK constraint", table, to)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows iteration error: %v", err)
	}
}

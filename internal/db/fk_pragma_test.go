package db

import (
	"path/filepath"
	"testing"
)

// TestOpen_ForeignKeysEnforcedViaDSN verifies that foreign-key enforcement
// is requested through the DSN, so that every connection database/sql opens
// (not only the first one) enforces REFERENCES and ON DELETE CASCADE.
func TestOpen_ForeignKeysEnforcedViaDSN(t *testing.T) {
	for _, tc := range []struct {
		name string
		open func() (*DB, error)
	}{
		{"file", func() (*DB, error) { return Open(filepath.Join(t.TempDir(), "fk.db")) }},
		{"memory", OpenMemory},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := tc.open()
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer d.Close()

			// Force the pool to discard the initial connection and open a
			// fresh one; the pragma must survive because it lives in the DSN.
			d.SqlDB.SetMaxIdleConns(0)
			d.SqlDB.SetMaxIdleConns(1)

			var fk int
			if err := d.SqlDB.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
				t.Fatalf("query pragma: %v", err)
			}
			if fk != 1 {
				t.Fatalf("PRAGMA foreign_keys = %d, want 1", fk)
			}

			// A dangling reference must be rejected.
			_, err = d.SqlDB.Exec(
				`INSERT INTO api_keys (key_id, user_id, secret_hash, expires_days, created_at)
				 VALUES ('fkkey001', 'no-such-user', 'h', 0, '2026-01-01T00:00:00Z')`)
			if err == nil {
				t.Fatal("insert with dangling user_id succeeded; foreign keys not enforced")
			}
		})
	}
}

package db

import (
	"path/filepath"
	"testing"
)

func TestOpenSQLite_RegistersDriver(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "open-sqlite.db")

	db, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
}

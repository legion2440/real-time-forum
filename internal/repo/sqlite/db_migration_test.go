package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpen_MigratesLegacyUsersTableBeforeDisplayNameIndex(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "legacy.db")

	legacyDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		if strings.Contains(err.Error(), "requires cgo to work") {
			t.Skip("sqlite3 driver requires cgo")
		}
		t.Fatalf("open legacy db: %v", err)
	}

	t.Cleanup(func() { _ = legacyDB.Close() })

	_, err = legacyDB.Exec(`
		CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT NOT NULL UNIQUE,
			username TEXT NOT NULL UNIQUE,
			pass_hash TEXT NOT NULL,
			created_at INTEGER NOT NULL
		);

		INSERT INTO users (email, username, pass_hash, created_at)
		VALUES ('legacy@example.com', 'legacy-user', 'hash', 1700000000);
	`)
	if err != nil {
		if strings.Contains(err.Error(), "requires cgo to work") {
			t.Skip("sqlite3 driver requires cgo")
		}
		t.Fatalf("seed legacy schema: %v", err)
	}

	_ = legacyDB.Close()

	db, err := Open(dbPath)
	if err != nil {
		if strings.Contains(err.Error(), "requires cgo to work") {
			t.Skip("sqlite3 driver requires cgo")
		}
		t.Fatalf("open migrated db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	hasDisplayName, err := tableHasColumn(db, "users", "display_name")
	if err != nil {
		t.Fatalf("check display_name column: %v", err)
	}
	if !hasDisplayName {
		t.Fatal("expected users.display_name column to be added")
	}

	hasProfileInitialized, err := tableHasColumn(db, "users", "profile_initialized")
	if err != nil {
		t.Fatalf("check profile_initialized column: %v", err)
	}
	if !hasProfileInitialized {
		t.Fatal("expected users.profile_initialized column to be added")
	}

	userRepo := NewUserRepo(db)
	user, err := userRepo.GetByUsername(context.Background(), "legacy-user")
	if err != nil {
		t.Fatalf("get migrated user: %v", err)
	}
	if user.Email != "legacy@example.com" {
		t.Fatalf("expected migrated user email to be preserved, got %q", user.Email)
	}
}

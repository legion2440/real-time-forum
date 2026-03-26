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

		CREATE TABLE posts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			title TEXT NOT NULL,
			body TEXT NOT NULL,
			created_at INTEGER NOT NULL
		);

		CREATE TABLE private_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			from_user_id INTEGER NOT NULL,
			to_user_id INTEGER NOT NULL,
			body TEXT NOT NULL,
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

	hasFirstName, err := tableHasColumn(db, "users", "first_name")
	if err != nil {
		t.Fatalf("check first_name column: %v", err)
	}
	if !hasFirstName {
		t.Fatal("expected users.first_name column to be added")
	}

	hasLastName, err := tableHasColumn(db, "users", "last_name")
	if err != nil {
		t.Fatalf("check last_name column: %v", err)
	}
	if !hasLastName {
		t.Fatal("expected users.last_name column to be added")
	}

	hasAge, err := tableHasColumn(db, "users", "age")
	if err != nil {
		t.Fatalf("check age column: %v", err)
	}
	if !hasAge {
		t.Fatal("expected users.age column to be added")
	}

	hasGender, err := tableHasColumn(db, "users", "gender")
	if err != nil {
		t.Fatalf("check gender column: %v", err)
	}
	if !hasGender {
		t.Fatal("expected users.gender column to be added")
	}
	hasAttachmentsTable, err := tableExists(db, "attachments")
	if err != nil {
		t.Fatalf("check attachments table: %v", err)
	}
	if !hasAttachmentsTable {
		t.Fatal("expected attachments table to be added")
	}

	hasDMReadStateTable, err := tableExists(db, "dm_read_state")
	if err != nil {
		t.Fatalf("check dm_read_state table: %v", err)
	}
	if !hasDMReadStateTable {
		t.Fatal("expected dm_read_state table to be added")
	}

	hasNotificationsTable, err := tableExists(db, "notifications")
	if err != nil {
		t.Fatalf("check notifications table: %v", err)
	}
	if !hasNotificationsTable {
		t.Fatal("expected notifications table to be added")
	}

	hasPostSubscriptionsTable, err := tableExists(db, "post_subscriptions")
	if err != nil {
		t.Fatalf("check post_subscriptions table: %v", err)
	}
	if !hasPostSubscriptionsTable {
		t.Fatal("expected post_subscriptions table to be added")
	}

	hasUserFollowsTable, err := tableExists(db, "user_follows")
	if err != nil {
		t.Fatalf("check user_follows table: %v", err)
	}
	if !hasUserFollowsTable {
		t.Fatal("expected user_follows table to be added")
	}

	hasPostAttachment, err := tableHasColumn(db, "posts", "attachment_id")
	if err != nil {
		t.Fatalf("check posts.attachment_id column: %v", err)
	}
	if !hasPostAttachment {
		t.Fatal("expected posts.attachment_id column to be added")
	}

	hasPrivateMessageAttachment, err := tableHasColumn(db, "private_messages", "attachment_id")
	if err != nil {
		t.Fatalf("check private_messages.attachment_id column: %v", err)
	}
	if !hasPrivateMessageAttachment {
		t.Fatal("expected private_messages.attachment_id column to be added")
	}

	hasPostReactionCreatedAt, err := tableHasColumn(db, "post_reactions", "created_at")
	if err != nil {
		t.Fatalf("check post_reactions.created_at column: %v", err)
	}
	if !hasPostReactionCreatedAt {
		t.Fatal("expected post_reactions.created_at column to be added")
	}

	hasCommentReactionCreatedAt, err := tableHasColumn(db, "comment_reactions", "created_at")
	if err != nil {
		t.Fatalf("check comment_reactions.created_at column: %v", err)
	}
	if !hasCommentReactionCreatedAt {
		t.Fatal("expected comment_reactions.created_at column to be added")
	}

	userRepo := NewUserRepo(db)
	user, err := userRepo.GetByUsername(context.Background(), "legacy-user")
	if err != nil {
		t.Fatalf("get migrated user: %v", err)
	}
	if user.Email != "legacy@example.com" {
		t.Fatalf("expected migrated user email to be preserved, got %q", user.Email)
	}
	if user.FirstName != "" || user.LastName != "" || user.Age != 0 || user.Gender != "" {
		t.Fatalf("expected new profile fields to keep defaults, got %+v", user)
	}
}

func TestOpen_MigratesLegacyCommentsTableDeletedAtColumn(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "legacy-comments.db")

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

		CREATE TABLE posts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			title TEXT NOT NULL,
			body TEXT NOT NULL,
			created_at INTEGER NOT NULL
		);

		CREATE TABLE comments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			post_id INTEGER NOT NULL,
			parent_id INTEGER,
			user_id INTEGER NOT NULL,
			body TEXT NOT NULL,
			created_at INTEGER NOT NULL
		);
	`)
	if err != nil {
		if strings.Contains(err.Error(), "requires cgo to work") {
			t.Skip("sqlite3 driver requires cgo")
		}
		t.Fatalf("seed legacy comments schema: %v", err)
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

	hasDeletedAt, err := tableHasColumn(db, "comments", "deleted_at")
	if err != nil {
		t.Fatalf("check comments.deleted_at column: %v", err)
	}
	if !hasDeletedAt {
		t.Fatal("expected comments.deleted_at column to be added")
	}
}

func tableExists(db *sql.DB, tableName string) (bool, error) {
	row := db.QueryRow(`SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?`, tableName)
	var marker int
	if err := row.Scan(&marker); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return marker == 1, nil
}

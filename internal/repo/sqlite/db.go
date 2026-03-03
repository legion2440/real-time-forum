package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "embed"
	_ "github.com/mattn/go-sqlite3"
)

//go:embed schema.sql
var schemaSQL string

var defaultCategories = []string{
	"General",
	"Tech",
	"Science",
	"Art",
	"Sports",
	"Music",
	"News",
}

func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}

	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return nil, err
	}

	if err := applySchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureCommentParentColumn(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureAttachmentsTable(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureUserDisplayNameColumn(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureUserFirstNameColumn(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureUserLastNameColumn(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureUserAgeColumn(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureUserGenderColumn(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureUserProfileInitializedColumn(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensurePostAttachmentColumn(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensurePrivateMessageAttachmentColumn(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureUserDisplayNameIndex(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureAttachmentIndexes(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	if err := seedCategories(context.Background(), db, defaultCategories); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

func applySchema(db *sql.DB) error {
	schema := strings.TrimSpace(schemaSQL)
	if schema == "" {
		return fmt.Errorf("schema is empty")
	}
	_, err := db.Exec(schema)
	return err
}

func ensureCommentParentColumn(db *sql.DB) error {
	hasColumn, err := tableHasColumn(db, "comments", "parent_id")
	if err != nil {
		return err
	}
	if hasColumn {
		return nil
	}

	_, err = db.Exec("ALTER TABLE comments ADD COLUMN parent_id INTEGER")
	return err
}

func ensureAttachmentsTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS attachments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			owner_user_id INTEGER NOT NULL,
			mime TEXT NOT NULL,
			size INTEGER NOT NULL,
			storage_key TEXT NOT NULL,
			original_name TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			FOREIGN KEY(owner_user_id) REFERENCES users(id) ON DELETE CASCADE
		)
	`)
	return err
}

func ensureUserDisplayNameColumn(db *sql.DB) error {
	hasColumn, err := tableHasColumn(db, "users", "display_name")
	if err != nil {
		return err
	}
	if hasColumn {
		return nil
	}

	_, err = db.Exec("ALTER TABLE users ADD COLUMN display_name TEXT")
	return err
}

func ensureUserProfileInitializedColumn(db *sql.DB) error {
	hasColumn, err := tableHasColumn(db, "users", "profile_initialized")
	if err != nil {
		return err
	}
	if hasColumn {
		return nil
	}

	_, err = db.Exec("ALTER TABLE users ADD COLUMN profile_initialized INTEGER NOT NULL DEFAULT 0")
	return err
}

func ensureUserFirstNameColumn(db *sql.DB) error {
	hasColumn, err := tableHasColumn(db, "users", "first_name")
	if err != nil {
		return err
	}
	if hasColumn {
		return nil
	}

	_, err = db.Exec("ALTER TABLE users ADD COLUMN first_name TEXT NOT NULL DEFAULT ''")
	return err
}

func ensureUserLastNameColumn(db *sql.DB) error {
	hasColumn, err := tableHasColumn(db, "users", "last_name")
	if err != nil {
		return err
	}
	if hasColumn {
		return nil
	}

	_, err = db.Exec("ALTER TABLE users ADD COLUMN last_name TEXT NOT NULL DEFAULT ''")
	return err
}

func ensureUserAgeColumn(db *sql.DB) error {
	hasColumn, err := tableHasColumn(db, "users", "age")
	if err != nil {
		return err
	}
	if hasColumn {
		return nil
	}

	_, err = db.Exec("ALTER TABLE users ADD COLUMN age INTEGER NOT NULL DEFAULT 0")
	return err
}

func ensureUserGenderColumn(db *sql.DB) error {
	hasColumn, err := tableHasColumn(db, "users", "gender")
	if err != nil {
		return err
	}
	if hasColumn {
		return nil
	}

	_, err = db.Exec("ALTER TABLE users ADD COLUMN gender TEXT NOT NULL DEFAULT ''")
	return err
}

func ensurePostAttachmentColumn(db *sql.DB) error {
	hasColumn, err := tableHasColumn(db, "posts", "attachment_id")
	if err != nil {
		return err
	}
	if hasColumn {
		return nil
	}

	_, err = db.Exec("ALTER TABLE posts ADD COLUMN attachment_id INTEGER")
	return err
}

func ensurePrivateMessageAttachmentColumn(db *sql.DB) error {
	hasColumn, err := tableHasColumn(db, "private_messages", "attachment_id")
	if err != nil {
		return err
	}
	if hasColumn {
		return nil
	}

	_, err = db.Exec("ALTER TABLE private_messages ADD COLUMN attachment_id INTEGER")
	return err
}

func ensureUserDisplayNameIndex(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_users_display_name_nocase
		ON users(display_name COLLATE NOCASE)
		WHERE display_name IS NOT NULL AND display_name <> ''
	`)
	return err
}

func ensureAttachmentIndexes(db *sql.DB) error {
	statements := []string{
		`CREATE INDEX IF NOT EXISTS idx_attachments_owner_user_id ON attachments(owner_user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_private_messages_from_to_created_at ON private_messages(from_user_id, to_user_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_private_messages_to_from_created_at ON private_messages(to_user_id, from_user_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_private_messages_attachment_id ON private_messages(attachment_id)`,
		`CREATE INDEX IF NOT EXISTS idx_posts_attachment_id ON posts(attachment_id)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func tableHasColumn(db *sql.DB, tableName, columnName string) (bool, error) {
	rows, err := db.Query("PRAGMA table_info(" + tableName + ")")
	if err != nil {
		return false, err
	}
	defer rows.Close()

	var (
		cid       int
		name      string
		ctype     string
		notnull   int
		dfltValue sql.NullString
		pk        int
	)
	for rows.Next() {
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return false, err
		}
		if strings.EqualFold(name, columnName) {
			return true, rows.Err()
		}
	}

	return false, rows.Err()
}

func seedCategories(ctx context.Context, db *sql.DB, categories []string) error {
	for _, name := range categories {
		if strings.TrimSpace(name) == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, "INSERT OR IGNORE INTO categories(name) VALUES (?)", name); err != nil {
			return err
		}
	}
	return nil
}

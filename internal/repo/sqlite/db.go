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

type categorySeed struct {
	Code     string
	Name     string
	IsSystem bool
}

var defaultCategories = []categorySeed{
	{Code: "other", Name: "Other", IsSystem: true},
	{Code: "general", Name: "General"},
	{Code: "tech", Name: "Tech"},
	{Code: "science", Name: "Science"},
	{Code: "art", Name: "Art"},
	{Code: "sports", Name: "Sports"},
	{Code: "music", Name: "Music"},
	{Code: "news", Name: "News"},
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
	if err := ensureCommentDeletedAtColumn(db); err != nil {
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
	if err := ensureUserRoleColumn(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureUserProfileInitializedColumn(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureUsersEmailIndex(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensurePostAttachmentColumn(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensurePostModerationColumns(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensurePrivateMessageAttachmentColumn(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureCommentModerationColumns(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureCategoryCodeColumn(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureCategorySystemColumn(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureModerationTables(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureDMReadStateTable(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureReactionCreatedAtColumns(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureNotificationsTable(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensurePostSubscriptionsTable(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureUserFollowsTable(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureAuthIdentitiesTable(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureAuthFlowsTable(db); err != nil {
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
	if err := ensureDMReadStateIndexes(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureCenterIndexes(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureAuthIndexes(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureModerationIndexes(db); err != nil {
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

func ensureCommentDeletedAtColumn(db *sql.DB) error {
	hasColumn, err := tableHasColumn(db, "comments", "deleted_at")
	if err != nil {
		return err
	}
	if hasColumn {
		return nil
	}

	_, err = db.Exec("ALTER TABLE comments ADD COLUMN deleted_at INTEGER")
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

func ensureUsersEmailIndex(db *sql.DB) error {
	hasDuplicates, err := usersHaveCaseInsensitiveDuplicateEmails(db)
	if err != nil {
		return err
	}

	if hasDuplicates {
		if _, err := db.Exec(`DROP INDEX IF EXISTS idx_users_email_nocase`); err != nil {
			return err
		}
		_, err = db.Exec(`
			CREATE INDEX IF NOT EXISTS idx_users_email_lookup_nocase
			ON users(email COLLATE NOCASE)
		`)
		return err
	}

	if _, err := db.Exec(`DROP INDEX IF EXISTS idx_users_email_lookup_nocase`); err != nil {
		return err
	}
	_, err = db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_nocase
		ON users(email COLLATE NOCASE)
	`)
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

func ensureUserRoleColumn(db *sql.DB) error {
	hasColumn, err := tableHasColumn(db, "users", "role")
	if err != nil {
		return err
	}
	if !hasColumn {
		if _, err := db.Exec("ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user'"); err != nil {
			return err
		}
	}

	if _, err := db.Exec("UPDATE users SET role = 'user' WHERE TRIM(COALESCE(role, '')) = ''"); err != nil {
		return err
	}
	_, err = db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_users_single_owner
		ON users(role)
		WHERE role = 'owner'
	`)
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

func ensurePostModerationColumns(db *sql.DB) error {
	columns := map[string]string{
		"is_under_review": "ALTER TABLE posts ADD COLUMN is_under_review INTEGER NOT NULL DEFAULT 0",
		"approved_by":     "ALTER TABLE posts ADD COLUMN approved_by INTEGER",
		"approved_at":     "ALTER TABLE posts ADD COLUMN approved_at INTEGER",
		"delete_protected": "ALTER TABLE posts ADD COLUMN delete_protected INTEGER NOT NULL DEFAULT 0",
		"deleted_at":      "ALTER TABLE posts ADD COLUMN deleted_at INTEGER",
		"deleted_by":      "ALTER TABLE posts ADD COLUMN deleted_by INTEGER",
		"deleted_by_role": "ALTER TABLE posts ADD COLUMN deleted_by_role TEXT NOT NULL DEFAULT ''",
	}
	for name, statement := range columns {
		hasColumn, err := tableHasColumn(db, "posts", name)
		if err != nil {
			return err
		}
		if hasColumn {
			continue
		}
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
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

func ensureCommentModerationColumns(db *sql.DB) error {
	columns := map[string]string{
		"deleted_body":    "ALTER TABLE comments ADD COLUMN deleted_body TEXT NOT NULL DEFAULT ''",
		"deleted_by":      "ALTER TABLE comments ADD COLUMN deleted_by INTEGER",
		"deleted_by_role": "ALTER TABLE comments ADD COLUMN deleted_by_role TEXT NOT NULL DEFAULT ''",
	}
	for name, statement := range columns {
		hasColumn, err := tableHasColumn(db, "comments", name)
		if err != nil {
			return err
		}
		if hasColumn {
			continue
		}
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func ensureCategoryCodeColumn(db *sql.DB) error {
	hasColumn, err := tableHasColumn(db, "categories", "code")
	if err != nil {
		return err
	}
	if !hasColumn {
		if _, err := db.Exec("ALTER TABLE categories ADD COLUMN code TEXT"); err != nil {
			return err
		}
	}
	if err := backfillCategoryCodes(db); err != nil {
		return err
	}
	_, err = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_categories_code ON categories(code)`)
	return err
}

func ensureCategorySystemColumn(db *sql.DB) error {
	hasColumn, err := tableHasColumn(db, "categories", "is_system")
	if err != nil {
		return err
	}
	if !hasColumn {
		if _, err := db.Exec("ALTER TABLE categories ADD COLUMN is_system INTEGER NOT NULL DEFAULT 0"); err != nil {
			return err
		}
	}
	_, err = db.Exec(`UPDATE categories SET is_system = 1 WHERE code = 'other'`)
	return err
}

func ensureModerationTables(db *sql.DB) error {
	statements := []string{
		`
		CREATE TABLE IF NOT EXISTS moderation_role_requests (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			requester_user_id INTEGER NOT NULL,
			requested_role TEXT NOT NULL,
			note TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			reviewed_at INTEGER,
			reviewed_by INTEGER,
			review_note TEXT NOT NULL DEFAULT '',
			FOREIGN KEY(requester_user_id) REFERENCES users(id) ON DELETE CASCADE,
			FOREIGN KEY(reviewed_by) REFERENCES users(id) ON DELETE SET NULL
		)
		`,
		`
		CREATE TABLE IF NOT EXISTS moderation_reports (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			target_type TEXT NOT NULL,
			target_id INTEGER NOT NULL,
			reporter_user_id INTEGER NOT NULL,
			reporter_role TEXT NOT NULL,
			content_author_user_id INTEGER NOT NULL,
			reason TEXT NOT NULL,
			note TEXT NOT NULL,
			status TEXT NOT NULL,
			route_to_roles TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			closed_at INTEGER,
			closed_by INTEGER,
			closed_by_role TEXT NOT NULL DEFAULT '',
			decision_reason TEXT NOT NULL DEFAULT '',
			decision_note TEXT NOT NULL DEFAULT '',
			linked_previous_decision_id INTEGER,
			FOREIGN KEY(reporter_user_id) REFERENCES users(id) ON DELETE CASCADE,
			FOREIGN KEY(content_author_user_id) REFERENCES users(id) ON DELETE CASCADE,
			FOREIGN KEY(closed_by) REFERENCES users(id) ON DELETE SET NULL
		)
		`,
		`
		CREATE TABLE IF NOT EXISTS moderation_appeals (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			target_type TEXT NOT NULL,
			target_id INTEGER NOT NULL,
			requester_user_id INTEGER NOT NULL,
			target_role TEXT NOT NULL,
			status TEXT NOT NULL,
			note TEXT NOT NULL,
			source_history_id INTEGER NOT NULL,
			linked_previous_decision_id INTEGER,
			created_at INTEGER NOT NULL,
			closed_at INTEGER,
			closed_by INTEGER,
			closed_by_role TEXT NOT NULL DEFAULT '',
			decision_note TEXT NOT NULL DEFAULT '',
			FOREIGN KEY(requester_user_id) REFERENCES users(id) ON DELETE CASCADE,
			FOREIGN KEY(closed_by) REFERENCES users(id) ON DELETE SET NULL
		)
		`,
		`
		CREATE TABLE IF NOT EXISTS moderation_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			acted_at INTEGER NOT NULL,
			action_type TEXT NOT NULL,
			target_type TEXT NOT NULL,
			target_id INTEGER NOT NULL,
			content_author_user_id INTEGER NOT NULL DEFAULT 0,
			content_author_name TEXT NOT NULL DEFAULT '',
			actor_user_id INTEGER NOT NULL DEFAULT 0,
			actor_username TEXT NOT NULL DEFAULT '',
			actor_display_name TEXT NOT NULL DEFAULT '',
			actor_role TEXT NOT NULL DEFAULT '',
			reason TEXT NOT NULL DEFAULT '',
			note TEXT NOT NULL DEFAULT '',
			current_status TEXT NOT NULL DEFAULT '',
			route_to_role TEXT NOT NULL DEFAULT '',
			linked_previous_decision_id INTEGER,
			post_title_snapshot TEXT NOT NULL DEFAULT '',
			post_body_snapshot TEXT NOT NULL DEFAULT '',
			comment_body_snapshot TEXT NOT NULL DEFAULT ''
		)
		`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func ensureDMReadStateTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS dm_read_state (
			user_id INTEGER NOT NULL,
			peer_id INTEGER NOT NULL,
			last_read_message_id INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY (user_id, peer_id),
			FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
			FOREIGN KEY(peer_id) REFERENCES users(id) ON DELETE CASCADE
		)
	`)
	return err
}

func ensureAuthIdentitiesTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS auth_identities (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			provider TEXT NOT NULL,
			provider_user_id TEXT NOT NULL,
			provider_email TEXT NOT NULL DEFAULT '',
			provider_email_verified INTEGER NOT NULL DEFAULT 0,
			provider_display_name TEXT NOT NULL DEFAULT '',
			provider_avatar_url TEXT NOT NULL DEFAULT '',
			linked_at INTEGER NOT NULL,
			last_login_at INTEGER NOT NULL,
			UNIQUE(provider, provider_user_id),
			UNIQUE(user_id, provider),
			FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
		)
	`)
	return err
}

func ensureAuthFlowsTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS auth_flows (
			token TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			user_id INTEGER,
			payload_json TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL,
			FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
		)
	`)
	return err
}

func ensureReactionCreatedAtColumns(db *sql.DB) error {
	for _, tableName := range []string{"post_reactions", "comment_reactions"} {
		hasColumn, err := tableHasColumn(db, tableName, "created_at")
		if err != nil {
			return err
		}
		if hasColumn {
			continue
		}
		if _, err := db.Exec("ALTER TABLE " + tableName + " ADD COLUMN created_at INTEGER NOT NULL DEFAULT 0"); err != nil {
			return err
		}
	}
	return nil
}

func ensureNotificationsTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS notifications (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			actor_user_id INTEGER,
			bucket TEXT NOT NULL,
			type TEXT NOT NULL,
			entity_type TEXT NOT NULL DEFAULT '',
			entity_id INTEGER NOT NULL DEFAULT 0,
			secondary_entity_type TEXT NOT NULL DEFAULT '',
			secondary_entity_id INTEGER NOT NULL DEFAULT 0,
			payload_json TEXT NOT NULL DEFAULT '{}',
			is_read INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			read_at INTEGER,
			FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
			FOREIGN KEY(actor_user_id) REFERENCES users(id) ON DELETE SET NULL
		)
	`)
	return err
}

func ensurePostSubscriptionsTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS post_subscriptions (
			user_id INTEGER NOT NULL,
			post_id INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			PRIMARY KEY (user_id, post_id),
			FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
			FOREIGN KEY(post_id) REFERENCES posts(id) ON DELETE CASCADE
		)
	`)
	return err
}

func ensureUserFollowsTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS user_follows (
			follower_user_id INTEGER NOT NULL,
			followed_user_id INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			PRIMARY KEY (follower_user_id, followed_user_id),
			FOREIGN KEY(follower_user_id) REFERENCES users(id) ON DELETE CASCADE,
			FOREIGN KEY(followed_user_id) REFERENCES users(id) ON DELETE CASCADE
		)
	`)
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

func ensureDMReadStateIndexes(db *sql.DB) error {
	_, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_private_messages_to_user_id_id ON private_messages(to_user_id, id)`)
	return err
}

func ensureCenterIndexes(db *sql.DB) error {
	statements := []string{
		`CREATE INDEX IF NOT EXISTS idx_notifications_user_created_at ON notifications(user_id, created_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_notifications_user_bucket_read_created_at ON notifications(user_id, bucket, is_read, created_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_notifications_user_read_created_at ON notifications(user_id, is_read, created_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_notifications_entity_lookup ON notifications(entity_type, entity_id)`,
		`CREATE INDEX IF NOT EXISTS idx_post_subscriptions_post_id ON post_subscriptions(post_id, user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_user_follows_followed_user_id ON user_follows(followed_user_id, follower_user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_post_reactions_user_created_at ON post_reactions(user_id, created_at DESC, post_id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_comment_reactions_user_created_at ON comment_reactions(user_id, created_at DESC, comment_id DESC)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func ensureAuthIndexes(db *sql.DB) error {
	statements := []string{
		`CREATE INDEX IF NOT EXISTS idx_auth_identities_user_id ON auth_identities(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_identities_provider_email_nocase ON auth_identities(provider, provider_email COLLATE NOCASE)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_flows_expires_at ON auth_flows(expires_at)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func ensureModerationIndexes(db *sql.DB) error {
	statements := []string{
		`CREATE INDEX IF NOT EXISTS idx_users_role ON users(role, id)`,
		`CREATE INDEX IF NOT EXISTS idx_posts_under_review_created_at ON posts(is_under_review, created_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_posts_deleted_at ON posts(deleted_at, id)`,
		`CREATE INDEX IF NOT EXISTS idx_comments_deleted_at ON comments(deleted_at, id)`,
		`CREATE INDEX IF NOT EXISTS idx_role_requests_status_created_at ON moderation_role_requests(status, created_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_role_requests_requested_role_status ON moderation_role_requests(requested_role, status, created_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_reports_status_created_at ON moderation_reports(status, created_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_reports_reporter_status ON moderation_reports(reporter_user_id, status, created_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_appeals_status_created_at ON moderation_appeals(status, created_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_appeals_requester_status ON moderation_appeals(requester_user_id, status, created_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_history_acted_at ON moderation_history(acted_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_history_target ON moderation_history(target_type, target_id, acted_at DESC, id DESC)`,
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

func seedCategories(ctx context.Context, db *sql.DB, categories []categorySeed) error {
	for _, category := range categories {
		code := strings.TrimSpace(category.Code)
		name := strings.TrimSpace(category.Name)
		if code == "" || name == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO categories(code, name, is_system)
			VALUES (?, ?, ?)
			ON CONFLICT(code) DO UPDATE SET
				name = excluded.name,
				is_system = CASE
					WHEN categories.code = 'other' THEN 1
					ELSE categories.is_system
				END
		`, code, name, boolToInt(category.IsSystem)); err != nil {
			return err
		}
	}
	return nil
}

func backfillCategoryCodes(db *sql.DB) error {
	rows, err := db.Query(`SELECT id, name, code FROM categories ORDER BY id ASC`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type item struct {
		id   int64
		name string
		code sql.NullString
	}
	var items []item
	for rows.Next() {
		var value item
		if err := rows.Scan(&value.id, &value.name, &value.code); err != nil {
			return err
		}
		items = append(items, value)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	used := make(map[string]struct{}, len(items))
	for _, item := range items {
		code := strings.TrimSpace(item.code.String)
		if code != "" {
			used[code] = struct{}{}
		}
	}

	for _, item := range items {
		if strings.TrimSpace(item.code.String) != "" {
			continue
		}
		code := categoryCodeFromName(item.name)
		if code == "" {
			code = fmt.Sprintf("category-%d", item.id)
		}
		original := code
		suffix := 1
		for {
			if _, exists := used[code]; !exists {
				break
			}
			code = fmt.Sprintf("%s-%d", original, suffix)
			suffix++
		}
		used[code] = struct{}{}
		if _, err := db.Exec(`UPDATE categories SET code = ? WHERE id = ?`, code, item.id); err != nil {
			return err
		}
	}
	return nil
}

func categoryCodeFromName(name string) string {
	name = strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(name)), "-"))
	name = strings.ReplaceAll(name, "--", "-")
	name = strings.Trim(name, "-")
	if name == "" {
		return ""
	}
	if name == "other" {
		return "other"
	}
	return name
}

func usersHaveCaseInsensitiveDuplicateEmails(db *sql.DB) (bool, error) {
	row := db.QueryRow(`
		SELECT 1
		FROM users
		GROUP BY LOWER(TRIM(email))
		HAVING COUNT(*) > 1
		LIMIT 1
	`)

	var marker int
	if err := row.Scan(&marker); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return marker == 1, nil
}

package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"forum/internal/domain"

	"forum/internal/repo"
)

type AccountRepo struct {
	db *sql.DB
}

func NewAccountRepo(db *sql.DB) *AccountRepo {
	return &AccountRepo{db: db}
}

func (r *AccountRepo) HasDirectMessagesBetweenUsers(ctx context.Context, userA, userB int64) (bool, error) {
	row := r.db.QueryRowContext(ctx, `
        SELECT 1
        FROM private_messages
        WHERE (
                (from_user_id = ? AND to_user_id = ?)
                OR
                (from_user_id = ? AND to_user_id = ?)
              )
        LIMIT 1
    `, userA, userB, userB, userA)

	var marker int
	if err := row.Scan(&marker); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return marker == 1, nil
}

func (r *AccountRepo) CreateUserWithIdentity(ctx context.Context, user *domain.User, identity *domain.AuthIdentity) (int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
        INSERT INTO users (email, username, display_name, pass_hash, created_at, profile_initialized)
        VALUES (?, ?, ?, ?, ?, ?)
    `,
		strings.TrimSpace(user.Email),
		strings.TrimSpace(user.Username),
		nullableTrimmedText(user.DisplayName),
		strings.TrimSpace(user.PassHash),
		timeToUnix(user.CreatedAt),
		boolToInt(user.ProfileInitialized),
	)
	if err != nil {
		return 0, err
	}

	userID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	if _, err := tx.ExecContext(ctx, `
        INSERT INTO auth_identities (
            user_id,
            provider,
            provider_user_id,
            provider_email,
            provider_email_verified,
            provider_display_name,
            provider_avatar_url,
            linked_at,
            last_login_at
        )
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
    `,
		userID,
		strings.TrimSpace(identity.Provider),
		strings.TrimSpace(identity.ProviderUserID),
		strings.TrimSpace(identity.ProviderEmail),
		boolToInt(identity.ProviderEmailVerified),
		strings.TrimSpace(identity.ProviderDisplayName),
		strings.TrimSpace(identity.ProviderAvatarURL),
		timeToUnix(identity.LinkedAt),
		timeToUnix(identity.LastLoginAt),
	); err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return userID, nil
}

func (r *AccountRepo) MergeUsers(ctx context.Context, input repo.AccountMergeInput) error {
	if input.TargetUserID <= 0 || input.SourceUserID <= 0 || input.TargetUserID == input.SourceUserID {
		return fmt.Errorf("invalid merge input")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := ensureUsersExistForMerge(ctx, tx, input.TargetUserID, input.SourceUserID); err != nil {
		return err
	}
	if err := mergePostReactions(ctx, tx, input.TargetUserID, input.SourceUserID); err != nil {
		return err
	}
	if err := mergeCommentReactions(ctx, tx, input.TargetUserID, input.SourceUserID); err != nil {
		return err
	}
	if err := mergeDMReadState(ctx, tx, input.TargetUserID, input.SourceUserID); err != nil {
		return err
	}

	statements := []struct {
		query string
		args  []any
	}{
		{query: `UPDATE posts SET user_id = ? WHERE user_id = ?`, args: []any{input.TargetUserID, input.SourceUserID}},
		{query: `UPDATE comments SET user_id = ? WHERE user_id = ?`, args: []any{input.TargetUserID, input.SourceUserID}},
		{query: `UPDATE attachments SET owner_user_id = ? WHERE owner_user_id = ?`, args: []any{input.TargetUserID, input.SourceUserID}},
		{query: `UPDATE private_messages SET from_user_id = ? WHERE from_user_id = ?`, args: []any{input.TargetUserID, input.SourceUserID}},
		{query: `UPDATE private_messages SET to_user_id = ? WHERE to_user_id = ?`, args: []any{input.TargetUserID, input.SourceUserID}},
		{query: `DELETE FROM sessions WHERE user_id IN (?, ?)`, args: []any{input.TargetUserID, input.SourceUserID}},
		{query: `DELETE FROM auth_flows WHERE user_id IN (?, ?)`, args: []any{input.TargetUserID, input.SourceUserID}},
		{query: `UPDATE auth_identities SET user_id = ? WHERE user_id = ?`, args: []any{input.TargetUserID, input.SourceUserID}},
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement.query, statement.args...); err != nil {
			return err
		}
	}

	if input.Identity != nil {
		if _, err := tx.ExecContext(ctx, `
            INSERT INTO auth_identities (
                user_id,
                provider,
                provider_user_id,
                provider_email,
                provider_email_verified,
                provider_display_name,
                provider_avatar_url,
                linked_at,
                last_login_at
            )
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
        `,
			input.TargetUserID,
			input.Identity.Provider,
			input.Identity.ProviderUserID,
			input.Identity.ProviderEmail,
			boolToInt(input.Identity.ProviderEmailVerified),
			input.Identity.ProviderDisplayName,
			input.Identity.ProviderAvatarURL,
			timeToUnix(input.Identity.LinkedAt),
			timeToUnix(input.Identity.LastLoginAt),
		); err != nil {
			return err
		}
	}

	res, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, input.SourceUserID)
	if err != nil {
		return err
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return repo.ErrNotFound
	}

	if _, err := tx.ExecContext(ctx, `
        UPDATE users
        SET email = ?,
            username = ?,
            pass_hash = ?,
            display_name = ?,
            profile_initialized = ?
        WHERE id = ?
    `,
		strings.TrimSpace(input.TargetEmail),
		strings.TrimSpace(input.TargetUsername),
		strings.TrimSpace(input.TargetPassHash),
		nullableDisplayName(input.DisplayName),
		boolToInt(input.TargetProfileInitialized),
		input.TargetUserID,
	); err != nil {
		return err
	}

	return tx.Commit()
}

func ensureUsersExistForMerge(ctx context.Context, tx *sql.Tx, targetUserID, sourceUserID int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM users WHERE id IN (?, ?)`, targetUserID, sourceUserID)
	if err != nil {
		return err
	}
	defer rows.Close()

	found := make(map[int64]struct{}, 2)
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			return err
		}
		found[userID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, ok := found[targetUserID]; !ok {
		return repo.ErrNotFound
	}
	if _, ok := found[sourceUserID]; !ok {
		return repo.ErrNotFound
	}
	return nil
}

func mergePostReactions(ctx context.Context, tx *sql.Tx, targetUserID, sourceUserID int64) error {
	if _, err := tx.ExecContext(ctx, `
        INSERT INTO post_reactions (post_id, user_id, value)
        SELECT post_id, ?, value
        FROM post_reactions
        WHERE user_id = ?
        ON CONFLICT(post_id, user_id) DO NOTHING
    `, targetUserID, sourceUserID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM post_reactions WHERE user_id = ?`, sourceUserID)
	return err
}

func mergeCommentReactions(ctx context.Context, tx *sql.Tx, targetUserID, sourceUserID int64) error {
	if _, err := tx.ExecContext(ctx, `
        INSERT INTO comment_reactions (comment_id, user_id, value)
        SELECT comment_id, ?, value
        FROM comment_reactions
        WHERE user_id = ?
        ON CONFLICT(comment_id, user_id) DO NOTHING
    `, targetUserID, sourceUserID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM comment_reactions WHERE user_id = ?`, sourceUserID)
	return err
}

func mergeDMReadState(ctx context.Context, tx *sql.Tx, targetUserID, sourceUserID int64) error {
	if _, err := tx.ExecContext(ctx, `
        CREATE TEMP TABLE IF NOT EXISTS merged_dm_read_state (
            user_id INTEGER NOT NULL,
            peer_id INTEGER NOT NULL,
            last_read_message_id INTEGER NOT NULL,
            updated_at INTEGER NOT NULL,
            PRIMARY KEY (user_id, peer_id)
        )
    `); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM merged_dm_read_state`); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
        INSERT INTO merged_dm_read_state (user_id, peer_id, last_read_message_id, updated_at)
        SELECT
            CASE WHEN user_id = ? THEN ? ELSE user_id END AS next_user_id,
            CASE WHEN peer_id = ? THEN ? ELSE peer_id END AS next_peer_id,
            last_read_message_id,
            updated_at
        FROM dm_read_state
        WHERE user_id IN (?, ?)
           OR peer_id IN (?, ?)
        ON CONFLICT(user_id, peer_id) DO UPDATE SET
            last_read_message_id = CASE
                WHEN excluded.last_read_message_id > merged_dm_read_state.last_read_message_id
                    THEN excluded.last_read_message_id
                ELSE merged_dm_read_state.last_read_message_id
            END,
            updated_at = CASE
                WHEN excluded.updated_at > merged_dm_read_state.updated_at
                    THEN excluded.updated_at
                ELSE merged_dm_read_state.updated_at
            END
    `, sourceUserID, targetUserID, sourceUserID, targetUserID, targetUserID, sourceUserID, targetUserID, sourceUserID); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
        DELETE FROM merged_dm_read_state
        WHERE user_id = peer_id
    `); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
        DELETE FROM dm_read_state
        WHERE user_id IN (?, ?)
           OR peer_id IN (?, ?)
    `, targetUserID, sourceUserID, targetUserID, sourceUserID); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
        INSERT INTO dm_read_state (user_id, peer_id, last_read_message_id, updated_at)
        SELECT user_id, peer_id, last_read_message_id, updated_at
        FROM merged_dm_read_state
    `); err != nil {
		return err
	}

	_, err := tx.ExecContext(ctx, `DELETE FROM merged_dm_read_state`)
	return err
}

func nullableDisplayName(displayName *string) any {
	if displayName == nil {
		return nil
	}
	return nullableTrimmedText(*displayName)
}

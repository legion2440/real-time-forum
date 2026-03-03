package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"forum/internal/domain"
)

type PrivateMessageRepo struct {
	db *sql.DB
}

func NewPrivateMessageRepo(db *sql.DB) *PrivateMessageRepo {
	return &PrivateMessageRepo{db: db}
}

func (r *PrivateMessageRepo) SavePrivateMessage(ctx context.Context, fromID, toID int64, body string, attachment *domain.Attachment, createdAt time.Time) (*domain.PrivateMessage, error) {
	res, err := r.db.ExecContext(ctx, `
        INSERT INTO private_messages (from_user_id, to_user_id, body, attachment_id, created_at)
        VALUES (?, ?, ?, ?, ?)
    `, fromID, toID, body, nullableAttachmentID(attachment), timeToUnix(createdAt))
	if err != nil {
		return nil, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	return &domain.PrivateMessage{
		ID:         id,
		FromUserID: fromID,
		ToUserID:   toID,
		Body:       body,
		Attachment: attachment.Public(),
		CreatedAt:  createdAt.UTC(),
	}, nil
}

func (r *PrivateMessageRepo) ListConversationLast(ctx context.Context, userA, userB int64, limit int) ([]domain.PrivateMessage, error) {
	return r.listConversation(ctx, userA, userB, limit, 0, 0, false)
}

func (r *PrivateMessageRepo) ListConversationBefore(ctx context.Context, userA, userB, beforeTs, beforeID int64, limit int) ([]domain.PrivateMessage, error) {
	return r.listConversation(ctx, userA, userB, limit, beforeTs, beforeID, true)
}

func (r *PrivateMessageRepo) listConversation(ctx context.Context, userA, userB int64, limit int, beforeTs, beforeID int64, useCursor bool) ([]domain.PrivateMessage, error) {
	query := `
        SELECT pm.id, pm.from_user_id, u.username, u.display_name, pm.to_user_id, pm.body,
               a.id, a.mime, a.size,
               pm.created_at
        FROM private_messages pm
        JOIN users u ON u.id = pm.from_user_id
        LEFT JOIN attachments a ON a.id = pm.attachment_id
        WHERE (
                (pm.from_user_id = ? AND pm.to_user_id = ?)
                OR
                (pm.from_user_id = ? AND pm.to_user_id = ?)
              )
    `
	args := []any{userA, userB, userB, userA}
	if useCursor {
		query += `
        AND ((pm.created_at < ?) OR (pm.created_at = ? AND pm.id < ?))
    `
		args = append(args, beforeTs, beforeTs, beforeID)
	}
	query += `
        ORDER BY pm.created_at DESC, pm.id DESC
        LIMIT ?
    `
	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	messages := make([]domain.PrivateMessage, 0)
	for rows.Next() {
		var msg domain.PrivateMessage
		var created int64
		var fromDisplayName sql.NullString
		var attachmentID sql.NullInt64
		var attachmentMime sql.NullString
		var attachmentSize sql.NullInt64
		if err := rows.Scan(&msg.ID, &msg.FromUserID, &msg.FromUsername, &fromDisplayName, &msg.ToUserID, &msg.Body, &attachmentID, &attachmentMime, &attachmentSize, &created); err != nil {
			return nil, err
		}
		msg.FromDisplayName = strings.TrimSpace(fromDisplayName.String)
		msg.Attachment = attachmentFromNullableFields(attachmentID, attachmentMime, attachmentSize)
		msg.CreatedAt = unixToTime(created)
		messages = append(messages, msg)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return messages, nil
}

func (r *PrivateMessageRepo) ListPeers(ctx context.Context, userID int64) ([]domain.PrivateMessagePeer, error) {
	rows, err := r.db.QueryContext(ctx, `
        SELECT
            u.id,
            u.username,
            u.display_name,
            COALESCE(MAX(pm.created_at), 0) AS last_message_at
        FROM users u
        LEFT JOIN private_messages pm
            ON (
                (pm.from_user_id = ? AND pm.to_user_id = u.id)
                OR
                (pm.from_user_id = u.id AND pm.to_user_id = ?)
            )
        WHERE u.id <> ?
        GROUP BY u.id, u.username, u.display_name
        ORDER BY
            last_message_at DESC,
            LOWER(CASE
                WHEN TRIM(COALESCE(u.display_name, '')) <> '' THEN TRIM(u.display_name)
                ELSE u.username
            END) ASC,
            u.id ASC
    `, userID, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	peers := make([]domain.PrivateMessagePeer, 0)
	for rows.Next() {
		var peer domain.PrivateMessagePeer
		var displayName sql.NullString
		if err := rows.Scan(&peer.ID, &peer.Username, &displayName, &peer.LastMessageAt); err != nil {
			return nil, err
		}
		peer.DisplayName = strings.TrimSpace(displayName.String)
		peers = append(peers, peer)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return peers, nil
}

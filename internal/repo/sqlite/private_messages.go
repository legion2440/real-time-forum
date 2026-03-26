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
            COALESCE(last_pm.created_at, 0) AS last_message_at,
            COALESCE(last_pm.id, 0) AS last_message_id,
            COALESCE(last_pm.from_user_id, 0) AS last_message_from_user_id,
            COALESCE(last_pm.body, '') AS last_message_preview,
            CASE
                WHEN last_pm.attachment_id IS NOT NULL THEN 1
                ELSE 0
            END AS last_message_has_attachment,
            COALESCE((
                SELECT CASE
                    WHEN EXISTS (
                        SELECT 1
                        FROM notifications n0
                        WHERE n0.user_id = ?
                          AND n0.bucket = ?
                          AND n0.type = ?
                          AND n0.secondary_entity_type = ?
                          AND n0.secondary_entity_id = u.id
                        LIMIT 1
                    ) THEN (
                        SELECT COUNT(*)
                        FROM notifications n
                        WHERE n.user_id = ?
                          AND n.bucket = ?
                          AND n.type = ?
                          AND n.secondary_entity_type = ?
                          AND n.secondary_entity_id = u.id
                          AND n.is_read = 0
                    )
                    ELSE (
                        SELECT COUNT(*)
                        FROM private_messages incoming
                        WHERE incoming.from_user_id = u.id
                          AND incoming.to_user_id = ?
                          AND incoming.id > COALESCE((
                              SELECT rs.last_read_message_id
                              FROM dm_read_state rs
                              WHERE rs.user_id = ? AND rs.peer_id = u.id
                          ), 0)
                    )
                END
            ), 0) AS unread_count
        FROM users u
        LEFT JOIN private_messages last_pm
            ON last_pm.id = (
                SELECT pm2.id
                FROM private_messages pm2
                WHERE (
                        (pm2.from_user_id = ? AND pm2.to_user_id = u.id)
                        OR
                        (pm2.from_user_id = u.id AND pm2.to_user_id = ?)
                      )
                ORDER BY pm2.created_at DESC, pm2.id DESC
                LIMIT 1
            )
        WHERE u.id <> ?
        ORDER BY
            last_message_at DESC,
            LOWER(CASE
                WHEN TRIM(COALESCE(u.display_name, '')) <> '' THEN TRIM(u.display_name)
                ELSE u.username
            END) ASC,
            u.id ASC
    `, userID, domain.NotificationBucketDM, domain.NotificationTypeDMMessage, domain.NotificationEntityTypeUser,
		userID, domain.NotificationBucketDM, domain.NotificationTypeDMMessage, domain.NotificationEntityTypeUser,
		userID, userID,
		userID, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	peers := make([]domain.PrivateMessagePeer, 0)
	for rows.Next() {
		var peer domain.PrivateMessagePeer
		var displayName sql.NullString
		var lastMessagePreview sql.NullString
		var lastMessageHasAttachment int
		if err := rows.Scan(&peer.ID, &peer.Username, &displayName, &peer.LastMessageAt, &peer.LastMessageID, &peer.LastMessageFromUserID, &lastMessagePreview, &lastMessageHasAttachment, &peer.UnreadCount); err != nil {
			return nil, err
		}
		peer.DisplayName = strings.TrimSpace(displayName.String)
		peer.LastMessagePreview = strings.TrimSpace(lastMessagePreview.String)
		peer.LastMessageHasAttachment = lastMessageHasAttachment == 1
		peers = append(peers, peer)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return peers, nil
}

func (r *PrivateMessageRepo) MarkRead(ctx context.Context, userID, peerID, lastReadMessageID int64, updatedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
        INSERT INTO dm_read_state (user_id, peer_id, last_read_message_id, updated_at)
        VALUES (?, ?, ?, ?)
        ON CONFLICT(user_id, peer_id) DO UPDATE SET
            last_read_message_id = CASE
                WHEN excluded.last_read_message_id > dm_read_state.last_read_message_id
                    THEN excluded.last_read_message_id
                ELSE dm_read_state.last_read_message_id
            END,
            updated_at = excluded.updated_at
    `, userID, peerID, lastReadMessageID, timeToUnix(updatedAt))
	return err
}

func (r *PrivateMessageRepo) ConversationHasMessage(ctx context.Context, userA, userB, messageID int64) (bool, error) {
	row := r.db.QueryRowContext(ctx, `
        SELECT 1
        FROM private_messages
        WHERE id = ?
          AND (
                (from_user_id = ? AND to_user_id = ?)
                OR
                (from_user_id = ? AND to_user_id = ?)
              )
        LIMIT 1
    `, messageID, userA, userB, userB, userA)

	var marker int
	if err := row.Scan(&marker); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return marker == 1, nil
}

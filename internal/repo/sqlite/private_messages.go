package sqlite

import (
	"context"
	"database/sql"
	"time"

	"forum/internal/domain"
)

type PrivateMessageRepo struct {
	db *sql.DB
}

func NewPrivateMessageRepo(db *sql.DB) *PrivateMessageRepo {
	return &PrivateMessageRepo{db: db}
}

func (r *PrivateMessageRepo) SavePrivateMessage(ctx context.Context, fromID, toID int64, body string, createdAt time.Time) (*domain.PrivateMessage, error) {
	res, err := r.db.ExecContext(ctx, `
        INSERT INTO private_messages (from_user_id, to_user_id, body, created_at)
        VALUES (?, ?, ?, ?)
    `, fromID, toID, body, timeToUnix(createdAt))
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
		CreatedAt:  createdAt.UTC(),
	}, nil
}

func (r *PrivateMessageRepo) ListConversationLast(ctx context.Context, userA, userB int64, limit int) ([]domain.PrivateMessage, error) {
	rows, err := r.db.QueryContext(ctx, `
        SELECT pm.id, pm.from_user_id, u.username, pm.to_user_id, pm.body, pm.created_at
        FROM private_messages pm
        JOIN users u ON u.id = pm.from_user_id
        WHERE (pm.from_user_id = ? AND pm.to_user_id = ?)
           OR (pm.from_user_id = ? AND pm.to_user_id = ?)
        ORDER BY pm.created_at DESC, pm.id DESC
        LIMIT ?
    `, userA, userB, userB, userA, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	messages := make([]domain.PrivateMessage, 0)
	for rows.Next() {
		var msg domain.PrivateMessage
		var created int64
		if err := rows.Scan(&msg.ID, &msg.FromUserID, &msg.FromUsername, &msg.ToUserID, &msg.Body, &created); err != nil {
			return nil, err
		}
		msg.CreatedAt = unixToTime(created)
		messages = append(messages, msg)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return messages, nil
}

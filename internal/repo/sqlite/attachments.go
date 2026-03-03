package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"forum/internal/domain"
	"forum/internal/repo"
)

type AttachmentRepo struct {
	db *sql.DB
}

func NewAttachmentRepo(db *sql.DB) *AttachmentRepo {
	return &AttachmentRepo{db: db}
}

func (r *AttachmentRepo) Create(ctx context.Context, ownerUserID int64, mime string, size int64, storageKey, originalName string, createdAt time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
        INSERT INTO attachments (owner_user_id, mime, size, storage_key, original_name, created_at)
        VALUES (?, ?, ?, ?, ?, ?)
    `, ownerUserID, strings.TrimSpace(mime), size, strings.TrimSpace(storageKey), strings.TrimSpace(originalName), timeToUnix(createdAt))
	if err != nil {
		return 0, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (r *AttachmentRepo) GetByID(ctx context.Context, id int64) (*domain.Attachment, error) {
	row := r.db.QueryRowContext(ctx, `
        SELECT id, owner_user_id, mime, size, storage_key, original_name, created_at
        FROM attachments
        WHERE id = ?
    `, id)

	var attachment domain.Attachment
	var createdAt int64
	if err := row.Scan(&attachment.ID, &attachment.OwnerUserID, &attachment.Mime, &attachment.Size, &attachment.StorageKey, &attachment.OriginalName, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, repo.ErrNotFound
		}
		return nil, err
	}
	attachment.Mime = strings.TrimSpace(attachment.Mime)
	attachment.StorageKey = strings.TrimSpace(attachment.StorageKey)
	attachment.OriginalName = strings.TrimSpace(attachment.OriginalName)
	attachment.CreatedAt = unixToTime(createdAt)
	attachment.URL = domain.AttachmentURL(attachment.ID)

	return &attachment, nil
}

func (r *AttachmentRepo) GetUsage(ctx context.Context, id int64) (domain.AttachmentUsage, error) {
	var usage domain.AttachmentUsage

	row := r.db.QueryRowContext(ctx, `
        SELECT id, from_user_id, to_user_id
        FROM private_messages
        WHERE attachment_id = ?
        ORDER BY id ASC
        LIMIT 1
    `, id)
	if err := row.Scan(&usage.PrivateMessageID, &usage.FromUserID, &usage.ToUserID); err == nil {
		return usage, nil
	} else if err != sql.ErrNoRows {
		return domain.AttachmentUsage{}, err
	}

	row = r.db.QueryRowContext(ctx, `
        SELECT id
        FROM posts
        WHERE attachment_id = ?
        ORDER BY id ASC
        LIMIT 1
    `, id)
	if err := row.Scan(&usage.PostID); err == nil {
		return usage, nil
	} else if err != sql.ErrNoRows {
		return domain.AttachmentUsage{}, err
	}

	return domain.AttachmentUsage{}, nil
}

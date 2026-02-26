package sqlite

import (
	"context"
	"database/sql"
)

type ReactionRepo struct {
	db *sql.DB
}

func NewReactionRepo(db *sql.DB) *ReactionRepo {
	return &ReactionRepo{db: db}
}

func (r *ReactionRepo) ReactPost(ctx context.Context, postID, userID int64, value int) error {
	if value == 0 {
		_, err := r.db.ExecContext(ctx, `DELETE FROM post_reactions WHERE post_id = ? AND user_id = ?`, postID, userID)
		return err
	}
	_, err := r.db.ExecContext(ctx, `
        INSERT INTO post_reactions (post_id, user_id, value)
        VALUES (?, ?, ?)
        ON CONFLICT(post_id, user_id) DO UPDATE SET value = excluded.value
    `, postID, userID, value)
	return err
}

func (r *ReactionRepo) ReactComment(ctx context.Context, commentID, userID int64, value int) error {
	if value == 0 {
		_, err := r.db.ExecContext(ctx, `DELETE FROM comment_reactions WHERE comment_id = ? AND user_id = ?`, commentID, userID)
		return err
	}
	_, err := r.db.ExecContext(ctx, `
        INSERT INTO comment_reactions (comment_id, user_id, value)
        VALUES (?, ?, ?)
        ON CONFLICT(comment_id, user_id) DO UPDATE SET value = excluded.value
    `, commentID, userID, value)
	return err
}

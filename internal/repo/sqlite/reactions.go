package sqlite

import (
	"context"
	"database/sql"

	"forum/internal/domain"
	"forum/internal/repo"
	"time"
)

type ReactionRepo struct {
	db *sql.DB
}

func NewReactionRepo(db *sql.DB) *ReactionRepo {
	return &ReactionRepo{db: db}
}

func (r *ReactionRepo) ReactPost(ctx context.Context, postID, userID int64, value int, reactedAt time.Time) (domain.ReactionChange, error) {
	return r.react(ctx, "post_reactions", "post_id", postID, userID, value, reactedAt)
}

func (r *ReactionRepo) ReactComment(ctx context.Context, commentID, userID int64, value int, reactedAt time.Time) (domain.ReactionChange, error) {
	return r.react(ctx, "comment_reactions", "comment_id", commentID, userID, value, reactedAt)
}

func (r *ReactionRepo) react(ctx context.Context, tableName, entityColumn string, entityID, userID int64, value int, reactedAt time.Time) (change domain.ReactionChange, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return change, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	query := "SELECT value FROM " + tableName + " WHERE " + entityColumn + " = ? AND user_id = ?"
	row := tx.QueryRowContext(ctx, query, entityID, userID)
	switch scanErr := row.Scan(&change.PreviousValue); scanErr {
	case nil:
	case sql.ErrNoRows:
		change.PreviousValue = 0
	default:
		err = scanErr
		return change, err
	}

	change.CurrentValue = value
	if value == 0 {
		if _, err = tx.ExecContext(ctx, "DELETE FROM "+tableName+" WHERE "+entityColumn+" = ? AND user_id = ?", entityID, userID); err != nil {
			return change, err
		}
	} else {
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO `+tableName+` (`+entityColumn+`, user_id, value, created_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(`+entityColumn+`, user_id) DO UPDATE SET
				value = excluded.value,
				created_at = excluded.created_at
		`, entityID, userID, value, timeToUnix(reactedAt)); err != nil {
			return change, err
		}
	}

	if err = tx.Commit(); err != nil {
		if err == sql.ErrNoRows {
			err = repo.ErrNotFound
		}
		return change, err
	}

	return change, nil
}

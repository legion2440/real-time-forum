package sqlite

import (
	"context"
	"database/sql"
	"strings"

	"forum/internal/domain"
	"forum/internal/repo"
	forumsearch "forum/internal/search"
)

type CommentRepo struct {
	db             *sql.DB
	searchStrategy forumsearch.CommentSearchStrategy
}

func NewCommentRepo(db *sql.DB) *CommentRepo {
	return NewCommentRepoWithSearch(db, forumsearch.LikeCommentSearchStrategy{})
}

func NewCommentRepoWithSearch(db *sql.DB, strategy forumsearch.CommentSearchStrategy) *CommentRepo {
	if strategy == nil {
		strategy = forumsearch.LikeCommentSearchStrategy{}
	}
	return &CommentRepo{db: db, searchStrategy: strategy}
}

func (r *CommentRepo) Create(ctx context.Context, comment *domain.Comment) (int64, error) {
	var parentID interface{}
	if comment.ParentID != nil && *comment.ParentID > 0 {
		parentID = *comment.ParentID
	}
	res, err := r.db.ExecContext(ctx, `
        INSERT INTO comments (post_id, parent_id, user_id, body, created_at)
        VALUES (?, ?, ?, ?, ?)
    `, comment.PostID, parentID, comment.UserID, comment.Body, timeToUnix(comment.CreatedAt))
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (r *CommentRepo) ListByPost(ctx context.Context, postID int64, filter domain.CommentFilter) ([]domain.Comment, error) {
	var sb strings.Builder
	args := []any{postID}
	sb.WriteString(`
		SELECT c.id, c.post_id, c.parent_id, c.user_id, u.username, u.display_name, c.body, c.created_at,
		       COALESCE(SUM(CASE WHEN cr.value = 1 THEN 1 ELSE 0 END), 0) AS likes,
		       COALESCE(SUM(CASE WHEN cr.value = -1 THEN 1 ELSE 0 END), 0) AS dislikes
		FROM comments c
		JOIN users u ON u.id = c.user_id
		LEFT JOIN comment_reactions cr ON cr.comment_id = c.id
		WHERE c.post_id = ?
	`)
	if r.searchStrategy != nil {
		if clause, clauseArgs := r.searchStrategy.Clause(filter.Query); clause != "" {
			sb.WriteString(" AND ")
			sb.WriteString(clause)
			args = append(args, clauseArgs...)
		}
	}
	sb.WriteString(`
		GROUP BY c.id, c.post_id, c.parent_id, c.user_id, u.username, u.display_name, c.body, c.created_at
		ORDER BY c.created_at ASC
	`)

	rows, err := r.db.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Comment
	for rows.Next() {
		var c domain.Comment
		var created int64
		var parentID sql.NullInt64
		var authorUsername string
		var authorDisplayName sql.NullString
		if err := rows.Scan(&c.ID, &c.PostID, &parentID, &c.UserID, &authorUsername, &authorDisplayName, &c.Body, &created, &c.Likes, &c.Dislikes); err != nil {
			return nil, err
		}
		if parentID.Valid {
			c.ParentID = &parentID.Int64
		}
		c.Author = domain.UserRef{
			ID:          c.UserID,
			Username:    authorUsername,
			DisplayName: strings.TrimSpace(authorDisplayName.String),
		}
		c.CreatedAt = unixToTime(created)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *CommentRepo) GetByID(ctx context.Context, id int64) (*domain.Comment, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT c.id, c.post_id, c.parent_id, c.user_id, u.username, u.display_name, c.body, c.created_at,
		       COALESCE(SUM(CASE WHEN cr.value = 1 THEN 1 ELSE 0 END), 0) AS likes,
		       COALESCE(SUM(CASE WHEN cr.value = -1 THEN 1 ELSE 0 END), 0) AS dislikes
		FROM comments c
		JOIN users u ON u.id = c.user_id
		LEFT JOIN comment_reactions cr ON cr.comment_id = c.id
		WHERE c.id = ?
		GROUP BY c.id, c.post_id, c.parent_id, c.user_id, u.username, u.display_name, c.body, c.created_at
	`, id)

	var c domain.Comment
	var created int64
	var parentID sql.NullInt64
	var authorUsername string
	var authorDisplayName sql.NullString
	if err := row.Scan(&c.ID, &c.PostID, &parentID, &c.UserID, &authorUsername, &authorDisplayName, &c.Body, &created, &c.Likes, &c.Dislikes); err != nil {
		if err == sql.ErrNoRows {
			return nil, repo.ErrNotFound
		}
		return nil, err
	}
	if parentID.Valid {
		c.ParentID = &parentID.Int64
	}
	c.Author = domain.UserRef{
		ID:          c.UserID,
		Username:    authorUsername,
		DisplayName: strings.TrimSpace(authorDisplayName.String),
	}
	c.CreatedAt = unixToTime(created)
	return &c, nil
}

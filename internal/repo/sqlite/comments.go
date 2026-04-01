package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"time"

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
		SELECT c.id, c.post_id, c.parent_id, c.user_id, u.username, u.display_name, u.role, c.body, c.created_at, c.deleted_at, c.deleted_body, c.deleted_by, c.deleted_by_role,
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
		GROUP BY c.id, c.post_id, c.parent_id, c.user_id, u.username, u.display_name, u.role, c.body, c.created_at, c.deleted_at, c.deleted_body, c.deleted_by, c.deleted_by_role
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
		var deletedAt sql.NullInt64
		var authorUsername string
		var authorDisplayName sql.NullString
		var authorRole string
		var deletedBody string
		var deletedBy sql.NullInt64
		var deletedByRole sql.NullString
		if err := rows.Scan(&c.ID, &c.PostID, &parentID, &c.UserID, &authorUsername, &authorDisplayName, &authorRole, &c.Body, &created, &deletedAt, &deletedBody, &deletedBy, &deletedByRole, &c.Likes, &c.Dislikes); err != nil {
			return nil, err
		}
		if parentID.Valid {
			c.ParentID = &parentID.Int64
		}
		if deletedAt.Valid && deletedAt.Int64 > 0 {
			deleted := unixToTime(deletedAt.Int64)
			c.DeletedAt = &deleted
		}
		role := domain.NormalizeUserRole(authorRole)
		c.Author = domain.UserRef{
			ID:          c.UserID,
			Username:    authorUsername,
			DisplayName: strings.TrimSpace(authorDisplayName.String),
			Role:        role,
			Badges:      domain.StaffBadgesForRole(role),
		}
		c.CreatedAt = unixToTime(created)
		c.DeletedBody = strings.TrimSpace(deletedBody)
		if deletedBy.Valid && deletedBy.Int64 > 0 {
			c.DeletedByUserID = &deletedBy.Int64
		}
		c.DeletedByRole = domain.NormalizeUserRole(strings.TrimSpace(deletedByRole.String))
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return filterFullyDeletedCommentThreads(out), nil
}

func (r *CommentRepo) GetByID(ctx context.Context, id int64) (*domain.Comment, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT c.id, c.post_id, c.parent_id, c.user_id, u.username, u.display_name, u.role, c.body, c.created_at, c.deleted_at, c.deleted_body, c.deleted_by, c.deleted_by_role,
		       COALESCE(SUM(CASE WHEN cr.value = 1 THEN 1 ELSE 0 END), 0) AS likes,
		       COALESCE(SUM(CASE WHEN cr.value = -1 THEN 1 ELSE 0 END), 0) AS dislikes
		FROM comments c
		JOIN users u ON u.id = c.user_id
		LEFT JOIN comment_reactions cr ON cr.comment_id = c.id
		WHERE c.id = ?
		GROUP BY c.id, c.post_id, c.parent_id, c.user_id, u.username, u.display_name, u.role, c.body, c.created_at, c.deleted_at, c.deleted_body, c.deleted_by, c.deleted_by_role
	`, id)

	var c domain.Comment
	var created int64
	var parentID sql.NullInt64
	var deletedAt sql.NullInt64
	var authorUsername string
	var authorDisplayName sql.NullString
	var authorRole string
	var deletedBody string
	var deletedBy sql.NullInt64
	var deletedByRole sql.NullString
	if err := row.Scan(&c.ID, &c.PostID, &parentID, &c.UserID, &authorUsername, &authorDisplayName, &authorRole, &c.Body, &created, &deletedAt, &deletedBody, &deletedBy, &deletedByRole, &c.Likes, &c.Dislikes); err != nil {
		if err == sql.ErrNoRows {
			return nil, repo.ErrNotFound
		}
		return nil, err
	}
	if parentID.Valid {
		c.ParentID = &parentID.Int64
	}
	if deletedAt.Valid && deletedAt.Int64 > 0 {
		deleted := unixToTime(deletedAt.Int64)
		c.DeletedAt = &deleted
	}
	role := domain.NormalizeUserRole(authorRole)
	c.Author = domain.UserRef{
		ID:          c.UserID,
		Username:    authorUsername,
		DisplayName: strings.TrimSpace(authorDisplayName.String),
		Role:        role,
		Badges:      domain.StaffBadgesForRole(role),
	}
	c.CreatedAt = unixToTime(created)
	c.DeletedBody = strings.TrimSpace(deletedBody)
	if deletedBy.Valid && deletedBy.Int64 > 0 {
		c.DeletedByUserID = &deletedBy.Int64
	}
	c.DeletedByRole = domain.NormalizeUserRole(strings.TrimSpace(deletedByRole.String))
	return &c, nil
}

func (r *CommentRepo) HasDescendants(ctx context.Context, id int64) (bool, error) {
	row := r.db.QueryRowContext(ctx, `
		WITH RECURSIVE descendants(id) AS (
			SELECT id
			FROM comments
			WHERE parent_id = ?

			UNION ALL

			SELECT c.id
			FROM comments c
			JOIN descendants d ON c.parent_id = d.id
		)
		SELECT 1
		FROM descendants
		LIMIT 1
	`, id)

	var marker int
	if err := row.Scan(&marker); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return marker == 1, nil
}

func (r *CommentRepo) HasActiveThreadComments(ctx context.Context, rootID int64) (bool, error) {
	row := r.db.QueryRowContext(ctx, `
		WITH RECURSIVE thread(id, deleted_at) AS (
			SELECT id, deleted_at
			FROM comments
			WHERE id = ?

			UNION ALL

			SELECT c.id, c.deleted_at
			FROM comments c
			JOIN thread t ON c.parent_id = t.id
		)
		SELECT 1
		FROM thread
		WHERE deleted_at IS NULL
		LIMIT 1
	`, rootID)

	var marker int
	if err := row.Scan(&marker); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return marker == 1, nil
}

func (r *CommentRepo) SoftDelete(ctx context.Context, id int64, deletedAt time.Time, actorUserID int64, actorRole domain.UserRole) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE comments
		SET deleted_body = CASE WHEN deleted_body = '' THEN body ELSE deleted_body END,
		    body = ?,
		    deleted_at = ?,
		    deleted_by = ?,
		    deleted_by_role = ?
		WHERE id = ? AND deleted_at IS NULL
	`, "[deleted]", timeToUnix(deletedAt), actorUserID, strings.TrimSpace(string(actorRole)), id)
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
	return nil
}

func (r *CommentRepo) Restore(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE comments
		SET body = CASE WHEN deleted_body <> '' THEN deleted_body ELSE body END,
		    deleted_body = '',
		    deleted_at = NULL,
		    deleted_by = NULL,
		    deleted_by_role = ''
		WHERE id = ? AND deleted_at IS NOT NULL
	`, id)
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
	return nil
}

func (r *CommentRepo) Update(ctx context.Context, comment *domain.Comment) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE comments
		SET body = ?
		WHERE id = ?
	`, comment.Body, comment.ID)
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
	return nil
}

func (r *CommentRepo) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM comments WHERE id = ?`, id)
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
	return nil
}

func filterFullyDeletedCommentThreads(comments []domain.Comment) []domain.Comment {
	if len(comments) == 0 {
		return comments
	}

	byID := make(map[int64]*domain.Comment, len(comments))
	for i := range comments {
		byID[comments[i].ID] = &comments[i]
	}

	rootCache := make(map[int64]int64, len(comments))
	var findRoot func(id int64) int64
	findRoot = func(id int64) int64 {
		if id <= 0 {
			return 0
		}
		if cached, ok := rootCache[id]; ok {
			return cached
		}
		comment := byID[id]
		if comment == nil || comment.ParentID == nil || *comment.ParentID <= 0 || *comment.ParentID == comment.ID {
			rootCache[id] = id
			return id
		}
		parent := byID[*comment.ParentID]
		if parent == nil {
			rootCache[id] = id
			return id
		}
		rootID := findRoot(parent.ID)
		if rootID <= 0 {
			rootID = id
		}
		rootCache[id] = rootID
		return rootID
	}

	hasActiveByRoot := make(map[int64]bool, len(comments))
	for i := range comments {
		rootID := findRoot(comments[i].ID)
		if rootID > 0 && comments[i].DeletedAt == nil {
			hasActiveByRoot[rootID] = true
		}
	}

	filtered := make([]domain.Comment, 0, len(comments))
	for i := range comments {
		rootID := findRoot(comments[i].ID)
		root := byID[rootID]
		if root != nil && root.DeletedAt != nil && !hasActiveByRoot[rootID] {
			continue
		}
		filtered = append(filtered, comments[i])
	}
	return filtered
}

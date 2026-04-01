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

type PostRepo struct {
	db             *sql.DB
	searchStrategy forumsearch.PostSearchStrategy
}

func NewPostRepo(db *sql.DB) *PostRepo {
	return NewPostRepoWithSearch(db, forumsearch.LikePostSearchStrategy{})
}

func NewPostRepoWithSearch(db *sql.DB, strategy forumsearch.PostSearchStrategy) *PostRepo {
	if strategy == nil {
		strategy = forumsearch.LikePostSearchStrategy{}
	}
	return &PostRepo{db: db, searchStrategy: strategy}
}

func (r *PostRepo) Create(ctx context.Context, post *domain.Post, categoryIDs []int64) (int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	res, err := tx.ExecContext(ctx, `
        INSERT INTO posts (user_id, title, body, attachment_id, is_under_review, created_at)
        VALUES (?, ?, ?, ?, ?, ?)
    `, post.UserID, post.Title, post.Body, nullableAttachmentID(post.Attachment), 1, timeToUnix(post.CreatedAt))
	if err != nil {
		return 0, err
	}

	postID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	if len(categoryIDs) > 0 {
		stmt, err := tx.PrepareContext(ctx, `
            INSERT INTO post_categories (post_id, category_id) VALUES (?, ?)
        `)
		if err != nil {
			return 0, err
		}
		defer stmt.Close()

		for _, catID := range categoryIDs {
			if _, err := stmt.ExecContext(ctx, postID, catID); err != nil {
				return 0, err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return postID, nil
}

func (r *PostRepo) Exists(ctx context.Context, id int64) (bool, error) {
	row := r.db.QueryRowContext(ctx, `SELECT 1 FROM posts WHERE id = ?`, id)
	var tmp int
	if err := row.Scan(&tmp); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *PostRepo) ListUnderReview(ctx context.Context) ([]domain.Post, error) {
	return r.listByQuery(ctx, `
		SELECT p.id, p.user_id, u.username, u.display_name, u.role, p.title, p.body,
		       a.id, a.mime, a.size,
		       p.created_at, p.is_under_review, p.approved_at, p.delete_protected, p.deleted_at,
		       au.id, au.username, au.display_name, au.role,
		       du.id, du.username, du.display_name, du.role,
		       COALESCE(SUM(CASE WHEN pr.value = 1 THEN 1 ELSE 0 END), 0) AS likes,
		       COALESCE(SUM(CASE WHEN pr.value = -1 THEN 1 ELSE 0 END), 0) AS dislikes,
		       (SELECT COUNT(1) FROM comments c WHERE c.post_id = p.id) AS comments_count
		FROM posts p
		JOIN users u ON u.id = p.user_id
		LEFT JOIN attachments a ON a.id = p.attachment_id
		LEFT JOIN users au ON au.id = p.approved_by
		LEFT JOIN users du ON du.id = p.deleted_by
		LEFT JOIN post_reactions pr ON pr.post_id = p.id
		WHERE p.is_under_review = 1 AND p.deleted_at IS NULL
		GROUP BY p.id, p.user_id, u.username, u.display_name, u.role, p.title, p.body,
		         a.id, a.mime, a.size, p.created_at, p.is_under_review, p.approved_at, p.delete_protected, p.deleted_at,
		         au.id, au.username, au.display_name, au.role,
		         du.id, du.username, du.display_name, du.role
		ORDER BY p.created_at DESC, p.id DESC
	`, nil)
}

func (r *PostRepo) Update(ctx context.Context, post *domain.Post, categoryIDs []int64) (err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	res, err := tx.ExecContext(ctx, `
		UPDATE posts
		SET title = ?, body = ?
		WHERE id = ?
	`, post.Title, post.Body, post.ID)
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

	if _, err := tx.ExecContext(ctx, `DELETE FROM post_categories WHERE post_id = ?`, post.ID); err != nil {
		return err
	}

	if len(categoryIDs) > 0 {
		stmt, err := tx.PrepareContext(ctx, `INSERT INTO post_categories (post_id, category_id) VALUES (?, ?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for _, categoryID := range categoryIDs {
			if _, err := stmt.ExecContext(ctx, post.ID, categoryID); err != nil {
				return err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (r *PostRepo) UpdateCategories(ctx context.Context, postID int64, categoryIDs []int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(ctx, `DELETE FROM post_categories WHERE post_id = ?`, postID); err != nil {
		return err
	}
	for _, categoryID := range categoryIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO post_categories (post_id, category_id) VALUES (?, ?)`, postID, categoryID); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (r *PostRepo) Approve(ctx context.Context, postID, approvedByUserID int64, approvedAt time.Time, categoryIDs []int64, updateCategories bool) (err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	res, err := tx.ExecContext(ctx, `
		UPDATE posts
		SET is_under_review = 0,
		    approved_by = ?,
		    approved_at = ?
		WHERE id = ? AND deleted_at IS NULL
	`, approvedByUserID, timeToUnix(approvedAt), postID)
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
	if updateCategories {
		if _, err := tx.ExecContext(ctx, `DELETE FROM post_categories WHERE post_id = ?`, postID); err != nil {
			return err
		}
		for _, categoryID := range categoryIDs {
			if _, err := tx.ExecContext(ctx, `INSERT INTO post_categories (post_id, category_id) VALUES (?, ?)`, postID, categoryID); err != nil {
				return err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (r *PostRepo) SoftDelete(ctx context.Context, id, actorUserID int64, actorRole domain.UserRole, deletedAt time.Time) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE posts
		SET deleted_at = ?, deleted_by = ?, deleted_by_role = ?
		WHERE id = ? AND deleted_at IS NULL AND delete_protected = 0
	`, timeToUnix(deletedAt), actorUserID, strings.TrimSpace(string(actorRole)), id)
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

func (r *PostRepo) Restore(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE posts
		SET deleted_at = NULL, deleted_by = NULL, deleted_by_role = ''
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

func (r *PostRepo) SetDeleteProtection(ctx context.Context, id int64, protected bool) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE posts
		SET delete_protected = ?
		WHERE id = ?
	`, boolToInt(protected), id)
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

func (r *PostRepo) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM posts WHERE id = ?`, id)
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

func (r *PostRepo) GetByID(ctx context.Context, id int64) (*domain.Post, error) {
	posts, err := r.listByQuery(ctx, `
		SELECT p.id, p.user_id, u.username, u.display_name, u.role, p.title, p.body,
		       a.id, a.mime, a.size,
		       p.created_at, p.is_under_review, p.approved_at, p.delete_protected, p.deleted_at,
		       au.id, au.username, au.display_name, au.role,
		       du.id, du.username, du.display_name, du.role,
		       COALESCE(SUM(CASE WHEN pr.value = 1 THEN 1 ELSE 0 END), 0) AS likes,
		       COALESCE(SUM(CASE WHEN pr.value = -1 THEN 1 ELSE 0 END), 0) AS dislikes,
		       (SELECT COUNT(1) FROM comments c WHERE c.post_id = p.id) AS comments_count
		FROM posts p
		JOIN users u ON u.id = p.user_id
		LEFT JOIN attachments a ON a.id = p.attachment_id
		LEFT JOIN users au ON au.id = p.approved_by
		LEFT JOIN users du ON du.id = p.deleted_by
		LEFT JOIN post_reactions pr ON pr.post_id = p.id
		WHERE p.id = ?
		GROUP BY p.id, p.user_id, u.username, u.display_name, u.role, p.title, p.body,
		         a.id, a.mime, a.size, p.created_at, p.is_under_review, p.approved_at, p.delete_protected, p.deleted_at,
		         au.id, au.username, au.display_name, au.role,
		         du.id, du.username, du.display_name, du.role
	`, []any{id})
	if err != nil {
		return nil, err
	}
	if len(posts) == 0 {
		return nil, repo.ErrNotFound
	}
	return &posts[0], nil
}

func (r *PostRepo) List(ctx context.Context, filter domain.PostFilter) ([]domain.Post, error) {
	var sb strings.Builder
	var args []any

	sb.WriteString(`
		SELECT p.id, p.user_id, u.username, u.display_name, u.role, p.title, p.body,
		       a.id, a.mime, a.size,
		       p.created_at, p.is_under_review, p.approved_at, p.delete_protected, p.deleted_at,
		       au.id, au.username, au.display_name, au.role,
		       du.id, du.username, du.display_name, du.role,
		       COALESCE(SUM(CASE WHEN pr.value = 1 THEN 1 ELSE 0 END), 0) AS likes,
		       COALESCE(SUM(CASE WHEN pr.value = -1 THEN 1 ELSE 0 END), 0) AS dislikes,
		       (SELECT COUNT(1) FROM comments c WHERE c.post_id = p.id) AS comments_count
		FROM posts p
		JOIN users u ON u.id = p.user_id
		LEFT JOIN attachments a ON a.id = p.attachment_id
		LEFT JOIN users au ON au.id = p.approved_by
		LEFT JOIN users du ON du.id = p.deleted_by
		LEFT JOIN post_reactions pr ON pr.post_id = p.id
		WHERE 1 = 1
	`)

	if filter.Mine && filter.UserID != nil {
		sb.WriteString(" AND p.user_id = ?")
		args = append(args, *filter.UserID)
	}

	if filter.Liked && filter.UserID != nil {
		sb.WriteString(" AND EXISTS (SELECT 1 FROM post_reactions pr2 WHERE pr2.post_id = p.id AND pr2.user_id = ? AND pr2.value = 1)")
		args = append(args, *filter.UserID)
	}

	if len(filter.CategoryIDs) > 0 {
		sb.WriteString(" AND EXISTS (SELECT 1 FROM post_categories pc WHERE pc.post_id = p.id AND pc.category_id IN (")
		sb.WriteString(placeholders(len(filter.CategoryIDs)))
		sb.WriteString("))")
		for _, id := range filter.CategoryIDs {
			args = append(args, id)
		}
	}

	if r.searchStrategy != nil {
		if clause, clauseArgs := r.searchStrategy.Clause(filter.Query); clause != "" {
			sb.WriteString(" AND ")
			sb.WriteString(clause)
			args = append(args, clauseArgs...)
		}
	}

	sb.WriteString(`
		GROUP BY p.id, p.user_id, u.username, u.display_name, u.role, p.title, p.body,
		         a.id, a.mime, a.size, p.created_at, p.is_under_review, p.approved_at, p.delete_protected, p.deleted_at,
		         au.id, au.username, au.display_name, au.role,
		         du.id, du.username, du.display_name, du.role
		ORDER BY p.created_at DESC, p.id DESC
	`)

	return r.listByQuery(ctx, sb.String(), args)
}

func (r *PostRepo) fetchCategories(ctx context.Context, postIDs []int64) (map[int64][]domain.Category, error) {
	out := make(map[int64][]domain.Category)
	if len(postIDs) == 0 {
		return out, nil
	}

	query := `
        SELECT pc.post_id, c.id, c.code, c.name, c.is_system
        FROM post_categories pc
        JOIN categories c ON c.id = pc.category_id
        WHERE pc.post_id IN (` + placeholders(len(postIDs)) + `)
        ORDER BY c.is_system DESC, c.name ASC
    `

	args := make([]interface{}, 0, len(postIDs))
	for _, id := range postIDs {
		args = append(args, id)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var postID int64
		var c domain.Category
		var isSystem int
		if err := rows.Scan(&postID, &c.ID, &c.Code, &c.Name, &isSystem); err != nil {
			return nil, err
		}
		c.Code = strings.TrimSpace(c.Code)
		c.Name = strings.TrimSpace(c.Name)
		c.IsSystem = isSystem != 0
		out[postID] = append(out[postID], c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return out, nil
}

func (r *PostRepo) listByQuery(ctx context.Context, query string, args []any) ([]domain.Post, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []domain.Post
	var ids []int64
	for rows.Next() {
		post, err := scanPostRow(rows)
		if err != nil {
			return nil, err
		}
		posts = append(posts, *post)
		ids = append(ids, post.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(posts) == 0 {
		return posts, nil
	}
	categories, err := r.fetchCategories(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range posts {
		posts[i].Categories = categories[posts[i].ID]
	}
	return posts, nil
}

func scanPostRow(s scanner) (*domain.Post, error) {
	var (
		post                    domain.Post
		createdAt               int64
		underReview             int
		approvedAt              sql.NullInt64
		deleteProtected         int
		deletedAt               sql.NullInt64
		authorUsername          string
		authorDisplayName       sql.NullString
		authorRole              string
		attachmentID            sql.NullInt64
		attachmentMime          sql.NullString
		attachmentSize          sql.NullInt64
		approvedByID            sql.NullInt64
		approvedByUsername      sql.NullString
		approvedByDisplayName   sql.NullString
		approvedByRole          sql.NullString
		deletedByID             sql.NullInt64
		deletedByUsername       sql.NullString
		deletedByDisplayName    sql.NullString
		deletedByRole           sql.NullString
	)
	if err := s.Scan(
		&post.ID, &post.UserID, &authorUsername, &authorDisplayName, &authorRole, &post.Title, &post.Body,
		&attachmentID, &attachmentMime, &attachmentSize,
		&createdAt, &underReview, &approvedAt, &deleteProtected, &deletedAt,
		&approvedByID, &approvedByUsername, &approvedByDisplayName, &approvedByRole,
		&deletedByID, &deletedByUsername, &deletedByDisplayName, &deletedByRole,
		&post.Likes, &post.Dislikes, &post.CommentsCount,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, repo.ErrNotFound
		}
		return nil, err
	}

	post.Author = domain.UserRef{
		ID:          post.UserID,
		Username:    authorUsername,
		DisplayName: strings.TrimSpace(authorDisplayName.String),
		Role:        domain.NormalizeUserRole(authorRole),
		Badges:      domain.StaffBadgesForRole(domain.NormalizeUserRole(authorRole)),
	}
	post.Attachment = attachmentFromNullableFields(attachmentID, attachmentMime, attachmentSize)
	post.CreatedAt = unixToTime(createdAt)
	post.UnderReview = underReview != 0
	post.DeleteProtected = deleteProtected != 0
	if approvedAt.Valid && approvedAt.Int64 > 0 {
		value := unixToTime(approvedAt.Int64)
		post.ApprovedAt = &value
	}
	if approvedByID.Valid && approvedByID.Int64 > 0 {
		role := domain.NormalizeUserRole(strings.TrimSpace(approvedByRole.String))
		post.ApprovedBy = &domain.UserRef{
			ID:          approvedByID.Int64,
			Username:    strings.TrimSpace(approvedByUsername.String),
			DisplayName: strings.TrimSpace(approvedByDisplayName.String),
			Role:        role,
			Badges:      domain.StaffBadgesForRole(role),
		}
	}
	if deletedAt.Valid && deletedAt.Int64 > 0 {
		value := unixToTime(deletedAt.Int64)
		post.DeletedAt = &value
	}
	if deletedByID.Valid && deletedByID.Int64 > 0 {
		role := domain.NormalizeUserRole(strings.TrimSpace(deletedByRole.String))
		post.DeletedBy = &domain.UserRef{
			ID:          deletedByID.Int64,
			Username:    strings.TrimSpace(deletedByUsername.String),
			DisplayName: strings.TrimSpace(deletedByDisplayName.String),
			Role:        role,
			Badges:      domain.StaffBadgesForRole(role),
		}
	}
	return &post, nil
}

package sqlite

import (
	"context"
	"database/sql"
	"strings"

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
        INSERT INTO posts (user_id, title, body, attachment_id, created_at)
        VALUES (?, ?, ?, ?, ?)
    `, post.UserID, post.Title, post.Body, nullableAttachmentID(post.Attachment), timeToUnix(post.CreatedAt))
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
	row := r.db.QueryRowContext(ctx, `
		SELECT p.id, p.user_id, u.username, u.display_name, p.title, p.body,
		       a.id, a.mime, a.size,
		       p.created_at,
		       COALESCE(SUM(CASE WHEN pr.value = 1 THEN 1 ELSE 0 END), 0) AS likes,
		       COALESCE(SUM(CASE WHEN pr.value = -1 THEN 1 ELSE 0 END), 0) AS dislikes,
		       (SELECT COUNT(1) FROM comments c WHERE c.post_id = p.id) AS comments_count
		FROM posts p
		JOIN users u ON u.id = p.user_id
		LEFT JOIN attachments a ON a.id = p.attachment_id
		LEFT JOIN post_reactions pr ON pr.post_id = p.id
		WHERE p.id = ?
		GROUP BY p.id, p.user_id, u.username, u.display_name, p.title, p.body, a.id, a.mime, a.size, p.created_at
	`, id)

	var post domain.Post
	var created int64
	var authorUsername string
	var authorDisplayName sql.NullString
	var attachmentID sql.NullInt64
	var attachmentMime sql.NullString
	var attachmentSize sql.NullInt64
	if err := row.Scan(&post.ID, &post.UserID, &authorUsername, &authorDisplayName, &post.Title, &post.Body, &attachmentID, &attachmentMime, &attachmentSize, &created, &post.Likes, &post.Dislikes, &post.CommentsCount); err != nil {
		if err == sql.ErrNoRows {
			return nil, repo.ErrNotFound
		}
		return nil, err
	}
	post.Author = domain.UserRef{
		ID:          post.UserID,
		Username:    authorUsername,
		DisplayName: strings.TrimSpace(authorDisplayName.String),
	}
	post.Attachment = attachmentFromNullableFields(attachmentID, attachmentMime, attachmentSize)
	post.CreatedAt = unixToTime(created)

	categories, err := r.fetchCategories(ctx, []int64{post.ID})
	if err != nil {
		return nil, err
	}
	post.Categories = categories[post.ID]

	return &post, nil
}

func (r *PostRepo) List(ctx context.Context, filter domain.PostFilter) ([]domain.Post, error) {
	var sb strings.Builder
	var args []any

	sb.WriteString(`
		SELECT p.id, p.user_id, u.username, u.display_name, p.title, p.body,
		       a.id, a.mime, a.size,
		       p.created_at,
		       COALESCE(SUM(CASE WHEN pr.value = 1 THEN 1 ELSE 0 END), 0) AS likes,
		       COALESCE(SUM(CASE WHEN pr.value = -1 THEN 1 ELSE 0 END), 0) AS dislikes,
		       (SELECT COUNT(1) FROM comments c WHERE c.post_id = p.id) AS comments_count
		FROM posts p
		JOIN users u ON u.id = p.user_id
		LEFT JOIN attachments a ON a.id = p.attachment_id
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

	sb.WriteString(" GROUP BY p.id, p.user_id, u.username, u.display_name, p.title, p.body, a.id, a.mime, a.size, p.created_at ORDER BY p.created_at DESC")

	rows, err := r.db.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []domain.Post
	var ids []int64
	for rows.Next() {
		var p domain.Post
		var created int64
		var authorUsername string
		var authorDisplayName sql.NullString
		var attachmentID sql.NullInt64
		var attachmentMime sql.NullString
		var attachmentSize sql.NullInt64
		if err := rows.Scan(&p.ID, &p.UserID, &authorUsername, &authorDisplayName, &p.Title, &p.Body, &attachmentID, &attachmentMime, &attachmentSize, &created, &p.Likes, &p.Dislikes, &p.CommentsCount); err != nil {
			return nil, err
		}
		p.Author = domain.UserRef{
			ID:          p.UserID,
			Username:    authorUsername,
			DisplayName: strings.TrimSpace(authorDisplayName.String),
		}
		p.Attachment = attachmentFromNullableFields(attachmentID, attachmentMime, attachmentSize)
		p.CreatedAt = unixToTime(created)
		posts = append(posts, p)
		ids = append(ids, p.ID)
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

func (r *PostRepo) fetchCategories(ctx context.Context, postIDs []int64) (map[int64][]domain.Category, error) {
	out := make(map[int64][]domain.Category)
	if len(postIDs) == 0 {
		return out, nil
	}

	query := `
        SELECT pc.post_id, c.id, c.name
        FROM post_categories pc
        JOIN categories c ON c.id = pc.category_id
        WHERE pc.post_id IN (` + placeholders(len(postIDs)) + `)
        ORDER BY c.name ASC
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
		if err := rows.Scan(&postID, &c.ID, &c.Name); err != nil {
			return nil, err
		}
		out[postID] = append(out[postID], c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return out, nil
}

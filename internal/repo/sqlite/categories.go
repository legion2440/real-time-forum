package sqlite

import (
	"context"
	"database/sql"
	"strings"

	"forum/internal/domain"
	"forum/internal/repo"
)

type CategoryRepo struct {
	db *sql.DB
}

func NewCategoryRepo(db *sql.DB) *CategoryRepo {
	return &CategoryRepo{db: db}
}

func (r *CategoryRepo) List(ctx context.Context) ([]domain.Category, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, code, name, is_system FROM categories ORDER BY is_system DESC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Category
	for rows.Next() {
		var c domain.Category
		var isSystem int
		if err := rows.Scan(&c.ID, &c.Code, &c.Name, &isSystem); err != nil {
			return nil, err
		}
		c.Code = strings.TrimSpace(c.Code)
		c.Name = strings.TrimSpace(c.Name)
		c.IsSystem = isSystem != 0
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *CategoryRepo) GetByID(ctx context.Context, id int64) (*domain.Category, error) {
	row := r.db.QueryRowContext(ctx, `SELECT id, code, name, is_system FROM categories WHERE id = ?`, id)
	return scanCategory(row)
}

func (r *CategoryRepo) GetByCode(ctx context.Context, code string) (*domain.Category, error) {
	row := r.db.QueryRowContext(ctx, `SELECT id, code, name, is_system FROM categories WHERE code = ?`, strings.TrimSpace(code))
	return scanCategory(row)
}

func (r *CategoryRepo) Create(ctx context.Context, category *domain.Category) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		INSERT INTO categories(code, name, is_system)
		VALUES (?, ?, ?)
	`, strings.TrimSpace(category.Code), strings.TrimSpace(category.Name), boolToInt(category.IsSystem))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (r *CategoryRepo) Delete(ctx context.Context, categoryID int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM categories WHERE id = ?`, categoryID)
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

func (r *CategoryRepo) DeleteAndMovePostsToCategory(ctx context.Context, categoryID, fallbackCategoryID int64) (int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	rows, err := tx.QueryContext(ctx, `
		SELECT DISTINCT post_id
		FROM post_categories
		WHERE category_id = ?
	`, categoryID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var postIDs []int64
	for rows.Next() {
		var postID int64
		if err := rows.Scan(&postID); err != nil {
			return 0, err
		}
		postIDs = append(postIDs, postID)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM post_categories WHERE category_id = ?`, categoryID); err != nil {
		return 0, err
	}
	for _, postID := range postIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO post_categories(post_id, category_id)
			VALUES (?, ?)
		`, postID, fallbackCategoryID); err != nil {
			return 0, err
		}
	}

	res, err := tx.ExecContext(ctx, `DELETE FROM categories WHERE id = ?`, categoryID)
	if err != nil {
		return 0, err
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if rowsAffected == 0 {
		return 0, repo.ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int64(len(postIDs)), nil
}

func scanCategory(s scanner) (*domain.Category, error) {
	var (
		category domain.Category
		isSystem int
	)
	if err := s.Scan(&category.ID, &category.Code, &category.Name, &isSystem); err != nil {
		if err == sql.ErrNoRows {
			return nil, repo.ErrNotFound
		}
		return nil, err
	}
	category.Code = strings.TrimSpace(category.Code)
	category.Name = strings.TrimSpace(category.Name)
	category.IsSystem = isSystem != 0
	return &category, nil
}

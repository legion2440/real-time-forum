package sqlite

import (
	"context"
	"database/sql"
	"strings"

	"forum/internal/domain"
	"forum/internal/repo"
)

type UserRepo struct {
	db *sql.DB
}

func NewUserRepo(db *sql.DB) *UserRepo {
	return &UserRepo{db: db}
}

func (r *UserRepo) Create(ctx context.Context, user *domain.User) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
        INSERT INTO users (email, username, pass_hash, created_at)
        VALUES (?, ?, ?, ?)
    `, user.Email, user.Username, user.PassHash, timeToUnix(user.CreatedAt))
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (r *UserRepo) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	row := r.db.QueryRowContext(ctx, `
        SELECT id, email, username, display_name, pass_hash, created_at, profile_initialized
        FROM users
        WHERE email = ?
    `, email)

	return scanUser(row)
}

func (r *UserRepo) GetByUsername(ctx context.Context, username string) (*domain.User, error) {
	row := r.db.QueryRowContext(ctx, `
        SELECT id, email, username, display_name, pass_hash, created_at, profile_initialized
        FROM users
        WHERE username = ?
    `, username)

	return scanUser(row)
}

func (r *UserRepo) GetByUsernameCI(ctx context.Context, username string) (*domain.User, error) {
	row := r.db.QueryRowContext(ctx, `
        SELECT id, email, username, display_name, pass_hash, created_at, profile_initialized
        FROM users
        WHERE username = ? COLLATE NOCASE
        ORDER BY id ASC
        LIMIT 1
    `, username)

	return scanUser(row)
}

func (r *UserRepo) GetByID(ctx context.Context, id int64) (*domain.User, error) {
	row := r.db.QueryRowContext(ctx, `
        SELECT id, email, username, display_name, pass_hash, created_at, profile_initialized
        FROM users
        WHERE id = ?
    `, id)

	return scanUser(row)
}

func (r *UserRepo) GetByDisplayNameCI(ctx context.Context, displayName string) (*domain.User, error) {
	row := r.db.QueryRowContext(ctx, `
        SELECT id, email, username, display_name, pass_hash, created_at, profile_initialized
        FROM users
        WHERE display_name = ? COLLATE NOCASE
          AND display_name IS NOT NULL
          AND display_name <> ''
        ORDER BY id ASC
        LIMIT 1
    `, displayName)

	return scanUser(row)
}

func (r *UserRepo) GetPublicByUsername(ctx context.Context, username string) (*domain.User, error) {
	row := r.db.QueryRowContext(ctx, `
        SELECT id, username, display_name
        FROM users
        WHERE username = ?
    `, username)

	var (
		user        domain.User
		displayName sql.NullString
	)
	if err := row.Scan(&user.ID, &user.Username, &displayName); err != nil {
		if err == sql.ErrNoRows {
			return nil, repo.ErrNotFound
		}
		return nil, err
	}
	user.DisplayName = strings.TrimSpace(displayName.String)
	return &user, nil
}

func (r *UserRepo) UpdateProfile(ctx context.Context, userID int64, displayName *string, profileInitialized bool) error {
	var normalizedDisplayName any
	if displayName != nil {
		trimmed := strings.TrimSpace(*displayName)
		if trimmed != "" {
			normalizedDisplayName = trimmed
		}
	}

	res, err := r.db.ExecContext(ctx, `
        UPDATE users
        SET display_name = ?, profile_initialized = ?
        WHERE id = ?
    `, normalizedDisplayName, boolToInt(profileInitialized), userID)
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

func (r *UserRepo) List(ctx context.Context) ([]domain.User, error) {
	rows, err := r.db.QueryContext(ctx, `
        SELECT id, email, username, display_name, pass_hash, created_at, profile_initialized
        FROM users
        ORDER BY id ASC
    `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]domain.User, 0)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *user)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return users, nil
}

func (r *UserRepo) ListPublic(ctx context.Context) ([]domain.User, error) {
	rows, err := r.db.QueryContext(ctx, `
        SELECT id, username, display_name
        FROM users
        ORDER BY id ASC
    `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]domain.User, 0)
	for rows.Next() {
		var user domain.User
		var displayName sql.NullString
		if err := rows.Scan(&user.ID, &user.Username, &displayName); err != nil {
			return nil, err
		}
		user.DisplayName = strings.TrimSpace(displayName.String)
		users = append(users, user)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return users, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanUser(s scanner) (*domain.User, error) {
	var (
		user               domain.User
		displayName        sql.NullString
		createdAtUnix      int64
		profileInitialized int
	)
	if err := s.Scan(&user.ID, &user.Email, &user.Username, &displayName, &user.PassHash, &createdAtUnix, &profileInitialized); err != nil {
		if err == sql.ErrNoRows {
			return nil, repo.ErrNotFound
		}
		return nil, err
	}

	user.DisplayName = strings.TrimSpace(displayName.String)
	user.CreatedAt = unixToTime(createdAtUnix)
	user.ProfileInitialized = profileInitialized != 0
	return &user, nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

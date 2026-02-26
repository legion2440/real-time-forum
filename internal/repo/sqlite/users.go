package sqlite

import (
	"context"
	"database/sql"

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
        SELECT id, email, username, pass_hash, created_at
        FROM users
        WHERE email = ?
    `, email)

	var u domain.User
	var created int64
	if err := row.Scan(&u.ID, &u.Email, &u.Username, &u.PassHash, &created); err != nil {
		if err == sql.ErrNoRows {
			return nil, repo.ErrNotFound
		}
		return nil, err
	}
	u.CreatedAt = unixToTime(created)
	return &u, nil
}

func (r *UserRepo) GetByUsername(ctx context.Context, username string) (*domain.User, error) {
	row := r.db.QueryRowContext(ctx, `
        SELECT id, email, username, pass_hash, created_at
        FROM users
        WHERE username = ?
    `, username)

	var u domain.User
	var created int64
	if err := row.Scan(&u.ID, &u.Email, &u.Username, &u.PassHash, &created); err != nil {
		if err == sql.ErrNoRows {
			return nil, repo.ErrNotFound
		}
		return nil, err
	}
	u.CreatedAt = unixToTime(created)
	return &u, nil
}

func (r *UserRepo) GetByID(ctx context.Context, id int64) (*domain.User, error) {
	row := r.db.QueryRowContext(ctx, `
        SELECT id, email, username, pass_hash, created_at
        FROM users
        WHERE id = ?
    `, id)

	var u domain.User
	var created int64
	if err := row.Scan(&u.ID, &u.Email, &u.Username, &u.PassHash, &created); err != nil {
		if err == sql.ErrNoRows {
			return nil, repo.ErrNotFound
		}
		return nil, err
	}
	u.CreatedAt = unixToTime(created)
	return &u, nil
}

package sqlite

import (
	"context"
	"database/sql"

	"forum/internal/domain"
	"forum/internal/repo"
)

type SessionRepo struct {
	db *sql.DB
}

func NewSessionRepo(db *sql.DB) *SessionRepo {
	return &SessionRepo{db: db}
}

func (r *SessionRepo) Create(ctx context.Context, session *domain.Session) error {
	_, err := r.db.ExecContext(ctx, `
        INSERT INTO sessions (token, user_id, expires_at)
        VALUES (?, ?, ?)
    `, session.Token, session.UserID, timeToUnix(session.ExpiresAt))
	return err
}

func (r *SessionRepo) GetByToken(ctx context.Context, token string) (*domain.Session, error) {
	row := r.db.QueryRowContext(ctx, `
        SELECT token, user_id, expires_at
        FROM sessions
        WHERE token = ?
    `, token)

	var s domain.Session
	var expires int64
	if err := row.Scan(&s.Token, &s.UserID, &expires); err != nil {
		if err == sql.ErrNoRows {
			return nil, repo.ErrNotFound
		}
		return nil, err
	}
	s.ExpiresAt = unixToTime(expires)
	return &s, nil
}

func (r *SessionRepo) DeleteByToken(ctx context.Context, token string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE token = ?`, token)
	return err
}

func (r *SessionRepo) DeleteByUserID(ctx context.Context, userID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}

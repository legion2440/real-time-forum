package sqlite

import (
	"context"
	"database/sql"

	"forum/internal/domain"
	"forum/internal/repo"
)

type AuthFlowRepo struct {
	db *sql.DB
}

func NewAuthFlowRepo(db *sql.DB) *AuthFlowRepo {
	return &AuthFlowRepo{db: db}
}

func (r *AuthFlowRepo) Create(ctx context.Context, flow *domain.AuthFlow) error {
	_, err := r.db.ExecContext(ctx, `
        INSERT INTO auth_flows (token, kind, user_id, payload_json, created_at, expires_at)
        VALUES (?, ?, ?, ?, ?, ?)
    `,
		flow.Token,
		flow.Kind,
		nullableInt64(flow.UserID),
		flow.Payload,
		timeToUnix(flow.CreatedAt),
		timeToUnix(flow.ExpiresAt),
	)
	return err
}

func (r *AuthFlowRepo) GetByToken(ctx context.Context, token string) (*domain.AuthFlow, error) {
	row := r.db.QueryRowContext(ctx, `
        SELECT token, kind, user_id, payload_json, created_at, expires_at
        FROM auth_flows
        WHERE token = ?
    `, token)

	return scanAuthFlow(row)
}

func (r *AuthFlowRepo) TakeByToken(ctx context.Context, token string) (*domain.AuthFlow, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRowContext(ctx, `
        SELECT token, kind, user_id, payload_json, created_at, expires_at
        FROM auth_flows
        WHERE token = ?
    `, token)

	flow, err := scanAuthFlow(row)
	if err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_flows WHERE token = ?`, token); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return flow, nil
}

func (r *AuthFlowRepo) DeleteByToken(ctx context.Context, token string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM auth_flows WHERE token = ?`, token)
	return err
}

type authFlowScanner interface {
	Scan(dest ...any) error
}

func scanAuthFlow(s authFlowScanner) (*domain.AuthFlow, error) {
	var (
		flow       domain.AuthFlow
		userID     sql.NullInt64
		createdAt  int64
		expiresAt  int64
	)
	if err := s.Scan(&flow.Token, &flow.Kind, &userID, &flow.Payload, &createdAt, &expiresAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, repo.ErrNotFound
		}
		return nil, err
	}
	if userID.Valid {
		flow.UserID = userID.Int64
	}
	flow.CreatedAt = unixToTime(createdAt)
	flow.ExpiresAt = unixToTime(expiresAt)
	return &flow, nil
}

func nullableInt64(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}


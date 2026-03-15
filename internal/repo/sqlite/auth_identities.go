package sqlite

import (
	"context"
	"database/sql"
	"strings"

	"forum/internal/domain"
	"forum/internal/repo"
)

type AuthIdentityRepo struct {
	db *sql.DB
}

func NewAuthIdentityRepo(db *sql.DB) *AuthIdentityRepo {
	return &AuthIdentityRepo{db: db}
}

func (r *AuthIdentityRepo) Create(ctx context.Context, identity *domain.AuthIdentity) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
        INSERT INTO auth_identities (
            user_id,
            provider,
            provider_user_id,
            provider_email,
            provider_email_verified,
            provider_display_name,
            provider_avatar_url,
            linked_at,
            last_login_at
        )
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
    `,
		identity.UserID,
		strings.TrimSpace(identity.Provider),
		strings.TrimSpace(identity.ProviderUserID),
		strings.TrimSpace(identity.ProviderEmail),
		boolToInt(identity.ProviderEmailVerified),
		strings.TrimSpace(identity.ProviderDisplayName),
		strings.TrimSpace(identity.ProviderAvatarURL),
		timeToUnix(identity.LinkedAt),
		timeToUnix(identity.LastLoginAt),
	)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (r *AuthIdentityRepo) GetByProviderUserID(ctx context.Context, provider, providerUserID string) (*domain.AuthIdentity, error) {
	row := r.db.QueryRowContext(ctx, `
        SELECT id, user_id, provider, provider_user_id, provider_email, provider_email_verified,
               provider_display_name, provider_avatar_url, linked_at, last_login_at
        FROM auth_identities
        WHERE provider = ? AND provider_user_id = ?
    `, strings.TrimSpace(provider), strings.TrimSpace(providerUserID))

	return scanAuthIdentity(row)
}

func (r *AuthIdentityRepo) GetByUserProvider(ctx context.Context, userID int64, provider string) (*domain.AuthIdentity, error) {
	row := r.db.QueryRowContext(ctx, `
        SELECT id, user_id, provider, provider_user_id, provider_email, provider_email_verified,
               provider_display_name, provider_avatar_url, linked_at, last_login_at
        FROM auth_identities
        WHERE user_id = ? AND provider = ?
    `, userID, strings.TrimSpace(provider))

	return scanAuthIdentity(row)
}

func (r *AuthIdentityRepo) ListByUserID(ctx context.Context, userID int64) ([]domain.AuthIdentity, error) {
	rows, err := r.db.QueryContext(ctx, `
        SELECT id, user_id, provider, provider_user_id, provider_email, provider_email_verified,
               provider_display_name, provider_avatar_url, linked_at, last_login_at
        FROM auth_identities
        WHERE user_id = ?
        ORDER BY provider ASC, id ASC
    `, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	identities := make([]domain.AuthIdentity, 0)
	for rows.Next() {
		identity, err := scanAuthIdentity(rows)
		if err != nil {
			return nil, err
		}
		identities = append(identities, *identity)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return identities, nil
}

func (r *AuthIdentityRepo) Update(ctx context.Context, identity *domain.AuthIdentity) error {
	res, err := r.db.ExecContext(ctx, `
        UPDATE auth_identities
        SET provider_email = ?,
            provider_email_verified = ?,
            provider_display_name = ?,
            provider_avatar_url = ?,
            last_login_at = ?
        WHERE id = ?
    `,
		strings.TrimSpace(identity.ProviderEmail),
		boolToInt(identity.ProviderEmailVerified),
		strings.TrimSpace(identity.ProviderDisplayName),
		strings.TrimSpace(identity.ProviderAvatarURL),
		timeToUnix(identity.LastLoginAt),
		identity.ID,
	)
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

func (r *AuthIdentityRepo) DeleteByUserProvider(ctx context.Context, userID int64, provider string) error {
	res, err := r.db.ExecContext(ctx, `
        DELETE FROM auth_identities
        WHERE user_id = ? AND provider = ?
    `, userID, strings.TrimSpace(provider))
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

func (r *AuthIdentityRepo) CountByUserID(ctx context.Context, userID int64) (int, error) {
	row := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_identities WHERE user_id = ?`, userID)
	var count int
	if err := row.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

type authIdentityScanner interface {
	Scan(dest ...any) error
}

func scanAuthIdentity(s authIdentityScanner) (*domain.AuthIdentity, error) {
	var (
		identity      domain.AuthIdentity
		emailVerified int
		linkedAt      int64
		lastLoginAt   int64
	)
	if err := s.Scan(
		&identity.ID,
		&identity.UserID,
		&identity.Provider,
		&identity.ProviderUserID,
		&identity.ProviderEmail,
		&emailVerified,
		&identity.ProviderDisplayName,
		&identity.ProviderAvatarURL,
		&linkedAt,
		&lastLoginAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, repo.ErrNotFound
		}
		return nil, err
	}

	identity.Provider = strings.TrimSpace(identity.Provider)
	identity.ProviderUserID = strings.TrimSpace(identity.ProviderUserID)
	identity.ProviderEmail = strings.TrimSpace(identity.ProviderEmail)
	identity.ProviderEmailVerified = emailVerified != 0
	identity.ProviderDisplayName = strings.TrimSpace(identity.ProviderDisplayName)
	identity.ProviderAvatarURL = strings.TrimSpace(identity.ProviderAvatarURL)
	identity.LinkedAt = unixToTime(linkedAt)
	identity.LastLoginAt = unixToTime(lastLoginAt)

	return &identity, nil
}


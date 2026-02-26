package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"forum/internal/domain"
	"forum/internal/platform/clock"
	"forum/internal/platform/id"
	"forum/internal/repo"

	"golang.org/x/crypto/bcrypt"
)

type PasswordHasher interface {
	Hash(password string) (string, error)
	Compare(hash string, password string) error
}

type bcryptHasher struct{}

func (bcryptHasher) Hash(password string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashed), nil
}

func (bcryptHasher) Compare(hash string, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

type AuthService struct {
	users      repo.UserRepo
	sessions   repo.SessionRepo
	clock      clock.Clock
	idGen      id.Generator
	hasher     PasswordHasher
	sessionTTL time.Duration
}

func NewAuthService(users repo.UserRepo, sessions repo.SessionRepo, clock clock.Clock, idGen id.Generator, ttl time.Duration) *AuthService {
	return &AuthService{
		users:      users,
		sessions:   sessions,
		clock:      clock,
		idGen:      idGen,
		hasher:     bcryptHasher{},
		sessionTTL: ttl,
	}
}

func (s *AuthService) Register(ctx context.Context, email, username, password string) (*domain.User, error) {
	email = strings.TrimSpace(email)
	username = strings.TrimSpace(username)
	if email == "" || username == "" || strings.TrimSpace(password) == "" {
		return nil, ErrInvalidInput
	}

	if _, err := s.users.GetByEmail(ctx, email); err == nil {
		return nil, errors.Join(ErrConflict, ErrEmailTaken)
	} else if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return nil, err
	}

	if _, err := s.users.GetByUsername(ctx, username); err == nil {
		return nil, errors.Join(ErrConflict, ErrUsernameTaken)
	} else if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return nil, err
	}

	hash, err := s.hasher.Hash(password)
	if err != nil {
		return nil, err
	}

	user := &domain.User{
		Email:     email,
		Username:  username,
		PassHash:  hash,
		CreatedAt: s.clock.Now(),
	}

	id, err := s.users.Create(ctx, user)
	if err != nil {
		return nil, err
	}
	user.ID = id
	return user, nil
}

func (s *AuthService) Login(ctx context.Context, email, username, password string) (*domain.Session, *domain.User, error) {
	email = strings.TrimSpace(email)
	username = strings.TrimSpace(username)
	if strings.TrimSpace(password) == "" || (email == "" && username == "") {
		return nil, nil, ErrInvalidInput
	}

	var user *domain.User
	var err error
	if email != "" {
		user, err = s.users.GetByEmail(ctx, email)
	} else {
		user, err = s.users.GetByUsername(ctx, username)
	}
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, nil, ErrUnauthorized
		}
		return nil, nil, err
	}

	if err := s.hasher.Compare(user.PassHash, password); err != nil {
		return nil, nil, ErrUnauthorized
	}

	if err := s.sessions.DeleteByUserID(ctx, user.ID); err != nil {
		return nil, nil, err
	}

	token, err := s.idGen.New()
	if err != nil {
		return nil, nil, err
	}

	session := &domain.Session{
		Token:     token,
		UserID:    user.ID,
		ExpiresAt: s.clock.Now().Add(s.sessionTTL),
	}

	if err := s.sessions.Create(ctx, session); err != nil {
		return nil, nil, err
	}

	return session, user, nil
}

func (s *AuthService) Logout(ctx context.Context, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return ErrInvalidInput
	}
	return s.sessions.DeleteByToken(ctx, token)
}

func (s *AuthService) GetSession(ctx context.Context, token string) (*domain.Session, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrUnauthorized
	}
	session, err := s.sessions.GetByToken(ctx, token)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, ErrUnauthorized
		}
		return nil, err
	}
	if session.ExpiresAt.Before(s.clock.Now()) {
		_ = s.sessions.DeleteByToken(ctx, token)
		return nil, ErrUnauthorized
	}
	return session, nil
}

func (s *AuthService) GetUserByID(ctx context.Context, id int64) (*domain.User, error) {
	user, err := s.users.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return user, nil
}

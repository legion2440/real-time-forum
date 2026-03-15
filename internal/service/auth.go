package service

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

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
	oauth      *oauthDependencies
}

const maxDisplayNameLength = 64
const maxProfileAge = 150

func NewAuthService(users repo.UserRepo, sessions repo.SessionRepo, clock clock.Clock, idGen id.Generator, ttl time.Duration, opts ...AuthOption) *AuthService {
	service := &AuthService{
		users:      users,
		sessions:   sessions,
		clock:      clock,
		idGen:      idGen,
		hasher:     bcryptHasher{},
		sessionTTL: ttl,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(service)
		}
	}
	return service
}

func (s *AuthService) Register(ctx context.Context, email, username, password string) (*domain.User, error) {
	email = strings.TrimSpace(email)
	username = strings.TrimSpace(username)
	if email == "" || username == "" || strings.TrimSpace(password) == "" {
		return nil, ErrInvalidInput
	}
	if !isValidEmail(email) {
		return nil, ErrInvalidInput
	}

	if _, err := s.users.GetByEmailCI(ctx, email); err == nil {
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

	user, err := s.findLoginUser(ctx, email, username)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(user.PassHash) == "" {
		return nil, nil, ErrUnauthorized
	}

	if err := s.hasher.Compare(user.PassHash, password); err != nil {
		return nil, nil, ErrUnauthorized
	}

	session, err := s.createSessionForUser(ctx, user.ID)
	if err != nil {
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

func (s *AuthService) GetPublicProfile(ctx context.Context, username string) (*domain.User, error) {
	username = normalizeUsername(username)
	if username == "" {
		return nil, ErrInvalidInput
	}

	user, err := s.users.GetPublicByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return user, nil
}

func (s *AuthService) GetMyProfile(ctx context.Context, userID int64) (*domain.User, error) {
	return s.GetUserByID(ctx, userID)
}

func (s *AuthService) UpdateMyProfile(ctx context.Context, userID int64, displayName, firstName, lastName *string, age *int, gender *string, markInitialized, skip bool) (*domain.User, error) {
	if userID <= 0 {
		return nil, ErrInvalidInput
	}

	user, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	if skip {
		if err := s.users.UpdateProfile(ctx, userID, normalizeStoredDisplayName(user.DisplayName), user.FirstName, user.LastName, user.Age, user.Gender, true); err != nil {
			if errors.Is(err, repo.ErrNotFound) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		return s.GetUserByID(ctx, userID)
	}

	displayNameToStore := normalizeStoredDisplayName(user.DisplayName)
	if displayName != nil {
		nextDisplayName := strings.TrimSpace(*displayName)
		if utf8.RuneCountInString(nextDisplayName) > maxDisplayNameLength {
			return nil, ErrInvalidInput
		}

		displayNameToStore = normalizeProfileDisplayName(nextDisplayName, user.Username)
		if displayNameToStore != nil {
			taken, err := s.isDisplayNameTaken(ctx, *displayNameToStore, userID)
			if err != nil {
				return nil, err
			}
			if taken {
				return nil, ErrDisplayNameTaken
			}
		}
	}

	nextFirstName := user.FirstName
	if firstName != nil {
		nextFirstName = strings.TrimSpace(*firstName)
	}

	nextLastName := user.LastName
	if lastName != nil {
		nextLastName = strings.TrimSpace(*lastName)
	}

	nextAge := user.Age
	if age != nil {
		if *age < 0 || *age > maxProfileAge {
			return nil, ErrInvalidInput
		}
		nextAge = *age
	}

	nextGender := user.Gender
	if gender != nil {
		nextGender = strings.TrimSpace(*gender)
	}

	nextProfileInitialized := user.ProfileInitialized || markInitialized

	if err := s.users.UpdateProfile(ctx, userID, displayNameToStore, nextFirstName, nextLastName, nextAge, nextGender, nextProfileInitialized); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return s.GetUserByID(ctx, userID)
}

func (s *AuthService) ListUsers(ctx context.Context) ([]domain.User, error) {
	return s.users.List(ctx)
}

func (s *AuthService) ListUsersPublic(ctx context.Context) ([]domain.User, error) {
	return s.users.ListPublic(ctx)
}

func (s *AuthService) findLoginUser(ctx context.Context, email, username string) (*domain.User, error) {
	attempts := make([]func(context.Context, string) (*domain.User, error), 0, 2)
	values := make([]string, 0, 2)

	switch {
	case email != "" && username != "":
		attempts = append(attempts, s.users.GetByEmailCI, s.users.GetByUsername)
		values = append(values, email, username)
	default:
		identifier := email
		if identifier == "" {
			identifier = username
		}
		if strings.Contains(identifier, "@") {
			attempts = append(attempts, s.users.GetByEmailCI, s.users.GetByUsername)
		} else {
			attempts = append(attempts, s.users.GetByUsername, s.users.GetByEmailCI)
		}
		values = append(values, identifier, identifier)
	}

	for i, attempt := range attempts {
		user, err := attempt(ctx, values[i])
		if err == nil {
			return user, nil
		}
		if errors.Is(err, repo.ErrNotFound) {
			continue
		}
		return nil, err
	}

	return nil, ErrUnauthorized
}

func (s *AuthService) isDisplayNameTaken(ctx context.Context, displayName string, excludeUserID int64) (bool, error) {
	usernameMatch, err := s.users.GetByUsernameCI(ctx, displayName)
	if err == nil && usernameMatch.ID != excludeUserID {
		return true, nil
	}
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return false, err
	}

	displayNameMatch, err := s.users.GetByDisplayNameCI(ctx, displayName)
	if err == nil && displayNameMatch.ID != excludeUserID {
		return true, nil
	}
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return false, err
	}

	return false, nil
}

func (s *AuthService) createSessionForUser(ctx context.Context, userID int64) (*domain.Session, error) {
	if err := s.sessions.DeleteByUserID(ctx, userID); err != nil {
		return nil, err
	}

	token, err := s.idGen.New()
	if err != nil {
		return nil, err
	}

	session := &domain.Session{
		Token:     token,
		UserID:    userID,
		ExpiresAt: s.clock.Now().Add(s.sessionTTL),
	}
	if err := s.sessions.Create(ctx, session); err != nil {
		return nil, err
	}

	return session, nil
}

func normalizeProfileDisplayName(displayName, username string) *string {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" || strings.EqualFold(displayName, strings.TrimSpace(username)) {
		return nil
	}
	return &displayName
}

func normalizeStoredDisplayName(displayName string) *string {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		return nil
	}
	return &displayName
}

func normalizeUsername(username string) string {
	return strings.TrimPrefix(strings.TrimSpace(username), "@")
}

func isValidEmail(email string) bool {
	addr, err := mail.ParseAddress(strings.TrimSpace(email))
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(addr.Address), strings.TrimSpace(email))
}

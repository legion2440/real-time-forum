package sqlite

import (
	"context"
	"testing"
	"time"

	"forum/internal/domain"
	"forum/internal/repo"
)

func TestSessionRepo_CreateAndDelete(t *testing.T) {
	db := newTestDB(t)
	users := NewUserRepo(db)
	sessions := NewSessionRepo(db)

	user := &domain.User{
		Email:     "user2@example.com",
		Username:  "user2",
		PassHash:  "hash",
		CreatedAt: time.Now().UTC(),
	}
	userID, err := users.Create(context.Background(), user)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	session := &domain.Session{
		Token:     "token-1",
		UserID:    userID,
		ExpiresAt: time.Now().Add(time.Hour),
	}

	if err := sessions.Create(context.Background(), session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	got, err := sessions.GetByToken(context.Background(), "token-1")
	if err != nil {
		t.Fatalf("get by token: %v", err)
	}
	if got.UserID != userID {
		t.Fatalf("expected user id %d, got %d", userID, got.UserID)
	}

	if err := sessions.DeleteByUserID(context.Background(), userID); err != nil {
		t.Fatalf("delete by user: %v", err)
	}

	_, err = sessions.GetByToken(context.Background(), "token-1")
	if err == nil || err != repo.ErrNotFound {
		t.Fatalf("expected not found, got %v", err)
	}
}

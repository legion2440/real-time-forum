package sqlite

import (
	"context"
	"testing"
	"time"

	"forum/internal/domain"
	"forum/internal/repo"
)

func TestUserRepo_CreateAndGet(t *testing.T) {
	db := newTestDB(t)
	users := NewUserRepo(db)

	user := &domain.User{
		Email:     "user@example.com",
		Username:  "user1",
		PassHash:  "hash",
		CreatedAt: time.Now().UTC(),
	}

	id, err := users.Create(context.Background(), user)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	got, err := users.GetByEmail(context.Background(), "user@example.com")
	if err != nil {
		t.Fatalf("get by email: %v", err)
	}
	if got.ID != id {
		t.Fatalf("expected id %d, got %d", id, got.ID)
	}

	_, err = users.GetByEmail(context.Background(), "missing@example.com")
	if err == nil || err != repo.ErrNotFound {
		t.Fatalf("expected not found, got %v", err)
	}
}

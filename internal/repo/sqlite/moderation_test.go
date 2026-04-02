package sqlite

import (
	"context"
	"strings"
	"testing"
	"time"

	"forum/internal/domain"
)

func TestModerationRepo_SingleOwnerConstraint(t *testing.T) {
	db := newTestDB(t)
	users := NewUserRepo(db)
	moderation := NewModerationRepo(db)

	ctx := context.Background()
	firstOwner := &domain.User{
		Email:     "owner-one@example.com",
		Username:  "owner_one",
		PassHash:  "hash",
		Role:      domain.RoleOwner,
		CreatedAt: time.Unix(1700030000, 0).UTC(),
	}
	if _, err := moderation.CreateBootstrapOwner(ctx, firstOwner); err != nil {
		t.Fatalf("create first owner: %v", err)
	}

	secondOwner := &domain.User{
		Email:     "owner-two@example.com",
		Username:  "owner_two",
		PassHash:  "hash",
		Role:      domain.RoleOwner,
		CreatedAt: time.Unix(1700030001, 0).UTC(),
	}
	if _, err := moderation.CreateBootstrapOwner(ctx, secondOwner); err == nil {
		t.Fatalf("expected second owner creation to fail")
	} else if !strings.Contains(strings.ToLower(err.Error()), "unique") {
		t.Fatalf("expected unique constraint error for second owner, got %v", err)
	}

	user := &domain.User{
		Email:     "user@example.com",
		Username:  "user_regular",
		PassHash:  "hash",
		CreatedAt: time.Unix(1700030002, 0).UTC(),
	}
	userID, err := users.Create(ctx, user)
	if err != nil {
		t.Fatalf("create regular user: %v", err)
	}
	if err := users.UpdateRole(ctx, userID, domain.RoleModerator); err != nil {
		t.Fatalf("promote regular user to moderator: %v", err)
	}

	admin := &domain.User{
		Email:     "admin@example.com",
		Username:  "admin_regular",
		PassHash:  "hash",
		CreatedAt: time.Unix(1700030003, 0).UTC(),
	}
	adminID, err := users.Create(ctx, admin)
	if err != nil {
		t.Fatalf("create admin user: %v", err)
	}
	if err := users.UpdateRole(ctx, adminID, domain.RoleAdmin); err != nil {
		t.Fatalf("promote regular user to admin: %v", err)
	}
}

package sqlite

import (
	"context"
	"strings"
	"testing"
	"time"

	"forum/internal/domain"
	"forum/internal/repo"
)

func TestAccountRepo_MergeUsersMovesOwnedRecords(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	users := NewUserRepo(db)
	identities := NewAuthIdentityRepo(db)
	accounts := NewAccountRepo(db)

	target := &domain.User{
		Email:     "target@example.com",
		Username:  "target",
		PassHash:  "target-hash",
		CreatedAt: time.Unix(100, 0),
	}
	targetID, err := users.Create(ctx, target)
	if err != nil {
		t.Fatalf("create target user: %v", err)
	}

	source := &domain.User{
		Email:     "source@example.com",
		Username:  "source",
		PassHash:  "",
		CreatedAt: time.Unix(200, 0),
	}
	sourceID, err := users.Create(ctx, source)
	if err != nil {
		t.Fatalf("create source user: %v", err)
	}

	postRes, err := db.ExecContext(ctx, `INSERT INTO posts (user_id, title, body, created_at) VALUES (?, ?, ?, ?)`, sourceID, "hello", "body", timeToUnix(time.Unix(300, 0)))
	if err != nil {
		t.Fatalf("create source post: %v", err)
	}
	postID, err := postRes.LastInsertId()
	if err != nil {
		t.Fatalf("post last insert id: %v", err)
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO comments (post_id, user_id, body, created_at) VALUES (?, ?, ?, ?)`, postID, sourceID, "comment", timeToUnix(time.Unix(301, 0))); err != nil {
		t.Fatalf("create source comment: %v", err)
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?), (?, ?, ?)`,
		"target-session", targetID, timeToUnix(time.Unix(500, 0)),
		"source-session", sourceID, timeToUnix(time.Unix(500, 0)),
	); err != nil {
		t.Fatalf("create sessions: %v", err)
	}

	if _, err := identities.Create(ctx, &domain.AuthIdentity{
		UserID:                sourceID,
		Provider:              "github",
		ProviderUserID:        "gh-1",
		ProviderEmail:         "source@example.com",
		ProviderEmailVerified: true,
		ProviderDisplayName:   "Source Github",
		LinkedAt:              time.Unix(400, 0),
		LastLoginAt:           time.Unix(401, 0),
	}); err != nil {
		t.Fatalf("create source identity: %v", err)
	}

	displayName := "Merged User"
	if err := accounts.MergeUsers(ctx, repo.AccountMergeInput{
		TargetUserID:             targetID,
		SourceUserID:             sourceID,
		DisplayName:              &displayName,
		TargetEmail:              "target@example.com",
		TargetUsername:           "target",
		TargetPassHash:           "target-hash",
		TargetProfileInitialized: true,
		Now:                      time.Unix(600, 0),
	}); err != nil {
		t.Fatalf("merge users: %v", err)
	}

	mergedTarget, err := users.GetByID(ctx, targetID)
	if err != nil {
		t.Fatalf("get merged target: %v", err)
	}
	if strings.TrimSpace(mergedTarget.DisplayName) != displayName {
		t.Fatalf("expected target display name %q, got %q", displayName, mergedTarget.DisplayName)
	}

	if _, err := users.GetByID(ctx, sourceID); err != repo.ErrNotFound {
		t.Fatalf("expected source user to be deleted, got %v", err)
	}

	var postOwnerID int64
	if err := db.QueryRowContext(ctx, `SELECT user_id FROM posts WHERE id = ?`, postID).Scan(&postOwnerID); err != nil {
		t.Fatalf("read merged post owner: %v", err)
	}
	if postOwnerID != targetID {
		t.Fatalf("expected merged post owner %d, got %d", targetID, postOwnerID)
	}

	var commentOwners int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM comments WHERE user_id = ?`, targetID).Scan(&commentOwners); err != nil {
		t.Fatalf("count merged comments: %v", err)
	}
	if commentOwners != 1 {
		t.Fatalf("expected 1 comment moved to target, got %d", commentOwners)
	}

	identity, err := identities.GetByUserProvider(ctx, targetID, "github")
	if err != nil {
		t.Fatalf("get merged identity: %v", err)
	}
	if identity.UserID != targetID {
		t.Fatalf("expected identity to move to target user %d, got %d", targetID, identity.UserID)
	}

	var sessionCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions`).Scan(&sessionCount); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessionCount != 0 {
		t.Fatalf("expected sessions to be cleared during merge, got %d", sessionCount)
	}
}

func TestAccountRepo_MergeUsersRollsBackOnIdentityConflict(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	users := NewUserRepo(db)
	identities := NewAuthIdentityRepo(db)
	accounts := NewAccountRepo(db)

	targetID, err := users.Create(ctx, &domain.User{
		Email:     "target@example.com",
		Username:  "target",
		PassHash:  "target-hash",
		CreatedAt: time.Unix(100, 0),
	})
	if err != nil {
		t.Fatalf("create target user: %v", err)
	}

	sourceID, err := users.Create(ctx, &domain.User{
		Email:     "source@example.com",
		Username:  "source",
		PassHash:  "",
		CreatedAt: time.Unix(200, 0),
	})
	if err != nil {
		t.Fatalf("create source user: %v", err)
	}

	postRes, err := db.ExecContext(ctx, `INSERT INTO posts (user_id, title, body, created_at) VALUES (?, ?, ?, ?)`, sourceID, "conflict", "body", timeToUnix(time.Unix(300, 0)))
	if err != nil {
		t.Fatalf("create source post: %v", err)
	}
	postID, err := postRes.LastInsertId()
	if err != nil {
		t.Fatalf("post last insert id: %v", err)
	}

	for _, identity := range []domain.AuthIdentity{
		{
			UserID:                targetID,
			Provider:              "google",
			ProviderUserID:        "g-target",
			ProviderEmail:         "target@example.com",
			ProviderEmailVerified: true,
			LinkedAt:              time.Unix(400, 0),
			LastLoginAt:           time.Unix(401, 0),
		},
		{
			UserID:                sourceID,
			Provider:              "google",
			ProviderUserID:        "g-source",
			ProviderEmail:         "source@example.com",
			ProviderEmailVerified: true,
			LinkedAt:              time.Unix(402, 0),
			LastLoginAt:           time.Unix(403, 0),
		},
	} {
		identity := identity
		if _, err := identities.Create(ctx, &identity); err != nil {
			t.Fatalf("create identity %+v: %v", identity, err)
		}
	}

	err = accounts.MergeUsers(ctx, repo.AccountMergeInput{
		TargetUserID:             targetID,
		SourceUserID:             sourceID,
		TargetEmail:              "target@example.com",
		TargetUsername:           "target",
		TargetPassHash:           "target-hash",
		TargetProfileInitialized: false,
		Now:                      time.Unix(500, 0),
	})
	if err == nil {
		t.Fatal("expected merge to fail on provider conflict")
	}

	var postOwnerID int64
	if err := db.QueryRowContext(ctx, `SELECT user_id FROM posts WHERE id = ?`, postID).Scan(&postOwnerID); err != nil {
		t.Fatalf("read post owner after rollback: %v", err)
	}
	if postOwnerID != sourceID {
		t.Fatalf("expected source post owner to remain %d after rollback, got %d", sourceID, postOwnerID)
	}

	if _, err := users.GetByID(ctx, sourceID); err != nil {
		t.Fatalf("expected source user to remain after rollback, got %v", err)
	}
}

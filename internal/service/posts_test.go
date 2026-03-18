package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"forum/internal/domain"
	"forum/internal/repo/sqlite"
)

func newPostServiceFixture(t *testing.T, now time.Time) (*AuthService, *PostService, *sqlite.CommentRepo, func()) {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := sqlite.Open(dbPath)
	if err != nil {
		if strings.Contains(err.Error(), "requires cgo to work") {
			t.Skip("sqlite3 driver requires cgo")
		}
		t.Fatalf("open db: %v", err)
	}

	userRepo := sqlite.NewUserRepo(db)
	sessionRepo := sqlite.NewSessionRepo(db)
	postRepo := sqlite.NewPostRepo(db)
	commentRepo := sqlite.NewCommentRepo(db)
	categoryRepo := sqlite.NewCategoryRepo(db)
	reactionRepo := sqlite.NewReactionRepo(db)

	testClock := fixedClock{t: now.UTC()}
	authService := NewAuthService(userRepo, sessionRepo, testClock, &seqID{}, 24*time.Hour)
	postService := NewPostService(postRepo, commentRepo, categoryRepo, reactionRepo, nil, testClock)

	return authService, postService, commentRepo, func() { _ = db.Close() }
}

func TestPostService_CommentEditWindowAndOwnership(t *testing.T) {
	now := time.Unix(1700000500, 0).UTC()
	auth, posts, commentRepo, cleanup := newPostServiceFixture(t, now)
	defer cleanup()

	aliceID := registerTestUser(t, auth, "alice-posts@example.com", "alice_posts")
	bobID := registerTestUser(t, auth, "bob-posts@example.com", "bob_posts")

	post, err := posts.CreatePost(context.Background(), aliceID, "Post", "Body", []int64{1}, nil)
	if err != nil {
		t.Fatalf("create post: %v", err)
	}

	oldCommentID, err := commentRepo.Create(context.Background(), &domain.Comment{
		PostID:    post.ID,
		UserID:    aliceID,
		Body:      "Old comment",
		CreatedAt: now.Add(-31 * time.Minute),
	})
	if err != nil {
		t.Fatalf("create old comment: %v", err)
	}

	if _, err := posts.UpdateComment(context.Background(), aliceID, oldCommentID, "Updated"); !errors.Is(err, ErrCommentEditWindowExpired) {
		t.Fatalf("expected comment edit window error, got %v", err)
	}
	if _, err := posts.UpdateComment(context.Background(), bobID, oldCommentID, "Nope"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected forbidden on other-user comment edit, got %v", err)
	}
	if err := posts.DeleteComment(context.Background(), aliceID, oldCommentID); err != nil {
		t.Fatalf("delete old comment: %v", err)
	}
}

func TestPostService_PostEditDeleteRequireOwnership(t *testing.T) {
	now := time.Unix(1700000600, 0).UTC()
	auth, posts, _, cleanup := newPostServiceFixture(t, now)
	defer cleanup()

	aliceID := registerTestUser(t, auth, "alice-own@example.com", "alice_own")
	bobID := registerTestUser(t, auth, "bob-own@example.com", "bob_own")

	post, err := posts.CreatePost(context.Background(), aliceID, "Owned", "Body", []int64{1}, nil)
	if err != nil {
		t.Fatalf("create post: %v", err)
	}

	if _, err := posts.UpdatePost(context.Background(), bobID, post.ID, "Hack", "Hack", []int64{1}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected forbidden on other-user post edit, got %v", err)
	}
	if err := posts.DeletePost(context.Background(), bobID, post.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected forbidden on other-user post delete, got %v", err)
	}

	updated, err := posts.UpdatePost(context.Background(), aliceID, post.ID, "Updated", "Updated body", []int64{1})
	if err != nil {
		t.Fatalf("update own post: %v", err)
	}
	if updated.Title != "Updated" {
		t.Fatalf("expected updated title, got %+v", updated)
	}
	if err := posts.DeletePost(context.Background(), aliceID, post.ID); err != nil {
		t.Fatalf("delete own post: %v", err)
	}
}

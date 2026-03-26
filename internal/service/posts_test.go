package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"forum/internal/domain"
	"forum/internal/repo"
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

func TestPostService_DeleteCommentKeepsDeletedPlaceholderWhileRepliesRemain(t *testing.T) {
	now := time.Unix(1700000700, 0).UTC()
	auth, posts, commentRepo, cleanup := newPostServiceFixture(t, now)
	defer cleanup()

	aliceID := registerTestUser(t, auth, "alice-thread@example.com", "alice_thread")
	bobID := registerTestUser(t, auth, "bob-thread@example.com", "bob_thread")
	charlieID := registerTestUser(t, auth, "charlie-thread@example.com", "charlie_thread")

	post, err := posts.CreatePost(context.Background(), aliceID, "Thread", "Body", []int64{1}, nil)
	if err != nil {
		t.Fatalf("create post: %v", err)
	}

	root, err := posts.CreateComment(context.Background(), aliceID, post.ID, "Root comment", nil)
	if err != nil {
		t.Fatalf("create root comment: %v", err)
	}
	if _, err := posts.CreateComment(context.Background(), bobID, post.ID, "Reply", &root.ID); err != nil {
		t.Fatalf("create reply: %v", err)
	}

	if err := posts.DeleteComment(context.Background(), aliceID, root.ID); err != nil {
		t.Fatalf("delete root with reply: %v", err)
	}

	stored, err := commentRepo.GetByID(context.Background(), root.ID)
	if err != nil {
		t.Fatalf("get soft-deleted root: %v", err)
	}
	if stored.DeletedAt == nil {
		t.Fatalf("expected deleted_at on root comment, got %+v", stored)
	}
	if stored.Body != "[deleted]" {
		t.Fatalf("expected deleted placeholder body, got %+v", stored)
	}

	comments, err := posts.ListComments(context.Background(), post.ID, domain.CommentFilter{})
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	if len(comments) != 2 {
		t.Fatalf("expected root placeholder and reply, got %+v", comments)
	}
	if comments[0].ID != root.ID || comments[0].DeletedAt == nil || comments[0].Body != "[deleted]" {
		t.Fatalf("expected first comment to be deleted placeholder, got %+v", comments[0])
	}

	if _, err := posts.CreateComment(context.Background(), charlieID, post.ID, "Reply to deleted root", &root.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found when replying to deleted comment, got %v", err)
	}
	if _, err := posts.ReactComment(context.Background(), charlieID, root.ID, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found when reacting to deleted comment, got %v", err)
	}
	if _, err := posts.UpdateComment(context.Background(), aliceID, root.ID, "edited"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found when editing deleted comment, got %v", err)
	}
}

func TestPostService_DeleteReplyWithoutDescendantsKeepsDeletedPlaceholder(t *testing.T) {
	now := time.Unix(1700000800, 0).UTC()
	auth, posts, commentRepo, cleanup := newPostServiceFixture(t, now)
	defer cleanup()

	aliceID := registerTestUser(t, auth, "alice-purge@example.com", "alice_purge")
	bobID := registerTestUser(t, auth, "bob-purge@example.com", "bob_purge")

	post, err := posts.CreatePost(context.Background(), aliceID, "Thread", "Body", []int64{1}, nil)
	if err != nil {
		t.Fatalf("create post: %v", err)
	}

	root, err := posts.CreateComment(context.Background(), aliceID, post.ID, "Root comment", nil)
	if err != nil {
		t.Fatalf("create root comment: %v", err)
	}
	reply, err := posts.CreateComment(context.Background(), bobID, post.ID, "Reply", &root.ID)
	if err != nil {
		t.Fatalf("create reply: %v", err)
	}

	if err := posts.DeleteComment(context.Background(), bobID, reply.ID); err != nil {
		t.Fatalf("delete reply: %v", err)
	}

	storedReply, err := commentRepo.GetByID(context.Background(), reply.ID)
	if err != nil {
		t.Fatalf("get soft-deleted reply: %v", err)
	}
	if storedReply.DeletedAt == nil || storedReply.Body != "[deleted]" {
		t.Fatalf("expected soft-deleted reply placeholder, got %+v", storedReply)
	}
	comments, err := posts.ListComments(context.Background(), post.ID, domain.CommentFilter{})
	if err != nil {
		t.Fatalf("list comments after reply delete: %v", err)
	}
	if len(comments) != 2 {
		t.Fatalf("expected root and deleted reply to remain, got %+v", comments)
	}
	if comments[1].ID != reply.ID || comments[1].DeletedAt == nil || comments[1].Body != "[deleted]" {
		t.Fatalf("expected deleted reply artifact in list, got %+v", comments[1])
	}
}

func TestPostService_DeleteRootWithoutDescendantsRemovesComment(t *testing.T) {
	now := time.Unix(1700000900, 0).UTC()
	auth, posts, commentRepo, cleanup := newPostServiceFixture(t, now)
	defer cleanup()

	aliceID := registerTestUser(t, auth, "alice-root-only@example.com", "alice_root_only")

	post, err := posts.CreatePost(context.Background(), aliceID, "Thread", "Body", []int64{1}, nil)
	if err != nil {
		t.Fatalf("create post: %v", err)
	}

	root, err := posts.CreateComment(context.Background(), aliceID, post.ID, "Solo root", nil)
	if err != nil {
		t.Fatalf("create root comment: %v", err)
	}

	if err := posts.DeleteComment(context.Background(), aliceID, root.ID); err != nil {
		t.Fatalf("delete root without descendants: %v", err)
	}

	if _, err := commentRepo.GetByID(context.Background(), root.ID); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("expected hard-deleted root, got %v", err)
	}
	comments, err := posts.ListComments(context.Background(), post.ID, domain.CommentFilter{})
	if err != nil {
		t.Fatalf("list comments after root delete: %v", err)
	}
	if len(comments) != 0 {
		t.Fatalf("expected empty comment list after root delete, got %+v", comments)
	}
}

func TestPostService_DeleteWholeDeletedThreadPurgesArtifacts(t *testing.T) {
	now := time.Unix(1700001000, 0).UTC()
	auth, posts, commentRepo, cleanup := newPostServiceFixture(t, now)
	defer cleanup()

	aliceID := registerTestUser(t, auth, "alice-thread-purge@example.com", "alice_thread_purge")
	bobID := registerTestUser(t, auth, "bob-thread-purge@example.com", "bob_thread_purge")

	post, err := posts.CreatePost(context.Background(), aliceID, "Thread", "Body", []int64{1}, nil)
	if err != nil {
		t.Fatalf("create post: %v", err)
	}

	root, err := posts.CreateComment(context.Background(), aliceID, post.ID, "01", nil)
	if err != nil {
		t.Fatalf("create root comment: %v", err)
	}
	replyOne, err := posts.CreateComment(context.Background(), bobID, post.ID, "02", &root.ID)
	if err != nil {
		t.Fatalf("create first reply: %v", err)
	}
	replyTwo, err := posts.CreateComment(context.Background(), bobID, post.ID, "03", &root.ID)
	if err != nil {
		t.Fatalf("create second reply: %v", err)
	}

	if err := posts.DeleteComment(context.Background(), aliceID, root.ID); err != nil {
		t.Fatalf("delete root to placeholder: %v", err)
	}
	if err := posts.DeleteComment(context.Background(), bobID, replyOne.ID); err != nil {
		t.Fatalf("delete first reply to placeholder: %v", err)
	}

	comments, err := posts.ListComments(context.Background(), post.ID, domain.CommentFilter{})
	if err != nil {
		t.Fatalf("list comments before final purge: %v", err)
	}
	if len(comments) != 3 {
		t.Fatalf("expected full thread to remain while one reply is still active, got %+v", comments)
	}

	if err := posts.DeleteComment(context.Background(), bobID, replyTwo.ID); err != nil {
		t.Fatalf("delete final reply: %v", err)
	}

	if _, err := commentRepo.GetByID(context.Background(), root.ID); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("expected root to be purged with thread, got %v", err)
	}
	if _, err := commentRepo.GetByID(context.Background(), replyOne.ID); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("expected first reply to be purged with thread, got %v", err)
	}
	if _, err := commentRepo.GetByID(context.Background(), replyTwo.ID); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("expected second reply to be purged with thread, got %v", err)
	}

	comments, err = posts.ListComments(context.Background(), post.ID, domain.CommentFilter{})
	if err != nil {
		t.Fatalf("list comments after thread purge: %v", err)
	}
	if len(comments) != 0 {
		t.Fatalf("expected empty comments after full deleted-thread purge, got %+v", comments)
	}
}

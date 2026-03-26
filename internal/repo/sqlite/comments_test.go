package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"forum/internal/domain"
)

func TestCommentRepo_ListByPostHidesFullyDeletedThread(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "comments.db")

	db, err := Open(dbPath)
	if err != nil {
		if strings.Contains(err.Error(), "requires cgo to work") {
			t.Skip("sqlite3 driver requires cgo")
		}
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	users := NewUserRepo(db)
	posts := NewPostRepo(db)
	comments := NewCommentRepo(db)

	ctx := context.Background()
	now := time.Unix(1700001100, 0).UTC()

	userID, err := users.Create(ctx, &domain.User{
		Email:     "comments-test@example.com",
		Username:  "comments_test",
		PassHash:  "hash",
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	postID, err := posts.Create(ctx, &domain.Post{
		UserID:    userID,
		Title:     "Thread",
		Body:      "Body",
		CreatedAt: now,
	}, []int64{1})
	if err != nil {
		t.Fatalf("create post: %v", err)
	}

	rootID, err := comments.Create(ctx, &domain.Comment{
		PostID:    postID,
		UserID:    userID,
		Body:      "[deleted]",
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("create root comment: %v", err)
	}
	replyID, err := comments.Create(ctx, &domain.Comment{
		PostID:    postID,
		ParentID:  &rootID,
		UserID:    userID,
		Body:      "[deleted]",
		CreatedAt: now.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("create reply comment: %v", err)
	}

	if err := comments.SoftDelete(ctx, rootID, now.Add(2*time.Second)); err != nil {
		t.Fatalf("soft delete root: %v", err)
	}
	if err := comments.SoftDelete(ctx, replyID, now.Add(3*time.Second)); err != nil {
		t.Fatalf("soft delete reply: %v", err)
	}

	list, err := comments.ListByPost(ctx, postID, domain.CommentFilter{})
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected fully deleted thread to be hidden, got %+v", list)
	}
}

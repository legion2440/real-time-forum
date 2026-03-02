package sqlite

import (
	"context"
	"testing"
	"time"

	"forum/internal/domain"
)

func TestPostRepo_SearchByTitleBodyAuthor(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	users := NewUserRepo(db)
	posts := NewPostRepo(db)

	aliceID := mustCreateUser(t, ctx, users, "alice@example.com", "AliceUser")
	bobID := mustCreateUser(t, ctx, users, "bob@example.com", "BobUser")
	if err := users.UpdateProfile(ctx, aliceID, stringPtr("Captain Alice"), "", "", 0, "", true); err != nil {
		t.Fatalf("set alice display name: %v", err)
	}
	if err := users.UpdateProfile(ctx, bobID, stringPtr("Builder Bob"), "", "", 0, "", true); err != nil {
		t.Fatalf("set bob display name: %v", err)
	}

	post1ID := mustCreatePost(t, ctx, posts, aliceID, "Alpha release", "General notes", time.Now().UTC().Add(-3*time.Hour))
	post2ID := mustCreatePost(t, ctx, posts, aliceID, "Roadmap", "Contains HELM migration details", time.Now().UTC().Add(-2*time.Hour))
	_ = mustCreatePost(t, ctx, posts, bobID, "Another post", "Nothing relevant here", time.Now().UTC().Add(-time.Hour))

	byTitle, err := posts.List(ctx, domain.PostFilter{Query: "alpha"})
	if err != nil {
		t.Fatalf("search by title: %v", err)
	}
	if len(byTitle) != 1 || byTitle[0].ID != post1ID {
		t.Fatalf("expected only post %d by title search, got %+v", post1ID, byTitle)
	}

	byBody, err := posts.List(ctx, domain.PostFilter{Query: "helm"})
	if err != nil {
		t.Fatalf("search by body: %v", err)
	}
	if len(byBody) != 1 || byBody[0].ID != post2ID {
		t.Fatalf("expected only post %d by body search, got %+v", post2ID, byBody)
	}

	byAuthor, err := posts.List(ctx, domain.PostFilter{Query: "aliceuser"})
	if err != nil {
		t.Fatalf("search by author: %v", err)
	}
	if len(byAuthor) != 2 {
		t.Fatalf("expected 2 posts for author search, got %d", len(byAuthor))
	}
	for _, p := range byAuthor {
		if p.Author.Username != "AliceUser" {
			t.Fatalf("expected author username AliceUser, got %+v", p.Author)
		}
		if p.Author.DisplayName != "Captain Alice" {
			t.Fatalf("expected author display name Captain Alice, got %+v", p.Author)
		}
		if p.Author.ID != p.UserID {
			t.Fatalf("expected author id %d, got %+v", p.UserID, p.Author)
		}
	}

	byAuthorTag, err := posts.List(ctx, domain.PostFilter{Query: "@AliceUser"})
	if err != nil {
		t.Fatalf("search by @author: %v", err)
	}
	if len(byAuthorTag) != 2 {
		t.Fatalf("expected 2 posts for @author search, got %d", len(byAuthorTag))
	}

	byDisplayName, err := posts.List(ctx, domain.PostFilter{Query: "Captain"})
	if err != nil {
		t.Fatalf("search by display name: %v", err)
	}
	if len(byDisplayName) != 2 {
		t.Fatalf("expected 2 posts for display name search, got %d", len(byDisplayName))
	}

	got, err := posts.GetByID(ctx, post1ID)
	if err != nil {
		t.Fatalf("get post by id: %v", err)
	}
	if got.Author.Username != "AliceUser" || got.Author.DisplayName != "Captain Alice" || got.Author.ID != aliceID {
		t.Fatalf("expected author object on get by id, got %+v", got.Author)
	}
}

func TestCommentRepo_SearchByBodyAuthor(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	users := NewUserRepo(db)
	posts := NewPostRepo(db)
	comments := NewCommentRepo(db)

	aliceID := mustCreateUser(t, ctx, users, "alice2@example.com", "alice_thread")
	bobID := mustCreateUser(t, ctx, users, "bob2@example.com", "bob_thread")
	if err := users.UpdateProfile(ctx, aliceID, stringPtr("Thread Alice"), "", "", 0, "", true); err != nil {
		t.Fatalf("set alice display name: %v", err)
	}
	if err := users.UpdateProfile(ctx, bobID, stringPtr("Thread Bob"), "", "", 0, "", true); err != nil {
		t.Fatalf("set bob display name: %v", err)
	}
	postID := mustCreatePost(t, ctx, posts, aliceID, "Thread host", "Body", time.Now().UTC())

	comment1ID := mustCreateComment(t, ctx, comments, postID, nil, aliceID, "Need to check migration appendix", time.Now().UTC().Add(-2*time.Minute))
	_ = mustCreateComment(t, ctx, comments, postID, nil, bobID, "I can help with setup", time.Now().UTC().Add(-time.Minute))

	byBody, err := comments.ListByPost(ctx, postID, domain.CommentFilter{Query: "appendix"})
	if err != nil {
		t.Fatalf("comment search by body: %v", err)
	}
	if len(byBody) != 1 || byBody[0].ID != comment1ID {
		t.Fatalf("expected only comment %d by body search, got %+v", comment1ID, byBody)
	}

	byAuthor, err := comments.ListByPost(ctx, postID, domain.CommentFilter{Query: "bob_THREAD"})
	if err != nil {
		t.Fatalf("comment search by author: %v", err)
	}
	if len(byAuthor) != 1 {
		t.Fatalf("expected 1 comment for author search, got %d", len(byAuthor))
	}
	if byAuthor[0].Author.Username != "bob_thread" || byAuthor[0].Author.DisplayName != "Thread Bob" || byAuthor[0].Author.ID != bobID {
		t.Fatalf("expected author object on comment search result, got %+v", byAuthor[0].Author)
	}

	byAuthorTag, err := comments.ListByPost(ctx, postID, domain.CommentFilter{Query: "@bob_THREAD"})
	if err != nil {
		t.Fatalf("comment search by @author: %v", err)
	}
	if len(byAuthorTag) != 1 || byAuthorTag[0].ID != byAuthor[0].ID {
		t.Fatalf("expected same comment for @author search, got %+v", byAuthorTag)
	}

	byDisplayName, err := comments.ListByPost(ctx, postID, domain.CommentFilter{Query: "Thread Bob"})
	if err != nil {
		t.Fatalf("comment search by display name: %v", err)
	}
	if len(byDisplayName) != 1 || byDisplayName[0].ID != byAuthor[0].ID {
		t.Fatalf("expected same comment for display name search, got %+v", byDisplayName)
	}

	got, err := comments.GetByID(ctx, comment1ID)
	if err != nil {
		t.Fatalf("get comment by id: %v", err)
	}
	if got.Author.Username != "alice_thread" || got.Author.DisplayName != "Thread Alice" || got.Author.ID != aliceID {
		t.Fatalf("expected author object on comment get by id, got %+v", got.Author)
	}
}

func stringPtr(value string) *string {
	return &value
}

func mustCreateUser(t *testing.T, ctx context.Context, users *UserRepo, email, username string) int64 {
	t.Helper()
	id, err := users.Create(ctx, &domain.User{
		Email:     email,
		Username:  username,
		PassHash:  "hash",
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create user %s: %v", username, err)
	}
	return id
}

func mustCreatePost(t *testing.T, ctx context.Context, posts *PostRepo, userID int64, title, body string, createdAt time.Time) int64 {
	t.Helper()
	id, err := posts.Create(ctx, &domain.Post{
		UserID:    userID,
		Title:     title,
		Body:      body,
		CreatedAt: createdAt,
	}, nil)
	if err != nil {
		t.Fatalf("create post %q: %v", title, err)
	}
	return id
}

func mustCreateComment(t *testing.T, ctx context.Context, comments *CommentRepo, postID int64, parentID *int64, userID int64, body string, createdAt time.Time) int64 {
	t.Helper()
	id, err := comments.Create(ctx, &domain.Comment{
		PostID:    postID,
		ParentID:  parentID,
		UserID:    userID,
		Body:      body,
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("create comment %q: %v", body, err)
	}
	return id
}

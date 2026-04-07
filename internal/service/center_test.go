package service

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"forum/internal/domain"
	"forum/internal/repo/sqlite"
)

type centerFixture struct {
	auth   *AuthService
	posts  *PostService
	center *CenterService
	db     *sql.DB
}

func newCenterFixture(t *testing.T, now time.Time) (*centerFixture, func()) {
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
	centerRepo := sqlite.NewCenterRepo(db)

	testClock := fixedClock{t: now.UTC()}
	authService := NewAuthService(userRepo, sessionRepo, testClock, &seqID{}, 24*time.Hour)
	centerService := NewCenterService(centerRepo, userRepo, postRepo, commentRepo, testClock)
	postService := NewPostService(postRepo, commentRepo, categoryRepo, reactionRepo, nil, testClock, centerService)

	return &centerFixture{
		auth:   authService,
		posts:  postService,
		center: centerService,
		db:     db,
	}, func() { _ = db.Close() }
}

func registerTestUser(t *testing.T, auth *AuthService, email, username string) int64 {
	t.Helper()
	user, err := auth.Register(context.Background(), email, username, "secret")
	if err != nil {
		t.Fatalf("register %s: %v", username, err)
	}
	return user.ID
}

func TestCenterService_ReactionSwitchCreatesNewNotificationsAndSkipsSelf(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	fixture, cleanup := newCenterFixture(t, now)
	defer cleanup()

	aliceID := registerTestUser(t, fixture.auth, "alice-center@example.com", "alice_center")
	bobID := registerTestUser(t, fixture.auth, "bob-center@example.com", "bob_center")

	post, err := fixture.posts.CreatePost(context.Background(), aliceID, "Roadmap", "Quarterly roadmap body", []int64{1}, nil)
	if err != nil {
		t.Fatalf("create post: %v", err)
	}

	if _, err := fixture.posts.ReactPost(context.Background(), bobID, post.ID, 1); err != nil {
		t.Fatalf("like post: %v", err)
	}
	if _, err := fixture.posts.ReactPost(context.Background(), bobID, post.ID, -1); err != nil {
		t.Fatalf("switch reaction: %v", err)
	}
	if _, err := fixture.posts.ReactPost(context.Background(), aliceID, post.ID, 1); err != nil {
		t.Fatalf("self like post: %v", err)
	}

	notifications, err := fixture.center.ListNotifications(context.Background(), aliceID, domain.NotificationFilter{
		Bucket: domain.NotificationBucketMyContent,
		Limit:  20,
	})
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}

	if len(notifications.Items) != 2 {
		t.Fatalf("expected 2 notifications, got %d", len(notifications.Items))
	}
	if notifications.Items[0].Type != domain.NotificationTypePostDisliked {
		t.Fatalf("expected newest notification type %q, got %q", domain.NotificationTypePostDisliked, notifications.Items[0].Type)
	}
	if notifications.Items[1].Type != domain.NotificationTypePostLiked {
		t.Fatalf("expected previous notification type %q, got %q", domain.NotificationTypePostLiked, notifications.Items[1].Type)
	}
	if notifications.Summary.Total != 2 || notifications.Summary.MyContent != 2 {
		t.Fatalf("unexpected unread summary: %+v", notifications.Summary)
	}
}

func TestCenterService_CommentNotificationsRespectMyContentAndSubscriptionsBuckets(t *testing.T) {
	now := time.Unix(1700000100, 0).UTC()
	fixture, cleanup := newCenterFixture(t, now)
	defer cleanup()

	aliceID := registerTestUser(t, fixture.auth, "alice-comments@example.com", "alice_comments")
	bobID := registerTestUser(t, fixture.auth, "bob-comments@example.com", "bob_comments")
	charlieID := registerTestUser(t, fixture.auth, "charlie-comments@example.com", "charlie_comments")

	post, err := fixture.posts.CreatePost(context.Background(), aliceID, "Thread", "Discussion body", []int64{1}, nil)
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	if err := fixture.center.SubscribePost(context.Background(), bobID, post.ID); err != nil {
		t.Fatalf("subscribe post: %v", err)
	}

	if _, err := fixture.posts.CreateComment(context.Background(), charlieID, post.ID, "Count me in", nil); err != nil {
		t.Fatalf("create comment: %v", err)
	}

	aliceNotifications, err := fixture.center.ListNotifications(context.Background(), aliceID, domain.NotificationFilter{
		Bucket: domain.NotificationBucketMyContent,
		Limit:  20,
	})
	if err != nil {
		t.Fatalf("list alice notifications: %v", err)
	}
	if len(aliceNotifications.Items) != 1 || aliceNotifications.Items[0].Type != domain.NotificationTypePostCommented {
		t.Fatalf("expected one my-content comment notification for alice, got %+v", aliceNotifications.Items)
	}

	aliceSubscriptions, err := fixture.center.ListNotifications(context.Background(), aliceID, domain.NotificationFilter{
		Bucket: domain.NotificationBucketSubscriptions,
		Limit:  20,
	})
	if err != nil {
		t.Fatalf("list alice subscription notifications: %v", err)
	}
	if len(aliceSubscriptions.Items) != 0 {
		t.Fatalf("expected no duplicate subscription notifications for author, got %+v", aliceSubscriptions.Items)
	}

	bobNotifications, err := fixture.center.ListNotifications(context.Background(), bobID, domain.NotificationFilter{
		Bucket: domain.NotificationBucketSubscriptions,
		Limit:  20,
	})
	if err != nil {
		t.Fatalf("list bob notifications: %v", err)
	}
	if len(bobNotifications.Items) != 1 || bobNotifications.Items[0].Type != domain.NotificationTypeSubscribedPostCommented {
		t.Fatalf("expected one subscription notification for bob, got %+v", bobNotifications.Items)
	}
}

func TestCenterService_FollowingAuthorCreatesNewPostNotification(t *testing.T) {
	now := time.Unix(1700000200, 0).UTC()
	fixture, cleanup := newCenterFixture(t, now)
	defer cleanup()

	aliceID := registerTestUser(t, fixture.auth, "alice-follow@example.com", "alice_follow")
	bobID := registerTestUser(t, fixture.auth, "bob-follow@example.com", "bob_follow")

	if err := fixture.center.FollowUser(context.Background(), bobID, aliceID); err != nil {
		t.Fatalf("follow user: %v", err)
	}
	if err := fixture.center.FollowUser(context.Background(), aliceID, aliceID); err == nil {
		t.Fatal("expected self-follow to fail")
	}

	if _, err := fixture.posts.CreatePost(context.Background(), aliceID, "Launch", "Ship it", []int64{1}, nil); err != nil {
		t.Fatalf("create followed author post: %v", err)
	}

	notifications, err := fixture.center.ListNotifications(context.Background(), bobID, domain.NotificationFilter{
		Bucket: domain.NotificationBucketSubscriptions,
		Limit:  20,
	})
	if err != nil {
		t.Fatalf("list follower notifications: %v", err)
	}
	if len(notifications.Items) != 1 || notifications.Items[0].Type != domain.NotificationTypeFollowedAuthorPublished {
		t.Fatalf("expected followed-author notification, got %+v", notifications.Items)
	}
}

func TestCenterService_ListActivityIncludesPostsReactionsAndComments(t *testing.T) {
	now := time.Unix(1700000300, 0).UTC()
	fixture, cleanup := newCenterFixture(t, now)
	defer cleanup()

	aliceID := registerTestUser(t, fixture.auth, "alice-activity@example.com", "alice_activity")
	bobID := registerTestUser(t, fixture.auth, "bob-activity@example.com", "bob_activity")

	ownPost, err := fixture.posts.CreatePost(context.Background(), aliceID, "My Post", "Own body", []int64{1}, nil)
	if err != nil {
		t.Fatalf("create own post: %v", err)
	}
	otherPost, err := fixture.posts.CreatePost(context.Background(), bobID, "Bob Post", "Bob body", []int64{1}, nil)
	if err != nil {
		t.Fatalf("create other post: %v", err)
	}
	if _, err := fixture.posts.ReactPost(context.Background(), aliceID, otherPost.ID, 1); err != nil {
		t.Fatalf("react to bob post: %v", err)
	}
	comment, err := fixture.posts.CreateComment(context.Background(), aliceID, otherPost.ID, "Alice comment", nil)
	if err != nil {
		t.Fatalf("create alice comment: %v", err)
	}

	activity, err := fixture.center.ListActivity(context.Background(), aliceID, 20, 0, 0, 0)
	if err != nil {
		t.Fatalf("list activity: %v", err)
	}

	if len(activity.Posts) != 1 || activity.Posts[0].ID != ownPost.ID {
		t.Fatalf("expected own post in activity, got %+v", activity.Posts)
	}
	if len(activity.Reactions) != 1 || activity.Reactions[0].PostID != otherPost.ID || activity.Reactions[0].Value != 1 {
		t.Fatalf("expected own reaction in activity, got %+v", activity.Reactions)
	}
	if len(activity.Comments) != 1 || activity.Comments[0].Comment.ID != comment.ID || activity.Comments[0].PostID != otherPost.ID {
		t.Fatalf("expected own comment with post context in activity, got %+v", activity.Comments)
	}
}

func TestCenterService_ListActivityFallsBackLegacyReactionTimestamp(t *testing.T) {
	now := time.Unix(1700000300, 0).UTC()
	fixture, cleanup := newCenterFixture(t, now)
	defer cleanup()

	aliceID := registerTestUser(t, fixture.auth, "alice-legacy@example.com", "alice_legacy")
	bobID := registerTestUser(t, fixture.auth, "bob-legacy@example.com", "bob_legacy")

	post, err := fixture.posts.CreatePost(context.Background(), bobID, "Legacy Post", "Legacy body", []int64{1}, nil)
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	comment, err := fixture.posts.CreateComment(context.Background(), bobID, post.ID, "Legacy comment", nil)
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}
	if _, err := fixture.posts.ReactPost(context.Background(), aliceID, post.ID, 1); err != nil {
		t.Fatalf("react post: %v", err)
	}
	if _, err := fixture.posts.ReactComment(context.Background(), aliceID, comment.ID, -1); err != nil {
		t.Fatalf("react comment: %v", err)
	}

	postFallback := time.Unix(1700000400, 0).UTC()
	commentFallback := time.Unix(1700000500, 0).UTC()
	if _, err := fixture.db.ExecContext(context.Background(), `UPDATE posts SET created_at = ? WHERE id = ?`, postFallback.Unix(), post.ID); err != nil {
		t.Fatalf("set post fallback created_at: %v", err)
	}
	if _, err := fixture.db.ExecContext(context.Background(), `UPDATE comments SET created_at = ? WHERE id = ?`, commentFallback.Unix(), comment.ID); err != nil {
		t.Fatalf("set comment fallback created_at: %v", err)
	}
	if _, err := fixture.db.ExecContext(context.Background(), `UPDATE post_reactions SET created_at = 0 WHERE post_id = ? AND user_id = ?`, post.ID, aliceID); err != nil {
		t.Fatalf("zero post reaction created_at: %v", err)
	}
	if _, err := fixture.db.ExecContext(context.Background(), `UPDATE comment_reactions SET created_at = 0 WHERE comment_id = ? AND user_id = ?`, comment.ID, aliceID); err != nil {
		t.Fatalf("zero comment reaction created_at: %v", err)
	}

	activity, err := fixture.center.ListActivity(context.Background(), aliceID, 20, 0, 0, 0)
	if err != nil {
		t.Fatalf("list activity: %v", err)
	}
	if len(activity.Reactions) != 2 {
		t.Fatalf("expected 2 reactions in activity, got %+v", activity.Reactions)
	}

	var (
		postReaction    *domain.ActivityReaction
		commentReaction *domain.ActivityReaction
	)
	for i := range activity.Reactions {
		switch activity.Reactions[i].TargetType {
		case domain.NotificationEntityTypePost:
			postReaction = &activity.Reactions[i]
		case domain.NotificationEntityTypeComment:
			commentReaction = &activity.Reactions[i]
		}
	}

	if postReaction == nil || commentReaction == nil {
		t.Fatalf("expected both post and comment reactions, got %+v", activity.Reactions)
	}
	if !postReaction.CreatedAt.Equal(postFallback) {
		t.Fatalf("expected post reaction fallback timestamp %v, got %v", postFallback, postReaction.CreatedAt)
	}
	if !commentReaction.CreatedAt.Equal(commentFallback) {
		t.Fatalf("expected comment reaction fallback timestamp %v, got %v", commentFallback, commentReaction.CreatedAt)
	}
}

func TestCenterService_MarkReadAndMarkAll(t *testing.T) {
	now := time.Unix(1700000400, 0).UTC()
	fixture, cleanup := newCenterFixture(t, now)
	defer cleanup()

	aliceID := registerTestUser(t, fixture.auth, "alice-read@example.com", "alice_read")
	bobID := registerTestUser(t, fixture.auth, "bob-read@example.com", "bob_read")

	post, err := fixture.posts.CreatePost(context.Background(), aliceID, "Read Test", "Post body", []int64{1}, nil)
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	if _, err := fixture.posts.ReactPost(context.Background(), bobID, post.ID, 1); err != nil {
		t.Fatalf("like post: %v", err)
	}
	if _, err := fixture.posts.ReactPost(context.Background(), bobID, post.ID, -1); err != nil {
		t.Fatalf("dislike post: %v", err)
	}

	notifications, err := fixture.center.ListNotifications(context.Background(), aliceID, domain.NotificationFilter{
		Bucket: domain.NotificationBucketMyContent,
		Limit:  20,
	})
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	if len(notifications.Items) != 2 {
		t.Fatalf("expected 2 notifications, got %d", len(notifications.Items))
	}

	item, summary, err := fixture.center.MarkNotificationRead(context.Background(), aliceID, notifications.Items[0].ID)
	if err != nil {
		t.Fatalf("mark one read: %v", err)
	}
	if !item.IsRead {
		t.Fatalf("expected marked notification to be read, got %+v", item)
	}
	if summary.Total != 1 || summary.MyContent != 1 {
		t.Fatalf("unexpected summary after mark one read: %+v", summary)
	}

	summary, err = fixture.center.MarkAllNotificationsRead(context.Background(), aliceID, domain.NotificationBucketMyContent)
	if err != nil {
		t.Fatalf("mark all read: %v", err)
	}
	if summary.Total != 0 || summary.MyContent != 0 {
		t.Fatalf("unexpected summary after mark all read: %+v", summary)
	}
}

func TestCenterService_DeletedPostNotificationKeepsDeletedContext(t *testing.T) {
	now := time.Unix(1700000600, 0).UTC()
	fixture, cleanup := newCenterFixture(t, now)
	defer cleanup()

	aliceID := registerTestUser(t, fixture.auth, "alice-deleted@example.com", "alice_deleted")
	bobID := registerTestUser(t, fixture.auth, "bob-deleted@example.com", "bob_deleted")

	post, err := fixture.posts.CreatePost(context.Background(), aliceID, "test test", "body", []int64{1}, nil)
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	if _, err := fixture.posts.CreateComment(context.Background(), bobID, post.ID, "once more", nil); err != nil {
		t.Fatalf("create comment: %v", err)
	}
	if err := fixture.posts.DeletePost(context.Background(), aliceID, post.ID); err != nil {
		t.Fatalf("delete post: %v", err)
	}

	notifications, err := fixture.center.ListNotifications(context.Background(), aliceID, domain.NotificationFilter{
		Bucket: domain.NotificationBucketMyContent,
		Limit:  20,
	})
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	if len(notifications.Items) != 1 {
		t.Fatalf("expected 1 notification, got %+v", notifications.Items)
	}
	if notifications.Items[0].EntityAvailable {
		t.Fatalf("expected deleted post notification to be unavailable, got %+v", notifications.Items[0])
	}
	if notifications.Items[0].Context != "[deleted] test test" {
		t.Fatalf("expected deleted post context, got %+v", notifications.Items[0])
	}
}

func TestCenterService_PostCommentNotificationUsesPostAvailabilityEvenIfCommentStillExists(t *testing.T) {
	now := time.Unix(1700000700, 0).UTC()
	fixture, cleanup := newCenterFixture(t, now)
	defer cleanup()

	aliceID := registerTestUser(t, fixture.auth, "alice-orphaned@example.com", "alice_orphaned")
	bobID := registerTestUser(t, fixture.auth, "bob-orphaned@example.com", "bob_orphaned")

	post, err := fixture.posts.CreatePost(context.Background(), aliceID, "18", "body", []int64{1}, nil)
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	comment, err := fixture.posts.CreateComment(context.Background(), bobID, post.ID, "once more", nil)
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}
	if comment.ID <= 0 {
		t.Fatalf("expected comment id, got %+v", comment)
	}

	conn, err := fixture.db.Conn(context.Background())
	if err != nil {
		t.Fatalf("open db conn: %v", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(context.Background(), `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatalf("disable foreign keys: %v", err)
	}
	if _, err := conn.ExecContext(context.Background(), `DELETE FROM posts WHERE id = ?`, post.ID); err != nil {
		t.Fatalf("delete post without cascade: %v", err)
	}
	if _, err := conn.ExecContext(context.Background(), `PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}

	notifications, err := fixture.center.ListNotifications(context.Background(), aliceID, domain.NotificationFilter{
		Bucket: domain.NotificationBucketMyContent,
		Limit:  20,
	})
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	if len(notifications.Items) != 1 {
		t.Fatalf("expected 1 notification, got %+v", notifications.Items)
	}
	if notifications.Items[0].EntityAvailable {
		t.Fatalf("expected deleted post notification to be unavailable, got %+v", notifications.Items[0])
	}
	if notifications.Items[0].Context != "[deleted] 18" {
		t.Fatalf("expected deleted post label, got %+v", notifications.Items[0])
	}
}

func TestBuildNotificationLinkPath_DeletedCommentUsesCommentLinkAndFallsBackToPost(t *testing.T) {
	commentLink := buildNotificationLinkPath(domain.Notification{
		Type:       "content_deleted",
		EntityType: domain.NotificationEntityTypeComment,
		EntityID:   77,
		Payload: domain.NotificationPayload{
			PostID:    42,
			CommentID: 77,
		},
	})
	if commentLink != "/post/42#comment-77" {
		t.Fatalf("expected deleted comment deep link, got %q", commentLink)
	}

	postFallbackLink := buildNotificationLinkPath(domain.Notification{
		Type:       "content_deleted",
		EntityType: domain.NotificationEntityTypeComment,
		Payload: domain.NotificationPayload{
			PostID: 42,
		},
	})
	if postFallbackLink != "/post/42" {
		t.Fatalf("expected deleted comment fallback post link, got %q", postFallbackLink)
	}

	postLink := buildNotificationLinkPath(domain.Notification{
		Type:       "content_deleted",
		EntityType: domain.NotificationEntityTypePost,
		EntityID:   15,
	})
	if postLink != "/post/15" {
		t.Fatalf("expected deleted post link to remain unchanged, got %q", postLink)
	}
}

package service

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"forum/internal/domain"
	"forum/internal/repo/sqlite"
)

type moderationFixture struct {
	auth       *AuthService
	posts      *PostService
	center     *CenterService
	moderation *ModerationService
	centerRepo *sqlite.CenterRepo
	modRepo    *sqlite.ModerationRepo
	users      *sqlite.UserRepo
	categories *sqlite.CategoryRepo
	comments   *sqlite.CommentRepo
}

func newModerationFixture(t *testing.T, now time.Time) (*moderationFixture, func()) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
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
	moderationRepo := sqlite.NewModerationRepo(db)

	testClock := fixedClock{t: now.UTC()}
	authService := NewAuthService(userRepo, sessionRepo, testClock, &seqID{}, 24*time.Hour)
	centerService := NewCenterService(centerRepo, userRepo, postRepo, commentRepo, testClock)
	postService := NewPostService(postRepo, commentRepo, categoryRepo, reactionRepo, nil, testClock, centerService)
	moderationService := NewModerationService(userRepo, postRepo, commentRepo, categoryRepo, moderationRepo, testClock, centerService)
	centerService.SetAppealChecker(moderationService)

	return &moderationFixture{
		auth:       authService,
		posts:      postService,
		center:     centerService,
		moderation: moderationService,
		centerRepo: centerRepo,
		modRepo:    moderationRepo,
		users:      userRepo,
		categories: categoryRepo,
		comments:   commentRepo,
	}, func() { _ = db.Close() }
}

func bootstrapOwner(t *testing.T, fixture *moderationFixture) *domain.User {
	t.Helper()
	owner, err := fixture.moderation.BootstrapOwner(context.Background(), "owner@example.com", "owner", "secret")
	if err != nil {
		t.Fatalf("bootstrap owner: %v", err)
	}
	return owner
}

func userRole(t *testing.T, fixture *moderationFixture, userID int64) domain.UserRole {
	t.Helper()
	user, err := fixture.users.GetByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("get user %d: %v", userID, err)
	}
	return user.Role
}

func TestModerationService_BootstrapOwnerOnlyOnce(t *testing.T) {
	now := time.Unix(1700010000, 0).UTC()
	fixture, cleanup := newModerationFixture(t, now)
	defer cleanup()

	owner := bootstrapOwner(t, fixture)
	if owner.Role != domain.RoleOwner {
		t.Fatalf("expected owner role, got %s", owner.Role)
	}

	if _, err := fixture.moderation.BootstrapOwner(context.Background(), "owner2@example.com", "owner2", "secret"); !errors.Is(err, ErrBootstrapUnavailable) {
		t.Fatalf("expected bootstrap unavailable, got %v", err)
	}
}

func TestModerationService_RoleRequestsAndDirectChanges(t *testing.T) {
	now := time.Unix(1700010100, 0).UTC()
	fixture, cleanup := newModerationFixture(t, now)
	defer cleanup()

	ctx := context.Background()
	owner := bootstrapOwner(t, fixture)
	adminID := registerTestUser(t, fixture.auth, "admin@example.com", "admin")
	modID := registerTestUser(t, fixture.auth, "moder@example.com", "moder")
	secondModID := registerTestUser(t, fixture.auth, "moder-two@example.com", "moder_two")

	adminUser, err := fixture.moderation.ChangeUserRole(ctx, owner.ID, adminID, domain.RoleAdmin, "bootstrap admin")
	if err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	if adminUser.Role != domain.RoleAdmin {
		t.Fatalf("expected admin role, got %s", adminUser.Role)
	}

	modRequest, err := fixture.moderation.RequestRole(ctx, modID, domain.RoleModerator, "ready to moderate")
	if err != nil {
		t.Fatalf("request moderator: %v", err)
	}
	if modRequest.RequestedRole != domain.RoleModerator {
		t.Fatalf("unexpected requested role: %+v", modRequest)
	}
	if _, err := fixture.moderation.ReviewRoleRequest(ctx, adminID, modRequest.ID, true, "approved"); err != nil {
		t.Fatalf("admin approve moderator: %v", err)
	}
	if role := userRole(t, fixture, modID); role != domain.RoleModerator {
		t.Fatalf("expected moderator role, got %s", role)
	}

	adminRequest, err := fixture.moderation.RequestRole(ctx, modID, domain.RoleAdmin, "ready for admin")
	if err != nil {
		t.Fatalf("request admin: %v", err)
	}
	if _, err := fixture.moderation.ReviewRoleRequest(ctx, owner.ID, adminRequest.ID, true, "approved"); err != nil {
		t.Fatalf("owner approve admin: %v", err)
	}
	if role := userRole(t, fixture, modID); role != domain.RoleAdmin {
		t.Fatalf("expected admin role after owner approval, got %s", role)
	}

	secondModRequest, err := fixture.moderation.RequestRole(ctx, secondModID, domain.RoleModerator, "moderator request")
	if err != nil {
		t.Fatalf("second moderator request: %v", err)
	}
	if _, err := fixture.moderation.ReviewRoleRequest(ctx, adminID, secondModRequest.ID, true, "approved"); err != nil {
		t.Fatalf("admin approve second moderator: %v", err)
	}
	if _, err := fixture.moderation.ChangeUserRole(ctx, adminID, secondModID, domain.RoleUser, "demote moderator"); err != nil {
		t.Fatalf("admin demote moderator: %v", err)
	}
	if role := userRole(t, fixture, secondModID); role != domain.RoleUser {
		t.Fatalf("expected user role after admin demotion, got %s", role)
	}

	if _, err := fixture.moderation.ChangeUserRole(ctx, owner.ID, modID, domain.RoleModerator, "demote admin"); err != nil {
		t.Fatalf("owner demote admin: %v", err)
	}
	if role := userRole(t, fixture, modID); role != domain.RoleModerator {
		t.Fatalf("expected moderator role after owner demotion, got %s", role)
	}
}

func TestModerationService_DeleteRestoreAndProtection(t *testing.T) {
	now := time.Unix(1700010200, 0).UTC()
	fixture, cleanup := newModerationFixture(t, now)
	defer cleanup()

	ctx := context.Background()
	owner := bootstrapOwner(t, fixture)
	adminID := registerTestUser(t, fixture.auth, "admin-delete@example.com", "admin_delete")
	mod1ID := registerTestUser(t, fixture.auth, "mod-one@example.com", "mod_one")
	mod2ID := registerTestUser(t, fixture.auth, "mod-two@example.com", "mod_two")
	authorID := registerTestUser(t, fixture.auth, "author-delete@example.com", "author_delete")
	commenterID := registerTestUser(t, fixture.auth, "commenter-delete@example.com", "commenter_delete")

	if _, err := fixture.moderation.ChangeUserRole(ctx, owner.ID, adminID, domain.RoleAdmin, "admin"); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	if _, err := fixture.moderation.ChangeUserRole(ctx, owner.ID, mod1ID, domain.RoleModerator, "moderator one"); err != nil {
		t.Fatalf("promote mod1: %v", err)
	}
	if _, err := fixture.moderation.ChangeUserRole(ctx, owner.ID, mod2ID, domain.RoleModerator, "moderator two"); err != nil {
		t.Fatalf("promote mod2: %v", err)
	}

	protectedPost, err := fixture.posts.CreatePost(ctx, authorID, "Protected", "Protected body", []int64{1}, nil)
	if err != nil {
		t.Fatalf("create protected post: %v", err)
	}
	if _, err := fixture.moderation.SetPostDeleteProtection(ctx, owner.ID, protectedPost.ID, true, "protect"); err != nil {
		t.Fatalf("protect post: %v", err)
	}
	if err := fixture.moderation.SoftDeleteContent(ctx, mod1ID, domain.ModerationTargetPost, protectedPost.ID, domain.ModerationReasonObscene, "blocked"); !errors.Is(err, ErrProtectedContent) {
		t.Fatalf("expected protected-content error, got %v", err)
	}

	post, err := fixture.posts.CreatePost(ctx, authorID, "Foreign post", "body", []int64{1}, nil)
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	if err := fixture.moderation.SoftDeleteContent(ctx, mod1ID, domain.ModerationTargetPost, post.ID, domain.ModerationReasonObscene, "remove post"); err != nil {
		t.Fatalf("moderator soft delete post: %v", err)
	}
	if err := fixture.moderation.RestoreContent(ctx, mod2ID, domain.ModerationTargetPost, post.ID, "other moderator"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected forbidden restore for other moderator, got %v", err)
	}
	if err := fixture.moderation.RestoreContent(ctx, adminID, domain.ModerationTargetPost, post.ID, "admin restore"); err != nil {
		t.Fatalf("admin restore any post: %v", err)
	}

	commentPost, err := fixture.posts.CreatePost(ctx, authorID, "Comment target", "body", []int64{1}, nil)
	if err != nil {
		t.Fatalf("create comment post: %v", err)
	}
	comment, err := fixture.posts.CreateComment(ctx, commenterID, commentPost.ID, "comment body", nil)
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}
	if err := fixture.moderation.SoftDeleteContent(ctx, mod1ID, domain.ModerationTargetComment, comment.ID, domain.ModerationReasonObscene, "remove comment"); err != nil {
		t.Fatalf("moderator soft delete comment: %v", err)
	}
	if err := fixture.moderation.RestoreContent(ctx, mod1ID, domain.ModerationTargetComment, comment.ID, "own restore"); err != nil {
		t.Fatalf("moderator restore own comment: %v", err)
	}

	hardDeletePost, err := fixture.posts.CreatePost(ctx, authorID, "Hard delete", "body", []int64{1}, nil)
	if err != nil {
		t.Fatalf("create hard-delete post: %v", err)
	}
	if _, err := fixture.posts.CreateComment(ctx, commenterID, hardDeletePost.ID, "child comment", nil); err != nil {
		t.Fatalf("create post child comment: %v", err)
	}
	if err := fixture.moderation.HardDeleteContent(ctx, adminID, domain.ModerationTargetPost, hardDeletePost.ID, domain.ModerationReasonIllegal, "purge post"); err != nil {
		t.Fatalf("admin hard delete post: %v", err)
	}
	if _, err := fixture.posts.GetPost(ctx, hardDeletePost.ID, domain.RoleUser); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected deleted post to be gone, got %v", err)
	}

	hardDeleteCommentPost, err := fixture.posts.CreatePost(ctx, authorID, "Comment purge", "body", []int64{1}, nil)
	if err != nil {
		t.Fatalf("create comment purge post: %v", err)
	}
	commentToPurge, err := fixture.posts.CreateComment(ctx, commenterID, hardDeleteCommentPost.ID, "purge me", nil)
	if err != nil {
		t.Fatalf("create purge comment: %v", err)
	}
	if err := fixture.moderation.SoftDeleteContent(ctx, mod1ID, domain.ModerationTargetComment, commentToPurge.ID, domain.ModerationReasonObscene, "soft delete before purge"); err != nil {
		t.Fatalf("soft delete purge comment: %v", err)
	}
	if err := fixture.moderation.HardDeleteContent(ctx, owner.ID, domain.ModerationTargetComment, commentToPurge.ID, domain.ModerationReasonIllegal, "owner purge comment"); err != nil {
		t.Fatalf("owner hard delete comment: %v", err)
	}
	if _, err := fixture.comments.GetByID(ctx, commentToPurge.ID); err == nil {
		t.Fatalf("expected hard-deleted comment to be removed")
	}
}

func TestModerationService_ReportRoutingAndAppeals(t *testing.T) {
	now := time.Unix(1700010300, 0).UTC()
	fixture, cleanup := newModerationFixture(t, now)
	defer cleanup()

	ctx := context.Background()
	owner := bootstrapOwner(t, fixture)
	adminID := registerTestUser(t, fixture.auth, "admin-route@example.com", "admin_route")
	moderatorID := registerTestUser(t, fixture.auth, "moder-route@example.com", "moder_route")
	reporterID := registerTestUser(t, fixture.auth, "reporter@example.com", "reporter")
	authorID := registerTestUser(t, fixture.auth, "author-route@example.com", "author_route")

	if _, err := fixture.moderation.ChangeUserRole(ctx, owner.ID, adminID, domain.RoleAdmin, "admin"); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	if _, err := fixture.moderation.ChangeUserRole(ctx, owner.ID, moderatorID, domain.RoleModerator, "moderator"); err != nil {
		t.Fatalf("promote moderator: %v", err)
	}

	reportPost, err := fixture.posts.CreatePost(ctx, authorID, "Reported post", "body", []int64{1}, nil)
	if err != nil {
		t.Fatalf("create report post: %v", err)
	}

	userReport, err := fixture.moderation.CreateReport(ctx, reporterID, domain.ModerationTargetPost, reportPost.ID, domain.ModerationReasonObscene, "user report")
	if err != nil {
		t.Fatalf("user report: %v", err)
	}
	if len(userReport.RouteToRoles) != 3 {
		t.Fatalf("expected user report routed to moderator/admin/owner, got %+v", userReport.RouteToRoles)
	}

	staffReport, err := fixture.moderation.CreateReport(ctx, moderatorID, domain.ModerationTargetPost, reportPost.ID, domain.ModerationReasonIllegal, "staff escalation")
	if err != nil {
		t.Fatalf("moderator report: %v", err)
	}
	if len(staffReport.RouteToRoles) != 2 {
		t.Fatalf("expected moderator report routed to admin/owner, got %+v", staffReport.RouteToRoles)
	}

	moderatorReports, err := fixture.moderation.ListReports(ctx, moderatorID, false, domain.ModerationStatusPending)
	if err != nil {
		t.Fatalf("list moderator reports: %v", err)
	}
	if len(moderatorReports) != 1 || moderatorReports[0].ID != userReport.ID {
		t.Fatalf("expected only user report for moderator, got %+v", moderatorReports)
	}

	adminReports, err := fixture.moderation.ListReports(ctx, adminID, false, domain.ModerationStatusPending)
	if err != nil {
		t.Fatalf("list admin reports: %v", err)
	}
	if len(adminReports) != 2 {
		t.Fatalf("expected two pending reports for admin, got %d", len(adminReports))
	}

	if _, err := fixture.moderation.CloseReport(ctx, adminID, userReport.ID, true, domain.ModerationReasonObscene, "action taken"); err != nil {
		t.Fatalf("close report: %v", err)
	}
	ownerPendingReports, err := fixture.moderation.ListReports(ctx, owner.ID, false, domain.ModerationStatusPending)
	if err != nil {
		t.Fatalf("list owner reports: %v", err)
	}
	if len(ownerPendingReports) != 1 || ownerPendingReports[0].ID != staffReport.ID {
		t.Fatalf("expected only unresolved staff report after close, got %+v", ownerPendingReports)
	}

	appealPost, err := fixture.posts.CreatePost(ctx, authorID, "Appeal post", "body", []int64{1}, nil)
	if err != nil {
		t.Fatalf("create appeal post: %v", err)
	}
	if err := fixture.moderation.SoftDeleteContent(ctx, moderatorID, domain.ModerationTargetPost, appealPost.ID, domain.ModerationReasonObscene, "moderator delete"); err != nil {
		t.Fatalf("moderator soft delete for appeal: %v", err)
	}

	appealToAdmin, err := fixture.moderation.CreateAppeal(ctx, authorID, domain.ModerationTargetPost, appealPost.ID, "appeal to admin")
	if err != nil {
		t.Fatalf("create appeal to admin: %v", err)
	}
	if appealToAdmin.TargetRole != domain.RoleAdmin {
		t.Fatalf("expected appeal route to admin, got %s", appealToAdmin.TargetRole)
	}
	adminAppeals, err := fixture.moderation.ListAppeals(ctx, adminID, false, domain.AppealStatusPending)
	if err != nil {
		t.Fatalf("list admin appeals: %v", err)
	}
	if len(adminAppeals) != 1 || adminAppeals[0].ID != appealToAdmin.ID {
		t.Fatalf("expected pending appeal for admin, got %+v", adminAppeals)
	}
	if _, err := fixture.moderation.CloseAppeal(ctx, adminID, appealToAdmin.ID, false, "uphold"); err != nil {
		t.Fatalf("close admin appeal: %v", err)
	}

	appealToOwner, err := fixture.moderation.CreateAppeal(ctx, authorID, domain.ModerationTargetPost, appealPost.ID, "appeal to owner")
	if err != nil {
		t.Fatalf("create appeal to owner: %v", err)
	}
	if appealToOwner.TargetRole != domain.RoleOwner {
		t.Fatalf("expected appeal route to owner, got %s", appealToOwner.TargetRole)
	}
	ownerAppeals, err := fixture.moderation.ListAppeals(ctx, owner.ID, false, domain.AppealStatusPending)
	if err != nil {
		t.Fatalf("list owner appeals: %v", err)
	}
	if len(ownerAppeals) != 1 || ownerAppeals[0].ID != appealToOwner.ID {
		t.Fatalf("expected pending appeal for owner, got %+v", ownerAppeals)
	}
	if _, err := fixture.moderation.CloseAppeal(ctx, owner.ID, appealToOwner.ID, false, "final"); err != nil {
		t.Fatalf("close owner appeal: %v", err)
	}
	if _, err := fixture.moderation.CreateAppeal(ctx, authorID, domain.ModerationTargetPost, appealPost.ID, "third appeal"); !errors.Is(err, ErrNoFurtherAppeal) {
		t.Fatalf("expected no further appeal, got %v", err)
	}
}

func TestModerationService_CreateAppealRequiresContentAuthor(t *testing.T) {
	now := time.Unix(1700010350, 0).UTC()
	fixture, cleanup := newModerationFixture(t, now)
	defer cleanup()

	ctx := context.Background()
	owner := bootstrapOwner(t, fixture)
	adminID := registerTestUser(t, fixture.auth, "admin-appeal@example.com", "admin_appeal")
	moderatorID := registerTestUser(t, fixture.auth, "moderator-appeal@example.com", "moderator_appeal")
	postAuthorID := registerTestUser(t, fixture.auth, "post-author@example.com", "post_author")
	commentAuthorID := registerTestUser(t, fixture.auth, "comment-author@example.com", "comment_author")
	otherUserID := registerTestUser(t, fixture.auth, "other-appeal@example.com", "other_appeal")

	if _, err := fixture.moderation.ChangeUserRole(ctx, owner.ID, adminID, domain.RoleAdmin, "admin"); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	if _, err := fixture.moderation.ChangeUserRole(ctx, owner.ID, moderatorID, domain.RoleModerator, "moderator"); err != nil {
		t.Fatalf("promote moderator: %v", err)
	}

	foreignPost, err := fixture.posts.CreatePost(ctx, postAuthorID, "Foreign post", "body", []int64{1}, nil)
	if err != nil {
		t.Fatalf("create foreign post: %v", err)
	}
	if err := fixture.moderation.SoftDeleteContent(ctx, moderatorID, domain.ModerationTargetPost, foreignPost.ID, domain.ModerationReasonObscene, "moderator delete"); err != nil {
		t.Fatalf("soft delete foreign post: %v", err)
	}
	if _, err := fixture.moderation.CreateAppeal(ctx, otherUserID, domain.ModerationTargetPost, foreignPost.ID, "foreign appeal"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected forbidden on foreign post appeal, got %v", err)
	}

	ownPost, err := fixture.posts.CreatePost(ctx, postAuthorID, "Own post", "body", []int64{1}, nil)
	if err != nil {
		t.Fatalf("create own post: %v", err)
	}
	if err := fixture.moderation.SoftDeleteContent(ctx, moderatorID, domain.ModerationTargetPost, ownPost.ID, domain.ModerationReasonObscene, "moderator delete own post"); err != nil {
		t.Fatalf("soft delete own post: %v", err)
	}
	postAppeal, err := fixture.moderation.CreateAppeal(ctx, postAuthorID, domain.ModerationTargetPost, ownPost.ID, "own post appeal")
	if err != nil {
		t.Fatalf("expected own post appeal to succeed: %v", err)
	}
	if postAppeal.TargetRole != domain.RoleAdmin {
		t.Fatalf("expected post appeal to route to admin, got %s", postAppeal.TargetRole)
	}

	commentPost, err := fixture.posts.CreatePost(ctx, postAuthorID, "Comment post", "body", []int64{1}, nil)
	if err != nil {
		t.Fatalf("create comment post: %v", err)
	}
	foreignComment, err := fixture.posts.CreateComment(ctx, commentAuthorID, commentPost.ID, "foreign comment", nil)
	if err != nil {
		t.Fatalf("create foreign comment: %v", err)
	}
	if err := fixture.moderation.SoftDeleteContent(ctx, moderatorID, domain.ModerationTargetComment, foreignComment.ID, domain.ModerationReasonObscene, "delete foreign comment"); err != nil {
		t.Fatalf("soft delete foreign comment: %v", err)
	}
	if _, err := fixture.moderation.CreateAppeal(ctx, otherUserID, domain.ModerationTargetComment, foreignComment.ID, "foreign comment appeal"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected forbidden on foreign comment appeal, got %v", err)
	}

	ownComment, err := fixture.posts.CreateComment(ctx, commentAuthorID, commentPost.ID, "own comment", nil)
	if err != nil {
		t.Fatalf("create own comment: %v", err)
	}
	if err := fixture.moderation.SoftDeleteContent(ctx, moderatorID, domain.ModerationTargetComment, ownComment.ID, domain.ModerationReasonObscene, "delete own comment"); err != nil {
		t.Fatalf("soft delete own comment: %v", err)
	}
	commentAppeal, err := fixture.moderation.CreateAppeal(ctx, commentAuthorID, domain.ModerationTargetComment, ownComment.ID, "own comment appeal")
	if err != nil {
		t.Fatalf("expected own comment appeal to succeed: %v", err)
	}
	if commentAppeal.TargetRole != domain.RoleAdmin {
		t.Fatalf("expected comment appeal to route to admin, got %s", commentAppeal.TargetRole)
	}
}

func TestModerationService_CloseAppealReverseFailureLeavesAppealPending(t *testing.T) {
	now := time.Unix(1700010375, 0).UTC()
	fixture, cleanup := newModerationFixture(t, now)
	defer cleanup()

	ctx := context.Background()
	owner := bootstrapOwner(t, fixture)
	adminID := registerTestUser(t, fixture.auth, "admin-reverse-fail@example.com", "admin_reverse_fail")
	moderatorID := registerTestUser(t, fixture.auth, "moderator-reverse-fail@example.com", "moderator_reverse_fail")
	authorID := registerTestUser(t, fixture.auth, "author-reverse-fail@example.com", "author_reverse_fail")

	if _, err := fixture.moderation.ChangeUserRole(ctx, owner.ID, adminID, domain.RoleAdmin, "admin"); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	if _, err := fixture.moderation.ChangeUserRole(ctx, owner.ID, moderatorID, domain.RoleModerator, "moderator"); err != nil {
		t.Fatalf("promote moderator: %v", err)
	}

	post, err := fixture.posts.CreatePost(ctx, authorID, "Reverse fail", "body", []int64{1}, nil)
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	if err := fixture.moderation.SoftDeleteContent(ctx, moderatorID, domain.ModerationTargetPost, post.ID, domain.ModerationReasonObscene, "soft delete"); err != nil {
		t.Fatalf("soft delete post: %v", err)
	}
	appeal, err := fixture.moderation.CreateAppeal(ctx, authorID, domain.ModerationTargetPost, post.ID, "please restore")
	if err != nil {
		t.Fatalf("create appeal: %v", err)
	}
	if err := fixture.moderation.HardDeleteContent(ctx, owner.ID, domain.ModerationTargetPost, post.ID, domain.ModerationReasonIllegal, "final removal"); err != nil {
		t.Fatalf("hard delete post: %v", err)
	}

	if _, err := fixture.moderation.CloseAppeal(ctx, adminID, appeal.ID, true, "reverse"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected restore failure to surface as not found, got %v", err)
	}

	reloadedAppeal, err := fixture.modRepo.GetAppealByID(ctx, appeal.ID)
	if err != nil {
		t.Fatalf("reload appeal: %v", err)
	}
	if reloadedAppeal.Status != domain.AppealStatusPending {
		t.Fatalf("expected appeal to remain pending, got %s", reloadedAppeal.Status)
	}

	history, err := fixture.modRepo.ListHistory(ctx, domain.ModerationHistoryFilter{TargetType: domain.ModerationTargetAppeal, Limit: 20})
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	for _, item := range history {
		if item.TargetID == appeal.ID && item.ActionType == domain.ActionAppealClosed {
			t.Fatalf("unexpected appeal_closed history after failed reverse: %+v", item)
		}
	}

	notifications, err := fixture.centerRepo.ListNotifications(ctx, authorID, domain.NotificationFilter{
		Bucket: domain.NotificationBucketAppeals,
		Limit:  20,
	})
	if err != nil {
		t.Fatalf("list appeal notifications: %v", err)
	}
	if len(notifications) != 0 {
		t.Fatalf("expected no appeal_closed notification after failed reverse, got %+v", notifications)
	}
}

func TestModerationService_CloseAppealReverseSuccessRestoresContent(t *testing.T) {
	now := time.Unix(1700010380, 0).UTC()
	fixture, cleanup := newModerationFixture(t, now)
	defer cleanup()

	ctx := context.Background()
	owner := bootstrapOwner(t, fixture)
	adminID := registerTestUser(t, fixture.auth, "admin-reverse-ok@example.com", "admin_reverse_ok")
	moderatorID := registerTestUser(t, fixture.auth, "moderator-reverse-ok@example.com", "moderator_reverse_ok")
	authorID := registerTestUser(t, fixture.auth, "author-reverse-ok@example.com", "author_reverse_ok")

	if _, err := fixture.moderation.ChangeUserRole(ctx, owner.ID, adminID, domain.RoleAdmin, "admin"); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	if _, err := fixture.moderation.ChangeUserRole(ctx, owner.ID, moderatorID, domain.RoleModerator, "moderator"); err != nil {
		t.Fatalf("promote moderator: %v", err)
	}

	post, err := fixture.posts.CreatePost(ctx, authorID, "Reverse ok", "body", []int64{1}, nil)
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	if err := fixture.moderation.SoftDeleteContent(ctx, moderatorID, domain.ModerationTargetPost, post.ID, domain.ModerationReasonObscene, "soft delete"); err != nil {
		t.Fatalf("soft delete post: %v", err)
	}
	appeal, err := fixture.moderation.CreateAppeal(ctx, authorID, domain.ModerationTargetPost, post.ID, "restore me")
	if err != nil {
		t.Fatalf("create appeal: %v", err)
	}

	updated, err := fixture.moderation.CloseAppeal(ctx, adminID, appeal.ID, true, "reversed")
	if err != nil {
		t.Fatalf("close appeal reverse: %v", err)
	}
	if updated.Status != domain.AppealStatusReversed {
		t.Fatalf("expected reversed appeal, got %s", updated.Status)
	}

	reloadedPost, err := fixture.posts.GetPost(ctx, post.ID, domain.RoleUser)
	if err != nil {
		t.Fatalf("reload post: %v", err)
	}
	if reloadedPost.DeletedAt != nil {
		t.Fatalf("expected post to be restored, got deleted_at=%v", reloadedPost.DeletedAt)
	}
}

func TestModerationService_DeleteNotificationsContainSnapshots(t *testing.T) {
	now := time.Unix(1700010390, 0).UTC()
	fixture, cleanup := newModerationFixture(t, now)
	defer cleanup()

	ctx := context.Background()
	owner := bootstrapOwner(t, fixture)
	moderatorID := registerTestUser(t, fixture.auth, "moderator-notify@example.com", "moderator_notify")
	postAuthorID := registerTestUser(t, fixture.auth, "post-notify@example.com", "post_notify")
	commentAuthorID := registerTestUser(t, fixture.auth, "comment-notify@example.com", "comment_notify")

	if _, err := fixture.moderation.ChangeUserRole(ctx, owner.ID, moderatorID, domain.RoleModerator, "moderator"); err != nil {
		t.Fatalf("promote moderator: %v", err)
	}

	post, err := fixture.posts.CreatePost(ctx, postAuthorID, "Deleted title", "Deleted body content", []int64{1}, nil)
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	if err := fixture.moderation.SoftDeleteContent(ctx, moderatorID, domain.ModerationTargetPost, post.ID, domain.ModerationReasonObscene, "delete post"); err != nil {
		t.Fatalf("soft delete post: %v", err)
	}

	postNotifications, err := fixture.centerRepo.ListNotifications(ctx, postAuthorID, domain.NotificationFilter{
		Bucket: domain.NotificationBucketDeleted,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("list post delete notifications: %v", err)
	}
	if len(postNotifications) != 1 {
		t.Fatalf("expected one post delete notification, got %d", len(postNotifications))
	}
	postPayload := postNotifications[0].Payload
	if postPayload.PostTitle != "Deleted title" {
		t.Fatalf("expected deleted post title in payload, got %+v", postPayload)
	}
	if !strings.Contains(postPayload.PostPreview, "Deleted body content") {
		t.Fatalf("expected deleted post body preview in payload, got %+v", postPayload)
	}
	postItems, err := fixture.center.ListNotifications(ctx, postAuthorID, domain.NotificationFilter{
		Bucket: domain.NotificationBucketDeleted,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("list post delete notification items: %v", err)
	}
	if len(postItems.Items) != 1 || !strings.Contains(postItems.Items[0].Context, "Deleted title") || !strings.Contains(postItems.Items[0].Context, "Deleted body content") {
		t.Fatalf("expected deleted post context to show title and body, got %+v", postItems.Items)
	}
	if postItems.Items[0].LinkPath != "/post/"+strconv.FormatInt(post.ID, 10) {
		t.Fatalf("expected deleted post link path to stay on post, got %+v", postItems.Items[0])
	}

	commentPost, err := fixture.posts.CreatePost(ctx, postAuthorID, "Comment holder", "body", []int64{1}, nil)
	if err != nil {
		t.Fatalf("create comment holder post: %v", err)
	}
	comment, err := fixture.posts.CreateComment(ctx, commentAuthorID, commentPost.ID, "Comment body content", nil)
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}
	if err := fixture.moderation.SoftDeleteContent(ctx, moderatorID, domain.ModerationTargetComment, comment.ID, domain.ModerationReasonObscene, "delete comment"); err != nil {
		t.Fatalf("soft delete comment: %v", err)
	}

	commentNotifications, err := fixture.centerRepo.ListNotifications(ctx, commentAuthorID, domain.NotificationFilter{
		Bucket: domain.NotificationBucketDeleted,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("list comment delete notifications: %v", err)
	}
	if len(commentNotifications) != 1 {
		t.Fatalf("expected one comment delete notification, got %d", len(commentNotifications))
	}
	commentPayload := commentNotifications[0].Payload
	if !strings.Contains(commentPayload.CommentPreview, "Comment body content") {
		t.Fatalf("expected deleted comment body preview in payload, got %+v", commentPayload)
	}
	commentItems, err := fixture.center.ListNotifications(ctx, commentAuthorID, domain.NotificationFilter{
		Bucket: domain.NotificationBucketDeleted,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("list comment delete notification items: %v", err)
	}
	if len(commentItems.Items) != 1 || !strings.Contains(commentItems.Items[0].Context, "Comment body content") {
		t.Fatalf("expected deleted comment context to show body, got %+v", commentItems.Items)
	}
	expectedCommentPath := "/post/" + strconv.FormatInt(commentPost.ID, 10) + "#comment-" + strconv.FormatInt(comment.ID, 10)
	if commentItems.Items[0].LinkPath != expectedCommentPath {
		t.Fatalf("expected deleted comment link path %q, got %+v", expectedCommentPath, commentItems.Items[0])
	}
}

func TestModerationService_DestructiveActionsRequireNotes(t *testing.T) {
	now := time.Unix(1700010395, 0).UTC()
	fixture, cleanup := newModerationFixture(t, now)
	defer cleanup()

	ctx := context.Background()
	owner := bootstrapOwner(t, fixture)
	adminID := registerTestUser(t, fixture.auth, "admin-note@example.com", "admin_note")
	moderatorID := registerTestUser(t, fixture.auth, "moderator-note@example.com", "moderator_note")
	authorID := registerTestUser(t, fixture.auth, "author-note@example.com", "author_note")

	if _, err := fixture.moderation.ChangeUserRole(ctx, owner.ID, adminID, domain.RoleAdmin, "admin"); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	if _, err := fixture.moderation.ChangeUserRole(ctx, owner.ID, moderatorID, domain.RoleModerator, "moderator"); err != nil {
		t.Fatalf("promote moderator: %v", err)
	}

	post, err := fixture.posts.CreatePost(ctx, authorID, "Hard delete target", "body", []int64{1}, nil)
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	if err := fixture.moderation.SoftDeleteContent(ctx, moderatorID, domain.ModerationTargetPost, post.ID, domain.ModerationReasonObscene, "soft delete"); err != nil {
		t.Fatalf("soft delete post: %v", err)
	}
	if err := fixture.moderation.HardDeleteContent(ctx, adminID, domain.ModerationTargetPost, post.ID, domain.ModerationReasonIllegal, "   "); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected hard delete without note to fail, got %v", err)
	}
	if err := fixture.moderation.HardDeleteContent(ctx, adminID, domain.ModerationTargetPost, post.ID, domain.ModerationReasonIllegal, "valid hard delete note"); err != nil {
		t.Fatalf("expected hard delete with note to succeed, got %v", err)
	}

	category, err := fixture.moderation.CreateCategory(ctx, adminID, "Needs Note")
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	if _, err := fixture.moderation.DeleteCategory(ctx, adminID, category.ID, "   "); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected delete category without note to fail, got %v", err)
	}
	postInCategory, err := fixture.posts.CreatePost(ctx, authorID, "Category move", "body", []int64{category.ID}, nil)
	if err != nil {
		t.Fatalf("create category post: %v", err)
	}
	moved, err := fixture.moderation.DeleteCategory(ctx, adminID, category.ID, "move with note")
	if err != nil {
		t.Fatalf("expected delete category with note to succeed, got %v", err)
	}
	if moved != 1 {
		t.Fatalf("expected one moved post, got %d", moved)
	}
	reloadedPost, err := fixture.posts.GetPost(ctx, postInCategory.ID, domain.RoleUser)
	if err != nil {
		t.Fatalf("reload moved post: %v", err)
	}
	if len(reloadedPost.Categories) != 1 || reloadedPost.Categories[0].Code != "other" {
		t.Fatalf("expected moved post to land in other, got %+v", reloadedPost.Categories)
	}
}

func TestModerationService_DeleteCategoryMovesPostsToOther(t *testing.T) {
	now := time.Unix(1700010400, 0).UTC()
	fixture, cleanup := newModerationFixture(t, now)
	defer cleanup()

	ctx := context.Background()
	owner := bootstrapOwner(t, fixture)
	adminID := registerTestUser(t, fixture.auth, "admin-category@example.com", "admin_category")
	authorID := registerTestUser(t, fixture.auth, "author-category@example.com", "author_category")

	if _, err := fixture.moderation.ChangeUserRole(ctx, owner.ID, adminID, domain.RoleAdmin, "admin"); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	category, err := fixture.moderation.CreateCategory(ctx, adminID, "Guides")
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	post, err := fixture.posts.CreatePost(ctx, authorID, "Guide", "body", []int64{category.ID}, nil)
	if err != nil {
		t.Fatalf("create categorized post: %v", err)
	}

	moved, err := fixture.moderation.DeleteCategory(ctx, adminID, category.ID, "move to other")
	if err != nil {
		t.Fatalf("delete category: %v", err)
	}
	if moved != 1 {
		t.Fatalf("expected one moved post, got %d", moved)
	}

	reloaded, err := fixture.posts.GetPost(ctx, post.ID, domain.RoleUser)
	if err != nil {
		t.Fatalf("reload post: %v", err)
	}
	if len(reloaded.Categories) != 1 || reloaded.Categories[0].Code != "other" {
		t.Fatalf("expected post to move to other, got %+v", reloaded.Categories)
	}

	other, err := fixture.categories.GetByCode(ctx, "other")
	if err != nil {
		t.Fatalf("load other category: %v", err)
	}
	if _, err := fixture.moderation.DeleteCategory(ctx, adminID, other.ID, "should fail"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected deleting other to be forbidden, got %v", err)
	}
}

func TestModerationService_DeletedCommentNotificationProvidesAppealEntrypoint(t *testing.T) {
	now := time.Unix(1700010500, 0).UTC()
	fixture, cleanup := newModerationFixture(t, now)
	defer cleanup()

	ctx := context.Background()
	owner := bootstrapOwner(t, fixture)
	moderatorID := registerTestUser(t, fixture.auth, "moderator-comment-entry@example.com", "moderator_comment_entry")
	authorID := registerTestUser(t, fixture.auth, "author-comment-entry@example.com", "author_comment_entry")
	otherUserID := registerTestUser(t, fixture.auth, "other-comment-entry@example.com", "other_comment_entry")

	if _, err := fixture.moderation.ChangeUserRole(ctx, owner.ID, moderatorID, domain.RoleModerator, "promote moderator"); err != nil {
		t.Fatalf("promote moderator: %v", err)
	}

	post, err := fixture.posts.CreatePost(ctx, authorID, "Hidden thread", "body", []int64{1}, nil)
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	comment, err := fixture.posts.CreateComment(ctx, authorID, post.ID, "comment hidden from thread", nil)
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}
	if err := fixture.moderation.SoftDeleteContent(ctx, moderatorID, domain.ModerationTargetComment, comment.ID, domain.ModerationReasonObscene, "delete thread root"); err != nil {
		t.Fatalf("soft delete comment: %v", err)
	}

	visibleComments, err := fixture.comments.ListByPost(ctx, post.ID, domain.CommentFilter{})
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	if len(visibleComments) != 0 {
		t.Fatalf("expected fully deleted thread to be hidden, got %+v", visibleComments)
	}

	authorNotifications, err := fixture.center.ListNotifications(ctx, authorID, domain.NotificationFilter{
		Bucket: domain.NotificationBucketDeleted,
		Limit:  20,
	})
	if err != nil {
		t.Fatalf("list author deleted notifications: %v", err)
	}
	if len(authorNotifications.Items) != 1 {
		t.Fatalf("expected one deleted notification, got %+v", authorNotifications.Items)
	}
	item := authorNotifications.Items[0]
	if item.Type != "content_deleted" || item.EntityType != domain.NotificationEntityTypeComment || item.EntityID != comment.ID {
		t.Fatalf("unexpected deleted notification item: %+v", item)
	}
	if !item.CanAppeal {
		t.Fatalf("expected deleted comment notification to expose appeal action, got %+v", item)
	}
	expectedPath := "/post/" + strconv.FormatInt(post.ID, 10) + "#comment-" + strconv.FormatInt(comment.ID, 10)
	if item.LinkPath != expectedPath {
		t.Fatalf("expected deleted comment notification to link to comment path %q, got %+v", expectedPath, item)
	}
	if !strings.Contains(item.CommentPreview, "comment hidden from thread") {
		t.Fatalf("expected comment preview in notification, got %+v", item)
	}

	otherNotifications, err := fixture.center.ListNotifications(ctx, otherUserID, domain.NotificationFilter{
		Bucket: domain.NotificationBucketDeleted,
		Limit:  20,
	})
	if err != nil {
		t.Fatalf("list other deleted notifications: %v", err)
	}
	if len(otherNotifications.Items) != 0 {
		t.Fatalf("expected non-author to have no deleted notification entrypoint, got %+v", otherNotifications.Items)
	}

	if _, err := fixture.moderation.CreateAppeal(ctx, authorID, item.EntityType, item.EntityID, "appeal from deleted notification"); err != nil {
		t.Fatalf("create appeal from deleted notification: %v", err)
	}

	updatedNotifications, err := fixture.center.ListNotifications(ctx, authorID, domain.NotificationFilter{
		Bucket: domain.NotificationBucketDeleted,
		Limit:  20,
	})
	if err != nil {
		t.Fatalf("list updated deleted notifications: %v", err)
	}
	if len(updatedNotifications.Items) != 1 || updatedNotifications.Items[0].CanAppeal {
		t.Fatalf("expected appeal action to disappear after pending appeal, got %+v", updatedNotifications.Items)
	}

	ownerDeletedComment, err := fixture.posts.CreateComment(ctx, authorID, post.ID, "owner final decision", nil)
	if err != nil {
		t.Fatalf("create owner-deleted comment: %v", err)
	}
	if err := fixture.moderation.SoftDeleteContent(ctx, owner.ID, domain.ModerationTargetComment, ownerDeletedComment.ID, domain.ModerationReasonOther, "owner delete"); err != nil {
		t.Fatalf("owner soft delete comment: %v", err)
	}

	finalNotifications, err := fixture.center.ListNotifications(ctx, authorID, domain.NotificationFilter{
		Bucket: domain.NotificationBucketDeleted,
		Limit:  20,
	})
	if err != nil {
		t.Fatalf("list final deleted notifications: %v", err)
	}
	var ownerItem *domain.NotificationItem
	for i := range finalNotifications.Items {
		if finalNotifications.Items[i].EntityType == domain.NotificationEntityTypeComment && finalNotifications.Items[i].EntityID == ownerDeletedComment.ID {
			ownerItem = &finalNotifications.Items[i]
			break
		}
	}
	if ownerItem == nil {
		t.Fatalf("expected owner delete notification, got %+v", finalNotifications.Items)
	}
	if ownerItem.CanAppeal {
		t.Fatalf("expected no appeal action after owner decision, got %+v", ownerItem)
	}
}

func TestCenterService_DeleteNotificationOwnOnlyAndKeepsSourceData(t *testing.T) {
	now := time.Unix(1700010600, 0).UTC()
	fixture, cleanup := newModerationFixture(t, now)
	defer cleanup()

	ctx := context.Background()
	owner := bootstrapOwner(t, fixture)
	moderatorID := registerTestUser(t, fixture.auth, "moderator-delete-notification@example.com", "moderator_delete_notification")
	authorID := registerTestUser(t, fixture.auth, "author-delete-notification@example.com", "author_delete_notification")
	otherUserID := registerTestUser(t, fixture.auth, "other-delete-notification@example.com", "other_delete_notification")

	if _, err := fixture.moderation.ChangeUserRole(ctx, owner.ID, moderatorID, domain.RoleModerator, "promote moderator"); err != nil {
		t.Fatalf("promote moderator: %v", err)
	}

	post, err := fixture.posts.CreatePost(ctx, authorID, "Delete notification source", "body", []int64{1}, nil)
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	if err := fixture.moderation.SoftDeleteContent(ctx, moderatorID, domain.ModerationTargetPost, post.ID, domain.ModerationReasonObscene, "deleted content notification"); err != nil {
		t.Fatalf("soft delete post: %v", err)
	}

	notifications, err := fixture.center.ListNotifications(ctx, authorID, domain.NotificationFilter{
		Bucket: domain.NotificationBucketDeleted,
		Limit:  20,
	})
	if err != nil {
		t.Fatalf("list deleted notifications: %v", err)
	}
	if len(notifications.Items) != 1 {
		t.Fatalf("expected one deleted notification, got %+v", notifications.Items)
	}
	notificationID := notifications.Items[0].ID

	if _, err := fixture.center.DeleteNotification(ctx, otherUserID, notificationID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected deleting foreign notification to fail with not found, got %v", err)
	}

	summary, err := fixture.center.DeleteNotification(ctx, authorID, notificationID)
	if err != nil {
		t.Fatalf("delete notification: %v", err)
	}
	if summary.Total != 0 || summary.Deleted != 0 {
		t.Fatalf("expected unread summary to be cleared after delete, got %+v", summary)
	}

	remaining, err := fixture.center.ListNotifications(ctx, authorID, domain.NotificationFilter{
		Bucket: domain.NotificationBucketDeleted,
		Limit:  20,
	})
	if err != nil {
		t.Fatalf("list remaining notifications: %v", err)
	}
	if len(remaining.Items) != 0 {
		t.Fatalf("expected notification to disappear from list, got %+v", remaining.Items)
	}

	storedPost, err := fixture.posts.GetPost(ctx, post.ID, domain.RoleUser)
	if err != nil {
		t.Fatalf("get deleted post after notification delete: %v", err)
	}
	if storedPost.DeletedAt == nil {
		t.Fatalf("expected source post to remain deleted after notification delete, got %+v", storedPost)
	}

	history, err := fixture.moderation.ListHistory(ctx, owner.ID, domain.ModerationHistoryFilter{
		TargetType: domain.ModerationTargetPost,
		Limit:      20,
	})
	if err != nil {
		t.Fatalf("list moderation history: %v", err)
	}
	foundDeleteRecord := false
	for _, record := range history {
		if record.TargetID == post.ID && record.ActionType == domain.ActionPostSoftDeleted {
			foundDeleteRecord = true
			break
		}
	}
	if !foundDeleteRecord {
		t.Fatalf("expected moderation history to remain after notification delete, got %+v", history)
	}
}

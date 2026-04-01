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

type moderationFixture struct {
	auth       *AuthService
	posts      *PostService
	moderation *ModerationService
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

	return &moderationFixture{
		auth:       authService,
		posts:      postService,
		moderation: moderationService,
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

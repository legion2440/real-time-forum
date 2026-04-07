package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"forum/internal/domain"
	"forum/internal/platform/clock"
	"forum/internal/repo"
)

const moderationNoteMaxLen = 2000

type ModerationService struct {
	users      repo.UserRepo
	posts      repo.PostRepo
	comments   repo.CommentRepo
	categories repo.CategoryRepo
	moderation repo.ModerationRepo
	clock      clock.Clock
	center     *CenterService
}

func NewModerationService(users repo.UserRepo, posts repo.PostRepo, comments repo.CommentRepo, categories repo.CategoryRepo, moderation repo.ModerationRepo, clock clock.Clock, deps ...any) *ModerationService {
	service := &ModerationService{
		users:      users,
		posts:      posts,
		comments:   comments,
		categories: categories,
		moderation: moderation,
		clock:      clock,
	}
	for _, dependency := range deps {
		if center, ok := dependency.(*CenterService); ok && center != nil {
			service.center = center
		}
	}
	return service
}

func (s *ModerationService) BootstrapOwner(ctx context.Context, email, username, password string) (*domain.User, error) {
	email = strings.TrimSpace(email)
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if email == "" || username == "" || password == "" || !isValidEmail(email) {
		return nil, ErrInvalidInput
	}
	if exists, err := s.moderation.HasAdminOrOwner(ctx); err != nil {
		return nil, err
	} else if exists {
		return nil, ErrBootstrapUnavailable
	}
	if _, err := s.users.GetByEmailCI(ctx, email); err == nil {
		return nil, ErrConflict
	} else if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return nil, err
	}
	if _, err := s.users.GetByUsername(ctx, username); err == nil {
		return nil, ErrConflict
	} else if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return nil, err
	}

	hash, err := bcryptHasher{}.Hash(password)
	if err != nil {
		return nil, err
	}
	owner := &domain.User{
		Email:     email,
		Username:  username,
		PassHash:  hash,
		Role:      domain.RoleOwner,
		CreatedAt: s.clock.Now(),
	}
	id, err := s.moderation.CreateBootstrapOwner(ctx, owner)
	if err != nil {
		return nil, err
	}
	owner.ID = id
	owner.Badges = domain.StaffBadgesForRole(owner.Role)
	return owner, nil
}

func (s *ModerationService) RequestRole(ctx context.Context, actorID int64, requestedRole domain.UserRole, note string) (*domain.ModerationRoleRequest, error) {
	actor, err := s.loadUser(ctx, actorID)
	if err != nil {
		return nil, err
	}
	switch {
	case actor.Role == domain.RoleUser && requestedRole == domain.RoleModerator:
	case actor.Role == domain.RoleModerator && requestedRole == domain.RoleAdmin:
	default:
		return nil, ErrInvalidRoleTransition
	}

	request, err := s.moderation.CreateRoleRequest(ctx, repo.RoleRequestCreateInput{
		Requester:     *actor,
		RequestedRole: requestedRole,
		Note:          strings.TrimSpace(note),
		CreatedAt:     s.clock.Now(),
	})
	if err != nil {
		return nil, err
	}
	s.notifyRoleRequest(ctx, *actor, *request)
	return request, nil
}

func (s *ModerationService) ListRoleRequests(ctx context.Context, actorID int64, requestedRole domain.UserRole, status domain.RoleRequestStatus) ([]domain.ModerationRoleRequest, error) {
	actor, err := s.loadUser(ctx, actorID)
	if err != nil {
		return nil, err
	}
	if actor.Role != domain.RoleAdmin && actor.Role != domain.RoleOwner {
		return nil, ErrForbidden
	}
	return s.moderation.ListRoleRequests(ctx, repo.RoleRequestFilter{
		ViewerRole:    actor.Role,
		Status:        status,
		RequestedRole: requestedRole,
	})
}

func (s *ModerationService) ReviewRoleRequest(ctx context.Context, actorID, requestID int64, approve bool, note string) (*domain.ModerationRoleRequest, error) {
	actor, err := s.loadUser(ctx, actorID)
	if err != nil {
		return nil, err
	}
	request, err := s.moderation.GetRoleRequestByID(ctx, requestID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if request.Status != domain.RoleRequestStatusPending {
		return nil, ErrConflict
	}
	if request.RequestedRole == domain.RoleAdmin {
		if actor.Role != domain.RoleOwner {
			return nil, ErrForbidden
		}
	} else if actor.Role != domain.RoleAdmin && actor.Role != domain.RoleOwner {
		return nil, ErrForbidden
	}

	status := domain.RoleRequestStatusRejected
	if approve {
		status = domain.RoleRequestStatusApproved
	}
	updated, err := s.moderation.ReviewRoleRequest(ctx, repo.RoleRequestReviewInput{
		RequestID:  requestID,
		Actor:      *actor,
		Status:     status,
		Note:       strings.TrimSpace(note),
		ReviewedAt: s.clock.Now(),
	})
	if err != nil {
		return nil, err
	}
	s.notifyRoleRequestReviewed(ctx, *actor, *updated)
	return updated, nil
}

func (s *ModerationService) ChangeUserRole(ctx context.Context, actorID, targetUserID int64, nextRole domain.UserRole, note string) (*domain.User, error) {
	actor, err := s.loadUser(ctx, actorID)
	if err != nil {
		return nil, err
	}
	target, err := s.loadUser(ctx, targetUserID)
	if err != nil {
		return nil, err
	}
	if target.Role == domain.RoleOwner {
		return nil, ErrForbidden
	}
	if !isAllowedDirectRoleChange(actor.Role, target.Role, nextRole) {
		return nil, ErrInvalidRoleTransition
	}
	updated, err := s.moderation.ChangeUserRole(ctx, repo.RoleChangeInput{
		TargetUserID: targetUserID,
		Actor:        *actor,
		NewRole:      nextRole,
		Note:         strings.TrimSpace(note),
		ChangedAt:    s.clock.Now(),
	})
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	s.notifyRoleChanged(ctx, *actor, *updated)
	return updated, nil
}

func (s *ModerationService) ListUnderReviewPosts(ctx context.Context, actorID int64) ([]domain.Post, error) {
	actor, err := s.loadUser(ctx, actorID)
	if err != nil {
		return nil, err
	}
	if !actor.Role.IsStaff() {
		return nil, ErrForbidden
	}
	return s.posts.ListUnderReview(ctx)
}

func (s *ModerationService) ApprovePost(ctx context.Context, actorID, postID int64, categoryIDs []int64, note string) (*domain.Post, error) {
	actor, err := s.loadUser(ctx, actorID)
	if err != nil {
		return nil, err
	}
	if !actor.Role.IsStaff() {
		return nil, ErrForbidden
	}
	post, err := s.posts.GetByID(ctx, postID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if post.DeletedAt != nil {
		return nil, ErrConflict
	}
	updateCategories := (actor.Role == domain.RoleAdmin || actor.Role == domain.RoleOwner) && len(categoryIDs) > 0
	if actor.Role == domain.RoleModerator {
		updateCategories = false
		categoryIDs = nil
	}
	approved, err := s.moderation.ApprovePost(ctx, repo.PostApprovalInput{
		PostID:           postID,
		Actor:            *actor,
		ApprovedAt:       s.clock.Now(),
		CategoryIDs:      categoryIDs,
		UpdateCategories: updateCategories,
		Note:             strings.TrimSpace(note),
	})
	if err != nil {
		return nil, err
	}
	s.notifyPostApproved(ctx, *actor, *approved)
	return approved, nil
}

func (s *ModerationService) UpdatePostCategories(ctx context.Context, actorID, postID int64, categoryIDs []int64, note string) (*domain.Post, error) {
	actor, err := s.loadUser(ctx, actorID)
	if err != nil {
		return nil, err
	}
	if actor.Role != domain.RoleAdmin && actor.Role != domain.RoleOwner {
		return nil, ErrForbidden
	}
	post, err := s.posts.GetByID(ctx, postID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if post.DeletedAt != nil {
		return nil, ErrConflict
	}
	if len(categoryIDs) == 0 {
		return nil, ErrInvalidInput
	}
	return s.moderation.UpdatePostCategories(ctx, repo.PostCategoryUpdateInput{
		PostID:      postID,
		Actor:       *actor,
		CategoryIDs: categoryIDs,
		UpdatedAt:   s.clock.Now(),
		Note:        strings.TrimSpace(note),
	})
}

func (s *ModerationService) SoftDeleteContent(ctx context.Context, actorID int64, targetType string, targetID int64, reason domain.ModerationReason, note string) error {
	actor, err := s.loadUser(ctx, actorID)
	if err != nil {
		return err
	}
	if !actor.Role.IsStaff() {
		return ErrForbidden
	}
	note, err = normalizeModerationNote(note, true)
	if err != nil {
		return err
	}
	if !isValidModerationReason(reason) {
		return ErrInvalidInput
	}
	switch targetType {
	case domain.ModerationTargetPost:
		post, err := s.posts.GetByID(ctx, targetID)
		if err != nil {
			if errors.Is(err, repo.ErrNotFound) {
				return ErrNotFound
			}
			return err
		}
		if post.DeleteProtected {
			return ErrProtectedContent
		}
		if post.DeletedAt != nil {
			return ErrConflict
		}
	case domain.ModerationTargetComment:
		comment, err := s.comments.GetByID(ctx, targetID)
		if err != nil {
			if errors.Is(err, repo.ErrNotFound) {
				return ErrNotFound
			}
			return err
		}
		if comment.DeletedAt != nil {
			return ErrConflict
		}
	default:
		return ErrInvalidInput
	}
	if err := s.moderation.SoftDeleteContent(ctx, repo.ContentModerationInput{
		TargetType: targetType,
		TargetID:   targetID,
		Actor:      *actor,
		Reason:     reason,
		Note:       note,
		ActedAt:    s.clock.Now(),
	}); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	s.notifyContentDeleted(ctx, targetType, targetID, *actor, note)
	return nil
}

func (s *ModerationService) RestoreContent(ctx context.Context, actorID int64, targetType string, targetID int64, note string) error {
	actor, err := s.loadUser(ctx, actorID)
	if err != nil {
		return err
	}
	if !actor.Role.IsStaff() {
		return ErrForbidden
	}
	note, err = normalizeModerationNote(note, false)
	if err != nil {
		return err
	}

	switch targetType {
	case domain.ModerationTargetPost:
		post, err := s.posts.GetByID(ctx, targetID)
		if err != nil {
			if errors.Is(err, repo.ErrNotFound) {
				return ErrNotFound
			}
			return err
		}
		if post.DeletedAt == nil {
			return ErrConflict
		}
		if actor.Role == domain.RoleModerator && (post.DeletedBy == nil || post.DeletedBy.ID != actor.ID) {
			return ErrForbidden
		}
	case domain.ModerationTargetComment:
		comment, err := s.comments.GetByID(ctx, targetID)
		if err != nil {
			if errors.Is(err, repo.ErrNotFound) {
				return ErrNotFound
			}
			return err
		}
		if comment.DeletedAt == nil {
			return ErrConflict
		}
		if actor.Role == domain.RoleModerator && (comment.DeletedByUserID == nil || *comment.DeletedByUserID != actor.ID) {
			return ErrForbidden
		}
	default:
		return ErrInvalidInput
	}
	if err := s.moderation.RestoreContent(ctx, repo.ContentModerationInput{
		TargetType: targetType,
		TargetID:   targetID,
		Actor:      *actor,
		Reason:     domain.ModerationReasonOther,
		Note:       note,
		ActedAt:    s.clock.Now(),
	}); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	s.notifyContentRestored(ctx, targetType, targetID, *actor, note)
	return nil
}

func (s *ModerationService) HardDeleteContent(ctx context.Context, actorID int64, targetType string, targetID int64, reason domain.ModerationReason, note string) error {
	actor, err := s.loadUser(ctx, actorID)
	if err != nil {
		return err
	}
	if actor.Role != domain.RoleAdmin && actor.Role != domain.RoleOwner {
		return ErrForbidden
	}
	note, err = normalizeModerationNote(note, true)
	if err != nil {
		return err
	}
	if !isValidModerationReason(reason) {
		return ErrInvalidInput
	}
	switch targetType {
	case domain.ModerationTargetPost:
		post, err := s.posts.GetByID(ctx, targetID)
		if err != nil {
			if errors.Is(err, repo.ErrNotFound) {
				return ErrNotFound
			}
			return err
		}
		if post.DeleteProtected {
			return ErrProtectedContent
		}
	case domain.ModerationTargetComment:
		comment, err := s.comments.GetByID(ctx, targetID)
		if err != nil {
			if errors.Is(err, repo.ErrNotFound) {
				return ErrNotFound
			}
			return err
		}
		if comment.DeletedAt == nil {
			hasDescendants, err := s.comments.HasDescendants(ctx, targetID)
			if err != nil {
				return err
			}
			if hasDescendants {
				return ErrForbidden
			}
		}
	default:
		return ErrInvalidInput
	}
	return s.moderation.HardDeleteContent(ctx, repo.ContentModerationInput{
		TargetType: targetType,
		TargetID:   targetID,
		Actor:      *actor,
		Reason:     reason,
		Note:       note,
		ActedAt:    s.clock.Now(),
	})
}

func (s *ModerationService) SetPostDeleteProtection(ctx context.Context, actorID, postID int64, protected bool, note string) (*domain.Post, error) {
	actor, err := s.loadUser(ctx, actorID)
	if err != nil {
		return nil, err
	}
	if actor.Role != domain.RoleOwner {
		return nil, ErrForbidden
	}
	post, err := s.posts.GetByID(ctx, postID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if post.DeletedAt != nil {
		return nil, ErrConflict
	}
	return s.moderation.SetPostDeleteProtection(ctx, postID, *actor, protected, s.clock.Now(), strings.TrimSpace(note))
}

func (s *ModerationService) CreateReport(ctx context.Context, actorID int64, targetType string, targetID int64, reason domain.ModerationReason, note string) (*domain.ModerationReport, error) {
	actor, err := s.loadUser(ctx, actorID)
	if err != nil {
		return nil, err
	}
	if !isValidModerationReason(reason) {
		return nil, ErrInvalidInput
	}
	note, err = normalizeModerationNote(note, true)
	if err != nil {
		return nil, err
	}
	switch targetType {
	case domain.ModerationTargetPost:
		post, err := s.posts.GetByID(ctx, targetID)
		if err != nil {
			if errors.Is(err, repo.ErrNotFound) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		if post.DeletedAt != nil {
			return nil, ErrConflict
		}
	case domain.ModerationTargetComment:
		comment, err := s.comments.GetByID(ctx, targetID)
		if err != nil {
			if errors.Is(err, repo.ErrNotFound) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		if comment.DeletedAt != nil {
			return nil, ErrConflict
		}
	default:
		return nil, ErrInvalidInput
	}
	reportItem, err := s.moderation.CreateReport(ctx, repo.ReportCreateInput{
		TargetType: targetType,
		TargetID:   targetID,
		Reporter:   *actor,
		Reason:     reason,
		Note:       note,
		CreatedAt:  s.clock.Now(),
	})
	if err != nil {
		return nil, err
	}
	s.notifyReportCreated(ctx, *actor, *reportItem)
	return reportItem, nil
}

func (s *ModerationService) ListReports(ctx context.Context, actorID int64, mine bool, status domain.ModerationStatus) ([]domain.ModerationReport, error) {
	actor, err := s.loadUser(ctx, actorID)
	if err != nil {
		return nil, err
	}
	filter := repo.ReportFilter{ViewerRole: actor.Role, Status: status}
	if mine {
		filter.ReporterUserID = actor.ID
	} else if !actor.Role.IsStaff() {
		return nil, ErrForbidden
	}
	return s.moderation.ListReports(ctx, filter)
}

func (s *ModerationService) CloseReport(ctx context.Context, actorID, reportID int64, takeAction bool, reason domain.ModerationReason, note string) (*domain.ModerationReport, error) {
	actor, err := s.loadUser(ctx, actorID)
	if err != nil {
		return nil, err
	}
	reportItem, err := s.moderation.GetReportByID(ctx, reportID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if !canActorSeeReport(actor.Role, reportItem.Reporter.Role) {
		return nil, ErrForbidden
	}
	if reportItem.Status != domain.ModerationStatusPending {
		return nil, ErrConflict
	}
	if !isValidModerationReason(reason) {
		return nil, ErrInvalidInput
	}
	note, err = normalizeModerationNote(note, true)
	if err != nil {
		return nil, err
	}
	status := domain.ModerationStatusDismissed
	if takeAction {
		status = domain.ModerationStatusActionTaken
	}
	updated, err := s.moderation.CloseReport(ctx, repo.ReportCloseInput{
		ReportID:       reportID,
		Actor:          *actor,
		Status:         status,
		DecisionReason: reason,
		DecisionNote:   note,
		ClosedAt:       s.clock.Now(),
	})
	if err != nil {
		return nil, err
	}
	s.notifyReportClosed(ctx, *actor, *updated)
	return updated, nil
}

func (s *ModerationService) CreateAppeal(ctx context.Context, actorID int64, targetType string, targetID int64, note string) (*domain.ModerationAppeal, error) {
	actor, err := s.loadUser(ctx, actorID)
	if err != nil {
		return nil, err
	}
	note, err = normalizeModerationNote(note, true)
	if err != nil {
		return nil, err
	}
	ownerID, err := s.appealTargetOwnerID(ctx, targetType, targetID)
	if err != nil {
		return nil, err
	}
	if ownerID != actor.ID {
		return nil, ErrForbidden
	}
	pendingAppeals, err := s.moderation.ListAppeals(ctx, repo.AppealFilter{
		RequesterUserID: actor.ID,
		Status:          domain.AppealStatusPending,
	})
	if err != nil {
		return nil, err
	}
	for _, appeal := range pendingAppeals {
		if appeal.Target.TargetType == targetType && appeal.Target.TargetID == targetID {
			return nil, ErrAlreadyPending
		}
	}
	targetRole, sourceHistoryID, err := s.nextAppealStage(ctx, actor.ID, targetType, targetID)
	if err != nil {
		return nil, err
	}
	appeal, err := s.moderation.CreateAppeal(ctx, repo.AppealCreateInput{
		TargetType:      targetType,
		TargetID:        targetID,
		SourceHistoryID: sourceHistoryID,
		Requester:       *actor,
		TargetRole:      targetRole,
		Note:            note,
		CreatedAt:       s.clock.Now(),
	})
	if err != nil {
		return nil, err
	}
	s.notifyAppealCreated(ctx, *actor, *appeal)
	return appeal, nil
}

func (s *ModerationService) CanCreateAppeal(ctx context.Context, actorID int64, targetType string, targetID int64) (bool, error) {
	actor, err := s.loadUser(ctx, actorID)
	if err != nil {
		return false, err
	}
	ownerID, err := s.appealTargetOwnerID(ctx, targetType, targetID)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound), errors.Is(err, ErrInvalidInput):
			return false, nil
		default:
			return false, err
		}
	}
	if ownerID != actor.ID {
		return false, nil
	}
	pendingAppeals, err := s.moderation.ListAppeals(ctx, repo.AppealFilter{
		RequesterUserID: actor.ID,
		Status:          domain.AppealStatusPending,
	})
	if err != nil {
		return false, err
	}
	for _, appeal := range pendingAppeals {
		if appeal.Target.TargetType == targetType && appeal.Target.TargetID == targetID {
			return false, nil
		}
	}
	if _, _, err := s.nextAppealStage(ctx, actor.ID, targetType, targetID); err != nil {
		switch {
		case errors.Is(err, ErrNoFurtherAppeal), errors.Is(err, ErrNotFound), errors.Is(err, ErrInvalidInput):
			return false, nil
		default:
			return false, err
		}
	}
	return true, nil
}

func (s *ModerationService) ListAppeals(ctx context.Context, actorID int64, mine bool, status domain.AppealStatus) ([]domain.ModerationAppeal, error) {
	actor, err := s.loadUser(ctx, actorID)
	if err != nil {
		return nil, err
	}
	filter := repo.AppealFilter{ViewerRole: actor.Role, Status: status}
	if mine {
		filter.RequesterUserID = actor.ID
	} else if actor.Role != domain.RoleAdmin && actor.Role != domain.RoleOwner {
		return nil, ErrForbidden
	}
	return s.moderation.ListAppeals(ctx, filter)
}

func (s *ModerationService) CloseAppeal(ctx context.Context, actorID, appealID int64, reverse bool, note string) (*domain.ModerationAppeal, error) {
	actor, err := s.loadUser(ctx, actorID)
	if err != nil {
		return nil, err
	}
	appeal, err := s.moderation.GetAppealByID(ctx, appealID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if appeal.TargetRole != actor.Role {
		return nil, ErrForbidden
	}
	if appeal.Status != domain.AppealStatusPending {
		return nil, ErrConflict
	}
	note, err = normalizeModerationNote(note, true)
	if err != nil {
		return nil, err
	}
	if reverse {
		if err := s.RestoreContent(ctx, actorID, appeal.Target.TargetType, appeal.Target.TargetID, note); err != nil {
			return nil, err
		}
	}
	status := domain.AppealStatusDismissed
	if reverse {
		status = domain.AppealStatusReversed
	}
	updated, err := s.moderation.CloseAppeal(ctx, repo.AppealCloseInput{
		AppealID:     appealID,
		Actor:        *actor,
		Status:       status,
		DecisionNote: note,
		ClosedAt:     s.clock.Now(),
	})
	if err != nil {
		return nil, err
	}
	s.notifyAppealClosed(ctx, *actor, *updated)
	return updated, nil
}

func (s *ModerationService) CreateCategory(ctx context.Context, actorID int64, name string) (*domain.Category, error) {
	actor, err := s.loadUser(ctx, actorID)
	if err != nil {
		return nil, err
	}
	if actor.Role != domain.RoleAdmin && actor.Role != domain.RoleOwner {
		return nil, ErrForbidden
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrInvalidInput
	}
	return s.moderation.CreateCategory(ctx, repo.CategoryCreateInput{
		Category: domain.Category{
			Code: categoryCodeFromName(name),
			Name: name,
		},
		Actor:     *actor,
		CreatedAt: s.clock.Now(),
	})
}

func (s *ModerationService) DeleteCategory(ctx context.Context, actorID, categoryID int64, note string) (int64, error) {
	actor, err := s.loadUser(ctx, actorID)
	if err != nil {
		return 0, err
	}
	if actor.Role != domain.RoleAdmin && actor.Role != domain.RoleOwner {
		return 0, ErrForbidden
	}
	category, err := s.categories.GetByID(ctx, categoryID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	if category.Code == "other" || category.IsSystem {
		return 0, ErrForbidden
	}
	note, err = normalizeModerationNote(note, true)
	if err != nil {
		return 0, err
	}
	return s.moderation.DeleteCategory(ctx, repo.CategoryDeleteInput{
		CategoryID: categoryID,
		Actor:      *actor,
		DeletedAt:  s.clock.Now(),
		Note:       note,
	})
}

func (s *ModerationService) ListHistory(ctx context.Context, actorID int64, filter domain.ModerationHistoryFilter) ([]domain.ModerationHistoryRecord, error) {
	actor, err := s.loadUser(ctx, actorID)
	if err != nil {
		return nil, err
	}
	if !actor.Role.IsStaff() {
		return nil, ErrForbidden
	}
	return s.moderation.ListHistory(ctx, filter)
}

func (s *ModerationService) PurgeHistory(ctx context.Context, actorID int64, filter domain.ModerationHistoryFilter, note string) (int64, error) {
	actor, err := s.loadUser(ctx, actorID)
	if err != nil {
		return 0, err
	}
	if actor.Role != domain.RoleOwner {
		return 0, ErrForbidden
	}
	note, err = normalizeModerationNote(note, false)
	if err != nil {
		return 0, err
	}
	return s.moderation.PurgeHistory(ctx, repo.HistoryPurgeInput{
		Actor:    *actor,
		Filter:   filter,
		PurgedAt: s.clock.Now(),
		Note:     note,
	})
}

func (s *ModerationService) loadUser(ctx context.Context, userID int64) (*domain.User, error) {
	if userID <= 0 {
		return nil, ErrUnauthorized
	}
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, ErrUnauthorized
		}
		return nil, err
	}
	return user, nil
}

func normalizeModerationNote(note string, required bool) (string, error) {
	note = strings.TrimSpace(note)
	if required && note == "" {
		return "", ErrInvalidInput
	}
	if utf8.RuneCountInString(note) > moderationNoteMaxLen {
		return "", ErrInvalidInput
	}
	return note, nil
}

func (s *ModerationService) appealTargetOwnerID(ctx context.Context, targetType string, targetID int64) (int64, error) {
	switch targetType {
	case domain.ModerationTargetPost:
		post, err := s.posts.GetByID(ctx, targetID)
		if err != nil {
			if errors.Is(err, repo.ErrNotFound) {
				return 0, ErrNotFound
			}
			return 0, err
		}
		return post.UserID, nil
	case domain.ModerationTargetComment:
		comment, err := s.comments.GetByID(ctx, targetID)
		if err != nil {
			if errors.Is(err, repo.ErrNotFound) {
				return 0, ErrNotFound
			}
			return 0, err
		}
		return comment.UserID, nil
	default:
		return 0, ErrInvalidInput
	}
}

func isValidModerationReason(reason domain.ModerationReason) bool {
	switch reason {
	case domain.ModerationReasonIrrelevant, domain.ModerationReasonObscene, domain.ModerationReasonIllegal, domain.ModerationReasonInsulting, domain.ModerationReasonOther:
		return true
	default:
		return false
	}
}

func isAllowedDirectRoleChange(actorRole, currentRole, nextRole domain.UserRole) bool {
	switch actorRole {
	case domain.RoleOwner:
		return (currentRole == domain.RoleUser && (nextRole == domain.RoleModerator || nextRole == domain.RoleAdmin)) ||
			(currentRole == domain.RoleModerator && (nextRole == domain.RoleUser || nextRole == domain.RoleAdmin)) ||
			(currentRole == domain.RoleAdmin && nextRole == domain.RoleModerator)
	case domain.RoleAdmin:
		return (currentRole == domain.RoleUser && nextRole == domain.RoleModerator) ||
			(currentRole == domain.RoleModerator && nextRole == domain.RoleUser)
	default:
		return false
	}
}

func canActorSeeReport(actorRole, reporterRole domain.UserRole) bool {
	switch actorRole {
	case domain.RoleModerator:
		return reporterRole == domain.RoleUser
	case domain.RoleAdmin:
		return reporterRole == domain.RoleUser || reporterRole == domain.RoleModerator
	case domain.RoleOwner:
		return true
	default:
		return false
	}
}

func categoryCodeFromName(name string) string {
	code := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(name)), "-"))
	code = strings.Trim(code, "-")
	if code == "" {
		return fmt.Sprintf("category-%d", time.Now().UnixNano())
	}
	return code
}

func (s *ModerationService) nextAppealStage(ctx context.Context, requesterID int64, targetType string, targetID int64) (domain.UserRole, int64, error) {
	appeals, err := s.moderation.ListAppeals(ctx, repo.AppealFilter{RequesterUserID: requesterID})
	if err != nil {
		return "", 0, err
	}
	for _, appeal := range appeals {
		if appeal.Target.TargetType == targetType && appeal.Target.TargetID == targetID && appeal.Status != domain.AppealStatusPending {
			if appeal.TargetRole == domain.RoleAdmin {
				return domain.RoleOwner, 0, nil
			}
		}
	}
	history, err := s.moderation.ListHistory(ctx, domain.ModerationHistoryFilter{TargetType: targetType, Limit: 50})
	if err != nil {
		return "", 0, err
	}
	for _, record := range history {
		if record.TargetID != targetID {
			continue
		}
		switch record.ActionType {
		case domain.ActionPostSoftDeleted, domain.ActionPostHardDeleted, domain.ActionCommentSoftDeleted, domain.ActionCommentHardDeleted:
			switch record.ActorRole {
			case domain.RoleModerator:
				return domain.RoleAdmin, record.ID, nil
			case domain.RoleAdmin:
				return domain.RoleOwner, record.ID, nil
			default:
				return "", 0, ErrNoFurtherAppeal
			}
		}
	}
	return "", 0, ErrNoFurtherAppeal
}

func (s *ModerationService) notifyRoleRequest(ctx context.Context, actor domain.User, request domain.ModerationRoleRequest) {
	if s.center == nil {
		return
	}
	for _, reviewer := range s.usersByRoles(ctx, roleRequestRecipients(request.RequestedRole)...) {
		_ = s.center.createAndPublishNotification(ctx, domain.Notification{
			UserID:      reviewer.ID,
			ActorUserID: int64Ptr(actor.ID),
			Bucket:      "management",
			Type:        "role_request_created",
			EntityType:  domain.NotificationEntityTypeUser,
			EntityID:    request.Applicant.ID,
			Payload: domain.NotificationPayload{
				ActorName:     displayNameOrUsername(&actor),
				ActorUsername: actor.Username,
				Reason:        string(request.RequestedRole),
			},
			CreatedAt: request.CreatedAt,
		})
	}
}

func (s *ModerationService) notifyRoleRequestReviewed(ctx context.Context, actor domain.User, request domain.ModerationRoleRequest) {
	if s.center == nil {
		return
	}
	_ = s.center.createAndPublishNotification(ctx, domain.Notification{
		UserID:      request.Applicant.ID,
		ActorUserID: int64Ptr(actor.ID),
		Bucket:      "management",
		Type:        "role_request_reviewed",
		EntityType:  domain.NotificationEntityTypeUser,
		EntityID:    request.Applicant.ID,
		Payload: domain.NotificationPayload{
			ActorName:     displayNameOrUsername(&actor),
			ActorUsername: actor.Username,
			Reason:        string(request.Status),
		},
		CreatedAt: s.clock.Now(),
	})
}

func (s *ModerationService) notifyRoleChanged(ctx context.Context, actor domain.User, target domain.User) {
	if s.center == nil {
		return
	}
	_ = s.center.createAndPublishNotification(ctx, domain.Notification{
		UserID:      target.ID,
		ActorUserID: int64Ptr(actor.ID),
		Bucket:      "management",
		Type:        "role_changed",
		EntityType:  domain.NotificationEntityTypeUser,
		EntityID:    target.ID,
		Payload: domain.NotificationPayload{
			ActorName:     displayNameOrUsername(&actor),
			ActorUsername: actor.Username,
			Reason:        string(target.Role),
		},
		CreatedAt: s.clock.Now(),
	})
}

func (s *ModerationService) notifyPostApproved(ctx context.Context, actor domain.User, post domain.Post) {
	if s.center == nil || post.UserID == actor.ID {
		return
	}
	_ = s.center.createAndPublishNotification(ctx, domain.Notification{
		UserID:      post.UserID,
		ActorUserID: int64Ptr(actor.ID),
		Bucket:      "my_content",
		Type:        "post_approved",
		EntityType:  domain.NotificationEntityTypePost,
		EntityID:    post.ID,
		Payload: domain.NotificationPayload{
			ActorName:     displayNameOrUsername(&actor),
			ActorUsername: actor.Username,
			PostID:        post.ID,
			PostTitle:     post.Title,
			PostPreview:   previewText(post.Body, 140),
		},
		CreatedAt: s.clock.Now(),
	})
}

func (s *ModerationService) notifyContentDeleted(ctx context.Context, targetType string, targetID int64, actor domain.User, note string) {
	if s.center == nil {
		return
	}
	if userID := s.contentOwnerID(ctx, targetType, targetID); userID > 0 && userID != actor.ID {
		payload := s.deletedContentNotificationPayload(ctx, targetType, targetID)
		payload.ActorName = displayNameOrUsername(&actor)
		payload.ActorUsername = actor.Username
		payload.Reason = note
		_ = s.center.createAndPublishNotification(ctx, domain.Notification{
			UserID:      userID,
			ActorUserID: int64Ptr(actor.ID),
			Bucket:      "deleted",
			Type:        "content_deleted",
			EntityType:  targetType,
			EntityID:    targetID,
			Payload:     payload,
			CreatedAt:   s.clock.Now(),
		})
	}
}

func (s *ModerationService) notifyContentRestored(ctx context.Context, targetType string, targetID int64, actor domain.User, note string) {
	if s.center == nil {
		return
	}
	if userID := s.contentOwnerID(ctx, targetType, targetID); userID > 0 && userID != actor.ID {
		_ = s.center.createAndPublishNotification(ctx, domain.Notification{
			UserID:      userID,
			ActorUserID: int64Ptr(actor.ID),
			Bucket:      "deleted",
			Type:        "content_restored",
			EntityType:  targetType,
			EntityID:    targetID,
			Payload: domain.NotificationPayload{
				ActorName:     displayNameOrUsername(&actor),
				ActorUsername: actor.Username,
				Reason:        note,
			},
			CreatedAt: s.clock.Now(),
		})
	}
}

func (s *ModerationService) notifyReportCreated(ctx context.Context, actor domain.User, reportItem domain.ModerationReport) {
	if s.center == nil {
		return
	}
	for _, reviewer := range s.usersByRoles(ctx, rolesFromStrings(reportItem.RouteToRoles)...) {
		if reviewer.ID == actor.ID {
			continue
		}
		_ = s.center.createAndPublishNotification(ctx, domain.Notification{
			UserID:      reviewer.ID,
			ActorUserID: int64Ptr(actor.ID),
			Bucket:      "reports",
			Type:        "report_created",
			EntityType:  reportItem.Target.TargetType,
			EntityID:    reportItem.Target.TargetID,
			Payload: domain.NotificationPayload{
				ActorName:     displayNameOrUsername(&actor),
				ActorUsername: actor.Username,
				Reason:        string(reportItem.Reason),
			},
			CreatedAt: reportItem.CreatedAt,
		})
	}
}

func (s *ModerationService) notifyReportClosed(ctx context.Context, actor domain.User, reportItem domain.ModerationReport) {
	if s.center == nil || reportItem.Reporter.ID == actor.ID {
		return
	}
	_ = s.center.createAndPublishNotification(ctx, domain.Notification{
		UserID:      reportItem.Reporter.ID,
		ActorUserID: int64Ptr(actor.ID),
		Bucket:      "reports",
		Type:        "report_closed",
		EntityType:  reportItem.Target.TargetType,
		EntityID:    reportItem.Target.TargetID,
		Payload: domain.NotificationPayload{
			ActorName:     displayNameOrUsername(&actor),
			ActorUsername: actor.Username,
			Reason:        string(reportItem.Status),
		},
		CreatedAt: s.clock.Now(),
	})
}

func (s *ModerationService) notifyAppealCreated(ctx context.Context, actor domain.User, appeal domain.ModerationAppeal) {
	if s.center == nil {
		return
	}
	for _, reviewer := range s.usersByRoles(ctx, appeal.TargetRole) {
		_ = s.center.createAndPublishNotification(ctx, domain.Notification{
			UserID:      reviewer.ID,
			ActorUserID: int64Ptr(actor.ID),
			Bucket:      "appeals",
			Type:        "appeal_created",
			EntityType:  appeal.Target.TargetType,
			EntityID:    appeal.Target.TargetID,
			Payload: domain.NotificationPayload{
				ActorName:     displayNameOrUsername(&actor),
				ActorUsername: actor.Username,
				Reason:        string(appeal.TargetRole),
			},
			CreatedAt: appeal.CreatedAt,
		})
	}
}

func (s *ModerationService) notifyAppealClosed(ctx context.Context, actor domain.User, appeal domain.ModerationAppeal) {
	if s.center == nil || appeal.Requester.ID == actor.ID {
		return
	}
	_ = s.center.createAndPublishNotification(ctx, domain.Notification{
		UserID:      appeal.Requester.ID,
		ActorUserID: int64Ptr(actor.ID),
		Bucket:      "appeals",
		Type:        "appeal_closed",
		EntityType:  appeal.Target.TargetType,
		EntityID:    appeal.Target.TargetID,
		Payload: domain.NotificationPayload{
			ActorName:     displayNameOrUsername(&actor),
			ActorUsername: actor.Username,
			Reason:        string(appeal.Status),
		},
		CreatedAt: s.clock.Now(),
	})
}

func (s *ModerationService) usersByRoles(ctx context.Context, roles ...domain.UserRole) []domain.User {
	users, err := s.users.List(ctx)
	if err != nil {
		return nil
	}
	allowed := make(map[domain.UserRole]struct{}, len(roles))
	for _, role := range roles {
		allowed[role] = struct{}{}
	}
	var filtered []domain.User
	for _, user := range users {
		if _, ok := allowed[user.Role]; ok {
			filtered = append(filtered, user)
		}
	}
	return filtered
}

func roleRequestRecipients(requestedRole domain.UserRole) []domain.UserRole {
	if requestedRole == domain.RoleAdmin {
		return []domain.UserRole{domain.RoleOwner}
	}
	return []domain.UserRole{domain.RoleAdmin, domain.RoleOwner}
}

func rolesFromStrings(values []string) []domain.UserRole {
	out := make([]domain.UserRole, 0, len(values))
	for _, value := range values {
		out = append(out, domain.NormalizeUserRole(value))
	}
	return out
}

func (s *ModerationService) contentOwnerID(ctx context.Context, targetType string, targetID int64) int64 {
	switch targetType {
	case domain.ModerationTargetPost:
		post, err := s.posts.GetByID(ctx, targetID)
		if err == nil {
			return post.UserID
		}
	case domain.ModerationTargetComment:
		comment, err := s.comments.GetByID(ctx, targetID)
		if err == nil {
			return comment.UserID
		}
	}
	return 0
}

func (s *ModerationService) deletedContentNotificationPayload(ctx context.Context, targetType string, targetID int64) domain.NotificationPayload {
	switch targetType {
	case domain.ModerationTargetPost:
		post, err := s.posts.GetByID(ctx, targetID)
		if err == nil {
			return domain.NotificationPayload{
				PostID:      post.ID,
				PostTitle:   strings.TrimSpace(post.Title),
				PostPreview: previewText(post.Body, 160),
			}
		}
	case domain.ModerationTargetComment:
		comment, err := s.comments.GetByID(ctx, targetID)
		if err == nil {
			body := strings.TrimSpace(comment.Body)
			if comment.DeletedAt != nil && strings.TrimSpace(comment.DeletedBody) != "" {
				body = strings.TrimSpace(comment.DeletedBody)
			}
			return domain.NotificationPayload{
				PostID:         comment.PostID,
				CommentID:      comment.ID,
				CommentPreview: previewText(body, 160),
			}
		}
	}
	return domain.NotificationPayload{}
}

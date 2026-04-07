package repo

import (
	"context"
	"time"

	"forum/internal/domain"
)

type UserRepo interface {
	Create(ctx context.Context, user *domain.User) (int64, error)
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
	GetByEmailCI(ctx context.Context, email string) (*domain.User, error)
	GetByUsername(ctx context.Context, username string) (*domain.User, error)
	GetByUsernameCI(ctx context.Context, username string) (*domain.User, error)
	GetByID(ctx context.Context, id int64) (*domain.User, error)
	GetByDisplayNameCI(ctx context.Context, displayName string) (*domain.User, error)
	GetPublicByUsername(ctx context.Context, username string) (*domain.User, error)
	UpdateProfile(ctx context.Context, userID int64, displayName *string, firstName, lastName string, age int, gender string, profileInitialized bool) error
	UpdateRole(ctx context.Context, userID int64, role domain.UserRole) error
	AnyByRoles(ctx context.Context, roles ...domain.UserRole) (bool, error)
	List(ctx context.Context) ([]domain.User, error)
	ListPublic(ctx context.Context) ([]domain.User, error)
}

type SessionRepo interface {
	Create(ctx context.Context, session *domain.Session) error
	GetByToken(ctx context.Context, token string) (*domain.Session, error)
	DeleteByToken(ctx context.Context, token string) error
	DeleteByUserID(ctx context.Context, userID int64) error
}

type AuthIdentityRepo interface {
	Create(ctx context.Context, identity *domain.AuthIdentity) (int64, error)
	GetByProviderUserID(ctx context.Context, provider, providerUserID string) (*domain.AuthIdentity, error)
	GetByUserProvider(ctx context.Context, userID int64, provider string) (*domain.AuthIdentity, error)
	ListByUserID(ctx context.Context, userID int64) ([]domain.AuthIdentity, error)
	Update(ctx context.Context, identity *domain.AuthIdentity) error
	DeleteByUserProvider(ctx context.Context, userID int64, provider string) error
	CountByUserID(ctx context.Context, userID int64) (int, error)
}

type AuthFlowRepo interface {
	Create(ctx context.Context, flow *domain.AuthFlow) error
	GetByToken(ctx context.Context, token string) (*domain.AuthFlow, error)
	TakeByToken(ctx context.Context, token string) (*domain.AuthFlow, error)
	DeleteByToken(ctx context.Context, token string) error
}

type AccountMergeInput struct {
	TargetUserID             int64
	SourceUserID             int64
	DisplayName              *string
	TargetEmail              string
	TargetUsername           string
	TargetPassHash           string
	TargetProfileInitialized bool
	Identity                 *domain.AuthIdentity
	Now                      time.Time
}

type AccountRepo interface {
	HasDirectMessagesBetweenUsers(ctx context.Context, userA, userB int64) (bool, error)
	CreateUserWithIdentity(ctx context.Context, user *domain.User, identity *domain.AuthIdentity) (int64, error)
	MergeUsers(ctx context.Context, input AccountMergeInput) error
}

type PostRepo interface {
	Create(ctx context.Context, post *domain.Post, categoryIDs []int64) (int64, error)
	List(ctx context.Context, filter domain.PostFilter) ([]domain.Post, error)
	ListUnderReview(ctx context.Context) ([]domain.Post, error)
	GetByID(ctx context.Context, id int64) (*domain.Post, error)
	Exists(ctx context.Context, id int64) (bool, error)
	Update(ctx context.Context, post *domain.Post, categoryIDs []int64) error
	UpdateCategories(ctx context.Context, postID int64, categoryIDs []int64) error
	Approve(ctx context.Context, postID, approvedByUserID int64, approvedAt time.Time, categoryIDs []int64, updateCategories bool) error
	SoftDelete(ctx context.Context, id, actorUserID int64, actorRole domain.UserRole, deletedAt time.Time) error
	Restore(ctx context.Context, id int64) error
	SetDeleteProtection(ctx context.Context, id int64, protected bool) error
	Delete(ctx context.Context, id int64) error
}

type CommentRepo interface {
	Create(ctx context.Context, comment *domain.Comment) (int64, error)
	ListByPost(ctx context.Context, postID int64, filter domain.CommentFilter) ([]domain.Comment, error)
	GetByID(ctx context.Context, id int64) (*domain.Comment, error)
	HasDescendants(ctx context.Context, id int64) (bool, error)
	HasActiveThreadComments(ctx context.Context, rootID int64) (bool, error)
	SoftDelete(ctx context.Context, id int64, deletedAt time.Time, actorUserID int64, actorRole domain.UserRole) error
	Restore(ctx context.Context, id int64) error
	Update(ctx context.Context, comment *domain.Comment) error
	Delete(ctx context.Context, id int64) error
}

type CategoryRepo interface {
	List(ctx context.Context) ([]domain.Category, error)
	GetByID(ctx context.Context, id int64) (*domain.Category, error)
	GetByCode(ctx context.Context, code string) (*domain.Category, error)
	Create(ctx context.Context, category *domain.Category) (int64, error)
	Delete(ctx context.Context, categoryID int64) error
	DeleteAndMovePostsToCategory(ctx context.Context, categoryID, fallbackCategoryID int64) (int64, error)
}

type ReactionRepo interface {
	ReactPost(ctx context.Context, postID, userID int64, value int, reactedAt time.Time) (domain.ReactionChange, error)
	ReactComment(ctx context.Context, commentID, userID int64, value int, reactedAt time.Time) (domain.ReactionChange, error)
}

type AttachmentRepo interface {
	Create(ctx context.Context, ownerUserID int64, mime string, size int64, storageKey, originalName string, createdAt time.Time) (int64, error)
	GetByID(ctx context.Context, id int64) (*domain.Attachment, error)
	GetUsage(ctx context.Context, id int64) (domain.AttachmentUsage, error)
}

type PrivateMessageRepo interface {
	SavePrivateMessage(ctx context.Context, fromID, toID int64, body string, attachment *domain.Attachment, createdAt time.Time) (*domain.PrivateMessage, error)
	ListConversationLast(ctx context.Context, userA, userB int64, limit int) ([]domain.PrivateMessage, error)
	ListConversationBefore(ctx context.Context, userA, userB, beforeTs, beforeID int64, limit int) ([]domain.PrivateMessage, error)
	ListPeers(ctx context.Context, userID int64) ([]domain.PrivateMessagePeer, error)
	MarkRead(ctx context.Context, userID, peerID, lastReadMessageID int64, updatedAt time.Time) error
	ConversationHasMessage(ctx context.Context, userA, userB, messageID int64) (bool, error)
}

type CenterRepo interface {
	CreateNotification(ctx context.Context, notification *domain.Notification) (int64, error)
	GetNotification(ctx context.Context, userID, notificationID int64) (*domain.Notification, error)
	ListNotifications(ctx context.Context, userID int64, filter domain.NotificationFilter) ([]domain.Notification, error)
	CountUnreadNotifications(ctx context.Context, userID int64) (domain.NotificationUnreadSummary, error)
	MarkNotificationRead(ctx context.Context, userID, notificationID int64, readAt time.Time) error
	DeleteNotification(ctx context.Context, userID, notificationID int64) error
	MarkAllNotificationsRead(ctx context.Context, userID int64, bucket string, readAt time.Time) error
	MarkDMNotificationsRead(ctx context.Context, userID, peerID, lastReadMessageID int64, readAt time.Time) error
	CreatePostSubscription(ctx context.Context, userID, postID int64, createdAt time.Time) error
	DeletePostSubscription(ctx context.Context, userID, postID int64) error
	IsPostSubscribed(ctx context.Context, userID, postID int64) (bool, error)
	ListPostSubscriberUserIDs(ctx context.Context, postID int64) ([]int64, error)
	CreateUserFollow(ctx context.Context, followerUserID, followedUserID int64, createdAt time.Time) error
	DeleteUserFollow(ctx context.Context, followerUserID, followedUserID int64) error
	IsFollowingUser(ctx context.Context, followerUserID, followedUserID int64) (bool, error)
	ListFollowerUserIDs(ctx context.Context, followedUserID int64) ([]int64, error)
	ListActivityPosts(ctx context.Context, userID int64, limit, offset int) ([]domain.Post, error)
	ListActivityComments(ctx context.Context, userID int64, limit, offset int) ([]domain.ActivityComment, error)
	ListActivityReactions(ctx context.Context, userID int64, limit, offset int) ([]domain.ActivityReaction, error)
}

type RoleRequestFilter struct {
	ViewerRole    domain.UserRole
	Status        domain.RoleRequestStatus
	RequestedRole domain.UserRole
}

type ReportFilter struct {
	ViewerUserID   int64
	ViewerRole     domain.UserRole
	Status         domain.ModerationStatus
	ReporterUserID int64
}

type AppealFilter struct {
	ViewerUserID    int64
	ViewerRole      domain.UserRole
	RequesterUserID int64
	Status          domain.AppealStatus
}

type RoleRequestCreateInput struct {
	Requester     domain.User
	RequestedRole domain.UserRole
	Note          string
	CreatedAt     time.Time
}

type RoleRequestReviewInput struct {
	RequestID  int64
	Actor      domain.User
	Status     domain.RoleRequestStatus
	Note       string
	ReviewedAt time.Time
}

type RoleChangeInput struct {
	TargetUserID int64
	Actor        domain.User
	NewRole      domain.UserRole
	Note         string
	ChangedAt    time.Time
}

type ReportCreateInput struct {
	TargetType               string
	TargetID                 int64
	Reporter                 domain.User
	Reason                   domain.ModerationReason
	Note                     string
	CreatedAt                time.Time
	LinkedPreviousDecisionID *int64
}

type ReportCloseInput struct {
	ReportID       int64
	Actor          domain.User
	Status         domain.ModerationStatus
	DecisionReason domain.ModerationReason
	DecisionNote   string
	ClosedAt       time.Time
}

type AppealCreateInput struct {
	TargetType               string
	TargetID                 int64
	SourceHistoryID          int64
	LinkedPreviousDecisionID *int64
	Requester                domain.User
	TargetRole               domain.UserRole
	Note                     string
	CreatedAt                time.Time
}

type AppealCloseInput struct {
	AppealID     int64
	Actor        domain.User
	Status       domain.AppealStatus
	DecisionNote string
	ClosedAt     time.Time
}

type PostApprovalInput struct {
	PostID           int64
	Actor            domain.User
	ApprovedAt       time.Time
	CategoryIDs      []int64
	UpdateCategories bool
	Note             string
}

type PostCategoryUpdateInput struct {
	PostID      int64
	Actor       domain.User
	CategoryIDs []int64
	UpdatedAt   time.Time
	Note        string
}

type ContentModerationInput struct {
	TargetType string
	TargetID   int64
	Actor      domain.User
	Reason     domain.ModerationReason
	Note       string
	ActedAt    time.Time
}

type CategoryCreateInput struct {
	Category  domain.Category
	Actor     domain.User
	CreatedAt time.Time
	Note      string
}

type CategoryDeleteInput struct {
	CategoryID int64
	Actor      domain.User
	DeletedAt  time.Time
	Note       string
}

type HistoryPurgeInput struct {
	Actor    domain.User
	Filter   domain.ModerationHistoryFilter
	PurgedAt time.Time
	Note     string
}

type ModerationRepo interface {
	HasAdminOrOwner(ctx context.Context) (bool, error)
	CreateBootstrapOwner(ctx context.Context, owner *domain.User) (int64, error)
	CreateRoleRequest(ctx context.Context, input RoleRequestCreateInput) (*domain.ModerationRoleRequest, error)
	ListRoleRequests(ctx context.Context, filter RoleRequestFilter) ([]domain.ModerationRoleRequest, error)
	GetRoleRequestByID(ctx context.Context, requestID int64) (*domain.ModerationRoleRequest, error)
	ReviewRoleRequest(ctx context.Context, input RoleRequestReviewInput) (*domain.ModerationRoleRequest, error)
	ChangeUserRole(ctx context.Context, input RoleChangeInput) (*domain.User, error)
	ListReports(ctx context.Context, filter ReportFilter) ([]domain.ModerationReport, error)
	GetReportByID(ctx context.Context, reportID int64) (*domain.ModerationReport, error)
	CreateReport(ctx context.Context, input ReportCreateInput) (*domain.ModerationReport, error)
	CloseReport(ctx context.Context, input ReportCloseInput) (*domain.ModerationReport, error)
	ListAppeals(ctx context.Context, filter AppealFilter) ([]domain.ModerationAppeal, error)
	GetAppealByID(ctx context.Context, appealID int64) (*domain.ModerationAppeal, error)
	CreateAppeal(ctx context.Context, input AppealCreateInput) (*domain.ModerationAppeal, error)
	CloseAppeal(ctx context.Context, input AppealCloseInput) (*domain.ModerationAppeal, error)
	ApprovePost(ctx context.Context, input PostApprovalInput) (*domain.Post, error)
	UpdatePostCategories(ctx context.Context, input PostCategoryUpdateInput) (*domain.Post, error)
	SoftDeleteContent(ctx context.Context, input ContentModerationInput) error
	RestoreContent(ctx context.Context, input ContentModerationInput) error
	HardDeleteContent(ctx context.Context, input ContentModerationInput) error
	SetPostDeleteProtection(ctx context.Context, postID int64, actor domain.User, protected bool, actedAt time.Time, note string) (*domain.Post, error)
	CreateCategory(ctx context.Context, input CategoryCreateInput) (*domain.Category, error)
	DeleteCategory(ctx context.Context, input CategoryDeleteInput) (int64, error)
	ListHistory(ctx context.Context, filter domain.ModerationHistoryFilter) ([]domain.ModerationHistoryRecord, error)
	PurgeHistory(ctx context.Context, input HistoryPurgeInput) (int64, error)
}

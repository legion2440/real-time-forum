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
	GetByID(ctx context.Context, id int64) (*domain.Post, error)
	Exists(ctx context.Context, id int64) (bool, error)
	Update(ctx context.Context, post *domain.Post, categoryIDs []int64) error
	Delete(ctx context.Context, id int64) error
}

type CommentRepo interface {
	Create(ctx context.Context, comment *domain.Comment) (int64, error)
	ListByPost(ctx context.Context, postID int64, filter domain.CommentFilter) ([]domain.Comment, error)
	GetByID(ctx context.Context, id int64) (*domain.Comment, error)
	Update(ctx context.Context, comment *domain.Comment) error
	Delete(ctx context.Context, id int64) error
}

type CategoryRepo interface {
	List(ctx context.Context) ([]domain.Category, error)
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

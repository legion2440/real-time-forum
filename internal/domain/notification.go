package domain

import "time"

const (
	NotificationBucketDM            = "dm"
	NotificationBucketMyContent     = "my_content"
	NotificationBucketSubscriptions = "subscriptions"
	NotificationBucketDeleted       = "deleted"
	NotificationBucketReports       = "reports"
	NotificationBucketAppeals       = "appeals"
	NotificationBucketManagement    = "management"
)

const (
	NotificationTypeDMMessage               = "dm_message"
	NotificationTypePostLiked               = "post_liked"
	NotificationTypePostDisliked            = "post_disliked"
	NotificationTypePostCommented           = "post_commented"
	NotificationTypeCommentLiked            = "comment_liked"
	NotificationTypeCommentDisliked         = "comment_disliked"
	NotificationTypeSubscribedPostCommented = "subscribed_post_commented"
	NotificationTypeFollowedAuthorPublished = "followed_author_published"
)

const (
	NotificationEntityTypeNone           = ""
	NotificationEntityTypePrivateMessage = "private_message"
	NotificationEntityTypePost           = "post"
	NotificationEntityTypeComment        = "comment"
	NotificationEntityTypeUser           = "user"
)

type NotificationPayload struct {
	ActorName      string `json:"actorName,omitempty"`
	ActorUsername  string `json:"actorUsername,omitempty"`
	PostID         int64  `json:"postId,omitempty"`
	PostTitle      string `json:"postTitle,omitempty"`
	PostPreview    string `json:"postPreview,omitempty"`
	CommentID      int64  `json:"commentId,omitempty"`
	CommentPreview string `json:"commentPreview,omitempty"`
	PeerID         int64  `json:"peerId,omitempty"`
	MessagePreview string `json:"messagePreview,omitempty"`
	HasAttachment  bool   `json:"hasAttachment,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

type Notification struct {
	ID                  int64
	UserID              int64
	ActorUserID         *int64
	Bucket              string
	Type                string
	EntityType          string
	EntityID            int64
	SecondaryEntityType string
	SecondaryEntityID   int64
	Payload             NotificationPayload
	IsRead              bool
	CreatedAt           time.Time
	ReadAt              *time.Time
}

type NotificationFilter struct {
	Bucket string
	Limit  int
	Offset int
}

type NotificationUnreadSummary struct {
	Total         int `json:"total"`
	DM            int `json:"dm"`
	MyContent     int `json:"myContent"`
	Subscriptions int `json:"subscriptions"`
	Deleted       int `json:"deleted"`
	Reports       int `json:"reports"`
	Appeals       int `json:"appeals"`
	Management    int `json:"management"`
}

type NotificationList struct {
	Items   []NotificationItem        `json:"items"`
	HasMore bool                      `json:"hasMore"`
	Summary NotificationUnreadSummary `json:"summary"`
}

type NotificationItem struct {
	ID              int64     `json:"id"`
	Bucket          string    `json:"bucket"`
	Type            string    `json:"type"`
	Text            string    `json:"text"`
	Context         string    `json:"context,omitempty"`
	LinkPath        string    `json:"linkPath,omitempty"`
	EntityAvailable bool      `json:"entityAvailable"`
	IsRead          bool      `json:"isRead"`
	CreatedAt       time.Time `json:"createdAt"`
}

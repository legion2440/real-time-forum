package domain

import "time"

const (
	ModerationTargetPost     = "post"
	ModerationTargetComment  = "comment"
	ModerationTargetCategory = "category"
	ModerationTargetUser     = "user"
	ModerationTargetReport   = "report"
	ModerationTargetAppeal   = "appeal"
	ModerationTargetRequest  = "role_request"
	ModerationTargetHistory  = "history"
)

type ModerationReason string

const (
	ModerationReasonIrrelevant ModerationReason = "irrelevant"
	ModerationReasonObscene    ModerationReason = "obscene"
	ModerationReasonIllegal    ModerationReason = "illegal"
	ModerationReasonInsulting  ModerationReason = "insulting"
	ModerationReasonOther      ModerationReason = "other"
)

type ModerationStatus string

const (
	ModerationStatusPending     ModerationStatus = "pending"
	ModerationStatusActionTaken ModerationStatus = "action_taken"
	ModerationStatusDismissed   ModerationStatus = "dismissed"
	ModerationStatusApproved    ModerationStatus = "approved"
	ModerationStatusRejected    ModerationStatus = "rejected"
	ModerationStatusRestored    ModerationStatus = "restored"
	ModerationStatusPurged      ModerationStatus = "purged"
)

type AppealStatus string

const (
	AppealStatusPending  AppealStatus = "pending"
	AppealStatusUpheld   AppealStatus = "upheld"
	AppealStatusReversed AppealStatus = "reversed"
	AppealStatusDismissed AppealStatus = "dismissed"
)

type RoleRequestStatus string

const (
	RoleRequestStatusPending  RoleRequestStatus = "pending"
	RoleRequestStatusApproved RoleRequestStatus = "approved"
	RoleRequestStatusRejected RoleRequestStatus = "rejected"
)

type ModerationActionType string

const (
	ActionBootstrapOwner        ModerationActionType = "bootstrap_owner"
	ActionPostQueued           ModerationActionType = "post_queued"
	ActionPostApproved         ModerationActionType = "post_approved"
	ActionPostCategoriesUpdated ModerationActionType = "post_categories_updated"
	ActionPostSoftDeleted      ModerationActionType = "post_soft_deleted"
	ActionPostRestored         ModerationActionType = "post_restored"
	ActionPostHardDeleted      ModerationActionType = "post_hard_deleted"
	ActionPostProtectionSet    ModerationActionType = "post_protection_set"
	ActionPostProtectionCleared ModerationActionType = "post_protection_cleared"
	ActionCommentSoftDeleted   ModerationActionType = "comment_soft_deleted"
	ActionCommentRestored      ModerationActionType = "comment_restored"
	ActionCommentHardDeleted   ModerationActionType = "comment_hard_deleted"
	ActionReportCreated        ModerationActionType = "report_created"
	ActionReportClosed         ModerationActionType = "report_closed"
	ActionAppealCreated        ModerationActionType = "appeal_created"
	ActionAppealClosed         ModerationActionType = "appeal_closed"
	ActionRoleRequested        ModerationActionType = "role_requested"
	ActionRoleRequestReviewed  ModerationActionType = "role_request_reviewed"
	ActionRoleChanged          ModerationActionType = "role_changed"
	ActionCategoryCreated      ModerationActionType = "category_created"
	ActionCategoryDeleted      ModerationActionType = "category_deleted"
	ActionHistoryPurged        ModerationActionType = "history_purged"
)

type ModerationTargetPreview struct {
	TargetType      string     `json:"targetType"`
	TargetID        int64      `json:"targetId"`
	PostID          int64      `json:"postId,omitempty"`
	CommentID       int64      `json:"commentId,omitempty"`
	PostTitle       string     `json:"postTitle,omitempty"`
	PostBody        string     `json:"postBody,omitempty"`
	CommentBody     string     `json:"commentBody,omitempty"`
	Categories      []Category `json:"categories,omitempty"`
	UnderReview     bool       `json:"underReview"`
	DeleteProtected bool       `json:"deleteProtected"`
	DeletedAt       *time.Time `json:"deletedAt,omitempty"`
}

type ModerationRoleRequest struct {
	ID            int64             `json:"id"`
	RequestedRole UserRole          `json:"requestedRole"`
	Applicant     UserRef           `json:"applicant"`
	Status        RoleRequestStatus `json:"status"`
	Note          string            `json:"note"`
	CreatedAt     time.Time         `json:"createdAt"`
	ReviewedAt    *time.Time        `json:"reviewedAt,omitempty"`
	ReviewedBy    *UserRef          `json:"reviewedBy,omitempty"`
	ReviewNote    string            `json:"reviewNote,omitempty"`
}

type ModerationReport struct {
	ID                       int64               `json:"id"`
	Target                   ModerationTargetPreview `json:"target"`
	Reporter                 UserRef             `json:"reporter"`
	ContentAuthor            UserRef             `json:"contentAuthor"`
	Reason                   ModerationReason    `json:"reason"`
	Note                     string              `json:"note"`
	Status                   ModerationStatus    `json:"status"`
	CreatedAt                time.Time           `json:"createdAt"`
	ClosedAt                 *time.Time          `json:"closedAt,omitempty"`
	ClosedBy                 *UserRef            `json:"closedBy,omitempty"`
	DecisionReason           ModerationReason    `json:"decisionReason,omitempty"`
	DecisionNote             string              `json:"decisionNote,omitempty"`
	LinkedPreviousDecisionID *int64              `json:"linkedPreviousDecisionId,omitempty"`
	RouteToRoles             []string            `json:"routeToRoles,omitempty"`
}

type ModerationAppeal struct {
	ID                       int64            `json:"id"`
	Target                   ModerationTargetPreview `json:"target"`
	Requester                UserRef          `json:"requester"`
	TargetRole               UserRole         `json:"targetRole"`
	Status                   AppealStatus     `json:"status"`
	Note                     string           `json:"note"`
	CreatedAt                time.Time        `json:"createdAt"`
	ClosedAt                 *time.Time       `json:"closedAt,omitempty"`
	ClosedBy                 *UserRef         `json:"closedBy,omitempty"`
	DecisionNote             string           `json:"decisionNote,omitempty"`
	LinkedPreviousDecisionID *int64           `json:"linkedPreviousDecisionId,omitempty"`
}

type ModerationHistoryRecord struct {
	ID                       int64                `json:"id"`
	ActedAt                  time.Time            `json:"actedAt"`
	ActionType               ModerationActionType `json:"actionType"`
	TargetType               string               `json:"targetType"`
	TargetID                 int64                `json:"targetId"`
	ContentAuthor            UserRef              `json:"contentAuthor"`
	Actor                    UserRef              `json:"actor"`
	ActorRole                UserRole             `json:"actorRole"`
	Reason                   string               `json:"reason,omitempty"`
	Note                     string               `json:"note,omitempty"`
	CurrentStatus            string               `json:"currentStatus,omitempty"`
	RouteToRole              string               `json:"routeToRole,omitempty"`
	LinkedPreviousDecisionID *int64               `json:"linkedPreviousDecisionId,omitempty"`
	PostTitleSnapshot        string               `json:"postTitleSnapshot,omitempty"`
	PostBodySnapshot         string               `json:"postBodySnapshot,omitempty"`
	CommentBodySnapshot      string               `json:"commentBodySnapshot,omitempty"`
}

type ModerationHistoryFilter struct {
	ActionType string
	TargetType string
	Status     string
	From       *time.Time
	To         *time.Time
	Limit      int
	Offset     int
}

type FutureRestrictionScope string

const (
	FutureRestrictionPosting FutureRestrictionScope = "posting"
	FutureRestrictionCommenting FutureRestrictionScope = "commenting"
	FutureRestrictionChatting FutureRestrictionScope = "chatting"
)

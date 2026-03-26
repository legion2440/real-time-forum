package service

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"forum/internal/domain"
	"forum/internal/platform/clock"
	"forum/internal/repo"
)

const defaultCenterPageSize = 20

type NotificationRealtime interface {
	PublishNotificationNew(userID int64, notification domain.NotificationItem, summary domain.NotificationUnreadSummary)
	PublishNotificationUpdate(userID int64, notification domain.NotificationItem, summary domain.NotificationUnreadSummary)
	PublishNotificationSummary(userID int64, summary domain.NotificationUnreadSummary)
}

type CenterService struct {
	center   repo.CenterRepo
	users    repo.UserRepo
	posts    repo.PostRepo
	comments repo.CommentRepo
	clock    clock.Clock
	realtime NotificationRealtime
}

func NewCenterService(center repo.CenterRepo, users repo.UserRepo, posts repo.PostRepo, comments repo.CommentRepo, clock clock.Clock, deps ...any) *CenterService {
	service := &CenterService{
		center:   center,
		users:    users,
		posts:    posts,
		comments: comments,
		clock:    clock,
	}
	for _, dependency := range deps {
		if realtime, ok := dependency.(NotificationRealtime); ok && realtime != nil {
			service.realtime = realtime
		}
	}
	return service
}

func (s *CenterService) GetUnreadSummary(ctx context.Context, userID int64) (domain.NotificationUnreadSummary, error) {
	if userID <= 0 {
		return domain.NotificationUnreadSummary{}, ErrInvalidInput
	}
	return s.center.CountUnreadNotifications(ctx, userID)
}

func (s *CenterService) ListNotifications(ctx context.Context, userID int64, filter domain.NotificationFilter) (domain.NotificationList, error) {
	if userID <= 0 {
		return domain.NotificationList{}, ErrInvalidInput
	}

	normalizedBucket, err := normalizeNotificationBucket(filter.Bucket, true)
	if err != nil {
		return domain.NotificationList{}, err
	}

	limit := normalizePageSize(filter.Limit)
	offset := normalizeOffset(filter.Offset)
	notifications, err := s.center.ListNotifications(ctx, userID, domain.NotificationFilter{
		Bucket: normalizedBucket,
		Limit:  limit + 1,
		Offset: offset,
	})
	if err != nil {
		return domain.NotificationList{}, err
	}

	hasMore := len(notifications) > limit
	if hasMore {
		notifications = notifications[:limit]
	}

	items := make([]domain.NotificationItem, 0, len(notifications))
	for _, notification := range notifications {
		item, err := s.buildNotificationItem(ctx, notification)
		if err != nil {
			return domain.NotificationList{}, err
		}
		items = append(items, item)
	}

	summary, err := s.center.CountUnreadNotifications(ctx, userID)
	if err != nil {
		return domain.NotificationList{}, err
	}

	return domain.NotificationList{
		Items:   items,
		HasMore: hasMore,
		Summary: summary,
	}, nil
}

func (s *CenterService) MarkNotificationRead(ctx context.Context, userID, notificationID int64) (*domain.NotificationItem, domain.NotificationUnreadSummary, error) {
	if userID <= 0 || notificationID <= 0 {
		return nil, domain.NotificationUnreadSummary{}, ErrInvalidInput
	}

	if err := s.center.MarkNotificationRead(ctx, userID, notificationID, s.clock.Now()); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, domain.NotificationUnreadSummary{}, ErrNotFound
		}
		return nil, domain.NotificationUnreadSummary{}, err
	}

	notification, err := s.center.GetNotification(ctx, userID, notificationID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, domain.NotificationUnreadSummary{}, ErrNotFound
		}
		return nil, domain.NotificationUnreadSummary{}, err
	}

	item, err := s.buildNotificationItem(ctx, *notification)
	if err != nil {
		return nil, domain.NotificationUnreadSummary{}, err
	}
	summary, err := s.center.CountUnreadNotifications(ctx, userID)
	if err != nil {
		return nil, domain.NotificationUnreadSummary{}, err
	}
	if s.realtime != nil {
		s.realtime.PublishNotificationUpdate(userID, item, summary)
	}
	return &item, summary, nil
}

func (s *CenterService) MarkAllNotificationsRead(ctx context.Context, userID int64, bucket string) (domain.NotificationUnreadSummary, error) {
	if userID <= 0 {
		return domain.NotificationUnreadSummary{}, ErrInvalidInput
	}

	normalizedBucket, err := normalizeNotificationBucket(bucket, true)
	if err != nil {
		return domain.NotificationUnreadSummary{}, err
	}
	if err := s.center.MarkAllNotificationsRead(ctx, userID, normalizedBucket, s.clock.Now()); err != nil {
		return domain.NotificationUnreadSummary{}, err
	}

	summary, err := s.center.CountUnreadNotifications(ctx, userID)
	if err != nil {
		return domain.NotificationUnreadSummary{}, err
	}
	if s.realtime != nil {
		s.realtime.PublishNotificationSummary(userID, summary)
	}
	return summary, nil
}

func (s *CenterService) MarkDMConversationNotificationsRead(ctx context.Context, userID, peerID, lastReadMessageID int64) (domain.NotificationUnreadSummary, error) {
	if userID <= 0 || peerID <= 0 || lastReadMessageID < 0 {
		return domain.NotificationUnreadSummary{}, ErrInvalidInput
	}

	if err := s.center.MarkDMNotificationsRead(ctx, userID, peerID, lastReadMessageID, s.clock.Now()); err != nil {
		return domain.NotificationUnreadSummary{}, err
	}
	summary, err := s.center.CountUnreadNotifications(ctx, userID)
	if err != nil {
		return domain.NotificationUnreadSummary{}, err
	}
	if s.realtime != nil {
		s.realtime.PublishNotificationSummary(userID, summary)
	}
	return summary, nil
}

func (s *CenterService) ListActivity(ctx context.Context, userID int64, limit, postsOffset, reactionsOffset, commentsOffset int) (*domain.ActivityData, error) {
	if userID <= 0 {
		return nil, ErrInvalidInput
	}

	pageSize := normalizePageSize(limit)
	posts, err := s.center.ListActivityPosts(ctx, userID, pageSize+1, normalizeOffset(postsOffset))
	if err != nil {
		return nil, err
	}
	reactions, err := s.center.ListActivityReactions(ctx, userID, pageSize+1, normalizeOffset(reactionsOffset))
	if err != nil {
		return nil, err
	}
	comments, err := s.center.ListActivityComments(ctx, userID, pageSize+1, normalizeOffset(commentsOffset))
	if err != nil {
		return nil, err
	}

	data := &domain.ActivityData{
		PostsHasMore:     len(posts) > pageSize,
		ReactionsHasMore: len(reactions) > pageSize,
		CommentsHasMore:  len(comments) > pageSize,
	}
	if data.PostsHasMore {
		posts = posts[:pageSize]
	}
	if data.ReactionsHasMore {
		reactions = reactions[:pageSize]
	}
	if data.CommentsHasMore {
		comments = comments[:pageSize]
	}

	for i := range comments {
		comments[i].LinkPath = buildPostCommentPath(comments[i].PostID, comments[i].Comment.ID)
	}
	for i := range reactions {
		switch reactions[i].TargetType {
		case domain.NotificationEntityTypeComment:
			reactions[i].LinkPath = buildPostCommentPath(reactions[i].PostID, reactions[i].CommentID)
		default:
			reactions[i].LinkPath = buildPostPath(reactions[i].PostID)
		}
	}

	data.Posts = posts
	data.Reactions = reactions
	data.Comments = comments
	return data, nil
}

func (s *CenterService) SubscribePost(ctx context.Context, userID, postID int64) error {
	if userID <= 0 || postID <= 0 {
		return ErrInvalidInput
	}
	exists, err := s.posts.Exists(ctx, postID)
	if err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return s.center.CreatePostSubscription(ctx, userID, postID, s.clock.Now())
}

func (s *CenterService) UnsubscribePost(ctx context.Context, userID, postID int64) error {
	if userID <= 0 || postID <= 0 {
		return ErrInvalidInput
	}
	return s.center.DeletePostSubscription(ctx, userID, postID)
}

func (s *CenterService) IsPostSubscribed(ctx context.Context, userID, postID int64) (bool, error) {
	if userID <= 0 || postID <= 0 {
		return false, nil
	}
	return s.center.IsPostSubscribed(ctx, userID, postID)
}

func (s *CenterService) FollowUser(ctx context.Context, followerUserID, followedUserID int64) error {
	if followerUserID <= 0 || followedUserID <= 0 || followerUserID == followedUserID {
		return ErrInvalidInput
	}
	if _, err := s.users.GetByID(ctx, followedUserID); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	return s.center.CreateUserFollow(ctx, followerUserID, followedUserID, s.clock.Now())
}

func (s *CenterService) UnfollowUser(ctx context.Context, followerUserID, followedUserID int64) error {
	if followerUserID <= 0 || followedUserID <= 0 || followerUserID == followedUserID {
		return ErrInvalidInput
	}
	return s.center.DeleteUserFollow(ctx, followerUserID, followedUserID)
}

func (s *CenterService) IsFollowingUser(ctx context.Context, followerUserID, followedUserID int64) (bool, error) {
	if followerUserID <= 0 || followedUserID <= 0 || followerUserID == followedUserID {
		return false, nil
	}
	return s.center.IsFollowingUser(ctx, followerUserID, followedUserID)
}

func (s *CenterService) HandlePostCreated(ctx context.Context, post *domain.Post) error {
	if post == nil || post.ID <= 0 || post.UserID <= 0 {
		return nil
	}
	if err := s.center.CreatePostSubscription(ctx, post.UserID, post.ID, s.clock.Now()); err != nil {
		return err
	}

	authorName, authorUsername := userRefSnapshot(post.Author, post.UserID)
	followerIDs, err := s.center.ListFollowerUserIDs(ctx, post.UserID)
	if err != nil {
		return err
	}
	for _, followerID := range followerIDs {
		if followerID <= 0 || followerID == post.UserID {
			continue
		}
		if err := s.createAndPublishNotification(ctx, domain.Notification{
			UserID:              followerID,
			ActorUserID:         int64Ptr(post.UserID),
			Bucket:              domain.NotificationBucketSubscriptions,
			Type:                domain.NotificationTypeFollowedAuthorPublished,
			EntityType:          domain.NotificationEntityTypePost,
			EntityID:            post.ID,
			SecondaryEntityType: domain.NotificationEntityTypeUser,
			SecondaryEntityID:   post.UserID,
			Payload: domain.NotificationPayload{
				ActorName:     authorName,
				ActorUsername: authorUsername,
				PostID:        post.ID,
				PostTitle:     strings.TrimSpace(post.Title),
				PostPreview:   previewText(post.Body, 140),
			},
			CreatedAt: post.CreatedAt,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *CenterService) HandleCommentCreated(ctx context.Context, comment *domain.Comment) error {
	if comment == nil || comment.ID <= 0 || comment.UserID <= 0 || comment.PostID <= 0 {
		return nil
	}

	post, err := s.posts.GetByID(ctx, comment.PostID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}

	actorName, actorUsername := userRefSnapshot(comment.Author, comment.UserID)
	payload := domain.NotificationPayload{
		ActorName:      actorName,
		ActorUsername:  actorUsername,
		PostID:         post.ID,
		PostTitle:      strings.TrimSpace(post.Title),
		PostPreview:    previewText(post.Body, 140),
		CommentID:      comment.ID,
		CommentPreview: previewText(comment.Body, 140),
	}

	if comment.UserID != post.UserID {
		if err := s.createAndPublishNotification(ctx, domain.Notification{
			UserID:              post.UserID,
			ActorUserID:         int64Ptr(comment.UserID),
			Bucket:              domain.NotificationBucketMyContent,
			Type:                domain.NotificationTypePostCommented,
			EntityType:          domain.NotificationEntityTypeComment,
			EntityID:            comment.ID,
			SecondaryEntityType: domain.NotificationEntityTypePost,
			SecondaryEntityID:   post.ID,
			Payload:             payload,
			CreatedAt:           comment.CreatedAt,
		}); err != nil {
			return err
		}
	}

	subscriberIDs, err := s.center.ListPostSubscriberUserIDs(ctx, post.ID)
	if err != nil {
		return err
	}
	for _, subscriberID := range subscriberIDs {
		if subscriberID <= 0 || subscriberID == comment.UserID || subscriberID == post.UserID {
			continue
		}
		if err := s.createAndPublishNotification(ctx, domain.Notification{
			UserID:              subscriberID,
			ActorUserID:         int64Ptr(comment.UserID),
			Bucket:              domain.NotificationBucketSubscriptions,
			Type:                domain.NotificationTypeSubscribedPostCommented,
			EntityType:          domain.NotificationEntityTypeComment,
			EntityID:            comment.ID,
			SecondaryEntityType: domain.NotificationEntityTypePost,
			SecondaryEntityID:   post.ID,
			Payload:             payload,
			CreatedAt:           comment.CreatedAt,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *CenterService) HandlePostReaction(ctx context.Context, actorUserID, postID int64, change domain.ReactionChange) error {
	if actorUserID <= 0 || postID <= 0 || !change.Changed() || change.CurrentValue == 0 {
		return nil
	}

	post, err := s.posts.GetByID(ctx, postID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if actorUserID == post.UserID {
		return nil
	}

	actor, err := s.users.GetByID(ctx, actorUserID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}

	notificationType := domain.NotificationTypePostLiked
	if change.CurrentValue < 0 {
		notificationType = domain.NotificationTypePostDisliked
	}

	return s.createAndPublishNotification(ctx, domain.Notification{
		UserID:      post.UserID,
		ActorUserID: int64Ptr(actorUserID),
		Bucket:      domain.NotificationBucketMyContent,
		Type:        notificationType,
		EntityType:  domain.NotificationEntityTypePost,
		EntityID:    post.ID,
		Payload: domain.NotificationPayload{
			ActorName:     displayNameOrUsername(actor),
			ActorUsername: strings.TrimSpace(actor.Username),
			PostID:        post.ID,
			PostTitle:     strings.TrimSpace(post.Title),
			PostPreview:   previewText(post.Body, 140),
		},
		CreatedAt: s.clock.Now(),
	})
}

func (s *CenterService) HandleCommentReaction(ctx context.Context, actorUserID, commentID int64, change domain.ReactionChange) error {
	if actorUserID <= 0 || commentID <= 0 || !change.Changed() || change.CurrentValue == 0 {
		return nil
	}

	comment, err := s.comments.GetByID(ctx, commentID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if actorUserID == comment.UserID {
		return nil
	}

	post, err := s.posts.GetByID(ctx, comment.PostID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}

	actor, err := s.users.GetByID(ctx, actorUserID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}

	notificationType := domain.NotificationTypeCommentLiked
	if change.CurrentValue < 0 {
		notificationType = domain.NotificationTypeCommentDisliked
	}

	return s.createAndPublishNotification(ctx, domain.Notification{
		UserID:              comment.UserID,
		ActorUserID:         int64Ptr(actorUserID),
		Bucket:              domain.NotificationBucketMyContent,
		Type:                notificationType,
		EntityType:          domain.NotificationEntityTypeComment,
		EntityID:            comment.ID,
		SecondaryEntityType: domain.NotificationEntityTypePost,
		SecondaryEntityID:   post.ID,
		Payload: domain.NotificationPayload{
			ActorName:      displayNameOrUsername(actor),
			ActorUsername:  strings.TrimSpace(actor.Username),
			PostID:         post.ID,
			PostTitle:      strings.TrimSpace(post.Title),
			PostPreview:    previewText(post.Body, 140),
			CommentID:      comment.ID,
			CommentPreview: previewText(comment.Body, 140),
		},
		CreatedAt: s.clock.Now(),
	})
}

func (s *CenterService) HandlePrivateMessage(ctx context.Context, msg *domain.PrivateMessage) error {
	if msg == nil || msg.ID <= 0 || msg.FromUserID <= 0 || msg.ToUserID <= 0 || msg.FromUserID == msg.ToUserID {
		return nil
	}

	actor, err := s.users.GetByID(ctx, msg.FromUserID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}

	return s.createAndPublishNotification(ctx, domain.Notification{
		UserID:              msg.ToUserID,
		ActorUserID:         int64Ptr(msg.FromUserID),
		Bucket:              domain.NotificationBucketDM,
		Type:                domain.NotificationTypeDMMessage,
		EntityType:          domain.NotificationEntityTypePrivateMessage,
		EntityID:            msg.ID,
		SecondaryEntityType: domain.NotificationEntityTypeUser,
		SecondaryEntityID:   msg.FromUserID,
		Payload: domain.NotificationPayload{
			ActorName:      displayNameOrUsername(actor),
			ActorUsername:  strings.TrimSpace(actor.Username),
			PeerID:         msg.FromUserID,
			MessagePreview: previewText(msg.Body, 140),
			HasAttachment:  msg.Attachment != nil,
		},
		CreatedAt: msg.CreatedAt,
	})
}

func (s *CenterService) createAndPublishNotification(ctx context.Context, notification domain.Notification) error {
	notification.CreatedAt = notification.CreatedAt.UTC()
	id, err := s.center.CreateNotification(ctx, &notification)
	if err != nil {
		return err
	}
	notification.ID = id

	item, err := s.buildNotificationItem(ctx, notification)
	if err != nil {
		return err
	}
	summary, err := s.center.CountUnreadNotifications(ctx, notification.UserID)
	if err != nil {
		return err
	}
	if s.realtime != nil {
		s.realtime.PublishNotificationNew(notification.UserID, item, summary)
	}
	return nil
}

func (s *CenterService) buildNotificationItem(ctx context.Context, notification domain.Notification) (domain.NotificationItem, error) {
	item := domain.NotificationItem{
		ID:              notification.ID,
		Bucket:          notification.Bucket,
		Type:            notification.Type,
		Text:            buildNotificationText(notification),
		Context:         buildNotificationContext(notification),
		LinkPath:        buildNotificationLinkPath(notification),
		EntityAvailable: true,
		IsRead:          notification.IsRead,
		CreatedAt:       notification.CreatedAt,
	}

	available, err := s.isNotificationEntityAvailable(ctx, notification)
	if err != nil {
		return domain.NotificationItem{}, err
	}
	item.EntityAvailable = available
	if !available {
		item.Context = buildUnavailableNotificationContext(notification)
	}
	return item, nil
}

func (s *CenterService) isNotificationEntityAvailable(ctx context.Context, notification domain.Notification) (bool, error) {
	switch notification.Type {
	case domain.NotificationTypeDMMessage:
		return true, nil
	case domain.NotificationTypePostLiked, domain.NotificationTypePostDisliked, domain.NotificationTypeFollowedAuthorPublished,
		domain.NotificationTypePostCommented, domain.NotificationTypeSubscribedPostCommented:
		return s.notificationPostExists(ctx, notification)
	case domain.NotificationTypeCommentLiked, domain.NotificationTypeCommentDisliked:
		commentExists, err := s.notificationCommentExists(ctx, notification)
		if err != nil || !commentExists {
			return commentExists, err
		}
		postID := notificationPostID(notification)
		if postID <= 0 {
			return true, nil
		}
		return s.posts.Exists(ctx, postID)
	default:
		return true, nil
	}
}

func (s *CenterService) notificationPostExists(ctx context.Context, notification domain.Notification) (bool, error) {
	postID := notificationPostID(notification)
	if postID <= 0 {
		return false, nil
	}
	return s.posts.Exists(ctx, postID)
}

func (s *CenterService) notificationCommentExists(ctx context.Context, notification domain.Notification) (bool, error) {
	commentID := notificationCommentID(notification)
	if commentID <= 0 {
		return false, nil
	}
	comment, err := s.comments.GetByID(ctx, commentID)
	if err == nil {
		if comment.DeletedAt != nil {
			return false, nil
		}
		return true, nil
	}
	if errors.Is(err, repo.ErrNotFound) {
		return false, nil
	}
	return false, err
}

func notificationPostID(notification domain.Notification) int64 {
	if notification.Payload.PostID > 0 {
		return notification.Payload.PostID
	}
	if notification.SecondaryEntityType == domain.NotificationEntityTypePost && notification.SecondaryEntityID > 0 {
		return notification.SecondaryEntityID
	}
	if notification.EntityType == domain.NotificationEntityTypePost && notification.EntityID > 0 {
		return notification.EntityID
	}
	return 0
}

func notificationCommentID(notification domain.Notification) int64 {
	if notification.Payload.CommentID > 0 {
		return notification.Payload.CommentID
	}
	if notification.EntityType == domain.NotificationEntityTypeComment && notification.EntityID > 0 {
		return notification.EntityID
	}
	return 0
}

func buildNotificationText(notification domain.Notification) string {
	actorName := strings.TrimSpace(notification.Payload.ActorName)
	if actorName == "" {
		actorName = "Someone"
	}

	switch notification.Type {
	case domain.NotificationTypeDMMessage:
		if notification.Payload.HasAttachment && strings.TrimSpace(notification.Payload.MessagePreview) == "" {
			return actorName + " sent you an image"
		}
		return actorName + " sent you a new DM"
	case domain.NotificationTypePostLiked:
		return actorName + " liked your post"
	case domain.NotificationTypePostDisliked:
		return actorName + " disliked your post"
	case domain.NotificationTypePostCommented:
		return actorName + " commented on your post"
	case domain.NotificationTypeCommentLiked:
		return actorName + " liked your comment"
	case domain.NotificationTypeCommentDisliked:
		return actorName + " disliked your comment"
	case domain.NotificationTypeSubscribedPostCommented:
		return actorName + " commented on a post you subscribed to"
	case domain.NotificationTypeFollowedAuthorPublished:
		return actorName + " published a new post"
	default:
		return "New notification"
	}
}

func buildNotificationContext(notification domain.Notification) string {
	switch notification.Type {
	case domain.NotificationTypeDMMessage:
		if strings.TrimSpace(notification.Payload.MessagePreview) != "" {
			return notification.Payload.MessagePreview
		}
		if notification.Payload.HasAttachment {
			return "Image attachment"
		}
		return ""
	case domain.NotificationTypePostLiked, domain.NotificationTypePostDisliked, domain.NotificationTypePostCommented, domain.NotificationTypeFollowedAuthorPublished:
		if strings.TrimSpace(notification.Payload.PostTitle) != "" {
			return notification.Payload.PostTitle
		}
		return notification.Payload.PostPreview
	case domain.NotificationTypeCommentLiked, domain.NotificationTypeCommentDisliked, domain.NotificationTypeSubscribedPostCommented:
		if strings.TrimSpace(notification.Payload.CommentPreview) != "" {
			return notification.Payload.CommentPreview
		}
		if strings.TrimSpace(notification.Payload.PostTitle) != "" {
			return notification.Payload.PostTitle
		}
		return notification.Payload.PostPreview
	default:
		return ""
	}
}

func buildUnavailableNotificationContext(notification domain.Notification) string {
	switch notification.Type {
	case domain.NotificationTypeDMMessage:
		return buildNotificationContext(notification)
	case domain.NotificationTypePostLiked, domain.NotificationTypePostDisliked, domain.NotificationTypePostCommented, domain.NotificationTypeFollowedAuthorPublished, domain.NotificationTypeSubscribedPostCommented:
		if title := strings.TrimSpace(notification.Payload.PostTitle); title != "" {
			return "[deleted] " + title
		}
		if preview := strings.TrimSpace(notification.Payload.PostPreview); preview != "" {
			return "[deleted] " + preview
		}
		return "post deleted"
	case domain.NotificationTypeCommentLiked, domain.NotificationTypeCommentDisliked:
		if preview := strings.TrimSpace(notification.Payload.CommentPreview); preview != "" {
			return "[deleted comment] " + preview
		}
		return "comment deleted"
	default:
		return "content is no longer available"
	}
}

func buildNotificationLinkPath(notification domain.Notification) string {
	switch notification.Type {
	case domain.NotificationTypeDMMessage:
		if notification.Payload.PeerID > 0 {
			return "/dm/" + strconv.FormatInt(notification.Payload.PeerID, 10)
		}
	case domain.NotificationTypePostLiked, domain.NotificationTypePostDisliked, domain.NotificationTypePostCommented, domain.NotificationTypeFollowedAuthorPublished:
		postID := notification.Payload.PostID
		if postID <= 0 {
			postID = notification.EntityID
		}
		return buildPostPath(postID)
	case domain.NotificationTypeCommentLiked, domain.NotificationTypeCommentDisliked, domain.NotificationTypeSubscribedPostCommented:
		postID := notification.Payload.PostID
		commentID := notification.Payload.CommentID
		if postID <= 0 && notification.SecondaryEntityType == domain.NotificationEntityTypePost {
			postID = notification.SecondaryEntityID
		}
		if commentID <= 0 && notification.EntityType == domain.NotificationEntityTypeComment {
			commentID = notification.EntityID
		}
		return buildPostCommentPath(postID, commentID)
	}
	return ""
}

func buildPostPath(postID int64) string {
	if postID <= 0 {
		return ""
	}
	return "/post/" + strconv.FormatInt(postID, 10)
}

func buildPostCommentPath(postID, commentID int64) string {
	if postID <= 0 {
		return ""
	}
	if commentID <= 0 {
		return buildPostPath(postID)
	}
	return buildPostPath(postID) + "#comment-" + strconv.FormatInt(commentID, 10)
}

func normalizeNotificationBucket(bucket string, allowEmpty bool) (string, error) {
	normalized := strings.TrimSpace(bucket)
	if normalized == "" && allowEmpty {
		return "", nil
	}
	switch normalized {
	case domain.NotificationBucketDM, domain.NotificationBucketMyContent, domain.NotificationBucketSubscriptions:
		return normalized, nil
	default:
		return "", ErrInvalidInput
	}
}

func normalizePageSize(limit int) int {
	if limit <= 0 {
		return defaultCenterPageSize
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func normalizeOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}

func previewText(value string, maxLen int) string {
	trimmed := strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if maxLen <= 0 || len(trimmed) <= maxLen {
		return trimmed
	}
	if maxLen <= 1 {
		return trimmed[:maxLen]
	}
	return strings.TrimSpace(trimmed[:maxLen-1]) + "…"
}

func displayNameOrUsername(user *domain.User) string {
	if user == nil {
		return ""
	}
	displayName := strings.TrimSpace(user.DisplayName)
	if displayName != "" {
		return displayName
	}
	return strings.TrimSpace(user.Username)
}

func userRefSnapshot(ref domain.UserRef, fallbackID int64) (string, string) {
	name := strings.TrimSpace(ref.DisplayName)
	username := strings.TrimSpace(ref.Username)
	if name == "" {
		name = username
	}
	if name == "" && fallbackID > 0 {
		name = "user-" + strconv.FormatInt(fallbackID, 10)
	}
	return name, username
}

func int64Ptr(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	return &value
}

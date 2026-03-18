package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"forum/internal/domain"
	"forum/internal/repo"
)

type CenterRepo struct {
	db *sql.DB
}

func NewCenterRepo(db *sql.DB) *CenterRepo {
	return &CenterRepo{db: db}
}

func (r *CenterRepo) CreateNotification(ctx context.Context, notification *domain.Notification) (int64, error) {
	payloadJSON, err := json.Marshal(notification.Payload)
	if err != nil {
		return 0, err
	}

	var actorUserID any
	if notification.ActorUserID != nil && *notification.ActorUserID > 0 {
		actorUserID = *notification.ActorUserID
	}

	var readAt any
	if notification.ReadAt != nil && !notification.ReadAt.IsZero() {
		readAt = timeToUnix(notification.ReadAt.UTC())
	}

	res, err := r.db.ExecContext(ctx, `
		INSERT INTO notifications (
			user_id, actor_user_id, bucket, type, entity_type, entity_id,
			secondary_entity_type, secondary_entity_id, payload_json, is_read, created_at, read_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, notification.UserID, actorUserID, notification.Bucket, notification.Type, notification.EntityType, notification.EntityID,
		notification.SecondaryEntityType, notification.SecondaryEntityID, string(payloadJSON), boolToInt(notification.IsRead),
		timeToUnix(notification.CreatedAt), readAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (r *CenterRepo) GetNotification(ctx context.Context, userID, notificationID int64) (*domain.Notification, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, actor_user_id, bucket, type, entity_type, entity_id,
		       secondary_entity_type, secondary_entity_id, payload_json, is_read, created_at, read_at
		FROM notifications
		WHERE id = ? AND user_id = ?
	`, notificationID, userID)

	notification, err := scanNotification(row)
	if err != nil {
		return nil, err
	}
	return notification, nil
}

func (r *CenterRepo) ListNotifications(ctx context.Context, userID int64, filter domain.NotificationFilter) ([]domain.Notification, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	var (
		query strings.Builder
		args  []any
	)
	query.WriteString(`
		SELECT id, user_id, actor_user_id, bucket, type, entity_type, entity_id,
		       secondary_entity_type, secondary_entity_id, payload_json, is_read, created_at, read_at
		FROM notifications
		WHERE user_id = ?
	`)
	args = append(args, userID)
	if strings.TrimSpace(filter.Bucket) != "" {
		query.WriteString(` AND bucket = ?`)
		args = append(args, filter.Bucket)
	}
	query.WriteString(` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`)
	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]domain.Notification, 0)
	for rows.Next() {
		item, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *CenterRepo) CountUnreadNotifications(ctx context.Context, userID int64) (domain.NotificationUnreadSummary, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN is_read = 0 THEN 1 ELSE 0 END), 0) AS total,
			COALESCE(SUM(CASE WHEN is_read = 0 AND bucket = ? THEN 1 ELSE 0 END), 0) AS dm_total,
			COALESCE(SUM(CASE WHEN is_read = 0 AND bucket = ? THEN 1 ELSE 0 END), 0) AS my_content_total,
			COALESCE(SUM(CASE WHEN is_read = 0 AND bucket = ? THEN 1 ELSE 0 END), 0) AS subscriptions_total
		FROM notifications
		WHERE user_id = ?
	`, domain.NotificationBucketDM, domain.NotificationBucketMyContent, domain.NotificationBucketSubscriptions, userID)

	var summary domain.NotificationUnreadSummary
	if err := row.Scan(&summary.Total, &summary.DM, &summary.MyContent, &summary.Subscriptions); err != nil {
		return summary, err
	}
	return summary, nil
}

func (r *CenterRepo) MarkNotificationRead(ctx context.Context, userID, notificationID int64, readAt time.Time) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE notifications
		SET is_read = 1, read_at = COALESCE(read_at, ?)
		WHERE id = ? AND user_id = ?
	`, timeToUnix(readAt), notificationID, userID)
	if err != nil {
		return err
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return repo.ErrNotFound
	}
	return nil
}

func (r *CenterRepo) MarkAllNotificationsRead(ctx context.Context, userID int64, bucket string, readAt time.Time) error {
	if strings.TrimSpace(bucket) == "" {
		_, err := r.db.ExecContext(ctx, `
			UPDATE notifications
			SET is_read = 1, read_at = COALESCE(read_at, ?)
			WHERE user_id = ? AND is_read = 0
		`, timeToUnix(readAt), userID)
		return err
	}

	_, err := r.db.ExecContext(ctx, `
		UPDATE notifications
		SET is_read = 1, read_at = COALESCE(read_at, ?)
		WHERE user_id = ? AND bucket = ? AND is_read = 0
	`, timeToUnix(readAt), userID, bucket)
	return err
}

func (r *CenterRepo) MarkDMNotificationsRead(ctx context.Context, userID, peerID, lastReadMessageID int64, readAt time.Time) error {
	if lastReadMessageID <= 0 {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE notifications
		SET is_read = 1, read_at = COALESCE(read_at, ?)
		WHERE user_id = ?
		  AND bucket = ?
		  AND type = ?
		  AND secondary_entity_type = ?
		  AND secondary_entity_id = ?
		  AND entity_id <= ?
		  AND is_read = 0
	`, timeToUnix(readAt), userID, domain.NotificationBucketDM, domain.NotificationTypeDMMessage, domain.NotificationEntityTypeUser, peerID, lastReadMessageID)
	return err
}

func (r *CenterRepo) CreatePostSubscription(ctx context.Context, userID, postID int64, createdAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO post_subscriptions (user_id, post_id, created_at)
		VALUES (?, ?, ?)
	`, userID, postID, timeToUnix(createdAt))
	return err
}

func (r *CenterRepo) DeletePostSubscription(ctx context.Context, userID, postID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM post_subscriptions WHERE user_id = ? AND post_id = ?`, userID, postID)
	return err
}

func (r *CenterRepo) IsPostSubscribed(ctx context.Context, userID, postID int64) (bool, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT 1
		FROM post_subscriptions
		WHERE user_id = ? AND post_id = ?
		LIMIT 1
	`, userID, postID)
	var marker int
	if err := row.Scan(&marker); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return marker == 1, nil
}

func (r *CenterRepo) ListPostSubscriberUserIDs(ctx context.Context, postID int64) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT user_id
		FROM post_subscriptions
		WHERE post_id = ?
		ORDER BY user_id ASC
	`, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			return nil, err
		}
		ids = append(ids, userID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *CenterRepo) CreateUserFollow(ctx context.Context, followerUserID, followedUserID int64, createdAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO user_follows (follower_user_id, followed_user_id, created_at)
		VALUES (?, ?, ?)
	`, followerUserID, followedUserID, timeToUnix(createdAt))
	return err
}

func (r *CenterRepo) DeleteUserFollow(ctx context.Context, followerUserID, followedUserID int64) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM user_follows
		WHERE follower_user_id = ? AND followed_user_id = ?
	`, followerUserID, followedUserID)
	return err
}

func (r *CenterRepo) IsFollowingUser(ctx context.Context, followerUserID, followedUserID int64) (bool, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT 1
		FROM user_follows
		WHERE follower_user_id = ? AND followed_user_id = ?
		LIMIT 1
	`, followerUserID, followedUserID)
	var marker int
	if err := row.Scan(&marker); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return marker == 1, nil
}

func (r *CenterRepo) ListFollowerUserIDs(ctx context.Context, followedUserID int64) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT follower_user_id
		FROM user_follows
		WHERE followed_user_id = ?
		ORDER BY follower_user_id ASC
	`, followedUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			return nil, err
		}
		ids = append(ids, userID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *CenterRepo) ListActivityPosts(ctx context.Context, userID int64, limit, offset int) ([]domain.Post, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT p.id, p.user_id, u.username, u.display_name, p.title, p.body,
		       a.id, a.mime, a.size,
		       p.created_at,
		       COALESCE(SUM(CASE WHEN pr.value = 1 THEN 1 ELSE 0 END), 0) AS likes,
		       COALESCE(SUM(CASE WHEN pr.value = -1 THEN 1 ELSE 0 END), 0) AS dislikes,
		       (SELECT COUNT(1) FROM comments c WHERE c.post_id = p.id) AS comments_count
		FROM posts p
		JOIN users u ON u.id = p.user_id
		LEFT JOIN attachments a ON a.id = p.attachment_id
		LEFT JOIN post_reactions pr ON pr.post_id = p.id
		WHERE p.user_id = ?
		GROUP BY p.id, p.user_id, u.username, u.display_name, p.title, p.body, a.id, a.mime, a.size, p.created_at
		ORDER BY p.created_at DESC, p.id DESC
		LIMIT ? OFFSET ?
	`, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	posts := make([]domain.Post, 0)
	postIDs := make([]int64, 0)
	for rows.Next() {
		var (
			post              domain.Post
			createdAt         int64
			authorUsername    string
			authorDisplayName sql.NullString
			attachmentID      sql.NullInt64
			attachmentMime    sql.NullString
			attachmentSize    sql.NullInt64
		)
		if err := rows.Scan(&post.ID, &post.UserID, &authorUsername, &authorDisplayName, &post.Title, &post.Body,
			&attachmentID, &attachmentMime, &attachmentSize, &createdAt, &post.Likes, &post.Dislikes, &post.CommentsCount); err != nil {
			return nil, err
		}
		post.Author = domain.UserRef{
			ID:          post.UserID,
			Username:    authorUsername,
			DisplayName: strings.TrimSpace(authorDisplayName.String),
		}
		post.Attachment = attachmentFromNullableFields(attachmentID, attachmentMime, attachmentSize)
		post.CreatedAt = unixToTime(createdAt)
		posts = append(posts, post)
		postIDs = append(postIDs, post.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(posts) == 0 {
		return posts, nil
	}

	postRepo := NewPostRepo(r.db)
	categories, err := postRepo.fetchCategories(ctx, postIDs)
	if err != nil {
		return nil, err
	}
	for i := range posts {
		posts[i].Categories = categories[posts[i].ID]
	}
	return posts, nil
}

func (r *CenterRepo) ListActivityComments(ctx context.Context, userID int64, limit, offset int) ([]domain.ActivityComment, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT c.id, c.post_id, c.parent_id, c.user_id, u.username, u.display_name, c.body, c.created_at,
		       COALESCE(SUM(CASE WHEN cr.value = 1 THEN 1 ELSE 0 END), 0) AS likes,
		       COALESCE(SUM(CASE WHEN cr.value = -1 THEN 1 ELSE 0 END), 0) AS dislikes,
		       p.title
		FROM comments c
		JOIN users u ON u.id = c.user_id
		JOIN posts p ON p.id = c.post_id
		LEFT JOIN comment_reactions cr ON cr.comment_id = c.id
		WHERE c.user_id = ?
		GROUP BY c.id, c.post_id, c.parent_id, c.user_id, u.username, u.display_name, c.body, c.created_at, p.title
		ORDER BY c.created_at DESC, c.id DESC
		LIMIT ? OFFSET ?
	`, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	comments := make([]domain.ActivityComment, 0)
	for rows.Next() {
		var (
			item              domain.ActivityComment
			createdAt         int64
			parentID          sql.NullInt64
			authorUsername    string
			authorDisplayName sql.NullString
		)
		if err := rows.Scan(&item.Comment.ID, &item.Comment.PostID, &parentID, &item.Comment.UserID, &authorUsername, &authorDisplayName,
			&item.Comment.Body, &createdAt, &item.Comment.Likes, &item.Comment.Dislikes, &item.PostTitle); err != nil {
			return nil, err
		}
		if parentID.Valid {
			item.Comment.ParentID = &parentID.Int64
		}
		item.Comment.Author = domain.UserRef{
			ID:          item.Comment.UserID,
			Username:    authorUsername,
			DisplayName: strings.TrimSpace(authorDisplayName.String),
		}
		item.Comment.CreatedAt = unixToTime(createdAt)
		item.PostID = item.Comment.PostID
		comments = append(comments, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return comments, nil
}

func (r *CenterRepo) ListActivityReactions(ctx context.Context, userID int64, limit, offset int) ([]domain.ActivityReaction, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT target_type, target_id, value, created_at, post_id, post_title, post_preview,
		       comment_id, comment_preview, target_author_id, target_author_username, target_author_display_name
		FROM (
			SELECT
				'post' AS target_type,
				pr.post_id AS target_id,
				pr.value AS value,
				pr.created_at AS created_at,
				p.id AS post_id,
				p.title AS post_title,
				p.body AS post_preview,
				0 AS comment_id,
				'' AS comment_preview,
				u.id AS target_author_id,
				u.username AS target_author_username,
				u.display_name AS target_author_display_name
			FROM post_reactions pr
			JOIN posts p ON p.id = pr.post_id
			JOIN users u ON u.id = p.user_id
			WHERE pr.user_id = ?

			UNION ALL

			SELECT
				'comment' AS target_type,
				cr.comment_id AS target_id,
				cr.value AS value,
				cr.created_at AS created_at,
				p.id AS post_id,
				p.title AS post_title,
				p.body AS post_preview,
				c.id AS comment_id,
				c.body AS comment_preview,
				u.id AS target_author_id,
				u.username AS target_author_username,
				u.display_name AS target_author_display_name
			FROM comment_reactions cr
			JOIN comments c ON c.id = cr.comment_id
			JOIN posts p ON p.id = c.post_id
			JOIN users u ON u.id = c.user_id
			WHERE cr.user_id = ?
		) reactions
		ORDER BY created_at DESC, target_id DESC
		LIMIT ? OFFSET ?
	`, userID, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]domain.ActivityReaction, 0)
	for rows.Next() {
		var (
			item                    domain.ActivityReaction
			createdAt               int64
			commentID               int64
			targetAuthorDisplayName sql.NullString
		)
		if err := rows.Scan(&item.TargetType, &item.TargetID, &item.Value, &createdAt, &item.PostID, &item.PostTitle, &item.PostPreview,
			&commentID, &item.CommentPreview, &item.TargetAuthor.ID, &item.TargetAuthor.Username, &targetAuthorDisplayName); err != nil {
			return nil, err
		}
		item.TargetAuthor.DisplayName = strings.TrimSpace(targetAuthorDisplayName.String)
		item.CreatedAt = unixToTime(createdAt)
		if commentID > 0 {
			item.CommentID = commentID
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

type notificationScanner interface {
	Scan(dest ...any) error
}

func scanNotification(s notificationScanner) (*domain.Notification, error) {
	var (
		notification domain.Notification
		actorUserID  sql.NullInt64
		createdAt    int64
		readAt       sql.NullInt64
		payloadJSON  string
		isRead       int
	)
	if err := s.Scan(&notification.ID, &notification.UserID, &actorUserID, &notification.Bucket, &notification.Type,
		&notification.EntityType, &notification.EntityID, &notification.SecondaryEntityType, &notification.SecondaryEntityID,
		&payloadJSON, &isRead, &createdAt, &readAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, repo.ErrNotFound
		}
		return nil, err
	}
	if actorUserID.Valid {
		notification.ActorUserID = &actorUserID.Int64
	}
	if strings.TrimSpace(payloadJSON) != "" {
		if err := json.Unmarshal([]byte(payloadJSON), &notification.Payload); err != nil {
			return nil, err
		}
	}
	notification.IsRead = isRead != 0
	notification.CreatedAt = unixToTime(createdAt)
	if readAt.Valid {
		value := unixToTime(readAt.Int64)
		notification.ReadAt = &value
	}
	return &notification, nil
}

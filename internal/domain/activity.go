package domain

import "time"

type ActivityData struct {
	Posts            []Post             `json:"posts"`
	PostsHasMore     bool               `json:"postsHasMore"`
	Reactions        []ActivityReaction `json:"reactions"`
	ReactionsHasMore bool               `json:"reactionsHasMore"`
	Comments         []ActivityComment  `json:"comments"`
	CommentsHasMore  bool               `json:"commentsHasMore"`
}

type ActivityReaction struct {
	TargetType     string    `json:"targetType"`
	TargetID       int64     `json:"targetId"`
	Value          int       `json:"value"`
	CreatedAt      time.Time `json:"createdAt"`
	PostID         int64     `json:"postId"`
	PostTitle      string    `json:"postTitle"`
	PostPreview    string    `json:"postPreview,omitempty"`
	CommentID      int64     `json:"commentId,omitempty"`
	CommentPreview string    `json:"commentPreview,omitempty"`
	TargetAuthor   UserRef   `json:"targetAuthor"`
	LinkPath       string    `json:"linkPath,omitempty"`
}

type ActivityComment struct {
	Comment   Comment `json:"comment"`
	PostID    int64   `json:"postId"`
	PostTitle string  `json:"postTitle"`
	LinkPath  string  `json:"linkPath,omitempty"`
}

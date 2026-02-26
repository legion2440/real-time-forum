package domain

import "time"

type Comment struct {
	ID        int64     `json:"id"`
	PostID    int64     `json:"post_id"`
	ParentID  *int64    `json:"parent_id,omitempty"`
	UserID    int64     `json:"user_id"`
	Author    UserRef   `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	Likes     int       `json:"likes"`
	Dislikes  int       `json:"dislikes"`
}

type CommentFilter struct {
	Query string
}

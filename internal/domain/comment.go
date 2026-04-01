package domain

import "time"

type Comment struct {
	ID            int64      `json:"id"`
	PostID        int64      `json:"post_id"`
	ParentID      *int64     `json:"parent_id,omitempty"`
	UserID        int64      `json:"user_id"`
	Author        UserRef    `json:"author"`
	Body          string     `json:"body"`
	CreatedAt     time.Time  `json:"created_at"`
	DeletedAt     *time.Time `json:"deleted_at,omitempty"`
	Likes         int        `json:"likes"`
	Dislikes      int        `json:"dislikes"`
	DeletedBody   string     `json:"-"`
	DeletedByUserID *int64   `json:"-"`
	DeletedByRole UserRole   `json:"-"`
}

type CommentFilter struct {
	Query string
	ViewerRole UserRole
}

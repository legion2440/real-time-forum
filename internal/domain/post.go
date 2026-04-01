package domain

import "time"

type UserRef struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name,omitempty"`
	Role        UserRole `json:"role,omitempty"`
	Badges      []string `json:"badges,omitempty"`
}

type Post struct {
	ID            int64       `json:"id"`
	UserID        int64       `json:"user_id"`
	Author        UserRef     `json:"author"`
	Title         string      `json:"title"`
	Body          string      `json:"body"`
	Attachment    *Attachment `json:"attachment,omitempty"`
	CreatedAt     time.Time   `json:"created_at"`
	Categories    []Category  `json:"categories,omitempty"`
	Likes         int         `json:"likes"`
	Dislikes      int         `json:"dislikes"`
	CommentsCount int         `json:"comments_count"`
	Comments      []Comment   `json:"comments,omitempty"`
	UnderReview   bool        `json:"under_review"`
	ApprovedAt    *time.Time  `json:"approved_at,omitempty"`
	ApprovedBy    *UserRef    `json:"approved_by,omitempty"`
	DeleteProtected bool      `json:"delete_protected"`
	DeletedAt     *time.Time  `json:"deleted_at,omitempty"`
	DeletedBy     *UserRef    `json:"deleted_by,omitempty"`
	DeletedTitle  string      `json:"-"`
	DeletedBody   string      `json:"-"`
}

type PostFilter struct {
	CategoryIDs []int64
	Mine        bool
	Liked       bool
	UserID      *int64
	Query       string
	ViewerRole  UserRole
}

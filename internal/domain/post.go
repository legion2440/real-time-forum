package domain

import "time"

type UserRef struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type Post struct {
	ID            int64      `json:"id"`
	UserID        int64      `json:"user_id"`
	Author        UserRef    `json:"author"`
	Title         string     `json:"title"`
	Body          string     `json:"body"`
	CreatedAt     time.Time  `json:"created_at"`
	Categories    []Category `json:"categories,omitempty"`
	Likes         int        `json:"likes"`
	Dislikes      int        `json:"dislikes"`
	CommentsCount int        `json:"comments_count"`
	Comments      []Comment  `json:"comments,omitempty"`
}

type PostFilter struct {
	CategoryIDs []int64
	Mine        bool
	Liked       bool
	UserID      *int64
	Query       string
}

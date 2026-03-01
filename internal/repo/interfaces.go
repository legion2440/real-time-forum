package repo

import (
	"context"

	"forum/internal/domain"
)

type UserRepo interface {
	Create(ctx context.Context, user *domain.User) (int64, error)
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
	GetByUsername(ctx context.Context, username string) (*domain.User, error)
	GetByID(ctx context.Context, id int64) (*domain.User, error)
	List(ctx context.Context) ([]domain.User, error)
	ListPublic(ctx context.Context) ([]domain.User, error)
}

type SessionRepo interface {
	Create(ctx context.Context, session *domain.Session) error
	GetByToken(ctx context.Context, token string) (*domain.Session, error)
	DeleteByToken(ctx context.Context, token string) error
	DeleteByUserID(ctx context.Context, userID int64) error
}

type PostRepo interface {
	Create(ctx context.Context, post *domain.Post, categoryIDs []int64) (int64, error)
	List(ctx context.Context, filter domain.PostFilter) ([]domain.Post, error)
	GetByID(ctx context.Context, id int64) (*domain.Post, error)
	Exists(ctx context.Context, id int64) (bool, error)
}

type CommentRepo interface {
	Create(ctx context.Context, comment *domain.Comment) (int64, error)
	ListByPost(ctx context.Context, postID int64, filter domain.CommentFilter) ([]domain.Comment, error)
	GetByID(ctx context.Context, id int64) (*domain.Comment, error)
}

type CategoryRepo interface {
	List(ctx context.Context) ([]domain.Category, error)
}

type ReactionRepo interface {
	ReactPost(ctx context.Context, postID, userID int64, value int) error
	ReactComment(ctx context.Context, commentID, userID int64, value int) error
}

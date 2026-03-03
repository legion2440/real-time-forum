package service

import (
	"context"
	"errors"
	"strings"

	"forum/internal/domain"
	"forum/internal/platform/clock"
	"forum/internal/repo"
)

type PostService struct {
	posts       repo.PostRepo
	comments    repo.CommentRepo
	categories  repo.CategoryRepo
	reactions   repo.ReactionRepo
	attachments *AttachmentService
	clock       clock.Clock
}

func NewPostService(posts repo.PostRepo, comments repo.CommentRepo, categories repo.CategoryRepo, reactions repo.ReactionRepo, attachments *AttachmentService, clock clock.Clock) *PostService {
	return &PostService{
		posts:       posts,
		comments:    comments,
		categories:  categories,
		reactions:   reactions,
		attachments: attachments,
		clock:       clock,
	}
}

func (s *PostService) ListPosts(ctx context.Context, filter domain.PostFilter) ([]domain.Post, error) {
	filter.Query = strings.TrimSpace(filter.Query)
	if filter.UserID == nil {
		filter.Mine = false
		filter.Liked = false
	}
	return s.posts.List(ctx, filter)
}

func (s *PostService) GetPost(ctx context.Context, id int64) (*domain.Post, error) {
	post, err := s.posts.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	comments, err := s.comments.ListByPost(ctx, id, domain.CommentFilter{})
	if err != nil {
		return nil, err
	}
	post.Comments = comments
	post.CommentsCount = len(comments)
	return post, nil
}

func (s *PostService) ListComments(ctx context.Context, postID int64, filter domain.CommentFilter) ([]domain.Comment, error) {
	exists, err := s.posts.Exists(ctx, postID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}

	filter.Query = strings.TrimSpace(filter.Query)
	return s.comments.ListByPost(ctx, postID, filter)
}

func (s *PostService) CreatePost(ctx context.Context, userID int64, title, body string, categoryIDs []int64, attachmentID *int64) (*domain.Post, error) {
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if title == "" || body == "" {
		return nil, ErrInvalidInput
	}
	if len(categoryIDs) == 0 {
		return nil, ErrInvalidInput
	}
	if attachmentID != nil && s.attachments == nil {
		return nil, ErrInvalidInput
	}

	var attachment *domain.Attachment
	if attachmentID != nil {
		var err error
		attachment, err = s.attachments.GetOwnedAttachment(ctx, userID, attachmentID)
		if err != nil {
			return nil, err
		}
	}

	post := &domain.Post{
		UserID:     userID,
		Title:      title,
		Body:       body,
		Attachment: attachment,
		CreatedAt:  s.clock.Now(),
	}

	id, err := s.posts.Create(ctx, post, categoryIDs)
	if err != nil {
		return nil, err
	}

	return s.posts.GetByID(ctx, id)
}

func (s *PostService) CreateComment(ctx context.Context, userID, postID int64, body string, parentID *int64) (*domain.Comment, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, ErrInvalidInput
	}

	exists, err := s.posts.Exists(ctx, postID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}

	if parentID != nil {
		if *parentID <= 0 {
			return nil, ErrInvalidInput
		}
		parent, err := s.comments.GetByID(ctx, *parentID)
		if err != nil {
			if errors.Is(err, repo.ErrNotFound) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		if parent.PostID != postID {
			return nil, ErrInvalidInput
		}
		// Depth is limited to one nested level. If user replies to a reply,
		// normalize it to the root comment instead of failing.
		if parent.ParentID != nil {
			rootID := *parent.ParentID
			if rootID <= 0 {
				return nil, ErrInvalidInput
			}
			root, err := s.comments.GetByID(ctx, rootID)
			if err != nil {
				if errors.Is(err, repo.ErrNotFound) {
					return nil, ErrNotFound
				}
				return nil, err
			}
			if root.PostID != postID {
				return nil, ErrInvalidInput
			}
			parentID = &rootID
		}
	}

	comment := &domain.Comment{
		PostID:    postID,
		ParentID:  parentID,
		UserID:    userID,
		Body:      body,
		CreatedAt: s.clock.Now(),
	}

	id, err := s.comments.Create(ctx, comment)
	if err != nil {
		return nil, err
	}
	return s.comments.GetByID(ctx, id)
}

func (s *PostService) ReactPost(ctx context.Context, userID, postID int64, value int) error {
	if value != -1 && value != 0 && value != 1 {
		return ErrInvalidInput
	}

	exists, err := s.posts.Exists(ctx, postID)
	if err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}

	return s.reactions.ReactPost(ctx, postID, userID, value)
}

func (s *PostService) ReactComment(ctx context.Context, userID, commentID int64, value int) error {
	if value != -1 && value != 0 && value != 1 {
		return ErrInvalidInput
	}

	if _, err := s.comments.GetByID(ctx, commentID); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}

	return s.reactions.ReactComment(ctx, commentID, userID, value)
}

func (s *PostService) ListCategories(ctx context.Context) ([]domain.Category, error) {
	return s.categories.List(ctx)
}

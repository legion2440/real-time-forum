package service

import (
	"context"
	"errors"
	"strings"
	"time"

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
	center      *CenterService
}

func NewPostService(posts repo.PostRepo, comments repo.CommentRepo, categories repo.CategoryRepo, reactions repo.ReactionRepo, attachments *AttachmentService, clock clock.Clock, deps ...any) *PostService {
	service := &PostService{
		posts:       posts,
		comments:    comments,
		categories:  categories,
		reactions:   reactions,
		attachments: attachments,
		clock:       clock,
	}
	for _, dependency := range deps {
		if center, ok := dependency.(*CenterService); ok && center != nil {
			service.center = center
		}
	}
	return service
}

func (s *PostService) ListPosts(ctx context.Context, filter domain.PostFilter) ([]domain.Post, error) {
	filter.Query = strings.TrimSpace(filter.Query)
	if filter.UserID == nil {
		filter.Mine = false
		filter.Liked = false
	}
	posts, err := s.posts.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	for i := range posts {
		maskDeletedPost(&posts[i], filter.ViewerRole)
	}
	return posts, nil
}

func (s *PostService) GetPost(ctx context.Context, id int64, viewerRole domain.UserRole) (*domain.Post, error) {
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
	maskDeletedPost(post, viewerRole)
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

	createdPost, err := s.posts.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.center != nil {
		if err := s.center.HandlePostCreated(ctx, createdPost); err != nil {
			return nil, err
		}
	}
	return createdPost, nil
}

func (s *PostService) CreateComment(ctx context.Context, userID, postID int64, body string, parentID *int64) (*domain.Comment, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, ErrInvalidInput
	}

	post, err := s.posts.GetByID(ctx, postID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if post.DeletedAt != nil {
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
		if isDeletedComment(parent) {
			return nil, ErrNotFound
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
			if isDeletedComment(root) {
				return nil, ErrNotFound
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
	createdComment, err := s.comments.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.center != nil {
		if err := s.center.HandleCommentCreated(ctx, createdComment); err != nil {
			return nil, err
		}
	}
	return createdComment, nil
}

func (s *PostService) ReactPost(ctx context.Context, userID, postID int64, value int) (domain.ReactionChange, error) {
	if value != -1 && value != 0 && value != 1 {
		return domain.ReactionChange{}, ErrInvalidInput
	}

	post, err := s.posts.GetByID(ctx, postID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return domain.ReactionChange{}, ErrNotFound
		}
		return domain.ReactionChange{}, err
	}
	if post.DeletedAt != nil {
		return domain.ReactionChange{}, ErrNotFound
	}

	change, err := s.reactions.ReactPost(ctx, postID, userID, value, s.clock.Now())
	if err != nil {
		return domain.ReactionChange{}, err
	}
	if s.center != nil {
		if err := s.center.HandlePostReaction(ctx, userID, postID, change); err != nil {
			return domain.ReactionChange{}, err
		}
	}
	return change, nil
}

func (s *PostService) ReactComment(ctx context.Context, userID, commentID int64, value int) (domain.ReactionChange, error) {
	if value != -1 && value != 0 && value != 1 {
		return domain.ReactionChange{}, ErrInvalidInput
	}

	comment, err := s.comments.GetByID(ctx, commentID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return domain.ReactionChange{}, ErrNotFound
		}
		return domain.ReactionChange{}, err
	}
	if isDeletedComment(comment) {
		return domain.ReactionChange{}, ErrNotFound
	}

	change, err := s.reactions.ReactComment(ctx, commentID, userID, value, s.clock.Now())
	if err != nil {
		return domain.ReactionChange{}, err
	}
	if s.center != nil {
		if err := s.center.HandleCommentReaction(ctx, userID, commentID, change); err != nil {
			return domain.ReactionChange{}, err
		}
	}
	return change, nil
}

func (s *PostService) ListCategories(ctx context.Context) ([]domain.Category, error) {
	return s.categories.List(ctx)
}

func (s *PostService) UpdatePost(ctx context.Context, userID, postID int64, title, body string, categoryIDs []int64) (*domain.Post, error) {
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if userID <= 0 || postID <= 0 || title == "" || body == "" || len(categoryIDs) == 0 {
		return nil, ErrInvalidInput
	}

	post, err := s.posts.GetByID(ctx, postID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if post.UserID != userID {
		return nil, ErrForbidden
	}
	if post.DeletedAt != nil {
		return nil, ErrNotFound
	}

	post.Title = title
	post.Body = body
	if err := s.posts.Update(ctx, post, categoryIDs); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s.posts.GetByID(ctx, postID)
}

func (s *PostService) DeletePost(ctx context.Context, userID, postID int64) error {
	if userID <= 0 || postID <= 0 {
		return ErrInvalidInput
	}

	post, err := s.posts.GetByID(ctx, postID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if post.UserID != userID {
		return ErrForbidden
	}
	if post.DeletedAt != nil {
		return ErrNotFound
	}
	if err := s.posts.Delete(ctx, postID); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

func (s *PostService) UpdateComment(ctx context.Context, userID, commentID int64, body string) (*domain.Comment, error) {
	body = strings.TrimSpace(body)
	if userID <= 0 || commentID <= 0 || body == "" {
		return nil, ErrInvalidInput
	}

	comment, err := s.comments.GetByID(ctx, commentID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if isDeletedComment(comment) {
		return nil, ErrNotFound
	}
	if comment.UserID != userID {
		return nil, ErrForbidden
	}
	if s.clock.Now().UTC().Sub(comment.CreatedAt.UTC()) > 30*time.Minute {
		return nil, ErrCommentEditWindowExpired
	}

	comment.Body = body
	if err := s.comments.Update(ctx, comment); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s.comments.GetByID(ctx, commentID)
}

func (s *PostService) DeleteComment(ctx context.Context, userID, commentID int64) error {
	if userID <= 0 || commentID <= 0 {
		return ErrInvalidInput
	}

	comment, err := s.comments.GetByID(ctx, commentID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if isDeletedComment(comment) {
		return ErrNotFound
	}
	if comment.UserID != userID {
		return ErrForbidden
	}
	hasDescendants, err := s.comments.HasDescendants(ctx, commentID)
	if err != nil {
		return err
	}
	if hasDescendants {
		if err := s.comments.SoftDelete(ctx, commentID, s.clock.Now(), userID, domain.RoleUser); err != nil {
			if errors.Is(err, repo.ErrNotFound) {
				return ErrNotFound
			}
			return err
		}
		if err := s.cleanupDeletedCommentThread(ctx, comment); err != nil {
			return err
		}
		return nil
	}
	if err := s.comments.Delete(ctx, commentID); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if err := s.cleanupDeletedCommentAncestors(ctx, comment.ParentID); err != nil {
		return err
	}
	return nil
}

func (s *PostService) cleanupDeletedCommentAncestors(ctx context.Context, parentID *int64) error {
	currentParentID := parentID
	for currentParentID != nil && *currentParentID > 0 {
		comment, err := s.comments.GetByID(ctx, *currentParentID)
		if err != nil {
			if errors.Is(err, repo.ErrNotFound) {
				return nil
			}
			return err
		}
		if !isDeletedComment(comment) {
			return nil
		}
		hasDescendants, err := s.comments.HasDescendants(ctx, comment.ID)
		if err != nil {
			return err
		}
		if hasDescendants {
			return nil
		}
		nextParentID := comment.ParentID
		if err := s.comments.Delete(ctx, comment.ID); err != nil {
			if errors.Is(err, repo.ErrNotFound) {
				return nil
			}
			return err
		}
		currentParentID = nextParentID
	}
	return nil
}

func (s *PostService) cleanupDeletedCommentThread(ctx context.Context, comment *domain.Comment) error {
	rootID, err := s.findThreadRootCommentID(ctx, comment)
	if err != nil || rootID <= 0 {
		return err
	}
	root, err := s.comments.GetByID(ctx, rootID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil
		}
		return err
	}
	if !isDeletedComment(root) {
		return nil
	}
	hasActiveComments, err := s.comments.HasActiveThreadComments(ctx, rootID)
	if err != nil {
		return err
	}
	if hasActiveComments {
		return nil
	}
	if err := s.comments.Delete(ctx, rootID); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil
		}
		return err
	}
	return nil
}

func (s *PostService) findThreadRootCommentID(ctx context.Context, comment *domain.Comment) (int64, error) {
	if comment == nil {
		return 0, nil
	}
	current := comment
	for current.ParentID != nil && *current.ParentID > 0 {
		parent, err := s.comments.GetByID(ctx, *current.ParentID)
		if err != nil {
			if errors.Is(err, repo.ErrNotFound) {
				return current.ID, nil
			}
			return 0, err
		}
		current = parent
	}
	return current.ID, nil
}

func isDeletedComment(comment *domain.Comment) bool {
	return comment != nil && comment.DeletedAt != nil
}

func maskDeletedPost(post *domain.Post, viewerRole domain.UserRole) {
	if post == nil || post.DeletedAt == nil {
		return
	}
	switch viewerRole {
	case domain.RoleAdmin, domain.RoleOwner:
		return
	default:
		post.Title = "[deleted]"
		post.Body = ""
		post.Attachment = nil
		post.UnderReview = false
	}
}

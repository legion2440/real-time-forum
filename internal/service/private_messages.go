package service

import (
	"context"
	"errors"
	"strings"

	"forum/internal/domain"
	"forum/internal/platform/clock"
	"forum/internal/repo"
)

type PrivateMessageService struct {
	users       repo.UserRepo
	messages    repo.PrivateMessageRepo
	attachments *AttachmentService
	clock       clock.Clock
	center      *CenterService
}

func NewPrivateMessageService(users repo.UserRepo, messages repo.PrivateMessageRepo, attachments *AttachmentService, clock clock.Clock, deps ...any) *PrivateMessageService {
	service := &PrivateMessageService{
		users:       users,
		messages:    messages,
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

func (s *PrivateMessageService) Send(ctx context.Context, fromID, toID int64, body string, attachmentID *int64) (*domain.PrivateMessage, error) {
	body = strings.TrimSpace(body)
	if fromID <= 0 || toID <= 0 {
		return nil, ErrInvalidInput
	}
	if fromID == toID {
		return nil, ErrInvalidInput
	}
	if attachmentID != nil && s.attachments == nil {
		return nil, ErrInvalidInput
	}

	var attachment *domain.Attachment
	if attachmentID != nil {
		var err error
		attachment, err = s.attachments.GetOwnedAttachment(ctx, fromID, attachmentID)
		if err != nil {
			return nil, err
		}
	}
	if body == "" && attachment == nil {
		return nil, ErrInvalidInput
	}

	if _, err := s.users.GetByID(ctx, toID); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	message, err := s.messages.SavePrivateMessage(ctx, fromID, toID, body, attachment, s.clock.Now())
	if err != nil {
		return nil, err
	}
	if s.center != nil {
		if err := s.center.HandlePrivateMessage(ctx, message); err != nil {
			return nil, err
		}
	}
	return message, nil
}

func (s *PrivateMessageService) ListConversationLast(ctx context.Context, userA, userB int64, limit int) ([]domain.PrivateMessage, error) {
	if userA <= 0 || userB <= 0 || limit <= 0 || userA == userB {
		return nil, ErrInvalidInput
	}

	if _, err := s.users.GetByID(ctx, userB); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return s.messages.ListConversationLast(ctx, userA, userB, limit)
}

func (s *PrivateMessageService) ListConversationBefore(ctx context.Context, userA, userB, beforeTs, beforeID int64, limit int) ([]domain.PrivateMessage, error) {
	if userA <= 0 || userB <= 0 || beforeTs <= 0 || beforeID <= 0 || limit <= 0 || userA == userB {
		return nil, ErrInvalidInput
	}

	if _, err := s.users.GetByID(ctx, userB); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return s.messages.ListConversationBefore(ctx, userA, userB, beforeTs, beforeID, limit)
}

func (s *PrivateMessageService) ListPeers(ctx context.Context, userID int64) ([]domain.PrivateMessagePeer, error) {
	if userID <= 0 {
		return nil, ErrInvalidInput
	}
	return s.messages.ListPeers(ctx, userID)
}

func (s *PrivateMessageService) MarkRead(ctx context.Context, userID, peerID, lastReadMessageID int64) error {
	if userID <= 0 || peerID <= 0 || userID == peerID || lastReadMessageID < 0 {
		return ErrInvalidInput
	}

	if _, err := s.users.GetByID(ctx, peerID); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}

	if lastReadMessageID > 0 {
		exists, err := s.messages.ConversationHasMessage(ctx, userID, peerID, lastReadMessageID)
		if err != nil {
			return err
		}
		if !exists {
			return ErrInvalidInput
		}
	}

	return s.messages.MarkRead(ctx, userID, peerID, lastReadMessageID, s.clock.Now())
}

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
	users    repo.UserRepo
	messages repo.PrivateMessageRepo
	clock    clock.Clock
}

func NewPrivateMessageService(users repo.UserRepo, messages repo.PrivateMessageRepo, clock clock.Clock) *PrivateMessageService {
	return &PrivateMessageService{
		users:    users,
		messages: messages,
		clock:    clock,
	}
}

func (s *PrivateMessageService) Send(ctx context.Context, fromID, toID int64, body string) (*domain.PrivateMessage, error) {
	body = strings.TrimSpace(body)
	if fromID <= 0 || toID <= 0 || body == "" {
		return nil, ErrInvalidInput
	}
	if fromID == toID {
		return nil, ErrInvalidInput
	}

	if _, err := s.users.GetByID(ctx, toID); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return s.messages.SavePrivateMessage(ctx, fromID, toID, body, s.clock.Now())
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

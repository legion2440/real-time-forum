package http

import (
	"net/http"

	"forum/internal/domain"
)

func (h *Handler) currentActor(r *http.Request) (*domain.User, bool, error) {
	if h == nil || h.auth == nil || r == nil {
		return nil, false, nil
	}
	userID, ok := userIDFromContext(r.Context())
	if !ok || userID <= 0 {
		return nil, false, nil
	}
	user, err := h.auth.GetUserByID(r.Context(), userID)
	if err != nil {
		return nil, false, err
	}
	return user, true, nil
}

func (h *Handler) viewerRole(r *http.Request) domain.UserRole {
	user, ok, err := h.currentActor(r)
	if err != nil || !ok || user == nil {
		return domain.RoleGuest
	}
	return user.Role
}

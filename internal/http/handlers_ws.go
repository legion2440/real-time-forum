package http

import (
	"net/http"
	"strings"

	realtimews "forum/internal/realtime/ws"
)

func (h *Handler) handleWS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}

	user, err := h.auth.GetUserByID(r.Context(), userID)
	if handleServiceError(w, err) {
		return
	}

	name := strings.TrimSpace(user.DisplayName)
	if name == "" {
		name = strings.TrimSpace(user.Username)
	}

	if err := realtimews.ServeWS(w, r, h.hub, h.pms, realtimews.User{
		ID:   user.ID,
		Name: name,
	}); err != nil {
		return
	}
}

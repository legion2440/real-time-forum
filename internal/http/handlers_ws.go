package http

import (
	"net/http"

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

	if err := realtimews.ServeWS(w, r, h.hub, realtimews.User{
		ID:   user.ID,
		Name: user.Username,
	}); err != nil {
		return
	}
}

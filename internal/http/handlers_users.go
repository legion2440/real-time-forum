package http

import (
	"net/http"
	"strconv"
	"strings"
)

type userListItem struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
}

func (h *Handler) handleUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if _, ok := userIDFromContext(r.Context()); !ok {
		writeAuthUnauthorized(w, r)
		return
	}

	users, err := h.auth.ListUsersPublic(r.Context())
	if handleServiceError(w, err) {
		return
	}

	response := make([]userListItem, 0, len(users))
	for _, user := range users {
		name := strings.TrimSpace(user.DisplayName)
		if name == "" {
			name = strings.TrimSpace(user.Username)
		}
		response = append(response, userListItem{
			ID:       strconv.FormatInt(user.ID, 10),
			Username: user.Username,
			Name:     name,
		})
	}

	writeJSON(w, http.StatusOK, response)
}

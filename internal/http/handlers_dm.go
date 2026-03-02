package http

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"forum/internal/domain"
)

type privateMessageUserDTO struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type privateMessageDTO struct {
	ID        string                `json:"id"`
	From      privateMessageUserDTO `json:"from"`
	To        privateMessageUserDTO `json:"to"`
	Body      string                `json:"body"`
	CreatedAt time.Time             `json:"createdAt"`
}

func (h *Handler) handleDMConversation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}

	prefix := "/api/dm/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	peerRaw := strings.Trim(strings.TrimPrefix(r.URL.Path, prefix), "/")
	if peerRaw == "" || strings.Contains(peerRaw, "/") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	peerID, err := strconv.ParseInt(peerRaw, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}

	limit := 10
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		limit = parsed
	}

	messages, err := h.pms.ListConversationLast(r.Context(), userID, peerID, limit)
	if handleServiceError(w, err) {
		return
	}

	response := make([]privateMessageDTO, 0, len(messages))
	for i := len(messages) - 1; i >= 0; i-- {
		response = append(response, newPrivateMessageDTO(messages[i]))
	}

	writeJSON(w, http.StatusOK, response)
}

func newPrivateMessageDTO(msg domain.PrivateMessage) privateMessageDTO {
	name := strings.TrimSpace(msg.FromDisplayName)
	if name == "" {
		name = strings.TrimSpace(msg.FromUsername)
	}
	return privateMessageDTO{
		ID: strconv.FormatInt(msg.ID, 10),
		From: privateMessageUserDTO{
			ID:   strconv.FormatInt(msg.FromUserID, 10),
			Name: name,
		},
		To: privateMessageUserDTO{
			ID: strconv.FormatInt(msg.ToUserID, 10),
		},
		Body:      msg.Body,
		CreatedAt: msg.CreatedAt.UTC(),
	}
}

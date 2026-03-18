package http

import (
	"encoding/json"
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
	ID         string                 `json:"id"`
	From       privateMessageUserDTO  `json:"from"`
	To         privateMessageUserDTO  `json:"to"`
	Body       string                 `json:"body"`
	Attachment *attachmentResponseDTO `json:"attachment,omitempty"`
	CreatedAt  time.Time              `json:"createdAt"`
}

type privateMessagePeerDTO struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	DisplayName   string `json:"displayName"`
	LastMessageAt int64  `json:"lastMessageAt"`
	UnreadCount   int    `json:"unreadCount"`
}

type markDMReadRequest struct {
	LastReadMessageID json.RawMessage `json:"lastReadMessageId"`
}

func (h *Handler) handleDMPeers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}

	peers, err := h.pms.ListPeers(r.Context(), userID)
	if handleServiceError(w, err) {
		return
	}

	response := make([]privateMessagePeerDTO, 0, len(peers))
	for _, peer := range peers {
		response = append(response, privateMessagePeerDTO{
			ID:            strconv.FormatInt(peer.ID, 10),
			Username:      strings.TrimSpace(peer.Username),
			DisplayName:   strings.TrimSpace(peer.DisplayName),
			LastMessageAt: peer.LastMessageAt,
			UnreadCount:   peer.UnreadCount,
		})
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handleDMConversation(w http.ResponseWriter, r *http.Request) {
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

	pathParts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, prefix), "/"), "/")
	if len(pathParts) == 0 || len(pathParts) > 2 || strings.TrimSpace(pathParts[0]) == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	peerID, err := strconv.ParseInt(strings.TrimSpace(pathParts[0]), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}
	action := ""
	if len(pathParts) == 2 {
		action = strings.TrimSpace(pathParts[1])
	}

	switch {
	case action == "" && r.Method == http.MethodGet:
		h.handleDMConversationGet(w, r, userID, peerID)
	case action == "read" && r.Method == http.MethodPost:
		h.handleDMMarkRead(w, r, userID, peerID)
	case action == "":
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	case action == "read":
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (h *Handler) handleDMConversationGet(w http.ResponseWriter, r *http.Request, userID, peerID int64) {
	limit := 10
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		limit = parsed
	}

	rawBeforeTs := strings.TrimSpace(r.URL.Query().Get("beforeTs"))
	rawBeforeID := strings.TrimSpace(r.URL.Query().Get("beforeID"))
	hasBeforeTs := rawBeforeTs != ""
	hasBeforeID := rawBeforeID != ""
	if hasBeforeTs != hasBeforeID {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}

	var (
		messages []domain.PrivateMessage
		err      error
	)
	if hasBeforeTs {
		beforeTs, err := strconv.ParseInt(rawBeforeTs, 10, 64)
		if err != nil || beforeTs <= 0 {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		beforeID, err := strconv.ParseInt(rawBeforeID, 10, 64)
		if err != nil || beforeID <= 0 {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		messages, err = h.pms.ListConversationBefore(r.Context(), userID, peerID, beforeTs, beforeID, limit)
		if handleServiceError(w, err) {
			return
		}
	} else {
		messages, err = h.pms.ListConversationLast(r.Context(), userID, peerID, limit)
		if handleServiceError(w, err) {
			return
		}
	}

	response := make([]privateMessageDTO, 0, len(messages))
	for i := len(messages) - 1; i >= 0; i-- {
		response = append(response, newPrivateMessageDTO(messages[i]))
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handleDMMarkRead(w http.ResponseWriter, r *http.Request, userID, peerID int64) {
	var req markDMReadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}

	lastReadMessageID, err := parseFlexibleInt64(req.LastReadMessageID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}

	if handleServiceError(w, h.pms.MarkRead(r.Context(), userID, peerID, lastReadMessageID)) {
		return
	}
	if h.center != nil {
		if _, err := h.center.MarkDMConversationNotificationsRead(r.Context(), userID, peerID, lastReadMessageID); handleServiceError(w, err) {
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

func parseFlexibleInt64(raw json.RawMessage) (int64, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return 0, strconv.ErrSyntax
	}
	if strings.HasPrefix(trimmed, `"`) {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return 0, err
		}
		return strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	}
	return strconv.ParseInt(trimmed, 10, 64)
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
		Body:       msg.Body,
		Attachment: newAttachmentResponseDTO(msg.Attachment),
		CreatedAt:  msg.CreatedAt.UTC(),
	}
}

package http

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"forum/internal/domain"
)

type markAllNotificationsReadRequest struct {
	Bucket string `json:"bucket"`
}

const defaultCenterPageSize = 20

func (h *Handler) handleCenterSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.center == nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}

	summary, err := h.center.GetUnreadSummary(r.Context(), userID)
	if handleServiceError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (h *Handler) handleCenterActivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.center == nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}

	limit, err := parseIntQuery(r, "limit", defaultCenterPageSize)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}
	postsOffset, err := parseIntQuery(r, "postsOffset", 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}
	reactionsOffset, err := parseIntQuery(r, "reactionsOffset", 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}
	commentsOffset, err := parseIntQuery(r, "commentsOffset", 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}

	activity, err := h.center.ListActivity(r.Context(), userID, limit, postsOffset, reactionsOffset, commentsOffset)
	if handleServiceError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, activity)
}

func (h *Handler) handleCenterNotifications(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.center == nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}

	limit, err := parseIntQuery(r, "limit", defaultCenterPageSize)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}
	offset, err := parseIntQuery(r, "offset", 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}

	notifications, err := h.center.ListNotifications(r.Context(), userID, domain.NotificationFilter{
		Bucket: strings.TrimSpace(r.URL.Query().Get("bucket")),
		Limit:  limit,
		Offset: offset,
	})
	if handleServiceError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, notifications)
}

func (h *Handler) handleCenterNotificationSubroutes(w http.ResponseWriter, r *http.Request) {
	if h.center == nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	const prefix = "/api/center/notifications/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, prefix), "/")
	if rest == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	if rest == "read-all" {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.handleCenterNotificationsReadAll(w, r)
		return
	}

	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[1] != "read" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	notificationID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || notificationID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}
	h.handleCenterNotificationRead(w, r, notificationID)
}

func (h *Handler) handleCenterNotificationRead(w http.ResponseWriter, r *http.Request, notificationID int64) {
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}

	item, summary, err := h.center.MarkNotificationRead(r.Context(), userID, notificationID)
	if handleServiceError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"notification": item,
		"summary":      summary,
	})
}

func (h *Handler) handleCenterNotificationsReadAll(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}

	var req markAllNotificationsReadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}

	summary, err := h.center.MarkAllNotificationsRead(r.Context(), userID, req.Bucket)
	if handleServiceError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"summary": summary})
}

func parseIntQuery(r *http.Request, key string, defaultValue int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, strconv.ErrSyntax
	}
	return value, nil
}

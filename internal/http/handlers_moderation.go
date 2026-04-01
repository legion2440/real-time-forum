package http

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"forum/internal/domain"
)

type roleRequestCreateRequest struct {
	RequestedRole string `json:"requestedRole"`
	Note          string `json:"note"`
}

type reviewDecisionRequest struct {
	Approve bool   `json:"approve"`
	Note    string `json:"note"`
}

type reportCreateRequest struct {
	TargetType string `json:"targetType"`
	TargetID   int64  `json:"targetId"`
	Reason     string `json:"reason"`
	Note       string `json:"note"`
}

type reportDecisionRequest struct {
	ActionTaken bool   `json:"actionTaken"`
	Reason      string `json:"reason"`
	Note        string `json:"note"`
}

type appealCreateRequest struct {
	TargetType string `json:"targetType"`
	TargetID   int64  `json:"targetId"`
	Note       string `json:"note"`
}

type appealDecisionRequest struct {
	Reverse bool   `json:"reverse"`
	Note    string `json:"note"`
}

type contentModerationRequest struct {
	Reason string `json:"reason"`
	Note   string `json:"note"`
}

type protectRequest struct {
	Protected bool   `json:"protected"`
	Note      string `json:"note"`
}

type categoryCreateRequest struct {
	Name string `json:"name"`
}

type categoryDeleteRequest struct {
	Note string `json:"note"`
}

type roleChangeRequest struct {
	Role string `json:"role"`
	Note string `json:"note"`
}

type historyPurgeRequest struct {
	ActionType string `json:"actionType"`
	TargetType string `json:"targetType"`
	Status     string `json:"status"`
	From       string `json:"from"`
	To         string `json:"to"`
	Note       string `json:"note"`
}

func (h *Handler) handleModerationQueue(w http.ResponseWriter, r *http.Request) {
	if h.moderation == nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}
	posts, err := h.moderation.ListUnderReviewPosts(r.Context(), userID)
	if handleServiceError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, posts)
}

func (h *Handler) handleModerationRequests(w http.ResponseWriter, r *http.Request) {
	if h.moderation == nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		requestedRole, err := parseUserRole(r.URL.Query().Get("requestedRole"))
		if err != nil && strings.TrimSpace(r.URL.Query().Get("requestedRole")) != "" {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		status := domain.RoleRequestStatus(strings.TrimSpace(r.URL.Query().Get("status")))
		items, err := h.moderation.ListRoleRequests(r.Context(), userID, requestedRole, status)
		if handleServiceError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var req roleRequestCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		role, err := parseUserRole(req.RequestedRole)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		item, err := h.moderation.RequestRole(r.Context(), userID, role, req.Note)
		if handleServiceError(w, err) {
			return
		}
		writeJSON(w, http.StatusCreated, item)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleModerationRequestSubroutes(w http.ResponseWriter, r *http.Request) {
	if h.moderation == nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	const prefix = "/api/moderation/requests/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, prefix), "/"), "/")
	if len(parts) != 2 || parts[1] != "review" || r.Method != http.MethodPost {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	requestID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || requestID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}
	var req reviewDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}
	item, err := h.moderation.ReviewRoleRequest(r.Context(), userID, requestID, req.Approve, req.Note)
	if handleServiceError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) handleModerationReports(w http.ResponseWriter, r *http.Request) {
	if h.moderation == nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		status := domain.ModerationStatus(strings.TrimSpace(r.URL.Query().Get("status")))
		items, err := h.moderation.ListReports(r.Context(), userID, parseBoolQuery(r.URL.Query().Get("mine")), status)
		if handleServiceError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var req reportCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		reason, err := parseModerationReason(req.Reason)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		item, err := h.moderation.CreateReport(r.Context(), userID, strings.TrimSpace(req.TargetType), req.TargetID, reason, req.Note)
		if handleServiceError(w, err) {
			return
		}
		writeJSON(w, http.StatusCreated, item)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleModerationReportSubroutes(w http.ResponseWriter, r *http.Request) {
	if h.moderation == nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	const prefix = "/api/moderation/reports/"
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, prefix), "/"), "/")
	if len(parts) != 2 || parts[1] != "review" || r.Method != http.MethodPost {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	reportID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || reportID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}
	var req reportDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}
	reason, err := parseModerationReason(req.Reason)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}
	item, err := h.moderation.CloseReport(r.Context(), userID, reportID, req.ActionTaken, reason, req.Note)
	if handleServiceError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) handleModerationAppeals(w http.ResponseWriter, r *http.Request) {
	if h.moderation == nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		status := domain.AppealStatus(strings.TrimSpace(r.URL.Query().Get("status")))
		items, err := h.moderation.ListAppeals(r.Context(), userID, parseBoolQuery(r.URL.Query().Get("mine")), status)
		if handleServiceError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var req appealCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		item, err := h.moderation.CreateAppeal(r.Context(), userID, strings.TrimSpace(req.TargetType), req.TargetID, req.Note)
		if handleServiceError(w, err) {
			return
		}
		writeJSON(w, http.StatusCreated, item)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleModerationAppealSubroutes(w http.ResponseWriter, r *http.Request) {
	if h.moderation == nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	const prefix = "/api/moderation/appeals/"
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, prefix), "/"), "/")
	if len(parts) != 2 || parts[1] != "review" || r.Method != http.MethodPost {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	appealID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || appealID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}
	var req appealDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}
	item, err := h.moderation.CloseAppeal(r.Context(), userID, appealID, req.Reverse, req.Note)
	if handleServiceError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) handleModerationPostSubroutes(w http.ResponseWriter, r *http.Request) {
	h.handleModerationContentSubroutes(w, r, "/api/moderation/posts/", domain.ModerationTargetPost)
}

func (h *Handler) handleModerationCommentSubroutes(w http.ResponseWriter, r *http.Request) {
	h.handleModerationContentSubroutes(w, r, "/api/moderation/comments/", domain.ModerationTargetComment)
}

func (h *Handler) handleModerationContentSubroutes(w http.ResponseWriter, r *http.Request, prefix, targetType string) {
	if h.moderation == nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, prefix), "/"), "/")
	if len(parts) != 2 || r.Method != http.MethodPost {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	targetID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || targetID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}
	switch parts[1] {
	case "approve":
		if targetType != domain.ModerationTargetPost {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		var req struct {
			Categories []int64 `json:"categories"`
			Note       string  `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		post, err := h.moderation.ApprovePost(r.Context(), userID, targetID, req.Categories, req.Note)
		if handleServiceError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, post)
	case "categories":
		if targetType != domain.ModerationTargetPost {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		var req struct {
			Categories []int64 `json:"categories"`
			Note       string  `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		post, err := h.moderation.UpdatePostCategories(r.Context(), userID, targetID, req.Categories, req.Note)
		if handleServiceError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, post)
	case "soft-delete":
		var req contentModerationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		reason, err := parseModerationReason(req.Reason)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		if err := h.moderation.SoftDeleteContent(r.Context(), userID, targetType, targetID, reason, req.Note); handleServiceError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case "restore":
		var req contentModerationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		if err := h.moderation.RestoreContent(r.Context(), userID, targetType, targetID, req.Note); handleServiceError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case "hard-delete":
		var req contentModerationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		reason, err := parseModerationReason(req.Reason)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		if err := h.moderation.HardDeleteContent(r.Context(), userID, targetType, targetID, reason, req.Note); handleServiceError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case "protection":
		if targetType != domain.ModerationTargetPost {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		var req protectRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		post, err := h.moderation.SetPostDeleteProtection(r.Context(), userID, targetID, req.Protected, req.Note)
		if handleServiceError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, post)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (h *Handler) handleModerationCategories(w http.ResponseWriter, r *http.Request) {
	if h.moderation == nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		categories, err := h.posts.ListCategories(r.Context())
		if handleServiceError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, categories)
	case http.MethodPost:
		var req categoryCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		category, err := h.moderation.CreateCategory(r.Context(), userID, req.Name)
		if handleServiceError(w, err) {
			return
		}
		writeJSON(w, http.StatusCreated, category)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleModerationCategorySubroutes(w http.ResponseWriter, r *http.Request) {
	if h.moderation == nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	const prefix = "/api/moderation/categories/"
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, prefix), "/"), "/")
	if len(parts) != 2 || parts[1] != "delete" || r.Method != http.MethodPost {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	categoryID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || categoryID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}
	var req categoryDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}
	moved, err := h.moderation.DeleteCategory(r.Context(), userID, categoryID, req.Note)
	if handleServiceError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"movedPosts": moved})
}

func (h *Handler) handleModerationHistory(w http.ResponseWriter, r *http.Request) {
	if h.moderation == nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}
	items, err := h.moderation.ListHistory(r.Context(), userID, parseHistoryFilter(r))
	if handleServiceError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) handleModerationHistoryPurge(w http.ResponseWriter, r *http.Request) {
	if h.moderation == nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}
	var req historyPurgeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}
	filter, err := parseHistoryFilterPayload(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}
	count, err := h.moderation.PurgeHistory(r.Context(), userID, filter, req.Note)
	if handleServiceError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"purged": count})
}

func (h *Handler) handleModerationUserSubroutes(w http.ResponseWriter, r *http.Request) {
	if h.moderation == nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	const prefix = "/api/moderation/users/"
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, prefix), "/"), "/")
	if len(parts) != 2 || parts[1] != "role" || r.Method != http.MethodPost {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	targetUserID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || targetUserID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}
	var req roleChangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}
	role, err := parseUserRole(req.Role)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}
	user, err := h.moderation.ChangeUserRole(r.Context(), userID, targetUserID, role, req.Note)
	if handleServiceError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func parseUserRole(raw string) (domain.UserRole, error) {
	switch strings.TrimSpace(raw) {
	case string(domain.RoleModerator):
		return domain.RoleModerator, nil
	case string(domain.RoleAdmin):
		return domain.RoleAdmin, nil
	case string(domain.RoleUser):
		return domain.RoleUser, nil
	default:
		return "", errors.New("invalid role")
	}
}

func parseModerationReason(raw string) (domain.ModerationReason, error) {
	switch strings.TrimSpace(raw) {
	case string(domain.ModerationReasonIrrelevant):
		return domain.ModerationReasonIrrelevant, nil
	case string(domain.ModerationReasonObscene):
		return domain.ModerationReasonObscene, nil
	case string(domain.ModerationReasonIllegal):
		return domain.ModerationReasonIllegal, nil
	case string(domain.ModerationReasonInsulting):
		return domain.ModerationReasonInsulting, nil
	case string(domain.ModerationReasonOther):
		return domain.ModerationReasonOther, nil
	default:
		return "", errors.New("invalid reason")
	}
}

func parseHistoryFilter(r *http.Request) domain.ModerationHistoryFilter {
	filter, _ := parseHistoryFilterPayload(historyPurgeRequest{
		ActionType: r.URL.Query().Get("actionType"),
		TargetType: r.URL.Query().Get("targetType"),
		Status:     r.URL.Query().Get("status"),
		From:       r.URL.Query().Get("from"),
		To:         r.URL.Query().Get("to"),
	})
	return filter
}

func parseHistoryFilterPayload(req historyPurgeRequest) (domain.ModerationHistoryFilter, error) {
	filter := domain.ModerationHistoryFilter{
		ActionType: strings.TrimSpace(req.ActionType),
		TargetType: strings.TrimSpace(req.TargetType),
		Status:     strings.TrimSpace(req.Status),
	}
	if strings.TrimSpace(req.From) != "" {
		value, err := time.Parse(time.RFC3339, strings.TrimSpace(req.From))
		if err != nil {
			return domain.ModerationHistoryFilter{}, err
		}
		filter.From = &value
	}
	if strings.TrimSpace(req.To) != "" {
		value, err := time.Parse(time.RFC3339, strings.TrimSpace(req.To))
		if err != nil {
			return domain.ModerationHistoryFilter{}, err
		}
		filter.To = &value
	}
	return filter, nil
}

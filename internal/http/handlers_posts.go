package http

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"forum/internal/domain"
	"forum/internal/service"
)

type createPostRequest struct {
	Title        string  `json:"title"`
	Body         string  `json:"body"`
	Categories   []int64 `json:"categories"`
	AttachmentID *int64  `json:"attachmentId,omitempty"`
}

type createCommentRequest struct {
	Body     string `json:"body"`
	ParentID *int64 `json:"parent_id,omitempty"`
}

type postDetailResponse struct {
	domain.Post
	IsSubscribed      bool `json:"isSubscribed"`
	IsFollowingAuthor bool `json:"isFollowingAuthor"`
}

type reactRequest struct {
	Value int `json:"value"`
}

func (h *Handler) handleCategories(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	categories, err := h.posts.ListCategories(r.Context())
	if handleServiceError(w, err) {
		return
	}
	if categories == nil {
		categories = []domain.Category{}
	}

	writeJSON(w, http.StatusOK, categories)
}

func (h *Handler) handlePosts(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/posts" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.handleListPosts(w, r)
	case http.MethodPost:
		h.handleCreatePost(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleListPosts(w http.ResponseWriter, r *http.Request) {
	var filter domain.PostFilter

	categories := r.URL.Query()["cat"]
	for _, c := range categories {
		if id, err := strconv.ParseInt(c, 10, 64); err == nil {
			filter.CategoryIDs = append(filter.CategoryIDs, id)
		}
	}

	filter.Mine = parseBoolQuery(r.URL.Query().Get("mine"))
	filter.Liked = parseBoolQuery(r.URL.Query().Get("liked"))
	q, err := parseSearchQuery(r.URL.Query().Get("q"))
	if handled := handleServiceError(w, err); handled {
		return
	}
	filter.Query = q

	if userID, ok := userIDFromContext(r.Context()); ok {
		filter.UserID = &userID
	}

	posts, err := h.posts.ListPosts(r.Context(), filter)
	if handleServiceError(w, err) {
		return
	}
	if posts == nil {
		posts = []domain.Post{}
	}

	writeJSON(w, http.StatusOK, posts)
}

func (h *Handler) handleCreatePost(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}

	var req createPostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}

	post, err := h.posts.CreatePost(r.Context(), userID, req.Title, req.Body, req.Categories, req.AttachmentID)
	if handleServiceError(w, err) {
		return
	}

	writeJSON(w, http.StatusCreated, post)
}

func (h *Handler) handlePostsSubroutes(w http.ResponseWriter, r *http.Request) {
	prefix := "/api/posts/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, prefix)
	parts := strings.Split(rest, "/")

	if len(parts) == 1 {
		id, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}

		switch r.Method {
		case http.MethodGet:
			h.handleGetPost(w, r, id)
		case http.MethodPut:
			h.handleUpdatePost(w, r, id)
		case http.MethodDelete:
			h.handleDeletePost(w, r, id)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	if len(parts) == 2 {
		id, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}

		switch parts[1] {
		case "comments":
			switch r.Method {
			case http.MethodGet:
				h.handleListComments(w, r, id)
			case http.MethodPost:
				h.handleCreateComment(w, r, id)
			default:
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			}
		case "react":
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			h.handlePostReaction(w, r, id)
		case "subscription":
			switch r.Method {
			case http.MethodPost:
				h.handleSubscribePost(w, r, id)
			case http.MethodDelete:
				h.handleUnsubscribePost(w, r, id)
			default:
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			}
		default:
			writeError(w, http.StatusNotFound, "not found")
		}
		return
	}

	writeError(w, http.StatusNotFound, "not found")
}

func (h *Handler) handleCreateComment(w http.ResponseWriter, r *http.Request, postID int64) {
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}

	var req createCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}

	if req.ParentID != nil && *req.ParentID <= 0 {
		req.ParentID = nil
	}

	comment, err := h.posts.CreateComment(r.Context(), userID, postID, req.Body, req.ParentID)
	if handleServiceError(w, err) {
		return
	}

	writeJSON(w, http.StatusCreated, comment)
}

func (h *Handler) handleGetPost(w http.ResponseWriter, r *http.Request, postID int64) {
	post, err := h.posts.GetPost(r.Context(), postID)
	if handleServiceError(w, err) {
		return
	}

	response := postDetailResponse{Post: *post}
	if h.center != nil {
		if userID, ok := userIDFromContext(r.Context()); ok {
			response.IsSubscribed, _ = h.center.IsPostSubscribed(r.Context(), userID, post.ID)
			response.IsFollowingAuthor, _ = h.center.IsFollowingUser(r.Context(), userID, post.UserID)
		}
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handleListComments(w http.ResponseWriter, r *http.Request, postID int64) {
	q, err := parseSearchQuery(r.URL.Query().Get("q"))
	if handleServiceError(w, err) {
		return
	}

	comments, err := h.posts.ListComments(r.Context(), postID, domain.CommentFilter{Query: q})
	if handleServiceError(w, err) {
		return
	}
	if comments == nil {
		comments = []domain.Comment{}
	}

	writeJSON(w, http.StatusOK, comments)
}

func (h *Handler) handlePostReaction(w http.ResponseWriter, r *http.Request, postID int64) {
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}

	var req reactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}

	if _, err := h.posts.ReactPost(r.Context(), userID, postID, req.Value); handleServiceError(w, err) {
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleUpdatePost(w http.ResponseWriter, r *http.Request, postID int64) {
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}

	var req createPostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}

	post, err := h.posts.UpdatePost(r.Context(), userID, postID, req.Title, req.Body, req.Categories)
	if handleServiceError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, post)
}

func (h *Handler) handleDeletePost(w http.ResponseWriter, r *http.Request, postID int64) {
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}

	if err := h.posts.DeletePost(r.Context(), userID, postID); handleServiceError(w, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleSubscribePost(w http.ResponseWriter, r *http.Request, postID int64) {
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}
	if h.center == nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if err := h.center.SubscribePost(r.Context(), userID, postID); handleServiceError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleUnsubscribePost(w http.ResponseWriter, r *http.Request, postID int64) {
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}
	if h.center == nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if err := h.center.UnsubscribePost(r.Context(), userID, postID); handleServiceError(w, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleCommentsSubroutes(w http.ResponseWriter, r *http.Request) {
	prefix := "/api/comments/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, prefix)
	parts := strings.Split(rest, "/")
	if len(parts) == 1 {
		id, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		switch r.Method {
		case http.MethodPut:
			h.handleUpdateComment(w, r, id)
		case http.MethodDelete:
			h.handleDeleteComment(w, r, id)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	if len(parts) != 2 || parts[1] != "react" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}

	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	h.handleCommentReaction(w, r, id)
}

func (h *Handler) handleCommentReaction(w http.ResponseWriter, r *http.Request, commentID int64) {
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}

	var req reactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}

	if _, err := h.posts.ReactComment(r.Context(), userID, commentID, req.Value); handleServiceError(w, err) {
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleUpdateComment(w http.ResponseWriter, r *http.Request, commentID int64) {
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}

	var req createCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}

	comment, err := h.posts.UpdateComment(r.Context(), userID, commentID, req.Body)
	if handleServiceError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, comment)
}

func (h *Handler) handleDeleteComment(w http.ResponseWriter, r *http.Request, commentID int64) {
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}

	if err := h.posts.DeleteComment(r.Context(), userID, commentID); handleServiceError(w, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func parseBoolQuery(val string) bool {
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func parseSearchQuery(raw string) (string, error) {
	q := strings.TrimSpace(raw)
	if utf8.RuneCountInString(q) > 100 {
		return "", service.ErrInvalidInput
	}
	return q, nil
}

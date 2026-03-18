package http

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"forum/internal/domain"
	"forum/internal/service"
)

type publicProfileResponse struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	FirstName   string `json:"firstName"`
	LastName    string `json:"lastName"`
	Age         int    `json:"age"`
	Gender      string `json:"gender"`
	IsFollowing bool   `json:"isFollowing"`
}

type meResponse struct {
	ID                int64                         `json:"id"`
	Email             string                        `json:"email"`
	Username          string                        `json:"username"`
	DisplayName       string                        `json:"displayName"`
	FirstName         string                        `json:"firstName"`
	LastName          string                        `json:"lastName"`
	Age               int                           `json:"age"`
	Gender            string                        `json:"gender"`
	NeedsProfileSetup bool                          `json:"needsProfileSetup"`
	CreatedAt         time.Time                     `json:"created_at"`
	HasPassword       bool                          `json:"hasPassword"`
	LinkedAccounts    []service.LinkedAccountStatus `json:"linkedAccounts,omitempty"`
}

type updateProfileRequest struct {
	DisplayName *string `json:"displayName"`
	FirstName   *string `json:"firstName"`
	LastName    *string `json:"lastName"`
	Age         *int    `json:"age"`
	Gender      *string `json:"gender"`
	Skip        bool    `json:"skip"`
}

func (h *Handler) handlePublicProfile(w http.ResponseWriter, r *http.Request) {
	const prefix = "/api/u/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, prefix), "/")
	if rest == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	parts := strings.Split(rest, "/")
	username := strings.TrimSpace(parts[0])
	if username == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.handlePublicProfileGet(w, r, username)
		return
	}

	if len(parts) == 2 && parts[1] == "follow" {
		switch r.Method {
		case http.MethodPost:
			h.handleFollowUser(w, r, username)
		case http.MethodDelete:
			h.handleUnfollowUser(w, r, username)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	writeError(w, http.StatusNotFound, "not found")
}

func (h *Handler) handlePublicProfileGet(w http.ResponseWriter, r *http.Request, username string) {
	user, err := h.auth.GetPublicProfile(r.Context(), username)
	if handleServiceError(w, err) {
		return
	}

	response := newPublicProfileResponse(*user)
	if h.center != nil {
		if userID, ok := userIDFromContext(r.Context()); ok {
			response.IsFollowing, _ = h.center.IsFollowingUser(r.Context(), userID, user.ID)
		}
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handleFollowUser(w http.ResponseWriter, r *http.Request, username string) {
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}
	if h.center == nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	target, err := h.auth.GetPublicProfile(r.Context(), username)
	if handleServiceError(w, err) {
		return
	}
	if err := h.center.FollowUser(r.Context(), userID, target.ID); handleServiceError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleUnfollowUser(w http.ResponseWriter, r *http.Request, username string) {
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}
	if h.center == nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	target, err := h.auth.GetPublicProfile(r.Context(), username)
	if handleServiceError(w, err) {
		return
	}
	if err := h.center.UnfollowUser(r.Context(), userID, target.ID); handleServiceError(w, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleMyProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}

	var req updateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}

	user, err := h.auth.UpdateMyProfile(r.Context(), userID, req.DisplayName, req.FirstName, req.LastName, req.Age, req.Gender, !req.Skip, req.Skip)
	if handleServiceError(w, err) {
		return
	}

	writeJSON(w, http.StatusOK, newMeResponse(*user, strings.TrimSpace(user.PassHash) != "", nil))
}

func newPublicProfileResponse(user domain.User) publicProfileResponse {
	return publicProfileResponse{
		ID:          strconv.FormatInt(user.ID, 10),
		Username:    user.Username,
		DisplayName: publicDisplayName(user),
		FirstName:   strings.TrimSpace(user.FirstName),
		LastName:    strings.TrimSpace(user.LastName),
		Age:         user.Age,
		Gender:      strings.TrimSpace(user.Gender),
	}
}

func newMeResponse(user domain.User, hasPassword bool, linkedAccounts []service.LinkedAccountStatus) meResponse {
	return meResponse{
		ID:                user.ID,
		Email:             user.Email,
		Username:          user.Username,
		DisplayName:       strings.TrimSpace(user.DisplayName),
		FirstName:         strings.TrimSpace(user.FirstName),
		LastName:          strings.TrimSpace(user.LastName),
		Age:               user.Age,
		Gender:            strings.TrimSpace(user.Gender),
		NeedsProfileSetup: !user.ProfileInitialized,
		CreatedAt:         user.CreatedAt,
		HasPassword:       hasPassword,
		LinkedAccounts:    linkedAccounts,
	}
}

func publicDisplayName(user domain.User) string {
	displayName := strings.TrimSpace(user.DisplayName)
	if displayName != "" {
		return displayName
	}
	return strings.TrimSpace(user.Username)
}

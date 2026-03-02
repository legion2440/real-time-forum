package http

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"forum/internal/domain"
)

type publicProfileResponse struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

type meResponse struct {
	ID                int64     `json:"id"`
	Email             string    `json:"email"`
	Username          string    `json:"username"`
	DisplayName       string    `json:"displayName"`
	NeedsProfileSetup bool      `json:"needsProfileSetup"`
	CreatedAt         time.Time `json:"created_at"`
}

type updateProfileRequest struct {
	DisplayName string `json:"displayName"`
	Skip        bool   `json:"skip"`
}

func (h *Handler) handlePublicProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	const prefix = "/api/u/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	username := strings.Trim(strings.TrimPrefix(r.URL.Path, prefix), "/")
	if username == "" || strings.Contains(username, "/") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	user, err := h.auth.GetPublicProfile(r.Context(), username)
	if handleServiceError(w, err) {
		return
	}

	writeJSON(w, http.StatusOK, newPublicProfileResponse(*user))
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

	user, err := h.auth.UpdateMyProfile(r.Context(), userID, req.DisplayName, req.Skip)
	if handleServiceError(w, err) {
		return
	}

	writeJSON(w, http.StatusOK, newMeResponse(*user))
}

func newPublicProfileResponse(user domain.User) publicProfileResponse {
	return publicProfileResponse{
		ID:          strconv.FormatInt(user.ID, 10),
		Username:    user.Username,
		DisplayName: strings.TrimSpace(user.DisplayName),
	}
}

func newMeResponse(user domain.User) meResponse {
	return meResponse{
		ID:                user.ID,
		Email:             user.Email,
		Username:          user.Username,
		DisplayName:       strings.TrimSpace(user.DisplayName),
		NeedsProfileSetup: !user.ProfileInitialized,
		CreatedAt:         user.CreatedAt,
	}
}

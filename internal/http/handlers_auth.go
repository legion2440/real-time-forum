package http

import (
	"encoding/json"
	"net/http"
	"strings"
)

type registerRequest struct {
	Email    string `json:"email"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginRequest struct {
	Login    string `json:"login"`
	Email    string `json:"email"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *Handler) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}

	user, err := h.auth.Register(r.Context(), req.Email, req.Username, req.Password)
	if handleServiceError(w, err) {
		return
	}

	writeJSON(w, http.StatusCreated, user)
}

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}

	loginIdentifier := strings.TrimSpace(req.Login)
	if loginIdentifier == "" {
		loginIdentifier = strings.TrimSpace(req.Email)
	}

	session, user, err := h.auth.Login(r.Context(), loginIdentifier, req.Username, req.Password)
	if handleServiceError(w, err) {
		return
	}

	setSessionCookie(w, session.Token, session.ExpiresAt)
	writeJSON(w, http.StatusOK, user)
}

func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	token, ok := sessionTokenFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}

	if err := h.auth.Logout(r.Context(), token); handleServiceError(w, err) {
		return
	}

	clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleMe(w http.ResponseWriter, r *http.Request) {
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

	writeJSON(w, http.StatusOK, newMeResponse(*user))
}

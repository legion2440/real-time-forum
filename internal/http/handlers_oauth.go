package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"forum/internal/service"
)

type oauthFlowLocalConfirmRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

type oauthFlowCompleteRequest struct {
	DisplayName string `json:"displayName"`
}

type localMergeRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

func (h *Handler) handleOAuthRoutes(w http.ResponseWriter, r *http.Request) {
	parts := splitPath(r.URL.Path)
	if len(parts) != 3 || parts[0] != "auth" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	provider := parts[1]
	action := parts[2]

	switch action {
	case "login":
		h.handleOAuthLogin(w, r, provider)
	case "callback":
		h.handleOAuthCallback(w, r, provider)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (h *Handler) handleOAuthLogin(w http.ResponseWriter, r *http.Request, provider string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	currentUserID, _ := userIDFromContext(r.Context())
	authURL, err := h.auth.StartOAuth(
		r.Context(),
		provider,
		r.URL.Query().Get("intent"),
		r.URL.Query().Get("next"),
		currentUserID,
	)
	if err != nil {
		http.Redirect(w, r, h.oauthErrorRedirectPath(r, currentUserID, err), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, authURL, http.StatusFound)
}

func (h *Handler) handleOAuthCallback(w http.ResponseWriter, r *http.Request, provider string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	currentUserID, _ := userIDFromContext(r.Context())
	result, err := h.auth.HandleOAuthCallback(
		r.Context(),
		provider,
		r.URL.Query().Get("state"),
		r.URL.Query().Get("code"),
		r.URL.Query().Get("error"),
	)
	if err != nil {
		http.Redirect(w, r, h.oauthErrorRedirectPath(r, currentUserID, err), http.StatusSeeOther)
		return
	}

	if result.Session != nil {
		setSessionCookie(w, result.Session.Token, result.Session.ExpiresAt)
	}
	http.Redirect(w, r, safeRedirectPath(result.RedirectPath), http.StatusSeeOther)
}

func (h *Handler) handleOAuthProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, h.auth.OAuthProviders())
}

func (h *Handler) handleAuthFlowRoutes(w http.ResponseWriter, r *http.Request) {
	parts := splitPath(r.URL.Path)
	if len(parts) < 4 || len(parts) > 5 || parts[0] != "api" || parts[1] != "auth" || parts[2] != "flows" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	token := strings.TrimSpace(parts[3])
	if token == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	if len(parts) == 4 {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		currentUserID, _ := userIDFromContext(r.Context())
		flowView, err := h.auth.GetAuthFlowView(r.Context(), token, currentUserID)
		if handleServiceError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, flowView)
		return
	}

	switch parts[4] {
	case "confirm-local":
		h.handleAuthFlowConfirmLocal(w, r, token)
	case "complete":
		h.handleAuthFlowComplete(w, r, token)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (h *Handler) handleAuthFlowConfirmLocal(w http.ResponseWriter, r *http.Request, token string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req oauthFlowLocalConfirmRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}

	result, err := h.auth.ConfirmLinkFlowWithLocal(r.Context(), token, req.Login, req.Password)
	if handleServiceError(w, err) {
		return
	}
	if result.Session != nil {
		setSessionCookie(w, result.Session.Token, result.Session.ExpiresAt)
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) handleAuthFlowComplete(w http.ResponseWriter, r *http.Request, token string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	currentUserID, _ := userIDFromContext(r.Context())
	flowView, err := h.auth.GetAuthFlowView(r.Context(), token, currentUserID)
	if handleServiceError(w, err) {
		return
	}

	var req oauthFlowCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, http.ErrBodyReadAfterClose) {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}

	var result *service.OAuthCallbackResult
	switch flowView.Kind {
	case "account_link":
		result, err = h.auth.CompleteLinkFlow(r.Context(), token, currentUserID)
	case "account_merge":
		result, err = h.auth.CompleteMergeFlow(r.Context(), token, currentUserID, req.DisplayName)
	default:
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}
	if handleServiceError(w, err) {
		return
	}
	if result.Session != nil {
		setSessionCookie(w, result.Session.Token, result.Session.ExpiresAt)
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) handleLinkedAccountRoutes(w http.ResponseWriter, r *http.Request) {
	parts := splitPath(r.URL.Path)
	if len(parts) != 5 || parts[0] != "api" || parts[1] != "profile" || parts[2] != "linked-accounts" || parts[4] != "unlink" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	currentUserID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}

	if err := h.auth.UnlinkAccount(r.Context(), currentUserID, parts[3]); handleServiceError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleLocalMerge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	currentUserID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}

	var req localMergeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}

	token, err := h.auth.StartLocalAccountMerge(r.Context(), currentUserID, req.Login, req.Password)
	if handleServiceError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"flowToken":    token,
		"redirectPath": "/account-merge?flow=" + url.QueryEscape(token),
	})
}

func (h *Handler) oauthErrorRedirectPath(r *http.Request, currentUserID int64, err error) string {
	base := "/login"
	if currentUserID > 0 {
		if user, getErr := h.auth.GetUserByID(r.Context(), currentUserID); getErr == nil {
			base = "/u/" + url.PathEscape(strings.TrimSpace(user.Username))
		}
	}

	return appendRedirectQuery(base, "authError", oauthErrorMessage(err))
}

func oauthErrorMessage(err error) string {
	switch {
	case errors.Is(err, service.ErrOAuthProviderUnavailable):
		return "OAuth provider is not configured."
	case errors.Is(err, service.ErrOAuthProviderReturnedError):
		return "Provider returned an authentication error."
	case errors.Is(err, service.ErrOAuthStateInvalid), errors.Is(err, service.ErrAuthFlowExpired):
		return "Authentication flow expired or became invalid. Start again."
	case errors.Is(err, service.ErrOAuthCodeMissing):
		return "Authentication callback was incomplete."
	case errors.Is(err, service.ErrOAuthTokenExchangeFailed):
		return "Failed to exchange OAuth code for token."
	case errors.Is(err, service.ErrOAuthIdentityFetchFailed):
		return "Failed to fetch account details from provider."
	case errors.Is(err, service.ErrOAuthEmailUnavailable):
		return "Provider did not return a usable email address."
	case errors.Is(err, service.ErrMergeDenied):
		return "Accounts cannot be merged safely."
	case errors.Is(err, service.ErrAlreadyLinked):
		return "Provider is already linked."
	case errors.Is(err, service.ErrConflict):
		return "Account conflict detected."
	default:
		return "Authentication failed."
	}
}

func splitPath(path string) []string {
	clean := strings.Trim(strings.TrimSpace(path), "/")
	if clean == "" {
		return nil
	}
	return strings.Split(clean, "/")
}

func appendRedirectQuery(rawPath, key, value string) string {
	rawPath = safeRedirectPath(rawPath)
	u, err := url.Parse(rawPath)
	if err != nil {
		return rawPath
	}
	query := u.Query()
	query.Set(key, value)
	u.RawQuery = query.Encode()
	return u.String()
}

func safeRedirectPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return "/"
	}
	return path
}

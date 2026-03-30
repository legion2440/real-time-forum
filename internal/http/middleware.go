package http

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"forum/internal/service"
)

func (h *Handler) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err == nil && cookie.Value != "" {
			session, err := h.auth.GetSession(r.Context(), cookie.Value)
			if err == nil {
				ctx := withUserID(r.Context(), session.UserID)
				ctx = withSessionToken(ctx, session.Token)
				r = r.WithContext(ctx)
			} else if errorsIsUnauthorized(err) {
				clearSessionCookie(w)
				r = r.WithContext(withSessionEnded(r.Context()))
			}
		}

		next.ServeHTTP(w, r)
	})
}

func errorsIsUnauthorized(err error) bool {
	return errors.Is(err, service.ErrUnauthorized)
}

func (h *Handler) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := userIDFromContext(r.Context()); !ok {
			writeAuthUnauthorized(w, r)
			return
		}
		next.ServeHTTP(w, r)
	}
}

func (h *Handler) globalRateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if allowed, retryAfter := h.security.allowGlobalRequest(r); !allowed {
			writeRateLimited(w, retryAfter)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) authEndpointRateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isAuthEndpointRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		if allowed, retryAfter := h.security.allowAuthEndpoint(r); !allowed {
			writeRateLimited(w, retryAfter)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) writeActionRateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isWriteActionRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		if allowed, retryAfter := h.security.allowWriteAction(r.Context(), r); !allowed {
			writeRateLimited(w, retryAfter)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) webSocketHandshakeRateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if allowed, retryAfter := h.security.allowWebSocketHandshake(r); !allowed {
			writeRateLimited(w, retryAfter)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isAuthEndpointRequest(r *http.Request) bool {
	if r == nil {
		return false
	}

	path := strings.TrimSpace(r.URL.Path)
	switch {
	case path == "/api/login":
		return true
	case path == "/api/register":
		return true
	case path == "/api/logout":
		return true
	case path == "/api/profile/local-account/merge":
		return true
	case strings.HasPrefix(path, "/api/auth/flows/"):
		return true
	case strings.HasPrefix(path, "/auth/"):
		return true
	default:
		return false
	}
}

func isWriteActionRequest(r *http.Request) bool {
	if r == nil {
		return false
	}

	path := strings.TrimSpace(r.URL.Path)
	switch {
	case path == "/api/posts" && r.Method == http.MethodPost:
		return true
	case strings.HasPrefix(path, "/api/posts/"):
		return isPostWriteAction(path, r.Method)
	case strings.HasPrefix(path, "/api/comments/"):
		return isCommentWriteAction(path, r.Method)
	case strings.HasPrefix(path, "/api/u/"):
		return isFollowWriteAction(path, r.Method)
	case strings.HasPrefix(path, "/api/dm/"):
		return isDMWriteAction(path, r.Method)
	default:
		return false
	}
}

func isPostWriteAction(path, method string) bool {
	parts := splitPath(path)
	if len(parts) == 3 {
		return method == http.MethodPut || method == http.MethodDelete
	}
	if len(parts) != 4 {
		return false
	}

	switch parts[3] {
	case "comments":
		return method == http.MethodPost
	case "react":
		return method == http.MethodPost
	case "subscription":
		return method == http.MethodPost || method == http.MethodDelete
	default:
		return false
	}
}

func isCommentWriteAction(path, method string) bool {
	parts := splitPath(path)
	if len(parts) == 3 {
		return method == http.MethodPut || method == http.MethodDelete
	}
	return len(parts) == 4 && parts[3] == "react" && method == http.MethodPost
}

func isFollowWriteAction(path, method string) bool {
	parts := splitPath(path)
	return len(parts) == 4 && parts[3] == "follow" && (method == http.MethodPost || method == http.MethodDelete)
}

func isDMWriteAction(path, method string) bool {
	parts := splitPath(path)
	return len(parts) == 4 && parts[3] == "read" && method == http.MethodPost
}

func setSessionCookie(w http.ResponseWriter, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

package http

import (
	"errors"
	"net/http"
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

func setSessionCookie(w http.ResponseWriter, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
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
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"forum/internal/platform/id"
	"forum/internal/repo/sqlite"
	"forum/internal/service"
)

func TestUsersWithoutSessionReturnsUnauthorized(t *testing.T) {
	h := NewHandler(nil, nil)
	handler := h.Routes(t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}

	if !strings.Contains(rec.Body.String(), `"error":"unauthorized"`) {
		t.Fatalf("expected unauthorized json body, got %q", rec.Body.String())
	}
}

type fixedClock struct {
	t time.Time
}

func (f fixedClock) Now() time.Time {
	return f.t
}

type staticID struct {
	token string
}

func (s staticID) New() (string, error) {
	return s.token, nil
}

func newAuthHandler(t *testing.T) (*Handler, func()) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sqlite.Open(dbPath)
	if err != nil {
		if strings.Contains(err.Error(), "requires cgo to work") {
			t.Skip("sqlite3 driver requires cgo")
		}
		t.Fatalf("open db: %v", err)
	}

	auth := service.NewAuthService(
		sqlite.NewUserRepo(db),
		sqlite.NewSessionRepo(db),
		fixedClock{t: time.Unix(1700000000, 0)},
		staticID{token: "session-token"},
		24*time.Hour,
	)

	return NewHandler(auth, nil), func() { _ = db.Close() }
}

func mustRegisterUser(t *testing.T, auth *service.AuthService, email, username string) int64 {
	t.Helper()
	user, err := auth.Register(context.Background(), email, username, "secret")
	if err != nil {
		t.Fatalf("register %s: %v", username, err)
	}
	return user.ID
}

func mustLoginUser(t *testing.T, auth *service.AuthService, email string) string {
	t.Helper()
	session, _, err := auth.Login(context.Background(), email, "", "secret")
	if err != nil {
		t.Fatalf("login %s: %v", email, err)
	}
	return session.Token
}

var _ id.Generator = staticID{}

func stringPtr(value string) *string {
	return &value
}

func TestUsersReturnsOnlyPublicFields(t *testing.T) {
	h, cleanup := newAuthHandler(t)
	defer cleanup()

	mustRegisterUser(t, h.auth, "first@example.com", "first")
	secondUserID := mustRegisterUser(t, h.auth, "second@example.com", "second")
	if _, err := h.auth.UpdateMyProfile(context.Background(), secondUserID, stringPtr("Visible Second"), nil, nil, nil, nil, true, false); err != nil {
		t.Fatalf("set display name: %v", err)
	}
	token := mustLoginUser(t, h.auth, "first@example.com")

	handler := h.Routes(t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token, Path: "/"})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%q", http.StatusOK, rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"id":"1"`) || !strings.Contains(body, `"name":"first"`) || !strings.Contains(body, `"username":"first"`) {
		t.Fatalf("expected first user in body, got %q", body)
	}
	if !strings.Contains(body, `"id":"2"`) || !strings.Contains(body, `"name":"Visible Second"`) || !strings.Contains(body, `"username":"second"`) {
		t.Fatalf("expected second user in body, got %q", body)
	}
	if strings.Contains(body, `"email"`) {
		t.Fatalf("expected response without email, got %q", body)
	}
	if strings.Contains(body, "pass") || strings.Contains(body, "hash") {
		t.Fatalf("expected response without pass/hash fields, got %q", body)
	}
}

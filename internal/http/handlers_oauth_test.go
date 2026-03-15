package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"forum/internal/oauth"
	"forum/internal/repo/sqlite"
	"forum/internal/service"
)

type oauthStubProvider struct {
	name     string
	label    string
	identity *oauth.Identity
}

func (p oauthStubProvider) Name() string  { return p.name }
func (p oauthStubProvider) Label() string { return p.label }
func (p oauthStubProvider) AuthCodeURL(state string) string {
	return "https://oauth.example.test/" + p.name + "?state=" + state
}
func (p oauthStubProvider) ExchangeCode(context.Context, string) (*oauth.Token, error) {
	return &oauth.Token{AccessToken: "token", TokenType: "Bearer"}, nil
}
func (p oauthStubProvider) FetchIdentity(context.Context, *oauth.Token) (*oauth.Identity, error) {
	return p.identity, nil
}

func newOAuthHandler(t *testing.T) (*Handler, func()) {
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
		service.WithOAuth(service.OAuthDependencies{
			Providers: oauth.NewRegistry(oauthStubProvider{
				name:  "google",
				label: "Google",
				identity: &oauth.Identity{
					Provider:       "google",
					ProviderUserID: "g-1",
					Email:          "oauth@example.com",
					EmailVerified:  true,
					DisplayName:    "OAuth User",
					Username:       "oauth-user",
				},
			}),
			Identities: sqlite.NewAuthIdentityRepo(db),
			Flows:      sqlite.NewAuthFlowRepo(db),
			Accounts:   sqlite.NewAccountRepo(db),
		}),
	)

	return NewHandler(auth, nil), func() { _ = db.Close() }
}

func TestOAuthCallbackInvalidStateRedirectsToLoginError(t *testing.T) {
	h, cleanup := newOAuthHandler(t)
	defer cleanup()

	handler := h.Routes(t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?state=missing-state&code=code", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected %d, got %d body=%q", http.StatusSeeOther, rec.Code, rec.Body.String())
	}

	location := rec.Header().Get("Location")
	if !strings.HasPrefix(location, "/login?") || !strings.Contains(location, "authError=") {
		t.Fatalf("expected redirect to login error, got %q", location)
	}
}

func TestOAuthCallbackMissingCodeRedirectsToLoginError(t *testing.T) {
	h, cleanup := newOAuthHandler(t)
	defer cleanup()

	handler := h.Routes(t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?state=state-only", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected %d, got %d body=%q", http.StatusSeeOther, rec.Code, rec.Body.String())
	}

	location := rec.Header().Get("Location")
	if !strings.HasPrefix(location, "/login?") || !strings.Contains(location, "authError=") {
		t.Fatalf("expected redirect to login error, got %q", location)
	}
}

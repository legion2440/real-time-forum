package service

import (
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"forum/internal/domain"
	"forum/internal/oauth"
	"forum/internal/repo/sqlite"
)

type stubOAuthProvider struct {
	name     string
	label    string
	identity *oauth.Identity
}

func (p stubOAuthProvider) Name() string  { return p.name }
func (p stubOAuthProvider) Label() string { return p.label }

func (p stubOAuthProvider) AuthCodeURL(state string) string {
	return "https://oauth.example.test/" + p.name + "?state=" + url.QueryEscape(state)
}

func (p stubOAuthProvider) ExchangeCode(context.Context, string) (*oauth.Token, error) {
	return &oauth.Token{AccessToken: "token", TokenType: "Bearer"}, nil
}

func (p stubOAuthProvider) FetchIdentity(context.Context, *oauth.Token) (*oauth.Identity, error) {
	return p.identity, nil
}

type oauthTestContext struct {
	service    *AuthService
	users      *sqlite.UserRepo
	identities *sqlite.AuthIdentityRepo
}

func newOAuthAuthService(t *testing.T, provider oauth.Provider) (*oauthTestContext, func()) {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := sqlite.Open(dbPath)
	if err != nil {
		if strings.Contains(err.Error(), "requires cgo to work") {
			t.Skip("sqlite3 driver requires cgo")
		}
		t.Fatalf("open db: %v", err)
	}

	users := sqlite.NewUserRepo(db)
	sessions := sqlite.NewSessionRepo(db)
	identities := sqlite.NewAuthIdentityRepo(db)
	flows := sqlite.NewAuthFlowRepo(db)
	accounts := sqlite.NewAccountRepo(db)

	svc := NewAuthService(
		users,
		sessions,
		fixedClock{t: time.Unix(1700000000, 0)},
		&seqID{},
		24*time.Hour,
		WithOAuth(OAuthDependencies{
			Providers:  oauth.NewRegistry(provider),
			Identities: identities,
			Flows:      flows,
			Accounts:   accounts,
		}),
	)

	return &oauthTestContext{
		service:    svc,
		users:      users,
		identities: identities,
	}, func() { _ = db.Close() }
}

func extractOAuthState(t *testing.T, authURL string) string {
	t.Helper()

	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse auth url: %v", err)
	}
	state := strings.TrimSpace(parsed.Query().Get("state"))
	if state == "" {
		t.Fatal("expected state query parameter")
	}
	return state
}

func TestAuthService_OAuthExistingIdentityLogsIntoLinkedUser(t *testing.T) {
	ctx, cleanup := newOAuthAuthService(t, stubOAuthProvider{
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
	})
	defer cleanup()

	authURL, err := ctx.service.StartOAuth(context.Background(), "google", "login", "/", 0)
	if err != nil {
		t.Fatalf("start oauth: %v", err)
	}

	result, err := ctx.service.HandleOAuthCallback(context.Background(), "google", extractOAuthState(t, authURL), "code-1", "")
	if err != nil {
		t.Fatalf("first callback: %v", err)
	}
	if result.Session == nil {
		t.Fatal("expected session to be created")
	}

	users, err := ctx.service.ListUsers(context.Background())
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user after first oauth login, got %d", len(users))
	}

	authURL, err = ctx.service.StartOAuth(context.Background(), "google", "login", "/", 0)
	if err != nil {
		t.Fatalf("start oauth second time: %v", err)
	}

	second, err := ctx.service.HandleOAuthCallback(context.Background(), "google", extractOAuthState(t, authURL), "code-2", "")
	if err != nil {
		t.Fatalf("second callback: %v", err)
	}
	if second.Session == nil {
		t.Fatal("expected session to be created for existing identity")
	}

	users, err = ctx.service.ListUsers(context.Background())
	if err != nil {
		t.Fatalf("list users after relogin: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected same linked user to be reused, got %d users", len(users))
	}
}

func TestAuthService_OAuthEmailMatchRequiresExplicitConfirmation(t *testing.T) {
	ctx, cleanup := newOAuthAuthService(t, stubOAuthProvider{
		name:  "google",
		label: "Google",
		identity: &oauth.Identity{
			Provider:       "google",
			ProviderUserID: "g-2",
			Email:          "local@example.com",
			EmailVerified:  true,
			DisplayName:    "Google Local",
			Username:       "google-local",
		},
	})
	defer cleanup()

	localUser, err := ctx.service.Register(context.Background(), "local@example.com", "local-user", "secret")
	if err != nil {
		t.Fatalf("register local user: %v", err)
	}

	authURL, err := ctx.service.StartOAuth(context.Background(), "google", "login", "/", 0)
	if err != nil {
		t.Fatalf("start oauth: %v", err)
	}

	result, err := ctx.service.HandleOAuthCallback(context.Background(), "google", extractOAuthState(t, authURL), "code", "")
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	if result.Session != nil {
		t.Fatal("expected no session while explicit confirmation is required")
	}
	if !strings.HasPrefix(result.RedirectPath, "/account-link?flow=") {
		t.Fatalf("expected redirect to account-link flow, got %q", result.RedirectPath)
	}

	count, err := ctx.identities.CountByUserID(context.Background(), localUser.ID)
	if err != nil {
		t.Fatalf("count identities: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no identity to be auto-linked, got %d", count)
	}
}

func TestAuthService_OAuthLoggedInLinkFlowLinksIdentityToCurrentUser(t *testing.T) {
	ctx, cleanup := newOAuthAuthService(t, stubOAuthProvider{
		name:  "github",
		label: "GitHub",
		identity: &oauth.Identity{
			Provider:       "github",
			ProviderUserID: "gh-1",
			Email:          "gh@example.com",
			EmailVerified:  true,
			DisplayName:    "GitHub User",
			Username:       "gh-user",
		},
	})
	defer cleanup()

	user, err := ctx.service.Register(context.Background(), "member@example.com", "member", "secret")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	authURL, err := ctx.service.StartOAuth(context.Background(), "github", "link", "/u/member", user.ID)
	if err != nil {
		t.Fatalf("start oauth link: %v", err)
	}

	result, err := ctx.service.HandleOAuthCallback(context.Background(), "github", extractOAuthState(t, authURL), "code", "")
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	if result.Session != nil {
		t.Fatal("expected existing authenticated session to remain in place")
	}
	if !strings.Contains(result.RedirectPath, "linked=github") {
		t.Fatalf("expected linked redirect path, got %q", result.RedirectPath)
	}

	identity, err := ctx.identities.GetByUserProvider(context.Background(), user.ID, "github")
	if err != nil {
		t.Fatalf("get linked identity: %v", err)
	}
	if identity.UserID != user.ID || identity.ProviderUserID != "gh-1" {
		t.Fatalf("unexpected linked identity: %+v", identity)
	}
}

func TestAuthService_UnlinkDeniedForLastSocialOnlyMethod(t *testing.T) {
	ctx, cleanup := newOAuthAuthService(t, stubOAuthProvider{
		name:  "facebook",
		label: "Facebook",
		identity: &oauth.Identity{
			Provider:       "facebook",
			ProviderUserID: "fb-1",
			Email:          "social@example.com",
			EmailVerified:  true,
			DisplayName:    "Social User",
			Username:       "social-user",
		},
	})
	defer cleanup()

	authURL, err := ctx.service.StartOAuth(context.Background(), "facebook", "login", "/", 0)
	if err != nil {
		t.Fatalf("start oauth: %v", err)
	}

	result, err := ctx.service.HandleOAuthCallback(context.Background(), "facebook", extractOAuthState(t, authURL), "code", "")
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	if result.Session == nil {
		t.Fatal("expected session to be created")
	}

	user, err := ctx.users.GetByEmailCI(context.Background(), "social@example.com")
	if err != nil {
		t.Fatalf("get social user: %v", err)
	}

	err = ctx.service.UnlinkAccount(context.Background(), user.ID, "facebook")
	if !errorsIs(t, err, ErrUnlinkDenied) {
		t.Fatalf("expected unlink denied, got %v", err)
	}
}

func TestChooseCanonicalMergeUsersPrefersOlderAccount(t *testing.T) {
	older := &domain.User{ID: 2, Username: "older", CreatedAt: time.Unix(100, 0)}
	newer := &domain.User{ID: 1, Username: "newer", CreatedAt: time.Unix(200, 0)}

	canonical, source := chooseCanonicalMergeUsers(newer, older)
	if canonical.ID != older.ID || source.ID != newer.ID {
		t.Fatalf("expected older user to be canonical, got canonical=%d source=%d", canonical.ID, source.ID)
	}
}

func errorsIs(t *testing.T, err error, target error) bool {
	t.Helper()
	return err != nil && errors.Is(err, target)
}

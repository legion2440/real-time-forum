package http

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicProfileReturnsOnlyPublicFields(t *testing.T) {
	h, cleanup := newAuthHandler(t)
	defer cleanup()

	mustRegisterUser(t, h.auth, "alpha@example.com", "alpha")
	if _, err := h.auth.UpdateMyProfile(context.Background(), 1, "Odinn", false); err != nil {
		t.Fatalf("update profile: %v", err)
	}

	handler := h.Routes(t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/u/alpha", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%q", http.StatusOK, rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"id":"1"`) || !strings.Contains(body, `"username":"alpha"`) || !strings.Contains(body, `"displayName":"Odinn"`) {
		t.Fatalf("expected public profile fields, got %q", body)
	}
	if strings.Contains(body, `"email"`) || strings.Contains(body, "pass") || strings.Contains(body, "hash") {
		t.Fatalf("expected response without sensitive fields, got %q", body)
	}
}

func TestUpdateMyProfileValidatesConflictsAndSetup(t *testing.T) {
	h, cleanup := newAuthHandler(t)
	defer cleanup()

	mustRegisterUser(t, h.auth, "alpha@example.com", "alpha")
	mustRegisterUser(t, h.auth, "beta@example.com", "beta")
	mustRegisterUser(t, h.auth, "gamma@example.com", "gamma")

	if _, err := h.auth.UpdateMyProfile(context.Background(), 2, "TakenName", false); err != nil {
		t.Fatalf("seed beta display name: %v", err)
	}

	alphaToken := mustLoginUser(t, h.auth, "alpha@example.com")
	handler := h.Routes(t.TempDir())

	meBefore := performProfileRequest(t, handler, http.MethodGet, "/api/me", "", alphaToken)
	if meBefore.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%q", http.StatusOK, meBefore.Code, meBefore.Body.String())
	}
	if !strings.Contains(meBefore.Body.String(), `"needsProfileSetup":true`) {
		t.Fatalf("expected needsProfileSetup=true, got %q", meBefore.Body.String())
	}

	conflictUsername := performProfileRequest(t, handler, http.MethodPut, "/api/me/profile", `{"displayName":"BETA"}`, alphaToken)
	if conflictUsername.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d body=%q", http.StatusBadRequest, conflictUsername.Code, conflictUsername.Body.String())
	}

	conflictDisplayName := performProfileRequest(t, handler, http.MethodPut, "/api/me/profile", `{"displayName":"takenname"}`, alphaToken)
	if conflictDisplayName.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d body=%q", http.StatusBadRequest, conflictDisplayName.Code, conflictDisplayName.Body.String())
	}

	ownUsernameAllowed := performProfileRequest(t, handler, http.MethodPut, "/api/me/profile", `{"displayName":"ALPHA"}`, alphaToken)
	if ownUsernameAllowed.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%q", http.StatusOK, ownUsernameAllowed.Code, ownUsernameAllowed.Body.String())
	}

	meAfterSave := performProfileRequest(t, handler, http.MethodGet, "/api/me", "", alphaToken)
	if meAfterSave.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%q", http.StatusOK, meAfterSave.Code, meAfterSave.Body.String())
	}
	if !strings.Contains(meAfterSave.Body.String(), `"needsProfileSetup":false`) {
		t.Fatalf("expected needsProfileSetup=false after save, got %q", meAfterSave.Body.String())
	}

	if err := h.auth.Logout(context.Background(), alphaToken); err != nil {
		t.Fatalf("logout alpha: %v", err)
	}

	gammaToken := mustLoginUser(t, h.auth, "gamma@example.com")
	meBeforeSkip := performProfileRequest(t, handler, http.MethodGet, "/api/me", "", gammaToken)
	if !strings.Contains(meBeforeSkip.Body.String(), `"needsProfileSetup":true`) {
		t.Fatalf("expected gamma needsProfileSetup=true, got %q", meBeforeSkip.Body.String())
	}

	skipResp := performProfileRequest(t, handler, http.MethodPut, "/api/me/profile", `{"skip":true}`, gammaToken)
	if skipResp.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%q", http.StatusOK, skipResp.Code, skipResp.Body.String())
	}

	meAfterSkip := performProfileRequest(t, handler, http.MethodGet, "/api/me", "", gammaToken)
	if !strings.Contains(meAfterSkip.Body.String(), `"needsProfileSetup":false`) {
		t.Fatalf("expected needsProfileSetup=false after skip, got %q", meAfterSkip.Body.String())
	}
}

func performProfileRequest(t *testing.T, handler http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()

	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}

	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token, Path: "/"})
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

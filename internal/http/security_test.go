package http

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"forum/internal/repo/sqlite"
	"forum/internal/service"
)

func TestLoginSetsSecureSessionCookieFlags(t *testing.T) {
	h, cleanup := newAuthHandlerWithSecurity(t, NewSecurity(SecurityOptions{}))
	defer cleanup()

	mustRegisterUser(t, h.auth, "cookie@example.com", "cookie_user")

	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"login":"cookie@example.com","password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.0.2.10:1234"

	rec := httptest.NewRecorder()
	h.Routes(newSecurityTestWebDir(t)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%q", http.StatusOK, rec.Code, rec.Body.String())
	}

	var sessionCookie *http.Cookie
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == sessionCookieName {
			sessionCookie = cookie
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session cookie to be set")
	}
	if !strings.HasPrefix(sessionCookie.Name, "__Host-") {
		t.Fatalf("expected __Host- prefixed cookie name, got %q", sessionCookie.Name)
	}
	if !sessionCookie.Secure {
		t.Fatal("expected Secure cookie flag")
	}
	if !sessionCookie.HttpOnly {
		t.Fatal("expected HttpOnly cookie flag")
	}
	if sessionCookie.Path != "/" {
		t.Fatalf("expected cookie path '/', got %q", sessionCookie.Path)
	}
	if sessionCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("expected SameSite=Lax, got %v", sessionCookie.SameSite)
	}
}

func TestGlobalRateLimiterReturnsTooManyRequests(t *testing.T) {
	security := NewSecurity(SecurityOptions{
		GlobalHTTP: RateLimitConfig{Requests: 1, Interval: time.Minute, Burst: 1},
	})

	h := NewHandler(nil, nil, security)
	handler := h.Routes(newSecurityTestWebDir(t))

	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	req1.RemoteAddr = "192.0.2.20:1234"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	if rec1.Code == http.StatusTooManyRequests {
		t.Fatalf("expected first request to pass, got %d", rec1.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "192.0.2.20:1234"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status %d, got %d", http.StatusTooManyRequests, rec2.Code)
	}
	if rec2.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header")
	}
}

func TestAuthRateLimiterReturnsTooManyRequests(t *testing.T) {
	security := NewSecurity(SecurityOptions{
		AuthEndpoints: RateLimitConfig{Requests: 1, Interval: time.Minute, Burst: 1},
	})
	h, cleanup := newAuthHandlerWithSecurity(t, security)
	defer cleanup()

	handler := h.Routes(newSecurityTestWebDir(t))
	requestBody := `{"login":"missing@example.com","password":"wrong"}`

	req1 := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(requestBody))
	req1.Header.Set("Content-Type", "application/json")
	req1.RemoteAddr = "192.0.2.30:1234"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	if rec1.Code == http.StatusTooManyRequests {
		t.Fatalf("expected first auth request to pass, got %d", rec1.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(requestBody))
	req2.Header.Set("Content-Type", "application/json")
	req2.RemoteAddr = "192.0.2.30:1234"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status %d, got %d", http.StatusTooManyRequests, rec2.Code)
	}
	if rec2.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header")
	}
}

func TestLoginThrottlerFirstFourFailuresHaveNoDelay(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	throttler := newLoginThrottler(
		func() time.Time { return now },
		[]time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second},
		30*time.Second,
		15*time.Minute,
		15*time.Minute,
	)

	for attempt := 1; attempt <= 4; attempt++ {
		delay := throttler.failure("demo@example.com")
		if delay != 0 {
			t.Fatalf("expected no delay for failure %d, got %v", attempt, delay)
		}
		if wait := throttler.wait("demo@example.com"); wait != 0 {
			t.Fatalf("expected no wait after failure %d, got %v", attempt, wait)
		}
	}

	if delay := throttler.failure("demo@example.com"); delay != time.Second {
		t.Fatalf("expected fifth failure delay %v, got %v", time.Second, delay)
	}
}

func TestLoginThrottlerBackoffGrowthAndCap(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	throttler := newLoginThrottler(
		func() time.Time { return now },
		[]time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second},
		30*time.Second,
		15*time.Minute,
		15*time.Minute,
	)

	expected := []time.Duration{
		0,
		0,
		0,
		0,
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		30 * time.Second,
		30 * time.Second,
	}

	for attempt, want := range expected {
		got := throttler.failure("demo@example.com")
		if got != want {
			t.Fatalf("failure %d: expected delay %v, got %v", attempt+1, want, got)
		}
	}
}

func TestFailedLoginThrottlingMessageAndReset(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	security := NewSecurity(SecurityOptions{
		Now: func() time.Time { return now },
	})
	h, cleanup := newAuthHandlerWithSecurity(t, security)
	defer cleanup()

	mustRegisterUser(t, h.auth, "throttle@example.com", "throttle_user")
	handler := h.Routes(newSecurityTestWebDir(t))

	login := func(password string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"login":"throttle@example.com","password":"`+password+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "192.0.2.40:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	for attempt := 1; attempt <= 5; attempt++ {
		rec := login("wrong")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected failed login %d to be unauthorized, got %d", attempt, rec.Code)
		}
	}

	throttled := login("wrong")
	if throttled.Code != http.StatusTooManyRequests {
		t.Fatalf("expected throttled login to return 429, got %d", throttled.Code)
	}
	if throttled.Header().Get("Retry-After") != "1" {
		t.Fatalf("expected Retry-After=1, got %q", throttled.Header().Get("Retry-After"))
	}
	if !strings.Contains(throttled.Body.String(), "Too many failed login attempts.") {
		t.Fatalf("expected specific throttling message, got %q", throttled.Body.String())
	}
	if strings.Contains(throttled.Body.String(), "Rate limit exceeded.") {
		t.Fatalf("expected specific throttling message instead of generic rate limit text, got %q", throttled.Body.String())
	}

	now = now.Add(1 * time.Second)
	success := login("secret")
	if success.Code != http.StatusOK {
		t.Fatalf("expected successful login to reset throttle, got %d body=%q", success.Code, success.Body.String())
	}

	afterReset := login("wrong")
	if afterReset.Code != http.StatusUnauthorized {
		t.Fatalf("expected throttle to reset after success, got %d", afterReset.Code)
	}
}

func TestWriteLimiterUsesAuthorizedUserID(t *testing.T) {
	security := NewSecurity(SecurityOptions{
		WriteActions: RateLimitConfig{Requests: 1, Interval: time.Minute, Burst: 1},
	})
	h, cleanup := newAuthHandlerWithSecurity(t, security)
	defer cleanup()

	mustRegisterUser(t, h.auth, "writer-one@example.com", "writer_one")
	mustRegisterUser(t, h.auth, "writer-two@example.com", "writer_two")
	token1 := mustLoginUser(t, h.auth, "writer-one@example.com")
	token2 := mustLoginUser(t, h.auth, "writer-two@example.com")

	handler := h.authMiddleware(h.writeActionRateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))

	sendWrite := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/posts", strings.NewReader(`{}`))
		req.RemoteAddr = "192.0.2.50:1234"
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token, Path: "/"})

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	rec1 := sendWrite(token1)
	if rec1.Code != http.StatusNoContent {
		t.Fatalf("expected first user write to pass, got %d", rec1.Code)
	}

	rec2 := sendWrite(token1)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second write for same user to be rate limited, got %d", rec2.Code)
	}

	rec3 := sendWrite(token2)
	if rec3.Code != http.StatusNoContent {
		t.Fatalf("expected second authorized user to get separate bucket, got %d", rec3.Code)
	}
}

func TestWebSocketHandshakeLimiterBlocksExcessiveUpgrades(t *testing.T) {
	security := NewSecurity(SecurityOptions{
		WebSocketHandshake: RateLimitConfig{Requests: 1, Interval: time.Minute, Burst: 1},
	})
	h := NewHandler(nil, nil, security)
	handler := h.Routes(newSecurityTestWebDir(t))

	newWSRequest := func() *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/ws", nil)
		req.RemoteAddr = "192.0.2.60:1234"
		req.Host = "example.com"
		req.Header.Set("Origin", "https://example.com")
		req.Header.Set("Connection", "Upgrade")
		req.Header.Set("Upgrade", "websocket")
		req.Header.Set("Sec-WebSocket-Version", "13")
		req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
		return req
	}

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, newWSRequest())
	if rec1.Code != http.StatusUnauthorized {
		t.Fatalf("expected first upgrade attempt to hit auth, got %d", rec1.Code)
	}

	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, newWSRequest())
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second upgrade attempt to be rate limited, got %d", rec2.Code)
	}
}

func newAuthHandlerWithSecurity(t *testing.T, security *Security) (*Handler, func()) {
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

	h := NewHandler(auth, nil, security)
	return h, func() {
		if security != nil {
			security.Close()
		}
		_ = db.Close()
	}
}

func newSecurityTestWebDir(t *testing.T) string {
	t.Helper()

	webDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "404.html"), []byte("missing"), 0o644); err != nil {
		t.Fatalf("write 404.html: %v", err)
	}
	return webDir
}

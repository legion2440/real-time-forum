package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWSWithoutSessionReturnsUnauthorized(t *testing.T) {
	h := NewHandler(nil, nil)
	handler := h.Routes(t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "http://example.com")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}

	if !strings.Contains(rec.Body.String(), `"error":"unauthorized"`) {
		t.Fatalf("expected unauthorized json body, got %q", rec.Body.String())
	}
}

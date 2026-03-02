package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDMWithoutSessionReturnsUnauthorized(t *testing.T) {
	h := NewHandler(nil, nil)
	handler := h.Routes(t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/api/dm/2", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}

	if !strings.Contains(rec.Body.String(), `"error":"unauthorized"`) {
		t.Fatalf("expected unauthorized json body, got %q", rec.Body.String())
	}
}

func TestDMPeersWithoutSessionReturnsUnauthorized(t *testing.T) {
	h := NewHandler(nil, nil)
	handler := h.Routes(t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/api/dm/peers", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}

	if !strings.Contains(rec.Body.String(), `"error":"unauthorized"`) {
		t.Fatalf("expected unauthorized json body, got %q", rec.Body.String())
	}
}

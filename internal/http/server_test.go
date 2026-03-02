package http

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSPAFallbackServesIndexForDMRoute(t *testing.T) {
	webDir := t.TempDir()
	marker := "dm-index-marker"

	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte(marker), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}

	h := NewHandler(nil, nil)
	handler := h.Routes(webDir)

	req := httptest.NewRequest(http.MethodGet, "/dm/1", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	if !strings.Contains(rec.Body.String(), marker) {
		t.Fatalf("expected body to contain index marker %q, got %q", marker, rec.Body.String())
	}
}

func TestSPAFallbackServesIndexForPostRoute(t *testing.T) {
	webDir := t.TempDir()
	marker := "post-index-marker"

	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte(marker), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}

	h := NewHandler(nil, nil)
	handler := h.Routes(webDir)

	req := httptest.NewRequest(http.MethodGet, "/post/1", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	if !strings.Contains(rec.Body.String(), marker) {
		t.Fatalf("expected body to contain index marker %q, got %q", marker, rec.Body.String())
	}
}

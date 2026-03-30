package app

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	httpserver "forum/internal/http"
	realtimews "forum/internal/realtime/ws"
)

func TestHTTPRedirectServerRedirectsToHTTPS(t *testing.T) {
	baseURL, err := url.Parse("https://127.0.0.1:8443")
	if err != nil {
		t.Fatalf("parse base url: %v", err)
	}

	server := buildHTTPRedirectServer("127.0.0.1:8080", baseURL)
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/post/1?view=full", nil)
	rec := httptest.NewRecorder()

	server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusPermanentRedirect {
		t.Fatalf("expected status %d, got %d", http.StatusPermanentRedirect, rec.Code)
	}
	if location := rec.Header().Get("Location"); location != "https://127.0.0.1:8443/post/1?view=full" {
		t.Fatalf("expected redirect location %q, got %q", "https://127.0.0.1:8443/post/1?view=full", location)
	}
}

func TestHTTPSServerUsesSecurityConfig(t *testing.T) {
	rootDir := repoRoot(t)
	security := httpserver.NewSecurity(httpserver.SecurityOptions{})
	defer security.Close()

	server, err := buildHTTPSServer(
		services{hub: realtimews.NewHub()},
		"127.0.0.1:8443",
		filepath.Join(rootDir, "web"),
		filepath.Join(rootDir, "certs", "dev-cert.pem"),
		filepath.Join(rootDir, "certs", "dev-key.pem"),
		security,
	)
	if err != nil {
		t.Fatalf("build https server: %v", err)
	}

	if server.ReadTimeout != 15*time.Second {
		t.Fatalf("expected ReadTimeout=15s, got %v", server.ReadTimeout)
	}
	if server.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("expected ReadHeaderTimeout=5s, got %v", server.ReadHeaderTimeout)
	}
	if server.WriteTimeout != 30*time.Second {
		t.Fatalf("expected WriteTimeout=30s, got %v", server.WriteTimeout)
	}
	if server.IdleTimeout != 60*time.Second {
		t.Fatalf("expected IdleTimeout=60s, got %v", server.IdleTimeout)
	}
	if server.MaxHeaderBytes != 1<<20 {
		t.Fatalf("expected MaxHeaderBytes=%d, got %d", 1<<20, server.MaxHeaderBytes)
	}
	if server.TLSConfig == nil {
		t.Fatal("expected TLS config")
	}
	if server.TLSConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("expected MinVersion TLS1.2, got %v", server.TLSConfig.MinVersion)
	}
	if len(server.TLSConfig.CipherSuites) == 0 {
		t.Fatal("expected explicit TLS 1.2 cipher suites")
	}

	expectedNextProtos := []string{"h2", "http/1.1"}
	if !reflect.DeepEqual(server.TLSConfig.NextProtos, expectedNextProtos) {
		t.Fatalf("expected NextProtos=%v, got %v", expectedNextProtos, server.TLSConfig.NextProtos)
	}
}

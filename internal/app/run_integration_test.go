package app

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"forum/internal/repo/sqlite"
)

func TestRunWithContextGracefulShutdown(t *testing.T) {
	t.Parallel()

	rootDir := repoRoot(t)
	tempDir := t.TempDir()
	requireSQLiteRuntime(t, filepath.Join(tempDir, "forum.db"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	listening := make(chan string, 1)
	runDone := make(chan error, 1)
	var appRuntime *runtime

	go func() {
		runDone <- runWithContext(ctx, runConfig{
			dbPath:      filepath.Join(tempDir, "forum.db"),
			httpAddr:    "127.0.0.1:0",
			httpsAddr:   "127.0.0.1:0",
			baseURL:     "https://127.0.0.1:8443",
			webDir:      filepath.Join(rootDir, "web"),
			uploadDir:   filepath.Join(tempDir, "uploads"),
			tlsCertFile: filepath.Join(rootDir, "certs", "dev-cert.pem"),
			tlsKeyFile:  filepath.Join(rootDir, "certs", "dev-key.pem"),
			onListening: func(rt *runtime, addr net.Addr) {
				appRuntime = rt
				listening <- addr.String()
			},
		}, nil)
	}()

	var addr string
	select {
	case addr = <-listening:
	case err := <-runDone:
		t.Fatalf("run exited before readiness: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for application startup")
	}

	waitForHTTPReady(t, "https://"+addr+"/")

	shutdownStarted := time.Now()
	cancel()

	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("run returned error on graceful shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for graceful shutdown")
	}

	if elapsed := time.Since(shutdownStarted); elapsed > 4*time.Second {
		t.Fatalf("graceful shutdown took too long: %v", elapsed)
	}
	if appRuntime == nil {
		t.Fatal("expected runtime from app lifecycle")
	}

	select {
	case <-appRuntime.hub.Done():
	default:
		t.Fatal("expected websocket hub to stop during shutdown")
	}

	if err := appRuntime.db.Ping(); err == nil {
		t.Fatal("expected database to be closed after shutdown")
	}

	client := &http.Client{
		Timeout: 300 * time.Millisecond,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	if resp, err := client.Get("https://" + addr + "/"); err == nil {
		resp.Body.Close()
		t.Fatal("expected HTTP server to stop accepting connections")
	}
}

func waitForHTTPReady(t *testing.T, rawURL string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	client := &http.Client{
		Timeout: 300 * time.Millisecond,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	for time.Now().Before(deadline) {
		resp, err := client.Get(rawURL)
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for HTTP readiness at %s", rawURL)
}

func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := goruntime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve test file path")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func requireSQLiteRuntime(t *testing.T, dbPath string) {
	t.Helper()

	db, err := sqlite.Open(dbPath)
	if err != nil {
		if strings.Contains(err.Error(), "requires cgo to work") {
			t.Skip("sqlite3 driver requires cgo")
		}
		t.Fatalf("open db for integration preflight: %v", err)
	}
	_ = db.Close()
}

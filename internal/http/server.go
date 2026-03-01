package http

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	realtimews "forum/internal/realtime/ws"
	"forum/internal/service"
)

const sessionCookieName = "forum_session"

type Handler struct {
	auth  *service.AuthService
	posts *service.PostService
	hub   *realtimews.Hub
}

func NewHandler(auth *service.AuthService, posts *service.PostService) *Handler {
	hub := realtimews.NewHub()
	go hub.Run()

	return &Handler{
		auth:  auth,
		posts: posts,
		hub:   hub,
	}
}

func (h *Handler) Routes(webDir string) http.Handler {
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("/api/register", h.handleRegister)
	apiMux.HandleFunc("/api/login", h.handleLogin)
	apiMux.HandleFunc("/api/logout", h.handleLogout)
	apiMux.HandleFunc("/api/me", h.handleMe)
	apiMux.HandleFunc("/api/users", h.handleUsers)
	apiMux.HandleFunc("/api/categories", h.handleCategories)
	apiMux.HandleFunc("/api/posts", h.handlePosts)
	apiMux.HandleFunc("/api/posts/", h.handlePostsSubroutes)
	apiMux.HandleFunc("/api/comments/", h.handleCommentsSubroutes)
	apiMux.HandleFunc("/api/debug/500", h.handleDebug500)
	apiMux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})

	rootMux := http.NewServeMux()
	rootMux.Handle("/api/", h.authMiddleware(apiMux))
	rootMux.Handle("/ws", h.authMiddleware(http.HandlerFunc(h.handleWS)))
	rootMux.Handle("/", spaHandler(webDir))

	return recoverMiddleware(rootMux)
}

func spaHandler(webDir string) http.Handler {
	fs := http.Dir(webDir)
	fileServer := http.FileServer(fs)
	indexPath := filepath.Join(webDir, "index.html")
	notFoundHTMLPath := filepath.Join(webDir, "404.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, http.StatusNotFound, "not found")
			return
		}

		reqPath := r.URL.Path
		if isKnownSPARoute(reqPath) {
			http.ServeFile(w, r, indexPath)
			return
		}

		cleanPath := path.Clean("/" + strings.TrimPrefix(reqPath, "/"))
		f, err := fs.Open(cleanPath)
		if err == nil {
			defer f.Close()
			if info, err := f.Stat(); err == nil && !info.IsDir() {
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		if ext := strings.ToLower(path.Ext(reqPath)); ext == "" || ext == ".html" {
			serveCustom404Page(w, notFoundHTMLPath)
			return
		}

		http.NotFound(w, r)
	})
}

func isKnownSPARoute(reqPath string) bool {
	cleanPath := path.Clean("/" + strings.TrimPrefix(reqPath, "/"))
	switch cleanPath {
	case "/", "/login", "/register", "/new":
		return true
	}
	if strings.HasPrefix(cleanPath, "/post/") {
		rest := strings.TrimPrefix(cleanPath, "/post/")
		return rest != "" && !strings.Contains(rest, "/")
	}
	return false
}

func serveCustom404Page(w http.ResponseWriter, pagePath string) {
	data, err := os.ReadFile(pagePath)
	if err != nil {
		http.Error(w, "404 page not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write(data)
}

func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

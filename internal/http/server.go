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

const sessionCookieName = "__Host-forum_session"

type Handler struct {
	auth        *service.AuthService
	posts       *service.PostService
	pms         *service.PrivateMessageService
	center      *service.CenterService
	attachments *service.AttachmentService
	moderation  *service.ModerationService
	hub         *realtimews.Hub
	security    *Security
}

func NewHandler(auth *service.AuthService, posts *service.PostService, services ...any) *Handler {
	hub := realtimews.NewHub()
	startHub := true

	var (
		pmService         *service.PrivateMessageService
		centerService     *service.CenterService
		attachmentService *service.AttachmentService
		moderationService *service.ModerationService
		security          *Security
	)
	for _, dependency := range services {
		switch value := dependency.(type) {
		case *service.PrivateMessageService:
			if pmService == nil {
				pmService = value
			}
		case *service.AttachmentService:
			if attachmentService == nil {
				attachmentService = value
			}
		case *service.CenterService:
			if centerService == nil {
				centerService = value
			}
		case *service.ModerationService:
			if moderationService == nil {
				moderationService = value
			}
		case *realtimews.Hub:
			if value != nil {
				hub = value
				startHub = false
			}
		case *Security:
			if value != nil {
				security = value
			}
		}
	}
	if startHub {
		go hub.Run()
	}
	if security == nil {
		security = NewSecurity(SecurityOptions{})
	}

	return &Handler{
		auth:        auth,
		posts:       posts,
		pms:         pmService,
		center:      centerService,
		attachments: attachmentService,
		moderation:  moderationService,
		hub:         hub,
		security:    security,
	}
}

func (h *Handler) Routes(webDir string) http.Handler {
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("/api/register", h.handleRegister)
	apiMux.HandleFunc("/api/login", h.handleLogin)
	apiMux.HandleFunc("/api/logout", h.handleLogout)
	apiMux.HandleFunc("/api/auth/providers", h.handleOAuthProviders)
	apiMux.HandleFunc("/api/auth/flows/", h.handleAuthFlowRoutes)
	apiMux.HandleFunc("/api/me/profile", h.handleMyProfile)
	apiMux.HandleFunc("/api/me", h.handleMe)
	apiMux.HandleFunc("/api/profile/local-account/merge", h.handleLocalMerge)
	apiMux.HandleFunc("/api/profile/linked-accounts/", h.handleLinkedAccountRoutes)
	apiMux.HandleFunc("/api/u/", h.handlePublicProfile)
	apiMux.HandleFunc("/api/users", h.handleUsers)
	apiMux.HandleFunc("/api/attachments", h.handleAttachments)
	apiMux.HandleFunc("/api/attachments/", h.handleAttachmentDownload)
	apiMux.HandleFunc("/api/dm/peers", h.handleDMPeers)
	apiMux.HandleFunc("/api/dm/", h.handleDMConversation)
	apiMux.HandleFunc("/api/center/summary", h.handleCenterSummary)
	apiMux.HandleFunc("/api/center/activity", h.handleCenterActivity)
	apiMux.HandleFunc("/api/center/notifications", h.handleCenterNotifications)
	apiMux.HandleFunc("/api/center/notifications/", h.handleCenterNotificationSubroutes)
	apiMux.HandleFunc("/api/moderation/queue", h.handleModerationQueue)
	apiMux.HandleFunc("/api/moderation/requests", h.handleModerationRequests)
	apiMux.HandleFunc("/api/moderation/requests/", h.handleModerationRequestSubroutes)
	apiMux.HandleFunc("/api/moderation/reports", h.handleModerationReports)
	apiMux.HandleFunc("/api/moderation/reports/", h.handleModerationReportSubroutes)
	apiMux.HandleFunc("/api/moderation/appeals", h.handleModerationAppeals)
	apiMux.HandleFunc("/api/moderation/appeals/", h.handleModerationAppealSubroutes)
	apiMux.HandleFunc("/api/moderation/posts/", h.handleModerationPostSubroutes)
	apiMux.HandleFunc("/api/moderation/comments/", h.handleModerationCommentSubroutes)
	apiMux.HandleFunc("/api/moderation/categories", h.handleModerationCategories)
	apiMux.HandleFunc("/api/moderation/categories/", h.handleModerationCategorySubroutes)
	apiMux.HandleFunc("/api/moderation/history", h.handleModerationHistory)
	apiMux.HandleFunc("/api/moderation/history/purge", h.handleModerationHistoryPurge)
	apiMux.HandleFunc("/api/moderation/users/", h.handleModerationUserSubroutes)
	apiMux.HandleFunc("/api/categories", h.handleCategories)
	apiMux.HandleFunc("/api/posts", h.handlePosts)
	apiMux.HandleFunc("/api/posts/", h.handlePostsSubroutes)
	apiMux.HandleFunc("/api/comments/", h.handleCommentsSubroutes)
	apiMux.HandleFunc("/api/debug/500", h.handleDebug500)
	apiMux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})

	rootMux := http.NewServeMux()
	rootMux.Handle("/api/", h.globalRateLimitMiddleware(h.authMiddleware(h.authEndpointRateLimitMiddleware(h.writeActionRateLimitMiddleware(apiMux)))))
	rootMux.Handle("/auth/", h.globalRateLimitMiddleware(h.authMiddleware(h.authEndpointRateLimitMiddleware(http.HandlerFunc(h.handleOAuthRoutes)))))
	rootMux.Handle("/ws", h.globalRateLimitMiddleware(h.webSocketHandshakeRateLimitMiddleware(h.authMiddleware(http.HandlerFunc(h.handleWS)))))
	rootMux.Handle("/", h.globalRateLimitMiddleware(spaHandler(webDir)))

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
	case "/", "/login", "/register", "/new", "/dm", "/u", "/center", "/account-link", "/account-merge":
		return true
	}
	if strings.HasPrefix(cleanPath, "/post/") {
		rest := strings.TrimPrefix(cleanPath, "/post/")
		return rest != "" && !strings.Contains(rest, "/")
	}
	if strings.HasPrefix(cleanPath, "/dm/") {
		rest := strings.TrimPrefix(cleanPath, "/dm/")
		return rest != "" && !strings.Contains(rest, "/")
	}
	if strings.HasPrefix(cleanPath, "/u/") {
		rest := strings.TrimPrefix(cleanPath, "/u/")
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

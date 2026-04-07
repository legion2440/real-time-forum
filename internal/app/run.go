package app

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	httpserver "forum/internal/http"
	"forum/internal/oauth"
	"forum/internal/platform/clock"
	"forum/internal/platform/id"
	realtimews "forum/internal/realtime/ws"
	"forum/internal/repo"
	"forum/internal/repo/sqlite"
	"forum/internal/service"
)

const (
	defaultDBPath          = "forum.db"
	defaultHTTPAddr        = "127.0.0.1:8080"
	defaultHTTPSAddr       = "127.0.0.1:8443"
	defaultBaseURL         = "https://127.0.0.1:8443"
	defaultTLSCertFile     = "./certs/dev-cert.pem"
	defaultTLSKeyFile      = "./certs/dev-key.pem"
	defaultWebDir          = "web"
	defaultUpload          = "var/uploads"
	defaultShutdownTimeout = 10 * time.Second
)

type runtime struct {
	db          *sql.DB
	httpServer  *http.Server
	httpsServer *http.Server
	hub         *realtimews.Hub
	security    *httpserver.Security
}

type runConfig struct {
	dbPath      string
	httpAddr    string
	httpsAddr   string
	baseURL     string
	webDir      string
	uploadDir   string
	tlsCertFile string
	tlsKeyFile  string
	onListening func(*runtime, net.Addr)
}

type repositories struct {
	users           repo.UserRepo
	sessions        repo.SessionRepo
	authIdentities  repo.AuthIdentityRepo
	authFlows       repo.AuthFlowRepo
	accounts        repo.AccountRepo
	posts           repo.PostRepo
	comments        repo.CommentRepo
	categories      repo.CategoryRepo
	reactions       repo.ReactionRepo
	privateMessages repo.PrivateMessageRepo
	attachments     repo.AttachmentRepo
	center          repo.CenterRepo
	moderation      repo.ModerationRepo
}

type services struct {
	auth            *service.AuthService
	posts           *service.PostService
	privateMessages *service.PrivateMessageService
	center          *service.CenterService
	attachments     *service.AttachmentService
	moderation      *service.ModerationService
	hub             *realtimews.Hub
}

type serverResult struct {
	name string
	err  error
}

func Run() error {
	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	return runWithContext(signalCtx, runConfig{}, stopSignals)
}

func runWithContext(signalCtx context.Context, cfg runConfig, stopSignals func()) error {
	cfg = cfg.withDefaults()

	runtime, err := bootstrap(cfg)
	if err != nil {
		return err
	}
	defer runtime.close()

	httpsListener, err := net.Listen("tcp", cfg.httpsAddr)
	if err != nil {
		return fmt.Errorf("https listen: %w", err)
	}
	runtime.httpsServer.Addr = httpsListener.Addr().String()

	baseURL, err := resolveBaseURL(cfg.baseURL, httpsListener.Addr())
	if err != nil {
		_ = httpsListener.Close()
		return err
	}

	runtime.httpServer = buildHTTPRedirectServer(cfg.httpAddr, baseURL)
	httpListener, err := net.Listen("tcp", cfg.httpAddr)
	if err != nil {
		_ = httpsListener.Close()
		return fmt.Errorf("http listen: %w", err)
	}
	runtime.httpServer.Addr = httpListener.Addr().String()

	serverErrs := make(chan serverResult, 2)
	go serveServer("http redirect", func() error {
		log.Printf("http redirect listening on %s", runtime.httpServer.Addr)
		return runtime.httpServer.Serve(httpListener)
	}, serverErrs)
	go serveServer("https", func() error {
		log.Printf("https server listening on %s", runtime.httpsServer.Addr)
		return runtime.httpsServer.Serve(tls.NewListener(httpsListener, runtime.httpsServer.TLSConfig))
	}, serverErrs)

	if cfg.onListening != nil {
		cfg.onListening(runtime, httpsListener.Addr())
	}

	shutdownStarted := false
	var shutdownErr error
	pendingServers := 2

	for pendingServers > 0 {
		select {
		case result := <-serverErrs:
			pendingServers--

			if result.err == nil && !shutdownStarted {
				result.err = fmt.Errorf("%s server stopped unexpectedly", result.name)
			}
			if result.err != nil {
				if !shutdownStarted {
					if stopSignals != nil {
						stopSignals()
					}
					shutdownStarted = true
					shutdownCtx, cancel := newShutdownContext()
					shutdownErr = runtime.shutdown(shutdownCtx)
					cancel()
				}
				if shutdownErr != nil {
					return shutdownErr
				}
				return result.err
			}

			if pendingServers == 0 {
				return shutdownErr
			}
		case <-signalCtx.Done():
			if shutdownStarted {
				continue
			}
			if stopSignals != nil {
				stopSignals()
			}

			shutdownStarted = true
			shutdownCtx, cancel := newShutdownContext()
			shutdownErr = runtime.shutdown(shutdownCtx)
			cancel()
		}
	}

	return shutdownErr
}

func newShutdownContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), defaultShutdownTimeout)
}

func serveServer(name string, serve func() error, results chan<- serverResult) {
	err := serve()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		results <- serverResult{name: name, err: fmt.Errorf("%s server error: %w", name, err)}
		return
	}
	results <- serverResult{name: name}
}

func (cfg runConfig) withDefaults() runConfig {
	if strings.TrimSpace(cfg.dbPath) == "" {
		cfg.dbPath = os.Getenv("FORUM_DB_PATH")
		if cfg.dbPath == "" {
			cfg.dbPath = defaultDBPath
		}
	}
	if strings.TrimSpace(cfg.httpAddr) == "" {
		cfg.httpAddr = getenvOrDefault("FORUM_HTTP_ADDR", defaultHTTPAddr)
	}
	if strings.TrimSpace(cfg.httpsAddr) == "" {
		cfg.httpsAddr = getenvOrDefault("FORUM_HTTPS_ADDR", defaultHTTPSAddr)
	}
	if strings.TrimSpace(cfg.baseURL) == "" {
		cfg.baseURL = getenvOrDefault("FORUM_BASE_URL", defaultBaseURL)
	}
	if strings.TrimSpace(cfg.tlsCertFile) == "" {
		cfg.tlsCertFile = getenvOrDefault("TLS_CERT_FILE", defaultTLSCertFile)
	}
	if strings.TrimSpace(cfg.tlsKeyFile) == "" {
		cfg.tlsKeyFile = getenvOrDefault("TLS_KEY_FILE", defaultTLSKeyFile)
	}
	if strings.TrimSpace(cfg.webDir) == "" {
		cfg.webDir = defaultWebDir
	}
	if strings.TrimSpace(cfg.uploadDir) == "" {
		cfg.uploadDir = defaultUpload
	}
	return cfg
}

func getenvOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func bootstrap(cfg runConfig) (*runtime, error) {
	db, err := openDB(cfg.dbPath)
	if err != nil {
		return nil, err
	}

	repos := buildRepositories(db)
	services, err := buildServices(repos, cfg.uploadDir)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	security := httpserver.NewSecurity(httpserver.SecurityOptions{StartCleanup: true})
	httpsServer, err := buildHTTPSServer(services, cfg.httpsAddr, cfg.webDir, cfg.tlsCertFile, cfg.tlsKeyFile, security)
	if err != nil {
		security.Close()
		_ = db.Close()
		return nil, err
	}

	return &runtime{
		db:          db,
		httpsServer: httpsServer,
		hub:         services.hub,
		security:    security,
	}, nil
}

func openDB(dbPath string) (*sql.DB, error) {
	db, err := sqlite.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("db open: %w", err)
	}
	return db, nil
}

func buildRepositories(db *sql.DB) repositories {
	return repositories{
		users:           sqlite.NewUserRepo(db),
		sessions:        sqlite.NewSessionRepo(db),
		authIdentities:  sqlite.NewAuthIdentityRepo(db),
		authFlows:       sqlite.NewAuthFlowRepo(db),
		accounts:        sqlite.NewAccountRepo(db),
		posts:           sqlite.NewPostRepo(db),
		comments:        sqlite.NewCommentRepo(db),
		categories:      sqlite.NewCategoryRepo(db),
		reactions:       sqlite.NewReactionRepo(db),
		privateMessages: sqlite.NewPrivateMessageRepo(db),
		attachments:     sqlite.NewAttachmentRepo(db),
		center:          sqlite.NewCenterRepo(db),
		moderation:      sqlite.NewModerationRepo(db),
	}
}

func buildServices(repos repositories, uploadDir string) (services, error) {
	appClock := clock.RealClock{}
	hub := realtimews.NewHub()
	go hub.Run()

	notificationPublisher := realtimews.NewNotificationPublisher(hub)
	attachmentService, err := service.NewAttachmentService(repos.attachments, appClock, id.UUIDGenerator{}, uploadDir)
	if err != nil {
		return services{}, fmt.Errorf("attachments init: %w", err)
	}

	authService := service.NewAuthService(
		repos.users,
		repos.sessions,
		appClock,
		id.UUIDGenerator{},
		24*time.Hour,
		service.WithOAuth(service.OAuthDependencies{
			Providers:  loadOAuthRegistry(),
			Identities: repos.authIdentities,
			Flows:      repos.authFlows,
			Accounts:   repos.accounts,
		}),
	)
	centerService := service.NewCenterService(repos.center, repos.users, repos.posts, repos.comments, appClock, notificationPublisher)
	postService := service.NewPostService(repos.posts, repos.comments, repos.categories, repos.reactions, attachmentService, appClock, centerService)
	privateMessageService := service.NewPrivateMessageService(repos.users, repos.privateMessages, attachmentService, appClock, centerService)
	moderationService := service.NewModerationService(repos.users, repos.posts, repos.comments, repos.categories, repos.moderation, appClock, centerService)
	centerService.SetAppealChecker(moderationService)

	return services{
		auth:            authService,
		posts:           postService,
		privateMessages: privateMessageService,
		center:          centerService,
		attachments:     attachmentService,
		moderation:      moderationService,
		hub:             hub,
	}, nil
}

func buildHTTPSServer(services services, addr, webDir, certFile, keyFile string, security *httpserver.Security) (*http.Server, error) {
	tlsConfig, err := buildTLSConfig(certFile, keyFile)
	if err != nil {
		return nil, err
	}

	handler := httpserver.NewHandler(
		services.auth,
		services.posts,
		services.privateMessages,
		services.center,
		services.attachments,
		services.moderation,
		services.hub,
		security,
	)

	return &http.Server{
		Addr:              addr,
		Handler:           handler.Routes(webDir),
		TLSConfig:         tlsConfig,
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}, nil
}

func buildHTTPRedirectServer(addr string, baseURL *url.URL) *http.Server {
	return &http.Server{
		Addr:    addr,
		Handler: http.HandlerFunc(httpsRedirectHandler(baseURL)),
	}
}

func httpsRedirectHandler(baseURL *url.URL) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, buildHTTPSRedirectLocation(baseURL, r), http.StatusPermanentRedirect)
	}
}

func buildHTTPSRedirectLocation(baseURL *url.URL, r *http.Request) string {
	target := *baseURL
	target.Path = joinURLPath(strings.TrimRight(baseURL.Path, "/"), r.URL.EscapedPath())
	target.RawPath = target.Path
	target.RawQuery = r.URL.RawQuery
	return target.String()
}

func joinURLPath(basePath, requestPath string) string {
	if requestPath == "" {
		requestPath = "/"
	}
	if !strings.HasPrefix(requestPath, "/") {
		requestPath = "/" + requestPath
	}
	if basePath == "" || basePath == "/" {
		return requestPath
	}
	return strings.TrimRight(basePath, "/") + requestPath
}

func buildTLSConfig(certFile, keyFile string) (*tls.Config, error) {
	certFile = strings.TrimSpace(certFile)
	keyFile = strings.TrimSpace(keyFile)
	if certFile == "" {
		return nil, errors.New("TLS_CERT_FILE is required")
	}
	if keyFile == "" {
		return nil, errors.New("TLS_KEY_FILE is required")
	}
	if _, err := os.Stat(certFile); err != nil {
		return nil, fmt.Errorf("tls cert file %q: %w", certFile, err)
	}
	if _, err := os.Stat(keyFile); err != nil {
		return nil, fmt.Errorf("tls key file %q: %w", keyFile, err)
	}

	certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load tls key pair: %w", err)
	}

	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		Certificates: []tls.Certificate{
			certificate,
		},
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
		},
		NextProtos: []string{"h2", "http/1.1"},
	}, nil
}

func resolveBaseURL(rawBaseURL string, httpsAddr net.Addr) (*url.URL, error) {
	rawBaseURL = strings.TrimSpace(rawBaseURL)
	if rawBaseURL == "" {
		rawBaseURL = "https://" + httpsAddr.String()
	}

	baseURL, err := url.Parse(rawBaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse FORUM_BASE_URL: %w", err)
	}
	if !strings.EqualFold(baseURL.Scheme, "https") {
		return nil, errors.New("FORUM_BASE_URL must use https")
	}
	if strings.TrimSpace(baseURL.Host) == "" {
		return nil, errors.New("FORUM_BASE_URL must include host")
	}
	baseURL.RawQuery = ""
	baseURL.Fragment = ""
	baseURL.Path = strings.TrimRight(baseURL.Path, "/")
	return baseURL, nil
}

func loadOAuthRegistry() *oauth.Registry {
	providers := make([]oauth.Provider, 0, 3)

	if provider, err := oauth.NewGoogleProvider(oauth.ProviderConfig{
		ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		RedirectURL:  os.Getenv("GOOGLE_REDIRECT_URL"),
	}); err == nil {
		providers = append(providers, provider)
	}

	if provider, err := oauth.NewGitHubProvider(oauth.ProviderConfig{
		ClientID:     os.Getenv("GITHUB_CLIENT_ID"),
		ClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
		RedirectURL:  os.Getenv("GITHUB_REDIRECT_URL"),
	}); err == nil {
		providers = append(providers, provider)
	}

	if provider, err := oauth.NewFacebookProvider(oauth.ProviderConfig{
		ClientID:     os.Getenv("FACEBOOK_CLIENT_ID"),
		ClientSecret: os.Getenv("FACEBOOK_CLIENT_SECRET"),
		RedirectURL:  os.Getenv("FACEBOOK_REDIRECT_URL"),
	}); err == nil {
		providers = append(providers, provider)
	}

	return oauth.NewRegistry(providers...)
}

func (r *runtime) shutdown(ctx context.Context) error {
	if r == nil {
		return nil
	}

	var shutdownErr error
	if r.httpServer != nil {
		shutdownErr = errors.Join(shutdownErr, shutdownHTTPServer(ctx, "http redirect", r.httpServer))
	}
	if r.httpsServer != nil {
		shutdownErr = errors.Join(shutdownErr, shutdownHTTPServer(ctx, "https", r.httpsServer))
	}
	if r.hub != nil {
		r.hub.Stop()
	}
	if err := waitForHubShutdown(ctx, r.hub); err != nil {
		shutdownErr = errors.Join(shutdownErr, fmt.Errorf("realtime shutdown: %w", err))
	}
	if r.security != nil {
		r.security.Close()
	}
	return shutdownErr
}

func shutdownHTTPServer(ctx context.Context, name string, server *http.Server) error {
	if server == nil {
		return nil
	}
	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("%s server shutdown: %w", name, err)
	}
	return nil
}

func waitForHubShutdown(ctx context.Context, hub *realtimews.Hub) error {
	if hub == nil {
		return nil
	}

	done := hub.Done()
	if done == nil {
		return nil
	}

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *runtime) close() {
	if r == nil {
		return
	}
	if r.security != nil {
		r.security.Close()
	}
	if r.db != nil {
		_ = r.db.Close()
	}
}

package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
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
	defaultAddr            = ":8080"
	defaultWebDir          = "web"
	defaultUpload          = "var/uploads"
	defaultShutdownTimeout = 10 * time.Second
)

type runtime struct {
	db     *sql.DB
	server *http.Server
	hub    *realtimews.Hub
}

type runConfig struct {
	dbPath      string
	addr        string
	webDir      string
	uploadDir   string
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
}

type services struct {
	auth            *service.AuthService
	posts           *service.PostService
	privateMessages *service.PrivateMessageService
	center          *service.CenterService
	attachments     *service.AttachmentService
	hub             *realtimews.Hub
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

	listener, err := net.Listen("tcp", cfg.addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	runtime.server.Addr = listener.Addr().String()

	serverErr := make(chan error, 1)
	go func() {
		log.Printf("server listening on %s", runtime.server.Addr)
		if err := runtime.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- fmt.Errorf("server error: %w", err)
			return
		}
		serverErr <- nil
	}()

	if cfg.onListening != nil {
		cfg.onListening(runtime, listener.Addr())
	}

	select {
	case err := <-serverErr:
		return err
	case <-signalCtx.Done():
		if stopSignals != nil {
			stopSignals()
		}

		shutdownCtx, cancel := context.WithTimeout(context.Background(), defaultShutdownTimeout)
		defer cancel()

		if err := runtime.shutdown(shutdownCtx); err != nil {
			return err
		}
		return <-serverErr
	}
}

func (cfg runConfig) withDefaults() runConfig {
	if strings.TrimSpace(cfg.dbPath) == "" {
		cfg.dbPath = os.Getenv("FORUM_DB_PATH")
		if cfg.dbPath == "" {
			cfg.dbPath = defaultDBPath
		}
	}
	if strings.TrimSpace(cfg.addr) == "" {
		cfg.addr = defaultAddr
	}
	if strings.TrimSpace(cfg.webDir) == "" {
		cfg.webDir = defaultWebDir
	}
	if strings.TrimSpace(cfg.uploadDir) == "" {
		cfg.uploadDir = defaultUpload
	}
	return cfg
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

	return &runtime{
		db:     db,
		server: buildServer(services, cfg.addr, cfg.webDir),
		hub:    services.hub,
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

	return services{
		auth:            authService,
		posts:           postService,
		privateMessages: privateMessageService,
		center:          centerService,
		attachments:     attachmentService,
		hub:             hub,
	}, nil
}

func buildServer(services services, addr, webDir string) *http.Server {
	handler := httpserver.NewHandler(
		services.auth,
		services.posts,
		services.privateMessages,
		services.center,
		services.attachments,
		services.hub,
	)

	return &http.Server{
		Addr:    addr,
		Handler: handler.Routes(webDir),
	}
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

	if r.hub != nil {
		r.hub.Stop()
	}
	if r.server != nil {
		if err := r.server.Shutdown(ctx); err != nil {
			return fmt.Errorf("server shutdown: %w", err)
		}
	}
	if err := waitForHubShutdown(ctx, r.hub); err != nil {
		return fmt.Errorf("realtime shutdown: %w", err)
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
	if r == nil || r.db == nil {
		return
	}
	_ = r.db.Close()
}

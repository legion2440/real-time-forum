package app

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
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
	defaultDBPath = "forum.db"
	defaultAddr   = ":8080"
	defaultWebDir = "web"
	defaultUpload = "var/uploads"
)

type runtime struct {
	db     *sql.DB
	server *http.Server
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
	runtime, err := bootstrap()
	if err != nil {
		return err
	}
	defer runtime.close()

	log.Printf("server listening on %s", runtime.server.Addr)
	if err := runtime.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}

func bootstrap() (*runtime, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}

	repos := buildRepositories(db)
	services, err := buildServices(repos)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	return &runtime{
		db:     db,
		server: buildServer(services),
	}, nil
}

func openDB() (*sql.DB, error) {
	dbPath := os.Getenv("FORUM_DB_PATH")
	if dbPath == "" {
		dbPath = defaultDBPath
	}

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

func buildServices(repos repositories) (services, error) {
	appClock := clock.RealClock{}
	hub := realtimews.NewHub()
	go hub.Run()

	notificationPublisher := realtimews.NewNotificationPublisher(hub)
	attachmentService, err := service.NewAttachmentService(repos.attachments, appClock, id.UUIDGenerator{}, defaultUpload)
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

func buildServer(services services) *http.Server {
	handler := httpserver.NewHandler(
		services.auth,
		services.posts,
		services.privateMessages,
		services.center,
		services.attachments,
		services.hub,
	)

	return &http.Server{
		Addr:    defaultAddr,
		Handler: handler.Routes(defaultWebDir),
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

func (r *runtime) close() {
	if r == nil || r.db == nil {
		return
	}
	_ = r.db.Close()
}

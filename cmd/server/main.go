package main

import (
	"log"
	"net/http"
	"os"
	"time"

	httpserver "forum/internal/http"
	"forum/internal/oauth"
	"forum/internal/platform/clock"
	"forum/internal/platform/id"
	realtimews "forum/internal/realtime/ws"
	"forum/internal/repo/sqlite"
	"forum/internal/service"
)

func main() {
	dbPath := os.Getenv("FORUM_DB_PATH")
	if dbPath == "" {
		dbPath = "forum.db"
	}

	db, err := sqlite.Open(dbPath)
	if err != nil {
		log.Fatalf("db open: %v", err)
	}
	defer db.Close()

	userRepo := sqlite.NewUserRepo(db)
	sessionRepo := sqlite.NewSessionRepo(db)
	authIdentityRepo := sqlite.NewAuthIdentityRepo(db)
	authFlowRepo := sqlite.NewAuthFlowRepo(db)
	accountRepo := sqlite.NewAccountRepo(db)
	postRepo := sqlite.NewPostRepo(db)
	commentRepo := sqlite.NewCommentRepo(db)
	categoryRepo := sqlite.NewCategoryRepo(db)
	reactionRepo := sqlite.NewReactionRepo(db)
	privateMessageRepo := sqlite.NewPrivateMessageRepo(db)
	attachmentRepo := sqlite.NewAttachmentRepo(db)
	centerRepo := sqlite.NewCenterRepo(db)

	clock := clock.RealClock{}
	hub := realtimews.NewHub()
	go hub.Run()
	notificationPublisher := realtimews.NewNotificationPublisher(hub)
	attachmentService, err := service.NewAttachmentService(attachmentRepo, clock, id.UUIDGenerator{}, "var/uploads")
	if err != nil {
		log.Fatalf("attachments init: %v", err)
	}
	authService := service.NewAuthService(
		userRepo,
		sessionRepo,
		clock,
		id.UUIDGenerator{},
		24*time.Hour,
		service.WithOAuth(service.OAuthDependencies{
			Providers:  loadOAuthRegistry(),
			Identities: authIdentityRepo,
			Flows:      authFlowRepo,
			Accounts:   accountRepo,
		}),
	)
	centerService := service.NewCenterService(centerRepo, userRepo, postRepo, commentRepo, clock, notificationPublisher)
	postService := service.NewPostService(postRepo, commentRepo, categoryRepo, reactionRepo, attachmentService, clock, centerService)
	privateMessageService := service.NewPrivateMessageService(userRepo, privateMessageRepo, attachmentService, clock, centerService)

	handler := httpserver.NewHandler(authService, postService, privateMessageService, centerService, attachmentService, hub)

	srv := &http.Server{
		Addr:    ":8080",
		Handler: handler.Routes("web"),
	}

	log.Println("server listening on :8080")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
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

package main

import (
	"log"
	"net/http"
	"os"
	"time"

	httpserver "forum/internal/http"
	"forum/internal/platform/clock"
	"forum/internal/platform/id"
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
	postRepo := sqlite.NewPostRepo(db)
	commentRepo := sqlite.NewCommentRepo(db)
	categoryRepo := sqlite.NewCategoryRepo(db)
	reactionRepo := sqlite.NewReactionRepo(db)

	clock := clock.RealClock{}
	authService := service.NewAuthService(userRepo, sessionRepo, clock, id.UUIDGenerator{}, 24*time.Hour)
	postService := service.NewPostService(postRepo, commentRepo, categoryRepo, reactionRepo, clock)

	handler := httpserver.NewHandler(authService, postService)

	srv := &http.Server{
		Addr:    ":8080",
		Handler: handler.Routes("web"),
	}

	log.Println("server listening on :8080")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

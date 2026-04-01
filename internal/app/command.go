package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"forum/internal/service"
)

func Execute(args []string) error {
	if len(args) == 0 {
		return Run()
	}

	switch strings.TrimSpace(args[0]) {
	case "serve":
		return Run()
	case "bootstrap-owner":
		return runBootstrapOwner(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runBootstrapOwner(args []string) error {
	fs := flag.NewFlagSet("bootstrap-owner", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)

	var (
		dbPath   string
		email    string
		username string
		password string
	)
	fs.StringVar(&dbPath, "db", "", "path to SQLite database")
	fs.StringVar(&email, "email", "", "owner email")
	fs.StringVar(&username, "username", "", "owner username")
	fs.StringVar(&password, "password", "", "owner password")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(email) == "" || strings.TrimSpace(username) == "" || strings.TrimSpace(password) == "" {
		return errors.New("bootstrap-owner requires --email, --username and --password")
	}

	cfg := (runConfig{dbPath: dbPath}).withDefaults()
	db, err := openDB(cfg.dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	repos := buildRepositories(db)
	services, err := buildServices(repos, cfg.uploadDir)
	if err != nil {
		return err
	}
	defer func() {
		if services.hub != nil {
			services.hub.Stop()
		}
	}()

	owner, err := services.moderation.BootstrapOwner(context.Background(), email, username, password)
	if err != nil {
		if errors.Is(err, service.ErrBootstrapUnavailable) {
			return fmt.Errorf("bootstrap owner unavailable: admin or owner already exists")
		}
		return err
	}

	fmt.Fprintf(os.Stdout, "owner created: id=%d username=%s role=%s\n", owner.ID, owner.Username, owner.Role)
	return nil
}

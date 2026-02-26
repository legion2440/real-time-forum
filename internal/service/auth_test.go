package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"forum/internal/repo/sqlite"
)

type fixedClock struct {
	t time.Time
}

func (f fixedClock) Now() time.Time {
	return f.t
}

type seqID struct {
	n int
}

func (s *seqID) New() (string, error) {
	s.n++
	return fmt.Sprintf("token-%d", s.n), nil
}

func newAuthService(t *testing.T) (*AuthService, func()) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := sqlite.Open(dbPath)
	if err != nil {
		if strings.Contains(err.Error(), "requires cgo to work") {
			t.Skip("sqlite3 driver requires cgo")
		}
		t.Fatalf("open db: %v", err)
	}

	users := sqlite.NewUserRepo(db)
	sessions := sqlite.NewSessionRepo(db)

	svc := NewAuthService(users, sessions, fixedClock{t: time.Unix(1700000000, 0)}, &seqID{}, 24*time.Hour)
	return svc, func() { _ = db.Close() }
}

func TestAuthService_RegisterConflict(t *testing.T) {
	svc, cleanup := newAuthService(t)
	defer cleanup()

	_, err := svc.Register(context.Background(), "a@example.com", "usera", "pass")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	_, err = svc.Register(context.Background(), "a@example.com", "userb", "pass")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestAuthService_LoginInvalidatesSession(t *testing.T) {
	svc, cleanup := newAuthService(t)
	defer cleanup()

	_, err := svc.Register(context.Background(), "b@example.com", "userb", "pass")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	session1, _, err := svc.Login(context.Background(), "b@example.com", "", "pass")
	if err != nil {
		t.Fatalf("login1: %v", err)
	}

	session2, _, err := svc.Login(context.Background(), "b@example.com", "", "pass")
	if err != nil {
		t.Fatalf("login2: %v", err)
	}

	if session1.Token == session2.Token {
		t.Fatalf("expected different token")
	}

	if _, err := svc.GetSession(context.Background(), session1.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected old session unauthorized, got %v", err)
	}

	if _, err := svc.GetSession(context.Background(), session2.Token); err != nil {
		t.Fatalf("expected new session valid, got %v", err)
	}
}

package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"forum/internal/repo/sqlite"
	"forum/internal/service"
)

func TestDMWithoutSessionReturnsUnauthorized(t *testing.T) {
	h := NewHandler(nil, nil)
	handler := h.Routes(t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/api/dm/2", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}

	if !strings.Contains(rec.Body.String(), `"error":"unauthorized"`) {
		t.Fatalf("expected unauthorized json body, got %q", rec.Body.String())
	}
}

func TestDMPeersWithoutSessionReturnsUnauthorized(t *testing.T) {
	h := NewHandler(nil, nil)
	handler := h.Routes(t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/api/dm/peers", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}

	if !strings.Contains(rec.Body.String(), `"error":"unauthorized"`) {
		t.Fatalf("expected unauthorized json body, got %q", rec.Body.String())
	}
}

func newDMHandler(t *testing.T) (*Handler, *service.AuthService, *sqlite.PrivateMessageRepo, func()) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sqlite.Open(dbPath)
	if err != nil {
		if strings.Contains(err.Error(), "requires cgo to work") {
			t.Skip("sqlite3 driver requires cgo")
		}
		t.Fatalf("open db: %v", err)
	}

	users := sqlite.NewUserRepo(db)
	auth := service.NewAuthService(
		users,
		sqlite.NewSessionRepo(db),
		fixedClock{t: time.Unix(1700000000, 0)},
		staticID{token: "session-token"},
		24*time.Hour,
	)
	pms := sqlite.NewPrivateMessageRepo(db)

	return NewHandler(auth, nil, service.NewPrivateMessageService(users, pms, nil, fixedClock{t: time.Unix(1700000000, 0)})), auth, pms, func() { _ = db.Close() }
}

func TestDMConversationCursorReturnsOlderMessagesAscending(t *testing.T) {
	h, auth, pms, cleanup := newDMHandler(t)
	defer cleanup()

	meID := mustRegisterUser(t, auth, "me-dm@example.com", "me_dm")
	peerID := mustRegisterUser(t, auth, "peer-dm@example.com", "peer_dm")
	token := mustLoginUser(t, auth, "me-dm@example.com")

	first, err := pms.SavePrivateMessage(context.Background(), meID, peerID, "first", nil, time.Unix(1700000000, 0).UTC())
	if err != nil {
		t.Fatalf("save first message: %v", err)
	}
	second, err := pms.SavePrivateMessage(context.Background(), peerID, meID, "second", nil, time.Unix(1700000010, 0).UTC())
	if err != nil {
		t.Fatalf("save second message: %v", err)
	}
	third, err := pms.SavePrivateMessage(context.Background(), meID, peerID, "third", nil, time.Unix(1700000010, 0).UTC())
	if err != nil {
		t.Fatalf("save third message: %v", err)
	}
	if _, err := pms.SavePrivateMessage(context.Background(), peerID, meID, "fourth", nil, time.Unix(1700000020, 0).UTC()); err != nil {
		t.Fatalf("save fourth message: %v", err)
	}

	handler := h.Routes(t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/dm/"+strconv.FormatInt(peerID, 10)+"?limit=10&beforeTs="+strconv.FormatInt(third.CreatedAt.Unix(), 10)+"&beforeID="+strconv.FormatInt(third.ID, 10), nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token, Path: "/"})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%q", http.StatusOK, rec.Code, rec.Body.String())
	}

	var got []privateMessageDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got))
	}
	if got[0].ID != strconv.FormatInt(first.ID, 10) || got[1].ID != strconv.FormatInt(second.ID, 10) {
		t.Fatalf("expected ASC response order for older messages, got %+v", got)
	}
}

func TestDMPeersReturnsUnreadCounts(t *testing.T) {
	h, auth, pms, cleanup := newDMHandler(t)
	defer cleanup()

	meID := mustRegisterUser(t, auth, "me-dm-peers@example.com", "me_dm_peers")
	peerID := mustRegisterUser(t, auth, "peer-dm-peers@example.com", "peer_dm_peers")
	idlePeerID := mustRegisterUser(t, auth, "idle-dm-peers@example.com", "idle_dm_peers")
	token := mustLoginUser(t, auth, "me-dm-peers@example.com")

	if _, err := pms.SavePrivateMessage(context.Background(), peerID, meID, "one", nil, time.Unix(1700000100, 0).UTC()); err != nil {
		t.Fatalf("save first unread: %v", err)
	}
	if _, err := pms.SavePrivateMessage(context.Background(), meID, peerID, "outgoing", nil, time.Unix(1700000110, 0).UTC()); err != nil {
		t.Fatalf("save outgoing: %v", err)
	}
	if _, err := pms.SavePrivateMessage(context.Background(), peerID, meID, "two", nil, time.Unix(1700000120, 0).UTC()); err != nil {
		t.Fatalf("save second unread: %v", err)
	}

	handler := h.Routes(t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/dm/peers", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token, Path: "/"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%q", http.StatusOK, rec.Code, rec.Body.String())
	}

	var peers []privateMessagePeerDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &peers); err != nil {
		t.Fatalf("decode peers response: %v", err)
	}

	if len(peers) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(peers))
	}
	if peers[0].ID != strconv.FormatInt(peerID, 10) || peers[0].UnreadCount != 2 {
		t.Fatalf("expected active peer with unreadCount=2 first, got %+v", peers)
	}
	if peers[1].ID != strconv.FormatInt(idlePeerID, 10) || peers[1].UnreadCount != 0 {
		t.Fatalf("expected idle peer unreadCount=0 second, got %+v", peers)
	}
}

func TestDMReadMarksConversationAsRead(t *testing.T) {
	h, auth, pms, cleanup := newDMHandler(t)
	defer cleanup()

	meID := mustRegisterUser(t, auth, "me-dm-read@example.com", "me_dm_read")
	peerID := mustRegisterUser(t, auth, "peer-dm-read@example.com", "peer_dm_read")
	token := mustLoginUser(t, auth, "me-dm-read@example.com")

	message, err := pms.SavePrivateMessage(context.Background(), peerID, meID, "unread", nil, time.Unix(1700000200, 0).UTC())
	if err != nil {
		t.Fatalf("save unread message: %v", err)
	}

	handler := h.Routes(t.TempDir())

	readReq := httptest.NewRequest(http.MethodPost, "/api/dm/"+strconv.FormatInt(peerID, 10)+"/read", strings.NewReader(`{"lastReadMessageId":"`+strconv.FormatInt(message.ID, 10)+`"}`))
	readReq.Header.Set("Content-Type", "application/json")
	readReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token, Path: "/"})
	readRec := httptest.NewRecorder()
	handler.ServeHTTP(readRec, readReq)

	if readRec.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d body=%q", http.StatusNoContent, readRec.Code, readRec.Body.String())
	}

	peersReq := httptest.NewRequest(http.MethodGet, "/api/dm/peers", nil)
	peersReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token, Path: "/"})
	peersRec := httptest.NewRecorder()
	handler.ServeHTTP(peersRec, peersReq)

	if peersRec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%q", http.StatusOK, peersRec.Code, peersRec.Body.String())
	}

	var peers []privateMessagePeerDTO
	if err := json.Unmarshal(peersRec.Body.Bytes(), &peers); err != nil {
		t.Fatalf("decode peers response: %v", err)
	}

	if len(peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(peers))
	}
	if peers[0].UnreadCount != 0 {
		t.Fatalf("expected unreadCount=0 after mark-read, got %+v", peers[0])
	}
}

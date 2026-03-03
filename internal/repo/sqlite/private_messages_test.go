package sqlite

import (
	"context"
	"testing"
	"time"
)

func TestPrivateMessageRepo_ListPeersOrdersByLastMessageThenName(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	users := NewUserRepo(db)
	messages := NewPrivateMessageRepo(db)

	meID := mustCreateUser(t, ctx, users, "me@example.com", "me_user")
	activePeerID := mustCreateUser(t, ctx, users, "active@example.com", "active_peer")
	idlePeerID := mustCreateUser(t, ctx, users, "idle@example.com", "idle_peer")

	if err := users.UpdateProfile(ctx, activePeerID, nil, "", "", 0, "", false); err != nil {
		t.Fatalf("normalize active peer: %v", err)
	}
	if err := users.UpdateProfile(ctx, idlePeerID, nil, "", "", 0, "", false); err != nil {
		t.Fatalf("normalize idle peer: %v", err)
	}

	createdAt := time.Unix(1700000000, 0).UTC()
	if _, err := messages.SavePrivateMessage(ctx, activePeerID, meID, "hello", createdAt); err != nil {
		t.Fatalf("save message: %v", err)
	}

	peers, err := messages.ListPeers(ctx, meID)
	if err != nil {
		t.Fatalf("list peers: %v", err)
	}

	if len(peers) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(peers))
	}

	if peers[0].ID != activePeerID {
		t.Fatalf("expected peer with last message first, got %+v", peers)
	}
	if peers[0].LastMessageAt != createdAt.Unix() {
		t.Fatalf("expected lastMessageAt=%d, got %+v", createdAt.Unix(), peers[0])
	}

	if peers[1].ID != idlePeerID {
		t.Fatalf("expected idle peer second, got %+v", peers)
	}
	if peers[1].LastMessageAt != 0 {
		t.Fatalf("expected idle peer lastMessageAt=0, got %+v", peers[1])
	}
}

func TestPrivateMessageRepo_ListPeersOrdersIdlePeersAlphabetically(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	users := NewUserRepo(db)
	messages := NewPrivateMessageRepo(db)

	meID := mustCreateUser(t, ctx, users, "me2@example.com", "me_user2")
	zetaID := mustCreateUser(t, ctx, users, "zeta@example.com", "zeta")
	alphaID := mustCreateUser(t, ctx, users, "alpha@example.com", "alpha")

	peers, err := messages.ListPeers(ctx, meID)
	if err != nil {
		t.Fatalf("list peers: %v", err)
	}

	if len(peers) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(peers))
	}
	if peers[0].ID != alphaID || peers[1].ID != zetaID {
		t.Fatalf("expected alphabetical order for idle peers, got %+v", peers)
	}
}

func TestPrivateMessageRepo_ListPeersUsesDisplayNameForSecondarySort(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	users := NewUserRepo(db)
	messages := NewPrivateMessageRepo(db)

	meID := mustCreateUser(t, ctx, users, "me3@example.com", "me_user3")
	zetaID := mustCreateUser(t, ctx, users, "zeta2@example.com", "zeta_username")
	alphaID := mustCreateUser(t, ctx, users, "alpha2@example.com", "alpha_username")

	if err := users.UpdateProfile(ctx, zetaID, stringPtr("Zulu"), "", "", 0, "", false); err != nil {
		t.Fatalf("update zeta: %v", err)
	}
	if err := users.UpdateProfile(ctx, alphaID, stringPtr("Alpha"), "", "", 0, "", false); err != nil {
		t.Fatalf("update alpha: %v", err)
	}

	peers, err := messages.ListPeers(ctx, meID)
	if err != nil {
		t.Fatalf("list peers: %v", err)
	}

	if len(peers) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(peers))
	}
	if peers[0].ID != alphaID || peers[1].ID != zetaID {
		t.Fatalf("expected display-name secondary sort order, got %+v", peers)
	}
}

func TestPrivateMessageRepo_ListConversationBeforeReturnsOlderMessages(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	users := NewUserRepo(db)
	messages := NewPrivateMessageRepo(db)

	meID := mustCreateUser(t, ctx, users, "me4@example.com", "me_user4")
	peerID := mustCreateUser(t, ctx, users, "peer4@example.com", "peer_user4")

	first, err := messages.SavePrivateMessage(ctx, meID, peerID, "first", time.Unix(1700000000, 0).UTC())
	if err != nil {
		t.Fatalf("save first message: %v", err)
	}
	second, err := messages.SavePrivateMessage(ctx, peerID, meID, "second", time.Unix(1700000010, 0).UTC())
	if err != nil {
		t.Fatalf("save second message: %v", err)
	}
	third, err := messages.SavePrivateMessage(ctx, meID, peerID, "third", time.Unix(1700000010, 0).UTC())
	if err != nil {
		t.Fatalf("save third message: %v", err)
	}
	if _, err := messages.SavePrivateMessage(ctx, peerID, meID, "fourth", time.Unix(1700000020, 0).UTC()); err != nil {
		t.Fatalf("save fourth message: %v", err)
	}

	history, err := messages.ListConversationBefore(ctx, meID, peerID, third.CreatedAt.Unix(), third.ID, 10)
	if err != nil {
		t.Fatalf("list conversation before: %v", err)
	}

	if len(history) != 2 {
		t.Fatalf("expected 2 messages before cursor, got %d", len(history))
	}
	if history[0].ID != second.ID || history[1].ID != first.ID {
		t.Fatalf("expected DESC order with strict cursor filter, got %+v", history)
	}
}

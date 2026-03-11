package ws

import (
	"encoding/json"
	"testing"
	"time"
)

func TestHandleTypingEventDMDeliversOnlyToPeer(t *testing.T) {
	h := NewHub()
	sender := newReadyTestClient(7, "Nazar")
	peer := newReadyTestClient(42, "Rauan")
	other := newReadyTestClient(99, "Other")

	registerTestClient(h, sender)
	registerTestClient(h, peer)
	registerTestClient(h, other)

	now := time.Now().UTC()
	h.handleTypingEvent(typingEvent{
		client:   sender,
		scope:    typingScopeDM,
		targetID: peer.userID,
		kind:     typingEventStart,
	}, now)

	start := readTypingUpdate(t, peer.send)
	if start.Type != "typing:update" || start.Scope != typingScopeDM || start.TargetID != "42" || start.Status != typingStatusStart {
		t.Fatalf("unexpected start payload: %+v", start)
	}
	if start.User.ID != "7" || start.User.Name != "Nazar" {
		t.Fatalf("unexpected sender identity in payload: %+v", start.User)
	}

	assertNoMessage(t, sender.send)
	assertNoMessage(t, other.send)

	h.handleTypingEvent(typingEvent{
		client:   sender,
		scope:    typingScopeDM,
		targetID: peer.userID,
		kind:     typingEventStop,
	}, now)

	stop := readTypingUpdate(t, peer.send)
	if stop.Status != typingStatusStop {
		t.Fatalf("expected stop status, got %+v", stop)
	}
}

func TestHandleTypingEventPostRequiresSubscription(t *testing.T) {
	h := NewHub()
	author := newReadyTestClient(11, "Author")
	viewer := newReadyTestClient(12, "Viewer")

	registerTestClient(h, author)
	registerTestClient(h, viewer)

	now := time.Now().UTC()
	h.handleTypingEvent(typingEvent{
		client:   author,
		scope:    typingScopePost,
		targetID: 1001,
		kind:     typingEventStart,
	}, now)
	assertNoMessage(t, viewer.send)

	h.subscribeClientToPost(author, 1001, now)
	h.subscribeClientToPost(viewer, 1001, now)

	h.handleTypingEvent(typingEvent{
		client:   author,
		scope:    typingScopePost,
		targetID: 1001,
		kind:     typingEventStart,
	}, now)

	update := readTypingUpdate(t, viewer.send)
	if update.Scope != typingScopePost || update.TargetID != "1001" || update.Status != typingStatusStart {
		t.Fatalf("unexpected post typing payload: %+v", update)
	}
}

func TestTypingStopEmittedOnlyAfterLastClientForUserStops(t *testing.T) {
	h := NewHub()
	clientA := newReadyTestClient(7, "Nazar")
	clientB := newReadyTestClient(7, "Nazar")
	viewer := newReadyTestClient(42, "Rauan")

	registerTestClient(h, clientA)
	registerTestClient(h, clientB)
	registerTestClient(h, viewer)

	postID := int64(501)
	now := time.Now().UTC()
	h.subscribeClientToPost(clientA, postID, now)
	h.subscribeClientToPost(clientB, postID, now)
	h.subscribeClientToPost(viewer, postID, now)

	h.handleTypingEvent(typingEvent{
		client:   clientA,
		scope:    typingScopePost,
		targetID: postID,
		kind:     typingEventStart,
	}, now)
	_ = readTypingUpdate(t, viewer.send)

	h.handleTypingEvent(typingEvent{
		client:   clientB,
		scope:    typingScopePost,
		targetID: postID,
		kind:     typingEventStart,
	}, now)
	assertNoMessage(t, viewer.send)

	h.handleTypingEvent(typingEvent{
		client:   clientA,
		scope:    typingScopePost,
		targetID: postID,
		kind:     typingEventStop,
	}, now)
	assertNoMessage(t, viewer.send)

	h.handleTypingEvent(typingEvent{
		client:   clientB,
		scope:    typingScopePost,
		targetID: postID,
		kind:     typingEventStop,
	}, now)

	stop := readTypingUpdate(t, viewer.send)
	if stop.Status != typingStatusStop {
		t.Fatalf("expected stop after last client, got %+v", stop)
	}
}

func TestExpireTypingSendsStopUpdate(t *testing.T) {
	h := NewHub()
	sender := newReadyTestClient(7, "Nazar")
	peer := newReadyTestClient(42, "Rauan")

	registerTestClient(h, sender)
	registerTestClient(h, peer)

	now := time.Now().UTC()
	h.handleTypingEvent(typingEvent{
		client:   sender,
		scope:    typingScopeDM,
		targetID: peer.userID,
		kind:     typingEventStart,
	}, now)
	_ = readTypingUpdate(t, peer.send)

	h.expireTyping(now.Add(typingTTL + 50*time.Millisecond))

	stop := readTypingUpdate(t, peer.send)
	if stop.Status != typingStatusStop {
		t.Fatalf("expected stop from expiry, got %+v", stop)
	}
}

func TestSubscribeClientToPostGetsActiveTypingSnapshot(t *testing.T) {
	h := NewHub()
	author := newReadyTestClient(7, "Nazar")
	viewer := newReadyTestClient(42, "Rauan")
	lateViewer := newReadyTestClient(13, "Late")

	registerTestClient(h, author)
	registerTestClient(h, viewer)
	registerTestClient(h, lateViewer)

	postID := int64(777)
	now := time.Now().UTC()
	h.subscribeClientToPost(author, postID, now)
	h.subscribeClientToPost(viewer, postID, now)

	h.handleTypingEvent(typingEvent{
		client:   author,
		scope:    typingScopePost,
		targetID: postID,
		kind:     typingEventStart,
	}, now)
	_ = readTypingUpdate(t, viewer.send)

	h.subscribeClientToPost(lateViewer, postID, now.Add(200*time.Millisecond))

	snapshot := readTypingUpdate(t, lateViewer.send)
	if snapshot.Status != typingStatusStart || snapshot.Scope != typingScopePost || snapshot.TargetID != "777" {
		t.Fatalf("unexpected snapshot payload: %+v", snapshot)
	}
}

func newReadyTestClient(userID int64, name string) *Client {
	return &Client{
		send:     make(chan []byte, 16),
		userID:   userID,
		userName: name,
		ready:    true,
		done:     make(chan struct{}),
	}
}

func registerTestClient(h *Hub, client *Client) {
	userClients := h.clients[client.userID]
	if userClients == nil {
		userClients = make(map[*Client]struct{})
		h.clients[client.userID] = userClients
	}
	userClients[client] = struct{}{}
}

func readTypingUpdate(t *testing.T, ch <-chan []byte) typingUpdateMessage {
	t.Helper()
	select {
	case payload := <-ch:
		var msg typingUpdateMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			t.Fatalf("failed to decode typing payload: %v", err)
		}
		return msg
	case <-time.After(300 * time.Millisecond):
		t.Fatal("timed out waiting for typing update")
		return typingUpdateMessage{}
	}
}

func assertNoMessage(t *testing.T, ch <-chan []byte) {
	t.Helper()
	select {
	case payload := <-ch:
		t.Fatalf("unexpected message: %s", string(payload))
	case <-time.After(50 * time.Millisecond):
	}
}

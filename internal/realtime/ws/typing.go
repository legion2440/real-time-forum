package ws

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

const (
	typingScopeDM   = "dm"
	typingScopePost = "post"

	typingStatusStart = "start"
	typingStatusStop  = "stop"

	typingTTL           = 5 * time.Second
	typingSweepInterval = time.Second
)

type typingEventKind uint8

const (
	typingEventStart typingEventKind = iota + 1
	typingEventStop
	typingEventHeartbeat
)

type typingEvent struct {
	client   *Client
	scope    string
	targetID int64
	kind     typingEventKind
}

type postSubscription struct {
	client *Client
	postID int64
}

type typingTarget struct {
	scope    string
	targetID int64
}

type typingEntry struct {
	userID       int64
	userName     string
	lastSignalAt time.Time
}

type typingUser struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type typingUpdateMessage struct {
	Type     string     `json:"type"`
	Scope    string     `json:"scope"`
	TargetID string     `json:"targetId"`
	User     typingUser `json:"user"`
	Status   string     `json:"status"`
}

func (h *Hub) handleTypingEvent(event typingEvent, now time.Time) {
	if event.client == nil || !h.isClientRegistered(event.client) {
		return
	}

	target, ok := normalizeTypingTarget(event.scope, event.targetID)
	if !ok {
		return
	}

	switch target.scope {
	case typingScopeDM:
		if target.targetID == event.client.userID {
			return
		}
	case typingScopePost:
		if !h.clientSubscribedToPost(event.client, target.targetID) {
			return
		}
	default:
		return
	}

	switch event.kind {
	case typingEventStart, typingEventHeartbeat:
		h.upsertTyping(event.client, target, now, event.kind)
	case typingEventStop:
		h.removeTypingForClient(event.client, target, true)
	default:
		return
	}
}

func normalizeTypingTarget(scope string, targetID int64) (typingTarget, bool) {
	normalizedScope := strings.ToLower(strings.TrimSpace(scope))
	if targetID <= 0 {
		return typingTarget{}, false
	}

	switch normalizedScope {
	case typingScopeDM, typingScopePost:
		return typingTarget{
			scope:    normalizedScope,
			targetID: targetID,
		}, true
	default:
		return typingTarget{}, false
	}
}

func (h *Hub) upsertTyping(client *Client, target typingTarget, now time.Time, kind typingEventKind) {
	if now.IsZero() {
		now = time.Now().UTC()
	}

	bucket := h.typingByTarget[target]
	if bucket == nil {
		bucket = make(map[*Client]*typingEntry)
		h.typingByTarget[target] = bucket
	}

	wasUserActive := typingBucketHasUser(bucket, client.userID)
	entry, exists := bucket[client]
	if !exists {
		entry = &typingEntry{
			userID:   client.userID,
			userName: normalizeTypingUserName(client.userName, client.userID),
		}
		bucket[client] = entry
		h.trackTypingTarget(client, target)
	}

	entry.userName = normalizeTypingUserName(client.userName, client.userID)
	entry.lastSignalAt = now

	// Emit start on first activation and heartbeat to keep remote clients fresh.
	if !wasUserActive || kind == typingEventHeartbeat {
		h.notifyTypingUpdate(target, entry.userID, entry.userName, typingStatusStart)
	}
}

func (h *Hub) removeTypingForClient(client *Client, target typingTarget, notify bool) {
	bucket, ok := h.typingByTarget[target]
	if !ok {
		return
	}

	entry, ok := bucket[client]
	if !ok {
		return
	}

	delete(bucket, client)
	h.untrackTypingTarget(client, target)
	if len(bucket) == 0 {
		delete(h.typingByTarget, target)
	}

	if !notify {
		return
	}
	if typingBucketHasUser(bucket, entry.userID) {
		return
	}

	h.notifyTypingUpdate(target, entry.userID, entry.userName, typingStatusStop)
}

func (h *Hub) clearClientTyping(client *Client, notify bool) {
	targets := h.typingByClient[client]
	for target := range targets {
		h.removeTypingForClient(client, target, notify)
	}
	delete(h.typingByClient, client)
}

func (h *Hub) expireTyping(now time.Time) {
	if now.IsZero() {
		now = time.Now().UTC()
	}

	for target, bucket := range h.typingByTarget {
		for client, entry := range bucket {
			if now.Sub(entry.lastSignalAt) <= typingTTL {
				continue
			}
			h.removeTypingForClient(client, target, true)
		}
	}
}

func (h *Hub) trackTypingTarget(client *Client, target typingTarget) {
	targets := h.typingByClient[client]
	if targets == nil {
		targets = make(map[typingTarget]struct{})
		h.typingByClient[client] = targets
	}
	targets[target] = struct{}{}
}

func (h *Hub) untrackTypingTarget(client *Client, target typingTarget) {
	targets, ok := h.typingByClient[client]
	if !ok {
		return
	}
	delete(targets, target)
	if len(targets) == 0 {
		delete(h.typingByClient, client)
	}
}

func typingBucketHasUser(bucket map[*Client]*typingEntry, userID int64) bool {
	for _, entry := range bucket {
		if entry.userID == userID {
			return true
		}
	}
	return false
}

func (h *Hub) notifyTypingUpdate(target typingTarget, userID int64, userName, status string) {
	userIDs := h.typingRecipientUserIDs(target, userID)
	if len(userIDs) == 0 {
		return
	}

	payload, err := marshalTypingUpdate(target.scope, target.targetID, userID, userName, status)
	if err != nil {
		return
	}

	h.deliverPayload(userIDs, payload)
}

func (h *Hub) typingRecipientUserIDs(target typingTarget, actorUserID int64) []int64 {
	switch target.scope {
	case typingScopeDM:
		if target.targetID <= 0 || target.targetID == actorUserID {
			return nil
		}
		return []int64{target.targetID}
	case typingScopePost:
		subscribers := h.postSubscribers[target.targetID]
		if len(subscribers) == 0 {
			return nil
		}

		userIDs := make([]int64, 0, len(subscribers))
		seen := make(map[int64]struct{}, len(subscribers))
		for client := range subscribers {
			if client.userID == actorUserID {
				continue
			}
			if _, ok := seen[client.userID]; ok {
				continue
			}
			seen[client.userID] = struct{}{}
			userIDs = append(userIDs, client.userID)
		}
		return userIDs
	default:
		return nil
	}
}

func (h *Hub) subscribeClientToPost(client *Client, postID int64, now time.Time) {
	if client == nil || postID <= 0 || !h.isClientRegistered(client) {
		return
	}

	posts := h.postSubscriptionsByClient[client]
	if posts == nil {
		posts = make(map[int64]struct{})
		h.postSubscriptionsByClient[client] = posts
	}
	if _, ok := posts[postID]; ok {
		return
	}
	posts[postID] = struct{}{}

	subscribers := h.postSubscribers[postID]
	if subscribers == nil {
		subscribers = make(map[*Client]struct{})
		h.postSubscribers[postID] = subscribers
	}
	subscribers[client] = struct{}{}

	h.sendPostTypingSnapshot(client, postID, now)
}

func (h *Hub) unsubscribeClientFromPost(client *Client, postID int64, clearTyping bool) {
	if client == nil || postID <= 0 {
		return
	}

	if posts, ok := h.postSubscriptionsByClient[client]; ok {
		delete(posts, postID)
		if len(posts) == 0 {
			delete(h.postSubscriptionsByClient, client)
		}
	}

	if subscribers, ok := h.postSubscribers[postID]; ok {
		delete(subscribers, client)
		if len(subscribers) == 0 {
			delete(h.postSubscribers, postID)
		}
	}

	if clearTyping {
		h.removeTypingForClient(client, typingTarget{scope: typingScopePost, targetID: postID}, true)
	}
}

func (h *Hub) unsubscribeClientFromAllPosts(client *Client) {
	posts := h.postSubscriptionsByClient[client]
	for postID := range posts {
		h.unsubscribeClientFromPost(client, postID, false)
	}
	delete(h.postSubscriptionsByClient, client)
}

func (h *Hub) clientSubscribedToPost(client *Client, postID int64) bool {
	posts := h.postSubscriptionsByClient[client]
	if len(posts) == 0 {
		return false
	}
	_, ok := posts[postID]
	return ok
}

func (h *Hub) sendTypingSnapshotsForClient(client *Client, now time.Time) {
	if client == nil || !client.ready {
		return
	}

	for postID := range h.postSubscriptionsByClient[client] {
		h.sendPostTypingSnapshot(client, postID, now)
	}
}

func (h *Hub) sendPostTypingSnapshot(client *Client, postID int64, now time.Time) {
	if client == nil || !client.ready {
		return
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	target := typingTarget{
		scope:    typingScopePost,
		targetID: postID,
	}
	bucket := h.typingByTarget[target]
	if len(bucket) == 0 {
		return
	}

	sent := make(map[int64]struct{})
	for _, entry := range bucket {
		if entry.userID == client.userID {
			continue
		}
		if now.Sub(entry.lastSignalAt) > typingTTL {
			continue
		}
		if _, ok := sent[entry.userID]; ok {
			continue
		}
		sent[entry.userID] = struct{}{}

		payload, err := marshalTypingUpdate(target.scope, target.targetID, entry.userID, entry.userName, typingStatusStart)
		if err != nil {
			continue
		}

		if ok := h.sendPayloadToClient(client, payload); !ok {
			return
		}
	}
}

func (h *Hub) sendPayloadToClient(client *Client, payload []byte) bool {
	if client == nil || !client.ready {
		return true
	}

	select {
	case client.send <- payload:
		return true
	default:
		h.unregisterClient(client, true)
		return false
	}
}

func (h *Hub) isClientRegistered(client *Client) bool {
	if client == nil {
		return false
	}
	userClients, ok := h.clients[client.userID]
	if !ok {
		return false
	}
	_, ok = userClients[client]
	return ok
}

func marshalTypingUpdate(scope string, targetID, userID int64, userName, status string) ([]byte, error) {
	return json.Marshal(typingUpdateMessage{
		Type:     "typing:update",
		Scope:    scope,
		TargetID: strconv.FormatInt(targetID, 10),
		User: typingUser{
			ID:   strconv.FormatInt(userID, 10),
			Name: normalizeTypingUserName(userName, userID),
		},
		Status: status,
	})
}

func normalizeTypingUserName(name string, userID int64) string {
	normalized := strings.TrimSpace(name)
	if normalized != "" {
		return normalized
	}
	return "user-" + strconv.FormatInt(userID, 10)
}

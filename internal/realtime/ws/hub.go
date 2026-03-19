package ws

import (
	"encoding/json"
	"strconv"
	"sync"
	"time"
)

type presenceUser struct {
	ID string `json:"id"`
}

type presenceInitMessage struct {
	Type  string         `json:"type"`
	Users []presenceUser `json:"users"`
}

type presenceUpdateMessage struct {
	Type   string       `json:"type"`
	User   presenceUser `json:"user"`
	Status string       `json:"status"`
}

type Hub struct {
	clients                   map[int64]map[*Client]struct{}
	postSubscribers           map[int64]map[*Client]struct{}
	postSubscriptionsByClient map[*Client]map[int64]struct{}
	typingByTarget            map[typingTarget]map[*Client]*typingEntry
	typingByClient            map[*Client]map[typingTarget]struct{}
	register                  chan *Client
	initialize                chan *Client
	unregister                chan *Client
	broadcast                 chan []byte
	deliver                   chan delivery
	typingEvents              chan typingEvent
	postSubscriptions         chan postSubscription
	postUnsubscriptions       chan postSubscription
	stop                      chan struct{}
	done                      chan struct{}
	stopOnce                  sync.Once
}

type delivery struct {
	userIDs []int64
	payload []byte
}

func NewHub() *Hub {
	return &Hub{
		clients:                   make(map[int64]map[*Client]struct{}),
		postSubscribers:           make(map[int64]map[*Client]struct{}),
		postSubscriptionsByClient: make(map[*Client]map[int64]struct{}),
		typingByTarget:            make(map[typingTarget]map[*Client]*typingEntry),
		typingByClient:            make(map[*Client]map[typingTarget]struct{}),
		register:                  make(chan *Client),
		initialize:                make(chan *Client),
		unregister:                make(chan *Client),
		broadcast:                 make(chan []byte),
		deliver:                   make(chan delivery),
		typingEvents:              make(chan typingEvent),
		postSubscriptions:         make(chan postSubscription),
		postUnsubscriptions:       make(chan postSubscription),
		stop:                      make(chan struct{}),
		done:                      make(chan struct{}),
	}
}

func (h *Hub) Run() {
	expiryTicker := time.NewTicker(typingSweepInterval)
	defer func() {
		expiryTicker.Stop()
		h.shutdownClients()
		close(h.done)
	}()

	for {
		select {
		case <-h.stop:
			return
		case client := <-h.register:
			h.registerClient(client)
		case client := <-h.initialize:
			h.initializeClient(client)
		case client := <-h.unregister:
			h.unregisterClient(client, true)
		case payload := <-h.broadcast:
			h.broadcastPayload(payload, nil)
		case item := <-h.deliver:
			h.deliverPayload(item.userIDs, item.payload)
		case event := <-h.typingEvents:
			h.handleTypingEvent(event, time.Now().UTC())
		case subscription := <-h.postSubscriptions:
			h.subscribeClientToPost(subscription.client, subscription.postID, time.Now().UTC())
		case subscription := <-h.postUnsubscriptions:
			h.unsubscribeClientFromPost(subscription.client, subscription.postID, true)
		case now := <-expiryTicker.C:
			h.expireTyping(now.UTC())
		}
	}
}

func (h *Hub) Stop() {
	if h == nil {
		return
	}
	h.stopOnce.Do(func() {
		close(h.stop)
	})
}

func (h *Hub) Done() <-chan struct{} {
	if h == nil {
		return nil
	}
	return h.done
}

func (h *Hub) queueRegister(client *Client) bool {
	return queueHubMessage(h, h.register, client)
}

func (h *Hub) queueInitialize(client *Client) bool {
	return queueHubMessage(h, h.initialize, client)
}

func (h *Hub) queueUnregister(client *Client) bool {
	return queueHubMessage(h, h.unregister, client)
}

func (h *Hub) queueDelivery(item delivery) bool {
	return queueHubMessage(h, h.deliver, item)
}

func (h *Hub) queueTypingEvent(event typingEvent) bool {
	return queueHubMessage(h, h.typingEvents, event)
}

func (h *Hub) queuePostSubscription(item postSubscription, subscribe bool) bool {
	if subscribe {
		return queueHubMessage(h, h.postSubscriptions, item)
	}
	return queueHubMessage(h, h.postUnsubscriptions, item)
}

func (h *Hub) shutdownClients() {
	clients := make([]*Client, 0)
	for _, userClients := range h.clients {
		for client := range userClients {
			clients = append(clients, client)
		}
	}

	for _, client := range clients {
		h.unregisterClient(client, false)
	}
}

func queueHubMessage[T any](h *Hub, ch chan T, value T) bool {
	if h == nil {
		return false
	}

	select {
	case <-h.stop:
		return false
	case <-h.done:
		return false
	case ch <- value:
		return true
	}
}

func (h *Hub) registerClient(client *Client) {
	userClients := h.clients[client.userID]
	firstConnection := len(userClients) == 0
	if userClients == nil {
		userClients = make(map[*Client]struct{})
		h.clients[client.userID] = userClients
	}
	client.ready = false
	userClients[client] = struct{}{}

	if !firstConnection {
		return
	}

	payload, err := marshalPresenceUpdate(client.userID, "online")
	if err != nil {
		return
	}
	h.broadcastPayload(payload, client)
}

func (h *Hub) initializeClient(client *Client) {
	userClients, ok := h.clients[client.userID]
	if !ok {
		return
	}
	if _, ok := userClients[client]; !ok {
		return
	}

	payload, err := marshalPresenceInit(h.onlineUserIDs())
	if err != nil {
		h.unregisterClient(client, true)
		return
	}

	select {
	case client.send <- payload:
		client.ready = true
		h.sendTypingSnapshotsForClient(client, time.Now().UTC())
	default:
		h.unregisterClient(client, true)
	}
}

func (h *Hub) unregisterClient(client *Client, notify bool) {
	client.markUnregistered()
	h.clearClientTyping(client, notify)
	h.unsubscribeClientFromAllPosts(client)

	userClients, ok := h.clients[client.userID]
	if ok {
		delete(userClients, client)
		if len(userClients) == 0 {
			delete(h.clients, client.userID)
			if notify {
				if payload, err := marshalPresenceUpdate(client.userID, "offline"); err == nil {
					h.broadcastPayload(payload, nil)
				}
			}
		}
	}

	client.close()
}

func (h *Hub) broadcastPayload(payload []byte, skip *Client) {
	var stale []*Client

	for _, userClients := range h.clients {
		for client := range userClients {
			if (skip != nil && client == skip) || !client.ready {
				continue
			}
			select {
			case client.send <- payload:
			default:
				stale = append(stale, client)
			}
		}
	}

	for _, client := range stale {
		h.unregisterClient(client, true)
	}
}

func (h *Hub) deliverPayload(userIDs []int64, payload []byte) {
	var stale []*Client
	seen := make(map[int64]struct{})

	for _, userID := range userIDs {
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}

		for client := range h.clients[userID] {
			if !client.ready {
				continue
			}
			select {
			case client.send <- payload:
			default:
				stale = append(stale, client)
			}
		}
	}

	for _, client := range stale {
		h.unregisterClient(client, true)
	}
}

func (h *Hub) onlineUserIDs() []string {
	ids := make([]string, 0, len(h.clients))
	for userID := range h.clients {
		ids = append(ids, strconv.FormatInt(userID, 10))
	}
	return ids
}

func marshalPresenceInit(userIDs []string) ([]byte, error) {
	users := make([]presenceUser, 0, len(userIDs))
	for _, userID := range userIDs {
		if userID == "" {
			continue
		}
		users = append(users, presenceUser{ID: userID})
	}

	return json.Marshal(presenceInitMessage{
		Type:  "presence:init",
		Users: users,
	})
}

func marshalPresenceUpdate(userID int64, status string) ([]byte, error) {
	return json.Marshal(presenceUpdateMessage{
		Type:   "presence:update",
		User:   presenceUser{ID: strconv.FormatInt(userID, 10)},
		Status: status,
	})
}

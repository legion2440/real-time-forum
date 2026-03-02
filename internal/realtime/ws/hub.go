package ws

import (
	"encoding/json"
	"strconv"
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
	clients    map[int64]map[*Client]struct{}
	register   chan *Client
	initialize chan *Client
	unregister chan *Client
	broadcast  chan []byte
	deliver    chan delivery
}

type delivery struct {
	userIDs []int64
	payload []byte
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[int64]map[*Client]struct{}),
		register:   make(chan *Client),
		initialize: make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan []byte),
		deliver:    make(chan delivery),
	}
}

func (h *Hub) Run() {
	for {
		select {
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
		}
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
	default:
		h.unregisterClient(client, true)
	}
}

func (h *Hub) unregisterClient(client *Client, notify bool) {
	client.markUnregistered()

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

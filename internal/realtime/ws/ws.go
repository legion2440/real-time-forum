package ws

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 4096
)

type User struct {
	ID   int64
	Name string
}

type helloMessage struct {
	Type string    `json:"type"`
	User helloUser `json:"user"`
}

type helloUser struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type Client struct {
	conn *websocket.Conn
	send chan []byte
	hub  *Hub

	userID   int64
	userName string
	ready    bool

	done           chan struct{}
	closeOnce      sync.Once
	unregisterOnce sync.Once
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     IsSameOrigin,
}

func IsSameOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return false
	}

	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}

	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return false
	}

	return strings.EqualFold(parsed.Host, r.Host)
}

func ServeWS(w http.ResponseWriter, r *http.Request, hub *Hub, user User) error {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}

	client := &Client{
		conn:     conn,
		send:     make(chan []byte, 8),
		hub:      hub,
		userID:   user.ID,
		userName: strings.TrimSpace(user.Name),
		done:     make(chan struct{}),
	}

	hub.register <- client

	helloPayload, err := json.Marshal(helloMessage{
		Type: "hello",
		User: helloUser{
			ID:   strconv.FormatInt(client.userID, 10),
			Name: client.userName,
		},
	})
	if err != nil {
		client.unregister()
		return err
	}

	client.send <- helloPayload
	hub.initialize <- client

	go client.writePump()
	client.readPump()

	return nil
}

func (c *Client) readPump() {
	defer c.unregister()

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.unregister()
	}()

	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			writer, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			if _, err := writer.Write(message); err != nil {
				_ = writer.Close()
				return
			}
			if err := writer.Close(); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-c.done:
			return
		}
	}
}

func (c *Client) close() {
	c.closeOnce.Do(func() {
		close(c.done)
		close(c.send)
		_ = c.conn.Close()
	})
}

func (c *Client) unregister() {
	c.unregisterOnce.Do(func() {
		c.hub.unregister <- c
	})
}

func (c *Client) markUnregistered() {
	c.unregisterOnce.Do(func() {})
}

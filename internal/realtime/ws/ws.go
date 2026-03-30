package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"forum/internal/domain"
	"forum/internal/service"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 4096

	inboundEventsPerMinute = 120
	typingEventsPerMinute  = 60
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

type pmSendRequest struct {
	Type         string            `json:"type"`
	To           privateMessageRef `json:"to"`
	Body         string            `json:"body"`
	AttachmentID string            `json:"attachmentId,omitempty"`
}

type typingSignalRequest struct {
	Type     string `json:"type"`
	Scope    string `json:"scope"`
	TargetID string `json:"targetId"`
}

type privateMessageRef struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type pmNewMessage struct {
	ID         string                  `json:"id"`
	From       privateMessageRef       `json:"from"`
	To         privateMessageRef       `json:"to"`
	Body       string                  `json:"body"`
	Attachment *privateMessageMediaRef `json:"attachment,omitempty"`
	CreatedAt  time.Time               `json:"createdAt"`
}

type pmNewEnvelope struct {
	Type    string       `json:"type"`
	Message pmNewMessage `json:"message"`
}

type privateMessageMediaRef struct {
	ID   string `json:"id"`
	URL  string `json:"url"`
	Mime string `json:"mime"`
	Size int64  `json:"size"`
}

type Client struct {
	conn *websocket.Conn
	send chan []byte
	hub  *Hub
	pms  *service.PrivateMessageService

	userID   int64
	userName string
	ready    bool

	messageLimiter *perMinuteLimiter
	typingLimiter  *perMinuteLimiter

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

func ServeWS(w http.ResponseWriter, r *http.Request, hub *Hub, pms *service.PrivateMessageService, user User) error {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}

	client := &Client{
		conn:           conn,
		send:           make(chan []byte, 8),
		hub:            hub,
		pms:            pms,
		userID:         user.ID,
		userName:       strings.TrimSpace(user.Name),
		messageLimiter: newPerMinuteLimiter(inboundEventsPerMinute),
		typingLimiter:  newPerMinuteLimiter(typingEventsPerMinute),
		done:           make(chan struct{}),
	}

	helloPayload, err := json.Marshal(helloMessage{
		Type: "hello",
		User: helloUser{
			ID:   strconv.FormatInt(client.userID, 10),
			Name: client.userName,
		},
	})
	if err != nil {
		client.close()
		return err
	}

	client.send <- helloPayload
	if !hub.queueRegister(client) {
		client.close()
		return nil
	}
	if !hub.queueInitialize(client) {
		client.unregister()
		client.close()
		return nil
	}

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
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		c.handleIncomingMessage(raw)
	}
}

func (c *Client) handleIncomingMessage(raw []byte) {
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return
	}
	if !c.allowInboundEvent(envelope.Type) {
		c.closeRateLimited()
		return
	}

	switch envelope.Type {
	case "pm:send":
		c.handlePrivateMessageSend(raw)
	case "typing:start":
		c.handleTypingSignal(raw, typingEventStart)
	case "typing:stop":
		c.handleTypingSignal(raw, typingEventStop)
	case "typing:heartbeat":
		c.handleTypingSignal(raw, typingEventHeartbeat)
	case "typing:subscribe":
		c.handleTypingSubscription(raw, true)
	case "typing:unsubscribe":
		c.handleTypingSubscription(raw, false)
	default:
		return
	}
}

func (c *Client) allowInboundEvent(eventType string) bool {
	if c == nil || c.messageLimiter == nil {
		return true
	}
	if !c.messageLimiter.Allow(time.Now()) {
		return false
	}
	if !isLowPriorityRealtimeEvent(eventType) || c.typingLimiter == nil {
		return true
	}
	return c.typingLimiter.Allow(time.Now())
}

func isLowPriorityRealtimeEvent(eventType string) bool {
	eventType = strings.ToLower(strings.TrimSpace(eventType))
	return strings.HasPrefix(eventType, "typing:") || strings.HasPrefix(eventType, "presence:")
}

func (c *Client) handlePrivateMessageSend(raw []byte) {
	if c.pms == nil {
		return
	}

	var req pmSendRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return
	}

	toID, err := strconv.ParseInt(strings.TrimSpace(req.To.ID), 10, 64)
	if err != nil || toID <= 0 {
		return
	}
	var attachmentID *int64
	if rawAttachmentID := strings.TrimSpace(req.AttachmentID); rawAttachmentID != "" {
		parsedAttachmentID, err := strconv.ParseInt(rawAttachmentID, 10, 64)
		if err != nil || parsedAttachmentID <= 0 {
			return
		}
		attachmentID = &parsedAttachmentID
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	msg, err := c.pms.Send(ctx, c.userID, toID, req.Body, attachmentID)
	if err != nil {
		return
	}

	payload, err := marshalPrivateMessageEvent(*msg, c.userName)
	if err != nil {
		return
	}

	_ = c.hub.queueDelivery(delivery{
		userIDs: []int64{c.userID, toID},
		payload: payload,
	})

	_ = c.hub.queueTypingEvent(typingEvent{
		client:   c,
		scope:    typingScopeDM,
		targetID: toID,
		kind:     typingEventStop,
	})
}

func (c *Client) handleTypingSignal(raw []byte, kind typingEventKind) {
	scope, targetID, ok := parseTypingSignalTarget(raw)
	if !ok {
		return
	}
	if scope == typingScopeDM && targetID == c.userID {
		return
	}

	_ = c.hub.queueTypingEvent(typingEvent{
		client:   c,
		scope:    scope,
		targetID: targetID,
		kind:     kind,
	})
}

func (c *Client) handleTypingSubscription(raw []byte, subscribe bool) {
	scope, targetID, ok := parseTypingSignalTarget(raw)
	if !ok || scope != typingScopePost {
		return
	}

	subscription := postSubscription{
		client: c,
		postID: targetID,
	}

	if subscribe {
		_ = c.hub.queuePostSubscription(subscription, true)
		return
	}
	_ = c.hub.queuePostSubscription(subscription, false)
}

func parseTypingSignalTarget(raw []byte) (string, int64, bool) {
	var req typingSignalRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return "", 0, false
	}

	scope := strings.ToLower(strings.TrimSpace(req.Scope))
	targetID, err := strconv.ParseInt(strings.TrimSpace(req.TargetID), 10, 64)
	if err != nil || targetID <= 0 {
		return "", 0, false
	}

	return scope, targetID, true
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
		if c.conn != nil {
			_ = c.conn.Close()
		}
	})
}

func (c *Client) closeRateLimited() {
	if c == nil || c.conn == nil {
		return
	}
	_ = c.conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "rate limited"),
		time.Now().Add(writeWait),
	)
	c.close()
}

func (c *Client) unregister() {
	c.unregisterOnce.Do(func() {
		if c.hub == nil {
			return
		}
		if !c.hub.queueUnregister(c) {
			c.close()
		}
	})
}

func (c *Client) markUnregistered() {
	c.unregisterOnce.Do(func() {})
}

func marshalPrivateMessageEvent(msg domain.PrivateMessage, senderName string) ([]byte, error) {
	return json.Marshal(pmNewEnvelope{
		Type: "pm:new",
		Message: pmNewMessage{
			ID: strconv.FormatInt(msg.ID, 10),
			From: privateMessageRef{
				ID:   strconv.FormatInt(msg.FromUserID, 10),
				Name: strings.TrimSpace(senderName),
			},
			To: privateMessageRef{
				ID: strconv.FormatInt(msg.ToUserID, 10),
			},
			Body:       msg.Body,
			Attachment: newPrivateMessageMediaRef(msg.Attachment),
			CreatedAt:  msg.CreatedAt.UTC(),
		},
	})
}

func newPrivateMessageMediaRef(attachment *domain.Attachment) *privateMessageMediaRef {
	if attachment == nil || attachment.ID <= 0 {
		return nil
	}
	return &privateMessageMediaRef{
		ID:   strconv.FormatInt(attachment.ID, 10),
		URL:  strings.TrimSpace(attachment.URL),
		Mime: strings.TrimSpace(attachment.Mime),
		Size: attachment.Size,
	}
}

type perMinuteLimiter struct {
	limit       int
	windowStart time.Time
	count       int
}

func newPerMinuteLimiter(limit int) *perMinuteLimiter {
	return &perMinuteLimiter{limit: limit}
}

func (l *perMinuteLimiter) Allow(now time.Time) bool {
	if l == nil || l.limit <= 0 {
		return true
	}

	if l.windowStart.IsZero() || now.Sub(l.windowStart) >= time.Minute {
		l.windowStart = now
		l.count = 0
	}

	if l.count >= l.limit {
		return false
	}

	l.count++
	return true
}

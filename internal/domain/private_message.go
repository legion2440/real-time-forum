package domain

import "time"

type PrivateMessage struct {
	ID              int64
	FromUserID      int64
	FromUsername    string
	FromDisplayName string
	ToUserID        int64
	Body            string
	CreatedAt       time.Time
}

type PrivateMessagePeer struct {
	ID            int64
	Username      string
	DisplayName   string
	LastMessageAt int64
}

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

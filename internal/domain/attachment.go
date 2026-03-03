package domain

import (
	"strconv"
	"time"
)

type Attachment struct {
	ID           int64     `json:"id"`
	OwnerUserID  int64     `json:"-"`
	Mime         string    `json:"mime"`
	Size         int64     `json:"size"`
	StorageKey   string    `json:"-"`
	OriginalName string    `json:"-"`
	CreatedAt    time.Time `json:"-"`
	URL          string    `json:"url"`
}

type AttachmentUsage struct {
	PostID           int64
	PrivateMessageID int64
	FromUserID       int64
	ToUserID         int64
}

func AttachmentURL(id int64) string {
	if id <= 0 {
		return ""
	}
	return "/api/attachments/" + strconv.FormatInt(id, 10)
}

func (a *Attachment) Public() *Attachment {
	if a == nil {
		return nil
	}
	return &Attachment{
		ID:   a.ID,
		Mime: a.Mime,
		Size: a.Size,
		URL:  AttachmentURL(a.ID),
	}
}

func (u AttachmentUsage) HasPrivateMessage() bool {
	return u.PrivateMessageID > 0
}

func (u AttachmentUsage) HasPost() bool {
	return u.PostID > 0
}

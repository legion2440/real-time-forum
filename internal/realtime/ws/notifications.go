package ws

import (
	"encoding/json"

	"forum/internal/domain"
)

type NotificationPublisher struct {
	hub *Hub
}

type notificationEvent struct {
	Type         string                           `json:"type"`
	Notification *domain.NotificationItem         `json:"notification,omitempty"`
	Summary      domain.NotificationUnreadSummary `json:"summary"`
}

func NewNotificationPublisher(hub *Hub) *NotificationPublisher {
	return &NotificationPublisher{hub: hub}
}

func (p *NotificationPublisher) PublishNotificationNew(userID int64, notification domain.NotificationItem, summary domain.NotificationUnreadSummary) {
	p.publish(userID, notificationEvent{
		Type:         "notification:new",
		Notification: &notification,
		Summary:      summary,
	})
}

func (p *NotificationPublisher) PublishNotificationUpdate(userID int64, notification domain.NotificationItem, summary domain.NotificationUnreadSummary) {
	p.publish(userID, notificationEvent{
		Type:         "notification:update",
		Notification: &notification,
		Summary:      summary,
	})
}

func (p *NotificationPublisher) PublishNotificationSummary(userID int64, summary domain.NotificationUnreadSummary) {
	p.publish(userID, notificationEvent{
		Type:    "notification:summary",
		Summary: summary,
	})
}

func (p *NotificationPublisher) publish(userID int64, event notificationEvent) {
	if p == nil || p.hub == nil || userID <= 0 {
		return
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}
	_ = p.hub.queueDelivery(delivery{
		userIDs: []int64{userID},
		payload: payload,
	})
}

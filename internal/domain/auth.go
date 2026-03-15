package domain

import "time"

type AuthIdentity struct {
	ID                    int64
	UserID                int64
	Provider              string
	ProviderUserID        string
	ProviderEmail         string
	ProviderEmailVerified bool
	ProviderDisplayName   string
	ProviderAvatarURL     string
	LinkedAt              time.Time
	LastLoginAt           time.Time
}

type AuthFlow struct {
	Token     string
	Kind      string
	UserID    int64
	Payload   string
	CreatedAt time.Time
	ExpiresAt time.Time
}


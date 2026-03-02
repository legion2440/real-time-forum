package domain

import "time"

type User struct {
	ID                 int64     `json:"id"`
	Email              string    `json:"email"`
	Username           string    `json:"username"`
	DisplayName        string    `json:"display_name,omitempty"`
	PassHash           string    `json:"-"`
	CreatedAt          time.Time `json:"created_at"`
	ProfileInitialized bool      `json:"profile_initialized"`
}

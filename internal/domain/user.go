package domain

import "time"

type UserRole string

const (
	RoleGuest     UserRole = "guest"
	RoleUser      UserRole = "user"
	RoleModerator UserRole = "moderator"
	RoleAdmin     UserRole = "admin"
	RoleOwner     UserRole = "owner"
)

func NormalizeUserRole(value string) UserRole {
	switch UserRole(value) {
	case RoleModerator, RoleAdmin, RoleOwner:
		return UserRole(value)
	default:
		return RoleUser
	}
}

func (r UserRole) Level() int {
	switch r {
	case RoleModerator:
		return 2
	case RoleAdmin:
		return 3
	case RoleOwner:
		return 4
	case RoleGuest:
		return 0
	default:
		return 1
	}
}

func (r UserRole) IsStaff() bool {
	return r == RoleModerator || r == RoleAdmin || r == RoleOwner
}

func (r UserRole) Badge() string {
	switch r {
	case RoleModerator:
		return "moder"
	case RoleAdmin:
		return "admin"
	case RoleOwner:
		return "owner"
	default:
		return ""
	}
}

type User struct {
	ID                 int64     `json:"id"`
	Email              string    `json:"email"`
	Username           string    `json:"username"`
	DisplayName        string    `json:"display_name,omitempty"`
	Role               UserRole  `json:"role"`
	Badges             []string  `json:"badges,omitempty"`
	FirstName          string    `json:"-"`
	LastName           string    `json:"-"`
	Age                int       `json:"-"`
	Gender             string    `json:"-"`
	PassHash           string    `json:"-"`
	CreatedAt          time.Time `json:"created_at"`
	ProfileInitialized bool      `json:"profile_initialized"`
}

func StaffBadgesForRole(role UserRole) []string {
	badge := role.Badge()
	if badge == "" {
		return nil
	}
	return []string{badge}
}

package domain

type Category struct {
	ID       int64  `json:"id"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	IsSystem bool   `json:"is_system"`
}

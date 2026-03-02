package service

import "errors"

var (
	ErrInvalidInput     = errors.New("invalid input")
	ErrUnauthorized     = errors.New("unauthorized")
	ErrNotFound         = errors.New("not found")
	ErrConflict         = errors.New("conflict")
	ErrEmailTaken       = errors.New("email already registered")
	ErrUsernameTaken    = errors.New("username already registered")
	ErrDisplayNameTaken = errors.New("display name already taken")
)

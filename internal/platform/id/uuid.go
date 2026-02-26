package id

import "github.com/google/uuid"

type UUIDGenerator struct{}

func (UUIDGenerator) New() (string, error) {
	return uuid.NewString(), nil
}

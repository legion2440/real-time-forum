package http

import (
	"errors"
	"net/http"

	"forum/internal/service"
)

func handleServiceError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, service.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "invalid input")
	case errors.Is(err, service.ErrUnauthorized):
		writeError(w, http.StatusUnauthorized, "unauthorized")
	case errors.Is(err, service.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, service.ErrEmailTaken):
		writeError(w, http.StatusConflict, "this e-mail already registered")
	case errors.Is(err, service.ErrUsernameTaken):
		writeError(w, http.StatusConflict, "this username already registered")
	case errors.Is(err, service.ErrConflict):
		writeError(w, http.StatusConflict, "conflict")
	default:
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
	return true
}

package http

import (
	"net/http"
	"os"
)

func (h *Handler) handleDebug500(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("DEBUG_500") != "1" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	if r.URL.Path != "/api/debug/500" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	panic("debug 500")
}

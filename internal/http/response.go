package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

type errorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

func writeErrorMessage(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{
		Error:   code,
		Message: message,
	})
}

const sessionEndedMessage = "Session ended (expired, logged out, or logged in elsewhere). Please sign in again."

func writeSessionEndedUnauthorized(w http.ResponseWriter) {
	writeJSON(w, http.StatusUnauthorized, errorResponse{
		Error:   "unauthorized",
		Message: sessionEndedMessage,
	})
}

func writeAuthUnauthorized(w http.ResponseWriter, r *http.Request) {
	if r != nil && sessionEndedFromContext(r.Context()) {
		writeSessionEndedUnauthorized(w)
		return
	}
	writeError(w, http.StatusUnauthorized, "unauthorized")
}

func writeRateLimited(w http.ResponseWriter, retryAfter time.Duration) {
	retryAfterSeconds := retryAfterSeconds(retryAfter)
	w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds))
	writeErrorMessage(w, http.StatusTooManyRequests, "too_many_requests", "Rate limit exceeded.")
}

func writeFailedLoginThrottled(w http.ResponseWriter, retryAfter time.Duration) {
	retryAfterSeconds := retryAfterSeconds(retryAfter)
	w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds))
	writeErrorMessage(
		w,
		http.StatusTooManyRequests,
		"login_throttled",
		fmt.Sprintf("Too many failed login attempts. Try again in %d seconds.", retryAfterSeconds),
	)
}

func retryAfterSeconds(retryAfter time.Duration) int {
	retryAfterSeconds := int(retryAfter / time.Second)
	if retryAfter%time.Second != 0 {
		retryAfterSeconds++
	}
	if retryAfterSeconds <= 0 {
		retryAfterSeconds = 1
	}
	return retryAfterSeconds
}

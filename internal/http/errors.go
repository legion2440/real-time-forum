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
	case errors.Is(err, service.ErrInvalidImageType):
		writeError(w, http.StatusBadRequest, "Only JPEG/PNG/GIF allowed")
	case errors.Is(err, service.ErrImageTooBig):
		writeError(w, http.StatusRequestEntityTooLarge, "image is too big (max 20MB)")
	case errors.Is(err, service.ErrUnauthorized):
		writeError(w, http.StatusUnauthorized, "unauthorized")
	case errors.Is(err, service.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden")
	case errors.Is(err, service.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, service.ErrEmailTaken):
		writeError(w, http.StatusConflict, "this e-mail already registered")
	case errors.Is(err, service.ErrUsernameTaken):
		writeError(w, http.StatusConflict, "this username already registered")
	case errors.Is(err, service.ErrDisplayNameTaken):
		writeError(w, http.StatusBadRequest, "display name already taken")
	case errors.Is(err, service.ErrOAuthProviderUnavailable):
		writeErrorMessage(w, http.StatusNotFound, "oauth_provider_unavailable", "OAuth provider is not configured.")
	case errors.Is(err, service.ErrOAuthProviderReturnedError):
		writeErrorMessage(w, http.StatusBadRequest, "oauth_provider_error", "OAuth provider returned an error.")
	case errors.Is(err, service.ErrOAuthStateInvalid):
		writeErrorMessage(w, http.StatusBadRequest, "oauth_state_invalid", "OAuth state is invalid or expired.")
	case errors.Is(err, service.ErrOAuthCodeMissing):
		writeErrorMessage(w, http.StatusBadRequest, "oauth_code_missing", "OAuth callback is missing the code parameter.")
	case errors.Is(err, service.ErrOAuthTokenExchangeFailed):
		writeErrorMessage(w, http.StatusBadGateway, "oauth_token_exchange_failed", "Failed to exchange OAuth code for token.")
	case errors.Is(err, service.ErrOAuthIdentityFetchFailed):
		writeErrorMessage(w, http.StatusBadGateway, "oauth_identity_fetch_failed", "Failed to fetch OAuth identity from provider.")
	case errors.Is(err, service.ErrOAuthEmailUnavailable):
		writeErrorMessage(w, http.StatusConflict, "oauth_email_unavailable", "Provider did not return a usable email address.")
	case errors.Is(err, service.ErrAuthFlowExpired):
		writeErrorMessage(w, http.StatusGone, "auth_flow_expired", "Authentication flow expired. Start again.")
	case errors.Is(err, service.ErrMergeDenied):
		writeErrorMessage(w, http.StatusConflict, "merge_denied", "Account merge cannot be completed safely.")
	case errors.Is(err, service.ErrUnlinkDenied):
		writeErrorMessage(w, http.StatusConflict, "unlink_denied", "Cannot unlink the last remaining sign-in method.")
	case errors.Is(err, service.ErrAlreadyLinked):
		writeErrorMessage(w, http.StatusConflict, "already_linked", "This provider is already linked to the account.")
	case errors.Is(err, service.ErrConflict):
		writeError(w, http.StatusConflict, "conflict")
	default:
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
	return true
}

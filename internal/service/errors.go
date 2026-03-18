package service

import "errors"

var (
	ErrInvalidInput               = errors.New("invalid input")
	ErrUnauthorized               = errors.New("unauthorized")
	ErrNotFound                   = errors.New("not found")
	ErrForbidden                  = errors.New("forbidden")
	ErrConflict                   = errors.New("conflict")
	ErrEmailTaken                 = errors.New("email already registered")
	ErrUsernameTaken              = errors.New("username already registered")
	ErrDisplayNameTaken           = errors.New("display name already taken")
	ErrCommentEditWindowExpired   = errors.New("comment edit window expired")
	ErrImageTooBig                = errors.New("image is too big")
	ErrInvalidImageType           = errors.New("invalid image type")
	ErrOAuthProviderUnavailable   = errors.New("oauth provider unavailable")
	ErrOAuthProviderReturnedError = errors.New("oauth provider returned error")
	ErrOAuthStateInvalid          = errors.New("oauth state invalid")
	ErrOAuthCodeMissing           = errors.New("oauth code missing")
	ErrOAuthTokenExchangeFailed   = errors.New("oauth token exchange failed")
	ErrOAuthIdentityFetchFailed   = errors.New("oauth identity fetch failed")
	ErrOAuthEmailUnavailable      = errors.New("oauth email unavailable")
	ErrAuthFlowExpired            = errors.New("auth flow expired")
	ErrMergeDenied                = errors.New("account merge denied")
	ErrUnlinkDenied               = errors.New("unlink denied")
	ErrAlreadyLinked              = errors.New("already linked")
)

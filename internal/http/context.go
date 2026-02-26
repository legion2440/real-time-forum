package http

import "context"

type ctxKey string

const (
	ctxUserIDKey    ctxKey = "userID"
	ctxSessionToken ctxKey = "sessionToken"
	ctxSessionEnded ctxKey = "sessionEnded"
)

func withUserID(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, ctxUserIDKey, userID)
}

func withSessionToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, ctxSessionToken, token)
}

func withSessionEnded(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxSessionEnded, true)
}

func userIDFromContext(ctx context.Context) (int64, bool) {
	v := ctx.Value(ctxUserIDKey)
	if v == nil {
		return 0, false
	}
	id, ok := v.(int64)
	return id, ok
}

func sessionTokenFromContext(ctx context.Context) (string, bool) {
	v := ctx.Value(ctxSessionToken)
	if v == nil {
		return "", false
	}
	token, ok := v.(string)
	return token, ok
}

func sessionEndedFromContext(ctx context.Context) bool {
	v := ctx.Value(ctxSessionEnded)
	if v == nil {
		return false
	}
	ended, ok := v.(bool)
	return ok && ended
}

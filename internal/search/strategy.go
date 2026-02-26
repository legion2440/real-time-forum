package search

import "strings"

func normalizeSearchQuery(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return ""
	}
	// Support searching authors via @username while usernames are stored without @.
	if strings.HasPrefix(q, "@") {
		q = strings.TrimLeft(q, "@")
		q = strings.TrimSpace(q)
	}
	return q
}

type PostSearchStrategy interface {
	Clause(q string) (where string, args []any)
}

type CommentSearchStrategy interface {
	Clause(q string) (where string, args []any)
}

type LikePostSearchStrategy struct{}

func (LikePostSearchStrategy) Clause(q string) (string, []any) {
	q = normalizeSearchQuery(q)
	if q == "" {
		return "", nil
	}
	pattern := "%" + q + "%"
	return `(p.title LIKE ? OR p.body LIKE ? OR u.username LIKE ?)`, []any{pattern, pattern, pattern}
}

type LikeCommentSearchStrategy struct{}

func (LikeCommentSearchStrategy) Clause(q string) (string, []any) {
	q = normalizeSearchQuery(q)
	if q == "" {
		return "", nil
	}
	pattern := "%" + q + "%"
	return `(c.body LIKE ? OR u.username LIKE ?)`, []any{pattern, pattern}
}

// TODO: add FTSPostSearchStrategy / HeuristicPostSearchStrategy without changing handlers/services.
// TODO: add FTSCommentSearchStrategy / HeuristicCommentSearchStrategy without changing handlers/services.

package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"forum/internal/domain"
	"forum/internal/repo/sqlite"
	"forum/internal/service"
)

type moderationHandlerFixture struct {
	handler    *Handler
	auth       *service.AuthService
	posts      *service.PostService
	moderation *service.ModerationService
}

type seqToken struct {
	next int
}

func (s *seqToken) New() (string, error) {
	s.next++
	return "session-token-" + strconv.Itoa(s.next), nil
}

func newModerationHandler(t *testing.T) (*moderationHandlerFixture, func()) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sqlite.Open(dbPath)
	if err != nil {
		if strings.Contains(err.Error(), "requires cgo to work") {
			t.Skip("sqlite3 driver requires cgo")
		}
		t.Fatalf("open db: %v", err)
	}

	userRepo := sqlite.NewUserRepo(db)
	sessionRepo := sqlite.NewSessionRepo(db)
	postRepo := sqlite.NewPostRepo(db)
	commentRepo := sqlite.NewCommentRepo(db)
	categoryRepo := sqlite.NewCategoryRepo(db)
	reactionRepo := sqlite.NewReactionRepo(db)
	centerRepo := sqlite.NewCenterRepo(db)
	moderationRepo := sqlite.NewModerationRepo(db)

	testClock := fixedClock{t: time.Unix(1700020000, 0).UTC()}
	authService := service.NewAuthService(userRepo, sessionRepo, testClock, &seqToken{}, 24*time.Hour)
	centerService := service.NewCenterService(centerRepo, userRepo, postRepo, commentRepo, testClock)
	postService := service.NewPostService(postRepo, commentRepo, categoryRepo, reactionRepo, nil, testClock, centerService)
	moderationService := service.NewModerationService(userRepo, postRepo, commentRepo, categoryRepo, moderationRepo, testClock, centerService)
	centerService.SetAppealChecker(moderationService)

	return &moderationHandlerFixture{
		handler:    NewHandler(authService, postService, centerService, moderationService),
		auth:       authService,
		posts:      postService,
		moderation: moderationService,
	}, func() { _ = db.Close() }
}

func performJSONRequest(t *testing.T, h http.Handler, method, path string, payload any, token string) *httptest.ResponseRecorder {
	t.Helper()

	var body []byte
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		body = data
	}

	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token, Path: "/"})
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode body: %v body=%q", err, rec.Body.String())
	}
	return out
}

func TestModerationHTTP_GuestReadOnlyAndUserCanCreateContent(t *testing.T) {
	fixture, cleanup := newModerationHandler(t)
	defer cleanup()

	handler := fixture.handler.Routes(t.TempDir())
	userID := mustRegisterUser(t, fixture.auth, "user-http@example.com", "user_http")
	token := mustLoginUser(t, fixture.auth, "user-http@example.com")

	getPostsRec := performJSONRequest(t, handler, http.MethodGet, "/api/posts", nil, "")
	if getPostsRec.Code != http.StatusOK {
		t.Fatalf("guest list posts status=%d body=%q", getPostsRec.Code, getPostsRec.Body.String())
	}

	guestCreateRec := performJSONRequest(t, handler, http.MethodPost, "/api/posts", map[string]any{
		"title":      "Guest",
		"body":       "Nope",
		"categories": []int64{1},
	}, "")
	if guestCreateRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected guest create post unauthorized, got %d body=%q", guestCreateRec.Code, guestCreateRec.Body.String())
	}

	createPostRec := performJSONRequest(t, handler, http.MethodPost, "/api/posts", map[string]any{
		"title":      "HTTP Post",
		"body":       "Created over HTTP",
		"categories": []int64{1},
	}, token)
	if createPostRec.Code != http.StatusCreated {
		t.Fatalf("user create post status=%d body=%q", createPostRec.Code, createPostRec.Body.String())
	}
	post := decodeBody[map[string]any](t, createPostRec)
	postID := int64(post["id"].(float64))

	commentRec := performJSONRequest(t, handler, http.MethodPost, "/api/posts/"+strconv.FormatInt(postID, 10)+"/comments", map[string]any{
		"body": "HTTP comment",
	}, token)
	if commentRec.Code != http.StatusCreated {
		t.Fatalf("user create comment status=%d body=%q", commentRec.Code, commentRec.Body.String())
	}

	reactRec := performJSONRequest(t, handler, http.MethodPost, "/api/posts/"+strconv.FormatInt(postID, 10)+"/react", map[string]any{
		"value": 1,
	}, token)
	if reactRec.Code != http.StatusOK {
		t.Fatalf("user react post status=%d body=%q", reactRec.Code, reactRec.Body.String())
	}

	meRec := performJSONRequest(t, handler, http.MethodGet, "/api/me", nil, token)
	if meRec.Code != http.StatusOK || !strings.Contains(meRec.Body.String(), `"id":"`+strconv.FormatInt(userID, 10)+`"`) {
		t.Fatalf("expected logged-in me response, got status=%d body=%q", meRec.Code, meRec.Body.String())
	}
}

func TestModerationHTTP_RoleFlowReportsAndDeletedPlaceholders(t *testing.T) {
	fixture, cleanup := newModerationHandler(t)
	defer cleanup()

	ctx := context.Background()
	owner, err := fixture.moderation.BootstrapOwner(ctx, "owner-http@example.com", "owner_http", "secret")
	if err != nil {
		t.Fatalf("bootstrap owner: %v", err)
	}

	adminID := mustRegisterUser(t, fixture.auth, "admin-http@example.com", "admin_http")
	moderatorID := mustRegisterUser(t, fixture.auth, "moderator-http@example.com", "moderator_http")
	mustRegisterUser(t, fixture.auth, "author-http@example.com", "author_http")

	adminToken := mustLoginUser(t, fixture.auth, "admin-http@example.com")
	moderatorToken := mustLoginUser(t, fixture.auth, "moderator-http@example.com")
	authorToken := mustLoginUser(t, fixture.auth, "author-http@example.com")

	if _, err := fixture.moderation.ChangeUserRole(ctx, owner.ID, adminID, domain.RoleAdmin, "bootstrap admin"); err != nil {
		t.Fatalf("promote admin: %v", err)
	}

	handler := fixture.handler.Routes(t.TempDir())

	requestRec := performJSONRequest(t, handler, http.MethodPost, "/api/moderation/requests", map[string]any{
		"requestedRole": "moderator",
		"note":          "ready to moderate",
	}, moderatorToken)
	if requestRec.Code != http.StatusCreated {
		t.Fatalf("create moderator request status=%d body=%q", requestRec.Code, requestRec.Body.String())
	}
	requestBody := decodeBody[map[string]any](t, requestRec)
	requestID := int64(requestBody["id"].(float64))

	adminRequestsRec := performJSONRequest(t, handler, http.MethodGet, "/api/moderation/requests", nil, adminToken)
	if adminRequestsRec.Code != http.StatusOK || !strings.Contains(adminRequestsRec.Body.String(), `"id":`+strconv.FormatInt(requestID, 10)) {
		t.Fatalf("admin should receive moderator request, got status=%d body=%q", adminRequestsRec.Code, adminRequestsRec.Body.String())
	}

	approveRequestRec := performJSONRequest(t, handler, http.MethodPost, "/api/moderation/requests/"+strconv.FormatInt(requestID, 10)+"/review", map[string]any{
		"approve": true,
		"note":    "approved",
	}, adminToken)
	if approveRequestRec.Code != http.StatusOK {
		t.Fatalf("approve moderator request status=%d body=%q", approveRequestRec.Code, approveRequestRec.Body.String())
	}

	moderatorMeRec := performJSONRequest(t, handler, http.MethodGet, "/api/me", nil, moderatorToken)
	if moderatorMeRec.Code != http.StatusOK || !strings.Contains(moderatorMeRec.Body.String(), `"role":"moderator"`) {
		t.Fatalf("expected immediate moderator role, got status=%d body=%q", moderatorMeRec.Code, moderatorMeRec.Body.String())
	}

	obscenePostRec := performJSONRequest(t, handler, http.MethodPost, "/api/posts", map[string]any{
		"title":      "Obscene",
		"body":       "Body",
		"categories": []int64{1},
	}, authorToken)
	if obscenePostRec.Code != http.StatusCreated {
		t.Fatalf("author create obscene post status=%d body=%q", obscenePostRec.Code, obscenePostRec.Body.String())
	}
	obscenePost := decodeBody[map[string]any](t, obscenePostRec)
	obscenePostID := int64(obscenePost["id"].(float64))

	deleteRec := performJSONRequest(t, handler, http.MethodPost, "/api/moderation/posts/"+strconv.FormatInt(obscenePostID, 10)+"/soft-delete", map[string]any{
		"reason": "obscene",
		"note":   "obscene post",
	}, moderatorToken)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("moderator soft delete status=%d body=%q", deleteRec.Code, deleteRec.Body.String())
	}

	guestDeletedRec := performJSONRequest(t, handler, http.MethodGet, "/api/posts/"+strconv.FormatInt(obscenePostID, 10), nil, "")
	if guestDeletedRec.Code != http.StatusOK || !strings.Contains(guestDeletedRec.Body.String(), `"[deleted]"`) {
		t.Fatalf("expected deleted placeholder for guest, got status=%d body=%q", guestDeletedRec.Code, guestDeletedRec.Body.String())
	}

	illegalPostRec := performJSONRequest(t, handler, http.MethodPost, "/api/posts", map[string]any{
		"title":      "Illegal",
		"body":       "Body",
		"categories": []int64{1},
	}, authorToken)
	if illegalPostRec.Code != http.StatusCreated {
		t.Fatalf("author create illegal post status=%d body=%q", illegalPostRec.Code, illegalPostRec.Body.String())
	}
	illegalPost := decodeBody[map[string]any](t, illegalPostRec)
	illegalPostID := int64(illegalPost["id"].(float64))

	reportRec := performJSONRequest(t, handler, http.MethodPost, "/api/moderation/reports", map[string]any{
		"targetType": "post",
		"targetId":   illegalPostID,
		"reason":     "illegal",
		"note":       "needs admin review",
	}, moderatorToken)
	if reportRec.Code != http.StatusCreated {
		t.Fatalf("moderator report status=%d body=%q", reportRec.Code, reportRec.Body.String())
	}
	reportBody := decodeBody[map[string]any](t, reportRec)
	reportID := int64(reportBody["id"].(float64))

	adminReportsRec := performJSONRequest(t, handler, http.MethodGet, "/api/moderation/reports?status=pending", nil, adminToken)
	if adminReportsRec.Code != http.StatusOK || !strings.Contains(adminReportsRec.Body.String(), `"id":`+strconv.FormatInt(reportID, 10)) {
		t.Fatalf("admin should receive moderator report, got status=%d body=%q", adminReportsRec.Code, adminReportsRec.Body.String())
	}

	reportCloseRec := performJSONRequest(t, handler, http.MethodPost, "/api/moderation/reports/"+strconv.FormatInt(reportID, 10)+"/review", map[string]any{
		"actionTaken": true,
		"reason":      "illegal",
		"note":        "removed from queue",
	}, adminToken)
	if reportCloseRec.Code != http.StatusOK {
		t.Fatalf("admin close report status=%d body=%q", reportCloseRec.Code, reportCloseRec.Body.String())
	}

	demoteRec := performJSONRequest(t, handler, http.MethodPost, "/api/moderation/users/"+strconv.FormatInt(moderatorID, 10)+"/role", map[string]any{
		"role": "user",
		"note": "demote after review",
	}, adminToken)
	if demoteRec.Code != http.StatusOK {
		t.Fatalf("admin demote moderator status=%d body=%q", demoteRec.Code, demoteRec.Body.String())
	}

	demotedMeRec := performJSONRequest(t, handler, http.MethodGet, "/api/me", nil, moderatorToken)
	if demotedMeRec.Code != http.StatusOK || !strings.Contains(demotedMeRec.Body.String(), `"role":"user"`) {
		t.Fatalf("expected immediate user role after demotion, got status=%d body=%q", demotedMeRec.Code, demotedMeRec.Body.String())
	}
}

func TestModerationHTTP_DeletedNotificationAppealActionAndDelete(t *testing.T) {
	fixture, cleanup := newModerationHandler(t)
	defer cleanup()

	ctx := context.Background()
	owner, err := fixture.moderation.BootstrapOwner(ctx, "owner-center-http@example.com", "owner_center_http", "secret")
	if err != nil {
		t.Fatalf("bootstrap owner: %v", err)
	}

	moderatorID := mustRegisterUser(t, fixture.auth, "moderator-center-http@example.com", "moderator_center_http")
	mustRegisterUser(t, fixture.auth, "author-center-http@example.com", "author_center_http")
	mustRegisterUser(t, fixture.auth, "other-center-http@example.com", "other_center_http")

	if _, err := fixture.moderation.ChangeUserRole(ctx, owner.ID, moderatorID, domain.RoleModerator, "promote moderator"); err != nil {
		t.Fatalf("promote moderator: %v", err)
	}

	moderatorToken := mustLoginUser(t, fixture.auth, "moderator-center-http@example.com")
	authorToken := mustLoginUser(t, fixture.auth, "author-center-http@example.com")
	otherToken := mustLoginUser(t, fixture.auth, "other-center-http@example.com")
	handler := fixture.handler.Routes(t.TempDir())

	createPostRec := performJSONRequest(t, handler, http.MethodPost, "/api/posts", map[string]any{
		"title":      "Comment target",
		"body":       "body",
		"categories": []int64{1},
	}, authorToken)
	if createPostRec.Code != http.StatusCreated {
		t.Fatalf("create post status=%d body=%q", createPostRec.Code, createPostRec.Body.String())
	}
	post := decodeBody[map[string]any](t, createPostRec)
	postID := int64(post["id"].(float64))

	createCommentRec := performJSONRequest(t, handler, http.MethodPost, "/api/posts/"+strconv.FormatInt(postID, 10)+"/comments", map[string]any{
		"body": "comment hidden from thread",
	}, authorToken)
	if createCommentRec.Code != http.StatusCreated {
		t.Fatalf("create comment status=%d body=%q", createCommentRec.Code, createCommentRec.Body.String())
	}
	comment := decodeBody[map[string]any](t, createCommentRec)
	commentID := int64(comment["id"].(float64))

	deleteRec := performJSONRequest(t, handler, http.MethodPost, "/api/moderation/comments/"+strconv.FormatInt(commentID, 10)+"/soft-delete", map[string]any{
		"reason": "obscene",
		"note":   "delete hidden thread",
	}, moderatorToken)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("soft delete comment status=%d body=%q", deleteRec.Code, deleteRec.Body.String())
	}

	authorNotificationsRec := performJSONRequest(t, handler, http.MethodGet, "/api/center/notifications?bucket=deleted", nil, authorToken)
	if authorNotificationsRec.Code != http.StatusOK {
		t.Fatalf("author notifications status=%d body=%q", authorNotificationsRec.Code, authorNotificationsRec.Body.String())
	}
	notifications := decodeBody[domain.NotificationList](t, authorNotificationsRec)
	if len(notifications.Items) != 1 {
		t.Fatalf("expected one deleted notification, got %+v", notifications.Items)
	}
	item := notifications.Items[0]
	if item.Type != "content_deleted" || item.EntityType != domain.NotificationEntityTypeComment || item.EntityID != commentID {
		t.Fatalf("unexpected deleted notification item: %+v", item)
	}
	if !item.CanAppeal {
		t.Fatalf("expected deleted notification to expose appeal action, got %+v", item)
	}
	if !strings.Contains(item.CommentPreview, "comment hidden from thread") {
		t.Fatalf("expected deleted notification to include comment preview, got %+v", item)
	}

	otherNotificationsRec := performJSONRequest(t, handler, http.MethodGet, "/api/center/notifications?bucket=deleted", nil, otherToken)
	if otherNotificationsRec.Code != http.StatusOK {
		t.Fatalf("other notifications status=%d body=%q", otherNotificationsRec.Code, otherNotificationsRec.Body.String())
	}
	otherNotifications := decodeBody[domain.NotificationList](t, otherNotificationsRec)
	if len(otherNotifications.Items) != 0 {
		t.Fatalf("expected non-author to have no deleted notification action entrypoint, got %+v", otherNotifications.Items)
	}

	appealRec := performJSONRequest(t, handler, http.MethodPost, "/api/moderation/appeals", map[string]any{
		"targetType": item.EntityType,
		"targetId":   item.EntityID,
		"note":       "appeal via deleted notification",
	}, authorToken)
	if appealRec.Code != http.StatusCreated {
		t.Fatalf("create appeal status=%d body=%q", appealRec.Code, appealRec.Body.String())
	}

	updatedNotificationsRec := performJSONRequest(t, handler, http.MethodGet, "/api/center/notifications?bucket=deleted", nil, authorToken)
	if updatedNotificationsRec.Code != http.StatusOK {
		t.Fatalf("updated notifications status=%d body=%q", updatedNotificationsRec.Code, updatedNotificationsRec.Body.String())
	}
	updatedNotifications := decodeBody[domain.NotificationList](t, updatedNotificationsRec)
	if len(updatedNotifications.Items) != 1 || updatedNotifications.Items[0].CanAppeal {
		t.Fatalf("expected appeal action to disappear after pending appeal, got %+v", updatedNotifications.Items)
	}

	otherDeleteRec := performJSONRequest(t, handler, http.MethodDelete, "/api/center/notifications/"+strconv.FormatInt(item.ID, 10), nil, otherToken)
	if otherDeleteRec.Code != http.StatusNotFound {
		t.Fatalf("expected deleting foreign notification to return 404, got status=%d body=%q", otherDeleteRec.Code, otherDeleteRec.Body.String())
	}

	deleteNotificationRec := performJSONRequest(t, handler, http.MethodDelete, "/api/center/notifications/"+strconv.FormatInt(item.ID, 10), nil, authorToken)
	if deleteNotificationRec.Code != http.StatusOK {
		t.Fatalf("delete notification status=%d body=%q", deleteNotificationRec.Code, deleteNotificationRec.Body.String())
	}

	finalNotificationsRec := performJSONRequest(t, handler, http.MethodGet, "/api/center/notifications?bucket=deleted", nil, authorToken)
	if finalNotificationsRec.Code != http.StatusOK {
		t.Fatalf("final notifications status=%d body=%q", finalNotificationsRec.Code, finalNotificationsRec.Body.String())
	}
	finalNotifications := decodeBody[domain.NotificationList](t, finalNotificationsRec)
	if len(finalNotifications.Items) != 0 {
		t.Fatalf("expected deleted notification to disappear after delete, got %+v", finalNotifications.Items)
	}

	postRec := performJSONRequest(t, handler, http.MethodGet, "/api/posts/"+strconv.FormatInt(postID, 10), nil, "")
	if postRec.Code != http.StatusOK {
		t.Fatalf("post should remain after notification delete, got status=%d body=%q", postRec.Code, postRec.Body.String())
	}
	if !strings.Contains(postRec.Body.String(), `"id":"`+strconv.FormatInt(postID, 10)+`"`) {
		t.Fatalf("expected source content response to remain available, got body=%q", postRec.Body.String())
	}
}

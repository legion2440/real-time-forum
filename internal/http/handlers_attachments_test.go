package http

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"forum/internal/platform/id"
	"forum/internal/repo/sqlite"
	"forum/internal/service"
)

type sequenceID struct {
	prefix string
	next   int
}

func (s *sequenceID) New() (string, error) {
	s.next++
	return fmt.Sprintf("%s-%d", s.prefix, s.next), nil
}

var _ id.Generator = (*sequenceID)(nil)

func newAttachmentHandler(t *testing.T) (*Handler, *service.AuthService, *service.PostService, *service.PrivateMessageService, func()) {
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
	privateMessageRepo := sqlite.NewPrivateMessageRepo(db)
	attachmentRepo := sqlite.NewAttachmentRepo(db)

	clock := fixedClock{t: time.Unix(1700000000, 0).UTC()}
	attachmentService, err := service.NewAttachmentService(attachmentRepo, clock, &sequenceID{prefix: "file"}, filepath.Join(t.TempDir(), "uploads"))
	if err != nil {
		t.Fatalf("new attachment service: %v", err)
	}
	authService := service.NewAuthService(userRepo, sessionRepo, clock, &sequenceID{prefix: "session"}, 24*time.Hour)
	postService := service.NewPostService(postRepo, commentRepo, categoryRepo, reactionRepo, attachmentService, clock)
	privateMessageService := service.NewPrivateMessageService(userRepo, privateMessageRepo, attachmentService, clock)

	return NewHandler(authService, postService, privateMessageService, attachmentService), authService, postService, privateMessageService, func() { _ = db.Close() }
}

func TestAttachmentUploadTooLargeReturnsRequestEntityTooLarge(t *testing.T) {
	h, auth, _, _, cleanup := newAttachmentHandler(t)
	defer cleanup()

	mustRegisterUser(t, auth, "upload-large@example.com", "upload_large")
	token := mustLoginUser(t, auth, "upload-large@example.com")

	body, contentType := newMultipartBody(t, "file", "huge.png", bytes.Repeat([]byte("A"), int(service.MaxAttachmentBytes+1)))
	req := httptest.NewRequest(http.MethodPost, "/api/attachments", body)
	req.Header.Set("Content-Type", contentType)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token, Path: "/"})

	rec := httptest.NewRecorder()
	h.Routes(t.TempDir()).ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status %d, got %d body=%q", http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"error":"image is too big (max 20MB)"`) {
		t.Fatalf("expected upload-too-large error, got %q", rec.Body.String())
	}
}

func TestAttachmentUploadTooLargeOverHTTPReturnsJSON(t *testing.T) {
	h, auth, _, _, cleanup := newAttachmentHandler(t)
	defer cleanup()

	mustRegisterUser(t, auth, "upload-large-http@example.com", "upload_large_http")
	token := mustLoginUser(t, auth, "upload-large-http@example.com")

	server := httptest.NewServer(h.Routes(t.TempDir()))
	defer server.Close()

	body, contentType := newMultipartBody(t, "file", "huge.png", bytes.Repeat([]byte("A"), int(service.MaxAttachmentBodyBytes+1)))
	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/attachments", bytes.NewReader(body.Bytes()))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token, Path: "/"})

	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("upload request failed: %v", err)
	}
	defer res.Body.Close()

	responseBody, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status %d, got %d body=%q", http.StatusRequestEntityTooLarge, res.StatusCode, string(responseBody))
	}
	if !strings.Contains(string(responseBody), `"error":"image is too big (max 20MB)"`) {
		t.Fatalf("expected upload-too-large error, got %q", string(responseBody))
	}
}

func TestAttachmentUploadInvalidTypeReturnsBadRequest(t *testing.T) {
	h, auth, _, _, cleanup := newAttachmentHandler(t)
	defer cleanup()

	mustRegisterUser(t, auth, "upload-invalid@example.com", "upload_invalid")
	token := mustLoginUser(t, auth, "upload-invalid@example.com")

	body, contentType := newMultipartBody(t, "file", "note.txt", []byte("plain text is not an image"))
	req := httptest.NewRequest(http.MethodPost, "/api/attachments", body)
	req.Header.Set("Content-Type", contentType)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token, Path: "/"})

	rec := httptest.NewRecorder()
	h.Routes(t.TempDir()).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d body=%q", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"error":"Only JPEG/PNG/GIF allowed"`) {
		t.Fatalf("expected invalid-type error, got %q", rec.Body.String())
	}
}

func TestAttachmentDownloadForDMRejectsOutsider(t *testing.T) {
	h, auth, _, pms, cleanup := newAttachmentHandler(t)
	defer cleanup()

	aliceID := mustRegisterUser(t, auth, "alice-dm-attachment@example.com", "alice_dm_attachment")
	bobID := mustRegisterUser(t, auth, "bob-dm-attachment@example.com", "bob_dm_attachment")
	mustRegisterUser(t, auth, "mallory-dm-attachment@example.com", "mallory_dm_attachment")

	aliceToken := mustLoginUser(t, auth, "alice-dm-attachment@example.com")
	malloryToken := mustLoginUser(t, auth, "mallory-dm-attachment@example.com")

	attachment := mustUploadAttachment(t, h, aliceToken, tinyPNGBytes(t), "tiny.png")
	attachmentID, err := strconv.ParseInt(attachment.ID, 10, 64)
	if err != nil {
		t.Fatalf("parse attachment id: %v", err)
	}

	if _, err := pms.Send(context.Background(), aliceID, bobID, "", &attachmentID); err != nil {
		t.Fatalf("send dm attachment: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, attachment.URL, nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: malloryToken, Path: "/"})
	rec := httptest.NewRecorder()

	h.Routes(t.TempDir()).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d body=%q", http.StatusForbidden, rec.Code, rec.Body.String())
	}
}

func TestAttachmentDownloadForPostReturnsOK(t *testing.T) {
	h, auth, _, _, cleanup := newAttachmentHandler(t)
	defer cleanup()

	mustRegisterUser(t, auth, "alice-post-attachment@example.com", "alice_post_attachment")
	aliceToken := mustLoginUser(t, auth, "alice-post-attachment@example.com")

	attachment := mustUploadAttachment(t, h, aliceToken, tinyPNGBytes(t), "tiny.png")
	attachmentID, err := strconv.ParseInt(attachment.ID, 10, 64)
	if err != nil {
		t.Fatalf("parse attachment id: %v", err)
	}

	postReq := httptest.NewRequest(http.MethodPost, "/api/posts", strings.NewReader(fmt.Sprintf(`{"title":"Post with image","body":"Image body","categories":[1],"attachmentId":%d}`, attachmentID)))
	postReq.Header.Set("Content-Type", "application/json")
	postReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: aliceToken, Path: "/"})
	postRec := httptest.NewRecorder()

	h.Routes(t.TempDir()).ServeHTTP(postRec, postReq)

	if postRec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d body=%q", http.StatusCreated, postRec.Code, postRec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, attachment.URL, nil)
	rec := httptest.NewRecorder()

	h.Routes(t.TempDir()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%q", http.StatusOK, rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "image/png") {
		t.Fatalf("expected image/png content type, got %q", got)
	}
}

func mustUploadAttachment(t *testing.T, h *Handler, token string, content []byte, filename string) attachmentResponseDTO {
	t.Helper()

	body, contentType := newMultipartBody(t, "file", filename, content)
	req := httptest.NewRequest(http.MethodPost, "/api/attachments", body)
	req.Header.Set("Content-Type", contentType)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token, Path: "/"})

	rec := httptest.NewRecorder()
	h.Routes(t.TempDir()).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload attachment status %d body=%q", rec.Code, rec.Body.String())
	}

	var response attachmentResponseDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode attachment response: %v", err)
	}
	return response
}

func newMultipartBody(t *testing.T, fieldName, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile(fieldName, filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return &body, writer.FormDataContentType()
}

func tinyPNGBytes(t *testing.T) []byte {
	t.Helper()

	raw, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+yF9kAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatalf("decode tiny png: %v", err)
	}
	return raw
}

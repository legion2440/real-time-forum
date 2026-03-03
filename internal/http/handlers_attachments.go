package http

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"forum/internal/domain"
	"forum/internal/service"
)

type attachmentResponseDTO struct {
	ID   string `json:"id"`
	URL  string `json:"url"`
	Mime string `json:"mime"`
	Size int64  `json:"size"`
}

func (h *Handler) handleAttachments(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/attachments" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.attachments == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeAuthUnauthorized(w, r)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, service.MaxAttachmentBodyBytes)
	if err := r.ParseMultipartForm(service.MaxAttachmentBodyBytes); err != nil {
		if isMultipartTooLarge(err) {
			writeError(w, http.StatusRequestEntityTooLarge, "image is too big (max 20MB)")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}
	defer file.Close()

	attachment, err := h.attachments.UploadAttachment(r.Context(), userID, service.AttachmentUpload{
		OriginalName: header.Filename,
		Reader:       file,
	})
	if handleServiceError(w, err) {
		return
	}

	writeJSON(w, http.StatusCreated, newAttachmentResponseDTO(attachment))
}

func (h *Handler) handleAttachmentDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.attachments == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	prefix := "/api/attachments/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	rawID := strings.Trim(strings.TrimPrefix(r.URL.Path, prefix), "/")
	if rawID == "" || strings.Contains(rawID, "/") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	attachmentID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || attachmentID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid input")
		return
	}

	var currentUserID *int64
	if userID, ok := userIDFromContext(r.Context()); ok {
		currentUserID = &userID
	}

	attachment, filePath, err := h.attachments.OpenAttachment(r.Context(), attachmentID, currentUserID)
	if handleServiceError(w, err) {
		return
	}

	if _, err := os.Stat(filePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.Header().Set("Content-Type", attachment.Mime)
	w.Header().Set("Content-Length", strconv.FormatInt(attachment.Size, 10))
	w.Header().Set("Cache-Control", "private, max-age=60")
	http.ServeFile(w, r, filePath)
}

func newAttachmentResponseDTO(attachment *domain.Attachment) *attachmentResponseDTO {
	if attachment == nil || attachment.ID <= 0 {
		return nil
	}
	return &attachmentResponseDTO{
		ID:   strconv.FormatInt(attachment.ID, 10),
		URL:  strings.TrimSpace(attachment.URL),
		Mime: strings.TrimSpace(attachment.Mime),
		Size: attachment.Size,
	}
}

func isMultipartTooLarge(err error) bool {
	if err == nil {
		return false
	}
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), strings.ToLower(fmt.Sprintf("http: request body too large")))
}

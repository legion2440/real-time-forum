package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"forum/internal/domain"
	"forum/internal/platform/clock"
	"forum/internal/platform/id"
	"forum/internal/repo"
)

const (
	MaxAttachmentBytes     int64 = 20 << 20
	MaxAttachmentBodyBytes int64 = MaxAttachmentBytes + (1 << 20)
)

type AttachmentUpload struct {
	OriginalName string
	Reader       io.Reader
}

type AttachmentService struct {
	attachments repo.AttachmentRepo
	clock       clock.Clock
	ids         id.Generator
	uploadDir   string
}

func NewAttachmentService(attachments repo.AttachmentRepo, clock clock.Clock, ids id.Generator, uploadDir string) (*AttachmentService, error) {
	uploadDir = strings.TrimSpace(uploadDir)
	if uploadDir == "" {
		uploadDir = filepath.Join(".", "var", "uploads")
	}
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return nil, err
	}

	return &AttachmentService{
		attachments: attachments,
		clock:       clock,
		ids:         ids,
		uploadDir:   uploadDir,
	}, nil
}

func (s *AttachmentService) UploadAttachment(ctx context.Context, userID int64, upload AttachmentUpload) (*domain.Attachment, error) {
	if userID <= 0 || upload.Reader == nil {
		return nil, ErrInvalidInput
	}

	tmpKey, err := s.ids.New()
	if err != nil {
		return nil, err
	}
	tmpPath := filepath.Join(s.uploadDir, tmpKey+".tmp")
	file, err := os.Create(tmpPath)
	if err != nil {
		return nil, err
	}

	var (
		size      int64
		head      []byte
		writeErr  error
		closeErr  error
		finalPath string
	)
	defer func() {
		if closeErr == nil {
			closeErr = file.Close()
		}
		if writeErr != nil {
			_ = os.Remove(tmpPath)
		}
		if finalPath != "" && writeErr != nil {
			_ = os.Remove(finalPath)
		}
	}()

	buffer := make([]byte, 32*1024)
	for {
		n, readErr := upload.Reader.Read(buffer)
		if n > 0 {
			chunk := buffer[:n]
			if len(head) < 512 {
				need := 512 - len(head)
				if need > len(chunk) {
					need = len(chunk)
				}
				head = append(head, chunk[:need]...)
			}
			size += int64(n)
			if size > MaxAttachmentBytes {
				writeErr = ErrImageTooBig
				return nil, writeErr
			}
			if _, err := file.Write(chunk); err != nil {
				writeErr = err
				return nil, writeErr
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			writeErr = readErr
			return nil, writeErr
		}
	}

	mime := http.DetectContentType(head)
	ext, ok := attachmentExtension(mime)
	if !ok {
		writeErr = ErrInvalidImageType
		return nil, writeErr
	}
	if size <= 0 {
		writeErr = ErrInvalidImageType
		return nil, writeErr
	}

	if closeErr = file.Close(); closeErr != nil {
		writeErr = closeErr
		return nil, writeErr
	}

	finalKey, err := s.ids.New()
	if err != nil {
		writeErr = err
		return nil, writeErr
	}
	storageKey := finalKey + ext
	finalPath = filepath.Join(s.uploadDir, storageKey)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		writeErr = err
		return nil, writeErr
	}

	attachment := &domain.Attachment{
		OwnerUserID:  userID,
		Mime:         mime,
		Size:         size,
		StorageKey:   storageKey,
		OriginalName: strings.TrimSpace(upload.OriginalName),
		CreatedAt:    s.clock.Now(),
	}
	attachment.ID, err = s.attachments.Create(ctx, attachment.OwnerUserID, attachment.Mime, attachment.Size, attachment.StorageKey, attachment.OriginalName, attachment.CreatedAt)
	if err != nil {
		writeErr = err
		return nil, writeErr
	}
	attachment.URL = domain.AttachmentURL(attachment.ID)

	return attachment.Public(), nil
}

func (s *AttachmentService) GetOwnedAttachment(ctx context.Context, userID int64, attachmentID *int64) (*domain.Attachment, error) {
	if attachmentID == nil {
		return nil, nil
	}
	if userID <= 0 || *attachmentID <= 0 {
		return nil, ErrInvalidInput
	}

	attachment, err := s.attachments.GetByID(ctx, *attachmentID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, ErrInvalidInput
		}
		return nil, err
	}
	if attachment.OwnerUserID != userID {
		return nil, ErrForbidden
	}
	return attachment.Public(), nil
}

func (s *AttachmentService) OpenAttachment(ctx context.Context, attachmentID int64, userID *int64) (*domain.Attachment, string, error) {
	if attachmentID <= 0 {
		return nil, "", ErrInvalidInput
	}

	attachment, err := s.attachments.GetByID(ctx, attachmentID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, "", ErrNotFound
		}
		return nil, "", err
	}

	usage, err := s.attachments.GetUsage(ctx, attachmentID)
	if err != nil {
		return nil, "", err
	}

	switch {
	case usage.HasPrivateMessage():
		if userID == nil || *userID <= 0 {
			return nil, "", ErrUnauthorized
		}
		if *userID != usage.FromUserID && *userID != usage.ToUserID {
			return nil, "", ErrForbidden
		}
	case usage.HasPost():
		// Public attachments are accessible without auth once linked to a post.
	case userID != nil && *userID == attachment.OwnerUserID:
		// Allow owner preview for uploaded-yet-unlinked attachments.
	default:
		return nil, "", ErrNotFound
	}

	return attachment.Public(), filepath.Join(s.uploadDir, attachment.StorageKey), nil
}

func attachmentExtension(mime string) (string, bool) {
	switch strings.TrimSpace(mime) {
	case "image/jpeg":
		return ".jpg", true
	case "image/png":
		return ".png", true
	case "image/gif":
		return ".gif", true
	default:
		return "", false
	}
}

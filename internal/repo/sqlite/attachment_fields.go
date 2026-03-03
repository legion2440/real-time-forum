package sqlite

import (
	"database/sql"
	"strings"

	"forum/internal/domain"
)

func attachmentFromNullableFields(id sql.NullInt64, mime sql.NullString, size sql.NullInt64) *domain.Attachment {
	if !id.Valid || id.Int64 <= 0 {
		return nil
	}
	attachment := &domain.Attachment{
		ID:   id.Int64,
		Mime: strings.TrimSpace(mime.String),
		Size: size.Int64,
		URL:  domain.AttachmentURL(id.Int64),
	}
	return attachment.Public()
}

func nullableAttachmentID(attachment *domain.Attachment) any {
	if attachment == nil || attachment.ID <= 0 {
		return nil
	}
	return attachment.ID
}

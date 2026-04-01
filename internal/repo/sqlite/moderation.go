package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"forum/internal/domain"
	"forum/internal/repo"
)

type ModerationRepo struct {
	db *sql.DB
}

func NewModerationRepo(db *sql.DB) *ModerationRepo {
	return &ModerationRepo{db: db}
}

func (r *ModerationRepo) HasAdminOrOwner(ctx context.Context) (bool, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT 1
		FROM users
		WHERE role IN (?, ?)
		LIMIT 1
	`, string(domain.RoleAdmin), string(domain.RoleOwner))
	var marker int
	if err := row.Scan(&marker); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return marker == 1, nil
}

func (r *ModerationRepo) CreateBootstrapOwner(ctx context.Context, owner *domain.User) (int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	res, err := tx.ExecContext(ctx, `
		INSERT INTO users (email, username, display_name, role, pass_hash, created_at, profile_initialized)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, strings.TrimSpace(owner.Email), strings.TrimSpace(owner.Username), nullableTrimmedText(owner.DisplayName), string(domain.RoleOwner), strings.TrimSpace(owner.PassHash), timeToUnix(owner.CreatedAt), boolToInt(owner.ProfileInitialized))
	if err != nil {
		return 0, err
	}
	ownerID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	owner.ID = ownerID
	owner.Role = domain.RoleOwner

	if err := insertHistoryTx(ctx, tx, historyInsert{
		ActedAt:       owner.CreatedAt,
		ActionType:    domain.ActionBootstrapOwner,
		TargetType:    domain.ModerationTargetUser,
		TargetID:      ownerID,
		Actor:         *owner,
		CurrentStatus: string(domain.RoleOwner),
	}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return ownerID, nil
}

func (r *ModerationRepo) CreateRoleRequest(ctx context.Context, input repo.RoleRequestCreateInput) (*domain.ModerationRoleRequest, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	res, err := tx.ExecContext(ctx, `
		INSERT INTO moderation_role_requests (requester_user_id, requested_role, note, status, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, input.Requester.ID, string(input.RequestedRole), strings.TrimSpace(input.Note), string(domain.RoleRequestStatusPending), timeToUnix(input.CreatedAt))
	if err != nil {
		return nil, err
	}
	requestID, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	if err := insertHistoryTx(ctx, tx, historyInsert{
		ActedAt:       input.CreatedAt,
		ActionType:    domain.ActionRoleRequested,
		TargetType:    domain.ModerationTargetRequest,
		TargetID:      requestID,
		Actor:         input.Requester,
		CurrentStatus: string(domain.RoleRequestStatusPending),
		RouteToRole:   roleRequestRoute(input.RequestedRole),
		Note:          strings.TrimSpace(input.Note),
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetRoleRequestByID(ctx, requestID)
}

func (r *ModerationRepo) ListRoleRequests(ctx context.Context, filter repo.RoleRequestFilter) ([]domain.ModerationRoleRequest, error) {
	var (
		query strings.Builder
		args  []any
	)
	query.WriteString(`
		SELECT rr.id, rr.requested_role, rr.note, rr.status, rr.created_at, rr.reviewed_at, rr.review_note,
		       applicant.id, applicant.username, applicant.display_name, applicant.role,
		       reviewer.id, reviewer.username, reviewer.display_name, reviewer.role
		FROM moderation_role_requests rr
		JOIN users applicant ON applicant.id = rr.requester_user_id
		LEFT JOIN users reviewer ON reviewer.id = rr.reviewed_by
		WHERE 1 = 1
	`)
	switch filter.ViewerRole {
	case domain.RoleAdmin:
		query.WriteString(` AND rr.requested_role = ?`)
		args = append(args, string(domain.RoleModerator))
	case domain.RoleOwner:
	default:
		query.WriteString(` AND 1 = 0`)
	}
	if filter.RequestedRole != "" {
		query.WriteString(` AND rr.requested_role = ?`)
		args = append(args, string(filter.RequestedRole))
	}
	if filter.Status != "" {
		query.WriteString(` AND rr.status = ?`)
		args = append(args, string(filter.Status))
	}
	query.WriteString(` ORDER BY rr.created_at DESC, rr.id DESC`)

	rows, err := r.db.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var requests []domain.ModerationRoleRequest
	for rows.Next() {
		item, err := scanRoleRequest(rows)
		if err != nil {
			return nil, err
		}
		requests = append(requests, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return requests, nil
}

func (r *ModerationRepo) GetRoleRequestByID(ctx context.Context, requestID int64) (*domain.ModerationRoleRequest, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT rr.id, rr.requested_role, rr.note, rr.status, rr.created_at, rr.reviewed_at, rr.review_note,
		       applicant.id, applicant.username, applicant.display_name, applicant.role,
		       reviewer.id, reviewer.username, reviewer.display_name, reviewer.role
		FROM moderation_role_requests rr
		JOIN users applicant ON applicant.id = rr.requester_user_id
		LEFT JOIN users reviewer ON reviewer.id = rr.reviewed_by
		WHERE rr.id = ?
	`, requestID)
	return scanRoleRequest(row)
}

func (r *ModerationRepo) ReviewRoleRequest(ctx context.Context, input repo.RoleRequestReviewInput) (*domain.ModerationRoleRequest, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	request, err := loadRoleRequestTx(ctx, tx, input.RequestID)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE moderation_role_requests
		SET status = ?, reviewed_at = ?, reviewed_by = ?, review_note = ?
		WHERE id = ?
	`, string(input.Status), timeToUnix(input.ReviewedAt), input.Actor.ID, strings.TrimSpace(input.Note), input.RequestID); err != nil {
		return nil, err
	}

	if input.Status == domain.RoleRequestStatusApproved {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET role = ? WHERE id = ?`, string(request.RequestedRole), request.Applicant.ID); err != nil {
			return nil, err
		}
		if err := insertHistoryTx(ctx, tx, historyInsert{
			ActedAt:       input.ReviewedAt,
			ActionType:    domain.ActionRoleChanged,
			TargetType:    domain.ModerationTargetUser,
			TargetID:      request.Applicant.ID,
			Actor:         input.Actor,
			ContentAuthor: &request.Applicant,
			CurrentStatus: string(request.RequestedRole),
			Note:          strings.TrimSpace(input.Note),
		}); err != nil {
			return nil, err
		}
	}
	if err := insertHistoryTx(ctx, tx, historyInsert{
		ActedAt:       input.ReviewedAt,
		ActionType:    domain.ActionRoleRequestReviewed,
		TargetType:    domain.ModerationTargetRequest,
		TargetID:      input.RequestID,
		Actor:         input.Actor,
		ContentAuthor: &request.Applicant,
		CurrentStatus: string(input.Status),
		Note:          strings.TrimSpace(input.Note),
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetRoleRequestByID(ctx, input.RequestID)
}

func (r *ModerationRepo) ChangeUserRole(ctx context.Context, input repo.RoleChangeInput) (*domain.User, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	target, err := loadUserRefTx(ctx, tx, input.TargetUserID)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET role = ? WHERE id = ?`, string(input.NewRole), input.TargetUserID); err != nil {
		return nil, err
	}
	if err := insertHistoryTx(ctx, tx, historyInsert{
		ActedAt:       input.ChangedAt,
		ActionType:    domain.ActionRoleChanged,
		TargetType:    domain.ModerationTargetUser,
		TargetID:      input.TargetUserID,
		Actor:         input.Actor,
		ContentAuthor: &target,
		CurrentStatus: string(input.NewRole),
		Note:          strings.TrimSpace(input.Note),
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return NewUserRepo(r.db).GetByID(ctx, input.TargetUserID)
}

type historyInsert struct {
	ActedAt                  time.Time
	ActionType               domain.ModerationActionType
	TargetType               string
	TargetID                 int64
	ContentAuthor            *domain.UserRef
	Actor                    domain.User
	Reason                   string
	Note                     string
	CurrentStatus            string
	RouteToRole              string
	LinkedPreviousDecisionID *int64
	PostTitle                string
	PostBody                 string
	CommentBody              string
}

func insertHistoryTx(ctx context.Context, tx *sql.Tx, item historyInsert) error {
	var (
		contentAuthorID   int64
		contentAuthorName string
	)
	if item.ContentAuthor != nil {
		contentAuthorID = item.ContentAuthor.ID
		contentAuthorName = displayNameOrUsernameRef(*item.ContentAuthor)
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO moderation_history (
			acted_at, action_type, target_type, target_id, content_author_user_id, content_author_name,
			actor_user_id, actor_username, actor_display_name, actor_role, reason, note, current_status,
			route_to_role, linked_previous_decision_id, post_title_snapshot, post_body_snapshot, comment_body_snapshot
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, timeToUnix(item.ActedAt), string(item.ActionType), item.TargetType, item.TargetID, contentAuthorID, contentAuthorName,
		item.Actor.ID, strings.TrimSpace(item.Actor.Username), strings.TrimSpace(item.Actor.DisplayName), string(item.Actor.Role),
		strings.TrimSpace(item.Reason), strings.TrimSpace(item.Note), strings.TrimSpace(item.CurrentStatus), strings.TrimSpace(item.RouteToRole),
		nullableInt64Ptr(item.LinkedPreviousDecisionID), strings.TrimSpace(item.PostTitle), strings.TrimSpace(item.PostBody), strings.TrimSpace(item.CommentBody))
	return err
}

func loadRoleRequestTx(ctx context.Context, tx *sql.Tx, requestID int64) (*domain.ModerationRoleRequest, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT rr.id, rr.requested_role, rr.note, rr.status, rr.created_at, rr.reviewed_at, rr.review_note,
		       applicant.id, applicant.username, applicant.display_name, applicant.role,
		       reviewer.id, reviewer.username, reviewer.display_name, reviewer.role
		FROM moderation_role_requests rr
		JOIN users applicant ON applicant.id = rr.requester_user_id
		LEFT JOIN users reviewer ON reviewer.id = rr.reviewed_by
		WHERE rr.id = ?
	`, requestID)
	return scanRoleRequest(row)
}

func scanRoleRequest(s scanner) (*domain.ModerationRoleRequest, error) {
	var (
		item                 domain.ModerationRoleRequest
		createdAt            int64
		reviewedAt           sql.NullInt64
		applicantRole        string
		applicantDisplayName sql.NullString
		reviewerID           sql.NullInt64
		reviewerUsername     sql.NullString
		reviewerDisplayName  sql.NullString
		reviewerRole         sql.NullString
	)
	if err := s.Scan(&item.ID, &item.RequestedRole, &item.Note, &item.Status, &createdAt, &reviewedAt, &item.ReviewNote,
		&item.Applicant.ID, &item.Applicant.Username, &applicantDisplayName, &applicantRole,
		&reviewerID, &reviewerUsername, &reviewerDisplayName, &reviewerRole); err != nil {
		if err == sql.ErrNoRows {
			return nil, repo.ErrNotFound
		}
		return nil, err
	}
	item.Applicant.DisplayName = strings.TrimSpace(applicantDisplayName.String)
	item.Applicant.Role = domain.NormalizeUserRole(applicantRole)
	item.Applicant.Badges = domain.StaffBadgesForRole(item.Applicant.Role)
	item.CreatedAt = unixToTime(createdAt)
	if reviewedAt.Valid && reviewedAt.Int64 > 0 {
		value := unixToTime(reviewedAt.Int64)
		item.ReviewedAt = &value
	}
	if reviewerID.Valid && reviewerID.Int64 > 0 {
		role := domain.NormalizeUserRole(strings.TrimSpace(reviewerRole.String))
		item.ReviewedBy = &domain.UserRef{
			ID:          reviewerID.Int64,
			Username:    strings.TrimSpace(reviewerUsername.String),
			DisplayName: strings.TrimSpace(reviewerDisplayName.String),
			Role:        role,
			Badges:      domain.StaffBadgesForRole(role),
		}
	}
	return &item, nil
}

func loadUserRefTx(ctx context.Context, tx *sql.Tx, userID int64) (domain.UserRef, error) {
	row := tx.QueryRowContext(ctx, `SELECT id, username, display_name, role FROM users WHERE id = ?`, userID)
	var (
		user        domain.UserRef
		displayName sql.NullString
		role        string
	)
	if err := row.Scan(&user.ID, &user.Username, &displayName, &role); err != nil {
		if err == sql.ErrNoRows {
			return domain.UserRef{}, repo.ErrNotFound
		}
		return domain.UserRef{}, err
	}
	user.DisplayName = strings.TrimSpace(displayName.String)
	user.Role = domain.NormalizeUserRole(role)
	user.Badges = domain.StaffBadgesForRole(user.Role)
	return user, nil
}

func (r *ModerationRepo) mustLoadUserRef(ctx context.Context, userID int64) (domain.UserRef, error) {
	row := r.db.QueryRowContext(ctx, `SELECT id, username, display_name, role FROM users WHERE id = ?`, userID)
	var (
		user        domain.UserRef
		displayName sql.NullString
		role        string
	)
	if err := row.Scan(&user.ID, &user.Username, &displayName, &role); err != nil {
		if err == sql.ErrNoRows {
			return domain.UserRef{}, repo.ErrNotFound
		}
		return domain.UserRef{}, err
	}
	user.DisplayName = strings.TrimSpace(displayName.String)
	user.Role = domain.NormalizeUserRole(role)
	user.Badges = domain.StaffBadgesForRole(user.Role)
	return user, nil
}

func roleRequestRoute(requestedRole domain.UserRole) string {
	if requestedRole == domain.RoleAdmin {
		return string(domain.RoleOwner)
	}
	return string(domain.RoleAdmin)
}

func displayNameOrUsernameRef(ref domain.UserRef) string {
	if strings.TrimSpace(ref.DisplayName) != "" {
		return strings.TrimSpace(ref.DisplayName)
	}
	return strings.TrimSpace(ref.Username)
}

func nullableInt64Ptr(value *int64) any {
	if value == nil || *value <= 0 {
		return nil
	}
	return *value
}

func (r *ModerationRepo) ListReports(ctx context.Context, filter repo.ReportFilter) ([]domain.ModerationReport, error) {
	query, args := buildReportQuery(filter)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reports []domain.ModerationReport
	for rows.Next() {
		reportID, err := scanModerationID(rows)
		if err != nil {
			return nil, err
		}
		item, err := r.GetReportByID(ctx, reportID)
		if err != nil {
			return nil, err
		}
		reports = append(reports, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return reports, nil
}

func (r *ModerationRepo) GetReportByID(ctx context.Context, reportID int64) (*domain.ModerationReport, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, target_type, target_id, reporter_user_id, reporter_role, content_author_user_id,
		       reason, note, status, route_to_roles, created_at, closed_at, closed_by, closed_by_role,
		       decision_reason, decision_note, linked_previous_decision_id
		FROM moderation_reports
		WHERE id = ?
	`, reportID)
	return r.scanReport(ctx, row)
}

func (r *ModerationRepo) CreateReport(ctx context.Context, input repo.ReportCreateInput) (*domain.ModerationReport, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	target, contentAuthor, err := loadTargetPreviewTx(ctx, tx, input.TargetType, input.TargetID)
	if err != nil {
		return nil, err
	}
	routes := reportRouteRoles(input.Reporter.Role)
	res, err := tx.ExecContext(ctx, `
		INSERT INTO moderation_reports (
			target_type, target_id, reporter_user_id, reporter_role, content_author_user_id,
			reason, note, status, route_to_roles, created_at, linked_previous_decision_id
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, input.TargetType, input.TargetID, input.Reporter.ID, string(input.Reporter.Role), contentAuthor.ID,
		string(input.Reason), strings.TrimSpace(input.Note), string(domain.ModerationStatusPending), strings.Join(routes, ","), timeToUnix(input.CreatedAt), nullableInt64Ptr(input.LinkedPreviousDecisionID))
	if err != nil {
		return nil, err
	}
	reportID, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := insertHistoryTx(ctx, tx, historyInsert{
		ActedAt:                  input.CreatedAt,
		ActionType:               domain.ActionReportCreated,
		TargetType:               domain.ModerationTargetReport,
		TargetID:                 reportID,
		Actor:                    input.Reporter,
		ContentAuthor:            &contentAuthor,
		Reason:                   string(input.Reason),
		Note:                     strings.TrimSpace(input.Note),
		CurrentStatus:            string(domain.ModerationStatusPending),
		RouteToRole:              strings.Join(routes, ","),
		LinkedPreviousDecisionID: input.LinkedPreviousDecisionID,
		PostTitle:                target.PostTitle,
		PostBody:                 target.PostBody,
		CommentBody:              target.CommentBody,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetReportByID(ctx, reportID)
}

func (r *ModerationRepo) CloseReport(ctx context.Context, input repo.ReportCloseInput) (*domain.ModerationReport, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	item, err := r.scanReportTx(ctx, tx, input.ReportID)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE moderation_reports
		SET status = ?, closed_at = ?, closed_by = ?, closed_by_role = ?, decision_reason = ?, decision_note = ?
		WHERE id = ?
	`, string(input.Status), timeToUnix(input.ClosedAt), input.Actor.ID, string(input.Actor.Role), string(input.DecisionReason), strings.TrimSpace(input.DecisionNote), input.ReportID); err != nil {
		return nil, err
	}
	if err := insertHistoryTx(ctx, tx, historyInsert{
		ActedAt:       input.ClosedAt,
		ActionType:    domain.ActionReportClosed,
		TargetType:    domain.ModerationTargetReport,
		TargetID:      input.ReportID,
		Actor:         input.Actor,
		ContentAuthor: &item.ContentAuthor,
		Reason:        string(input.DecisionReason),
		Note:          strings.TrimSpace(input.DecisionNote),
		CurrentStatus: string(input.Status),
		PostTitle:     item.Target.PostTitle,
		PostBody:      item.Target.PostBody,
		CommentBody:   item.Target.CommentBody,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetReportByID(ctx, input.ReportID)
}

func buildReportQuery(filter repo.ReportFilter) (string, []any) {
	var (
		query strings.Builder
		args  []any
	)
	query.WriteString(`SELECT id FROM moderation_reports WHERE 1 = 1`)
	if filter.ReporterUserID > 0 {
		query.WriteString(` AND reporter_user_id = ?`)
		args = append(args, filter.ReporterUserID)
	} else {
		switch filter.ViewerRole {
		case domain.RoleModerator:
			query.WriteString(` AND reporter_role = ?`)
			args = append(args, string(domain.RoleUser))
		case domain.RoleAdmin:
			query.WriteString(` AND reporter_role IN (?, ?)`)
			args = append(args, string(domain.RoleUser), string(domain.RoleModerator))
		case domain.RoleOwner:
		default:
			query.WriteString(` AND 1 = 0`)
		}
	}
	if filter.Status != "" {
		query.WriteString(` AND status = ?`)
		args = append(args, string(filter.Status))
	}
	query.WriteString(` ORDER BY created_at DESC, id DESC`)
	return query.String(), args
}

func reportRouteRoles(role domain.UserRole) []string {
	switch role {
	case domain.RoleModerator:
		return []string{string(domain.RoleAdmin), string(domain.RoleOwner)}
	case domain.RoleAdmin:
		return []string{string(domain.RoleOwner)}
	default:
		return []string{string(domain.RoleModerator), string(domain.RoleAdmin), string(domain.RoleOwner)}
	}
}

func (r *ModerationRepo) scanReport(ctx context.Context, s scanner) (*domain.ModerationReport, error) {
	var (
		item                     domain.ModerationReport
		reporterRole             string
		routeToRoles             string
		createdAt                int64
		closedAt                 sql.NullInt64
		closedByID               sql.NullInt64
		closedByRole             sql.NullString
		linkedPreviousDecisionID sql.NullInt64
	)
	if err := s.Scan(&item.ID, &item.Target.TargetType, &item.Target.TargetID, &item.Reporter.ID, &reporterRole, &item.ContentAuthor.ID,
		&item.Reason, &item.Note, &item.Status, &routeToRoles, &createdAt, &closedAt, &closedByID, &closedByRole,
		&item.DecisionReason, &item.DecisionNote, &linkedPreviousDecisionID); err != nil {
		if err == sql.ErrNoRows {
			return nil, repo.ErrNotFound
		}
		return nil, err
	}
	reporter, err := r.mustLoadUserRef(ctx, item.Reporter.ID)
	if err != nil {
		return nil, err
	}
	contentAuthor, err := r.mustLoadUserRef(ctx, item.ContentAuthor.ID)
	if err != nil {
		return nil, err
	}
	item.Reporter = reporter
	item.ContentAuthor = contentAuthor
	item.Reporter.Role = domain.NormalizeUserRole(reporterRole)
	item.Reporter.Badges = domain.StaffBadgesForRole(item.Reporter.Role)
	item.RouteToRoles = splitCSV(routeToRoles)
	item.CreatedAt = unixToTime(createdAt)
	if linkedPreviousDecisionID.Valid && linkedPreviousDecisionID.Int64 > 0 {
		item.LinkedPreviousDecisionID = &linkedPreviousDecisionID.Int64
	}
	if closedAt.Valid && closedAt.Int64 > 0 {
		value := unixToTime(closedAt.Int64)
		item.ClosedAt = &value
	}
	if closedByID.Valid && closedByID.Int64 > 0 {
		closedBy, err := r.mustLoadUserRef(ctx, closedByID.Int64)
		if err != nil {
			return nil, err
		}
		role := domain.NormalizeUserRole(strings.TrimSpace(closedByRole.String))
		closedBy.Role = role
		closedBy.Badges = domain.StaffBadgesForRole(role)
		item.ClosedBy = &closedBy
	}
	target, _, err := loadTargetPreviewDB(ctx, r.db, item.Target.TargetType, item.Target.TargetID)
	if err != nil {
		return nil, err
	}
	item.Target = target
	return &item, nil
}

func (r *ModerationRepo) scanReportTx(ctx context.Context, tx *sql.Tx, reportID int64) (*domain.ModerationReport, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, target_type, target_id, reporter_user_id, reporter_role, content_author_user_id,
		       reason, note, status, route_to_roles, created_at, closed_at, closed_by, closed_by_role,
		       decision_reason, decision_note, linked_previous_decision_id
		FROM moderation_reports
		WHERE id = ?
	`, reportID)
	return r.scanReport(ctx, row)
}

func (r *ModerationRepo) ListAppeals(ctx context.Context, filter repo.AppealFilter) ([]domain.ModerationAppeal, error) {
	var (
		query strings.Builder
		args  []any
	)
	query.WriteString(`SELECT id FROM moderation_appeals WHERE 1 = 1`)
	if filter.RequesterUserID > 0 {
		query.WriteString(` AND requester_user_id = ?`)
		args = append(args, filter.RequesterUserID)
	} else {
		switch filter.ViewerRole {
		case domain.RoleAdmin:
			query.WriteString(` AND target_role = ?`)
			args = append(args, string(domain.RoleAdmin))
		case domain.RoleOwner:
			query.WriteString(` AND target_role = ?`)
			args = append(args, string(domain.RoleOwner))
		default:
			query.WriteString(` AND 1 = 0`)
		}
	}
	if filter.Status != "" {
		query.WriteString(` AND status = ?`)
		args = append(args, string(filter.Status))
	}
	query.WriteString(` ORDER BY created_at DESC, id DESC`)

	rows, err := r.db.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var appeals []domain.ModerationAppeal
	for rows.Next() {
		appealID, err := scanModerationID(rows)
		if err != nil {
			return nil, err
		}
		item, err := r.GetAppealByID(ctx, appealID)
		if err != nil {
			return nil, err
		}
		appeals = append(appeals, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return appeals, nil
}

func (r *ModerationRepo) GetAppealByID(ctx context.Context, appealID int64) (*domain.ModerationAppeal, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, target_type, target_id, requester_user_id, target_role, status, note,
		       source_history_id, linked_previous_decision_id, created_at, closed_at, closed_by, closed_by_role, decision_note
		FROM moderation_appeals
		WHERE id = ?
	`, appealID)
	return r.scanAppeal(ctx, row)
}

func (r *ModerationRepo) CreateAppeal(ctx context.Context, input repo.AppealCreateInput) (*domain.ModerationAppeal, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	target, contentAuthor, err := loadTargetPreviewTx(ctx, tx, input.TargetType, input.TargetID)
	if err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO moderation_appeals (
			target_type, target_id, requester_user_id, target_role, status, note, source_history_id,
			linked_previous_decision_id, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, input.TargetType, input.TargetID, input.Requester.ID, string(input.TargetRole), string(domain.AppealStatusPending),
		strings.TrimSpace(input.Note), input.SourceHistoryID, nullableInt64Ptr(input.LinkedPreviousDecisionID), timeToUnix(input.CreatedAt))
	if err != nil {
		return nil, err
	}
	appealID, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := insertHistoryTx(ctx, tx, historyInsert{
		ActedAt:                  input.CreatedAt,
		ActionType:               domain.ActionAppealCreated,
		TargetType:               domain.ModerationTargetAppeal,
		TargetID:                 appealID,
		Actor:                    input.Requester,
		ContentAuthor:            &contentAuthor,
		Note:                     strings.TrimSpace(input.Note),
		CurrentStatus:            string(domain.AppealStatusPending),
		RouteToRole:              string(input.TargetRole),
		LinkedPreviousDecisionID: input.LinkedPreviousDecisionID,
		PostTitle:                target.PostTitle,
		PostBody:                 target.PostBody,
		CommentBody:              target.CommentBody,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetAppealByID(ctx, appealID)
}

func (r *ModerationRepo) CloseAppeal(ctx context.Context, input repo.AppealCloseInput) (*domain.ModerationAppeal, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	item, err := r.scanAppealTx(ctx, tx, input.AppealID)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE moderation_appeals
		SET status = ?, closed_at = ?, closed_by = ?, closed_by_role = ?, decision_note = ?
		WHERE id = ?
	`, string(input.Status), timeToUnix(input.ClosedAt), input.Actor.ID, string(input.Actor.Role), strings.TrimSpace(input.DecisionNote), input.AppealID); err != nil {
		return nil, err
	}
	if err := insertHistoryTx(ctx, tx, historyInsert{
		ActedAt:       input.ClosedAt,
		ActionType:    domain.ActionAppealClosed,
		TargetType:    domain.ModerationTargetAppeal,
		TargetID:      input.AppealID,
		Actor:         input.Actor,
		ContentAuthor: &item.Requester,
		Note:          strings.TrimSpace(input.DecisionNote),
		CurrentStatus: string(input.Status),
		PostTitle:     item.Target.PostTitle,
		PostBody:      item.Target.PostBody,
		CommentBody:   item.Target.CommentBody,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetAppealByID(ctx, input.AppealID)
}

func (r *ModerationRepo) scanAppeal(ctx context.Context, s scanner) (*domain.ModerationAppeal, error) {
	var (
		item                     domain.ModerationAppeal
		sourceHistoryID          int64
		createdAt                int64
		closedAt                 sql.NullInt64
		closedByID               sql.NullInt64
		closedByRole             sql.NullString
		linkedPreviousDecisionID sql.NullInt64
	)
	if err := s.Scan(&item.ID, &item.Target.TargetType, &item.Target.TargetID, &item.Requester.ID, &item.TargetRole, &item.Status, &item.Note,
		&sourceHistoryID, &linkedPreviousDecisionID, &createdAt, &closedAt, &closedByID, &closedByRole, &item.DecisionNote); err != nil {
		if err == sql.ErrNoRows {
			return nil, repo.ErrNotFound
		}
		return nil, err
	}
	requester, err := r.mustLoadUserRef(ctx, item.Requester.ID)
	if err != nil {
		return nil, err
	}
	item.Requester = requester
	item.CreatedAt = unixToTime(createdAt)
	if linkedPreviousDecisionID.Valid && linkedPreviousDecisionID.Int64 > 0 {
		item.LinkedPreviousDecisionID = &linkedPreviousDecisionID.Int64
	}
	if closedAt.Valid && closedAt.Int64 > 0 {
		value := unixToTime(closedAt.Int64)
		item.ClosedAt = &value
	}
	if closedByID.Valid && closedByID.Int64 > 0 {
		closedBy, err := r.mustLoadUserRef(ctx, closedByID.Int64)
		if err != nil {
			return nil, err
		}
		role := domain.NormalizeUserRole(strings.TrimSpace(closedByRole.String))
		closedBy.Role = role
		closedBy.Badges = domain.StaffBadgesForRole(role)
		item.ClosedBy = &closedBy
	}
	target, _, err := loadTargetPreviewDB(ctx, r.db, item.Target.TargetType, item.Target.TargetID)
	if err != nil {
		return nil, err
	}
	item.Target = target
	return &item, nil
}

func (r *ModerationRepo) scanAppealTx(ctx context.Context, tx *sql.Tx, appealID int64) (*domain.ModerationAppeal, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, target_type, target_id, requester_user_id, target_role, status, note,
		       source_history_id, linked_previous_decision_id, created_at, closed_at, closed_by, closed_by_role, decision_note
		FROM moderation_appeals
		WHERE id = ?
	`, appealID)
	return r.scanAppeal(ctx, row)
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func scanModerationID(s scanner) (int64, error) {
	var id int64
	if err := s.Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

func loadTargetPreviewDB(ctx context.Context, db *sql.DB, targetType string, targetID int64) (domain.ModerationTargetPreview, domain.UserRef, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ModerationTargetPreview{}, domain.UserRef{}, err
	}
	defer tx.Rollback()
	return loadTargetPreviewTx(ctx, tx, targetType, targetID)
}

func loadTargetPreviewTx(ctx context.Context, tx *sql.Tx, targetType string, targetID int64) (domain.ModerationTargetPreview, domain.UserRef, error) {
	switch targetType {
	case domain.ModerationTargetPost:
		return loadPostPreviewTx(ctx, tx, targetID)
	case domain.ModerationTargetComment:
		return loadCommentPreviewTx(ctx, tx, targetID)
	default:
		return domain.ModerationTargetPreview{}, domain.UserRef{}, repo.ErrNotFound
	}
}

func loadPostPreviewTx(ctx context.Context, tx *sql.Tx, postID int64) (domain.ModerationTargetPreview, domain.UserRef, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT p.id, p.title, p.body, p.is_under_review, p.delete_protected, p.deleted_at,
		       u.id, u.username, u.display_name, u.role
		FROM posts p
		JOIN users u ON u.id = p.user_id
		WHERE p.id = ?
	`, postID)
	var (
		target            domain.ModerationTargetPreview
		author            domain.UserRef
		underReview       int
		deleteProtected   int
		deletedAt         sql.NullInt64
		authorDisplayName sql.NullString
		authorRole        string
	)
	if err := row.Scan(&target.TargetID, &target.PostTitle, &target.PostBody, &underReview, &deleteProtected, &deletedAt, &author.ID, &author.Username, &authorDisplayName, &authorRole); err != nil {
		if err == sql.ErrNoRows {
			return domain.ModerationTargetPreview{}, domain.UserRef{}, repo.ErrNotFound
		}
		return domain.ModerationTargetPreview{}, domain.UserRef{}, err
	}
	author.DisplayName = strings.TrimSpace(authorDisplayName.String)
	author.Role = domain.NormalizeUserRole(authorRole)
	author.Badges = domain.StaffBadgesForRole(author.Role)
	target.TargetType = domain.ModerationTargetPost
	target.PostID = postID
	target.UnderReview = underReview != 0
	target.DeleteProtected = deleteProtected != 0
	if deletedAt.Valid && deletedAt.Int64 > 0 {
		value := unixToTime(deletedAt.Int64)
		target.DeletedAt = &value
	}
	return target, author, nil
}

func loadCommentPreviewTx(ctx context.Context, tx *sql.Tx, commentID int64) (domain.ModerationTargetPreview, domain.UserRef, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT c.id, c.post_id, p.title, c.body, c.deleted_body, c.deleted_at,
		       u.id, u.username, u.display_name, u.role
		FROM comments c
		JOIN posts p ON p.id = c.post_id
		JOIN users u ON u.id = c.user_id
		WHERE c.id = ?
	`, commentID)
	var (
		target            domain.ModerationTargetPreview
		author            domain.UserRef
		postTitle         string
		body              string
		deletedBody       string
		deletedAt         sql.NullInt64
		authorDisplayName sql.NullString
		authorRole        string
	)
	if err := row.Scan(&target.TargetID, &target.PostID, &postTitle, &body, &deletedBody, &deletedAt, &author.ID, &author.Username, &authorDisplayName, &authorRole); err != nil {
		if err == sql.ErrNoRows {
			return domain.ModerationTargetPreview{}, domain.UserRef{}, repo.ErrNotFound
		}
		return domain.ModerationTargetPreview{}, domain.UserRef{}, err
	}
	author.DisplayName = strings.TrimSpace(authorDisplayName.String)
	author.Role = domain.NormalizeUserRole(authorRole)
	author.Badges = domain.StaffBadgesForRole(author.Role)
	target.TargetType = domain.ModerationTargetComment
	target.CommentID = commentID
	target.PostTitle = strings.TrimSpace(postTitle)
	target.CommentBody = strings.TrimSpace(body)
	if target.CommentBody == "[deleted]" && strings.TrimSpace(deletedBody) != "" {
		target.CommentBody = strings.TrimSpace(deletedBody)
	}
	if deletedAt.Valid && deletedAt.Int64 > 0 {
		value := unixToTime(deletedAt.Int64)
		target.DeletedAt = &value
	}
	return target, author, nil
}

func (r *ModerationRepo) ApprovePost(ctx context.Context, input repo.PostApprovalInput) (*domain.Post, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	target, contentAuthor, err := loadTargetPreviewTx(ctx, tx, domain.ModerationTargetPost, input.PostID)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE posts
		SET is_under_review = 0, approved_by = ?, approved_at = ?
		WHERE id = ?
	`, input.Actor.ID, timeToUnix(input.ApprovedAt), input.PostID); err != nil {
		return nil, err
	}
	if input.UpdateCategories {
		if _, err := tx.ExecContext(ctx, `DELETE FROM post_categories WHERE post_id = ?`, input.PostID); err != nil {
			return nil, err
		}
		for _, categoryID := range input.CategoryIDs {
			if _, err := tx.ExecContext(ctx, `INSERT INTO post_categories(post_id, category_id) VALUES (?, ?)`, input.PostID, categoryID); err != nil {
				return nil, err
			}
		}
	}
	if err := insertHistoryTx(ctx, tx, historyInsert{
		ActedAt:       input.ApprovedAt,
		ActionType:    domain.ActionPostApproved,
		TargetType:    domain.ModerationTargetPost,
		TargetID:      input.PostID,
		Actor:         input.Actor,
		ContentAuthor: &contentAuthor,
		Note:          strings.TrimSpace(input.Note),
		CurrentStatus: string(domain.ModerationStatusApproved),
		PostTitle:     target.PostTitle,
		PostBody:      target.PostBody,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return NewPostRepo(r.db).GetByID(ctx, input.PostID)
}

func (r *ModerationRepo) UpdatePostCategories(ctx context.Context, input repo.PostCategoryUpdateInput) (*domain.Post, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	target, contentAuthor, err := loadTargetPreviewTx(ctx, tx, domain.ModerationTargetPost, input.PostID)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM post_categories WHERE post_id = ?`, input.PostID); err != nil {
		return nil, err
	}
	for _, categoryID := range input.CategoryIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO post_categories(post_id, category_id) VALUES (?, ?)`, input.PostID, categoryID); err != nil {
			return nil, err
		}
	}
	if err := insertHistoryTx(ctx, tx, historyInsert{
		ActedAt:       input.UpdatedAt,
		ActionType:    domain.ActionPostCategoriesUpdated,
		TargetType:    domain.ModerationTargetPost,
		TargetID:      input.PostID,
		Actor:         input.Actor,
		ContentAuthor: &contentAuthor,
		Note:          strings.TrimSpace(input.Note),
		CurrentStatus: string(domain.ModerationStatusApproved),
		PostTitle:     target.PostTitle,
		PostBody:      target.PostBody,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return NewPostRepo(r.db).GetByID(ctx, input.PostID)
}

func (r *ModerationRepo) SoftDeleteContent(ctx context.Context, input repo.ContentModerationInput) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	target, contentAuthor, err := loadTargetPreviewTx(ctx, tx, input.TargetType, input.TargetID)
	if err != nil {
		return err
	}
	switch input.TargetType {
	case domain.ModerationTargetPost:
		if _, err := tx.ExecContext(ctx, `
			UPDATE posts
			SET deleted_at = ?, deleted_by = ?, deleted_by_role = ?
			WHERE id = ? AND deleted_at IS NULL
		`, timeToUnix(input.ActedAt), input.Actor.ID, string(input.Actor.Role), input.TargetID); err != nil {
			return err
		}
	case domain.ModerationTargetComment:
		hasDescendants, err := hasCommentDescendantsTx(ctx, tx, input.TargetID)
		if err != nil {
			return err
		}
		if hasDescendants {
			if _, err := tx.ExecContext(ctx, `
				UPDATE comments
				SET deleted_body = CASE WHEN deleted_body = '' THEN body ELSE deleted_body END,
				    body = '[deleted]',
				    deleted_at = ?, deleted_by = ?, deleted_by_role = ?
				WHERE id = ? AND deleted_at IS NULL
			`, timeToUnix(input.ActedAt), input.Actor.ID, string(input.Actor.Role), input.TargetID); err != nil {
				return err
			}
		} else {
			parentID, err := loadCommentParentIDTx(ctx, tx, input.TargetID)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM comments WHERE id = ?`, input.TargetID); err != nil {
				return err
			}
			if err := cleanupDeletedCommentAncestorsTx(ctx, tx, parentID); err != nil {
				return err
			}
		}
	default:
		return repo.ErrNotFound
	}

	action := domain.ActionPostSoftDeleted
	if input.TargetType == domain.ModerationTargetComment {
		action = domain.ActionCommentSoftDeleted
	}
	if err := insertHistoryTx(ctx, tx, historyInsert{
		ActedAt:       input.ActedAt,
		ActionType:    action,
		TargetType:    input.TargetType,
		TargetID:      input.TargetID,
		Actor:         input.Actor,
		ContentAuthor: &contentAuthor,
		Reason:        string(input.Reason),
		Note:          strings.TrimSpace(input.Note),
		CurrentStatus: string(domain.ModerationStatusActionTaken),
		PostTitle:     target.PostTitle,
		PostBody:      target.PostBody,
		CommentBody:   target.CommentBody,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *ModerationRepo) RestoreContent(ctx context.Context, input repo.ContentModerationInput) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	target, contentAuthor, err := loadTargetPreviewTx(ctx, tx, input.TargetType, input.TargetID)
	if err != nil {
		return err
	}
	switch input.TargetType {
	case domain.ModerationTargetPost:
		if _, err := tx.ExecContext(ctx, `
			UPDATE posts
			SET deleted_at = NULL, deleted_by = NULL, deleted_by_role = ''
			WHERE id = ? AND deleted_at IS NOT NULL
		`, input.TargetID); err != nil {
			return err
		}
	case domain.ModerationTargetComment:
		if _, err := tx.ExecContext(ctx, `
			UPDATE comments
			SET body = CASE WHEN deleted_body <> '' THEN deleted_body ELSE body END,
			    deleted_body = '',
			    deleted_at = NULL,
			    deleted_by = NULL,
			    deleted_by_role = ''
			WHERE id = ? AND deleted_at IS NOT NULL
		`, input.TargetID); err != nil {
			return err
		}
	default:
		return repo.ErrNotFound
	}
	action := domain.ActionPostRestored
	if input.TargetType == domain.ModerationTargetComment {
		action = domain.ActionCommentRestored
	}
	if err := insertHistoryTx(ctx, tx, historyInsert{
		ActedAt:       input.ActedAt,
		ActionType:    action,
		TargetType:    input.TargetType,
		TargetID:      input.TargetID,
		Actor:         input.Actor,
		ContentAuthor: &contentAuthor,
		Reason:        string(input.Reason),
		Note:          strings.TrimSpace(input.Note),
		CurrentStatus: string(domain.ModerationStatusRestored),
		PostTitle:     target.PostTitle,
		PostBody:      target.PostBody,
		CommentBody:   target.CommentBody,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *ModerationRepo) HardDeleteContent(ctx context.Context, input repo.ContentModerationInput) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	target, contentAuthor, err := loadTargetPreviewTx(ctx, tx, input.TargetType, input.TargetID)
	if err != nil {
		return err
	}
	switch input.TargetType {
	case domain.ModerationTargetPost:
		if _, err := tx.ExecContext(ctx, `DELETE FROM posts WHERE id = ?`, input.TargetID); err != nil {
			return err
		}
	case domain.ModerationTargetComment:
		parentID, err := loadCommentParentIDTx(ctx, tx, input.TargetID)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM comments WHERE id = ?`, input.TargetID); err != nil {
			return err
		}
		if err := cleanupDeletedCommentAncestorsTx(ctx, tx, parentID); err != nil {
			return err
		}
	default:
		return repo.ErrNotFound
	}
	action := domain.ActionPostHardDeleted
	if input.TargetType == domain.ModerationTargetComment {
		action = domain.ActionCommentHardDeleted
	}
	if err := insertHistoryTx(ctx, tx, historyInsert{
		ActedAt:       input.ActedAt,
		ActionType:    action,
		TargetType:    input.TargetType,
		TargetID:      input.TargetID,
		Actor:         input.Actor,
		ContentAuthor: &contentAuthor,
		Reason:        string(input.Reason),
		Note:          strings.TrimSpace(input.Note),
		CurrentStatus: string(domain.ModerationStatusActionTaken),
		PostTitle:     target.PostTitle,
		PostBody:      target.PostBody,
		CommentBody:   target.CommentBody,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *ModerationRepo) SetPostDeleteProtection(ctx context.Context, postID int64, actor domain.User, protected bool, actedAt time.Time, note string) (*domain.Post, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	target, contentAuthor, err := loadTargetPreviewTx(ctx, tx, domain.ModerationTargetPost, postID)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE posts SET delete_protected = ? WHERE id = ?`, boolToInt(protected), postID); err != nil {
		return nil, err
	}
	action := domain.ActionPostProtectionSet
	status := "protected"
	if !protected {
		action = domain.ActionPostProtectionCleared
		status = "unprotected"
	}
	if err := insertHistoryTx(ctx, tx, historyInsert{
		ActedAt:       actedAt,
		ActionType:    action,
		TargetType:    domain.ModerationTargetPost,
		TargetID:      postID,
		Actor:         actor,
		ContentAuthor: &contentAuthor,
		Note:          strings.TrimSpace(note),
		CurrentStatus: status,
		PostTitle:     target.PostTitle,
		PostBody:      target.PostBody,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return NewPostRepo(r.db).GetByID(ctx, postID)
}

func (r *ModerationRepo) CreateCategory(ctx context.Context, input repo.CategoryCreateInput) (*domain.Category, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	res, err := tx.ExecContext(ctx, `
		INSERT INTO categories(code, name, is_system)
		VALUES (?, ?, ?)
	`, strings.TrimSpace(input.Category.Code), strings.TrimSpace(input.Category.Name), boolToInt(input.Category.IsSystem))
	if err != nil {
		return nil, err
	}
	categoryID, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := insertHistoryTx(ctx, tx, historyInsert{
		ActedAt:       input.CreatedAt,
		ActionType:    domain.ActionCategoryCreated,
		TargetType:    domain.ModerationTargetCategory,
		TargetID:      categoryID,
		Actor:         input.Actor,
		CurrentStatus: "created",
		Note:          strings.TrimSpace(input.Note),
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return NewCategoryRepo(r.db).GetByID(ctx, categoryID)
}

func (r *ModerationRepo) DeleteCategory(ctx context.Context, input repo.CategoryDeleteInput) (int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	category, err := loadCategoryTx(ctx, tx, input.CategoryID)
	if err != nil {
		return 0, err
	}
	other, err := loadCategoryByCodeTx(ctx, tx, "other")
	if err != nil {
		return 0, err
	}
	postIDs, err := loadCategoryPostIDsTx(ctx, tx, input.CategoryID)
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM post_categories WHERE category_id = ?`, input.CategoryID); err != nil {
		return 0, err
	}
	for _, postID := range postIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO post_categories(post_id, category_id)
			VALUES (?, ?)
		`, postID, other.ID); err != nil {
			return 0, err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM categories WHERE id = ?`, input.CategoryID); err != nil {
		return 0, err
	}
	if err := insertHistoryTx(ctx, tx, historyInsert{
		ActedAt:       input.DeletedAt,
		ActionType:    domain.ActionCategoryDeleted,
		TargetType:    domain.ModerationTargetCategory,
		TargetID:      input.CategoryID,
		Actor:         input.Actor,
		CurrentStatus: fmt.Sprintf("moved_to_other:%d", len(postIDs)),
		Note:          strings.TrimSpace(input.Note),
		PostTitle:     category.Name,
		PostBody:      other.Name,
	}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int64(len(postIDs)), nil
}

func (r *ModerationRepo) ListHistory(ctx context.Context, filter domain.ModerationHistoryFilter) ([]domain.ModerationHistoryRecord, error) {
	var (
		query strings.Builder
		args  []any
	)
	query.WriteString(`
		SELECT id, acted_at, action_type, target_type, target_id, content_author_user_id, content_author_name,
		       actor_user_id, actor_username, actor_display_name, actor_role, reason, note, current_status,
		       route_to_role, linked_previous_decision_id, post_title_snapshot, post_body_snapshot, comment_body_snapshot
		FROM moderation_history
		WHERE 1 = 1
	`)
	if strings.TrimSpace(filter.ActionType) != "" {
		query.WriteString(` AND action_type = ?`)
		args = append(args, strings.TrimSpace(filter.ActionType))
	}
	if strings.TrimSpace(filter.TargetType) != "" {
		query.WriteString(` AND target_type = ?`)
		args = append(args, strings.TrimSpace(filter.TargetType))
	}
	if strings.TrimSpace(filter.Status) != "" {
		query.WriteString(` AND current_status = ?`)
		args = append(args, strings.TrimSpace(filter.Status))
	}
	if filter.From != nil && !filter.From.IsZero() {
		query.WriteString(` AND acted_at >= ?`)
		args = append(args, timeToUnix(*filter.From))
	}
	if filter.To != nil && !filter.To.IsZero() {
		query.WriteString(` AND acted_at <= ?`)
		args = append(args, timeToUnix(*filter.To))
	}
	query.WriteString(` ORDER BY acted_at DESC, id DESC`)
	if filter.Limit > 0 {
		query.WriteString(` LIMIT ?`)
		args = append(args, filter.Limit)
		if filter.Offset > 0 {
			query.WriteString(` OFFSET ?`)
			args = append(args, filter.Offset)
		}
	}

	rows, err := r.db.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []domain.ModerationHistoryRecord
	for rows.Next() {
		record, err := scanHistoryRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func (r *ModerationRepo) PurgeHistory(ctx context.Context, input repo.HistoryPurgeInput) (int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	query, args := buildHistoryDeleteQuery(input.Filter)
	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := insertHistoryTx(ctx, tx, historyInsert{
		ActedAt:       input.PurgedAt,
		ActionType:    domain.ActionHistoryPurged,
		TargetType:    domain.ModerationTargetHistory,
		TargetID:      rowsAffected,
		Actor:         input.Actor,
		CurrentStatus: string(domain.ModerationStatusPurged),
		Note:          strings.TrimSpace(input.Note),
	}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return rowsAffected, nil
}

func scanHistoryRecord(s scanner) (*domain.ModerationHistoryRecord, error) {
	var (
		record                   domain.ModerationHistoryRecord
		actedAt                  int64
		linkedPreviousDecisionID sql.NullInt64
	)
	if err := s.Scan(&record.ID, &actedAt, &record.ActionType, &record.TargetType, &record.TargetID, &record.ContentAuthor.ID, &record.ContentAuthor.DisplayName,
		&record.Actor.ID, &record.Actor.Username, &record.Actor.DisplayName, &record.ActorRole, &record.Reason, &record.Note, &record.CurrentStatus,
		&record.RouteToRole, &linkedPreviousDecisionID, &record.PostTitleSnapshot, &record.PostBodySnapshot, &record.CommentBodySnapshot); err != nil {
		if err == sql.ErrNoRows {
			return nil, repo.ErrNotFound
		}
		return nil, err
	}
	record.ActedAt = unixToTime(actedAt)
	record.Actor.Role = domain.NormalizeUserRole(string(record.ActorRole))
	record.Actor.Badges = domain.StaffBadgesForRole(record.Actor.Role)
	if linkedPreviousDecisionID.Valid && linkedPreviousDecisionID.Int64 > 0 {
		record.LinkedPreviousDecisionID = &linkedPreviousDecisionID.Int64
	}
	return &record, nil
}

func buildHistoryDeleteQuery(filter domain.ModerationHistoryFilter) (string, []any) {
	var (
		query strings.Builder
		args  []any
	)
	query.WriteString(`DELETE FROM moderation_history WHERE 1 = 1`)
	if strings.TrimSpace(filter.ActionType) != "" {
		query.WriteString(` AND action_type = ?`)
		args = append(args, strings.TrimSpace(filter.ActionType))
	}
	if strings.TrimSpace(filter.TargetType) != "" {
		query.WriteString(` AND target_type = ?`)
		args = append(args, strings.TrimSpace(filter.TargetType))
	}
	if strings.TrimSpace(filter.Status) != "" {
		query.WriteString(` AND current_status = ?`)
		args = append(args, strings.TrimSpace(filter.Status))
	}
	if filter.From != nil && !filter.From.IsZero() {
		query.WriteString(` AND acted_at >= ?`)
		args = append(args, timeToUnix(*filter.From))
	}
	if filter.To != nil && !filter.To.IsZero() {
		query.WriteString(` AND acted_at <= ?`)
		args = append(args, timeToUnix(*filter.To))
	}
	return query.String(), args
}

func hasCommentDescendantsTx(ctx context.Context, tx *sql.Tx, commentID int64) (bool, error) {
	row := tx.QueryRowContext(ctx, `
		WITH RECURSIVE descendants(id) AS (
			SELECT id FROM comments WHERE parent_id = ?
			UNION ALL
			SELECT c.id FROM comments c JOIN descendants d ON c.parent_id = d.id
		)
		SELECT 1 FROM descendants LIMIT 1
	`, commentID)
	var marker int
	if err := row.Scan(&marker); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return marker == 1, nil
}

func loadCommentParentIDTx(ctx context.Context, tx *sql.Tx, commentID int64) (*int64, error) {
	row := tx.QueryRowContext(ctx, `SELECT parent_id FROM comments WHERE id = ?`, commentID)
	var parentID sql.NullInt64
	if err := row.Scan(&parentID); err != nil {
		if err == sql.ErrNoRows {
			return nil, repo.ErrNotFound
		}
		return nil, err
	}
	if !parentID.Valid || parentID.Int64 <= 0 {
		return nil, nil
	}
	value := parentID.Int64
	return &value, nil
}

func cleanupDeletedCommentAncestorsTx(ctx context.Context, tx *sql.Tx, parentID *int64) error {
	currentParentID := parentID
	for currentParentID != nil && *currentParentID > 0 {
		var (
			nextParentID sql.NullInt64
			deletedAt    sql.NullInt64
		)
		row := tx.QueryRowContext(ctx, `SELECT parent_id, deleted_at FROM comments WHERE id = ?`, *currentParentID)
		if err := row.Scan(&nextParentID, &deletedAt); err != nil {
			if err == sql.ErrNoRows {
				return nil
			}
			return err
		}
		if !deletedAt.Valid || deletedAt.Int64 == 0 {
			return nil
		}
		hasDescendants, err := hasCommentDescendantsTx(ctx, tx, *currentParentID)
		if err != nil {
			return err
		}
		if hasDescendants {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM comments WHERE id = ?`, *currentParentID); err != nil {
			return err
		}
		if !nextParentID.Valid || nextParentID.Int64 <= 0 {
			currentParentID = nil
			continue
		}
		value := nextParentID.Int64
		currentParentID = &value
	}
	return nil
}

func loadCategoryTx(ctx context.Context, tx *sql.Tx, categoryID int64) (*domain.Category, error) {
	row := tx.QueryRowContext(ctx, `SELECT id, code, name, is_system FROM categories WHERE id = ?`, categoryID)
	return scanCategory(row)
}

func loadCategoryByCodeTx(ctx context.Context, tx *sql.Tx, code string) (*domain.Category, error) {
	row := tx.QueryRowContext(ctx, `SELECT id, code, name, is_system FROM categories WHERE code = ?`, strings.TrimSpace(code))
	return scanCategory(row)
}

func loadCategoryPostIDsTx(ctx context.Context, tx *sql.Tx, categoryID int64) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT post_id FROM post_categories WHERE category_id = ?`, categoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var postIDs []int64
	for rows.Next() {
		var postID int64
		if err := rows.Scan(&postID); err != nil {
			return nil, err
		}
		postIDs = append(postIDs, postID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return postIDs, nil
}

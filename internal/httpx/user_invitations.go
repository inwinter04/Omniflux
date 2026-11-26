package httpx

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/inwinter04/omniflux/internal/auth"
)

type inviteUserRequest struct {
	Email             string `json:"email"`
	Role              string `json:"role"`
	PermissionGroupID string `json:"permission_group_id"`
	OrganizationID    string `json:"organization_id"`
}

func (d ServerDeps) InviteUser(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermUserManage) != nil {
		return
	}
	var body inviteUserRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	email := normalizeEmail(body.Email)
	role := strings.TrimSpace(body.Role)
	if email == "" || !strings.Contains(email, "@") {
		adminError(w, http.StatusBadRequest, "USER_EMAIL_INVALID", "invalid-email-format", nil)
		return
	}
	targetOrgID, targetOrgOK := d.inviteTargetOrganization(w, r, principal, body.OrganizationID)
	if !targetOrgOK {
		return
	}

	var permissionGroupID any
	var permissionGroupPtr *uuid.UUID
	groupIDParam := strings.TrimSpace(body.PermissionGroupID)
	if groupIDParam != "" {
		parsedGroupID, parseErr := uuid.Parse(groupIDParam)
		if parseErr != nil {
			adminError(w, http.StatusBadRequest, "PERMISSION_GROUP_ID_INVALID", "invalid-permission-group-id", nil)
			return
		}
		var groupPerms []string
		if err := d.Pool.QueryRow(r.Context(), `
			SELECT permissions FROM permission_groups WHERE id = $1 AND organization_id = $2
		`, parsedGroupID, targetOrgID).Scan(&groupPerms); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				adminError(w, http.StatusBadRequest, "PERMISSION_GROUP_NOT_FOUND", "permission-group-does-not-exist", nil)
				return
			}
			adminError(w, http.StatusInternalServerError, "USER_INVITE_FAILED", "failed-to-invite-user", nil)
			return
		}
		if !ensureWithinPrincipalPermissions(w, principal, auth.NormalizePermissions(groupPerms)) {
			return
		}
		role = "viewer"
		permissionGroupID = parsedGroupID
		permissionGroupPtr = &parsedGroupID
	} else {
		if !assignableRoles[role] {
			adminError(w, http.StatusBadRequest, "USER_ROLE_INVALID", "roles-support-only-org-admin-project", nil)
			return
		}
		if !ensureWithinPrincipalPermissions(w, principal, auth.PresetPermissions(role)) {
			return
		}
	}

	if !d.requireEmailService(r.Context(), w) {
		return
	}
	var count int
	if err := d.Pool.QueryRow(r.Context(), `SELECT count(*) FROM users WHERE email=$1`, email).Scan(&count); err != nil {
		adminError(w, http.StatusInternalServerError, "USER_INVITE_FAILED", "failed-to-invite-user", nil)
		return
	}
	if count > 0 {
		adminError(w, http.StatusConflict, "USER_EMAIL_EXISTS", "this-email-is-already-registered", nil)
		return
	}
	temporaryPassword, err := auth.GeneratePassword()
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USER_INVITE_FAILED", "failed-to-invite-user", nil)
		return
	}
	temporaryHash, err := auth.HashPassword(temporaryPassword)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USER_INVITE_FAILED", "failed-to-invite-user", nil)
		return
	}
	userID := uuid.New()
	var token string
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		if _, insertErr := tx.Exec(r.Context(), `
			INSERT INTO users(id,organization_id,email,password_hash,role,permission_group_id,status)
			VALUES($1,$2,$3,$4,$5,$6,'pending_email_verification')
		`, userID, targetOrgID, email, temporaryHash, role, permissionGroupID); insertErr != nil {
			return insertErr
		}
		var tokenErr error
		token, _, tokenErr = d.createEmailToken(r.Context(), tx, createEmailTokenParams{
			purpose: "user_invitation", email: email, organizationID: &targetOrgID,
			userID: &userID, role: role, permissionGroupID: permissionGroupPtr, ttl: userInvitationTTL,
		})
		if tokenErr != nil {
			return tokenErr
		}
		createdState := map[string]any{"email": email, "role": role, "status": "pending_email_verification"}
		invitedState := map[string]any{"email": email, "role": role}
		if permissionGroupPtr != nil {
			createdState["permission_group_id"] = permissionGroupPtr.String()
			invitedState["permission_group_id"] = permissionGroupPtr.String()
		}
		if auditErr := writeAuditEvent(r.Context(), tx, targetOrgID, principal.UserID, "user.created", "user", userID.String(), nil, createdState); auditErr != nil {
			return auditErr
		}
		return writeAuditEvent(r.Context(), tx, targetOrgID, principal.UserID, "user.invited", "user", userID.String(), nil, invitedState)
	})
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USER_INVITE_FAILED", "failed-to-invite-user", nil)
		return
	}
	if err := d.sendEmailToken(r.Context(), "user_invitation", email, token); err != nil {
		adminError(w, http.StatusBadGateway, "USER_INVITE_EMAIL_FAILED", "failed-to-send-invitation-email", nil)
		return
	}
	adminOK(w, map[string]string{"status": "invitation_email_sent"})
}

func (d ServerDeps) ResendUserInvitation(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermUserManage) != nil {
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "USER_ID_INVALID", "invalid-user-id", nil)
		return
	}
	if !d.requireEmailService(r.Context(), w) {
		return
	}
	var email string
	var organizationID uuid.UUID
	var role string
	var groupID uuid.NullUUID
	if isPlatformAdmin(principal) {
		err = d.Pool.QueryRow(r.Context(), `
			SELECT id,email,organization_id,role,permission_group_id FROM users WHERE id=$1 AND status='pending_email_verification'
		`, userID).Scan(&userID, &email, &organizationID, &role, &groupID)
	} else {
		err = d.Pool.QueryRow(r.Context(), `
			SELECT id,email,organization_id,role,permission_group_id FROM users WHERE id=$1 AND organization_id=$2 AND status='pending_email_verification'
		`, userID, principal.OrganizationID).Scan(&userID, &email, &organizationID, &role, &groupID)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "USER_INVITE_NOT_FOUND", "pending-invitation-does-not-exist", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USER_INVITE_RESEND_FAILED", "failed-to-resend-invitation", nil)
		return
	}
	var groupPtr *uuid.UUID
	if groupID.Valid {
		id := groupID.UUID
		groupPtr = &id
	}
	var token string
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var tokenErr error
		token, _, tokenErr = d.createEmailToken(r.Context(), tx, createEmailTokenParams{
			purpose: "user_invitation", email: email, organizationID: &organizationID,
			userID: &userID, role: role, permissionGroupID: groupPtr, ttl: userInvitationTTL,
		})
		if tokenErr != nil {
			return tokenErr
		}
		return writeAuditEvent(r.Context(), tx, organizationID, principal.UserID, "user.invitation_resent", "user", userID.String(), nil, map[string]any{"email": email})
	})
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USER_INVITE_RESEND_FAILED", "failed-to-resend-invitation", nil)
		return
	}
	if err := d.sendEmailToken(r.Context(), "user_invitation", email, token); err != nil {
		adminError(w, http.StatusBadGateway, "USER_INVITE_EMAIL_FAILED", "failed-to-send-invitation-email", nil)
		return
	}
	adminOK(w, map[string]string{"status": "invitation_email_sent"})
}

func (d ServerDeps) AcceptInvitation(w http.ResponseWriter, r *http.Request) {
	var body resetPasswordRequest
	if !decodeStrictJSON(w, r, &body) {
		return
	}
	if err := auth.ValidatePassword(body.Password); err != nil {
		adminError(w, http.StatusBadRequest, "USER_PASSWORD_WEAK", "password-policy-unsatisfied", nil)
		return
	}
	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USER_INVITE_ACCEPT_FAILED", "failed-to-accept-invitation", nil)
		return
	}
	var sessionSecret string
	var response loginTokenResponse
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		record, consumeErr := d.consumeEmailToken(r.Context(), tx, "user_invitation", body.Token)
		if consumeErr != nil {
			return consumeErr
		}
		role, roleOK := roleFromText(record.Role)
		if !record.OrganizationID.Valid || !record.UserID.Valid || !roleOK || !assignableRoles[role] {
			return pgx.ErrNoRows
		}
		userID := record.UserID.UUID
		tag, updateErr := tx.Exec(r.Context(), `
			UPDATE users SET password_hash=$3,status='active',email_verified_at=now(),last_login_at=now(),updated_at=now()
			WHERE id=$1 AND email=$2 AND organization_id=$4 AND role=$5 AND status='pending_email_verification'
		`, userID, record.Email, hash, record.OrganizationID.UUID, role)
		if updateErr != nil {
			return updateErr
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		secret, sessionErr := d.Auth.CreateWithTx(r.Context(), tx, userID, clientIP(r), r.UserAgent())
		if sessionErr != nil {
			return sessionErr
		}
		sessionSecret = secret
		response = loginTokenResponse{Token: "cookie", User: adminUserResponse{
			UserID: userID.String(), OrganizationID: record.OrganizationID.UUID.String(), Email: record.Email, Role: role,
		}}
		if err := writeAuditEvent(r.Context(), tx, record.OrganizationID.UUID, userID, "user.created", "user", userID.String(), nil, map[string]any{"email": record.Email, "role": role}); err != nil {
			return err
		}
		return writeAuditEvent(r.Context(), tx, record.OrganizationID.UUID, userID, "auth.invitation_accepted", "user", userID.String(), nil, map[string]any{"email": record.Email})
	})
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusBadRequest, "EMAIL_TOKEN_INVALID", "email-token-is-invalid-or-expired", nil)
		return
	}
	if isUniqueViolation(err) {
		adminError(w, http.StatusConflict, "USER_EMAIL_EXISTS", "this-email-is-already-registered", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USER_INVITE_ACCEPT_FAILED", "failed-to-accept-invitation", nil)
		return
	}
	setSessionCookie(w, sessionSecret, int(auth.SessionTTL/time.Second), cookieSecure(d.Env))
	adminOK(w, response)
}

func (d ServerDeps) inviteTargetOrganization(w http.ResponseWriter, r *http.Request, principal auth.Principal, organizationID string) (uuid.UUID, bool) {
	target := principal.OrganizationID
	if organizationID != "" {
		if !isPlatformAdmin(principal) {
			adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "only-platform-admins-can-assign-a", nil)
			return uuid.UUID{}, false
		}
		parsed, err := uuid.Parse(organizationID)
		if err != nil {
			adminError(w, http.StatusBadRequest, "USER_ORG_INVALID", "invalid-organization-id-t4rbtt", nil)
			return uuid.UUID{}, false
		}
		var count int
		if err := d.Pool.QueryRow(r.Context(), `SELECT count(*) FROM organizations WHERE id=$1`, parsed).Scan(&count); err != nil || count == 0 {
			adminError(w, http.StatusBadRequest, "USER_ORG_NOT_FOUND", "organization-does-not-exist", nil)
			return uuid.UUID{}, false
		}
		target = parsed
	}
	return target, true
}

func (d ServerDeps) requireEmailService(ctx context.Context, w http.ResponseWriter) bool {
	settings, err := d.Mailer.Config(ctx)
	if err != nil || !settings.Enabled {
		adminError(w, http.StatusPreconditionFailed, "EMAIL_SERVICE_NOT_CONFIGURED", "email-service-is-not-configured", nil)
		return false
	}
	return true
}

func (d ServerDeps) RevokeUserInvitation(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermUserManage) != nil {
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "USER_ID_INVALID", "invalid-user-id", nil)
		return
	}
	var organizationID uuid.UUID
	if isPlatformAdmin(principal) {
		err = d.Pool.QueryRow(r.Context(), `SELECT organization_id FROM users WHERE id=$1 AND status='pending_email_verification'`, userID).Scan(&organizationID)
	} else {
		err = d.Pool.QueryRow(r.Context(), `SELECT organization_id FROM users WHERE id=$1 AND organization_id=$2 AND status='pending_email_verification'`, userID, principal.OrganizationID).Scan(&organizationID)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "USER_INVITE_NOT_FOUND", "pending-invitation-does-not-exist", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USER_INVITE_REVOKE_FAILED", "failed-to-revoke-invitation", nil)
		return
	}
	err = d.execWithAudit(r.Context(), `
		UPDATE email_tokens SET consumed_at=now()
		WHERE user_id=$1 AND purpose='user_invitation' AND consumed_at IS NULL
	`, []any{userID}, organizationID, principal.UserID, "user.invitation_revoked", "user", userID.String(), nil, nil)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USER_INVITE_REVOKE_FAILED", "failed-to-revoke-invitation", nil)
		return
	}
	adminOK(w, map[string]string{"status": "invitation_revoked"})
}

func (d ServerDeps) InviteOptions(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireAnyPermission(w, principal, auth.PermUserRead, auth.PermUserManage) != nil {
		return
	}
	settings, err := d.Mailer.Config(r.Context())
	if err != nil {
		settings.Enabled = false
	}
	adminOK(w, map[string]bool{"email_enabled": settings.Enabled})
}

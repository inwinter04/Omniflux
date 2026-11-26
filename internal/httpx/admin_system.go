package httpx

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/inwinter04/omniflux/internal/auth"
)

var registrationRoles = map[string]bool{
	"pending":   true,
	"developer": true,
	"viewer":    true,
}

type systemSettingsResponse struct {
	PublicRegistrationEnabled  bool    `json:"public_registration_enabled"`
	RegistrationOrganizationID *string `json:"registration_organization_id"`
	RegistrationRole           string  `json:"registration_role"`
	EmailVerificationRequired  bool    `json:"email_verification_required"`
	UpdatedBy                  *string `json:"updated_by"`
	UpdatedAt                  string  `json:"updated_at"`
}

type updateSystemSettingsRequest struct {
	PublicRegistrationEnabled  bool   `json:"public_registration_enabled"`
	RegistrationOrganizationID string `json:"registration_organization_id"`
	EmailVerificationRequired  bool   `json:"email_verification_required"`
	RegistrationRole           string `json:"registration_role"`
}

type systemSettingsRecord struct {
	PublicRegistrationEnabled  bool
	RegistrationOrganizationID *uuid.UUID
	RegistrationRole           string
	EmailVerificationRequired  bool
	UpdatedBy                  *uuid.UUID
	UpdatedAt                  time.Time
}

// SystemSettings 返回平台级系统配置;仅平台管理员可访问。
func (d ServerDeps) SystemSettings(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		return
	}
	if !isPlatformAdmin(principal) {
		adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "platform-admin-permission-is-required", nil)
		return
	}
	settings, err := d.readSystemSettings(r.Context())
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusInternalServerError, "SYSTEM_SETTINGS_MISSING", "system-settings-record-is-missing", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "SYSTEM_SETTINGS_READ_FAILED", "failed-to-query-system-settings", nil)
		return
	}
	adminOK(w, newSystemSettingsResponse(settings))
}

// UpdateSystemSettings 修改平台级系统配置;仅平台管理员可访问。
func (d ServerDeps) UpdateSystemSettings(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		return
	}
	if !isPlatformAdmin(principal) {
		adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "platform-admin-permission-is-required", nil)
		return
	}
	var body updateSystemSettingsRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	current, err := d.readSystemSettings(r.Context())
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusInternalServerError, "SYSTEM_SETTINGS_MISSING", "system-settings-record-is-missing", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "SYSTEM_SETTINGS_READ_FAILED", "failed-to-query-system-settings", nil)
		return
	}

	next := current
	next.PublicRegistrationEnabled = body.PublicRegistrationEnabled
	next.EmailVerificationRequired = body.EmailVerificationRequired
	body.RegistrationOrganizationID = strings.TrimSpace(body.RegistrationOrganizationID)
	if body.RegistrationOrganizationID == "" {
		next.RegistrationOrganizationID = nil
	} else {
		parsed, parseErr := uuid.Parse(body.RegistrationOrganizationID)
		if parseErr != nil {
			adminError(w, http.StatusBadRequest, "SYSTEM_SETTINGS_ORG_INVALID", "invalid-registration-organization", nil)
			return
		}
		next.RegistrationOrganizationID = &parsed
	}
	body.RegistrationRole = strings.TrimSpace(body.RegistrationRole)
	if !registrationRoles[body.RegistrationRole] {
		adminError(w, http.StatusBadRequest, "SYSTEM_SETTINGS_ROLE_INVALID", "registration-role-supports-only-developer-or-viewer", nil)
		return
	}
	next.RegistrationRole = body.RegistrationRole
	if next.PublicRegistrationEnabled && next.RegistrationOrganizationID == nil {
		adminError(w, http.StatusBadRequest, "SYSTEM_SETTINGS_ORG_REQUIRED", "public-registration-requires-an-organization", nil)
		return
	}
	if next.EmailVerificationRequired {
		var emailEnabled bool
		if err := d.Pool.QueryRow(r.Context(), `SELECT enabled FROM email_settings WHERE singleton`).Scan(&emailEnabled); err != nil {
			adminError(w, http.StatusInternalServerError, "SYSTEM_SETTINGS_READ_FAILED", "failed-to-query-system-settings", nil)
			return
		}
		if !emailEnabled {
			adminError(w, http.StatusBadRequest, "SYSTEM_SETTINGS_EMAIL_REQUIRED", "email-verification-requires-email-service", nil)
			return
		}
	}
	if next.RegistrationOrganizationID != nil {
		var count int
		if err := d.Pool.QueryRow(r.Context(), `SELECT count(*) FROM organizations WHERE id = $1`, *next.RegistrationOrganizationID).Scan(&count); err != nil {
			adminError(w, http.StatusInternalServerError, "SYSTEM_SETTINGS_UPDATE_FAILED", "failed-to-update-system-settings", nil)
			return
		}
		if count == 0 {
			adminError(w, http.StatusBadRequest, "SYSTEM_SETTINGS_ORG_NOT_FOUND", "organization-does-not-exist", nil)
			return
		}
	}

	var organizationID any
	if next.RegistrationOrganizationID != nil {
		organizationID = *next.RegistrationOrganizationID
	}
	err = d.execWithAudit(r.Context(), `
		UPDATE system_settings SET
			public_registration_enabled = $2,
			registration_organization_id = $3,
			registration_role = $4, email_verification_required = $5,
			updated_by = $6,
			updated_at = now()
		WHERE singleton = $1::boolean
	`, []any{true, next.PublicRegistrationEnabled, organizationID, next.RegistrationRole, next.EmailVerificationRequired, principal.UserID},
		principal.OrganizationID, principal.UserID, "system.settings_updated", "system_settings", "singleton",
		map[string]any{
			"public_registration_enabled": current.PublicRegistrationEnabled, "email_verification_required": current.EmailVerificationRequired,
			"registration_organization_id": uuidString(current.RegistrationOrganizationID),
			"registration_role":            current.RegistrationRole,
		},
		map[string]any{
			"public_registration_enabled": next.PublicRegistrationEnabled, "email_verification_required": next.EmailVerificationRequired,
			"registration_organization_id": uuidString(next.RegistrationOrganizationID),
			"registration_role":            next.RegistrationRole,
		})
	if err != nil {
		adminError(w, http.StatusInternalServerError, "SYSTEM_SETTINGS_UPDATE_FAILED", "failed-to-update-system-settings", nil)
		return
	}
	updated, err := d.readSystemSettings(r.Context())
	if err != nil {
		adminError(w, http.StatusInternalServerError, "SYSTEM_SETTINGS_READ_FAILED", "failed-to-query-system-settings", nil)
		return
	}
	adminOK(w, newSystemSettingsResponse(updated))
}

// RegistrationOptions 只暴露匿名登录页需要的注册开关,不泄露组织与角色配置。
func (d ServerDeps) RegistrationOptions(w http.ResponseWriter, r *http.Request) {
	var enabled, emailVerificationRequired bool
	err := d.Pool.QueryRow(r.Context(), `
		SELECT public_registration_enabled
		   AND registration_organization_id IS NOT NULL
		   AND registration_role IN ('pending', 'developer', 'viewer'),
		   email_verification_required
		FROM system_settings WHERE singleton
	`).Scan(&enabled, &emailVerificationRequired)
	if errors.Is(err, pgx.ErrNoRows) {
		enabled = false
	} else if err != nil {
		adminError(w, http.StatusInternalServerError, "REGISTRATION_OPTIONS_FAILED", "failed-to-query-registration-options", nil)
		return
	}
	adminOK(w, map[string]bool{"enabled": enabled, "email_verification_required": emailVerificationRequired})
}

func (d ServerDeps) readSystemSettings(ctx context.Context) (systemSettingsRecord, error) {
	var record systemSettingsRecord
	var organizationID uuid.NullUUID
	var updatedBy uuid.NullUUID
	err := d.Pool.QueryRow(ctx, `
		SELECT public_registration_enabled, registration_organization_id, registration_role, email_verification_required, updated_by, updated_at
		FROM system_settings WHERE singleton
	`).Scan(&record.PublicRegistrationEnabled, &organizationID, &record.RegistrationRole, &record.EmailVerificationRequired, &updatedBy, &record.UpdatedAt)
	if err != nil {
		return record, err
	}
	if organizationID.Valid {
		value := organizationID.UUID
		record.RegistrationOrganizationID = &value
	}
	if updatedBy.Valid {
		value := updatedBy.UUID
		record.UpdatedBy = &value
	}
	return record, nil
}

func newSystemSettingsResponse(record systemSettingsRecord) systemSettingsResponse {
	response := systemSettingsResponse{
		PublicRegistrationEnabled: record.PublicRegistrationEnabled,
		EmailVerificationRequired: record.EmailVerificationRequired,
		RegistrationRole:          record.RegistrationRole,
		UpdatedAt:                 record.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if record.RegistrationOrganizationID != nil {
		value := record.RegistrationOrganizationID.String()
		response.RegistrationOrganizationID = &value
	}
	if record.UpdatedBy != nil {
		value := record.UpdatedBy.String()
		response.UpdatedBy = &value
	}
	return response
}

func uuidString(value *uuid.UUID) any {
	if value == nil {
		return nil
	}
	return value.String()
}

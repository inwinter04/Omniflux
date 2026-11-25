package httpx

import (
	"errors"
	"fmt"
	"net/http"
	netmail "net/mail"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/inwinter04/omniflux/internal/auth"

	"github.com/inwinter04/omniflux/internal/mail"
	"github.com/inwinter04/omniflux/internal/vault"
)

type emailSettingsResponse struct {
	Enabled       bool   `json:"enabled"`
	SMTPHost      string `json:"smtp_host"`
	SMTPPort      int    `json:"smtp_port"`
	SMTPUsername  string `json:"smtp_username"`
	FromAddress   string `json:"from_address"`
	FromName      string `json:"from_name"`
	Encryption    string `json:"encryption"`
	PublicBaseURL string `json:"public_base_url"`
	HasPassword   bool   `json:"has_password"`
}

type updateEmailSettingsRequest struct {
	Enabled       bool    `json:"enabled"`
	SMTPHost      string  `json:"smtp_host"`
	SMTPPort      int     `json:"smtp_port"`
	SMTPUsername  string  `json:"smtp_username"`
	SMTPPassword  *string `json:"smtp_password"`
	FromAddress   string  `json:"from_address"`
	FromName      string  `json:"from_name"`
	Encryption    string  `json:"encryption"`
	PublicBaseURL string  `json:"public_base_url"`
}

type testEmailSettingsRequest struct {
	To string `json:"to"`
}

func (d ServerDeps) EmailSettings(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		return
	}
	if !isPlatformAdmin(principal) {
		adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "platform-admin-permission-is-required", nil)
		return
	}
	settings, err := d.Mailer.Config(r.Context())
	if errors.Is(err, mail.ErrNotConfigured) {
		adminOK(w, emailSettingsResponse{})
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "EMAIL_SETTINGS_READ_FAILED", "failed-to-query-email-settings", nil)
		return
	}
	adminOK(w, emailSettingsResponse{
		Enabled: settings.Enabled, SMTPHost: settings.SMTPHost, SMTPPort: settings.SMTPPort,
		SMTPUsername: settings.SMTPUsername, FromAddress: settings.FromAddress, FromName: settings.FromName,
		Encryption: settings.Encryption, PublicBaseURL: settings.PublicBaseURL, HasPassword: settings.HasPassword,
	})
}

func (d ServerDeps) UpdateEmailSettings(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		return
	}
	if !isPlatformAdmin(principal) {
		adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "platform-admin-permission-is-required", nil)
		return
	}
	var body updateEmailSettingsRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	current, err := d.Mailer.Config(r.Context())
	if err != nil && !errors.Is(err, mail.ErrNotConfigured) {
		adminError(w, http.StatusInternalServerError, "EMAIL_SETTINGS_READ_FAILED", "failed-to-query-email-settings", nil)
		return
	}
	normalized, password, validationErr := normalizeEmailSettings(body, current)
	if validationErr != "" {
		adminError(w, http.StatusBadRequest, "EMAIL_SETTINGS_INVALID", validationErr, nil)
		return
	}
	if !normalized.Enabled {
		var verificationRequired bool
		if err := d.Pool.QueryRow(r.Context(), `SELECT email_verification_required FROM system_settings WHERE singleton`).Scan(&verificationRequired); err != nil {
			adminError(w, http.StatusInternalServerError, "SYSTEM_SETTINGS_READ_FAILED", "failed-to-query-system-settings", nil)
			return
		}
		if verificationRequired {
			adminError(w, http.StatusPreconditionFailed, "EMAIL_SETTINGS_STILL_REQUIRED", "email-verification-still-requires-email", nil)
			return
		}
	}

	var ciphertext any
	passwordColumn := "smtp_password_ciphertext"
	if password != nil || normalized.SMTPUsername == "" {
		if password != nil {
			encrypted, encryptErr := vault.Encrypt(d.MasterKey, []byte(*password))
			if encryptErr != nil {
				adminError(w, http.StatusInternalServerError, "EMAIL_SETTINGS_UPDATE_FAILED", "failed-to-update-email-settings", nil)
				return
			}
			ciphertext = encrypted
		} else {
			ciphertext = nil
		}
		passwordColumn = "$6"
	}
	args := []any{true, normalized.Enabled, normalized.SMTPHost, normalized.SMTPPort, normalized.SMTPUsername}
	nextArg := 6
	if passwordColumn == "$6" {
		args = append(args, ciphertext)
		nextArg++
	}
	query := fmt.Sprintf(`
		UPDATE email_settings SET
			enabled=$2, smtp_host=$3, smtp_port=$4, smtp_username=$5,
			smtp_password_ciphertext=%s,
			from_address=$%d, from_name=$%d, encryption=$%d, public_base_url=$%d,
			updated_by=$%d, updated_at=now()
		WHERE singleton = $1::boolean
	`, passwordColumn, nextArg, nextArg+1, nextArg+2, nextArg+3, nextArg+4)
	args = append(args, normalized.FromAddress, normalized.FromName, normalized.Encryption, normalized.PublicBaseURL, principal.UserID)
	err = d.execWithAudit(r.Context(), query, args,
		principal.OrganizationID, principal.UserID, "email.settings_updated", "email_settings", "singleton",
		map[string]any{"enabled": current.Enabled, "smtp_host": current.SMTPHost, "from_address": current.FromAddress},
		map[string]any{"enabled": normalized.Enabled, "smtp_host": normalized.SMTPHost, "from_address": normalized.FromAddress})
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusInternalServerError, "EMAIL_SETTINGS_MISSING", "email-settings-record-is-missing", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "EMAIL_SETTINGS_UPDATE_FAILED", "failed-to-update-email-settings", nil)
		return
	}
	d.writeEmailSettings(w, r)
}

func (d ServerDeps) TestEmailSettings(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		return
	}
	if !isPlatformAdmin(principal) {
		adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "platform-admin-permission-is-required", nil)
		return
	}
	var body testEmailSettingsRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	to := strings.ToLower(strings.TrimSpace(body.To))
	if _, err := netmail.ParseAddress(to); err != nil {
		adminError(w, http.StatusBadRequest, "EMAIL_TEST_RECIPIENT_INVALID", "invalid-email-test-recipient", nil)
		return
	}
	if err := d.Mailer.Send(r.Context(), mail.TestMessage(to)); err != nil {
		adminError(w, http.StatusBadGateway, "EMAIL_TEST_FAILED", "failed-to-send-test-email", nil)
		return
	}
	_, _ = d.Pool.Exec(r.Context(), `
		INSERT INTO audit_events(organization_id,actor_user_id,actor_type,action,resource_type,resource_id)
		VALUES($1,$2,'user','email.test_sent','email_settings','singleton')
	`, principal.OrganizationID, principal.UserID)
	adminOK(w, map[string]string{"status": "sent"})
}

func (d ServerDeps) writeEmailSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := d.Mailer.Config(r.Context())
	if err != nil {
		adminError(w, http.StatusInternalServerError, "EMAIL_SETTINGS_READ_FAILED", "failed-to-query-email-settings", nil)
		return
	}
	adminOK(w, emailSettingsResponse{
		Enabled: settings.Enabled, SMTPHost: settings.SMTPHost, SMTPPort: settings.SMTPPort,
		SMTPUsername: settings.SMTPUsername, FromAddress: settings.FromAddress, FromName: settings.FromName,
		Encryption: settings.Encryption, PublicBaseURL: settings.PublicBaseURL, HasPassword: settings.HasPassword,
	})
}

func normalizeEmailSettings(body updateEmailSettingsRequest, current mail.StoredSettings) (mail.Settings, *string, string) {
	normalized := mail.Settings{
		Enabled: body.Enabled, SMTPHost: strings.TrimSpace(body.SMTPHost), SMTPPort: body.SMTPPort,
		SMTPUsername: strings.TrimSpace(body.SMTPUsername), FromAddress: strings.TrimSpace(body.FromAddress),
		FromName: strings.TrimSpace(body.FromName), Encryption: strings.TrimSpace(body.Encryption),
		PublicBaseURL: strings.TrimRight(strings.TrimSpace(body.PublicBaseURL), "/"),
	}
	password := body.SMTPPassword
	if password != nil && *password == "" {
		password = nil
	}
	if normalized.SMTPPort == 0 {
		normalized.SMTPPort = 587
	}
	if normalized.Encryption == "" {
		normalized.Encryption = "starttls"
	}
	if normalized.Encryption != "starttls" && normalized.Encryption != "tls" && normalized.Encryption != "none" {
		return normalized, password, "invalid-smtp-encryption"
	}
	if normalized.SMTPPort < 1 || normalized.SMTPPort > 65535 {
		return normalized, password, "invalid-smtp-port"
	}
	if strings.ContainsAny(normalized.SMTPHost, " \t\r\n") || strings.ContainsAny(normalized.SMTPUsername, " \t\r\n") {
		return normalized, password, "invalid-smtp-account"
	}
	if strings.ContainsAny(normalized.FromName, "\r\n") {
		return normalized, password, "invalid-from-name"
	}
	if normalized.FromAddress != "" {
		if address, err := netmail.ParseAddress(normalized.FromAddress); err != nil || address.Address != normalized.FromAddress {
			return normalized, password, "invalid-from-address"
		}
	}
	if normalized.PublicBaseURL != "" {
		parsed, err := url.Parse(normalized.PublicBaseURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
			return normalized, password, "invalid-public-base-url"
		}
	}
	if !normalized.Enabled {
		return normalized, password, ""
	}
	if normalized.SMTPHost == "" || normalized.FromAddress == "" || normalized.PublicBaseURL == "" {
		return normalized, password, "enabled-email-settings-require"
	}
	effectivePassword := current.HasPassword
	if password != nil {
		effectivePassword = *password != ""
	}
	if normalized.SMTPUsername != "" && normalized.Encryption == "none" {
		return normalized, password, "smtp-authentication-requires-tls"
	}
	if normalized.SMTPUsername != "" && !effectivePassword {
		return normalized, password, "smtp-password-is-required"
	}
	return normalized, password, ""
}

package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/inwinter04/omniflux/internal/auth"
)

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

const registrationRateLimitPerMinute = 5

// Register 按系统设置创建账号；需要邮箱验证时先发送验证邮件，验证后才建立会话。
func (d ServerDeps) Register(w http.ResponseWriter, r *http.Request) {
	var body registerRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		adminError(w, http.StatusBadRequest, "REGISTRATION_INVALID_REQUEST", "invalid-registration-request-format", nil)
		return
	}
	email := normalizeEmail(body.Email)
	if email == "" || !strings.Contains(email, "@") {
		adminError(w, http.StatusBadRequest, "REGISTRATION_EMAIL_INVALID", "invalid-email-format", nil)
		return
	}
	if len(body.Password) < 16 {
		adminError(w, http.StatusBadRequest, "REGISTRATION_PASSWORD_WEAK", "password-must-be-at-least-16", nil)
		return
	}
	ip := clientIP(r)
	agent := r.UserAgent()
	if !d.allowRegistration(r.Context(), ip) {
		adminError(w, http.StatusTooManyRequests, "REGISTRATION_RATE_LIMITED", "too-many-registration-attempts", emailRetryParams())
		return
	}

	var configuredOrganizationID uuid.UUID
	var configuredRole string
	var verificationRequired bool
	err := d.Pool.QueryRow(r.Context(), `
		SELECT registration_organization_id, registration_role, email_verification_required
		FROM system_settings
		WHERE singleton AND public_registration_enabled
		  AND registration_organization_id IS NOT NULL
		  AND registration_role IN ('developer', 'viewer')
	`).Scan(&configuredOrganizationID, &configuredRole, &verificationRequired)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusForbidden, "REGISTRATION_DISABLED", "registration-is-not-enabled", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "REGISTRATION_INTERNAL_ERROR", "registration-service-is-temporarily-unavailable", nil)
		return
	}
	if verificationRequired && !d.requireEmailService(r.Context(), w) {
		return
	}

	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "REGISTRATION_INTERNAL_ERROR", "registration-service-is-temporarily-unavailable", nil)
		return
	}
	userID := uuid.New()
	var organizationID uuid.UUID
	var role string
	var sessionSecret string
	var verificationToken string
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		settingsErr := tx.QueryRow(r.Context(), `
			SELECT registration_organization_id, registration_role, email_verification_required
			FROM system_settings
			WHERE singleton AND public_registration_enabled
			  AND registration_organization_id IS NOT NULL
			  AND registration_role IN ('developer', 'viewer')
			FOR SHARE
		`).Scan(&organizationID, &role, &verificationRequired)
		if errors.Is(settingsErr, pgx.ErrNoRows) {
			return errRegistrationDisabled
		}
		if settingsErr != nil {
			return settingsErr
		}
		if verificationRequired && d.Mailer == nil {
			return errEmailServiceUnavailable
		}

		var existingID, existingOrgID uuid.UUID
		var existingStatus string
		existingErr := tx.QueryRow(r.Context(), `
			SELECT id,organization_id,status FROM users WHERE email=$1 FOR SHARE
		`, email).Scan(&existingID, &existingOrgID, &existingStatus)
		switch {
		case existingErr == nil:
			if !verificationRequired || existingStatus != "pending_email_verification" {
				return errEmailExists
			}
			// 待验证邮箱再次注册时必须替换为本次提交的密码，避免攻击者预先注册邮箱后保留旧凭据。
			if _, updateErr := tx.Exec(r.Context(), `
			UPDATE users SET password_hash=$2,updated_at=now()
				WHERE id=$1 AND status='pending_email_verification'
			`, existingID, hash); updateErr != nil {
				return updateErr
			}
			token, _, tokenErr := d.createEmailToken(r.Context(), tx, createEmailTokenParams{
				purpose: "email_verification", email: email, organizationID: &existingOrgID,
				userID: &existingID, ttl: emailVerificationTTL,
			})
			if tokenErr != nil {
				return tokenErr
			}
			verificationToken = token
			return writeSystemAuditEvent(r.Context(), tx, &existingOrgID, "email.verification_sent", "user", existingID.String(), map[string]any{"email": email})
		case !errors.Is(existingErr, pgx.ErrNoRows):
			return existingErr
		}

		status := "active"
		if verificationRequired {
			status = "pending_email_verification"
		}
		if _, insertErr := tx.Exec(r.Context(), `
			INSERT INTO users(id,organization_id,email,password_hash,role,status,last_login_at)
			VALUES ($1,$2,$3,$4,$5,$6,CASE WHEN $6='active' THEN now() ELSE NULL END)
		`, userID, organizationID, email, hash, role, status); insertErr != nil {
			return insertErr
		}
		if _, auditErr := tx.Exec(r.Context(), `
			INSERT INTO audit_events(organization_id,actor_user_id,actor_type,action,resource_type,resource_id,before_state,after_state,ip_address,user_agent)
			VALUES ($1,$2,'user','user.self_registered','user',$3,'{}',$4,$5,$6)
		`, organizationID, userID, userID.String(), auditJSON(map[string]any{"email": email, "role": role, "status": status}), nullableInet(ip), agent); auditErr != nil {
			return auditErr
		}
		if verificationRequired {
			token, _, tokenErr := d.createEmailToken(r.Context(), tx, createEmailTokenParams{
				purpose: "email_verification", email: email, organizationID: &organizationID,
				userID: &userID, ttl: emailVerificationTTL,
			})
			if tokenErr != nil {
				return tokenErr
			}
			verificationToken = token
			if auditErr := writeSystemAuditEvent(r.Context(), tx, &organizationID, "email.verification_sent", "user", userID.String(), map[string]any{"email": email}); auditErr != nil {
				return auditErr
			}
			return nil
		}
		secret, sessionErr := d.Auth.CreateWithTx(r.Context(), tx, userID, ip, agent)
		if sessionErr != nil {
			return sessionErr
		}
		sessionSecret = secret
		return nil
	})
	if errors.Is(err, errRegistrationDisabled) {
		adminError(w, http.StatusForbidden, "REGISTRATION_DISABLED", "registration-is-not-enabled", nil)
		return
	}
	if errors.Is(err, errEmailExists) {
		if verificationRequired {
			w.Header().Set("Cache-Control", "no-store")
			adminOK(w, map[string]string{"status": "verification_email_sent"})
			return
		}
		adminError(w, http.StatusConflict, "REGISTRATION_EMAIL_EXISTS", "this-email-is-already-registered", nil)
		return
	}
	if isUniqueViolation(err) {
		if verificationRequired {
			w.Header().Set("Cache-Control", "no-store")
			adminOK(w, map[string]string{"status": "verification_email_sent"})
			return
		}
		adminError(w, http.StatusConflict, "REGISTRATION_EMAIL_EXISTS", "this-email-is-already-registered", nil)
		return
	}
	if errors.Is(err, errEmailServiceUnavailable) {
		adminError(w, http.StatusPreconditionFailed, "EMAIL_SERVICE_NOT_CONFIGURED", "email-service-is-not-configured", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "REGISTRATION_INTERNAL_ERROR", "registration-service-is-temporarily-unavailable", nil)
		return
	}
	if verificationRequired {
		if sendErr := d.sendEmailToken(r.Context(), "email_verification", email, verificationToken); sendErr != nil {
			adminError(w, http.StatusBadGateway, "REGISTRATION_EMAIL_SEND_FAILED", "failed-to-send-verification-email", nil)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		adminOK(w, map[string]string{"status": "verification_email_sent"})
		return
	}
	setSessionCookie(w, sessionSecret, int(auth.SessionTTL/time.Second), cookieSecure(d.Env))
	adminOK(w, loginTokenResponse{Token: "cookie", RefreshToken: "", User: adminUserResponse{
		UserID: userID.String(), OrganizationID: organizationID.String(), Email: email, Role: role,
	}})
}

var errRegistrationDisabled = errors.New("registration disabled")
var errEmailExists = errors.New("email exists")
var errEmailServiceUnavailable = errors.New("email service unavailable")

// allowRegistration 用 Redis 固定窗口限制同一 IP 的公开注册频率；测试依赖未注入 Redis 时保持可用。
func (d ServerDeps) allowRegistration(ctx context.Context, ip string) bool {
	if d.Redis == nil {
		return true
	}
	window := time.Now().Unix() / 60
	key := fmt.Sprintf("omniflux:registration-rl:%s:%d", ip, window)
	if err := d.Redis.SetNX(ctx, key, 0, 2*time.Minute).Err(); err != nil {
		return false
	}
	count, err := d.Redis.Incr(ctx, key).Result()
	if err != nil {
		return false
	}
	return count <= registrationRateLimitPerMinute
}

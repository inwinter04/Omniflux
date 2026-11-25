package httpx

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/inwinter04/omniflux/internal/auth"
	"github.com/inwinter04/omniflux/internal/mail"
)

type emailTokenRequest struct {
	Token string `json:"token"`
}

type emailOnlyRequest struct {
	Email string `json:"email"`
}

type resetPasswordRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

func (d ServerDeps) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	var body emailTokenRequest
	if !decodeStrictJSON(w, r, &body) {
		return
	}
	record, loginToken, sessionSecret, err := d.consumeEmailVerification(r, body.Token)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusBadRequest, "EMAIL_TOKEN_INVALID", "email-token-is-invalid-or-expired", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "EMAIL_VERIFY_FAILED", "failed-to-verify-email", nil)
		return
	}
	setSessionCookie(w, sessionSecret, int(auth.SessionTTL/time.Second), cookieSecure(d.Env))
	adminOK(w, loginToken)
	_ = record
}

func (d ServerDeps) ResendEmailVerification(w http.ResponseWriter, r *http.Request) {
	var body emailOnlyRequest
	if !decodeStrictJSON(w, r, &body) {
		return
	}
	email := normalizeEmail(body.Email)
	ip := clientIP(r)
	if !d.allowEmailTokenRequest(r.Context(), ip, "verification", email) {
		adminError(w, http.StatusTooManyRequests, "EMAIL_TOKEN_RATE_LIMITED", "too-many-email-token-requests", emailRetryParams())
		return
	}
	var userID, organizationID uuid.UUID
	err := d.Pool.QueryRow(r.Context(), `
		SELECT id,organization_id FROM users WHERE email=$1 AND status='pending_email_verification'
	`, email).Scan(&userID, &organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		adminOK(w, map[string]string{"status": "verification_email_sent"})
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "EMAIL_VERIFICATION_SEND_FAILED", "failed-to-send-verification-email", nil)
		return
	}
	var token string
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var tokenErr error
		token, _, tokenErr = d.createEmailToken(r.Context(), tx, createEmailTokenParams{
			purpose: "email_verification", email: email, organizationID: &organizationID,
			userID: &userID, ttl: emailVerificationTTL,
		})
		if tokenErr != nil {
			return tokenErr
		}
		return writeSystemAuditEvent(r.Context(), tx, &organizationID, "email.verification_resent", "user", userID.String(), map[string]any{"email": email})
	})
	if err == nil {
		err = d.sendEmailToken(r.Context(), "email_verification", email, token)
	}
	if err != nil {
		adminError(w, http.StatusBadGateway, "EMAIL_VERIFICATION_SEND_FAILED", "failed-to-send-verification-email", nil)
		return
	}
	adminOK(w, map[string]string{"status": "verification_email_sent"})
}

func (d ServerDeps) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var body emailOnlyRequest
	if !decodeStrictJSON(w, r, &body) {
		return
	}
	email := normalizeEmail(body.Email)
	ip := clientIP(r)
	if !d.allowEmailTokenRequest(r.Context(), ip, "reset", email) {
		adminError(w, http.StatusTooManyRequests, "EMAIL_TOKEN_RATE_LIMITED", "too-many-email-token-requests", emailRetryParams())
		return
	}
	var userID, organizationID uuid.UUID
	err := d.Pool.QueryRow(r.Context(), `
		SELECT id,organization_id FROM users WHERE email=$1 AND status='active'
	`, email).Scan(&userID, &organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		adminOK(w, map[string]string{"status": "reset_email_sent"})
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PASSWORD_RESET_REQUEST_FAILED", "failed-to-request-password-reset", nil)
		return
	}
	var token string
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var tokenErr error
		token, _, tokenErr = d.createEmailToken(r.Context(), tx, createEmailTokenParams{
			purpose: "password_reset", email: email, organizationID: &organizationID,
			userID: &userID, ttl: passwordResetTTL,
		})
		if tokenErr != nil {
			return tokenErr
		}
		return writeSystemAuditEvent(r.Context(), tx, &organizationID, "auth.password_reset_requested", "user", userID.String(), map[string]any{"email": email})
	})
	if err == nil {
		err = d.sendEmailToken(r.Context(), "password_reset", email, token)
	}
	if err != nil {
		adminError(w, http.StatusBadGateway, "PASSWORD_RESET_REQUEST_FAILED", "failed-to-request-password-reset", nil)
		return
	}
	adminOK(w, map[string]string{"status": "reset_email_sent"})
}

func (d ServerDeps) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var body resetPasswordRequest
	if !decodeStrictJSON(w, r, &body) {
		return
	}
	ip := clientIP(r)
	if !d.allowEmailTokenRequest(r.Context(), ip, "reset", body.Token) {
		adminError(w, http.StatusTooManyRequests, "EMAIL_TOKEN_RATE_LIMITED", "too-many-email-token-requests", emailRetryParams())
		return
	}
	tokenHash := auth.HashEmailToken(d.APIKeyPepper, "password_reset", body.Token)
	var tokenUserID uuid.UUID
	err := d.Pool.QueryRow(r.Context(), `
		SELECT user_id FROM email_tokens
		WHERE token_hash=$1 AND purpose='password_reset' AND consumed_at IS NULL AND expires_at>now()
	`, tokenHash).Scan(&tokenUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusBadRequest, "EMAIL_TOKEN_INVALID", "email-token-is-invalid-or-expired", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PASSWORD_RESET_FAILED", "failed-to-reset-password", nil)
		return
	}
	if len(body.Password) < 16 {
		adminError(w, http.StatusBadRequest, "PASSWORD_RESET_WEAK", "password-must-be-at-least-16", nil)
		return
	}
	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PASSWORD_RESET_FAILED", "failed-to-reset-password", nil)
		return
	}
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		record, consumeErr := d.consumeEmailToken(r.Context(), tx, "password_reset", body.Token)
		if consumeErr != nil {
			return consumeErr
		}
		if !record.UserID.Valid {
			return pgx.ErrNoRows
		}
		tag, updateErr := tx.Exec(r.Context(), `
			UPDATE users SET password_hash=$2,failed_login_count=0,locked_until=NULL,updated_at=now()
			WHERE id=$1 AND status='active'
		`, record.UserID.UUID, hash)
		if updateErr != nil {
			return updateErr
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		if _, err := tx.Exec(r.Context(), `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, record.UserID.UUID); err != nil {
			return err
		}
		return writeAuditEvent(r.Context(), tx, record.OrganizationID.UUID, record.UserID.UUID, "auth.password_reset", "user", record.UserID.UUID.String(), nil, map[string]any{"password_reset": true})
	})
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusBadRequest, "EMAIL_TOKEN_INVALID", "email-token-is-invalid-or-expired", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PASSWORD_RESET_FAILED", "failed-to-reset-password", nil)
		return
	}
	adminOK(w, map[string]string{"status": "password_reset"})
}

func (d ServerDeps) consumeEmailVerification(r *http.Request, token string) (emailTokenRecord, loginTokenResponse, string, error) {
	var record emailTokenRecord
	var response loginTokenResponse
	var sessionSecret string
	err := pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var consumeErr error
		record, consumeErr = d.consumeEmailToken(r.Context(), tx, "email_verification", token)
		if consumeErr != nil {
			return consumeErr
		}
		if !record.UserID.Valid {
			return pgx.ErrNoRows
		}
		tag, updateErr := tx.Exec(r.Context(), `
			UPDATE users SET status='active',email_verified_at=now(),updated_at=now()
			WHERE id=$1 AND status='pending_email_verification'
		`, record.UserID.UUID)
		if updateErr != nil {
			return updateErr
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		secret, sessionErr := d.Auth.CreateWithTx(r.Context(), tx, record.UserID.UUID, clientIP(r), r.UserAgent())
		if sessionErr != nil {
			return sessionErr
		}
		var role string
		if err := tx.QueryRow(r.Context(), `SELECT role FROM users WHERE id=$1`, record.UserID.UUID).Scan(&role); err != nil {
			return err
		}
		response = loginTokenResponse{Token: "cookie", User: adminUserResponse{
			UserID: record.UserID.UUID.String(), OrganizationID: record.OrganizationID.UUID.String(),
			Email: record.Email, Role: role,
		}}
		sessionSecret = secret
		return writeAuditEvent(r.Context(), tx, record.OrganizationID.UUID, record.UserID.UUID, "auth.email_verified", "user", record.UserID.UUID.String(), nil, map[string]any{"email_verified": true})
	})
	return record, response, sessionSecret, err
}

func (d ServerDeps) sendEmailToken(ctx context.Context, purpose, to, token string) error {
	settings, err := d.Mailer.Config(ctx)
	if err != nil {
		return err
	}
	baseURL := strings.TrimRight(settings.PublicBaseURL, "/")
	switch purpose {
	case "email_verification":
		return d.Mailer.Send(ctx, mail.VerificationMessage(to, token, baseURL))
	case "user_invitation":
		return d.Mailer.Send(ctx, mail.InvitationMessage(to, token, baseURL))
	case "password_reset":
		return d.Mailer.Send(ctx, mail.PasswordResetMessage(to, token, baseURL))
	default:
		return errors.New("unknown email token purpose")
	}
}

func (d ServerDeps) allowEmailTokenRequest(ctx context.Context, ip, kind, email string) bool {
	if d.Redis == nil {
		return true
	}
	window := time.Now().Unix() / 60
	ipKey := fmt.Sprintf("omniflux:email-token-ip-rl:%s:%s:%d", kind, ip, window)
	subjectHash := sha256.Sum256([]byte(kind + "\x00" + email))
	subjectKey := fmt.Sprintf("omniflux:email-token-subject-rl:%s:%s:%d", kind, base64.RawURLEncoding.EncodeToString(subjectHash[:]), window)
	if err := d.Redis.SetNX(ctx, ipKey, 0, 2*time.Minute).Err(); err != nil {
		return false
	}
	if err := d.Redis.SetNX(ctx, subjectKey, 0, 2*time.Minute).Err(); err != nil {
		return false
	}
	ipCount, err := d.Redis.Incr(ctx, ipKey).Result()
	if err != nil {
		return false
	}
	subjectCount, err := d.Redis.Incr(ctx, subjectKey).Result()
	if err != nil {
		return false
	}
	return ipCount <= 20 && subjectCount <= 3
}

func decodeStrictJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		adminError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid-request-body", nil, err.Error())
		return false
	}
	return true
}

func normalizeEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func emailRetryParams() map[string]any {
	return map[string]any{"retry_after_seconds": 60 - int(time.Now().Unix()%60)}
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func roleFromText(role pgtype.Text) (string, bool) {
	return role.String, role.Valid && role.String != ""
}

package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/inwinter04/omniflux/internal/auth"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type authResponse struct {
	UserID         string `json:"user_id"`
	OrganizationID string `json:"organization_id"`
	Email          string `json:"email"`
	Role           string `json:"role"`
}

type loginRecord struct {
	UserID         uuid.UUID
	OrganizationID uuid.UUID
	PasswordHash   string
	Role           string
	Status         string
	FailedCount    int
	LockedUntil    *time.Time
}

func (d ServerDeps) Login(w http.ResponseWriter, r *http.Request) {
	var body loginRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		adminError(w, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "invalid-login-request-format", nil)
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	if email == "" || body.Password == "" {
		adminError(w, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "enter-an-email-address-and-password", nil)
		return
	}
	ip := clientIP(r)
	agent := r.UserAgent()
	record := loginRecord{}
	err := d.Pool.QueryRow(r.Context(), `SELECT id,organization_id,password_hash,role,status,failed_login_count,locked_until FROM users WHERE email=$1`, email).Scan(&record.UserID, &record.OrganizationID, &record.PasswordHash, &record.Role, &record.Status, &record.FailedCount, &record.LockedUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		d.recordLogin(r.Context(), nil, nil, email, ip, agent, "failure")
		adminError(w, http.StatusUnauthorized, "AUTH_INVALID_CREDENTIALS", "incorrect-email-or-password", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "AUTH_INTERNAL_ERROR", "login-service-is-temporarily-unavailable", nil)
		return
	}
	now := time.Now()
	if record.Status != "active" || (record.LockedUntil != nil && record.LockedUntil.After(now)) {
		d.recordLogin(r.Context(), &record.OrganizationID, &record.UserID, email, ip, agent, "failure")
		if record.Status != "active" {
			adminError(w, http.StatusForbidden, "AUTH_ACCOUNT_DISABLED", "account-is-unavailable", nil)
			return
		}
		retryAfter := int(time.Until(*record.LockedUntil).Seconds())
		if retryAfter < 1 {
			retryAfter = 1
		}
		adminError(w, http.StatusTooManyRequests, "AUTH_ACCOUNT_LOCKED", "too-many-login-attempts-the-account", map[string]any{"retry_after_seconds": retryAfter})
		return
	}
	valid, verifyErr := auth.VerifyPassword(record.PasswordHash, body.Password)
	if verifyErr != nil || !valid {
		if _, updateErr := d.Pool.Exec(r.Context(), `UPDATE users SET failed_login_count=failed_login_count+1, locked_until=CASE WHEN failed_login_count+1 >= 5 THEN now()+interval '15 minutes' ELSE locked_until END, updated_at=now() WHERE id=$1`, record.UserID); updateErr != nil {
			adminError(w, http.StatusInternalServerError, "AUTH_INTERNAL_ERROR", "login-service-is-temporarily-unavailable", nil)
			return
		}
		d.recordLogin(r.Context(), &record.OrganizationID, &record.UserID, email, ip, agent, "failure")
		adminError(w, http.StatusUnauthorized, "AUTH_INVALID_CREDENTIALS", "incorrect-email-or-password", nil)
		return
	}
	var sessionSecret string
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `UPDATE users SET failed_login_count=0,locked_until=NULL,last_login_at=now(),updated_at=now() WHERE id=$1`, record.UserID); err != nil {
			return err
		}
		created, err := d.Auth.CreateWithTx(r.Context(), tx, record.UserID, ip, agent)
		if err != nil {
			return err
		}
		sessionSecret = created
		_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(organization_id,actor_user_id,actor_type,action,resource_type,resource_id,ip_address,user_agent) VALUES($1,$2,'user','auth.login','session',$3,$4,$5)`, record.OrganizationID, record.UserID, record.UserID.String(), nullableInet(ip), agent)
		return err
	})
	if err != nil {
		adminError(w, http.StatusInternalServerError, "AUTH_INTERNAL_ERROR", "login-service-is-temporarily-unavailable", nil)
		return
	}
	setSessionCookie(w, sessionSecret, int(auth.SessionTTL/time.Second), cookieSecure(d.Env))
	adminOK(w, loginTokenResponse{Token: "cookie", RefreshToken: "", User: adminUserResponse{UserID: record.UserID.String(), OrganizationID: record.OrganizationID.String(), Email: email, Role: record.Role}})
}

func (d ServerDeps) Logout(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		adminFail(w, http.StatusUnauthorized, "authentication-required")
		return
	}
	if err := d.Auth.Revoke(r.Context(), principal.SessionID); err != nil {
		adminError(w, http.StatusInternalServerError, "AUTH_INTERNAL_ERROR", "login-service-is-temporarily-unavailable", nil)
		return
	}
	setSessionCookie(w, "", -1, cookieSecure(d.Env))
	adminOK(w, map[string]string{"status": "logged_out"})
}

func (d ServerDeps) Me(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		adminFail(w, http.StatusUnauthorized, "authentication-required")
		return
	}
	adminOK(w, adminUserInfoResponse{UserID: principal.UserID.String(), UserName: principal.Email, Roles: adminRoles(principal.Role), Buttons: []string{}})
}

func (d ServerDeps) recordLogin(ctx context.Context, organizationID, userID *uuid.UUID, email, ip, agent, result string) {
	action := "auth.login_failed"
	if result == "success" {
		action = "auth.login"
	}
	_, _ = d.Pool.Exec(ctx, `INSERT INTO audit_events(organization_id,actor_user_id,actor_type,action,resource_type,resource_id,result,ip_address,user_agent) VALUES($1,$2,'user',$3,'user',$4,$5,$6,$7)`, nullableUUID(organizationID), nullableUUID(userID), action, nullableUUID(userID), result, nullableInet(ip), agent)
	_ = email
}

func nullableUUID(value *uuid.UUID) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInet(value string) any {
	if value == "" || net.ParseIP(value) == nil {
		return nil
	}
	return value
}

func setSessionCookie(w http.ResponseWriter, secret string, maxAge int, secure bool) {
	http.SetCookie(w, &http.Cookie{Name: auth.SessionCookieName, Value: secret, Path: "/", MaxAge: maxAge, HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
}

func RequireSession(deps ServerDeps, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(auth.SessionCookieName)
		if err != nil || cookie.Value == "" {
			adminFail(w, http.StatusUnauthorized, "authentication-required")
			return
		}
		principal, ok, err := deps.Auth.Resolve(r.Context(), cookie.Value)
		if err != nil {
			adminError(w, http.StatusInternalServerError, "AUTH_INTERNAL_ERROR", "login-service-is-temporarily-unavailable", nil)
			return
		}
		if !ok {
			adminFail(w, http.StatusUnauthorized, "authentication-required")
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), principal)))
	})
}

func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		value := strings.TrimSpace(parts[0])
		if net.ParseIP(value) != nil {
			return value
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func cookieSecure(env string) bool { return env != "development" }

type loginTokenResponse struct {
	Token        string            `json:"token"`
	RefreshToken string            `json:"refreshToken"`
	User         adminUserResponse `json:"user"`
}

type adminUserResponse struct {
	UserID         string `json:"user_id"`
	OrganizationID string `json:"organization_id"`
	Email          string `json:"email"`
	Role           string `json:"role"`
}

type adminUserInfoResponse struct {
	UserID   string   `json:"userId"`
	UserName string   `json:"userName"`
	Roles    []string `json:"roles"`
	Buttons  []string `json:"buttons"`
}

func adminRoles(role string) []string {
	if role == "platform_admin" || role == "org_owner" || role == "org_admin" {
		return []string{"R_SUPER", role}
	}
	return []string{role}
}

package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const SessionCookieName = "omniflux_session"
const SessionTTL = 24 * time.Hour

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func hashSession(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return base64.RawStdEncoding.EncodeToString(sum[:])
}

func (s *Store) Create(ctx context.Context, userID uuid.UUID, ipAddress, userAgent string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	secret := base64.RawURLEncoding.EncodeToString(raw)
	expires := time.Now().Add(SessionTTL)
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO sessions(user_id, secret_hash, ip_address, user_agent, expires_at) VALUES($1,$2,$3,$4,$5)`, userID, hashSession(secret), nullableIP(ipAddress), userAgent, expires)
		return err
	})
	if err != nil {
		return "", err
	}
	return secret, nil
}

func (s *Store) Resolve(ctx context.Context, secret string) (Principal, bool, error) {
	if secret == "" {
		return Principal{}, false, nil
	}
	row := s.pool.QueryRow(ctx, `
		SELECT s.id, s.created_at, u.id, u.organization_id, u.email, u.role
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.secret_hash = $1 AND s.revoked_at IS NULL AND s.expires_at > now() AND u.status = 'active'
	`, hashSession(secret))
	var principal Principal
	var sessionID, userID, organizationID uuid.UUID
	var createdAt time.Time
	if err := row.Scan(&sessionID, &createdAt, &userID, &organizationID, &principal.Email, &principal.Role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Principal{}, false, nil
		}
		return Principal{}, false, err
	}
	principal.SessionID = sessionID
	principal.UserID = userID
	principal.OrganizationID = organizationID
	principal.SessionIssuedAt = createdAt.UTC().Format(time.RFC3339)
	return principal, true, nil
}

func (s *Store) Revoke(ctx context.Context, sessionID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, sessionID)
	return err
}

func (s *Store) RevokeBySecret(ctx context.Context, secret string) error {
	_, err := s.pool.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE secret_hash = $1 AND revoked_at IS NULL`, hashSession(secret))
	return err
}

func CompareSessionHash(secret, stored string) bool {
	return subtle.ConstantTimeCompare([]byte(hashSession(secret)), []byte(stored)) == 1
}

func nullableIP(value string) any {
	if value == "" {
		return nil
	}
	ip := net.ParseIP(value)
	if ip == nil {
		return nil
	}
	return ip.String()
}

func (s *Store) CreateWithTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, ipAddress, userAgent string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	secret := base64.RawURLEncoding.EncodeToString(raw)
	expires := time.Now().Add(SessionTTL)
	if _, err := tx.Exec(ctx, `INSERT INTO sessions(user_id, secret_hash, ip_address, user_agent, expires_at) VALUES($1,$2,$3,$4,$5)`, userID, hashSession(secret), nullableIP(ipAddress), userAgent, expires); err != nil {
		return "", err
	}
	return secret, nil
}

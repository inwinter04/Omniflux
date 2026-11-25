package httpx

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/inwinter04/omniflux/internal/auth"
)

const (
	emailVerificationTTL = time.Hour
	userInvitationTTL    = 72 * time.Hour
	passwordResetTTL     = time.Hour
)

type emailTokenRecord struct {
	ID             uuid.UUID
	Purpose        string
	OrganizationID uuid.NullUUID
	UserID         uuid.NullUUID
	Email          string
	Role           pgtype.Text
	ExpiresAt      time.Time
}

type createEmailTokenParams struct {
	purpose        string
	email          string
	organizationID *uuid.UUID
	userID         *uuid.UUID
	role           string
	ttl            time.Duration
}

func (d ServerDeps) createEmailToken(ctx context.Context, q rowQuerier, params createEmailTokenParams) (string, uuid.UUID, error) {
	token, err := auth.GenerateEmailToken()
	if err != nil {
		return "", uuid.UUID{}, err
	}
	tokenHash := auth.HashEmailToken(d.APIKeyPepper, params.purpose, token)
	id := uuid.New()
	var organizationID, userID any
	if params.organizationID != nil {
		organizationID = *params.organizationID
	}
	if params.userID != nil {
		userID = *params.userID
	}
	var role any
	if params.role != "" {
		role = params.role
	}
	if _, err := q.Exec(ctx, `
		UPDATE email_tokens SET consumed_at=now()
		WHERE purpose=$1 AND email=$2 AND consumed_at IS NULL
	`, params.purpose, params.email); err != nil {
		return "", uuid.UUID{}, err
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO email_tokens(id,purpose,token_hash,organization_id,user_id,email,role,expires_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,now()+$8::interval)
	`, id, params.purpose, tokenHash, organizationID, userID, params.email, role, params.ttl.String()); err != nil {
		return "", uuid.UUID{}, err
	}
	return token, id, nil
}

func (d ServerDeps) consumeEmailToken(ctx context.Context, q rowQuerier, purpose, token string) (emailTokenRecord, error) {
	tokenHash := auth.HashEmailToken(d.APIKeyPepper, purpose, token)
	var record emailTokenRecord
	err := q.QueryRow(ctx, `
		UPDATE email_tokens SET consumed_at=now()
		WHERE token_hash=$1 AND purpose=$2 AND consumed_at IS NULL AND expires_at>now()
		RETURNING id,purpose,organization_id,user_id,email,role,expires_at
	`, tokenHash, purpose).Scan(
		&record.ID, &record.Purpose, &record.OrganizationID, &record.UserID, &record.Email, &record.Role, &record.ExpiresAt,
	)
	return record, err
}

func writeSystemAuditEvent(ctx context.Context, q rowQuerier, organizationID *uuid.UUID, action, resourceType, resourceID string, after map[string]any) error {
	var orgID any
	if organizationID != nil {
		orgID = *organizationID
	}
	_, err := q.Exec(ctx, `
		INSERT INTO audit_events(organization_id,actor_type,action,resource_type,resource_id,after_state)
		VALUES($1,'system',$2,$3,$4,$5)
	`, orgID, action, resourceType, resourceID, auditAnyJSON(after))
	return err
}

var errEmailTokenNotFound = errors.New("email token not found")

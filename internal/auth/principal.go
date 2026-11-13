package auth

import (
	"context"

	"github.com/google/uuid"
)

type Principal struct {
	UserID          uuid.UUID
	OrganizationID  uuid.UUID
	Email           string
	Role            string
	SessionID       uuid.UUID
	SessionIssuedAt string
}

type principalKey struct{}

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, principal)
}

func FromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalKey{}).(Principal)
	return principal, ok
}

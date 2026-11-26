package auth

import (
	"context"

	"github.com/google/uuid"
)

type Principal struct {
	UserID              uuid.UUID
	OrganizationID      uuid.UUID
	Email               string
	Role                string
	PermissionGroupID   *uuid.UUID
	PermissionGroupName string
	Permissions         map[string]bool
	SessionID           uuid.UUID
	SessionIssuedAt     string
}

func (p Principal) HasPermission(permission string) bool {
	if p.Role == "platform_admin" || p.Role == "org_owner" {
		return IsValidPermission(permission)
	}
	return p.Permissions[permission]
}

func (p Principal) PermissionList() []string {
	list := make([]string, 0, len(allPermissionCodes))
	for _, code := range allPermissionCodes {
		if p.HasPermission(code) {
			list = append(list, code)
		}
	}
	return list
}

type principalKey struct{}

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, principal)
}

func FromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalKey{}).(Principal)
	return principal, ok
}

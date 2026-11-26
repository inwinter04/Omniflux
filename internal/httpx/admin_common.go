package httpx

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/inwinter04/omniflux/internal/auth"
)

// requirePermission 校验当前 principal 拥有指定细粒度权限节点;不足时写出 403。
func (d ServerDeps) requirePermission(w http.ResponseWriter, principal auth.Principal, permission string) error {
	if err := auth.RequirePermission(principal, permission); err != nil {
		adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "permission-denied", map[string]any{"permission": permission})
		return err
	}
	return nil
}

// requireAnyPermission 校验当前 principal 至少拥有给定权限节点之一;不足时写出 403。
func (d ServerDeps) requireAnyPermission(w http.ResponseWriter, principal auth.Principal, permissions ...string) error {
	for _, perm := range permissions {
		if principal.HasPermission(perm) {
			return nil
		}
	}
	adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "permission-denied", nil)
	return auth.ErrPermissionDenied
}

// decodeJSON 解析请求体;失败时写出 400。
func (d ServerDeps) decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		adminError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid-request-body", nil, err.Error())
		return false
	}
	return true
}

// optionalIntEqual 比较两个可空整数是否相等,都为空也算相等。
func optionalIntEqual(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// nullableIntPtr 把可选限流指针转成可空参数。
func nullableIntPtr(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

// writeAuditEvent 在业务事务内写入审计事件;失败时返回错误,让调用方回滚业务写入。
func writeAuditEvent(ctx context.Context, q rowQuerier, organizationID, userID uuid.UUID, action, resourceType, resourceID string, before, after any) error {
	_, err := q.Exec(ctx, `
		INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, before_state, after_state)
		VALUES ($1,$2,'user',$3,$4,$5,$6,$7)
	`, organizationID, userID, action, resourceType, resourceID, auditAnyJSON(before), auditAnyJSON(after))
	return err
}

func auditAnyJSON(value any) []byte {
	if value == nil {
		return []byte("{}")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return []byte("{}")
	}
	return data
}

// execWithAudit 在同一事务内执行业务写入和审计写入;审计失败则回滚。
func (d ServerDeps) execWithAudit(ctx context.Context, sql string, args []any, organizationID, userID uuid.UUID, action, resourceType, resourceID string, before, after any) error {
	return pgx.BeginFunc(ctx, d.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, sql, args...)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return writeAuditEvent(ctx, tx, organizationID, userID, action, resourceType, resourceID, before, after)
	})
}

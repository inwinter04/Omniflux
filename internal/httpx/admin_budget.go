package httpx

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/inwinter04/omniflux/internal/auth"
)

var allowedBudgetScopes = map[string]bool{
	"organization": true, "project": true, "application": true, "api_key": true,
}

type budgetResponse struct {
	ID          string    `json:"id"`
	ScopeType   string    `json:"scope_type"`
	ScopeID     string    `json:"scope_id"`
	ScopeName   string    `json:"scope_name"`
	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`
	LimitAmount string    `json:"limit_amount"`
	SpentAmount string    `json:"spent_amount"`
	Currency    string    `json:"currency"`
	Exceeded    bool      `json:"exceeded"`
}

type createBudgetRequest struct {
	ScopeType   string    `json:"scope_type"`
	ScopeID     string    `json:"scope_id"`
	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`
	LimitAmount string    `json:"limit_amount"`
	Currency    string    `json:"currency"`
}

type updateBudgetRequest struct {
	LimitAmount string `json:"limit_amount"`
}

// resolveBudgetScope 校验 scope 属于当前组织并返回业务名称。
func (d ServerDeps) resolveBudgetScope(ctx context.Context, scopeType string, scopeID, orgID uuid.UUID) (string, error) {
	var query string
	switch scopeType {
	case "organization":
		query = `SELECT name FROM organizations WHERE id = $1 AND id = $2`
	case "project":
		query = `SELECT name FROM projects WHERE id = $1 AND organization_id = $2`
	case "application":
		query = `SELECT a.name FROM applications a JOIN projects p ON p.id = a.project_id WHERE a.id = $1 AND p.organization_id = $2`
	case "api_key":
		query = `SELECT name FROM api_keys WHERE id = $1 AND organization_id = $2 AND deleted_at IS NULL`
	default:
		return "", errors.New("unsupported budget scope")
	}
	var name string
	err := d.Pool.QueryRow(ctx, query, scopeID, orgID).Scan(&name)
	return name, err
}

// amountValid 校验预算金额是合法的非负小数。
func amountValid(raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return false
	}
	v, err := parseBudgetAmount(raw)
	return err == nil && v.Sign() >= 0
}

// Budgets 返回当前组织的预算窗口列表,附带 scope 业务名称。
func (d ServerDeps) Budgets(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermBudgetRead) != nil {
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT id, scope_type, scope_id, window_start, window_end,
		       limit_amount::text, spent_amount::text, currency
		FROM budget_windows
		WHERE organization_id = $1
		ORDER BY window_start DESC, updated_at DESC
	`, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "BUDGET_LIST_FAILED", "failed-to-query-budgets", nil)
		return
	}
	defer rows.Close()
	list := []budgetResponse{}
	for rows.Next() {
		var item budgetResponse
		var id, scopeID uuid.UUID
		if err := rows.Scan(&id, &item.ScopeType, &scopeID, &item.WindowStart, &item.WindowEnd,
			&item.LimitAmount, &item.SpentAmount, &item.Currency); err != nil {
			d.Logger.Error("scan budget failed", "error", err)
			adminError(w, http.StatusInternalServerError, "BUDGET_LIST_FAILED", "failed-to-query-budgets", nil)
			return
		}
		item.ID = id.String()
		item.ScopeID = scopeID.String()
		item.Exceeded = amountCompare(item.SpentAmount, item.LimitAmount) >= 0
		list = append(list, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		adminError(w, http.StatusInternalServerError, "BUDGET_LIST_FAILED", "failed-to-query-budgets", nil)
		return
	}
	for i := range list {
		name, err := d.resolveBudgetScope(r.Context(), list[i].ScopeType, uuid.MustParse(list[i].ScopeID), principal.OrganizationID)
		if err != nil {
			name = "-"
		}
		list[i].ScopeName = name
	}
	adminOK(w, list)
}

// CreateBudget 创建预算窗口;scope 必须属于当前组织,时间窗必须有效。
func (d ServerDeps) CreateBudget(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermBudgetManage) != nil {
		return
	}
	var body createBudgetRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	body.ScopeType = strings.TrimSpace(body.ScopeType)
	if !allowedBudgetScopes[body.ScopeType] {
		adminError(w, http.StatusBadRequest, "BUDGET_SCOPE_UNSUPPORTED", "unsupported-budget-scope-type", nil)
		return
	}
	var scopeID uuid.UUID
	if body.ScopeType == "organization" {
		scopeID = principal.OrganizationID
	} else {
		parsed, err := uuid.Parse(body.ScopeID)
		if err != nil {
			adminError(w, http.StatusBadRequest, "BUDGET_SCOPE_INVALID", "invalid-scope-id", nil)
			return
		}
		scopeID = parsed
	}
	scopeName, err := d.resolveBudgetScope(r.Context(), body.ScopeType, scopeID, principal.OrganizationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			adminError(w, http.StatusBadRequest, "BUDGET_SCOPE_NOT_FOUND", "budget-subject-does-not-exist-or", nil)
			return
		}
		adminError(w, http.StatusInternalServerError, "BUDGET_CREATE_FAILED", "failed-to-create-the-budget", nil)
		return
	}
	if !body.WindowStart.Before(body.WindowEnd) {
		adminError(w, http.StatusBadRequest, "BUDGET_WINDOW_INVALID", "window-start-must-be-earlier-than", nil)
		return
	}
	if !amountValid(body.LimitAmount) {
		adminError(w, http.StatusBadRequest, "BUDGET_AMOUNT_INVALID", "limit-amount-must-be-a-non", nil)
		return
	}
	if strings.TrimSpace(body.Currency) == "" {
		body.Currency = "USD"
	}
	budgetID := uuid.New()
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO budget_windows(
				id, organization_id, scope_type, scope_id,
				window_start, window_end, limit_amount, currency
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		`, budgetID, principal.OrganizationID, body.ScopeType, scopeID,
			body.WindowStart, body.WindowEnd, body.LimitAmount, body.Currency); err != nil {
			return err
		}
		return writeAuditEvent(r.Context(), tx, principal.OrganizationID, principal.UserID,
			"budget.created", "budget", budgetID.String(), nil, map[string]any{
				"scope_type": body.ScopeType, "scope_id": scopeID.String(), "scope_name": scopeName,
				"limit_amount": body.LimitAmount,
			})
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && strings.Contains(pgErr.ConstraintName, "budget_windows_scope_type_scope_id_window_start_key") {
			adminError(w, http.StatusConflict, "BUDGET_WINDOW_EXISTS", "a-budget-already-exists-for-the", nil)
			return
		}
		adminError(w, http.StatusInternalServerError, "BUDGET_CREATE_FAILED", "failed-to-create-the-budget", nil)
		return
	}
	adminOK(w, map[string]string{"id": budgetID.String()})
}

// UpdateBudget 调整预算限额。
func (d ServerDeps) UpdateBudget(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermBudgetManage) != nil {
		return
	}
	var body updateBudgetRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	if !amountValid(body.LimitAmount) {
		adminError(w, http.StatusBadRequest, "BUDGET_AMOUNT_INVALID", "limit-amount-must-be-a-non", nil)
		return
	}
	budgetID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "BUDGET_ID_INVALID", "invalid-budget-id", nil)
		return
	}
	var oldLimit string
	err = d.Pool.QueryRow(r.Context(), `
		SELECT limit_amount::text FROM budget_windows WHERE id = $1 AND organization_id = $2
	`, budgetID, principal.OrganizationID).Scan(&oldLimit)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "BUDGET_NOT_FOUND", "budget-does-not-exist-or-does", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "BUDGET_UPDATE_FAILED", "failed-to-update-the-budget", nil)
		return
	}
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `
			UPDATE budget_windows SET limit_amount = $3, updated_at = now() WHERE id = $1 AND organization_id = $2
		`, budgetID, principal.OrganizationID, body.LimitAmount); err != nil {
			return err
		}
		return writeAuditEvent(r.Context(), tx, principal.OrganizationID, principal.UserID,
			"budget.updated", "budget", budgetID.String(),
			map[string]any{"limit_amount": oldLimit}, map[string]any{"limit_amount": body.LimitAmount})
	})
	if err != nil {
		adminError(w, http.StatusInternalServerError, "BUDGET_UPDATE_FAILED", "failed-to-update-the-budget", nil)
		return
	}
	adminOK(w, nil)
}

// DeleteBudget 删除预算窗口;已有结算记录时数据库外键会拒绝,提前检查并提示。
func (d ServerDeps) DeleteBudget(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermBudgetManage) != nil {
		return
	}
	budgetID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "BUDGET_ID_INVALID", "invalid-budget-id", nil)
		return
	}
	var exists bool
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT EXISTS (SELECT 1 FROM budget_windows WHERE id = $1 AND organization_id = $2)
	`, budgetID, principal.OrganizationID).Scan(&exists); err != nil {
		adminError(w, http.StatusInternalServerError, "BUDGET_DELETE_FAILED", "failed-to-delete-the-budget", nil)
		return
	}
	if !exists {
		adminError(w, http.StatusNotFound, "BUDGET_NOT_FOUND", "budget-does-not-exist-or-does", nil)
		return
	}
	var reservationCount int
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM budget_reservations WHERE budget_window_id = $1
	`, budgetID).Scan(&reservationCount); err != nil {
		adminError(w, http.StatusInternalServerError, "BUDGET_DELETE_FAILED", "failed-to-delete-the-budget", nil)
		return
	}
	if reservationCount > 0 {
		adminError(w, http.StatusConflict, "BUDGET_HAS_RESERVATIONS",
			"budget-delete-has-settlements", nil)
		return
	}
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		// 预算没有自己的名称,删除后审计靠快照里的所属范围与额度识别“删的是哪条预算”。
		var scopeType, scopeName, limitAmount string
		var scopeID uuid.UUID
		if err := tx.QueryRow(r.Context(), `
			DELETE FROM budget_windows WHERE id = $1
			RETURNING scope_type, scope_id, limit_amount::text, COALESCE(CASE scope_type
				WHEN 'organization' THEN (SELECT name FROM organizations WHERE id = budget_windows.scope_id)
				WHEN 'project' THEN (SELECT name FROM projects WHERE id = budget_windows.scope_id)
				WHEN 'application' THEN (SELECT name FROM applications WHERE id = budget_windows.scope_id)
				WHEN 'api_key' THEN (SELECT name FROM api_keys WHERE id = budget_windows.scope_id)
			END, '')
		`, budgetID).Scan(&scopeType, &scopeID, &limitAmount, &scopeName); err != nil {
			return err
		}
		return writeAuditEvent(r.Context(), tx, principal.OrganizationID, principal.UserID,
			"budget.deleted", "budget", budgetID.String(), map[string]any{
				"scope_type": scopeType, "scope_id": scopeID.String(), "scope_name": scopeName,
				"limit_amount": limitAmount,
			}, nil)
	})
	if err != nil {
		adminError(w, http.StatusInternalServerError, "BUDGET_DELETE_FAILED", "failed-to-delete-the-budget", nil)
		return
	}
	adminOK(w, nil)
}

package dataplane

import (
	"context"
	"fmt"
	"math/big"
	"net/http"

	"github.com/google/uuid"

	"github.com/jackc/pgx/v5"
)

// budgetScopeRow 是当前请求命中的预算窗口。
type budgetScopeRow struct {
	ID          uuid.UUID
	ScopeType   string
	ScopeID     uuid.UUID
	LimitAmount string
	SpentAmount string
	Currency    string
}

// loadActiveBudgets 查当前请求命中的活跃预算窗口(组织/项目/应用/Key 四类)。
func (d Deps) loadActiveBudgets(ctx context.Context, keyCtx apiKeyContext) ([]budgetScopeRow, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT id, scope_type, scope_id, limit_amount::text, spent_amount::text, currency
		FROM budget_windows
		WHERE organization_id = $1
		  AND now() >= window_start AND now() < window_end
		  AND (
		    scope_type = 'organization'
		    OR (scope_type = 'project' AND scope_id = $2)
		    OR (scope_type = 'application' AND scope_id = $3)
		    OR (scope_type = 'api_key' AND scope_id = $4)
		  )
	`, keyCtx.OrganizationID, keyCtx.ProjectID, keyCtx.ApplicationID, keyCtx.KeyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	windows := []budgetScopeRow{}
	for rows.Next() {
		var w budgetScopeRow
		if err := rows.Scan(&w.ID, &w.ScopeType, &w.ScopeID, &w.LimitAmount, &w.SpentAmount, &w.Currency); err != nil {
			return nil, err
		}
		windows = append(windows, w)
	}
	return windows, rows.Err()
}

// checkBudgets 在请求处理前校验预算;任一命中窗口已花满则拒绝。
func (d Deps) checkBudgets(w http.ResponseWriter, r *http.Request, keyCtx apiKeyContext) bool {
	windows, err := d.loadActiveBudgets(r.Context(), keyCtx)
	if err != nil {
		// 预算查询失败不应阻断请求,记日志放行。
		d.Logger.Warn("load budgets failed", "error", err)
		return true
	}
	for _, win := range windows {
		if spentExceeded(win.SpentAmount, win.LimitAmount) {
			apiError(w, http.StatusTooManyRequests, "budget_exceeded",
				fmt.Sprintf("Budget exceeded for %s window (limit %s %s). Contact an administrator to adjust it.", win.ScopeType, win.LimitAmount, win.Currency),
				apiErrorOptions{Reason: "budget_exceeded", Params: map[string]any{"scope_type": win.ScopeType, "limit": win.LimitAmount, "currency": win.Currency}})
			return false
		}
	}
	return true
}

// settleBudget 把成功请求的实际成本累加到命中的窗口,并为每个窗口写一条结算记录。
// 预算采用后付口径:请求前按已结算金额拦截,请求后累加实际成本。
// 并发在途请求可能小幅越过限额(与 LiteLLM 等主流网关一致),
// 不做前置预留以避免失败退还/超时释放的额外复杂度;budget_reservations 仅作结算审计。
func (d Deps) settleBudget(ctx context.Context, keyCtx apiKeyContext, requestID uuid.UUID, cost float64, currency string) {
	if currency == "" {
		currency = "USD"
	}
	if cost <= 0 {
		return
	}
	err := pgx.BeginFunc(ctx, d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE budget_windows SET spent_amount = spent_amount + $1, updated_at = now()
			WHERE organization_id = $2 AND currency = $6
			  AND now() >= window_start AND now() < window_end
			  AND (
			    scope_type = 'organization'
			    OR (scope_type = 'project' AND scope_id = $3)
			    OR (scope_type = 'application' AND scope_id = $4)
			    OR (scope_type = 'api_key' AND scope_id = $5)
			  )
		`, cost, keyCtx.OrganizationID, keyCtx.ProjectID, keyCtx.ApplicationID, keyCtx.KeyID, currency); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO budget_reservations(budget_window_id, request_id, estimated_amount, final_amount, state, settled_at)
			SELECT id, $1, 0, $2, 'settled', now()
			FROM budget_windows
			WHERE organization_id = $3 AND currency = $7
			  AND now() >= window_start AND now() < window_end
			  AND (
			    scope_type = 'organization'
			    OR (scope_type = 'project' AND scope_id = $4)
			    OR (scope_type = 'application' AND scope_id = $5)
			    OR (scope_type = 'api_key' AND scope_id = $6)
			  )
			ON CONFLICT (budget_window_id, request_id) DO NOTHING
		`, requestID, cost, keyCtx.OrganizationID, keyCtx.ProjectID, keyCtx.ApplicationID, keyCtx.KeyID, currency)
		return err
	})
	if err != nil {
		// 结算失败不影响响应,记日志保留真实错误。
		d.Logger.Warn("settle budget failed", "request_id", requestID, "error", err)
	}
}

// spentExceeded 判断已花金额是否达到限额;数值以字符串表示,直接按小数比较。
func spentExceeded(spent, limit string) bool {
	spentN, okS := new(big.Float).SetString(spent)
	limitN, okL := new(big.Float).SetString(limit)
	if !okS || !okL {
		return false
	}
	return spentN.Cmp(limitN) >= 0
}

// keyFromContextOrEmpty 从请求上下文取调用方信息,缺失时返回零值。
func keyFromContextOrEmpty(r *http.Request) apiKeyContext {
	ctx, _ := keyFromContext(r.Context())
	return ctx
}

package httpx

import (
	"net/http"
	"strconv"

	"github.com/inwinter04/omniflux/internal/auth"
)

type dashboardResponse struct {
	ActiveServices  int64  `json:"active_services"`
	ActiveProviders int64  `json:"active_providers"`
	ActiveAPIKeys   int64  `json:"active_api_keys"`
	UserCount       int64  `json:"user_count"`
	Requests7d      int64  `json:"requests_7d"`
	SuccessRate7d   string `json:"success_rate_7d"`
	Cost7d          string `json:"cost_7d"`
	CostCurrency    string `json:"cost_currency"`
	ActiveBudgets   int64  `json:"active_budgets"`
	ExceededBudgets int64  `json:"exceeded_budgets"`
}

// Dashboard 返回首页概览统计:资源规模、近 7 天用量与预算状态。
func (d ServerDeps) Dashboard(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireViewer(w, principal) != nil {
		return
	}
	var res dashboardResponse
	err := d.Pool.QueryRow(r.Context(), `
		SELECT
			(SELECT count(*) FROM model_services WHERE organization_id = $1 AND status = 'active'),
			(SELECT count(*) FROM provider_accounts WHERE organization_id = $1 AND status = 'active'),
			(SELECT count(*) FROM api_keys WHERE organization_id = $1 AND status = 'active' AND deleted_at IS NULL),
			(SELECT count(*) FROM users WHERE organization_id = $1 AND status = 'active'),
			(SELECT count(*) FROM usage_records WHERE organization_id = $1 AND created_at >= now() - interval '7 days'),
			(SELECT CASE WHEN count(*) = 0 THEN '-' ELSE (count(*) FILTER (WHERE state = 'success') * 100 / count(*))::text || '%' END
				FROM usage_records WHERE organization_id = $1 AND created_at >= now() - interval '7 days'),
			(SELECT COALESCE(sum(cost), 0)::text FROM usage_records WHERE organization_id = $1 AND created_at >= now() - interval '7 days'),
			(SELECT COALESCE(max(cost_currency), 'USD') FROM usage_records WHERE organization_id = $1 AND created_at >= now() - interval '7 days'),
			(SELECT count(*) FROM budget_windows WHERE organization_id = $1 AND now() >= window_start AND now() < window_end),
			(SELECT count(*) FROM budget_windows WHERE organization_id = $1 AND now() >= window_start AND now() < window_end AND spent_amount >= limit_amount)
	`, principal.OrganizationID).Scan(
		&res.ActiveServices, &res.ActiveProviders, &res.ActiveAPIKeys, &res.UserCount,
		&res.Requests7d, &res.SuccessRate7d, &res.Cost7d, &res.CostCurrency,
		&res.ActiveBudgets, &res.ExceededBudgets,
	)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "DASHBOARD_FAILED", "无法加载概览数据", nil)
		return
	}
	adminOK(w, res)
}

var _ = strconv.Itoa

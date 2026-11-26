package httpx

import (
	"net/http"
	"strconv"

	"github.com/inwinter04/omniflux/internal/auth"
)

type dashboardResponse struct {
	ActiveServices   int64                `json:"active_services"`
	ActiveProviders  int64                `json:"active_providers"`
	ActiveAPIKeys    int64                `json:"active_api_keys"`
	UserCount        int64                `json:"user_count"`
	DepartmentCount  int64                `json:"department_count"`
	ProjectCount     int64                `json:"project_count"`
	ApplicationCount int64                `json:"application_count"`
	Requests7d       int64                `json:"requests_7d"`
	SuccessRate7d    string               `json:"success_rate_7d"`
	Cost7d           string               `json:"cost_7d"`
	CostCurrency     string               `json:"cost_currency"`
	Costs            []usageCurrencyTotal `json:"costs"`
	ActiveBudgets    int64                `json:"active_budgets"`
	ExceededBudgets  int64                `json:"exceeded_budgets"`
}

// Dashboard 返回首页概览统计:资源规模、近 7 天用量与预算状态。
func (d ServerDeps) Dashboard(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermDashboardRead) != nil {
		return
	}
	var res dashboardResponse
	err := d.Pool.QueryRow(r.Context(), `
		SELECT
			(SELECT count(*) FROM model_services WHERE organization_id = $1 AND status = 'active'),
			(SELECT count(*) FROM provider_accounts WHERE organization_id = $1 AND status = 'active'),
			(SELECT count(*) FROM api_keys k WHERE k.organization_id = $1 AND k.status = 'active' AND k.deleted_at IS NULL
			AND (k.expires_at IS NULL OR k.expires_at > now())
			AND EXISTS (SELECT 1 FROM projects p WHERE p.id = k.project_id AND p.status = 'active')
			AND EXISTS (SELECT 1 FROM applications a WHERE a.id = k.application_id AND a.status = 'active')),
			(SELECT count(*) FROM users WHERE organization_id = $1 AND status = 'active'),
			(SELECT count(*) FROM departments WHERE organization_id = $1),
			(SELECT count(*) FROM projects WHERE organization_id = $1),
			(SELECT count(*) FROM applications WHERE organization_id = $1),
			(SELECT count(*) FROM usage_records WHERE organization_id = $1 AND created_at >= now() - interval '7 days'),
			(SELECT CASE WHEN count(*) = 0 THEN '-' ELSE (count(*) FILTER (WHERE state = 'success') * 100 / count(*))::text || '%' END
				FROM usage_records WHERE organization_id = $1 AND created_at >= now() - interval '7 days'),
			(SELECT count(*) FROM budget_windows WHERE organization_id = $1 AND now() >= window_start AND now() < window_end),
			(SELECT count(*) FROM budget_windows WHERE organization_id = $1 AND now() >= window_start AND now() < window_end AND spent_amount >= limit_amount)
	`, principal.OrganizationID).Scan(
		&res.ActiveServices, &res.ActiveProviders, &res.ActiveAPIKeys, &res.UserCount,
		&res.DepartmentCount, &res.ProjectCount, &res.ApplicationCount,
		&res.Requests7d, &res.SuccessRate7d, &res.ActiveBudgets, &res.ExceededBudgets,
	)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "DASHBOARD_FAILED", "failed-to-load-overview-data", nil)
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT cost_currency, COALESCE(sum(cost), 0)::text FROM reporting_usage_records
		WHERE organization_id = $1 AND created_at >= now() - interval '7 days'
		GROUP BY cost_currency ORDER BY cost_currency
	`, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "DASHBOARD_FAILED", "failed-to-query-cost-currencies", nil)
		return
	}
	defer rows.Close()
	res.Costs = []usageCurrencyTotal{}
	for rows.Next() {
		var item usageCurrencyTotal
		if err := rows.Scan(&item.Currency, &item.Total); err != nil {
			adminError(w, http.StatusInternalServerError, "DASHBOARD_FAILED", "failed-to-read-cost-currencies", nil)
			return
		}
		res.Costs = append(res.Costs, item)
	}
	if len(res.Costs) == 1 {
		res.Cost7d = res.Costs[0].Total
		res.CostCurrency = res.Costs[0].Currency
	} else if len(res.Costs) > 1 {
		res.Cost7d = "0"
		res.CostCurrency = "MIXED"
	} else {
		res.Cost7d = "0"
		res.CostCurrency = "USD"
	}
	adminOK(w, res)
}

var _ = strconv.Itoa

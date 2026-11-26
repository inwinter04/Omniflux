package httpx

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/inwinter04/omniflux/internal/auth"
)

type usageCurrencyTotal struct {
	Currency string `json:"currency"`
	Total    string `json:"total"`
}

type usageSummaryResponse struct {
	Costs             []usageCurrencyTotal `json:"costs"`
	TotalRequests     int64                `json:"total_requests"`
	SuccessRequests   int64                `json:"success_requests"`
	FailedRequests    int64                `json:"failed_requests"`
	TotalInputTokens  int64                `json:"total_input_tokens"`
	TotalOutputTokens int64                `json:"total_output_tokens"`
	TotalCost         string               `json:"total_cost"`
	CostCurrency      string               `json:"cost_currency"`
	ByModel           []usageModelSummary  `json:"by_model"`
	ByProject         []usageScopeSummary  `json:"by_project"`
	ByApplication     []usageScopeSummary  `json:"by_application"`
	ByAPIKey          []usageScopeSummary  `json:"by_api_key"`
	ByDepartment      []usageScopeSummary  `json:"by_department"`
	ByModelService    []usageScopeSummary  `json:"by_model_service"`
	ByState           []usageScopeSummary  `json:"by_state"`
	BySource          []usageScopeSummary  `json:"by_source"`
	ByProvider        []usageScopeSummary  `json:"by_provider"`
	ByRoutePolicy     []usageScopeSummary  `json:"by_route_policy"`
	ByErrorType       []usageScopeSummary  `json:"by_error_type"`
	ByDay             []usageDaySummary    `json:"by_day"`
}

type usageScopeSummary struct {
	Name         string `json:"name"`
	Requests     int64  `json:"requests"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	Cost         string `json:"cost"`
	CostCurrency string `json:"cost_currency"`
}

type usageModelSummary struct {
	Provider      string `json:"provider"`
	UpstreamModel string `json:"upstream_model"`
	CostCurrency  string `json:"cost_currency"`
	Requests      int64  `json:"requests"`
	InputTokens   int64  `json:"input_tokens"`
	OutputTokens  int64  `json:"output_tokens"`
	Cost          string `json:"cost"`
}

type usageDaySummary struct {
	Day      string `json:"day"`
	Requests int64  `json:"requests"`
}

type usageTrendPoint struct {
	Day      string `json:"day"`
	Name     string `json:"name"`
	Requests int64  `json:"requests"`
}

type usageRecordResponse struct {
	ID              string  `json:"id"`
	RequestID       string  `json:"request_id"`
	CreatedAt       string  `json:"created_at"`
	ProjectName     *string `json:"project_name"`
	ApplicationName *string `json:"application_name"`
	KeyName         *string `json:"key_name"`
	ModelService    string  `json:"model_service"`
	RoutePolicyID   *string `json:"route_policy_id"`
	State           string  `json:"state"`
	StatusCode      *int    `json:"status_code"`
	Provider        *string `json:"provider"`
	UpstreamModel   *string `json:"upstream_model"`
	InputTokens     int64   `json:"input_tokens"`
	OutputTokens    int64   `json:"output_tokens"`
	CachedTokens    int64   `json:"cached_tokens"`
	ReasoningTokens int64   `json:"reasoning_tokens"`
	FirstTokenMS    *int    `json:"first_token_ms"`
	TotalMS         *int    `json:"total_ms"`
	Cost            string  `json:"cost"`
	CostCurrency    string  `json:"cost_currency"`
	Source          string  `json:"source"`
	ErrorType       *string `json:"error_type"`
	ErrorMessage    *string `json:"error_message"`
}

type usageRecordsPage struct {
	Total    int64                 `json:"total"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"page_size"`
	Records  []usageRecordResponse `json:"records"`
}

func parsePaging(r *http.Request) (int, int) {
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 1 {
		page = 1
	}
	pageSize, err := strconv.Atoi(r.URL.Query().Get("page_size"))
	if err != nil || pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func buildUsageFilters(r *http.Request, organizationID uuid.UUID) (string, []any, error) {
	where := []string{"u.organization_id = $1"}
	args := []any{organizationID}
	addUUIDFilter := func(param, column string) error {
		value := strings.TrimSpace(r.URL.Query().Get(param))
		if value == "" {
			return nil
		}
		parsed, err := uuid.Parse(value)
		if err != nil {
			return errors.New("invalid usage filter")
		}
		args = append(args, parsed)
		where = append(where, fmt.Sprintf("%s = $%d", column, len(args)))
		return nil
	}
	for _, filter := range []struct{ param, column string }{
		{"project_id", "u.project_id"},
		{"application_id", "u.application_id"},
		{"api_key_id", "u.api_key_id"},
		{"model_service_id", "u.model_service_id"},
	} {
		if err := addUUIDFilter(filter.param, filter.column); err != nil {
			return "", nil, err
		}
	}
	if value := strings.TrimSpace(r.URL.Query().Get("state")); value != "" {
		switch value {
		case "pending", "success", "failed", "interrupted":
			args = append(args, value)
			where = append(where, fmt.Sprintf("u.state = $%d", len(args)))
		default:
			return "", nil, errors.New("invalid usage filter")
		}
	}
	if value := strings.TrimSpace(r.URL.Query().Get("source")); value != "" {
		switch value {
		case "api", "prompt_debug", "agent_debug":
			args = append(args, value)
			where = append(where, fmt.Sprintf("u.source = $%d", len(args)))
		default:
			return "", nil, errors.New("invalid usage filter")
		}
	}
	if value := strings.TrimSpace(r.URL.Query().Get("start_time")); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return "", nil, errors.New("invalid usage filter")
		}
		args = append(args, parsed)
		where = append(where, fmt.Sprintf("u.created_at >= $%d", len(args)))
	}
	if value := strings.TrimSpace(r.URL.Query().Get("end_time")); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return "", nil, errors.New("invalid usage filter")
		}
		args = append(args, parsed)
		where = append(where, fmt.Sprintf("u.created_at < $%d", len(args)))
	}
	return strings.Join(where, " AND "), args, nil
}

// UsageTrend 返回按天聚合的趋势数据,支持总览/模型/项目/应用/供应商维度。
func (d ServerDeps) UsageTrend(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermUsageRead) != nil {
		return
	}
	whereSQL, args, err := buildUsageFilters(r, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusBadRequest, "USAGE_FILTER_INVALID", "invalid-usage-filter", nil)
		return
	}
	days, err := strconv.Atoi(r.URL.Query().Get("days"))
	if err != nil || days < 1 {
		days = 7
	}
	if days > 90 {
		days = 90
	}
	if strings.TrimSpace(r.URL.Query().Get("start_time")) == "" {
		args = append(args, strconv.Itoa(days))
		whereSQL += fmt.Sprintf(" AND u.created_at >= now() - ($%d || ' days')::interval", len(args))
	}
	nameExpr, join := "''", ""
	switch r.URL.Query().Get("dimension") {
	case "model":
		nameExpr = "COALESCE(u.provider, '') || '/' || COALESCE(u.upstream_model, '')"
	case "project":
		nameExpr, join = "COALESCE(p.name, '-')", "LEFT JOIN projects p ON p.id = u.project_id"
	case "application":
		nameExpr, join = "COALESCE(a.name, '-')", "LEFT JOIN applications a ON a.id = u.application_id"
	case "provider":
		nameExpr = "COALESCE(u.provider, '-')"
	case "state":
		nameExpr = "COALESCE(u.state, '-')"
	case "source":
		nameExpr = "COALESCE(u.source, '-')"
	case "route_policy":
		nameExpr, join = "COALESCE(rp.name, '-')", "LEFT JOIN route_policies rp ON rp.id = u.route_policy_id"
	case "error_type":
		nameExpr = "COALESCE(u.error_type, '-')"
	case "model_service":
		nameExpr, join = "m.name", "JOIN model_services m ON m.id = u.model_service_id"
	case "department":
		nameExpr = "COALESCE(d.name, '-')"
		join = "LEFT JOIN projects p ON p.id = u.project_id LEFT JOIN departments d ON d.id = p.department_id"
	case "api_key":
		nameExpr, join = "COALESCE(k.name, '-')", "LEFT JOIN api_keys k ON k.id = u.api_key_id"
	case "project_application":
		nameExpr = "COALESCE(p.name, '-') || ' / ' || COALESCE(a.name, '-')"
		join = "LEFT JOIN projects p ON p.id = u.project_id LEFT JOIN applications a ON a.id = u.application_id"
	case "project_model_service":
		nameExpr = "COALESCE(p.name, '-') || ' / ' || m.name"
		join = "JOIN model_services m ON m.id = u.model_service_id LEFT JOIN projects p ON p.id = u.project_id"
	case "department_project":
		nameExpr = "COALESCE(d.name, '-') || ' / ' || COALESCE(p.name, '-')"
		join = "LEFT JOIN projects p ON p.id = u.project_id LEFT JOIN departments d ON d.id = p.department_id"
	case "department_project_application":
		nameExpr = "COALESCE(d.name, '-') || ' / ' || COALESCE(p.name, '-') || ' / ' || COALESCE(a.name, '-')"
		join = "LEFT JOIN projects p ON p.id = u.project_id LEFT JOIN departments d ON d.id = p.department_id LEFT JOIN applications a ON a.id = u.application_id"
	case "project_model_service_application":
		nameExpr = "COALESCE(p.name, '-') || ' / ' || m.name || ' / ' || COALESCE(a.name, '-')"
		join = "JOIN model_services m ON m.id = u.model_service_id LEFT JOIN projects p ON p.id = u.project_id LEFT JOIN applications a ON a.id = u.application_id"
	case "department_project_model_service":
		nameExpr = "COALESCE(d.name, '-') || ' / ' || COALESCE(p.name, '-') || ' / ' || m.name"
		join = "JOIN model_services m ON m.id = u.model_service_id LEFT JOIN projects p ON p.id = u.project_id LEFT JOIN departments d ON d.id = p.department_id"
	case "department_project_model_service_application":
		nameExpr = "COALESCE(d.name, '-') || ' / ' || COALESCE(p.name, '-') || ' / ' || m.name || ' / ' || COALESCE(a.name, '-')"
		join = "JOIN model_services m ON m.id = u.model_service_id LEFT JOIN projects p ON p.id = u.project_id LEFT JOIN departments d ON d.id = p.department_id LEFT JOIN applications a ON a.id = u.application_id"
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT to_char(date_trunc('day', u.created_at), 'YYYY-MM-DD') AS day,
		       `+nameExpr+` AS name, count(*)
		FROM reporting_usage_records u
		`+join+`
		WHERE `+whereSQL+`
		GROUP BY 1, 2
		ORDER BY 1, 2
	`, args...)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USAGE_TREND_FAILED", "failed-to-query-usage-trend", nil)
		return
	}
	defer rows.Close()
	items := []usageTrendPoint{}
	for rows.Next() {
		var item usageTrendPoint
		if err := rows.Scan(&item.Day, &item.Name, &item.Requests); err != nil {
			adminError(w, http.StatusInternalServerError, "USAGE_TREND_FAILED", "failed-to-query-usage-trend", nil)
			return
		}
		items = append(items, item)
	}
	adminOK(w, map[string]any{"items": items})
}

// UsageSummary 返回近 N 天的请求/Token/成本汇总,默认 7 天,上限 90 天。
func (d ServerDeps) UsageSummary(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermUsageRead) != nil {
		return
	}
	days, err := strconv.Atoi(r.URL.Query().Get("days"))
	if err != nil || days < 1 {
		days = 7
	}
	if days > 90 {
		days = 90
	}
	whereSQL, args, err := buildUsageFilters(r, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusBadRequest, "USAGE_FILTER_INVALID", "invalid-usage-filter", nil)
		return
	}
	// 自定义开始时间存在时,以时间范围为准,不再叠加“近 N 天”窗口。
	if strings.TrimSpace(r.URL.Query().Get("start_time")) == "" {
		args = append(args, strconv.Itoa(days))
		whereSQL += fmt.Sprintf(" AND u.created_at >= now() - ($%d || ' days')::interval", len(args))
	}
	summary := usageSummaryResponse{}
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FILTER (WHERE u.state IN ('success','failed','interrupted')),
		       count(*) FILTER (WHERE u.state = 'success'),
		       count(*) FILTER (WHERE u.state IN ('failed','interrupted')),
		       COALESCE(sum(u.input_tokens), 0), COALESCE(sum(u.output_tokens), 0)
		FROM reporting_usage_records u
		WHERE `+whereSQL, args...).Scan(
		&summary.TotalRequests, &summary.SuccessRequests, &summary.FailedRequests,
		&summary.TotalInputTokens, &summary.TotalOutputTokens,
	); err != nil {
		adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "failed-to-query-usage-summaries", nil)
		return
	}
	currencyRows, err := d.Pool.Query(r.Context(), `
		SELECT u.cost_currency, COALESCE(sum(u.cost), 0)::text
		FROM reporting_usage_records u
		WHERE `+whereSQL+`
		GROUP BY u.cost_currency ORDER BY u.cost_currency
	`, args...)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "failed-to-query-currency-summaries", nil)
		return
	}
	defer currencyRows.Close()
	summary.Costs = []usageCurrencyTotal{}
	for currencyRows.Next() {
		var item usageCurrencyTotal
		if err := currencyRows.Scan(&item.Currency, &item.Total); err != nil {
			adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "failed-to-read-currency-summaries", nil)
			return
		}
		summary.Costs = append(summary.Costs, item)
	}
	if len(summary.Costs) == 1 {
		summary.TotalCost = summary.Costs[0].Total
		summary.CostCurrency = summary.Costs[0].Currency
	} else if len(summary.Costs) > 1 {
		summary.TotalCost = "0"
		summary.CostCurrency = "MIXED"
	} else {
		summary.TotalCost = "0"
		summary.CostCurrency = "USD"
	}
	modelRows, err := d.Pool.Query(r.Context(), `
		SELECT COALESCE(u.provider, ''), COALESCE(u.upstream_model, ''), u.cost_currency,
		       count(*), COALESCE(sum(u.input_tokens), 0), COALESCE(sum(u.output_tokens), 0), COALESCE(sum(u.cost), 0)::text
		FROM reporting_usage_records u
		WHERE `+whereSQL+`
		GROUP BY u.provider, u.upstream_model, u.cost_currency
		ORDER BY count(*) DESC
		LIMIT 20
	`, args...)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "failed-to-query-usage-summaries", nil)
		return
	}
	defer modelRows.Close()
	summary.ByModel = []usageModelSummary{}
	for modelRows.Next() {
		var item usageModelSummary
		if err := modelRows.Scan(&item.Provider, &item.UpstreamModel, &item.CostCurrency, &item.Requests,
			&item.InputTokens, &item.OutputTokens, &item.Cost); err != nil {
			adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "failed-to-query-usage-summaries", nil)
			return
		}
		summary.ByModel = append(summary.ByModel, item)
	}
	projectRows, err := d.Pool.Query(r.Context(), `
		SELECT COALESCE(p.name, '-'), count(*), COALESCE(sum(u.input_tokens), 0), COALESCE(sum(u.output_tokens), 0),
		       COALESCE(sum(u.cost), 0)::text, u.cost_currency
		FROM reporting_usage_records u
		LEFT JOIN projects p ON p.id = u.project_id
		WHERE `+whereSQL+`
		GROUP BY p.name, u.cost_currency
		ORDER BY count(*) DESC
		LIMIT 20
	`, args...)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "failed-to-query-usage-summaries", nil)
		return
	}
	defer projectRows.Close()
	summary.ByProject = []usageScopeSummary{}
	for projectRows.Next() {
		var item usageScopeSummary
		if err := projectRows.Scan(&item.Name, &item.Requests, &item.InputTokens, &item.OutputTokens, &item.Cost, &item.CostCurrency); err != nil {
			adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "failed-to-query-usage-summaries", nil)
			return
		}
		summary.ByProject = append(summary.ByProject, item)
	}
	applicationRows, err := d.Pool.Query(r.Context(), `
		SELECT COALESCE(a.name, '-'), count(*), COALESCE(sum(u.input_tokens), 0), COALESCE(sum(u.output_tokens), 0),
		       COALESCE(sum(u.cost), 0)::text, u.cost_currency
		FROM reporting_usage_records u
		LEFT JOIN applications a ON a.id = u.application_id
		WHERE `+whereSQL+`
		GROUP BY a.name, u.cost_currency
		ORDER BY count(*) DESC
		LIMIT 20
	`, args...)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "failed-to-query-usage-summaries", nil)
		return
	}
	defer applicationRows.Close()
	summary.ByApplication = []usageScopeSummary{}
	for applicationRows.Next() {
		var item usageScopeSummary
		if err := applicationRows.Scan(&item.Name, &item.Requests, &item.InputTokens, &item.OutputTokens, &item.Cost, &item.CostCurrency); err != nil {
			adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "failed-to-query-usage-summaries", nil)
			return
		}
		summary.ByApplication = append(summary.ByApplication, item)
	}
	apiKeyRows, err := d.Pool.Query(r.Context(), `
		SELECT COALESCE(k.name, '-'), count(*), COALESCE(sum(u.input_tokens), 0), COALESCE(sum(u.output_tokens), 0),
		       COALESCE(sum(u.cost), 0)::text, u.cost_currency
		FROM reporting_usage_records u
		LEFT JOIN api_keys k ON k.id = u.api_key_id
		WHERE `+whereSQL+`
		GROUP BY k.name, u.cost_currency
		ORDER BY count(*) DESC
		LIMIT 20
	`, args...)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "failed-to-query-usage-summaries", nil)
		return
	}
	defer apiKeyRows.Close()
	summary.ByAPIKey = []usageScopeSummary{}
	for apiKeyRows.Next() {
		var item usageScopeSummary
		if err := apiKeyRows.Scan(&item.Name, &item.Requests, &item.InputTokens, &item.OutputTokens, &item.Cost, &item.CostCurrency); err != nil {
			adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "failed-to-query-usage-summaries", nil)
			return
		}
		summary.ByAPIKey = append(summary.ByAPIKey, item)
	}
	departmentRows, err := d.Pool.Query(r.Context(), `
		SELECT COALESCE(d.name, '-'), count(*), COALESCE(sum(u.input_tokens), 0), COALESCE(sum(u.output_tokens), 0),
		       COALESCE(sum(u.cost), 0)::text, u.cost_currency
		FROM reporting_usage_records u
		LEFT JOIN projects p ON p.id = u.project_id
		LEFT JOIN departments d ON d.id = p.department_id
		WHERE `+whereSQL+`
		GROUP BY d.name, u.cost_currency
		ORDER BY count(*) DESC
		LIMIT 20
	`, args...)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "failed-to-query-usage-summaries", nil)
		return
	}
	defer departmentRows.Close()
	summary.ByDepartment = []usageScopeSummary{}
	for departmentRows.Next() {
		var item usageScopeSummary
		if err := departmentRows.Scan(&item.Name, &item.Requests, &item.InputTokens, &item.OutputTokens, &item.Cost, &item.CostCurrency); err != nil {
			adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "failed-to-query-usage-summaries", nil)
			return
		}
		summary.ByDepartment = append(summary.ByDepartment, item)
	}
	modelServiceRows, err := d.Pool.Query(r.Context(), `
		SELECT m.name, count(*), COALESCE(sum(u.input_tokens), 0), COALESCE(sum(u.output_tokens), 0),
		       COALESCE(sum(u.cost), 0)::text, u.cost_currency
		FROM reporting_usage_records u
		JOIN model_services m ON m.id = u.model_service_id
		WHERE `+whereSQL+`
		GROUP BY m.name, u.cost_currency
		ORDER BY count(*) DESC
		LIMIT 20
	`, args...)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "failed-to-query-usage-summaries", nil)
		return
	}
	defer modelServiceRows.Close()
	summary.ByModelService = []usageScopeSummary{}
	for modelServiceRows.Next() {
		var item usageScopeSummary
		if err := modelServiceRows.Scan(&item.Name, &item.Requests, &item.InputTokens, &item.OutputTokens, &item.Cost, &item.CostCurrency); err != nil {
			adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "failed-to-query-usage-summaries", nil)
			return
		}
		summary.ByModelService = append(summary.ByModelService, item)
	}
	stateRows, err := d.Pool.Query(r.Context(), `
		SELECT u.state, count(*), COALESCE(sum(u.input_tokens), 0), COALESCE(sum(u.output_tokens), 0),
		       COALESCE(sum(u.cost), 0)::text, u.cost_currency
		FROM reporting_usage_records u
		WHERE `+whereSQL+`
		GROUP BY u.state, u.cost_currency
		ORDER BY count(*) DESC
	`, args...)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "failed-to-query-usage-summaries", nil)
		return
	}
	defer stateRows.Close()
	summary.ByState = []usageScopeSummary{}
	for stateRows.Next() {
		var item usageScopeSummary
		if err := stateRows.Scan(&item.Name, &item.Requests, &item.InputTokens, &item.OutputTokens, &item.Cost, &item.CostCurrency); err != nil {
			adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "failed-to-query-usage-summaries", nil)
			return
		}
		summary.ByState = append(summary.ByState, item)
	}
	sourceRows, err := d.Pool.Query(r.Context(), `
		SELECT u.source, count(*), COALESCE(sum(u.input_tokens), 0), COALESCE(sum(u.output_tokens), 0),
		       COALESCE(sum(u.cost), 0)::text, u.cost_currency
		FROM reporting_usage_records u
		WHERE `+whereSQL+`
		GROUP BY u.source, u.cost_currency
		ORDER BY count(*) DESC
	`, args...)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "failed-to-query-usage-summaries", nil)
		return
	}
	defer sourceRows.Close()
	summary.BySource = []usageScopeSummary{}
	for sourceRows.Next() {
		var item usageScopeSummary
		if err := sourceRows.Scan(&item.Name, &item.Requests, &item.InputTokens, &item.OutputTokens, &item.Cost, &item.CostCurrency); err != nil {
			adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "failed-to-query-usage-summaries", nil)
			return
		}
		summary.BySource = append(summary.BySource, item)
	}
	providerRows, err := d.Pool.Query(r.Context(), `
		SELECT COALESCE(u.provider, '-'), count(*), COALESCE(sum(u.input_tokens), 0), COALESCE(sum(u.output_tokens), 0),
		       COALESCE(sum(u.cost), 0)::text, u.cost_currency
		FROM reporting_usage_records u
		WHERE `+whereSQL+`
		GROUP BY u.provider, u.cost_currency
		ORDER BY count(*) DESC
	`, args...)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "failed-to-query-usage-summaries", nil)
		return
	}
	defer providerRows.Close()
	summary.ByProvider = []usageScopeSummary{}
	for providerRows.Next() {
		var item usageScopeSummary
		if err := providerRows.Scan(&item.Name, &item.Requests, &item.InputTokens, &item.OutputTokens, &item.Cost, &item.CostCurrency); err != nil {
			adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "failed-to-query-usage-summaries", nil)
			return
		}
		summary.ByProvider = append(summary.ByProvider, item)
	}
	routePolicyRows, err := d.Pool.Query(r.Context(), `
		SELECT COALESCE(rp.name, '-'), count(*), COALESCE(sum(u.input_tokens), 0), COALESCE(sum(u.output_tokens), 0),
		       COALESCE(sum(u.cost), 0)::text, u.cost_currency
		FROM reporting_usage_records u
		LEFT JOIN route_policies rp ON rp.id = u.route_policy_id
		WHERE `+whereSQL+`
		GROUP BY rp.name, u.cost_currency
		ORDER BY count(*) DESC
	`, args...)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "failed-to-query-usage-summaries", nil)
		return
	}
	defer routePolicyRows.Close()
	summary.ByRoutePolicy = []usageScopeSummary{}
	for routePolicyRows.Next() {
		var item usageScopeSummary
		if err := routePolicyRows.Scan(&item.Name, &item.Requests, &item.InputTokens, &item.OutputTokens, &item.Cost, &item.CostCurrency); err != nil {
			adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "failed-to-query-usage-summaries", nil)
			return
		}
		summary.ByRoutePolicy = append(summary.ByRoutePolicy, item)
	}
	errorTypeRows, err := d.Pool.Query(r.Context(), `
		SELECT COALESCE(u.error_type, '-'), count(*), COALESCE(sum(u.input_tokens), 0), COALESCE(sum(u.output_tokens), 0),
		       COALESCE(sum(u.cost), 0)::text, u.cost_currency
		FROM reporting_usage_records u
		WHERE `+whereSQL+`
		GROUP BY u.error_type, u.cost_currency
		ORDER BY count(*) DESC
	`, args...)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "failed-to-query-usage-summaries", nil)
		return
	}
	defer errorTypeRows.Close()
	summary.ByErrorType = []usageScopeSummary{}
	for errorTypeRows.Next() {
		var item usageScopeSummary
		if err := errorTypeRows.Scan(&item.Name, &item.Requests, &item.InputTokens, &item.OutputTokens, &item.Cost, &item.CostCurrency); err != nil {
			adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "failed-to-query-usage-summaries", nil)
			return
		}
		summary.ByErrorType = append(summary.ByErrorType, item)
	}
	dayRows, err := d.Pool.Query(r.Context(), `
		SELECT to_char(date_trunc('day', u.created_at), 'YYYY-MM-DD') AS day, count(*)
		FROM reporting_usage_records u
		WHERE `+whereSQL+`
		GROUP BY 1 ORDER BY 1
	`, args...)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "failed-to-query-usage-summaries", nil)
		return
	}
	defer dayRows.Close()
	summary.ByDay = []usageDaySummary{}
	for dayRows.Next() {
		var item usageDaySummary
		if err := dayRows.Scan(&item.Day, &item.Requests); err != nil {
			adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "failed-to-query-usage-summaries", nil)
			return
		}
		summary.ByDay = append(summary.ByDay, item)
	}
	adminOK(w, summary)
}

// UsageRecords 返回用量明细分页,支持项目/应用/Key/模型/状态/来源/时间筛选。
func (d ServerDeps) UsageRecords(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermUsageRead) != nil {
		return
	}
	page, pageSize := parsePaging(r)
	offset := (page - 1) * pageSize
	result := usageRecordsPage{Page: page, PageSize: pageSize, Records: []usageRecordResponse{}}

	where := []string{"u.organization_id = $1"}
	args := []any{principal.OrganizationID}
	addUUIDFilter := func(param, column string) bool {
		value := strings.TrimSpace(r.URL.Query().Get(param))
		if value == "" {
			return true
		}
		parsed, err := uuid.Parse(value)
		if err != nil {
			adminError(w, http.StatusBadRequest, "USAGE_FILTER_INVALID", "invalid-usage-filter", nil)
			return false
		}
		args = append(args, parsed)
		where = append(where, fmt.Sprintf("%s = $%d", column, len(args)))
		return true
	}
	for _, filter := range []struct{ param, column string }{
		{"project_id", "u.project_id"},
		{"application_id", "u.application_id"},
		{"api_key_id", "u.api_key_id"},
		{"model_service_id", "u.model_service_id"},
	} {
		if !addUUIDFilter(filter.param, filter.column) {
			return
		}
	}
	if value := strings.TrimSpace(r.URL.Query().Get("state")); value != "" {
		switch value {
		case "pending", "success", "failed", "interrupted":
			args = append(args, value)
			where = append(where, fmt.Sprintf("u.state = $%d", len(args)))
		default:
			adminError(w, http.StatusBadRequest, "USAGE_FILTER_INVALID", "invalid-usage-filter", nil)
			return
		}
	}
	if value := strings.TrimSpace(r.URL.Query().Get("source")); value != "" {
		switch value {
		case "api", "prompt_debug", "agent_debug":
			args = append(args, value)
			where = append(where, fmt.Sprintf("u.source = $%d", len(args)))
		default:
			adminError(w, http.StatusBadRequest, "USAGE_FILTER_INVALID", "invalid-usage-filter", nil)
			return
		}
	}
	if value := strings.TrimSpace(r.URL.Query().Get("start_time")); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			adminError(w, http.StatusBadRequest, "USAGE_FILTER_INVALID", "invalid-usage-filter", nil)
			return
		}
		args = append(args, parsed)
		where = append(where, fmt.Sprintf("u.created_at >= $%d", len(args)))
	}
	if value := strings.TrimSpace(r.URL.Query().Get("end_time")); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			adminError(w, http.StatusBadRequest, "USAGE_FILTER_INVALID", "invalid-usage-filter", nil)
			return
		}
		args = append(args, parsed)
		where = append(where, fmt.Sprintf("u.created_at < $%d", len(args)))
	}

	whereSQL := strings.Join(where, " AND ")
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM reporting_usage_records u WHERE `+whereSQL, args...).Scan(&result.Total); err != nil {
		adminError(w, http.StatusInternalServerError, "USAGE_RECORDS_FAILED", "failed-to-query-usage-records", nil)
		return
	}
	args = append(args, pageSize, offset)
	rows, err := d.Pool.Query(r.Context(), `
		SELECT u.id, u.request_id, u.created_at, p.name, a.name, k.name, m.name, u.route_policy_id,
		       u.state, u.status_code, u.provider, u.upstream_model,
		       u.input_tokens, u.output_tokens, u.cached_tokens, u.reasoning_tokens,
		       u.first_token_ms, u.total_ms, u.cost::text, u.cost_currency, u.source,
		       u.error_type, u.error_message
		FROM reporting_usage_records u
		LEFT JOIN projects p ON p.id = u.project_id
		LEFT JOIN applications a ON a.id = u.application_id
		LEFT JOIN api_keys k ON k.id = u.api_key_id
		JOIN model_services m ON m.id = u.model_service_id
		WHERE `+whereSQL+`
		ORDER BY u.created_at DESC
		LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USAGE_RECORDS_FAILED", "failed-to-query-usage-records", nil)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var item usageRecordResponse
		var id, requestID uuid.UUID
		var routePolicyID *uuid.UUID
		var createdAt time.Time
		if err := rows.Scan(&id, &requestID, &createdAt, &item.ProjectName, &item.ApplicationName, &item.KeyName,
			&item.ModelService, &routePolicyID, &item.State, &item.StatusCode, &item.Provider, &item.UpstreamModel,
			&item.InputTokens, &item.OutputTokens, &item.CachedTokens, &item.ReasoningTokens,
			&item.FirstTokenMS, &item.TotalMS, &item.Cost, &item.CostCurrency, &item.Source,
			&item.ErrorType, &item.ErrorMessage); err != nil {
			adminError(w, http.StatusInternalServerError, "USAGE_RECORDS_FAILED", "failed-to-query-usage-records", nil)
			return
		}
		item.ID = id.String()
		item.RequestID = requestID.String()
		if routePolicyID != nil {
			policyID := routePolicyID.String()
			item.RoutePolicyID = &policyID
		}
		item.CreatedAt = createdAt.Format(time.RFC3339)
		result.Records = append(result.Records, item)
	}
	adminOK(w, result)
}

// UsageRecordsExport 导出当前筛选条件下的用量明细 CSV。
func (d ServerDeps) UsageRecordsExport(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermUsageExport) != nil {
		return
	}
	whereSQL, args, err := buildUsageFilters(r, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusBadRequest, "USAGE_FILTER_INVALID", "invalid-usage-filter", nil)
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT u.request_id, u.created_at, COALESCE(p.name, ''), COALESCE(a.name, ''), COALESCE(k.name, ''), m.name,
		       COALESCE(u.route_policy_id::text, ''), u.state, COALESCE(u.status_code::text, ''),
		       COALESCE(u.provider, ''), COALESCE(u.upstream_model, ''),
		       u.input_tokens, u.output_tokens, u.cached_tokens, u.reasoning_tokens,
		       COALESCE(u.first_token_ms::text, ''), COALESCE(u.total_ms::text, ''),
		       u.cost::text, u.cost_currency, u.source, COALESCE(u.error_type, ''), COALESCE(u.error_message, '')
		FROM reporting_usage_records u
		LEFT JOIN projects p ON p.id = u.project_id
		LEFT JOIN applications a ON a.id = u.application_id
		LEFT JOIN api_keys k ON k.id = u.api_key_id
		JOIN model_services m ON m.id = u.model_service_id
		WHERE `+whereSQL+`
		ORDER BY u.created_at DESC
		LIMIT 10000
	`, args...)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USAGE_EXPORT_FAILED", "failed-to-export-usage-records", nil)
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="usage-records.csv"`)
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{
		"request_id", "created_at", "project", "application", "api_key", "model_service", "route_policy_id",
		"state", "status_code", "provider", "upstream_model", "input_tokens", "output_tokens",
		"cached_tokens", "reasoning_tokens", "first_token_ms", "total_ms", "cost", "cost_currency",
		"source", "error_type", "error_message",
	})
	for rows.Next() {
		var requestID uuid.UUID
		var createdAt time.Time
		var record [22]string
		if err := rows.Scan(&requestID, &createdAt, &record[2], &record[3], &record[4], &record[5], &record[6],
			&record[7], &record[8], &record[9], &record[10], &record[11], &record[12], &record[13],
			&record[14], &record[15], &record[16], &record[17], &record[18], &record[19], &record[20],
			&record[21]); err != nil {
			adminError(w, http.StatusInternalServerError, "USAGE_EXPORT_FAILED", "failed-to-export-usage-records", nil)
			return
		}
		record[0] = requestID.String()
		record[1] = createdAt.Format(time.RFC3339)
		if err := writer.Write(record[:]); err != nil {
			return
		}
	}
	if err := rows.Err(); err != nil {
		adminError(w, http.StatusInternalServerError, "USAGE_EXPORT_FAILED", "failed-to-export-usage-records", nil)
		return
	}
	writer.Flush()
}

type auditEventResponse struct {
	ID           string  `json:"id"`
	CreatedAt    string  `json:"created_at"`
	ActorType    string  `json:"actor_type"`
	ActorName    string  `json:"actor_name"`
	Action       string  `json:"action"`
	ResourceType string  `json:"resource_type"`
	ResourceID   *string `json:"resource_id"`
	ResourceName string  `json:"resource_name"`
	// ResourceExists 表示受影响资源是否仍然存在(软删除的 API Key 视为不存在),前端据此决定是否提供直达入口。
	ResourceExists bool   `json:"resource_exists"`
	Result         string `json:"result"`

	// 以下两项只在服务端解析资源名称时使用,不下发。
	snapshotName string
	parentID     string
}

// auditResourceNameSQL 按资源 ID 取当前名称与是否仍然存在,供审计列表展示“资源”列并决定是否提供直达入口。
// 各分支都限定在当前组织内;模型候选用上游模型名,预算用所属范围名称。
// 预算所属范围可能已被删除,取不到名称时得到 NULL,外层统一转成空串。
// API Key 是软删除,记录还在但已不可用,present 记为 false。
const auditResourceNameSQL = `
	SELECT id::text, COALESCE(name, ''), present FROM (
		SELECT id, name, true AS present FROM model_services WHERE organization_id = $1 AND id = ANY($2::uuid[])
		UNION ALL SELECT c.id, c.upstream_model, true FROM model_candidates c
			JOIN model_services s ON s.id = c.model_service_id
			WHERE s.organization_id = $1 AND c.id = ANY($2::uuid[])
		UNION ALL SELECT id, name, true FROM route_policies WHERE organization_id = $1 AND id = ANY($2::uuid[])
		UNION ALL SELECT w.id, CASE w.scope_type
			WHEN 'organization' THEN (SELECT name FROM organizations WHERE id = w.scope_id)
			WHEN 'project' THEN (SELECT name FROM projects WHERE id = w.scope_id)
			WHEN 'application' THEN (SELECT name FROM applications WHERE id = w.scope_id)
			WHEN 'api_key' THEN (SELECT name FROM api_keys WHERE id = w.scope_id)
		END, true
		FROM budget_windows w WHERE w.organization_id = $1 AND w.id = ANY($2::uuid[])
		UNION ALL SELECT id, name, true FROM projects WHERE organization_id = $1 AND id = ANY($2::uuid[])
		UNION ALL SELECT id, name, true FROM applications WHERE organization_id = $1 AND id = ANY($2::uuid[])
		UNION ALL SELECT id, name, true FROM departments WHERE organization_id = $1 AND id = ANY($2::uuid[])
		UNION ALL SELECT id, name, true FROM provider_accounts WHERE organization_id = $1 AND id = ANY($2::uuid[])
		UNION ALL SELECT id, name, true FROM assets WHERE organization_id = $1 AND id = ANY($2::uuid[])
		UNION ALL SELECT id, name, deleted_at IS NULL FROM api_keys WHERE organization_id = $1 AND id = ANY($2::uuid[])
		UNION ALL SELECT id, email, true FROM users WHERE organization_id = $1 AND id = ANY($2::uuid[])
		UNION ALL SELECT id, name, true FROM prompt_evaluation_sets WHERE organization_id = $1 AND id = ANY($2::uuid[])
		UNION ALL SELECT id, name, true FROM organizations WHERE id = $1 AND id = ANY($2::uuid[])
	) n
`

// fillAuditResourceNames 为审计事件补上受影响资源的当前名称。
// 资源已删除取不到当前名称时,先取审计快照里记录的自身名称;
// 授权类事件的快照里没有自身名称,再退回所属模型服务 / 资产的名称;都没有则留空,由前端显示“-”。
func (d ServerDeps) fillAuditResourceNames(ctx context.Context, organizationID uuid.UUID, records []auditEventResponse) error {
	ids := []uuid.UUID{}
	for i := range records {
		if records[i].ResourceID != nil {
			if id, err := uuid.Parse(*records[i].ResourceID); err == nil {
				ids = append(ids, id)
			}
		}
		if id, err := uuid.Parse(records[i].parentID); err == nil {
			ids = append(ids, id)
		}
	}
	names, err := d.auditResourceNames(ctx, organizationID, ids)
	if err != nil {
		return err
	}
	for i := range records {
		item := &records[i]
		name, present := "", false
		if item.ResourceID != nil {
			if row, ok := names[*item.ResourceID]; ok {
				name, present = row.Name, row.Present
			}
		}
		// 授权类事件直达的是所属模型服务 / 资产,所属资源仍在就可以跳转。
		isGrant := item.ResourceType == "model_service_grant" || item.ResourceType == "asset_grant"
		if isGrant && item.parentID != "" && names[item.parentID].Present {
			present = true
		}
		if name == "" {
			name = item.snapshotName
		}
		if name == "" {
			name = names[item.parentID].Name
		}
		item.ResourceName = name
		item.ResourceExists = present
	}
	return nil
}

// auditResourceRow 是一条资源记录当前的名称,以及它是否仍然可用。
type auditResourceRow struct {
	Name    string
	Present bool
}

// auditResourceNames 按资源 ID 批量取仍有记录的资源;名称可能为空(例如所属范围已删除的预算窗口)。
func (d ServerDeps) auditResourceNames(ctx context.Context, organizationID uuid.UUID, ids []uuid.UUID) (map[string]auditResourceRow, error) {
	names := map[string]auditResourceRow{}
	if len(ids) == 0 {
		return names, nil
	}
	rows, err := d.Pool.Query(ctx, auditResourceNameSQL, organizationID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var row auditResourceRow
		if err := rows.Scan(&id, &row.Name, &row.Present); err != nil {
			return nil, err
		}
		names[id.String()] = row
	}
	return names, rows.Err()
}

// snapshotUUIDs 从审计快照 JSON 里收集 UUID 形式的字符串值(含数组元素),
// 用于把快照里的资源 ID 解析成名称。快照来自 jsonb 列,解析失败时按没有 ID 处理。
func snapshotUUIDs(states ...[]byte) []uuid.UUID {
	ids := []uuid.UUID{}
	var walk func(node any)
	walk = func(node any) {
		switch typed := node.(type) {
		case map[string]any:
			for _, child := range typed {
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		case string:
			if id, err := uuid.Parse(typed); err == nil {
				ids = append(ids, id)
			}
		}
	}
	for _, raw := range states {
		if len(raw) == 0 {
			continue
		}
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			continue
		}
		walk(value)
	}
	return ids
}

type auditEventsPage struct {
	Total    int64                `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
	Records  []auditEventResponse `json:"records"`
}

// AuditEvents 返回当前组织审计事件分页,支持身份/动作/资源/结果/时间筛选。
func (d ServerDeps) AuditEvents(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermAuditRead) != nil {
		return
	}
	page, pageSize := parsePaging(r)
	offset := (page - 1) * pageSize
	result := auditEventsPage{Page: page, PageSize: pageSize, Records: []auditEventResponse{}}

	where := []string{"e.organization_id = $1"}
	args := []any{principal.OrganizationID}
	addFilter := func(column, value string, exact bool) {
		if value == "" {
			return
		}
		args = append(args, value)
		if exact {
			where = append(where, fmt.Sprintf("%s = $%d", column, len(args)))
			return
		}
		args[len(args)-1] = "%" + value + "%"
		where = append(where, fmt.Sprintf("%s ILIKE $%d", column, len(args)))
	}
	addFilter("e.actor_type", strings.TrimSpace(r.URL.Query().Get("actor_type")), true)
	addFilter("e.action", strings.TrimSpace(r.URL.Query().Get("action")), false)
	// 资源类型按精确匹配:模糊匹配会让“模型服务”同时命中“模型服务授权”。
	addFilter("e.resource_type", strings.TrimSpace(r.URL.Query().Get("resource_type")), true)
	addFilter("e.resource_id", strings.TrimSpace(r.URL.Query().Get("resource_id")), true)
	addFilter("e.result", strings.TrimSpace(r.URL.Query().Get("result")), true)
	if value := strings.TrimSpace(r.URL.Query().Get("start_time")); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			adminError(w, http.StatusBadRequest, "AUDIT_FILTER_INVALID", "invalid-audit-filter", nil)
			return
		}
		args = append(args, parsed)
		where = append(where, fmt.Sprintf("e.created_at >= $%d", len(args)))
	}
	if value := strings.TrimSpace(r.URL.Query().Get("end_time")); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			adminError(w, http.StatusBadRequest, "AUDIT_FILTER_INVALID", "invalid-audit-filter", nil)
			return
		}
		args = append(args, parsed)
		where = append(where, fmt.Sprintf("e.created_at < $%d", len(args)))
	}
	if value := strings.TrimSpace(r.URL.Query().Get("actor_type")); value != "" {
		switch value {
		case "user", "api_key", "system":
		default:
			adminError(w, http.StatusBadRequest, "AUDIT_FILTER_INVALID", "invalid-audit-filter", nil)
			return
		}
	}
	if value := strings.TrimSpace(r.URL.Query().Get("result")); value != "" {
		switch value {
		case "success", "failure":
		default:
			adminError(w, http.StatusBadRequest, "AUDIT_FILTER_INVALID", "invalid-audit-filter", nil)
			return
		}
	}

	whereSQL := strings.Join(where, " AND ")
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM audit_events e WHERE `+whereSQL, args...).Scan(&result.Total); err != nil {
		adminError(w, http.StatusInternalServerError, "AUDIT_LIST_FAILED", "failed-to-query-audit-events", nil)
		return
	}
	args = append(args, pageSize, offset)
	rows, err := d.Pool.Query(r.Context(), `
		SELECT e.id, e.created_at, e.actor_type, COALESCE(u.email, ''),
		       e.action, e.resource_type, e.resource_id, e.result,
		       COALESCE(e.after_state->>'name', e.before_state->>'name',
		                e.after_state->>'scope_name', e.before_state->>'scope_name', e.after_state->>'email', ''),
		       COALESCE(e.after_state->>'model_service_id', e.before_state->>'model_service_id',
		                e.after_state->>'asset_id', e.before_state->>'asset_id',
		                NULLIF(e.after_state->>'scope_id', ''), NULLIF(e.before_state->>'scope_id', ''), '')
		FROM audit_events e
		LEFT JOIN users u ON u.id = e.actor_user_id
		WHERE `+whereSQL+`
		ORDER BY e.created_at DESC
		LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "AUDIT_LIST_FAILED", "failed-to-query-audit-events", nil)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var item auditEventResponse
		var id uuid.UUID
		var createdAt time.Time
		if err := rows.Scan(&id, &createdAt, &item.ActorType, &item.ActorName,
			&item.Action, &item.ResourceType, &item.ResourceID, &item.Result,
			&item.snapshotName, &item.parentID); err != nil {
			adminError(w, http.StatusInternalServerError, "AUDIT_LIST_FAILED", "failed-to-query-audit-events", nil)
			return
		}
		item.ID = id.String()
		item.CreatedAt = createdAt.Format(time.RFC3339)
		result.Records = append(result.Records, item)
	}
	rows.Close()
	if err := d.fillAuditResourceNames(r.Context(), principal.OrganizationID, result.Records); err != nil {
		adminError(w, http.StatusInternalServerError, "AUDIT_LIST_FAILED", "failed-to-query-audit-events", nil)
		return
	}
	adminOK(w, result)
}

type auditEventDetailResponse struct {
	ID           string          `json:"id"`
	CreatedAt    string          `json:"created_at"`
	ActorType    string          `json:"actor_type"`
	ActorName    string          `json:"actor_name"`
	Action       string          `json:"action"`
	ResourceType string          `json:"resource_type"`
	ResourceID   *string         `json:"resource_id"`
	Result       string          `json:"result"`
	BeforeState  json.RawMessage `json:"before_state"`
	AfterState   json.RawMessage `json:"after_state"`
	// SnapshotNames 是快照里出现的资源 ID 对应的当前名称,供前端把 ID 显示成名称。
	SnapshotNames map[string]string `json:"snapshot_names"`
	IPAddress     *string           `json:"ip_address"`
	UserAgent     *string           `json:"user_agent"`
}

// AuditEventDetail 返回单条审计事件的完整内容,含 before/after 状态与请求来源。
func (d ServerDeps) AuditEventDetail(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermAuditRead) != nil {
		return
	}
	eventID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "AUDIT_ID_INVALID", "invalid-audit-event-id", nil)
		return
	}
	var item auditEventDetailResponse
	var id uuid.UUID
	var createdAt time.Time
	var beforeRaw, afterRaw []byte
	err = d.Pool.QueryRow(r.Context(), `
		SELECT e.id, e.created_at, e.actor_type, COALESCE(u.email, ''),
		       e.action, e.resource_type, e.resource_id, e.result,
		       e.before_state, e.after_state, host(e.ip_address), e.user_agent
		FROM audit_events e
		LEFT JOIN users u ON u.id = e.actor_user_id
		WHERE e.id = $1 AND e.organization_id = $2
	`, eventID, principal.OrganizationID).Scan(
		&id, &createdAt, &item.ActorType, &item.ActorName,
		&item.Action, &item.ResourceType, &item.ResourceID, &item.Result,
		&beforeRaw, &afterRaw, &item.IPAddress, &item.UserAgent)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "AUDIT_NOT_FOUND", "audit-event-does-not-exist-or", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "AUDIT_DETAIL_FAILED", "failed-to-query-audit-details", nil)
		return
	}
	item.ID = id.String()
	item.CreatedAt = createdAt.Format(time.RFC3339)
	item.BeforeState = normalizeAuditJSON(beforeRaw)
	item.AfterState = normalizeAuditJSON(afterRaw)
	resolved, err := d.auditResourceNames(r.Context(), principal.OrganizationID, snapshotUUIDs(beforeRaw, afterRaw))
	if err != nil {
		adminError(w, http.StatusInternalServerError, "AUDIT_DETAIL_FAILED", "failed-to-query-audit-details", nil)
		return
	}
	// 快照里只关心 ID 对应的名称;取不到名称的 ID 不放进映射,前端会原样显示 ID。
	item.SnapshotNames = map[string]string{}
	for id, row := range resolved {
		if row.Name != "" {
			item.SnapshotNames[id] = row.Name
		}
	}
	adminOK(w, item)
}

// normalizeAuditJSON 把空 JSON 归一为 null,避免前端渲染 "{}" 干扰。
func normalizeAuditJSON(raw []byte) json.RawMessage {
	if len(raw) == 0 || string(raw) == "{}" {
		return nil
	}
	return raw
}

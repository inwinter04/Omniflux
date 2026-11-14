package httpx

import (
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/inwinter04/omniflux/internal/auth"
)

type usageSummaryResponse struct {
	TotalRequests      int64                  `json:"total_requests"`
	SuccessRequests    int64                  `json:"success_requests"`
	FailedRequests     int64                  `json:"failed_requests"`
	TotalInputTokens   int64                  `json:"total_input_tokens"`
	TotalOutputTokens  int64                  `json:"total_output_tokens"`
	TotalCost          string                 `json:"total_cost"`
	CostCurrency       string                 `json:"cost_currency"`
	ByModel            []usageModelSummary    `json:"by_model"`
	ByDay              []usageDaySummary      `json:"by_day"`
}

type usageModelSummary struct {
	Provider         string `json:"provider"`
	UpstreamModel    string `json:"upstream_model"`
	Requests         int64  `json:"requests"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	Cost             string `json:"cost"`
}

type usageDaySummary struct {
	Day      string `json:"day"`
	Requests int64  `json:"requests"`
}

type usageRecordResponse struct {
	ID             string  `json:"id"`
	CreatedAt      string  `json:"created_at"`
	ProjectName    string  `json:"project_name"`
	ApplicationName string `json:"application_name"`
	KeyName        string  `json:"key_name"`
	ModelService   string  `json:"model_service"`
	State          string  `json:"state"`
	StatusCode     *int    `json:"status_code"`
	Provider       *string `json:"provider"`
	UpstreamModel  *string `json:"upstream_model"`
	InputTokens    int64   `json:"input_tokens"`
	OutputTokens   int64   `json:"output_tokens"`
	Cost           string  `json:"cost"`
	TotalMS        *int    `json:"total_ms"`
}

type usageRecordsPage struct {
	Total   int64                 `json:"total"`
	Page    int                   `json:"page"`
	PageSize int                  `json:"page_size"`
	Records []usageRecordResponse `json:"records"`
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

// UsageSummary 返回近 N 天的请求/Token/成本汇总,默认 7 天,上限 90 天。
func (d ServerDeps) UsageSummary(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireViewer(w, principal) != nil {
		return
	}
	days, err := strconv.Atoi(r.URL.Query().Get("days"))
	if err != nil || days < 1 {
		days = 7
	}
	if days > 90 {
		days = 90
	}
	summary := usageSummaryResponse{}
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FILTER (WHERE state IN ('success','failed','interrupted')),
		       count(*) FILTER (WHERE state = 'success'),
		       count(*) FILTER (WHERE state IN ('failed','interrupted')),
		       COALESCE(sum(input_tokens), 0), COALESCE(sum(output_tokens), 0),
		       COALESCE(sum(cost), 0)::text, COALESCE(max(cost_currency), 'USD')
		FROM usage_records
		WHERE organization_id = $1 AND created_at >= now() - ($2 || ' days')::interval
	`, principal.OrganizationID, strconv.Itoa(days)).Scan(
		&summary.TotalRequests, &summary.SuccessRequests, &summary.FailedRequests,
		&summary.TotalInputTokens, &summary.TotalOutputTokens, &summary.TotalCost, &summary.CostCurrency,
	); err != nil {
		adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "无法查询用量汇总", nil)
		return
	}
	modelRows, err := d.Pool.Query(r.Context(), `
		SELECT COALESCE(provider, '未知'), COALESCE(upstream_model, '未知'),
		       count(*), COALESCE(sum(input_tokens), 0), COALESCE(sum(output_tokens), 0), COALESCE(sum(cost), 0)::text
		FROM usage_records
		WHERE organization_id = $1 AND created_at >= now() - ($2 || ' days')::interval
		GROUP BY provider, upstream_model
		ORDER BY count(*) DESC
		LIMIT 20
	`, principal.OrganizationID, strconv.Itoa(days))
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "无法查询用量汇总", nil)
		return
	}
	defer modelRows.Close()
	summary.ByModel = []usageModelSummary{}
	for modelRows.Next() {
		var item usageModelSummary
		if err := modelRows.Scan(&item.Provider, &item.UpstreamModel, &item.Requests,
			&item.InputTokens, &item.OutputTokens, &item.Cost); err != nil {
			adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "无法查询用量汇总", nil)
			return
		}
		summary.ByModel = append(summary.ByModel, item)
	}
	dayRows, err := d.Pool.Query(r.Context(), `
		SELECT to_char(date_trunc('day', created_at), 'YYYY-MM-DD') AS day, count(*)
		FROM usage_records
		WHERE organization_id = $1 AND created_at >= now() - ($2 || ' days')::interval
		GROUP BY 1 ORDER BY 1
	`, principal.OrganizationID, strconv.Itoa(days))
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "无法查询用量汇总", nil)
		return
	}
	defer dayRows.Close()
	summary.ByDay = []usageDaySummary{}
	for dayRows.Next() {
		var item usageDaySummary
		if err := dayRows.Scan(&item.Day, &item.Requests); err != nil {
			adminError(w, http.StatusInternalServerError, "USAGE_SUMMARY_FAILED", "无法查询用量汇总", nil)
			return
		}
		summary.ByDay = append(summary.ByDay, item)
	}
	adminOK(w, summary)
}

// UsageRecords 返回用量明细分页,按时间倒序,附带项目/应用/Key/模型的业务名称。
func (d ServerDeps) UsageRecords(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireViewer(w, principal) != nil {
		return
	}
	page, pageSize := parsePaging(r)
	offset := (page - 1) * pageSize
	result := usageRecordsPage{Page: page, PageSize: pageSize, Records: []usageRecordResponse{}}
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM usage_records WHERE organization_id = $1
	`, principal.OrganizationID).Scan(&result.Total); err != nil {
		adminError(w, http.StatusInternalServerError, "USAGE_RECORDS_FAILED", "无法查询用量明细", nil)
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT u.id, u.created_at, p.name, a.name, k.name, m.name,
		       u.state, u.status_code, u.provider, u.upstream_model,
		       u.input_tokens, u.output_tokens, u.cost::text, u.total_ms
		FROM usage_records u
		JOIN projects p ON p.id = u.project_id
		JOIN applications a ON a.id = u.application_id
		JOIN api_keys k ON k.id = u.api_key_id
		JOIN model_services m ON m.id = u.model_service_id
		WHERE u.organization_id = $1
		ORDER BY u.created_at DESC
		LIMIT $2 OFFSET $3
	`, principal.OrganizationID, pageSize, offset)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USAGE_RECORDS_FAILED", "无法查询用量明细", nil)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var item usageRecordResponse
		var id uuid.UUID
		var createdAt time.Time
		if err := rows.Scan(&id, &createdAt, &item.ProjectName, &item.ApplicationName, &item.KeyName,
			&item.ModelService, &item.State, &item.StatusCode, &item.Provider, &item.UpstreamModel,
			&item.InputTokens, &item.OutputTokens, &item.Cost, &item.TotalMS); err != nil {
			adminError(w, http.StatusInternalServerError, "USAGE_RECORDS_FAILED", "无法查询用量明细", nil)
			return
		}
		item.ID = id.String()
		item.CreatedAt = createdAt.Format(time.RFC3339)
		result.Records = append(result.Records, item)
	}
	adminOK(w, result)
}

type auditEventResponse struct {
	ID           string `json:"id"`
	CreatedAt    string `json:"created_at"`
	ActorType    string `json:"actor_type"`
	ActorName    string `json:"actor_name"`
	Action       string `json:"action"`
	ResourceType string `json:"resource_type"`
	ResourceID   *string `json:"resource_id"`
	Result       string `json:"result"`
}

type auditEventsPage struct {
	Total   int64                `json:"total"`
	Page    int                  `json:"page"`
	PageSize int                 `json:"page_size"`
	Records []auditEventResponse `json:"records"`
}

// AuditEvents 返回当前组织审计事件分页,按时间倒序,actor 显示业务身份。
func (d ServerDeps) AuditEvents(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireViewer(w, principal) != nil {
		return
	}
	page, pageSize := parsePaging(r)
	offset := (page - 1) * pageSize
	result := auditEventsPage{Page: page, PageSize: pageSize, Records: []auditEventResponse{}}
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM audit_events WHERE organization_id = $1
	`, principal.OrganizationID).Scan(&result.Total); err != nil {
		adminError(w, http.StatusInternalServerError, "AUDIT_LIST_FAILED", "无法查询审计事件", nil)
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT e.id, e.created_at, e.actor_type, COALESCE(u.email, '系统'),
		       e.action, e.resource_type, e.resource_id, e.result
		FROM audit_events e
		LEFT JOIN users u ON u.id = e.actor_user_id
		WHERE e.organization_id = $1
		ORDER BY e.created_at DESC
		LIMIT $2 OFFSET $3
	`, principal.OrganizationID, pageSize, offset)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "AUDIT_LIST_FAILED", "无法查询审计事件", nil)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var item auditEventResponse
		var id uuid.UUID
		var createdAt time.Time
		if err := rows.Scan(&id, &createdAt, &item.ActorType, &item.ActorName,
			&item.Action, &item.ResourceType, &item.ResourceID, &item.Result); err != nil {
			adminError(w, http.StatusInternalServerError, "AUDIT_LIST_FAILED", "无法查询审计事件", nil)
			return
		}
		item.ID = id.String()
		item.CreatedAt = createdAt.Format(time.RFC3339)
		result.Records = append(result.Records, item)
	}
	adminOK(w, result)
}



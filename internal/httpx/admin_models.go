package httpx

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/inwinter04/omniflux/internal/auth"
)

type candidateResponse struct {
	ID                   string  `json:"id"`
	ProviderAccountID    string  `json:"provider_account_id"`
	Provider             string  `json:"provider"`
	UpstreamModel        string  `json:"upstream_model"`
	Priority             int     `json:"priority"`
	Weight               float64 `json:"weight"`
	InputCostPerMillion  float64 `json:"input_cost_per_million"`
	OutputCostPerMillion float64 `json:"output_cost_per_million"`
	QualityScore         float64 `json:"quality_score"`
	CostCurrency         string  `json:"cost_currency"`
	Status               string  `json:"status"`
}

type modelServiceResponse struct {
	ID               string              `json:"id"`
	Name             string              `json:"name"`
	RoutePolicyID    *string             `json:"route_policy_id"`
	Status           string              `json:"status"`
	CreatedAt        time.Time           `json:"created_at"`
	UpdatedAt        time.Time           `json:"updated_at"`
	SuccessRate24h   *float64            `json:"success_rate_24h"`
	Requests24h      int64               `json:"requests_24h"`
	AvgLatencyMS24h  *float64            `json:"avg_latency_ms_24h"`
	LastUsedAt       *time.Time          `json:"last_used_at"`
	ActiveCandidates int                 `json:"active_candidates"`
	HealthStatus     string              `json:"health_status"`
	Candidates       []candidateResponse `json:"candidates"`
}

type createCandidateRequest struct {
	ProviderAccountID    string   `json:"provider_account_id"`
	UpstreamModel        string   `json:"upstream_model"`
	Priority             int      `json:"priority"`
	Weight               float64  `json:"weight"`
	InputCostPerMillion  float64  `json:"input_cost_per_million"`
	OutputCostPerMillion float64  `json:"output_cost_per_million"`
	QualityScore         *float64 `json:"quality_score"`
	CostCurrency         string   `json:"cost_currency"`
	Status               string   `json:"status"`
}

type createModelServiceRequest struct {
	Name          string                   `json:"name"`
	RoutePolicyID *string                  `json:"route_policy_id"`
	Candidates    []createCandidateRequest `json:"candidates"`
}

// ModelServices 返回当前组织的模型服务列表(含候选)。
func (d ServerDeps) ModelServices(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermModelServiceRead) != nil {
		return
	}
	svcRows, err := d.Pool.Query(r.Context(), `
		SELECT id, name, route_policy_id, status, created_at, updated_at
		FROM model_services WHERE organization_id = $1 ORDER BY created_at DESC
	`, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "SERVICE_LIST_FAILED", "failed-to-query-model-services", nil)
		return
	}
	defer svcRows.Close()
	list := []modelServiceResponse{}
	serviceIDs := map[uuid.UUID]int{}
	for svcRows.Next() {
		var item modelServiceResponse
		var id uuid.UUID
		var policyID *uuid.UUID
		if err := svcRows.Scan(&id, &item.Name, &policyID, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			adminError(w, http.StatusInternalServerError, "SERVICE_LIST_FAILED", "failed-to-query-model-services", nil)
			return
		}
		item.ID = id.String()
		if policyID != nil {
			policy := policyID.String()
			item.RoutePolicyID = &policy
		}
		item.Candidates = []candidateResponse{}
		list = append(list, item)
		serviceIDs[id] = len(list) - 1
	}
	if err := svcRows.Err(); err != nil {
		adminError(w, http.StatusInternalServerError, "SERVICE_LIST_FAILED", "failed-to-query-model-services", nil)
		return
	}
	if len(list) == 0 {
		adminOK(w, list)
		return
	}
	candRows, err := d.Pool.Query(r.Context(), `
		SELECT c.id, c.model_service_id, c.provider_account_id, a.provider, c.upstream_model, c.priority, c.weight,
		       c.input_cost_per_million, c.output_cost_per_million, c.quality_score, c.cost_currency, c.status
		FROM model_candidates c
		JOIN provider_accounts a ON a.id = c.provider_account_id
		WHERE a.organization_id = $1 AND c.retired_at IS NULL
		ORDER BY c.priority ASC
	`, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "SERVICE_LIST_FAILED", "failed-to-query-model-candidates", nil)
		return
	}
	defer candRows.Close()
	for candRows.Next() {
		var candID, svcID, accountID uuid.UUID
		var c candidateResponse
		if err := candRows.Scan(&candID, &svcID, &accountID, &c.Provider, &c.UpstreamModel, &c.Priority, &c.Weight,
			&c.InputCostPerMillion, &c.OutputCostPerMillion, &c.QualityScore, &c.CostCurrency, &c.Status); err != nil {
			adminError(w, http.StatusInternalServerError, "SERVICE_LIST_FAILED", "failed-to-query-model-candidates", nil)
			return
		}
		c.ID = candID.String()
		c.ProviderAccountID = accountID.String()
		if index, ok := serviceIDs[svcID]; ok {
			list[index].Candidates = append(list[index].Candidates, c)
		}
	}
	healthRows, err := d.Pool.Query(r.Context(), `
		SELECT c.model_service_id, count(u.id),
		       avg(CASE WHEN u.state = 'success' THEN 1.0 ELSE 0.0 END) FILTER (WHERE u.id IS NOT NULL),
		       avg(u.total_ms) FILTER (WHERE u.state = 'success'), max(u.created_at)
		FROM model_candidates c
		LEFT JOIN usage_records u ON u.model_candidate_id = c.id
		     AND u.created_at >= now() - interval '24 hours' AND u.state <> 'pending'
		WHERE c.model_service_id IN (SELECT id FROM model_services WHERE organization_id = $1)
		GROUP BY c.model_service_id
	`, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "SERVICE_LIST_FAILED", "failed-to-query-model-service-health", nil)
		return
	}
	defer healthRows.Close()
	type serviceHealth struct {
		requests int64
		rate     *float64
		latency  *float64
		lastUsed *time.Time
	}
	health := map[uuid.UUID]serviceHealth{}
	for healthRows.Next() {
		var id uuid.UUID
		var item serviceHealth
		if err := healthRows.Scan(&id, &item.requests, &item.rate, &item.latency, &item.lastUsed); err != nil {
			adminError(w, http.StatusInternalServerError, "SERVICE_LIST_FAILED", "failed-to-read-model-service-health", nil)
			return
		}
		health[id] = item
	}
	for index := range list {
		activeCandidates := 0
		for _, candidate := range list[index].Candidates {
			if candidate.Status == "active" {
				activeCandidates++
			}
		}
		list[index].ActiveCandidates = activeCandidates
		item := health[uuid.MustParse(list[index].ID)]
		list[index].Requests24h = item.requests
		list[index].SuccessRate24h = item.rate
		list[index].AvgLatencyMS24h = item.latency
		list[index].LastUsedAt = item.lastUsed
		list[index].HealthStatus = deriveHealthStatus(list[index].Status, item.requests, item.rate, item.latency, activeCandidates)
	}
	adminOK(w, list)
}

// CreateModelService 创建模型服务与候选;候选引用的账号必须属于当前组织。
func (d ServerDeps) CreateModelService(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermModelServiceManage) != nil {
		return
	}
	var body createModelServiceRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		adminError(w, http.StatusBadRequest, "SERVICE_NAME_REQUIRED", "enter-a-model-service-name", nil)
		return
	}
	if len(body.Candidates) == 0 {
		adminError(w, http.StatusBadRequest, "SERVICE_CANDIDATES_REQUIRED", "at-least-one-model-candidate-is", nil)
		return
	}
	accountIDs := make([]uuid.UUID, 0, len(body.Candidates))
	candidateKeys := map[string]bool{}
	for _, c := range body.Candidates {
		if strings.TrimSpace(c.UpstreamModel) == "" {
			adminError(w, http.StatusBadRequest, "SERVICE_CANDIDATE_INVALID", "candidate-is-missing-upstream-model", nil)
			return
		}
		if c.QualityScore != nil && (*c.QualityScore < 0 || *c.QualityScore > 1) {
			adminError(w, http.StatusBadRequest, "SERVICE_CANDIDATE_INVALID", "candidate-quality-score-must-be-between", nil)
			return
		}
		parsed, err := uuid.Parse(c.ProviderAccountID)
		if err != nil {
			adminError(w, http.StatusBadRequest, "SERVICE_CANDIDATE_INVALID", "invalid-candidate-provider-account-id", nil)
			return
		}
		key := parsed.String() + "|" + strings.TrimSpace(c.UpstreamModel)
		if candidateKeys[key] {
			adminError(w, http.StatusBadRequest, "SERVICE_CANDIDATE_INVALID", "duplicate-model-candidate", nil)
			return
		}
		candidateKeys[key] = true
		accountIDs = append(accountIDs, parsed)
	}
	// 同一账号可挂多个候选,校验前去重;候选引用的账号必须属于当前组织。
	uniqueAccountIDs := make([]uuid.UUID, 0, len(accountIDs))
	seen := make(map[uuid.UUID]bool, len(accountIDs))
	for _, id := range accountIDs {
		if !seen[id] {
			seen[id] = true
			uniqueAccountIDs = append(uniqueAccountIDs, id)
		}
	}
	var accountCount int
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM provider_accounts WHERE id = ANY($1) AND organization_id = $2
	`, uniqueAccountIDs, principal.OrganizationID).Scan(&accountCount); err != nil {
		adminError(w, http.StatusInternalServerError, "SERVICE_CREATE_FAILED", "failed-to-create-the-model-service", nil)
		return
	}
	if accountCount != len(uniqueAccountIDs) {
		adminError(w, http.StatusBadRequest, "SERVICE_ACCOUNT_NOT_FOUND", "the-provider-account-referenced-by-the", nil)
		return
	}
	var policyID *uuid.UUID
	if body.RoutePolicyID != nil && *body.RoutePolicyID != "" {
		parsed, err := uuid.Parse(*body.RoutePolicyID)
		if err != nil {
			adminError(w, http.StatusBadRequest, "SERVICE_POLICY_INVALID", "invalid-route-policy-id-1abhob0", nil)
			return
		}
		var count int
		if err := d.Pool.QueryRow(r.Context(), `
			SELECT count(*) FROM route_policies WHERE id = $1 AND organization_id = $2
		`, parsed, principal.OrganizationID).Scan(&count); err != nil {
			adminError(w, http.StatusInternalServerError, "SERVICE_CREATE_FAILED", "failed-to-create-the-model-service", nil)
			return
		}
		if count == 0 {
			adminError(w, http.StatusBadRequest, "SERVICE_POLICY_NOT_FOUND", "route-policy-does-not-exist-or", nil)
			return
		}
		policyID = &parsed
	}
	if _, err := normalizeCandidateCurrencies(r.Context(), d.Pool, body.Candidates); err != nil {
		reason := "serviceCurrencyInvalid"
		if errors.Is(err, errCandidateCurrencyMismatch) {
			reason = "candidate-currency-must-match-provider-account"
		}
		adminError(w, http.StatusBadRequest, "SERVICE_CURRENCY_INVALID", reason, nil)
		return
	}
	serviceID := uuid.New()
	err := pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO model_services(id, organization_id, name, route_policy_id)
			VALUES ($1,$2,$3,$4)
		`, serviceID, principal.OrganizationID, body.Name, nullablePolicyID(policyID)); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO model_service_grants(model_service_id, organization_id, subject_type, subject_id, actions, created_by)
			VALUES ($1,$2,'organization',$2,ARRAY['read','invoke']::text[],$3)
		`, serviceID, principal.OrganizationID, principal.UserID); err != nil {
			return err
		}
		for _, c := range body.Candidates {
			accountID, _ := uuid.Parse(c.ProviderAccountID)
			weight := c.Weight
			if weight <= 0 {
				weight = 1
			}
			priority := c.Priority
			if priority == 0 {
				priority = 100
			}
			if _, err := tx.Exec(r.Context(), `
				INSERT INTO model_candidates(model_service_id, provider_account_id, upstream_model,
				                             priority, weight, input_cost_per_million, output_cost_per_million, quality_score, cost_currency, status)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
			`, serviceID, accountID, c.UpstreamModel, priority, weight,
				c.InputCostPerMillion, c.OutputCostPerMillion, candidateQuality(c), c.CostCurrency, candidateStatus(c)); err != nil {
				return err
			}
		}
		_, err := tx.Exec(r.Context(), `
			INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, after_state)
			VALUES ($1,$2,'user','model_service.created','model_service',$3,$4)
		`, principal.OrganizationID, principal.UserID, serviceID.String(), auditJSON(map[string]any{"name": body.Name, "candidates": len(body.Candidates)}))
		return err
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "model_services_organization_id_name_key" {
			adminError(w, http.StatusConflict, "SERVICE_NAME_EXISTS", "a-model-service-with-the-same", nil)
			return
		}
		adminError(w, http.StatusInternalServerError, "SERVICE_CREATE_FAILED", "failed-to-create-the-model-service", nil)
		return
	}
	adminOK(w, map[string]string{"id": serviceID.String()})
}

// candidateQuality 返回候选入库质量分;未传时取 schema 默认 0.5,由路由评分读取。
func candidateQuality(c createCandidateRequest) float64 {
	if c.QualityScore == nil {
		return 0.5
	}
	return *c.QualityScore
}

func candidateStatus(c createCandidateRequest) string {
	if c.Status == "disabled" {
		return "disabled"
	}
	return "active"
}

func nullablePolicyID(id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	return *id
}

// DeleteModelService 删除模型服务;存在用量记录时拒绝,否则连同候选一起删除。
func (d ServerDeps) DeleteModelService(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermModelServiceManage) != nil {
		return
	}
	serviceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "SERVICE_ID_INVALID", "invalid-model-service-id", nil)
		return
	}
	var exists bool
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT EXISTS (SELECT 1 FROM model_services WHERE id = $1 AND organization_id = $2)
	`, serviceID, principal.OrganizationID).Scan(&exists); err != nil {
		adminError(w, http.StatusInternalServerError, "SERVICE_DELETE_FAILED", "failed-to-delete-the-model-service", nil)
		return
	}
	if !exists {
		adminError(w, http.StatusNotFound, "SERVICE_NOT_FOUND", "model-service-does-not-exist-or", nil)
		return
	}
	var usageCount int
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM usage_records WHERE model_service_id = $1
	`, serviceID).Scan(&usageCount); err != nil {
		adminError(w, http.StatusInternalServerError, "SERVICE_DELETE_FAILED", "failed-to-delete-the-model-service", nil)
		return
	}
	if usageCount > 0 {
		adminError(w, http.StatusConflict, "SERVICE_HAS_USAGE",
			"service-has-usage", nil)
		return
	}
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		// 资源删除后已查不到名称,审计里记下它,审计列表才能显示“删的是什么”。
		var name string
		if err := tx.QueryRow(r.Context(), `DELETE FROM model_services WHERE id = $1 RETURNING name`, serviceID).Scan(&name); err != nil {
			return err
		}
		return writeAuditEvent(r.Context(), tx, principal.OrganizationID, principal.UserID,
			"model_service.deleted", "model_service", serviceID.String(), map[string]any{"name": name}, nil)
	})
	if err != nil {
		adminError(w, http.StatusInternalServerError, "SERVICE_DELETE_FAILED", "failed-to-delete-the-model-service", nil)
		return
	}
	adminOK(w, nil)
}

// ModelCandidateLookup 返回候选所属的模型服务,供审计资源跳转使用。
func (d ServerDeps) ModelCandidateLookup(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermModelServiceRead) != nil {
		return
	}
	candidateID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "CANDIDATE_ID_INVALID", "invalid-candidate-model-id", nil)
		return
	}
	var serviceID uuid.UUID
	err = d.Pool.QueryRow(r.Context(), `
		SELECT model_service_id FROM model_candidates
		WHERE id = $1 AND model_service_id IN (
			SELECT id FROM model_services WHERE organization_id = $2
		)
	`, candidateID, principal.OrganizationID).Scan(&serviceID)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "CANDIDATE_NOT_FOUND", "candidate-model-does-not-exist-or", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "CANDIDATE_LOOKUP_FAILED", "failed-to-query-model-candidates", nil)
		return
	}
	adminOK(w, map[string]string{"model_service_id": serviceID.String()})
}

// UpdateModelService 更新服务名称/绑定策略,并按稳定业务键 upsert 候选。
// 已不再提交的候选只标记 retired,保留 ID 让历史 usage 继续关联。
func (d ServerDeps) UpdateModelService(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermModelServiceManage) != nil {
		return
	}
	serviceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "SERVICE_ID_INVALID", "invalid-model-service-id", nil)
		return
	}
	var body createModelServiceRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		adminError(w, http.StatusBadRequest, "SERVICE_NAME_REQUIRED", "enter-a-model-service-name", nil)
		return
	}
	if len(body.Candidates) == 0 {
		adminError(w, http.StatusBadRequest, "SERVICE_CANDIDATES_REQUIRED", "at-least-one-model-candidate-is", nil)
		return
	}
	accountIDs := make([]uuid.UUID, 0, len(body.Candidates))
	candidateKeys := map[string]bool{}
	for _, c := range body.Candidates {
		if strings.TrimSpace(c.UpstreamModel) == "" {
			adminError(w, http.StatusBadRequest, "SERVICE_CANDIDATE_INVALID", "candidate-is-missing-upstream-model", nil)
			return
		}
		if c.QualityScore != nil && (*c.QualityScore < 0 || *c.QualityScore > 1) {
			adminError(w, http.StatusBadRequest, "SERVICE_CANDIDATE_INVALID", "candidate-quality-score-must-be-between", nil)
			return
		}
		parsed, err := uuid.Parse(c.ProviderAccountID)
		if err != nil {
			adminError(w, http.StatusBadRequest, "SERVICE_CANDIDATE_INVALID", "invalid-candidate-provider-account-id", nil)
			return
		}
		key := parsed.String() + "|" + strings.TrimSpace(c.UpstreamModel)
		if candidateKeys[key] {
			adminError(w, http.StatusBadRequest, "SERVICE_CANDIDATE_INVALID", "duplicate-model-candidate", nil)
			return
		}
		candidateKeys[key] = true
		accountIDs = append(accountIDs, parsed)
	}
	// 同一账号可挂多个候选,校验前去重。
	uniqueAccountIDs := make([]uuid.UUID, 0, len(accountIDs))
	seen := make(map[uuid.UUID]bool, len(accountIDs))
	for _, id := range accountIDs {
		if !seen[id] {
			seen[id] = true
			uniqueAccountIDs = append(uniqueAccountIDs, id)
		}
	}
	var accountCount int
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM provider_accounts WHERE id = ANY($1) AND organization_id = $2
	`, uniqueAccountIDs, principal.OrganizationID).Scan(&accountCount); err != nil {
		adminError(w, http.StatusInternalServerError, "SERVICE_UPDATE_FAILED", "failed-to-update-the-model-service", nil)
		return
	}
	if accountCount != len(uniqueAccountIDs) {
		adminError(w, http.StatusBadRequest, "SERVICE_ACCOUNT_NOT_FOUND", "the-provider-account-referenced-by-the", nil)
		return
	}
	var policyID *uuid.UUID
	if body.RoutePolicyID != nil && *body.RoutePolicyID != "" {
		parsed, err := uuid.Parse(*body.RoutePolicyID)
		if err != nil {
			adminError(w, http.StatusBadRequest, "SERVICE_POLICY_INVALID", "invalid-route-policy-id-1abhob0", nil)
			return
		}
		var count int
		if err := d.Pool.QueryRow(r.Context(), `
			SELECT count(*) FROM route_policies WHERE id = $1 AND organization_id = $2
		`, parsed, principal.OrganizationID).Scan(&count); err != nil {
			adminError(w, http.StatusInternalServerError, "SERVICE_UPDATE_FAILED", "failed-to-update-the-model-service", nil)
			return
		}
		if count == 0 {
			adminError(w, http.StatusBadRequest, "SERVICE_POLICY_NOT_FOUND", "route-policy-does-not-exist-or", nil)
			return
		}
		policyID = &parsed
	}
	if _, err := normalizeCandidateCurrencies(r.Context(), d.Pool, body.Candidates); err != nil {
		reason := "serviceCurrencyInvalid"
		if errors.Is(err, errCandidateCurrencyMismatch) {
			reason = "candidate-currency-must-match-provider-account"
		}
		adminError(w, http.StatusBadRequest, "SERVICE_CURRENCY_INVALID", reason, nil)
		return
	}
	var oldName string
	var oldPolicyID *uuid.UUID
	var beforeCandidates []candidateAuditState
	afterCandidates := make([]candidateAuditState, 0, len(body.Candidates))
	for _, c := range body.Candidates {
		weight := c.Weight
		if weight <= 0 {
			weight = 1
		}
		priority := c.Priority
		if priority == 0 {
			priority = 100
		}
		afterCandidates = append(afterCandidates, candidateAuditState{
			ProviderAccountID: c.ProviderAccountID, UpstreamModel: strings.TrimSpace(c.UpstreamModel),
			Priority: priority, Weight: weight, InputCostPerMillion: c.InputCostPerMillion,
			OutputCostPerMillion: c.OutputCostPerMillion, QualityScore: candidateQuality(c),
			CostCurrency: c.CostCurrency, Status: candidateStatus(c),
		})
	}
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(r.Context(), `
			SELECT name, route_policy_id FROM model_services WHERE id = $1 AND organization_id = $2 FOR UPDATE
		`, serviceID, principal.OrganizationID).Scan(&oldName, &oldPolicyID); err != nil {
			return err
		}
		var err error
		beforeCandidates, err = loadCandidateAuditStates(r.Context(), tx, serviceID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `
			UPDATE model_services SET name = $3, route_policy_id = $4, updated_at = now()
			WHERE id = $1 AND organization_id = $2
		`, serviceID, principal.OrganizationID, body.Name, nullablePolicyID(policyID)); err != nil {
			return err
		}
		for _, c := range body.Candidates {
			accountID, _ := uuid.Parse(c.ProviderAccountID)
			weight := c.Weight
			if weight <= 0 {
				weight = 1
			}
			priority := c.Priority
			if priority == 0 {
				priority = 100
			}
			if _, err := tx.Exec(r.Context(), `
				INSERT INTO model_candidates(model_service_id, provider_account_id, upstream_model,
				                             priority, weight, input_cost_per_million, output_cost_per_million,
				                             quality_score, cost_currency, status, retired_at)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULL)
				ON CONFLICT (model_service_id, provider_account_id, upstream_model) DO UPDATE SET
					priority = EXCLUDED.priority,
					weight = EXCLUDED.weight,
					input_cost_per_million = EXCLUDED.input_cost_per_million,
					output_cost_per_million = EXCLUDED.output_cost_per_million,
					quality_score = EXCLUDED.quality_score,
					cost_currency = EXCLUDED.cost_currency,
					status = EXCLUDED.status,
					retired_at = NULL,
					updated_at = now()
			`, serviceID, accountID, strings.TrimSpace(c.UpstreamModel), priority, weight,
				c.InputCostPerMillion, c.OutputCostPerMillion, candidateQuality(c), c.CostCurrency, candidateStatus(c)); err != nil {
				return err
			}
		}
		// 请求中不再出现的候选只退役,不删除,保留历史 usage 的 candidate_id。
		for _, old := range beforeCandidates {
			key := old.ProviderAccountID + "|" + old.UpstreamModel
			if candidateKeys[key] {
				continue
			}
			if _, err := tx.Exec(r.Context(), `
				UPDATE model_candidates SET retired_at = now(), updated_at = now()
				WHERE model_service_id = $1 AND provider_account_id = $2 AND upstream_model = $3 AND retired_at IS NULL
			`, serviceID, old.ProviderAccountID, old.UpstreamModel); err != nil {
				return err
			}
		}
		_, err = tx.Exec(r.Context(), `
			INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, before_state, after_state)
			VALUES ($1,$2,'user','model_service.updated','model_service',$3,$4,$5)
		`, principal.OrganizationID, principal.UserID, serviceID.String(),
			auditJSON(map[string]any{"name": oldName, "route_policy_id": uuidPtrString(oldPolicyID), "candidates": beforeCandidates}),
			auditJSON(map[string]any{"name": body.Name, "route_policy_id": body.RoutePolicyID, "candidates": afterCandidates}))
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "SERVICE_NOT_FOUND", "model-service-does-not-exist-or", nil)
		return
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "model_services_organization_id_name_key" {
			adminError(w, http.StatusConflict, "SERVICE_NAME_EXISTS", "a-model-service-with-the-same", nil)
			return
		}
		adminError(w, http.StatusInternalServerError, "SERVICE_UPDATE_FAILED", "failed-to-update-the-model-service", nil)
		return
	}
	adminOK(w, nil)
}

type candidateAuditState struct {
	ProviderAccountID    string  `json:"provider_account_id"`
	UpstreamModel        string  `json:"upstream_model"`
	Priority             int     `json:"priority"`
	Weight               float64 `json:"weight"`
	InputCostPerMillion  float64 `json:"input_cost_per_million"`
	OutputCostPerMillion float64 `json:"output_cost_per_million"`
	QualityScore         float64 `json:"quality_score"`
	CostCurrency         string  `json:"cost_currency"`
	Status               string  `json:"status"`
}

func loadCandidateAuditStates(ctx context.Context, q rowQuerier, serviceID uuid.UUID) ([]candidateAuditState, error) {
	rows, err := q.Query(ctx, `
		SELECT provider_account_id, upstream_model, priority, weight, input_cost_per_million,
		       output_cost_per_million, quality_score, cost_currency, status
		FROM model_candidates
		WHERE model_service_id = $1 AND retired_at IS NULL
		ORDER BY priority ASC, upstream_model ASC
	`, serviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []candidateAuditState{}
	for rows.Next() {
		var item candidateAuditState
		var accountID uuid.UUID
		if err := rows.Scan(&accountID, &item.UpstreamModel, &item.Priority, &item.Weight,
			&item.InputCostPerMillion, &item.OutputCostPerMillion, &item.QualityScore,
			&item.CostCurrency, &item.Status); err != nil {
			return nil, err
		}
		item.ProviderAccountID = accountID.String()
		items = append(items, item)
	}
	return items, rows.Err()
}

func uuidPtrString(value *uuid.UUID) any {
	if value == nil {
		return nil
	}
	return value.String()
}

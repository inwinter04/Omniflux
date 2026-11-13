package httpx

import (
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
	Status               string  `json:"status"`
}

type modelServiceResponse struct {
	ID            string              `json:"id"`
	Name          string              `json:"name"`
	RoutePolicyID *string             `json:"route_policy_id"`
	Status        string              `json:"status"`
	CreatedAt     time.Time           `json:"created_at"`
	UpdatedAt     time.Time           `json:"updated_at"`
	Candidates    []candidateResponse `json:"candidates"`
}

type createCandidateRequest struct {
	ProviderAccountID    string  `json:"provider_account_id"`
	UpstreamModel        string  `json:"upstream_model"`
	Priority             int     `json:"priority"`
	Weight               float64 `json:"weight"`
	InputCostPerMillion  float64 `json:"input_cost_per_million"`
	OutputCostPerMillion float64 `json:"output_cost_per_million"`
}

type createModelServiceRequest struct {
	Name          string                   `json:"name"`
	RoutePolicyID *string                  `json:"route_policy_id"`
	Candidates    []createCandidateRequest `json:"candidates"`
}

// ModelServices 返回当前组织的模型服务列表(含候选)。
func (d ServerDeps) ModelServices(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireAdmin(w, principal) != nil {
		return
	}
	svcRows, err := d.Pool.Query(r.Context(), `
		SELECT id, name, route_policy_id, status, created_at, updated_at
		FROM model_services WHERE organization_id = $1 ORDER BY created_at DESC
	`, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "SERVICE_LIST_FAILED", "无法查询模型服务", nil)
		return
	}
	defer svcRows.Close()
	list := []modelServiceResponse{}
	serviceIDs := map[uuid.UUID]*modelServiceResponse{}
	for svcRows.Next() {
		var item modelServiceResponse
		var id uuid.UUID
		var policyID *uuid.UUID
		if err := svcRows.Scan(&id, &item.Name, &policyID, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			adminError(w, http.StatusInternalServerError, "SERVICE_LIST_FAILED", "无法查询模型服务", nil)
			return
		}
		item.ID = id.String()
		if policyID != nil {
			policy := policyID.String()
			item.RoutePolicyID = &policy
		}
		item.Candidates = []candidateResponse{}
		list = append(list, item)
		serviceIDs[id] = &list[len(list)-1]
	}
	if err := svcRows.Err(); err != nil {
		adminError(w, http.StatusInternalServerError, "SERVICE_LIST_FAILED", "无法查询模型服务", nil)
		return
	}
	if len(list) == 0 {
		adminOK(w, list)
		return
	}
	candRows, err := d.Pool.Query(r.Context(), `
		SELECT c.id, c.model_service_id, c.provider_account_id, a.provider, c.upstream_model, c.priority, c.weight,
		       c.input_cost_per_million, c.output_cost_per_million, c.status
		FROM model_candidates c
		JOIN provider_accounts a ON a.id = c.provider_account_id
		WHERE a.organization_id = $1
		ORDER BY c.priority ASC
	`, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "SERVICE_LIST_FAILED", "无法查询模型候选", nil)
		return
	}
	defer candRows.Close()
	for candRows.Next() {
		var candID, svcID, accountID uuid.UUID
		var c candidateResponse
		if err := candRows.Scan(&candID, &svcID, &accountID, &c.Provider, &c.UpstreamModel, &c.Priority, &c.Weight,
			&c.InputCostPerMillion, &c.OutputCostPerMillion, &c.Status); err != nil {
			adminError(w, http.StatusInternalServerError, "SERVICE_LIST_FAILED", "无法查询模型候选", nil)
			return
		}
		c.ID = candID.String()
		c.ProviderAccountID = accountID.String()
		if svc, ok := serviceIDs[svcID]; ok {
			svc.Candidates = append(svc.Candidates, c)
		}
	}
	adminOK(w, list)
}

// CreateModelService 创建模型服务与候选;候选引用的账号必须属于当前组织。
func (d ServerDeps) CreateModelService(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireAdmin(w, principal) != nil {
		return
	}
	var body createModelServiceRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		adminError(w, http.StatusBadRequest, "SERVICE_NAME_REQUIRED", "请填写模型服务名称", nil)
		return
	}
	if len(body.Candidates) == 0 {
		adminError(w, http.StatusBadRequest, "SERVICE_CANDIDATES_REQUIRED", "至少需要一个模型候选", nil)
		return
	}
	accountIDs := make([]uuid.UUID, 0, len(body.Candidates))
	for _, c := range body.Candidates {
		if strings.TrimSpace(c.UpstreamModel) == "" {
			adminError(w, http.StatusBadRequest, "SERVICE_CANDIDATE_INVALID", "候选缺少 upstream_model", nil)
			return
		}
		parsed, err := uuid.Parse(c.ProviderAccountID)
		if err != nil {
			adminError(w, http.StatusBadRequest, "SERVICE_CANDIDATE_INVALID", "候选的 provider_account_id 不合法", nil)
			return
		}
		accountIDs = append(accountIDs, parsed)
	}
	// 候选引用的账号必须属于当前组织,FK 只保证存在,组织归属需要显式校验。
	var accountCount int
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM provider_accounts WHERE id = ANY($1) AND organization_id = $2
	`, accountIDs, principal.OrganizationID).Scan(&accountCount); err != nil {
		adminError(w, http.StatusInternalServerError, "SERVICE_CREATE_FAILED", "创建模型服务失败", nil)
		return
	}
	if accountCount != len(accountIDs) {
		adminError(w, http.StatusBadRequest, "SERVICE_ACCOUNT_NOT_FOUND", "候选引用的 Provider 账号不存在或不属于当前组织", nil)
		return
	}
	var policyID *uuid.UUID
	if body.RoutePolicyID != nil && *body.RoutePolicyID != "" {
		parsed, err := uuid.Parse(*body.RoutePolicyID)
		if err != nil {
			adminError(w, http.StatusBadRequest, "SERVICE_POLICY_INVALID", "route_policy_id 不合法", nil)
			return
		}
		var count int
		if err := d.Pool.QueryRow(r.Context(), `
			SELECT count(*) FROM route_policies WHERE id = $1 AND organization_id = $2
		`, parsed, principal.OrganizationID).Scan(&count); err != nil {
			adminError(w, http.StatusInternalServerError, "SERVICE_CREATE_FAILED", "创建模型服务失败", nil)
			return
		}
		if count == 0 {
			adminError(w, http.StatusBadRequest, "SERVICE_POLICY_NOT_FOUND", "路由策略不存在或不属于当前组织", nil)
			return
		}
		policyID = &parsed
	}
	serviceID := uuid.New()
	err := pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO model_services(id, organization_id, name, route_policy_id)
			VALUES ($1,$2,$3,$4)
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
				                             priority, weight, input_cost_per_million, output_cost_per_million)
				VALUES ($1,$2,$3,$4,$5,$6,$7)
			`, serviceID, accountID, c.UpstreamModel, priority, weight,
				c.InputCostPerMillion, c.OutputCostPerMillion); err != nil {
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
			adminError(w, http.StatusConflict, "SERVICE_NAME_EXISTS", "同名模型服务已存在", nil)
			return
		}
		adminError(w, http.StatusInternalServerError, "SERVICE_CREATE_FAILED", "创建模型服务失败", nil)
		return
	}
	adminOK(w, map[string]string{"id": serviceID.String()})
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
	if !ok || d.requireAdmin(w, principal) != nil {
		return
	}
	serviceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "SERVICE_ID_INVALID", "模型服务 ID 不合法", nil)
		return
	}
	var exists bool
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT EXISTS (SELECT 1 FROM model_services WHERE id = $1 AND organization_id = $2)
	`, serviceID, principal.OrganizationID).Scan(&exists); err != nil {
		adminError(w, http.StatusInternalServerError, "SERVICE_DELETE_FAILED", "删除模型服务失败", nil)
		return
	}
	if !exists {
		adminError(w, http.StatusNotFound, "SERVICE_NOT_FOUND", "模型服务不存在或不属于当前组织", nil)
		return
	}
	var usageCount int
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM usage_records WHERE model_service_id = $1
	`, serviceID).Scan(&usageCount); err != nil {
		adminError(w, http.StatusInternalServerError, "SERVICE_DELETE_FAILED", "删除模型服务失败", nil)
		return
	}
	if usageCount > 0 {
		adminError(w, http.StatusConflict, "SERVICE_HAS_USAGE",
			"模型服务存在用量记录,为保留审计数据无法删除", nil)
		return
	}
	if _, err := d.Pool.Exec(r.Context(), `DELETE FROM model_services WHERE id = $1`, serviceID); err != nil {
		adminError(w, http.StatusInternalServerError, "SERVICE_DELETE_FAILED", "删除模型服务失败", nil)
		return
	}
	_, _ = d.Pool.Exec(r.Context(), `
		INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id)
		VALUES ($1,$2,'user','model_service.deleted','model_service',$3)
	`, principal.OrganizationID, principal.UserID, serviceID.String())
	adminOK(w, nil)
}


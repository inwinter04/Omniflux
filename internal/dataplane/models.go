package dataplane

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

type authorizedModelRow struct {
	ServiceName    string
	ExposeRouter   bool
	RouterModelID  string
	ServiceCreated time.Time
	Provider       string
	UpstreamModel  string
}

type authorizedModelItem struct {
	ID       string
	Created  int64
	OwnedBy  string
	IsRouter bool
}

// ValidateRouterModelID verifies that an externally exposed smart-routing model alias
// is a normalized ASCII identifier safe for OpenAI/Anthropic/Gemini/Codex clients.
func ValidateRouterModelID(raw string) (string, error) {
	id := strings.TrimSpace(raw)
	if id == "" {
		return "", fmt.Errorf("router_model_id is required when expose_router is enabled")
	}
	if len(id) > 128 {
		return "", fmt.Errorf("router_model_id cannot exceed 128 characters")
	}
	if strings.HasSuffix(id, "/") || strings.Contains(id, "..") {
		return "", fmt.Errorf("router_model_id contains invalid path segments")
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		isAlphaNum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		isSymbol := c == '.' || c == '_' || c == '/' || c == '-'
		if i == 0 && !isAlphaNum {
			return "", fmt.Errorf("router_model_id must start with an ASCII letter or digit")
		}
		if !isAlphaNum && !isSymbol {
			return "", fmt.Errorf("router_model_id may only contain ASCII letters, digits, '.', '_', '-', and '/'")
		}
	}
	return id, nil
}

// buildAuthorizedModelList builds a deduplicated OpenAI/Gemini model list that
// transparently exposes every authorized concrete upstream model alongside each
// explicitly exposed smart-routing virtual model identifier.
func buildAuthorizedModelList(rows []authorizedModelRow) []authorizedModelItem {
	type concreteAgg struct {
		created   int64
		providers map[string]bool
	}
	type serviceAgg struct {
		created int64
	}
	concreteMap := make(map[string]*concreteAgg)
	routerMap := make(map[string]*serviceAgg)

	for _, row := range rows {
		routerID := strings.TrimSpace(row.RouterModelID)
		modelID := strings.TrimSpace(row.UpstreamModel)
		provider := strings.TrimSpace(logicalProviderName(row.Provider))
		ts := row.ServiceCreated.Unix()

		if modelID != "" {
			agg, exists := concreteMap[modelID]
			if !exists {
				agg = &concreteAgg{
					created:   ts,
					providers: make(map[string]bool),
				}
				concreteMap[modelID] = agg
			} else if ts < agg.created {
				agg.created = ts
			}
			if provider != "" {
				agg.providers[provider] = true
			}
		}

		if row.ExposeRouter && routerID != "" {
			if _, exists := routerMap[routerID]; !exists {
				routerMap[routerID] = &serviceAgg{created: ts}
			}
		}
	}

	concreteIDs := make([]string, 0, len(concreteMap))
	for id := range concreteMap {
		concreteIDs = append(concreteIDs, id)
	}
	sort.Strings(concreteIDs)

	items := make([]authorizedModelItem, 0, len(concreteIDs)+len(routerMap))
	for _, id := range concreteIDs {
		agg := concreteMap[id]
		ownedBy := "organization"
		if len(agg.providers) == 1 {
			for p := range agg.providers {
				ownedBy = p
			}
		}
		items = append(items, authorizedModelItem{
			ID:       id,
			Created:  agg.created,
			OwnedBy:  ownedBy,
			IsRouter: false,
		})
	}

	routerIDs := make([]string, 0, len(routerMap))
	for name := range routerMap {
		if _, duplicate := concreteMap[name]; duplicate {
			continue
		}
		routerIDs = append(routerIDs, name)
	}
	sort.Strings(routerIDs)

	for _, id := range routerIDs {
		items = append(items, authorizedModelItem{
			ID:       id,
			Created:  routerMap[id].created,
			OwnedBy:  "omniflux-router",
			IsRouter: true,
		})
	}
	return items
}

func (d Deps) listAuthorizedModels(ctx context.Context, keyCtx apiKeyContext) ([]authorizedModelItem, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT s.name, s.expose_router, COALESCE(s.router_model_id, ''), s.created_at, a.provider, c.upstream_model
		FROM model_services s
		JOIN model_candidates c
		  ON c.model_service_id = s.id AND c.status = 'active' AND c.retired_at IS NULL
		JOIN provider_accounts a
		  ON a.id = c.provider_account_id AND a.status = 'active'
		WHERE s.organization_id = $1 AND s.status = 'active'
		  AND EXISTS (
		    SELECT 1 FROM model_service_grants g
		    WHERE g.model_service_id = s.id AND g.organization_id = $1
		      AND g.actions && ARRAY['read','invoke']::text[]
		      AND ((g.subject_type = 'organization' AND g.subject_id = $1)
		        OR (g.subject_type = 'project' AND g.subject_id = $2)
		        OR (g.subject_type = 'department' AND g.subject_id = (
		          SELECT p.department_id FROM projects p WHERE p.id = $2 AND p.organization_id = $1
		        )))
		  )
		ORDER BY c.upstream_model ASC, s.created_at ASC
	`, keyCtx.OrganizationID, keyCtx.ProjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	collected := make([]authorizedModelRow, 0, 16)
	for rows.Next() {
		var row authorizedModelRow
		if err := rows.Scan(&row.ServiceName, &row.ExposeRouter, &row.RouterModelID, &row.ServiceCreated, &row.Provider, &row.UpstreamModel); err != nil {
			return nil, err
		}
		collected = append(collected, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return buildAuthorizedModelList(collected), nil
}

// resolveAuthorizedTarget resolves a client-requested model identifier either as an
// explicitly exposed smart-routing virtual model ID or as a concrete upstream model
// exposed through one or more authorized active model services.
func (d Deps) resolveAuthorizedTarget(ctx context.Context, keyCtx apiKeyContext, requestedModel, action string) (svc resolvedService, found bool, allowed bool, err error) {
	target := strings.TrimSpace(requestedModel)
	if target == "" {
		return resolvedService{}, false, false, nil
	}

	var deniedSvc resolvedService
	routerSvc, routerFound, err := d.loadRouterService(ctx, keyCtx.OrganizationID, target)
	if err != nil {
		return resolvedService{}, false, false, err
	}
	if routerFound {
		okAccess, accErr := d.hasModelServiceAccess(ctx, keyCtx.OrganizationID, keyCtx.ProjectID, routerSvc.ID, action)
		if accErr != nil {
			return resolvedService{}, false, false, accErr
		}
		if okAccess {
			return routerSvc, true, true, nil
		}
		deniedSvc = routerSvc
	}

	rows, err := d.Pool.Query(ctx, `
		SELECT s.id, s.route_policy_id,
		       c.id, a.provider, COALESCE(a.api_protocol, ''), a.id::text, a.name, c.upstream_model,
		       c.priority, c.weight, c.input_cost_per_million, c.output_cost_per_million,
		       c.cache_read_cost_per_million, c.cache_write_cost_per_million,
		       c.cost_currency, c.quality_score,
		       c.context_window, c.capabilities, c.tags
		FROM model_candidates c
		JOIN model_services s ON s.id = c.model_service_id
		JOIN provider_accounts a ON a.id = c.provider_account_id
		WHERE s.organization_id = $1 AND s.status = 'active'
		  AND c.status = 'active' AND c.retired_at IS NULL AND a.status = 'active'
		  AND c.upstream_model = $3
		  AND EXISTS (
		    SELECT 1 FROM model_service_grants g
		    WHERE g.model_service_id = s.id AND g.organization_id = $1
		      AND g.actions @> ARRAY[$4]::text[]
		      AND ((g.subject_type = 'organization' AND g.subject_id = $1)
		        OR (g.subject_type = 'project' AND g.subject_id = $2)
		        OR (g.subject_type = 'department' AND g.subject_id = (
		          SELECT p.department_id FROM projects p WHERE p.id = $2 AND p.organization_id = $1
		        )))
		  )
		ORDER BY c.priority ASC, s.created_at ASC, c.id ASC
	`, keyCtx.OrganizationID, keyCtx.ProjectID, target, action)
	if err != nil {
		return resolvedService{}, false, false, err
	}
	defer rows.Close()

	var resolved resolvedService
	seenAccModel := make(map[string]bool)
	for rows.Next() {
		var (
			serviceID uuid.UUID
			policyID  *uuid.UUID
			c         candidate
		)
		if err := rows.Scan(
			&serviceID, &policyID,
			&c.ID, &c.Provider, &c.APIProtocol, &c.AccountID, &c.AccountName, &c.UpstreamModel,
			&c.Priority, &c.Weight, &c.InputCostPerM, &c.OutputCostPerM,
			&c.CacheReadCostPerM, &c.CacheWriteCostPerM,
			&c.CostCurrency, &c.Quality,
			&c.ContextWindow, &c.Capabilities, &c.Tags,
		); err != nil {
			return resolvedService{}, false, false, err
		}
		if resolved.ID == uuid.Nil {
			resolved.ID = serviceID
			resolved.Name = target
			resolved.PolicyID = policyID
		}
		dedupeKey := c.AccountID + "\x00" + c.UpstreamModel
		if seenAccModel[dedupeKey] {
			continue
		}
		seenAccModel[dedupeKey] = true
		resolved.Candidates = append(resolved.Candidates, c)
	}
	if err := rows.Err(); err != nil {
		return resolvedService{}, false, false, err
	}

	if len(resolved.Candidates) > 0 {
		resolved.Policy = defaultPolicy
		if resolved.PolicyID != nil {
			policy, pFound, pErr := d.loadPolicyByID(ctx, keyCtx.OrganizationID, *resolved.PolicyID)
			if pErr != nil {
				return resolvedService{}, false, false, pErr
			}
			if pFound {
				resolved.Policy = policy
			}
		}
		return resolved, true, true, nil
	}

	if deniedSvc.ID != uuid.Nil {
		return deniedSvc, true, false, nil
	}

	var deniedServiceID uuid.UUID
	err = d.Pool.QueryRow(ctx, `
		SELECT s.id
		FROM model_candidates c
		JOIN model_services s ON s.id = c.model_service_id
		JOIN provider_accounts a ON a.id = c.provider_account_id
		WHERE s.organization_id = $1 AND s.status = 'active'
		  AND c.status = 'active' AND c.retired_at IS NULL AND a.status = 'active'
		  AND c.upstream_model = $2
		LIMIT 1
	`, keyCtx.OrganizationID, target).Scan(&deniedServiceID)
	if err == nil && deniedServiceID != uuid.Nil {
		return resolvedService{ID: deniedServiceID, Name: target}, true, false, nil
	}
	return resolvedService{}, false, false, nil
}

// ListModels 处理 GET /v1/models,返回当前项目授权可用的具体模型与智能路由服务,格式对齐 OpenAI。
func (d Deps) ListModels(w http.ResponseWriter, r *http.Request) {
	keyCtx, ok := keyFromContext(r.Context())
	if !ok {
		apiError(w, http.StatusUnauthorized, "missing_api_key", "Missing API key. Provide Authorization: Bearer <key>.")
		return
	}
	items, err := d.listAuthorizedModels(r.Context(), keyCtx)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal_error", "Failed to query model list.")
		return
	}
	type modelEntry struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}
	data := make([]modelEntry, 0, len(items))
	for _, item := range items {
		data = append(data, modelEntry{
			ID:      item.ID,
			Object:  "model",
			Created: item.Created,
			OwnedBy: item.OwnedBy,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

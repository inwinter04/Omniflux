package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/inwinter04/omniflux/internal/auth"
	"github.com/inwinter04/omniflux/internal/bifrostx"
)

type providerModelResponse struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	InputCostPerMillion  *float64 `json:"input_cost_per_million"`
	OutputCostPerMillion *float64 `json:"output_cost_per_million"`
}

func (d ServerDeps) ProviderModels(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	accountID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "PROVIDER_ID_INVALID", "账号 ID 不合法", nil)
		return
	}
	access, err := d.loadProviderAccess(r.Context(), principal.OrganizationID, accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "PROVIDER_NOT_FOUND", "账号不存在、无权访问或没有凭据", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_MODELS_FAILED", "凭据读取失败", nil)
		return
	}
	modelsURL, headers, err := providerProbeRequest(access.Provider, access.BaseURL, access.APIKey)
	if err != nil {
		adminError(w, http.StatusBadRequest, "PROVIDER_MODELS_UNSUPPORTED", "该账号暂不支持自动获取模型，可手动输入模型标识", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_MODELS_FAILED", "无法创建模型查询请求", nil)
		return
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		adminError(w, http.StatusBadGateway, "PROVIDER_MODELS_UNREACHABLE", "无法连接上游模型列表: "+err.Error(), nil)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		adminError(w, http.StatusBadGateway, "PROVIDER_MODELS_REJECTED", "上游未返回模型列表，请检查凭据、接口地址或改为手动输入", nil)
		return
	}
	var payload struct {
		Data []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
		Models []struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
		} `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8*1024*1024)).Decode(&payload); err != nil {
		adminError(w, http.StatusBadGateway, "PROVIDER_MODELS_INVALID", "模型列表格式无法识别，可手动输入模型标识", nil)
		return
	}
	byID := make(map[string]string, len(payload.Data)+len(payload.Models))
	for _, item := range payload.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = id
		}
		byID[id] = name
	}
	for _, item := range payload.Models {
		id := strings.TrimPrefix(strings.TrimSpace(item.Name), "models/")
		if id == "" {
			continue
		}
		name := strings.TrimSpace(item.DisplayName)
		if name == "" {
			name = id
		}
		byID[id] = name
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	prices := bifrostx.LookupModelPrices(ctx, access.Provider, ids)
	models := make([]providerModelResponse, 0, len(ids))
	for _, id := range ids {
		item := providerModelResponse{ID: id, Name: byID[id]}
		if price := prices[id]; price.Found {
			item.InputCostPerMillion = new(price.Input)
			item.OutputCostPerMillion = new(price.Output)
		}
		models = append(models, item)
	}
	adminOK(w, map[string]any{"models": models, "pricing_source": "models.dev"})
}

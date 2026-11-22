package httpx

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/inwinter04/omniflux/internal/auth"
)

type evaluationCaseInput struct {
	Input     map[string]any `json:"input"`
	MatchType string         `json:"match_type"`
	Expected  string         `json:"expected"`
}

type evaluationSetRequest struct {
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Cases       []evaluationCaseInput `json:"cases"`
}

type evaluationSetResponse struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Cases       []evaluationCaseOutput `json:"cases"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

type evaluationCaseOutput struct {
	ID        string         `json:"id"`
	Input     map[string]any `json:"input"`
	MatchType string         `json:"match_type"`
	Expected  string         `json:"expected"`
}

type evaluatePromptRequest struct {
	VersionID       string   `json:"version_id"`
	EvaluationSetID string   `json:"evaluation_set_id"`
	ModelServiceIDs []string `json:"model_service_ids"`
}

type evaluationRunTarget struct {
	SetID     uuid.UUID
	CaseID    uuid.UUID
	MatchType string
	Expected  string
}

func (d ServerDeps) PromptEvaluationSets(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireViewer(w, principal) != nil {
		return
	}
	assetID, err := parseAssetID(r)
	if err != nil {
		writeAssetError(w, err, "EVALUATION_SET_LIST_FAILED", "查询测试集失败")
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT id, name, description, created_at, updated_at FROM prompt_evaluation_sets
		WHERE organization_id = $1 AND asset_id = $2 ORDER BY created_at DESC
	`, principal.OrganizationID, assetID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "EVALUATION_SET_LIST_FAILED", "查询测试集失败", nil)
		return
	}
	defer rows.Close()
	results := []evaluationSetResponse{}
	sets := map[uuid.UUID]*evaluationSetResponse{}
	ids := []uuid.UUID{}
	for rows.Next() {
		var item evaluationSetResponse
		var id uuid.UUID
		if err := rows.Scan(&id, &item.Name, &item.Description, &item.CreatedAt, &item.UpdatedAt); err != nil {
			adminError(w, http.StatusInternalServerError, "EVALUATION_SET_LIST_FAILED", "读取测试集失败", nil)
			return
		}
		item.ID, item.Cases = id.String(), []evaluationCaseOutput{}
		results = append(results, item)
		sets[id] = &results[len(results)-1]
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		adminOK(w, results)
		return
	}
	caseRows, err := d.Pool.Query(r.Context(), `
		SELECT c.id, c.evaluation_set_id, c.input, c.match_type, c.expected
		FROM prompt_evaluation_cases c
		JOIN prompt_evaluation_sets s ON s.id = c.evaluation_set_id
		WHERE s.organization_id = $1 AND s.asset_id = $2 AND s.id = ANY($3)
		ORDER BY c.position, c.created_at
	`, principal.OrganizationID, assetID, ids)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "EVALUATION_SET_LIST_FAILED", "查询测试用例失败", nil)
		return
	}
	defer caseRows.Close()
	for caseRows.Next() {
		var item evaluationCaseOutput
		var id, setID uuid.UUID
		if err := caseRows.Scan(&id, &setID, &item.Input, &item.MatchType, &item.Expected); err != nil {
			adminError(w, http.StatusInternalServerError, "EVALUATION_SET_LIST_FAILED", "读取测试用例失败", nil)
			return
		}
		item.ID = id.String()
		if set := sets[setID]; set != nil {
			set.Cases = append(set.Cases, item)
		}
	}
	adminOK(w, results)
}

func (d ServerDeps) CreatePromptEvaluationSet(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	assetID, err := parseAssetID(r)
	if err != nil {
		writeAssetError(w, err, "EVALUATION_SET_CREATE_FAILED", "创建测试集失败")
		return
	}
	var body evaluationSetRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	if err := validateEvaluationSet(body); err != nil {
		writeAssetError(w, err, "EVALUATION_SET_CREATE_FAILED", "创建测试集失败")
		return
	}
	setID := uuid.New()
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO prompt_evaluation_sets(id, organization_id, asset_id, name, description, created_by)
			SELECT $1,$2,$3,$4,$5,$6 WHERE EXISTS(SELECT 1 FROM assets WHERE id=$3 AND organization_id=$2)
		`, setID, principal.OrganizationID, assetID, strings.TrimSpace(body.Name), strings.TrimSpace(body.Description), principal.UserID); err != nil {
			return err
		}
		var exists bool
		if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM prompt_evaluation_sets WHERE id=$1)`, setID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return assetError(http.StatusNotFound, "ASSET_NOT_FOUND", "资产不存在或不属于当前组织")
		}
		for index, item := range body.Cases {
			input, _ := json.Marshal(item.Input)
			if _, err := tx.Exec(r.Context(), `
				INSERT INTO prompt_evaluation_cases(evaluation_set_id, input, match_type, expected, position)
				VALUES ($1,$2,$3,$4,$5)
			`, setID, input, item.MatchType, item.Expected, index); err != nil {
				return err
			}
		}
		_, err := tx.Exec(r.Context(), `
			INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, after_state)
			VALUES ($1,$2,'user','evaluation_set.created','evaluation_set',$3,$4)
		`, principal.OrganizationID, principal.UserID, setID.String(), auditJSON(map[string]any{"asset_id": assetID.String(), "name": body.Name, "cases": len(body.Cases)}))
		return err
	})
	if err != nil {
		writeAssetError(w, err, "EVALUATION_SET_CREATE_FAILED", "创建测试集失败")
		return
	}
	adminOK(w, map[string]string{"id": setID.String()})
}

func (d ServerDeps) DeletePromptEvaluationSet(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	assetID, err := parseAssetID(r)
	if err != nil {
		writeAssetError(w, err, "EVALUATION_SET_DELETE_FAILED", "删除测试集失败")
		return
	}
	setID, parseErr := uuid.Parse(chi.URLParam(r, "setID"))
	if parseErr != nil {
		adminError(w, http.StatusBadRequest, "EVALUATION_SET_ID_INVALID", "测试集 ID 不合法", nil)
		return
	}
	tag, err := d.Pool.Exec(r.Context(), `DELETE FROM prompt_evaluation_sets WHERE id=$1 AND organization_id=$2 AND asset_id=$3`, setID, principal.OrganizationID, assetID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "EVALUATION_SET_DELETE_FAILED", "删除测试集失败", nil)
		return
	}
	if tag.RowsAffected() == 0 {
		adminError(w, http.StatusNotFound, "EVALUATION_SET_NOT_FOUND", "测试集不存在", nil)
		return
	}
	adminOK(w, nil)
}

func (d ServerDeps) EvaluatePromptAsset(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	assetID, err := parseAssetID(r)
	if err != nil {
		writeAssetError(w, err, "PROMPT_EVALUATION_FAILED", "批量评估失败")
		return
	}
	var body evaluatePromptRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	setID, parseErr := uuid.Parse(body.EvaluationSetID)
	if parseErr != nil {
		adminError(w, http.StatusBadRequest, "EVALUATION_SET_ID_INVALID", "测试集 ID 不合法", nil)
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT c.id, c.input, c.match_type, c.expected FROM prompt_evaluation_cases c
		JOIN prompt_evaluation_sets s ON s.id = c.evaluation_set_id
		WHERE s.id=$1 AND s.organization_id=$2 AND s.asset_id=$3
		ORDER BY c.position, c.created_at LIMIT 51
	`, setID, principal.OrganizationID, assetID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROMPT_EVALUATION_FAILED", "查询测试用例失败", nil)
		return
	}
	type caseItem struct {
		id        uuid.UUID
		input     map[string]any
		matchType string
		expected  string
	}
	cases := []caseItem{}
	for rows.Next() {
		var item caseItem
		if err := rows.Scan(&item.id, &item.input, &item.matchType, &item.expected); err != nil {
			rows.Close()
			adminError(w, http.StatusInternalServerError, "PROMPT_EVALUATION_FAILED", "读取测试用例失败", nil)
			return
		}
		cases = append(cases, item)
	}
	rows.Close()
	if len(cases) == 0 || len(cases) > 50 {
		adminError(w, http.StatusBadRequest, "EVALUATION_CASE_COUNT_INVALID", "测试集需要包含 1 到 50 个用例", nil)
		return
	}
	type job struct {
		caseIndex  int
		serviceID  uuid.UUID
		runContext promptRunContext
	}
	jobs := []job{}
	for index, item := range cases {
		runContext, err := d.preparePromptRun(r.Context(), principal, assetID, body.VersionID, item.input, body.ModelServiceIDs)
		if err != nil {
			writeAssetError(w, err, "PROMPT_EVALUATION_FAILED", "批量评估失败")
			return
		}
		for _, serviceID := range runContext.serviceIDs {
			jobs = append(jobs, job{caseIndex: index, serviceID: serviceID, runContext: runContext})
		}
	}
	groupID := uuid.New()
	results := make([]promptRunResponse, len(jobs))
	work := make(chan int)
	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range work {
				current := jobs[index]
				item := cases[current.caseIndex]
				target := &evaluationRunTarget{SetID: setID, CaseID: item.id, MatchType: item.matchType, Expected: item.expected}
				results[index] = d.executePromptRun(r.Context(), principal, groupID, "evaluation", assetID, current.runContext, current.serviceID, target)
			}
		}()
	}
	for index := range jobs {
		work <- index
	}
	close(work)
	wg.Wait()
	summary := summarizeEvaluationRuns(results)
	_, _ = d.Pool.Exec(r.Context(), `
		INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, after_state)
		VALUES ($1,$2,'user','prompt_evaluation.executed','asset',$3,$4)
	`, principal.OrganizationID, principal.UserID, assetID.String(), auditJSON(map[string]any{"group_id": groupID.String(), "evaluation_set_id": setID.String(), "runs": len(results), "summary": summary}))
	adminOK(w, map[string]any{"group_id": groupID.String(), "runs": results, "summary": summary})
}

func validateEvaluationSet(body evaluationSetRequest) error {
	if strings.TrimSpace(body.Name) == "" {
		return assetError(http.StatusBadRequest, "EVALUATION_SET_NAME_REQUIRED", "请填写测试集名称")
	}
	if len(body.Cases) == 0 || len(body.Cases) > 50 {
		return assetError(http.StatusBadRequest, "EVALUATION_CASE_COUNT_INVALID", "测试集需要包含 1 到 50 个用例")
	}
	for _, item := range body.Cases {
		if len(item.Expected) == 0 || len(item.Expected) > 10000 {
			return assetError(http.StatusBadRequest, "EVALUATION_EXPECTED_INVALID", "每个用例需要 1 到 10000 字符的期望内容")
		}
		switch item.MatchType {
		case "exact", "contains":
		case "regex":
			if len(item.Expected) > 500 {
				return assetError(http.StatusBadRequest, "EVALUATION_REGEX_TOO_LONG", "正则表达式不能超过 500 字符")
			}
			if _, err := regexp.Compile(item.Expected); err != nil {
				return assetError(http.StatusBadRequest, "EVALUATION_REGEX_INVALID", "正则表达式不正确: "+err.Error())
			}
		default:
			return assetError(http.StatusBadRequest, "EVALUATION_MATCH_TYPE_INVALID", "匹配规则仅支持精确、包含和正则")
		}
	}
	return nil
}

func evaluatePromptOutput(matchType, expected, output string) (bool, float64) {
	output = strings.TrimSpace(output)
	expected = strings.TrimSpace(expected)
	matched := false
	switch matchType {
	case "exact":
		matched = output == expected
	case "contains":
		matched = strings.Contains(output, expected)
	case "regex":
		pattern, err := regexp.Compile(expected)
		matched = err == nil && pattern.MatchString(output)
	}
	if matched {
		return true, 1
	}
	return false, 0
}

func summarizeEvaluationRuns(results []promptRunResponse) map[string]any {
	total := len(results)
	successful, matched := 0, 0
	cost := 0.0
	latency := 0
	for _, item := range results {
		if item.Status == "success" {
			successful++
		}
		if item.Matched != nil && *item.Matched {
			matched++
		}
		cost += item.Cost
		latency += item.TotalMS
	}
	summary := map[string]any{"total": total, "successful": successful, "matched": matched, "total_cost": cost}
	if total > 0 {
		summary["accuracy"] = float64(matched) / float64(total)
		summary["average_latency_ms"] = latency / total
	}
	return summary
}

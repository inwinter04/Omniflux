package httpx

import (
	"encoding/json"
	"errors"
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
	if !ok || d.requirePermission(w, principal, auth.PermEvaluationRead) != nil {
		return
	}
	assetID, err := parseAssetID(r)
	if err != nil {
		writeAssetError(w, err, "EVALUATION_SET_LIST_FAILED", "failed-to-query-evaluation-sets")
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT id, name, description, created_at, updated_at FROM prompt_evaluation_sets
		WHERE organization_id = $1 AND asset_id = $2 ORDER BY created_at DESC
	`, principal.OrganizationID, assetID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "EVALUATION_SET_LIST_FAILED", "failed-to-query-evaluation-sets", nil)
		return
	}
	defer rows.Close()
	results := []evaluationSetResponse{}
	sets := map[uuid.UUID]int{}
	ids := []uuid.UUID{}
	for rows.Next() {
		var item evaluationSetResponse
		var id uuid.UUID
		if err := rows.Scan(&id, &item.Name, &item.Description, &item.CreatedAt, &item.UpdatedAt); err != nil {
			adminError(w, http.StatusInternalServerError, "EVALUATION_SET_LIST_FAILED", "failed-to-read-evaluation-sets", nil)
			return
		}
		item.ID, item.Cases = id.String(), []evaluationCaseOutput{}
		results = append(results, item)
		sets[id] = len(results) - 1
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
		adminError(w, http.StatusInternalServerError, "EVALUATION_SET_LIST_FAILED", "failed-to-query-test-cases", nil)
		return
	}
	defer caseRows.Close()
	for caseRows.Next() {
		var item evaluationCaseOutput
		var id, setID uuid.UUID
		if err := caseRows.Scan(&id, &setID, &item.Input, &item.MatchType, &item.Expected); err != nil {
			adminError(w, http.StatusInternalServerError, "EVALUATION_SET_LIST_FAILED", "failed-to-read-test-cases", nil)
			return
		}
		item.ID = id.String()
		if index, exists := sets[setID]; exists {
			results[index].Cases = append(results[index].Cases, item)
		}
	}
	adminOK(w, results)
}

func (d ServerDeps) CreatePromptEvaluationSet(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermEvaluationManage) != nil {
		return
	}
	assetID, err := parseAssetID(r)
	if err != nil {
		writeAssetError(w, err, "EVALUATION_SET_CREATE_FAILED", "create-evaluation-set-failed")
		return
	}
	var body evaluationSetRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	if err := validateEvaluationSet(body); err != nil {
		writeAssetError(w, err, "EVALUATION_SET_CREATE_FAILED", "create-evaluation-set-failed")
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
			return assetError(http.StatusNotFound, "ASSET_NOT_FOUND", "asset-does-not-exist-or-does")
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
		writeAssetError(w, err, "EVALUATION_SET_CREATE_FAILED", "create-evaluation-set-failed")
		return
	}
	adminOK(w, map[string]string{"id": setID.String()})
}

func (d ServerDeps) DeletePromptEvaluationSet(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermEvaluationManage) != nil {
		return
	}
	assetID, err := parseAssetID(r)
	if err != nil {
		writeAssetError(w, err, "EVALUATION_SET_DELETE_FAILED", "failed-to-delete-the-evaluation-set")
		return
	}
	setID, parseErr := uuid.Parse(chi.URLParam(r, "setID"))
	if parseErr != nil {
		adminError(w, http.StatusBadRequest, "EVALUATION_SET_ID_INVALID", "invalid-evaluation-set-id", nil)
		return
	}
	// 测试集删除后记录已不存在,审计里记下名称与用例数,和创建 / 更新保持同样的可追溯性。
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var name string
		var caseCount int
		if err := tx.QueryRow(r.Context(), `
			SELECT s.name, (SELECT count(*) FROM prompt_evaluation_cases c WHERE c.evaluation_set_id = s.id)
			FROM prompt_evaluation_sets s
			WHERE s.id = $1 AND s.organization_id = $2 AND s.asset_id = $3
			FOR UPDATE
		`, setID, principal.OrganizationID, assetID).Scan(&name, &caseCount); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `DELETE FROM prompt_evaluation_sets WHERE id = $1`, setID); err != nil {
			return err
		}
		return writeAuditEvent(r.Context(), tx, principal.OrganizationID, principal.UserID,
			"evaluation_set.deleted", "evaluation_set", setID.String(),
			map[string]any{"asset_id": assetID.String(), "name": name, "cases": caseCount}, nil)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "EVALUATION_SET_NOT_FOUND", "evaluation-set-does-not-exist", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "EVALUATION_SET_DELETE_FAILED", "failed-to-delete-the-evaluation-set", nil)
		return
	}
	adminOK(w, nil)
}

func (d ServerDeps) EvaluatePromptAsset(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermEvaluationManage) != nil {
		return
	}
	assetID, err := parseAssetID(r)
	if err != nil {
		writeAssetError(w, err, "PROMPT_EVALUATION_FAILED", "evaluation-run-failed")
		return
	}
	var body evaluatePromptRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	setID, parseErr := uuid.Parse(body.EvaluationSetID)
	if parseErr != nil {
		adminError(w, http.StatusBadRequest, "EVALUATION_SET_ID_INVALID", "invalid-evaluation-set-id", nil)
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT c.id, c.input, c.match_type, c.expected FROM prompt_evaluation_cases c
		JOIN prompt_evaluation_sets s ON s.id = c.evaluation_set_id
		WHERE s.id=$1 AND s.organization_id=$2 AND s.asset_id=$3
		ORDER BY c.position, c.created_at LIMIT 51
	`, setID, principal.OrganizationID, assetID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROMPT_EVALUATION_FAILED", "failed-to-query-test-cases", nil)
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
			adminError(w, http.StatusInternalServerError, "PROMPT_EVALUATION_FAILED", "failed-to-read-test-cases", nil)
			return
		}
		cases = append(cases, item)
	}
	rows.Close()
	if len(cases) == 0 || len(cases) > 50 {
		adminError(w, http.StatusBadRequest, "EVALUATION_CASE_COUNT_INVALID", "an-evaluation-set-must-contain-1", nil)
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
			writeAssetError(w, err, "PROMPT_EVALUATION_FAILED", "evaluation-run-failed")
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
		return assetError(http.StatusBadRequest, "EVALUATION_SET_NAME_REQUIRED", "enter-an-evaluation-set-name")
	}
	if len(body.Cases) == 0 || len(body.Cases) > 50 {
		return assetError(http.StatusBadRequest, "EVALUATION_CASE_COUNT_INVALID", "an-evaluation-set-must-contain-1")
	}
	for _, item := range body.Cases {
		if len(item.Expected) == 0 || len(item.Expected) > 10000 {
			return assetError(http.StatusBadRequest, "EVALUATION_EXPECTED_INVALID", "each-case-needs-expected-content-of")
		}
		switch item.MatchType {
		case "exact", "contains":
		case "regex":
			if len(item.Expected) > 500 {
				return assetError(http.StatusBadRequest, "EVALUATION_REGEX_TOO_LONG", "regex-cannot-exceed-500-characters")
			}
			if _, err := regexp.Compile(item.Expected); err != nil {
				return assetError(http.StatusBadRequest, "EVALUATION_REGEX_INVALID", "evaluationRegexInvalid", map[string]any{"detail": err.Error()})
			}
		default:
			return assetError(http.StatusBadRequest, "EVALUATION_MATCH_TYPE_INVALID", "match-rules-support-only-exact-contains")
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

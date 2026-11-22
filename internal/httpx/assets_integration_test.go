package httpx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestIntegrationAssetRegistry(t *testing.T) {
	env := setupIntegration(t)
	admin := sessionClient(t, "it-admin@test.local", "it-admin-password-123456")

	departmentID := createAssetTestDepartment(t, admin)
	allowedProjectID := createAssetTestProject(t, admin, departmentID, "Asset Allowed Project")
	deniedProjectID := createAssetTestProject(t, admin, departmentID, "Asset Denied Project")
	allowedKey := createAssetTestKey(t, admin, allowedProjectID, "Asset Allowed App", "Asset Allowed Key")
	deniedKey := createAssetTestKey(t, admin, deniedProjectID, "Asset Denied App", "Asset Denied Key")

	specV1 := map[string]any{
		"template":  "Hello {{ .name }}",
		"variables": []any{map[string]any{"name": "name", "type": "string", "required": true}},
	}
	code, body := doJSON(t, admin, http.MethodPost, "/api/v1/assets", map[string]any{
		"name": "Browser Greeting", "description": "Asset integration prompt", "spec": specV1,
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	assetID := assetDataMap(t, body)["id"].(string)
	version1ID := assetDataMap(t, body)["version_id"].(string)

	code, body = doJSON(t, admin, http.MethodGet, "/api/v1/assets/"+assetID, nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	draftDetail := assetDataMap(t, body)
	if draftDetail["current_version"] != nil || !draftDetail["has_draft"].(bool) {
		t.Fatalf("draft detail = %#v", draftDetail)
	}
	if draftDetail["versions"].([]any)[0].(map[string]any)["current"].(bool) {
		t.Fatalf("unpublished version marked current: %#v", draftDetail)
	}
	code, body = doJSON(t, admin, http.MethodGet, "/api/v1/assets/"+assetID+"/versions/"+version1ID, nil)
	assertAssetStatus(t, code, body, http.StatusOK)

	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/assets/"+assetID+"/publish", map[string]any{"version_id": version1ID})
	assertAssetStatus(t, code, body, http.StatusOK)

	code, _ = doDataJSON(t, http.MethodGet, "/v1/prompt-templates/Browser Greeting", nil, deniedKey)
	if code != http.StatusNotFound {
		t.Fatalf("ungranted detail status = %d, want 404", code)
	}
	code, _ = doDataJSON(t, http.MethodPost, "/v1/prompt-templates/Browser Greeting/render", map[string]any{
		"variables": map[string]any{"name": "Ada"},
	}, allowedKey)
	if code != http.StatusForbidden {
		t.Fatalf("ungranted render status = %d, want 403", code)
	}

	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/assets/"+assetID+"/grants", map[string]any{
		"subject_type": "project", "subject_id": allowedProjectID, "actions": []string{"read", "invoke"},
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	projectGrantID := assetDataMap(t, body)["id"].(string)

	code, raw := doDataJSON(t, http.MethodGet, "/v1/prompt-templates", nil, allowedKey)
	if code != http.StatusOK || len(raw.([]any)) != 1 {
		t.Fatalf("allowed prompt list status = %d body = %#v", code, raw)
	}
	code, raw = doDataJSON(t, http.MethodGet, "/v1/prompt-templates", nil, deniedKey)
	if code != http.StatusOK || len(raw.([]any)) != 0 {
		t.Fatalf("denied prompt list status = %d body = %#v", code, raw)
	}
	code, raw = doDataJSON(t, http.MethodGet, "/v1/prompt-templates/Browser Greeting", nil, allowedKey)
	if code != http.StatusOK || raw.(map[string]any)["version"].(float64) != 1 {
		t.Fatalf("allowed prompt detail status = %d body = %#v", code, raw)
	}
	code, raw = doDataJSON(t, http.MethodPost, "/v1/prompt-templates/Browser Greeting/render", map[string]any{
		"variables": map[string]any{},
	}, allowedKey)
	if code != http.StatusBadRequest {
		t.Fatalf("invalid variables status = %d body = %#v, want 400", code, raw)
	}
	assertDataRender(t, allowedKey, "Ada", "Hello Ada", 1)

	specV2 := map[string]any{
		"template":  "Welcome {{ .name }}",
		"variables": []any{map[string]any{"name": "name", "type": "string", "required": true}},
	}
	code, body = doJSON(t, admin, http.MethodPatch, "/api/v1/assets/"+assetID, map[string]any{
		"name": "Browser Greeting", "description": "v2 draft", "spec": specV2, "expected_version": 1,
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	version2ID := assetDataMap(t, body)["version_id"].(string)
	assertDataRender(t, allowedKey, "Ada", "Hello Ada", 1)

	code, body = doJSON(t, admin, http.MethodPatch, "/api/v1/assets/"+assetID, map[string]any{
		"name": "Stale write", "description": "", "spec": specV2, "expected_version": 1,
	})
	assertAssetStatus(t, code, body, http.StatusConflict)
	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/assets/"+assetID+"/publish", map[string]any{"version_id": version1ID})
	assertAssetStatus(t, code, body, http.StatusConflict)

	code, body = doJSON(t, admin, http.MethodGet, "/api/v1/assets/"+assetID+"/versions/diff?from="+version1ID+"&to="+version2ID, nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	diff := assetDataMap(t, body)["diff"].(string)
	if !strings.Contains(diff, "Hello") || !strings.Contains(diff, "Welcome") {
		t.Fatalf("diff missing template changes: %s", diff)
	}

	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/assets/"+assetID+"/publish", map[string]any{"version_id": version2ID})
	assertAssetStatus(t, code, body, http.StatusOK)
	assertDataRender(t, allowedKey, "Ada", "Welcome Ada", 2)

	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/assets/"+assetID+"/rollback", map[string]any{
		"version_id": version1ID, "expected_version": 2,
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	version3ID := assetDataMap(t, body)["version_id"].(string)
	if assetDataMap(t, body)["version"].(float64) != 3 {
		t.Fatalf("rollback body = %#v, want v3", body)
	}
	assertDataRender(t, allowedKey, "Ada", "Hello Ada", 3)

	code, body = doJSON(t, admin, http.MethodGet, "/api/v1/assets/"+assetID, nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	detail := assetDataMap(t, body)
	if detail["current_version"].(float64) != 3 || detail["has_draft"].(bool) {
		t.Fatalf("detail after rollback = %#v", detail)
	}
	versions := detail["versions"].([]any)
	if len(versions) != 3 || versions[0].(map[string]any)["source_version_id"].(string) != version1ID {
		t.Fatalf("rollback history = %#v", versions)
	}

	code, body = doJSON(t, admin, http.MethodDelete, "/api/v1/assets/"+assetID+"/grants/"+projectGrantID, nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	code, _ = doDataJSON(t, http.MethodPost, "/v1/prompt-templates/Browser Greeting/render", map[string]any{
		"variables": map[string]any{"name": "Ada"},
	}, allowedKey)
	if code != http.StatusForbidden {
		t.Fatalf("revoked project grant status = %d, want 403", code)
	}

	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/assets/"+assetID+"/grants", map[string]any{
		"subject_type": "department", "subject_id": departmentID, "actions": []string{"invoke"},
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	departmentGrantID := assetDataMap(t, body)["id"].(string)
	assertDataRender(t, allowedKey, "Grace", "Hello Grace", 3)
	code, _ = doDataJSON(t, http.MethodGet, "/v1/prompt-templates/Browser Greeting", nil, allowedKey)
	if code != http.StatusNotFound {
		t.Fatalf("invoke-only grant should not read spec, status = %d", code)
	}

	code, body = doJSON(t, admin, http.MethodPatch, "/api/v1/assets/"+assetID+"/status", map[string]any{"status": "disabled"})
	assertAssetStatus(t, code, body, http.StatusOK)
	code, _ = doDataJSON(t, http.MethodPost, "/v1/prompt-templates/Browser Greeting/render", map[string]any{
		"variables": map[string]any{"name": "Ada"},
	}, allowedKey)
	if code != http.StatusForbidden {
		t.Fatalf("disabled asset render status = %d, want 403", code)
	}
	code, body = doJSON(t, admin, http.MethodPatch, "/api/v1/assets/"+assetID+"/status", map[string]any{"status": "published"})
	assertAssetStatus(t, code, body, http.StatusOK)
	assertDataRender(t, allowedKey, "Lin", "Hello Lin", 3)

	if _, err := env.pool.Exec(t.Context(), `UPDATE asset_versions SET spec = '{}'::jsonb WHERE id = $1`, uuid.MustParse(version1ID)); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("version content mutation error = %v, want immutability trigger", err)
	}
	if _, err := env.pool.Exec(t.Context(), `DELETE FROM asset_versions WHERE id = $1`, uuid.MustParse(version1ID)); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("version deletion error = %v, want immutability trigger", err)
	}

	code, body = doJSON(t, admin, http.MethodGet, "/api/v1/audit-events?page=1&page_size=100", nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	actions := map[string]bool{}
	for _, rawEvent := range assetDataMap(t, body)["records"].([]any) {
		actions[rawEvent.(map[string]any)["action"].(string)] = true
	}
	for _, action := range []string{"asset.created", "asset.draft_created", "asset.published", "asset.rolled_back", "asset_grant.upserted", "asset_grant.revoked", "prompt_template.invoked", "asset.status_changed"} {
		if !actions[action] {
			t.Errorf("missing audit action %s in %#v", action, actions)
		}
	}

	code, body = doJSON(t, admin, http.MethodDelete, "/api/v1/assets/"+assetID+"/grants/"+departmentGrantID, nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	code, body = doJSON(t, admin, http.MethodPatch, "/api/v1/assets/"+assetID+"/status", map[string]any{"status": "archived"})
	assertAssetStatus(t, code, body, http.StatusOK)
	code, body = doJSON(t, admin, http.MethodPatch, "/api/v1/assets/"+assetID, map[string]any{
		"name": "Archived edit", "description": "", "spec": specV2, "expected_version": 3,
	})
	assertAssetStatus(t, code, body, http.StatusConflict)

	code, body = doJSON(t, admin, http.MethodGet, "/api/v1/assets/"+assetID+"/versions/"+version3ID, nil)
	assertAssetStatus(t, code, body, http.StatusOK)
}

func createAssetTestDepartment(t *testing.T, admin *http.Client) string {
	t.Helper()
	code, body := doJSON(t, admin, http.MethodPost, "/api/v1/departments", map[string]any{"name": "Asset Registry Department"})
	assertAssetStatus(t, code, body, http.StatusOK)
	return assetDataMap(t, body)["id"].(string)
}

func createAssetTestProject(t *testing.T, admin *http.Client, departmentID, name string) string {
	t.Helper()
	code, body := doJSON(t, admin, http.MethodPost, "/api/v1/projects", map[string]any{"name": name, "department_id": departmentID})
	assertAssetStatus(t, code, body, http.StatusOK)
	return assetDataMap(t, body)["id"].(string)
}

func createAssetTestKey(t *testing.T, admin *http.Client, projectID, appName, keyName string) string {
	t.Helper()
	code, body := doJSON(t, admin, http.MethodPost, "/api/v1/applications", map[string]any{"name": appName, "project_id": projectID})
	assertAssetStatus(t, code, body, http.StatusOK)
	applicationID := assetDataMap(t, body)["id"].(string)
	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/api-keys", map[string]any{
		"name": keyName, "project_id": projectID, "application_id": applicationID,
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	return assetDataMap(t, body)["key"].(string)
}

func assertDataRender(t *testing.T, key, name, want string, version int) {
	t.Helper()
	code, raw := doDataJSON(t, http.MethodPost, "/v1/prompt-templates/Browser Greeting/render", map[string]any{
		"variables": map[string]any{"name": name},
	}, key)
	if code != http.StatusOK {
		t.Fatalf("render status = %d body = %#v", code, raw)
	}
	data := raw.(map[string]any)
	if data["rendered"] != want || data["version"].(float64) != float64(version) {
		t.Fatalf("render body = %#v, want %q v%d", data, want, version)
	}
}

func doDataJSON(t *testing.T, method, path string, payload any, apiKey string) (int, any) {
	t.Helper()
	var body bytes.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = *bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, itEnv.base()+path, &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var decoded any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("%s %s decode: %v", method, path, err)
	}
	return resp.StatusCode, decoded
}

func assetDataMap(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("response data is not object: %#v", body)
	}
	return data
}

func assertAssetStatus(t *testing.T, code int, body map[string]any, want int) {
	t.Helper()
	if code != want {
		t.Fatalf("status = %d body = %#v, want %d (%s)", code, body, want, fmt.Sprint(body["msg"]))
	}
}

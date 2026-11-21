package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/inwinter04/omniflux/internal/app"
	"github.com/inwinter04/omniflux/internal/auth"
	"github.com/inwinter04/omniflux/internal/bifrostx"
	"github.com/inwinter04/omniflux/internal/bootstrap"
	"github.com/inwinter04/omniflux/internal/config"
	"github.com/inwinter04/omniflux/internal/infra"
)

// 集成测试需要真实 PostgreSQL;设置 OMNIFLUX_TEST_DATABASE_URL 启用,
// 例如 postgres://omniflux:omniflux@localhost:5432/omniflux_test?sslmode=disable。
// 测试会清空并重建该库的 public schema,请勿指向有真实数据的库。
const integrationDBEnv = "OMNIFLUX_TEST_DATABASE_URL"

type integrationEnv struct {
	srv  *httptest.Server
	pool *pgxpool.Pool
}

var itEnv *integrationEnv

func setupIntegration(t *testing.T) *integrationEnv {
	t.Helper()
	if itEnv != nil {
		return itEnv
	}
	dbURL := os.Getenv(integrationDBEnv)
	if dbURL == "" {
		t.Skipf("%s not set; skip integration test", integrationDBEnv)
	}
	ctx := context.Background()
	logger := slog.New(slog.DiscardHandler)

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect test db: %v", err)
	}
	t.Cleanup(pool.Close)

	// 清空并重建 schema,保证测试从干净状态开始。
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatalf("reset schema: %v", err)
	}
	mdb, err := infra.NewMigrationDB(dbURL)
	if err != nil {
		t.Fatalf("migration db: %v", err)
	}
	defer mdb.Close()
	if err := app.Migrate(ctx, logger, mdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	cfg := config.Config{
		Env:               "development",
		DatabaseURL:       dbURL,
		BootstrapEmail:    "it-admin@test.local",
		BootstrapPassword: "it-admin-password-123456",
		MasterKey:         bytes.Repeat([]byte{0x2a}, 32),
		APIKeyPepper:      "integration-test-pepper-0123456789abcdef",
	}
	if err := bootstrap.Seed(ctx, logger, pool, cfg); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	engine, err := bifrostx.NewEngine(ctx, []bifrostx.ProviderSpec{})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	handler := New(ServerDeps{
		Logger: logger, Pool: pool, Engine: engine,
		Auth: auth.NewStore(pool), Env: "development",
		MasterKey: cfg.MasterKey, APIKeyPepper: cfg.APIKeyPepper,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	itEnv = &integrationEnv{srv: srv, pool: pool}
	return itEnv
}

// sessionClient 用邮箱密码登录并返回带会话 Cookie 的客户端。
func sessionClient(t *testing.T, email, password string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}
	code, _ := doJSON(t, client, http.MethodPost, "/api/v1/auth/login",
		map[string]string{"email": email, "password": password})
	if code != http.StatusOK {
		t.Fatalf("login %s status = %d", email, code)
	}
	return client
}

// doJSON 对集成服务器发起 JSON 请求并解析标准响应包。
func doJSON(t *testing.T, client *http.Client, method, path string, payload any) (int, map[string]any) {
	t.Helper()
	var body *bytes.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		body = bytes.NewReader(raw)
	} else {
		body = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, itEnv.base()+path, body)
	if err != nil {
		t.Fatalf("build request %s %s: %v", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	envelope := map[string]any{}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("%s %s decode: %v", method, path, err)
	}
	return resp.StatusCode, envelope
}

func (e *integrationEnv) base() string {
	return e.srv.URL
}

// TestIntegrationGovernance 按顺序覆盖:平台管理员组织治理、跨组织用户归属、
// 数据面认证与预算拦截。子测试共享同一套种子数据,必须保持顺序。
func TestIntegrationGovernance(t *testing.T) {
	env := setupIntegration(t)

	admin := sessionClient(t, "it-admin@test.local", "it-admin-password-123456")

	var acmeOrgID string
	t.Run("platform admin creates and lists organizations", func(t *testing.T) {
		code, body := doJSON(t, admin, http.MethodPost, "/api/v1/organizations",
			map[string]string{"name": "Acme", "slug": "acme"})
		if code != http.StatusOK {
			t.Fatalf("create org status = %d body = %v", code, body)
		}
		code, body = doJSON(t, admin, http.MethodPost, "/api/v1/organizations",
			map[string]string{"name": "Dup", "slug": "acme"})
		if code != http.StatusConflict {
			t.Fatalf("duplicate slug status = %d, want 409", code)
		}
		code, body = doJSON(t, admin, http.MethodGet, "/api/v1/organizations", nil)
		if code != http.StatusOK {
			t.Fatalf("list orgs status = %d", code)
		}
		orgs := body["data"].([]any)
		if len(orgs) != 2 {
			t.Fatalf("orgs count = %d, want 2", len(orgs))
		}
		first := orgs[0].(map[string]any)
		acmeOrgID = body["data"].([]any)[1].(map[string]any)["id"].(string)
		if first["slug"] != "omniflux" {
			t.Fatalf("first org slug = %v, want omniflux", first["slug"])
		}
		if acmeOrgID == "" {
			t.Fatal("acme org id empty")
		}
	})

	var acmeUserID string
	t.Run("platform admin creates user in another organization", func(t *testing.T) {
		code, body := doJSON(t, admin, http.MethodPost, "/api/v1/users", map[string]any{
			"email": "acme-admin@test.local", "password": "acme-user-password-1",
			"role": "org_admin", "organization_id": acmeOrgID,
		})
		if code != http.StatusOK {
			t.Fatalf("create user status = %d body = %v", code, body)
		}
		acmeUserID = body["data"].(map[string]any)["id"].(string)
	})

	var acmeSession *http.Client
	t.Run("org admin sees only own organization", func(t *testing.T) {
		acmeSession = sessionClient(t, "acme-admin@test.local", "acme-user-password-1")
		code, body := doJSON(t, acmeSession, http.MethodGet, "/api/v1/organizations", nil)
		if code != http.StatusOK {
			t.Fatalf("list orgs status = %d", code)
		}
		orgs := body["data"].([]any)
		if len(orgs) != 1 {
			t.Fatalf("org admin should see 1 org, got %d", len(orgs))
		}
		if orgs[0].(map[string]any)["id"].(string) != acmeOrgID {
			t.Fatal("org admin saw wrong organization")
		}
		code, _ = doJSON(t, acmeSession, http.MethodPost, "/api/v1/organizations",
			map[string]string{"name": "Nope", "slug": "nope"})
		if code != http.StatusForbidden {
			t.Fatalf("org admin create org status = %d, want 403", code)
		}
	})

	t.Run("platform admin moves user across organizations", func(t *testing.T) {
		code, body := doJSON(t, admin, http.MethodGet, "/api/v1/users?organization_id="+acmeOrgID, nil)
		if code != http.StatusOK {
			t.Fatalf("list acme users status = %d", code)
		}
		users := body["data"].([]any)
		if len(users) != 1 {
			t.Fatalf("acme users = %d, want 1", len(users))
		}
		code, _ = doJSON(t, admin, http.MethodPatch, "/api/v1/users/"+acmeUserID,
			map[string]string{"organization_id": acmeOrgID})
		if code != http.StatusOK {
			t.Fatalf("move user to own org status = %d", code)
		}
		// 移动到主组织(种子组织)
		var mainOrgID string
		_, body = doJSON(t, admin, http.MethodGet, "/api/v1/organizations", nil)
		for _, raw := range body["data"].([]any) {
			org := raw.(map[string]any)
			if org["slug"] == "omniflux" {
				mainOrgID = org["id"].(string)
			}
		}
		if mainOrgID == "" {
			t.Fatal("main org not found")
		}
		code, _ = doJSON(t, admin, http.MethodPatch, "/api/v1/users/"+acmeUserID,
			map[string]string{"organization_id": mainOrgID})
		if code != http.StatusOK {
			t.Fatalf("move user to main org status = %d", code)
		}
		_, body = doJSON(t, admin, http.MethodGet, "/api/v1/users?organization_id="+acmeOrgID, nil)
		if got := len(body["data"].([]any)); got != 0 {
			t.Fatalf("acme users after move = %d, want 0", got)
		}
		// 会话在移动后仍有效,principal 按新组织生效
		code, _ = doJSON(t, acmeSession, http.MethodGet, "/api/v1/auth/me", nil)
		if code != http.StatusOK {
			t.Fatalf("me after move status = %d", code)
		}
	})

	var plainKey string
	t.Run("data plane authenticates api keys", func(t *testing.T) {
		// 准备 provider 账号 + 模型服务 + API Key
		code, body := doJSON(t, admin, http.MethodPost, "/api/v1/provider-accounts", map[string]any{
			"provider": "openai_compatible", "name": "it-upstream",
			"base_url": "https://upstream.invalid", "api_key": "sk-integration-0123456789abcdef",
		})
		if code != http.StatusOK {
			t.Fatalf("create provider account status = %d body = %v", code, body)
		}
		accountID := body["data"].(map[string]any)["id"].(string)
		code, body = doJSON(t, admin, http.MethodPost, "/api/v1/model-services", map[string]any{
			"name": "it-model",
			"candidates": []any{map[string]any{
				"provider_account_id": accountID, "upstream_model": "gpt-test",
				"priority": 1, "weight": 1,
				"input_cost_per_million": 10.0, "output_cost_per_million": 20.0,
				"quality_score": 0.8,
			}},
		})
		if code != http.StatusOK {
			t.Fatalf("create model service status = %d body = %v", code, body)
		}
		// 部门/项目/应用链路:种子库已有默认部门
		_, body = doJSON(t, admin, http.MethodGet, "/api/v1/departments", nil)
		deptID := body["data"].([]any)[0].(map[string]any)["id"].(string)
		_, body = doJSON(t, admin, http.MethodPost, "/api/v1/projects",
			map[string]string{"name": "it-project", "department_id": deptID})
		projectID := body["data"].(map[string]any)["id"].(string)
		_, body = doJSON(t, admin, http.MethodPost, "/api/v1/applications",
			map[string]string{"name": "it-app", "project_id": projectID})
		applicationID := body["data"].(map[string]any)["id"].(string)
		code, body = doJSON(t, admin, http.MethodPost, "/api/v1/api-keys", map[string]any{
			"name": "it-key", "project_id": projectID, "application_id": applicationID,
			"rate_limit_per_min": 1000,
		})
		if code != http.StatusOK {
			t.Fatalf("create api key status = %d body = %v", code, body)
		}
		plainKey = body["data"].(map[string]any)["key"].(string)

		anonymous := &http.Client{}
		req, _ := http.NewRequest(http.MethodGet, env.base()+"/v1/models", nil)
		resp, err := anonymous.Do(req)
		if err != nil {
			t.Fatalf("models without key: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("models without key status = %d, want 401", resp.StatusCode)
		}

		req, _ = http.NewRequest(http.MethodGet, env.base()+"/v1/models", nil)
		req.Header.Set("Authorization", "Bearer not-a-real-key")
		resp, err = anonymous.Do(req)
		if err != nil {
			t.Fatalf("models with bad key: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("models with bad key status = %d, want 401", resp.StatusCode)
		}

		req, _ = http.NewRequest(http.MethodGet, env.base()+"/v1/models", nil)
		req.Header.Set("Authorization", "Bearer "+plainKey)
		resp, err = anonymous.Do(req)
		if err != nil {
			t.Fatalf("models with valid key: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("models with valid key status = %d, want 200", resp.StatusCode)
		}
		var payload struct {
			Data []map[string]any `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatalf("decode models: %v", err)
		}
		if len(payload.Data) != 1 || payload.Data[0]["id"] != "it-model" {
			t.Fatalf("models payload = %v, want it-model", payload.Data)
		}
	})

	t.Run("budget exhaustion blocks chat", func(t *testing.T) {
		// 组织级预算窗口:已用 = 限额,新请求应被 429 拦截。
		var mainOrgID string
		_, body := doJSON(t, admin, http.MethodGet, "/api/v1/organizations", nil)
		for _, raw := range body["data"].([]any) {
			org := raw.(map[string]any)
			if org["slug"] == "omniflux" {
				mainOrgID = org["id"].(string)
			}
		}
		_, err := itEnv.pool.Exec(context.Background(), `
			INSERT INTO budget_windows(id, organization_id, scope_type, scope_id,
				window_start, window_end, limit_amount, spent_amount, currency)
			VALUES ($1,$2,'organization',$3, now(), now() + interval '24 hours', 100, 100, 'USD')
		`, uuid.New(), mainOrgID, mainOrgID)
		if err != nil {
			t.Fatalf("insert budget window: %v", err)
		}
		req, _ := http.NewRequest(http.MethodPost, env.base()+"/v1/chat/completions",
			bytes.NewBufferString(`{"model":"it-model","messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("Authorization", "Bearer "+plainKey)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("chat request: %v", err)
		}
		defer resp.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatalf("decode chat error: %v", err)
		}
		if resp.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("chat with exhausted budget status = %d body = %v", resp.StatusCode, payload)
		}
		errObj := payload["error"].(map[string]any)
		if errObj["code"] != float64(429) {
			t.Fatalf("error code = %v, want 429", errObj["code"])
		}
		// 用量记录:预算拦截发生在 pending 写入之前,不产生用量。
		var count int
		if err := itEnv.pool.QueryRow(context.Background(),
			`SELECT count(*) FROM usage_records`).Scan(&count); err != nil {
			t.Fatalf("count usage: %v", err)
		}
		if count != 0 {
			t.Fatalf("usage records = %d, want 0 (budget check before pending)", count)
		}
	})
}

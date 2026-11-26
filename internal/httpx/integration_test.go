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
	"github.com/inwinter04/omniflux/internal/governance"
	"github.com/inwinter04/omniflux/internal/infra"
	"github.com/inwinter04/omniflux/internal/mail"
	"github.com/inwinter04/omniflux/internal/vault"
)

// 集成测试需要真实 PostgreSQL;设置 OMNIFLUX_TEST_DATABASE_URL 启用,
// 例如 postgres://omniflux:omniflux@localhost:5432/omniflux_test?sslmode=disable。
// 测试会清空并重建该库的 public schema,请勿指向有真实数据的库。
const integrationDBEnv = "OMNIFLUX_TEST_DATABASE_URL"

type integrationEnv struct {
	mailer *fakeMailer
	srv    *httptest.Server
	pool   *pgxpool.Pool
	engine *bifrostx.Engine
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
	mailer := &fakeMailer{settings: mail.StoredSettings{Settings: mail.Settings{PublicBaseURL: "http://127.0.0.1:1"}}}
	engine, err := bifrostx.NewEngine(ctx, []bifrostx.ProviderSpec{})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	gov := governance.NewManager(pool, cfg.MasterKey, logger, engine)
	reloadEngine := func(ctx context.Context) error {
		proxies, err := gov.ResolveAllActiveProxies(ctx)
		if err != nil {
			return err
		}
		rows, err := pool.Query(ctx, `
			SELECT a.id, a.provider, a.name, a.base_url, c.ciphertext
			FROM provider_accounts a
			JOIN provider_credentials c ON c.provider_account_id = a.id
			WHERE a.status = 'active'
		`)
		if err != nil {
			return err
		}
		defer rows.Close()
		specs := []bifrostx.ProviderSpec{}
		for rows.Next() {
			var id uuid.UUID
			var provider, name string
			var baseURL *string
			var ciphertext []byte
			if err := rows.Scan(&id, &provider, &name, &baseURL, &ciphertext); err != nil {
				return err
			}
			plaintext, err := vault.Decrypt(cfg.MasterKey, ciphertext)
			if err != nil {
				return err
			}
			providerKey, err := bifrostx.MapProvider(provider)
			if err != nil {
				return err
			}
			proxy := proxies[string(providerKey)]
			spec := bifrostx.ProviderSpec{
				Provider:  provider,
				AccountID: id.String(),
				Name:      name,
				APIKey:    string(plaintext),
				ProxyType: proxy.Type,
				ProxyURL:  proxy.URL,
			}
			if baseURL != nil {
				spec.BaseURL = *baseURL
			}
			specs = append(specs, spec)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return engine.Reload(ctx, specs)
	}
	handler := New(ServerDeps{
		Logger: logger, Pool: pool, Engine: engine,
		Auth: auth.NewStore(pool), Mailer: mailer, Env: "development",
		MasterKey: cfg.MasterKey, APIKeyPepper: cfg.APIKeyPepper,
		Governance:   gov,
		ReloadEngine: reloadEngine,
	})
	srv := httptest.NewServer(handler)
	mailer.settings.PublicBaseURL = srv.URL
	t.Cleanup(srv.Close)
	itEnv = &integrationEnv{mailer: mailer, srv: srv, pool: pool, engine: engine}
	t.Cleanup(func() {
		itEnv = nil
	})
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
	code, body := doJSON(t, client, http.MethodPost, "/api/v1/auth/login",
		map[string]string{"email": email, "password": password})
	if code != http.StatusOK {
		t.Fatalf("login %s status = %d body = %v", email, code, body)
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

	t.Run("public registration follows system settings", func(t *testing.T) {
		public := &http.Client{}
		code, body := doJSON(t, public, http.MethodGet, "/api/v1/auth/registration-options", nil)
		if code != http.StatusOK || body["data"].(map[string]any)["enabled"] != false {
			t.Fatal("registration should be disabled by default")
		}
		code, _ = doJSON(t, public, http.MethodPost, "/api/v1/auth/register",
			map[string]string{"email": "registered@test.local", "password": "registered-password-123"})
		if code != http.StatusForbidden {
			t.Fatalf("disabled register status = %d, want 403", code)
		}

		code, body = doJSON(t, admin, http.MethodGet, "/api/v1/system/settings", nil)
		if code != http.StatusOK {
			t.Fatalf("read system settings status = %d body = %v", code, body)
		}
		settings := body["data"].(map[string]any)
		organizationID := settings["registration_organization_id"].(string)
		if organizationID == "" || settings["public_registration_enabled"] != false {
			t.Fatalf("unexpected initial settings = %v", settings)
		}

		code, _ = doJSON(t, admin, http.MethodPatch, "/api/v1/system/settings", map[string]any{
			"public_registration_enabled": true, "registration_organization_id": "", "registration_role": "developer",
		})
		if code != http.StatusBadRequest {
			t.Fatalf("enable without organization status = %d, want 400", code)
		}
		code, body = doJSON(t, admin, http.MethodPatch, "/api/v1/system/settings", map[string]any{
			"public_registration_enabled": false, "registration_organization_id": "", "registration_role": "viewer",
		})
		if code != http.StatusOK || body["data"].(map[string]any)["registration_organization_id"] != nil {
			t.Fatalf("clear registration organization status = %d body = %v", code, body)
		}

		code, body = doJSON(t, admin, http.MethodPatch, "/api/v1/system/settings", map[string]any{
			"public_registration_enabled": true, "registration_organization_id": organizationID, "registration_role": "developer",
		})
		if code != http.StatusOK {
			t.Fatalf("enable registration status = %d body = %v", code, body)
		}
		code, body = doJSON(t, public, http.MethodGet, "/api/v1/auth/registration-options", nil)
		if code != http.StatusOK || body["data"].(map[string]any)["enabled"] != true {
			t.Fatal("registration should be enabled after settings update")
		}

		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatalf("cookie jar: %v", err)
		}
		registered := &http.Client{Jar: jar}
		code, body = doJSON(t, registered, http.MethodPost, "/api/v1/auth/register",
			map[string]string{"email": "registered@test.local", "password": "registered-password-123"})
		if code != http.StatusOK {
			t.Fatalf("register status = %d body = %v", code, body)
		}
		if body["data"].(map[string]any)["user"].(map[string]any)["role"] != "developer" {
			t.Fatalf("registered role = %v, want developer", body["data"])
		}
		code, body = doJSON(t, registered, http.MethodGet, "/api/v1/auth/me", nil)
		if code != http.StatusOK {
			t.Fatalf("registered me status = %d body = %v", code, body)
		}
		code, _ = doJSON(t, registered, http.MethodGet, "/api/v1/system/settings", nil)
		if code != http.StatusForbidden {
			t.Fatalf("registered developer system settings status = %d, want 403", code)
		}

		code, _ = doJSON(t, public, http.MethodPost, "/api/v1/auth/register",
			map[string]string{"email": "registered@test.local", "password": "registered-password-123"})
		if code != http.StatusConflict {
			t.Fatalf("duplicate register status = %d, want 409", code)
		}
		code, body = doJSON(t, admin, http.MethodPatch, "/api/v1/system/settings", map[string]any{
			"public_registration_enabled": false, "registration_organization_id": organizationID, "registration_role": "developer",
		})
		if code != http.StatusOK {
			t.Fatalf("disable registration status = %d", code)
		}
	})

	t.Run("platform admin creates user with generated password", func(t *testing.T) {
		code, _ := doJSON(t, admin, http.MethodPost, "/api/v1/users", map[string]any{
			"email": "generated-admin@test.local", "role": "viewer",
		})
		if code != http.StatusBadRequest {
			t.Fatalf("create without password status = %d, want 400", code)
		}

		payload, err := json.Marshal(map[string]any{
			"email": "generated-admin@test.local", "generate_password": true, "role": "viewer",
		})
		if err != nil {
			t.Fatalf("marshal generated user: %v", err)
		}
		req, err := http.NewRequest(http.MethodPost, itEnv.base()+"/api/v1/users", bytes.NewReader(payload))
		if err != nil {
			t.Fatalf("build generated user request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := admin.Do(req)
		if err != nil {
			t.Fatalf("create generated user: %v", err)
		}
		defer resp.Body.Close()
		envelope := map[string]any{}
		if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode generated user: %v", err)
		}
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("generated user status = %d cache-control = %q", resp.StatusCode, resp.Header.Get("Cache-Control"))
		}
		data := envelope["data"].(map[string]any)
		password := data["generated_password"].(string)
		if len(password) != 16 {
			t.Fatalf("generated password length = %d, want 16", len(password))
		}
		var auditAfter string
		if err := env.pool.QueryRow(context.Background(), `
			SELECT after_state::text FROM audit_events
			WHERE action='user.created' AND resource_id=$1
		`, data["id"].(string)).Scan(&auditAfter); err != nil {
			t.Fatalf("read generated user audit: %v", err)
		}
		if bytes.Contains([]byte(auditAfter), []byte(password)) {
			t.Fatal("generated password must not be written to audit state")
		}
		generatedSession := sessionClient(t, "generated-admin@test.local", password)
		code, _ = doJSON(t, generatedSession, http.MethodGet, "/api/v1/auth/me", nil)
		if code != http.StatusOK {
			t.Fatalf("generated user me status = %d", code)
		}
	})

	t.Run("email verification, invitation and password reset close the loop", func(t *testing.T) {
		env.mailer.settings.Enabled = true
		code, body := doJSON(t, admin, http.MethodPatch, "/api/v1/system/email/settings", map[string]any{
			"enabled": true, "smtp_host": "smtp.example.com", "smtp_port": 587,
			"from_address": "omniflux@example.com", "encryption": "starttls",
			"public_base_url": itEnv.base(),
		})
		if code != http.StatusOK {
			t.Fatalf("configure email settings status = %d body = %v", code, body)
		}

		code, body = doJSON(t, admin, http.MethodGet, "/api/v1/system/settings", nil)
		if code != http.StatusOK {
			t.Fatalf("read system settings for email status = %d", code)
		}
		organizationID := body["data"].(map[string]any)["registration_organization_id"].(string)
		code, _ = doJSON(t, admin, http.MethodPatch, "/api/v1/system/settings", map[string]any{
			"public_registration_enabled": true, "registration_organization_id": organizationID,
			"registration_role": "viewer", "email_verification_required": true,
		})
		if code != http.StatusOK {
			t.Fatalf("enable verified registration status = %d", code)
		}

		public := &http.Client{}
		code, body = doJSON(t, public, http.MethodPost, "/api/v1/auth/register",
			map[string]string{"email": "verified-user@test.local", "password": "verified-password-123"})
		if code != http.StatusOK || body["data"].(map[string]any)["status"] != "verification_email_sent" {
			t.Fatalf("verified registration status = %d body = %v", code, body)
		}
		code, body = doJSON(t, public, http.MethodPost, "/api/v1/auth/register",
			map[string]string{"email": "verified-user@test.local", "password": "re-verified-password-123"})
		if code != http.StatusOK || body["data"].(map[string]any)["status"] != "verification_email_sent" {
			t.Fatalf("re-registration status = %d body = %v", code, body)
		}
		var storedPasswordHash string
		if err := env.pool.QueryRow(context.Background(), `SELECT password_hash FROM users WHERE email=$1`, "verified-user@test.local").Scan(&storedPasswordHash); err != nil {
			t.Fatalf("read re-registered password hash: %v", err)
		}
		if valid, verifyErr := auth.VerifyPassword(storedPasswordHash, "re-verified-password-123"); verifyErr != nil || !valid {
			t.Fatalf("re-registered password does not verify: valid=%v error=%v", valid, verifyErr)
		}
		verificationToken := lastEmailToken(t, env)
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatalf("verification cookie jar: %v", err)
		}
		verified := &http.Client{Jar: jar}
		code, body = doJSON(t, verified, http.MethodPost, "/api/v1/auth/verify-email", map[string]string{"token": verificationToken})
		if code != http.StatusOK {
			t.Fatalf("verify email status = %d body = %v", code, body)
		}

		oldPasswordSession := &http.Client{}
		code, _ = doJSON(t, oldPasswordSession, http.MethodPost, "/api/v1/auth/login",
			map[string]string{"email": "verified-user@test.local", "password": "verified-password-123"})
		if code != http.StatusUnauthorized {
			t.Fatalf("old password after re-registration status = %d, want 401", code)
		}
		var loginStatus string
		var failedLogins int
		if err := env.pool.QueryRow(context.Background(), `SELECT status,failed_login_count FROM users WHERE email=$1`, "verified-user@test.local").Scan(&loginStatus, &failedLogins); err != nil {
			t.Fatalf("read login state: %v", err)
		}
		if loginStatus != "active" || failedLogins >= 5 {
			t.Fatalf("login state status=%s failed=%d", loginStatus, failedLogins)
		}
		code, _ = doJSON(t, public, http.MethodPost, "/api/v1/auth/login",
			map[string]string{"email": "verified-user@test.local", "password": "re-verified-password-123"})
		if code != http.StatusOK {
			t.Fatalf("new password login status = %d", code)
		}
		code, _ = doJSON(t, admin, http.MethodPost, "/api/v1/users/invite", map[string]any{
			"email": "invited-user@test.local", "role": "developer", "organization_id": organizationID,
		})
		if code != http.StatusOK {
			t.Fatalf("invite user status = %d body = %v", code, body)
		}
		invitationToken := lastEmailToken(t, env)
		inviteJar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatalf("invitation cookie jar: %v", err)
		}
		invited := &http.Client{Jar: inviteJar}
		code, body = doJSON(t, invited, http.MethodPost, "/api/v1/auth/accept-invitation", map[string]string{
			"token": invitationToken, "password": "invited-password-123",
		})
		if code != http.StatusOK {
			t.Fatalf("accept invitation status = %d body = %v", code, body)
		}
		code, _ = doJSON(t, invited, http.MethodGet, "/api/v1/auth/me", nil)
		if code != http.StatusOK {
			t.Fatalf("invited me status = %d", code)
		}

		code, _ = doJSON(t, public, http.MethodPost, "/api/v1/auth/forgot-password",
			map[string]string{"email": "invited-user@test.local"})
		if code != http.StatusOK {
			t.Fatalf("forgot password status = %d", code)
		}
		resetToken := lastEmailToken(t, env)
		code, _ = doJSON(t, public, http.MethodPost, "/api/v1/auth/reset-password", map[string]string{
			"token": resetToken, "password": "reset-password-123",
		})
		if code != http.StatusOK {
			t.Fatalf("reset password status = %d", code)
		}
		code, _ = doJSON(t, invited, http.MethodGet, "/api/v1/auth/me", nil)
		if code != http.StatusUnauthorized {
			t.Fatalf("old invited session status = %d, want 401", code)
		}
		code, body = doJSON(t, public, http.MethodPost, "/api/v1/auth/register",
			map[string]string{"email": "invited-user@test.local", "password": "verified-password-123"})
		if code != http.StatusOK || body["data"].(map[string]any)["status"] != "verification_email_sent" {
			t.Fatalf("enumeration-safe duplicate registration status = %d body = %v", code, body)
		}
	})
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

	var pendingUserID string
	var pendingSession *http.Client
	t.Run("password policy allows 8+ alphanumeric and rejects weak passwords", func(t *testing.T) {
		for _, weak := range []string{"abc1234", "12345678", "abcdefgh", " abc12345", "abc12345 "} {
			code, _ := doJSON(t, admin, http.MethodPost, "/api/v1/users", map[string]any{
				"email": "weak-pw@test.local", "password": weak, "role": "pending",
			})
			if code != http.StatusBadRequest {
				t.Fatalf("weak password %q status = %d, want 400", weak, code)
			}
		}
		code, body := doJSON(t, admin, http.MethodPost, "/api/v1/users", map[string]any{
			"email": "policy-pending@test.local", "password": "pass1234", "role": "pending",
		})
		if code != http.StatusOK {
			t.Fatalf("create user with 8-char alphanumeric password status = %d body = %v", code, body)
		}
		pendingUserID = body["data"].(map[string]any)["id"].(string)
		pendingSession = sessionClient(t, "policy-pending@test.local", "pass1234")
	})

	t.Run("pending role is isolated to auth/me and returns 403 on business endpoints", func(t *testing.T) {
		code, body := doJSON(t, pendingSession, http.MethodGet, "/api/v1/auth/me", nil)
		if code != http.StatusOK {
			t.Fatalf("pending me status = %d body = %v", code, body)
		}
		me := body["data"].(map[string]any)
		if me["role"] != "pending" {
			t.Fatalf("pending me role = %v, want pending", me["role"])
		}
		if perms, ok := me["permissions"].([]any); !ok || len(perms) != 0 {
			t.Fatalf("pending permissions = %v, want empty slice", me["permissions"])
		}
		for _, path := range []string{
			"/api/v1/dashboard",
			"/api/v1/provider-catalog",
			"/api/v1/assets",
			"/api/v1/usage/summary",
		} {
			code, _ := doJSON(t, pendingSession, http.MethodGet, path, nil)
			if code != http.StatusForbidden {
				t.Fatalf("pending GET %s status = %d, want 403", path, code)
			}
		}
	})

	t.Run("viewer role respects default read boundary", func(t *testing.T) {
		code, body := doJSON(t, admin, http.MethodPost, "/api/v1/users", map[string]any{
			"email": "viewer-boundary@test.local", "password": "viewer123", "role": "viewer",
		})
		if code != http.StatusOK {
			t.Fatalf("create viewer status = %d body = %v", code, body)
		}
		viewerSession := sessionClient(t, "viewer-boundary@test.local", "viewer123")
		for _, path := range []string{"/api/v1/dashboard", "/api/v1/usage/summary"} {
			code, _ := doJSON(t, viewerSession, http.MethodGet, path, nil)
			if code != http.StatusOK {
				t.Fatalf("viewer GET %s status = %d, want 200", path, code)
			}
		}
		for _, path := range []string{"/api/v1/audit-events", "/api/v1/usage/records/export"} {
			code, _ := doJSON(t, viewerSession, http.MethodGet, path, nil)
			if code != http.StatusForbidden {
				t.Fatalf("viewer GET %s status = %d, want 403", path, code)
			}
		}
	})

	t.Run("custom permission groups, user overrides, and escalation prevention", func(t *testing.T) {
		code, body := doJSON(t, admin, http.MethodPost, "/api/v1/permission-groups", map[string]any{
			"name":        "ops-reader",
			"description": "read dashboard and usage",
			"permissions": []string{"dashboard:read", "usage:read", "prompt:read"},
		})
		if code != http.StatusOK {
			t.Fatalf("create permission group status = %d body = %v", code, body)
		}
		groupID := body["data"].(map[string]any)["id"].(string)

		code, body = doJSON(t, admin, http.MethodGet, "/api/v1/permission-groups", nil)
		if code != http.StatusOK {
			t.Fatalf("list permission groups status = %d body = %v", code, body)
		}
		foundGroup := false
		for _, item := range body["data"].([]any) {
			if item.(map[string]any)["id"] == groupID {
				foundGroup = true
				break
			}
		}
		if !foundGroup {
			t.Fatalf("created permission group %s not found in list", groupID)
		}

		code, _ = doJSON(t, admin, http.MethodPatch, "/api/v1/permission-groups/"+groupID, map[string]any{
			"name":        "ops-reader",
			"description": "updated read group",
			"permissions": []string{"dashboard:read", "usage:read", "prompt:read"},
		})
		if code != http.StatusOK {
			t.Fatalf("update permission group status = %d", code)
		}

		code, _ = doJSON(t, admin, http.MethodPut, "/api/v1/users/"+pendingUserID+"/permissions", map[string]any{
			"permission_group_id": groupID,
			"allow":               []string{"usage:read"},
			"deny":                []string{"usage:read"},
		})
		if code != http.StatusBadRequest {
			t.Fatalf("conflicting allow/deny status = %d, want 400", code)
		}

		code, body = doJSON(t, admin, http.MethodPut, "/api/v1/users/"+pendingUserID+"/permissions", map[string]any{
			"permission_group_id": groupID,
			"allow":               []string{"api_key:read"},
			"deny":                []string{"usage:read"},
		})
		if code != http.StatusOK {
			t.Fatalf("update user permissions status = %d body = %v", code, body)
		}

		code, _ = doJSON(t, admin, http.MethodDelete, "/api/v1/permission-groups/"+groupID, nil)
		if code != http.StatusConflict {
			t.Fatalf("delete in-use permission group status = %d, want 409", code)
		}

		for _, path := range []string{"/api/v1/dashboard", "/api/v1/assets?type=prompt_template", "/api/v1/api-keys"} {
			code, _ := doJSON(t, pendingSession, http.MethodGet, path, nil)
			if code != http.StatusOK {
				t.Fatalf("overridden user GET %s status = %d, want 200", path, code)
			}
		}
		code, _ = doJSON(t, pendingSession, http.MethodGet, "/api/v1/usage/summary", nil)
		if code != http.StatusForbidden {
			t.Fatalf("denied permission GET /api/v1/usage/summary status = %d, want 403", code)
		}

		code, body = doJSON(t, admin, http.MethodPost, "/api/v1/users", map[string]any{
			"email": "limited-admin@test.local", "password": "limited123", "role": "project_admin",
		})
		if code != http.StatusOK {
			t.Fatalf("create limited admin status = %d body = %v", code, body)
		}
		limitedAdminID := body["data"].(map[string]any)["id"].(string)
		code, _ = doJSON(t, admin, http.MethodPut, "/api/v1/users/"+limitedAdminID+"/permissions", map[string]any{
			"role":  "project_admin",
			"allow": []string{"user:read", "user:manage", "permission_group:read", "permission_group:manage"},
			"deny":  []string{},
		})
		if code != http.StatusOK {
			t.Fatalf("grant user management to limited admin status = %d", code)
		}
		limitedSession := sessionClient(t, "limited-admin@test.local", "limited123")
		code, _ = doJSON(t, limitedSession, http.MethodPost, "/api/v1/permission-groups", map[string]any{
			"name":        "escalated-group",
			"permissions": []string{"dashboard:read", "audit:read"},
		})
		if code != http.StatusForbidden {
			t.Fatalf("escalation create group status = %d, want 403", code)
		}
		code, _ = doJSON(t, limitedSession, http.MethodPut, "/api/v1/users/"+pendingUserID+"/permissions", map[string]any{
			"role":  "viewer",
			"allow": []string{"audit:read"},
			"deny":  []string{},
		})
		if code != http.StatusForbidden {
			t.Fatalf("escalation grant audit:read status = %d, want 403", code)
		}

		code, _ = doJSON(t, admin, http.MethodPut, "/api/v1/users/"+pendingUserID+"/permissions", map[string]any{
			"role":  "pending",
			"allow": []string{},
			"deny":  []string{},
		})
		if code != http.StatusOK {
			t.Fatalf("unbind user group status = %d", code)
		}
		code, _ = doJSON(t, admin, http.MethodDelete, "/api/v1/permission-groups/"+groupID, nil)
		if code != http.StatusOK {
			t.Fatalf("delete unbound permission group status = %d, want 200", code)
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
		if resp.Header.Get("X-Request-ID") == "" {
			t.Fatalf("missing X-Request-ID header")
		}
		errObj := payload["error"].(map[string]any)
		if errObj["type"] != "rate_limit_error" || errObj["code"] != "budget_exceeded" || errObj["reason"] != "budget_exceeded" {
			t.Fatalf("error contract = %#v, want budget_exceeded", errObj)
		}
		if _, ok := errObj["request_id"].(string); !ok {
			t.Fatalf("error request_id = %#v, want string", errObj["request_id"])
		}
		if _, ok := errObj["params"].(map[string]any); !ok {
			t.Fatalf("error params = %#v, want object", errObj["params"])
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

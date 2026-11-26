package auth

import "errors"

var ErrPermissionDenied = errors.New("permission denied")

const (
	PermDashboardRead        = "dashboard:read"
	PermOrgRead              = "org:read"
	PermOrgManage            = "org:manage"
	PermProviderRead         = "provider:read"
	PermProviderManage       = "provider:manage"
	PermRoutePolicyRead      = "route_policy:read"
	PermRoutePolicyManage    = "route_policy:manage"
	PermModelServiceRead     = "model_service:read"
	PermModelServiceManage   = "model_service:manage"
	PermAPIKeyRead           = "api_key:read"
	PermAPIKeyManage         = "api_key:manage"
	PermPromptRead           = "prompt:read"
	PermPromptManage         = "prompt:manage"
	PermEvaluationRead       = "evaluation:read"
	PermEvaluationManage     = "evaluation:manage"
	PermAgentRead            = "agent:read"
	PermAgentManage          = "agent:manage"
	PermBudgetRead           = "budget:read"
	PermBudgetManage         = "budget:manage"
	PermUsageRead            = "usage:read"
	PermUsageExport          = "usage:export"
	PermAuditRead            = "audit:read"
	PermUserRead             = "user:read"
	PermUserManage           = "user:manage"
	PermPermissionGroupRead  = "permission_group:read"
	PermPermissionGroupManage = "permission_group:manage"
)

type PermissionModule struct {
	Key         string   `json:"key"`
	Permissions []string `json:"permissions"`
}

type SystemPreset struct {
	Role        string   `json:"role"`
	Permissions []string `json:"permissions"`
}

var permissionModules = []PermissionModule{
	{Key: "dashboard", Permissions: []string{PermDashboardRead}},
	{Key: "organization", Permissions: []string{PermOrgRead, PermOrgManage}},
	{Key: "model", Permissions: []string{
		PermProviderRead, PermProviderManage,
		PermRoutePolicyRead, PermRoutePolicyManage,
		PermModelServiceRead, PermModelServiceManage,
	}},
	{Key: "api_key", Permissions: []string{PermAPIKeyRead, PermAPIKeyManage}},
	{Key: "asset", Permissions: []string{
		PermPromptRead, PermPromptManage,
		PermEvaluationRead, PermEvaluationManage,
		PermAgentRead, PermAgentManage,
	}},
	{Key: "observability", Permissions: []string{
		PermBudgetRead, PermBudgetManage,
		PermUsageRead, PermUsageExport,
		PermAuditRead,
	}},
	{Key: "access", Permissions: []string{
		PermUserRead, PermUserManage,
		PermPermissionGroupRead, PermPermissionGroupManage,
	}},
}

var allPermissionCodes = func() []string {
	codes := make([]string, 0, 26)
	for _, mod := range permissionModules {
		codes = append(codes, mod.Permissions...)
	}
	return codes
}()

var validPermissionSet = func() map[string]bool {
	set := make(map[string]bool, len(allPermissionCodes))
	for _, code := range allPermissionCodes {
		set[code] = true
	}
	return set
}()

var systemPresetOrder = []string{"pending", "viewer", "developer", "project_admin", "org_admin"}

var presetPermissionsMap = map[string][]string{
	"pending": {},
	"viewer": {
		PermDashboardRead,
		PermOrgRead,
		PermProviderRead, PermRoutePolicyRead, PermModelServiceRead,
		PermAPIKeyRead,
		PermPromptRead, PermEvaluationRead, PermAgentRead,
		PermBudgetRead, PermUsageRead,
	},
	"developer": {
		PermDashboardRead,
		PermOrgRead,
		PermProviderRead, PermRoutePolicyRead, PermModelServiceRead,
		PermAPIKeyRead, PermAPIKeyManage,
		PermPromptRead, PermPromptManage,
		PermEvaluationRead, PermEvaluationManage,
		PermAgentRead, PermAgentManage,
		PermUsageRead,
	},
	"project_admin": {
		PermDashboardRead,
		PermOrgRead, PermOrgManage,
		PermProviderRead, PermProviderManage,
		PermRoutePolicyRead, PermRoutePolicyManage,
		PermModelServiceRead, PermModelServiceManage,
		PermAPIKeyRead, PermAPIKeyManage,
		PermPromptRead, PermPromptManage,
		PermEvaluationRead, PermEvaluationManage,
		PermAgentRead, PermAgentManage,
		PermBudgetRead, PermBudgetManage,
		PermUsageRead, PermUsageExport,
	},
	"org_admin": append([]string(nil), allPermissionCodes...),
}

var roleRank = map[string]int{
	"pending":        0,
	"viewer":         1,
	"developer":      2,
	"project_admin":  3,
	"org_admin":      4,
	"org_owner":      5,
	"platform_admin": 6,
}

func IsValidPermission(code string) bool {
	return validPermissionSet[code]
}

func AllPermissionCodes() []string {
	out := make([]string, len(allPermissionCodes))
	copy(out, allPermissionCodes)
	return out
}

func PermissionCatalog() []PermissionModule {
	out := make([]PermissionModule, len(permissionModules))
	for i, mod := range permissionModules {
		perms := make([]string, len(mod.Permissions))
		copy(perms, mod.Permissions)
		out[i] = PermissionModule{Key: mod.Key, Permissions: perms}
	}
	return out
}

func SystemPresets() []SystemPreset {
	out := make([]SystemPreset, len(systemPresetOrder))
	for i, role := range systemPresetOrder {
		perms := PresetPermissions(role)
		out[i] = SystemPreset{Role: role, Permissions: perms}
	}
	return out
}

func PresetPermissions(role string) []string {
	if role == "org_owner" || role == "platform_admin" {
		return AllPermissionCodes()
	}
	source := presetPermissionsMap[role]
	if len(source) == 0 {
		return []string{}
	}
	out := make([]string, len(source))
	copy(out, source)
	return out
}

// NormalizePermissions 过滤非法权限并按目录固定顺序去重返回。
func NormalizePermissions(codes []string) []string {
	set := make(map[string]bool, len(codes))
	for _, code := range codes {
		if validPermissionSet[code] {
			set[code] = true
		}
	}
	out := make([]string, 0, len(set))
	for _, code := range allPermissionCodes {
		if set[code] {
			out = append(out, code)
		}
	}
	return out
}

// ResolveEffectivePermissions 计算最终有效权限:Deny 优先于 Allow。
func ResolveEffectivePermissions(role string, groupPermissions []string, hasGroup bool, allow, deny []string) map[string]bool {
	result := make(map[string]bool, len(allPermissionCodes))
	if role == "platform_admin" || role == "org_owner" {
		for _, code := range allPermissionCodes {
			result[code] = true
		}
		return result
	}
	base := PresetPermissions(role)
	if hasGroup {
		base = groupPermissions
	}
	for _, code := range base {
		if validPermissionSet[code] {
			result[code] = true
		}
	}
	for _, code := range allow {
		if validPermissionSet[code] {
			result[code] = true
		}
	}
	for _, code := range deny {
		delete(result, code)
	}
	return result
}

func RequirePermission(principal Principal, permission string) error {
	if principal.HasPermission(permission) {
		return nil
	}
	return ErrPermissionDenied
}

func RequireRole(principal Principal, minimum string) error {
	got, ok := roleRank[principal.Role]
	if !ok {
		return ErrPermissionDenied
	}
	want, ok := roleRank[minimum]
	if !ok {
		return errors.New("unknown role")
	}
	if got < want {
		return ErrPermissionDenied
	}
	return nil
}

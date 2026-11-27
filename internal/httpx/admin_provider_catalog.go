package httpx

import (
	"net/http"

	"github.com/inwinter04/omniflux/internal/auth"
	"github.com/inwinter04/omniflux/internal/bifrostx"
)

// ProviderCatalog 下发厂商目录:管理台据此渲染厂商选择器,
// 前端不需要维护厂商清单,新增厂商只改服务端目录。
func (d ServerDeps) ProviderCatalog(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermProviderRead) != nil {
		return
	}
	type catalogGroup struct {
		Group   string                        `json:"group"`
		Entries []bifrostx.VendorCatalogEntry `json:"entries"`
	}
	order := []bifrostx.VendorGroup{
		bifrostx.GroupCN, bifrostx.GroupGlobal, bifrostx.GroupAggregator,
		bifrostx.GroupThirdParty, bifrostx.GroupLocal,
	}
	entries := bifrostx.VendorCatalog()
	byGroup := map[string][]bifrostx.VendorCatalogEntry{
		string(bifrostx.GroupGlobal): {
			{
				ID:              "openai",
				Name:            "OpenAI",
				Group:           string(bifrostx.GroupGlobal),
				FamilyID:        "openai",
				VersionKind:     "standard",
				RegionKind:      "global",
				DefaultProtocol: bifrostx.ProtocolOpenAIResponses,
				BaseURL:         "https://api.openai.com",
				APIBaseURL:      "https://api.openai.com/v1",
				WebsiteURL:      "https://platform.openai.com",
				APIKeyURL:       "https://platform.openai.com/api-keys",
				EditableURL:     true,
			},
			{
				ID:              "anthropic",
				Name:            "Anthropic",
				Group:           string(bifrostx.GroupGlobal),
				FamilyID:        "anthropic",
				VersionKind:     "standard",
				RegionKind:      "global",
				DefaultProtocol: bifrostx.ProtocolAnthropicMessages,
				BaseURL:         "https://api.anthropic.com",
				APIBaseURL:      "https://api.anthropic.com/v1",
				WebsiteURL:      "https://console.anthropic.com",
				APIKeyURL:       "https://console.anthropic.com/settings/keys",
				EditableURL:     true,
			},
			{
				ID:              "gemini",
				Name:            "Google Gemini",
				Group:           string(bifrostx.GroupGlobal),
				FamilyID:        "gemini",
				VersionKind:     "standard",
				RegionKind:      "global",
				DefaultProtocol: bifrostx.ProtocolGeminiNative,
				BaseURL:         "https://generativelanguage.googleapis.com/v1beta",
				APIBaseURL:      "https://generativelanguage.googleapis.com/v1beta",
				WebsiteURL:      "https://ai.google.dev",
				APIKeyURL:       "https://aistudio.google.com/apikey",
				EditableURL:     true,
			},
		},
	}
	for _, e := range entries {
		byGroup[e.Group] = append(byGroup[e.Group], e)
	}
	groups := make([]catalogGroup, 0, len(order)+1)
	for _, g := range order {
		list := byGroup[string(g)]
		if len(list) == 0 {
			continue
		}
		groups = append(groups, catalogGroup{Group: string(g), Entries: list})
	}
	// 自定义 OpenAI 兼容:沿用协议级 provider,允许任意 base_url。
	groups = append(groups, catalogGroup{Group: string(bifrostx.GroupCustom), Entries: []bifrostx.VendorCatalogEntry{{
		ID:              "openai_compatible",
		Name:            "OpenAI Compatible",
		Group:           string(bifrostx.GroupCustom),
		FamilyID:        "openai_compatible",
		DefaultProtocol: bifrostx.ProtocolOpenAIChat,
		WebsiteURL:      "",
		APIKeyURL:       "",
		EditableURL:     true,
		VersionKind:     "custom",
		RegionKind:      "custom",
	}}})
	adminOK(w, groups)
}

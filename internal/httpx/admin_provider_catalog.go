package httpx

import (
	"net/http"

	"github.com/inwinter04/omniflux/internal/bifrostx"
)

// ProviderCatalog 下发厂商目录:管理台据此渲染厂商选择器,
// 前端不需要维护厂商清单,新增厂商只改服务端目录。
func (d ServerDeps) ProviderCatalog(w http.ResponseWriter, _ *http.Request) {
	type catalogGroup struct {
		Group   string                       `json:"group"`
		Label   string                       `json:"label"`
		Entries []bifrostx.VendorCatalogEntry `json:"entries"`
	}
	labels := map[string]string{
		string(bifrostx.GroupCN):         "国内官方",
		string(bifrostx.GroupGlobal):     "海外与云",
		string(bifrostx.GroupAggregator): "聚合平台",
		string(bifrostx.GroupThirdParty): "第三方服务",
		string(bifrostx.GroupLocal):      "本地与自部署",
	}
	order := []bifrostx.VendorGroup{
		bifrostx.GroupCN, bifrostx.GroupGlobal, bifrostx.GroupAggregator,
		bifrostx.GroupThirdParty, bifrostx.GroupLocal,
	}
	entries := bifrostx.VendorCatalog()
	byGroup := map[string][]bifrostx.VendorCatalogEntry{}
	for _, e := range entries {
		byGroup[e.Group] = append(byGroup[e.Group], e)
	}
	groups := make([]catalogGroup, 0, len(order)+1)
	for _, g := range order {
		list := byGroup[string(g)]
		if len(list) == 0 {
			continue
		}
		groups = append(groups, catalogGroup{Group: string(g), Label: labels[string(g)], Entries: list})
	}
	// 自定义 OpenAI 兼容:沿用协议级 provider,允许任意 base_url。
	groups = append(groups, catalogGroup{Group: string(bifrostx.GroupCustom), Label: "自定义", Entries: []bifrostx.VendorCatalogEntry{{
		ID:          "openai_compatible",
		Name:        "自定义 OpenAI 兼容",
		Group:       string(bifrostx.GroupCustom),
		WebsiteURL:  "",
		APIKeyURL:   "",
		EditableURL: true,
	}}})
	adminOK(w, groups)
}

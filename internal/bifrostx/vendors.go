package bifrostx

import "strings"

// VendorGroup 厂商分组,用于管理台按分组展示。
type VendorGroup string

const (
	GroupCN         VendorGroup = "cn"          // 国内官方
	GroupGlobal     VendorGroup = "global"      // 海外与云
	GroupAggregator VendorGroup = "aggregator"  // 聚合平台
	GroupThirdParty VendorGroup = "third_party" // 第三方服务
	GroupLocal      VendorGroup = "local"       // 本地与自部署
	GroupCustom     VendorGroup = "custom"      // 自定义 OpenAI 兼容
)

// vendorEntry 是厂商目录的单条预置。
// 换算成 Omniflux 数据面的 OpenAI Chat Completions 口径:BaseURL 为 API 根地址,
// URLPath 为其下的接口路径前缀(拼接 /chat/completions 等得到完整路径)。
type vendorEntry struct {
	ID          string
	Name        string
	Group       VendorGroup
	BaseURL     string
	URLPath     string
	WebsiteURL  string
	APIKeyURL   string
	BalanceKind string
	EditableURL bool
}

// vendorCatalog 厂商预置目录,顺序即管理台展示顺序。
var vendorCatalog = []vendorEntry{
	{ID: "kimi", Name: "Kimi", Group: GroupCN, BaseURL: "https://api.moonshot.cn", URLPath: "/v1", WebsiteURL: "https://platform.kimi.com", APIKeyURL: "https://platform.kimi.com/console/api-keys", BalanceKind: "", EditableURL: false},
	{ID: "kimi-global", Name: "Kimi Global", Group: GroupCN, BaseURL: "https://api.moonshot.ai", URLPath: "/v1", WebsiteURL: "https://platform.kimi.ai", APIKeyURL: "https://platform.kimi.ai/console/api-keys", BalanceKind: "", EditableURL: false},
	{ID: "kimi-for-coding", Name: "Kimi For Coding", Group: GroupCN, BaseURL: "https://api.kimi.com", URLPath: "/coding/v1", WebsiteURL: "https://www.kimi.com/code/", APIKeyURL: "https://www.kimi.com/code/", BalanceKind: "", EditableURL: false},
	{ID: "kimi-for-coding-global", Name: "Kimi For Coding Global", Group: GroupCN, BaseURL: "https://api.kimi.ai", URLPath: "/coding/v1", WebsiteURL: "https://www.kimi.ai/code", APIKeyURL: "https://www.kimi.ai/code", BalanceKind: "", EditableURL: false},
	{ID: "packycode", Name: "PackyCode", Group: GroupThirdParty, BaseURL: "https://www.packyapi.ai", URLPath: "/v1", WebsiteURL: "https://www.packyapi.ai", APIKeyURL: "https://www.packyapi.ai/register", BalanceKind: "", EditableURL: false},
	{ID: "patewayai", Name: "PatewayAI", Group: GroupThirdParty, BaseURL: "https://api.pateway.ai", URLPath: "/v1", WebsiteURL: "https://pateway.ai", APIKeyURL: "https://pateway.ai/?ch=etzpm8&aff=WB6M6F67#/", BalanceKind: "", EditableURL: false},
	{ID: "modelflare", Name: "Modelflare", Group: GroupAggregator, BaseURL: "https://modelflare.dev", URLPath: "/v1", WebsiteURL: "https://modelflare.dev", APIKeyURL: "https://modelflare.dev/sign-up", BalanceKind: "", EditableURL: false},
	{ID: "haoai", Name: "hao.ai", Group: GroupAggregator, BaseURL: "https://api.hao.ai", URLPath: "/v1", WebsiteURL: "https://hao.ai", APIKeyURL: "https://hao.ai/invite/DTBZS3OR", BalanceKind: "", EditableURL: false},
	{ID: "fennoai", Name: "FennoAI", Group: GroupAggregator, BaseURL: "https://api.fenno.ai", URLPath: "", WebsiteURL: "https://api.fenno.ai", APIKeyURL: "https://api.fenno.ai/register?redirect=/purchase?tab=subscription%26group=16&aff=P9MR3D3PLCNL", BalanceKind: "", EditableURL: false},
	{ID: "runapi", Name: "RunAPI", Group: GroupAggregator, BaseURL: "https://runapi.host", URLPath: "/v1", WebsiteURL: "https://runapi.host", APIKeyURL: "https://runapi.host/register?aff=iOKB", BalanceKind: "", EditableURL: false},
	{ID: "openmodel", Name: "OpenModel", Group: GroupAggregator, BaseURL: "https://api.openmodel.ai", URLPath: "/v1", WebsiteURL: "https://www.openmodel.ai", APIKeyURL: "https://www.openmodel.ai", BalanceKind: "", EditableURL: false},
	{ID: "grooroute-pro", Name: "GrooRoute Pro", Group: GroupAggregator, BaseURL: "https://grooroute.pro", URLPath: "/v1", WebsiteURL: "https://grooroute.pro", APIKeyURL: "https://grooroute.pro/sign-up?aff=qqm9", BalanceKind: "", EditableURL: false},
	{ID: "buytoken", Name: "BuyToken", Group: GroupAggregator, BaseURL: "https://api.buytoken.work", URLPath: "/v1", WebsiteURL: "https://buytoken.work", APIKeyURL: "https://buytoken.work/sign-up", BalanceKind: "", EditableURL: false},
	{ID: "vapi", Name: "V-API", Group: GroupAggregator, BaseURL: "https://api.gpt.ge", URLPath: "/v1", WebsiteURL: "https://gpt.ge", APIKeyURL: "https://gpt.ge/signup?aff=URNP", BalanceKind: "", EditableURL: false},
	{ID: "tokenfactory", Name: "TokenFactory", Group: GroupAggregator, BaseURL: "https://api.tokenfactory.cn", URLPath: "/v1", WebsiteURL: "https://tokenfactory.cn", APIKeyURL: "https://console.tokenfactory.cn/login", BalanceKind: "", EditableURL: false},
	{ID: "aigocode", Name: "AIGoCode", Group: GroupThirdParty, BaseURL: "https://api.aigocode.dev", URLPath: "", WebsiteURL: "https://aigocode.net", APIKeyURL: "https://aigocode.net", BalanceKind: "", EditableURL: false},
	{ID: "qiniu", Name: "Qiniu", Group: GroupAggregator, BaseURL: "https://api.qnaigc.com", URLPath: "/bypass/openai/v1", WebsiteURL: "https://s.qiniu.com/nMvAvy", APIKeyURL: "https://s.qiniu.com/nMvAvy", BalanceKind: "", EditableURL: false},
	{ID: "aicoding", Name: "AICoding", Group: GroupThirdParty, BaseURL: "https://api.aicoding.inc", URLPath: "", WebsiteURL: "https://aicoding.inc", APIKeyURL: "https://aicoding.inc", BalanceKind: "", EditableURL: false},
	{ID: "subrouter", Name: "SubRouter", Group: GroupAggregator, BaseURL: "https://subrouter.ai", URLPath: "/v1", WebsiteURL: "https://subrouter.ai", APIKeyURL: "https://subrouter.ai/register?aff=l3ri", BalanceKind: "", EditableURL: false},
	{ID: "ospreyai", Name: "OspreyAI", Group: GroupAggregator, BaseURL: "https://open.ospreyai.cn", URLPath: "/v1", WebsiteURL: "https://open.ospreyai.cn", APIKeyURL: "https://open.ospreyai.cn", BalanceKind: "", EditableURL: false},
	{ID: "shengsuanyun", Name: "Shengsuanyun", Group: GroupAggregator, BaseURL: "https://router.shengsuanyun.com", URLPath: "/api/v1", WebsiteURL: "https://www.shengsuanyun.com/?from=CH_4HHXMRYF", APIKeyURL: "https://www.shengsuanyun.com/?from=CH_4HHXMRYF", BalanceKind: "", EditableURL: false},
	{ID: "api88", Name: "88API", Group: GroupAggregator, BaseURL: "https://api.88api.ai", URLPath: "/v1", WebsiteURL: "https://88api.ai", APIKeyURL: "https://88api.ai/sign-up?aff=HSGY", BalanceKind: "", EditableURL: false},
	{ID: "code9527", Name: "9527CODE", Group: GroupAggregator, BaseURL: "https://9527.codes", URLPath: "/v1", WebsiteURL: "https://9527.codes/register?channel=ccs", APIKeyURL: "https://9527.codes/register?channel=ccs", BalanceKind: "", EditableURL: false},
	{ID: "code0", Name: "Code0", Group: GroupAggregator, BaseURL: "https://code0.ai", URLPath: "/v1", WebsiteURL: "https://code0.ai", APIKeyURL: "https://code0.ai/agent/register/B2XHxGjGmRvqgznY", BalanceKind: "", EditableURL: false},
	{ID: "teamorouter", Name: "TeamoRouter", Group: GroupAggregator, BaseURL: "https://api.teamorouter.com", URLPath: "/v1", WebsiteURL: "https://teamorouter.com", APIKeyURL: "https://teamorouter.com", BalanceKind: "", EditableURL: false},
	{ID: "ppio", Name: "PPIO", Group: GroupAggregator, BaseURL: "https://api.ppio.com", URLPath: "/openai/v1", WebsiteURL: "https://ppio.com", APIKeyURL: "https://ppio.com", BalanceKind: "", EditableURL: false},
	{ID: "claudecn", Name: "ClaudeCN", Group: GroupThirdParty, BaseURL: "https://claudecn.top", URLPath: "/v1", WebsiteURL: "https://claudecn.top", APIKeyURL: "https://claudecn.ai/register?aff=HEL9", BalanceKind: "", EditableURL: false},
	{ID: "volcano-agent-plan", Name: "火山 Agent Plan", Group: GroupCN, BaseURL: "https://ark.cn-beijing.volces.com", URLPath: "/api/plan/v3", WebsiteURL: "https://www.volcengine.com/activity/agentplan", APIKeyURL: "https://www.volcengine.com/activity/agentplan", BalanceKind: "", EditableURL: false},
	{ID: "volcano-coding-plan", Name: "火山 Coding Plan", Group: GroupCN, BaseURL: "https://ark.cn-beijing.volces.com", URLPath: "/api/coding/v3", WebsiteURL: "https://www.volcengine.com/activity/codingplan", APIKeyURL: "https://www.volcengine.com/activity/codingplan", BalanceKind: "", EditableURL: false},
	{ID: "byteplus", Name: "BytePlus", Group: GroupCN, BaseURL: "https://ark.ap-southeast.bytepluses.com", URLPath: "/api/coding/v3", WebsiteURL: "https://www.byteplus.com/en/product/modelark", APIKeyURL: "https://www.byteplus.com/en/product/modelark", BalanceKind: "", EditableURL: false},
	{ID: "volcengine-doubao", Name: "Volcengine Doubao", Group: GroupCN, BaseURL: "https://ark.cn-beijing.volces.com", URLPath: "/api/v3", WebsiteURL: "https://console.volcengine.com/ark/region:ark+cn-beijing/apiKey", APIKeyURL: "https://console.volcengine.com/ark/region:ark+cn-beijing/apiKey", BalanceKind: "", EditableURL: false},
	{ID: "siliconflow", Name: "SiliconFlow", Group: GroupAggregator, BaseURL: "https://api.siliconflow.cn", URLPath: "/v1", WebsiteURL: "https://siliconflow.cn", APIKeyURL: "https://cloud.siliconflow.cn/i/YflgU2Ve", BalanceKind: "siliconflow", EditableURL: false},
	{ID: "siliconflow-en", Name: "SiliconFlow en", Group: GroupAggregator, BaseURL: "https://api.siliconflow.com", URLPath: "/v1", WebsiteURL: "https://siliconflow.com", APIKeyURL: "https://cloud.siliconflow.cn/i/YflgU2Ve", BalanceKind: "siliconflow", EditableURL: false},
	{ID: "a6api", Name: "A6API", Group: GroupAggregator, BaseURL: "https://api.a6api.vip", URLPath: "/v1", WebsiteURL: "https://www.a6api.vip", APIKeyURL: "https://a6api.vip/register?aff=AqNr", BalanceKind: "", EditableURL: false},
	{ID: "compshare", Name: "Compshare", Group: GroupAggregator, BaseURL: "https://api.modelverse.cn", URLPath: "/v1", WebsiteURL: "https://www.compshare.cn", APIKeyURL: "https://www.compshare.cn/coding-plan", BalanceKind: "", EditableURL: false},
	{ID: "compshare-coding", Name: "Compshare Coding Plan", Group: GroupAggregator, BaseURL: "https://cp.compshare.cn", URLPath: "/v1", WebsiteURL: "https://www.compshare.cn", APIKeyURL: "https://www.compshare.cn/coding-plan", BalanceKind: "", EditableURL: false},
	{ID: "ccsub", Name: "CCSub", Group: GroupAggregator, BaseURL: "https://www.ccsub.net", URLPath: "/v1", WebsiteURL: "https://www.ccsub.net", APIKeyURL: "https://www.ccsub.net/register?ref=Y6Z8DXEA", BalanceKind: "", EditableURL: false},
	{ID: "sssaicode", Name: "SSSAiCode", Group: GroupThirdParty, BaseURL: "https://node-hk.sssaicodeapi.com", URLPath: "/api/v1", WebsiteURL: "https://sssaicodeapi.com", APIKeyURL: "https://sssaicodeapi.com/register?ref=DCP0SM", BalanceKind: "", EditableURL: false},
	{ID: "soleapi", Name: "SoleAPI", Group: GroupAggregator, BaseURL: "https://soleapi.com", URLPath: "/v1", WebsiteURL: "https://soleapi.com", APIKeyURL: "https://soleapi.com", BalanceKind: "", EditableURL: false},
	{ID: "micu", Name: "Micu", Group: GroupThirdParty, BaseURL: "https://www.micuapi.ai", URLPath: "/v1", WebsiteURL: "https://www.micuapi.ai", APIKeyURL: "https://www.micuapi.ai/register?aff=aOYQ", BalanceKind: "", EditableURL: false},
	{ID: "rightcode", Name: "RightCode", Group: GroupThirdParty, BaseURL: "https://www.rightapi.ai", URLPath: "/codex/v1", WebsiteURL: "https://www.rightapi.ai", APIKeyURL: "https://www.rightapi.ai/register", BalanceKind: "", EditableURL: false},
	{ID: "cubence", Name: "Cubence", Group: GroupThirdParty, BaseURL: "https://api.cubence.com", URLPath: "/v1", WebsiteURL: "https://cubence.com", APIKeyURL: "https://cubence.com/signup", BalanceKind: "", EditableURL: false},
	{ID: "crazyrouter", Name: "CrazyRouter", Group: GroupThirdParty, BaseURL: "https://cn.crazyrouter.com", URLPath: "/v1", WebsiteURL: "https://www.crazyrouter.com", APIKeyURL: "https://www.crazyrouter.com/register", BalanceKind: "", EditableURL: false},
	{ID: "dmxapi", Name: "DMXAPI", Group: GroupAggregator, BaseURL: "https://www.dmxapi.cn", URLPath: "/v1", WebsiteURL: "https://www.dmxapi.cn", APIKeyURL: "", BalanceKind: "", EditableURL: false},
	{ID: "xycai", Name: "XycAi", Group: GroupAggregator, BaseURL: "https://apicdn.xycai.us", URLPath: "/v1", WebsiteURL: "https://xycai.us", APIKeyURL: "https://xycai.us/register?aff=Uhu9", BalanceKind: "", EditableURL: false},
	{ID: "alltokens", Name: "AllTokens", Group: GroupAggregator, BaseURL: "https://alltokens.cc", URLPath: "/v1", WebsiteURL: "https://alltokens.cc", APIKeyURL: "https://alltokens.cc/register", BalanceKind: "", EditableURL: false},
	{ID: "tuzi", Name: "Tu-zi", Group: GroupAggregator, BaseURL: "https://api.tu-zi.com", URLPath: "/v1", WebsiteURL: "https://api.tu-zi.com", APIKeyURL: "https://api.tu-zi.com/token", BalanceKind: "", EditableURL: false},
	{ID: "amux", Name: "Amux", Group: GroupAggregator, BaseURL: "https://api.amux.ai", URLPath: "/v1", WebsiteURL: "https://amux.ai", APIKeyURL: "https://amux.ai", BalanceKind: "", EditableURL: false},
	{ID: "deepseek", Name: "DeepSeek", Group: GroupCN, BaseURL: "https://api.deepseek.com", URLPath: "", WebsiteURL: "https://platform.deepseek.com", APIKeyURL: "https://platform.deepseek.com/api_keys", BalanceKind: "deepseek", EditableURL: false},
	{ID: "zhipu-glm", Name: "Zhipu GLM", Group: GroupCN, BaseURL: "https://open.bigmodel.cn", URLPath: "/api/paas/v4", WebsiteURL: "https://open.bigmodel.cn", APIKeyURL: "https://www.bigmodel.cn/claude-code", BalanceKind: "zhipu", EditableURL: false},
	{ID: "zhipu-glm-en", Name: "Zhipu GLM en", Group: GroupCN, BaseURL: "https://api.z.ai", URLPath: "/api/paas/v4", WebsiteURL: "https://z.ai", APIKeyURL: "https://z.ai/subscribe", BalanceKind: "zhipu", EditableURL: false},
	{ID: "zhipu-glm-coding", Name: "智谱 GLM Coding Plan", Group: GroupCN, BaseURL: "https://open.bigmodel.cn", URLPath: "/api/coding/paas/v4", WebsiteURL: "https://www.bigmodel.cn/claude-code", APIKeyURL: "https://www.bigmodel.cn/claude-code", BalanceKind: "zhipu_plan", EditableURL: false},
	{ID: "zhipu-glm-en-coding", Name: "Z.ai GLM Coding Plan", Group: GroupCN, BaseURL: "https://api.z.ai", URLPath: "/api/coding/paas/v4", WebsiteURL: "https://z.ai/subscribe", APIKeyURL: "https://z.ai/subscribe", BalanceKind: "zhipu_plan", EditableURL: false},
	{ID: "baidu-qianfan", Name: "Baidu Qianfan", Group: GroupCN, BaseURL: "https://qianfan.baidubce.com", URLPath: "/v2", WebsiteURL: "https://cloud.baidu.com/product/qianfan_modelbuilder", APIKeyURL: "https://console.bce.baidu.com/qianfan/ais/console/applicationConsole/application", BalanceKind: "", EditableURL: false},
	{ID: "baidu-qianfan-coding", Name: "Baidu Qianfan Coding Plan", Group: GroupCN, BaseURL: "https://qianfan.baidubce.com", URLPath: "/v2/coding", WebsiteURL: "https://cloud.baidu.com/product/qianfan_modelbuilder", APIKeyURL: "https://console.bce.baidu.com/qianfan/ais/console/applicationConsole/application", BalanceKind: "", EditableURL: false},
	{ID: "qwenai", Name: "千问AI平台", Group: GroupCN, BaseURL: "https://maas.qianwenaiapi.com", URLPath: "/compatible-mode/v1", WebsiteURL: "https://platform.qianwenai.com/?utm_content=g_20000002971", APIKeyURL: "https://platform.qianwenai.com/home/api-keys?utm_content=g_20000002972", BalanceKind: "", EditableURL: false},
	{ID: "qwenai-token-plan", Name: "千问AI平台 Token Plan", Group: GroupCN, BaseURL: "https://token-plan.maas.qianwenaiapi.com", URLPath: "/compatible-mode/v1", WebsiteURL: "https://platform.qianwenai.com/pricing/token-plan?utm_content=g_20000002977", APIKeyURL: "https://platform.qianwenai.com/home/api-keys?utm_content=g_20000002978", BalanceKind: "", EditableURL: false},
	{ID: "qwencloud", Name: "QwenCloud", Group: GroupCN, BaseURL: "https://ap-southeast-1.maas.qwencloudapi.com", URLPath: "/compatible-mode/v1", WebsiteURL: "https://home.qwencloud.com/?utm_content=g_20000002974", APIKeyURL: "https://home.qwencloud.com/api-keys?utm_content=g_20000002975", BalanceKind: "", EditableURL: false},
	{ID: "qwencloud-coding", Name: "QwenCloud For Coding", Group: GroupCN, BaseURL: "https://coding-intl.dashscope.aliyuncs.com", URLPath: "/v1", WebsiteURL: "https://www.qwencloud.com", APIKeyURL: "https://home.qwencloud.com/api-keys", BalanceKind: "", EditableURL: false},
	{ID: "qwencloud-token-plan", Name: "QwenCloud Token Plan", Group: GroupCN, BaseURL: "https://token-plan.maas.qwencloudapi.com", URLPath: "/compatible-mode/v1", WebsiteURL: "https://www.qwencloud.com/pricing/token-plan?utm_content=g_20000002980", APIKeyURL: "https://home.qwencloud.com/api-keys?utm_content=g_20000002981", BalanceKind: "", EditableURL: false},
	{ID: "tencent-hunyuan", Name: "Tencent Hunyuan", Group: GroupCN, BaseURL: "https://tokenhub.tencentmaas.com", URLPath: "/v1", WebsiteURL: "https://cloud.tencent.com/product/tokenhub", APIKeyURL: "https://console.cloud.tencent.com/tokenhub/apikey", BalanceKind: "", EditableURL: false},
	{ID: "stepfun-api", Name: "StepFun API", Group: GroupCN, BaseURL: "https://api.stepfun.com", URLPath: "/v1", WebsiteURL: "https://platform.stepfun.com", APIKeyURL: "https://platform.stepfun.com/interface-key", BalanceKind: "stepfun", EditableURL: false},
	{ID: "stepfun-api-en", Name: "StepFun API en", Group: GroupCN, BaseURL: "https://api.stepfun.ai", URLPath: "/v1", WebsiteURL: "https://platform.stepfun.ai", APIKeyURL: "https://platform.stepfun.ai/interface-key", BalanceKind: "stepfun", EditableURL: false},
	{ID: "stepfun-plan", Name: "StepFun", Group: GroupCN, BaseURL: "https://api.stepfun.com", URLPath: "/step_plan/v1", WebsiteURL: "https://platform.stepfun.com/step-plan", APIKeyURL: "https://platform.stepfun.com/interface-key", BalanceKind: "", EditableURL: false},
	{ID: "stepfun-plan-en", Name: "StepFun en", Group: GroupCN, BaseURL: "https://api.stepfun.ai", URLPath: "/step_plan/v1", WebsiteURL: "https://platform.stepfun.ai/step-plan", APIKeyURL: "https://platform.stepfun.ai/interface-key", BalanceKind: "", EditableURL: false},
	{ID: "streamlake", Name: "StreamLake", Group: GroupCN, BaseURL: "https://wanqing.streamlakeapi.com", URLPath: "/api/gateway/v1/endpoints", WebsiteURL: "https://www.streamlake.com", APIKeyURL: "https://console.streamlake.com/console/wanqing/api-key", BalanceKind: "", EditableURL: false},
	{ID: "streamlake-en", Name: "StreamLake en", Group: GroupCN, BaseURL: "https://vanchin.streamlake.ai", URLPath: "/api/gateway/v1/endpoints", WebsiteURL: "https://console.streamlake.ai", APIKeyURL: "https://console.streamlake.ai/console/api-key", BalanceKind: "", EditableURL: false},
	{ID: "longcat", Name: "Longcat", Group: GroupCN, BaseURL: "https://api.longcat.chat", URLPath: "/openai/v1", WebsiteURL: "https://longcat.chat/platform", APIKeyURL: "https://longcat.chat/platform/api_keys", BalanceKind: "", EditableURL: false},
	{ID: "minimax", Name: "MiniMax", Group: GroupCN, BaseURL: "https://api.minimax.cn", URLPath: "/v1", WebsiteURL: "https://platform.minimax.cn", APIKeyURL: "https://platform.minimax.cn/subscribe/token-plan", BalanceKind: "", EditableURL: false},
	{ID: "minimax-en", Name: "MiniMax en", Group: GroupCN, BaseURL: "https://api.minimax.io", URLPath: "/v1", WebsiteURL: "https://platform.minimax.io", APIKeyURL: "https://platform.minimax.io/subscribe/coding-plan", BalanceKind: "", EditableURL: false},
	{ID: "astron-coding-plan", Name: "Astron Coding Plan", Group: GroupCN, BaseURL: "https://maas-coding-api.cn-huabei-1.xf-yun.com", URLPath: "/v1", WebsiteURL: "https://maas.xfyun.cn/packageSubscription", APIKeyURL: "https://maas.xfyun.cn/packageSubscription", BalanceKind: "", EditableURL: false},
	{ID: "bailing", Name: "BaiLing", Group: GroupCN, BaseURL: "https://api.ant-ling.com", URLPath: "/v1", WebsiteURL: "https://developer.ant-ling.com/zh-CN/docs/", APIKeyURL: "https://chat.ant-ling.com/open", BalanceKind: "", EditableURL: false},
	{ID: "xiaomi-mimo", Name: "Xiaomi MiMo", Group: GroupCN, BaseURL: "https://api.xiaomimimo.com", URLPath: "/v1", WebsiteURL: "https://platform.xiaomimimo.com", APIKeyURL: "https://platform.xiaomimimo.com/#/console/api-keys", BalanceKind: "", EditableURL: false},
	{ID: "xiaomi-mimo-token-plan", Name: "Xiaomi MiMo Token Plan (China)", Group: GroupCN, BaseURL: "https://token-plan-cn.xiaomimimo.com", URLPath: "/v1", WebsiteURL: "https://platform.xiaomimimo.com/#/token-plan", APIKeyURL: "https://platform.xiaomimimo.com/#/console/plan-manage", BalanceKind: "", EditableURL: false},
	{ID: "baseten", Name: "Baseten", Group: GroupAggregator, BaseURL: "https://inference.baseten.co", URLPath: "/v1", WebsiteURL: "https://www.baseten.co", APIKeyURL: "https://app.baseten.co/settings/api_keys", BalanceKind: "", EditableURL: false},
	{ID: "novita", Name: "Novita AI", Group: GroupAggregator, BaseURL: "https://api.novita.ai", URLPath: "/openai/v1", WebsiteURL: "https://novita.ai", APIKeyURL: "https://novita.ai", BalanceKind: "novita", EditableURL: false},
	{ID: "xai", Name: "xAI (Grok)", Group: GroupCN, BaseURL: "https://api.x.ai", URLPath: "/v1", WebsiteURL: "https://x.ai/api", APIKeyURL: "https://console.x.ai", BalanceKind: "", EditableURL: false},
	{ID: "nvidia", Name: "Nvidia", Group: GroupAggregator, BaseURL: "https://integrate.api.nvidia.com", URLPath: "/v1", WebsiteURL: "https://build.nvidia.com", APIKeyURL: "https://build.nvidia.com/settings/api-keys", BalanceKind: "", EditableURL: false},
	{ID: "ollama-cloud", Name: "Ollama Cloud", Group: GroupGlobal, BaseURL: "https://ollama.com", URLPath: "/v1", WebsiteURL: "https://ollama.com", APIKeyURL: "https://ollama.com/settings/keys", BalanceKind: "", EditableURL: false},
	{ID: "ollama", Name: "Ollama", Group: GroupLocal, BaseURL: "http://localhost:11434", URLPath: "/v1", WebsiteURL: "https://ollama.com", APIKeyURL: "", BalanceKind: "", EditableURL: true},
	{ID: "lm-studio", Name: "LM Studio", Group: GroupLocal, BaseURL: "http://localhost:1234", URLPath: "/v1", WebsiteURL: "https://lmstudio.ai", APIKeyURL: "", BalanceKind: "", EditableURL: true},
	{ID: "llama-cpp", Name: "llama.cpp", Group: GroupLocal, BaseURL: "http://127.0.0.1:9931", URLPath: "/v1", WebsiteURL: "https://github.com/ggml-org/llama.cpp", APIKeyURL: "", BalanceKind: "", EditableURL: true},
	{ID: "vllm", Name: "vLLM", Group: GroupLocal, BaseURL: "http://localhost:8000", URLPath: "/v1", WebsiteURL: "https://vllm.ai", APIKeyURL: "", BalanceKind: "", EditableURL: true},
	{ID: "deepinfra", Name: "DeepInfra", Group: GroupAggregator, BaseURL: "https://api.deepinfra.com", URLPath: "/v1", WebsiteURL: "https://deepinfra.com", APIKeyURL: "https://deepinfra.com/dash/api_keys", BalanceKind: "", EditableURL: false},
	{ID: "nebius", Name: "Nebius Token Factory", Group: GroupAggregator, BaseURL: "https://api.tokenfactory.nebius.com", URLPath: "/v1", WebsiteURL: "https://nebius.com/token-factory", APIKeyURL: "https://tokenfactory.nebius.com/project/api-keys", BalanceKind: "", EditableURL: false},
	{ID: "groq", Name: "Groq", Group: GroupAggregator, BaseURL: "https://api.groq.com", URLPath: "/openai/v1", WebsiteURL: "https://groq.com", APIKeyURL: "https://console.groq.com/keys", BalanceKind: "", EditableURL: false},
	{ID: "sambanova", Name: "SambaNova", Group: GroupGlobal, BaseURL: "https://api.sambanova.ai", URLPath: "/v1", WebsiteURL: "https://sambanova.ai", APIKeyURL: "https://cloud.sambanova.ai/apis", BalanceKind: "", EditableURL: false},
	{ID: "digitalocean", Name: "DigitalOcean", Group: GroupGlobal, BaseURL: "https://inference.do-ai.run", URLPath: "/v1", WebsiteURL: "https://www.digitalocean.com/products/ai-platform", APIKeyURL: "https://cloud.digitalocean.com/model-studio/manage-keys", BalanceKind: "", EditableURL: false},
	{ID: "huggingface", Name: "Hugging Face", Group: GroupAggregator, BaseURL: "https://router.huggingface.co", URLPath: "/v1", WebsiteURL: "https://huggingface.co/inference/models", APIKeyURL: "https://huggingface.co/settings/tokens/new?ownUserPermissions=inference.serverless.write&tokenType=fineGrained", BalanceKind: "", EditableURL: false},
	{ID: "fireworks", Name: "Fireworks AI", Group: GroupGlobal, BaseURL: "https://api.fireworks.ai", URLPath: "/inference/v1", WebsiteURL: "https://fireworks.ai", APIKeyURL: "https://app.fireworks.ai/settings/users/api-keys", BalanceKind: "", EditableURL: false},
	{ID: "mistral", Name: "Mistral AI", Group: GroupCN, BaseURL: "https://api.mistral.ai", URLPath: "/v1", WebsiteURL: "https://mistral.ai", APIKeyURL: "https://chat.mistral.ai/code/extensions", BalanceKind: "", EditableURL: false},
	{ID: "cloudflare-ai-gateway", Name: "Cloudflare AI Gateway", Group: GroupGlobal, BaseURL: "https://gateway.ai.cloudflare.com", URLPath: "/v1/YOUR_ACCOUNT_ID/default/openai", WebsiteURL: "https://developers.cloudflare.com/ai-gateway/", APIKeyURL: "https://dash.cloudflare.com/profile/api-tokens", BalanceKind: "", EditableURL: true},
	{ID: "aihubmix", Name: "AiHubMix", Group: GroupAggregator, BaseURL: "https://aihubmix.com", URLPath: "/v1", WebsiteURL: "https://aihubmix.com", APIKeyURL: "", BalanceKind: "", EditableURL: false},
	{ID: "genesis", Name: "Genesis", Group: GroupAggregator, BaseURL: "https://token.moi.matrixorigin.cn", URLPath: "/v1", WebsiteURL: "https://genesis.moi.matrixorigin.cn/taas/", APIKeyURL: "https://uc.moi.matrixorigin.cn/account/credentials?app=genesis", BalanceKind: "", EditableURL: false},
	{ID: "cherryin", Name: "CherryIN", Group: GroupAggregator, BaseURL: "https://open.cherryin.net", URLPath: "/v1", WebsiteURL: "https://open.cherryin.ai", APIKeyURL: "https://open.cherryin.ai/console/token", BalanceKind: "", EditableURL: false},
	{ID: "relaxycode", Name: "RelaxyCode", Group: GroupThirdParty, BaseURL: "https://www.relaxycode.com", URLPath: "/v1", WebsiteURL: "https://www.relaxycode.com", APIKeyURL: "https://www.relaxycode.com/register", BalanceKind: "", EditableURL: false},
	{ID: "openrouter", Name: "OpenRouter", Group: GroupAggregator, BaseURL: "https://openrouter.ai", URLPath: "/api/v1", WebsiteURL: "https://openrouter.ai", APIKeyURL: "https://openrouter.ai/keys", BalanceKind: "openrouter", EditableURL: false},
	{ID: "therouter", Name: "TheRouter", Group: GroupAggregator, BaseURL: "https://api.therouter.ai", URLPath: "/v1", WebsiteURL: "https://therouter.ai", APIKeyURL: "https://dashboard.therouter.ai", BalanceKind: "", EditableURL: false},
	{ID: "jiekou", Name: "JieKou AI", Group: GroupAggregator, BaseURL: "https://api.jiekou.ai", URLPath: "/openai/v1", WebsiteURL: "https://jiekou.ai/#model-library", APIKeyURL: "https://jiekou.ai/settings/key-management", BalanceKind: "", EditableURL: false},
	{ID: "aicodewith", Name: "AICodeWith", Group: GroupAggregator, BaseURL: "https://api.aicodewith.ai", URLPath: "/v1", WebsiteURL: "https://aicodewith.ai", APIKeyURL: "https://aicodewith.ai/login?tab=register", BalanceKind: "", EditableURL: false},
	{ID: "commandcode", Name: "Command Code", Group: GroupThirdParty, BaseURL: "https://api.commandcode.ai", URLPath: "/provider/v1", WebsiteURL: "https://commandcode.ai", APIKeyURL: "https://commandcode.ai/settings/keys", BalanceKind: "", EditableURL: false},
	{ID: "moark", Name: "模力方舟", Group: GroupAggregator, BaseURL: "https://moark.com", URLPath: "/v1", WebsiteURL: "https://moark.com", APIKeyURL: "https://moark.com/dashboard/tokens", BalanceKind: "", EditableURL: false},
	{ID: "soruxgpt", Name: "SoruxGPT", Group: GroupAggregator, BaseURL: "https://ai.soruxgpt.com", URLPath: "/v1", WebsiteURL: "https://ai.soruxgpt.com", APIKeyURL: "https://ai.soruxgpt.com/console/token", BalanceKind: "", EditableURL: false},
	{ID: "ikuncode", Name: "IKunCode", Group: GroupAggregator, BaseURL: "https://api.ikuncode.ai", URLPath: "/v1", WebsiteURL: "https://api.ikuncode.ai", APIKeyURL: "https://api.ikuncode.ai/console/token", BalanceKind: "", EditableURL: false},
	{ID: "runtoken", Name: "RunToken", Group: GroupAggregator, BaseURL: "https://www.runtoken.ai", URLPath: "/v1", WebsiteURL: "https://www.runtoken.ai", APIKeyURL: "https://www.runtoken.ai/console/token", BalanceKind: "", EditableURL: false},
	{ID: "botcf", Name: "BotCF", Group: GroupAggregator, BaseURL: "https://botcf.com", URLPath: "/v1", WebsiteURL: "https://botcf.com", APIKeyURL: "https://botcf.com/console/token", BalanceKind: "", EditableURL: false},
	{ID: "yylx", Name: "YYLX", Group: GroupAggregator, BaseURL: "https://app.yylx.io", URLPath: "/v1", WebsiteURL: "https://yylx.io", APIKeyURL: "https://app.yylx.io/keys", BalanceKind: "", EditableURL: false},
	{ID: "nekocode", Name: "NekoCode", Group: GroupAggregator, BaseURL: "https://nekoapi.ai", URLPath: "/v1", WebsiteURL: "https://nekoapi.ai", APIKeyURL: "https://nekoapi.ai/token", BalanceKind: "", EditableURL: false},
	{ID: "aicoming", Name: "AIComing", Group: GroupAggregator, BaseURL: "https://api.aicoming.top", URLPath: "/v1", WebsiteURL: "https://aicoming.top", APIKeyURL: "https://aicoming.top/console#keys", BalanceKind: "", EditableURL: false},
	{ID: "alltokens-si", Name: "AllTokens SI", Group: GroupAggregator, BaseURL: "https://alltokens.si", URLPath: "/v1", WebsiteURL: "https://alltokens.si", APIKeyURL: "https://alltokens.si/enterprise-register", BalanceKind: "", EditableURL: false},
	{ID: "modelscope", Name: "ModelScope", Group: GroupAggregator, BaseURL: "https://api-inference.modelscope.cn", URLPath: "/v1", WebsiteURL: "https://modelscope.cn", APIKeyURL: "https://modelscope.cn/my/myaccesstoken", BalanceKind: "", EditableURL: false},
	{ID: "tokenflux", Name: "TokenFlux", Group: GroupAggregator, BaseURL: "https://tokenflux.com", URLPath: "/v1", WebsiteURL: "https://tokenflux.com", APIKeyURL: "https://tokenflux.com/keys", BalanceKind: "", EditableURL: false},
}

// VendorByID 按 ID 查找厂商目录条目;不存在时返回 nil。
func VendorByID(id string) *vendorEntry {
	for i := range vendorCatalog {
		if vendorCatalog[i].ID == id {
			return &vendorCatalog[i]
		}
	}
	return nil
}

// VendorCatalogEntry 是下发给管理台的目录条目;不含密钥等敏感信息。
type VendorCatalogEntry struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Group        string `json:"group"`
	FamilyID     string `json:"family_id"`
	FamilyName   string `json:"family_name"`
	VersionLabel string `json:"version_label"`
	BaseURL      string `json:"base_url"`
	APIBaseURL   string `json:"api_base_url"`
	WebsiteURL   string `json:"website_url"`
	APIKeyURL    string `json:"api_key_url"`
	BalanceKind  string `json:"balance_kind"`
	EditableURL  bool   `json:"editable_url"`
}

// VendorCatalog 返回完整厂商目录。
func VendorCatalog() []VendorCatalogEntry {
	list := make([]VendorCatalogEntry, 0, len(vendorCatalog))
	for _, v := range vendorCatalog {
		familyID, familyName, versionLabel := vendorPresentation(v)
		list = append(list, VendorCatalogEntry{
			ID:           v.ID,
			Name:         v.Name,
			Group:        string(v.Group),
			FamilyID:     familyID,
			FamilyName:   familyName,
			VersionLabel: versionLabel,
			BaseURL:      v.BaseURL,
			APIBaseURL:   strings.TrimSuffix(v.BaseURL, "/") + v.URLPath,
			WebsiteURL:   v.WebsiteURL,
			APIKeyURL:    v.APIKeyURL,
			BalanceKind:  v.BalanceKind,
			EditableURL:  v.EditableURL,
		})
	}
	return list
}

func vendorPresentation(v vendorEntry) (string, string, string) {
	families := []struct {
		prefixes []string
		id       string
		name     string
	}{
		{[]string{"kimi"}, "kimi", "Kimi"},
		{[]string{"zhipu"}, "zhipu", "智谱 GLM"},
		{[]string{"volcano", "volcengine"}, "volcengine", "火山引擎"},
		{[]string{"qwencloud"}, "qwencloud", "千问云"},
		{[]string{"qwenai"}, "qwenai", "千问 AI 平台"},
		{[]string{"stepfun"}, "stepfun", "阶跃星辰"},
		{[]string{"streamlake"}, "streamlake", "StreamLake"},
		{[]string{"minimax"}, "minimax", "MiniMax"},
		{[]string{"siliconflow"}, "siliconflow", "硅基流动"},
		{[]string{"baidu-qianfan"}, "baidu-qianfan", "百度千帆"},
		{[]string{"xiaomi-mimo"}, "xiaomi-mimo", "小米 MiMo"},
		{[]string{"compshare"}, "compshare", "Compshare"},
	}
	familyID, familyName := v.ID, v.Name
	matched := false
	for _, family := range families {
		for _, prefix := range family.prefixes {
			if strings.HasPrefix(v.ID, prefix) {
				familyID, familyName, matched = family.id, family.name, true
				break
			}
		}
		if matched {
			break
		}
	}
	if !matched {
		return familyID, familyName, "标准服务"
	}
	plan := "标准 API"
	switch {
	case strings.Contains(v.ID, "coding"):
		plan = "编程套餐"
	case strings.Contains(v.ID, "agent-plan"):
		plan = "智能体套餐"
	case strings.Contains(v.ID, "token-plan"):
		plan = "令牌套餐"
	case strings.Contains(v.ID, "plan"):
		plan = "订阅套餐"
	}
	region := "国内站"
	globalIDs := map[string]bool{
		"kimi-global": true, "kimi-for-coding-global": true,
		"zhipu-glm-en": true, "zhipu-glm-en-coding": true,
		"qwencloud": true, "qwencloud-coding": true, "qwencloud-token-plan": true,
		"stepfun-api-en": true, "stepfun-plan-en": true,
		"streamlake-en": true, "minimax-en": true, "siliconflow-en": true,
	}
	if globalIDs[v.ID] {
		region = "国际站"
	}
	return familyID, familyName, plan + " · " + region
}

// vendorChatPath 返回厂商的 Chat Completions 完整路径。
func (v *vendorEntry) ChatPath() string {
	return strings.TrimSuffix(v.URLPath, "/") + "/chat/completions"
}

// vendorModelsPath 返回厂商的模型列表路径。
func (v *vendorEntry) ModelsPath() string {
	return strings.TrimSuffix(v.URLPath, "/") + "/models"
}

// vendorEmbeddingsPath 返回厂商的 Embeddings 路径。
func (v *vendorEntry) EmbeddingsPath() string {
	return strings.TrimSuffix(v.URLPath, "/") + "/embeddings"
}

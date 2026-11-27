package bifrostx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const modelsDevURL = "https://models.dev/api.json"

type ModelPrice struct {
	Input  float64
	Output float64
	Found  bool
}

type modelsDevModel struct {
	Cost *struct {
		Input  *float64 `json:"input"`
		Output *float64 `json:"output"`
	} `json:"cost"`
}

type modelsDevProvider struct {
	Models map[string]modelsDevModel `json:"models"`
}

// planPriceProviders 是按套餐计费的厂商。套餐内调用没有按 token 的边际成本。
// byteplus 目录当前指向官方 ModelArk Coding Plan 的 OpenAI 兼容端点。
var planPriceProviders = map[string]struct{}{
	"byteplus":               {},
	"kimi-for-coding":        {},
	"kimi-for-coding-global": {},
	"zhipu-glm-coding":       {},
	"zhipu-glm-en-coding":    {},
	"baidu-qianfan-coding":   {},
	"qwenai-token-plan":      {},
	"qwencloud-coding":       {},
	"qwencloud-token-plan":   {},
	"stepfun-plan":           {},
	"stepfun-plan-en":        {},
	"volcano-agent-plan":     {},
	"volcano-coding-plan":    {},
	"compshare-coding":       {},
	"astron-coding-plan":     {},
	"xiaomi-mimo-token-plan": {},
}

// IsPlanPriceProvider 判断模型发现是否来自套餐计费端点。
func IsPlanPriceProvider(provider string) bool {
	_, ok := planPriceProviders[provider]
	return ok
}

var modelPriceCache struct {
	sync.Mutex
	loadedAt time.Time
	values   map[string]map[string]ModelPrice
}

// modelsDevProviderIDs 只决定价格目录查询来源；上游连接端点以厂商目录/账号配置为准。
// 目录 ID 不同但服务端点一致的，也可以作为价格参考。
var modelsDevProviderIDs = map[string]string{
	"openai": "openai", "anthropic": "anthropic", "gemini": "google",
	"deepseek": "deepseek", "xai": "xai", "mistral": "mistral",
	"kimi": "moonshotai-cn", "kimi-global": "moonshotai",
	"zhipu-glm": "zhipuai", "zhipu-glm-en": "zai",
	"minimax": "minimax-cn", "minimax-en": "minimax",
	"qwencloud": "alibaba", "openrouter": "openrouter",
	"bailing": "bailing", "baseten": "baseten",
	"deepinfra": "deepinfra", "digitalocean": "digitalocean",
	"fireworks": "fireworks-ai", "groq": "groq",
	"huggingface": "huggingface", "longcat": "longcat",
	"modelscope": "modelscope", "nebius": "nebius",
	"novita": "novita-ai", "nvidia": "nvidia",
	"ollama-cloud": "ollama-cloud", "sambanova": "sambanova",
	"siliconflow": "siliconflow-cn", "siliconflow-en": "siliconflow",
	"stepfun-api": "stepfun", "stepfun-api-en": "stepfun-ai",
	"tencent-hunyuan":   "tencent-tokenhub",
	"volcengine-doubao": "volcengine", "xiaomi-mimo": "xiaomi",
}

func LookupModelPrices(ctx context.Context, provider string, modelIDs []string) map[string]ModelPrice {
	result := make(map[string]ModelPrice, len(modelIDs))
	if _, ok := planPriceProviders[provider]; ok {
		for _, id := range modelIDs {
			result[id] = ModelPrice{Found: true}
		}
		return result
	}
	providerID := modelsDevProviderIDs[provider]
	if providerID == "" || len(modelIDs) == 0 {
		return result
	}
	modelPriceCache.Lock()
	fresh := time.Since(modelPriceCache.loadedAt) < 24*time.Hour && modelPriceCache.values != nil
	values := modelPriceCache.values
	modelPriceCache.Unlock()
	if !fresh {
		loaded, err := fetchModelsDev(ctx)
		if err != nil {
			return result
		}
		modelPriceCache.Lock()
		modelPriceCache.values = loaded
		modelPriceCache.loadedAt = time.Now()
		values = loaded
		modelPriceCache.Unlock()
	}
	for _, id := range modelIDs {
		if price, ok := values[providerID][id]; ok {
			result[id] = price
		}
	}
	return result
}

func fetchModelsDev(ctx context.Context) (map[string]map[string]ModelPrice, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsDevURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("models.dev returned %s", resp.Status)
	}
	var payload map[string]modelsDevProvider
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024*1024)).Decode(&payload); err != nil {
		return nil, err
	}
	result := make(map[string]map[string]ModelPrice, len(payload))
	for providerID, provider := range payload {
		prices := make(map[string]ModelPrice)
		for modelID, model := range provider.Models {
			if model.Cost == nil || model.Cost.Input == nil || model.Cost.Output == nil {
				continue
			}
			prices[strings.TrimSpace(modelID)] = ModelPrice{Input: *model.Cost.Input, Output: *model.Cost.Output, Found: true}
		}
		result[providerID] = prices
	}
	return result, nil
}

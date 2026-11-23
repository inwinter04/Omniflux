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

var modelPriceCache struct {
	sync.Mutex
	loadedAt time.Time
	values   map[string]map[string]ModelPrice
}

var modelsDevProviderIDs = map[string]string{
	"openai": "openai", "anthropic": "anthropic", "gemini": "google",
	"deepseek": "deepseek", "xai": "xai", "mistral": "mistral",
	"kimi": "moonshotai", "kimi-global": "moonshotai",
	"zhipu-glm": "zai", "zhipu-glm-en": "zai",
	"minimax": "minimax-cn", "minimax-en": "minimax",
	"qwencloud": "alibaba", "openrouter": "openrouter",
}

func LookupModelPrices(ctx context.Context, provider string, modelIDs []string) map[string]ModelPrice {
	result := make(map[string]ModelPrice, len(modelIDs))
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

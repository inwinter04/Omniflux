package bifrostx

import (
	"context"
	"fmt"
	"strings"
	"sync"

	core "github.com/maximhq/bifrost/core"
	"github.com/maximhq/bifrost/core/schemas"
)

type ProviderSpec struct {
	Provider       string
	AccountID      string
	Name           string
	APIKey         string
	BaseURL        string
	AllowedModels  []string
	Weight         float64
	TimeoutSeconds int
	MaxRetries     int
}

type Account struct {
	specs map[schemas.ModelProvider][]ProviderSpec
}

// MapProvider 把数据库中的 provider 标识转换为 Bifrost 的 ModelProvider。
func MapProvider(provider string) (schemas.ModelProvider, error) {
	return mapProvider(provider)
}

func mapProvider(provider string) (schemas.ModelProvider, error) {
	switch provider {
	case "openai", "openai_compatible":
		return schemas.OpenAI, nil
	case "azure_openai":
		return schemas.Azure, nil
	case "anthropic":
		return schemas.Anthropic, nil
	case "gemini":
		return schemas.Gemini, nil
	case "vertex":
		return schemas.Vertex, nil
	case "bedrock":
		return schemas.Bedrock, nil
	default:
		return "", fmt.Errorf("unsupported provider %q", provider)
	}
}

func NewAccount(specs []ProviderSpec) (*Account, error) {
	byProvider := make(map[schemas.ModelProvider][]ProviderSpec)
	baseURLs := make(map[schemas.ModelProvider]string)
	for _, spec := range specs {
		provider, err := mapProvider(spec.Provider)
		if err != nil {
			return nil, err
		}
		if spec.APIKey == "" && provider != schemas.Bedrock {
			return nil, fmt.Errorf("provider account %q has no credential", spec.Name)
		}
		if spec.BaseURL != "" {
			previous, exists := baseURLs[provider]
			if exists && previous != spec.BaseURL {
				return nil, fmt.Errorf("provider %s has conflicting base URLs %q and %q; separate custom providers are required", provider, previous, spec.BaseURL)
			}
			baseURLs[provider] = spec.BaseURL
		}
		byProvider[provider] = append(byProvider[provider], spec)
	}
	return &Account{specs: byProvider}, nil
}

func (a *Account) GetConfiguredProviders() ([]schemas.ModelProvider, error) {
	providers := make([]schemas.ModelProvider, 0, len(a.specs))
	for provider := range a.specs {
		providers = append(providers, provider)
	}
	return providers, nil
}

func (a *Account) GetKeysForProvider(_ context.Context, provider schemas.ModelProvider) ([]schemas.Key, error) {
	specs := a.specs[provider]
	keys := make([]schemas.Key, 0, len(specs))
	for _, spec := range specs {
		models := spec.AllowedModels
		if len(models) == 0 {
			models = schemas.WhiteList{"*"}
		}
		weight := spec.Weight
		if weight <= 0 {
			weight = 1
		}
		key := schemas.Key{ID: spec.AccountID, Name: spec.Name, Value: schemas.SecretVar{Val: spec.APIKey}, Models: models, Weight: weight}
		keys = append(keys, key)
	}
	return keys, nil
}

func (a *Account) GetConfigForProvider(provider schemas.ModelProvider) (*schemas.ProviderConfig, error) {
	specs := a.specs[provider]
	if len(specs) == 0 {
		return nil, fmt.Errorf("provider %q is not configured", provider)
	}
	baseURL := ""
	for _, spec := range specs {
		if spec.BaseURL != "" {
			if baseURL != "" && baseURL != spec.BaseURL {
				return nil, fmt.Errorf("provider %s has conflicting base URLs", provider)
			}
			baseURL = spec.BaseURL
		}
	}
	timeout := 60
	retries := 1
	if specs[0].TimeoutSeconds > 0 {
		timeout = specs[0].TimeoutSeconds
	}
	if specs[0].MaxRetries >= 0 {
		retries = specs[0].MaxRetries
	}
	return &schemas.ProviderConfig{
		NetworkConfig: schemas.NetworkConfig{
			BaseURL:                        baseURL,
			DefaultRequestTimeoutInSeconds: timeout,
			MaxRetries:                     retries,
			AllowPrivateNetwork:            strings.Contains(baseURL, "localhost") || strings.Contains(baseURL, "127.0.0.1"),
		},
		ConcurrencyAndBufferSize: schemas.DefaultConcurrencyAndBufferSize,
	}, nil
}

type Engine struct {
	// mu 保护 client/account 的热替换;请求路径用 Client() 读取。
	mu      sync.RWMutex
	client  *core.Bifrost
	account *Account
}

func NewEngine(ctx context.Context, specs []ProviderSpec) (*Engine, error) {
	account, err := NewAccount(specs)
	if err != nil {
		return nil, err
	}
	client, err := core.Init(ctx, schemas.BifrostConfig{Account: account})
	if err != nil {
		return nil, fmt.Errorf("initialize bifrost: %w", err)
	}
	return &Engine{client: client, account: account}, nil
}

func (e *Engine) Client() *core.Bifrost {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.client
}

func (e *Engine) Account() *Account {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.account
}

// Reload 用新配置重建上游引擎并原子替换;旧客户端在替换后关闭。
func (e *Engine) Reload(ctx context.Context, specs []ProviderSpec) error {
	next, err := NewEngine(ctx, specs)
	if err != nil {
		return err
	}
	e.mu.Lock()
	previous := e.client
	e.client = next.client
	e.account = next.account
	e.mu.Unlock()
	if previous != nil {
		previous.Shutdown()
	}
	return nil
}

func (e *Engine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.client != nil {
		e.client.Shutdown()
	}
}

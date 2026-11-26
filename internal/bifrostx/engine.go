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
	ProxyType      string
	ProxyURL       string
	AllowedModels  []string
	Weight         float64
	TimeoutSeconds int
	MaxRetries     int
}

type Account struct {
	specs map[schemas.ModelProvider][]ProviderSpec
}

// MapProvider 把数据库中的 provider 标识转换为 Bifrost 的 ModelProvider。
// 厂商目录里的 ID 直接作为独立 provider key 使用(恒等映射);
// 其余仍走协议级映射(openai_compatible -> openai 等)。
func MapProvider(provider string) (schemas.ModelProvider, error) {
	if VendorByID(provider) != nil {
		return schemas.ModelProvider(provider), nil
	}
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

// resolveProviderKey 把数据库 provider 标识解析为引擎分组 key。
// 厂商账号按厂商 ID 分组,协议账号沿用协议映射。
func resolveProviderKey(provider string) (schemas.ModelProvider, error) {
	return MapProvider(provider)
}

// providerBaseURL 计算某 provider key 的上游根地址:
// 优先用账号自带 base_url(本地/自部署或存量数据),否则回落厂商目录预置。
func providerBaseURL(key schemas.ModelProvider, specs []ProviderSpec) string {
	for _, spec := range specs {
		if spec.BaseURL != "" {
			return spec.BaseURL
		}
	}
	if vendor := VendorByID(string(key)); vendor != nil && vendor.BaseURL != "" {
		return vendor.BaseURL
	}
	return ""
}

func NewAccount(specs []ProviderSpec) (*Account, error) {
	byProvider := make(map[schemas.ModelProvider][]ProviderSpec)
	baseURLs := make(map[schemas.ModelProvider]string)
	blankBaseURLs := make(map[schemas.ModelProvider]bool)
	proxySignatures := make(map[schemas.ModelProvider]string)
	for _, spec := range specs {
		provider, err := resolveProviderKey(spec.Provider)
		if err != nil {
			return nil, err
		}
		if spec.APIKey == "" && provider != schemas.Bedrock {
			return nil, fmt.Errorf("provider account %q has no credential", spec.Name)
		}
		proxySignature, err := ProxySignature(spec.ProxyType, spec.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("provider account %q: %w", spec.Name, err)
		}
		if previous, exists := proxySignatures[provider]; exists && previous != proxySignature {
			return nil, fmt.Errorf("provider %s has conflicting proxy configurations; set the same proxy for every account", provider)
		}
		proxySignatures[provider] = proxySignature
		if spec.BaseURL == "" {
			blankBaseURLs[provider] = true
		} else {
			previous, exists := baseURLs[provider]
			if exists && previous != spec.BaseURL {
				return nil, fmt.Errorf("provider %s has conflicting base URLs %q and %q; separate custom providers are required", provider, previous, spec.BaseURL)
			}
			baseURLs[provider] = spec.BaseURL
		}
		byProvider[provider] = append(byProvider[provider], spec)
		if blankBaseURLs[provider] && baseURLs[provider] != "" {
			return nil, fmt.Errorf("provider %s mixes default and custom base URLs; set the same effective endpoint for every account", provider)
		}
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
		key := schemas.Key{ID: spec.AccountID, Name: spec.AccountID, Value: schemas.SecretVar{Val: spec.APIKey}, Models: models, Weight: weight}
		keys = append(keys, key)
	}
	return keys, nil
}

func (a *Account) GetConfigForProvider(provider schemas.ModelProvider) (*schemas.ProviderConfig, error) {
	specs := a.specs[provider]
	if len(specs) == 0 {
		return nil, fmt.Errorf("provider %q is not configured", provider)
	}
	// 厂商目录条目委托给 openai 协议实现,并按厂商预置覆盖请求路径;
	// 这样同一协议下可以并存多个厂商,各自持有独立 endpoint 与密钥。
	baseURL := providerBaseURL(provider, specs)
	timeout := 60
	retries := 1
	if specs[0].TimeoutSeconds > 0 {
		timeout = specs[0].TimeoutSeconds
	}
	if specs[0].MaxRetries >= 0 {
		retries = specs[0].MaxRetries
	}
	proxyConfig, err := BuildProxyConfig(specs[0].ProxyType, specs[0].ProxyURL)
	if err != nil {
		return nil, fmt.Errorf("provider %q: %w", provider, err)
	}
	config := &schemas.ProviderConfig{
		NetworkConfig: schemas.NetworkConfig{
			BaseURL:                        baseURL,
			DefaultRequestTimeoutInSeconds: timeout,
			MaxRetries:                     retries,
			AllowPrivateNetwork:            strings.Contains(baseURL, "localhost") || strings.Contains(baseURL, "127.0.0.1"),
		},
		ConcurrencyAndBufferSize: schemas.DefaultConcurrencyAndBufferSize,
		ProxyConfig:              proxyConfig,
	}
	if vendor := VendorByID(string(provider)); vendor != nil {
		vendorBaseURL := vendor.BaseURL
		if baseURL != "" {
			vendorBaseURL = baseURL
		}
		config.NetworkConfig.BaseURL = vendorBaseURL
		config.NetworkConfig.AllowPrivateNetwork = strings.Contains(vendorBaseURL, "localhost") || strings.Contains(vendorBaseURL, "127.0.0.1")
		config.CustomProviderConfig = &schemas.CustomProviderConfig{
			BaseProviderType: schemas.OpenAI,
			RequestPathOverrides: map[schemas.RequestType]string{
				schemas.ChatCompletionRequest:       vendor.ChatPath(),
				schemas.ChatCompletionStreamRequest: vendor.ChatPath(),
				schemas.EmbeddingRequest:            vendor.EmbeddingsPath(),
				schemas.ListModelsRequest:           vendor.ModelsPath(),
			},
		}
	}
	return config, nil
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

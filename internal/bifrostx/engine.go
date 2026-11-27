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
	APIProtocol    string
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

// EffectiveProtocol returns the spec's resolved upstream api_protocol.
func (s ProviderSpec) EffectiveProtocol() string {
	return EffectiveProtocol(s.Provider, s.APIProtocol)
}

// EngineKey returns the Bifrost provider key isolating this account's endpoint and protocol.
func (s ProviderSpec) EngineKey() (schemas.ModelProvider, error) {
	return EngineProviderKey(s.AccountID, s.Provider, s.APIProtocol)
}

type Account struct {
	mu          sync.RWMutex
	specs       map[schemas.ModelProvider][]ProviderSpec
	logicalKeys map[schemas.ModelProvider][]schemas.ModelProvider
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

func NewAccount(specs []ProviderSpec) (*Account, error) {
	byEngineKey := make(map[schemas.ModelProvider][]ProviderSpec)
	logicalKeys := make(map[schemas.ModelProvider][]schemas.ModelProvider)
	seenLogical := make(map[schemas.ModelProvider]map[schemas.ModelProvider]bool)
	baseURLs := make(map[schemas.ModelProvider]string)
	blankBaseURLs := make(map[schemas.ModelProvider]bool)
	proxySignatures := make(map[schemas.ModelProvider]string)
	for _, spec := range specs {
		logicalProvider, err := resolveProviderKey(spec.Provider)
		if err != nil {
			return nil, err
		}
		proto := spec.EffectiveProtocol()
		if !IsValidAPIProtocol(proto) {
			return nil, fmt.Errorf("provider account %q has unsupported api_protocol %q", spec.Name, proto)
		}
		spec.APIProtocol = proto
		baseType, err := BaseProviderTypeForProtocol(proto)
		if err != nil {
			return nil, err
		}
		engineKey, err := EngineProviderKey(spec.AccountID, spec.Provider, proto)
		if err != nil {
			return nil, err
		}
		if spec.APIKey == "" && baseType != schemas.Bedrock {
			return nil, fmt.Errorf("provider account %q has no credential", spec.Name)
		}
		proxySignature, err := ProxySignature(spec.ProxyType, spec.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("provider account %q: %w", spec.Name, err)
		}
		if previous, exists := proxySignatures[engineKey]; exists && previous != proxySignature {
			return nil, fmt.Errorf("provider %s has conflicting proxy configurations; set the same proxy for every account", engineKey)
		}
		proxySignatures[engineKey] = proxySignature
		if spec.BaseURL == "" {
			blankBaseURLs[engineKey] = true
		} else {
			previous, exists := baseURLs[engineKey]
			if exists && previous != spec.BaseURL {
				return nil, fmt.Errorf("provider %s has conflicting base URLs %q and %q; separate custom providers are required", engineKey, previous, spec.BaseURL)
			}
			baseURLs[engineKey] = spec.BaseURL
		}
		byEngineKey[engineKey] = append(byEngineKey[engineKey], spec)
		if blankBaseURLs[engineKey] && baseURLs[engineKey] != "" {
			return nil, fmt.Errorf("provider %s mixes default and custom base URLs; set the same effective endpoint for every account", engineKey)
		}
		if seenLogical[logicalProvider] == nil {
			seenLogical[logicalProvider] = make(map[schemas.ModelProvider]bool)
		}
		if !seenLogical[logicalProvider][engineKey] {
			seenLogical[logicalProvider][engineKey] = true
			logicalKeys[logicalProvider] = append(logicalKeys[logicalProvider], engineKey)
		}
	}
	return &Account{specs: byEngineKey, logicalKeys: logicalKeys}, nil
}

// EngineKeysForLogical resolves all active engine keys belonging to a logical provider key
// (e.g. schemas.OpenAI or vendor ID) or returns the target itself if it is already an engine key.
func (a *Account) EngineKeysForLogical(key schemas.ModelProvider) []schemas.ModelProvider {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if keys := a.logicalKeys[key]; len(keys) > 0 {
		return append([]schemas.ModelProvider(nil), keys...)
	}
	if _, ok := a.specs[key]; ok {
		return []schemas.ModelProvider{key}
	}
	return nil
}

func allowedRequestsForProtocol(proto string) *schemas.AllowedRequests {
	switch proto {
	case ProtocolOpenAIChat:
		return &schemas.AllowedRequests{
			ListModels:           true,
			ModelRetrieve:        true,
			ChatCompletion:       true,
			ChatCompletionStream: true,
			Embedding:            true,
		}
	case ProtocolOpenAIResponses:
		return &schemas.AllowedRequests{
			ListModels:           true,
			ModelRetrieve:        true,
			ChatCompletion:       true,
			ChatCompletionStream: true,
			Responses:            true,
			ResponsesStream:      true,
			Compaction:           true,
			CountTokens:          true,
			Embedding:            true,
		}
	case ProtocolAnthropicMessages:
		return &schemas.AllowedRequests{
			ListModels:           true,
			ModelRetrieve:        true,
			ChatCompletion:       true,
			ChatCompletionStream: true,
			Responses:            true,
			ResponsesStream:      true,
			CountTokens:          true,
		}
	case ProtocolGeminiNative:
		return &schemas.AllowedRequests{
			ListModels:           true,
			ModelRetrieve:        true,
			ChatCompletion:       true,
			ChatCompletionStream: true,
			Responses:            true,
			ResponsesStream:      true,
			CountTokens:          true,
			Embedding:            true,
		}
	default:
		return nil
	}
}

func resolveSpecBaseURLAndOverrides(spec ProviderSpec, proto string, baseType schemas.ModelProvider) (string, map[schemas.RequestType]string) {
	vendor := VendorByID(spec.Provider)
	baseURL := strings.TrimRight(strings.TrimSpace(spec.BaseURL), "/")
	if baseURL == "" {
		if vendor != nil && vendor.BaseURL != "" {
			baseURL = strings.TrimRight(vendor.BaseURL, "/")
		} else {
			switch proto {
			case ProtocolAnthropicMessages:
				baseURL = "https://api.anthropic.com"
			case ProtocolGeminiNative:
				baseURL = "https://generativelanguage.googleapis.com/v1beta"
			case ProtocolOpenAIChat, ProtocolOpenAIResponses:
				baseURL = "https://api.openai.com"
			}
		}
	} else {
		if vendor != nil && vendor.URLPath != "" && strings.HasSuffix(baseURL, vendor.URLPath) {
			baseURL = strings.TrimSuffix(baseURL, vendor.URLPath)
		} else if vendor == nil && (baseType == schemas.OpenAI || baseType == schemas.Anthropic) && strings.HasSuffix(baseURL, "/v1") {
			baseURL = strings.TrimSuffix(baseURL, "/v1")
		}
	}

	if vendor == nil {
		return baseURL, nil
	}

	switch proto {
	case ProtocolOpenAIChat:
		return baseURL, map[schemas.RequestType]string{
			schemas.ChatCompletionRequest:       vendor.ChatPath(),
			schemas.ChatCompletionStreamRequest: vendor.ChatPath(),
			schemas.EmbeddingRequest:            vendor.EmbeddingsPath(),
			schemas.ListModelsRequest:           vendor.ModelsPath(),
		}
	case ProtocolOpenAIResponses:
		return baseURL, map[schemas.RequestType]string{
			schemas.ChatCompletionRequest:       vendor.ChatPath(),
			schemas.ChatCompletionStreamRequest: vendor.ChatPath(),
			schemas.ResponsesRequest:            vendor.ResponsesPath(),
			schemas.ResponsesStreamRequest:      vendor.ResponsesPath(),
			schemas.CompactionRequest:           vendor.CompactionPath(),
			schemas.CountTokensRequest:          strings.TrimSuffix(vendor.URLPath, "/") + "/responses/input_tokens",
			schemas.EmbeddingRequest:            vendor.EmbeddingsPath(),
			schemas.ListModelsRequest:           vendor.ModelsPath(),
		}
	case ProtocolAnthropicMessages:
		if strings.TrimSpace(spec.BaseURL) == "" && vendor.URLPath != "" {
			return baseURL, map[schemas.RequestType]string{
				schemas.ChatCompletionRequest:       vendor.MessagesPath(),
				schemas.ChatCompletionStreamRequest: vendor.MessagesPath(),
				schemas.ResponsesRequest:            vendor.MessagesPath(),
				schemas.ResponsesStreamRequest:      vendor.MessagesPath(),
				schemas.CountTokensRequest:          strings.TrimSuffix(vendor.URLPath, "/") + "/messages/count_tokens",
				schemas.ListModelsRequest:           vendor.ModelsPath(),
			}
		}
	}
	return baseURL, nil
}

func (a *Account) GetConfiguredProviders() ([]schemas.ModelProvider, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	providers := make([]schemas.ModelProvider, 0, len(a.specs))
	for provider := range a.specs {
		providers = append(providers, provider)
	}
	return providers, nil
}

func (a *Account) GetKeysForProvider(_ context.Context, provider schemas.ModelProvider) ([]schemas.Key, error) {
	a.mu.RLock()
	specs := append([]ProviderSpec(nil), a.specs[provider]...)
	a.mu.RUnlock()
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
	a.mu.RLock()
	specs := append([]ProviderSpec(nil), a.specs[provider]...)
	a.mu.RUnlock()
	if len(specs) == 0 {
		return nil, fmt.Errorf("provider %q is not configured", provider)
	}
	spec := specs[0]
	proto := spec.EffectiveProtocol()
	baseType, err := BaseProviderTypeForProtocol(proto)
	if err != nil {
		return nil, fmt.Errorf("provider %q: %w", provider, err)
	}
	baseURL, overrides := resolveSpecBaseURLAndOverrides(spec, proto, baseType)
	timeout := 300
	retries := 1
	if spec.TimeoutSeconds > 0 {
		timeout = spec.TimeoutSeconds
	}
	if spec.MaxRetries >= 0 {
		retries = spec.MaxRetries
	}
	proxyConfig, err := BuildProxyConfig(spec.ProxyType, spec.ProxyURL)
	if err != nil {
		return nil, fmt.Errorf("provider %q: %w", provider, err)
	}
	config := &schemas.ProviderConfig{
		NetworkConfig: schemas.NetworkConfig{
			BaseURL:                        baseURL,
			DefaultRequestTimeoutInSeconds: timeout,
			StreamIdleTimeoutInSeconds:     120,
			MaxRetries:                     retries,
			AllowPrivateNetwork:            strings.Contains(baseURL, "localhost") || strings.Contains(baseURL, "127.0.0.1") || strings.Contains(baseURL, "[::1]"),
		},
		ConcurrencyAndBufferSize: schemas.DefaultConcurrencyAndBufferSize,
		ProxyConfig:              proxyConfig,
	}
	if supportsPerAccountCustomProvider(baseType) {
		config.CustomProviderConfig = &schemas.CustomProviderConfig{
			BaseProviderType:     baseType,
			AllowedRequests:      allowedRequestsForProtocol(proto),
			RequestPathOverrides: overrides,
		}
	}
	return config, nil
}

// SetProviderProxy atomically updates the proxy configuration for a configured provider key.
func (a *Account) SetProviderProxy(provider schemas.ModelProvider, proxyType, proxyURL string) (changed bool, exists bool, err error) {
	normType, normURL, err := NormalizeProxy(proxyType, proxyURL)
	if err != nil {
		return false, false, err
	}
	nextSig, err := ProxySignature(normType, normURL)
	if err != nil {
		return false, false, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	specs := a.specs[provider]
	if len(specs) == 0 {
		return false, false, nil
	}
	prevSig, err := ProxySignature(specs[0].ProxyType, specs[0].ProxyURL)
	if err == nil && prevSig == nextSig {
		return false, true, nil
	}
	updated := make([]ProviderSpec, len(specs))
	for i, spec := range specs {
		spec.ProxyType = normType
		spec.ProxyURL = normURL
		updated[i] = spec
	}
	a.specs[provider] = updated
	return true, true, nil
}

type Engine struct {
	// mu 保护 client/account 的热替换;请求路径用 Client() 读取。
	// updateMu 串行化 Reload 与 UpdateProviderProxy,防止并发热替换冲突。
	lifecycleCtx context.Context
	mu           sync.RWMutex
	updateMu     sync.Mutex
	client       *core.Bifrost
	account      *Account
}

func NewEngine(ctx context.Context, specs []ProviderSpec) (*Engine, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	account, err := NewAccount(specs)
	if err != nil {
		return nil, err
	}
	client, err := core.Init(ctx, schemas.BifrostConfig{Account: account})
	if err != nil {
		return nil, fmt.Errorf("initialize bifrost: %w", err)
	}
	return &Engine{lifecycleCtx: ctx, client: client, account: account}, nil
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
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	e.updateMu.Lock()
	defer e.updateMu.Unlock()
	// Bifrost 将 Init 传入的 ctx 作为底层实例整个生命周期的上下文;
	// Reload 调用方通常会传入短时请求/超时 ctx，若直接透传会在调用返回取消后导致后续 UpdateProvider 误判为 shutting down。
	lifecycleCtx := e.lifecycleCtx
	if lifecycleCtx == nil {
		lifecycleCtx = context.Background()
	}
	if err := lifecycleCtx.Err(); err != nil {
		return err
	}
	next, err := NewEngine(lifecycleCtx, specs)
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

// UpdateProviderProxy hot-swaps the proxy settings for a logical provider (all its accounts)
// or a specific engine provider key without rebuilding unrelated providers.
func (e *Engine) UpdateProviderProxy(provider schemas.ModelProvider, proxyType, proxyURL string) error {
	e.updateMu.Lock()
	defer e.updateMu.Unlock()
	e.mu.RLock()
	client := e.client
	account := e.account
	e.mu.RUnlock()
	if client == nil || account == nil {
		return nil
	}
	targets := account.EngineKeysForLogical(provider)
	for _, target := range targets {
		changed, exists, err := account.SetProviderProxy(target, proxyType, proxyURL)
		if err != nil {
			return err
		}
		if !exists || !changed {
			continue
		}
		if err := client.UpdateProvider(target); err != nil {
			return err
		}
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

package bifrostx

import (
	"fmt"
	"slices"
	"strings"

	"github.com/maximhq/bifrost/core/schemas"
)

const (
	ProtocolOpenAIChat        = "openai_chat"
	ProtocolOpenAIResponses   = "openai_responses"
	ProtocolAnthropicMessages = "anthropic_messages"
	ProtocolGeminiNative      = "gemini_native"
	ProtocolAzureOpenAI       = "azure_openai"
	ProtocolBedrock           = "bedrock"
	ProtocolVertex            = "vertex"
)

var validAPIProtocols = []string{
	ProtocolOpenAIChat,
	ProtocolOpenAIResponses,
	ProtocolAnthropicMessages,
	ProtocolGeminiNative,
	ProtocolAzureOpenAI,
	ProtocolBedrock,
	ProtocolVertex,
}

// IsValidAPIProtocol reports whether protocol is a recognized upstream api_protocol identifier.
func IsValidAPIProtocol(protocol string) bool {
	return slices.Contains(validAPIProtocols, strings.TrimSpace(protocol))
}

// DefaultProtocolForProvider returns the default upstream api_protocol for a database provider ID.
func DefaultProtocolForProvider(provider string) string {
	if vendor := VendorByID(provider); vendor != nil {
		if vendor.DefaultProtocol != "" {
			return vendor.DefaultProtocol
		}
		return ProtocolOpenAIChat
	}
	switch provider {
	case "openai":
		return ProtocolOpenAIResponses
	case "openai_compatible":
		return ProtocolOpenAIChat
	case "anthropic":
		return ProtocolAnthropicMessages
	case "gemini":
		return ProtocolGeminiNative
	case "azure_openai":
		return ProtocolAzureOpenAI
	case "bedrock":
		return ProtocolBedrock
	case "vertex":
		return ProtocolVertex
	default:
		return ProtocolOpenAIChat
	}
}

// AllowedProtocolsForProvider returns the allowed upstream api_protocol options for a provider ID.
func AllowedProtocolsForProvider(provider string) []string {
	if VendorByID(provider) != nil {
		return []string{
			ProtocolOpenAIChat,
			ProtocolOpenAIResponses,
			ProtocolAnthropicMessages,
			ProtocolGeminiNative,
		}
	}
	switch provider {
	case "openai":
		return []string{ProtocolOpenAIResponses, ProtocolOpenAIChat}
	case "openai_compatible":
		return []string{
			ProtocolOpenAIChat,
			ProtocolOpenAIResponses,
			ProtocolAnthropicMessages,
			ProtocolGeminiNative,
		}
	case "anthropic":
		return []string{ProtocolAnthropicMessages}
	case "gemini":
		return []string{ProtocolGeminiNative}
	case "azure_openai":
		return []string{ProtocolAzureOpenAI}
	case "bedrock":
		return []string{ProtocolBedrock}
	case "vertex":
		return []string{ProtocolVertex}
	default:
		return nil
	}
}

// IsProtocolAllowedForProvider checks whether apiProtocol is allowed on the given provider ID.
func IsProtocolAllowedForProvider(provider, apiProtocol string) bool {
	return slices.Contains(AllowedProtocolsForProvider(provider), strings.TrimSpace(apiProtocol))
}

// EffectiveProtocol returns apiProtocol if valid/non-empty, otherwise the provider default.
func EffectiveProtocol(provider, apiProtocol string) string {
	trimmed := strings.TrimSpace(apiProtocol)
	if trimmed != "" {
		return trimmed
	}
	return DefaultProtocolForProvider(provider)
}

// BaseProviderTypeForProtocol maps an upstream api_protocol to the underlying Bifrost base provider.
func BaseProviderTypeForProtocol(protocol string) (schemas.ModelProvider, error) {
	switch strings.TrimSpace(protocol) {
	case ProtocolOpenAIChat, ProtocolOpenAIResponses:
		return schemas.OpenAI, nil
	case ProtocolAnthropicMessages:
		return schemas.Anthropic, nil
	case ProtocolGeminiNative:
		return schemas.Gemini, nil
	case ProtocolAzureOpenAI:
		return schemas.Azure, nil
	case ProtocolBedrock:
		return schemas.Bedrock, nil
	case ProtocolVertex:
		return schemas.Vertex, nil
	default:
		return "", fmt.Errorf("unsupported api_protocol %q", protocol)
	}
}

// EngineProviderKey returns the isolated Bifrost provider key for an account+provider+protocol.
// Accounts using HTTP protocols supported as Bifrost CustomProvider (OpenAI, Anthropic, Gemini)
// are isolated per account ID so each account can own independent base_url, api_protocol, and proxy settings.
func EngineProviderKey(accountID, provider, apiProtocol string) (schemas.ModelProvider, error) {
	proto := EffectiveProtocol(provider, apiProtocol)
	baseType, err := BaseProviderTypeForProtocol(proto)
	if err != nil {
		return "", err
	}
	trimmedID := strings.TrimSpace(accountID)
	if trimmedID != "" && supportsPerAccountCustomProvider(baseType) {
		return schemas.ModelProvider("acct_" + trimmedID), nil
	}
	return MapProvider(provider)
}

func supportsPerAccountCustomProvider(baseType schemas.ModelProvider) bool {
	return baseType == schemas.OpenAI || baseType == schemas.Anthropic || baseType == schemas.Gemini
}

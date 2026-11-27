package dataplane

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/maximhq/bifrost/core/schemas"
)

const (
	ClientProtocolOpenAIChat        = "openai_chat"
	ClientProtocolOpenAIResponses   = "openai_responses"
	ClientProtocolAnthropicMessages = "anthropic_messages"
	ClientProtocolGeminiNative      = "gemini_native"
)

type clientProtocolContextKey struct{}

func withClientProtocol(ctx context.Context, proto string) context.Context {
	return context.WithValue(ctx, clientProtocolContextKey{}, proto)
}

func clientProtocolFromRequest(r *http.Request) string {
	if r == nil {
		return ClientProtocolOpenAIChat
	}
	if proto, ok := r.Context().Value(clientProtocolContextKey{}).(string); ok && proto != "" {
		return proto
	}
	return protocolForPath(r.URL.Path)
}

func protocolForPath(path string) string {
	switch {
	case strings.HasPrefix(path, "/v1/responses"):
		return ClientProtocolOpenAIResponses
	case strings.HasPrefix(path, "/v1/messages"):
		return ClientProtocolAnthropicMessages
	case strings.HasPrefix(path, "/v1beta/models"):
		return ClientProtocolGeminiNative
	default:
		return ClientProtocolOpenAIChat
	}
}

func anthropicErrorType(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden:
		return "permission_error"
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return "invalid_request_error"
	case http.StatusNotFound:
		return "not_found_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	case http.StatusServiceUnavailable:
		return "overloaded_error"
	default:
		return "api_error"
	}
}

func geminiRPCStatus(status int) string {
	switch status {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return "INVALID_ARGUMENT"
	case http.StatusUnauthorized:
		return "UNAUTHENTICATED"
	case http.StatusForbidden:
		return "PERMISSION_DENIED"
	case http.StatusNotFound:
		return "NOT_FOUND"
	case http.StatusTooManyRequests:
		return "RESOURCE_EXHAUSTED"
	case http.StatusGatewayTimeout, http.StatusRequestTimeout:
		return "DEADLINE_EXCEEDED"
	case http.StatusServiceUnavailable, http.StatusBadGateway:
		return "UNAVAILABLE"
	default:
		return "INTERNAL"
	}
}

// writeProtocolError formats data-plane errors natively for the inbound protocol
// (OpenAI Chat/Responses, Anthropic Messages, or Gemini Native) while keeping X-Request-ID and stable reason/code.
func writeProtocolError(w http.ResponseWriter, r *http.Request, status int, code, message string, opts ...apiErrorOptions) {
	proto := clientProtocolFromRequest(r)
	if proto == ClientProtocolOpenAIChat || proto == ClientProtocolOpenAIResponses {
		apiError(w, status, code, message, opts...)
		return
	}

	var option apiErrorOptions
	if len(opts) > 0 {
		option = opts[0]
	}
	requestID := option.RequestID
	if requestID == "" {
		requestID = uuid.NewString()
	}
	reason := option.Reason
	if reason == "" {
		reason = code
	}

	w.Header().Set("X-Request-ID", requestID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	switch proto {
	case ClientProtocolAnthropicMessages:
		errBody := map[string]any{
			"type":       anthropicErrorType(status),
			"code":       code,
			"reason":     reason,
			"message":    message,
			"request_id": requestID,
		}
		if option.Detail != "" {
			errBody["detail"] = option.Detail
		}
		if len(option.Params) > 0 {
			errBody["params"] = option.Params
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type":  "error",
			"error": errBody,
		})
	case ClientProtocolGeminiNative:
		errBody := map[string]any{
			"code":       status,
			"status":     geminiRPCStatus(status),
			"error_code": code,
			"reason":     reason,
			"message":    message,
			"request_id": requestID,
		}
		if option.Detail != "" {
			errBody["detail"] = option.Detail
		}
		if len(option.Params) > 0 {
			errBody["params"] = option.Params
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": errBody,
		})
	}
}

func writeProtocolBifrostError(w http.ResponseWriter, r *http.Request, bifrostErr *schemas.BifrostError) {
	status := http.StatusBadGateway
	if bifrostErr != nil && bifrostErr.StatusCode != nil && *bifrostErr.StatusCode >= 400 && *bifrostErr.StatusCode < 600 {
		status = *bifrostErr.StatusCode
	}
	_, _, errType, message := extractBifrostErrorInfo(bifrostErr)
	writeProtocolError(w, r, status, "upstream_error", "Upstream request failed.",
		apiErrorOptions{
			Type:   "upstream_error",
			Reason: "upstream_request_failed",
			Detail: message,
			Params: map[string]any{"upstream_type": errType},
		})
}

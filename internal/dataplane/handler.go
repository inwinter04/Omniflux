package dataplane

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/inwinter04/omniflux/internal/bifrostx"
)

// Deps 是数据面处理器的依赖集合。
type Deps struct {
	Logger           *slog.Logger
	Pool             *pgxpool.Pool
	Engine           *bifrostx.Engine
	APIKeyPepper     string
	OnTransportError func(providerKey string)

	rateLimiter *rateLimiter
}

// SetRateLimit 配置基础限流;limit<=0 或 Redis 不可用时禁用。
func (d *Deps) SetRateLimit(client *redis.Client, limit int) {
	d.rateLimiter = newRateLimiter(client, limit)
}

type apiErrorOptions struct {
	Type      string
	Reason    string
	Param     string
	Params    map[string]any
	Detail    string
	RequestID string
}

type apiErrorObject struct {
	Type      string         `json:"type"`
	Code      string         `json:"code"`
	Reason    string         `json:"reason"`
	Message   string         `json:"message"`
	Param     string         `json:"param,omitempty"`
	Params    map[string]any `json:"params,omitempty"`
	Detail    string         `json:"detail,omitempty"`
	RequestID string         `json:"request_id"`
}

type apiErrorEnvelope struct {
	Error apiErrorObject `json:"error"`
}

func errorTypeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request_error"
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden:
		return "permission_error"
	case http.StatusNotFound:
		return "not_found_error"
	case http.StatusConflict:
		return "conflict_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	case http.StatusBadGateway:
		return "upstream_error"
	case http.StatusGatewayTimeout:
		return "timeout_error"
	default:
		return "server_error"
	}
}

func newAPIErrorObject(status int, code, message string, options apiErrorOptions) apiErrorObject {
	reason := options.Reason
	if reason == "" {
		reason = code
	}
	requestID := options.RequestID
	if requestID == "" {
		requestID = uuid.NewString()
	}
	errorType := options.Type
	if errorType == "" {
		errorType = errorTypeForStatus(status)
	}
	return apiErrorObject{
		Type:      errorType,
		Code:      code,
		Reason:    reason,
		Message:   message,
		Param:     options.Param,
		Params:    options.Params,
		Detail:    options.Detail,
		RequestID: requestID,
	}
}

// apiError 输出语言中立、机器可读的 OpenAI 兼容错误。
func apiError(w http.ResponseWriter, status int, code, message string, options ...apiErrorOptions) {
	opts := apiErrorOptions{}
	if len(options) > 0 {
		opts = options[0]
	}
	obj := newAPIErrorObject(status, code, message, opts)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Request-ID", obj.RequestID)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apiErrorEnvelope{Error: obj})
}

func apiErrorEvent(status int, code, message, requestID string, options ...apiErrorOptions) json.RawMessage {
	opts := apiErrorOptions{RequestID: requestID}
	if len(options) > 0 {
		opts = options[0]
		opts.RequestID = requestID
	}
	obj := newAPIErrorObject(status, code, message, opts)
	payload, _ := json.Marshal(apiErrorEnvelope{Error: obj})
	return payload
}

// RateLimit 返回数据面限流中间件。
func (d Deps) RateLimit(next http.Handler) http.Handler {
	return d.rateLimiter.middleware(next)
}

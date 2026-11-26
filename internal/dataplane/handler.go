package dataplane

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/inwinter04/omniflux/internal/bifrostx"
	"github.com/inwinter04/omniflux/internal/fx"
)

// Deps 是数据面处理器的依赖集合。
type Deps struct {
	Logger           *slog.Logger
	Pool             *pgxpool.Pool
	Engine           *bifrostx.Engine
	FX               *fx.Service
	APIKeyPepper     string
	OnTransportError func(providerKey string)

	rateLimiter *rateLimiter
	breaker     *CandidateBreaker
}

// SetRateLimit 配置基础限流与候选短期熔断共享的 Redis 客户端。
func (d *Deps) SetRateLimit(client *redis.Client, limit int) {
	d.rateLimiter = newRateLimiter(client, limit)
	d.breaker = NewCandidateBreaker(client)
}

// RateLimit 提供与 SetRateLimit 配套的 API Key 基础限流中间件。
func (d Deps) RateLimit(next http.Handler) http.Handler {
	if d.rateLimiter == nil {
		return next
	}
	return d.rateLimiter.middleware(next)
}

func (d *Deps) Breaker() *CandidateBreaker {
	if d.breaker == nil {
		d.breaker = NewCandidateBreaker(nil)
	}
	return d.breaker
}

func (d *Deps) fxRatesSnapshot() map[string]float64 {
	if d.FX != nil {
		return d.FX.Snapshot()
	}
	return (&fx.Service{}).Snapshot()
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

func writeErrorObject(w http.ResponseWriter, status int, errObj apiErrorObject) {
	if errObj.RequestID != "" {
		w.Header().Set("X-Request-ID", errObj.RequestID)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apiErrorEnvelope{Error: errObj})
}

// apiError 按 OpenAI 兼容格式返回数据面错误并携带稳定 code/reason 与 X-Request-ID。
func apiError(w http.ResponseWriter, status int, code, message string, opts ...apiErrorOptions) {
	var option apiErrorOptions
	if len(opts) > 0 {
		option = opts[0]
	}
	writeErrorObject(w, status, newAPIErrorObject(status, code, message, option))
}

func apiErrorEvent(status int, code, message, requestID string, opts ...apiErrorOptions) []byte {
	var option apiErrorOptions
	if len(opts) > 0 {
		option = opts[0]
	}
	if option.RequestID == "" {
		option.RequestID = requestID
	}
	payload, err := json.Marshal(apiErrorEnvelope{Error: newAPIErrorObject(status, code, message, option)})
	if err != nil {
		return []byte(`{"error":{"type":"server_error","code":"internal_error","reason":"internal_error","message":"Internal error."}}`)
	}
	return payload
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

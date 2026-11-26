package dataplane

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"unicode/utf8"
)

const (
	StrategyBalanced         = "balanced"
	StrategyCostFirst        = "cost_first"
	StrategyQualityFirst     = "quality_first"
	StrategyLatencyFirst     = "latency_first"
	StrategyReliabilityFirst = "reliability_first"
	StrategyWeighted         = "weighted"
	StrategyRoundRobin       = "round_robin"
	StrategyStrictFailover   = "strict_failover"
	StrategyCustom           = "custom"
)

var allowedStrategies = map[string]bool{
	StrategyBalanced:         true,
	StrategyCostFirst:        true,
	StrategyQualityFirst:     true,
	StrategyLatencyFirst:     true,
	StrategyReliabilityFirst: true,
	StrategyWeighted:         true,
	StrategyRoundRobin:       true,
	StrategyStrictFailover:   true,
	StrategyCustom:           true,
}

var allowedCapabilities = map[string]bool{
	"vision":    true,
	"tools":     true,
	"reasoning": true,
	"json_mode": true,
	"streaming": true,
}

func IsAllowedStrategy(strategy string) bool {
	return allowedStrategies[strings.TrimSpace(strategy)]
}

func PresetWeights(strategy string) policyWeights {
	switch strings.TrimSpace(strategy) {
	case StrategyCostFirst:
		return policyWeights{Cost: 0.70, Quality: 0.10, Latency: 0.10, Reliability: 0.10}
	case StrategyQualityFirst:
		return policyWeights{Cost: 0.10, Quality: 0.70, Latency: 0.10, Reliability: 0.10}
	case StrategyLatencyFirst:
		return policyWeights{Cost: 0.10, Quality: 0.10, Latency: 0.70, Reliability: 0.10}
	case StrategyReliabilityFirst:
		return policyWeights{Cost: 0.10, Quality: 0.10, Latency: 0.10, Reliability: 0.70}
	default:
		return policyWeights{Cost: 0.40, Quality: 0.30, Latency: 0.15, Reliability: 0.15}
	}
}

func MarshalPresetWeights(strategy string) json.RawMessage {
	w := PresetWeights(strategy)
	data, _ := json.Marshal(w)
	return data
}

func ValidateWeightsJSON(raw json.RawMessage) (policyWeights, error) {
	if len(raw) == 0 {
		return PresetWeights(StrategyBalanced), nil
	}
	var w struct {
		Cost        *float64 `json:"cost"`
		Quality     *float64 `json:"quality"`
		Latency     *float64 `json:"latency"`
		Reliability *float64 `json:"reliability"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return policyWeights{}, fmt.Errorf("weights must be a JSON object")
	}
	out := policyWeights{}
	if w.Cost != nil {
		out.Cost = *w.Cost
	}
	if w.Quality != nil {
		out.Quality = *w.Quality
	}
	if w.Latency != nil {
		out.Latency = *w.Latency
	}
	if w.Reliability != nil {
		out.Reliability = *w.Reliability
	}
	for name, val := range map[string]float64{
		"cost": out.Cost, "quality": out.Quality, "latency": out.Latency, "reliability": out.Reliability,
	} {
		if math.IsNaN(val) || math.IsInf(val, 0) || val < 0 || val > 100 {
			return policyWeights{}, fmt.Errorf("weights.%s must be between 0 and 100", name)
		}
	}
	if out.Cost+out.Quality+out.Latency+out.Reliability <= 0 {
		return policyWeights{}, fmt.Errorf("weights sum must be greater than zero")
	}
	return out, nil
}

func NormalizeCurrencyCode(value string) (string, error) {
	cur := strings.ToUpper(strings.TrimSpace(value))
	if cur == "" {
		return "USD", nil
	}
	if len(cur) != 3 {
		return "", fmt.Errorf("compare_currency must be a 3-letter ISO currency code")
	}
	for _, r := range cur {
		if r < 'A' || r > 'Z' {
			return "", fmt.Errorf("compare_currency must be a 3-letter ISO currency code")
		}
	}
	return cur, nil
}

func ValidateCandidateCapabilities(items []string) ([]string, error) {
	out := make([]string, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, raw := range items {
		val := strings.ToLower(strings.TrimSpace(raw))
		if val == "" {
			continue
		}
		if !allowedCapabilities[val] {
			return nil, fmt.Errorf("unsupported capability %q", raw)
		}
		if !seen[val] {
			seen[val] = true
			out = append(out, val)
		}
	}
	return out, nil
}

func NormalizeCandidateTags(items []string) []string {
	out := make([]string, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, raw := range items {
		val := strings.TrimSpace(raw)
		if val == "" || seen[val] {
			continue
		}
		seen[val] = true
		out = append(out, val)
	}
	return out
}

// RouteRuleWhen matches incoming request attributes for Phase 2 conditional routing.
type RouteRuleWhen struct {
	MinInputTokens *int                `json:"min_input_tokens,omitempty"`
	MaxInputTokens *int                `json:"max_input_tokens,omitempty"`
	Stream         *bool               `json:"stream,omitempty"`
	Capabilities   []string            `json:"capabilities,omitempty"`
	ProjectIDs     []string            `json:"project_ids,omitempty"`
	ApplicationIDs []string            `json:"application_ids,omitempty"`
	APIKeyIDs      []string            `json:"api_key_ids,omitempty"`
	Keywords       []string            `json:"keywords,omitempty"`
	Metadata       map[string][]string `json:"metadata,omitempty"`
}

// RouteRuleTarget defines candidate filtering and optional strategy override when a rule matches.
type RouteRuleTarget struct {
	Strategy         string   `json:"strategy,omitempty"`
	CandidateTags    []string `json:"candidate_tags,omitempty"`
	AllowedProviders []string `json:"allowed_providers,omitempty"`
	BlockedProviders []string `json:"blocked_providers,omitempty"`
	AllowedModels    []string `json:"allowed_models,omitempty"`
}

type RouteRule struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Enabled  bool            `json:"enabled"`
	Priority int             `json:"priority"`
	When     RouteRuleWhen   `json:"when"`
	Target   RouteRuleTarget `json:"target"`
}

func ParseRules(raw []byte) []RouteRule {
	if len(raw) == 0 {
		return nil
	}
	var rules []RouteRule
	if err := json.Unmarshal(raw, &rules); err != nil {
		return nil
	}
	return rules
}

func ValidateRulesJSON(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var rules []RouteRule
	if err := json.Unmarshal(raw, &rules); err != nil {
		return fmt.Errorf("rules must be a JSON array")
	}
	if len(rules) > 50 {
		return fmt.Errorf("at most 50 routing rules are allowed")
	}
	seenIDs := make(map[string]bool, len(rules))
	for i, rule := range rules {
		if strings.TrimSpace(rule.ID) == "" {
			return fmt.Errorf("rule[%d].id is required", i)
		}
		if seenIDs[rule.ID] {
			return fmt.Errorf("duplicate rule id %q", rule.ID)
		}
		seenIDs[rule.ID] = true
		if strings.TrimSpace(rule.Name) == "" {
			return fmt.Errorf("rule[%d].name is required", i)
		}
		if rule.When.MinInputTokens != nil && *rule.When.MinInputTokens < 0 {
			return fmt.Errorf("rule %q min_input_tokens cannot be negative", rule.Name)
		}
		if rule.When.MaxInputTokens != nil && *rule.When.MaxInputTokens < 0 {
			return fmt.Errorf("rule %q max_input_tokens cannot be negative", rule.Name)
		}
		if rule.When.MinInputTokens != nil && rule.When.MaxInputTokens != nil &&
			*rule.When.MinInputTokens > *rule.When.MaxInputTokens {
			return fmt.Errorf("rule %q min_input_tokens cannot exceed max_input_tokens", rule.Name)
		}
		for _, capName := range rule.When.Capabilities {
			if !allowedCapabilities[strings.ToLower(strings.TrimSpace(capName))] {
				return fmt.Errorf("rule %q references unsupported capability %q", rule.Name, capName)
			}
		}
		if strings.TrimSpace(rule.Target.Strategy) != "" && !IsAllowedStrategy(rule.Target.Strategy) {
			return fmt.Errorf("rule %q targets unsupported strategy %q", rule.Name, rule.Target.Strategy)
		}
	}
	return nil
}

// RetryPolicy controls per-error-code retry and fallback behavior.
type RetryPolicy struct {
	Retry429               bool `json:"retry_429"`
	Retry5xx               bool `json:"retry_5xx"`
	RetryTimeout           bool `json:"retry_timeout"`
	FallbackOnAuthError    bool `json:"fallback_on_auth_error"`
	FallbackOnNotFound     bool `json:"fallback_on_not_found"`
	FallbackOnRateLimit    bool `json:"fallback_on_rate_limit"`
	FallbackOnServerError  bool `json:"fallback_on_server_error"`
	FallbackOnTTFTTimeout  bool `json:"fallback_on_ttft_timeout"`
	FallbackOnClientError  bool `json:"fallback_on_client_error"`
	MaxRetriesPerCandidate int  `json:"max_retries_per_candidate"`
}

func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		Retry429:               true,
		Retry5xx:               true,
		RetryTimeout:           true,
		FallbackOnAuthError:    true,
		FallbackOnNotFound:     true,
		FallbackOnRateLimit:    true,
		FallbackOnServerError:  true,
		FallbackOnTTFTTimeout:  true,
		FallbackOnClientError:  false,
		MaxRetriesPerCandidate: 0,
	}
}

func ParseRetryPolicy(raw []byte) RetryPolicy {
	p := DefaultRetryPolicy()
	if len(raw) == 0 {
		return p
	}
	_ = json.Unmarshal(raw, &p)
	if p.MaxRetriesPerCandidate < 0 {
		p.MaxRetriesPerCandidate = 0
	}
	if p.MaxRetriesPerCandidate > 3 {
		p.MaxRetriesPerCandidate = 3
	}
	return p
}

func ValidateRetryPolicyJSON(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var rp RetryPolicy
	if err := json.Unmarshal(raw, &rp); err != nil {
		return fmt.Errorf("retry_policy must be a JSON object")
	}
	if rp.MaxRetriesPerCandidate < 0 || rp.MaxRetriesPerCandidate > 3 {
		return fmt.Errorf("retry_policy.max_retries_per_candidate must be between 0 and 3")
	}
	return nil
}

// CircuitBreakerPolicy configures short-term per-candidate circuit breaking.
type CircuitBreakerPolicy struct {
	Enabled          bool `json:"enabled"`
	FailureThreshold int  `json:"failure_threshold"`
	CooldownSeconds  int  `json:"cooldown_seconds"`
	HalfOpenMaxCalls int  `json:"half_open_max_calls"`
}

func DefaultCircuitBreakerPolicy() CircuitBreakerPolicy {
	return CircuitBreakerPolicy{
		Enabled:          true,
		FailureThreshold: 5,
		CooldownSeconds:  30,
		HalfOpenMaxCalls: 1,
	}
}

func ParseCircuitBreakerPolicy(raw []byte) CircuitBreakerPolicy {
	p := DefaultCircuitBreakerPolicy()
	if len(raw) == 0 {
		return p
	}
	_ = json.Unmarshal(raw, &p)
	if p.FailureThreshold < 1 {
		p.FailureThreshold = 1
	}
	if p.CooldownSeconds < 5 {
		p.CooldownSeconds = 5
	}
	if p.HalfOpenMaxCalls < 1 {
		p.HalfOpenMaxCalls = 1
	}
	return p
}

func ValidateCircuitBreakerJSON(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var cb CircuitBreakerPolicy
	if err := json.Unmarshal(raw, &cb); err != nil {
		return fmt.Errorf("circuit_breaker must be a JSON object")
	}
	if cb.FailureThreshold < 1 || cb.FailureThreshold > 50 {
		return fmt.Errorf("circuit_breaker.failure_threshold must be between 1 and 50")
	}
	if cb.CooldownSeconds < 5 || cb.CooldownSeconds > 3600 {
		return fmt.Errorf("circuit_breaker.cooldown_seconds must be between 5 and 3600")
	}
	if cb.HalfOpenMaxCalls < 1 || cb.HalfOpenMaxCalls > 10 {
		return fmt.Errorf("circuit_breaker.half_open_max_calls must be between 1 and 10")
	}
	return nil
}

// AttemptAction encodes the resilience decision after a failed candidate attempt.
type AttemptAction struct {
	Class           string
	ShouldRetry     bool
	ShouldFallback  bool
	SkipSameAccount bool
	TripBreaker     bool
}

// ClassifyAttemptOutcome resolves retry, fallback, account-skip, and circuit-breaker behavior by HTTP status/error code.
func ClassifyAttemptOutcome(statusCode int, errCode, errType string, rp RetryPolicy) AttemptAction {
	lowerCode := strings.ToLower(strings.TrimSpace(errCode))
	lowerType := strings.ToLower(strings.TrimSpace(errType))

	if lowerCode == "ttft_timeout" || lowerType == "first_token_timeout" {
		return AttemptAction{
			Class:          "ttft_timeout",
			ShouldRetry:    false,
			ShouldFallback: rp.FallbackOnTTFTTimeout,
			TripBreaker:    true,
		}
	}
	if statusCode == http.StatusBadRequest || statusCode == http.StatusRequestEntityTooLarge || statusCode == http.StatusUnprocessableEntity {
		return AttemptAction{
			Class:          "client_error",
			ShouldRetry:    false,
			ShouldFallback: rp.FallbackOnClientError,
			TripBreaker:    false,
		}
	}
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		return AttemptAction{
			Class:           "auth_error",
			ShouldRetry:     false,
			ShouldFallback:  rp.FallbackOnAuthError,
			SkipSameAccount: true,
			TripBreaker:     true,
		}
	}
	if statusCode == http.StatusNotFound {
		return AttemptAction{
			Class:          "not_found",
			ShouldRetry:    false,
			ShouldFallback: rp.FallbackOnNotFound,
			TripBreaker:    true,
		}
	}
	if statusCode == http.StatusTooManyRequests {
		return AttemptAction{
			Class:          "rate_limit",
			ShouldRetry:    rp.Retry429,
			ShouldFallback: rp.FallbackOnRateLimit,
			TripBreaker:    true,
		}
	}
	if statusCode == http.StatusRequestTimeout || statusCode == http.StatusGatewayTimeout || lowerType == "timeout" {
		return AttemptAction{
			Class:          "timeout",
			ShouldRetry:    rp.RetryTimeout,
			ShouldFallback: rp.FallbackOnServerError,
			TripBreaker:    true,
		}
	}
	if statusCode >= 500 || statusCode == 0 {
		return AttemptAction{
			Class:          "server_error",
			ShouldRetry:    rp.Retry5xx,
			ShouldFallback: rp.FallbackOnServerError,
			TripBreaker:    true,
		}
	}
	return AttemptAction{
		Class:          "upstream_error",
		ShouldRetry:    false,
		ShouldFallback: rp.FallbackOnServerError,
		TripBreaker:    true,
	}
}

// RouteContext carries extracted request features for conditional rule matching & cost estimation.
type RouteContext struct {
	EstimatedInputTokens  int               `json:"estimated_input_tokens"`
	EstimatedOutputTokens int               `json:"estimated_output_tokens"`
	Stream                bool              `json:"stream"`
	RequiredCapabilities  []string          `json:"required_capabilities,omitempty"`
	ProjectID             string            `json:"project_id,omitempty"`
	ApplicationID         string            `json:"application_id,omitempty"`
	APIKeyID              string            `json:"api_key_id,omitempty"`
	Source                string            `json:"source,omitempty"`
	KeywordText           string            `json:"keyword_text,omitempty"`
	Metadata              map[string]string `json:"metadata,omitempty"`
	RandomSeed            int64             `json:"random_seed,omitempty"`
}

func EstimateTokensFromMessagesJSON(rawMessages json.RawMessage) (tokens int, hasVision bool, textSample string) {
	if len(rawMessages) == 0 {
		return 0, false, ""
	}
	var msgs []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(rawMessages, &msgs); err != nil {
		return utf8.RuneCount(rawMessages) / 4, false, ""
	}
	runes := 0
	var sample strings.Builder
	for _, m := range msgs {
		runes += 8
		var s string
		if err := json.Unmarshal(m.Content, &s); err == nil {
			runes += utf8.RuneCountInString(s)
			if m.Role == "user" && sample.Len() < 4000 {
				if sample.Len() > 0 {
					sample.WriteByte(' ')
				}
				sample.WriteString(s)
			}
			continue
		}
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(m.Content, &parts); err == nil {
			for _, p := range parts {
				if strings.Contains(strings.ToLower(p.Type), "image") {
					hasVision = true
					runes += 512
				}
				if p.Text != "" {
					runes += utf8.RuneCountInString(p.Text)
					if m.Role == "user" && sample.Len() < 4000 {
						if sample.Len() > 0 {
							sample.WriteByte(' ')
						}
						sample.WriteString(p.Text)
					}
				}
			}
		}
	}
	est := int(math.Ceil(float64(runes) / 3.0))
	if est < 1 && len(msgs) > 0 {
		est = 1
	}
	return est, hasVision, sample.String()
}

func ExtractChatRouteContext(meta chatRequestMeta, keyCtx apiKeyContext, headers http.Header) RouteContext {
	estIn, hasVision, sample := EstimateTokensFromMessagesJSON(meta.Messages)
	caps := make([]string, 0, 4)
	if hasVision {
		caps = append(caps, "vision")
	}
	estOut := 500
	if meta.Params != nil {
		if len(meta.Params.Tools) > 0 || meta.Params.ToolChoice != nil {
			caps = append(caps, "tools")
		}
		if meta.Params.ResponseFormat != nil {
			caps = append(caps, "json_mode")
		}
		if meta.Params.Reasoning != nil {
			caps = append(caps, "reasoning")
		}
		if meta.Params.MaxCompletionTokens != nil && *meta.Params.MaxCompletionTokens > 0 {
			estOut = *meta.Params.MaxCompletionTokens
		}
		if meta.Params.Metadata != nil {
			// Checked below when merging header metadata.
		}
	}
	metaMap := map[string]string{}
	if meta.Params != nil && meta.Params.Metadata != nil {
		for k, v := range *meta.Params.Metadata {
			key := strings.TrimSpace(k)
			if key == "" {
				continue
			}
			metaMap[key] = fmt.Sprint(v)
		}
	}
	for name, vals := range headers {
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, "x-omniflux-meta-") && len(vals) > 0 {
			metaKey := strings.TrimPrefix(lower, "x-omniflux-meta-")
			if metaKey != "" {
				metaMap[metaKey] = vals[0]
			}
		}
	}
	return RouteContext{
		EstimatedInputTokens:  estIn,
		EstimatedOutputTokens: estOut,
		Stream:                meta.Stream,
		RequiredCapabilities:  caps,
		ProjectID:             keyCtx.ProjectID.String(),
		ApplicationID:         keyCtx.ApplicationID.String(),
		APIKeyID:              keyCtx.KeyID.String(),
		Source:                "api",
		KeywordText:           sample,
		Metadata:              metaMap,
	}
}

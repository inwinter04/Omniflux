package dataplane

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// 数据面业务指标:用量定稿(finalize)是所有请求的唯一出口,在这里统一计数。
// provider/model_service 标签取自请求事实,数量有界,不会造成标签基数爆炸。
var (
	metricRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "omniflux_dataplane_requests_total",
		Help: "Total data-plane requests finalized by state",
	}, []string{"provider", "state"})

	metricRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "omniflux_dataplane_request_duration_seconds",
		Help:    "Data-plane request duration in seconds",
		Buckets: []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300},
	}, []string{"provider", "state"})

	metricTokensTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "omniflux_dataplane_tokens_total",
		Help: "Total tokens processed by type (input/output/cached/reasoning)",
	}, []string{"provider", "state", "type"})

	metricCostTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "omniflux_dataplane_cost_usd_total",
		Help: "Total estimated request cost in USD",
	}, []string{"provider", "state"})
)

// recordFinalizedMetrics 在用量定稿时更新业务指标。
func recordFinalizedMetrics(f finalUsage) {
	provider := "unknown"
	if f.Provider != nil && *f.Provider != "" {
		provider = *f.Provider
	}
	state := f.State
	metricRequestsTotal.WithLabelValues(provider, state).Inc()
	metricRequestDuration.WithLabelValues(provider, state).Observe(float64(f.TotalMS) / 1000)
	metricTokensTotal.WithLabelValues(provider, state, "input").Add(float64(f.InputTokens))
	metricTokensTotal.WithLabelValues(provider, state, "output").Add(float64(f.OutputTokens))
	metricTokensTotal.WithLabelValues(provider, state, "cached").Add(float64(f.CachedTokens))
	metricTokensTotal.WithLabelValues(provider, state, "reasoning").Add(float64(f.ReasoningTokens))
	if f.Cost > 0 {
		metricCostTotal.WithLabelValues(provider, state).Add(f.Cost)
	}
}
package httpx

import "testing"

func ptr(value float64) *float64 {
	return &value
}

func TestDeriveHealthStatus(t *testing.T) {
	tests := []struct {
		name             string
		resourceStatus   string
		requests         int64
		successRate      *float64
		activeCandidates int
		wantStatus       string
		wantReason       string
	}{
		{name: "disabled", resourceStatus: "disabled", requests: 100, successRate: ptr(0.8), activeCandidates: 0, wantStatus: "disabled", wantReason: "disabled"},
		{name: "no candidates", resourceStatus: "active", activeCandidates: 0, wantStatus: "no_candidates", wantReason: "no_candidates"},
		{name: "no traffic", resourceStatus: "active", requests: 0, successRate: nil, activeCandidates: 2, wantStatus: "no_data", wantReason: "no_traffic"},
		{name: "full success", resourceStatus: "active", requests: 48, successRate: ptr(1), activeCandidates: 2, wantStatus: "healthy", wantReason: "healthy"},
		{name: "boundary healthy", resourceStatus: "active", requests: 100, successRate: ptr(0.99), activeCandidates: 2, wantStatus: "healthy", wantReason: "healthy"},
		{name: "just below healthy", resourceStatus: "active", requests: 100, successRate: ptr(0.989), activeCandidates: 2, wantStatus: "degraded", wantReason: "passive_degraded_success_rate"},
		{name: "boundary degraded", resourceStatus: "active", requests: 100, successRate: ptr(0.95), activeCandidates: 2, wantStatus: "degraded", wantReason: "passive_degraded_success_rate"},
		{name: "low success", resourceStatus: "active", requests: 100, successRate: ptr(0.949), activeCandidates: 2, wantStatus: "unhealthy", wantReason: "passive_low_success_rate"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, reason := deriveHealthStatus(tt.resourceStatus, tt.requests, tt.successRate, tt.activeCandidates)
			if status != tt.wantStatus || reason != tt.wantReason {
				t.Fatalf("deriveHealthStatus() = (%q, %q), want (%q, %q)", status, reason, tt.wantStatus, tt.wantReason)
			}
		})
	}
}

func TestDeriveAccountHealthStatus(t *testing.T) {
	tests := []struct {
		name             string
		resourceStatus   string
		activeStatus     string
		activeReason     string
		requests         int64
		successRate      *float64
		activeCandidates int
		wantStatus       string
		wantReason       string
	}{
		{
			name:           "GLM active healthy with successful long-output traffic",
			resourceStatus: "active", activeStatus: "healthy", activeReason: "healthy",
			requests: 48, successRate: ptr(1), activeCandidates: 2,
			wantStatus: "healthy", wantReason: "healthy",
		},
		{
			name:           "passive degraded with active healthy",
			resourceStatus: "active", activeStatus: "healthy", activeReason: "healthy",
			requests: 100, successRate: ptr(0.97), activeCandidates: 2,
			wantStatus: "degraded", wantReason: "passive_degraded_success_rate",
		},
		{
			name:           "passive and active degraded",
			resourceStatus: "active", activeStatus: "degraded", activeReason: "slow_response",
			requests: 100, successRate: ptr(0.97), activeCandidates: 2,
			wantStatus: "degraded", wantReason: "active_and_passive_degraded",
		},
		{
			name:           "passive unhealthy with active healthy",
			resourceStatus: "active", activeStatus: "healthy", activeReason: "healthy",
			requests: 100, successRate: ptr(0.8), activeCandidates: 2,
			wantStatus: "unhealthy", wantReason: "passive_low_success_rate",
		},
		{
			name:           "passive and active unhealthy",
			resourceStatus: "active", activeStatus: "unhealthy", activeReason: "network_error",
			requests: 100, successRate: ptr(0.8), activeCandidates: 2,
			wantStatus: "unhealthy", wantReason: "active_and_passive_unhealthy",
		},
		{
			name:           "active network failure with no traffic",
			resourceStatus: "active", activeStatus: "unhealthy", activeReason: "network_error",
			requests: 0, successRate: nil, activeCandidates: 2,
			wantStatus: "unhealthy", wantReason: "active_probe_unhealthy",
		},
		{
			name:           "active network failure conflicts with successful traffic",
			resourceStatus: "active", activeStatus: "unhealthy", activeReason: "network_error",
			requests: 48, successRate: ptr(1), activeCandidates: 2,
			wantStatus: "degraded", wantReason: "probe_traffic_mismatch",
		},
		{
			name:           "rejected credentials remain unhealthy with successful traffic",
			resourceStatus: "active", activeStatus: "unhealthy", activeReason: "credentials_rejected",
			requests: 48, successRate: ptr(1), activeCandidates: 2,
			wantStatus: "unhealthy", wantReason: "active_probe_unhealthy",
		},
		{
			name:           "suspected v1 suffix remains unhealthy with successful traffic",
			resourceStatus: "active", activeStatus: "unhealthy", activeReason: "v1_suffix_suspected",
			requests: 48, successRate: ptr(1), activeCandidates: 2,
			wantStatus: "unhealthy", wantReason: "active_probe_unhealthy",
		},
		{
			name:           "active degraded with successful traffic",
			resourceStatus: "active", activeStatus: "degraded", activeReason: "slow_response",
			requests: 48, successRate: ptr(1), activeCandidates: 2,
			wantStatus: "degraded", wantReason: "active_probe_degraded",
		},
		{
			name:           "unsupported probe with successful traffic",
			resourceStatus: "active", activeStatus: "unsupported", activeReason: "",
			requests: 48, successRate: ptr(1), activeCandidates: 2,
			wantStatus: "healthy", wantReason: "healthy",
		},
		{
			name:           "unsupported probe with no traffic and no candidates",
			resourceStatus: "active", activeStatus: "unsupported", activeReason: "",
			requests: 0, successRate: nil, activeCandidates: 0,
			wantStatus: "no_candidates", wantReason: "no_candidates",
		},
		{
			name:           "unsupported probe with no traffic and candidates",
			resourceStatus: "active", activeStatus: "unsupported", activeReason: "",
			requests: 0, successRate: nil, activeCandidates: 2,
			wantStatus: "no_data", wantReason: "no_data",
		},
		{
			name:           "disabled account",
			resourceStatus: "disabled", activeStatus: "unhealthy", activeReason: "network_error",
			requests: 100, successRate: ptr(0.8), activeCandidates: 2,
			wantStatus: "disabled", wantReason: "disabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, reason := deriveAccountHealthStatus(
				tt.resourceStatus, tt.activeStatus, tt.activeReason,
				tt.requests, tt.successRate, tt.activeCandidates,
			)
			if status != tt.wantStatus || reason != tt.wantReason {
				t.Fatalf("deriveAccountHealthStatus() = (%q, %q), want (%q, %q)", status, reason, tt.wantStatus, tt.wantReason)
			}
		})
	}
}

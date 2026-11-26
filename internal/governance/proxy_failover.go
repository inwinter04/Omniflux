package governance

import (
	"sort"
	"time"

	"github.com/google/uuid"
)

type ProxyStatus string

const (
	ProxyStatusUnknown   ProxyStatus = "unknown"
	ProxyStatusHealthy   ProxyStatus = "healthy"
	ProxyStatusSuspect   ProxyStatus = "suspect"
	ProxyStatusUnhealthy ProxyStatus = "unhealthy"
)

type FailbackMode string

const (
	FailbackSticky        FailbackMode = "sticky"
	FailbackAutoAfterSoak FailbackMode = "auto_after_soak"
)

// ProxyPolicy holds anti-flapping thresholds and failover behavior for proxy governance.
type ProxyPolicy struct {
	Enabled           bool          `json:"enabled"`
	ProbeInterval     time.Duration `json:"-"`
	FastProbeInterval time.Duration `json:"-"`
	ProbeTimeout      time.Duration `json:"-"`
	FallThreshold     int           `json:"fail_threshold"`
	RiseThreshold     int           `json:"recover_threshold"`
	SwitchCooldown    time.Duration `json:"-"`
	FailbackMode      FailbackMode  `json:"failback_mode"`
	FailbackSoak      time.Duration `json:"-"`
}

func DefaultProxyPolicy() ProxyPolicy {
	return ProxyPolicy{
		Enabled:           true,
		ProbeInterval:     60 * time.Second,
		FastProbeInterval: 10 * time.Second,
		ProbeTimeout:      8 * time.Second,
		FallThreshold:     3,
		RiseThreshold:     3,
		SwitchCooldown:    300 * time.Second,
		FailbackMode:      FailbackSticky,
		FailbackSoak:      1800 * time.Second,
	}
}

// BindingState tracks a single proxy's priority binding and hysteresis health state for one provider.
type BindingState struct {
	ProxyID              uuid.UUID
	ProxyName            string
	ProxyEnabled         bool
	BindingEnabled       bool
	Priority             int
	CreatedAt            time.Time
	Status               ProxyStatus
	ConsecutiveFailures  int
	ConsecutiveSuccesses int
	EjectionCount        int
	CooldownUntil        *time.Time
	HealthySince         *time.Time
	LastLatencyMS        *int
	LastReason           string
	LastErrorDetail      string
	LastCheckedAt        *time.Time
}

func (b BindingState) IsUsable() bool {
	return b.ProxyEnabled && b.BindingEnabled
}

func (b BindingState) IsQuarantined(now time.Time) bool {
	return b.CooldownUntil != nil && now.Before(*b.CooldownUntil)
}

// SwitchDecision describes whether the active proxy for a provider should change.
type SwitchDecision struct {
	ShouldSwitch bool
	ToProxyID    *uuid.UUID
	Trigger      string // auto_failover, auto_failback, config_change
	Reason       string
	Detail       string
}

// ApplyProbeResult advances a proxy binding's hysteresis state machine based on one probe outcome.
func ApplyProbeResult(state BindingState, ok bool, latencyMS int, reason, detail string, now time.Time, policy ProxyPolicy) BindingState {
	fall := policy.FallThreshold
	if fall < 1 {
		fall = 1
	}
	rise := policy.RiseThreshold
	if rise < 1 {
		rise = 1
	}

	state.LastCheckedAt = &now
	state.LastLatencyMS = &latencyMS
	state.LastReason = reason
	state.LastErrorDetail = detail

	if ok {
		state.ConsecutiveFailures = 0
		state.ConsecutiveSuccesses++
		switch state.Status {
		case ProxyStatusUnhealthy:
			if state.ConsecutiveSuccesses >= rise && !state.IsQuarantined(now) {
				state.Status = ProxyStatusHealthy
				hs := now
				state.HealthySince = &hs
				state.CooldownUntil = nil
			}
		case ProxyStatusHealthy:
			if state.HealthySince == nil {
				hs := now
				state.HealthySince = &hs
			}
		default: // unknown or suspect
			state.Status = ProxyStatusHealthy
			hs := now
			state.HealthySince = &hs
		}
		return state
	}

	state.ConsecutiveSuccesses = 0
	state.ConsecutiveFailures++
	state.HealthySince = nil

	if state.Status == ProxyStatusUnhealthy || state.ConsecutiveFailures >= fall {
		if state.Status != ProxyStatusUnhealthy {
			state.EjectionCount++
			state.Status = ProxyStatusUnhealthy
		}
		base := policy.SwitchCooldown
		if base <= 0 {
			base = 60 * time.Second
		}
		shift := state.EjectionCount - 1
		if shift < 0 {
			shift = 0
		}
		if shift > 3 {
			shift = 3
		}
		quarantine := base << shift
		if quarantine > 30*time.Minute {
			quarantine = 30 * time.Minute
		}
		until := now.Add(quarantine)
		state.CooldownUntil = &until
		return state
	}

	state.Status = ProxyStatusSuspect
	return state
}

func sortBindings(bindings []BindingState) []BindingState {
	ordered := make([]BindingState, len(bindings))
	copy(ordered, bindings)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Priority != ordered[j].Priority {
			return ordered[i].Priority < ordered[j].Priority
		}
		if !ordered[i].CreatedAt.Equal(ordered[j].CreatedAt) {
			return ordered[i].CreatedAt.Before(ordered[j].CreatedAt)
		}
		return ordered[i].ProxyID.String() < ordered[j].ProxyID.String()
	})
	return ordered
}

// EvaluateSwitch decides whether a provider pool should stick with its active proxy or switch.
func EvaluateSwitch(activeProxyID *uuid.UUID, lastSwitchedAt *time.Time, bindings []BindingState, now time.Time, policy ProxyPolicy) SwitchDecision {
	ordered := sortBindings(bindings)
	usable := make([]BindingState, 0, len(ordered))
	for _, b := range ordered {
		if b.IsUsable() {
			usable = append(usable, b)
		}
	}

	if len(usable) == 0 {
		if activeProxyID != nil {
			return SwitchDecision{
				ShouldSwitch: true,
				ToProxyID:    nil,
				Trigger:      "config_change",
				Reason:       "no_enabled_proxies",
			}
		}
		return SwitchDecision{}
	}

	var active *BindingState
	if activeProxyID != nil {
		for i := range usable {
			if usable[i].ProxyID == *activeProxyID {
				active = &usable[i]
				break
			}
		}
	}

	if active == nil {
		best := pickBestAvailable(usable, now)
		target := best.ProxyID
		return SwitchDecision{
			ShouldSwitch: true,
			ToProxyID:    &target,
			Trigger:      "config_change",
			Reason:       "active_proxy_unavailable",
		}
	}

	if active.Status == ProxyStatusUnhealthy {
		for _, candidate := range usable {
			if candidate.ProxyID == active.ProxyID {
				continue
			}
			if candidate.Status == ProxyStatusHealthy && !candidate.IsQuarantined(now) {
				target := candidate.ProxyID
				return SwitchDecision{
					ShouldSwitch: true,
					ToProxyID:    &target,
					Trigger:      "auto_failover",
					Reason:       "active_proxy_unhealthy",
					Detail:       active.LastErrorDetail,
				}
			}
		}
		// All-Down Guard: 无可用健康备选时不切换，避免无意义换 IP。
		return SwitchDecision{}
	}

	if policy.FailbackMode != FailbackAutoAfterSoak {
		return SwitchDecision{}
	}
	if policy.SwitchCooldown > 0 && lastSwitchedAt != nil && now.Sub(*lastSwitchedAt) < policy.SwitchCooldown {
		return SwitchDecision{}
	}

	for _, candidate := range usable {
		if candidate.Priority >= active.Priority {
			break
		}
		if candidate.Status == ProxyStatusHealthy &&
			!candidate.IsQuarantined(now) &&
			candidate.HealthySince != nil &&
			now.Sub(*candidate.HealthySince) >= policy.FailbackSoak {
			target := candidate.ProxyID
			return SwitchDecision{
				ShouldSwitch: true,
				ToProxyID:    &target,
				Trigger:      "auto_failback",
				Reason:       "primary_soak_complete",
			}
		}
	}
	return SwitchDecision{}
}

func pickBestAvailable(usable []BindingState, now time.Time) BindingState {
	rank := func(b BindingState) int {
		switch {
		case b.Status == ProxyStatusHealthy && !b.IsQuarantined(now):
			return 0
		case b.Status == ProxyStatusHealthy:
			return 1
		case b.Status == ProxyStatusUnknown:
			return 2
		case b.Status == ProxyStatusSuspect:
			return 3
		default:
			return 4
		}
	}
	best := usable[0]
	bestRank := rank(best)
	for _, b := range usable[1:] {
		r := rank(b)
		if r < bestRank {
			best = b
			bestRank = r
		}
	}
	return best
}

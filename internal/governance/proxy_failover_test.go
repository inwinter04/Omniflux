package governance

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestApplyProbeResultAntiFlappingHysteresis(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	policy := DefaultProxyPolicy() // Fall=3, Rise=3, SwitchCooldown=300s
	state := BindingState{
		ProxyID:        uuid.New(),
		ProxyName:      "primary",
		ProxyEnabled:   true,
		BindingEnabled: true,
		Priority:       1,
		Status:         ProxyStatusHealthy,
		HealthySince:   &now,
	}

	// Single and double failures only mark suspect, never unhealthy.
	state = ApplyProbeResult(state, false, 1200, "proxy_error", "timeout", now.Add(10*time.Second), policy)
	if state.Status != ProxyStatusSuspect || state.ConsecutiveFailures != 1 {
		t.Fatalf("after 1st fail = %+v", state)
	}
	state = ApplyProbeResult(state, false, 1200, "proxy_error", "timeout", now.Add(20*time.Second), policy)
	if state.Status != ProxyStatusSuspect || state.ConsecutiveFailures != 2 {
		t.Fatalf("after 2nd fail = %+v", state)
	}

	// Brief jitter recovers cleanly on next success.
	recovered := ApplyProbeResult(state, true, 90, "ok", "", now.Add(30*time.Second), policy)
	if recovered.Status != ProxyStatusHealthy || recovered.ConsecutiveFailures != 0 || recovered.EjectionCount != 0 {
		t.Fatalf("expected clean recovery from jitter, got %+v", recovered)
	}

	// 3rd consecutive failure triggers unhealthy + quarantine.
	state = ApplyProbeResult(state, false, 1200, "proxy_error", "timeout", now.Add(30*time.Second), policy)
	if state.Status != ProxyStatusUnhealthy || state.EjectionCount != 1 || state.CooldownUntil == nil {
		t.Fatalf("after 3rd fail = %+v", state)
	}

	// Successes before quarantine expires and < RiseThreshold keep it unhealthy.
	state = ApplyProbeResult(state, true, 50, "ok", "", now.Add(60*time.Second), policy)
	state = ApplyProbeResult(state, true, 50, "ok", "", now.Add(70*time.Second), policy)
	state = ApplyProbeResult(state, true, 50, "ok", "", now.Add(80*time.Second), policy)
	if state.Status != ProxyStatusUnhealthy {
		t.Fatalf("expected unhealthy while inside quarantine, got %s", state.Status)
	}

	// After quarantine expires with consecutive successes >= RiseThreshold, recovers to healthy.
	state = ApplyProbeResult(state, true, 50, "ok", "", now.Add(400*time.Second), policy)
	if state.Status != ProxyStatusHealthy || state.CooldownUntil != nil {
		t.Fatalf("expected recovery after quarantine + rise threshold, got %+v", state)
	}
}

func TestEvaluateSwitchStickyAndAllDownGuard(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	policy := DefaultProxyPolicy()
	idA := uuid.New()
	idB := uuid.New()
	later := now.Add(10 * time.Second)

	primary := BindingState{
		ProxyID: idA, ProxyName: "A", ProxyEnabled: true, BindingEnabled: true,
		Priority: 1, CreatedAt: now, Status: ProxyStatusSuspect, ConsecutiveFailures: 2,
	}
	standby := BindingState{
		ProxyID: idB, ProxyName: "B", ProxyEnabled: true, BindingEnabled: true,
		Priority: 2, CreatedAt: later, Status: ProxyStatusHealthy, HealthySince: &now,
	}

	// Suspect does not trigger switch.
	dec := EvaluateSwitch(&idA, nil, []BindingState{primary, standby}, now, policy)
	if dec.ShouldSwitch {
		t.Fatalf("suspect should not switch: %+v", dec)
	}

	// Unhealthy + unhealthy standby -> All-Down Guard stays on A.
	primary.Status = ProxyStatusUnhealthy
	standbyDown := standby
	standbyDown.Status = ProxyStatusUnhealthy
	dec = EvaluateSwitch(&idA, nil, []BindingState{primary, standbyDown}, now, policy)
	if dec.ShouldSwitch {
		t.Fatalf("all-down guard violated: %+v", dec)
	}

	// Unhealthy + healthy standby -> auto_failover to B.
	dec = EvaluateSwitch(&idA, nil, []BindingState{primary, standby}, now, policy)
	if !dec.ShouldSwitch || dec.ToProxyID == nil || *dec.ToProxyID != idB || dec.Trigger != "auto_failover" {
		t.Fatalf("expected auto_failover to B, got %+v", dec)
	}

	// Sticky mode: when primary A recovers, stay on B!
	primaryRecovered := primary
	primaryRecovered.Status = ProxyStatusHealthy
	hs := now.Add(-2 * time.Hour)
	primaryRecovered.HealthySince = &hs
	lastSwitch := now.Add(-time.Hour)
	dec = EvaluateSwitch(&idB, &lastSwitch, []BindingState{primaryRecovered, standby}, now, policy)
	if dec.ShouldSwitch {
		t.Fatalf("sticky mode must not auto failback: %+v", dec)
	}

	// auto_after_soak mode: fails back only after soak + cooldown elapsed.
	soakPolicy := policy
	soakPolicy.FailbackMode = FailbackAutoAfterSoak
	soakPolicy.FailbackSoak = 30 * time.Minute

	recentHealthy := now.Add(-5 * time.Minute)
	primaryRecovered.HealthySince = &recentHealthy
	dec = EvaluateSwitch(&idB, &lastSwitch, []BindingState{primaryRecovered, standby}, now, soakPolicy)
	if dec.ShouldSwitch {
		t.Fatalf("must not failback before soak duration elapsed: %+v", dec)
	}

	longHealthy := now.Add(-45 * time.Minute)
	primaryRecovered.HealthySince = &longHealthy
	dec = EvaluateSwitch(&idB, &lastSwitch, []BindingState{primaryRecovered, standby}, now, soakPolicy)
	if !dec.ShouldSwitch || dec.ToProxyID == nil || *dec.ToProxyID != idA || dec.Trigger != "auto_failback" {
		t.Fatalf("expected auto_failback to A after soak, got %+v", dec)
	}
}

func TestEvaluateSwitchExponentialBackoffAndConfigFallback(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	policy := DefaultProxyPolicy()
	policy.SwitchCooldown = 100 * time.Second
	idA := uuid.New()
	idB := uuid.New()

	// Exponential quarantine check: 1st=100s, 2nd=200s, 3rd=400s.
	b := BindingState{ProxyID: idA, ProxyEnabled: true, BindingEnabled: true, Status: ProxyStatusHealthy}
	for i := 0; i < 3; i++ {
		b = ApplyProbeResult(b, false, 500, "proxy_error", "err", now, policy)
	}
	if got := b.CooldownUntil.Sub(now); got != 100*time.Second {
		t.Fatalf("1st ejection cooldown = %v, want 100s", got)
	}
	b.Status = ProxyStatusSuspect
	b.ConsecutiveFailures = 2
	b = ApplyProbeResult(b, false, 500, "proxy_error", "err", now, policy)
	if got := b.CooldownUntil.Sub(now); got != 200*time.Second {
		t.Fatalf("2nd ejection cooldown = %v, want 200s", got)
	}

	// Disabled active proxy switches via config_change to standby.
	disabledA := BindingState{ProxyID: idA, ProxyEnabled: false, BindingEnabled: true, Priority: 1, Status: ProxyStatusHealthy}
	healthyB := BindingState{ProxyID: idB, ProxyEnabled: true, BindingEnabled: true, Priority: 2, Status: ProxyStatusHealthy}
	dec := EvaluateSwitch(&idA, nil, []BindingState{disabledA, healthyB}, now, policy)
	if !dec.ShouldSwitch || dec.Trigger != "config_change" || dec.ToProxyID == nil || *dec.ToProxyID != idB {
		t.Fatalf("expected config_change to B, got %+v", dec)
	}

	// All disabled -> ToProxyID == nil (direct).
	healthyB.BindingEnabled = false
	dec = EvaluateSwitch(&idA, nil, []BindingState{disabledA, healthyB}, now, policy)
	if !dec.ShouldSwitch || dec.Trigger != "config_change" || dec.ToProxyID != nil {
		t.Fatalf("expected config_change to nil/direct, got %+v", dec)
	}
}

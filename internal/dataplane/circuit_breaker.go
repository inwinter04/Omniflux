package dataplane

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type CircuitState struct {
	Open          bool       `json:"open"`
	Failures      int        `json:"failures"`
	CooldownUntil *time.Time `json:"cooldown_until,omitempty"`
}

type localBreakerEntry struct {
	failures      int
	cooldownUntil time.Time
	updatedAt     time.Time
}

type cachedServiceStats struct {
	expiresAt time.Time
	health    map[uuid.UUID]candidateHealth
}

// CandidateBreaker maintains short-term candidate failure circuit state (Redis primary, memory fallback),
// round-robin counters, and short-lived hot telemetry cache to avoid heavy per-request SQL aggregation.
type CandidateBreaker struct {
	redis *redis.Client

	mu       sync.RWMutex
	local    map[string]localBreakerEntry
	rrLocal  map[string]*atomic.Uint64
	statsMu  sync.RWMutex
	statsMap map[string]cachedServiceStats
}

func NewCandidateBreaker(client *redis.Client) *CandidateBreaker {
	return &CandidateBreaker{
		redis:    client,
		local:    make(map[string]localBreakerEntry),
		rrLocal:  make(map[string]*atomic.Uint64),
		statsMap: make(map[string]cachedServiceStats),
	}
}

func breakerKey(candidateID string) string {
	return "omniflux:cb:" + candidateID
}

func (b *CandidateBreaker) States(ctx context.Context, candidateIDs []uuid.UUID, cfg CircuitBreakerPolicy) map[uuid.UUID]CircuitState {
	out := make(map[uuid.UUID]CircuitState, len(candidateIDs))
	if b == nil || !cfg.Enabled || len(candidateIDs) == 0 {
		return out
	}
	now := time.Now()
	if b.redis != nil {
		keys := make([]string, len(candidateIDs))
		for i, id := range candidateIDs {
			keys[i] = breakerKey(id.String())
		}
		vals, err := b.redis.MGet(ctx, keys...).Result()
		if err == nil && len(vals) == len(candidateIDs) {
			for i, raw := range vals {
				id := candidateIDs[i]
				if raw == nil {
					continue
				}
				text := fmt.Sprint(raw)
				st := parseBreakerValue(text, now)
				if st.Open || st.Failures > 0 {
					out[id] = st
				}
			}
			return out
		}
	}

	b.mu.RLock()
	for _, id := range candidateIDs {
		if entry, ok := b.local[id.String()]; ok {
			open := !entry.cooldownUntil.IsZero() && now.Before(entry.cooldownUntil)
			if open || entry.failures > 0 {
				var cd *time.Time
				if open {
					t := entry.cooldownUntil
					cd = &t
				}
				out[id] = CircuitState{Open: open, Failures: entry.failures, CooldownUntil: cd}
			}
		}
	}
	b.mu.RUnlock()
	return out
}

func (b *CandidateBreaker) RecordSuccess(ctx context.Context, candidateID string) {
	if b == nil || candidateID == "" {
		return
	}
	if b.redis != nil {
		_ = b.redis.Del(ctx, breakerKey(candidateID)).Err()
	}
	b.mu.Lock()
	delete(b.local, candidateID)
	b.mu.Unlock()
}

func (b *CandidateBreaker) RecordFailure(ctx context.Context, candidateID string, statusCode int, errCode, errType string, rp RetryPolicy, cfg CircuitBreakerPolicy) {
	if b == nil || !cfg.Enabled || candidateID == "" {
		return
	}
	action := ClassifyAttemptOutcome(statusCode, errCode, errType, rp)
	if !action.TripBreaker {
		return
	}
	threshold := cfg.FailureThreshold
	if threshold < 1 {
		threshold = 5
	}
	cooldown := time.Duration(cfg.CooldownSeconds) * time.Second
	if cooldown < 5*time.Second {
		cooldown = 30 * time.Second
	}
	now := time.Now()

	b.mu.Lock()
	entry := b.local[candidateID]
	entry.failures++
	entry.updatedAt = now
	if entry.failures >= threshold {
		entry.cooldownUntil = now.Add(cooldown)
	}
	b.local[candidateID] = entry
	b.mu.Unlock()

	if b.redis != nil {
		key := breakerKey(candidateID)
		untilUnix := int64(0)
		if !entry.cooldownUntil.IsZero() {
			untilUnix = entry.cooldownUntil.UnixMilli()
		}
		val := fmt.Sprintf("%d|%d", entry.failures, untilUnix)
		ttl := cooldown + 60*time.Second
		_ = b.redis.Set(ctx, key, val, ttl).Err()
	}
}

func (b *CandidateBreaker) NextRoundRobin(ctx context.Context, serviceID string) uint64 {
	if b == nil || serviceID == "" {
		return 0
	}
	if b.redis != nil {
		if val, err := b.redis.Incr(ctx, "omniflux:rr:"+serviceID).Result(); err == nil && val > 0 {
			return uint64(val - 1)
		}
	}
	b.mu.Lock()
	ctr, ok := b.rrLocal[serviceID]
	if !ok {
		ctr = &atomic.Uint64{}
		b.rrLocal[serviceID] = ctr
	}
	b.mu.Unlock()
	return ctr.Add(1) - 1
}

func (b *CandidateBreaker) CachedStats(orgID uuid.UUID, candidateIDs []uuid.UUID) (map[uuid.UUID]candidateHealth, bool) {
	if b == nil || len(candidateIDs) == 0 {
		return nil, false
	}
	key := statsCacheKey(orgID, candidateIDs)
	b.statsMu.RLock()
	cached, ok := b.statsMap[key]
	b.statsMu.RUnlock()
	if !ok || time.Now().After(cached.expiresAt) {
		return nil, false
	}
	return cached.health, true
}

func (b *CandidateBreaker) StoreStats(orgID uuid.UUID, candidateIDs []uuid.UUID, health map[uuid.UUID]candidateHealth) {
	if b == nil || len(candidateIDs) == 0 {
		return
	}
	copied := make(map[uuid.UUID]candidateHealth, len(health))
	for k, v := range health {
		copied[k] = v
	}
	key := statsCacheKey(orgID, candidateIDs)
	b.statsMu.Lock()
	if len(b.statsMap) > 512 {
		now := time.Now()
		for k, v := range b.statsMap {
			if now.After(v.expiresAt) {
				delete(b.statsMap, k)
			}
		}
	}
	b.statsMap[key] = cachedServiceStats{
		expiresAt: time.Now().Add(15 * time.Second),
		health:    copied,
	}
	b.statsMu.Unlock()
}

func statsCacheKey(orgID uuid.UUID, ids []uuid.UUID) string {
	b := make([]byte, 0, 40+len(ids)*37)
	b = append(b, orgID.String()...)
	for _, id := range ids {
		b = append(b, '|')
		b = append(b, id.String()...)
	}
	return string(b)
}

func parseBreakerValue(text string, now time.Time) CircuitState {
	var failures int
	var untilMS int64
	for i := 0; i < len(text); i++ {
		if text[i] == '|' {
			failures, _ = strconv.Atoi(text[:i])
			untilMS, _ = strconv.ParseInt(text[i+1:], 10, 64)
			break
		}
	}
	if untilMS > 0 {
		t := time.UnixMilli(untilMS)
		if now.Before(t) {
			return CircuitState{Open: true, Failures: failures, CooldownUntil: &t}
		}
	}
	return CircuitState{Open: false, Failures: failures}
}

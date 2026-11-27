package dataplane

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDeriveSessionKeyStrategiesAndPrecedence(t *testing.T) {
	t.Run("header precedence under auto", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		req.Header.Set("X-Client-Session-Id", "client-fallback")
		req.Header.Set("X-Session-Id", "x-session")
		req.Header.Set("Session-Id", "codex-primary")

		if got := DeriveSessionKey(req, "cache-1", "resp-1", "user-1", "auto"); got != "codex-primary" {
			t.Fatalf("DeriveSessionKey = %q, want codex-primary", got)
		}

		req.Header.Del("Session-Id")
		if got := DeriveSessionKey(req, "cache-1", "resp-1", "user-1", "auto"); got != "x-session" {
			t.Fatalf("DeriveSessionKey without Session-Id = %q, want x-session", got)
		}
	})

	t.Run("auto fallback chain without headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		if got := DeriveSessionKey(req, "prompt-key-1", "resp-prev-1", "user-1", "auto"); got != "prompt-key-1" {
			t.Fatalf("DeriveSessionKey = %q, want prompt-key-1", got)
		}
		if got := DeriveSessionKey(req, "", "resp-prev-1", "user-1", "auto"); got != "resp-prev-1" {
			t.Fatalf("DeriveSessionKey = %q, want resp-prev-1", got)
		}
		if got := DeriveSessionKey(req, "", "", "user-1", "auto"); got != "user-1" {
			t.Fatalf("DeriveSessionKey = %q, want user-1", got)
		}
	})

	t.Run("user_id ignores headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
		req.Header.Set("Session-Id", "hdr-1")
		if got := DeriveSessionKey(req, "cache-2", "resp-2", "meta-user-9", "user_id"); got != "meta-user-9" {
			t.Fatalf("DeriveSessionKey(user_id) = %q, want meta-user-9", got)
		}
		if got := DeriveSessionKey(req, "cache-2", "resp-2", "", "user_id"); got != "cache-2" {
			t.Fatalf("DeriveSessionKey(user_id) = %q, want cache-2", got)
		}
	})

	t.Run("header_only ignores body identifiers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		if got := DeriveSessionKey(req, "cache-3", "resp-3", "user-3", "header_only"); got != "" {
			t.Fatalf("DeriveSessionKey(header_only) = %q, want empty string", got)
		}
		req.Header.Set("Conversation-Id", "conv-77")
		if got := DeriveSessionKey(req, "cache-3", "resp-3", "user-3", "header_only"); got != "conv-77" {
			t.Fatalf("DeriveSessionKey(header_only) = %q, want conv-77", got)
		}
	})
}

func TestSessionAffinityStoreMemoryAndTTLExpiry(t *testing.T) {
	store := newSessionAffinityStore(nil)
	svcID := uuid.NewString()

	store.Set(t.Context(), svcID, "session-a", "cand-1", time.Minute)
	if got := store.Get(t.Context(), svcID, "session-a"); got != "cand-1" {
		t.Fatalf("Get = %q, want cand-1", got)
	}
	if got := store.Get(t.Context(), svcID, "session-b"); got != "" {
		t.Fatalf("other session = %q, want empty", got)
	}
	if got := store.Get(t.Context(), uuid.NewString(), "session-a"); got != "" {
		t.Fatalf("other service = %q, want empty", got)
	}

	// Simulate an expired entry in memory map and verify Get evicts it.
	expiredKey := affinityStorageKey(svcID, "session-expired")
	store.mu.Lock()
	store.local[expiredKey] = affinityEntry{
		candidateID: "cand-old",
		expiresAt:   time.Now().Add(-time.Second),
	}
	store.mu.Unlock()

	if got := store.Get(t.Context(), svcID, "session-expired"); got != "" {
		t.Fatalf("expired Get = %q, want empty", got)
	}
	store.mu.RLock()
	_, stillPresent := store.local[expiredKey]
	store.mu.RUnlock()
	if stillPresent {
		t.Fatal("expected expired affinity entry to be removed on read")
	}
}

func TestEvaluateRouteSessionAffinitySameTierAndFailover(t *testing.T) {
	tier1A := testCandidate("tier1-a", 1, 1, 10, 10, 0.9)
	tier1B := testCandidate("tier1-b", 1, 1, 20, 20, 0.5) // lower score in same priority tier 1
	tier2Cheap := testCandidate("tier2-cheap", 2, 1, 1, 1, 0.99)

	policy := defaultPolicy
	policy.Strategy = StrategyCostFirst
	policy.Weights = PresetWeights(StrategyCostFirst)
	policy.FallbackLimit = 0
	policy.SessionAffinity = SessionAffinityPolicy{
		Enabled:     true,
		TTLSeconds:  1800,
		KeyStrategy: "auto",
	}

	t.Run("same priority tier sticky candidate promoted", func(t *testing.T) {
		decision := EvaluateRoute(
			[]candidate{tier1A, tier1B, tier2Cheap},
			policy,
			RouteContext{PreferredCandidateID: tier1B.ID, RandomSeed: 42},
			noHealthScores(),
			nil,
			0,
		)
		if len(decision.Chain) != 3 {
			t.Fatalf("chain len = %d, want 3", len(decision.Chain))
		}
		if decision.Chain[0].ID != tier1B.ID {
			t.Fatalf("top candidate = %s, want sticky %s", decision.Chain[0].ID, tier1B.ID)
		}
		if !decision.AffinityHit || decision.StickyCandidateID != tier1B.ID {
			t.Fatalf("expected AffinityHit=true StickyCandidateID=%s, got hit=%v sticky=%s",
				tier1B.ID, decision.AffinityHit, decision.StickyCandidateID)
		}
	})

	t.Run("lower priority sticky candidate does not bypass tier 1", func(t *testing.T) {
		decision := EvaluateRoute(
			[]candidate{tier1A, tier1B, tier2Cheap},
			policy,
			RouteContext{PreferredCandidateID: tier2Cheap.ID, RandomSeed: 42},
			noHealthScores(),
			nil,
			0,
		)
		if decision.Chain[0].Priority != 1 {
			t.Fatalf("priority guard broken: top priority = %d, want 1", decision.Chain[0].Priority)
		}
		if decision.AffinityHit {
			t.Fatal("did not expect cross-tier affinity hit")
		}
	})

	t.Run("open circuit sticky candidate fails over cleanly", func(t *testing.T) {
		scores := &policyScores{
			Health: map[uuid.UUID]candidateHealth{},
			Circuits: map[uuid.UUID]CircuitState{
				uuid.MustParse(tier1B.ID): {Open: true},
			},
		}
		decision := EvaluateRoute(
			[]candidate{tier1A, tier1B},
			policy,
			RouteContext{PreferredCandidateID: tier1B.ID, RandomSeed: 42},
			scores,
			nil,
			0,
		)
		if len(decision.Chain) != 1 || decision.Chain[0].ID != tier1A.ID {
			t.Fatalf("chain = %+v, want [%s]", decision.Chain, tier1A.ID)
		}
		if decision.AffinityHit {
			t.Fatal("did not expect affinity hit while preferred circuit is open")
		}
	})

	t.Run("disabled session affinity does not alter rank", func(t *testing.T) {
		disabledPolicy := policy
		disabledPolicy.SessionAffinity.Enabled = false
		decision := EvaluateRoute(
			[]candidate{tier1A, tier1B},
			disabledPolicy,
			RouteContext{PreferredCandidateID: tier1B.ID, RandomSeed: 42},
			noHealthScores(),
			nil,
			0,
		)
		if decision.Chain[0].ID != tier1A.ID {
			t.Fatalf("top candidate = %s, want %s when affinity disabled", decision.Chain[0].ID, tier1A.ID)
		}
		if decision.AffinityHit {
			t.Fatal("did not expect affinity hit when disabled")
		}
	})
}

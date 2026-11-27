package dataplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type affinityEntry struct {
	candidateID string
	expiresAt   time.Time
}

// sessionAffinityStore maintains sticky session -> candidate mappings in Redis
// with an in-memory TTL fallback so multi-turn coding and agent sessions stay pinned to the same KV-cache node.
type sessionAffinityStore struct {
	redis *redis.Client

	mu          sync.RWMutex
	local       map[string]affinityEntry
	lastCleanup time.Time
}

func newSessionAffinityStore(client *redis.Client) *sessionAffinityStore {
	return &sessionAffinityStore{
		redis: client,
		local: make(map[string]affinityEntry),
	}
}

func affinityStorageKey(serviceID, sessionKey string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(sessionKey)))
	return "omniflux:aff:" + strings.TrimSpace(serviceID) + ":" + hex.EncodeToString(sum[:])
}

func (s *sessionAffinityStore) Get(ctx context.Context, serviceID, sessionKey string) string {
	if s == nil || strings.TrimSpace(serviceID) == "" || strings.TrimSpace(sessionKey) == "" {
		return ""
	}
	storageKey := affinityStorageKey(serviceID, sessionKey)
	if s.redis != nil {
		rCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		val, err := s.redis.Get(rCtx, storageKey).Result()
		if err == nil {
			return val
		}
		if err == redis.Nil {
			return ""
		}
	}

	now := time.Now()
	s.mu.RLock()
	entry, ok := s.local[storageKey]
	s.mu.RUnlock()
	if !ok || now.After(entry.expiresAt) {
		if ok {
			s.mu.Lock()
			if cur, exists := s.local[storageKey]; exists && !now.Before(cur.expiresAt) {
				delete(s.local, storageKey)
			}
			s.mu.Unlock()
		}
		return ""
	}
	return entry.candidateID
}

func (s *sessionAffinityStore) Set(ctx context.Context, serviceID, sessionKey, candidateID string, ttl time.Duration) {
	if s == nil || strings.TrimSpace(serviceID) == "" || strings.TrimSpace(sessionKey) == "" || strings.TrimSpace(candidateID) == "" || ttl <= 0 {
		return
	}
	storageKey := affinityStorageKey(serviceID, sessionKey)
	if s.redis != nil {
		sCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if err := s.redis.Set(sCtx, storageKey, candidateID, ttl).Err(); err == nil {
			return
		}
	}

	now := time.Now()
	s.mu.Lock()
	if len(s.local) > 1000 || now.Sub(s.lastCleanup) > time.Minute {
		for k, v := range s.local {
			if !now.Before(v.expiresAt) {
				delete(s.local, k)
			}
		}
		s.lastCleanup = now
	}
	s.local[storageKey] = affinityEntry{
		candidateID: candidateID,
		expiresAt:   now.Add(ttl),
	}
	s.mu.Unlock()
}

// DeriveSessionKey resolves the sticky session identifier according to the route policy's key strategy.
func DeriveSessionKey(r *http.Request, promptCacheKey, previousRespID, metadataUserID, strategy string) string {
	var headerKey string
	if r != nil {
		for _, name := range []string{
			"Session-Id",
			"X-Session-Id",
			"X-Codex-Session-Id",
			"Conversation-Id",
			"X-Omniflux-Session-Id",
			"X-Client-Session-Id",
		} {
			if v := strings.TrimSpace(r.Header.Get(name)); v != "" {
				headerKey = v
				break
			}
		}
	}

	switch strings.TrimSpace(strategy) {
	case "header_only":
		return headerKey
	case "user_id":
		for _, cand := range []string{metadataUserID, promptCacheKey, previousRespID} {
			if v := strings.TrimSpace(cand); v != "" {
				return v
			}
		}
		return ""
	default: // "auto"
		for _, cand := range []string{headerKey, promptCacheKey, previousRespID, metadataUserID} {
			if v := strings.TrimSpace(cand); v != "" {
				return v
			}
		}
		return ""
	}
}
